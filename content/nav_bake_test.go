package content

import (
	"path/filepath"
	"testing"
)

func TestSaveNavBakeForImportedWorldManifestWritesManifestAndTiles(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "demo_0_0_0.gkchunk")
	navPath := filepath.Join(root, "nav", "demo.gknav")

	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    0.5,
		Voxels:             navTestFloorVoxels(1, 6, 1, 6, 0),
		NonEmptyVoxelCount: 36,
	}
	if err := SaveImportedWorldChunk(chunkPath, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	world := &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 0.5,
		SourceHash:      "world-source-hash",
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	result, err := SaveNavBakeForImportedWorldManifest(worldPath, navPath, NavBakeOptions{
		NavID: "nav-demo",
		AgentProfiles: []NavAgentProfileDef{
			navTestAgentProfile(0.2, 1.0, 0.5),
			navTestAgentProfileWithID("wide", 0.6, 1.0, 0.5),
		},
	})
	if err != nil {
		t.Fatalf("SaveNavBakeForImportedWorldManifest failed: %v", err)
	}
	if result.Manifest == nil || len(result.Manifest.Tiles) != 2 || len(result.Tiles) != 2 || len(result.Manifest.ClearanceSourceTiles) != 1 || len(result.ClearanceSourceTiles) != 1 {
		t.Fatalf("expected two profile tiles and one clearance source tile, got manifest=%+v tiles=%d source_tiles=%d", result.Manifest, len(result.Tiles), len(result.ClearanceSourceTiles))
	}

	loadedManifest, err := LoadNavManifest(navPath)
	if err != nil {
		t.Fatalf("LoadNavManifest failed: %v", err)
	}
	if loadedManifest.NavID != "nav-demo" || loadedManifest.SourceWorldID != "world-a" || loadedManifest.SourceLevelHash != "world-source-hash" {
		t.Fatalf("unexpected nav manifest metadata: %+v", loadedManifest)
	}
	if validation := ValidateNavManifest(loadedManifest, NavValidationOptions{DocumentPath: navPath}); validation.HasErrors() {
		t.Fatalf("ValidateNavManifest failed: %s (%+v)", validation.Error(), validation.Issues)
	}
	for _, entry := range loadedManifest.Tiles {
		if entry.SourcePayloadHash == "" || entry.NavBuildHash == "" {
			t.Fatalf("expected source/build hashes on tile entry: %+v", entry)
		}
		tile, err := LoadNavTile(ResolveNavTilePath(entry, navPath))
		if err != nil {
			t.Fatalf("LoadNavTile failed for %+v: %v", entry, err)
		}
		if tile.AgentProfileID != entry.AgentProfileID || tile.SourcePayloadHash != entry.SourcePayloadHash || tile.NavBuildHash != entry.NavBuildHash {
			t.Fatalf("tile metadata does not match entry: tile=%+v entry=%+v", tile, entry)
		}
		if len(tile.Polygons) == 0 {
			t.Fatalf("expected baked tile polygons, got %+v", tile)
		}
	}
	for _, entry := range loadedManifest.ClearanceSourceTiles {
		if entry.SourcePayloadHash == "" || entry.NavBuildHash == "" || entry.MaxClearanceRadius <= 0 {
			t.Fatalf("expected source/build hashes and max radius on clearance source tile entry: %+v", entry)
		}
		sourceTile, err := LoadNavClearanceSourceTile(ResolveNavClearanceSourceTilePath(entry, navPath))
		if err != nil {
			t.Fatalf("LoadNavClearanceSourceTile failed for %+v: %v", entry, err)
		}
		if sourceTile.Coord != entry.Coord || sourceTile.SourcePayloadHash != entry.SourcePayloadHash || sourceTile.NavBuildHash != entry.NavBuildHash {
			t.Fatalf("clearance source metadata does not match entry: tile=%+v entry=%+v", sourceTile, entry)
		}
		if len(sourceTile.Cells) == 0 {
			t.Fatalf("expected baked clearance source cells, got %+v", sourceTile)
		}
	}
}

