package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestProjectedCAVolumeScissorVisibleVolume(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 2, 8},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volume := gpu_rt.CAVolumeHost{
		Position:   mgl32.Vec3{-1, 0, 0},
		Rotation:   mgl32.QuatIdent(),
		VoxelScale: mgl32.Vec3{0.1, 0.1, 0.1},
		Resolution: [3]uint32{20, 30, 20},
	}

	rect, ok := projectedCAVolumeScissor(camera, 1280, 720, volume)
	if !ok {
		t.Fatal("expected visible CA volume scissor")
	}
	if rect.W == 0 || rect.H == 0 {
		t.Fatalf("expected non-empty scissor, got %+v", rect)
	}
	if rect.W >= 1280 || rect.H >= 720 {
		t.Fatalf("expected sub-rect scissor for off-center visible volume, got %+v", rect)
	}
}

func TestProjectedCAVolumeScissorCameraInsideUsesFullscreen(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0.5, 0.5, 0.5},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volume := gpu_rt.CAVolumeHost{
		Position:   mgl32.Vec3{0, 0, 0},
		Rotation:   mgl32.QuatIdent(),
		VoxelScale: mgl32.Vec3{1, 1, 1},
		Resolution: [3]uint32{2, 2, 2},
	}

	rect, ok := projectedCAVolumeScissor(camera, 800, 600, volume)
	if !ok {
		t.Fatal("expected fullscreen scissor when camera is inside volume")
	}
	if rect.X != 0 || rect.Y != 0 || rect.W != 800 || rect.H != 600 {
		t.Fatalf("expected fullscreen scissor, got %+v", rect)
	}
}
