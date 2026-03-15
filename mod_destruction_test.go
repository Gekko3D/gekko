package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)


func TestDestructionSystem_Split(t *testing.T) {
	app := NewApp()
	app.UseModules(DestructionModule{})
	cmd := app.Commands()

	// 1. Setup VoxelRtState manually for testing
	state := &VoxelRtState{
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		skyboxLayers:   make(map[EntityId]SkyboxLayerComponent),
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
			Camera:   &core.CameraState{},
		},
	}

	// 2. Create an entity with a VoxelObject containing two disconnected islands
	xbm := volume.NewXBrickMap()
	// Island 1 (Larger: 8 voxels)
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			for z := 0; z < 2; z++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}
	// Island 2 (Smaller: 1 voxel)
	xbm.SetVoxel(10, 10, 10, 2)

	entity := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelPalette: AssetId{},
			VoxelModel:   AssetId{},
		},
	)
	app.FlushCommands()

	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm
	state.instanceMap[entity] = obj

	// 3. Setup DestructionQueue
	queue := &DestructionQueue{
		Events: []DestructionEvent{
			{
				Entity: entity,
				Center: mgl32.Vec3{5, 5, 5}, // Doesn't hit anything, but triggers split
				Radius: 1,
			},
		},
	}

	// 4. Run the system
	paletteId := AssetId{}
	server := &AssetServer{
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	}
	server.voxPalettes[paletteId] = VoxelPaletteAsset{}

	destructionSystem(state, queue, cmd, server)
	app.FlushCommands()

	// 5. Sync ECS to internal state
	voxelRtSystem(nil, state, server, &Time{}, cmd)

	// 6. Verify results
	// The original entity should still exist (it had the largest component)
	// it should now only have the 8 voxels of Island 1
	if obj.XBrickMap.GetVoxelCount() != 8 {
		t.Errorf("Original entity should have 8 voxels, got %d", obj.XBrickMap.GetVoxelCount())
	}

	// 7. A new entity should have been created for Island 2 (if it was >= 8 voxels)
	// Island 1 was 8, Island 2 was 1. Threshold is 8.
	// So only 1 entity should exist.
	entities := app.ecs.storage.entityIndex
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity after split (Island 2 below threshold), got %d", len(entities))
	}
}

func TestDestructionSystem_SpawnDebris(t *testing.T) {
	app := NewApp()
	app.UseModules(DestructionModule{})
	cmd := app.Commands()

	state := &VoxelRtState{
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		skyboxLayers:   make(map[EntityId]SkyboxLayerComponent),
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
			Camera:   &core.CameraState{},
		},
	}

	xbm := volume.NewXBrickMap()
	// Island 1 (8 voxels) at (0,0,0)
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			for z := 0; z < 2; z++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}
	// Island 2 (8 voxels) at (10,10,10)
	for x := 10; x < 12; x++ {
		for y := 10; y < 12; y++ {
			for z := 10; z < 12; z++ {
				xbm.SetVoxel(x, y, z, 2)
			}
		}
	}

	entity := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{100, 100, 100},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelPalette: AssetId{},
		},
	)
	app.FlushCommands()

	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm
	state.instanceMap[entity] = obj

	queue := &DestructionQueue{
		Events: []DestructionEvent{
			{
				Entity: entity,
				Center: mgl32.Vec3{5, 5, 5},
				Radius: 1,
			},
		},
	}

	// Run system
	paletteId := AssetId{}
	server := &AssetServer{
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	}
	server.voxPalettes[paletteId] = VoxelPaletteAsset{}

	destructionSystem(state, queue, cmd, server)
	app.FlushCommands()

	// Sync ECS to internal state
	voxelRtSystem(nil, state, server, &Time{}, cmd)

	// Island 1 was kept in original (they are both 8, so it picks one)
	// Island 2 should be spawned as a new entity.

	entities := app.ecs.storage.entityIndex
	if len(entities) != 2 {
		t.Fatalf("Expected 2 entities after split, got %d", len(entities))
	}

	// One should have RigidBody (the debris)
	foundDebris := false
	for eid := range entities {
		if eid == entity {
			continue
		}

		for _, c := range cmd.GetAllComponents(eid) {
			switch c.(type) {
			case *RigidBodyComponent:
				foundDebris = true
			case RigidBodyComponent:
				foundDebris = true
			}
		}
	}

	if !foundDebris {
		t.Errorf("New entity should have a RigidBodyComponent")
	}
}