func TestSaveNavBakeForImportedWorldManifestReportsProgress(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "demo_0_0_0.gkchunk")
	navPath := filepath.Join(root, "nav", "demo.gknav")
	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-progress",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    0.5,
		Voxels:             navTestFloorVoxels(1, 6, 1, 6, 0),
		NonEmptyVoxelCount: 36,
	}
	if err := SaveImportedWorldChunk(chunkPath, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	world := &ImportedWorldDef{
		WorldID:         "world-progress",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 0.5,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	var events []NavBakeProgress
	_, err := SaveNavBakeForImportedWorldManifest(worldPath, navPath, NavBakeOptions{
		NavID: "nav-progress",
		AgentProfiles: []NavAgentProfileDef{
			navTestAgentProfile(0.2, 1.0, 0.5),
		},
		Progress: func(progress NavBakeProgress) {
			events = append(events, progress)
		},
	})
	if err != nil {
		t.Fatalf("SaveNavBakeForImportedWorldManifest failed: %v", err)
	}
	for _, stage := range []string{
		NavBakeProgressStageLoadChunk,
		NavBakeProgressStageIntermediate,
		NavBakeProgressStageBuildSourceTile,
		NavBakeProgressStageBuildTile,
		NavBakeProgressStagePortalStitch,
		NavBakeProgressStageSaveSourceTile,
		NavBakeProgressStageSaveTile,
		NavBakeProgressStageSaveManifest,
	} {
		if !navBakeProgressHasStage(events, stage) {
			t.Fatalf("expected progress stage %q in %+v", stage, events)
		}
	}
	buildEvent, ok := navBakeProgressEvent(events, NavBakeProgressStageBuildTile)
	if !ok {
		t.Fatalf("expected build progress event in %+v", events)
	}
	if !buildEvent.HasCoord || buildEvent.Coord != (TerrainChunkCoordDef{}) {
		t.Fatalf("expected zero coord to be explicitly reported, got %+v", buildEvent)
	}
	if buildEvent.Duration <= 0 {
		t.Fatalf("expected build progress duration, got %+v", buildEvent)
	}
	if buildEvent.Stats.OccupiedVoxels != chunk.NonEmptyVoxelCount ||
		buildEvent.Stats.CandidateSpans != chunk.NonEmptyVoxelCount ||
		buildEvent.Stats.AcceptedSpans == 0 ||
		buildEvent.Stats.CompactCells == 0 ||
		buildEvent.Stats.Regions == 0 ||
		buildEvent.Polygons == 0 {
		t.Fatalf("expected build progress stats, got %+v", buildEvent)
	}
	intermediateEvent, ok := navBakeProgressEvent(events, NavBakeProgressStageIntermediate)
	if !ok {
		t.Fatalf("expected intermediate progress event in %+v", events)
	}
	if !intermediateEvent.HasCoord || intermediateEvent.Coord != (TerrainChunkCoordDef{}) {
		t.Fatalf("expected zero coord in intermediate progress event, got %+v", intermediateEvent)
	}
	if intermediateEvent.Duration <= 0 ||
		intermediateEvent.Stats.OccupiedVoxels != chunk.NonEmptyVoxelCount ||
		intermediateEvent.Stats.AcceptedSpans == 0 ||
		intermediateEvent.Stats.CompactCells == 0 ||
		intermediateEvent.Stats.Regions == 0 {
		t.Fatalf("expected intermediate progress duration/stats, got %+v", intermediateEvent)
	}
}

func TestNavBakeBuildWorkerCountCapsToJobCount(t *testing.T) {
	if got := navBakeBuildWorkerCount(8, 3); got != 3 {
		t.Fatalf("expected requested workers to cap to job count, got %d", got)
	}
	if got := navBakeBuildWorkerCount(1, 3); got != 1 {
		t.Fatalf("expected explicit sequential worker count, got %d", got)
	}
	if got := navBakeBuildWorkerCount(0, 0); got != 0 {
		t.Fatalf("expected no workers for no jobs, got %d", got)
	}
	if got := navBakeBuildWorkerCount(0, 2); got < 1 || got > 2 {
		t.Fatalf("expected default workers to fit available jobs, got %d", got)
	}
}

func TestBakeNavFromImportedWorldSkipsUnwalkableChunks(t *testing.T) {
	world := &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       4,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          "chunks/solid.gkchunk",
			NonEmptyVoxelCount: 64,
		}},
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

	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{chunk.Coord: chunk}, "solid.gknav", NavBakeOptions{})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 0 || len(result.Tiles) != 0 {
		t.Fatalf("expected fully solid chunk to have no walkable nav tiles, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
}

func TestBakeNavFromImportedWorldSynthesizesEmptyNeighborContextForSparseWorld(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "sparse-boundary.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	chunk := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	chunk.Coord = coord
	world := &ImportedWorldDef{
		WorldID:         "world-sparse",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       4,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          "chunks/sparse_0_0_0.gkchunk",
			NonEmptyVoxelCount: 1,
		}},
	}

	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		coord: chunk,
	}, navPath, NavBakeOptions{
		NavID:         "nav-sparse",
		AgentProfiles: []NavAgentProfileDef{navTestAgentProfile(0.6, 1.0, 1.0)},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected sparse boundary chunk to bake one tile, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	for _, tile := range result.Tiles {
		if !navTileHasCell(tile, 3, 1, 1, chunk.VoxelResolution) {
			t.Fatalf("expected clear missing neighbor space to preserve boundary cell, got %+v", tile.Polygons)
		}
	}
}

