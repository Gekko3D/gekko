package hl1

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestBuildAndSaveGeneratedLevel(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "levelmap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
}
{
"classname" "info_player_start"
"origin" "8 0 8"
"angle" "90"
}
{
"classname" "light"
"origin" "16 0 8"
"_light" "255 128 64 200"
}
{
"classname" "light_spot"
"origin" "32 0 8"
"_light" "128 128 255 400"
"pitch" "-90"
"angle" "0"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 1, 0), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 0, 16),
			vec3(0, 0, 16),
		},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces:    []FaceHeader{{PlaneID: 0, FirstEdge: 0, EdgeCount: 4, TexInfoID: 0}},
		Edges: []Edge{
			{A: 0, B: 1},
			{A: 1, B: 2},
			{A: 2, B: 3},
			{A: 0, B: 3},
		},
		SurfEdges: []int32{0, 1, 2, -3},
		Models:    []Model{{FirstFace: 0, FaceCount: 1}},
	}))
	opts := ImportOptions{
		GameDir:           dir,
		MapName:           "levelmap",
		OutputRoot:        filepath.Join(dir, "out"),
		ChunkSize:         32,
		VoxelResolution:   0.1,
		EmitLightFixtures: true,
	}
	summary, err := BuildImportSummary(opts)
	if err != nil {
		t.Fatalf("BuildImportSummary failed: %v", err)
	}
	world, err := BuildDebugSurfaceWorld(opts)
	if err != nil {
		t.Fatalf("BuildDebugSurfaceWorld failed: %v", err)
	}
	if err := SaveDebugSurfaceWorld(world); err != nil {
		t.Fatalf("SaveDebugSurfaceWorld failed: %v", err)
	}
	level, err := BuildGeneratedLevel(opts, summary, world.ManifestPath)
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if err := SaveGeneratedLevel(level); err != nil {
		t.Fatalf("SaveGeneratedLevel failed: %v", err)
	}
	loaded, err := content.LoadLevel(level.LevelPath)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	if loaded.BaseWorld == nil || loaded.BaseWorld.ManifestPath != "worlds/levelmap.gkworld" {
		t.Fatalf("base world = %+v", loaded.BaseWorld)
	}
	if len(loaded.Markers) != 1 || loaded.Markers[0].Kind != MarkerKindHL1PlayerSpawn {
		t.Fatalf("markers = %+v", loaded.Markers)
	}
	if loaded.Environment == nil || loaded.Environment.Preset != "fullmoonnight_gi" {
		t.Fatalf("expected HL1 generated level to use night environment, got %+v", loaded.Environment)
	}
	if loaded.Environment.DirectionalCastsShadows == nil || !*loaded.Environment.DirectionalCastsShadows {
		t.Fatalf("expected HL1 generated level to request directional shadows, got %+v", loaded.Environment)
	}
	if loaded.Player == nil || loaded.Player.SpawnKind != MarkerKindHL1PlayerSpawn || loaded.Player.Height != 72*HammerUnitMeters || loaded.Player.EyeHeight != 64*HammerUnitMeters || loaded.Player.Radius != 16*HammerUnitMeters {
		t.Fatalf("expected HL1 player hull metadata, got %+v", loaded.Player)
	}
	if len(loaded.Lights) != 3 {
		t.Fatalf("lights = %+v", loaded.Lights)
	}
	if loaded.Lights[0].Type != content.LevelLightTypeAmbient || loaded.Lights[1].Type != content.LevelLightTypePoint || loaded.Lights[2].Type != content.LevelLightTypeSpot {
		t.Fatalf("light types = %+v", loaded.Lights)
	}
	if !loaded.Lights[1].CastsShadows || !loaded.Lights[2].CastsShadows {
		t.Fatalf("expected imported local lights to cast shadows, got %+v", loaded.Lights)
	}
	if loaded.Lights[2].ConeAngle != 45 {
		t.Fatalf("spot cone = %f", loaded.Lights[2].ConeAngle)
	}
	if len(level.LightFixtureAssets) != 2 {
		t.Fatalf("fixture assets = %+v", level.LightFixtureAssets)
	}
	if len(loaded.Placements) != 2 {
		t.Fatalf("fixture placements = %+v", loaded.Placements)
	}
	if loaded.Lights[1].EmitterLinkID == 0 || loaded.Lights[2].EmitterLinkID == 0 {
		t.Fatalf("expected light emitter links, got %+v", loaded.Lights)
	}
	fixtureAsset, err := content.LoadAsset(level.LightFixtureAssets[0].AssetPath)
	if err != nil {
		t.Fatalf("LoadAsset fixture failed: %v", err)
	}
	if validation := content.ValidateAsset(fixtureAsset, content.AssetValidationOptions{DocumentPath: level.LightFixtureAssets[0].AssetPath}); validation.HasErrors() {
		t.Fatalf("ValidateAsset fixture failed: %s", validation.Error())
	}
	if len(fixtureAsset.Parts) != 1 || fixtureAsset.Parts[0].EmitterLinkID != loaded.Lights[1].EmitterLinkID {
		t.Fatalf("fixture part link mismatch: asset=%+v light=%+v", fixtureAsset.Parts, loaded.Lights[1])
	}
	if validation := content.ValidateLevel(loaded, content.LevelValidationOptions{DocumentPath: level.LevelPath}); validation.HasErrors() {
		t.Fatalf("ValidateLevel failed: %s", validation.Error())
	}
}

