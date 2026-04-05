package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestHasLocalLightsIgnoresDirectionalOnly(t *testing.T) {
	manager := &GpuBufferManager{}
	scene := &core.Scene{
		Lights: []core.Light{
			{Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 0}},
		},
	}

	if manager.HasLocalLights(scene) {
		t.Fatal("expected directional-only scene to skip local light work")
	}
}

func TestHasLocalLightsDetectsSpotAndPoint(t *testing.T) {
	manager := &GpuBufferManager{}
	scene := &core.Scene{
		Lights: []core.Light{
			{Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 0}},
			{Params: [4]float32{0, 0, float32(core.LightTypeSpot), 0}},
		},
	}
	if !manager.HasLocalLights(scene) {
		t.Fatal("expected spot light to require tiled local light work")
	}

	scene.Lights = []core.Light{
		{Params: [4]float32{0, 0, float32(core.LightTypePoint), 0}},
	}
	if !manager.HasLocalLights(scene) {
		t.Fatal("expected point light to require tiled local light work")
	}
}

func TestHasVisibleTransparentOverlayUsesVisibleObjects(t *testing.T) {
	manager := &GpuBufferManager{}

	scene := &core.Scene{
		VisibleObjects:            []*core.VoxelObject{core.NewVoxelObject()},
		TransparentVisibleObjects: nil,
	}
	if manager.HasVisibleTransparentOverlay(scene) {
		t.Fatal("expected opaque visible objects to skip transparent overlay")
	}

	scene.TransparentVisibleObjects = []*core.VoxelObject{core.NewVoxelObject()}
	if !manager.HasVisibleTransparentOverlay(scene) {
		t.Fatal("expected visible transparent object to require transparent overlay")
	}

	scene.TransparentVisibleObjects = nil
	if manager.HasVisibleTransparentOverlay(scene) {
		t.Fatal("expected hidden transparent object to be ignored")
	}
}

func TestMaterialTableHasTransparencyDetectsTransparentEntries(t *testing.T) {
	if materialTableHasTransparency(nil) {
		t.Fatal("expected empty material table to be opaque")
	}
	if materialTableHasTransparency([]core.Material{{}}) {
		t.Fatal("expected default material to be opaque")
	}
	if !materialTableHasTransparency([]core.Material{{Transparency: 0.2}}) {
		t.Fatal("expected transparency to be detected")
	}
	if !materialTableHasTransparency([]core.Material{{Transmission: 0.5}}) {
		t.Fatal("expected transmission-only material to be treated as transparent")
	}
}

func TestHasContributionHelpersReflectBoundState(t *testing.T) {
	manager := &GpuBufferManager{}
	if manager.HasParticleContribution() {
		t.Fatal("expected empty particle state to skip accumulation")
	}
	if manager.HasSpriteContribution() {
		t.Fatal("expected empty sprite state to skip accumulation")
	}
	if manager.HasCAVolumeContribution() {
		t.Fatal("expected empty CA volume state to skip accumulation")
	}
	if manager.HasAnalyticMediumContribution() {
		t.Fatal("expected empty analytic medium state to skip accumulation")
	}
}

func TestHasParticleContributionRequiresActivatedSystem(t *testing.T) {
	manager := &GpuBufferManager{ParticleSystemActive: true}
	if manager.HasParticleContribution() {
		t.Fatal("expected missing particle bind groups to keep contribution disabled")
	}
}
