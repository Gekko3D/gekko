package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestWaterInteractionSystemEmitsImpactAndTracksRipple(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	interactions := &WaterInteractionState{}

	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{5, 3, 5},
		},
		&WaterSurfaceComponent{
			HalfExtents: [2]float32{2, 2},
			Depth:       2,
		},
	)
	body := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{5, 4, 5},
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&RigidBodyComponent{
			Mass:     1,
			Velocity: mgl32.Vec3{0, -6, 0},
		},
		&ColliderComponent{
			Shape:       ShapeBox,
			HalfExtents: mgl32.Vec3{0.35, 0.35, 0.35},
		},
	)
	app.FlushCommands()

	waterInteractionSystem(cmd, &Time{Dt: 1.0 / 60.0}, interactions)
	if got := interactions.ImpactEvents(); len(got) != 0 {
		t.Fatalf("expected no initial impact on first sample, got %d", len(got))
	}

	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid == body {
			tr.Position = mgl32.Vec3{5, 2.4, 5}
			return false
		}
		return true
	})

	waterInteractionSystem(cmd, &Time{Dt: 1.0 / 60.0}, interactions)
	events := interactions.ImpactEvents()
	if len(events) != 1 {
		t.Fatalf("expected one water impact event, got %d", len(events))
	}
	if events[0].Speed < 2.0 {
		t.Fatalf("expected meaningful impact speed, got %f", events[0].Speed)
	}
	if len(interactions.ActiveRipples()) != 1 {
		t.Fatalf("expected one active ripple, got %d", len(interactions.ActiveRipples()))
	}
}

func TestWaterInteractionCleanupSystemClearsImpacts(t *testing.T) {
	interactions := &WaterInteractionState{
		impactBuffer: []WaterImpactEvent{{Speed: 3}},
	}

	if len(interactions.ImpactEvents()) != 1 {
		t.Fatalf("expected one visible impact before cleanup")
	}

	waterInteractionCleanupSystem(interactions)

	if got := interactions.ImpactEvents(); len(got) != 0 {
		t.Fatalf("expected cleanup to clear impacts, got %d", len(got))
	}
}

func TestBuildWaterSurfaceHostsIncludesRippleHosts(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	waterA := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{2, 2}, Depth: 2},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{10, 2, 0}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{2, 2}, Depth: 2},
	)
	app.FlushCommands()

	interactions := &WaterInteractionState{
		activeRipples: []WaterRipple{
			{WaterEntity: waterA, Position: mgl32.Vec3{0.5, 2, 0.5}, Strength: 0.9, Age: 0.2, Lifetime: 2.0},
			{WaterEntity: EntityId(9999), Position: mgl32.Vec3{1, 2, 1}, Strength: 0.7, Age: 0.1, Lifetime: 2.0},
		},
	}

	hosts, ripples := buildWaterSurfaceHosts(cmd, interactions)
	if len(hosts) != 2 {
		t.Fatalf("expected two water hosts, got %d", len(hosts))
	}
	if len(ripples) != 1 {
		t.Fatalf("expected one mapped ripple host, got %d", len(ripples))
	}
	if ripples[0].WaterIndex != 0 {
		t.Fatalf("expected ripple to map to first sorted water host, got %d", ripples[0].WaterIndex)
	}
}
