package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

// Observe committed values through public queries; revision stamps are opaque.
func s3mStep(t *testing.T, cmd *Commands, call func(), worldIDs, localIDs []EntityId) {
	t.Helper()
	beforeValues := s3lSnapshot(cmd)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	call()
	afterValues := s3lSnapshot(cmd)
	wantRevision := [4]int{}
	for index, ids := range [2][]EntityId{worldIDs, localIDs} {
		want, got := map[EntityId]bool{}, map[EntityId]bool{}
		for _, id := range ids {
			want[id] = true
		}
		if len(beforeValues[index]) != len(afterValues[index]) {
			t.Fatalf("component %d committed membership changed before flush", index)
		}
		for id, old := range beforeValues[index] {
			current, present := afterValues[index][id]
			if !present {
				t.Fatalf("component %d entity %d disappeared before flush", index, id)
			}
			if current != old {
				got[id] = true
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("component %d changed destinations=%v, want %v", index, got, want)
		}
		if len(ids) != 0 {
			wantRevision[index] = 1
		}
	}
	s3gPublications(t, cmd, before, structural, wantRevision)
}

func s3mPose(position mgl32.Vec3) TransformComponent {
	return TransformComponent{Position: position, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
}

func s3mTRS(t *testing.T, cmd *Commands, id EntityId, position mgl32.Vec3) {
	t.Helper()
	want := s3gBits(position, mgl32.QuatIdent(), mgl32.Vec3{1, 1, 1})
	for index, snapshot := range s3lSnapshot(cmd) {
		if got, present := snapshot[id]; present && got != want {
			t.Errorf("component %d entity %d TRS bits=%v, want %v", index, id, got, want)
		}
	}
}

func s3mCamera(t *testing.T, got CameraComponent, before CameraComponent, base mgl32.Vec3, eyeHeight float32) {
	t.Helper()
	if eyeHeight < .01 {
		eyeHeight = .01
	}
	want := before
	want.Position = base.Add(mgl32.Vec3{0, eyeHeight, 0})
	// Independently spell out the existing finite yaw/pitch direction.
	yaw, pitch := float64(before.Yaw)*math.Pi/180, float64(before.Pitch)*math.Pi/180
	forward := mgl32.Vec3{float32(math.Sin(yaw) * math.Cos(pitch)),
		float32(math.Sin(pitch)), -float32(math.Cos(yaw) * math.Cos(pitch))}
	want.LookAt = want.Position.Add(forward)
	want.Up = mgl32.Vec3{0, 1, 0}
	if got.Position != want.Position || got.Up != want.Up || got.LookAt.Sub(want.LookAt).Len() > 1e-6 {
		t.Errorf("camera pose=%+v, want %+v", got, want)
	}
	got.Position, got.LookAt, got.Up = before.Position, before.LookAt, before.Up
	if got != before {
		t.Error("camera application changed yaw/pitch or presentation settings")
	}
}

func TestS3mDirectIndependentDestinations(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	base := mgl32.Vec3{1, 2, 3}
	target := s3mPose(base)
	old := s3mPose(mgl32.Vec3{9, 8, 7})
	parentWorld := s3mPose(mgl32.Vec3{70, 80, 90})
	parentWorld.Scale = mgl32.Vec3{2, 3, 4}
	parent := cmd.AddEntity(parentWorld, s3gUnrelated{7})
	both := cmd.AddEntity(old, s3lLocal(old), Parent{Entity: parent}, s3gUnrelated{8})
	worldOnlyChanged := cmd.AddEntity(old, s3lLocal(target))
	localOnlyChanged := cmd.AddEntity(target, s3lLocal(old))
	worldOnly := cmd.AddEntity(old)
	localOnly := cmd.AddEntity(s3lLocal(old))
	neither := cmd.AddEntity(s3gUnrelated{9})
	quiet := cmd.AddEntity(target, s3lLocal(target))
	// Same position still repairs representationally different rotation/scale.
	repair := target
	repair.Rotation = mgl32.Quat{W: -1}
	repair.Scale = mgl32.Vec3{2, 3, 4}
	repair.Pivot = mgl32.Vec3{5, 6, 7}
	stationary := cmd.AddEntity(repair, s3lLocal(repair))
	nanRepair := target
	nanRepair.Rotation.W = math.Float32frombits(0x7fc01234)
	nanRepair.Scale[1] = math.Float32frombits(0x7fc01235)
	nonfinite := cmd.AddEntity(nanRepair, s3lLocal(nanRepair))
	leaf := cmd.AddEntity(old, s3lLocal(old), Parent{Entity: both})
	app.FlushCommands()
	ctrl := &GroundedPlayerControllerComponent{}
	ids := []EntityId{both, worldOnlyChanged, localOnlyChanged, worldOnly, localOnly, neither, quiet, stationary, nonfinite}
	call := func() {
		for _, id := range ids {
			groundedPlayerApplyTransform(cmd, id, nil, ctrl, base)
		}
	}
	s3mStep(t, cmd, call, []EntityId{both, worldOnlyChanged, worldOnly, stationary, nonfinite}, []EntityId{both, localOnlyChanged, localOnly, stationary, nonfinite})
	for _, id := range ids {
		s3mTRS(t, cmd, id, base)
		if cmd.GetComponent(id, reflect.TypeOf(GroundedCharacterMotorComponent{})) != nil || cmd.GetComponent(id, reflect.TypeOf(CameraComponent{})) != nil {
			t.Fatal("direct helper created motor or camera")
		}
	}
	if *s3cComponent[TransformComponent](t, cmd, parent) != parentWorld || *s3cComponent[TransformComponent](t, cmd, leaf) != old || *s3cComponent[LocalTransformComponent](t, cmd, leaf) != s3lLocal(old) {
		t.Fatal("helper propagated hierarchy or changed unrelated transforms")
	}
	if s3cComponent[Parent](t, cmd, both).Entity != parent || s3cComponent[Parent](t, cmd, leaf).Entity != both || s3cComponent[TransformComponent](t, cmd, stationary).Pivot != repair.Pivot || s3cComponent[s3gUnrelated](t, cmd, both).Value != 8 {
		t.Fatal("helper changed Pivot, Parent or unrelated fields")
	}
	s3mStep(t, cmd, call, nil, nil)
	// A live external edit must be compared against the actual destination.
	s3cComponent[TransformComponent](t, cmd, both).Position[0] = 99
	s3cComponent[LocalTransformComponent](t, cmd, localOnly).Scale[2] = 8
	s3mStep(t, cmd, func() { groundedPlayerApplyTransform(cmd, both, nil, ctrl, base) }, []EntityId{both}, nil)
	s3mStep(t, cmd, func() { groundedPlayerApplyTransform(cmd, localOnly, nil, ctrl, base) }, nil, []EntityId{localOnly})
	s3mStep(t, cmd, call, nil, nil)
}

func TestS3mCameraAndSkippedTargets(t *testing.T) {
	for _, kind := range []string{"both", "camera only", "unknown entity", "nil camera", "nil commands", "nil controller"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			old := s3mPose(mgl32.Vec3{9, 8, 7})
			before := CameraComponent{Position: mgl32.Vec3{50, 60, 70}, LookAt: mgl32.Vec3{1, 0, 0}, Up: mgl32.Vec3{1, 0, 0}, Yaw: .7, Pitch: -.2, Fov: 63, Aspect: 1.4, Near: .02, Far: 900}
			original := before
			var id EntityId
			if kind == "camera only" {
				id = cmd.AddEntity(before)
			} else {
				id = cmd.AddEntity(old, s3lLocal(old), before)
			}
			app.FlushCommands()
			cam := s3cComponent[CameraComponent](t, cmd, id)
			passedCamera, targetID, passedCmd := cam, id, cmd
			ctrl := &GroundedPlayerControllerComponent{EyeHeight: -.5}
			base := mgl32.Vec3{1, 2, 3}
			if kind == "unknown entity" {
				targetID = EntityId(999999)
				passedCamera = &before
			}
			if kind == "nil camera" {
				passedCamera = nil
			}
			if kind == "nil commands" {
				passedCmd = nil
			}
			if kind == "nil controller" {
				ctrl = nil
			}
			var changed []EntityId
			if kind == "both" || kind == "nil camera" {
				changed = []EntityId{id}
			}
			s3mStep(t, cmd, func() { groundedPlayerApplyTransform(passedCmd, targetID, passedCamera, ctrl, base) }, changed, changed)
			if kind == "both" || kind == "camera only" || kind == "unknown entity" {
				s3mCamera(t, *passedCamera, original, base, -.5)
			}
			if kind == "nil commands" || kind == "nil controller" || kind == "nil camera" || kind == "unknown entity" {
				if *cam != original {
					t.Fatal("helper changed unsupplied or skipped committed camera")
				}
			}
			if kind == "both" || kind == "nil camera" {
				s3mTRS(t, cmd, id, base)
			}
			if kind == "unknown entity" && cmd.EntityExists(targetID) {
				t.Fatal("unknown entity created")
			}
		})
	}
}