func TestBuildGeneratedLevelPlacesGeneratedMDLAssets(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	modelPath := filepath.Join(gameDir, "valve", "models", "filecabinet.mdl")
	spritePath := filepath.Join(gameDir, "valve", "sprites", "flare1.spr")
	mustWriteFile(t, modelPath, syntheticMDL())
	mustWriteFile(t, spritePath, syntheticSPR())
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{
				{
					ClassName:     "monster_furniture",
					WorldPosition: importcommon.Vec3{X: 1, Y: 2, Z: 3},
					KeyValues: map[string]string{
						"model": "models/filecabinet.mdl",
						"angle": "90",
					},
				},
				{
					ClassName:     "env_sprite",
					WorldPosition: importcommon.Vec3{X: 4, Y: 5, Z: 6},
					KeyValues: map[string]string{
						"model": "sprites/flare1.spr",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{
				Kind:    "hl1",
				GameDir: gameDir,
				MapName: "propmap",
			},
		},
	}
	opts := ImportOptions{
		GameDir:         gameDir,
		MapName:         "propmap",
		OutputRoot:      outDir,
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	gameAssets, err := BuildGameAssetImport(opts, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if err := SaveGameAssetImport(gameAssets); err != nil {
		t.Fatalf("SaveGameAssetImport failed: %v", err)
	}
	level, err := BuildGeneratedLevelWithGameAssets(opts, summary, filepath.Join(outDir, "worlds", "propmap.gkworld"), gameAssets)
	if err != nil {
		t.Fatalf("BuildGeneratedLevelWithGameAssets failed: %v", err)
	}
	if len(level.Level.Placements) != 2 {
		t.Fatalf("placements = %+v", level.Level.Placements)
	}
	placement := level.Level.Placements[0]
	if placement.AssetPath != filepath.ToSlash(filepath.Join("hl1_assets", "propmap", "generated", "models", "filecabinet.gkasset")) {
		t.Fatalf("asset path = %q", placement.AssetPath)
	}
	if placement.PlacementMode != content.LevelPlacementModeFree3D || placement.Transform.Position != (content.Vec3{1, 2, 3}) {
		t.Fatalf("placement = %+v", placement)
	}
	if placement.Transform.Rotation == (content.Quat{0, 0, 0, 1}) {
		t.Fatalf("expected angle to produce non-identity rotation, got %+v", placement.Transform.Rotation)
	}
	spritePlacement := level.Level.Placements[1]
	if spritePlacement.ID != "hl1_sprite_flare1_0" || spritePlacement.AssetPath != filepath.ToSlash(filepath.Join("hl1_assets", "propmap", "generated", "sprites", "flare1.gkasset")) {
		t.Fatalf("sprite placement = %+v", spritePlacement)
	}
	if spritePlacement.Transform.Position != (content.Vec3{4, 5, 6}) {
		t.Fatalf("sprite position = %+v", spritePlacement.Transform.Position)
	}
}

func TestBuildGeneratedLevelCanEmitPointProxyLights(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "lightmap",
		OutputRoot:      dir,
		ChunkSize:       32,
		VoxelResolution: 0.1,
		LightMode:       HL1LightModePointProxy,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "lightmap"},
			Entities: []importcommon.Entity{{
				ClassName:     "light_spot",
				WorldPosition: importcommon.Vec3{X: 1, Y: 2, Z: 3},
				KeyValues: map[string]string{
					"_light": "128 128 255 400",
					"pitch":  "-90",
					"angle":  "0",
				},
			}},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "lightmap"},
		},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "lightmap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.Lights) != 2 {
		t.Fatalf("lights = %+v", level.Level.Lights)
	}
	if level.Level.Lights[1].Type != content.LevelLightTypePoint || level.Level.Lights[1].ConeAngle != 0 {
		t.Fatalf("point proxy light = %+v", level.Level.Lights[1])
	}
}

