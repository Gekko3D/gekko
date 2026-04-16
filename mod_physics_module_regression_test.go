package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestScaledPivotWorld_UsesVoxelResolutionOverride(t *testing.T) {
	tr := &TransformComponent{
		Scale: mgl32.Vec3{1, 1, 1},
		Pivot: mgl32.Vec3{3, 3, 3},
	}
	vm := &VoxelModelComponent{VoxelResolution: 1.0}

	got := scaledPivotWorld(tr, vm)
	want := mgl32.Vec3{3, 3, 3}
	if got != want {
		t.Fatalf("expected scaled pivot %v, got %v", want, got)
	}
}

func TestPhysicsToRenderPosition_RoundTripsCenteredVoxelModelAtCustomResolution(t *testing.T) {
	tr := &TransformComponent{
		Position: mgl32.Vec3{10, 20, 30},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
		Pivot:    mgl32.Vec3{3, 3, 3},
	}
	vm := &VoxelModelComponent{VoxelResolution: 1.0}
	pm := &PhysicsModel{
		CenterOffset: mgl32.Vec3{3, 3, 3},
	}

	physicsPos := tr.Position.Add(tr.Rotation.Rotate(renderToPhysicsOffset(tr, pm, vm)))
	got := physicsToRenderPosition(physicsPos, tr.Rotation, tr, pm, vm)

	if got != tr.Position {
		t.Fatalf("expected round-trip render position %v, got %v", tr.Position, got)
	}
}
