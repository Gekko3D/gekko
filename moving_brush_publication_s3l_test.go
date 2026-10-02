package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// These snapshots use committed public queries and independent float bits.
// Publication stamps are opaque; only advance versus quiet is observable.
func s3lSnapshot(cmd *Commands) [2]map[EntityId][10]uint32 {
	result := [2]map[EntityId][10]uint32{{}, {}}
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, tr *TransformComponent) bool {
		result[0][id] = s3gBits(tr.Position, tr.Rotation, tr.Scale)
		return true
	})
	MakeQuery1[LocalTransformComponent](cmd).Map(func(id EntityId, tr *LocalTransformComponent) bool {
		result[1][id] = s3gBits(tr.Position, tr.Rotation, tr.Scale)
		return true
	})
	return result
}

func s3lStep(t *testing.T, cmd *Commands, time *Time, worldIDs, localIDs []EntityId) {
	t.Helper()
	beforeValues := s3lSnapshot(cmd)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	movingBrushMotionSystem(cmd, time)
	afterValues := s3lSnapshot(cmd)
	wantRevision := [4]int{}
	for index, ids := range [2][]EntityId{worldIDs, localIDs} {
		want := map[EntityId]bool{}
		for _, id := range ids {
			want[id] = true
		}
		got := map[EntityId]bool{}
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
		if len(ids) > 0 {
			wantRevision[index] = 1
		}
	}
	s3gPublications(t, cmd, before, structural, wantRevision)
}

func s3lLinear(x float32) (TransformComponent, MovingBrushComponent) {
	world := s3dWorld(x)
	brush := MovingBrushComponent{
		ClosedPosition: world.Position, ClosedRotation: world.Rotation,
		BoundsCenter: mgl32.Vec3{x, .5, 0}, ClosedBoundsCenter: mgl32.Vec3{x, .5, 0},
		BoundsHalfExtents: mgl32.Vec3{1, .5, .5}, ClosedHalfExtents: mgl32.Vec3{1, .5, .5},
		OpenOffset: mgl32.Vec3{2, 0, 0}, Speed: 2, Open: true,
	}
	return world, brush
}

func s3lLocal(world TransformComponent) LocalTransformComponent {
	return LocalTransformComponent{Position: world.Position, Rotation: world.Rotation, Scale: world.Scale}
}

func TestS3lAcceptedMotionPublishesBrushDestinations(t *testing.T) {
	for _, kind := range []string{"linear", "rotation", "path"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			parentWorld := s3dWorld(70)
			parentWorld.Scale = mgl32.Vec3{2, 2, 2}
			parent := cmd.AddEntity(parentWorld, s3gUnrelated{7})
			world, brush := s3lLinear(10)
			world.Scale, world.Pivot = mgl32.Vec3{2, 3, 4}, mgl32.Vec3{5, 6, 7}
			want := world
			want.Position = mgl32.Vec3{12, 0, 0}
			wantCenter, wantHalf := mgl32.Vec3{12, .5, 0}, brush.BoundsHalfExtents
			switch kind {
			case "rotation":
				brush.MotionKind, brush.RotationOrigin, brush.RotationAxis = "rotate", world.Position, mgl32.Vec3{0, 1, 0}
				brush.OpenAngle, brush.Speed = 90, 90
				brush.BoundsCenter, brush.ClosedBoundsCenter = mgl32.Vec3{11, .5, 0}, mgl32.Vec3{11, .5, 0}
				want.Position, want.Rotation = world.Position, mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
				wantCenter, wantHalf = mgl32.Vec3{10, .5, -1}, mgl32.Vec3{.5, .5, 1}
			case "path":
				brush.PathTarget, brush.Speed = "arrival", 1
				cmd.AddEntity(PathNodeComponent{TargetName: "arrival", Target: "hold", Position: want.Position, Wait: 3, Speed: 2})
			}
			id := cmd.AddEntity(world, s3lLocal(world), brush, Parent{Entity: parent}, s3gUnrelated{8})
			childWorld, childLocal := s3dWorld(99), s3dLocal(3)
			child := cmd.AddEntity(childWorld, childLocal, Parent{Entity: id})
			app.FlushCommands()
			s3lStep(t, cmd, &Time{Dt: 1}, []EntityId{id}, []EntityId{id})
			s3dTRS(t, cmd, id, want.Position, want.Scale, want.Rotation)
			// This writer currently copies numerical World TRS into Local even
			// with Parent; hierarchy composition belongs to the later stage.
			s3gLocal(t, cmd, id, s3lLocal(want))
			got := s3cComponent[MovingBrushComponent](t, cmd, id)
			if got.BoundsCenter.Sub(wantCenter).Len() > 1e-4 || got.BoundsHalfExtents.Sub(wantHalf).Len() > 1e-4 {
				t.Fatalf("motion bounds=%v/%v, want %v/%v", got.BoundsCenter, got.BoundsHalfExtents, wantCenter, wantHalf)
			}
			if kind == "rotation" && got.CurrentAngle != 90 || kind == "path" && (got.PathTarget != "hold" || got.PathWaitRemaining != 3 || got.Speed != 2) {
				t.Fatalf("motion result changed: %+v", *got)
			}
			if s3cComponent[TransformComponent](t, cmd, id).Pivot != world.Pivot || *s3cComponent[TransformComponent](t, cmd, parent) != parentWorld || *s3cComponent[TransformComponent](t, cmd, child) != childWorld || *s3cComponent[LocalTransformComponent](t, cmd, child) != childLocal {
				t.Fatal("accepted brush changed Pivot, parent or descendant values")
			}
			if s3cComponent[Parent](t, cmd, id).Entity != parent || s3cComponent[s3gUnrelated](t, cmd, parent).Value != 7 || s3cComponent[s3gUnrelated](t, cmd, id).Value != 8 {
				t.Fatal("accepted brush changed hierarchy ownership or unrelated values")
			}
			s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
		})
	}
}

