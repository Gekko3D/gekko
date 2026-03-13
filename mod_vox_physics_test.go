package gekko

import (
	"testing"
	"reflect"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	rootassets "github.com/gekko3d/gekko/assets"
)

func TestVoxPhysicsPreCalcSystem_DynamicRebuild(t *testing.T) {
	ecs := MakeEcs()
	app := &App{
		resources: make(map[reflect.Type]any),
		ecs:       &ecs,
	}
	cmd := &Commands{app: app}

	server := &AssetServer{
		voxModels:   make(map[AssetId]VoxelModelAsset),
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	}
	rtState := &VoxelRtState{
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}

	aid := rootassets.NewID()

	// 1. Create an entity with a VoxelModelComponent
	eid := cmd.AddEntity(
		VoxelModelComponent{VoxelModel: aid},
		RigidBodyComponent{Mass: 1.0},
		TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
	)
	app.FlushCommands()

	// 2. Setup a VoxelObject in rtState with an XBrickMap
	xbm := volume.NewXBrickMap()
	// Create a 2x2x2 cube
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			for z := 0; z < 2; z++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}
	xbm.ClearDirty() // Clear initial dirty state

	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm
	rtState.instanceMap[eid] = obj

	// 3. Run the system for the first time (Initial build)
	VoxPhysicsPreCalcSystem(cmd, server, rtState)
	app.FlushCommands()

	// Verify PhysicsModel was added
	comps := cmd.GetAllComponents(eid)
	var pm PhysicsModel
	found := false
	for _, c := range comps {
		if p, ok := c.(PhysicsModel); ok {
			pm = p
			found = true
			break
		}
	}
	if !found {
		t.Fatal("PhysicsModel not created on first run")
	}
	if len(pm.Boxes) != 1 {
		t.Errorf("Expected 1 box for 2x2x2 cube, got %d", len(pm.Boxes))
	}

	// 4. Edit the XBrickMap (Carve it)
	// Remove top half (y=1)
	for x := 0; x < 2; x++ {
		for z := 0; z < 2; z++ {
			xbm.SetVoxel(x, 1, z, 0)
		}
	}
	// Now xbm should have StructureDirty = true or DirtySectors > 0
	if !xbm.StructureDirty && len(xbm.DirtySectors) == 0 {
		t.Fatal("XBrickMap should be dirty after editing")
	}

	// 5. Run the system again (Should rebuild)
	VoxPhysicsPreCalcSystem(cmd, server, rtState)
	app.FlushCommands()

	// Verify PhysicsModel was updated
	comps = cmd.GetAllComponents(eid)
	found = false
	var pm2 PhysicsModel
	for _, c := range comps {
		if p, ok := c.(PhysicsModel); ok {
			pm2 = p
			found = true
			break
		}
	}
	if !found {
		t.Fatal("PhysicsModel disappeared after rebuild")
	}

	// The new model should be 2x1x2 (half the height)
	// Volume of 2x2x2 was 8, volume of 2x1x2 is 4 (in voxels)
	// vSize = 0.1, so boxes[0].HalfExtents should be {1*0.1*0.5, 0.5*0.1*0.5, 1*0.1*0.5} = {0.1, 0.05, 0.1}
	
	if len(pm2.Boxes) != 1 {
		t.Errorf("Expected 1 box after carve, got %d", len(pm2.Boxes))
	}
	
	box := pm2.Boxes[0]
	if absf(box.HalfExtents.Y() - 0.05) > 1e-5 {
		t.Errorf("Expected Y HalfExtent 0.05, got %f", box.HalfExtents.Y())
	}
}
