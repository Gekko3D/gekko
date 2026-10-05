package derived

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// SaveImportedWorldPageBake publishes immutable generation-qualified payloads and
// regenerated normals before atomically replacing the manifest. Failed publication
// may leave unreferenced generation files, but cannot change a previous generation.
func SaveImportedWorldPageBake(manifestPath string, bake *ImportedWorldPageBake) error {
	if strings.TrimSpace(manifestPath) == "" {
		return fmt.Errorf("page manifest path is empty")
	}
	if bake == nil || bake.Manifest == nil || bake.fingerprint == "" {
		return fmt.Errorf("page bake is nil or unowned")
	}
	fingerprint, e := pageFingerprint(bake)
	if e != nil {
		return e
	}
	if fingerprint != bake.fingerprint {
		return fmt.Errorf("page bake draft changed; rebuild its generation")
	}
	d, e := pageClone(bake.Manifest)
	if e != nil {
		return e
	}
	chunks := map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{}
	for coord, c := range bake.Chunks {
		cloned, e := pageClone(c)
		if e != nil {
			return e
		}
		chunks[coord] = cloned
	}
	pages := map[string]*content.ImportedWorldChunkDef{}
	for path, c := range bake.PageChunks {
		cloned, e := pageClone(c)
		if e != nil {
			return e
		}
		pages[path] = cloned
	}
	// All draft ownership/geometry has been checked before creating staging files.
	staging, e := os.MkdirTemp("", "gekko-page-bake-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(staging)
	staged := map[string]string{}
	saveChunk := func(path string, c *content.ImportedWorldChunkDef) error {
		target := filepath.Join(staging, filepath.FromSlash(path))
		_, e := content.SaveImportedWorldChunkWithOptionsResult(target, c, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
		if e == nil {
			staged[path] = target
		}
		return e
	}
	for i := range d.Entries {
		entry := &d.Entries[i]
		c := chunks[entry.Coord]
		if c == nil {
			return fmt.Errorf("missing full payload")
		}
		if e := saveChunk(entry.ChunkPath, c); e != nil {
			return e
		}
		entry.PayloadKind = c.PayloadKind
		entry.PayloadHash = c.PayloadHash
		entry.PayloadSizeBytes = c.PayloadSizeBytes
		entry.OccupiedSectorCount, entry.OccupiedBrickCount = pageOccupiedCosts(c)
	}
	paths := make([]string, 0, len(pages))
	for p := range pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if e := saveChunk(path, pages[path]); e != nil {
			return e
		}
	}
	saveAux := func(path string, c *content.ImportedWorldChunkDef, neighbors bool) (*content.ImportedWorldChunkAuxRefDef, error) {
		aux := BuildImportedWorldChunkAux(c, chunks, c.PayloadHash, c.PayloadSizeBytes, neighbors)
		if aux == nil {
			return nil, nil
		}
		auxPath := content.DefaultImportedWorldChunkAuxPath(path)
		target := filepath.Join(staging, filepath.FromSlash(auxPath))
		if e := content.SaveImportedWorldChunkAux(target, aux); e != nil {
			return nil, e
		}
		staged[auxPath] = target
		return content.ImportedWorldChunkAuxRef(auxPath, aux), nil
	}
	for i := range d.Entries {
		entry := &d.Entries[i]
		entry.Aux, e = saveAux(entry.ChunkPath, chunks[entry.Coord], true)
		if e != nil {
			return e
		}
	}
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.Level == content.StreamPageLevelLeaf {
			if len(p.LeafEntryIndices) != 1 {
				return fmt.Errorf("invalid leaf page")
			}
			entry := d.Entries[p.LeafEntryIndices[0]]
			p.Payload.Kind = entry.PayloadKind
			p.Payload.PayloadHash = entry.PayloadHash
			p.Payload.PayloadSizeBytes = entry.PayloadSizeBytes
			p.Payload.Aux = entry.Aux
			p.Payload.OccupiedSectorCount, p.Payload.OccupiedBrickCount = entry.OccupiedSectorCount, entry.OccupiedBrickCount
		} else {
			c := pages[p.Payload.Path]
			if c == nil {
				return fmt.Errorf("missing page payload")
			}
			p.Payload.Kind = c.PayloadKind
			p.Payload.PayloadHash = c.PayloadHash
			p.Payload.PayloadSizeBytes = c.PayloadSizeBytes
			p.Payload.OccupiedSectorCount, p.Payload.OccupiedBrickCount = pageOccupiedCosts(c)
			p.Payload.Aux, e = saveAux(p.Payload.Path, c, false)
			if e != nil {
				return e
			}
		}
	}
	if _, e := content.ValidateImportedWorldV3(d); e != nil {
		return e
	}
	// Save in staging first, so JSON validation cannot fail after publication begins.
	stageManifest := filepath.Join(staging, "manifest.gkworld")
	if e := content.SaveImportedWorld(stageManifest, d); e != nil {
		return e
	}
	if validation := content.ValidateImportedWorld(d, content.ImportedWorldValidationOptions{DocumentPath: stageManifest}); validation.HasErrors() {
		return fmt.Errorf("staged page publication failed qualification: %+v", validation.Issues)
	}
	keys := make([]string, 0, len(staged))
	for k := range staged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		target := content.ResolveDocumentPath(key, manifestPath)
		data, e := os.ReadFile(staged[key])
		if e != nil {
			return e
		}
		if existing, e := os.ReadFile(target); e == nil {
			if !bytes.Equal(existing, data) {
				return fmt.Errorf("immutable payload already differs: %s", key)
			}
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		// A same-directory temporary and exclusive hard link publish a complete file
		// without permitting an old qualified path to be overwritten.
		tmp, e := os.CreateTemp(filepath.Dir(target), ".page-payload-")
		if e != nil {
			return e
		}
		tmpPath := tmp.Name()
		_, writeErr := tmp.Write(data)
		closeErr := tmp.Close()
		if writeErr != nil {
			os.Remove(tmpPath)
			return writeErr
		}
		if closeErr != nil {
			os.Remove(tmpPath)
			return closeErr
		}
		linkErr := os.Link(tmpPath, target)
		os.Remove(tmpPath)
		if linkErr != nil {
			existing, readErr := os.ReadFile(target)
			if readErr != nil || !bytes.Equal(existing, data) {
				return linkErr
			}
		}
	}
	data, e := os.ReadFile(stageManifest)
	if e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(manifestPath), 0755); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(manifestPath), ".page-manifest-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if _, e := tmp.Write(data); e != nil {
		tmp.Close()
		return e
	}
	if e := tmp.Close(); e != nil {
		return e
	}
	if err := os.Rename(tmp.Name(), manifestPath); err != nil {
		return err
	}
	*bake.Manifest = *d
	bake.fingerprint, e = pageFingerprint(bake)
	return e
}

func pageOccupiedCosts(c *content.ImportedWorldChunkDef) (int, int) {
	sectors, bricks := map[[3]int]struct{}{}, map[[3]int]struct{}{}
	for _, v := range c.Voxels {
		if v.Value == 0 {
			continue
		}
		sectors[[3]int{v.X / volume.SectorSize, v.Y / volume.SectorSize, v.Z / volume.SectorSize}] = struct{}{}
		bricks[[3]int{v.X / volume.BrickSize, v.Y / volume.BrickSize, v.Z / volume.BrickSize}] = struct{}{}
	}
	return len(sectors), len(bricks)
}