func TestS3lWorldAndLocalPublicationAreIndependent(t *testing.T) {
	for _, mode := range []string{"repair local", "world only", "no local", "mixed batch"} {
		t.Run(mode, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			world, brush := s3lLinear(10)
			want := world
			want.Position = mgl32.Vec3{12, 0, 0}
			local := s3lLocal(want)
			worldIDs, localIDs := []EntityId{}, []EntityId{}
			if mode == "repair local" {
				brush.Open = false
				want = world
				local = s3dLocal(-9)
				local.Rotation, local.Scale = mgl32.QuatRotate(.5, mgl32.Vec3{0, 0, 1}), mgl32.Vec3{7, 8, 9}
			}
			if mode == "mixed batch" {
				quietWorld, quietBrush := s3lLinear(30)
				quietBrush.Open = false
				cmd.AddEntity(quietWorld, s3lLocal(quietWorld), quietBrush)
				local = s3lLocal(world)
			}
			components := []any{world, brush, s3gUnrelated{11}}
			if mode != "no local" {
				components = append(components, local)
			}
			id := cmd.AddEntity(components...)
			if mode == "mixed batch" {
				quietWorld, quietBrush := s3lLinear(40)
				quietBrush.Open = false
				cmd.AddEntity(quietWorld, s3lLocal(quietWorld), quietBrush)
			}
			app.FlushCommands()
			if mode != "repair local" {
				worldIDs = append(worldIDs, id)
			}
			if mode == "repair local" || mode == "mixed batch" {
				localIDs = append(localIDs, id)
			}
			s3lStep(t, cmd, &Time{Dt: 1}, worldIDs, localIDs)
			s3dTRS(t, cmd, id, want.Position, want.Scale, want.Rotation)
			if mode == "no local" {
				if cmd.GetComponent(id, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("motion created missing Local")
				}
				app.FlushCommands()
				if cmd.GetComponent(id, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("motion queued missing Local creation")
				}
			} else {
				s3gLocal(t, cmd, id, s3lLocal(want))
			}
			if s3cComponent[s3gUnrelated](t, cmd, id).Value != 11 {
				t.Fatal("motion changed unrelated component")
			}
			s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
		})
	}
}

func TestS3lStateOnlyAndSkippedInvocationsStayQuiet(t *testing.T) {
	for _, state := range []string{"open timer", "path wait", "missing path"} {
		t.Run(state, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			world, brush := s3lLinear(10)
			brush.OpenOffset = mgl32.Vec3{}
			switch state {
			case "open timer":
				brush.Wait, brush.OpenWaitRemaining = 3, .5
			case "path wait":
				brush.PathTarget, brush.PathWaitRemaining = "later", 2
			case "missing path":
				brush.PathTarget = "missing"
			}
			id := cmd.AddEntity(world, s3lLocal(world), brush)
			app.FlushCommands()
			s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
			got := s3cComponent[MovingBrushComponent](t, cmd, id)
			if state == "open timer" && (got.Open || got.OpenWaitRemaining != 0) || state == "path wait" && got.PathWaitRemaining != 1 || state == "missing path" && got.Open {
				t.Fatalf("state-only update did not occur: %+v", *got)
			}
		})
	}
	t.Run("skips", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		world, brush := s3lLinear(10)
		active := cmd.AddEntity(world, s3lLocal(world), brush)
		brush.Kind, brush.SpawnFlags = "hl1_func_button", 1
		button := cmd.AddEntity(world, s3dLocal(99), brush)
		_, normalBrush := s3lLinear(10)
		noWorld := cmd.AddEntity(s3dLocal(99), normalBrush)
		noBrush := cmd.AddEntity(world, s3dLocal(99))
		app.FlushCommands()
		queued := cmd.AddEntity(s3dWorld(100), s3dLocal(100))
		movingBrushMotionSystem(nil, &Time{Dt: 1})
		for _, time := range []*Time{nil, {Dt: 0}, {Dt: -1}} {
			s3lStep(t, cmd, time, nil, nil)
		}
		s3lStep(t, cmd, &Time{Dt: 1}, []EntityId{active}, []EntityId{active})
		for _, id := range []EntityId{button, noWorld, noBrush} {
			s3gLocal(t, cmd, id, s3dLocal(99))
		}
		if cmd.EntityExists(queued) || cmd.GetComponent(noWorld, reflect.TypeOf(TransformComponent{})) != nil {
			t.Fatal("skipped writer flushed commands or created World")
		}
	})
}

