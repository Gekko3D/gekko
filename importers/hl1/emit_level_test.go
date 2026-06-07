package hl1

import (
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
	if len(loaded.Lights) != 3 {
		t.Fatalf("lights = %+v", loaded.Lights)
	}
	if loaded.Lights[0].Type != content.LevelLightTypeAmbient || loaded.Lights[1].Type != content.LevelLightTypePoint || loaded.Lights[2].Type != content.LevelLightTypeSpot {
		t.Fatalf("light types = %+v", loaded.Lights)
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
}
