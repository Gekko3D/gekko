package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildEffectiveStreamedPlacementIndexPreservesTags(t *testing.T) {
	level := content.NewLevelDef("tagged-stream")
	level.Placements = []content.LevelPlacementDef{
		{
			ID:        "direct-pool",
			AssetPath: "direct.gkasset",
			Transform: content.LevelTransformDef{
				Position: content.Vec3{1, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"pool-a"},
		},
	}
	level.PlacementVolumes = []content.PlacementVolumeDef{
		{
			ID:        "volume-pool",
			Kind:      content.PlacementVolumeKindBox,
			AssetPath: "volume.gkasset",
			Transform: content.LevelTransformDef{
				Position: content.Vec3{0, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Extents: content.Vec3{4, 1, 4},
			Rule:    content.PlacementVolumeRuleDef{Mode: content.PlacementVolumeRuleModeCount, Count: 1},
			Tags:    []string{"pool-b"},
		},
	}

	placements, err := buildEffectiveStreamedPlacementIndex(level, "", nil, nil, 4)
	if err != nil {
		t.Fatalf("buildEffectiveStreamedPlacementIndex failed: %v", err)
	}

	foundDirect := false
	foundVolume := false
	for _, placement := range placements {
		switch placement.PlacementID {
		case "direct-pool":
			foundDirect = true
			if !slices.Equal(placement.Tags, []string{"pool-a"}) {
				t.Fatalf("expected direct placement tags to survive, got %v", placement.Tags)
			}
		case "volume-pool:0":
			foundVolume = true
			if !slices.Equal(placement.Tags, []string{"pool-b"}) {
				t.Fatalf("expected volume placement tags to survive, got %v", placement.Tags)
			}
		}
	}
	if !foundDirect || !foundVolume {
		t.Fatalf("expected direct and volume placements, got %+v", placements)
	}
}

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
		ChunkSize:          16,
		VoxelResolution:    1,
		SolidValue:         2,
		Columns:            []content.TerrainChunkColumnDef{{X: 1, Z: 1, FilledVoxels: 2}},
		NonEmptyVoxelCount: 2,
	})
	writeTerrainChunkForStreamedTest(t, chunk1Path, &content.TerrainChunkDef{
		TerrainID:          "terrain-a",
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		ChunkSize:          16,
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
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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

func TestStreamedRuntimeSpawnsLevelWaterBodies(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "water.gklevel")
	level := content.NewLevelDef("water")
	directLightOcclusion := float32(0.8)
	level.WaterBodies = []content.LevelWaterBodyDef{{
		ID:                   "water-1",
		Mode:                 content.LevelWaterBodyModeExplicitRect,
		SurfaceY:             2,
		Depth:                1,
		RectHalfExtents:      content.Vec2{3, 4},
		DirectLightOcclusion: &directLightOcclusion,
		Transform: content.LevelTransformDef{
			Position: content.Vec3{5, 2, 6},
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
	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()
	var found bool
	MakeQuery1[WaterBodyComponent](cmd).Map(func(_ EntityId, water *WaterBodyComponent) bool {
		found = true
		if water.SurfaceY != 2 || water.Depth != 1 || water.RectHalfExtents != ([2]float32{3, 4}) {
			t.Fatalf("water component = %+v", water)
		}
		if water.DirectLightOcclusion != 0.8 {
			t.Fatalf("water direct light occlusion = %v", water.DirectLightOcclusion)
		}
		return true
	})
	if !found {
		t.Fatal("expected streamed runtime water body")
	}
}

func TestStreamedRuntimeSpawnsLevelLadderVolumes(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "ladder.gklevel")
	level := content.NewLevelDef("ladder")
	level.LadderVolumes = []content.LevelLadderVolumeDef{{
		ID:                "ladder-1",
		BoundsCenter:      content.Vec3{5, 2, 6},
		BoundsHalfExtents: content.Vec3{0.25, 2, 0.4},
		ClimbSpeed:        3.25,
		SourceTag:         "hl1:func_ladder",
	}}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}
	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()
	var found bool
	MakeQuery1[LadderVolumeComponent](cmd).Map(func(_ EntityId, ladder *LadderVolumeComponent) bool {
		found = true
		if ladder.BoundsCenter != (mgl32.Vec3{5, 2, 6}) || ladder.BoundsHalfExtents != (mgl32.Vec3{0.25, 2, 0.4}) || ladder.ClimbSpeed != 3.25 {
			t.Fatalf("ladder component = %+v", ladder)
		}
		return true
	})
	if !found {
		t.Fatal("expected streamed runtime ladder volume")
	}
}

func TestStreamedRuntimeSpawnsMovingBrushesAndUseTriggers(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "moving.gklevel")
	level := content.NewLevelDef("moving")
	level.MovingBrushes = []content.LevelMovingBrushDef{{
		ID:                "door-1",
		MotionKind:        "rotate",
		BoundsCenter:      content.Vec3{5, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 1, 0.25},
		RotationAxis:      content.Vec3{0, 1, 0},
		OpenAngle:         90,
		PathTarget:        "corner_a",
		TargetName:        "door_a",
	}}
	level.PathNodes = []content.LevelPathNodeDef{{
		ID:         "corner-1",
		TargetName: "corner_a",
		Target:     "corner_b",
		Position:   content.Vec3{8, 2, 6},
	}}
	level.UseTriggers = []content.LevelUseTriggerDef{{
		ID:                "button-1",
		BoundsCenter:      content.Vec3{4, 2, 6},
		BoundsHalfExtents: content.Vec3{0.25, 0.5, 0.25},
		Target:            "door_a",
	}}
	level.TriggerVolumes = []content.LevelTriggerVolumeDef{{
		ID:                "trigger-1",
		BoundsCenter:      content.Vec3{3, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 0.5, 0.5},
		Target:            "manager_a",
		Once:              true,
	}}
	level.DamageVolumes = []content.LevelDamageVolumeDef{{
		ID:                "damage-1",
		BoundsCenter:      content.Vec3{8, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 0.5, 0.5},
		Damage:            12,
		DamageInterval:    0.4,
		TargetName:        "acid_a",
	}}
	level.ChangeLevels = []content.LevelChangeLevelDef{{
		ID:                "change-1",
		BoundsCenter:      content.Vec3{9, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 0.5, 0.5},
		TargetMap:         "c1a1",
		Landmark:          "lm_a",
	}}
	level.Chargers = []content.LevelChargerDef{{
		ID:                "charger-1",
		BoundsCenter:      content.Vec3{10, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 0.5, 0.5},
		ChargeKind:        "health",
		Capacity:          50,
		Rate:              15,
		TargetName:        "charger_a",
	}}
	level.MultiTargets = []content.LevelMultiTargetDef{{
		ID:         "manager-1",
		TargetName: "manager_a",
		Events: []content.LevelTargetEventDef{{
			Target: "door_a",
		}},
	}}
	level.TargetRelays = []content.LevelTargetRelayDef{{
		ID:           "relay-1",
		Kind:         "hl1_trigger_relay",
		TargetName:   "relay_a",
		Target:       "door_a",
		TriggerState: 1,
	}}
	level.Breakables = []content.LevelBreakableDef{{
		ID:                "breakable-1",
		BoundsCenter:      content.Vec3{6, 2, 6},
		BoundsHalfExtents: content.Vec3{0.5, 0.5, 0.5},
		Health:            25,
		TargetName:        "crate_a",
		Target:            "door_a",
	}}
	level.Pickups = []content.LevelPickupDef{{
		ID:        "pickup-1",
		Kind:      "hl1_pickup",
		Category:  "ammo",
		Item:      "9mmclip",
		Amount:    17,
		ClassName: "ammo_9mmclip",
		Transform: content.LevelTransformDef{
			Position: content.Vec3{7, 2, 6},
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
	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()
	var brushCount, pathNodeCount, triggerCount, touchTriggerCount, damageCount, changeCount, chargerCount, multiTargetCount, relayCount, breakableCount, pickupCount int
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, _ *MovingBrushComponent) bool {
		brushCount++
		return true
	})
	MakeQuery1[PathNodeComponent](cmd).Map(func(_ EntityId, _ *PathNodeComponent) bool {
		pathNodeCount++
		return true
	})
	MakeQuery1[UseTriggerComponent](cmd).Map(func(_ EntityId, _ *UseTriggerComponent) bool {
		triggerCount++
		return true
	})
	MakeQuery1[TriggerVolumeComponent](cmd).Map(func(_ EntityId, _ *TriggerVolumeComponent) bool {
		touchTriggerCount++
		return true
	})
	MakeQuery1[DamageVolumeComponent](cmd).Map(func(_ EntityId, _ *DamageVolumeComponent) bool {
		damageCount++
		return true
	})
	MakeQuery1[ChangeLevelVolumeComponent](cmd).Map(func(_ EntityId, _ *ChangeLevelVolumeComponent) bool {
		changeCount++
		return true
	})
	MakeQuery1[ChargerComponent](cmd).Map(func(_ EntityId, _ *ChargerComponent) bool {
		chargerCount++
		return true
	})
	MakeQuery1[MultiTargetComponent](cmd).Map(func(_ EntityId, _ *MultiTargetComponent) bool {
		multiTargetCount++
		return true
	})
	MakeQuery1[TargetRelayComponent](cmd).Map(func(_ EntityId, _ *TargetRelayComponent) bool {
		relayCount++
		return true
	})
	MakeQuery1[BreakableComponent](cmd).Map(func(_ EntityId, _ *BreakableComponent) bool {
		breakableCount++
		return true
	})
	MakeQuery1[PickupComponent](cmd).Map(func(_ EntityId, _ *PickupComponent) bool {
		pickupCount++
		return true
	})
	if brushCount != 1 || pathNodeCount != 1 || triggerCount != 1 || touchTriggerCount != 1 || damageCount != 1 || changeCount != 1 || chargerCount != 1 || multiTargetCount != 1 || relayCount != 1 || breakableCount != 1 || pickupCount != 1 {
		t.Fatalf("expected 1 brush/path/use/touch/damage/change/charger/multi/relay/breakable/pickup, got %d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d", brushCount, pathNodeCount, triggerCount, touchTriggerCount, damageCount, changeCount, chargerCount, multiTargetCount, relayCount, breakableCount, pickupCount)
	}
}

func TestStreamedRuntimeSpawnsLevelLights(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "lights.gklevel")
	level := content.NewLevelDef("lights")
	level.Lights = []content.LevelLightDef{{
		ID:            "light-1",
		Type:          content.LevelLightTypePoint,
		Color:         [3]float32{1, 0.8, 0.4},
		Intensity:     3,
		Range:         12,
		SourceRadius:  0.25,
		EmitterLinkID: 44,
		Transform: content.LevelTransformDef{
			Position: content.Vec3{4, 5, 6},
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
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()
	if state.LightEntities["light-1"] == 0 {
		t.Fatal("expected streamed runtime to track level light entity")
	}
	var found bool
	MakeQuery2[LightComponent, TransformComponent](cmd).Map(func(_ EntityId, light *LightComponent, tr *TransformComponent) bool {
		found = true
		if light.Type != LightTypePoint || light.Intensity != 3 || light.Range != 12 || light.SourceRadius != 0.25 || light.EmitterLinkID != 44 {
			t.Fatalf("light component = %+v", light)
		}
		if tr.Position != (mgl32.Vec3{4, 5, 6}) {
			t.Fatalf("light position = %v", tr.Position)
		}
		return true
	})
	if !found {
		t.Fatal("expected streamed runtime light")
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
	if err := StartStreamedLevelRuntime(leftCmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	rightApp, rightCmd, rightState := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(rightCmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err == nil {
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
		ChunkSize:          16,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 0,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}

	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "station",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
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
			Position: content.Vec3{0, 0, 0},
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
		StreamingRadius: 0,
		AutoSpawnPlayer: true,
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()

	if len(state.MarkerEntities) != 1 {
		t.Fatalf("expected one runtime marker entity, got %+v", state.MarkerEntities)
	}
	if len(state.LoadedChunks) != 1 {
		t.Fatalf("expected spawn chunk to be loaded before player spawn, got %+v", state.LoadedChunks)
	}
	if _, ok := state.LoadedChunks[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected loaded chunk at player spawn coord, got %+v", state.LoadedChunks)
	}
	playerCount := 0
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		playerCount++
		if cam.Position != (mgl32.Vec3{0, 1.7, 0}) {
			t.Fatalf("unexpected player camera position %v", cam.Position)
		}
		return true
	})
	if playerCount != 1 {
		t.Fatalf("expected one grounded player camera, got %d", playerCount)
	}
}

func TestStartStreamedRuntimeAutoSpawnUsesConfiguredPlayerDimensions(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "player_spawn.gklevel")
	worldPath := filepath.Join(root, "worlds", "station.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "0_0_0.gkchunk")

	if err := content.SaveImportedWorldChunk(chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "station",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 0,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "station",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
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
			Position: content.Vec3{0, 0, 0},
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

	app, cmd, _ := newStreamedRuntimeHarness(t)
	cfg := GroundedPlayerControllerConfig{
		Height:    1.65,
		EyeHeight: 1.5,
		Radius:    0.22,
	}
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath:       levelPath,
		StreamingRadius: 0,
		AutoSpawnPlayer: true,
		PlayerConfig:    &cfg,
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()

	playerCount := 0
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		playerCount++
		if ctrl.Height != 1.65 || ctrl.EyeHeight != 1.5 || ctrl.Radius != 0.22 {
			t.Fatalf("expected configured player controller, got %+v", *ctrl)
		}
		if cam.Position != (mgl32.Vec3{0, 1.5, 0}) {
			t.Fatalf("unexpected player camera position %v", cam.Position)
		}
		return true
	})
	if playerCount != 1 {
		t.Fatalf("expected one grounded player camera, got %d", playerCount)
	}
}

func TestStartStreamedRuntimeAutoSpawnUsesLevelPlayerDimensions(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "player_spawn.gklevel")
	worldPath := filepath.Join(root, "worlds", "station.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "0_0_0.gkchunk")

	if err := content.SaveImportedWorldChunk(chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "station",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		NonEmptyVoxelCount: 0,
	}); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "station",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
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
	level.Player = &content.LevelPlayerDef{
		SpawnKind:  "hl1_player_spawn",
		Height:     1.8288,
		EyeHeight:  1.6256,
		Radius:     0.4064,
		StepHeight: 0.4572,
	}
	level.Markers = []content.LevelMarkerDef{{
		ID:   "player-1",
		Name: "player-1",
		Kind: "hl1_player_spawn",
		Transform: content.LevelTransformDef{
			Position: content.Vec3{0, 0, 0},
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

	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath:       levelPath,
		StreamingRadius: 0,
		AutoSpawnPlayer: true,
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	app.FlushCommands()

	playerCount := 0
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		playerCount++
		if ctrl.Height != 1.8288 || ctrl.EyeHeight != 1.6256 || ctrl.Radius != 0.4064 || ctrl.StepHeight != 0.4572 {
			t.Fatalf("expected level player controller, got %+v", *ctrl)
		}
		if cam.Position != (mgl32.Vec3{0, 1.6256, 0}) {
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
		ChunkSize:          16,
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
		ChunkSize:          16,
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
		LevelPath:       levelPath,
		StreamingRadius: 0,
		PlacementHooks: []PostSpawnPlacementHook{
			func(cmd *Commands, ctx PostSpawnPlacementContext) {
				placementHookCalls++
				entity := ctx.SpawnResult.EntitiesByAssetID["ship-asset-part"]
				vmc, ok := voxelModelComponentForEntity(cmd, entity)
				geometryMap, mapOK := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
				if !ok || !mapOK || geometryMap.GetVoxelCount() != 1 {
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
	terrainMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
	if !ok || terrainMap.GetVoxelCount() != 1 {
		t.Fatalf("expected terrain override to replace authored terrain, got %+v", terrainMap)
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
	itemMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &itemVMC)
	if !ok || itemMap.GetVoxelCount() != 1 {
		t.Fatalf("expected voxel override to apply on initial load, got %+v", itemMap)
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
	reloadedMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &reloadedVMC)
	if !ok || reloadedMap.GetVoxelCount() != 1 {
		t.Fatalf("expected voxel override to reapply after reload, got %+v", reloadedMap)
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
		ChunkSize:          16,
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
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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
	importedMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
	if !ok || importedMap.GetVoxelCount() != 1 {
		t.Fatalf("expected imported world override geometry with one voxel, got %+v", importedMap)
	}
	if vmc.ShadowGroupID == 0 {
		t.Fatal("expected imported world chunk to have a non-zero shadow group")
	}
	if vmc.ShadowSeamWorldEpsilon != 1 {
		t.Fatalf("expected imported world seam epsilon 1, got %v", vmc.ShadowSeamWorldEpsilon)
	}
	if !hasComponentOfType[RigidBodyComponent](cmd, entity) || !hasComponentOfType[ColliderComponent](cmd, entity) || !hasComponentOfType[AABBComponent](cmd, entity) {
		t.Fatal("expected static collision components on imported world chunk")
	}
}

func TestStreamedRuntimePreservesImportedBaseWorldPalette(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "baseworld_palette.gklevel")
	worldPath := filepath.Join(root, "worlds", "baseworld_palette.gkworld")
	chunkPath := filepath.Join(root, "worlds", "baseworld_palette_chunks", "0_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-palette",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 4}},
		NonEmptyVoxelCount: 1,
	})

	paletteColor := content.ImportedWorldPaletteColor{240, 96, 32, 255}
	if err := os.MkdirAll(filepath.Dir(worldPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "world-palette",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Palette:         []content.ImportedWorldPaletteColor{{0, 0, 0, 0}, {0, 0, 0, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}, paletteColor},
		Entries: []content.ImportedWorldChunkEntryDef{{
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 1,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("baseworld_palette")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	assets := app.resources[reflect.TypeOf(AssetServer{})].(*AssetServer)
	if err := StartStreamedLevelRuntime(cmd, assets, StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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

	if state.BaseWorldPalette == (AssetId{}) {
		t.Fatal("expected streamed runtime to allocate a base-world palette asset")
	}
	paletteAsset, ok := assets.voxPalettes[state.BaseWorldPalette]
	if !ok {
		t.Fatalf("expected palette asset %v to be registered", state.BaseWorldPalette)
	}
	if paletteAsset.VoxPalette[4] != [4]uint8{paletteColor[0], paletteColor[1], paletteColor[2], paletteColor[3]} {
		t.Fatalf("expected palette index 4 to be %v, got %v", paletteColor, paletteAsset.VoxPalette[4])
	}

	entity := importedWorldChunkEntityByCoordForStreamedTest(cmd, [3]int{0, 0, 0})
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	if vmc.VoxelPalette != state.BaseWorldPalette {
		t.Fatalf("expected chunk to use base-world palette %v, got %v", state.BaseWorldPalette, vmc.VoxelPalette)
	}
}

func TestStreamedRuntimeExposesImportedBaseWorldMaterialLookup(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "baseworld_materials.gklevel")
	worldPath := filepath.Join(root, "worlds", "baseworld_materials.gkworld")
	chunkPath := filepath.Join(root, "worlds", "baseworld_materials_chunks", "0_0_0.gkchunk")

	chunkDef := &content.ImportedWorldChunkDef{
		WorldID:            "world-materials",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 1, Y: 2, Z: 3, Value: 4}},
		NonEmptyVoxelCount: 1,
	}
	writeImportedWorldChunkForStreamedTest(t, chunkPath, chunkDef)

	if err := os.MkdirAll(filepath.Dir(worldPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "world-materials",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Palette:         []content.ImportedWorldPaletteColor{{0, 0, 0, 0}, {0, 0, 0, 255}, {0, 0, 0, 255}, {0, 0, 0, 255}, {64, 80, 96, 255}},
		Materials: []content.ImportedWorldMaterialDef{{
			ID:                12,
			PaletteIndex:      4,
			SourceTextureName: "CONCRETE01",
			Kind:              "stone",
			CollisionKind:     "solid",
			Roughness:         0.9,
			Tags:              []string{"material:stone"},
		}},
		Entries: []content.ImportedWorldChunkEntryDef{{
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 1,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("baseworld_materials")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	assets := app.resources[reflect.TypeOf(AssetServer{})].(*AssetServer)
	if err := StartStreamedLevelRuntime(cmd, assets, StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}

	if state.BaseWorldManifest == nil || state.BaseWorldManifest.WorldID != "world-materials" {
		t.Fatalf("expected base-world manifest to be retained, got %+v", state.BaseWorldManifest)
	}
	material, ok := state.BaseWorldMaterialForPalette(4)
	if !ok || material.SourceTextureName != "CONCRETE01" || material.Roughness != 0.9 {
		t.Fatalf("expected runtime material lookup, got %+v ok=%t", material, ok)
	}
	material, paletteIndex, ok := state.BaseWorldMaterialForChunkVoxel(chunkDef, [3]int{1, 2, 3})
	if !ok || paletteIndex != 4 || !content.ImportedWorldMaterialHasTag(material, "material:stone") {
		t.Fatalf("expected runtime chunk voxel material lookup, got material=%+v palette=%d ok=%t", material, paletteIndex, ok)
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
		ChunkSize:          16,
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
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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

func TestStreamedRuntimeAppliesImportedWorldChunkOverride(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "imported_delta.gklevel")
	deltaPath := content.DefaultWorldDeltaPath(levelPath)
	worldPath := filepath.Join(root, "worlds", "imported_delta.gkworld")
	chunkPath := filepath.Join(root, "worlds", "imported_delta_chunks", "0_0_0.gkchunk")
	overrideChunkPath := filepath.Join(root, "levels", "imported_delta.gkworlddelta_data", "imported_world-a_0_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldChunkForStreamedTest(t, overrideChunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 7}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-a", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})

	level := content.NewLevelDef("imported_delta")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}
	if err := content.SaveWorldDelta(deltaPath, &content.WorldDeltaDef{
		LevelID: level.ID,
		ImportedWorldChunkOverrides: []content.ImportedWorldChunkOverrideDef{{
			WorldID:      "world-a",
			ChunkCoord:   content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			SnapshotPath: content.AuthorDocumentPath(overrideChunkPath, deltaPath),
		}},
	}); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}

	app, cmd, _ := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
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
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	geometryMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
	if !ok {
		t.Fatalf("expected imported chunk geometry to resolve")
	}
	found, value := geometryMap.GetVoxel(0, 0, 0)
	if !found || value != 7 {
		t.Fatalf("expected imported override voxel value 7, got found=%t value=%d", found, value)
	}
}

func TestStreamedRuntimePersistsDirtyImportedWorldChunkOverrideOnUnload(t *testing.T) {
	root := t.TempDir()
	deltaPath := filepath.Join(root, "levels", "persist.gkworlddelta")
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "persist"
	state.Level = content.NewLevelDef("persist")
	state.Level.ChunkSize = 16
	state.BaseWorldID = "world-a"
	state.WorldDeltaPath = deltaPath
	state.WorldDataDir = content.DefaultWorldDeltaDataDir(deltaPath)
	state.WorldDelta = &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "persist"}
	state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "persist"})
	coord := ChunkCoord{X: 0, Y: 0, Z: 0}

	_, err := commitPreparedStreamedChunk(cmd, assetServerFromApp(cmd.app), state, streamedPreparedChunk{
		Coord: coord,
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-a",
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}
	loaded := state.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected loaded imported chunk, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		geometryMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
		if !ok {
			t.Fatalf("expected imported chunk geometry to resolve")
		}
		geometryMap.SetVoxel(0, 0, 0, 0)
		geometryMap.SetVoxel(1, 0, 0, 9)
	}

	if err := unloadStreamedChunk(cmd, state, coord); err != nil {
		t.Fatalf("unloadStreamedChunk failed: %v", err)
	}

	loadedDelta, err := content.LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if len(loadedDelta.ImportedWorldChunkOverrides) != 1 {
		t.Fatalf("expected one imported world override, got %+v", loadedDelta.ImportedWorldChunkOverrides)
	}
	override := loadedDelta.ImportedWorldChunkOverrides[0]
	snapshotPath := content.ResolveDocumentPath(override.SnapshotPath, deltaPath)
	snapshot, err := content.LoadImportedWorldChunk(snapshotPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk override failed: %v", err)
	}
	if snapshot.NonEmptyVoxelCount != 1 || len(snapshot.Voxels) != 1 || snapshot.Voxels[0] != (content.ImportedWorldVoxelDef{X: 1, Y: 0, Z: 0, Value: 9}) {
		t.Fatalf("unexpected persisted imported chunk snapshot: %+v", snapshot)
	}
}

func TestStreamedRuntimePersistsRuntimeEditedImportedWorldChunkAfterDirtyFlagsClear(t *testing.T) {
	root := t.TempDir()
	deltaPath := filepath.Join(root, "levels", "runtime-edit.gkworlddelta")
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "runtime-edit"
	state.Level = content.NewLevelDef("runtime-edit")
	state.Level.ChunkSize = 16
	state.BaseWorldID = "world-a"
	state.WorldDeltaPath = deltaPath
	state.WorldDataDir = content.DefaultWorldDeltaDataDir(deltaPath)
	state.WorldDelta = &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "runtime-edit"}
	state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "runtime-edit"})
	coord := ChunkCoord{X: 0, Y: 0, Z: 0}

	_, err := commitPreparedStreamedChunk(cmd, assetServerFromApp(cmd.app), state, streamedPreparedChunk{
		Coord: coord,
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-a",
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}
	loaded := state.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected loaded imported chunk, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		assetMap, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &vmc)
		if !ok {
			t.Fatalf("expected imported chunk geometry to resolve")
		}
		assetMap.ClearDirty()
		runtimeMap := assetMap.Copy()
		runtimeObj := core.NewVoxelObject()
		runtimeObj.XBrickMap = runtimeMap
		rtState := &VoxelRtState{
			instanceMap:                map[EntityId]*core.VoxelObject{entity: runtimeObj},
			runtimeEditedVoxelEntities: make(map[EntityId]struct{}),
		}
		cmd.AddResources(rtState)
		rtState.VoxelSphereEdit(entity, mgl32.Vec3{0.5, 0.5, 0.5}, 0.75, 0)
		rtState.VoxelSphereEdit(entity, mgl32.Vec3{2.5, 0.5, 0.5}, 0.75, 5)
		runtimeMap.ClearDirty()
	}

	if err := unloadStreamedChunk(cmd, state, coord); err != nil {
		t.Fatalf("unloadStreamedChunk failed: %v", err)
	}

	loadedDelta, err := content.LoadWorldDelta(deltaPath)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if len(loadedDelta.ImportedWorldChunkOverrides) != 1 {
		t.Fatalf("expected one imported world override, got %+v", loadedDelta.ImportedWorldChunkOverrides)
	}
	override := loadedDelta.ImportedWorldChunkOverrides[0]
	snapshotPath := content.ResolveDocumentPath(override.SnapshotPath, deltaPath)
	snapshot, err := content.LoadImportedWorldChunk(snapshotPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk override failed: %v", err)
	}
	if snapshot.NonEmptyVoxelCount != 1 || len(snapshot.Voxels) != 1 || snapshot.Voxels[0] != (content.ImportedWorldVoxelDef{X: 2, Y: 0, Z: 0, Value: 5}) {
		t.Fatalf("unexpected persisted imported runtime snapshot: %+v", snapshot)
	}
}

func TestStreamedRuntimeRecordsStreamingObservability(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "observability.gklevel")
	worldPath := filepath.Join(root, "worlds", "observability.gkworld")
	chunkPath := filepath.Join(root, "worlds", "observability_chunks", "0_0_0.gkchunk")
	metricSnapshots := make([]StreamedLevelRuntimeMetrics, 0, 8)

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-observe",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-observe", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})

	level := content.NewLevelDef("observability")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath:          levelPath,
		StreamingRadius:    0,
		MetricsLogInterval: time.Nanosecond,
		MetricsSink: func(metrics StreamedLevelRuntimeMetrics) {
			metricSnapshots = append(metricSnapshots, metrics)
		},
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	driveStreamedRuntimeUntil(t, app, func() bool {
		return state.Metrics.CommittedChunkCount == 1
	})

	metrics := state.Metrics
	if metrics.DesiredChunkCount != 1 || metrics.LoadedChunkCount != 1 || metrics.PendingLoadCount != 0 || metrics.PreparedQueueDepth != 0 {
		t.Fatalf("unexpected streaming counts: %+v", metrics)
	}
	if metrics.PreparedChunkCount != 1 || metrics.PrepareErrorCount != 0 {
		t.Fatalf("unexpected prepare metrics: %+v", metrics)
	}
	if metrics.LastPrepareDuration <= 0 || metrics.TotalPrepareDuration < metrics.LastPrepareDuration {
		t.Fatalf("unexpected prepare timing: %+v", metrics)
	}
	if metrics.ChunksCommittedLastFrame != 1 || metrics.FullChunksCommittedLastFrame != 1 || metrics.ProxyChunksCommittedLastFrame != 0 || metrics.EntitiesCommittedLastFrame != 1 || metrics.LastCommitEntityCount != 1 {
		t.Fatalf("unexpected commit counts: %+v", metrics)
	}
	if metrics.CommittedChunkCount != 1 || metrics.FullChunkCommitCount != 1 || metrics.ProxyChunkCommitCount != 0 || metrics.CollisionChunkCommitCount != 0 {
		t.Fatalf("unexpected commit totals: %+v", metrics)
	}
	if metrics.CommitErrorCount != 0 || metrics.LastCommitDuration <= 0 || metrics.TotalCommitDuration < metrics.LastCommitDuration {
		t.Fatalf("unexpected commit timing: %+v", metrics)
	}
	if metrics.LastCommitWorldDuration <= 0 || metrics.LastCommitTerrainDuration != 0 || metrics.LastCommitPlacementDuration != 0 || metrics.LastCommitFlushCount != 1 || metrics.LastCommitFlushDuration <= 0 {
		t.Fatalf("unexpected commit breakdown timing: %+v", metrics)
	}
	if metrics.LastCommitWorldVoxelCount != 1 || metrics.LastCommitWorldBuildDuration != 0 || metrics.LastCommitWorldRegisterDuration <= 0 || metrics.LastCommitWorldEntityDuration <= 0 {
		t.Fatalf("unexpected commit breakdown timing: %+v", metrics)
	}
	foundCommittedSnapshot := false
	for _, snapshot := range metricSnapshots {
		if snapshot.CommittedChunkCount == 1 && snapshot.LoadedChunkCount == 1 {
			foundCommittedSnapshot = true
			break
		}
	}
	if !foundCommittedSnapshot {
		t.Fatalf("expected metrics sink to receive committed snapshot, got %+v", metricSnapshots)
	}
	if line := metrics.LogLine(); line == "" || !strings.Contains(line, "streaming metrics:") || !strings.Contains(line, "committed_total=1") || !strings.Contains(line, "full_committed_total=1") || !strings.Contains(line, "commit_world_ms=") || !strings.Contains(line, "commit_world_register_ms=") || !strings.Contains(line, "commit_flushes=1") {
		t.Fatalf("unexpected metrics log line: %q", line)
	}
}

func TestStreamedRuntimePrepareJobLimitDefersScheduling(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "prepare_limit.gklevel")
	worldPath := filepath.Join(root, "worlds", "prepare_limit.gkworld")
	chunkPath := filepath.Join(root, "worlds", "prepare_limit_chunks", "0_0_0.gkchunk")
	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-prepare-limit",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-prepare-limit", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})
	level := content.NewLevelDef("prepare_limit")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0, MaxPrepareJobs: 1}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	state.activePrepareMu.Lock()
	state.activeChunkPrepares = 1
	state.activePrepareMu.Unlock()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	app.callSystems(0, execute, DynamicUpdate)

	if _, ok := state.DesiredChunks[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected desired chunk at origin, got %+v", state.DesiredChunks)
	}
	if len(state.PendingLoads) != 0 || len(state.PendingProxyLoads) != 0 {
		t.Fatalf("expected prepare limit to defer scheduling, got pending=%+v pending_proxy=%+v", state.PendingLoads, state.PendingProxyLoads)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.ActivePrepareJobCount != 1 || state.Metrics.ActiveChunkPrepareJobCount != 1 || state.Metrics.ActiveProxyPrepareJobCount != 0 {
		t.Fatalf("unexpected active prepare metrics: %+v", state.Metrics)
	}
	if line := state.Metrics.LogLine(); !strings.Contains(line, "active_prepare=1") || !strings.Contains(line, "active_prepare_chunks=1") || !strings.Contains(line, "active_prepare_proxies=0") {
		t.Fatalf("expected active prepare metrics in log line, got %q", line)
	}
}

