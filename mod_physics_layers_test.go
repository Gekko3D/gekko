package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func runCollisionLayerScene(t *testing.T, dynamicCollider ColliderComponent) (mgl32.Vec3, []PhysicsCollisionEvent) {
	t.Helper()

	cmd, _, world, proxy, sim, timeRes := newPhysicsSceneHarness()
	world.UpdateFrequency = 120
	timeRes.Dt = 1.0 / float64(world.UpdateFrequency)

	floorHalfExtents := mgl32.Vec3{6, 1, 6}
	bodyHalfExtents := mgl32.Vec3{0.5, 0.5, 0.5}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, -1, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Friction: 0.6, Restitution: 0.0},
		&PhysicsModel{
			Boxes: []CollisionBox{{
				HalfExtents: floorHalfExtents,
			}},
		},
	)

	bodyID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, GravityScale: 1, LinearDamping: 0.01, AngularDamping: 0.01},
		&dynamicCollider,
		&PhysicsModel{
			Boxes: []CollisionBox{{
				HalfExtents: bodyHalfExtents,
			}},
		},
	)
	cmd.app.FlushCommands()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 180)
	position, ok := testEntityPosition(cmd, bodyID)
	if !ok {
		t.Fatal("expected dynamic body transform after stepping")
	}
	return position, proxy.DrainCollisionEvents()
}

func TestCollisionLayerDefaultsPreserveLegacyCollisions(t *testing.T) {
	position, events := runCollisionLayerScene(t, ColliderComponent{
		Friction:    0.3,
		Restitution: 0.0,
	})

	if position.Y() < -0.6 {
		t.Fatalf("expected default collision filters to stop the body on the floor, got position %v", position)
	}
	if len(events) == 0 {
		t.Fatal("expected collision events with default collision filters")
	}
}

func TestCollisionLayerMaskFilteringCanSkipPairs(t *testing.T) {
	position, events := runCollisionLayerScene(t, ColliderComponent{
		Friction:       0.3,
		Restitution:    0.0,
		CollisionLayer: 1 << 1,
		CollisionMask:  1 << 1,
	})

	if position.Y() > -2.0 {
		t.Fatalf("expected masked body to fall through the floor, got position %v", position)
	}
	if len(events) != 0 {
		t.Fatalf("expected no collision events for masked-out pair, got %d", len(events))
	}
}
