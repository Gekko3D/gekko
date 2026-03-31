package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSyncVoxelRtLightsUsesDaylightDirectionalLightAsSun(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	applyLevelEnvironment(cmd, &content.LevelEnvironmentDef{Preset: "daylight"})
	app.FlushCommands()

	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
		},
	}

	syncVoxelRtLights(state, cmd)

	if len(state.RtApp.Scene.Lights) != 1 {
		t.Fatalf("expected one non-ambient GPU light, got %d", len(state.RtApp.Scene.Lights))
	}
	if state.SunIntensity <= 0 {
		t.Fatalf("expected positive sun intensity, got %f", state.SunIntensity)
	}
	if state.SunDirection.Len() <= 0 {
		t.Fatalf("expected non-zero sun direction, got %v", state.SunDirection)
	}
	if state.RtApp.Scene.AmbientLight.Len() <= 0 {
		t.Fatalf("expected non-zero ambient light, got %v", state.RtApp.Scene.AmbientLight)
	}
}

func TestVoxelRtDebugSystemCyclesNamedModes(t *testing.T) {
	input := &Input{}
	input.JustPressed[KeyF2] = true
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Camera: core.NewCameraState(),
		},
	}

	voxelRtDebugSystem(input, state)
	if got := state.DebugOverlayMode(); got != VoxelRtDebugModeScene {
		t.Fatalf("expected first F2 press to switch to scene debug, got %v", got)
	}

	voxelRtDebugSystem(input, state)
	if got := state.DebugOverlayMode(); got != VoxelRtDebugModeOff {
		t.Fatalf("expected second F2 press to wrap to off, got %v", got)
	}
}

func TestVoxelRtSystemOnlyMarksTransformDirtyOnRealChanges(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{128, 64, 32, 255})

	transform := &TransformComponent{
		Position: mgl32.Vec3{1, 2, 3},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	vox := &VoxelModelComponent{
		VoxelModel:   modelID,
		VoxelPalette: paletteID,
	}
	cmd.AddEntity(transform, vox)
	app.FlushCommands()
	frameTime := &Time{Dt: 1.0 / 60.0}

	voxelRtSystem(nil, state, server, frameTime, cmd)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	var obj *core.VoxelObject
	for _, synced := range state.instanceMap {
		obj = synced
	}
	if obj == nil {
		t.Fatalf("expected synced object")
	}

	obj.Transform.Dirty = false
	voxelRtSystem(nil, state, server, frameTime, cmd)
	if obj.Transform.Dirty {
		t.Fatalf("expected steady-state sync to keep transform clean")
	}

	obj.Transform.Position = mgl32.Vec3{4, 5, 6}
	obj.Transform.Dirty = false
	voxelRtSystem(nil, state, server, frameTime, cmd)
	if !obj.Transform.Dirty {
		t.Fatalf("expected transform change to mark renderer transform dirty")
	}
}

func TestVoxelRtSystemRebuildsMaterialTableWhenPaletteMutatesInPlace(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{32, 64, 128, 255})

	transform := &TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	vox := &VoxelModelComponent{
		VoxelModel:   modelID,
		VoxelPalette: paletteID,
	}
	cmd.AddEntity(transform, vox)
	app.FlushCommands()
	frameTime := &Time{Dt: 1.0 / 60.0}

	voxelRtSystem(nil, state, server, frameTime, cmd)

	var obj *core.VoxelObject
	for _, synced := range state.instanceMap {
		obj = synced
	}
	if obj == nil || len(obj.MaterialTable) <= 1 {
		t.Fatalf("expected synced object with material table")
	}
	initialMaterialPtr := &obj.MaterialTable[0]
	initialColor := obj.MaterialTable[1].BaseColor

	unchanged := server.voxPalettes[paletteID]
	unchanged.VoxPalette[1] = [4]uint8{200, 100, 50, 255}
	server.voxPalettes[paletteID] = unchanged

	voxelRtSystem(nil, state, server, frameTime, cmd)

	if &obj.MaterialTable[0] == initialMaterialPtr {
		t.Fatalf("expected material table slice to be replaced after palette mutation")
	}
	if obj.MaterialTable[1].BaseColor == initialColor {
		t.Fatalf("expected material table contents to reflect mutated palette")
	}
}

func TestSpriteAtlasTextureLooksUpTextureByAtlasKey(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	atlasID := server.CreateTextureFromTexels(
		[]uint8{
			255, 0, 0, 255,
			0, 255, 0, 255,
			0, 0, 255, 255,
			255, 255, 255, 255,
		},
		2, 2, 1, TextureDimension2D, TextureFormatRGBA8Unorm,
	)

	texAsset, ok := spriteAtlasTexture(server, spriteAtlasKey(atlasID))
	if !ok {
		t.Fatalf("expected atlas lookup to succeed")
	}
	if texAsset.Width != 2 || texAsset.Height != 2 {
		t.Fatalf("expected 2x2 texture, got %dx%d", texAsset.Width, texAsset.Height)
	}
	if _, ok := spriteAtlasTexture(server, "not-a-valid-atlas-key"); ok {
		t.Fatalf("expected invalid atlas key lookup to fail")
	}
}

func newVoxelRtStateTest() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Camera:   core.NewCameraState(),
			Profiler: app_rt.NewProfiler(),
		},
		loadedModels:       make(map[AssetId]*core.VoxelObject),
		instanceMap:        make(map[EntityId]*core.VoxelObject),
		lastMaterialKeys:   make(map[*core.VoxelObject]materialTableCacheKey),
		materialTableCache: make(map[materialTableCacheKey][]core.Material),
		particlePools:      make(map[EntityId]*particlePool),
		caVolumeMap:        make(map[EntityId]*core.VoxelObject),
		objectToEntity:     make(map[*core.VoxelObject]EntityId),
		skyboxLayers:       make(map[EntityId]SkyboxLayerComponent),
	}
}

func newVoxelRtAssetServerTest(t *testing.T) *AssetServer {
	t.Helper()
	return &AssetServer{
		meshes:         make(map[AssetId]MeshAsset),
		materials:      make(map[AssetId]MaterialAsset),
		textures:       make(map[AssetId]TextureAsset),
		samplers:       make(map[AssetId]SamplerAsset),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
}
