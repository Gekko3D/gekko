package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestMovingBrushCollisionQueryBlocksAndSupportsCharacter(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	brush := cmd.AddEntity(
		&TransformComponent{},
		&MovingBrushComponent{
			BoundsCenter:      mgl32.Vec3{0, 0.5, 0},
			BoundsHalfExtents: mgl32.Vec3{1, 0.5, 1},
		},
	)
	app.FlushCommands()

	query := MovingBrushCollisionQuery(cmd)
	hit, blocked := CharacterMovementBlockHit(nil, mgl32.Vec3{-2, 0, 0}, mgl32.Vec3{2, 0, 0}, CharacterCollisionConfig{
		Radius:                0.25,
		Height:                1.7,
		DynamicCollisionQuery: query,
	}, nil)
	if !blocked || hit.Entity != brush {
		t.Fatalf("expected moving brush to block character, hit=%+v blocked=%v", hit, blocked)
	}

	ground, ok := CharacterGroundHitAtWithin(nil, mgl32.Vec3{0, 1, 0}, CharacterGroundProbeConfig{
		Radius:                0.25,
		StepHeight:            0.6,
		GroundProbe:           0.15,
		DynamicCollisionQuery: query,
	}, 0.1, 2, nil)
	if !ok || absf(ground.Y-1) > 0.001 {
		t.Fatalf("expected moving brush top at y=1 to support character, ground=%+v ok=%v", ground, ok)
	}
}

func TestMovingBrushCarriesSupportedPlayerAndNPC(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	brush := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{}},
		&MovingBrushComponent{
			BoundsCenter:       mgl32.Vec3{},
			BoundsHalfExtents:  mgl32.Vec3{1, 0.1, 1},
			ClosedPosition:     mgl32.Vec3{},
			ClosedBoundsCenter: mgl32.Vec3{},
			OpenOffset:         mgl32.Vec3{0, 1, 0},
			Speed:              1,
			Open:               true,
		},
	)
	player := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0.1, 0}},
		&LocalTransformComponent{Position: mgl32.Vec3{0, 0.1, 0}},
		&CameraComponent{},
		&GroundedPlayerControllerComponent{Radius: 0.35, Height: 1.8, EyeHeight: 1.5},
	)
	npc := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0.5, 0.1, 0}},
		&LocalTransformComponent{Position: mgl32.Vec3{0.5, 0.1, 0}},
		&NPCComponent{},
	)
	nonRider := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{2, 0.1, 0}},
		&NPCComponent{},
	)
	app.FlushCommands()

	movingBrushMotionSystem(cmd, &Time{Dt: 1})

	if got := transformForEntityMust(t, cmd, player).Position; got != (mgl32.Vec3{0, 1.1, 0}) {
		t.Fatalf("player position = %v, want elevator carry", got)
	}
	if got := transformForEntityMust(t, cmd, npc).Position; got != (mgl32.Vec3{0.5, 1.1, 0}) {
		t.Fatalf("npc position = %v, want elevator carry", got)
	}
	if got := transformForEntityMust(t, cmd, nonRider).Position; got != (mgl32.Vec3{2, 0.1, 0}) {
		t.Fatalf("non-rider position = %v, want unchanged", got)
	}
	ctrl := cmd.GetComponent(player, reflect.TypeOf(GroundedPlayerControllerComponent{})).(*GroundedPlayerControllerComponent)
	if !ctrl.Grounded || ctrl.VerticalVelocity != 0 {
		t.Fatalf("expected carried player to remain grounded, got %+v", *ctrl)
	}
	if !ctrl.HasGroundPoint || ctrl.GroundPoint != (mgl32.Vec3{0, 1.1, 0}) {
		t.Fatalf("expected carried player support to move with brush, got %+v", *ctrl)
	}
	if ctrl.GroundContactCount != 1 || ctrl.GroundContacts[0].Entity != brush {
		t.Fatalf("expected carried player to retain moving support identity, got %+v", *ctrl)
	}
}

func TestGroundedPlayerLandsOnMovingBrushWithoutStaticVoxelWorld(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{},
		&MovingBrushComponent{
			BoundsCenter:      mgl32.Vec3{0, 0.5, 0},
			BoundsHalfExtents: mgl32.Vec3{1, 0.5, 1},
		},
	)
	player := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 1.05, 0}},
		&LocalTransformComponent{Position: mgl32.Vec3{0, 1.05, 0}},
		&CameraComponent{},
		&GroundedPlayerControllerComponent{Radius: 0.35, Height: 1.8, EyeHeight: 1.5},
	)
	app.FlushCommands()

	groundedPlayerControlSystem(cmd, &Time{Dt: 0.1}, nil, nil)

	ctrl := cmd.GetComponent(player, reflect.TypeOf(GroundedPlayerControllerComponent{})).(*GroundedPlayerControllerComponent)
	if !ctrl.Grounded {
		t.Fatalf("expected moving brush to ground player, got %+v", *ctrl)
	}
	if got := transformForEntityMust(t, cmd, player).Position.Y(); got < 1 || got > 1.02 {
		t.Fatalf("player base y = %v, want moving brush top", got)
	}
}
