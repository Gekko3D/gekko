package gekko

import (
	"math"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	rootassets "github.com/gekko3d/gekko/assets"
	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type voxelRtModuleTestRenderFeature struct {
	name string
	node string
}

func (f voxelRtModuleTestRenderFeature) Name() string { return f.name }

func (f voxelRtModuleTestRenderFeature) Enabled(*app_rt.App) bool { return true }

func (f voxelRtModuleTestRenderFeature) Setup(*app_rt.App) error { return nil }

func (f voxelRtModuleTestRenderFeature) Resize(*app_rt.App, uint32, uint32) error { return nil }

func (f voxelRtModuleTestRenderFeature) OnSceneBuffersRecreated(*app_rt.App) error { return nil }

func (f voxelRtModuleTestRenderFeature) Update(*app_rt.App) error { return nil }

func (f voxelRtModuleTestRenderFeature) Render(*app_rt.App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f voxelRtModuleTestRenderFeature) Shutdown(*app_rt.App) {}

func (f voxelRtModuleTestRenderFeature) GraphNodeNames() []string {
	if f.node == "" {
		return nil
	}
	return []string{f.node}
}

type voxelRtModuleTestRenderNode struct {
	name  string
	calls *[]string
}

func (n voxelRtModuleTestRenderNode) Name() string { return n.name }

func (n voxelRtModuleTestRenderNode) Enabled(*app_rt.App) bool { return true }

func (n voxelRtModuleTestRenderNode) Setup(*app_rt.App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":setup")
	}
	return nil
}

func (n voxelRtModuleTestRenderNode) Resize(*app_rt.App, uint32, uint32) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":resize")
	}
	return nil
}

func (n voxelRtModuleTestRenderNode) OnSceneBuffersRecreated(*app_rt.App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":recreate")
	}
	return nil
}

func (n voxelRtModuleTestRenderNode) Update(*app_rt.App) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":update")
	}
	return nil
}

func (n voxelRtModuleTestRenderNode) Record(*app_rt.App, *wgpu.CommandEncoder, *app_rt.FrameContext) error {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":record")
	}
	return nil
}

func (n voxelRtModuleTestRenderNode) Shutdown(*app_rt.App) {
	if n.calls != nil {
		*n.calls = append(*n.calls, n.name+":shutdown")
	}
}

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

func TestSyncVoxelRtLightsDerivesSourceRadiusFromLinkedEmitterGeometry(t *testing.T) {
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
	paletteID := server.CreateSimplePalette([4]uint8{255, 220, 96, 255})

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{2, 2, 2},
		},
		&VoxelModelComponent{
			VoxelModel:    modelID,
			VoxelPalette:  paletteID,
			EmitterLinkID: 77,
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LightComponent{
			Type:          LightTypePoint,
			Color:         [3]float32{1, 1, 1},
			Intensity:     4,
			Range:         20,
			CastsShadows:  true,
			EmitterLinkID: 77,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	syncVoxelRtLights(state, cmd)

	if len(state.RtApp.Scene.Lights) != 1 {
		t.Fatalf("expected one synced light, got %d", len(state.RtApp.Scene.Lights))
	}

	got := state.RtApp.Scene.Lights[0].Position[3]
	want := float32(math.Sqrt(0.12) / 2.0)
	if math.Abs(float64(got-want)) > 0.0001 {
		t.Fatalf("expected derived source radius %v, got %v", want, got)
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

func TestVoxelRtSystemOnlyRunsCoreBridgeScopes(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := newVoxelRtStateTest()
	state.RtApp.RegisterFeature(&app_rt.TextFeature{})
	state.RtApp.RegisterFeature(&app_rt.GizmoFeature{})
	state.RtApp.RegisterFeature(&app_rt.ParticlesFeature{})
	state.RtApp.RegisterFeature(&app_rt.WaterFeature{})
	state.RtApp.RegisterFeature(&app_rt.AnalyticMediumFeature{})
	state.RtApp.RegisterFeature(&app_rt.CAVolumeFeature{})
	state.RtApp.RegisterFeature(&app_rt.PlanetBodyFeature{})
	state.RtApp.RegisterFeature(&app_rt.AstronomicalFeature{})
	state.RtApp.RegisterFeature(&app_rt.FarPlanetRingFeature{})
	state.RtApp.RegisterFeature(&app_rt.DebrisMidfieldFeature{})
	state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	state.RtApp.RegisterFeature(&app_rt.SkyboxFeature{})

	voxelRtSystem(nil, state, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	for _, scope := range []string{"Sync Instances", "Sync Lights"} {
		if _, ok := state.RtApp.Profiler.ScopeTimes[scope]; !ok {
			t.Fatalf("expected core voxelRtSystem scope %q", scope)
		}
	}
	for _, scope := range []string{
		"Sync CA",
		"Sync Media",
		"Sync Planet Bodies",
		"Sync Astronomical",
		"Sync Far Planet Rings",
		"Sync Midfield Debris",
		"Sync Water",
		"Sync Particles",
		"Sync Sprites",
		"Sync Skybox",
		"Sync Gizmos",
	} {
		if _, ok := state.RtApp.Profiler.ScopeTimes[scope]; ok {
			t.Fatalf("expected optional bridge scope %q to stay out of voxelRtSystem", scope)
		}
	}
}

func TestVoxelRtSystemGatesTextAndGizmoBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := newVoxelRtStateTest()
	state.RtApp.RegisterFeature(&app_rt.TextFeature{})
	state.RtApp.RegisterFeature(&app_rt.GizmoFeature{})
	state.RtApp.DrawText("ui", 1, 2, 0.5, [4]float32{0, 1, 1, 1})

	cmd.AddEntity(&TextComponent{
		Text:     "overlay",
		Position: [2]float32{12, 24},
		Scale:    0.75,
		Color:    [4]float32{1, 1, 1, 1},
	})
	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&GizmoComponent{
			Type:  GizmoLine,
			Color: [4]float32{1, 0, 0, 1},
			Size:  2,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtTextBridgeSystem(state, cmd)
	voxelRtGizmoBridgeSystem(state, cmd)

	if got := len(state.RtApp.TextResources.Items); got != 2 {
		t.Fatalf("expected enabled text bridge to preserve UI text and append one ECS item, got %d", got)
	}
	if got := state.RtApp.TextResources.Items[0].Text; got != "ui" {
		t.Fatalf("expected existing UI text to survive text bridge, got %q", got)
	}
	if got := state.RtApp.TextResources.Items[1].Text; got != "overlay" {
		t.Fatalf("expected ECS text bridge item to be appended, got %q", got)
	}
	if got := len(state.RtApp.Scene.Gizmos); got != 1 {
		t.Fatalf("expected enabled gizmo bridge to sync one gizmo, got %d", got)
	}

	disabledState := newVoxelRtStateTest()
	disabledState.RtApp.TextResources = &app_rt.TextResources{Items: []core.TextItem{{Text: "stale"}}}
	disabledState.RtApp.Scene.Gizmos = []core.Gizmo{{Type: core.GizmoLine}}

	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtTextBridgeSystem(disabledState, cmd)
	voxelRtGizmoBridgeSystem(disabledState, cmd)

	if got := len(disabledState.RtApp.TextResources.Items); got != 1 {
		t.Fatalf("expected disabled text bridge to leave existing immediate text alone, got %d items", got)
	}
	if got := disabledState.RtApp.TextResources.Items[0].Text; got != "stale" {
		t.Fatalf("expected disabled text bridge not to mutate text resources, got %q", got)
	}
	if got := len(disabledState.RtApp.Scene.Gizmos); got != 0 {
		t.Fatalf("expected disabled gizmo bridge to clear stale gizmos without syncing, got %d gizmos", got)
	}
}

func TestBuildTextBridgeItemsMapsRendererDto(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&TextComponent{
		Text:     "overlay",
		Position: [2]float32{12, 24},
		Scale:    0.75,
		Color:    [4]float32{1, 0.5, 0.25, 1},
	})
	app.FlushCommands()

	items := buildTextBridgeItems(cmd)
	if len(items) != 1 {
		t.Fatalf("expected one text bridge item, got %d", len(items))
	}
	if items[0] != (app_rt.TextOverlayItem{
		Text:     "overlay",
		Position: [2]float32{12, 24},
		Scale:    0.75,
		Color:    [4]float32{1, 0.5, 0.25, 1},
	}) {
		t.Fatalf("unexpected text bridge item: %+v", items[0])
	}
}

func TestBuildGizmoBridgeItemsMapsUserAndLightHelpers(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&LightComponent{
			Type:  LightTypePoint,
			Color: [3]float32{0.25, 0.5, 1},
		},
		&TransformComponent{
			Position: mgl32.Vec3{4, 5, 6},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{1, 2, 3},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&GizmoComponent{
			Type:  GizmoLine,
			Color: [4]float32{1, 0, 0, 1},
			Size:  2,
		},
	)
	app.FlushCommands()

	items := buildGizmoBridgeItems(cmd, true)
	if len(items) != 2 {
		t.Fatalf("expected light helper and user gizmo, got %d", len(items))
	}
	if items[0].Type != core.GizmoSphere || items[0].Color != [4]float32{0.25, 0.5, 1, 0.8} {
		t.Fatalf("unexpected light helper gizmo: %+v", items[0])
	}
	if items[0].ModelMatrix != mgl32.Translate3D(4, 5, 6).Mul4(mgl32.Scale3D(1, 1, 1)) {
		t.Fatalf("unexpected light helper transform")
	}
	if items[1].Type != core.GizmoLine || items[1].Color != [4]float32{1, 0, 0, 1} {
		t.Fatalf("unexpected user gizmo: %+v", items[1])
	}
	if items[1].ModelMatrix != mgl32.Translate3D(1, 2, 3).Mul4(mgl32.Scale3D(1, 1, 1)).Mul4(mgl32.Scale3D(1, 1, 2)) {
		t.Fatalf("unexpected user line transform")
	}

	items = buildGizmoBridgeItems(cmd, false)
	if len(items) != 1 || items[0].Type != core.GizmoLine {
		t.Fatalf("expected only user gizmo when light helpers are disabled, got %+v", items)
	}
}

func TestVoxelRtSystemGatesParticleBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&ParticleEmitterComponent{
			Enabled:         true,
			SpawnRate:       4,
			LifetimeRange:   [2]float32{1, 1},
			StartSpeedRange: [2]float32{0, 0},
			StartSizeRange:  [2]float32{1, 1},
			StartColorMin:   [4]float32{1, 1, 1, 1},
			StartColorMax:   [4]float32{1, 1, 1, 1},
			AtlasCols:       1,
			AtlasRows:       1,
		},
	)
	app.FlushCommands()

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.ParticlesFeature{})
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0}, cmd, nil)
	voxelRtBatchEndSystem(enabledState)
	voxelRtParticlesBridgeSystem(enabledState, nil, &Time{Dt: 1.0}, cmd)

	if got := enabledState.RtApp.ParticleResources.SpawnCount; got != 4 {
		t.Fatalf("expected enabled particle bridge to sync four spawn requests, got %d", got)
	}
	if got := len(enabledState.particlePools); got != 1 {
		t.Fatalf("expected enabled particle bridge to track one emitter pool, got %d", got)
	}

	disabledState := newVoxelRtStateTest()
	disabledState.RtApp.SetParticleSpawnCount(9)
	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0}, cmd, nil)
	voxelRtBatchEndSystem(disabledState)
	voxelRtParticlesBridgeSystem(disabledState, nil, &Time{Dt: 1.0}, cmd)

	if got := disabledState.RtApp.ParticleResources.SpawnCount; got != 0 {
		t.Fatalf("expected disabled particle bridge to clear stale spawn count, got %d", got)
	}
	if got := len(disabledState.particlePools); got != 0 {
		t.Fatalf("expected disabled particle bridge to skip emitter pool sync, got %d pools", got)
	}
}

