package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestPhysicsIntegration(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.Gravity = mgl32.Vec3{0, -10, 0}

	// Entity with RigidBody
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 10, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1.0, GravityScale: 1.0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1}

	// Run system for a few frames
	for i := 0; i < 10; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	// Verify position fell
	var tr *TransformComponent
	var rb *RigidBodyComponent
	MakeQuery2[TransformComponent, RigidBodyComponent](cmd).Map(func(id EntityId, t *TransformComponent, r *RigidBodyComponent) bool {
		if id == eid {
			tr = t
			rb = r
		}
		return true
	})

	if tr.Position.Y() >= 10 {
		t.Errorf("Entity should have fallen, but Y = %f", tr.Position.Y())
	}
	if rb.Velocity.Y() >= 0 {
		t.Errorf("Entity should have negative velocity, but VY = %f", rb.Velocity.Y())
	}
}

func TestPhysicsSleeping(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.SleepThreshold = 0.1
	physics.SleepTime = 0.2

	// Entity at rest
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0.05, 0, 0}, GravityScale: 0}, // Velocity below threshold
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1}

	// Run system multiple times to trigger sleep
	for i := 0; i < 5; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	var rb *RigidBodyComponent
	MakeQuery1[RigidBodyComponent](cmd).Map(func(id EntityId, r *RigidBodyComponent) bool {
		if id == eid {
			rb = r
		}
		return true
	})

	if !rb.Sleeping {
		t.Errorf("Entity should be sleeping after being below threshold for enough time")
	}
	if rb.Velocity.Len() != 0 {
		t.Errorf("Sleeping entity should have zero velocity")
	}
}

func TestPhysicsCollision(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0

	world := NewWorldComponent("test", 10.0)
	// Add floor at y=0,1 (voxels are [0,1,2])
	world.MainXBM.SetVoxel(0, 0, 0, 1)

	cmd.AddEntity(world)
	cmd.app.FlushCommands()

	// Entity falling towards the floor
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -10, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1} // Move 1.0 units per frame

	// Run system
	for i := 0; i < 20; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	var tr *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, t *TransformComponent) bool {
		if id == eid {
			tr = t
		}
		return true
	})

	// Should be stopped at y=1.5 (floor top at y=1.0 + half-extent 0.5)
	if tr.Position.Y() < 1.49 {
		t.Errorf("Entity fell through floor! Y = %f", tr.Position.Y())
	}
}

func TestPhysicsScaling(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0

	world := NewWorldComponent("test", 10.0)
	world.MainXBM.SetVoxel(0, 0, 0, 1) // Floor at y=0,1
	cmd.AddEntity(world)
	cmd.app.FlushCommands()

	// Entity with Scale: 2.0 and HalfExtent: 0.5 -> Scaled HalfExtent: 1.0
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 5, 0}, Scale: mgl32.Vec3{2, 2, 2}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -10, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1}
	for i := 0; i < 20; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	var tr *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, t *TransformComponent) bool {
		if id == eid {
			tr = t
		}
		return true
	})

	// Floor top at 1.0. Scaled half-extent is 1.0 (0.5 * 2.0).
	// Center should stop at 1.0 + 1.0 = 2.0.
	if tr.Position.Y() < 1.99 {
		t.Errorf("Scaled entity fell too far! Y = %f (expected 2.0)", tr.Position.Y())
	}
	if tr.Position.Y() > 2.1 {
		t.Errorf("Scaled entity stopped too early! Y = %f (expected 2.0)", tr.Position.Y())
	}
}

func TestEntityCollision(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0

	// Create two entities
	// Body A: Static at origin
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)

	// Body B: Falling onto A
	eidB := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -10, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1}

	// Run system
	for i := 0; i < 20; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	var trB *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, t *TransformComponent) bool {
		if id == eidB {
			trB = t
		}
		return true
	})

	// Body A is at y=0, extents 0.5, so top is y=0.5
	// Body B has extents 0.5, so bottom is y_B - 0.5
	// Collision if y_B - 0.5 < 0.5  => y_B < 1.0
	// So B should stop at y=1.0
	if trB.Position.Y() < 0.99 {
		t.Errorf("Entity B fell through Entity A! Y = %f", trB.Position.Y())
	}
	if trB.Position.Y() > 1.1 {
		t.Errorf("Entity B stopped too early! Y = %f", trB.Position.Y())
	}
}

func TestPhysicsFriction(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0
	physics.Gravity = mgl32.Vec3{0, 0, 0} // No gravity for this test

	world := NewWorldComponent("test", 10.0)
	world.MainXBM.SetVoxel(0, 0, 0, 1) // Floor at y=0,1
	cmd.AddEntity(world)
	cmd.app.FlushCommands()

	// Entity moving horizontally ON the floor
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 1.5, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{10, -1, 0}, GravityScale: 0}, // Downward velocity to ensure floor contact
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}, Friction: 0.5},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1}

	// Run system
	for i := 0; i < 5; i++ {
		PhysicsSystem(cmd, tm, physics, nil, nil)
	}

	var rb *RigidBodyComponent
	MakeQuery1[RigidBodyComponent](cmd).Map(func(id EntityId, r *RigidBodyComponent) bool {
		if id == eid {
			rb = r
		}
		return true
	})

	// Initial vel X was 10. After 5 steps of 0.5 friction, it should be significantly lower.
	if rb.Velocity.X() >= 10 {
		t.Errorf("Friction did not slow down the entity! VX = %f", rb.Velocity.X())
	}
	if rb.Velocity.X() > 1.0 { // Should be roughly 10 * (0.5)^5 = 0.3125
		// wait, friction is applied on EVERY AXIS RESOLUTION.
		// resolves are Y, X, Z.
		// If it hits Y, X is slowed.
	}
}

