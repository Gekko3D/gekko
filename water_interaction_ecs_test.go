package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
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
	if events[0].Radius != 0.35 || events[0].Kind != WaterDisturbanceImpact {
		t.Fatalf("unexpected impact metadata: %+v", events[0])
	}
	ripples := interactions.ActiveRipples()
	if len(ripples) != 1 {
		t.Fatalf("expected one active ripple, got %d", len(ripples))
	}
	if ripples[0].Radius != 0.35 || ripples[0].Kind != WaterDisturbanceImpact {
		t.Fatalf("unexpected ripple metadata: %+v", ripples[0])
	}
}

func TestWaterInteractionSystemEmitsRateLimitedWakeForMovingBodyInWater(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	interactions := &WaterInteractionState{}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 3, 5}},
		&WaterSurfaceComponent{
			HalfExtents: [2]float32{3, 3},
			Depth:       2,
		},
	)
	body := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{5, 2.9, 5},
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&RigidBodyComponent{
			Mass:     1,
			Velocity: mgl32.Vec3{4, 0, 0},
		},
		&ColliderComponent{
			Shape:       ShapeBox,
			HalfExtents: mgl32.Vec3{0.35, 0.35, 0.35},
		},
	)
	app.FlushCommands()

	waterInteractionSystem(cmd, &Time{Dt: 1.0 / 60.0}, interactions)
	if len(interactions.ActiveRipples()) != 0 {
		t.Fatal("expected no wake before the body has a previous in-water sample")
	}

	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid == body {
			tr.Position = mgl32.Vec3{5.5, 2.9, 5}
			return false
		}
		return true
	})

	waterInteractionSystem(cmd, &Time{Dt: 0.5}, interactions)
	ripples := interactions.ActiveRipples()
	if len(ripples) != 1 {
		t.Fatalf("expected one wake ripple, got %d", len(ripples))
	}
	if ripples[0].Kind != WaterDisturbanceWake {
		t.Fatalf("expected wake disturbance, got %+v", ripples[0])
	}
	if got := interactions.ImpactEvents(); len(got) != 0 {
		t.Fatalf("expected wake not to emit splash impact events, got %d", len(got))
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

func TestBuildWaterSurfaceInputsIncludesRippleHosts(t *testing.T) {
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
			{
				WaterEntity:        waterA,
				Position:           mgl32.Vec3{0.5, 2, 0.5},
				Strength:           0.9,
				Age:                0.2,
				Lifetime:           2.0,
				Radius:             0.6,
				HorizontalVelocity: [2]float32{2, -1},
				Foam:               0.35,
				Kind:               WaterDisturbanceSkim,
			},
			{WaterEntity: EntityId(9999), Position: mgl32.Vec3{1, 2, 1}, Strength: 0.7, Age: 0.1, Lifetime: 2.0},
		},
	}

	hosts, ripples := buildWaterSurfaceInputs(cmd, interactions)
	if len(hosts) != 2 {
		t.Fatalf("expected two water hosts, got %d", len(hosts))
	}
	if len(ripples) != 1 {
		t.Fatalf("expected one mapped ripple host, got %d", len(ripples))
	}
	if ripples[0].WaterIndex != 0 {
		t.Fatalf("expected ripple to map to first sorted water host, got %d", ripples[0].WaterIndex)
	}
	if ripples[0].Radius != 0.6 || ripples[0].HorizontalVelocity != ([2]float32{2, -1}) || ripples[0].Foam != 0.35 || ripples[0].DisturbanceKind != uint32(WaterDisturbanceSkim) {
		t.Fatalf("unexpected mapped ripple metadata: %+v", ripples[0])
	}
}

func TestWaterInteractionSystemWorksWithResolvedWaterPatch(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	interactions := &WaterInteractionState{}

	patch := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{5, 3, 5},
		},
		&ResolvedWaterPatchComponent{
			Owner:       42,
			PatchIndex:  0,
			Kind:        WaterPatchKindSurface,
			Center:      mgl32.Vec3{5, 3, 5},
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
	if events[0].WaterEntity != patch {
		t.Fatalf("expected impact to target resolved patch entity %d, got %d", patch, events[0].WaterEntity)
	}
}