func TestStreamedRuntimeMetricsExposeSectorProxyAndLoadableCounts(t *testing.T) {
	state := &StreamedLevelRuntimeState{
		DesiredChunks: map[ChunkCoord]struct{}{
			{X: 0, Y: 0, Z: 0}: {},
			{X: 1, Y: 0, Z: 0}: {},
			{X: 9, Y: 0, Z: 0}: {},
		},
		KeepChunks: map[ChunkCoord]struct{}{
			{X: 0, Y: 0, Z: 0}: {},
			{X: 2, Y: 0, Z: 0}: {},
		},
		CollisionChunks: map[ChunkCoord]struct{}{
			{X: 0, Y: 0, Z: 0}: {},
		},
		DestructionChunks: map[ChunkCoord]struct{}{
			{X: 0, Y: 0, Z: 0}: {},
		},
		DesiredSectors: map[ChunkCoord]struct{}{
			{X: 10, Y: 0, Z: 0}: {},
		},
		KeepSectors: map[ChunkCoord]struct{}{
			{X: 10, Y: 0, Z: 0}: {},
			{X: 11, Y: 0, Z: 0}: {},
		},
		PendingLoads: map[ChunkCoord]struct{}{
			{X: 1, Y: 0, Z: 0}: {},
		},
		PendingProxyLoads: map[ChunkCoord]struct{}{
			{X: 10, Y: 0, Z: 0}: {},
		},
		PreparedLoads:      make(chan streamedPreparedChunk, 4),
		PreparedProxyLoads: make(chan streamedPreparedSectorProxy, 4),
		LoadedChunks: map[ChunkCoord]*streamedLoadedChunk{
			{X: 0, Y: 0, Z: 0}: {},
		},
		LoadedSectorProxies: map[ChunkCoord]*streamedLoadedSectorProxy{
			{X: 10, Y: 0, Z: 0}: {},
			{X: 11, Y: 0, Z: 0}: {},
			{X: 12, Y: 0, Z: 0}: {},
		},
		PlacementsByChunk: map[ChunkCoord][]streamedPlacementInstance{
			{X: 0, Y: 0, Z: 0}: {{PlacementID: "placement"}},
		},
		ImportedWorldSectors: map[ChunkCoord]content.ImportedWorldSectorDef{
			{X: 10, Y: 0, Z: 0}: {
				Coord:         content.TerrainChunkCoordDef{X: 10, Y: 0, Z: 0},
				FullChunkRefs: []content.TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
			},
			{X: 11, Y: 0, Z: 0}: {
				Coord:         content.TerrainChunkCoordDef{X: 11, Y: 0, Z: 0},
				FullChunkRefs: []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
			},
		},
		ImportedWorldEntries: map[ChunkCoord]content.ImportedWorldChunkEntryDef{
			{X: 0, Y: 0, Z: 0}: {NonEmptyVoxelCount: 1},
			{X: 1, Y: 0, Z: 0}: {NonEmptyVoxelCount: 1},
		},
	}
	state.PreparedLoads <- streamedPreparedChunk{Coord: ChunkCoord{X: 1, Y: 0, Z: 0}}
	state.PreparedProxyLoads <- streamedPreparedSectorProxy{SectorCoord: ChunkCoord{X: 10, Y: 0, Z: 0}}

	refreshStreamedRuntimeMetricsCounts(state)

	metrics := state.Metrics
	if metrics.DesiredChunkCount != 3 || metrics.DesiredLoadableChunkCount != 2 {
		t.Fatalf("unexpected desired metrics: %+v", metrics)
	}
	if metrics.KeepChunkCount != 2 || metrics.KeepLoadableChunkCount != 1 || metrics.CollisionChunkCount != 1 || metrics.CollisionLoadableChunkCount != 1 || metrics.DestructionChunkCount != 1 || metrics.DestructionLoadableChunkCount != 1 {
		t.Fatalf("unexpected keep/collision metrics: %+v", metrics)
	}
	if metrics.DesiredSectorCount != 1 || metrics.KeepSectorCount != 2 {
		t.Fatalf("unexpected sector metrics: %+v", metrics)
	}
	if metrics.DesiredSectorFullLoadedCount != 0 || metrics.KeepSectorFullLoadedCount != 1 {
		t.Fatalf("unexpected full-loaded sector metrics: %+v", metrics)
	}
	if metrics.PendingLoadCount != 1 || metrics.PendingProxyLoadCount != 1 {
		t.Fatalf("unexpected pending metrics: %+v", metrics)
	}
	if metrics.PreparedQueueDepth != 2 || metrics.PreparedChunkQueueDepth != 1 || metrics.PreparedProxyQueueDepth != 1 {
		t.Fatalf("unexpected prepared queue metrics: %+v", metrics)
	}
	if metrics.LoadedChunkCount != 1 || metrics.LoadedSectorProxyCount != 3 {
		t.Fatalf("unexpected loaded metrics: %+v", metrics)
	}
	if metrics.LoadedSectorProxyFullReadyCount != 1 || metrics.LoadedSectorProxyFullPendingCount != 1 || metrics.LoadedSectorProxyOutOfKeepCount != 1 {
		t.Fatalf("unexpected proxy residency metrics: %+v", metrics)
	}
	line := metrics.LogLine()
	for _, token := range []string{"desired_loadable=2", "collision_loadable=1", "destruction_loadable=1", "desired_sectors=1", "desired_sectors_full=0", "keep_sectors_full=1", "pending_proxy=1", "prepared_proxies=1", "loaded_proxies=3", "proxy_full_ready=1", "proxy_full_pending=1", "proxy_out_of_keep=1"} {
		if !strings.Contains(line, token) {
			t.Fatalf("expected log line to contain %q, got %q", token, line)
		}
	}
}

