package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func testTiledLightMatrices() (mgl32.Mat4, mgl32.Mat4, mgl32.Mat4) {
	view := mgl32.LookAtV(
		mgl32.Vec3{0, 0, 0},
		mgl32.Vec3{0, 0, -1},
		mgl32.Vec3{0, 1, 0},
	)
	proj := mgl32.Perspective(mgl32.DegToRad(90), 1.0, 0.1, 100.0)
	return view, proj, view.Inv()
}

func TestProjectLocalLightCoverageRejectsLightBehindCamera(t *testing.T) {
	view, proj, _ := testTiledLightMatrices()
	coverage := projectLocalLightCoverage(core.Light{
		Position: [4]float32{0, 0, 1, 0},
		Params:   [4]float32{0.25, 0, float32(core.LightTypePoint), 0},
	}, view, proj, 8, 8)

	if coverage.visible {
		t.Fatalf("expected light fully behind camera to have no tile coverage, got %+v", coverage)
	}
}

func TestProjectLocalLightCoverageUsesFullscreenWhenCameraInside(t *testing.T) {
	view, proj, _ := testTiledLightMatrices()
	coverage := projectLocalLightCoverage(core.Light{
		Position: [4]float32{0.2, 0, -0.1, 0},
		Params:   [4]float32{0.5, 0, float32(core.LightTypePoint), 0},
	}, view, proj, 8, 8)

	if !coverage.visible || !coverage.fullscreen {
		t.Fatalf("expected camera-containing light to stay fullscreen, got %+v", coverage)
	}
	if coverage.minX != 0 || coverage.minY != 0 || coverage.maxX != 7 || coverage.maxY != 7 {
		t.Fatalf("expected fullscreen tile bounds, got %+v", coverage)
	}
}

func TestProjectLocalLightCoverageKeepsEdgeLightsPartial(t *testing.T) {
	view, proj, _ := testTiledLightMatrices()
	coverage := projectLocalLightCoverage(core.Light{
		Position: [4]float32{4.5, 0, -5, 0},
		Params:   [4]float32{1.0, 0, float32(core.LightTypePoint), 0},
	}, view, proj, 8, 8)

	if !coverage.visible {
		t.Fatal("expected near-edge light to remain visible")
	}
	if coverage.fullscreen {
		t.Fatalf("expected near-edge light to avoid fullscreen classification, got %+v", coverage)
	}
	if coverage.maxX != 7 {
		t.Fatalf("expected near-edge light to reach the rightmost tile, got %+v", coverage)
	}
	if coverage.minX <= 0 {
		t.Fatalf("expected near-edge light to stay narrower than fullscreen, got %+v", coverage)
	}
}

func TestEstimateTiledLightMetricsIgnoresLocalLightsFullyBehindCamera(t *testing.T) {
	_, proj, invView := testTiledLightMatrices()
	manager := &GpuBufferManager{
		TileLightTilesX: 1,
		TileLightTilesY: 1,
	}
	scene := &core.Scene{
		Lights: []core.Light{
			{
				Position: [4]float32{0, 0, 1, 0},
				Params:   [4]float32{0.25, 0, float32(core.LightTypePoint), 0},
			},
		},
	}

	manager.EstimateTiledLightMetrics(scene, proj, invView, mgl32.Vec3{})
	if manager.TileLightAvgCount != 0 || manager.TileLightMaxCount != 0 {
		t.Fatalf("expected behind-camera local light to add no tile pressure, got avg=%d max=%d", manager.TileLightAvgCount, manager.TileLightMaxCount)
	}
}
