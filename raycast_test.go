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
		instanceMap: make(map[EntityId]*core.VoxelObject),
		caVolumeMap: make(map[EntityId]*core.VoxelObject),
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
	}

	// EDGE CASE: Negative Zero Ray Direction
	dirDanger := mgl32.Vec3{-0.5e-8, 1.0, 0}
	state.Raycast(mgl32.Vec3{0, 0, 0}, dirDanger, 100.0)
}