func TestStreamedRuntimeCommitBudgetLeavesPreparedChunksQueued(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.Config = StreamedLevelRuntimeConfig{MaxChunkCommitsPerFrame: 2}

	coords := []ChunkCoord{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 2, Y: 0, Z: 0},
	}
	for _, coord := range coords {
		state.DesiredChunks[coord] = struct{}{}
		state.PendingLoads[coord] = struct{}{}
		state.PreparedLoads <- streamedPreparedChunk{Coord: coord, PrepareDuration: time.Millisecond}
	}

	commitPreparedStreamedChunksSystem(cmd, newSpawnTestAssetServer(), state)

	if len(state.LoadedChunks) != 2 {
		t.Fatalf("expected two loaded chunks after first budgeted commit, got %+v", state.LoadedChunks)
	}
	if len(state.PreparedLoads) != 1 {
		t.Fatalf("expected one prepared chunk to remain queued, got %d", len(state.PreparedLoads))
	}
	if state.Metrics.ChunksCommittedLastFrame != 2 || !state.Metrics.CommitBudgetHitLastFrame || state.Metrics.CommitBudgetReason != "chunk_count" {
		t.Fatalf("unexpected first-frame budget metrics: %+v", state.Metrics)
	}

	commitPreparedStreamedChunksSystem(cmd, newSpawnTestAssetServer(), state)

	if len(state.LoadedChunks) != 3 {
		t.Fatalf("expected all chunks loaded after second commit, got %+v", state.LoadedChunks)
	}
	if len(state.PreparedLoads) != 0 {
		t.Fatalf("expected prepared queue to be empty, got %d", len(state.PreparedLoads))
	}
	if state.Metrics.ChunksCommittedLastFrame != 1 || state.Metrics.CommitBudgetHitLastFrame {
		t.Fatalf("unexpected second-frame budget metrics: %+v", state.Metrics)
	}
}

