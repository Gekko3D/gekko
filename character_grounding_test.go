package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestUpdateCharacterGroundVisualYSmoothsSmallGroundChanges(t *testing.T) {
	state := CharacterGroundVisualState{}
	ResetCharacterGroundVisualY(&state, 1)

	got := UpdateCharacterGroundVisualY(&state, 1.25, 1.0/60.0, CharacterGroundVisualConfig{
		SmoothingSpeed: 12,
		SnapDistance:   0.75,
	})
	if got <= 1 || got >= 1.25 {
		t.Fatalf("expected visual ground to move gradually toward target, got %.4f", got)
	}
	if state.RawY != 1.25 {
		t.Fatalf("expected raw ground to record target, got %.4f", state.RawY)
	}
}

func TestUpdateCharacterGroundVisualYSnapsLargeGroundChanges(t *testing.T) {
	state := CharacterGroundVisualState{}
	ResetCharacterGroundVisualY(&state, 1)

	got := UpdateCharacterGroundVisualY(&state, 3, 1.0/60.0, CharacterGroundVisualConfig{
		SmoothingSpeed: 12,
		SnapDistance:   0.75,
	})
	if got != 3 {
		t.Fatalf("expected large ground change to snap, got %.4f", got)
	}
}

func TestUpdateCharacterGroundVisualYIgnoresDeadbandNoise(t *testing.T) {
	state := CharacterGroundVisualState{}
	ResetCharacterGroundVisualY(&state, 1)

	for _, target := range []float32{1.01, 0.99, 1.02, 0.985, 1.005} {
		got := UpdateCharacterGroundVisualY(&state, target, 1.0/60.0, CharacterGroundVisualConfig{
			SmoothingSpeed: 8,
			SnapDistance:   0.75,
			Deadband:       0.025,
		})
		if got != 1 {
			t.Fatalf("expected deadband to keep visual ground stable for target %.4f, got %.4f", target, got)
		}
	}
	if state.RawY != 1.005 {
		t.Fatalf("expected raw ground to keep tracking latest sample, got %.4f", state.RawY)
	}
}

func TestCharacterGroundProbeOffsetsIncludeCenterAndFootprint(t *testing.T) {
	offsets := CharacterGroundProbeOffsets(0.4)
	if len(offsets) != 5 {
		t.Fatalf("expected four footprint probes plus center, got %d", len(offsets))
	}
	center := offsets[len(offsets)-1]
	if center.X() != 0 || center.Y() != 0 || center.Z() != 0 {
		t.Fatalf("expected last probe to be center, got %v", center)
	}
}

func TestApplyCharacterVisualGroundOffsetToChildrenUpdatesDirectVisualChildren(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	parent := cmd.AddEntity(&TransformComponent{Scale: mgl32.Vec3{1, 1, 1}})
	child := cmd.AddEntity(
		&Parent{Entity: parent},
		&LocalTransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
	)
	app.FlushCommands()

	ApplyCharacterVisualGroundOffsetToChildren(cmd, parent, -0.125)

	local := cmd.GetComponent(child, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
	if local.Position.Y() != -0.125 {
		t.Fatalf("expected child visual offset to update, got %v", local.Position)
	}
}