func TestS3lRejectedDoorAndCarrierDoNotPublishProposals(t *testing.T) {
	for _, mode := range []string{"opening door", "closing door", "carrier"} {
		t.Run(mode, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			world, brush := s3lLinear(10)
			world.Pivot = mgl32.Vec3{5, 6, 7}
			brush.NavigationRole = content.NavigationRoleDoor
			brush.BoundsHalfExtents, brush.ClosedHalfExtents = mgl32.Vec3{.5, .5, .5}, mgl32.Vec3{.5, .5, .5}
			character := s3dWorld(12)
			if mode == "closing door" {
				world.Position, brush.BoundsCenter, brush.Open = mgl32.Vec3{12, 0, 0}, mgl32.Vec3{12, .5, 0}, false
				character.Position = mgl32.Vec3{10, 0, 0}
			}
			if mode == "carrier" {
				brush.NavigationRole, brush.OpenOffset, brush.Speed = content.NavigationRoleCarrier, mgl32.Vec3{0, 1, 0}, 1
				brush.BoundsHalfExtents, brush.ClosedHalfExtents = mgl32.Vec3{1, .5, 1}, mgl32.Vec3{1, .5, 1}
				character.Position = mgl32.Vec3{10, 1, 0}
				cmd.AddEntity(s3dWorld(10), MovingBrushComponent{Kind: "hl1_func_button", SpawnFlags: 1, BoundsCenter: mgl32.Vec3{10, 3.2, 0}, BoundsHalfExtents: mgl32.Vec3{2, .1, 2}})
			}
			local := s3dLocal(99)
			id := cmd.AddEntity(world, local, brush)
			motor := GroundedCharacterMotorComponent{Radius: .35, Height: 1.8}
			if mode == "carrier" {
				motor.Grounded, motor.GroundContactCount = true, 1
				motor.GroundContacts[0] = CharacterGroundContact{Entity: id, Normal: mgl32.Vec3{0, 1, 0}}
			}
			cmd.AddEntity(character, motor)
			app.FlushCommands()
			if mode == "carrier" {
				_, blocked := CharacterVerticalMove(nil, character.Position, 1, GroundedCharacterCollisionConfig(cmd, &motor), func(entity EntityId, _ bool) bool { return entity != id })
				if !blocked {
					t.Fatal("ceiling fixture does not block upward carrier motion")
				}
			}
			s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
			if *s3cComponent[TransformComponent](t, cmd, id) != world || *s3cComponent[LocalTransformComponent](t, cmd, id) != local {
				t.Fatal("rejected proposal changed committed brush World or mismatched Local")
			}
			got := s3cComponent[MovingBrushComponent](t, cmd, id)
			if got.BoundsCenter != brush.BoundsCenter || got.BoundsHalfExtents != brush.BoundsHalfExtents {
				t.Fatal("rejected motion changed committed bounds")
			}
			if mode == "closing door" && (!got.Open || got.ActivationCount <= brush.ActivationCount) {
				t.Fatal("blocked closing door did not request reopening")
			}
		})
	}
}

