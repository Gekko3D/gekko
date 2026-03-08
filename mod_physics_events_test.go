package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestPhysicsProxyDrainCollisionEvents(t *testing.T) {
	proxy := &PhysicsProxy{}
	results := &PhysicsResults{
		Tick: 1,
		Collisions: []PhysicsCollisionEvent{{
			Type:          CollisionEventEnter,
			A:             2,
			B:             5,
			Point:         mgl32.Vec3{1, 2, 3},
			Normal:        mgl32.Vec3{0, 1, 0},
			Penetration:   0.25,
			NormalImpulse: 4,
			RelativeSpeed: 3,
			Tick:          1,
		}},
	}

	proxy.captureCollisionResults(results)
	proxy.captureCollisionResults(results)

	events := proxy.DrainCollisionEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 collision event, got %d", len(events))
	}
	if events[0].A != 2 || events[0].B != 5 {
		t.Fatalf("unexpected collision pair: %+v", events[0])
	}
	if drainedAgain := proxy.DrainCollisionEvents(); len(drainedAgain) != 0 {
		t.Fatalf("expected buffer to be empty after drain, got %d events", len(drainedAgain))
	}
}

func TestOrderedCollisionPair(t *testing.T) {
	pair := orderedCollisionPair(9, 3)
	if pair.A != 3 || pair.B != 9 {
		t.Fatalf("expected ordered pair 3,9; got %+v", pair)
	}
}
