package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestWaterGPUHostsMapRendererInput(t *testing.T) {
	waters := []WaterSurfaceInput{
		{
			EntityID:        3,
			Position:        mgl32.Vec3{1, 2, 3},
			HalfExtents:     [2]float32{4, 5},
			Depth:           6,
			Color:           [3]float32{0.1, 0.2, 0.3},
			AbsorptionColor: [3]float32{0.4, 0.5, 0.6},
			Opacity:         0.7,
			Roughness:       0.8,
			Refraction:      0.9,
			FlowDirection:   [2]float32{0, 1},
			FlowSpeed:       2,
			WaveAmplitude:   0.25,
		},
	}
	ripples := []WaterRippleInput{
		{
			WaterIndex: 0,
			Position:   mgl32.Vec3{1.5, 2, 3.5},
			Strength:   0.9,
			Age:        0.2,
			Lifetime:   2,
		},
	}

	waterHosts := waterSurfaceGPUHosts(waters)
	rippleHosts := waterRippleGPUHosts(ripples)

	if len(waterHosts) != 1 || len(rippleHosts) != 1 {
		t.Fatalf("expected one water host and one ripple host, got water=%d ripples=%d", len(waterHosts), len(rippleHosts))
	}
	if waterHosts[0] != (gpu.WaterSurfaceHost(waters[0])) {
		t.Fatalf("unexpected water host: %+v", waterHosts[0])
	}
	if rippleHosts[0] != (gpu.WaterRippleHost(ripples[0])) {
		t.Fatalf("unexpected ripple host: %+v", rippleHosts[0])
	}
	if len(waterSurfaceGPUHosts(nil)) != 0 || len(waterRippleGPUHosts(nil)) != 0 {
		t.Fatal("expected empty water host output")
	}
}

func TestClearWaterInputClearsContributionCounts(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{
			WaterCount:       2,
			WaterRippleCount: 3,
		},
	}

	app.ClearWaterInput()

	if app.BufferManager.WaterCount != 0 || app.BufferManager.WaterRippleCount != 0 {
		t.Fatalf("expected water contribution counts cleared, got water=%d ripples=%d",
			app.BufferManager.WaterCount,
			app.BufferManager.WaterRippleCount)
	}
}