func TestS3mExactPositionBitsAndPreservedPivot(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	pose := s3mPose(mgl32.Vec3{})
	pose.Pivot[0] = math.Float32frombits(0x7fc04567)
	id := cmd.AddEntity(pose, s3lLocal(pose))
	app.FlushCommands()
	ctrl := &GroundedPlayerControllerComponent{}
	for _, bits := range []uint32{0x80000000, 0x80000000, 0x7fc01234, 0x7fc01234, 0x7fc01235} {
		base := mgl32.Vec3{math.Float32frombits(bits), 0, 0}
		var changed []EntityId
		if math.Float32bits(s3cComponent[TransformComponent](t, cmd, id).Position[0]) != bits {
			changed = []EntityId{id}
		}
		s3mStep(t, cmd, func() { groundedPlayerApplyTransform(cmd, id, nil, ctrl, base) }, changed, changed)
		s3mTRS(t, cmd, id, base)
		if math.Float32bits(s3cComponent[TransformComponent](t, cmd, id).Pivot[0]) != 0x7fc04567 {
			t.Fatal("helper changed preserved NaN Pivot bits")
		}
	}
}

func TestS3mQueuedTransformBoundaries(t *testing.T) {
	for _, operation := range []string{"replace both", "remove World", "remove Local", "remove entity", "add World", "add Local", "new entity"} {
		t.Run(operation, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			old := s3mPose(mgl32.Vec3{9, 8, 7})
			var id EntityId
			switch operation {
			case "add World":
				id = cmd.AddEntity(s3lLocal(old))
			case "add Local":
				id = cmd.AddEntity(old)
			default:
				id = cmd.AddEntity(old, s3lLocal(old))
			}
			app.FlushCommands()
			queuedPose := s3mPose(mgl32.Vec3{80, 81, 82})
			var pending EntityId
			switch operation {
			case "replace both":
				cmd.AddComponents(id, queuedPose, s3lLocal(queuedPose))
			case "remove World":
				cmd.RemoveComponents(id, TransformComponent{})
			case "remove Local":
				cmd.RemoveComponents(id, LocalTransformComponent{})
			case "remove entity":
				cmd.RemoveEntity(id)
			case "add World":
				cmd.AddComponents(id, queuedPose)
			case "add Local":
				cmd.AddComponents(id, s3lLocal(queuedPose))
			case "new entity":
				pending = cmd.AddEntity(queuedPose, s3lLocal(queuedPose))
			}
			worldIDs, localIDs := []EntityId{id}, []EntityId{id}
			if operation == "add World" {
				worldIDs = nil
			}
			if operation == "add Local" {
				localIDs = nil
			}
			base := mgl32.Vec3{1, 2, 3}
			s3mStep(t, cmd, func() {
				groundedPlayerApplyTransform(cmd, id, nil, &GroundedPlayerControllerComponent{}, base)
				if pending != 0 {
					groundedPlayerApplyTransform(cmd, pending, nil, &GroundedPlayerControllerComponent{}, base)
				}
			}, worldIDs, localIDs)
			s3mTRS(t, cmd, id, base)
			if !cmd.EntityExists(id) || pending != 0 && cmd.EntityExists(pending) {
				t.Fatal("helper changed committed entity membership before flush")
			}
			app.FlushCommands()
			switch operation {
			case "replace both":
				s3cComponent[TransformComponent](t, cmd, id)
				s3cComponent[LocalTransformComponent](t, cmd, id)
				s3mTRS(t, cmd, id, queuedPose.Position)
			case "remove World":
				if cmd.GetComponent(id, s3gTypes[0]) != nil {
					t.Fatal("queued World removal lost")
				}
			case "remove Local":
				if cmd.GetComponent(id, s3gTypes[1]) != nil {
					t.Fatal("queued Local removal lost")
				}
			case "remove entity":
				if cmd.EntityExists(id) {
					t.Fatal("queued entity removal lost")
				}
			case "add World":
				if s3cComponent[TransformComponent](t, cmd, id).Position != queuedPose.Position || s3cComponent[LocalTransformComponent](t, cmd, id).Position != base {
					t.Fatal("queued World addition lost or changed existing Local")
				}
			case "add Local":
				if s3cComponent[LocalTransformComponent](t, cmd, id).Position != queuedPose.Position || s3cComponent[TransformComponent](t, cmd, id).Position != base {
					t.Fatal("queued Local addition lost or changed existing World")
				}
			case "new entity":
				if !cmd.EntityExists(pending) {
					t.Fatal("queued entity insertion lost")
				}
				s3cComponent[TransformComponent](t, cmd, pending)
				s3cComponent[LocalTransformComponent](t, cmd, pending)
				s3mTRS(t, cmd, pending, queuedPose.Position)
			}
		})
	}
}