func TestBuildGeneratedLevelCanEmitEmissiveSurfaceLights(t *testing.T) {
	dir := t.TempDir()
	palette := emissivePaletteIndexForToneLevel(emissiveWarmTone, emissiveRampLevels-1)
	opts := ImportOptions{
		MapName:                   "emissivemap",
		OutputRoot:                dir,
		ChunkSize:                 32,
		VoxelResolution:           0.1,
		EmitEmissiveSurfaceLights: true,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "emissivemap"},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "emissivemap"},
		},
	}
	voxelized := VoxelizeResult{
		Materials: []importcommon.Material{{
			ID:           int(palette),
			PaletteIndex: palette,
			BaseColor:    [4]uint8{255, 224, 132, 255},
			EmitsLight:   true,
			Emissive:     3,
		}},
	}
	for x := 0; x < minEmissiveSurfaceLightVoxels; x++ {
		voxelized.Voxels = append(voxelized.Voxels, importcommon.Voxel{
			X: x, Y: 10, Z: 20, Palette: palette, MaterialID: int(palette), SolidKind: "emissive",
		})
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "emissivemap.gkworld"), voxelized)
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.Lights) != 2 {
		t.Fatalf("lights = %+v", level.Level.Lights)
	}
	light := level.Level.Lights[1]
	if light.Type != content.LevelLightTypePoint || light.SourceTag != "hl1:emissive_surface" {
		t.Fatalf("emissive light = %+v", light)
	}
	if light.Intensity <= 0 || light.Range <= 0 || light.CastsShadows {
		t.Fatalf("unexpected emissive light params = %+v", light)
	}
}

func TestBuildGeneratedLevelCanDisableEmissiveSurfaceLights(t *testing.T) {
	dir := t.TempDir()
	palette := emissivePaletteIndexForToneLevel(emissiveWarmTone, emissiveRampLevels-1)
	opts := ImportOptions{
		MapName:         "emissivemap",
		OutputRoot:      dir,
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{Source: importcommon.SourceInfo{MapName: "emissivemap"}},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "emissivemap"},
		},
	}
	voxelized := VoxelizeResult{
		Materials: []importcommon.Material{{
			ID:           int(palette),
			PaletteIndex: palette,
			BaseColor:    [4]uint8{255, 224, 132, 255},
			EmitsLight:   true,
			Emissive:     3,
		}},
	}
	for x := 0; x < minEmissiveSurfaceLightVoxels; x++ {
		voxelized.Voxels = append(voxelized.Voxels, importcommon.Voxel{
			X: x, Y: 0, Z: 0, Palette: palette, MaterialID: int(palette), SolidKind: "emissive",
		})
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "emissivemap.gkworld"), voxelized)
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.Lights) != 1 {
		t.Fatalf("lights = %+v", level.Level.Lights)
	}
}

