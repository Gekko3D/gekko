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
	center := offsets[0]
	if center.X() != 0 || center.Y() != 0 || center.Z() != 0 {
		t.Fatalf("expected first probe to be center, got %v", center)
	}
}

func TestCharacterGroundHitPublishesContactsAndPrefersCenterOnFlatGround(t *testing.T) {
	const support EntityId = 17
	base := mgl32.Vec3{3, 1, 4}
	hit, ok := CharacterGroundHitAtWithin(nil, base, CharacterGroundProbeConfig{
		Radius: 0.4,
		DynamicCollisionQuery: func(origin, _ mgl32.Vec3, _ float32, _ func(EntityId, bool) bool) RaycastHit {
			return RaycastHit{Hit: true, T: origin.Y() - base.Y(), Normal: mgl32.Vec3{0, 1, 0}, Entity: support}
		},
	}, 0.1, 2, nil)
	if !ok || hit.Point != base {
		t.Fatalf("flat ground chose footprint corner: hit=%+v ok=%v", hit, ok)
	}
	if hit.ContactCount != CharacterGroundContactCapacity {
		t.Fatalf("ground contacts=%d, want %d", hit.ContactCount, CharacterGroundContactCapacity)
	}
	for index := 0; index < hit.ContactCount; index++ {
		if hit.Contacts[index].Entity != support {
			t.Fatalf("contact %d lost support entity: %+v", index, hit.Contacts[index])
		}
	}

	hit, ok = CharacterGroundHitAtWithin(nil, base, CharacterGroundProbeConfig{
		Radius: 0.4,
		DynamicCollisionQuery: func(origin, _ mgl32.Vec3, _ float32, _ func(EntityId, bool) bool) RaycastHit {
			y := base.Y()
			if origin.X() > base.X()+0.1 {
				y -= 0.5
			}
			return RaycastHit{Hit: true, T: origin.Y() - y, Normal: mgl32.Vec3{0, 1, 0}, Entity: support}
		},
	}, 0.1, 2, nil)
	if !ok || hit.ContactCount != 3 {
		t.Fatalf("lower non-bearing probes remained contacts: %+v", hit)
	}
	for index := 0; index < hit.ContactCount; index++ {
		if hit.Contacts[index].Point.Y() != base.Y() {
			t.Fatalf("contact %d retained lower floor: %+v", index, hit.Contacts[index])
		}
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
