package gpu

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func testShadowCasterObject(minB, maxB mgl32.Vec3) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.WorldAABB = &[2]mgl32.Vec3{minB, maxB}
	return obj
}

func TestUpdateLightsAssignsDirectionalCascadesAndShadowLayers(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Direction: [4]float32{0, -1, -0.25, 0},
				Params:    [4]float32{0, 0, float32(core.LightTypeDirectional), 0},
			},
			{
				Position:  [4]float32{0, 12, 0, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
			{
				Position:  [4]float32{24, 12, 0, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 16.0/9.0)

	dirLight := scene.Lights[0]
	if got, want := dirLight.ShadowMeta, [4]uint32{0, 2, 2, 0}; got != want {
		t.Fatalf("expected directional shadow metadata %v, got %v", want, got)
	}
	if dirLight.DirectionalCascades[0].Params[0] != 48.0 {
		t.Fatalf("expected first cascade far split 48, got %v", dirLight.DirectionalCascades[0].Params[0])
	}
	if dirLight.DirectionalCascades[1].Params[0] != 160.0 {
		t.Fatalf("expected second cascade far split 160, got %v", dirLight.DirectionalCascades[1].Params[0])
	}
	if scene.Lights[1].ShadowMeta != [4]uint32{2, 1, 0, 0} {
		t.Fatalf("expected first spot shadow metadata [2 1 0 0], got %v", scene.Lights[1].ShadowMeta)
	}
	if scene.Lights[2].ShadowMeta != [4]uint32{3, 1, 0, 0} {
		t.Fatalf("expected second spot shadow metadata [3 1 0 0], got %v", scene.Lights[2].ShadowMeta)
	}
	if got := totalShadowLayers(scene.Lights); got != 4 {
		t.Fatalf("expected 4 total shadow layers, got %d", got)
	}
	if manager.ShadowLayerParams[0].EffectiveResolution != 1024 {
		t.Fatalf("expected cascade 0 effective resolution 1024, got %d", manager.ShadowLayerParams[0].EffectiveResolution)
	}
	if manager.ShadowLayerParams[1].EffectiveResolution != 1024 {
		t.Fatalf("expected cascade 1 effective resolution 1024, got %d", manager.ShadowLayerParams[1].EffectiveResolution)
	}
	if manager.ShadowLayerParams[2].Tier != core.ShadowTierHero {
		t.Fatalf("expected first spot tier hero, got %d", manager.ShadowLayerParams[2].Tier)
	}
	if manager.ShadowLayerParams[3].Tier != core.ShadowTierNear {
		t.Fatalf("expected second spot tier near, got %d", manager.ShadowLayerParams[3].Tier)
	}
	if len(manager.shadowDirectionalVolumes) != 1 {
		t.Fatalf("expected 1 directional shadow cull volume, got %d", len(manager.shadowDirectionalVolumes))
	}
	if len(manager.shadowSpotVolumes) != 2 {
		t.Fatalf("expected 2 spot shadow cull volumes, got %d", len(manager.shadowSpotVolumes))
	}
	if len(manager.shadowPointVolumes) != 0 {
		t.Fatalf("expected no point shadow cull volumes, got %d", len(manager.shadowPointVolumes))
	}
}

func TestUpdateLightsUsesConfiguredQualityPreset(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Direction: [4]float32{0, -1, -0.25, 0},
				Params:    [4]float32{0, 0, float32(core.LightTypeDirectional), 0},
			},
			{
				Position:  [4]float32{0, 12, 20, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
			{
				Position:  [4]float32{0, 12, -5, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
			{
				Position:  [4]float32{0, 12, -50, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
			{
				Position:  [4]float32{0, 12, -120, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.LightingQuality = core.LightingQualityPresetConfig(core.LightingQualityPresetPerformance)
	manager.UpdateLights(scene, camera, 16.0/9.0)

	dirLight := scene.Lights[0]
	if dirLight.DirectionalCascades[0].Params[0] != 36.0 {
		t.Fatalf("expected performance first cascade far split 36, got %v", dirLight.DirectionalCascades[0].Params[0])
	}
	if dirLight.DirectionalCascades[1].Params[0] != 112.0 {
		t.Fatalf("expected performance second cascade far split 112, got %v", dirLight.DirectionalCascades[1].Params[0])
	}
	if manager.ShadowLayerParams[2].Tier != core.ShadowTierHero {
		t.Fatalf("expected nearest spot to stay hero, got %d", manager.ShadowLayerParams[2].Tier)
	}
	if manager.ShadowLayerParams[3].Tier != core.ShadowTierNear {
		t.Fatalf("expected second spot to use near tier, got %d", manager.ShadowLayerParams[3].Tier)
	}
	if manager.ShadowLayerParams[4].Tier != core.ShadowTierMedium {
		t.Fatalf("expected third spot to use medium tier, got %d", manager.ShadowLayerParams[4].Tier)
	}
	if manager.ShadowLayerParams[5].Tier != core.ShadowTierFar {
		t.Fatalf("expected farthest spot to use far tier, got %d", manager.ShadowLayerParams[5].Tier)
	}
}

func TestCollectShadowCastersUsesDirectionalLightSpaceVolumes(t *testing.T) {
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 2, 20}
	dir := mgl32.Vec3{0, -1, -0.25}.Normalize()
	_, volume := buildDirectionalShadowCascade(camera, 1.0, dir, 0, 160, 256)

	inside := testShadowCasterObject(mgl32.Vec3{-2, -1, -35}, mgl32.Vec3{2, 3, -31})
	outside := testShadowCasterObject(mgl32.Vec3{420, -1, -35}, mgl32.Vec3{424, 3, -31})

	shadowObjects := collectShadowCasters([]*core.VoxelObject{inside, outside}, []directionalShadowCullVolume{volume}, nil, nil)
	if len(shadowObjects) != 1 {
		t.Fatalf("expected exactly one directional shadow caster, got %d", len(shadowObjects))
	}
	if shadowObjects[0] != inside {
		t.Fatal("expected only the light-space-relevant object to remain a shadow caster")
	}
}

func TestBuildDirectionalShadowCascadeUsesRequestedResolution(t *testing.T) {
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 2, 20}
	dir := mgl32.Vec3{0, -1, -0.25}.Normalize()

	hiResCascade, _ := buildDirectionalShadowCascade(camera, 1.0, dir, 0, 48, 512)
	lowResCascade, _ := buildDirectionalShadowCascade(camera, 1.0, dir, 0, 48, 256)
	if lowResCascade.Params[1] <= hiResCascade.Params[1] {
		t.Fatalf("expected lower-resolution cascade texels to cover more world space, got hi=%v low=%v", hiResCascade.Params[1], lowResCascade.Params[1])
	}
}

func TestBuildDirectionalShadowCascadeTightensAgainstCameraSphereFit(t *testing.T) {
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 2, 20}
	dir := mgl32.Vec3{0, -1, -0.25}.Normalize()

	cascade, _ := buildDirectionalShadowCascade(camera, 16.0/9.0, dir, 0, 48, 1024)
	corners := cameraSliceCorners(camera, 16.0/9.0, maxf(camera.NearPlane(), 0), 48)
	oldRadius := float32(0.0)
	for _, corner := range corners {
		oldRadius = maxf(oldRadius, corner.Sub(camera.Position).Len())
	}
	oldTexel := (oldRadius * 2.0) / 1024.0
	if cascade.Params[1] >= oldTexel {
		t.Fatalf("expected tighter directional fit to reduce texel world size, got old=%v new=%v", oldTexel, cascade.Params[1])
	}
}

func TestBuildShadowUpdatesUsesCadenceAndInvalidation(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{0, 0, float32(core.LightTypeDirectional), 0},
			},
			{
				Position:  [4]float32{8, 12, 0, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)

	initial := manager.BuildShadowUpdates(scene, camera, 0, false)
	if len(initial) != 3 {
		t.Fatalf("expected all 3 shadow layers on first frame, got %d", len(initial))
	}
	manager.RecordShadowUpdates(initial, 0, scene.StructureRevision)

	next := manager.BuildShadowUpdates(scene, camera, 1, false)
	if len(next) != 3 {
		t.Fatalf("expected both directional cascades plus the hero spot at frame 1, got %d", len(next))
	}
	directionalCount := 0
	for _, update := range next {
		if update.Kind == core.ShadowUpdateKindDirectional {
			directionalCount++
		}
	}
	if directionalCount != 2 {
		t.Fatalf("expected both directional cascades to refresh at frame 1, got %d updates", directionalCount)
	}

	manager.VoxelUploadRevision++
	invalidated := manager.BuildShadowUpdates(scene, camera, 2, false)
	if len(invalidated) != 3 {
		t.Fatalf("expected conservative voxel invalidation to refresh every shadow layer, got %d", len(invalidated))
	}
}

func TestBuildShadowUpdatesKeepsDirectionalRefreshWhileCameraMoves(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{0, 0, float32(core.LightTypeDirectional), 0},
			},
			{
				Position:  [4]float32{8, 12, 0, 0},
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{32, float32(math.Cos(math.Pi / 6)), float32(core.LightTypeSpot), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)
	manager.RecordShadowUpdates(manager.BuildShadowUpdates(scene, camera, 0, false), 0, scene.StructureRevision)

	moving := manager.BuildShadowUpdates(scene, camera, 1, true)
	directionalCount := 0
	for _, update := range moving {
		if update.Kind == core.ShadowUpdateKindDirectional {
			directionalCount++
		}
	}
	if directionalCount != 2 {
		t.Fatalf("expected both directional cascades to refresh while camera is moving, got %d", directionalCount)
	}
}

func TestBuildLightsDataForGPUUsesCachedDirectionalCascade(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Direction: [4]float32{0, -1, 0, 0},
				Params:    [4]float32{0, 0, float32(core.LightTypeDirectional), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)
	layer := scene.Lights[0].ShadowMeta[0]
	manager.shadowCacheStates[layer] = shadowCacheState{Initialized: true}
	cached := scene.Lights[0].DirectionalCascades[0]
	cached.Params[0] = 123.0
	manager.shadowCachedCascades[layer] = cached

	data := manager.buildLightsDataForGPU(scene.Lights)
	if len(data) == 0 {
		t.Fatal("expected light buffer data")
	}
	const firstCascadeParamsOffset = 336
	gotBits := binary.LittleEndian.Uint32(data[firstCascadeParamsOffset : firstCascadeParamsOffset+4])
	got := math.Float32frombits(gotBits)
	if got != 123.0 {
		t.Fatalf("expected cached directional cascade params to be uploaded, got %v", got)
	}
}

func TestCollectShadowCastersUsesSpotVolumes(t *testing.T) {
	inside := testShadowCasterObject(mgl32.Vec3{-1, -1, -1}, mgl32.Vec3{1, 1, 1})
	outside := testShadowCasterObject(mgl32.Vec3{18, -1, -1}, mgl32.Vec3{22, 1, 1})

	spot := spotShadowCullVolume{
		Position: mgl32.Vec3{0, 10, 0},
		Dir:      mgl32.Vec3{0, -1, 0},
		Range:    20,
		CosCone:  float32(math.Cos(math.Pi / 8)),
	}

	shadowObjects := collectShadowCasters([]*core.VoxelObject{inside, outside}, nil, []spotShadowCullVolume{spot}, nil)
	if len(shadowObjects) != 1 {
		t.Fatalf("expected exactly one spot shadow caster, got %d", len(shadowObjects))
	}
	if shadowObjects[0] != inside {
		t.Fatal("expected only the object inside the spot shadow volume to remain a shadow caster")
	}
}

func TestUpdateLightsAssignsPointShadowFacesAndLayers(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Position:     [4]float32{4, 8, -2, 1},
				Params:       [4]float32{24, 0, float32(core.LightTypePoint), 0},
				CastsShadows: true,
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)

	light := scene.Lights[0]
	if got, want := light.ShadowMeta, [4]uint32{0, core.PointShadowFaceCount, 0, 0}; got != want {
		t.Fatalf("expected point shadow metadata %v, got %v", want, got)
	}
	if got := totalShadowLayers(scene.Lights); got != core.PointShadowFaceCount {
		t.Fatalf("expected %d total point shadow layers, got %d", core.PointShadowFaceCount, got)
	}
	for face := uint32(0); face < core.PointShadowFaceCount; face++ {
		params := manager.ShadowLayerParams[face]
		if params.Kind != core.ShadowUpdateKindPoint {
			t.Fatalf("expected point shadow kind for face %d, got %d", face, params.Kind)
		}
		if params.CascadeIndex != face {
			t.Fatalf("expected point face index %d, got %d", face, params.CascadeIndex)
		}
		if params.EffectiveResolution != 256 {
			t.Fatalf("expected hero point face resolution 256 for face %d, got %d", face, params.EffectiveResolution)
		}
	}
	if len(manager.shadowPointVolumes) != 1 {
		t.Fatalf("expected 1 point shadow cull volume, got %d", len(manager.shadowPointVolumes))
	}
}

func TestBuildShadowUpdatesRotatesPointFacesAcrossFrames(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Position:     [4]float32{8, 12, 0, 1},
				Params:       [4]float32{32, 0, float32(core.LightTypePoint), 0},
				CastsShadows: true,
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)

	initial := manager.BuildShadowUpdates(scene, camera, 0, false)
	if len(initial) != 3 {
		t.Fatalf("expected 3 point shadow face updates on first hero frame, got %d", len(initial))
	}
	for face, update := range initial {
		if update.Kind != core.ShadowUpdateKindPoint {
			t.Fatalf("expected point shadow update kind, got %d", update.Kind)
		}
		if update.CascadeIndex != uint32(face) {
			t.Fatalf("expected face update %d, got %d", face, update.CascadeIndex)
		}
	}
	manager.RecordShadowUpdates(initial, 0, scene.StructureRevision)

	next := manager.BuildShadowUpdates(scene, camera, 1, false)
	if len(next) != 3 {
		t.Fatalf("expected 3 point shadow face updates on second hero frame, got %d", len(next))
	}
	for i, update := range next {
		wantFace := uint32(i + 3)
		if update.CascadeIndex != wantFace {
			t.Fatalf("expected second frame face update %d, got %d", wantFace, update.CascadeIndex)
		}
	}
	manager.RecordShadowUpdates(next, 1, scene.StructureRevision)

	third := manager.BuildShadowUpdates(scene, camera, 2, false)
	if len(third) != 3 {
		t.Fatalf("expected 3 point shadow face updates on third hero frame, got %d", len(third))
	}
	for face, update := range third {
		if update.CascadeIndex != uint32(face) {
			t.Fatalf("expected third frame to rotate back to oldest faces %d, got %d", face, update.CascadeIndex)
		}
	}
}

func TestUpdateLightsLeavesPointLightsUnshadowedByDefault(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Position: [4]float32{4, 8, -2, 1},
				Params:   [4]float32{24, 0, float32(core.LightTypePoint), 0},
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)

	if got := scene.Lights[0].ShadowMeta; got != [4]uint32{} {
		t.Fatalf("expected point light shadows to stay disabled by default, got %v", got)
	}
	if got := totalShadowLayers(scene.Lights); got != 0 {
		t.Fatalf("expected 0 total shadow layers, got %d", got)
	}
}

func TestUpdateLightsUsesReducedPointShadowResolutionByTier(t *testing.T) {
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Position:     [4]float32{0, 8, 20, 1},
				Params:       [4]float32{24, 0, float32(core.LightTypePoint), 0},
				CastsShadows: true,
			},
			{
				Position:     [4]float32{0, 8, -10, 1},
				Params:       [4]float32{24, 0, float32(core.LightTypePoint), 0},
				CastsShadows: true,
			},
		},
	}
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 3, 20}

	var manager GpuBufferManager
	manager.UpdateLights(scene, camera, 1.0)

	if got := manager.ShadowLayerParams[0].EffectiveResolution; got != 256 {
		t.Fatalf("expected near hero point resolution 256, got %d", got)
	}
	if got := manager.ShadowLayerParams[6].EffectiveResolution; got != 128 {
		t.Fatalf("expected farther point resolution 128, got %d", got)
	}
}

func TestCollectShadowCastersUsesPointVolumes(t *testing.T) {
	inside := testShadowCasterObject(mgl32.Vec3{-1, -1, -1}, mgl32.Vec3{1, 1, 1})
	outside := testShadowCasterObject(mgl32.Vec3{30, -1, -1}, mgl32.Vec3{34, 1, 1})

	point := pointShadowCullVolume{
		Position: mgl32.Vec3{0, 0, 0},
		Range:    12,
	}

	shadowObjects := collectShadowCasters([]*core.VoxelObject{inside, outside}, nil, nil, []pointShadowCullVolume{point})
	if len(shadowObjects) != 1 {
		t.Fatalf("expected exactly one point shadow caster, got %d", len(shadowObjects))
	}
	if shadowObjects[0] != inside {
		t.Fatal("expected only the object inside the point shadow volume to remain a shadow caster")
	}
}