func TestVoxelSphereEdit_Carve(t *testing.T) {
	// 1. This test verifies the fix for world-to-voxel coordinate conversion
	state := &VoxelRtState{
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}

	// 1. Create a 10x10x10 voxel block
	xbm := volume.NewXBrickMap()
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			for z := 0; z < 10; z++ {
				xbm.SetVoxel(x, y, z, 1) // fill with color 1
			}
		}
	}

	entity := EntityId(1)
	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm

	// Set transform: Position at (10, 10, 10) in world
	// VoxelSize is 0.1, so Scale 1.0 means object size is 1.0 world units (10 * 0.1)
	// IMPORTANT: The renderer scale ALREADY includes VoxelSize.
	vSize := VoxelSize // 0.1
	obj.Transform.Position = mgl32.Vec3{10, 10, 10}
	obj.Transform.Scale = mgl32.Vec3{vSize, vSize, vSize} // Scale 1.0 in voxel units = 0.1 world units per voxel
	obj.Transform.Rotation = mgl32.QuatIdent()

	state.instanceMap[entity] = obj

	// 2. Carve a sphere at (10.5, 10.5, 10.5) with world radius 0.2
	// (10.5, 10.5, 10.5) world is at (5, 5, 5) in local voxel indices:
	// Local Pos = (WorldPos - ObjPos) / (Scale) = (0.5, 0.5, 0.5) / 0.1 = (5, 5, 5)
	// Radius in voxels = WorldRadius / Scale = 0.2 / 0.1 = 2.0 voxels
	state.VoxelSphereEdit(entity, mgl32.Vec3{10.5, 10.5, 10.5}, 0.2, 0)

	// Check if voxel (5, 5, 5) is carved
	found, val := xbm.GetVoxel(5, 5, 5)
	if found || val != 0 {
		t.Errorf("Voxel at (5, 5, 5) should be carved (found=false, val=0), got found=%v, val=%d. Scaling/Conversion logic might be broken.", found, val)
	}

	// Double check a voxel far away is NOT carved
	foundFar, valFar := xbm.GetVoxel(0, 0, 0)
	if !foundFar || valFar != 1 {
		t.Errorf("Voxel at (0, 0, 0) should still be 1 (found=true), got found=%v, val=%d", foundFar, valFar)
	}

	// 3. Test with object Scale = 2.0
	// Scale = 2.0 * VoxelSize = 0.2 meters per voxel
	obj.Transform.Scale = mgl32.Vec3{vSize * 2, vSize * 2, vSize * 2}

	// Carve at (11.0, 11.0, 11.0) with world radius 0.2
	// Local Pos = (1.0, 1.0, 1.0) meters / 0.2 meters/voxel = (5, 5, 5)
	// Radius in voxels = 0.2 / 0.2 = 1.0 voxel
	state.VoxelSphereEdit(entity, mgl32.Vec3{11.0, 11.0, 11.0}, 0.2, 3)

	found2, val2 := xbm.GetVoxel(5, 5, 5)
	if !found2 || val2 != 3 {
		t.Errorf("Voxel at (5, 5, 5) should be 3 (found=true) after second edit, got found=%v, val=%d", found2, val2)
	}
}

func TestVoxelRtSystem_MapSync(t *testing.T) {
	// This test verifies the fix for syncing CustomMap changes from ECS to internal state
	app := NewApp()
	cmd := app.Commands()

	// Mocking what we need for VoxelRtState
	state := &VoxelRtState{
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
			Camera:   &core.CameraState{},
		},
	}

	// Pre-load a dummy model template so it doesn't try to load assets for real
	dummyTemplate := core.NewVoxelObject()
	dummyTemplate.XBrickMap = volume.NewXBrickMap()
	state.loadedModels[AssetId{}] = dummyTemplate

	// 1. Create an entity with initial map
	map1 := volume.NewXBrickMap()
	map1.SetVoxel(0, 0, 0, 1)

	paletteId := AssetId{} // Using empty ID for simplicity
	server := &AssetServer{
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	}
	server.voxPalettes[paletteId] = VoxelPaletteAsset{}

	entity := cmd.AddEntity(
		&TransformComponent{},
		&VoxelModelComponent{
			VoxelModel:   AssetId{},
			VoxelPalette: paletteId,
			CustomMap:    map1,
		},
	)
	app.FlushCommands()

	// Run system once to "spawn" the internal object
	voxelRtSystem(nil, state, server, &Time{}, cmd)

	obj := state.instanceMap[entity]
	if obj == nil {
		t.Fatalf("VoxelObject not found in instanceMap")
	}
	if obj.XBrickMap != map1 {
		t.Fatalf("Internal XBrickMap doesn't match initial CustomMap")
	}

	// 2. Change CustomMap in ECS
	map2 := volume.NewXBrickMap()
	map2.SetVoxel(1, 1, 1, 2)

	cmd.AddComponents(entity, &VoxelModelComponent{
		VoxelModel: AssetId{},
		CustomMap:  map2,
	})
	app.FlushCommands()

	// Run system again
	voxelRtSystem(nil, state, nil, &Time{}, cmd)

	// Verify it synced
	if obj.XBrickMap != map2 {
		t.Errorf("Internal XBrickMap did not sync to map2 after ECS update")
	}
	if !obj.XBrickMap.StructureDirty {
		t.Errorf("XBrickMap should have StructureDirty set after sync")
	}
}

