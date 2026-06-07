package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestProjectedWaterScissorVisibleWater(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 4, 12},
		LookAt:   mgl32.Vec3{0, 0, 0},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	water := gpu_rt.WaterSurfaceHost{
		Position:       mgl32.Vec3{-1, 0, 0},
		HalfExtents:    [2]float32{2, 1.5},
		Depth:          1,
		VisualCellSize: 0.2,
		WaveAmplitude:  0.04,
	}

	rect, ok := projectedWaterScissor(camera, 1280, 720, water)
	if !ok {
		t.Fatal("expected visible water scissor")
	}
	if rect.W == 0 || rect.H == 0 {
		t.Fatalf("expected non-empty scissor, got %+v", rect)
	}
	if rect.W >= 1280 || rect.H >= 720 {
		t.Fatalf("expected sub-rect scissor for bounded visible water, got %+v", rect)
	}
}

func TestProjectedWaterScissorCameraInsideUsesFullscreen(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, -0.5, 0},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	water := gpu_rt.WaterSurfaceHost{
		Position:       mgl32.Vec3{0, 0, 0},
		HalfExtents:    [2]float32{3, 3},
		Depth:          2,
		VisualCellSize: 0.2,
	}

	rect, ok := projectedWaterScissor(camera, 800, 600, water)
	if !ok {
		t.Fatal("expected fullscreen scissor when camera is inside water")
	}
	if rect.X != 0 || rect.Y != 0 || rect.W != 800 || rect.H != 600 {
		t.Fatalf("expected fullscreen scissor, got %+v", rect)
	}
}

func TestProjectedWaterScissorRejectsOffscreenWater(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 4, 12},
		LookAt:   mgl32.Vec3{0, 0, 0},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	water := gpu_rt.WaterSurfaceHost{
		Position:    mgl32.Vec3{80, 0, 0},
		HalfExtents: [2]float32{1, 1},
		Depth:       1,
	}

	if rect, ok := projectedWaterScissor(camera, 1280, 720, water); ok {
		t.Fatalf("expected offscreen water to be rejected, got %+v", rect)
	}
}

func TestBuildWaterRenderCandidatesSkipsInvalidWater(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 4, 12},
		LookAt:   mgl32.Vec3{0, 0, 0},
		Up:       mgl32.Vec3{0, 1, 0},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	waters := []gpu_rt.WaterSurfaceHost{
		{Position: mgl32.Vec3{0, 0, 0}, HalfExtents: [2]float32{2, 2}, Depth: 1},
		{Position: mgl32.Vec3{80, 0, 0}, HalfExtents: [2]float32{1, 1}, Depth: 1},
		{Position: mgl32.Vec3{0, 0, 0}, HalfExtents: [2]float32{0, 1}, Depth: 1},
	}

	candidates := buildWaterRenderCandidates(camera, 640, 360, waters)
	if len(candidates) != 1 {
		t.Fatalf("expected one visible water candidate, got %d", len(candidates))
	}
	if candidates[0].WaterIndex != 0 {
		t.Fatalf("expected first water index, got %d", candidates[0].WaterIndex)
	}
}