func TestS3lExactDestinationBitsControlPublication(t *testing.T) {
	t.Run("signed-zero snap", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		world, brush := s3lLinear(10)
		brush.Open = false
		world.Position[2] = math.Float32frombits(0x80000000)
		id := cmd.AddEntity(world, s3lLocal(world), brush)
		app.FlushCommands()
		s3lStep(t, cmd, &Time{Dt: 1}, []EntityId{id}, []EntityId{id})
		got := s3cComponent[TransformComponent](t, cmd, id)
		if math.Float32bits(got.Position[2]) != 0 {
			t.Fatal("at-target snap did not copy positive-zero target bits")
		}
		s3dTRS(t, cmd, id, brush.ClosedPosition, world.Scale, world.Rotation)
		s3gLocal(t, cmd, id, s3lLocal(*got))
		s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
	})
	t.Run("preserved NaN payloads", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		world, brush := s3lLinear(10)
		brush.Open = false
		world.Scale[1], world.Pivot[2] = math.Float32frombits(0x7fc01234), math.Float32frombits(0x7fc05678)
		local := s3lLocal(world)
		local.Scale[1] = math.Float32frombits(0x7fc01235)
		id := cmd.AddEntity(world, local, brush, s3gUnrelated{12})
		app.FlushCommands()
		s3lStep(t, cmd, &Time{Dt: 1}, nil, []EntityId{id})
		gotWorld := s3cComponent[TransformComponent](t, cmd, id)
		gotLocal := s3cComponent[LocalTransformComponent](t, cmd, id)
		wantBits := s3gBits(world.Position, world.Rotation, world.Scale)
		if s3gBits(gotWorld.Position, gotWorld.Rotation, gotWorld.Scale) != wantBits || s3gBits(gotLocal.Position, gotLocal.Rotation, gotLocal.Scale) != wantBits || math.Float32bits(gotWorld.Pivot[2]) != 0x7fc05678 {
			t.Fatal("accepted copy changed preserved NaN payload bits")
		}
		if gotWorld.Position != world.Position || gotWorld.Rotation != world.Rotation || s3cComponent[s3gUnrelated](t, cmd, id).Value != 12 {
			t.Fatal("bit fixture changed finite pose or unrelated value")
		}
		s3lStep(t, cmd, &Time{Dt: 1}, nil, nil)
	})
}

func TestS3lPendingCommandsKeepCommittedBrushWritesUntilFlush(t *testing.T) {
	for _, pending := range []string{"replace World and Local", "remove World", "remove Local", "remove entity", "add Local", "add World", "new entity"} {
		t.Run(pending, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			world, brush := s3lLinear(10)
			components := []any{brush}
			if pending != "add World" {
				components = append(components, world)
			}
			if pending != "add Local" {
				components = append(components, s3lLocal(world))
			}
			id := cmd.AddEntity(components...)
			app.FlushCommands()
			queuedWorld, queuedLocal := s3dWorld(80), s3dLocal(90)
			var queued EntityId
			switch pending {
			case "replace World and Local":
				cmd.AddComponents(id, queuedWorld, queuedLocal)
			case "remove World":
				cmd.RemoveComponents(id, TransformComponent{})
			case "remove Local":
				cmd.RemoveComponents(id, LocalTransformComponent{})
			case "remove entity":
				cmd.RemoveEntity(id)
			case "add Local":
				cmd.AddComponents(id, queuedLocal)
			case "add World":
				cmd.AddComponents(id, queuedWorld)
			case "new entity":
				queued = cmd.AddEntity(queuedWorld, queuedLocal, brush)
			}
			var worldIDs, localIDs []EntityId
			if pending != "add World" {
				worldIDs = []EntityId{id}
				if pending != "add Local" {
					localIDs = []EntityId{id}
				}
			}
			s3lStep(t, cmd, &Time{Dt: 1}, worldIDs, localIDs)
			if !cmd.EntityExists(id) || queued != 0 && cmd.EntityExists(queued) {
				t.Fatal("motion changed committed entity membership before flush")
			}
			if pending != "add World" {
				s3dPosition(t, cmd, id, 12)
			} else if cmd.GetComponent(id, reflect.TypeOf(TransformComponent{})) != nil {
				t.Fatal("queued World became visible before flush")
			}
			if pending == "add Local" {
				if cmd.GetComponent(id, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("queued Local became visible before flush")
				}
			} else {
				want := float32(12)
				if pending == "add World" {
					want = 10
				}
				s3gLocal(t, cmd, id, s3dLocal(want))
			}
			app.FlushCommands()
			switch pending {
			case "replace World and Local":
				s3dPosition(t, cmd, id, 80)
				s3gLocal(t, cmd, id, queuedLocal)
			case "remove World":
				if cmd.GetComponent(id, reflect.TypeOf(TransformComponent{})) != nil {
					t.Fatal("explicit flush did not remove World")
				}
			case "remove Local":
				if cmd.GetComponent(id, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("explicit flush did not remove Local")
				}
			case "remove entity":
				if cmd.EntityExists(id) {
					t.Fatal("explicit flush did not remove entity")
				}
			case "add Local":
				s3gLocal(t, cmd, id, queuedLocal)
			case "add World":
				s3dPosition(t, cmd, id, 80)
			case "new entity":
				s3dPosition(t, cmd, queued, 80)
				s3gLocal(t, cmd, queued, queuedLocal)
			}
		})
	}
}
