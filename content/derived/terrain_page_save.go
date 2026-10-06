package derived

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
)

// SaveTerrainPageBake publishes immutable source and visual payloads before an
// atomic manifest replacement. Failed writes may leave only new orphan payloads.
func SaveTerrainPageBake(manifestPath string, bake *TerrainPageBake) error {
	if strings.TrimSpace(manifestPath) == "" || bake == nil || bake.Manifest == nil || bake.fingerprint == "" {
		return fmt.Errorf("terrain page bake is nil or unowned")
	}
	fingerprint, e := terrainPageFingerprint(bake)
	if e != nil {
		return e
	}
	if fingerprint != bake.fingerprint {
		return fmt.Errorf("terrain page draft changed; rebuild its generation")
	}
	d, e := pageClone(bake.Manifest)
	if e != nil {
		return e
	}
	staging, e := os.MkdirTemp("", "gekko-terrain-page-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(staging)
	staged := map[string]string{}
	saveTile := func(path string, tile *content.TerrainHeightTileDef) (content.TerrainHeightTileSaveResult, error) {
		target := filepath.Join(staging, filepath.FromSlash(path))
		result, e := content.SaveTerrainHeightTile(target, tile)
		if e == nil {
			staged[path] = target
		}
		return result, e
	}
	for i := range d.Entries {
		entry := &d.Entries[i]
		tile := bake.SourceTiles[entry.Coord]
		if tile == nil {
			return fmt.Errorf("missing backing tile")
		}
		result, e := saveTile(entry.ChunkPath, tile)
		if e != nil {
			return e
		}
		entry.PayloadHash = result.PayloadHash
		entry.PayloadSizeBytes = result.PayloadSizeBytes
		entry.PayloadKind = content.TerrainHeightTilePayloadKind
	}
	paths := make([]string, 0, len(bake.PageTiles))
	for path := range bake.PageTiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	results := map[string]content.TerrainHeightTileSaveResult{}
	for _, path := range paths {
		result, e := saveTile(path, bake.PageTiles[path])
		if e != nil {
			return e
		}
		results[path] = result
	}
	for i := range d.Pages {
		p := &d.Pages[i].Payload
		result, ok := results[p.Path]
		if !ok {
			return fmt.Errorf("missing visual tile")
		}
		p.PayloadHash = result.PayloadHash
		p.PayloadSizeBytes = result.PayloadSizeBytes
	}
	if _, e := content.ValidateTerrainPageManifest(d); e != nil {
		return e
	}
	// Validate the forest once; bounded codec loads below qualify every immutable
	// header and raw body without repeating full-forest indexing for each page.
	for _, entry := range d.Entries {
		loaded, e := content.LoadTerrainHeightTileEntry(entry, filepath.Join(staging, "manifest.gkterrainchunks"))
		if e != nil {
			return e
		}
		if !terrainTileEqual(loaded, bake.SourceTiles[entry.Coord]) {
			return fmt.Errorf("staged backing tile qualification failed")
		}
	}
	for _, page := range d.Pages {
		loaded, e := content.LoadTerrainHeightTile(staged[page.Payload.Path])
		if e != nil {
			return e
		}
		if !terrainTileEqual(loaded, bake.PageTiles[page.Payload.Path]) {
			return fmt.Errorf("staged visual tile qualification failed")
		}
		p := page.Payload
		if loaded.TerrainID != d.TerrainID || loaded.SourceHash != d.SourceHash || loaded.WorldOrigin != p.WorldOrigin || loaded.SampleWidth != p.ChunkSize || loaded.SampleHeight != p.ChunkSize || loaded.SampleSpacing != p.SampleSpacing || loaded.HeightOffset != p.HeightOffset || loaded.HeightScale != p.HeightScale || loaded.OutdoorNavCellSize != 0 || len(loaded.OutdoorNavExclusionMask) != 0 {
			return fmt.Errorf("staged visual header qualification failed")
		}
	}
	stageManifest := filepath.Join(staging, "manifest.gkterrainchunks")
	if e := content.SaveTerrainChunkManifest(stageManifest, d); e != nil {
		return e
	}
	keys := make([]string, 0, len(staged))
	for path := range staged {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		if e := publishImmutableTerrainFile(staged[path], content.ResolveDocumentPath(path, manifestPath)); e != nil {
			return e
		}
	}
	raw, e := os.ReadFile(stageManifest)
	if e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(manifestPath), 0755); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(manifestPath), ".terrain-page-manifest-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if _, e := tmp.Write(raw); e != nil {
		tmp.Close()
		return e
	}
	if e := tmp.Close(); e != nil {
		return e
	}
	if e := os.Rename(tmp.Name(), manifestPath); e != nil {
		return e
	}
	*bake.Manifest = *d
	bake.fingerprint, e = terrainPageFingerprint(bake)
	return e
}
func terrainTileEqual(a, b *content.TerrainHeightTileDef) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.SchemaVersion != b.SchemaVersion || a.TerrainID != b.TerrainID || a.SourceHash != b.SourceHash || a.Coord != b.Coord || a.WorldOrigin != b.WorldOrigin || a.SampleWidth != b.SampleWidth || a.SampleHeight != b.SampleHeight || a.SampleSpacing != b.SampleSpacing || a.HeightOffset != b.HeightOffset || a.HeightScale != b.HeightScale || a.OutdoorNavCellSize != b.OutdoorNavCellSize || len(a.HeightSamples) != len(b.HeightSamples) {
		return false
	}
	for i, v := range a.HeightSamples {
		if v != b.HeightSamples[i] {
			return false
		}
	}
	return bytes.Equal(a.SurfaceMask, b.SurfaceMask) && bytes.Equal(a.OutdoorNavExclusionMask, b.OutdoorNavExclusionMask)
}
func publishImmutableTerrainFile(staged, target string) error {
	raw, e := os.ReadFile(staged)
	if e != nil {
		return e
	}
	if existing, e := os.ReadFile(target); e == nil {
		if !bytes.Equal(existing, raw) {
			return fmt.Errorf("immutable terrain payload differs: %s", target)
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(target), ".terrain-page-payload-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if _, e := tmp.Write(raw); e != nil {
		tmp.Close()
		return e
	}
	if e := tmp.Close(); e != nil {
		return e
	}
	if e := os.Link(tmp.Name(), target); e != nil {
		existing, readErr := os.ReadFile(target)
		if readErr != nil || !bytes.Equal(existing, raw) {
			return e
		}
	}
	return nil
}
