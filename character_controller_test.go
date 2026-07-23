package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestCharacterMovementBlockHitReportsSampleMetadata(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := characterControllerTestVoxelWall(mgl32.Vec3{0, 0, 0})
	entity := EntityId(7)
	state.objectToEntity[wall] = entity
	state.RtApp.Scene.AddObject(wall)

	hit, ok := CharacterMovementBlockHit(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1, 0, 0}, CharacterCollisionConfig{
		Radius:     0.32,
		Height:     1.7,
		StepHeight: 0.45,
	}, nil)

	if !ok || !hit.Hit {
		t.Fatalf("expected movement probe to hit wall, got hit=%+v ok=%t", hit, ok)
	}
	if hit.Entity != entity {
		t.Fatalf("expected hit entity %d, got %+v", entity, hit)
	}
	if hit.SampleY <= 0 {
		t.Fatalf("expected sample metadata, got %+v", hit)
	}
}

func TestCharacterKinematicMoveMovesWhenClear(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()

	result := CharacterKinematicMove(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0.5, 0, 0.25}, CharacterKinematicMoveOptions{
		CollisionConfig: characterControllerTestCollisionConfig(),
	})

	if result.Blocked || result.Slid || result.Depenetrated {
		t.Fatalf("expected clear move, got %+v", result)
	}
	if !result.Position.ApproxEqualThreshold(mgl32.Vec3{0.5, 0, 0.25}, 0.001) {
		t.Fatalf("expected requested move to apply, got %+v", result)
	}
}

func TestCharacterKinematicMoveBlocksAgainstWall(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := characterControllerTestVoxelWall(mgl32.Vec3{1, 0, 0})
	state.RtApp.Scene.AddObject(wall)

	result := CharacterKinematicMove(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1, 0, 0}, CharacterKinematicMoveOptions{
		CollisionConfig: characterControllerTestCollisionConfig(),
		DisableSlide:    true,
	})

	if !result.Blocked || result.Slid {
		t.Fatalf("expected wall block without slide, got %+v", result)
	}
	if !result.Position.ApproxEqualThreshold(mgl32.Vec3{}, 0.001) {
		t.Fatalf("expected blocked move to keep start position, got %+v", result)
	}
}

func TestCharacterKinematicMoveSlidesAlongWall(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := characterControllerTestVoxelWall(mgl32.Vec3{1, 0, 0})
	state.RtApp.Scene.AddObject(wall)

	result := CharacterKinematicMove(state, mgl32.Vec3{0, 0, -0.5}, mgl32.Vec3{1, 0, 1}, CharacterKinematicMoveOptions{
		CollisionConfig: characterControllerTestCollisionConfig(),
	})

	if !result.Blocked || !result.Slid {
		t.Fatalf("expected wall contact to slide, got %+v", result)
	}
	if result.Position.Z() <= -0.25 {
		t.Fatalf("expected slide to preserve forward progress along wall, got %+v", result)
	}
	if result.Position.X() > 0.1 {
		t.Fatalf("expected slide not to move through wall, got %+v", result)
	}
}

func TestCharacterHorizontalHitBlocksMovementIgnoresWalkableNormals(t *testing.T) {
	if characterHorizontalHitBlocksMovement(mgl32.Vec3{0, 1, 0}) {
		t.Fatal("expected flat walkable normal not to block horizontal movement")
	}
	if characterHorizontalHitBlocksMovement(mgl32.Vec3{0.2, 0.6, 0}) {
		t.Fatal("expected steep-but-walkable upward normal not to block horizontal movement")
	}
	if !characterHorizontalHitBlocksMovement(mgl32.Vec3{-1, 0, 0}) {
		t.Fatal("expected wall normal to block horizontal movement")
	}
}

func TestCharacterKinematicMoveDepenetratesImmediateContact(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := characterControllerTestVoxelWall(mgl32.Vec3{0, 0, 0})
	state.RtApp.Scene.AddObject(wall)

	result := CharacterKinematicMove(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0.25, 0, 0}, CharacterKinematicMoveOptions{
		CollisionConfig: characterControllerTestCollisionConfig(),
	})

	if !result.Depenetrated || result.Depenetration.Iterations == 0 {
		t.Fatalf("expected kinematic move to depenetrate initial contact, got %+v", result)
	}
	if result.Position.ApproxEqualThreshold(mgl32.Vec3{}, 0.001) {
		t.Fatalf("expected position to change after depenetration, got %+v", result)
	}
}

func TestCharacterGroundedMoveStepsOntoWalkableVoxelRiser(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	ground := core.NewVoxelObject()
	ground.XBrickMap = volume.NewXBrickMap()
	for x := -4; x <= 0; x++ {
		for z := -2; z <= 2; z++ {
			ground.XBrickMap.SetVoxel(x, -1, z, 1)
		}
	}
	for x := 1; x <= 4; x++ {
		for z := -2; z <= 2; z++ {
			ground.XBrickMap.SetVoxel(x, 0, z, 1)
		}
	}
	ground.Transform.Scale = mgl32.Vec3{1, 1, 1}
	ground.Transform.Dirty = true
	ground.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(ground)

	result := CharacterGroundedMove(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1.1, 0, 0}, CharacterGroundedMoveOptions{
		CollisionConfig: CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 1.1},
		GroundConfig:    CharacterGroundProbeConfig{Radius: 0.25, StepHeight: 1.1, GroundProbe: 0.15},
	})

	if !result.Stepped || result.Blocked {
		t.Fatalf("expected walkable riser step, got %+v", result)
	}
	if result.Position.X() < 1 || result.Position.Y() < 0.99 || result.Position.Y() > 1.01 {
		t.Fatalf("expected character on upper step, got %+v", result)
	}
}