func TestStreamedRuntimeSkipsEmptyDesiredChunks(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "sparse_radius.gklevel")
	worldPath := filepath.Join(root, "worlds", "sparse_radius.gkworld")
	chunkPath := filepath.Join(root, "worlds", "sparse_radius_chunks", "0_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sparse",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-sparse", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})

	level := content.NewLevelDef("sparse_radius")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{
		LevelPath:       levelPath,
		StreamingRadius: 8,
	}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	app.callSystems(0, execute, DynamicUpdate)

	if len(state.DesiredChunks) != 4913 {
		t.Fatalf("expected full radius desired set, got %d", len(state.DesiredChunks))
	}
	realChunk := ChunkCoord{X: 0, Y: 0, Z: 0}
	if len(state.PendingLoads)+len(state.LoadedChunks) != 1 {
		t.Fatalf("expected only non-empty desired chunk to load or commit, got pending %+v loaded %+v", state.PendingLoads, state.LoadedChunks)
	}
	if _, ok := state.PendingLoads[realChunk]; !ok {
		if _, loaded := state.LoadedChunks[realChunk]; !loaded {
			t.Fatalf("expected pending or loaded real chunk, got pending %+v loaded %+v", state.PendingLoads, state.LoadedChunks)
		}
	}
}

func TestStreamedRuntimeIndexesImportedWorldChunksFromSectors(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "sector_runtime.gklevel")
	worldPath := filepath.Join(root, "worlds", "sector_runtime.gkworld")
	chunk0Path := filepath.Join(root, "worlds", "sector_runtime_chunks", "0_0_0.gkchunk")
	chunk1Path := filepath.Join(root, "worlds", "sector_runtime_chunks", "1_0_0.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunk0Path, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldChunkForStreamedTest(t, chunk1Path, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector",
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "world-sector",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Entries: []content.ImportedWorldChunkEntryDef{
			{
				Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
				ChunkPath:          content.AuthorDocumentPath(chunk0Path, worldPath),
				NonEmptyVoxelCount: 1,
			},
			{
				Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
				ChunkPath:          content.AuthorDocumentPath(chunk1Path, worldPath),
				NonEmptyVoxelCount: 1,
			},
		},
		Sectors: []content.ImportedWorldSectorDef{{
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin:          [3]float32{0, 0, 0},
			BoundsMax:          [3]float32{32, 16, 16},
			FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}},
			NonEmptyVoxelCount: 2,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("sector_runtime")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	_, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}

	if len(state.ImportedWorldSectors) != 1 {
		t.Fatalf("expected one imported world sector, got %+v", state.ImportedWorldSectors)
	}
	if _, ok := state.ImportedWorldEntries[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected sector-referenced chunk to be indexed, got %+v", state.ImportedWorldEntries)
	}
	if _, ok := state.ImportedWorldEntries[ChunkCoord{X: 1, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected second sector-referenced chunk to be indexed, got %+v", state.ImportedWorldEntries)
	}
}

