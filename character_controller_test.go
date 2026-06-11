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

func newCharacterControllerTestVoxelRtState() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene: core.NewScene(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
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
