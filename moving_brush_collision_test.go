package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
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
	environment := &groundedCharacterEnvironmentState{}
	environment.movingBrushQuery = environment.raycastMovingBrushes
	cmd.AddResources(environment)
	brush := cmd.AddEntity(
		&TransformComponent{},
		&MovingBrushComponent{
			BoundsHalfExtents: mgl32.Vec3{1, 0.1, 1},
			NavigationRole:    content.NavigationRoleCarrier,
			OpenOffset:        mgl32.Vec3{0, 1, 0}, Speed: 1, Open: true,
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
		&GroundedCharacterMotorComponent{Radius: 0.35, Height: 1.8},
	)
	nonRider := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{2, 0.1, 0}},
		&NPCComponent{},
		&GroundedCharacterMotorComponent{Radius: 0.35, Height: 1.8},
	)
	airborne := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-0.5, 0.5, 0}},
		&GroundedCharacterMotorComponent{Radius: 0.35, Height: 1.8, VerticalVelocity: 2},
	)
	otherBrush := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-0.5, 0.05, 0.7}},
		&MovingBrushComponent{BoundsCenter: mgl32.Vec3{-0.5, 0.05, 0.7}, BoundsHalfExtents: mgl32.Vec3{0.15, 0.1, 0.15}},
	)
	otherRider := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-0.5, 0.15, 0.7}},
		&GroundedCharacterMotorComponent{Radius: 0.1, Height: 1.8},
	)
	app.FlushCommands()

	// The motor probes live support before moving brushes in the normal update.
	groundedCharacterMotorSystem(cmd, &Time{Dt: 0.01}, nil, environment)
	for _, actor := range []EntityId{player, npc} {
		motor := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
		if !motor.Grounded || !motor.HasGroundPoint || motor.GroundContactCount == 0 {
			t.Fatalf("actor %d did not acquire live elevator support: %+v", actor, *motor)
		}
		for _, contact := range motor.GroundContacts[:motor.GroundContactCount] {
			if contact.Entity != brush {
				t.Fatalf("actor %d acquired another support: %+v", actor, contact)
			}
		}
	}
	otherMotor := cmd.GetComponent(otherRider, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
	if !otherMotor.Grounded || otherMotor.GroundContactCount == 0 || otherMotor.GroundContacts[0].Entity != otherBrush {
		t.Fatalf("other rider did not acquire its own support: %+v", *otherMotor)
	}
	for _, actor := range []EntityId{nonRider, airborne} {
		motor := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
		if motor.Grounded {
			t.Fatalf("unsupported actor %d unexpectedly grounded: %+v", actor, *motor)
		}
	}
	unsupportedPositions := map[EntityId]mgl32.Vec3{}
	for _, actor := range []EntityId{nonRider, airborne, otherRider} {
		unsupportedPositions[actor] = transformForEntityMust(t, cmd, actor).Position
	}

	movingBrushMotionSystem(cmd, &Time{Dt: 1})

	for actor, want := range map[EntityId]mgl32.Vec3{player: {0, 1.1, 0}, npc: {0.5, 1.1, 0}} {
		if got := transformForEntityMust(t, cmd, actor).Position; got.Sub(want).Len() > 0.001 {
			t.Fatalf("actor %d position = %v, want elevator carry to %v", actor, got, want)
		}
		local := cmd.GetComponent(actor, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
		if local.Position.Sub(want).Len() > 0.001 {
			t.Fatalf("actor %d local position = %v, want %v", actor, local.Position, want)
		}
		motor := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
		if !motor.Grounded || motor.VerticalVelocity != 0 || !motor.HasGroundPoint || motor.GroundPoint.Sub(want).Len() > 0.001 {
			t.Fatalf("actor %d lost carried support: %+v", actor, *motor)
		}
		if motor.GroundContactCount == 0 {
			t.Fatalf("actor %d lost support contacts", actor)
		}
		for _, contact := range motor.GroundContacts[:motor.GroundContactCount] {
			if contact.Entity != brush || absf(contact.Point.Y()-1.1) > 0.001 {
				t.Fatalf("actor %d support contact did not move with elevator: %+v", actor, contact)
			}
		}
	}
	camera := cmd.GetComponent(player, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
	if camera.Position.Sub(mgl32.Vec3{0, 2.6, 0}).Len() > 0.001 {
		t.Fatalf("camera position = %v, want carried eye position", camera.Position)
	}
	for actor, want := range unsupportedPositions {
		if got := transformForEntityMust(t, cmd, actor).Position; got != want {
			t.Fatalf("unsupported actor %d was carried from %v to %v", actor, want, got)
		}
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
