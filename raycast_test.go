package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestRaycastScaling(t *testing.T) {
	// Setup VoxelRtState with necessary maps
	state := &VoxelRtState{
		RtApp: &app.App{
			Scene: core.NewScene(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}

	// Create a test object
	// Scale 0.1 -> 1 world unit = 10 local units
	// Place it at World Z = 150
	// Local Z will be 1500 (relative to origin if unrotated)
	// Actually, let's just make it simpler.

	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()

	// Set a voxel at local 0,0,0
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)

	obj.Transform.Position = mgl32.Vec3{0, 0, 150}
	obj.Transform.Scale = mgl32.Vec3{0.1, 0.1, 0.1}
	obj.Transform.Dirty = true

	// Update transform matrices manually (usually done by system)
	// obj.Transform.Update() // Not needed, calculated on fly

	// Verify Sector Exists
	if len(obj.XBrickMap.Sectors) == 0 {
		t.Fatal("XBrickMap has no sectors! SetVoxel failed?")
	}
	sKey := [3]int{0, 0, 0}
	if _, ok := obj.XBrickMap.Sectors[sKey]; !ok {
		t.Fatal("Sector {0,0,0} not found!")
	}

	eid := EntityId(1)
	state.instanceMap[eid] = obj
	state.RtApp.Scene.AddObject(obj)

	// CONTROL TEST: Unscaled object
	// Move object close and unscale
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Position = mgl32.Vec3{0, 0, 10}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()

	// Local Pos of surface: (0,0,0)
	// World Pos of surface: (0,0,10)
	// Ray from (0,0,0) -> hits at t=10
	hitControl := state.Raycast(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, 1}, 200.0)
	if !hitControl.Hit {
		t.Error("Control test (unscaled) failed! Raycast missed completely.")
	} else if hitControl.T < 9.9 || hitControl.T > 10.1 {
		t.Errorf("Control test hit at wrong distance: %f (expected 10)", hitControl.T)
	}

	// SCALED TEST (The bug)
	obj.Transform.Scale = mgl32.Vec3{0.1, 0.1, 0.1}
	obj.Transform.Position = mgl32.Vec3{0, 0, 150}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()

	hit := state.Raycast(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, 1}, 200.0)

	if !hit.Hit {
		t.Errorf("Expected hit on scaled down object, but missed. Scaling issue confirmed.")
	} else {
		if hit.T < 149.0 || hit.T > 151.0 {
			t.Errorf("Hit wrong distance: %f, expected ~150", hit.T)
		}
		if hit.PaletteIndex != 1 {
			t.Errorf("expected hit palette index 1, got %d", hit.PaletteIndex)
		}
	}

	// EDGE CASE: Negative Zero Ray Direction
	dirDanger := mgl32.Vec3{-0.5e-8, 1.0, 0}
	state.Raycast(mgl32.Vec3{0, 0, 0}, dirDanger, 100.0)
}

func TestRaycastFilteredSkipsRejectedEntity(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app.App{
			Scene: core.NewScene(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}

	near := testRaycastVoxelObjectAt(mgl32.Vec3{0, 0, 4})
	far := testRaycastVoxelObjectAt(mgl32.Vec3{0, 0, 8})
	nearEntity := EntityId(10)
	farEntity := EntityId(20)
	state.instanceMap[nearEntity] = near
	state.instanceMap[farEntity] = far
	state.objectToEntity[near] = nearEntity
	state.objectToEntity[far] = farEntity
	state.RtApp.Scene.AddObject(near)
	state.RtApp.Scene.AddObject(far)

	unfiltered := state.Raycast(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, 1}, 20)
	if !unfiltered.Hit || unfiltered.Entity != nearEntity {
		t.Fatalf("expected unfiltered raycast to hit near entity, got %+v", unfiltered)
	}

	filtered := state.RaycastFiltered(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, 1}, 20, func(eid EntityId, known bool) bool {
		return !known || eid != nearEntity
	})
	if !filtered.Hit || filtered.Entity != farEntity {
		t.Fatalf("expected filtered raycast to skip near entity and hit far entity, got %+v", filtered)
	}
}

func TestRaycastVoxelExitMeasuresScaledSolidThickness(t *testing.T) {
	state := &VoxelRtState{
		RtApp:          &app.App{Scene: core.NewScene()},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}
	entity := EntityId(10)
	wall := core.NewVoxelObject()
	wall.XBrickMap = volume.NewXBrickMap()
	wall.XBrickMap.SetVoxel(0, 0, 0, 1)
	wall.XBrickMap.SetVoxel(0, 0, 1, 1)
	wall.Transform.Scale = mgl32.Vec3{0.25, 0.25, 0.25}
	wall.Transform.Dirty = true
	wall.UpdateWorldAABB()
	state.instanceMap[entity] = wall
	state.objectToEntity[wall] = entity

	exit, thickness, ok := state.RaycastVoxelExit(entity, mgl32.Vec3{0.125, 0.125, -0.001}, mgl32.Vec3{0, 0, 1}, 1)
	if !ok || thickness < 0.49 || thickness > 0.51 || exit.Z() < 0.49 || exit.Z() > 0.51 {
		t.Fatalf("voxel exit = %v thickness %.4f ok=%v, want about 0.5", exit, thickness, ok)
	}
	if _, _, ok := state.RaycastVoxelExit(entity, mgl32.Vec3{0.125, 0.125, -0.001}, mgl32.Vec3{0, 0, 1}, 0.25); ok {
		t.Fatal("voxel exit ignored the maximum punch distance")
	}
}

func testRaycastVoxelObjectAt(position mgl32.Vec3) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.Transform.Position = position
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	return obj
}
