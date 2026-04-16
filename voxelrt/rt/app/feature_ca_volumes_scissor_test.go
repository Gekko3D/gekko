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

func TestProjectedCAVolumeScissorRejectsOffscreenVolume(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 2, 8},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volume := gpu_rt.CAVolumeHost{
		Position:   mgl32.Vec3{400, 0, 0},
		Rotation:   mgl32.QuatIdent(),
		VoxelScale: mgl32.Vec3{0.1, 0.1, 0.1},
		Resolution: [3]uint32{12, 18, 12},
		Intensity:  1,
	}

	if rect, ok := projectedCAVolumeScissor(camera, 1280, 720, volume); ok {
		t.Fatalf("expected offscreen volume to be rejected, got %+v", rect)
	}
}

func TestBuildCAVolumeRenderCandidatesSkipsInvisibleAndOffscreenVolumes(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 2, 8},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volumes := []gpu_rt.CAVolumeHost{
		{
			EntityID:   10,
			Position:   mgl32.Vec3{0, 0, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.15, 0.15, 0.15},
			Resolution: [3]uint32{16, 20, 16},
			Intensity:  1,
		},
		{
			EntityID:   11,
			Position:   mgl32.Vec3{0, 0, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.15, 0.15, 0.15},
			Resolution: [3]uint32{16, 20, 16},
			Intensity:  0,
		},
		{
			EntityID:   12,
			Position:   mgl32.Vec3{300, 0, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.15, 0.15, 0.15},
			Resolution: [3]uint32{16, 20, 16},
			Intensity:  1,
		},
	}

	candidates := buildCAVolumeRenderCandidates(camera, 640, 360, volumes)
	if len(candidates) != 1 {
		t.Fatalf("expected only one renderable candidate, got %d", len(candidates))
	}
	if candidates[0].EntityID != 10 {
		t.Fatalf("expected entity 10 to survive filtering, got %d", candidates[0].EntityID)
	}
}

func TestBuildCAVolumeRenderCandidatesSortsBySurfaceDistanceNotCenter(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 4, 18},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volumes := []gpu_rt.CAVolumeHost{
		{
			EntityID:   1,
			Position:   mgl32.Vec3{0, 0, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.12, 0.12, 0.12},
			Resolution: [3]uint32{10, 18, 10},
			Intensity:  1,
		},
		{
			EntityID:   2,
			Position:   mgl32.Vec3{-2, -2, -10},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.5, 0.5, 0.5},
			Resolution: [3]uint32{40, 40, 40},
			Intensity:  1,
		},
	}

	candidates := buildCAVolumeRenderCandidates(camera, 960, 540, volumes)
	if len(candidates) != 2 {
		t.Fatalf("expected two render candidates, got %d", len(candidates))
	}
	if candidates[0].EntityID != 1 {
		t.Fatalf("expected smaller farther-surface volume to render first, got order %+v", candidates)
	}
	if candidates[1].EntityID != 2 {
		t.Fatalf("expected larger nearer-surface volume to render last, got order %+v", candidates)
	}
	if !(candidates[0].SurfaceDistance > candidates[1].SurfaceDistance) {
		t.Fatalf("expected sort to use descending surface distance, got %f <= %f", candidates[0].SurfaceDistance, candidates[1].SurfaceDistance)
	}
}

func TestBuildCAVolumeRenderCandidatesUsesStableEntityTieBreak(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{0, 0, 10},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volumes := []gpu_rt.CAVolumeHost{
		{
			EntityID:   22,
			Position:   mgl32.Vec3{-2, -2, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.25, 0.25, 0.25},
			Resolution: [3]uint32{8, 8, 8},
			Intensity:  1,
		},
		{
			EntityID:   21,
			Position:   mgl32.Vec3{-2, -2, 0},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.25, 0.25, 0.25},
			Resolution: [3]uint32{8, 8, 8},
			Intensity:  1,
		},
	}

	candidates := buildCAVolumeRenderCandidates(camera, 800, 600, volumes)
	if len(candidates) != 2 {
		t.Fatalf("expected two candidates, got %d", len(candidates))
	}
	if candidates[0].EntityID != 21 || candidates[1].EntityID != 22 {
		t.Fatalf("expected stable entity-id tie break, got order %+v", candidates)
	}
}

func TestBuildCAVolumeRenderCandidatesCameraInsideRendersVolumeLast(t *testing.T) {
	camera := &core.CameraState{
		Position: mgl32.Vec3{1, 1, 1},
		Fov:      60,
		Near:     0.1,
		Far:      1000,
	}
	volumes := []gpu_rt.CAVolumeHost{
		{
			EntityID:   7,
			Position:   mgl32.Vec3{-1, -1, -1},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{1, 1, 1},
			Resolution: [3]uint32{4, 4, 4},
			Intensity:  1,
		},
		{
			EntityID:   8,
			Position:   mgl32.Vec3{0, 0, -8},
			Rotation:   mgl32.QuatIdent(),
			VoxelScale: mgl32.Vec3{0.4, 0.4, 0.4},
			Resolution: [3]uint32{10, 10, 10},
			Intensity:  1,
		},
	}

	candidates := buildCAVolumeRenderCandidates(camera, 800, 600, volumes)
	if len(candidates) != 2 {
		t.Fatalf("expected two candidates, got %d", len(candidates))
	}
	if candidates[1].EntityID != 7 {
		t.Fatalf("expected camera-contained volume to render last, got order %+v", candidates)
	}
	if candidates[1].Scissor.W != 800 || candidates[1].Scissor.H != 600 {
		t.Fatalf("expected fullscreen scissor for camera-contained volume, got %+v", candidates[1].Scissor)
	}
	if candidates[1].SurfaceDistance != 0 {
		t.Fatalf("expected zero surface distance when camera is inside volume, got %f", candidates[1].SurfaceDistance)
	}
}
