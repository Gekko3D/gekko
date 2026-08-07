package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestCharacterFindTraversalTargetFindsWalkableChestHighLanding(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	state.RtApp.Scene.AddObject(characterTraversalTestLedge(true))

	target, ok := CharacterFindTraversalTarget(state, mgl32.Vec3{}, mgl32.Vec3{1, 0, 0}, CharacterTraversalConfig{
		CollisionConfig: CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 0.6},
		GroundConfig:    CharacterGroundProbeConfig{Radius: 0.25, StepHeight: 0.6, GroundProbe: 0.15},
		MaxHeight:       1.2,
		ForwardDistance: 1.2,
	}, nil)

	if !ok {
		t.Fatal("expected chest-high ledge traversal target")
	}
	if target.Height < 0.95 || target.Height > 1.05 || target.Landing.X() < 1 {
		t.Fatalf("unexpected traversal target: %+v", target)
	}
}

func TestCharacterFindTraversalTargetRejectsWallWithoutLanding(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	state.RtApp.Scene.AddObject(characterTraversalTestLedge(false))

	_, ok := CharacterFindTraversalTarget(state, mgl32.Vec3{}, mgl32.Vec3{1, 0, 0}, CharacterTraversalConfig{
		CollisionConfig: CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 0.6},
		GroundConfig:    CharacterGroundProbeConfig{Radius: 0.25, StepHeight: 0.6, GroundProbe: 0.15},
		MaxHeight:       1.2,
		ForwardDistance: 1.2,
	}, nil)
	if ok {
		t.Fatal("expected wall without a landing to be rejected")
	}
}

func TestCharacterDropAcceptsAndSettlesOnExpectedDynamicSupport(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	support := cmd.AddEntity(
		&TransformComponent{},
		&MovingBrushComponent{BoundsCenter: mgl32.Vec3{0, -0.48, 0}, BoundsHalfExtents: mgl32.Vec3{1, 0.52, 1}},
	)
	app.FlushCommands()
	base := mgl32.Vec3{}
	collision := CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 0.6, DynamicCollisionQuery: MovingBrushCollisionQuery(cmd)}
	if CharacterHasStandingClearance(nil, base, collision, nil) {
		t.Fatal("ordinary clearance unexpectedly accepted support inside the foot band")
	}
	if !characterHasLandingSupport(nil, base, collision, nil, support) ||
		!characterHasStandingClearance(nil, base, collision, nil, characterLandingSupportTolerance(collision)) {
		t.Fatal("expected dynamic landing support was rejected")
	}

	ctrl := GroundedCharacterMotorComponent{Grounded: true, GroundContactCount: 1}
	CharacterBeginTraversal(&ctrl, CharacterTraversalRequest{Kind: CharacterTraversalDrop, End: base, LandingSupportEntity: support})
	ctrl.Traversal.WasAirborne = true
	ctrl.GroundContacts[0] = CharacterGroundContact{Entity: support}
	finishGroundedBallisticLanding(&ctrl, base)
	if ctrl.Traversal.Status != CharacterTraversalSucceeded {
		t.Fatalf("expected landing support did not settle traversal: %+v", ctrl.Traversal)
	}
}

func TestCharacterDropRejectsWrongDynamicSupport(t *testing.T) {
	ctrl := GroundedCharacterMotorComponent{Grounded: true, GroundContactCount: 1}
	CharacterBeginTraversal(&ctrl, CharacterTraversalRequest{Kind: CharacterTraversalDrop, End: mgl32.Vec3{}, LandingSupportEntity: 1})
	ctrl.Traversal.WasAirborne = true
	ctrl.GroundContacts[0] = CharacterGroundContact{Entity: 2}
	finishGroundedBallisticLanding(&ctrl, mgl32.Vec3{})
	if ctrl.Traversal.Status != CharacterTraversalFailed || ctrl.Traversal.Reason != "wrong_landing_support" {
		t.Fatalf("wrong landing support was accepted: %+v", ctrl.Traversal)
	}
}

func characterTraversalTestLedge(withLanding bool) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	for x := -4; x <= 5; x++ {
		for z := -2; z <= 2; z++ {
			obj.XBrickMap.SetVoxel(x, -1, z, 1)
		}
	}
	if withLanding {
		for x := 1; x <= 5; x++ {
			for z := -2; z <= 2; z++ {
				obj.XBrickMap.SetVoxel(x, 0, z, 1)
			}
		}
	} else {
		for y := 0; y <= 3; y++ {
			for z := -2; z <= 2; z++ {
				obj.XBrickMap.SetVoxel(1, y, z, 1)
			}
		}
	}
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	return obj
}
