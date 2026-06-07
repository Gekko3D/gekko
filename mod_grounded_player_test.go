package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSpawnGroundedPlayerAtMarkerUsesModuleDefaults(t *testing.T) {
	app := NewApp()
	app.UseModules(GroundedPlayerControllerModule{
		Config: GroundedPlayerControllerConfig{
			Height:    1.65,
			EyeHeight: 1.5,
			Radius:    0.22,
		},
	})
	app.build()
	cmd := app.Commands()

	eid := SpawnGroundedPlayerAtMarker(cmd, content.LevelMarkerDef{
		ID:   "spawn",
		Kind: content.LevelMarkerKindPlayerSpawn,
		Transform: content.LevelTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	})
	app.FlushCommands()

	var got *GroundedPlayerControllerComponent
	MakeQuery1[GroundedPlayerControllerComponent](cmd).Map(func(found EntityId, ctrl *GroundedPlayerControllerComponent) bool {
		if found == eid {
			got = ctrl
			return false
		}
		return true
	})
	if got == nil {
		t.Fatal("expected grounded player controller component")
	}
	if got.Height != 1.65 || got.EyeHeight != 1.5 || got.Radius != 0.22 {
		t.Fatalf("expected configured player dimensions, got %+v", *got)
	}
}

func TestGroundedMovementBlockedUsesPlayerRadiusAtDoorway(t *testing.T) {
	state := newGroundedPlayerTestVoxelRtState()

	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	for y := 0; y < 3; y++ {
		obj.XBrickMap.SetVoxel(1, y, 1, 1)
	}
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(obj)

	basePos := mgl32.Vec3{1.3, 0, 0}
	move := mgl32.Vec3{0, 0, 0.8}
	ctrl := &GroundedPlayerControllerComponent{
		Height:     1.7,
		Radius:     0.35,
		StepHeight: 0.6,
	}
	if !groundedMovementBlocked(state, basePos, move, ctrl) {
		t.Fatal("expected doorway side collision to block movement when player radius overlaps the jamb")
	}
}

func TestGroundedPlayerClimbsOverlappingLadderVolume(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity(
		&CameraComponent{
			Position: mgl32.Vec3{0, 1.6, 0},
			LookAt:   mgl32.Vec3{0, 1.6, -1},
			Up:       mgl32.Vec3{0, 1, 0},
		},
		&GroundedPlayerControllerComponent{
			Height:           1.8,
			EyeHeight:        1.6,
			Radius:           0.35,
			Speed:            5.5,
			SprintMultiplier: 1.6,
			MoveInput:        mgl32.Vec2{0, 1},
		},
	)
	cmd.AddEntity(&LadderVolumeComponent{
		BoundsCenter:      mgl32.Vec3{0, 1.5, 0},
		BoundsHalfExtents: mgl32.Vec3{0.5, 2, 0.5},
		ClimbSpeed:        3,
	})
	app.FlushCommands()

	groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, nil)

	var found bool
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(eid EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		if eid != player {
			return true
		}
		found = true
		if !ctrl.OnLadder {
			t.Fatalf("expected player on ladder, got %+v", ctrl)
		}
		if absf(cam.Position.Y()-4.6) > 1e-5 {
			t.Fatalf("expected camera to climb to y=4.6, got %v", cam.Position.Y())
		}
		if ctrl.VerticalVelocity != 0 || ctrl.Grounded {
			t.Fatalf("expected ladder to pause gravity, got %+v", ctrl)
		}
		return false
	})
	if !found {
		t.Fatal("expected player query result")
	}
}

