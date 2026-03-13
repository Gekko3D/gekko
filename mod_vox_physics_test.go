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
	// Create two voxels in different bricks (within one sector)
	xbm.SetVoxel(0, 0, 0, 1) // Brick (0,0,0)
	xbm.SetVoxel(8, 0, 0, 1) // Brick (1,0,0)
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
	if len(pm.Boxes) != 2 {
		t.Errorf("Expected 2 boxes for two-brick object, got %d", len(pm.Boxes))
	}

	// 4. Edit the XBrickMap (Carve one brick completely)
	xbm.SetVoxel(8, 0, 0, 0)
	// Now xbm should have StructureDirty = true because brick (1,0,0) became empty
	if !xbm.StructureDirty {
		t.Fatal("XBrickMap should have StructureDirty=true after removing the only voxel in a brick")
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

	if len(pm2.Boxes) != 1 {
		t.Errorf("Expected 1 box after carving one of two bricks, got %d", len(pm2.Boxes))
	}

	// 6. Remove the LAST brick completely
	xbm.SetVoxel(0, 0, 0, 0)
	VoxPhysicsPreCalcSystem(cmd, server, rtState)
	app.FlushCommands()

	// Verify PhysicsModel was updated
	comps = cmd.GetAllComponents(eid)
	for _, c := range comps {
		if p, ok := c.(PhysicsModel); ok {
			pm2 = p
		}
	}

	if len(pm2.Boxes) != 0 {
		t.Errorf("Expected 0 boxes after final carve, got %d", len(pm2.Boxes))
	}
}