func TestBuildGeneratedLevelEmitsMovingBrushGameplayMarkers(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "doormap",
		OutputRoot:      dir,
		ChunkSize:       256,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "doormap"},
			Entities: []importcommon.Entity{
				{
					ClassName:    "func_door",
					BrushModelID: 1,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 1, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 5, Y: 6, Z: 7},
					},
					KeyValues: map[string]string{
						"targetname": "door_a",
						"target":     "button_a",
						"speed":      "120",
					},
				},
				{
					ClassName:    "func_button",
					BrushModelID: 2,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 10, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 12, Y: 4, Z: 5},
					},
					KeyValues: map[string]string{
						"target": "door_a",
						"wait":   "1",
					},
				},
				{
					ClassName: "path_corner",
					KeyValues: map[string]string{
						"targetname": "corner_a",
						"target":     "corner_b",
						"speed":      "80",
					},
					WorldPosition: importcommon.Vec3{X: 20, Y: 2, Z: 3},
				},
				{
					ClassName: "path_corner",
					KeyValues: map[string]string{
						"targetname": "corner_b",
					},
					WorldPosition: importcommon.Vec3{X: 22, Y: 2, Z: 3},
				},
				{
					ClassName:    "func_train",
					BrushModelID: 3,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 20, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 22, Y: 4, Z: 5},
					},
					KeyValues: map[string]string{
						"targetname": "train_a",
						"target":     "corner_a",
						"speed":      "100",
					},
				},
				{
					ClassName:    "func_door_rotating",
					BrushModelID: 4,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 30, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 32, Y: 4, Z: 5},
					},
					SourceOrigin:  importcommon.Vec3{X: 30, Y: 2, Z: 3},
					WorldPosition: importcommon.Vec3{X: 30, Y: 3, Z: -2},
					KeyValues: map[string]string{
						"targetname": "rot_a",
						"speed":      "90",
						"distance":   "120",
						"spawnflags": "2",
					},
				},
				{
					ClassName:    "func_healthcharger",
					BrushModelID: 5,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 40, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 42, Y: 4, Z: 5},
					},
					KeyValues: map[string]string{
						"targetname": "health_a",
					},
				},
				{
					ClassName:    "func_recharge",
					BrushModelID: 6,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 50, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 52, Y: 4, Z: 5},
					},
					KeyValues: map[string]string{
						"targetname": "suit_a",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "doormap"},
		},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "doormap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.Markers) != 4 {
		t.Fatalf("markers = %+v", level.Level.Markers)
	}
	if len(level.Level.MovingBrushes) != 4 {
		t.Fatalf("moving brushes = %+v", level.Level.MovingBrushes)
	}
	if len(level.Level.PathNodes) != 2 {
		t.Fatalf("path nodes = %+v", level.Level.PathNodes)
	}
	if len(level.Level.UseTriggers) != 1 {
		t.Fatalf("use triggers = %+v", level.Level.UseTriggers)
	}
	if len(level.Level.Chargers) != 2 {
		t.Fatalf("chargers = %+v", level.Level.Chargers)
	}
	door := level.Level.Markers[0]
	if door.Kind != MarkerKindHL1Door || door.Name != "door_a" {
		t.Fatalf("door marker = %+v", door)
	}
	if door.Transform.Position != (content.Vec3{3, 4, 5}) {
		t.Fatalf("door center = %+v", door.Transform.Position)
	}
	if !hasTag(door.Tags, "hl1_target:button_a") || !hasTag(door.Tags, "bounds_min:1.0000,2.0000,3.0000") {
		t.Fatalf("door tags = %+v", door.Tags)
	}
	button := level.Level.Markers[1]
	if button.Kind != MarkerKindHL1Button || !hasTag(button.Tags, "hl1_target:door_a") {
		t.Fatalf("button marker = %+v", button)
	}
	moving := level.Level.MovingBrushes[0]
	if moving.Kind != MovingBrushKindHL1Door || moving.TargetName != "door_a" || moving.Target != "button_a" {
		t.Fatalf("moving brush = %+v", moving)
	}
	if math.Abs(float64(moving.Speed-120*HammerUnitMeters)) > 1e-5 || moving.MoveDirection != (content.Vec3{1, 0, 0}) {
		t.Fatalf("moving brush motion = %+v", moving)
	}
	trigger := level.Level.UseTriggers[0]
	if trigger.Kind != UseTriggerKindHL1Button || trigger.Target != "door_a" {
		t.Fatalf("use trigger = %+v", trigger)
	}
	buttonBrush := level.Level.MovingBrushes[1]
	if buttonBrush.Kind != MovingBrushKindHL1Button || buttonBrush.Target != "door_a" {
		t.Fatalf("button moving brush = %+v", buttonBrush)
	}
	path := level.Level.PathNodes[0]
	if path.TargetName != "corner_a" || path.Target != "corner_b" || path.Speed != 80*HammerUnitMeters {
		t.Fatalf("path corner = %+v", path)
	}
	train := level.Level.MovingBrushes[2]
	if train.Kind != MovingBrushKindHL1Train || train.MotionKind != "path" || train.PathTarget != "corner_a" || train.Speed != 100*HammerUnitMeters {
		t.Fatalf("train moving brush = %+v", train)
	}
	rotating := level.Level.MovingBrushes[3]
	if rotating.Kind != MovingBrushKindHL1DoorRotating || rotating.MotionKind != "rotate" || rotating.OpenAngle != -120 || rotating.Speed != 90 || rotating.RotationAxis != (content.Vec3{0, 1, 0}) {
		t.Fatalf("rotating moving brush = %+v", rotating)
	}
	health := level.Level.Chargers[0]
	if health.Kind != ChargerKindHL1Health || health.ChargeKind != "health" || health.Capacity != 50 || health.Rate != 15 || health.TargetName != "health_a" {
		t.Fatalf("health charger = %+v", health)
	}
	suit := level.Level.Chargers[1]
	if suit.Kind != ChargerKindHL1Suit || suit.ChargeKind != "armor" || suit.Capacity != 75 || suit.Rate != 15 || suit.TargetName != "suit_a" {
		t.Fatalf("suit charger = %+v", suit)
	}
}

