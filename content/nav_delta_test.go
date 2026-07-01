package content

import (
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

func TestExpandNavDirtyTileCoordsUsesMultiChunkAgentReach(t *testing.T) {
	modified := []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}}
	coords := ExpandNavDirtyTileCoords(modified, 2, 1, NavDirtyTileExpansionOptions{
		AgentProfiles: []NavAgentProfileDef{navTestAgentProfileWithID("large", 3.2, 3.1, 0.5)},
	})
	if len(coords) != 125 {
		t.Fatalf("expected two-chunk dirty expansion on each axis, got %d: %+v", len(coords), coords)
	}
	expected := map[TerrainChunkCoordDef]struct{}{
		{X: -2, Y: 0, Z: 0}: {},
		{X: 2, Y: 0, Z: 0}:  {},
		{X: 0, Y: -2, Z: 0}: {},
		{X: 0, Y: 2, Z: 0}:  {},
		{X: 0, Y: 0, Z: -2}: {},
		{X: 0, Y: 0, Z: 2}:  {},
	}
	for _, coord := range coords {
		delete(expected, coord)
	}
	if len(expected) != 0 {
		t.Fatalf("dirty expansion missing multi-chunk coords: %+v", expected)
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
	if len(result.ClearanceSourceOverrides) != 1 || len(result.ClearanceSourceTiles) != 1 || len(delta.NavigationClearanceSourceTileOverrides) != 1 {
		t.Fatalf("expected one clearance source override and tile, result=%+v delta=%+v", result, delta.NavigationClearanceSourceTileOverrides)
	}
	override := delta.NavigationTileOverrides[0]
	if override.NavID != "nav-demo" || override.AgentProfileID != "tiny" || override.Empty || override.TilePath == "" || override.SourceDeltaHash == "" || override.NavBuildHash == "" {
		t.Fatalf("unexpected nav override: %+v", override)
	}
	if override.TilePath != filepath.Join("demo.gkworlddelta_data", "nav", "nav-demo", "tiny", "tiny_0_0_0.gknavtile") {
		t.Fatalf("unexpected relative tile path %q", override.TilePath)
	}
	sourceOverride := delta.NavigationClearanceSourceTileOverrides[0]
	if sourceOverride.NavID != "nav-demo" || sourceOverride.ChunkCoord != chunk.Coord || sourceOverride.Empty || sourceOverride.TilePath == "" || sourceOverride.SourceDeltaHash == "" || sourceOverride.NavBuildHash == "" {
		t.Fatalf("unexpected clearance source override: %+v", sourceOverride)
	}
	if sourceOverride.TilePath != filepath.Join("demo.gkworlddelta_data", "nav", "nav-demo", "sources", "source_0_0_0.gknavsource") {
		t.Fatalf("unexpected relative clearance source path %q", sourceOverride.TilePath)
	}
	tile, err := LoadNavTile(ResolveNavigationTileOverridePath(override, deltaPath))
	if err != nil {
		t.Fatalf("LoadNavTile failed: %v", err)
	}
	if tile.NavID != "nav-demo" || tile.AgentProfileID != "tiny" || tile.SourceDeltaHash != override.SourceDeltaHash || len(tile.Polygons) == 0 {
		t.Fatalf("unexpected delta nav tile: %+v", tile)
	}
	sourceTile, err := LoadNavClearanceSourceTile(ResolveNavigationClearanceSourceTileOverridePath(sourceOverride, deltaPath))
	if err != nil {
		t.Fatalf("LoadNavClearanceSourceTile failed: %v", err)
	}
	if sourceTile.NavID != "nav-demo" || sourceTile.SourceDeltaHash != sourceOverride.SourceDeltaHash || len(sourceTile.Cells) == 0 {
		t.Fatalf("unexpected delta clearance source tile: %+v", sourceTile)
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
	if !reflect.DeepEqual(delta.NavigationClearanceSourceTileOverrides, loaded.NavigationClearanceSourceTileOverrides) {
		t.Fatalf("expected clearance source override round-trip, want=%+v got=%+v", delta.NavigationClearanceSourceTileOverrides, loaded.NavigationClearanceSourceTileOverrides)
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksUsesCompactSourceForUnevenEdits(t *testing.T) {
	root := t.TempDir()
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.05, 0.5, 0.3)
	profile.NavCellSize = 0.025
	profile.MaxSlopeDegrees = 45
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       40,
		VoxelResolution: 0.025,
		AgentProfiles:   []NavAgentProfileDef{profile},
	}
	voxels := make([]ImportedWorldVoxelDef, 0)
	for x := 10; x <= 25; x++ {
		for z := 10; z <= 25; z++ {
			height := (x-10)/2 + (z-10)/8
			height += int(math.Round(5 * math.Sin(float64(x-10)/15*math.Pi)))
			for y := 0; y <= height; y++ {
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          40,
		VoxelResolution:    0.025,
		Voxels:             voxels,
		NonEmptyVoxelCount: len(voxels),
	}
	delta := &WorldDeltaDef{LevelID: "level-a"}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		chunk.Coord: chunk,
	}, NavDeltaBakeOptions{})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Tiles) != 1 {
		t.Fatalf("expected one delta tile, got %+v", result.Tiles)
	}
	if len(result.ClearanceSourceTiles) != 1 || len(delta.NavigationClearanceSourceTileOverrides) != 1 {
		t.Fatalf("expected uneven edit to emit canonical clearance source, result=%+v delta=%+v", result, delta.NavigationClearanceSourceTileOverrides)
	}
	for _, tile := range result.Tiles {
		if len(tile.Polygons) == 0 {
			t.Fatalf("expected derived uneven edit nav polygons, got %+v", tile.Polygons)
		}
		for _, polygon := range tile.Polygons {
			if strings.HasPrefix(polygon.ID, "raster_cell:") {
				t.Fatalf("did not expect raster-cell islands from delta rebuild, got %+v", polygon)
			}
		}
		if validation := ValidateNavTile(tile); validation.HasErrors() {
			t.Fatalf("ValidateNavTile failed: %s", validation.Error())
		}
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksPreservesEditedHole(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "demo.gknav")
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	staticTilePath := filepath.Join(root, "demo_navtiles", "tiny_0_0_0.gknavtile")
	baseChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              coord,
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(1, 14, 1, 14, 0),
		NonEmptyVoxelCount: 14 * 14,
	}
	baseTileResult, err := BuildNavTileFromImportedWorldChunk(baseChunk, profile, NavTileBuildOptions{
		NavID: "nav-demo",
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk base failed: %v", err)
	}
	if err := SaveNavTile(staticTilePath, baseTileResult.Tile); err != nil {
		t.Fatalf("SaveNavTile base failed: %v", err)
	}
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  "builder-a",
		ChunkSize:       16,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
		Tiles: []NavTileEntryDef{{
			Coord:          coord,
			AgentProfileID: profile.ID,
			TilePath:       AuthorDocumentPath(staticTilePath, navPath),
			BoundsMin:      baseTileResult.Tile.BoundsMin,
			BoundsMax:      baseTileResult.Tile.BoundsMax,
		}},
	}
	if err := SaveNavManifest(navPath, baseNav); err != nil {
		t.Fatalf("SaveNavManifest failed: %v", err)
	}
	deltaVoxels := removeNavTestFloorVoxels(baseChunk.Voxels, 6, 9, 6, 9, 0)
	deltaChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              coord,
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             deltaVoxels,
		NonEmptyVoxelCount: len(deltaVoxels),
	}
	delta := &WorldDeltaDef{LevelID: "level-a"}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		coord: deltaChunk,
	}, NavDeltaBakeOptions{BaseNavPath: navPath})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Overrides) != 1 || len(result.Tiles) != 1 || len(delta.NavigationTileOverrides) != 1 {
		t.Fatalf("expected one edited-hole nav override and tile, result=%+v delta=%+v", result, delta.NavigationTileOverrides)
	}
	lookup, err := LoadEffectiveNavTile(baseNav, navPath, delta, deltaPath, coord, profile.ID)
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile failed: %v", err)
	}
	if !lookup.Found || lookup.Empty || lookup.Source != NavTileLookupSourceDelta {
		t.Fatalf("expected effective delta tile, got %+v", lookup)
	}
	navAssertTileCoversCells(t, lookup.Tile, deltaChunk.VoxelResolution,
		navTestCellCoord{X: 5, Y: 1, Z: 5},
		navTestCellCoord{X: 10, Y: 1, Z: 10},
	)
	navAssertTileExcludesCells(t, lookup.Tile, deltaChunk.VoxelResolution,
		navTestCellCoord{X: 6, Y: 1, Z: 6},
		navTestCellCoord{X: 7, Y: 1, Z: 7},
		navTestCellCoord{X: 8, Y: 1, Z: 8},
		navTestCellCoord{X: 9, Y: 1, Z: 9},
	)
	path, err := FindEffectiveNavPath(baseNav, navPath, delta, deltaPath, Vec3{2.5, 1, 2.5}, Vec3{7.5, 1, 7.5}, NavPathOptions{
		AgentProfileID:       profile.ID,
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found || path.FailureReason != NavPathFailureEndPolygonMissing {
		t.Fatalf("expected endpoint inside edited hole to be rejected, got %+v", path)
	}
}

func TestSaveNavDeltaTilesForImportedWorldDeltaLoadsEditedChunkOverrides(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "demo.gkworld")
	navPath := filepath.Join(root, "demo.gknav")
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	world := &ImportedWorldDef{
		WorldID:          "world-a",
		SchemaVersion:    CurrentImportedWorldSchemaVersion,
		Kind:             ImportedWorldKindVoxelWorld,
		ChunkSize:        8,
		VoxelResolution:  1,
		ChunkPayloadKind: ImportedWorldChunkPayloadSparseJSONV1,
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
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
	if err := SaveNavManifest(navPath, baseNav); err != nil {
		t.Fatalf("SaveNavManifest failed: %v", err)
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(1, 6, 1, 6, 0),
		NonEmptyVoxelCount: 36,
	}
	chunkPath := filepath.Join(DefaultWorldDeltaDataDir(deltaPath), "imported_world-a_0_0_0.gkchunk")
	if err := SaveImportedWorldChunk(chunkPath, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		ImportedWorldChunkOverrides: []ImportedWorldChunkOverrideDef{{
			WorldID:      "world-a",
			ChunkCoord:   chunk.Coord,
			SnapshotPath: authorPathRelativeToDocument(chunkPath, deltaPath),
		}},
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}

	result, err := SaveNavDeltaTilesForImportedWorldDelta(worldPath, navPath, deltaPath, NavImportedWorldDeltaBakeOptions{})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldDelta failed: %v", err)
	}
	if len(result.Overrides) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected one regenerated delta nav tile, got result=%+v", result)
	}
	loaded, err := LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if len(loaded.NavigationTileOverrides) != 1 {
		t.Fatalf("expected saved navigation override, got %+v", loaded.NavigationTileOverrides)
	}
	override := loaded.NavigationTileOverrides[0]
	if override.NavID != "nav-demo" || override.AgentProfileID != profile.ID || override.ChunkCoord != chunk.Coord || override.Empty || override.TilePath == "" {
		t.Fatalf("unexpected saved navigation override: %+v", override)
	}
	tile, err := LoadNavTile(ResolveNavigationTileOverridePath(override, deltaPath))
	if err != nil {
		t.Fatalf("LoadNavTile failed: %v", err)
	}
	if tile.SourceDeltaHash != override.SourceDeltaHash || len(tile.Polygons) == 0 {
		t.Fatalf("unexpected saved delta nav tile: %+v", tile)
	}
}

