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
			Profiler: core.NewProfiler(),
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

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

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
	voxelRtSystem(nil, state, server, frameTime, cmd, nil)
	if obj.Transform.Dirty {
		t.Fatalf("expected steady-state sync to keep transform clean")
	}

	obj.Transform.Position = mgl32.Vec3{4, 5, 6}
	obj.Transform.Dirty = false
	voxelRtSystem(nil, state, server, frameTime, cmd, nil)
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

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

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

	voxelRtSystem(nil, state, server, frameTime, cmd, nil)

	if &obj.MaterialTable[0] == initialMaterialPtr {
		t.Fatalf("expected material table slice to be replaced after palette mutation")
	}
	if obj.MaterialTable[1].BaseColor == initialColor {
		t.Fatalf("expected material table contents to reflect mutated palette")
	}
}

func TestVoxelRtSystemCopiesAmbientOcclusionModeToRendererObject(t *testing.T) {
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
	paletteID := server.CreateSimplePalette([4]uint8{64, 96, 128, 255})

	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:           modelID,
			VoxelPalette:         paletteID,
			AmbientOcclusionMode: VoxelAODisabled,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	for _, obj := range state.instanceMap {
		if obj.AmbientOcclusionMode != core.AmbientOcclusionModeDisabled {
			t.Fatalf("expected renderer object AO mode %d, got %d", core.AmbientOcclusionModeDisabled, obj.AmbientOcclusionMode)
		}
	}
}

func TestVoxelRtSystemDefaultsAmbientOcclusionModeToInherited(t *testing.T) {
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
	paletteID := server.CreateSimplePalette([4]uint8{96, 64, 128, 255})

	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if len(state.instanceMap) != 1 {
		t.Fatalf("expected one synced instance, got %d", len(state.instanceMap))
	}
	for _, obj := range state.instanceMap {
		if obj.AmbientOcclusionMode != core.AmbientOcclusionModeDefault {
			t.Fatalf("expected default renderer AO mode %d, got %d", core.AmbientOcclusionModeDefault, obj.AmbientOcclusionMode)
		}
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

func TestVoxelObjectAllowsOcclusionKeepsTerrainAndGroupedChunksEligible(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	terrainEntity := cmd.AddEntity(
		&VoxelModelComponent{IsTerrainChunk: true, ShadowGroupID: 17},
		&AuthoredTerrainChunkRefComponent{},
	)
	importedEntity := cmd.AddEntity(
		&VoxelModelComponent{ShadowGroupID: 23},
		&AuthoredImportedWorldChunkRefComponent{},
	)
	app.FlushCommands()

	if !voxelObjectAllowsOcclusion(cmd, terrainEntity, &VoxelModelComponent{IsTerrainChunk: true, ShadowGroupID: 17}) {
		t.Fatal("expected terrain chunk to remain occlusion-eligible")
	}
	if !voxelObjectAllowsOcclusion(cmd, importedEntity, &VoxelModelComponent{ShadowGroupID: 23}) {
		t.Fatal("expected imported world chunk to remain occlusion-eligible")
	}
}

func TestBuildWaterSurfaceHostsNormalizesAndSortsResults(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{4, 3, 2},
			Scale:    mgl32.Vec3{2, 1, 3},
		},
		&WaterSurfaceComponent{
			HalfExtents:   [2]float32{2, 1},
			Depth:         2,
			FlowDirection: [2]float32{0, 3},
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{1, 2, 3},
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&WaterSurfaceComponent{
			Disabled:    true,
			HalfExtents: [2]float32{3, 3},
			Depth:       1,
		},
	)
	app.FlushCommands()

	hosts, ripples := buildWaterSurfaceHosts(cmd, nil)
	if len(hosts) != 1 {
		t.Fatalf("expected one enabled water host, got %d", len(hosts))
	}
	if len(ripples) != 0 {
		t.Fatalf("expected no ripple hosts, got %d", len(ripples))
	}
	if hosts[0].Position != (mgl32.Vec3{4, 3, 2}) {
		t.Fatalf("unexpected host position: %v", hosts[0].Position)
	}
	if hosts[0].HalfExtents != ([2]float32{4, 3}) {
		t.Fatalf("unexpected host half extents: %v", hosts[0].HalfExtents)
	}
	if hosts[0].FlowDirection != ([2]float32{0, 1}) {
		t.Fatalf("unexpected normalized flow direction: %v", hosts[0].FlowDirection)
	}
}

func TestBuildPlanetBodyHostsNormalizesAndSortsResults(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{8, 9, 10},
			Scale:    mgl32.Vec3{2, 2, 2},
			Rotation: mgl32.QuatRotate(mgl32.DegToRad(15), mgl32.Vec3{0, 1, 0}),
		},
		&PlanetBodyComponent{
			Radius:             20,
			OceanRadius:        21,
			AtmosphereRimWidth: 3,
			HeightAmplitude:    5,
			HandoffNearAlt:     9,
			HandoffFarAlt:      24,
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}},
		&PlanetBodyComponent{
			Disabled: true,
			Radius:   12,
		},
	)
	app.FlushCommands()

	hosts := buildPlanetBodyHosts(cmd)
	if len(hosts) != 1 {
		t.Fatalf("expected one enabled planet host, got %d", len(hosts))
	}
	if hosts[0].Position != (mgl32.Vec3{8, 9, 10}) {
		t.Fatalf("unexpected host position: %v", hosts[0].Position)
	}
	if hosts[0].Radius != 40 {
		t.Fatalf("expected scaled radius 40, got %v", hosts[0].Radius)
	}
	if hosts[0].OceanRadius != 42 {
		t.Fatalf("expected scaled ocean radius 42, got %v", hosts[0].OceanRadius)
	}
	if hosts[0].HeightAmplitude != 10 {
		t.Fatalf("expected scaled height amplitude 10, got %v", hosts[0].HeightAmplitude)
	}
	if hosts[0].AtmosphereRimWidth != 6 {
		t.Fatalf("expected scaled atmosphere rim width 6, got %v", hosts[0].AtmosphereRimWidth)
	}
	if hosts[0].HeightSteps != 6 {
		t.Fatalf("expected default height steps 6, got %d", hosts[0].HeightSteps)
	}
	if hosts[0].HandoffNearAlt != 18 {
		t.Fatalf("expected scaled handoff near altitude 18, got %v", hosts[0].HandoffNearAlt)
	}
	if hosts[0].HandoffFarAlt != 48 {
		t.Fatalf("expected scaled handoff far altitude 48, got %v", hosts[0].HandoffFarAlt)
	}
}

func newVoxelRtStateTest() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Camera:   core.NewCameraState(),
			Profiler: core.NewProfiler(),
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