func TestBuildGeneratedLevelEmitsHL1TriggerVolumesAndMultiTargets(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "triggermap",
		OutputRoot:      dir,
		ChunkSize:       256,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "triggermap"},
			Entities: []importcommon.Entity{
				{
					ClassName:    "trigger_once",
					BrushModelID: 3,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 1, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 5, Y: 6, Z: 7},
					},
					KeyValues: map[string]string{
						"targetname": "trigger_a",
						"target":     "manager_a",
						"delay":      "0.5",
					},
				},
				{
					ClassName:    "trigger_multiple",
					BrushModelID: 4,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 10, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 12, Y: 4, Z: 5},
					},
					KeyValues: map[string]string{
						"target": "door_b",
						"wait":   "1.25",
					},
				},
				{
					ClassName:    "trigger_hurt",
					BrushModelID: 5,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 20, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 24, Y: 6, Z: 7},
					},
					KeyValues: map[string]string{
						"targetname": "acid_a",
						"target":     "alarm_a",
						"dmg":        "25",
						"delay":      "0.75",
						"spawnflags": "2",
					},
				},
				{
					ClassName:    "trigger_changelevel",
					BrushModelID: 6,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 30, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 34, Y: 6, Z: 7},
					},
					KeyValues: map[string]string{
						"targetname": "exit_a",
						"map":        "c1a1",
						"landmark":   "lm_a",
					},
				},
				{
					ClassName: "multi_manager",
					KeyValues: map[string]string{
						"targetname": "manager_a",
						"door_a":     "0.25",
						"door_a#1":   "0.75",
						"wait":       "99",
						"not_delay":  "nope",
					},
				},
				{
					ClassName: "trigger_relay",
					KeyValues: map[string]string{
						"targetname":   "relay_a",
						"target":       "door_a",
						"delay":        "0.1",
						"killtarget":   "obsolete_a",
						"triggerstate": "1",
						"spawnflags":   "1",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "triggermap"},
		},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "triggermap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.TriggerVolumes) != 2 {
		t.Fatalf("trigger volumes = %+v", level.Level.TriggerVolumes)
	}
	once := level.Level.TriggerVolumes[0]
	if once.Kind != TriggerVolumeKindHL1TriggerOnce || !once.Once || once.TargetName != "trigger_a" || once.Target != "manager_a" || once.Delay != 0.5 {
		t.Fatalf("trigger_once volume = %+v", once)
	}
	if once.BoundsCenter != (content.Vec3{3, 4, 5}) || once.BoundsHalfExtents != (content.Vec3{2, 2, 2}) {
		t.Fatalf("trigger_once bounds = %+v/%+v", once.BoundsCenter, once.BoundsHalfExtents)
	}
	multiple := level.Level.TriggerVolumes[1]
	if multiple.Kind != TriggerVolumeKindHL1TriggerMultiple || multiple.Once || multiple.Target != "door_b" || multiple.Wait != 1.25 {
		t.Fatalf("trigger_multiple volume = %+v", multiple)
	}
	if len(level.Level.DamageVolumes) != 1 {
		t.Fatalf("damage volumes = %+v", level.Level.DamageVolumes)
	}
	damage := level.Level.DamageVolumes[0]
	if damage.Kind != DamageVolumeKindHL1TriggerHurt || damage.TargetName != "acid_a" || damage.Target != "alarm_a" || damage.Damage != 25 || damage.DamageInterval != 0.5 || damage.Delay != 0.75 || !damage.StartDisabled {
		t.Fatalf("trigger_hurt damage volume = %+v", damage)
	}
	if damage.BoundsCenter != (content.Vec3{22, 4, 5}) || damage.BoundsHalfExtents != (content.Vec3{2, 2, 2}) {
		t.Fatalf("trigger_hurt bounds = %+v/%+v", damage.BoundsCenter, damage.BoundsHalfExtents)
	}
	if len(level.Level.ChangeLevels) != 1 {
		t.Fatalf("changelevels = %+v", level.Level.ChangeLevels)
	}
	change := level.Level.ChangeLevels[0]
	if change.Kind != ChangeLevelKindHL1TriggerChangeLevel || change.TargetName != "exit_a" || change.TargetMap != "c1a1" || change.Landmark != "lm_a" {
		t.Fatalf("trigger_changelevel volume = %+v", change)
	}
	if change.BoundsCenter != (content.Vec3{32, 4, 5}) || change.BoundsHalfExtents != (content.Vec3{2, 2, 2}) {
		t.Fatalf("trigger_changelevel bounds = %+v/%+v", change.BoundsCenter, change.BoundsHalfExtents)
	}
	if len(level.Level.MultiTargets) != 1 {
		t.Fatalf("multi-targets = %+v", level.Level.MultiTargets)
	}
	manager := level.Level.MultiTargets[0]
	if manager.TargetName != "manager_a" || len(manager.Events) != 2 {
		t.Fatalf("multi-manager = %+v", manager)
	}
	if manager.Events[0].Target != "door_a" || manager.Events[0].Delay != 0.25 || manager.Events[1].Target != "door_a" || manager.Events[1].Delay != 0.75 {
		t.Fatalf("multi-manager events = %+v", manager.Events)
	}
	if len(level.Level.TargetRelays) != 1 {
		t.Fatalf("target relays = %+v", level.Level.TargetRelays)
	}
	relay := level.Level.TargetRelays[0]
	if relay.Kind != TargetRelayKindHL1TriggerRelay || relay.TargetName != "relay_a" || relay.Target != "door_a" || relay.Delay != 0.1 || relay.KillTarget != "obsolete_a" || relay.TriggerState != 1 || relay.SpawnFlags != 1 {
		t.Fatalf("trigger relay = %+v", relay)
	}
}