func TestS3mActualMotorCallers(t *testing.T) {
	t.Run("normal camera free", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		pose := s3mPose(mgl32.Vec3{})
		actor := cmd.AddEntity(pose, s3lLocal(pose), GroundedCharacterMotorComponent{Height: 1.8, EyeHeight: 1.7, Radius: .35, Speed: 2, Grounded: true}, GroundedCharacterIntentComponent{MoveDirection: mgl32.Vec3{1, 0, 0}, AimDirection: mgl32.Vec3{1, 0, 0}, Jump: true})
		app.FlushCommands()
		s3mStep(t, cmd, func() { groundedPlayerControlSystem(cmd, &Time{Dt: .5}, nil, nil) }, []EntityId{actor}, []EntityId{actor})
		world := s3cComponent[TransformComponent](t, cmd, actor)
		if world.Position[0] != 1 || s3cComponent[GroundedCharacterIntentComponent](t, cmd, actor).Jump {
			t.Fatal("camera-free motor did not move or consume jump")
		}
		s3mTRS(t, cmd, actor, world.Position)
		if cmd.GetComponent(actor, reflect.TypeOf(CameraComponent{})) != nil {
			t.Fatal("camera-free motor acquired camera")
		}
	})
	for _, scripted := range []bool{true, false} {
		name := "kinematic stationary"
		if scripted {
			name = "scripted stationary"
		}
		t.Run(name, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			base := mgl32.Vec3{1, 2, 3}
			pose := s3mPose(base)
			pose.Rotation, pose.Scale = mgl32.Quat{W: 2}, mgl32.Vec3{2, 3, 4}
			camera := CameraComponent{Yaw: .7, Pitch: -.2, Fov: 63, Near: .02, Far: 900, Aspect: 1.4}
			motor := GroundedCharacterMotorComponent{EyeHeight: 1.5, ScriptedMovement: scripted, JumpQueued: true, SwimUpRequested: true, VerticalVelocity: -5}
			if !scripted {
				motor.MotionMode = CharacterMotionKinematic
			}
			actor := cmd.AddEntity(pose, s3lLocal(pose), camera, motor)
			app.FlushCommands()
			call := func() { groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, nil) }
			s3mStep(t, cmd, call, []EntityId{actor}, []EntityId{actor})
			s3mTRS(t, cmd, actor, base)
			s3mCamera(t, *s3cComponent[CameraComponent](t, cmd, actor), camera, base, 1.5)
			got := s3cComponent[GroundedCharacterMotorComponent](t, cmd, actor)
			if got.JumpQueued || got.SwimUpRequested || got.VerticalVelocity != 0 {
				t.Fatal("stationary motor did not clear competing movement")
			}
			s3mStep(t, cmd, call, nil, nil)
		})
	}
}