func TestSaveNavDeltaTilesForImportedWorldDeltaReloadsEditedGeometryAndNavTogether(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	navPath := filepath.Join(root, "worlds", "demo.gknav")
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}

	baseChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              coord,
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(1, 14, 1, 14, 0),
		NonEmptyVoxelCount: 14 * 14,
	}
	baseChunkPath := filepath.Join(root, "worlds", "chunks", "chunk_0_0_0.gkchunk")
	if err := SaveImportedWorldChunk(baseChunkPath, baseChunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk base failed: %v", err)
	}
	world := &ImportedWorldDef{
		WorldID:            "world-a",
		SchemaVersion:      CurrentImportedWorldSchemaVersion,
		Kind:               ImportedWorldKindVoxelWorld,
		ChunkSize:          16,
		VoxelResolution:    1,
		ChunkPayloadKind:   ImportedWorldChunkPayloadSparseJSONV1,
		SourceBuildVersion: "test",
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          AuthorDocumentPath(baseChunkPath, worldPath),
			NonEmptyVoxelCount: baseChunk.NonEmptyVoxelCount,
			PayloadKind:        ImportedWorldChunkPayloadSparseJSONV1,
		}},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	baseTileResult, err := BuildNavTileFromImportedWorldChunk(baseChunk, profile, NavTileBuildOptions{NavID: "nav-demo"})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk base failed: %v", err)
	}
	staticTilePath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_0_0_0.gknavtile")
	if err := SaveNavTile(staticTilePath, baseTileResult.Tile); err != nil {
		t.Fatalf("SaveNavTile static failed: %v", err)
	}
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       16,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
		Tiles: []NavTileEntryDef{{
			Coord:          coord,
			AgentProfileID: profile.ID,
			TilePath:       AuthorDocumentPath(staticTilePath, navPath),
			BoundsMin:      baseTileResult.Tile.BoundsMin,
			BoundsMax:      baseTileResult.Tile.BoundsMax,
		}},
	}
	if err := SaveNavManifest(navPath, baseNav); err != nil {
		t.Fatalf("SaveNavManifest failed: %v", err)
	}

	editedVoxels := removeNavTestFloorVoxels(baseChunk.Voxels, 6, 9, 6, 9, 0)
	editedChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              coord,
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             editedVoxels,
		NonEmptyVoxelCount: len(editedVoxels),
	}
	editedChunkPath := filepath.Join(DefaultWorldDeltaDataDir(deltaPath), "imported_world-a_0_0_0.gkchunk")
	if err := SaveImportedWorldChunk(editedChunkPath, editedChunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk edited failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		ImportedWorldChunkOverrides: []ImportedWorldChunkOverrideDef{{
			WorldID:      "world-a",
			ChunkCoord:   coord,
			SnapshotPath: authorPathRelativeToDocument(editedChunkPath, deltaPath),
		}},
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		t.Fatalf("SaveWorldDelta edited failed: %v", err)
	}

	if _, err := SaveNavDeltaTilesForImportedWorldDelta(worldPath, navPath, deltaPath, NavImportedWorldDeltaBakeOptions{}); err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldDelta failed: %v", err)
	}
	reloadedDelta, err := LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if len(reloadedDelta.ImportedWorldChunkOverrides) != 1 {
		t.Fatalf("expected edited geometry override to survive reload, got %+v", reloadedDelta.ImportedWorldChunkOverrides)
	}
	reloadedChunk, err := LoadImportedWorldChunk(ResolveDocumentPath(reloadedDelta.ImportedWorldChunkOverrides[0].SnapshotPath, deltaPath))
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk reloaded edited failed: %v", err)
	}
	if navDeltaTestChunkHasVoxel(reloadedChunk, 7, 0, 7) {
		t.Fatalf("expected edited hole voxel to stay removed after reload")
	}
	if !navDeltaTestChunkHasVoxel(reloadedChunk, 5, 0, 5) || !navDeltaTestChunkHasVoxel(reloadedChunk, 10, 0, 10) {
		t.Fatalf("expected edited chunk to preserve floor around hole")
	}

	lookup, err := LoadEffectiveNavTile(baseNav, navPath, reloadedDelta, deltaPath, coord, profile.ID)
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile failed: %v", err)
	}
	if !lookup.Found || lookup.Empty || lookup.Source != NavTileLookupSourceDelta {
		t.Fatalf("expected reloaded effective nav to use delta tile, got %+v", lookup)
	}
	override, ok := findNavigationTileOverride(reloadedDelta.NavigationTileOverrides, "nav-demo", profile.ID, coord)
	if !ok || override.Empty || override.SourceDeltaHash != importedWorldChunkNavSourceHash(reloadedChunk) {
		t.Fatalf("expected nav override source hash to match reloaded edited chunk, override=%+v", override)
	}
	navAssertTileCoversCells(t, lookup.Tile, reloadedChunk.VoxelResolution,
		navTestCellCoord{X: 5, Y: 1, Z: 5},
		navTestCellCoord{X: 10, Y: 1, Z: 10},
	)
	navAssertTileExcludesCells(t, lookup.Tile, reloadedChunk.VoxelResolution,
		navTestCellCoord{X: 7, Y: 1, Z: 7},
	)
	path, err := FindEffectiveNavPath(baseNav, navPath, reloadedDelta, deltaPath, Vec3{2.5, 1, 2.5}, Vec3{7.5, 1, 7.5}, NavPathOptions{
		AgentProfileID:       profile.ID,
		MaxTileSearchRadius:  1,
		EndpointSnapDistance: 0.2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if path.Found || path.FailureReason != NavPathFailureEndPolygonMissing {
		t.Fatalf("expected path endpoint inside reloaded edited hole to be rejected, got %+v", path)
	}
}

func TestSaveNavDeltaTilesForImportedWorldDeltaUsesEditedBorderSpansForPortals(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	navPath := filepath.Join(root, "worlds", "demo.gknav")
	deltaPath := filepath.Join(root, "levels", "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}

	leftBaseChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              leftCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(7, 7, 0, 7, 0),
		NonEmptyVoxelCount: 8,
	}
	rightChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              rightCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(0, 0, 0, 7, 0),
		NonEmptyVoxelCount: 8,
	}
	leftBasePath := filepath.Join(root, "worlds", "chunks", "chunk_0_0_0.gkchunk")
	rightPath := filepath.Join(root, "worlds", "chunks", "chunk_1_0_0.gkchunk")
	if err := SaveImportedWorldChunk(leftBasePath, leftBaseChunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk left base failed: %v", err)
	}
	if err := SaveImportedWorldChunk(rightPath, rightChunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk right failed: %v", err)
	}
	world := &ImportedWorldDef{
		WorldID:          "world-a",
		SchemaVersion:    CurrentImportedWorldSchemaVersion,
		Kind:             ImportedWorldKindVoxelWorld,
		ChunkSize:        8,
		VoxelResolution:  1,
		ChunkPayloadKind: ImportedWorldChunkPayloadSparseJSONV1,
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: leftCoord, ChunkPath: AuthorDocumentPath(leftBasePath, worldPath), NonEmptyVoxelCount: leftBaseChunk.NonEmptyVoxelCount, PayloadKind: ImportedWorldChunkPayloadSparseJSONV1},
			{Coord: rightCoord, ChunkPath: AuthorDocumentPath(rightPath, worldPath), NonEmptyVoxelCount: rightChunk.NonEmptyVoxelCount, PayloadKind: ImportedWorldChunkPayloadSparseJSONV1},
		},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	leftStaticResult, err := BuildNavTileFromImportedWorldChunk(leftBaseChunk, profile, NavTileBuildOptions{NavID: "nav-demo", NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{leftCoord: leftBaseChunk, rightCoord: rightChunk}})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk left static failed: %v", err)
	}
	rightStaticResult, err := BuildNavTileFromImportedWorldChunk(rightChunk, profile, NavTileBuildOptions{NavID: "nav-demo", NeighborChunks: map[TerrainChunkCoordDef]*ImportedWorldChunkDef{leftCoord: leftBaseChunk, rightCoord: rightChunk}})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk right static failed: %v", err)
	}
	leftStaticPath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_0_0_0.gknavtile")
	rightStaticPath := filepath.Join(root, "worlds", "demo_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(leftStaticPath, leftStaticResult.Tile); err != nil {
		t.Fatalf("SaveNavTile left static failed: %v", err)
	}
	if err := SaveNavTile(rightStaticPath, rightStaticResult.Tile); err != nil {
		t.Fatalf("SaveNavTile right static failed: %v", err)
	}
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
		Tiles: []NavTileEntryDef{
			{Coord: leftCoord, AgentProfileID: profile.ID, TilePath: AuthorDocumentPath(leftStaticPath, navPath), BoundsMin: leftStaticResult.Tile.BoundsMin, BoundsMax: leftStaticResult.Tile.BoundsMax},
			{Coord: rightCoord, AgentProfileID: profile.ID, TilePath: AuthorDocumentPath(rightStaticPath, navPath), BoundsMin: rightStaticResult.Tile.BoundsMin, BoundsMax: rightStaticResult.Tile.BoundsMax},
		},
	}
	if err := SaveNavManifest(navPath, baseNav); err != nil {
		t.Fatalf("SaveNavManifest failed: %v", err)
	}

	leftEditedVoxels := removeNavTestFloorVoxels(leftBaseChunk.Voxels, 7, 7, 3, 4, 0)
	leftEditedChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              leftCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             leftEditedVoxels,
		NonEmptyVoxelCount: len(leftEditedVoxels),
	}
	leftEditedPath := filepath.Join(DefaultWorldDeltaDataDir(deltaPath), "imported_world-a_0_0_0.gkchunk")
	if err := SaveImportedWorldChunk(leftEditedPath, leftEditedChunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk left edited failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		ImportedWorldChunkOverrides: []ImportedWorldChunkOverrideDef{{
			WorldID:      "world-a",
			ChunkCoord:   leftCoord,
			SnapshotPath: authorPathRelativeToDocument(leftEditedPath, deltaPath),
		}},
	}
	if err := SaveWorldDelta(deltaPath, delta); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}

	if _, err := SaveNavDeltaTilesForImportedWorldDelta(worldPath, navPath, deltaPath, NavImportedWorldDeltaBakeOptions{DirtyCoords: []TerrainChunkCoordDef{leftCoord}}); err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldDelta failed: %v", err)
	}
	reloadedDelta, err := LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	leftLookup, err := LoadEffectiveNavTile(baseNav, navPath, reloadedDelta, deltaPath, leftCoord, profile.ID)
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile left failed: %v", err)
	}
	rightLookup, err := LoadEffectiveNavTile(baseNav, navPath, reloadedDelta, deltaPath, rightCoord, profile.ID)
	if err != nil {
		t.Fatalf("LoadEffectiveNavTile right failed: %v", err)
	}
	if !leftLookup.Found || leftLookup.Empty || leftLookup.Source != NavTileLookupSourceDelta || !rightLookup.Found || rightLookup.Empty {
		t.Fatalf("expected effective delta seam tiles, left=%+v right=%+v", leftLookup, rightLookup)
	}
	leftPortals := navDeltaTestPortalsToCoord(leftLookup.Tile, rightCoord)
	if len(leftPortals) != 2 {
		t.Fatalf("expected edited left border gap to split portals into two spans, got %+v", leftPortals)
	}
	if !navAlmostEqual(leftPortals[0].Start[2], 0, 1e-4) || !navAlmostEqual(leftPortals[0].End[2], 3, 1e-4) ||
		!navAlmostEqual(leftPortals[1].Start[2], 5, 1e-4) || !navAlmostEqual(leftPortals[1].End[2], 8, 1e-4) {
		t.Fatalf("expected portal intervals [0,3] and [5,8], got %+v", leftPortals)
	}
	if navDeltaTestPortalCoversZ(leftPortals, 3.5) {
		t.Fatalf("did not expect portal through edited seam gap, got %+v", leftPortals)
	}
	rightPortals := navDeltaTestPortalsToCoord(rightLookup.Tile, leftCoord)
	if len(rightPortals) != 2 {
		t.Fatalf("expected reciprocal right portals to match edited seam gap, got %+v", rightPortals)
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksAddsPortalsToStaticNeighbor(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "demo.gknav")
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	delta := &WorldDeltaDef{LevelID: "level-a"}
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	rightTilePath := filepath.Join(root, "demo_navtiles", "tiny_1_0_0.gknavtile")
	if err := SaveNavTile(rightTilePath, navPathTestTile("nav-demo", "tiny", rightCoord, "right_static", Vec3{8, 1, 1}, Vec3{9, 1, 4})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	baseNav := &NavManifestDef{
		NavID:           "nav-demo",
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		LevelID:         "level-a",
		SourceWorldID:   "world-a",
		BuilderVersion:  "builder-a",
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{profile},
		Tiles: []NavTileEntryDef{{
			Coord:          rightCoord,
			AgentProfileID: profile.ID,
			TilePath:       AuthorDocumentPath(rightTilePath, navPath),
			BoundsMin:      [3]float32{8, 0, 0},
			BoundsMax:      [3]float32{16, 8, 8},
		}},
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              leftCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(7, 7, 1, 3, 0),
		NonEmptyVoxelCount: 3,
	}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		leftCoord: chunk,
	}, NavDeltaBakeOptions{BaseNavPath: navPath})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Tiles) != 1 || len(delta.NavigationTileOverrides) != 1 {
		t.Fatalf("expected one delta tile, result=%+v delta=%+v", result, delta.NavigationTileOverrides)
	}
	tile := result.Tiles[ResolveNavigationTileOverridePath(delta.NavigationTileOverrides[0], deltaPath)]
	if tile == nil {
		t.Fatalf("expected saved delta tile in result map, got %+v", result.Tiles)
	}
	if len(tile.Portals) != 1 || tile.Portals[0].ToTileCoord != rightCoord || tile.Portals[0].ToPolygonID != "right_static" {
		t.Fatalf("expected delta tile portal to static neighbor, got %+v", tile.Portals)
	}
}