func TestPhysicsRestitution(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0
	physics.Gravity = mgl32.Vec3{0, 0, 0}

	world := NewWorldComponent("test", 10.0)
	world.MainXBM.SetVoxel(0, 0, 0, 1) // Floor at y=0,1
	cmd.AddEntity(world)
	cmd.app.FlushCommands()

	// Entity falling at y=2 towards y=1
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -10, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.5, 0.5, 0.5}, Restitution: 0.5},
	)
	cmd.app.FlushCommands()

	tm := &Time{Dt: 0.1} // At t=0.1, it should hit. pos 2 -> 1.

	PhysicsSystem(cmd, tm, physics, nil, nil)

	var rb *RigidBodyComponent
	MakeQuery1[RigidBodyComponent](cmd).Map(func(id EntityId, r *RigidBodyComponent) bool {
		if id == eid {
			rb = r
		}
		return true
	})

	// Should have bounced
	if rb.Velocity.Y() <= 0 {
		t.Errorf("Entity did not bounce! VY = %f", rb.Velocity.Y())
	}
	if rb.Velocity.Y() != 5.0 { // 10 * 0.5
		t.Errorf("Incorrect bounce velocity! VY = %f (expected 5.0)", rb.Velocity.Y())
	}
}

func TestPhysicsPreciseCollision(t *testing.T) {
	ecs := MakeEcs()
	cmd := &Commands{app: &App{ecs: &ecs, resources: make(map[reflect.Type]any)}}

	physics := NewPhysicsWorld()
	physics.VoxelSize = 1.0

	// Instantiate AssetServer manually
	assets := &AssetServer{
		meshes:      make(map[AssetId]MeshAsset),
		materials:   make(map[AssetId]MaterialAsset),
		textures:    make(map[AssetId]TextureAsset),
		samplers:    make(map[AssetId]SamplerAsset),
		voxModels:   make(map[AssetId]VoxelModelAsset),
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
		voxFiles:    make(map[AssetId]*VoxFile),
	}
	AssetServerModule{}.Install(cmd.app, cmd) // Helper install if needed, but we constructed it manually

	// Create a "Hollow" entity model (inverted cube or just top/sides)
	// Let's make an arch: Legs at X=0 and X=2, empty at X=1.
	// Size 3x1x1.
	voxels := []Voxel{
		{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		{X: 2, Y: 0, Z: 0, ColorIndex: 1},
	}
	archModel := VoxModel{SizeX: 3, SizeY: 1, SizeZ: 1, Voxels: voxels}
	archId := assets.CreateVoxelModel(archModel, 1.0)

	// Create world voxel at X=1, Y=0, Z=0.
	world := NewWorldComponent("test", 10.0)
	// world.MainXBM.SetVoxel(0, 0, 0, 1) // Spike at 0,0,0
	cmd.AddEntity(world)
	cmd.app.FlushCommands()

	// Scale = 10 so model voxels (0.1) become 1.0 world units.
	scale := float32(10.0)

	// Test 1: Hit (Leg at X=0)
	// Place Hit Scenario at X=100
	world.MainXBM.SetVoxel(100, 0, 0, 1) // Spike at 100,0,0

	// Model Center is 1.5. Leg at 0 (-1.5 offset).
	// We want Leg to be at 100.
	// EntityPos + (-1.5) = 100 => EntityPos = 101.5.
	eidHit := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{101.5, 5, 0}, Scale: mgl32.Vec3{scale, scale, scale}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -1, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.15, 0.05, 0.05}},
		&VoxelModelComponent{VoxelModel: archId},
	)

	// Test 2: Miss (Hole at X=1)
	// Place Miss Scenario at X=0
	// Hole at X=1 (-0.5 offset).
	// We want Hole to be at 0.
	// EntityPos + (-0.5) = 0 => EntityPos = 0.5.
	eidMiss := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0.5, 5, 0}, Scale: mgl32.Vec3{scale, scale, scale}},
		&RigidBodyComponent{Velocity: mgl32.Vec3{0, -1, 0}, GravityScale: 0},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{0.15, 0.05, 0.05}},
		&VoxelModelComponent{VoxelModel: archId},
	)

	cmd.app.FlushCommands()
	tm := &Time{Dt: 0.1}

	// Tick (Move 0.1 per frame. 5.0 -> -1.0 = 6.0 distance. 60 frames.
	for i := 0; i < 60; i++ {
		PhysicsSystem(cmd, tm, physics, nil, assets)
	}

	// Verify HIT
	var trHit *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, t *TransformComponent) bool {
		if id == eidHit {
			trHit = t
		}
		return true
	})
	// Should stop. Spike top y=1. Entity half-height 0.5. Stop center at y=1.5.
	if trHit.Position.Y() < 1.4 {
		t.Errorf("Entity HIT should have collided! Y = %f (expected ~1.5)", trHit.Position.Y())
	}

	// Verify MISS
	var trMiss *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, t *TransformComponent) bool {
		if id == eidMiss {
			trMiss = t
		}
		return true
	})
	// Should fall past the spike.
	// Started at 5. Should be well below 0.
	if trMiss.Position.Y() > 0.0 {
		t.Errorf("Entity MISS should have fallen through hole! Y = %f (expected < 0)", trMiss.Position.Y())
	}
}