func TestParticlesSyncMapsEmittersToRendererInput(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	textureID := rootassets.NewID()
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{1, 2, 3},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&ParticleEmitterComponent{
			Enabled:          true,
			SpawnRate:        3,
			LifetimeRange:    [2]float32{0.5, 1.5},
			StartSpeedRange:  [2]float32{2, 4},
			StartSizeRange:   [2]float32{0.25, 0.75},
			StartColorMin:    [4]float32{0.1, 0.2, 0.3, 0.4},
			StartColorMax:    [4]float32{0.5, 0.6, 0.7, 0.8},
			Gravity:          9.8,
			Drag:             0.5,
			ConeAngleDegrees: 35,
			SpriteIndex:      2,
			AtlasCols:        4,
			AtlasRows:        5,
			Texture:          textureID,
			AlphaMode:        SpriteAlphaLuminance,
		},
	)
	app.FlushCommands()
	state := newVoxelRtStateTest()

	spawnReqs, emitters, atlasID := particlesSync(state, &Time{Dt: 1.0}, cmd)

	if atlasID != textureID {
		t.Fatalf("expected atlas %v, got %v", textureID, atlasID)
	}
	if len(spawnReqs) != 3 || len(emitters) != 1 {
		t.Fatalf("expected three spawn requests and one emitter, got requests=%d emitters=%d", len(spawnReqs), len(emitters))
	}
	for i, req := range spawnReqs {
		if req != 0 {
			t.Fatalf("spawn request %d = %d, want emitter index 0", i, req)
		}
	}
	emitter := emitters[0]
	if emitter.Pos != [3]float32{1, 2, 3} || emitter.SpawnCount != 3 {
		t.Fatalf("unexpected emitter position/count: %+v", emitter)
	}
	if emitter.LifeMin != 0.5 || emitter.LifeMax != 1.5 || emitter.SpeedMin != 2 || emitter.SpeedMax != 4 {
		t.Fatalf("unexpected emitter ranges: %+v", emitter)
	}
	if emitter.SizeMin != 0.25 || emitter.SizeMax != 0.75 || emitter.Gravity != 9.8 || emitter.Drag != 0.5 {
		t.Fatalf("unexpected emitter physics: %+v", emitter)
	}
	if emitter.ColorMin != [4]float32{0.1, 0.2, 0.3, 0.4} || emitter.ColorMax != [4]float32{0.5, 0.6, 0.7, 0.8} {
		t.Fatalf("unexpected emitter colors: %+v", emitter)
	}
	if emitter.ConeAngle != 35 || emitter.SpriteIndex != 2 || emitter.AtlasCols != 4 || emitter.AtlasRows != 5 || emitter.AlphaMode != uint32(SpriteAlphaLuminance) {
		t.Fatalf("unexpected emitter atlas fields: %+v", emitter)
	}
}

func TestVoxelRtSystemGatesWaterBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&WaterSurfaceComponent{
			HalfExtents: [2]float32{2, 2},
			Depth:       1,
		},
	)
	app.FlushCommands()

	spriteOnlyState := newVoxelRtStateTest()
	spriteOnlyState.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	if spriteOnlyState.bridgeFeatureEnabled(voxelRtBridgeFeatureWater) {
		t.Fatal("expected sprite-only accumulation feature to leave water bridge disabled")
	}
	voxelRtSystem(nil, spriteOnlyState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtWaterBridgeSystem(spriteOnlyState, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	if _, ok := spriteOnlyState.RtApp.Profiler.ScopeTimes["Sync Water"]; ok {
		t.Fatal("expected sprite-only accumulation feature to skip water sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.WaterFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureWater) {
		t.Fatal("expected registered water feature to enable water bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtWaterBridgeSystem(enabledState, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Water"]; !ok {
		t.Fatal("expected enabled water bridge to record sync scope")
	}

	disabledState := newVoxelRtStateTest()
	disabledState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{
		WaterCount:       2,
		WaterRippleCount: 1,
	}
	clearVoxelRtWater(disabledState)
	if disabledState.RtApp.BufferManager.WaterCount != 0 || disabledState.RtApp.BufferManager.WaterRippleCount != 0 {
		t.Fatalf("expected disabled water bridge to clear stale contribution counts, got water=%d ripples=%d",
			disabledState.RtApp.BufferManager.WaterCount,
			disabledState.RtApp.BufferManager.WaterRippleCount)
	}
}

func TestVoxelRtSystemGatesAnalyticMediaBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&AnalyticMediumComponent{
			Shape:       AnalyticMediumShapeSphere,
			OuterRadius: 8,
			Density:     0.5,
			Color:       [3]float32{0.7, 0.8, 1.0},
			CloudSpeed:  2,
		},
	)
	app.FlushCommands()

	inputs := buildAnalyticMediumInputs(cmd, &Time{Elapsed: 3})
	if len(inputs) != 1 {
		t.Fatalf("expected one analytic medium input, got %d", len(inputs))
	}
	if inputs[0].CloudTime != 6 {
		t.Fatalf("expected cloud time to include elapsed time and speed, got %v", inputs[0].CloudTime)
	}

	disabledState := newVoxelRtStateTest()
	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0 / 60.0, Elapsed: 3}, cmd, nil)
	voxelRtAnalyticMediaBridgeSystem(disabledState, &Time{Dt: 1.0 / 60.0, Elapsed: 3}, cmd)
	if _, ok := disabledState.RtApp.Profiler.ScopeTimes["Sync Media"]; ok {
		t.Fatal("expected missing analytic-media feature to skip media sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.AnalyticMediumFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureAnalyticMedia) {
		t.Fatal("expected registered analytic-media feature to enable media bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0, Elapsed: 3}, cmd, nil)
	voxelRtAnalyticMediaBridgeSystem(enabledState, &Time{Dt: 1.0 / 60.0, Elapsed: 3}, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Media"]; !ok {
		t.Fatal("expected enabled analytic-media bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{AnalyticMediumCount: 2}
	clearVoxelRtAnalyticMedia(clearState)
	if got := clearState.RtApp.BufferManager.AnalyticMediumCount; got != 0 {
		t.Fatalf("expected disabled analytic-media bridge to clear stale count, got %d", got)
	}
}

func TestVoxelRtSystemGatesPlanetBodyBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&PlanetBodyComponent{
			Radius: 64,
		},
	)
	app.FlushCommands()

	disabledState := newVoxelRtStateTest()
	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtPlanetBodyBridgeSystem(disabledState, cmd)
	if _, ok := disabledState.RtApp.Profiler.ScopeTimes["Sync Planet Bodies"]; ok {
		t.Fatal("expected missing planet-body feature to skip planet-body sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.PlanetBodyFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeaturePlanetBodies) {
		t.Fatal("expected registered planet-body feature to enable planet-body bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtPlanetBodyBridgeSystem(enabledState, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Planet Bodies"]; !ok {
		t.Fatal("expected enabled planet-body bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{PlanetBodyCount: 2}
	clearVoxelRtPlanetBodies(clearState)
	if got := clearState.RtApp.BufferManager.PlanetBodyCount; got != 0 {
		t.Fatalf("expected disabled planet-body bridge to clear stale count, got %d", got)
	}
}

func TestVoxelRtSystemGatesAstronomicalBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&AstronomicalVisualComponent{
		Visuals: []AstronomicalVisualRecord{
			{
				BodyID:             "star-a",
				Kind:               AstronomicalVisualStar,
				DirectionViewSpace: [3]float32{0, 0, -1},
				AngularRadiusRad:   0.1,
				DistanceMeters:     1000,
				BodyTint:           [3]float32{1, 0.9, 0.7},
				EmissionStrength:   1,
			},
		},
	})
	app.FlushCommands()

	disabledState := newVoxelRtStateTest()
	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtAstronomicalBridgeSystem(disabledState, cmd)
	if _, ok := disabledState.RtApp.Profiler.ScopeTimes["Sync Astronomical"]; ok {
		t.Fatal("expected missing astronomical feature to skip astronomical sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.AstronomicalFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureAstronomical) {
		t.Fatal("expected registered astronomical feature to enable astronomical bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtAstronomicalBridgeSystem(enabledState, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Astronomical"]; !ok {
		t.Fatal("expected enabled astronomical bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{AstronomicalBodyCount: 2}
	clearVoxelRtAstronomical(clearState)
	if got := clearState.RtApp.BufferManager.AstronomicalBodyCount; got != 0 {
		t.Fatalf("expected disabled astronomical bridge to clear stale count, got %d", got)
	}
}

func TestVoxelRtSystemGatesFarPlanetRingBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&FarPlanetRingVisualComponent{
		Rings: []FarPlanetRingVisualRecord{
			testFarPlanetRingRecord("ring-a", "planet-a", 10),
		},
	})
	app.FlushCommands()

	spriteOnlyState := newVoxelRtStateTest()
	spriteOnlyState.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	if spriteOnlyState.bridgeFeatureEnabled(voxelRtBridgeFeatureFarPlanetRings) {
		t.Fatal("expected sprite-only accumulation feature to leave far-ring bridge disabled")
	}
	voxelRtSystem(nil, spriteOnlyState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtFarPlanetRingBridgeSystem(spriteOnlyState, cmd)
	if _, ok := spriteOnlyState.RtApp.Profiler.ScopeTimes["Sync Far Planet Rings"]; ok {
		t.Fatal("expected sprite-only accumulation feature to skip far-ring sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.FarPlanetRingFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureFarPlanetRings) {
		t.Fatal("expected registered far-ring feature to enable far-ring bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtFarPlanetRingBridgeSystem(enabledState, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Far Planet Rings"]; !ok {
		t.Fatal("expected enabled far-ring bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{FarPlanetRingCount: 2}
	clearVoxelRtFarPlanetRings(clearState)
	if got := clearState.RtApp.BufferManager.FarPlanetRingCount; got != 0 {
		t.Fatalf("expected disabled far-ring bridge to clear stale count, got %d", got)
	}
}

func TestVoxelRtSystemGatesDebrisMidfieldBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&DebrisMidfieldVisualComponent{
		Cells: []DebrisMidfieldCellRecord{
			testDebrisMidfieldRecord("ring-a", "ring-a-1-2-0", 10),
		},
	})
	app.FlushCommands()

	spriteOnlyState := newVoxelRtStateTest()
	spriteOnlyState.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	if spriteOnlyState.bridgeFeatureEnabled(voxelRtBridgeFeatureDebrisMidfield) {
		t.Fatal("expected sprite-only accumulation feature to leave debris-midfield bridge disabled")
	}
	voxelRtSystem(nil, spriteOnlyState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtDebrisMidfieldBridgeSystem(spriteOnlyState, cmd)
	if _, ok := spriteOnlyState.RtApp.Profiler.ScopeTimes["Sync Midfield Debris"]; ok {
		t.Fatal("expected sprite-only accumulation feature to skip debris-midfield sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.DebrisMidfieldFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureDebrisMidfield) {
		t.Fatal("expected registered debris-midfield feature to enable debris-midfield bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtDebrisMidfieldBridgeSystem(enabledState, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Midfield Debris"]; !ok {
		t.Fatal("expected enabled debris-midfield bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{DebrisMidfieldCount: 2}
	clearVoxelRtDebrisMidfield(clearState)
	if got := clearState.RtApp.BufferManager.DebrisMidfieldCount; got != 0 {
		t.Fatalf("expected disabled debris-midfield bridge to clear stale count, got %d", got)
	}
}

func TestVoxelRtSystemGatesCAVolumeBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	volume := &CellularVolumeComponent{
		Resolution:       [3]int{8, 8, 8},
		Type:             CellularSmoke,
		UseIntensity:     true,
		Intensity:        1,
		TickRate:         10,
		_gpuStepsPending: 3,
		_dirty:           true,
	}
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		volume,
	)
	app.FlushCommands()

	disabledState := newVoxelRtStateTest()
	voxelRtSystem(nil, disabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtCAVolumeBridgeSystem(disabledState, &Time{Dt: 1.0 / 60.0}, cmd)
	if _, ok := disabledState.RtApp.Profiler.ScopeTimes["Sync CA"]; ok {
		t.Fatal("expected missing CA volume feature to skip CA sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.CAVolumeFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureCAVolumes) {
		t.Fatal("expected registered CA volume feature to enable CA bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync CA"]; ok {
		t.Fatal("expected broad voxelRtSystem to leave CA sync to the registered bridge system")
	}
	voxelRtCAVolumeBridgeSystem(enabledState, &Time{Dt: 1.0 / 60.0}, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync CA"]; !ok {
		t.Fatal("expected enabled CA bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{
		CAVolumeCount:             2,
		CAVolumeVisibleCount:      1,
		CARequestedVolumeCount:    2,
		CAResolutionClampedCount:  1,
		CADeferredStepVolumeCount: 1,
		CASuspendedVolumeCount:    1,
		CADroppedVolumeCount:      1,
		CATotalScheduledSteps:     4,
		CAAtlasCellCount:          512,
		CAAtlasByteCount:          2048,
		CAVolumeBindingsDirty:     false,
	}
	clearState.RtApp.SetHadCAVolumePass(true)
	staleObject := core.NewVoxelObject()
	clearState.RtApp.Scene.AddObject(staleObject)
	clearState.caVolumeMap[EntityId(42)] = staleObject
	clearState.objectToEntity[staleObject] = EntityId(42)

	clearVoxelRtCAVolumes(clearState)

	if clearState.RtApp.HadCAVolumePass() {
		t.Fatal("expected disabled CA bridge to clear stale pass state")
	}
	if len(clearState.caVolumeMap) != 0 {
		t.Fatalf("expected disabled CA bridge to clear stale scene object map, got %d", len(clearState.caVolumeMap))
	}
	if _, ok := clearState.objectToEntity[staleObject]; ok {
		t.Fatal("expected disabled CA bridge to clear stale object entity lookup")
	}
	if len(clearState.RtApp.Scene.Objects) != 0 {
		t.Fatalf("expected disabled CA bridge to remove stale scene object, got %d objects", len(clearState.RtApp.Scene.Objects))
	}
	if bm := clearState.RtApp.BufferManager; bm.CAVolumeCount != 0 ||
		bm.CAVolumeVisibleCount != 0 ||
		bm.CARequestedVolumeCount != 0 ||
		bm.CAResolutionClampedCount != 0 ||
		bm.CADeferredStepVolumeCount != 0 ||
		bm.CASuspendedVolumeCount != 0 ||
		bm.CADroppedVolumeCount != 0 ||
		bm.CATotalScheduledSteps != 0 ||
		bm.CAAtlasCellCount != 0 ||
		bm.CAAtlasByteCount != 0 ||
		!bm.CAVolumeBindingsDirty {
		t.Fatalf("expected disabled CA bridge to clear stale buffer manager counters, got %+v", bm)
	}
}

func TestVoxelRtSystemGatesSpriteBridgeSyncByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&SpriteComponent{
		Enabled:       true,
		Position:      mgl32.Vec3{0, 0, -4},
		Size:          [2]float32{2, 2},
		Color:         [4]float32{1, 1, 1, 1},
		BillboardMode: BillboardSpherical,
	})
	app.FlushCommands()

	waterOnlyState := newVoxelRtStateTest()
	waterOnlyState.RtApp.RegisterFeature(&app_rt.WaterFeature{})
	if waterOnlyState.bridgeFeatureEnabled(voxelRtBridgeFeatureSprites) {
		t.Fatal("expected water-only accumulation feature to leave sprite bridge disabled")
	}
	voxelRtSystem(nil, waterOnlyState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtBatchEndSystem(waterOnlyState)
	voxelRtSpritesBridgeSystem(waterOnlyState, nil, cmd)
	if _, ok := waterOnlyState.RtApp.Profiler.ScopeTimes["Sync Sprites"]; ok {
		t.Fatal("expected water-only accumulation feature to skip sprite sync scope")
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureSprites) {
		t.Fatal("expected registered sprite feature to enable sprite bridge")
	}
	voxelRtSystem(nil, enabledState, nil, &Time{Dt: 1.0 / 60.0}, cmd, nil)
	voxelRtBatchEndSystem(enabledState)
	voxelRtSpritesBridgeSystem(enabledState, nil, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Sprites"]; !ok {
		t.Fatal("expected enabled sprite bridge to record sync scope")
	}

	clearState := newVoxelRtStateTest()
	clearState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{
		SpriteCount: 2,
		SpriteBatches: []gpu_rt.SpriteRenderBatch{
			{FirstInstance: 0, InstanceCount: 1},
			{FirstInstance: 1, InstanceCount: 1},
		},
	}
	clearVoxelRtSprites(clearState)
	if got := clearState.RtApp.BufferManager.SpriteCount; got != 0 {
		t.Fatalf("expected disabled sprite bridge to clear stale sprite count, got %d", got)
	}
	if got := len(clearState.RtApp.BufferManager.SpriteBatches); got != 0 {
		t.Fatalf("expected disabled sprite bridge to clear stale sprite batches, got %d", got)
	}
}

func TestVoxelRtSkyboxBridgeSyncIsGatedByRegisteredFeature(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerGradient,
		ColorA:     mgl32.Vec3{0.1, 0.2, 0.4},
		ColorB:     mgl32.Vec3{0.8, 0.9, 1.0},
		Opacity:    1,
		Resolution: [2]int{64, 32},
	})
	app.FlushCommands()

	disabledState := newVoxelRtStateTest()
	disabledState.skyboxLayers[EntityId(99)] = SkyboxLayerComponent{LayerType: SkyboxLayerStars}
	disabledState.skyboxSun = SkyboxSunComponent{Intensity: 4}
	if disabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureSkybox) {
		t.Fatal("expected missing skybox feature to leave skybox bridge disabled")
	}
	voxelRtSkyboxBridgeSystem(disabledState, &Time{Dt: 1.0 / 60.0}, cmd)
	if len(disabledState.skyboxLayers) != 0 {
		t.Fatalf("expected disabled skybox bridge to clear cached layers, got %d", len(disabledState.skyboxLayers))
	}
	if disabledState.skyboxSun != (SkyboxSunComponent{}) {
		t.Fatalf("expected disabled skybox bridge to clear cached sun, got %+v", disabledState.skyboxSun)
	}

	enabledState := newVoxelRtStateTest()
	enabledState.RtApp.BufferManager = &gpu_rt.GpuBufferManager{}
	enabledState.RtApp.RenderGraph = app_rt.NewDefaultRenderGraph()
	enabledState.RtApp.RegisterFeature(&app_rt.SkyboxFeature{})
	if !enabledState.bridgeFeatureEnabled(voxelRtBridgeFeatureSkybox) {
		t.Fatal("expected registered skybox feature to enable skybox bridge")
	}
	voxelRtSkyboxBridgeSystem(enabledState, &Time{Dt: 1.0 / 60.0}, cmd)
	if _, ok := enabledState.RtApp.Profiler.ScopeTimes["Sync Skybox"]; !ok {
		t.Fatal("expected enabled skybox bridge to record sync scope")
	}
	if got := len(enabledState.skyboxLayers); got != 1 {
		t.Fatalf("expected enabled skybox bridge to cache one layer, got %d", got)
	}
	if enabledState.RtApp.SkyboxResources == nil || !enabledState.RtApp.SkyboxResources.InputDirty {
		t.Fatal("expected enabled skybox bridge to leave pending renderer skybox input for graph update")
	}
	pending := enabledState.RtApp.SkyboxResources.PendingInput
	if len(pending.Layers) != 1 {
		t.Fatalf("expected one pending renderer skybox layer, got %d", len(pending.Layers))
	}
	if pending.Layers[0].ColorA != [3]float32{0.1, 0.2, 0.4} {
		t.Fatalf("expected pending renderer skybox layer color from ECS, got %+v", pending.Layers[0].ColorA)
	}
	if err := enabledState.RtApp.RenderGraph.Update(enabledState.RtApp); err != nil {
		t.Fatalf("expected render graph update to consume skybox input: %v", err)
	}
	if enabledState.RtApp.SkyboxResources.InputDirty {
		t.Fatal("expected skybox graph update to apply pending renderer input")
	}
}

func TestBuildSkyboxBridgeInputMapsRendererDtoDeterministically(t *testing.T) {
	input, ok := buildSkyboxBridgeInput(
		map[EntityId]SkyboxLayerComponent{
			EntityId(2): {
				LayerType:   SkyboxLayerStars,
				ColorA:      mgl32.Vec3{2, 0, 0},
				ColorB:      mgl32.Vec3{0, 2, 0},
				Offset:      mgl32.Vec3{0, 0, 2},
				Threshold:   0.2,
				Opacity:     0.8,
				Scale:       3,
				Persistence: 0.4,
				Lacunarity:  2.2,
				Seed:        20,
				Octaves:     5,
				BlendMode:   SkyboxBlendAdd,
				Invert:      true,
				Smooth:      true,
				Resolution:  [2]int{128, 64},
				Priority:    1,
			},
			EntityId(1): {
				LayerType:  SkyboxLayerGradient,
				ColorA:     mgl32.Vec3{1, 0, 0},
				Smooth:     false,
				Resolution: [2]int{64, 32},
				Priority:   1,
			},
			EntityId(3): {
				LayerType: SkyboxLayerNebula,
				ColorA:    mgl32.Vec3{3, 0, 0},
				Smooth:    true,
				Priority:  2,
			},
		},
		SkyboxSunComponent{
			Direction:              mgl32.Vec3{0, 1, 0},
			Intensity:              4,
			HaloColor:              mgl32.Vec3{0.1, 0.2, 0.3},
			CoreGlowStrength:       5,
			CoreGlowExponent:       6,
			AtmosphereExponent:     7,
			AtmosphereGlowStrength: 8,
			DiskColor:              mgl32.Vec3{0.4, 0.5, 0.6},
			DiskStrength:           9,
			DiskStart:              0.7,
			DiskEnd:                0.8,
		},
	)
	if !ok {
		t.Fatal("expected skybox bridge input")
	}
	if input.Width != 64 || input.Height != 32 {
		t.Fatalf("expected first sorted valid resolution 64x32, got %dx%d", input.Width, input.Height)
	}
	if input.Smooth {
		t.Fatal("expected smooth=false when any layer disables smoothing")
	}
	if got := len(input.Layers); got != 3 {
		t.Fatalf("expected three layers, got %d", got)
	}
	if input.Layers[0].ColorA != [3]float32{1, 0, 0} || input.Layers[1].ColorA != [3]float32{2, 0, 0} || input.Layers[2].ColorA != [3]float32{3, 0, 0} {
		t.Fatalf("expected priority/entity ordering, got %+v", input.Layers)
	}
	if input.Layers[1].LayerType != uint32(SkyboxLayerStars) || input.Layers[1].BlendMode != uint32(SkyboxBlendAdd) || !input.Layers[1].Invert {
		t.Fatalf("expected layer fields mapped to renderer input, got %+v", input.Layers[1])
	}
	if input.SunDir != [4]float32{0, 1, 0, 4} {
		t.Fatalf("unexpected sun dir params: %+v", input.SunDir)
	}
	if input.SunColor != [4]float32{0.1, 0.2, 0.3, 5} || input.SunParams != [4]float32{6, 7, 8, 0} ||
		input.DiskColor != [4]float32{0.4, 0.5, 0.6, 9} || input.DiskParams != [4]float32{0.7, 0.8, 0, 0} {
		t.Fatalf("unexpected packed sun fields: sunColor=%+v sunParams=%+v diskColor=%+v diskParams=%+v",
			input.SunColor, input.SunParams, input.DiskColor, input.DiskParams)
	}
}

func TestVoxelRtBridgeRegistrySupportsModuleRegistrations(t *testing.T) {
	var textSystemRegistered, gizmoSystemRegistered, analyticBatchedSystemRegistered, waterBatchedSystemRegistered bool
	var planetBatchedSystemRegistered, astronomicalBatchedSystemRegistered bool
	var farRingBatchedSystemRegistered, debrisBatchedSystemRegistered, caBatchedSystemRegistered bool
	var particleAfterBatchSystemRegistered, spriteAfterBatchSystemRegistered, skyboxSystemRegistered bool
	var skyboxRequiresGraphNode bool
	for _, registration := range DefaultVoxelRtBridgeFeatureRegistrations() {
		switch registration.Feature {
		case VoxelRtBridgeFeatureText:
			textSystemRegistered = registration.PreRenderSystem != nil
		case VoxelRtBridgeFeatureGizmos:
			gizmoSystemRegistered = registration.PreRenderSystem != nil
		case VoxelRtBridgeFeatureAnalyticMedia:
			analyticBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureWater:
			waterBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeaturePlanetBodies:
			planetBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureAstronomical:
			astronomicalBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureFarPlanetRings:
			farRingBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureDebrisMidfield:
			debrisBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureCAVolumes:
			caBatchedSystemRegistered = registration.PreRenderBatchedSystem != nil
		case VoxelRtBridgeFeatureParticles:
			particleAfterBatchSystemRegistered = registration.PreRenderAfterBatchSystem != nil
		case VoxelRtBridgeFeatureSprites:
			spriteAfterBatchSystemRegistered = registration.PreRenderAfterBatchSystem != nil
		case VoxelRtBridgeFeatureSkybox:
			skyboxSystemRegistered = registration.PreRenderSystem != nil
			skyboxRequiresGraphNode = len(registration.RequiredGraphNodes) == 1 &&
				registration.RequiredGraphNodes[0] == app_rt.RenderNodeFeatureSkyboxUpdate
		}
	}
	if !textSystemRegistered || !gizmoSystemRegistered || !analyticBatchedSystemRegistered || !waterBatchedSystemRegistered ||
		!planetBatchedSystemRegistered || !astronomicalBatchedSystemRegistered || !farRingBatchedSystemRegistered ||
		!debrisBatchedSystemRegistered || !caBatchedSystemRegistered || !particleAfterBatchSystemRegistered ||
		!spriteAfterBatchSystemRegistered || !skyboxSystemRegistered || !skyboxRequiresGraphNode {
		t.Fatalf("expected default bridge registrations to install systems, got text=%v gizmos=%v analyticBatched=%v waterBatched=%v planetBatched=%v astronomicalBatched=%v farRingBatched=%v debrisBatched=%v caBatched=%v particleAfterBatch=%v spriteAfterBatch=%v skyboxSystem=%v skyboxRequiresGraphNode=%v",
			textSystemRegistered, gizmoSystemRegistered, analyticBatchedSystemRegistered, waterBatchedSystemRegistered,
			planetBatchedSystemRegistered, astronomicalBatchedSystemRegistered, farRingBatchedSystemRegistered,
			debrisBatchedSystemRegistered, caBatchedSystemRegistered, particleAfterBatchSystemRegistered,
			spriteAfterBatchSystemRegistered, skyboxSystemRegistered, skyboxRequiresGraphNode)
	}

	customBridge := VoxelRtBridgeFeature("custom-water-like")
	registry := VoxelRtModule{
		BridgeFeatures: []VoxelRtBridgeFeatureRegistration{
			{
				Feature:            customBridge,
				AppFeatureName:     "water",
				RequiredGraphNodes: []string{app_rt.RenderNodeCoreAccumulation},
			},
		},
	}.bridgeFeatureRegistry()

	state := newVoxelRtStateTest()
	state.bridgeFeatures = registry
	state.RtApp.RegisterFeature(&app_rt.WaterFeature{})
	if !state.bridgeFeatureEnabled(customBridge) {
		t.Fatal("expected custom bridge registration to enable when its feature and graph node are registered")
	}
	if !state.bridgeFeatureEnabled(voxelRtBridgeFeatureWater) {
		t.Fatal("expected module bridge registrations to preserve default bridge gates")
	}

	spriteOnlyState := newVoxelRtStateTest()
	spriteOnlyState.bridgeFeatures = registry
	spriteOnlyState.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
	if spriteOnlyState.bridgeFeatureEnabled(customBridge) {
		t.Fatal("expected shared accumulation node to require the registered bridge feature name")
	}
}

func TestVoxelRtModuleAppliesCustomRenderFeatureAndGraphNode(t *testing.T) {
	const customNode = "feature-custom-test"
	customBridge := VoxelRtBridgeFeature("custom-test")
	rtApp := app_rt.NewApp(nil)
	mod := VoxelRtModule{
		RenderFeatures: []VoxelRtRenderFeature{
			voxelRtModuleTestRenderFeature{name: "custom-render", node: customNode},
		},
		RenderGraphNodes: []VoxelRtRenderNodeSpec{
			{
				Name:  customNode,
				After: []string{app_rt.RenderNodeCoreResolve},
				Node:  voxelRtModuleTestRenderNode{name: customNode},
			},
		},
		BridgeFeatures: []VoxelRtBridgeFeatureRegistration{
			{
				Feature:            customBridge,
				AppFeatureName:     "custom-render",
				RequiredGraphNodes: []string{customNode},
			},
		},
	}

	mod.applyRenderExtensions(rtApp)

	if !rtApp.HasFeature("custom-render") {
		t.Fatal("expected module custom render feature to be registered")
	}
	if !rtApp.HasFeatureGraphNode(customNode) {
		t.Fatal("expected module custom render graph node to be registered")
	}
	ordered, err := rtApp.RenderGraph.Compile()
	if err != nil {
		t.Fatalf("custom render graph did not compile: %v", err)
	}
	if indexOfRenderNode(ordered, app_rt.RenderNodeCoreResolve) > indexOfRenderNode(ordered, customNode) {
		t.Fatalf("expected custom node to run after core resolve, got order %v", renderNodeSpecNames(ordered))
	}

	state := newVoxelRtStateTest()
	state.RtApp = rtApp
	state.bridgeFeatures = mod.bridgeFeatureRegistry()
	if !state.bridgeFeatureEnabled(customBridge) {
		t.Fatal("expected custom bridge to be enabled by module custom feature and graph node")
	}

	missingFeatureState := newVoxelRtStateTest()
	missingFeatureState.bridgeFeatures = mod.bridgeFeatureRegistry()
	if missingFeatureState.bridgeFeatureEnabled(customBridge) {
		t.Fatal("expected custom bridge to require module custom render feature")
	}
}

func TestVoxelRtModuleCustomRenderGraphNodeLifecycle(t *testing.T) {
	const customNode = "feature-custom-lifecycle"
	var calls []string
	rtApp := app_rt.NewApp(nil)
	mod := VoxelRtModule{
		RenderGraphNodes: []VoxelRtRenderNodeSpec{
			{
				Name:  customNode,
				After: []string{app_rt.RenderNodeCoreResolve},
				Node:  voxelRtModuleTestRenderNode{name: customNode, calls: &calls},
			},
		},
	}

	mod.applyRenderExtensions(rtApp)

	if err := rtApp.RenderGraph.Setup(rtApp); err != nil {
		t.Fatalf("custom render graph setup failed: %v", err)
	}
	if err := rtApp.RenderGraph.Resize(rtApp, 640, 480); err != nil {
		t.Fatalf("custom render graph resize failed: %v", err)
	}
	if err := rtApp.RenderGraph.OnSceneBuffersRecreated(rtApp); err != nil {
		t.Fatalf("custom render graph scene-buffer recreation failed: %v", err)
	}
	if err := rtApp.RenderGraph.Update(rtApp); err != nil {
		t.Fatalf("custom render graph update failed: %v", err)
	}
	rtApp.RenderGraph.Shutdown(rtApp)

	want := []string{
		customNode + ":setup",
		customNode + ":resize",
		customNode + ":recreate",
		customNode + ":update",
		customNode + ":shutdown",
	}
	if !sameStringSlices(calls, want) {
		t.Fatalf("custom render graph lifecycle calls = %v, want %v", calls, want)
	}
}

func sameStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOfRenderNode(specs []app_rt.RenderNodeSpec, name string) int {
	for i, spec := range specs {
		if spec.Name == name {
			return i
		}
	}
	return -1
}

func renderNodeSpecNames(specs []app_rt.RenderNodeSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
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

func TestVoxelRtSystemUsesObjectScopedGeometryForTerrainChunksSharingModel(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateCubeModel(32, 5, 32, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{96, 128, 96, 255})
	leftID := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{
			VoxelModel:        modelID,
			VoxelPalette:      paletteID,
			IsTerrainChunk:    true,
			TerrainGroupID:    44,
			TerrainChunkCoord: [3]int{0, 0, 0},
			TerrainChunkSize:  32,
		},
	)
	rightID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{32, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{
			VoxelModel:        modelID,
			VoxelPalette:      paletteID,
			IsTerrainChunk:    true,
			TerrainGroupID:    44,
			TerrainChunkCoord: [3]int{1, 0, 0},
			TerrainChunkSize:  32,
		},
	)
	app.FlushCommands()

	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}
	left := state.instanceMap[leftID]
	right := state.instanceMap[rightID]
	if left == nil || right == nil {
		t.Fatalf("expected both terrain chunks to sync, got left=%v right=%v", left != nil, right != nil)
	}
	if left.XBrickMap == source.XBrickMap || right.XBrickMap == source.XBrickMap {
		t.Fatal("expected terrain chunks to use object-scoped geometry copies")
	}
	if left.XBrickMap == right.XBrickMap {
		t.Fatal("expected neighboring terrain chunks sharing a source model to have distinct runtime maps")
	}
	if !state.instanceObjectScopedGeometry[leftID] || !state.instanceObjectScopedGeometry[rightID] {
		t.Fatal("expected runtime state to mark terrain chunks as object-scoped")
	}
}

func TestEntityLODSelectionSystemUpdatesRuntimeSelection(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := newVoxelRtStateTest()

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -120},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)

	found := false
	MakeQuery1[EntityLODComponent](cmd).Map(func(entityId EntityId, lod *EntityLODComponent) bool {
		found = true
		if !lod.SelectionValid {
			t.Fatalf("expected runtime selection to be valid")
		}
		if lod.ActiveBandIndex != 2 {
			t.Fatalf("expected far band index 2, got %d", lod.ActiveBandIndex)
		}
		if lod.ActiveRepresentation != EntityLODRepresentationImpostor {
			t.Fatalf("expected impostor representation, got %v", lod.ActiveRepresentation)
		}
		if lod.ActiveDistance < 119.9 || lod.ActiveDistance > 120.1 {
			t.Fatalf("expected active distance near 120, got %v", lod.ActiveDistance)
		}
		return false
	})
	if !found {
		t.Fatalf("expected entity LOD component to exist")
	}
}

func TestVoxelRtSystemCapturesEntityLODSelectionForVoxelEntities(t *testing.T) {
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
	paletteID := server.CreateSimplePalette([4]uint8{96, 160, 224, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -75},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 25, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 50, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	selection, ok := state.entityLODSelections[eid]
	if !ok {
		t.Fatalf("expected voxel runtime state to capture entity LOD selection")
	}
	if selection.BandIndex != 2 {
		t.Fatalf("expected band index 2, got %d", selection.BandIndex)
	}
	if selection.Representation != EntityLODRepresentationImpostor {
		t.Fatalf("expected impostor representation, got %v", selection.Representation)
	}
}

func TestVoxelRtSystemUsesSimplifiedGeometryForSimplifiedLOD(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateCubeModel(8, 8, 8, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{120, 220, 160, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -75},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 25, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	obj, ok := state.instanceMap[eid]
	if !ok || obj == nil {
		t.Fatal("expected voxel object to remain synced")
	}
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}
	simplifiedID, simplified, ok := server.entityLODSimplifiedGeometry(modelID, paletteID, &source)
	if !ok || simplified == nil || simplified.XBrickMap == nil {
		t.Fatal("expected simplified geometry")
	}
	if obj.XBrickMap != simplified.XBrickMap {
		t.Fatal("expected renderer object to swap to simplified geometry")
	}
	if obj.Transform.Scale.X() <= VoxelSize {
		t.Fatalf("expected simplified render scale compensation above base voxel size, got %v", obj.Transform.Scale)
	}
	if _, exists := state.loadedModels[simplifiedID]; !exists {
		t.Fatal("expected simplified geometry template to be cached")
	}
	if len(state.runtimeSprites) != 0 {
		t.Fatalf("expected simplified voxel path to avoid runtime sprites, got %d", len(state.runtimeSprites))
	}
}

func TestVoxelRtSystemUsesRuntimeImpostorSpritesForFarLOD(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()
	state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})

	modelID := server.CreateFrameModel(12, 18, 12, 2, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{112, 206, 255, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -150},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if _, ok := state.instanceMap[eid]; ok {
		t.Fatal("expected impostor LOD to suppress voxel object sync")
	}
	if len(state.runtimeSprites) != 1 {
		t.Fatalf("expected one runtime sprite, got %d", len(state.runtimeSprites))
	}
	sprite := state.runtimeSprites[0]
	if sprite.Texture == (AssetId{}) {
		t.Fatal("expected runtime impostor sprite to have a texture")
	}
	if sprite.BillboardMode != BillboardSpherical {
		t.Fatalf("expected spherical billboard, got %v", sprite.BillboardMode)
	}
	if sprite.Unlit {
		t.Fatal("expected impostor sprite to use lit world-sprite shading")
	}
	if sprite.Size[0] <= 0 || sprite.Size[1] <= 0 {
		t.Fatalf("expected positive sprite size, got %v", sprite.Size)
	}
	spriteInstances, batches := spritesSync(state, cmd)
	if len(spriteInstances) != 1 || len(batches) != 1 {
		t.Fatalf("expected runtime sprite sync output, got instances=%d batches=%d", len(spriteInstances), len(batches))
	}
}

func TestVoxelRtSystemKeepsVoxelObjectForImpostorLODWhenSpritesDisabled(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()

	modelID := server.CreateFrameModel(12, 18, 12, 2, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{112, 206, 255, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -150},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if _, ok := state.RtApp.Profiler.ScopeTimes["Sync Sprites"]; ok {
		t.Fatal("expected missing sprite feature to skip sprite sync scope")
	}
	if len(state.runtimeSprites) != 0 {
		t.Fatalf("expected sprite-disabled impostor LOD to avoid runtime sprites, got %d", len(state.runtimeSprites))
	}
	if _, ok := state.instanceMap[eid]; !ok {
		t.Fatal("expected sprite-disabled impostor LOD to keep a voxel renderer object")
	}
}

func TestVoxelRtSystemFallsBackToDotSpritesWhenImpostorGenerationFails(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	server := newVoxelRtAssetServerTest(t)
	state := newVoxelRtStateTest()
	state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})

	modelID := server.CreateVoxelModel(VoxModel{}, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{255, 120, 232, 255})

	cmd.AddEntity(&CameraComponent{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 0, -1},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Aspect:   1,
		Near:     0.1,
		Far:      1000,
	})
	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, -150},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelModel:   modelID,
			VoxelPalette: paletteID,
			PivotMode:    PivotModeCenter,
		},
		&EntityLODComponent{
			Bands: []EntityLODBand{
				{MaxDistance: 10, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationImpostor},
			},
		},
	)
	app.FlushCommands()

	entityLODSelectionSystem(cmd, state)
	voxelRtSystem(nil, state, server, &Time{Dt: 1.0 / 60.0}, cmd, nil)

	if _, ok := state.instanceMap[eid]; ok {
		t.Fatal("expected far fallback path to suppress voxel object sync")
	}
	if len(state.runtimeSprites) != 1 {
		t.Fatalf("expected one dot fallback sprite, got %d", len(state.runtimeSprites))
	}
	sprite := state.runtimeSprites[0]
	if sprite.Texture != server.entityLODDotTexture() {
		t.Fatal("expected fallback sprite to use shared dot texture")
	}
	if sprite.Color[0] >= 1 || sprite.Color[1] >= 1 || sprite.Color[2] >= 1 {
		t.Fatalf("expected dot fallback sprite brightness tint below full white, got %v", sprite.Color)
	}
	if sprite.Size[0] < 2*VoxelSize || sprite.Size[1] < 2*VoxelSize {
		t.Fatalf("expected clamped dot sprite size, got %v", sprite.Size)
	}
}

func TestEntityLODImpostorBaseSizeUsesFull3DBounds(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	modelID := server.CreateCubeModel(2, 2, 40, 1.0)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	vox := &VoxelModelComponent{VoxelModel: modelID, PivotMode: PivotModeCenter}
	transform := &TransformComponent{Scale: mgl32.Vec3{1, 1, 1}}
	size := entityLODImpostorBaseSize(vox, transform, &source)
	extentX, extentY, extentZ := entityLODGeometryExtents(&source)
	baseScale := EffectiveVoxelScale(vox, transform)
	worldX := float32(math.Abs(float64(baseScale.X()))) * extentX
	worldY := float32(math.Abs(float64(baseScale.Y()))) * extentY
	worldZ := float32(math.Abs(float64(baseScale.Z()))) * extentZ
	want := float32(math.Sqrt(float64(worldX*worldX+worldY*worldY+worldZ*worldZ))) * 1.1
	if math.Abs(float64(size-want)) > 1e-4 {
		t.Fatalf("expected billboard size %v from 3D bounds, got %v", want, size)
	}
	if size <= max(worldX, worldY)*1.1 {
		t.Fatalf("expected billboard size to include the dominant Z extent, got %v", size)
	}
}

func TestEntityLODImpostorSpriteSizePreservesProjectedAspect(t *testing.T) {
	server := newVoxelRtAssetServerTest(t)
	modelID := server.CreateCubeModel(24, 8, 8, 1.0)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	size := entityLODImpostorSpriteSize(
		&VoxelModelComponent{VoxelModel: modelID, PivotMode: PivotModeCenter},
		&TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
		&source,
	)
	if size[0] <= size[1] {
		t.Fatalf("expected wide model impostor sprite to preserve projected aspect, got %v", size)
	}
}

func TestEntityLODImpostorBrightnessTintUsesDirectionalFacing(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
	}
	state.RtApp.Scene.AmbientLight = mgl32.Vec3{0.1, 0.1, 0.1}
	state.RtApp.Scene.Lights = []core.Light{
		{
			Direction: [4]float32{0, 0, -1, 0},
			Color:     [4]float32{1, 1, 1, 0.8},
			Params:    [4]float32{0, 0, float32(LightTypeDirectional), 0},
		},
	}

	facing := entityLODImpostorBrightnessTint(state, &TransformComponent{Rotation: mgl32.QuatIdent()})
	turned := entityLODImpostorBrightnessTint(state, &TransformComponent{
		Rotation: mgl32.QuatRotate(mgl32.DegToRad(180), mgl32.Vec3{0, 1, 0}),
	})
	if facing <= turned {
		t.Fatalf("expected front-facing impostor tint %v to exceed turned-away tint %v", facing, turned)
	}
}

func TestEntityLODDotBrightnessTintStaysBelowImpostorTint(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
	}
	state.RtApp.Scene.AmbientLight = mgl32.Vec3{0.1, 0.1, 0.1}
	state.RtApp.Scene.Lights = []core.Light{
		{
			Direction: [4]float32{0, 0, -1, 0},
			Color:     [4]float32{1, 1, 1, 0.8},
			Params:    [4]float32{0, 0, float32(LightTypeDirectional), 0},
		},
	}

	transform := &TransformComponent{Rotation: mgl32.QuatIdent()}
	impostor := entityLODImpostorBrightnessTint(state, transform)
	dot := entityLODDotBrightnessTint(state, transform)
	if dot >= impostor {
		t.Fatalf("expected dot tint %v to stay below impostor tint %v", dot, impostor)
	}
	if dot > 0.6 {
		t.Fatalf("expected dot tint to be capped, got %v", dot)
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

func TestBuildWaterSurfaceInputsNormalizesAndSortsResults(t *testing.T) {
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

	hosts, ripples := buildWaterSurfaceInputs(cmd, nil)
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

func TestBuildWaterSurfaceInputsIncludesResolvedPatches(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 2, 1}},
		&ResolvedWaterPatchComponent{
			Owner:           99,
			PatchIndex:      0,
			Kind:            WaterPatchKindSurface,
			Center:          mgl32.Vec3{5, 2, 1},
			HalfExtents:     [2]float32{3, 2},
			Depth:           4,
			Color:           [3]float32{0.1, 0.2, 0.3},
			AbsorptionColor: [3]float32{0.4, 0.5, 0.6},
			Opacity:         0.7,
			Roughness:       0.15,
			Refraction:      0.25,
			FlowDirection:   [2]float32{1, 0},
			FlowSpeed:       0.9,
			WaveAmplitude:   0.03,
		},
	)
	app.FlushCommands()

	hosts, _ := buildWaterSurfaceInputs(cmd, nil)
	if len(hosts) != 1 {
		t.Fatalf("expected one resolved water host, got %d", len(hosts))
	}
	if hosts[0].Position != (mgl32.Vec3{5, 2, 1}) || hosts[0].Depth != 4 || hosts[0].HalfExtents != ([2]float32{3, 2}) {
		t.Fatalf("unexpected resolved host %+v", hosts[0])
	}
}

func TestBuildPlanetBodyInputsNormalizesAndSortsResults(t *testing.T) {
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

	inputs := buildPlanetBodyInputs(cmd)
	if len(inputs) != 1 {
		t.Fatalf("expected one enabled planet input, got %d", len(inputs))
	}
	if inputs[0].Position != (mgl32.Vec3{8, 9, 10}) {
		t.Fatalf("unexpected input position: %v", inputs[0].Position)
	}
	if inputs[0].Radius != 40 {
		t.Fatalf("expected scaled radius 40, got %v", inputs[0].Radius)
	}
	if inputs[0].OceanRadius != 42 {
		t.Fatalf("expected scaled ocean radius 42, got %v", inputs[0].OceanRadius)
	}
	if inputs[0].HeightAmplitude != 10 {
		t.Fatalf("expected scaled height amplitude 10, got %v", inputs[0].HeightAmplitude)
	}
	if inputs[0].AtmosphereRimWidth != 6 {
		t.Fatalf("expected scaled atmosphere rim width 6, got %v", inputs[0].AtmosphereRimWidth)
	}
	if inputs[0].HeightSteps != 6 {
		t.Fatalf("expected default height steps 6, got %d", inputs[0].HeightSteps)
	}
	if inputs[0].HandoffNearAlt != 18 {
		t.Fatalf("expected scaled handoff near altitude 18, got %v", inputs[0].HandoffNearAlt)
	}
	if inputs[0].HandoffFarAlt != 48 {
		t.Fatalf("expected scaled handoff far altitude 48, got %v", inputs[0].HandoffFarAlt)
	}
}

func TestBuildPlanetBodySurfacePreloadInputsUsesDirectSampleSlice(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	samples := make([]PlanetBakedSurfaceSample, planetBodyBakedSurfaceFaceCount*2*2)
	samples[0].Height = 0.25

	cmd.AddEntity(&PlanetBodySurfacePreloadComponent{
		BakedSurfaceResolution: 2,
		BakedSurfaceSamples:    samples,
	})
	cmd.AddEntity(&PlanetBodySurfacePreloadComponent{
		BakedSurfaceResolution: 2,
		BakedSurfaceSamples:    samples[:2],
	})
	app.FlushCommands()

	inputs := buildPlanetBodySurfacePreloadInputs(cmd)
	if len(inputs) != 1 {
		t.Fatalf("expected one valid preload input, got %d", len(inputs))
	}
	if inputs[0].BakedSurfaceResolution != 2 {
		t.Fatalf("expected preload resolution 2, got %d", inputs[0].BakedSurfaceResolution)
	}
	if len(inputs[0].BakedSurfaceSamples) != len(samples) {
		t.Fatalf("expected preload sample count %d, got %d", len(samples), len(inputs[0].BakedSurfaceSamples))
	}
	if inputs[0].BakedSurfaceSamples[0].Height != 0.25 {
		t.Fatalf("expected direct baked sample data, got %+v", inputs[0].BakedSurfaceSamples[0])
	}
	if inputs[0].BakedSurfaceID == 0 {
		t.Fatal("expected direct preload surface pointer id")
	}
}

func newVoxelRtStateTest() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:         core.NewScene(),
			Camera:        core.NewCameraState(),
			Profiler:      core.NewProfiler(),
			FeatureConfig: app_rt.DefaultFeatureConfig(),
		},
		loadedModels:                 make(map[AssetId]*core.VoxelObject),
		instanceMap:                  make(map[EntityId]*core.VoxelObject),
		instanceGeometrySources:      make(map[EntityId]*volume.XBrickMap),
		instanceObjectScopedGeometry: make(map[EntityId]bool),
		entityLODSelections:          make(map[EntityId]EntityLODSelection),
		runtimeSprites:               make([]SpriteComponent, 0, 8),
		lastMaterialKeys:             make(map[*core.VoxelObject]materialTableCacheKey),
		materialTableCache:           make(map[materialTableCacheKey][]core.Material),
		particlePools:                make(map[EntityId]*particlePool),
		caVolumeMap:                  make(map[EntityId]*core.VoxelObject),
		objectToEntity:               make(map[*core.VoxelObject]EntityId),
		skyboxLayers:                 make(map[EntityId]SkyboxLayerComponent),
		bridgeFeatures:               voxelRtBridgeRegistryFrom(DefaultVoxelRtBridgeFeatureRegistrations()),
	}
}

func newVoxelRtAssetServerTest(t *testing.T) *AssetServer {
	t.Helper()
	return &AssetServer{
		meshes:         make(map[AssetId]MeshAsset),
		materials:      make(map[AssetId]MaterialAsset),
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		samplers:       make(map[AssetId]SamplerAsset),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
}