func TestDestruction_TotalAnnihilation(t *testing.T) {
	app := NewApp()
	cmd := &Commands{app: app}

	state := &VoxelRtState{
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
		},
	}

	// 1. Create a tiny 2x2x2 object
	xbm := volume.NewXBrickMap()
	for z := 0; z < 2; z++ {
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}

	ent := cmd.AddEntity(
		&TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{CustomMap: xbm},
	)

	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm
	state.instanceMap[ent] = obj

	// 2. Queue a huge destruction that covers everything
	queue := &DestructionQueue{
		Events: []DestructionEvent{
			{Entity: ent, Center: mgl32.Vec3{0.5, 0.5, 0.5}, Radius: 10.0},
		},
	}

	// 3. Process destruction
	destructionSystem(state, queue, cmd, nil)

	// 4. Verify ECS removal
	removed := false
	for _, pendEnt := range app.pendingRemovals {
		if pendEnt == ent {
			removed = true
			break
		}
	}

	if !removed {
		t.Error("Entity with 0 voxels was not removed from ECS")
	}
}

func TestDestructionSystem_MomentumInheritance(t *testing.T) {
	app := NewApp()
	app.UseModules(DestructionModule{})
	cmd := app.Commands()

	state := &VoxelRtState{
		loadedModels:   make(map[AssetId]*core.VoxelObject),
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
		skyboxLayers:   make(map[EntityId]SkyboxLayerComponent),
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
			Camera:   &core.CameraState{},
		},
	}

	xbm := volume.NewXBrickMap()
	// Island 1 (8 voxels) at (0,0,0) - stays at origin
	for x := 0; x < 2; x++ {
		for y := 0; y < 2; y++ {
			for z := 0; z < 2; z++ {
				xbm.SetVoxel(x, y, z, 1)
			}
		}
	}
	// Island 2 (8 voxels) at (10,0,0) - centered at (10.5, 0.5, 0.5)
	// After centering, shard origin will be (10.5, 0.5, 0.5) world offset
	for x := 10; x < 12; x++ {
		for y := 0; y < 2; y++ {
			for z := 0; z < 2; z++ {
				xbm.SetVoxel(x, y, z, 2)
			}
		}
	}

	parentPos := mgl32.Vec3{100, 100, 100}
	parentVel := mgl32.Vec3{10, 0, 0}
	parentAngVel := mgl32.Vec3{0, 10, 0} // Rotation around Y axis

	entity := cmd.AddEntity(
		&TransformComponent{
			Position: parentPos,
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{
			VoxelPalette: AssetId{},
		},
		&RigidBodyComponent{
			Velocity:        parentVel,
			AngularVelocity: parentAngVel,
			Mass:            10.0,
		},
	)
	app.FlushCommands()

	obj := core.NewVoxelObject()
	obj.XBrickMap = xbm
	obj.Transform.Position = parentPos
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Rotation = mgl32.QuatIdent()
	state.instanceMap[entity] = obj

	queue := &DestructionQueue{
		Events: []DestructionEvent{
			{
				Entity: entity,
				Center: parentPos.Add(mgl32.Vec3{0.5, 0, 0}), // Triggers split (point between islands)
				Radius: 0.05, // Small radius to avoid carving the islands themselves
			},

		},
	}


	// Run system
	server := &AssetServer{
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	}
	server.voxPalettes[AssetId{}] = VoxelPaletteAsset{}

	destructionSystem(state, queue, cmd, server)
	app.FlushCommands()

	// Island 2 should be a new entity with inherited momentum
	foundDebris := false
	for eid := range app.ecs.storage.entityIndex {
		comps := cmd.GetAllComponents(eid)
		var rb *RigidBodyComponent
		for _, c := range comps {
			if v, ok := c.(*RigidBodyComponent); ok {
				rb = v
			} else if v, ok := c.(RigidBodyComponent); ok {
				rb = &v
			}
		}

		if eid == entity {
			// Check parent mass update
			if rb != nil {
				expectedMass := float32(8) * 0.1
				if rb.Mass != expectedMass {
					t.Errorf("Parent mass should be %v, got %v", expectedMass, rb.Mass)
				}
			}
			continue
		}

		if rb != nil {
			foundDebris = true
			// V_shard = V_parent + Omega_parent x worldOffset
			
			// Island 2 voxels are [10,11]x[0,1]x[0,1]. Local center is (11, 1, 1).
			// Scale is 1.0, VoxelSize is 0.1.
			// worldOffset = rotation * (localCenter * vSize * scale)
			localCenter := mgl32.Vec3{11, 1, 1}
			worldOffset := mgl32.QuatIdent().Rotate(localCenter.Mul(0.1))
			expectedVel := parentVel.Add(parentAngVel.Cross(worldOffset))


			if rb.Velocity.Sub(expectedVel).Len() > 0.01 {
				t.Errorf("Debris velocity should be approx %v, got %v", expectedVel, rb.Velocity)
			}
			if rb.AngularVelocity.Sub(parentAngVel).Len() > 0.01 {
				t.Errorf("Debris angular velocity should be %v, got %v", parentAngVel, rb.AngularVelocity)
			}
		}
	}

	if !foundDebris {
		t.Errorf("No debris found with RigidBodyComponent")
	}
}


