package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestPhysicsSnapshotSkipsPresentationOnlyBodies(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	presentationEntity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{BodyMode: BodyModePresentationOnly, Mass: 1},
		&ColliderComponent{Shape: ShapeSphere, Radius: 1},
	)
	app.FlushCommands()

	snapshot, entities := collectPhysicsSnapshot(cmd, &Time{Dt: 1.0 / 60.0}, NewPhysicsWorld(), nil)
	if len(snapshot.Entities) != 0 {
		t.Fatalf("expected presentation-only body to be skipped, got %d snapshot entities", len(snapshot.Entities))
	}
	if _, ok := entities[presentationEntity]; ok {
		t.Fatal("expected presentation-only body to be absent from physics entity refs")
	}
}

func TestPhysicsSimulatorDoesNotIntegrateKinematicBodies(t *testing.T) {
	world := NewPhysicsWorld()
	world.UpdateFrequency = 60
	simulator := NewPhysicsSimulator(world.SpatialGridCellSize)
	proxy := &PhysicsProxy{}

	startPos := mgl32.Vec3{10, 0, 0}
	proxy.pendingState.Store(&PhysicsSnapshot{
		Dt: 1.0 / 60.0,
		Entities: []PhysicsEntityState{
			{
				Eid:      EntityId(1),
				Pos:      startPos,
				Rot:      mgl32.QuatIdent(),
				Vel:      mgl32.Vec3{60, 0, 0},
				BodyMode: BodyModeKinematic,
				Mass:     1,
				Shape:    ShapeSphere,
				Radius:   1,
			},
		},
	})

	results := simulator.Step(world, proxy)
	if len(results.Entities) != 1 {
		t.Fatalf("expected one physics result, got %d", len(results.Entities))
	}
	if got := results.Entities[0].Pos; got != startPos {
		t.Fatalf("expected kinematic body to remain at %v, got %v", startPos, got)
	}
}

func TestKinematicBodyFollowsSlowExternallyAuthoredPose(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{BodyMode: BodyModeKinematic, Mass: 1},
		&ColliderComponent{Shape: ShapeSphere, Radius: 1},
	)
	app.FlushCommands()

	world := NewPhysicsWorld()
	world.UpdateFrequency = 60
	simulator := NewPhysicsSimulator(world.SpatialGridCellSize)
	proxy := &PhysicsProxy{}
	time := &Time{Dt: 1.0 / 60.0}

	snapshot, _ := collectPhysicsSnapshot(cmd, time, world, nil)
	proxy.pendingState.Store(snapshot)
	if results := simulator.Step(world, proxy); len(results.Entities) != 1 || results.Entities[0].Pos != (mgl32.Vec3{}) {
		t.Fatalf("expected initial kinematic result at origin, got %+v", results.Entities)
	}
	PhysicsPullSystem(cmd, time, proxy, world)

	var transform *TransformComponent
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid != entity {
			return true
		}
		transform = tr
		return false
	})
	if transform == nil {
		t.Fatal("expected kinematic transform")
	}

	transform.Position = mgl32.Vec3{0.02, 0, 0}
	snapshot, _ = collectPhysicsSnapshot(cmd, time, world, nil)
	if len(snapshot.Entities) != 1 {
		t.Fatalf("expected one snapshot entity, got %d", len(snapshot.Entities))
	}
	if snapshot.Entities[0].Teleport {
		t.Fatal("expected slow authored kinematic movement to stay below teleport threshold")
	}
	if got := snapshot.Entities[0].Pos; got != transform.Position {
		t.Fatalf("expected snapshot to carry authored kinematic pose %v, got %v", transform.Position, got)
	}
	proxy.pendingState.Store(snapshot)
	results := simulator.Step(world, proxy)
	if len(results.Entities) != 1 {
		t.Fatalf("expected one physics result, got %d", len(results.Entities))
	}
	if got := results.Entities[0].Pos; got != transform.Position {
		t.Fatalf("expected internal kinematic body to follow authored pose %v, got %v", transform.Position, got)
	}
}