func TestBakeNavFromImportedWorldUsesKnownEmptyVerticalNeighborForClearance(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "vertical-boundary.gknav")
	baseCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	aboveCoord := TerrainChunkCoordDef{X: 0, Y: 1, Z: 0}
	chunk := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 1, Y: 3, Z: 1, Value: 1})
	chunk.Coord = baseCoord
	world := &ImportedWorldDef{
		WorldID:         "world-vertical",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       4,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{
			{
				Coord:              baseCoord,
				ChunkPath:          "chunks/base.gkchunk",
				NonEmptyVoxelCount: 1,
			},
			{
				Coord:              aboveCoord,
				ChunkPath:          "chunks/above.gkchunk",
				NonEmptyVoxelCount: 0,
			},
		},
	}

	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		baseCoord: chunk,
	}, navPath, NavBakeOptions{
		NavID:         "nav-vertical",
		AgentProfiles: []NavAgentProfileDef{navTestAgentProfile(0.2, 1.0, 1.0)},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected one top-boundary tile, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	for _, tile := range result.Tiles {
		if !navTileHasCell(tile, 1, 4, 1, chunk.VoxelResolution) {
			t.Fatalf("expected explicit empty chunk above to preserve top-boundary cell, got %+v", tile.Polygons)
		}
	}
}

func TestSaveNavBakeForImportedWorldManifestUsesGenericBuildSource(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	sourcePath := DefaultNavBuildSourcePath(worldPath)
	navPath := filepath.Join(root, "nav", "demo.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}

	world := &ImportedWorldDef{
		WorldID:         "world-source",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			NonEmptyVoxelCount: 0,
		}},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
	source := &NavBuildSourceDef{
		SourceID:      "source-only",
		SourceWorldID: world.WorldID,
		Surfaces: []NavBuildSurfaceDef{{
			ID: "walkable_quad",
			Vertices: []Vec3{
				{1, 1, 1},
				{4, 1, 1},
				{4, 1, 4},
				{1, 1, 4},
			},
		}},
	}
	if err := SaveNavBuildSource(sourcePath, source); err != nil {
		t.Fatalf("SaveNavBuildSource failed: %v", err)
	}

	result, err := SaveNavBakeForImportedWorldManifest(worldPath, navPath, NavBakeOptions{
		NavID:           "nav-source",
		BuildSourcePath: AuthorDocumentPath(sourcePath, worldPath),
		AgentProfiles:   []NavAgentProfileDef{navTestAgentProfile(0.2, 1.0, 0.5)},
	})
	if err != nil {
		t.Fatalf("SaveNavBakeForImportedWorldManifest failed: %v", err)
	}
	if result.Manifest == nil || len(result.Manifest.Tiles) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected one source-only tile, got manifest=%+v tiles=%d", result.Manifest, len(result.Tiles))
	}
	var tile *NavTileDef
	for _, candidate := range result.Tiles {
		tile = candidate
	}
	if tile == nil || !navTileHasPolygon(tile, "surface:walkable_quad") {
		t.Fatalf("expected generic surface polygon in baked tile, got %+v", tile)
	}
	entry := result.Manifest.Tiles[0]
	if entry.NavBuildHash == "" || entry.NavBuildHash != tile.NavBuildHash {
		t.Fatalf("expected source-aware build hash on tile entry: entry=%+v tile=%+v", entry, tile)
	}
}

