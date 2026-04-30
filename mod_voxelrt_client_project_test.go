package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func TestVoxelRtStateProjectSupportsReverseZ(t *testing.T) {
	state := &VoxelRtState{RtApp: &app_rt.App{}}
	camera := &CameraComponent{
		Position:  mgl32.Vec3{0, 0, 0},
		LookAt:    mgl32.Vec3{0, 0, -1},
		Up:        mgl32.Vec3{0, 1, 0},
		Fov:       60,
		Near:      0.1,
		Far:       1000,
		DepthMode: core.DepthModeReverseZ,
	}

	x, y, ok := state.Project(mgl32.Vec3{0, 0, -5}, camera)
	if !ok {
		t.Fatal("expected reverse-z projected point to be visible")
	}
	if x <= 0 || y <= 0 {
		t.Fatalf("expected on-screen coordinates, got %f %f", x, y)
	}
}

func TestVoxelRtStateProjectRejectsPointBeyondFarPlane(t *testing.T) {
	state := &VoxelRtState{RtApp: &app_rt.App{}}
	camera := &CameraComponent{
		Position:  mgl32.Vec3{0, 0, 0},
		LookAt:    mgl32.Vec3{0, 0, -1},
		Up:        mgl32.Vec3{0, 1, 0},
		Fov:       60,
		Near:      0.1,
		Far:       10,
		DepthMode: core.DepthModeReverseZ,
	}

	if _, _, ok := state.Project(mgl32.Vec3{0, 0, -20}, camera); ok {
		t.Fatal("expected point beyond far plane to be rejected")
	}
}
