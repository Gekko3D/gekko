package content

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindNavClearanceSourcePathFiltersSameSourceByAgentRadius(t *testing.T) {
	voxels := navTestFloorVoxels(1, 5, 1, 3, 0)
	for x := 1; x <= 5; x++ {
		voxels = append(voxels,
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 1, Value: 1},
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 3, Value: 1},
		)
	}
	chunk := navTestChunk(7, 1.0, voxels...)
	source, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-clearance-query", chunk, 1.0)
	_ = source

	small, err := FindNavClearanceSourcePath(manifest, navPath, Vec3{1.5, 1, 2.5}, Vec3{5.5, 1, 2.5}, NavClearancePathOptions{
		AgentProfile: navTestAgentProfileWithID("small", 0.2, 1.0, 1.0),
	})
	if err != nil {
		t.Fatalf("FindNavClearanceSourcePath small failed: %v", err)
	}
	if !small.Found || len(small.Steps) < 2 {
		t.Fatalf("expected small agent to path through shared clearance source, got %+v", small)
	}

	large, err := FindNavClearanceSourcePath(manifest, navPath, Vec3{1.5, 1, 2.5}, Vec3{5.5, 1, 2.5}, NavClearancePathOptions{
		AgentProfile: navTestAgentProfileWithID("large", 0.6, 1.0, 1.0),
	})
	if err != nil {
		t.Fatalf("FindNavClearanceSourcePath large failed: %v", err)
	}
	if large.Found {
		t.Fatalf("did not expect large agent to path through same narrow clearance source, got %+v", large)
	}
}

func TestFindNavClearanceSourcePathCrossesTileBoundary(t *testing.T) {
	left := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	manifest := &NavManifestDef{
		NavID:           "nav-clearance-cross",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		ChunkSize:       4,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{navTestAgentProfileWithID("small", 0.2, 1.0, 1.0)},
	}
	for _, chunk := range []*ImportedWorldChunkDef{left, right} {
		source, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              manifest.NavID,
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		sourcePath := filepath.Join(root, "worlds", "path_navsources", TerrainChunkKey(chunk.Coord)+".gknavsource")
		if err := SaveNavClearanceSourceTile(sourcePath, source.Tile); err != nil {
			t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
		}
		manifest.ClearanceSourceTiles = append(manifest.ClearanceSourceTiles, NavClearanceSourceTileEntryDef{
			Coord:              chunk.Coord,
			TilePath:           AuthorDocumentPath(sourcePath, navPath),
			PayloadKind:        source.Tile.PayloadKind,
			BoundsMin:          source.Tile.BoundsMin,
			BoundsMax:          source.Tile.BoundsMax,
			MaxClearanceRadius: source.Tile.MaxClearanceRadius,
		})
	}
	EnsureNavManifestDefaults(manifest)

	path, err := FindNavClearanceSourcePath(manifest, navPath, Vec3{0.5, 1, 1.5}, Vec3{7.5, 1, 1.5}, NavClearancePathOptions{AgentProfileID: "small"})
	if err != nil {
		t.Fatalf("FindNavClearanceSourcePath failed: %v", err)
	}
	if !path.Found || len(path.Steps) < 5 {
		t.Fatalf("expected clearance source path across tile boundary, got %+v", path)
	}
	if path.Steps[0].Coord != left.Coord || path.Steps[len(path.Steps)-1].Coord != right.Coord {
		t.Fatalf("expected path to start in left and end in right tile, got %+v", path.Steps)
	}
}