func TestSaveNavBakeForImportedWorldManifestUsesGenericBuildSourceWithoutWorldEntry(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	sourcePath := DefaultNavBuildSourcePath(worldPath)
	navPath := filepath.Join(root, "nav", "demo.gknav")

	world := &ImportedWorldDef{
		WorldID:         "world-source-only",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 1,
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
	source := &NavBuildSourceDef{
		SourceID:      "source-only-no-entry",
		SourceWorldID: world.WorldID,
		Surfaces: []NavBuildSurfaceDef{{
			ID:   "walkable_quad",
			Kind: NavBuildSurfaceWalkable,
			Area: NavTraversalWalk,
			Vertices: []Vec3{
				{9, 1, 1},
				{12, 1, 1},
				{12, 1, 4},
				{9, 1, 4},
			},
		}},
	}
	if err := SaveNavBuildSource(sourcePath, source); err != nil {
		t.Fatalf("SaveNavBuildSource failed: %v", err)
	}

	result, err := SaveNavBakeForImportedWorldManifest(worldPath, navPath, NavBakeOptions{
		NavID:           "nav-source-only",
		BuildSourcePath: AuthorDocumentPath(sourcePath, worldPath),
		AgentProfiles:   []NavAgentProfileDef{navTestAgentProfile(0.2, 1.0, 0.5)},
	})
	if err != nil {
		t.Fatalf("SaveNavBakeForImportedWorldManifest failed: %v", err)
	}
	if result.Manifest == nil || len(result.Manifest.Tiles) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected one source-only tile, got manifest=%+v tiles=%d", result.Manifest, len(result.Tiles))
	}
	entry := result.Manifest.Tiles[0]
	if entry.Coord != (TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("expected source-only tile in coord 1:0:0, got %+v", entry.Coord)
	}
	var tile *NavTileDef
	for _, candidate := range result.Tiles {
		tile = candidate
	}
	if tile == nil || !navTileHasPolygon(tile, "surface:walkable_quad") {
		t.Fatalf("expected generic source polygon in source-only tile, got %+v", tile)
	}
}

func TestSaveNavBakeForImportedWorldManifestUsesSingleSourceTileAtExactYBoundary(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "demo.gkworld")
	sourcePath := DefaultNavBuildSourcePath(worldPath)
	navPath := filepath.Join(root, "nav", "demo.gknav")

	world := &ImportedWorldDef{
		WorldID:         "world-source-boundary",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 1,
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
	source := &NavBuildSourceDef{
		SourceID:      "source-exact-y-boundary",
		SourceWorldID: world.WorldID,
		Surfaces: []NavBuildSurfaceDef{{
			ID:   "boundary_floor",
			Kind: NavBuildSurfaceWalkable,
			Area: NavTraversalWalk,
			Vertices: []Vec3{
				{1, 8, 1},
				{4, 8, 1},
				{4, 8, 4},
				{1, 8, 4},
			},
		}},
	}
	if err := SaveNavBuildSource(sourcePath, source); err != nil {
		t.Fatalf("SaveNavBuildSource failed: %v", err)
	}

	result, err := SaveNavBakeForImportedWorldManifest(worldPath, navPath, NavBakeOptions{
		NavID:           "nav-source-boundary",
		BuildSourcePath: AuthorDocumentPath(sourcePath, worldPath),
		AgentProfiles:   []NavAgentProfileDef{navTestAgentProfile(0.2, 1.0, 0.5)},
	})
	if err != nil {
		t.Fatalf("SaveNavBakeForImportedWorldManifest failed: %v", err)
	}
	if result.Manifest == nil || len(result.Manifest.Tiles) != 1 || len(result.Tiles) != 1 {
		t.Fatalf("expected one source boundary tile, got manifest=%+v tiles=%d", result.Manifest, len(result.Tiles))
	}
	entry := result.Manifest.Tiles[0]
	if entry.Coord != (TerrainChunkCoordDef{X: 0, Y: 1, Z: 0}) {
		t.Fatalf("expected exact-Y-boundary source tile in coord 0:1:0, got %+v", entry.Coord)
	}
	var tile *NavTileDef
	for _, candidate := range result.Tiles {
		tile = candidate
	}
	if tile == nil || !navTileHasPolygon(tile, "surface:boundary_floor") {
		t.Fatalf("expected generic boundary floor polygon in source-only tile, got %+v", tile)
	}
}