func TestHiddenWaterVolumeIsQueryableWithoutSurfaceEffects(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	interactions := &WaterInteractionState{}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 3, 5}},
		&ResolvedWaterPatchComponent{
			Owner:             1,
			Kind:              WaterPatchKindSurface,
			Center:            mgl32.Vec3{5, 3, 5},
			HalfExtents:       [2]float32{2, 2},
			Depth:             2,
			SurfaceVisibility: WaterSurfaceVisibilityHidden,
		},
	)
	body := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 4, 5}, Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{Mass: 1, Velocity: mgl32.Vec3{0, -6, 0}},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.35, 0.35, 0.35}},
	)
	app.FlushCommands()

	if hits := WaterVolumesAt(cmd, mgl32.Vec3{5, 2.5, 5}, 0.1); len(hits) != 1 || hits[0].SurfaceVisible {
		t.Fatalf("hidden water volume query = %+v", hits)
	}
	if hosts, _ := buildWaterSurfaceInputs(cmd, interactions); len(hosts) != 0 {
		t.Fatalf("hidden water must not reach renderer: %+v", hosts)
	}
	waterInteractionSystem(cmd, &Time{Dt: 1.0 / 60.0}, interactions)
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid == body {
			tr.Position = mgl32.Vec3{5, 2.4, 5}
		}
		return true
	})
	waterInteractionSystem(cmd, &Time{Dt: 1.0 / 60.0}, interactions)
	if len(interactions.ImpactEvents()) != 0 || len(interactions.ActiveRipples()) != 0 {
		t.Fatalf("hidden water emitted surface effects: impacts=%+v ripples=%+v", interactions.ImpactEvents(), interactions.ActiveRipples())
	}
}

func TestBuildUnderwaterInputUsesStableVolumeMedium(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{2, 2}, Depth: 4, VolumeGroup: "water-ramp", Color: [3]float32{1, 0, 0}, AbsorptionColor: [3]float32{1, 1, 1}, ScatteringStrength: 0.4},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 4, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{4, 4}, Depth: 6, VolumeGroup: "water-ramp", Color: [3]float32{0, 0.5, 1}, AbsorptionColor: [3]float32{0.2, 0.4, 0.6}},
	)
	app.FlushCommands()

	input := buildUnderwaterInput(cmd, mgl32.Vec3{0, 2.5, 0}, &Time{Elapsed: 3})
	if input.Color != ([3]float32{1, 0, 0}) || input.AbsorptionColor != ([3]float32{1, 1, 1}) || input.ScatteringStrength != 0.4 || input.Strength != 1 || input.Time != 3 {
		t.Fatalf("underwater input = %+v", input)
	}
}

func TestBuildUnderwaterInputBridgesNarrowVolumeSeam(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	for _, x := range []float32{-1, 1} {
		cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{x, 3, 0}, Scale: mgl32.Vec3{1, 1, 1}},
			&WaterSurfaceComponent{HalfExtents: [2]float32{0.75, 1}, Depth: 3, VolumeGroup: "pool"},
		)
	}
	app.FlushCommands()

	if input := buildUnderwaterInput(cmd, mgl32.Vec3{0, 2, 0}, nil); input.Strength != 1 {
		t.Fatalf("underwater input at narrow seam = %+v", input)
	}
}

func TestSmoothUnderwaterInputFadesOnlyAtMediumBoundary(t *testing.T) {
	state := &VoxelRtState{}
	entered := smoothUnderwaterInput(state, app_rt.UnderwaterInput{Strength: 1, Color: [3]float32{0, 0.5, 1}}, 0.1)
	seam := smoothUnderwaterInput(state, app_rt.UnderwaterInput{Strength: 1, Color: [3]float32{0, 0.5, 1}}, 0.1)
	left := smoothUnderwaterInput(state, app_rt.UnderwaterInput{}, 0.1)
	if entered.Strength <= 0 || seam.Strength <= entered.Strength || left.Strength <= 0 || left.Strength >= seam.Strength {
		t.Fatalf("underwater fade entered=%v seam=%v left=%v", entered.Strength, seam.Strength, left.Strength)
	}
}

func TestBuildUnderwaterInputReducesOnlyOpenSurfaceDistortion(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 3, 0}, Scale: mgl32.Vec3{1, 1, 1}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{2, 2}, Depth: 3, VolumeGroup: "pool"},
	)
	app.FlushCommands()
	nearSurface := buildUnderwaterInput(cmd, mgl32.Vec3{0, 2.95, 0}, nil)
	if nearSurface.NearSurface <= 0 || nearSurface.NearSurface >= 1 {
		t.Fatalf("open near-surface factor = %v", nearSurface.NearSurface)
	}
	MakeQuery1[WaterSurfaceComponent](cmd).Map(func(eid EntityId, water *WaterSurfaceComponent) bool {
		if eid == entity {
			water.SurfaceVisibility = WaterSurfaceVisibilityHidden
		}
		return true
	})
	sealed := buildUnderwaterInput(cmd, mgl32.Vec3{0, 2.95, 0}, nil)
	if sealed.NearSurface != 1 {
		t.Fatalf("sealed near-surface factor = %v", sealed.NearSurface)
	}
}