func TestCharacterGroundedMoveDoesNotStepOverTallVoxelWall(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := core.NewVoxelObject()
	wall.XBrickMap = volume.NewXBrickMap()
	for x := -4; x <= 0; x++ {
		for z := -2; z <= 2; z++ {
			wall.XBrickMap.SetVoxel(x, -1, z, 1)
		}
	}
	for x := 1; x <= 4; x++ {
		for z := -2; z <= 2; z++ {
			wall.XBrickMap.SetVoxel(x, 1, z, 1)
		}
	}
	wall.Transform.Scale = mgl32.Vec3{1, 1, 1}
	wall.Transform.Dirty = true
	wall.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(wall)

	result := CharacterGroundedMove(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1.1, 0, 0}, CharacterGroundedMoveOptions{
		CollisionConfig: CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 1.1},
		GroundConfig:    CharacterGroundProbeConfig{Radius: 0.25, StepHeight: 1.1, GroundProbe: 0.15},
	})

	if !result.Blocked || result.Position.X() > 0.01 {
		t.Fatalf("expected tall wall to remain blocked, got %+v", result)
	}
}

func TestCharacterVerticalMoveStopsOnThinVoxelFloor(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	floor := core.NewVoxelObject()
	floor.XBrickMap = volume.NewXBrickMap()
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			floor.XBrickMap.SetVoxel(x, -1, z, 1)
		}
	}
	floor.Transform.Scale = mgl32.Vec3{1, 1, 1}
	floor.Transform.Dirty = true
	floor.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(floor)

	next, blocked := CharacterVerticalMove(state, mgl32.Vec3{0, 2, 0}, -3, CharacterCollisionConfig{Radius: 0.25, Height: 1.7}, nil)
	if !blocked || next.Y() < -0.001 || next.Y() > 0.03 {
		t.Fatalf("expected thin floor to stop fall, got next=%v blocked=%t", next, blocked)
	}
}

func TestCharacterGroundedMoveFindsFloorAcrossObjectSeam(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	for _, object := range []*core.VoxelObject{
		characterControllerTestFloorObject(mgl32.Vec3{}, -1, 0),
		characterControllerTestFloorObject(mgl32.Vec3{1, 0, 0}, 0, 1),
	} {
		state.RtApp.Scene.AddObject(object)
	}

	result := CharacterGroundedMove(state, mgl32.Vec3{0.25, 0.2, 0}, mgl32.Vec3{1, 0, 0}, CharacterGroundedMoveOptions{
		CollisionConfig: CharacterCollisionConfig{Radius: 0.25, Height: 1.7, StepHeight: 0.6},
		GroundConfig:    CharacterGroundProbeConfig{Radius: 0.25, StepHeight: 0.6, GroundProbe: 0.15},
	})

	if result.Position.Y() < -0.001 || result.Position.Y() > 0.01 {
		t.Fatalf("expected seam floor to ground character, got %+v", result)
	}
}

func TestCharacterDepenetrateInitialContactsPushesOutOfImmediateHit(t *testing.T) {
	state := newCharacterControllerTestVoxelRtState()
	wall := characterControllerTestVoxelWall(mgl32.Vec3{0, 0, 0})
	state.RtApp.Scene.AddObject(wall)

	result := CharacterDepenetrateInitialContacts(state, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{1, 0, 0}, CharacterCollisionConfig{
		Radius:                  0.32,
		Height:                  1.7,
		StepHeight:              0.45,
		SkinWidth:               0.06,
		ContactEpsilon:          0.02,
		MaxDepenetration:        0.2,
		DepenetrationIterations: 2,
	}, nil)

	if !result.Depenetrated || result.Iterations == 0 {
		t.Fatalf("expected initial overlap to depenetrate, got %+v", result)
	}
	if result.Offset.Len() <= 0 || result.Position.ApproxEqualThreshold(mgl32.Vec3{0, 0, 0}, 0.001) {
		t.Fatalf("expected nonzero depenetration offset, got %+v", result)
	}
	if result.Offset.Y() != 0 {
		t.Fatalf("expected horizontal depenetration, got %+v", result)
	}
}

func characterControllerTestCollisionConfig() CharacterCollisionConfig {
	return CharacterCollisionConfig{
		Radius:                  0.32,
		Height:                  1.7,
		StepHeight:              0.45,
		SkinWidth:               0.06,
		ContactEpsilon:          0.02,
		MaxDepenetration:        0.2,
		DepenetrationIterations: 2,
	}
}

func newCharacterControllerTestVoxelRtState() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}
}

func characterControllerTestVoxelWall(position mgl32.Vec3) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	for y := 0; y < 4; y++ {
		obj.XBrickMap.SetVoxel(0, y, 0, 1)
	}
	obj.Transform.Position = position
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	return obj
}

func characterControllerTestFloorObject(position mgl32.Vec3, minX, maxX int) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	for x := minX; x <= maxX; x++ {
		for z := -1; z <= 1; z++ {
			obj.XBrickMap.SetVoxel(x, -1, z, 1)
		}
	}
	obj.Transform.Position = position
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	return obj
}
