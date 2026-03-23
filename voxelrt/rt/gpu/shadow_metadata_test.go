package gpu

import (
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
	if len(manager.shadowDirectionalVolumes) != 1 {
		t.Fatalf("expected 1 directional shadow cull volume, got %d", len(manager.shadowDirectionalVolumes))
	}
	if len(manager.shadowSpotVolumes) != 2 {
		t.Fatalf("expected 2 spot shadow cull volumes, got %d", len(manager.shadowSpotVolumes))
	}
}

func TestCollectShadowCastersUsesDirectionalLightSpaceVolumes(t *testing.T) {
	camera := core.NewCameraState()
	camera.Position = mgl32.Vec3{0, 2, 20}
	dir := mgl32.Vec3{0, -1, -0.25}.Normalize()
	_, volume := buildDirectionalShadowCascade(camera, 1.0, dir, 0, 160)

	inside := testShadowCasterObject(mgl32.Vec3{-2, -1, -35}, mgl32.Vec3{2, 3, -31})
	outside := testShadowCasterObject(mgl32.Vec3{420, -1, -35}, mgl32.Vec3{424, 3, -31})

	shadowObjects := collectShadowCasters([]*core.VoxelObject{inside, outside}, []directionalShadowCullVolume{volume}, nil)
	if len(shadowObjects) != 1 {
		t.Fatalf("expected exactly one directional shadow caster, got %d", len(shadowObjects))
	}
	if shadowObjects[0] != inside {
		t.Fatal("expected only the light-space-relevant object to remain a shadow caster")
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

	shadowObjects := collectShadowCasters([]*core.VoxelObject{inside, outside}, nil, []spotShadowCullVolume{spot})
	if len(shadowObjects) != 1 {
		t.Fatalf("expected exactly one spot shadow caster, got %d", len(shadowObjects))
	}
	if shadowObjects[0] != inside {
		t.Fatal("expected only the object inside the spot shadow volume to remain a shadow caster")
	}
}
