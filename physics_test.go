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
		PhysicsSystem(cmd, tm, physics, nil)
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
		PhysicsSystem(cmd, tm, physics, nil)
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
		PhysicsSystem(cmd, tm, physics, nil)
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
		PhysicsSystem(cmd, tm, physics, nil)
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
		PhysicsSystem(cmd, tm, physics, nil)
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