func TestBuildGeneratedLevelEmitsHL1Breakables(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "breakablemap",
		OutputRoot:      dir,
		ChunkSize:       256,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "breakablemap"},
			Entities: []importcommon.Entity{
				{
					ClassName:    "func_breakable",
					BrushModelID: 5,
					BrushWorldBounds: importcommon.Bounds{
						Min: importcommon.Vec3{X: 1, Y: 2, Z: 3},
						Max: importcommon.Vec3{X: 5, Y: 6, Z: 7},
					},
					KeyValues: map[string]string{
						"targetname":       "crate_a",
						"target":           "door_a",
						"health":           "35",
						"material":         "2",
						"spawnobject":      "4",
						"spawnflags":       "1",
						"delay":            "0.25",
						"explodemagnitude": "80",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "breakablemap"},
		},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "breakablemap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.Breakables) != 1 {
		t.Fatalf("breakables = %+v", level.Level.Breakables)
	}
	breakable := level.Level.Breakables[0]
	if breakable.Kind != BreakableKindHL1FuncBreakable || breakable.TargetName != "crate_a" || breakable.Target != "door_a" {
		t.Fatalf("breakable target metadata = %+v", breakable)
	}
	if breakable.Health != 35 || breakable.Material != "2" || breakable.SpawnObject != "4" || breakable.SpawnFlags != 1 || breakable.Delay != 0.25 {
		t.Fatalf("breakable gameplay metadata = %+v", breakable)
	}
	if breakable.BoundsCenter != (content.Vec3{3, 4, 5}) || breakable.BoundsHalfExtents != (content.Vec3{2, 2, 2}) {
		t.Fatalf("breakable bounds = %+v/%+v", breakable.BoundsCenter, breakable.BoundsHalfExtents)
	}
	if !hasTag(breakable.Tags, "hl1_target:door_a") || !hasTag(breakable.Tags, "hl1_health:35") || !hasTag(breakable.Tags, "hl1_explodemagnitude:80") {
		t.Fatalf("breakable tags = %+v", breakable.Tags)
	}
}

