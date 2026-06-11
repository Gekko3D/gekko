package content

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandNavDirtyTileCoordsIncludesAgentMarginNeighbors(t *testing.T) {
	modified := []TerrainChunkCoordDef{{X: 3, Y: 4, Z: 5}}
	coords := ExpandNavDirtyTileCoords(modified, 16, 1, NavDirtyTileExpansionOptions{
		AgentProfiles: []NavAgentProfileDef{navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)},
	})
	if len(coords) != 27 {
		t.Fatalf("expected conservative 3x3x3 dirty expansion, got %d: %+v", len(coords), coords)
	}
	expected := map[TerrainChunkCoordDef]struct{}{
		{X: 3, Y: 4, Z: 5}: {},
		{X: 2, Y: 4, Z: 5}: {},
		{X: 4, Y: 4, Z: 5}: {},
		{X: 3, Y: 3, Z: 5}: {},
		{X: 3, Y: 5, Z: 5}: {},
		{X: 3, Y: 4, Z: 4}: {},
		{X: 3, Y: 4, Z: 6}: {},
	}
	for _, coord := range coords {
		delete(expected, coord)
	}
	if len(expected) != 0 {
		t.Fatalf("dirty expansion missing expected coords: %+v", expected)
	}
}

func TestExpandNavDirtyTileCoordsKeepsCenterWhenResolutionUnknown(t *testing.T) {
	modified := []TerrainChunkCoordDef{{X: 1, Y: 2, Z: 3}}
	coords := ExpandNavDirtyTileCoords(modified, 0, 0, NavDirtyTileExpansionOptions{})
	if !reflect.DeepEqual(coords, modified) {
		t.Fatalf("expected unknown chunk metrics to keep modified coord only, got %+v", coords)
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksWritesOverrideTiles(t *testing.T) {
	root := t.TempDir()
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	delta := &WorldDeltaDef{LevelID: "level-a"}
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  "builder-a",
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(1, 6, 1, 6, 0),
		NonEmptyVoxelCount: 36,
	}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavDeltaBakeOptions{})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Overrides) != 1 || len(result.Tiles) != 1 || len(delta.NavigationTileOverrides) != 1 {
		t.Fatalf("expected one nav override and tile, result=%+v delta=%+v", result, delta.NavigationTileOverrides)
	}
	override := delta.NavigationTileOverrides[0]
	if override.NavID != "nav-demo" || override.AgentProfileID != "tiny" || override.Empty || override.TilePath == "" || override.SourceDeltaHash == "" || override.NavBuildHash == "" {
		t.Fatalf("unexpected nav override: %+v", override)
	}
	if override.TilePath != filepath.Join("demo.gkworlddelta_data", "nav", "nav-demo", "tiny", "tiny_0_0_0.gknavtile") {
		t.Fatalf("unexpected relative tile path %q", override.TilePath)
	}
	tile, err := LoadNavTile(ResolveNavigationTileOverridePath(override, deltaPath))
	if err != nil {
		t.Fatalf("LoadNavTile failed: %v", err)
	}
	if tile.NavID != "nav-demo" || tile.AgentProfileID != "tiny" || tile.SourceDeltaHash != override.SourceDeltaHash || len(tile.Polygons) == 0 {
		t.Fatalf("unexpected delta nav tile: %+v", tile)
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}
	loaded, err := LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if !reflect.DeepEqual(delta.NavigationTileOverrides, loaded.NavigationTileOverrides) {
		t.Fatalf("expected nav override round-trip, want=%+v got=%+v", delta.NavigationTileOverrides, loaded.NavigationTileOverrides)
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksRecordsEmptyOverride(t *testing.T) {
	deltaPath := filepath.Join(t.TempDir(), "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          "nav-demo",
			AgentProfileID: profile.ID,
			ChunkCoord:     TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			TilePath:       "old.gknavtile",
			NavBuildHash:   "old-build",
		}},
	}
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		BuilderVersion:  "builder-a",
		ChunkSize:       4,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          4,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 64,
	}
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			for z := 0; z < 4; z++ {
				chunk.Voxels = append(chunk.Voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavDeltaBakeOptions{})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Tiles) != 0 || len(result.Overrides) != 1 || len(delta.NavigationTileOverrides) != 1 {
		t.Fatalf("expected one empty override and no tile writes, result=%+v delta=%+v", result, delta.NavigationTileOverrides)
	}
	override := delta.NavigationTileOverrides[0]
	if !override.Empty || override.TilePath != "" || override.NavBuildHash == "" || override.NavBuildHash == "old-build" {
		t.Fatalf("expected stale override to be replaced by empty rebuilt override, got %+v", override)
	}
}
