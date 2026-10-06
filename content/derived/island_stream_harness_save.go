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

const islandHarnessLevelFile = "island_streaming_harness.gklevel"

// ValidateIslandStreamHarnessOutput checks the named output and borrowed input
// paths without expanding geometry. It is a preflight, not a publication grant;
// SaveIslandStreamHarness repeats these checks against its sealed source.
func ValidateIslandStreamHarnessOutput(outDir, sourcePath string, source *content.ImportedWorldDef) error {
	_, err := islandHarnessOutput(outDir, sourcePath, source)
	return err
}
func islandCanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	var tail []string
	current := abs
	for {
		_, err := os.Lstat(current)
		if err == nil {
			base, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(tail) - 1; i >= 0; i-- {
				base = filepath.Join(base, tail[i])
			}
			return base, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		tail = append(tail, filepath.Base(current))
		current = parent
	}
}
func islandPathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func islandHarnessOutput(outDir, sourcePath string, source *content.ImportedWorldDef) (string, error) {
	if strings.TrimSpace(outDir) == "" || strings.TrimSpace(sourcePath) == "" || source == nil {
		return "", fmt.Errorf("harness output/source context is empty")
	}
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if info, e := os.Lstat(abs); e == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("harness output must be a real directory")
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	root, err := islandCanonicalPath(abs)
	if err != nil {
		return "", err
	}
	input, err := islandCanonicalPath(sourcePath)
	if err != nil {
		return "", err
	}
	inputDir := filepath.Dir(input)
	if islandPathWithin(inputDir, root) || islandPathWithin(root, inputDir) {
		return "", fmt.Errorf("harness output overlaps source tree")
	}
	paths := []string{sourcePath}
	for _, e := range source.Entries {
		paths = append(paths, content.ResolveImportedWorldChunkPath(e, sourcePath))
		if e.Aux != nil {
			paths = append(paths, content.ResolveDocumentPath(e.Aux.AuxPath, sourcePath))
		}
	}
	if source.Backing != nil {
		paths = append(paths, content.ResolveDocumentPath(source.Backing.Path, sourcePath))
	}
	for _, path := range paths {
		actual, err := islandCanonicalPath(path)
		if err != nil {
			return "", err
		}
		if islandPathWithin(root, actual) {
			return "", fmt.Errorf("harness output owns borrowed input: %s", path)
		}
	}
	if err := islandHarnessCheckTarget(root, islandHarnessLevelFile); err != nil {
		return "", err
	}
	levelPath := filepath.Join(root, islandHarnessLevelFile)
	if _, err := os.Stat(levelPath); err == nil {
		old, err := content.LoadLevel(levelPath)
		if err != nil {
			return "", fmt.Errorf("existing root level is not an owned harness: %w", err)
		}
		owned := strings.HasPrefix(old.ID, "island_streaming_harness:")
		tag := false
		for _, v := range old.Tags {
			tag = tag || v == "fixture_recipe:"+islandHarnessRecipe
		}
		if !owned || !tag {
			return "", fmt.Errorf("existing root level is not an owned harness")
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return root, nil
}
func islandHarnessCheckTarget(root, rel string) error {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid generated harness path: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("generated harness path escapes output: %s", rel)
	}
	current := root
	parts := strings.Split(clean, string(os.PathSeparator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated harness path follows symlink: %s", rel)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("generated harness parent is not directory: %s", rel)
		}
	}
	return nil
}
func islandHarnessRootPath(ref, manifest string, published bool) (string, error) {
	p := filepath.FromSlash(ref)
	if published {
		p = filepath.Join(filepath.Dir(filepath.FromSlash(manifest)), p)
	}
	p = filepath.Clean(p)
	if filepath.IsAbs(p) || p == "." || p == ".." || strings.HasPrefix(p, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("generated harness path escapes output: %s", ref)
	}
	return filepath.ToSlash(p), nil
}
func islandHarnessRelative(rootPath, manifest string) (string, error) {
	rel, err := filepath.Rel(filepath.Dir(filepath.FromSlash(manifest)), filepath.FromSlash(rootPath))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// SaveIslandStreamHarness borrows qualified original full/aux files read-only,
// publishes only generated immutable artifacts, and atomically replaces the
// fixed level last. Failed writes can leave new orphans, never alter old refs.
func SaveIslandStreamHarness(outDir, sourcePath string, b *IslandStreamHarness) error {
	if b == nil || b.Level == nil || b.Terrain == nil || b.POI == nil || b.sourceSnapshot == nil || b.fingerprint == "" {
		return fmt.Errorf("harness draft is nil or unowned")
	}
	fingerprint, err := islandHarnessFingerprint(b)
	if err != nil {
		return err
	}
	if fingerprint != b.fingerprint {
		return fmt.Errorf("harness draft changed; rebuild generation")
	}
	source, err := content.LoadImportedWorld(sourcePath)
	if err != nil {
		return err
	}
	identity, err := islandHarnessSourceIdentity(source)
	if err != nil {
		return err
	}
	sealed, err := islandHarnessSourceIdentity(b.sourceSnapshot)
	if err != nil {
		return err
	}
	if identity != sealed {
		return fmt.Errorf("harness source manifest changed; rebuild generation")
	}
	root, err := islandHarnessOutput(outDir, sourcePath, source)
	if err != nil {
		return err
	}
	level, err := pageClone(b.Level)
	if err != nil {
		return err
	}
	terrain, err := pageClone(b.Terrain)
	if err != nil {
		return err
	}
	poi, err := pageClone(b.POI)
	if err != nil {
		return err
	}
	terrainManifest, poiManifest := level.Terrain.ManifestPath, level.BaseWorld.ManifestPath
	for _, p := range []string{terrainManifest, poiManifest} {
		if err := islandHarnessCheckTarget(root, p); err != nil {
			return err
		}
	}
	// Qualify borrowed files before creating the output or staging directory.
	if ref := source.Backing; ref != nil {
		if strings.TrimSpace(ref.Path) == "" || strings.ToLower(filepath.Ext(ref.Path)) != ".gkvoxelbacking" || ref.Kind != content.VoxelBackingKindPlaneTreeV1 {
			return fmt.Errorf("invalid borrowed voxel backing reference")
		}
		backingPath := content.ResolveDocumentPath(ref.Path, sourcePath)
		backing, err := content.LoadVoxelBacking(backingPath)
		if err != nil {
			return fmt.Errorf("invalid borrowed voxel backing: %w", err)
		}
		if backing.Kind != ref.Kind || backing.SourceHash != ref.SourceHash || backing.BoundsMin != ref.BoundsMin || backing.BoundsMax != ref.BoundsMax {
			return fmt.Errorf("borrowed voxel backing metadata mismatch")
		}
		// The sidecar stays owned by the source, even after a repeated save.
		copyRef := *ref
		copyRef.Path = content.AuthorDocumentPath(backingPath, filepath.Join(root, filepath.FromSlash(poiManifest)))
		poi.Backing = &copyRef
	}
	sourceByCoord := map[content.TerrainChunkCoordDef]content.ImportedWorldChunkEntryDef{}
	for i, e := range source.Entries {
		chunk, err := content.LoadImportedWorldChunkEntry(source, sourcePath, uint32(i))
		if err != nil {
			return err
		}
		sectors, bricks := pageOccupiedCosts(chunk)
		sourceByCoord[e.Coord] = e
		for j := range poi.Entries {
			if poi.Entries[j].Coord == e.Coord {
				entry := &poi.Entries[j]
				if entry.PayloadHash != e.PayloadHash || entry.PayloadSizeBytes != e.PayloadSizeBytes || entry.PayloadKind != e.PayloadKind || entry.NonEmptyVoxelCount != e.NonEmptyVoxelCount || entry.OccupiedSectorCount != sectors || entry.OccupiedBrickCount != bricks {
					return fmt.Errorf("sealed full reference differs from qualified source")
				}
			}
		}
	}
	generated := map[string]bool{terrainManifest: true, poiManifest: true}
	for _, e := range terrain.Entries {
		p, err := islandHarnessRootPath(e.ChunkPath, terrainManifest, b.published)
		if err != nil {
			return err
		}
		generated[p] = true
	}
	for p := range b.TerrainPageTiles {
		generated[p] = true
	}
	for p := range b.POIPageChunks {
		generated[p] = true
		generated[content.DefaultImportedWorldChunkAuxPath(p)] = true
	}
	for p := range generated {
		if err := islandHarnessCheckTarget(root, p); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".island-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	files := map[string]string{}
	saveHeight := func(p string, tile *content.TerrainHeightTileDef) (content.TerrainHeightTileSaveResult, error) {
		result, err := content.SaveTerrainHeightTile(filepath.Join(stage, filepath.FromSlash(p)), tile)
		if err == nil {
			files[p] = filepath.Join(stage, filepath.FromSlash(p))
		}
		return result, err
	}
	for i := range terrain.Entries {
		entry := &terrain.Entries[i]
		p, err := islandHarnessRootPath(entry.ChunkPath, terrainManifest, b.published)
		if err != nil {
			return err
		}
		tile := b.SourceTiles[entry.Coord]
		if tile == nil {
			return fmt.Errorf("missing harness source tile")
		}
		result, err := saveHeight(p, tile)
		if err != nil {
			return err
		}
		entry.PayloadKind = content.TerrainHeightTilePayloadKind
		entry.PayloadHash = result.PayloadHash
		entry.PayloadSizeBytes = result.PayloadSizeBytes
		entry.ChunkPath, err = islandHarnessRelative(p, terrainManifest)
		if err != nil {
			return err
		}
	}
	for i := range terrain.Pages {
		p := &terrain.Pages[i].Payload
		key, err := islandHarnessRootPath(p.Path, terrainManifest, b.published)
		if err != nil {
			return err
		}
		tile := b.TerrainPageTiles[key]
		if tile == nil {
			return fmt.Errorf("missing harness terrain visual tile")
		}
		result, err := saveHeight(key, tile)
		if err != nil {
			return err
		}
		p.PayloadHash = result.PayloadHash
		p.PayloadSizeBytes = result.PayloadSizeBytes
		p.Path, err = islandHarnessRelative(key, terrainManifest)
		if err != nil {
			return err
		}
	}
	for i := range poi.Entries {
		entry := &poi.Entries[i]
		original, ok := sourceByCoord[entry.Coord]
		if !ok {
			return fmt.Errorf("missing borrowed full entry")
		}
		entry.ChunkPath = content.AuthorDocumentPath(content.ResolveImportedWorldChunkPath(original, sourcePath), filepath.Join(root, filepath.FromSlash(poiManifest)))
		if original.Aux != nil {
			entry.Aux = cloneHarnessAux(original.Aux)
			entry.Aux.AuxPath = content.AuthorDocumentPath(content.ResolveDocumentPath(original.Aux.AuxPath, sourcePath), filepath.Join(root, filepath.FromSlash(poiManifest)))
		}
	}
	for i := range poi.Pages {
		page := &poi.Pages[i]
		p := &page.Payload
		if page.Level == content.StreamPageLevelLeaf {
			if len(page.LeafEntryIndices) != 1 || uint64(page.LeafEntryIndices[0]) >= uint64(len(poi.Entries)) {
				return fmt.Errorf("invalid full leaf reuse")
			}
			entry := poi.Entries[page.LeafEntryIndices[0]]
			p.Path = entry.ChunkPath
			p.Kind = entry.PayloadKind
			p.PayloadHash = entry.PayloadHash
			p.PayloadSizeBytes = entry.PayloadSizeBytes
			p.Aux = cloneHarnessAux(entry.Aux)
			continue
		}
		key, err := islandHarnessRootPath(p.Path, poiManifest, b.published)
		if err != nil {
			return err
		}
		chunk := b.POIPageChunks[key]
		if chunk == nil {
			return fmt.Errorf("missing generated harness POI payload")
		}
		chunk, err = pageClone(chunk)
		if err != nil {
			return err
		}
		target := filepath.Join(stage, filepath.FromSlash(key))
		if _, err := content.SaveImportedWorldChunkWithOptionsResult(target, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
			return err
		}
		files[key] = target
		p.Kind = chunk.PayloadKind
		p.PayloadHash = chunk.PayloadHash
		p.PayloadSizeBytes = chunk.PayloadSizeBytes
		p.OccupiedSectorCount, p.OccupiedBrickCount = pageOccupiedCosts(chunk)
		p.Path, err = islandHarnessRelative(key, poiManifest)
		if err != nil {
			return err
		}
		aux := BuildImportedWorldChunkAux(chunk, nil, chunk.PayloadHash, chunk.PayloadSizeBytes, false)
		if aux != nil {
			auxKey := content.DefaultImportedWorldChunkAuxPath(key)
			target := filepath.Join(stage, filepath.FromSlash(auxKey))
			if err := content.SaveImportedWorldChunkAux(target, aux); err != nil {
				return err
			}
			files[auxKey] = target
			auxRef, err := islandHarnessRelative(auxKey, poiManifest)
			if err != nil {
				return err
			}
			p.Aux = content.ImportedWorldChunkAuxRef(auxRef, aux)
		} else {
			p.Aux = nil
		}
	}
	if _, err := content.ValidateTerrainPageManifest(terrain); err != nil {
		return err
	}
	if _, err := content.ValidateImportedWorldV3(poi); err != nil {
		return err
	}
	if err := content.SaveTerrainChunkManifest(filepath.Join(stage, filepath.FromSlash(terrainManifest)), terrain); err != nil {
		return err
	}
	files[terrainManifest] = filepath.Join(stage, filepath.FromSlash(terrainManifest))
	if err := content.SaveImportedWorld(filepath.Join(stage, filepath.FromSlash(poiManifest)), poi); err != nil {
		return err
	}
	files[poiManifest] = filepath.Join(stage, filepath.FromSlash(poiManifest))
	if err := content.SaveLevel(filepath.Join(stage, islandHarnessLevelFile), level); err != nil {
		return err
	}
	// Metadata is validated once; every generated body/header is qualified against
	// its encoder result, while borrowed full/aux qualification above runs once.
	for _, entry := range terrain.Entries {
		if _, err := content.LoadTerrainHeightTileEntry(entry, filepath.Join(stage, filepath.FromSlash(terrainManifest))); err != nil {
			return err
		}
	}
	for i, page := range terrain.Pages {
		key, err := islandHarnessRootPath(page.Payload.Path, terrainManifest, true)
		if err != nil {
			return err
		}
		tile, err := content.LoadTerrainHeightTile(files[key])
		if err != nil {
			return err
		}
		if !terrainTileEqual(tile, b.TerrainPageTiles[key]) {
			return fmt.Errorf("generated height page %d failed qualification", i)
		}
	}
	for _, page := range poi.Pages {
		if page.Level == content.StreamPageLevelLeaf {
			continue
		}
		key, err := islandHarnessRootPath(page.Payload.Path, poiManifest, true)
		if err != nil {
			return err
		}
		qualified := &content.ImportedWorldDef{SchemaVersion: 3, Kind: content.ImportedWorldKindVoxelWorld, WorldID: poi.WorldID, ChunkSize: page.Payload.ChunkSize, VoxelResolution: page.Payload.VoxelResolution, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: page.Payload.Path, PayloadKind: page.Payload.Kind, PayloadHash: page.Payload.PayloadHash, PayloadSizeBytes: page.Payload.PayloadSizeBytes, NonEmptyVoxelCount: b.POIPageChunks[key].NonEmptyVoxelCount, OccupiedSectorCount: page.Payload.OccupiedSectorCount, OccupiedBrickCount: page.Payload.OccupiedBrickCount, Aux: page.Payload.Aux}}}
		chunk, err := content.LoadImportedWorldChunkEntry(qualified, filepath.Join(stage, filepath.FromSlash(poiManifest)), 0)
		if err != nil {
			return err
		}
		if chunk.WorldID != poi.WorldID || chunk.Coord != (content.TerrainChunkCoordDef{}) || chunk.ChunkSize != page.Payload.ChunkSize || chunk.VoxelResolution != page.Payload.VoxelResolution || chunk.PayloadHash != page.Payload.PayloadHash || chunk.PayloadSizeBytes != page.Payload.PayloadSizeBytes {
			return fmt.Errorf("generated POI header/body qualification failed")
		}
	}
	if _, err := content.BuildLevelStreamingIndex(level, terrain, poi); err != nil {
		return err
	}
	// Scan every existing target before publishing any bytes, including collisions
	// that sort after otherwise-new files.
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := islandHarnessCheckTarget(root, key); err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(key))
		existing, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		data, err := os.ReadFile(files[key])
		if err != nil {
			return err
		}
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("immutable harness artifact differs: %s", key)
		}
	}
	for _, key := range keys {
		if err := publishImmutableTerrainFile(files[key], filepath.Join(root, filepath.FromSlash(key))); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(stage, islandHarnessLevelFile))
	if err != nil {
		return err
	}
	if err := islandHarnessCheckTarget(root, islandHarnessLevelFile); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".island-level-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), filepath.Join(root, islandHarnessLevelFile)); err != nil {
		return err
	}
	*b.Level = *level
	*b.Terrain = *terrain
	*b.POI = *poi
	b.published = true
	b.fingerprint, err = islandHarnessFingerprint(b)
	return err
}
func cloneHarnessAux(ref *content.ImportedWorldChunkAuxRefDef) *content.ImportedWorldChunkAuxRefDef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}