func TestFindNavClearanceSourcePathUsesIndexedCells(t *testing.T) {
	chunk := navTestChunk(64, 1.0, navTestFloorVoxels(0, 63, 0, 63, 0)...)
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-clearance-indexed", chunk, 1.0)

	started := time.Now()
	path, err := FindNavClearanceSourcePath(manifest, navPath, Vec3{0.5, 1, 0.5}, Vec3{63.5, 1, 63.5}, NavClearancePathOptions{
		AgentProfileID:       "small",
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindNavClearanceSourcePath failed: %v", err)
	}
	if !path.Found || len(path.Steps) < 64 {
		t.Fatalf("expected indexed clearance source path across large tile, got %+v", path)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("expected indexed clearance path query to stay interactive, took %s for %d steps", elapsed, len(path.Steps))
	}
}

func TestFindEffectiveNavPathUsesClearanceSourceWhenAvailable(t *testing.T) {
	chunk := navTestChunk(7, 1.0, navTestFloorVoxels(1, 5, 2, 2, 0)...)
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-effective-clearance", chunk, 1.0)

	path, err := FindEffectiveNavPath(manifest, navPath, nil, "", Vec3{1.5, 1, 2.5}, Vec3{5.5, 1, 2.5}, NavPathOptions{
		AgentProfileID: "small",
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 1 || !path.RegionPath.Found || path.RegionPath.RawStepCount != 0 {
		t.Fatalf("expected effective path through compact clearance region, got %+v", path)
	}
	for _, step := range path.Steps {
		if step.Source != NavPathSourceClearance {
			t.Fatalf("expected clearance-source step, got %+v in path %+v", step, path)
		}
		if !strings.HasPrefix(step.PolygonID, "region:") {
			t.Fatalf("expected compact region step, got %+v in path %+v", step, path)
		}
	}
}

func TestFindEffectiveNavPathUsesSingleCompactRegionWithoutRawCellDetail(t *testing.T) {
	chunk := navTestChunk(16, 1.0, navTestFloorVoxels(1, 14, 2, 2, 0)...)
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-effective-region-detail", chunk, 1.0)

	path, err := FindEffectiveNavPath(manifest, navPath, nil, "", Vec3{1.5, 1, 2.5}, Vec3{14.5, 1, 2.5}, NavPathOptions{
		AgentProfileID:       "small",
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || !path.RegionPath.Found {
		t.Fatalf("expected compact effective path, got %+v", path)
	}
	if len(path.Steps) != 1 || !strings.HasPrefix(path.Steps[0].PolygonID, "region:") {
		t.Fatalf("expected normal effective path steps to use compact region ids, got %+v", path.Steps)
	}
	if path.RegionPath.RawStepCount != 0 || len(path.RegionPath.RawSteps) != 0 {
		t.Fatalf("expected normal same-region route to avoid raw cell expansion, got %+v", path.RegionPath)
	}
}

func TestFindEffectiveNavPathUsesRegionGraphBeforeRawCellsAcrossTiles(t *testing.T) {
	manifest, navPath := navClearanceQueryTestStripSourceManifest(t, "nav-effective-region-graph", 4)

	path, err := FindEffectiveNavPath(manifest, navPath, nil, "", Vec3{0.5, 1, 1.5}, Vec3{15.5, 1, 1.5}, NavPathOptions{
		AgentProfileID:       "small",
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || !path.RegionPath.Found {
		t.Fatalf("expected region graph effective path, got %+v", path)
	}
	if len(path.Steps) != 4 || len(path.RegionPath.Portals) != 3 {
		t.Fatalf("expected one compact region step per tile and three portals, got %+v", path)
	}
	if path.RegionPath.RawStepCount != 0 || len(path.RegionPath.RawSteps) != 0 {
		t.Fatalf("expected region graph route to avoid raw cell expansion, got %+v", path.RegionPath)
	}
	for i, step := range path.Steps {
		expected := TerrainChunkCoordDef{X: i, Y: 0, Z: 0}
		if step.Coord != expected || step.Source != NavPathSourceClearance || !strings.HasPrefix(step.PolygonID, "region:") {
			t.Fatalf("unexpected compact step %d: %+v path=%+v", i, step, path)
		}
	}
}

func TestFindEffectiveNavPathClearanceSourceFiltersLargeAgent(t *testing.T) {
	voxels := navTestFloorVoxels(1, 5, 1, 3, 0)
	for x := 1; x <= 5; x++ {
		voxels = append(voxels,
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 1, Value: 1},
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 3, Value: 1},
		)
	}
	chunk := navTestChunk(7, 1.0, voxels...)
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-effective-large-clearance", chunk, 1.0)

	path, err := FindEffectiveNavPath(manifest, navPath, nil, "", Vec3{1.5, 1, 2.5}, Vec3{5.5, 1, 2.5}, NavPathOptions{
		AgentProfileID: "large",
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found {
		t.Fatalf("did not expect large agent effective path through narrow clearance source, got %+v", path)
	}
}

func TestFindEffectiveNavPathUsesDeltaClearanceSourceOverride(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	baseChunk := navTestChunk(16, 1.0, navTestFloorVoxels(1, 14, 1, 14, 0)...)
	baseChunk.Coord = coord
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-delta-clearance", baseChunk, 1.0)
	manifest.SourceWorldID = "world-a"

	editedVoxels := removeNavTestFloorVoxels(baseChunk.Voxels, 6, 9, 6, 9, 0)
	editedChunk := navTestChunk(16, 1.0, editedVoxels...)
	editedChunk.WorldID = "world-a"
	editedChunk.Coord = coord
	editedChunk.NonEmptyVoxelCount = len(editedChunk.Voxels)
	sourceResult, err := BuildNavClearanceSourceTileFromImportedWorldChunk(editedChunk, NavClearanceSourceTileBuildOptions{
		NavID:              manifest.NavID,
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	root := filepath.Dir(filepath.Dir(navPath))
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	sourcePath := DefaultWorldDeltaNavClearanceSourceTilePath(deltaPath, manifest.NavID, coord)
	if err := SaveNavClearanceSourceTile(sourcePath, sourceResult.Tile); err != nil {
		t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		ImportedWorldChunkOverrides: []ImportedWorldChunkOverrideDef{{
			WorldID:    "world-a",
			ChunkCoord: coord,
		}},
		NavigationClearanceSourceTileOverrides: []NavigationClearanceSourceTileOverrideDef{{
			NavID:      manifest.NavID,
			ChunkCoord: coord,
			TilePath:   authorPathRelativeToDocument(sourcePath, deltaPath),
		}},
	}

	path, err := FindEffectiveNavPath(manifest, navPath, delta, deltaPath, Vec3{2.5, 1, 2.5}, Vec3{7.5, 1, 7.5}, NavPathOptions{
		AgentProfileID:       "small",
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found || path.FailureReason != NavPathFailureEndPolygonMissing {
		t.Fatalf("expected delta clearance source hole to reject endpoint, got %+v", path)
	}
}

func TestFindEffectiveNavPathDoesNotFallbackToPolygonTileForEditedChunkWithoutClearanceSource(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	baseChunk := navTestChunk(8, 1.0, navTestFloorVoxels(1, 6, 1, 6, 0)...)
	baseChunk.WorldID = "world-a"
	baseChunk.Coord = coord
	_, manifest, navPath := navClearanceQueryTestSourceManifest(t, "nav-stale-delta-clearance", baseChunk, 1.0)
	manifest.SourceWorldID = "world-a"

	root := filepath.Dir(filepath.Dir(navPath))
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	staleTilePath := filepath.Join(DefaultWorldDeltaNavTileDir(deltaPath, manifest.NavID, "small"), "small_0_0_0.gknavtile")
	if err := SaveNavTile(staleTilePath, navPathTestTile(manifest.NavID, "small", coord, "legacy_delta_poly", Vec3{1, 1, 1}, Vec3{6, 1, 6})); err != nil {
		t.Fatalf("SaveNavTile failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		ImportedWorldChunkOverrides: []ImportedWorldChunkOverrideDef{{
			WorldID:    "world-a",
			ChunkCoord: coord,
		}},
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          manifest.NavID,
			AgentProfileID: "small",
			ChunkCoord:     coord,
			TilePath:       authorPathRelativeToDocument(staleTilePath, deltaPath),
		}},
	}

	path, err := FindEffectiveNavPath(manifest, navPath, delta, deltaPath, Vec3{2, 1, 2}, Vec3{5, 1, 5}, NavPathOptions{
		AgentProfileID:       "small",
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found || path.FailureReason != NavPathFailureStartPolygonMissing {
		t.Fatalf("expected edited chunk without clearance source to reject legacy polygon fallback, got %+v", path)
	}
}

func TestBakeNavFromImportedWorldRecastEmitsClearanceSourceTiles(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	chunk := navTestChunk(8, 1.0, navTestFloorVoxels(1, 6, 1, 6, 0)...)
	chunk.Coord = coord
	chunk.NonEmptyVoxelCount = len(chunk.Voxels)
	world := &ImportedWorldDef{
		WorldID:         "world-clearance-recast",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          "chunks/center.gkchunk",
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{coord: chunk}, filepath.Join(t.TempDir(), "nav.gknav"), NavBakeOptions{
		BuilderVersion: NavBuilderVersionVoxelRecastV1,
		AgentProfiles:  []NavAgentProfileDef{navTestAgentProfileWithID("small", 0.2, 1.0, 0.5)},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.ClearanceSourceTiles) != 1 || len(result.ClearanceSourceTiles) != 1 {
		t.Fatalf("expected Recast bake to emit canonical clearance source tile, manifest=%+v sources=%d", result.Manifest.ClearanceSourceTiles, len(result.ClearanceSourceTiles))
	}
}

func navClearanceQueryTestSourceManifest(t *testing.T, navID string, chunk *ImportedWorldChunkDef, maxClearance float32) (*NavClearanceSourceTileDef, *NavManifestDef, string) {
	t.Helper()
	result, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              navID,
		MaxClearanceRadius: maxClearance,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	sourcePath := filepath.Join(root, "worlds", "path_navsources", "source.gknavsource")
	if err := SaveNavClearanceSourceTile(sourcePath, result.Tile); err != nil {
		t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
	}
	manifest := &NavManifestDef{
		NavID:           navID,
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		AgentProfiles: []NavAgentProfileDef{
			navTestAgentProfileWithID("small", 0.2, 1.0, 1.0),
			navTestAgentProfileWithID("large", 0.6, 1.0, 1.0),
		},
		ClearanceSourceTiles: []NavClearanceSourceTileEntryDef{{
			Coord:              chunk.Coord,
			TilePath:           AuthorDocumentPath(sourcePath, navPath),
			PayloadKind:        result.Tile.PayloadKind,
			BoundsMin:          result.Tile.BoundsMin,
			BoundsMax:          result.Tile.BoundsMax,
			MaxClearanceRadius: result.Tile.MaxClearanceRadius,
		}},
	}
	EnsureNavManifestDefaults(manifest)
	return result.Tile, manifest, navPath
}

func navClearanceQueryTestStripSourceManifest(t *testing.T, navID string, count int) (*NavManifestDef, string) {
	t.Helper()
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "path.gknav")
	manifest := &NavManifestDef{
		NavID:           navID,
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		ChunkSize:       4,
		VoxelResolution: 1,
		AgentProfiles: []NavAgentProfileDef{
			navTestAgentProfileWithID("small", 0.2, 1.0, 1.0),
		},
	}
	for x := 0; x < count; x++ {
		coord := TerrainChunkCoordDef{X: x, Y: 0, Z: 0}
		chunk := navTestChunk(4, 1.0, navTestFloorVoxels(0, 3, 1, 1, 0)...)
		chunk.Coord = coord
		result, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
			NavID:              navID,
			MaxClearanceRadius: 1.0,
		})
		if err != nil {
			t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
		}
		sourcePath := filepath.Join(root, "worlds", "path_navsources", TerrainChunkKey(coord)+".gknavsource")
		if err := SaveNavClearanceSourceTile(sourcePath, result.Tile); err != nil {
			t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
		}
		manifest.ClearanceSourceTiles = append(manifest.ClearanceSourceTiles, NavClearanceSourceTileEntryDef{
			Coord:              coord,
			TilePath:           AuthorDocumentPath(sourcePath, navPath),
			PayloadKind:        result.Tile.PayloadKind,
			BoundsMin:          result.Tile.BoundsMin,
			BoundsMax:          result.Tile.BoundsMax,
			MaxClearanceRadius: result.Tile.MaxClearanceRadius,
		})
	}
	EnsureNavManifestDefaults(manifest)
	return manifest, navPath
}