func TestS3mActualRiderBridge(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	center, half := mgl32.Vec3{0, .5, 0}, mgl32.Vec3{2, .5, 2}
	brush := cmd.AddEntity(s3mPose(mgl32.Vec3{}), MovingBrushComponent{BoundsCenter: center, BoundsHalfExtents: half})
	camera := CameraComponent{Yaw: .7, Pitch: -.2, Fov: 63, Near: .02, Far: 900, Aspect: 1.4}
	playerPose, npcPose := s3mPose(mgl32.Vec3{0, 1, 0}), s3mPose(mgl32.Vec3{.5, 1, 0})
	motor := GroundedCharacterMotorComponent{Radius: .25, Height: 1.7, EyeHeight: 1.5}
	player := cmd.AddEntity(playerPose, s3lLocal(playerPose), motor, camera)
	npc := cmd.AddEntity(npcPose, s3lLocal(npcPose), motor, NPCComponent{})
	unsupported := cmd.AddEntity(s3mPose(mgl32.Vec3{4, 1, 0}), motor)
	airborne := cmd.AddEntity(s3mPose(mgl32.Vec3{0, 2, 0}), motor)
	app.FlushCommands()
	for _, id := range []EntityId{player, npc} {
		position := s3cComponent[TransformComponent](t, cmd, id).Position
		hit, ok := CharacterGroundHitAtWithin(nil, position, CharacterGroundProbeConfig{Radius: .25, StepHeight: .6, GroundProbe: .15, DynamicCollisionQuery: MovingBrushCollisionQuery(cmd)}, .1, 2, nil)
		if !ok || hit.ContactCount == 0 || hit.Contacts[0].Entity != brush {
			t.Fatalf("rider %d did not acquire public brush support: %+v", id, hit)
		}
		ctrl := s3cComponent[GroundedCharacterMotorComponent](t, cmd, id)
		ctrl.Grounded, ctrl.HasGroundPoint, ctrl.GroundPoint = true, true, hit.Point
		ctrl.GroundContacts, ctrl.GroundContactCount = hit.Contacts, hit.ContactCount
	}
	s3cComponent[GroundedCharacterMotorComponent](t, cmd, unsupported).Grounded = true
	// Even a matching contact cannot carry an airborne actor.
	air := s3cComponent[GroundedCharacterMotorComponent](t, cmd, airborne)
	air.GroundContactCount = 1
	air.GroundContacts[0].Entity = brush
	delta := mgl32.Vec3{.25, 0, 0}
	s3mStep(t, cmd, func() { moveMovingBrushRiders(cmd, brush, center, half, delta) }, []EntityId{player, npc}, []EntityId{player, npc})
	for id, want := range map[EntityId]mgl32.Vec3{player: playerPose.Position.Add(delta), npc: npcPose.Position.Add(delta)} {
		s3mTRS(t, cmd, id, want)
		ctrl := s3cComponent[GroundedCharacterMotorComponent](t, cmd, id)
		if !ctrl.Grounded || !ctrl.HasGroundPoint || ctrl.GroundContactCount != 1 || ctrl.GroundContacts[0].Entity != brush || ctrl.GroundPoint != want {
			t.Fatalf("rider %d lost carried support: %+v", id, *ctrl)
		}
	}
	s3mCamera(t, *s3cComponent[CameraComponent](t, cmd, player), camera, playerPose.Position.Add(delta), 1.5)
	if cmd.GetComponent(npc, reflect.TypeOf(CameraComponent{})) != nil || cmd.GetComponent(unsupported, s3gTypes[1]) != nil || cmd.GetComponent(airborne, s3gTypes[1]) != nil {
		t.Fatal("rider bridge created missing camera or Local")
	}
	s3mStep(t, cmd, func() { moveMovingBrushRiders(cmd, brush, center.Add(delta), half, mgl32.Vec3{}) }, nil, nil)
}
