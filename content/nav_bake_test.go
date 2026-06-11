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
	if result.Manifest == nil || len(result.Manifest.Tiles) != 2 || len(result.Tiles) != 2 {
		t.Fatalf("expected two profile tiles, got manifest=%+v tiles=%d", result.Manifest, len(result.Tiles))
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
		NavBakeProgressStageBuildTile,
		NavBakeProgressStageSaveTile,
		NavBakeProgressStageSaveManifest,
	} {
		if !navBakeProgressHasStage(events, stage) {
			t.Fatalf("expected progress stage %q in %+v", stage, events)
		}
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

func navBakeProgressHasStage(events []NavBakeProgress, stage string) bool {
	for _, event := range events {
		if event.Stage == stage {
			return true
		}
	}
	return false
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

func navTestAgentProfileWithID(id string, radius, height, stepHeight float32) NavAgentProfileDef {
	profile := navTestAgentProfile(radius, height, stepHeight)
	profile.ID = id
	return profile
}