func TestBuildGeneratedLevelImportsHL1Pickups(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "pickupmap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{
				{
					ClassName:     "weapon_9mmhandgun",
					WorldPosition: importcommon.Vec3{X: 1, Y: 2, Z: 3},
					KeyValues:     map[string]string{"targetname": "pistol_a"},
				},
				{
					ClassName:     "ammo_9mmclip",
					WorldPosition: importcommon.Vec3{X: 4, Y: 5, Z: 6},
				},
				{
					ClassName:     "item_healthkit",
					WorldPosition: importcommon.Vec3{X: 7, Y: 8, Z: 9},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "pickupmap"},
		},
	}
	levelPath := filepath.Join(dir, "out", "worlds", "pickupmap.gkworld")
	handgunAssetPath := filepath.Join(dir, "out", "hl1_assets", "pickupmap", "generated", "models", "w_9mmhandgun.gkasset")
	level, err := BuildGeneratedLevelWithGameAssets(opts, summary, levelPath, GameAssetImportResult{
		Manifest: &GameAssetManifest{Assets: []GameAssetManifestEntry{{
			Kind:               "model",
			SourceRef:          "models/w_9mmhandgun.mdl",
			GeneratedAssetPath: handgunAssetPath,
			ConvertState:       "generated_voxel_asset",
		}}},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	pickups := level.Level.Pickups
	if len(pickups) != 3 {
		t.Fatalf("pickups = %+v", level.Level.Pickups)
	}
	if pickups[0].Name != "pistol_a" || pickups[0].Transform.Position != (content.Vec3{1, 2, 3}) {
		t.Fatalf("weapon pickup = %+v", pickups[0])
	}
	if pickups[0].Kind != MarkerKindHL1Pickup || pickups[0].ClassName != "weapon_9mmhandgun" || pickups[0].Category != "weapon" || pickups[0].Item != "9mmhandgun" || pickups[0].TargetName != "pistol_a" {
		t.Fatalf("weapon pickup metadata = %+v", pickups[0])
	}
	expectedAssetPath, err := filepath.Rel(filepath.Join(dir, "out"), handgunAssetPath)
	if err != nil {
		t.Fatal(err)
	}
	if pickups[0].AssetPath != filepath.ToSlash(expectedAssetPath) {
		t.Fatalf("weapon pickup asset path = %q, want %q", pickups[0].AssetPath, filepath.ToSlash(expectedAssetPath))
	}
	if !hasTag(pickups[0].Tags, "pickup_category:weapon") || !hasTag(pickups[0].Tags, "pickup_item:9mmhandgun") || !hasTag(pickups[0].Tags, "hl1_targetname:pistol_a") {
		t.Fatalf("weapon pickup tags = %+v", pickups[0].Tags)
	}
	if pickups[1].Category != "ammo" || pickups[1].Item != "9mmclip" || pickups[1].Amount != 17 || pickups[2].Category != "item" || pickups[2].Item != "healthkit" || pickups[2].Amount != 15 {
		t.Fatalf("pickup metadata = %+v %+v", pickups[1], pickups[2])
	}
	if !hasTag(pickups[1].Tags, "pickup_category:ammo") || !hasTag(pickups[2].Tags, "pickup_category:item") {
		t.Fatalf("pickup tags = %+v %+v", pickups[1].Tags, pickups[2].Tags)
	}
}

func TestBuildGeneratedLevelEmitsMovingBrushVoxelAssets(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "doormap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
}
{
"classname" "func_door"
"model" "*1"
"targetname" "door_a"
"speed" "100"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 1, 0), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0), vec3(16, 0, 0), vec3(16, 0, 16), vec3(0, 0, 16),
			vec3(32, 0, 0), vec3(48, 0, 0), vec3(48, 0, 16), vec3(32, 0, 16),
		},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces: []FaceHeader{
			{PlaneID: 0, FirstEdge: 0, EdgeCount: 4, TexInfoID: 0},
			{PlaneID: 0, FirstEdge: 4, EdgeCount: 4, TexInfoID: 0},
		},
		Edges: []Edge{
			{A: 0, B: 1}, {A: 1, B: 2}, {A: 2, B: 3}, {A: 0, B: 3},
			{A: 4, B: 5}, {A: 5, B: 6}, {A: 6, B: 7}, {A: 4, B: 7},
		},
		SurfEdges: []int32{0, 1, 2, -3, 4, 5, 6, -7},
		Models: []Model{
			{FirstFace: 0, FaceCount: 1},
			{FirstFace: 1, FaceCount: 1},
		},
	}))
	opts := ImportOptions{
		GameDir:         dir,
		MapName:         "doormap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	summary, err := BuildImportSummary(opts)
	if err != nil {
		t.Fatalf("BuildImportSummary failed: %v", err)
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "out", "worlds", "doormap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.MovingBrushAssets) != 1 {
		t.Fatalf("moving brush assets = %+v", level.MovingBrushAssets)
	}
	if len(level.Level.MovingBrushes) != 1 || level.Level.MovingBrushes[0].AssetPath == "" {
		t.Fatalf("moving brush missing asset path: %+v", level.Level.MovingBrushes)
	}
	if err := SaveGeneratedLevel(level); err != nil {
		t.Fatalf("SaveGeneratedLevel failed: %v", err)
	}
	asset, err := content.LoadAsset(level.MovingBrushAssets[0].AssetPath)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	if len(asset.Parts) != 1 || asset.Parts[0].Source.VoxelShape == nil || len(asset.Parts[0].Source.VoxelShape.Voxels) == 0 {
		t.Fatalf("moving brush asset payload = %+v", asset.Parts)
	}
}