func navBakeProgressHasStage(events []NavBakeProgress, stage string) bool {
	_, ok := navBakeProgressEvent(events, stage)
	return ok
}

func navBakeProgressEvent(events []NavBakeProgress, stage string) (NavBakeProgress, bool) {
	for _, event := range events {
		if event.Stage == stage {
			return event, true
		}
	}
	return NavBakeProgress{}, false
}

func TestBakeNavFromImportedWorldConnectsFourChunkFlatField(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "flat_field.gknav")
	world := &ImportedWorldDef{
		WorldID:         "world-flat-field",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 1,
	}
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	for _, coord := range []TerrainChunkCoordDef{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 0, Y: 0, Z: 1},
		{X: 1, Y: 0, Z: 1},
	} {
		chunk := navTestChunk(8, 1.0, navTestFloorVoxels(0, 7, 0, 7, 0)...)
		chunk.WorldID = world.WorldID
		chunk.Coord = coord
		chunks[coord] = chunk
		world.Entries = append(world.Entries, ImportedWorldChunkEntryDef{
			Coord:              coord,
			ChunkPath:          "chunks/" + TerrainChunkKey(coord) + ".gkchunk",
			NonEmptyVoxelCount: len(chunk.Voxels),
		})
	}
	profile := navTestAgentProfileWithID("small", 0.2, 1.0, 0.5)

	result, err := BakeNavFromImportedWorld(world, chunks, navPath, NavBakeOptions{
		NavID:         "nav-flat-field",
		AgentProfiles: []NavAgentProfileDef{profile},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 4 || len(result.Tiles) != 4 {
		t.Fatalf("expected four flat-field tiles, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	if validation := ValidateNavTileTopology(navBakeTileSlice(result.Tiles), navAgentProfilesByID(result.Manifest.AgentProfiles)); validation.HasErrors() {
		t.Fatalf("flat-field nav topology validation failed: %+v", validation.Issues)
	}
	for path, tile := range result.Tiles {
		if len(tile.Portals) == 0 {
			t.Fatalf("expected flat-field tile %s to have seam portals, got %+v", TerrainChunkKey(tile.Coord), tile.Portals)
		}
		if err := SaveNavTile(path, tile); err != nil {
			t.Fatalf("SaveNavTile failed: %v", err)
		}
	}

	path, err := FindEffectiveNavPath(result.Manifest, navPath, nil, "", Vec3{1.5, 1, 1.5}, Vec3{14.5, 1, 14.5}, NavPathOptions{
		AgentProfileID:      profile.ID,
		MaxTileSearchRadius: 2,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) < 3 {
		t.Fatalf("expected path across four-chunk flat field, got %+v", path)
	}
}

func TestBakeNavFromImportedWorldConnectsCrossChunkVoxelRamp(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "voxel_ramp.gknav")
	world := &ImportedWorldDef{
		WorldID:         "world-voxel-ramp",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 0.5,
	}
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	for _, coord := range []TerrainChunkCoordDef{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
	} {
		voxels := make([]ImportedWorldVoxelDef, 0)
		for x := 0; x < world.ChunkSize; x++ {
			for z := 0; z < world.ChunkSize; z++ {
				height := z / 2
				for y := 0; y <= height; y++ {
					voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
				}
			}
		}
		chunk := navTestChunk(world.ChunkSize, world.VoxelResolution, voxels...)
		chunk.WorldID = world.WorldID
		chunk.Coord = coord
		chunks[coord] = chunk
		world.Entries = append(world.Entries, ImportedWorldChunkEntryDef{
			Coord:              coord,
			ChunkPath:          "chunks/" + TerrainChunkKey(coord) + ".gkchunk",
			NonEmptyVoxelCount: len(chunk.Voxels),
		})
	}
	profile := navTestAgentProfileWithID("ramp-walker", 0.2, 1.0, 0.5)

	result, err := BakeNavFromImportedWorld(world, chunks, navPath, NavBakeOptions{
		NavID:         "nav-voxel-ramp",
		AgentProfiles: []NavAgentProfileDef{profile},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 2 || len(result.Tiles) != 2 {
		t.Fatalf("expected two ramp tiles, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	if validation := ValidateNavTileTopology(navBakeTileSlice(result.Tiles), navAgentProfilesByID(result.Manifest.AgentProfiles)); validation.HasErrors() {
		t.Fatalf("ramp nav topology validation failed: %+v", validation.Issues)
	}
	nonFlatPortal := false
	for path, tile := range result.Tiles {
		if len(tile.Portals) == 0 {
			t.Fatalf("expected ramp tile %s to have seam portals, got %+v", TerrainChunkKey(tile.Coord), tile.Portals)
		}
		for _, portal := range tile.Portals {
			if absNavFloat32(portal.Start[1]-portal.End[1]) > 1e-4 {
				nonFlatPortal = true
			}
		}
		if err := SaveNavTile(path, tile); err != nil {
			t.Fatalf("SaveNavTile failed: %v", err)
		}
	}
	if !nonFlatPortal {
		t.Fatalf("expected at least one ramp seam portal to follow changing height, got %+v", result.Tiles)
	}

	path, err := FindEffectiveNavPath(result.Manifest, navPath, nil, "", Vec3{0.75, 0.5, 0.75}, Vec3{7.25, 2.0, 3.25}, NavPathOptions{
		AgentProfileID:      profile.ID,
		MaxTileSearchRadius: 1,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) < 2 {
		t.Fatalf("expected path across cross-chunk voxel ramp, got %+v", path)
	}
}

func TestBakeNavFromImportedWorldConnectsCrossChunkVoxelStairs(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "voxel_stairs.gknav")
	world := &ImportedWorldDef{
		WorldID:         "world-voxel-stairs",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       8,
		VoxelResolution: 0.5,
	}
	chunks := make(map[TerrainChunkCoordDef]*ImportedWorldChunkDef)
	for _, coord := range []TerrainChunkCoordDef{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
	} {
		voxels := make([]ImportedWorldVoxelDef, 0)
		for x := 0; x < world.ChunkSize; x++ {
			globalX := coord.X*world.ChunkSize + x
			height := globalX / 4
			for z := 2; z <= 5; z++ {
				for y := 0; y <= height; y++ {
					voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
				}
			}
		}
		chunk := navTestChunk(world.ChunkSize, world.VoxelResolution, voxels...)
		chunk.WorldID = world.WorldID
		chunk.Coord = coord
		chunks[coord] = chunk
		world.Entries = append(world.Entries, ImportedWorldChunkEntryDef{
			Coord:              coord,
			ChunkPath:          "chunks/" + TerrainChunkKey(coord) + ".gkchunk",
			NonEmptyVoxelCount: len(chunk.Voxels),
		})
	}
	profile := navTestAgentProfileWithID("stair-walker", 0.2, 1.0, 0.5)

	result, err := BakeNavFromImportedWorld(world, chunks, navPath, NavBakeOptions{
		NavID:         "nav-voxel-stairs",
		AgentProfiles: []NavAgentProfileDef{profile},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 2 || len(result.Tiles) != 2 {
		t.Fatalf("expected two stair tiles, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	if validation := ValidateNavTileTopology(navBakeTileSlice(result.Tiles), navAgentProfilesByID(result.Manifest.AgentProfiles)); validation.HasErrors() {
		t.Fatalf("stair nav topology validation failed: %+v", validation.Issues)
	}
	stepPortal := false
	for path, tile := range result.Tiles {
		if len(tile.Portals) == 0 {
			t.Fatalf("expected stair tile %s to have seam portals, got %+v", TerrainChunkKey(tile.Coord), tile.Portals)
		}
		for _, portal := range tile.Portals {
			if portal.ToTileCoord != (TerrainChunkCoordDef{X: 1 - tile.Coord.X, Y: 0, Z: 0}) {
				continue
			}
			if navAlmostEqual(portal.Start[1], 1.25, 1e-4) && navAlmostEqual(portal.End[1], 1.25, 1e-4) {
				stepPortal = true
			}
		}
		if err := SaveNavTile(path, tile); err != nil {
			t.Fatalf("SaveNavTile failed: %v", err)
		}
	}
	if !stepPortal {
		t.Fatalf("expected cross-chunk stair portal halfway between one-voxel step surfaces, got %+v", result.Tiles)
	}

	path, err := FindEffectiveNavPath(result.Manifest, navPath, nil, "", Vec3{1.75, 0.5, 1.25}, Vec3{6.25, 2.0, 1.25}, NavPathOptions{
		AgentProfileID:      profile.ID,
		MaxTileSearchRadius: 1,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) < 2 {
		t.Fatalf("expected path across cross-chunk voxel stairs, got %+v", path)
	}
}

func TestBakeNavFromImportedWorldUsesNeighborChunksForBoundaryPath(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "path.gknav")
	leftCoord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	left := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	left.Coord = leftCoord
	right := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 0, Y: 0, Z: 1, Value: 1})
	right.Coord = rightCoord
	world := &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       4,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: leftCoord, ChunkPath: "chunks/left.gkchunk", NonEmptyVoxelCount: 1},
			{Coord: rightCoord, ChunkPath: "chunks/right.gkchunk", NonEmptyVoxelCount: 1},
		},
	}

	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		leftCoord:  left,
		rightCoord: right,
	}, navPath, NavBakeOptions{
		NavID:         "nav-path",
		AgentProfiles: []NavAgentProfileDef{navTestAgentProfileWithID("wide", 0.6, 1.0, 1.0)},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Manifest.Tiles) != 2 || len(result.Tiles) != 2 {
		t.Fatalf("expected two boundary tiles, got manifest=%+v tiles=%d", result.Manifest.Tiles, len(result.Tiles))
	}
	for _, tile := range result.Tiles {
		if len(tile.Portals) == 0 {
			t.Fatalf("expected baked boundary tile %s to include explicit portals, got %+v", TerrainChunkKey(tile.Coord), tile)
		}
	}
	for path, tile := range result.Tiles {
		if err := SaveNavTile(path, tile); err != nil {
			t.Fatalf("SaveNavTile failed: %v", err)
		}
	}

	path, err := FindEffectiveNavPath(result.Manifest, navPath, nil, "", Vec3{3.5, 1, 1.5}, Vec3{4.5, 1, 1.5}, NavPathOptions{
		AgentProfileID:      "wide",
		MaxTileSearchRadius: 1,
	})
	if err != nil {
		t.Fatalf("FindEffectiveNavPath failed: %v", err)
	}
	if !path.Found || len(path.Steps) != 2 {
		t.Fatalf("expected baked cross-tile boundary path, got %+v", path)
	}
}

func TestBakeNavFromImportedWorldSynthesizesMultiChunkNeighborContext(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "large_context.gknav")
	coord := TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	chunk := navTestChunk(2, 1.0, ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1})
	chunk.Coord = coord
	world := &ImportedWorldDef{
		WorldID:         "world-large-context",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       2,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              coord,
			ChunkPath:          "chunks/center.gkchunk",
			NonEmptyVoxelCount: 1,
		}},
	}
	profile := navTestAgentProfileWithID("large", 3.2, 1.0, 1.0)

	result, err := BakeNavFromImportedWorld(world, map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		coord: chunk,
	}, navPath, NavBakeOptions{
		NavID:         "nav-large-context",
		AgentProfiles: []NavAgentProfileDef{profile},
	})
	if err != nil {
		t.Fatalf("BakeNavFromImportedWorld failed: %v", err)
	}
	if len(result.Tiles) != 1 {
		t.Fatalf("expected one baked tile, got %d", len(result.Tiles))
	}
	for _, tile := range result.Tiles {
		if !navTileHasCell(tile, 1, 1, 1, chunk.VoxelResolution) {
			t.Fatalf("expected baked boundary cell with synthesized multi-chunk context, got %+v", tile.Polygons)
		}
	}
}

func navTestAgentProfileWithID(id string, radius, height, stepHeight float32) NavAgentProfileDef {
	profile := navTestAgentProfile(radius, height, stepHeight)
	profile.ID = id
	return profile
}