func TestSaveNavDeltaTilesForImportedWorldChunksDoesNotPortalToFreshEmptyNeighbor(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "demo.gknav")
	deltaPath := filepath.Join(root, "demo.gkworlddelta")
	profile := navTestAgentProfileWithID("tiny", 0.2, 1.0, 0.5)
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	staleRightTilePath := filepath.Join(root, "demo.gkworlddelta_data", "nav", "nav-demo", "tiny", "stale_right.gknavtile")
	if err := SaveNavTile(staleRightTilePath, navPathTestTile("nav-demo", "tiny", rightCoord, "right_stale", Vec3{8, 1, 1}, Vec3{9, 1, 4})); err != nil {
		t.Fatalf("SaveNavTile stale right failed: %v", err)
	}
	delta := &WorldDeltaDef{
		LevelID: "level-a",
		NavigationTileOverrides: []NavigationTileOverrideDef{{
			NavID:          "nav-demo",
			AgentProfileID: profile.ID,
			ChunkCoord:     rightCoord,
			TilePath:       authorPathRelativeToDocument(staleRightTilePath, deltaPath),
			NavBuildHash:   "old-build",
		}},
	}
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
	leftChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              leftCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             navTestFloorVoxels(7, 7, 1, 3, 0),
		NonEmptyVoxelCount: 3,
	}
	rightChunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              rightCoord,
		ChunkSize:          8,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 8 * 8 * 8,
	}
	for x := 0; x < rightChunk.ChunkSize; x++ {
		for y := 0; y < rightChunk.ChunkSize; y++ {
			for z := 0; z < rightChunk.ChunkSize; z++ {
				rightChunk.Voxels = append(rightChunk.Voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
			}
		}
	}

	result, err := SaveNavDeltaTilesForImportedWorldChunks(deltaPath, delta, baseNav, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		leftCoord:  leftChunk,
		rightCoord: rightChunk,
	}, NavDeltaBakeOptions{BaseNavPath: navPath})
	if err != nil {
		t.Fatalf("SaveNavDeltaTilesForImportedWorldChunks failed: %v", err)
	}
	if len(result.Tiles) != 1 || len(delta.NavigationTileOverrides) != 2 {
		t.Fatalf("expected one saved left tile and two overrides, result=%+v delta=%+v", result, delta.NavigationTileOverrides)
	}
	var leftOverride NavigationTileOverrideDef
	var rightOverride NavigationTileOverrideDef
	for _, override := range delta.NavigationTileOverrides {
		switch override.ChunkCoord {
		case leftCoord:
			leftOverride = override
		case rightCoord:
			rightOverride = override
		}
	}
	if rightOverride.ChunkCoord != rightCoord || !rightOverride.Empty || rightOverride.TilePath != "" || rightOverride.NavBuildHash == "old-build" {
		t.Fatalf("expected stale right override to become fresh empty override, got %+v", rightOverride)
	}
	leftTile := result.Tiles[ResolveNavigationTileOverridePath(leftOverride, deltaPath)]
	if leftTile == nil {
		t.Fatalf("expected saved left tile in result map, got %+v", result.Tiles)
	}
	for _, portal := range leftTile.Portals {
		if portal.ToTileCoord == rightCoord {
			t.Fatalf("did not expect portal to freshly empty right neighbor, got %+v", leftTile.Portals)
		}
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

func navDeltaTestChunkHasVoxel(chunk *ImportedWorldChunkDef, x, y, z int) bool {
	if chunk == nil {
		return false
	}
	for _, voxel := range chunk.Voxels {
		if voxel.X == x && voxel.Y == y && voxel.Z == z && voxel.Value != 0 {
			return true
		}
	}
	return false
}

func navDeltaTestPortalsToCoord(tile *NavTileDef, coord TerrainChunkCoordDef) []NavPortalDef {
	if tile == nil {
		return nil
	}
	out := make([]NavPortalDef, 0)
	for _, portal := range tile.Portals {
		if portal.ToTileCoord == coord {
			out = append(out, portal)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if navAlmostEqual(out[i].Start[2], out[j].Start[2], 1e-4) {
			return out[i].End[2] < out[j].End[2]
		}
		return out[i].Start[2] < out[j].Start[2]
	})
	return out
}

func navDeltaTestPortalCoversZ(portals []NavPortalDef, z float32) bool {
	for _, portal := range portals {
		minZ := portal.Start[2]
		maxZ := portal.End[2]
		if minZ > maxZ {
			minZ, maxZ = maxZ, minZ
		}
		if z > minZ+1e-4 && z < maxZ-1e-4 {
			return true
		}
	}
	return false
}