func TestStreamedRuntimeUsesImportedWorldSectorVisibilityForDesiredChunks(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "sector_visibility_runtime.gklevel")
	worldPath := filepath.Join(root, "worlds", "sector_visibility_runtime.gkworld")
	chunkDir := filepath.Join(root, "worlds", "sector_visibility_runtime_chunks")
	chunkCoords := []content.TerrainChunkCoordDef{
		{X: 0, Y: 0, Z: 0},
		{X: 2, Y: 0, Z: 0},
		{X: 4, Y: 0, Z: 0},
	}
	entries := make([]content.ImportedWorldChunkEntryDef, 0, len(chunkCoords))
	for _, coord := range chunkCoords {
		chunkPath := filepath.Join(chunkDir, fmt.Sprintf("%d_%d_%d.gkchunk", coord.X, coord.Y, coord.Z))
		writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
			WorldID:            "world-sector-visibility",
			Coord:              coord,
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		})
		entries = append(entries, content.ImportedWorldChunkEntryDef{
			Coord:              coord,
			ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
			NonEmptyVoxelCount: 1,
		})
	}
	if err := content.SaveImportedWorld(worldPath, &content.ImportedWorldDef{
		WorldID:         "world-sector-visibility",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Entries:         entries,
		Sectors: []content.ImportedWorldSectorDef{{
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin:          [3]float32{0, 0, 0},
			BoundsMax:          [3]float32{16, 16, 16},
			FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
			SourceLeafIDs:      []int{1},
			VisibleSectorRefs:  []content.TerrainChunkCoordDef{{X: 4, Y: 0, Z: 0}},
			NonEmptyVoxelCount: 1,
		}, {
			Coord:              content.TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
			BoundsMin:          [3]float32{32, 0, 0},
			BoundsMax:          [3]float32{48, 16, 16},
			FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 2, Y: 0, Z: 0}},
			NonEmptyVoxelCount: 1,
		}, {
			Coord:              content.TerrainChunkCoordDef{X: 4, Y: 0, Z: 0},
			BoundsMin:          [3]float32{64, 0, 0},
			BoundsMax:          [3]float32{80, 16, 16},
			FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 4, Y: 0, Z: 0}},
			NonEmptyVoxelCount: 1,
		}},
	}); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("sector_visibility_runtime")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0, KeepRadius: 3, PrefetchRadius: 3},
	)
	app.FlushCommands()

	app.callSystems(0, execute, DynamicUpdate)

	current := ChunkCoord{X: 0, Y: 0, Z: 0}
	hiddenInsideRadius := ChunkCoord{X: 2, Y: 0, Z: 0}
	visibleOutsideRadius := ChunkCoord{X: 4, Y: 0, Z: 0}
	if _, ok := state.DesiredSectors[current]; !ok {
		t.Fatalf("expected current sector to be desired, got %+v", state.DesiredSectors)
	}
	if _, ok := state.DesiredSectors[visibleOutsideRadius]; !ok {
		t.Fatalf("expected PVS-visible sector to be desired, got %+v", state.DesiredSectors)
	}
	if _, ok := state.DesiredSectors[hiddenInsideRadius]; ok {
		t.Fatalf("did not expect hidden imported sector inside old radius to be desired, got %+v", state.DesiredSectors)
	}
	if _, ok := state.PendingLoads[current]; !ok {
		t.Fatalf("expected current chunk to be scheduled, got %+v", state.PendingLoads)
	}
	if _, ok := state.PendingLoads[visibleOutsideRadius]; !ok {
		t.Fatalf("expected PVS-visible chunk outside old radius to be scheduled, got %+v", state.PendingLoads)
	}
	if _, ok := state.PendingLoads[hiddenInsideRadius]; ok {
		t.Fatalf("did not expect hidden imported chunk inside old radius to be scheduled, got %+v", state.PendingLoads)
	}
}

func TestStreamedRuntimeSchedulesSectorProxyForDesiredImportedChunk(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "sector_proxy_schedule.gklevel")
	worldPath := filepath.Join(root, "worlds", "sector_proxy_schedule.gkworld")
	chunkPath := filepath.Join(root, "worlds", "sector_proxy_schedule_chunks", "0_0_0.gkchunk")
	proxyPath := filepath.Join(root, "worlds", "lods", "sector_proxy_schedule_lod1.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector-proxy",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldChunkForStreamedTest(t, proxyPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector-proxy",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          4,
		VoxelResolution:    4,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-sector-proxy", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})
	manifest, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	manifest.Sectors[0].LODs = []content.ImportedWorldLODDef{{
		Level:              1,
		Kind:               "voxel_proxy",
		ChunkPath:          content.AuthorDocumentPath(proxyPath, worldPath),
		ChunkSize:          4,
		VoxelResolution:    4,
		NonEmptyVoxelCount: 1,
	}}
	if err := content.SaveImportedWorld(worldPath, manifest); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("sector_proxy_schedule")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	app.callSystems(0, execute, DynamicUpdate)

	if _, ok := state.DesiredSectors[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected desired sector at origin, got %+v", state.DesiredSectors)
	}
	if _, ok := state.PendingProxyLoads[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected pending proxy load at origin, got %+v", state.PendingProxyLoads)
	}
}

func TestStreamedRuntimeCanDisableSectorProxyScheduling(t *testing.T) {
	root := t.TempDir()
	levelPath := filepath.Join(root, "levels", "sector_proxy_disabled.gklevel")
	worldPath := filepath.Join(root, "worlds", "sector_proxy_disabled.gkworld")
	chunkPath := filepath.Join(root, "worlds", "sector_proxy_disabled_chunks", "0_0_0.gkchunk")
	proxyPath := filepath.Join(root, "worlds", "lods", "sector_proxy_disabled_lod1.gkchunk")

	writeImportedWorldChunkForStreamedTest(t, chunkPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector-proxy-disabled",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldChunkForStreamedTest(t, proxyPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector-proxy-disabled",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          4,
		VoxelResolution:    4,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	writeImportedWorldManifestForStreamedTest(t, worldPath, "world-sector-proxy-disabled", []content.ImportedWorldChunkEntryDef{{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkPath:          content.AuthorDocumentPath(chunkPath, worldPath),
		NonEmptyVoxelCount: 1,
	}})
	manifest, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	manifest.Sectors[0].LODs = []content.ImportedWorldLODDef{{
		Level:              1,
		Kind:               "voxel_proxy",
		ChunkPath:          content.AuthorDocumentPath(proxyPath, worldPath),
		ChunkSize:          4,
		VoxelResolution:    4,
		NonEmptyVoxelCount: 1,
	}}
	if err := content.SaveImportedWorld(worldPath, manifest); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	level := content.NewLevelDef("sector_proxy_disabled")
	level.ChunkSize = 16
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:         content.ImportedWorldKindVoxelWorld,
		ManifestPath: content.AuthorDocumentPath(worldPath, levelPath),
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0, DisableSectorProxies: true}); err != nil {
		t.Fatalf("StartStreamedLevelRuntime failed: %v", err)
	}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 0},
	)
	app.FlushCommands()

	app.callSystems(0, execute, DynamicUpdate)

	if _, ok := state.DesiredSectors[ChunkCoord{X: 0, Y: 0, Z: 0}]; !ok {
		t.Fatalf("expected desired sector at origin, got %+v", state.DesiredSectors)
	}
	if _, ok := state.PendingProxyLoads[ChunkCoord{X: 0, Y: 0, Z: 0}]; ok {
		t.Fatalf("did not expect pending proxy load when proxies are disabled, got %+v", state.PendingProxyLoads)
	}
}