func TestBuildGeneratedLevelEmitsFuncPlatMovingBrushAsset(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "platmap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
}
{
"classname" "func_plat"
"model" "*1"
"targetname" "lift_a"
"height" "128"
"speed" "200"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 1, 0), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0), vec3(16, 0, 0), vec3(16, 0, 16), vec3(0, 0, 16),
			vec3(32, 0, 0), vec3(48, 0, 0), vec3(48, 0, 16), vec3(32, 0, 16),
		},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces: []FaceHeader{
			{PlaneID: 0, FirstEdge: 0, EdgeCount: 4, TexInfoID: 0},
			{PlaneID: 0, FirstEdge: 4, EdgeCount: 4, TexInfoID: 0},
		},
		Edges: []Edge{
			{A: 0, B: 1}, {A: 1, B: 2}, {A: 2, B: 3}, {A: 0, B: 3},
			{A: 4, B: 5}, {A: 5, B: 6}, {A: 6, B: 7}, {A: 4, B: 7},
		},
		SurfEdges: []int32{0, 1, 2, -3, 4, 5, 6, -7},
		Models: []Model{
			{FirstFace: 0, FaceCount: 1},
			{FirstFace: 1, FaceCount: 1},
		},
	}))
	opts := ImportOptions{
		GameDir:         dir,
		MapName:         "platmap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	summary, err := BuildImportSummary(opts)
	if err != nil {
		t.Fatalf("BuildImportSummary failed: %v", err)
	}
	if len(summary.BakeFaces) != 1 {
		t.Fatalf("expected func_plat excluded from static bake, got %d bake faces", len(summary.BakeFaces))
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "out", "worlds", "platmap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.MovingBrushAssets) != 1 {
		t.Fatalf("moving brush assets = %+v", level.MovingBrushAssets)
	}
	if len(level.Level.MovingBrushes) != 1 {
		t.Fatalf("moving brushes = %+v", level.Level.MovingBrushes)
	}
	plat := level.Level.MovingBrushes[0]
	if plat.Kind != MovingBrushKindHL1Plat || plat.TargetName != "lift_a" {
		t.Fatalf("plat moving brush = %+v", plat)
	}
	if plat.MoveDirection != (content.Vec3{0, 1, 0}) || math.Abs(float64(plat.MoveDistance-128*HammerUnitMeters)) > 1e-5 || math.Abs(float64(plat.Speed-200*HammerUnitMeters)) > 1e-5 {
		t.Fatalf("plat motion = %+v", plat)
	}
	if plat.AssetPath == "" {
		t.Fatalf("plat missing asset path: %+v", plat)
	}
}

func TestBuildGeneratedLevelEmitsLadderVolumes(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "laddermap",
		OutputRoot:      dir,
		ChunkSize:       256,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "laddermap"},
			Entities: []importcommon.Entity{{
				ClassName:    "func_ladder",
				BrushModelID: 3,
				BrushWorldBounds: importcommon.Bounds{
					Min: importcommon.Vec3{X: 1, Y: 2, Z: 3},
					Max: importcommon.Vec3{X: 2, Y: 6, Z: 4},
				},
				KeyValues: map[string]string{"targetname": "ladder_a"},
			}},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "laddermap"},
		},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "laddermap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.LadderVolumes) != 1 {
		t.Fatalf("ladder volumes = %+v", level.Level.LadderVolumes)
	}
	ladder := level.Level.LadderVolumes[0]
	if ladder.Name != "ladder_a" || ladder.SourceTag != "hl1:func_ladder" {
		t.Fatalf("ladder metadata = %+v", ladder)
	}
	if ladder.BoundsCenter != (content.Vec3{1.5, 4, 3.5}) {
		t.Fatalf("ladder center = %+v", ladder.BoundsCenter)
	}
	if ladder.BoundsHalfExtents != (content.Vec3{0.55, 2.05, 0.55}) {
		t.Fatalf("ladder half extents = %+v", ladder.BoundsHalfExtents)
	}
	if ladder.ClimbSpeed != DefaultHL1LadderClimbSpeed || !hasTag(ladder.Tags, "classname:func_ladder") {
		t.Fatalf("ladder tags/speed = %+v", ladder)
	}
}

func TestBuildGeneratedLevelEmitsWaterBodies(t *testing.T) {
	dir := t.TempDir()
	opts := ImportOptions{
		MapName:         "watermap",
		OutputRoot:      dir,
		ChunkSize:       32,
		VoxelResolution: 0.1,
	}
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Source: importcommon.SourceInfo{MapName: "watermap"},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{MapName: "watermap"},
		},
		WorldFaces: []Face{{
			TextureName: "!WATERBLUE",
			Normal:      vec3(0, 0, 1),
			Vertices: []importcommon.Vec3{
				vec3(0, 0, 64),
				vec3(128, 0, 64),
				vec3(128, 128, 64),
				vec3(0, 128, 64),
			},
		}},
	}
	level, err := BuildGeneratedLevel(opts, summary, filepath.Join(dir, "worlds", "watermap.gkworld"))
	if err != nil {
		t.Fatalf("BuildGeneratedLevel failed: %v", err)
	}
	if len(level.Level.WaterBodies) != 1 {
		t.Fatalf("water bodies = %+v", level.Level.WaterBodies)
	}
	if level.Level.WaterBodies[0].SourceTag != "hl1:water" {
		t.Fatalf("water source tag = %q", level.Level.WaterBodies[0].SourceTag)
	}
	if level.Level.WaterBodies[0].DirectLightOcclusion == nil || *level.Level.WaterBodies[0].DirectLightOcclusion != 1 {
		t.Fatalf("water direct light occlusion = %v", level.Level.WaterBodies[0].DirectLightOcclusion)
	}
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
