package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestStreamedRuntimeLoadsOnlyDesiredChunk(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "asteroid.gkasset")
	levelPath := filepath.Join(root, "levels", "streamed.gklevel")
	terrainPath := filepath.Join(root, "terrain", "terrain.gkterrain")
	manifestPath := filepath.Join(root, "terrain", "terrain.gkterrainmanifest")
	chunk0Path := filepath.Join(root, "terrain", "terrain_chunks", "0_0_0.gkchunk")
	chunk1Path := filepath.Join(root, "terrain", "terrain_chunks", "1_0_0.gkchunk")

	writeProceduralAssetForLevelTest(t, assetPath, "asteroid-asset")
	writeTerrainSourceForStreamedTest(t, terrainPath)
	writeTerrainChunkForStreamedTest(t, chunk0Path, &content.TerrainChunkDef{
		TerrainID:          "terrain-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		SolidValue:         2,
		Columns:            []content.TerrainChunkColumnDef{{X: 1, Z: 1, FilledVoxels: 2}},
		NonEmptyVoxelCount: 2,
	})
	writeTerrainChunkForStreamedTest(t, chunk1Path, &content.TerrainChunkDef{
		TerrainID:          "terrain-a",
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		SolidValue:         2,
		Columns:            []content.TerrainChunkColumnDef{{X: 3, Z: 4, FilledVoxels: 2}},
		NonEmptyVoxelCount: 2,
	})
	writeTerrainManifestForStreamedTest(t, manifestPath, "terrain-a", []content.TerrainChunkEntryDef{
		terrainEntryForStreamedTest(chunk0Path, manifestPath, content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}),
		terrainEntryForStreamedTest(chunk1Path, manifestPath, content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}),
	})

	level := content.NewLevelDef("streamed")
	level.ChunkSize = 16
	level.StreamingRadius = 0
	level.Placements = []content.LevelPlacementDef{
		{
			ID:        "chunk-0-placement",
			AssetPath: filepath.Join("..", "assets", "asteroid.gkasset"),
			Transform: content.LevelTransformDef{Position: content.Vec3{1, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		},
		{
			ID:        "chunk-1-placement",
			AssetPath: filepath.Join("..", "assets", "asteroid.gkasset"),
			Transform: content.LevelTransformDef{Position: content.Vec3{20, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		},
	}
	level.Terrain = &content.LevelTerrainDef{
		Kind:         content.TerrainKindHeightfield,
		SourcePath:   content.AuthorDocumentPath(terrainPath, levelPath),
		ManifestPath: content.AuthorDocumentPath(manifestPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	observer := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	driveStreamedRuntimeUntil(t, app, func() bool {
		_, loaded := state.LoadedChunks[ChunkCoord{X: 0, Y: 0, Z: 0}]
		return loaded
	})

	if len(state.LoadedChunks) != 1 {
		t.Fatalf("expected one loaded chunk, got %+v", state.LoadedChunks)
	}
	if _, ok := state.LoadedChunks[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatal("expected chunk 0 to be loaded")
	}
	if _, ok := state.LoadedChunks[ChunkCoord{X: 1, Y: 0, Z: 0}]; ok {
		t.Fatal("did not expect chunk 1 to be loaded")
	}
	if placementEntityByIDForStreamedTest(cmd, "chunk-0-placement") == 0 {
		t.Fatal("expected chunk-0 placement to spawn")
	}
	if placementEntityByIDForStreamedTest(cmd, "chunk-1-placement") != 0 {
		t.Fatal("did not expect chunk-1 placement to spawn")
	}
	if terrainChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0}) == 0 {
		t.Fatal("expected terrain chunk 0 to spawn")
	}
	if terrainChunkEntityByCoordForStreamedTest(cmd, [3]int{1, 0, 0}) != 0 {
		t.Fatal("did not expect terrain chunk 1 to spawn")
	}

	cmd.AddComponents(observer, &TransformComponent{Position: mgl32.Vec3{20, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	app.FlushCommands()
	driveStreamedRuntimeUntil(t, app, func() bool {
		_, loaded := state.LoadedChunks[ChunkCoord{X: 1, Y: 0, Z: 0}]
		return loaded && placementEntityByIDForStreamedTest(cmd, "chunk-1-placement") != 0
	})

	if _, ok := state.LoadedChunks[ChunkCoord{X: 0, Y: 0, Z: 0}]; ok {
		t.Fatal("expected chunk 0 to unload after observer moved")
	}
	if placementEntityByIDForStreamedTest(cmd, "chunk-0-placement") != 0 {
		t.Fatal("expected chunk-0 placement to unload")
	}
	if terrainChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0}) != 0 {
		t.Fatal("expected terrain chunk 0 to unload")
	}
}

func TestStreamedRuntimePlacementVolumesUseStableSyntheticIDs(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "asteroid.gkasset")
	levelPath := filepath.Join(root, "levels", "volumes.gklevel")
	writeProceduralAssetForLevelTest(t, assetPath, "asteroid-asset")

	level := content.NewLevelDef("volumes")
	level.ChunkSize = 16
	level.PlacementVolumes = []content.PlacementVolumeDef{{
		ID:         "volume-a",
		Kind:       content.PlacementVolumeKindSphere,
		AssetPath:  filepath.Join("..", "assets", "asteroid.gkasset"),
		Transform:  content.LevelTransformDef{Position: content.Vec3{24, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		Radius:     4,
		RandomSeed: 7,
		Rule: content.PlacementVolumeRuleDef{
			Mode:  content.PlacementVolumeRuleModeCount,
			Count: 3,
		},
	}}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	leftApp, leftCmd, leftState := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(leftCmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	rightApp, rightCmd, rightState := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(rightCmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	_ = leftApp
	_ = rightApp

	for _, placementID := range []string{"volume-a:0", "volume-a:1", "volume-a:2"} {
		leftCoord, leftOK := leftState.PlacementChunk[placementID]
		rightCoord, rightOK := rightState.PlacementChunk[placementID]
		if !leftOK || !rightOK {
			t.Fatalf("expected synthetic placement %s in both runtimes", placementID)
		}
		if leftCoord != rightCoord {
			t.Fatalf("expected stable chunk assignment for %s, left=%v right=%v", placementID, leftCoord, rightCoord)
		}
	}
}

func TestStartStreamedRuntimeRejectsTerrainChunkSizeMismatch(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "mismatch.gklevel")
	terrainPath := filepath.Join(root, "terrain", "terrain.gkterrain")
	manifestPath := filepath.Join(root, "terrain", "terrain.gkterrainmanifest")

	writeTerrainSourceForStreamedTest(t, terrainPath)
	writeTerrainManifestForStreamedTest(t, manifestPath, "terrain-a", nil)

	level := content.NewLevelDef("mismatch")
	level.ChunkSize = 32
	level.Terrain = &content.LevelTerrainDef{
		Kind:         content.TerrainKindHeightfield,
		SourcePath:   content.AuthorDocumentPath(terrainPath, levelPath),
		ManifestPath: content.AuthorDocumentPath(manifestPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, _ := newStreamedRuntimeHarness(t)
	_ = app
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err == nil {
		t.Fatal("expected terrain chunk size mismatch error")
	}
}

func TestStartStreamedRuntimeCanAutoSpawnGroundedPlayerFromMarker(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "player_spawn.gklevel")
	worldPath := filepath.Join(root, "worlds", "station.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "0_0_0.gkchunk")

	if err := content.SaveImportedWorldChunk(chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "station",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 0,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}

	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "station",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       160,
		VoxelResolution: 1,
		Entries: []content.ImportedWorldChunkEntryDef{{
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 0,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("player_spawn")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	level.Markers = []content.LevelMarkerDef{{
		ID:   "player-1",
		Name: "player-1",
		Kind: content.LevelMarkerKindPlayerSpawn,
		Transform: content.LevelTransformDef{
			Position: content.Vec3{4, 2, 3},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath:       levelPath,
		AutoSpawnPlayer: true,
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()

	if len(state.MarkerEntities) != 1 {
		t.Fatalf("expected one runtime marker entity, got %+v", state.MarkerEntities)
	}
	playerCount := 0
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		playerCount++
		if cam.Position != (mgl32.Vec3{4, 3.7, 3}) {
			t.Fatalf("unexpected player camera position %v", cam.Position)
		}
		return true
	})
	if playerCount != 1 {
		t.Fatalf("expected one grounded player camera, got %d", playerCount)
	}
}

func TestStreamedRuntimeAppliesWorldDeltaReconciliationAndHooks(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "ship.gkasset")
	levelPath := filepath.Join(root, "levels", "delta.gklevel")
	deltaPath := content.DefaultWorldDeltaPath(levelPath)
	terrainPath := filepath.Join(root, "terrain", "terrain.gkterrain")
	manifestPath := filepath.Join(root, "terrain", "terrain.gkterrainmanifest")
	chunk0Path := filepath.Join(root, "terrain", "terrain_chunks", "0_0_0.gkchunk")
	overrideChunkPath := filepath.Join(root, "levels", "delta.gkworlddelta_data", "terrain_override.gkchunk")
	overrideVoxelPath := filepath.Join(root, "levels", "delta.gkworlddelta_data", "ship_hull.gkvoxobj")

	writeProceduralAssetForLevelTest(t, assetPath, "ship-asset")
	writeTerrainSourceForStreamedTest(t, terrainPath)
	writeTerrainChunkForStreamedTest(t, chunk0Path, &content.TerrainChunkDef{
		TerrainID:          "terrain-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		SolidValue:         2,
		Columns:            []content.TerrainChunkColumnDef{{X: 1, Z: 1, FilledVoxels: 4}},
		NonEmptyVoxelCount: 4,
	})
	writeTerrainManifestForStreamedTest(t, manifestPath, "terrain-a", []content.TerrainChunkEntryDef{
		terrainEntryForStreamedTest(chunk0Path, manifestPath, content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}),
	})
	writeTerrainChunkForStreamedTest(t, overrideChunkPath, &content.TerrainChunkDef{
		TerrainID:          "terrain-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		SolidValue:         5,
		Columns:            []content.TerrainChunkColumnDef{{X: 7, Z: 7, FilledVoxels: 1}},
		NonEmptyVoxelCount: 1,
	})
	if err := content.SaveVoxelObjectSnapshot(overrideVoxelPath, &content.VoxelObjectSnapshotDef{
		Voxels: []content.VoxelObjectVoxelDef{{X: 0, Y: 0, Z: 0, Value: 9}},
	}); err != nil {
		t.Fatalf("SaveVoxelObjectSnapshot failed: %v", err)
	}

	level := content.NewLevelDef("delta")
	level.ChunkSize = 16
	level.StreamingRadius = 0
	level.Placements = []content.LevelPlacementDef{
		{
			ID:        "kept-but-moved",
			AssetPath: filepath.Join("..", "assets", "ship.gkasset"),
			Transform: content.LevelTransformDef{Position: content.Vec3{1, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		},
		{
			ID:        "deleted-placement",
			AssetPath: filepath.Join("..", "assets", "ship.gkasset"),
			Transform: content.LevelTransformDef{Position: content.Vec3{1, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		},
	}
	level.Terrain = &content.LevelTerrainDef{
		Kind:         content.TerrainKindHeightfield,
		SourcePath:   content.AuthorDocumentPath(terrainPath, levelPath),
		ManifestPath: content.AuthorDocumentPath(manifestPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	worldDelta := &content.WorldDeltaDef{
		LevelID: level.ID,
		PlacementTransformOverrides: []content.PlacementTransformOverrideDef{{
			PlacementID: "kept-but-moved",
			Transform:   content.LevelTransformDef{Position: content.Vec3{20, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		}},
		PlacementDeletions: []content.PlacementDeletionDef{{PlacementID: "deleted-placement"}},
		TerrainChunkOverrides: []content.TerrainChunkOverrideDef{{
			TerrainID:    "terrain-a",
			ChunkCoord:   content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			SnapshotPath: content.AuthorDocumentPath(overrideChunkPath, deltaPath),
		}},
		VoxelObjectOverrides: []content.VoxelObjectOverrideDef{{
			PlacementID:  "kept-but-moved",
			ItemID:       "ship-asset-part",
			SnapshotPath: content.AuthorDocumentPath(overrideVoxelPath, deltaPath),
		}},
	}
	if err := content.SaveWorldDelta(deltaPath, worldDelta); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}

	var placementHookCalls int
	var terrainHookCalls int
	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath: levelPath,
		PlacementHooks: []PostSpawnPlacementHook{
			func(cmd *Commands, ctx PostSpawnPlacementContext) {
				placementHookCalls++
				entity := ctx.SpawnResult.EntitiesByAssetID["ship-asset-part"]
				vmc, ok := voxelModelComponentForEntity(cmd, entity)
				if !ok || vmc.CustomMap == nil || vmc.CustomMap.GetVoxelCount() != 1 {
					t.Fatalf("expected placement hook to observe reconciled voxel override, got %+v", vmc)
				}
			},
		},
		TerrainHooks: []PostSpawnTerrainHook{
			func(cmd *Commands, ctx PostSpawnTerrainContext) {
				terrainHookCalls++
			},
		},
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	observer := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	driveStreamedRuntimeUntil(t, app, func() bool {
		return terrainChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0}) != 0
	})

	if placementEntityByIDForStreamedTest(cmd, "deleted-placement") != 0 {
		t.Fatal("expected deleted placement to stay deleted")
	}
	if placementEntityByIDForStreamedTest(cmd, "kept-but-moved") != 0 {
		t.Fatal("did not expect moved placement to spawn in authored chunk")
	}
	terrainEntity := terrainChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0})
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, terrainEntity)
	if vmc.CustomMap == nil || vmc.CustomMap.GetVoxelCount() != 1 {
		t.Fatalf("expected terrain override to replace authored terrain, got %+v", vmc.CustomMap)
	}
	if terrainHookCalls != 1 {
		t.Fatalf("expected one terrain hook call, got %d", terrainHookCalls)
	}

	cmd.AddComponents(observer, &TransformComponent{Position: mgl32.Vec3{20, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	app.FlushCommands()
	driveStreamedRuntimeUntil(t, app, func() bool {
		return placementEntityByIDForStreamedTest(cmd, "kept-but-moved") != 0
	})

	placementRoot := placementEntityByIDForStreamedTest(cmd, "kept-but-moved")
	if placementRoot == 0 {
		t.Fatal("expected moved placement to stream in at overridden chunk")
	}
	itemEntity := placementItemEntityByIDForStreamedTest(cmd, "kept-but-moved", "ship-asset-part")
	itemVMC := mustVoxelModelComponentForLevelTest(t, cmd, itemEntity)
	if itemVMC.CustomMap == nil || itemVMC.CustomMap.GetVoxelCount() != 1 {
		t.Fatalf("expected voxel override to apply on initial load, got %+v", itemVMC.CustomMap)
	}
	if placementHookCalls != 1 {
		t.Fatalf("expected one placement hook call, got %d", placementHookCalls)
	}

	cmd.AddComponents(observer, &TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	app.FlushCommands()
	driveStreamedRuntimeUntil(t, app, func() bool {
		return placementEntityByIDForStreamedTest(cmd, "kept-but-moved") == 0
	})
	cmd.AddComponents(observer, &TransformComponent{Position: mgl32.Vec3{20, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	app.FlushCommands()
	driveStreamedRuntimeUntil(t, app, func() bool {
		return placementItemEntityByIDForStreamedTest(cmd, "kept-but-moved", "ship-asset-part") != 0
	})

	reloadedItemEntity := placementItemEntityByIDForStreamedTest(cmd, "kept-but-moved", "ship-asset-part")
	reloadedVMC := mustVoxelModelComponentForLevelTest(t, cmd, reloadedItemEntity)
	if reloadedVMC.CustomMap == nil || reloadedVMC.CustomMap.GetVoxelCount() != 1 {
		t.Fatalf("expected voxel override to reapply after reload, got %+v", reloadedVMC.CustomMap)
	}
	if state.ObjectChunk[voxelObjectRuntimeKey("kept-but-moved", "ship-asset-part")] != (ChunkCoord{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("expected object chunk ownership to track overridden chunk, got %+v", state.ObjectChunk)
	}
}

func TestStreamedRuntimeLoadsImportedBaseWorldChunkWithCollision(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "baseworld.gklevel")
	worldPath := filepath.Join(root, "worlds", "baseworld.gkworld")
	chunkPath := filepath.Join(root, "worlds", "baseworld_chunks", "0_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 1, Y: 2, Z: 3, Value: 4}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-a", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})

	level := content.NewLevelDef("baseworld")
	level.ChunkSize = 16
	level.StreamingRadius = 0
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:             content.ImportedWorldKindVoxelWorld,
		ManifestPath:     content.AuthorDocumentPath(worldPath, levelPath),
		CollisionEnabled: true,
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	driveStreamedRuntimeUntil(t, app, func() bool {
		return importedWorldChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0}) != 0
	})

	entity := importedWorldChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0})
	if entity == 0 {
		t.Fatal("expected imported world chunk entity")
	}
	ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, entity)
	if !ok || ref.WorldID != "world-a" {
		t.Fatalf("expected imported world metadata, got %+v ok=%v", ref, ok)
	}
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	if vmc.CustomMap == nil || vmc.CustomMap.GetVoxelCount() != 1 {
		t.Fatalf("expected imported world custom map with one voxel, got %+v", vmc.CustomMap)
	}
	if !hasComponentOfType[RigidBodyComponent](cmd, entity) || !hasComponentOfType[ColliderComponent](cmd, entity) || !hasComponentOfType[AABBComponent](cmd, entity) {
		t.Fatal("expected static collision components on imported world chunk")
	}
}

func TestStreamedRuntimeSkipsImportedBaseWorldCollisionWhenDisabled(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "baseworld_nocollision.gklevel")
	worldPath := filepath.Join(root, "worlds", "baseworld_nocollision.gkworld")
	chunkPath := filepath.Join(root, "worlds", "baseworld_nocollision_chunks", "0_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          160,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-a", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})

	level := content.NewLevelDef("baseworld_nocollision")
	level.ChunkSize = 16
	level.StreamingRadius = 0
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:             content.ImportedWorldKindVoxelWorld,
		ManifestPath:     content.AuthorDocumentPath(worldPath, levelPath),
		CollisionEnabled: false,
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	driveStreamedRuntimeUntil(t, app, func() bool {
		return importedWorldChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0}) != 0
	})

	entity := importedWorldChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0})
	if entity == 0 {
		t.Fatal("expected imported world chunk entity")
	}
	if hasComponentOfType[RigidBodyComponent](cmd, entity) || hasComponentOfType[ColliderComponent](cmd, entity) || hasComponentOfType[AABBComponent](cmd, entity) {
		t.Fatal("did not expect static collision components when collision is disabled")
	}
}

func newStreamedRuntimeHarness(t *testing.T) (*App, *Commands, *StreamedLevelRuntimeState) {
	t.Helper()
	app := NewApp()
	app.UseModules(StreamedLevelRuntimeModule{})
	app.build()
	cmd := app.Commands()
	cmd.AddResources(newSpawnTestAssetServer())
	return app, cmd, streamedLevelRuntimeStateFromApp(app)
}

func driveStreamedRuntimeUntil(t *testing.T, app *App, done func() bool) {
	t.Helper()
	for i := 0; i < 80; i++ {
		app.callSystems(0, execute)
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for streamed runtime")
}

func placementEntityByIDForStreamedTest(cmd *Commands, placementID string) EntityId {
	var found EntityId
	MakeQuery1[AuthoredLevelPlacementRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredLevelPlacementRefComponent) bool {
		if ref.PlacementID == placementID {
			found = eid
			return false
		}
		return true
	})
	return found
}

func placementItemEntityByIDForStreamedTest(cmd *Commands, placementID string, itemID string) EntityId {
	var found EntityId
	MakeQuery1[AuthoredLevelItemRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredLevelItemRefComponent) bool {
		if ref.PlacementID == placementID && ref.ItemID == itemID {
			found = eid
			return false
		}
		return true
	})
	return found
}

func terrainChunkEntityByCoordForStreamedTest(cmd *Commands, coord [3]int) EntityId {
	var found EntityId
	MakeQuery1[AuthoredTerrainChunkRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredTerrainChunkRefComponent) bool {
		if ref.ChunkCoord == coord {
			found = eid
			return false
		}
		return true
	})
	return found
}

func importedWorldChunkEntityByCoordForStreamedTest(cmd *Commands, coord [3]int) EntityId {
	var found EntityId
	MakeQuery1[AuthoredImportedWorldChunkRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredImportedWorldChunkRefComponent) bool {
		if ref.ChunkCoord == coord {
			found = eid
			return false
		}
		return true
	})
	return found
}

func writeTerrainChunkForStreamedTest(t *testing.T, path string, chunk *content.TerrainChunkDef) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveTerrainChunk(path, chunk); err != nil {
		t.Fatalf("SaveTerrainChunk failed: %v", err)
	}
}

func writeTerrainManifestForStreamedTest(t *testing.T, path string, terrainID string, entries []content.TerrainChunkEntryDef) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveTerrainChunkManifest(path, &content.TerrainChunkManifestDef{
		TerrainID:       terrainID,
		ChunkSize:       160,
		VoxelResolution: 1,
		Entries:         entries,
	}); err != nil {
		t.Fatalf("SaveTerrainChunkManifest failed: %v", err)
	}
}

func writeImportedWorldChunkForStreamedTest(t *testing.T, path string, chunk *content.ImportedWorldChunkDef) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveImportedWorldChunk(path, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
}

func writeImportedWorldManifestForStreamedTest(t *testing.T, path string, worldID string, entries []content.ImportedWorldChunkEntryDef) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveImportedWorld(path, &content.ImportedWorldDef{
		WorldID:         worldID,
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       160,
		VoxelResolution: 1,
		Entries:         entries,
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
}

func hasComponentOfType[T any](cmd *Commands, eid EntityId) bool {
	wantValue := reflect.TypeOf((*T)(nil)).Elem()
	wantPtr := reflect.PointerTo(wantValue)
	for _, comp := range cmd.GetAllComponents(eid) {
		if comp == nil {
			continue
		}
		got := reflect.TypeOf(comp)
		if got == wantValue || got == wantPtr {
			return true
		}
	}
	return false
}

func writeTerrainSourceForStreamedTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	terrain := content.NewTerrainSourceDef("terrain-a")
	terrain.SampleWidth = 1
	terrain.SampleHeight = 1
	terrain.HeightSamples = []uint16{65535}
	terrain.WorldSize = content.Vec2{16, 16}
	terrain.HeightScale = 4
	terrain.VoxelResolution = 1
	terrain.ChunkSize = 160
	if err := content.SaveTerrainSource(path, terrain); err != nil {
		t.Fatalf("SaveTerrainSource failed: %v", err)
	}
}

func terrainEntryForStreamedTest(chunkPath string, manifestPath string, coord content.TerrainChunkCoordDef) content.TerrainChunkEntryDef {
	return content.TerrainChunkEntryDef{
		Coord:              coord,
		ChunkSize:          160,
		VoxelResolution:    1,
		TerrainID:          "terrain-a",
		ChunkPath:          content.AuthorDocumentPath(chunkPath, manifestPath),
		NonEmptyVoxelCount: 1,
	}
}