func TestStreamedRuntimeCommitsSectorProxyWithoutCollision(t *testing.T) {
	root := t.TempDir()
	worldPath := filepath.Join(root, "worlds", "sector_proxy_commit.gkworld")
	proxyPath := filepath.Join(root, "worlds", "lods", "sector_proxy_commit_lod1.gkchunk")
	writeImportedWorldChunkForStreamedTest(t, proxyPath, &content.ImportedWorldChunkDef{
		WorldID:            "world-sector-proxy",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          4,
		VoxelResolution:    4,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	})
	lod := content.ImportedWorldLODDef{
		Level:              1,
		Kind:               "voxel_proxy",
		ChunkPath:          content.AuthorDocumentPath(proxyPath, worldPath),
		ChunkSize:          4,
		VoxelResolution:    4,
		NonEmptyVoxelCount: 1,
	}

	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "proxy_level"
	state.BaseWorldID = "world-sector-proxy"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy_level"})
	state.DesiredSectors[ChunkCoord{X: 0, Y: 0, Z: 0}] = struct{}{}
	job := streamedSectorProxyLoadJob{
		SectorCoord:  ChunkCoord{X: 0, Y: 0, Z: 0},
		ManifestPath: worldPath,
		LOD:          lod,
		Loader:       NewRuntimeContentLoader(),
	}
	prepared := prepareStreamedSectorProxyLoad(job)
	if prepared.Err != nil {
		t.Fatalf("prepareStreamedSectorProxyLoad failed: %v", prepared.Err)
	}
	if prepared.PreparedGeometry == nil || prepared.PreparedGeometry.GetVoxelCount() != 1 {
		t.Fatalf("expected prepared proxy geometry, got %+v", prepared.PreparedGeometry)
	}
	entities, err := commitPreparedStreamedSectorProxy(cmd, newSpawnTestAssetServer(), state, prepared)
	if err != nil {
		t.Fatalf("commitPreparedStreamedSectorProxy failed: %v", err)
	}
	if state.Metrics.LastCommitWorldBuildDuration != 0 || state.Metrics.LastCommitWorldRegisterDuration <= 0 {
		t.Fatalf("expected proxy commit to use prepared geometry, got %+v", state.Metrics)
	}
	if entities != 1 {
		t.Fatalf("expected one proxy entity, got %d", entities)
	}
	loaded := state.LoadedSectorProxies[ChunkCoord{X: 0, Y: 0, Z: 0}]
	if loaded == nil || loaded.Entity == 0 {
		t.Fatalf("expected loaded proxy, got %+v", state.LoadedSectorProxies)
	}
	if hasComponentOfType[RigidBodyComponent](cmd, loaded.Entity) || hasComponentOfType[ColliderComponent](cmd, loaded.Entity) {
		t.Fatalf("expected proxy entity %d to be visual-only", loaded.Entity)
	}
	vmc := mustVoxelModelComponentForLevelTest(t, cmd, loaded.Entity)
	if vmc.IsTerrainChunk || vmc.TerrainGroupID != 0 || vmc.TerrainChunkSize != 0 || vmc.ShadowSeamWorldEpsilon != 0 ||
		vmc.VoxelAdjacencyGroupID != 0 || vmc.VoxelAdjacencyChunkSize != 0 {
		t.Fatalf("expected proxy entity %d to skip terrain renderer metadata, got %+v", loaded.Entity, vmc)
	}
	if !vmc.DisableShadows || !vmc.DisableOcclusionCulling {
		t.Fatalf("expected proxy entity %d to skip shadows and occlusion culling, got %+v", loaded.Entity, vmc)
	}
	if !vmc.ShareTerrainGeometry || !vmc.RetainRendererGeometry {
		t.Fatalf("expected proxy entity %d to opt into renderer geometry residency, got %+v", loaded.Entity, vmc)
	}
}

func TestStreamedRuntimeSkipsLateSectorProxyForFullKeptSector(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	assets := newSpawnTestAssetServer()
	state.Initialized = true
	state.LevelID = "proxy_skip"
	state.BaseWorldID = "world-sector-proxy-skip"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy_skip"})
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.KeepSectors[sectorCoord] = struct{}{}
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 1,
	}
	state.ImportedWorldEntries[fullCoord] = content.ImportedWorldChunkEntryDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		NonEmptyVoxelCount: 1,
	}
	state.LoadedChunks[fullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	entities, err := commitPreparedStreamedSectorProxy(cmd, assets, state, streamedPreparedSectorProxyForTest(sectorCoord))
	if err != nil {
		t.Fatalf("commitPreparedStreamedSectorProxy failed: %v", err)
	}
	if entities != 0 {
		t.Fatalf("expected late proxy commit to be skipped for full-kept sector, got %d entities", entities)
	}
	if _, loaded := state.LoadedSectorProxies[sectorCoord]; loaded {
		t.Fatalf("did not expect a proxy to be loaded for full-kept sector, got %+v", state.LoadedSectorProxies)
	}
}

func TestStreamedRuntimeCommitsFallbackSectorProxyHiddenWhileFullLoaded(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	assets := newSpawnTestAssetServer()
	state.Initialized = true
	state.LevelID = "proxy_fallback"
	state.BaseWorldID = "world-sector-proxy-fallback"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy_fallback"})
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 1,
	}
	state.ImportedWorldEntries[fullCoord] = content.ImportedWorldChunkEntryDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		NonEmptyVoxelCount: 1,
	}
	state.LoadedChunks[fullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	entities, err := commitPreparedStreamedSectorProxy(cmd, assets, state, streamedPreparedSectorProxyForTest(sectorCoord))
	if err != nil {
		t.Fatalf("commitPreparedStreamedSectorProxy failed: %v", err)
	}
	if entities != 1 {
		t.Fatalf("expected fallback proxy to commit, got %d entities", entities)
	}
	loaded := state.LoadedSectorProxies[sectorCoord]
	if loaded == nil || loaded.Entity == 0 {
		t.Fatalf("expected loaded fallback proxy, got %+v", state.LoadedSectorProxies)
	}
	if !VoxelEntityRenderHidden(cmd, loaded.Entity) {
		t.Fatalf("expected fallback proxy %d to spawn hidden while full detail is still loaded", loaded.Entity)
	}
}

func TestStreamedRuntimeKeepsLateSectorProxyVisibleWhenFullChunksArePartial(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	assets := newSpawnTestAssetServer()
	state.Initialized = true
	state.LevelID = "proxy_partial"
	state.BaseWorldID = "world-sector-proxy-partial"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy_partial"})
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	loadedFullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	missingFullCoord := ChunkCoord{X: 1, Y: 0, Z: 0}
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord: content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs: []content.TerrainChunkCoordDef{
			{X: 0, Y: 0, Z: 0},
			{X: 1, Y: 0, Z: 0},
		},
		NonEmptyVoxelCount: 2,
	}
	for _, coord := range []ChunkCoord{loadedFullCoord, missingFullCoord} {
		state.ImportedWorldEntries[coord] = content.ImportedWorldChunkEntryDef{
			Coord:              content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
			NonEmptyVoxelCount: 1,
		}
	}
	state.LoadedChunks[loadedFullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	entities, err := commitPreparedStreamedSectorProxy(cmd, assets, state, streamedPreparedSectorProxyForTest(sectorCoord))
	if err != nil {
		t.Fatalf("commitPreparedStreamedSectorProxy failed: %v", err)
	}
	if entities != 1 {
		t.Fatalf("expected late partial proxy to commit hidden for fallback, got %d entities", entities)
	}
	loaded := state.LoadedSectorProxies[sectorCoord]
	if loaded == nil || loaded.Entity == 0 {
		t.Fatalf("expected loaded partial fallback proxy, got %+v", state.LoadedSectorProxies)
	}
	if VoxelEntityRenderHidden(cmd, loaded.Entity) {
		t.Fatalf("expected partial fallback proxy %d to remain visible while full sector coverage is incomplete", loaded.Entity)
	}
}

func TestStreamedRuntimeSectorProxyCommitNeededRespectsFullKeepResidency(t *testing.T) {
	_, _, state := newStreamedRuntimeHarness(t)
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 1,
	}
	state.ImportedWorldEntries[fullCoord] = content.ImportedWorldChunkEntryDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		NonEmptyVoxelCount: 1,
	}
	state.LoadedChunks[fullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	state.KeepSectors[sectorCoord] = struct{}{}
	if streamedSectorProxyCommitNeeded(state, sectorCoord) {
		t.Fatal("did not expect a proxy to be needed while full sector is kept")
	}
	delete(state.KeepSectors, sectorCoord)
	if !streamedSectorProxyCommitNeeded(state, sectorCoord) {
		t.Fatal("expected a proxy to be needed when full sector is loaded but outside keep residency")
	}
}