func TestGroundedPlayerLadderClimbStopsAtCeiling(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity(
		&CameraComponent{
			Position: mgl32.Vec3{0, 1.6, 0},
			LookAt:   mgl32.Vec3{0, 1.6, -1},
			Up:       mgl32.Vec3{0, 1, 0},
		},
		&GroundedPlayerControllerComponent{
			Height:           1.8,
			EyeHeight:        1.6,
			Radius:           0.35,
			Speed:            5.5,
			SprintMultiplier: 1.6,
			MoveInput:        mgl32.Vec2{0, 1},
		},
	)
	cmd.AddEntity(&LadderVolumeComponent{
		BoundsCenter:      mgl32.Vec3{0, 1.5, 0},
		BoundsHalfExtents: mgl32.Vec3{0.5, 3, 0.5},
		ClimbSpeed:        3,
	})
	app.FlushCommands()

	state := newGroundedPlayerTestVoxelRtState()
	ceiling := core.NewVoxelObject()
	ceiling.XBrickMap = volume.NewXBrickMap()
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			ceiling.XBrickMap.SetVoxel(x, 2, z, 1)
		}
	}
	ceiling.Transform.Scale = mgl32.Vec3{1, 1, 1}
	ceiling.Transform.Dirty = true
	ceiling.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(ceiling)

	groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, state)

	var found bool
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(eid EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		if eid != player {
			return true
		}
		found = true
		if !ctrl.OnLadder {
			t.Fatalf("expected player on ladder, got %+v", ctrl)
		}
		if cam.Position.Y() >= 2 {
			t.Fatalf("expected ceiling to block ladder climb before camera y=2, got %v", cam.Position.Y())
		}
		return false
	})
	if !found {
		t.Fatal("expected player query result")
	}
}

func TestGroundedPlayerUseActivatesLinkedMovingBrush(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&CameraComponent{
			Position: mgl32.Vec3{0, 1.6, 0},
			LookAt:   mgl32.Vec3{0, 1.6, -1},
			Up:       mgl32.Vec3{0, 1, 0},
			Yaw:      0,
			Pitch:    0,
		},
		&GroundedPlayerControllerComponent{},
	)
	cmd.AddEntity(&UseTriggerComponent{
		BoundsCenter:      mgl32.Vec3{0, 1.6, -1},
		BoundsHalfExtents: mgl32.Vec3{0.25, 0.25, 0.25},
		Target:            "door_a",
	})
	cmd.AddEntity(&MovingBrushComponent{
		BoundsCenter:      mgl32.Vec3{0, 1.6, -2},
		BoundsHalfExtents: mgl32.Vec3{0.5, 1, 0.25},
		TargetName:        "door_a",
	})
	app.FlushCommands()

	input := &Input{}
	input.JustPressed[KeyE] = true
	groundedPlayerUseSystem(cmd, input)

	var triggerCount, brushCount int
	var doorOpen bool
	MakeQuery1[UseTriggerComponent](cmd).Map(func(_ EntityId, trigger *UseTriggerComponent) bool {
		triggerCount += trigger.ActivationCount
		return true
	})
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		brushCount += brush.ActivationCount
		doorOpen = brush.Open
		return true
	})
	if triggerCount != 1 || brushCount != 1 || !doorOpen {
		t.Fatalf("expected linked button to open door, trigger=%d brush=%d open=%v", triggerCount, brushCount, doorOpen)
	}
}

func TestMovingBrushMotionMovesTowardOpenOffset(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&MovingBrushComponent{
			BoundsCenter:       mgl32.Vec3{2, 2, 3},
			ClosedPosition:     mgl32.Vec3{1, 2, 3},
			ClosedBoundsCenter: mgl32.Vec3{2, 2, 3},
			OpenOffset:         mgl32.Vec3{4, 0, 0},
			Speed:              2,
			Open:               true,
		},
	)
	app.FlushCommands()

	movingBrushMotionSystem(cmd, &Time{Dt: 1})

	tr := transformForEntityMust(t, cmd, eid)
	if tr.Position != (mgl32.Vec3{3, 2, 3}) {
		t.Fatalf("moving brush position = %v", tr.Position)
	}
	var brush *MovingBrushComponent
	MakeQuery1[MovingBrushComponent](cmd).Map(func(found EntityId, candidate *MovingBrushComponent) bool {
		if found == eid {
			brush = candidate
			return false
		}
		return true
	})
	if brush == nil || brush.BoundsCenter != (mgl32.Vec3{4, 2, 3}) {
		t.Fatalf("moving brush bounds center = %+v", brush)
	}
}

func transformForEntityMust(t *testing.T, cmd *Commands, eid EntityId) *TransformComponent {
	t.Helper()
	tr, ok := transformForEntity(cmd, eid)
	if !ok {
		t.Fatalf("missing transform for entity %d", eid)
	}
	return tr
}

func newGroundedPlayerTestVoxelRtState() *VoxelRtState {
	return &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: core.NewProfiler(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}
}
