package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestTriggerVolumeReportsOverlapWithoutPhysicalResolution(t *testing.T) {
	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	triggerHalfExtents := mgl32.Vec3{2, 2, 2}
	bodyHalfExtents := mgl32.Vec3{0.5, 0.5, 0.5}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{IsTrigger: true, CollisionLayer: DefaultCollisionLayer, CollisionMask: AllCollisionLayers},
		&PhysicsModel{
			Boxes: []CollisionBox{{
				HalfExtents: triggerHalfExtents,
			}},
		},
	)

	bodyID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 6, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, GravityScale: 1, LinearDamping: 0.01, AngularDamping: 0.01},
		&ColliderComponent{Friction: 0.3, Restitution: 0.0},
		&PhysicsModel{
			Boxes: []CollisionBox{{
				HalfExtents: bodyHalfExtents,
			}},
		},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 220)

	position, ok := testEntityPosition(cmd, bodyID)
	if !ok {
		t.Fatal("expected dynamic body transform after trigger test")
	}
	if position.Y() >= -2.0 {
		t.Fatalf("expected body to pass through trigger volume without physical resolution, got %v", position)
	}

	events := proxy.DrainCollisionEvents()
	if len(events) == 0 {
		t.Fatal("expected trigger overlap events")
	}

	var sawTriggerEnter bool
	var sawTriggerExit bool
	for _, event := range events {
		if !event.IsTrigger {
			continue
		}
		if event.Type == CollisionEventEnter {
			sawTriggerEnter = true
		}
		if event.Type == CollisionEventExit {
			sawTriggerExit = true
		}
	}

	if !sawTriggerEnter {
		t.Fatal("expected trigger enter event")
	}
	if !sawTriggerExit {
		t.Fatal("expected trigger exit event")
	}
}