func TestStreamedRuntimeHidesSectorProxyAfterFullChunksLoaded(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy-hide"})
	proxyEntity := cmd.AddEntity(&TransformComponent{})
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 1,
	}
	state.ImportedWorldEntries[fullCoord] = content.ImportedWorldChunkEntryDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		NonEmptyVoxelCount: 1,
	}
	state.LoadedSectorProxies[sectorCoord] = &streamedLoadedSectorProxy{Entity: proxyEntity}
	state.LoadedChunks[fullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
	cmd.app.FlushCommands()

	if _, ok := state.LoadedSectorProxies[sectorCoord]; !ok {
		t.Fatalf("expected full chunk residency to retain hidden proxy, got %+v", state.LoadedSectorProxies)
	}
	if !VoxelEntityRenderHidden(cmd, proxyEntity) {
		t.Fatalf("expected proxy entity %d to be hidden", proxyEntity)
	}
}

func TestStreamedRuntimeKeepsSectorProxyVisibleUntilAllFullChunksLoaded(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	loadedFullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	missingFullCoord := ChunkCoord{X: 1, Y: 0, Z: 0}
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy-hide-partial"})
	proxyEntity := cmd.AddEntity(&TransformComponent{})
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord: content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs: []content.TerrainChunkCoordDef{
			{X: 0, Y: 0, Z: 0},
			{X: 1, Y: 0, Z: 0},
		},
		NonEmptyVoxelCount: 2,
	}
	for _, coord := range []ChunkCoord{loadedFullCoord, missingFullCoord} {
		state.ImportedWorldEntries[coord] = content.ImportedWorldChunkEntryDef{
			Coord:              content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
			NonEmptyVoxelCount: 1,
		}
	}
	state.LoadedSectorProxies[sectorCoord] = &streamedLoadedSectorProxy{Entity: proxyEntity}
	state.LoadedChunks[loadedFullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
	cmd.app.FlushCommands()

	if VoxelEntityRenderHidden(cmd, proxyEntity) {
		t.Fatalf("expected proxy entity %d to remain visible while full sector coverage is incomplete", proxyEntity)
	}
}

func TestStreamedRuntimeShowsSectorProxyWhenFullChunksUnload(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "proxy-show"})
	proxyEntity := cmd.AddEntity(&TransformComponent{}, &VoxelRenderHiddenComponent{})
	cmd.app.FlushCommands()
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs:      []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 1,
	}
	state.ImportedWorldEntries[fullCoord] = content.ImportedWorldChunkEntryDef{
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		NonEmptyVoxelCount: 1,
	}
	state.LoadedSectorProxies[sectorCoord] = &streamedLoadedSectorProxy{Entity: proxyEntity}
	state.LoadedChunks[fullCoord] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}

	setStreamedSectorProxyHidden(cmd, state, sectorCoord, false)
	if err := unloadStreamedChunk(cmd, state, fullCoord); err != nil {
		t.Fatalf("unloadStreamedChunk failed: %v", err)
	}
	cmd.app.FlushCommands()

	if _, ok := state.LoadedSectorProxies[sectorCoord]; !ok {
		t.Fatalf("expected proxy to remain loaded")
	}
	if VoxelEntityRenderHidden(cmd, proxyEntity) {
		t.Fatalf("expected proxy entity %d to be visible after full chunk unload", proxyEntity)
	}
}

func TestStreamedRuntimeDefersImportedFullChunkUnloadUntilProxyLoaded(t *testing.T) {
	_, _, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	sectorCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	fullCoord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.ImportedChunkSector[fullCoord] = sectorCoord
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		Coord:         content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		FullChunkRefs: []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		LODs: []content.ImportedWorldLODDef{{
			Kind:      "voxel_proxy",
			ChunkPath: "lods/0_0_0.gkchunk",
		}},
	}

	if !streamedChunkNeedsProxyBeforeUnload(state, fullCoord) {
		t.Fatalf("expected full chunk unload to wait for missing proxy")
	}

	state.LoadedSectorProxies[sectorCoord] = &streamedLoadedSectorProxy{Entity: 1}
	if streamedChunkNeedsProxyBeforeUnload(state, fullCoord) {
		t.Fatalf("expected full chunk unload to proceed once proxy is loaded")
	}
}

func TestStreamedRuntimeCanCommitImportedWorldFullVisualWithoutCollision(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "visual_only_level"
	state.BaseWorldID = "world-visual-only"
	state.BaseWorldCollisionEnabled = true
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "visual_only_level"})

	prepared := streamedPreparedChunk{
		Coord: ChunkCoord{X: 2, Y: 0, Z: 0},
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-visual-only",
			Coord:              content.TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	}

	_, err := commitPreparedStreamedChunk(cmd, newSpawnTestAssetServer(), state, prepared)
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}

	loaded := state.LoadedChunks[prepared.Coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected one imported full visual entity, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		if hasComponentOfType[RigidBodyComponent](cmd, entity) || hasComponentOfType[ColliderComponent](cmd, entity) || hasComponentOfType[AABBComponent](cmd, entity) {
			t.Fatalf("expected prefetched full visual chunk %d to have no collision", entity)
		}
		vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		if !vmc.IsTerrainChunk || vmc.TerrainGroupID == 0 || vmc.TerrainChunkSize == 0 {
			t.Fatalf("expected full imported chunk %d to keep terrain metadata, got %+v", entity, vmc)
		}
		if vmc.VoxelAdjacencyGroupID == 0 || vmc.VoxelAdjacencyChunkCoord != [3]int{prepared.Coord.X, prepared.Coord.Y, prepared.Coord.Z} || vmc.VoxelAdjacencyChunkSize == 0 {
			t.Fatalf("expected full imported chunk %d to keep voxel adjacency metadata, got %+v", entity, vmc)
		}
		if !vmc.ShareTerrainGeometry || !vmc.RetainRendererGeometry {
			t.Fatalf("expected immutable full imported chunk %d to opt into renderer geometry residency, got %+v", entity, vmc)
		}
	}
}

func TestStreamedRuntimeReusesPreparedImportedWorldGeometryAcrossReload(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	assets := newSpawnTestAssetServer()
	state.Initialized = true
	state.LevelID = "geometry_cache_level"
	state.BaseWorldID = "world-geometry-cache"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "geometry_cache_level"})
	state.PreparedGeometryCache = newStreamedPreparedGeometryCache(8)
	coord := ChunkCoord{X: 0, Y: 0, Z: 0}
	chunk := &content.ImportedWorldChunkDef{
		WorldID:            "world-geometry-cache",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	}
	cacheKey := "test:world-geometry-cache:0:0:0"
	preparedGeometry, _ := state.PreparedGeometryCache.getOrBuild(cacheKey, func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunk)
	})

	firstPrepared := streamedPreparedChunk{
		Coord:                                 coord,
		ImportedWorldChunk:                    chunk,
		PreparedImportedWorldGeometry:         preparedGeometry,
		PreparedImportedWorldGeometryCacheKey: cacheKey,
	}
	if _, err := commitPreparedStreamedChunk(cmd, assets, state, firstPrepared); err != nil {
		t.Fatalf("first commitPreparedStreamedChunk failed: %v", err)
	}
	firstLoaded := state.LoadedChunks[coord]
	if firstLoaded == nil || len(firstLoaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected first loaded chunk, got %+v", state.LoadedChunks)
	}
	var firstAsset AssetId
	for entity := range firstLoaded.ImportedWorldEntities {
		firstAsset = mustVoxelModelComponentForLevelTest(t, cmd, entity).OverrideGeometry
	}
	if firstAsset == (AssetId{}) {
		t.Fatal("expected first imported chunk to use override geometry")
	}
	if len(assets.voxModels) != 1 {
		t.Fatalf("expected one geometry asset after first commit, got %d", len(assets.voxModels))
	}

	if err := unloadStreamedChunk(cmd, state, coord); err != nil {
		t.Fatalf("unloadStreamedChunk failed: %v", err)
	}
	cmd.app.FlushCommands()
	if len(assets.voxModels) != 1 {
		t.Fatalf("expected cache to retain unreferenced geometry asset, got %d", len(assets.voxModels))
	}

	secondGeometry, hit := state.PreparedGeometryCache.getOrBuild(cacheKey, func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunk)
	})
	if !hit || secondGeometry != preparedGeometry {
		t.Fatal("expected second prepare to reuse cached geometry")
	}
	secondPrepared := streamedPreparedChunk{
		Coord:                                 coord,
		ImportedWorldChunk:                    chunk,
		PreparedImportedWorldGeometry:         secondGeometry,
		PreparedImportedWorldGeometryCacheKey: cacheKey,
	}
	if _, err := commitPreparedStreamedChunk(cmd, assets, state, secondPrepared); err != nil {
		t.Fatalf("second commitPreparedStreamedChunk failed: %v", err)
	}
	secondLoaded := state.LoadedChunks[coord]
	var secondAsset AssetId
	for entity := range secondLoaded.ImportedWorldEntities {
		secondAsset = mustVoxelModelComponentForLevelTest(t, cmd, entity).OverrideGeometry
	}
	if secondAsset != firstAsset {
		t.Fatalf("expected reload to reuse geometry asset %s, got %s", firstAsset, secondAsset)
	}
	if len(assets.voxModels) != 1 {
		t.Fatalf("expected asset count to stay at one after reload, got %d", len(assets.voxModels))
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PreparedGeometryCacheHits <= 0 || state.Metrics.PreparedGeometryAssetReuses <= 0 {
		t.Fatalf("expected cache hit and asset reuse metrics, got %+v", state.Metrics)
	}
}

func TestStreamedPreparedGeometryCacheEvictsUnreferencedAssetsByEntryBudget(t *testing.T) {
	assets := newSpawnTestAssetServer()
	cache := newStreamedPreparedGeometryCache(1)
	chunkA := &content.ImportedWorldChunkDef{
		WorldID:            "world-cache",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	}
	chunkB := &content.ImportedWorldChunkDef{
		WorldID:            "world-cache",
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 1, Y: 0, Z: 0, Value: 2}},
		NonEmptyVoxelCount: 1,
	}
	geometryA, _ := cache.getOrBuild("a", func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunkA)
	})
	assetA, _ := cache.acquireAsset(assets, "a", geometryA)
	cache.releaseAsset(assets, "a")
	geometryB, _ := cache.getOrBuild("b", func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunkB)
	})
	assetB, _ := cache.acquireAsset(assets, "b", geometryB)
	if assetA == (AssetId{}) || assetB == (AssetId{}) {
		t.Fatalf("expected non-empty assets, got %s and %s", assetA, assetB)
	}
	if _, ok := assets.GetVoxelGeometry(assetA); ok {
		t.Fatal("expected first unreferenced geometry asset to be evicted")
	}
	if _, ok := assets.GetVoxelGeometry(assetB); !ok {
		t.Fatal("expected second geometry asset to remain resident")
	}
	stats := cache.snapshot()
	if stats.Entries != 1 || stats.Evictions != 1 {
		t.Fatalf("expected one cache entry and one eviction, got %+v", stats)
	}
}

func TestStreamedRuntimeCommitsImportedWorldFullVisualWithCollisionInsideRadius(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "collision_level"
	state.BaseWorldID = "world-collision"
	state.BaseWorldCollisionEnabled = true
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "collision_level"})
	coord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.CollisionChunks[coord] = struct{}{}

	prepared := streamedPreparedChunk{
		Coord: coord,
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-collision",
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	}
	_, err := commitPreparedStreamedChunk(cmd, newSpawnTestAssetServer(), state, prepared)
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}

	loaded := state.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected one loaded imported chunk, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		if !hasComponentOfType[RigidBodyComponent](cmd, entity) || !hasComponentOfType[ColliderComponent](cmd, entity) || !hasComponentOfType[AABBComponent](cmd, entity) {
			t.Fatalf("expected near imported chunk %d to commit with collision", entity)
		}
	}
}

func TestStreamedRuntimeCommitsImportedWorldDestructionMarkerInsideRadius(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "destruction_level"
	state.BaseWorldID = "world-destruction"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "destruction_level"})
	coord := ChunkCoord{X: 0, Y: 0, Z: 0}
	state.DestructionChunks = make(map[ChunkCoord]struct{})
	state.DestructionChunks[coord] = struct{}{}

	prepared := streamedPreparedChunk{
		Coord: coord,
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-destruction",
			Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	}
	_, err := commitPreparedStreamedChunk(cmd, newSpawnTestAssetServer(), state, prepared)
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}

	loaded := state.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected one loaded imported chunk, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		if !hasComponentOfType[StreamedDestructionResidentComponent](cmd, entity) {
			t.Fatalf("expected near imported chunk %d to commit with destruction residency marker", entity)
		}
		vmc := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		if vmc.ShareTerrainGeometry || vmc.RetainRendererGeometry {
			t.Fatalf("expected destruction-resident imported chunk %d to keep isolated renderer geometry, got %+v", entity, vmc)
		}
	}
}

func TestStreamedRuntimeOmitsImportedWorldDestructionMarkerOutsideRadius(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.LevelID = "destruction_level"
	state.BaseWorldID = "world-destruction"
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: "destruction_level"})

	prepared := streamedPreparedChunk{
		Coord: ChunkCoord{X: 2, Y: 0, Z: 0},
		ImportedWorldChunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-destruction",
			Coord:              content.TerrainChunkCoordDef{X: 2, Y: 0, Z: 0},
			ChunkSize:          16,
			VoxelResolution:    1,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
	}
	_, err := commitPreparedStreamedChunk(cmd, newSpawnTestAssetServer(), state, prepared)
	if err != nil {
		t.Fatalf("commitPreparedStreamedChunk failed: %v", err)
	}

	loaded := state.LoadedChunks[prepared.Coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("expected one loaded imported chunk, got %+v", state.LoadedChunks)
	}
	for entity := range loaded.ImportedWorldEntities {
		if hasComponentOfType[StreamedDestructionResidentComponent](cmd, entity) {
			t.Fatalf("expected far imported chunk %d to commit without destruction residency marker", entity)
		}
	}
}

func TestStreamedRuntimeKeepRadiusRetainsLoadedChunks(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.ChunkSize = 16
	state.StreamingRadius = 1
	state.StreamingKeepRadius = 2
	state.StreamingPrefetchRadius = 1
	kept := ChunkCoord{X: 2, Y: 0, Z: 0}
	unloaded := ChunkCoord{X: 3, Y: 0, Z: 0}
	state.LoadedChunks[kept] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}
	state.LoadedChunks[unloaded] = &streamedLoadedChunk{OwnedEntities: make(map[EntityId]struct{}), ObjectEntities: make(map[string]EntityId)}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 1, KeepRadius: 2},
	)
	cmd.app.FlushCommands()

	updateStreamedLevelObserverSystem(cmd, state)

	if _, ok := state.LoadedChunks[kept]; !ok {
		t.Fatalf("expected chunk %v to stay loaded inside keep radius", kept)
	}
	if _, ok := state.LoadedChunks[unloaded]; ok {
		t.Fatalf("expected chunk %v to unload outside keep radius", unloaded)
	}
	if state.Metrics.DesiredChunkCount != 27 || state.Metrics.KeepChunkCount != 125 {
		t.Fatalf("unexpected desired/keep metrics: %+v", state.Metrics)
	}
}

func TestStreamedRuntimePrefetchRadiusSchedulesAheadContent(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.ChunkSize = 16
	state.StreamingRadius = 1
	state.StreamingKeepRadius = 1
	state.StreamingPrefetchRadius = 2
	prefetched := ChunkCoord{X: 2, Y: 0, Z: 0}
	state.PlacementsByChunk[prefetched] = []streamedPlacementInstance{{
		PlacementID: "prefetch-placement",
		AssetPath:   "prefetch.gkasset",
		Transform:   content.LevelTransformDef{Position: content.Vec3{32, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{Radius: 1, PrefetchRadius: 2},
	)
	cmd.app.FlushCommands()

	updateStreamedLevelObserverSystem(cmd, state)

	if _, ok := state.PendingLoads[prefetched]; !ok {
		t.Fatalf("expected prefetch chunk %v to be scheduled, pending %+v", prefetched, state.PendingLoads)
	}
	if state.Metrics.DesiredChunkCount != 125 || state.Metrics.KeepChunkCount != 27 {
		t.Fatalf("unexpected desired/keep metrics: %+v", state.Metrics)
	}
}

func TestStreamedRuntimeCollisionRadiusCanBeNarrowerThanVisualRadius(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.ChunkSize = 16
	state.StreamingRadius = 2
	state.StreamingKeepRadius = 2
	state.StreamingPrefetchRadius = 2
	state.StreamingCollisionRadius = 1
	state.StreamingDestructionRadius = 0
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{},
	)
	cmd.app.FlushCommands()

	updateStreamedLevelObserverSystem(cmd, state)

	if state.Metrics.DesiredChunkCount != 125 || state.Metrics.KeepChunkCount != 125 {
		t.Fatalf("unexpected desired/keep metrics: %+v", state.Metrics)
	}
	if state.Metrics.CollisionChunkCount != 27 {
		t.Fatalf("expected collision radius 1 to request 27 chunks, got %+v", state.Metrics)
	}
	if state.Metrics.DestructionChunkCount != 27 {
		t.Fatalf("expected unset destruction radius to follow collision radius, got %+v", state.Metrics)
	}
}

func TestStreamedRuntimeDestructionRadiusCanBeNarrowerThanCollisionRadius(t *testing.T) {
	_, cmd, state := newStreamedRuntimeHarness(t)
	state.Initialized = true
	state.ChunkSize = 16
	state.StreamingRadius = 2
	state.StreamingKeepRadius = 2
	state.StreamingPrefetchRadius = 2
	state.StreamingCollisionRadius = 2
	state.StreamingDestructionRadius = 1
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&StreamedLevelObserverComponent{},
	)
	cmd.app.FlushCommands()

	updateStreamedLevelObserverSystem(cmd, state)

	if state.Metrics.CollisionChunkCount != 125 {
		t.Fatalf("expected collision radius 2 to request 125 chunks, got %+v", state.Metrics)
	}
	if state.Metrics.DestructionChunkCount != 27 {
		t.Fatalf("expected destruction radius 1 to request 27 chunks, got %+v", state.Metrics)
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
		app.callSystems(0, execute, DynamicUpdate)
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

func streamedPreparedSectorProxyForTest(coord ChunkCoord) streamedPreparedSectorProxy {
	geometry := volume.NewXBrickMap()
	geometry.SetVoxel(0, 0, 0, 1)
	geometry.ComputeAABB()
	geometry.ClearDirty()
	return streamedPreparedSectorProxy{
		SectorCoord: coord,
		LOD:         content.ImportedWorldLODDef{Level: 1, Kind: "voxel_proxy", NonEmptyVoxelCount: 1},
		Chunk: &content.ImportedWorldChunkDef{
			WorldID:            "world-sector-proxy-test",
			Coord:              content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
			ChunkSize:          4,
			VoxelResolution:    4,
			Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
			NonEmptyVoxelCount: 1,
		},
		PreparedGeometry: geometry,
	}
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
		ChunkSize:       16,
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
		ChunkSize:       16,
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
	terrain.ChunkSize = 16
	if err := content.SaveTerrainSource(path, terrain); err != nil {
		t.Fatalf("SaveTerrainSource failed: %v", err)
	}
}

func terrainEntryForStreamedTest(chunkPath string, manifestPath string, coord content.TerrainChunkCoordDef) content.TerrainChunkEntryDef {
	return content.TerrainChunkEntryDef{
		Coord:              coord,
		ChunkSize:          16,
		VoxelResolution:    1,
		TerrainID:          "terrain-a",
		ChunkPath:          content.AuthorDocumentPath(chunkPath, manifestPath),
		NonEmptyVoxelCount: 1,
	}
}
