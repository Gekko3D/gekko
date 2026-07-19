package gekko

import (
	"reflect"
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

func TestGroundedPlayerBasePositionUsesControllerTransform(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{1, 2, 3}})
	app.FlushCommands()

	base := groundedPlayerBasePosition(cmd, player, &CameraComponent{Position: mgl32.Vec3{10, 20, 30}}, &GroundedPlayerControllerComponent{EyeHeight: 1.7})
	if base != (mgl32.Vec3{1, 2, 3}) {
		t.Fatalf("base position = %v, want controller transform", base)
	}
}

func TestGroundedPlayerScriptedMovementKeepsControllerAndCameraAligned(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&CameraComponent{},
		&GroundedPlayerControllerComponent{EyeHeight: 1.5, ScriptedMovement: true, JumpQueued: true, SwimUpRequested: true, VerticalVelocity: -5},
	)
	app.FlushCommands()

	groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, nil)
	ctrl := cmd.GetComponent(player, reflect.TypeOf(GroundedPlayerControllerComponent{})).(*GroundedPlayerControllerComponent)
	tr := cmd.GetComponent(player, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	cam := cmd.GetComponent(player, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
	if tr.Position != (mgl32.Vec3{1, 2, 3}) || cam.Position != (mgl32.Vec3{1, 3.5, 3}) {
		t.Fatalf("expected scripted controller to preserve transform and update camera, transform=%+v camera=%+v", tr.Position, cam.Position)
	}
	if ctrl.JumpQueued || ctrl.SwimUpRequested || ctrl.VerticalVelocity != 0 {
		t.Fatalf("expected scripted controller to clear competing movement state, got %+v", *ctrl)
	}
}

func TestGroundedCharacterMotorMovesWithoutCameraAndConsumesJump(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	actor := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&GroundedCharacterMotorComponent{Height: 1.8, EyeHeight: 1.7, Radius: 0.35, Speed: 2, Grounded: true},
		&GroundedCharacterIntentComponent{MoveDirection: mgl32.Vec3{1, 0, 0}, AimDirection: mgl32.Vec3{1, 0, 0}, Jump: true},
	)
	app.FlushCommands()

	groundedPlayerControlSystem(cmd, &Time{Dt: 0.5}, nil, nil)
	motor := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
	intent := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterIntentComponent{})).(*GroundedCharacterIntentComponent)
	tr := cmd.GetComponent(actor, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if tr.Position.X() != 1 || motor.ActualVelocity.X() != 2 {
		t.Fatalf("camera-free motor position=%v velocity=%v", tr.Position, motor.ActualVelocity)
	}
	if intent.Jump {
		t.Fatal("camera-free motor did not consume one-frame jump")
	}
	if cmd.GetComponent(actor, reflect.TypeOf(CameraComponent{})) != nil {
		t.Fatal("camera-free motor acquired a render camera")
	}
}

func TestGroundedCharacterIntentForcesLadderOnlyExplicitly(t *testing.T) {
	tests := []struct {
		name     string
		intent   GroundedCharacterIntentComponent
		wantX    float32
		wantY    float32
		wantZ    float32
		onLadder bool
	}{
		{
			name:   "forward input stays grounded",
			intent: GroundedCharacterIntentComponent{MoveDirection: mgl32.Vec3{0, 0, -1}, LadderMovement: 1},
			wantZ:  -1,
		},
		{
			name:     "authored traversal forces climb",
			intent:   GroundedCharacterIntentComponent{LadderMovement: 1, ForceLadder: true},
			wantY:    1.5,
			onLadder: true,
		},
		{
			name:     "authored traversal bounds lateral travel",
			intent:   GroundedCharacterIntentComponent{MoveDirection: mgl32.Vec3{1, 0, 0}, MaxDistance: 0.2, LadderMovement: 1, ForceLadder: true},
			wantX:    0.2,
			wantY:    1.5,
			onLadder: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			actor := cmd.AddEntity(
				&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&GroundedCharacterMotorComponent{Height: 1.8, Radius: 0.35, Speed: 2, Gravity: 0.0001, Grounded: true},
				&tt.intent,
			)
			app.FlushCommands()

			groundedPlayerControlSystem(cmd, &Time{Dt: 0.5}, nil, nil)
			tr := cmd.GetComponent(actor, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
			motor := cmd.GetComponent(actor, reflect.TypeOf(GroundedCharacterMotorComponent{})).(*GroundedCharacterMotorComponent)
			if absf(tr.Position.X()-tt.wantX) > 0.001 || absf(tr.Position.Y()-tt.wantY) > 0.001 || absf(tr.Position.Z()-tt.wantZ) > 0.001 || motor.OnLadder != tt.onLadder {
				t.Fatalf("position=%v onLadder=%t, want x=%v y=%v z=%v onLadder=%t", tr.Position, motor.OnLadder, tt.wantX, tt.wantY, tt.wantZ, tt.onLadder)
			}
		})
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
	if !groundedMovementBlocked(state, basePos, move, ctrl, nil) {
		t.Fatal("expected doorway side collision to block movement when player radius overlaps the jamb")
	}
}

func TestGroundedMovementIgnoresConfiguredVisualSubtree(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity()
	visualRoot := cmd.AddEntity(&Parent{Entity: player})
	visualPart := cmd.AddEntity(&Parent{Entity: visualRoot})
	app.FlushCommands()

	state := newGroundedPlayerTestVoxelRtState()
	visual := core.NewVoxelObject()
	visual.XBrickMap = volume.NewXBrickMap()
	for y := 0; y < 3; y++ {
		visual.XBrickMap.SetVoxel(1, y, 1, 1)
	}
	visual.Transform.Scale = mgl32.Vec3{1, 1, 1}
	visual.Transform.Dirty = true
	visual.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(visual)
	state.instanceMap[visualPart] = visual
	state.objectToEntity[visual] = visualPart

	ctrl := &GroundedPlayerControllerComponent{
		Height:                 1.7,
		Radius:                 0.35,
		StepHeight:             0.6,
		CollisionIgnoredEntity: visualRoot,
	}
	basePos := mgl32.Vec3{1.3, 0, 0}
	move := mgl32.Vec3{0, 0, 0.8}
	if groundedMovementBlocked(state, basePos, move, ctrl, groundedPlayerCollisionRaycastFilter(cmd, ctrl)) {
		t.Fatal("expected visual subtree to be ignored by grounded movement")
	}
}

func TestGroundedPlayerVerticalUsesFootprintGroundProbe(t *testing.T) {
	state := newGroundedPlayerTestVoxelRtState()
	floor := core.NewVoxelObject()
	floor.XBrickMap = volume.NewXBrickMap()
	floor.XBrickMap.SetVoxel(1, 0, 0, 1)
	floor.Transform.Scale = mgl32.Vec3{1, 1, 1}
	floor.Transform.Dirty = true
	floor.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(floor)

	basePos := mgl32.Vec3{0.7, 1, 0.5}
	ctrl := &GroundedPlayerControllerComponent{
		Height:      1.7,
		Radius:      0.7,
		StepHeight:  0.6,
		GroundProbe: 0.15,
		Grounded:    true,
	}

	resolveGroundedVertical(nil, state, &basePos, ctrl, 1.0/60.0, nil)

	if !ctrl.Grounded {
		t.Fatalf("expected player footprint to stay grounded on edge-supported floor, got %+v", *ctrl)
	}
	if absf(basePos.Y()-1) > 0.002 {
		t.Fatalf("expected player base to remain on floor top, got %v", basePos)
	}
}

func TestGroundedPlayerCrouchRestoresOnlyWhenClear(t *testing.T) {
	ctrl := &GroundedPlayerControllerComponent{
		Height:            1.8,
		EyeHeight:         1.7,
		StandingHeight:    1.8,
		StandingEyeHeight: 1.7,
		CrouchHeight:      1.0,
		CrouchEyeHeight:   0.9,
		Radius:            0.35,
		CrouchRequested:   true,
	}
	basePos := mgl32.Vec3{}
	groundedPlayerUpdateStance(nil, nil, basePos, ctrl, nil)
	if !ctrl.Crouching || ctrl.Height != 1.0 || ctrl.EyeHeight != 0.9 {
		t.Fatalf("expected crouch dimensions, got %+v", *ctrl)
	}

	state := newGroundedPlayerTestVoxelRtState()
	ceiling := core.NewVoxelObject()
	ceiling.XBrickMap = volume.NewXBrickMap()
	ceiling.XBrickMap.SetVoxel(0, 1, 0, 1)
	ceiling.Transform.Scale = mgl32.Vec3{1, 1, 1}
	ceiling.Transform.Dirty = true
	ceiling.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(ceiling)
	ctrl.CrouchRequested = false
	groundedPlayerUpdateStance(nil, state, basePos, ctrl, nil)
	if !ctrl.Crouching || ctrl.Height != 1.0 {
		t.Fatalf("expected ceiling to keep player crouched, got %+v", *ctrl)
	}

	groundedPlayerUpdateStance(nil, nil, basePos, ctrl, nil)
	if ctrl.Crouching || ctrl.Height != 1.8 || ctrl.EyeHeight != 1.7 {
		t.Fatalf("expected clear space to restore standing dimensions, got %+v", *ctrl)
	}
}

func TestGroundedPlayerSwimsInsideWaterBody(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	player := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Position: mgl32.Vec3{}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&CameraComponent{Position: mgl32.Vec3{0, 1.7, 0}, LookAt: mgl32.Vec3{0, 1.7, -1}, Up: mgl32.Vec3{0, 1, 0}},
		&GroundedPlayerControllerComponent{Height: 1.8, EyeHeight: 1.7, Radius: 0.35, Speed: 5.5, SwimSpeed: 2, MoveInput: mgl32.Vec2{0, 1}, SwimUpRequested: true, Grounded: true},
	)
	water := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 1, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&WaterSurfaceComponent{HalfExtents: [2]float32{8, 8}, Depth: 4},
	)
	app.FlushCommands()

	groundedPlayerControlSystem(cmd, &Time{Dt: 1}, nil, nil)
	ctrl := cmd.GetComponent(player, reflect.TypeOf(GroundedPlayerControllerComponent{})).(*GroundedPlayerControllerComponent)
	tr := cmd.GetComponent(player, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	cam := cmd.GetComponent(player, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
	if !ctrl.Swimming || ctrl.WaterEntity != water || ctrl.Grounded {
		t.Fatalf("expected active swim state, got %+v", *ctrl)
	}
	if tr.Position != (mgl32.Vec3{0, 2, -2}) || cam.Position != (mgl32.Vec3{0, 3.7, -2}) {
		t.Fatalf("expected swim movement and camera height, transform=%+v camera=%+v", tr.Position, cam.Position)
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

func TestStaticHL1ButtonActivatesTargetWithoutMoving(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&MovingBrushComponent{
		Kind:              "hl1_func_button",
		SpawnFlags:        1,
		BoundsCenter:      mgl32.Vec3{1, 1, 1},
		BoundsHalfExtents: mgl32.Vec3{0.25, 0.25, 0.25},
	})
	app.FlushCommands()

	activateMovingBrushAtBounds(cmd, mgl32.Vec3{1, 1, 1}, mgl32.Vec3{0.25, 0.25, 0.25})
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, button *MovingBrushComponent) bool {
		if button.Open || button.ActivationCount != 1 {
			t.Fatalf("expected static button to record use without moving, got %+v", button)
		}
		return false
	})
}

func TestTriggerVolumeTouchActivatesLinkedMovingBrushOnce(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&GroundedPlayerControllerComponent{Height: 1.8, Radius: 0.35},
	)
	cmd.AddEntity(&TriggerVolumeComponent{
		Kind:              "hl1_trigger_once",
		BoundsCenter:      mgl32.Vec3{0, 0.9, 0},
		BoundsHalfExtents: mgl32.Vec3{1, 1, 1},
		Target:            "door_a",
		Once:              true,
	})
	cmd.AddEntity(&MovingBrushComponent{
		BoundsCenter:      mgl32.Vec3{2, 0.9, 0},
		BoundsHalfExtents: mgl32.Vec3{0.5, 1, 0.25},
		TargetName:        "door_a",
	})
	app.FlushCommands()

	triggerVolumeTouchSystem(cmd, &Time{Dt: 0.016})
	triggerVolumeTouchSystem(cmd, &Time{Dt: 1})

	var triggerCount, brushCount int
	var doorOpen bool
	MakeQuery1[TriggerVolumeComponent](cmd).Map(func(_ EntityId, trigger *TriggerVolumeComponent) bool {
		triggerCount += trigger.ActivationCount
		return true
	})
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		brushCount += brush.ActivationCount
		doorOpen = brush.Open
		return true
	})
	if triggerCount != 1 || brushCount != 1 || !doorOpen {
		t.Fatalf("expected one-shot trigger to open door once, trigger=%d brush=%d open=%v", triggerCount, brushCount, doorOpen)
	}
}

func TestMultiTargetDispatchQueuesDelayedOutputs(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&MovingBrushComponent{TargetName: "door_a"})
	cmd.AddEntity(&MultiTargetComponent{
		TargetName: "manager_a",
		Events: []TargetEventDef{{
			Target: "door_a",
			Delay:  0.5,
		}},
	})
	app.FlushCommands()

	ActivateTarget(cmd, "manager_a", 0)
	app.FlushCommands()
	targetEventSystem(cmd, &Time{Dt: 0.25})

	var doorOpen bool
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		doorOpen = brush.Open
		return true
	})
	if doorOpen {
		t.Fatal("expected delayed multi-target output to wait")
	}

	targetEventSystem(cmd, &Time{Dt: 0.25})
	app.FlushCommands()
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		doorOpen = brush.Open
		if brush.ActivationCount != 1 {
			t.Fatalf("expected delayed multi-target to activate once, got %+v", brush)
		}
		return true
	})
	if !doorOpen {
		t.Fatal("expected delayed multi-target output to open door")
	}
}

func TestTargetEventDoesNotCrossStreamedRuntimeGeneration(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	runtime := &StreamedLevelRuntimeState{Initialized: true, Generation: 1}
	cmd.AddResources(runtime)
	cmd.AddEntity(&MovingBrushComponent{TargetName: "door_a"})
	app.FlushCommands()

	QueueTargetEvent(cmd, "door_a", 0.25, 0, "test")
	app.FlushCommands()
	runtime.Generation++
	targetEventSystem(cmd, &Time{Dt: 1})
	app.FlushCommands()

	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		if brush.Open || brush.ActivationCount != 0 {
			t.Fatalf("stale target event activated new runtime: %+v", brush)
		}
		return true
	})
	count := 0
	MakeQuery1[TargetEventComponent](cmd).Map(func(_ EntityId, _ *TargetEventComponent) bool { count++; return true })
	if count != 0 {
		t.Fatalf("expected stale target event removal, got %d", count)
	}
}

func TestTargetRelayDispatchesStateKillTargetAndRemoveOnFire(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	relay := cmd.AddEntity(&TargetRelayComponent{
		TargetName:   "relay_a",
		Target:       "door_a",
		Delay:        0.25,
		KillTarget:   "obsolete_a",
		TriggerState: 1,
		SpawnFlags:   1,
	})
	obsolete := cmd.AddEntity(&MovingBrushComponent{TargetName: "obsolete_a"})
	cmd.AddEntity(&MovingBrushComponent{TargetName: "door_a"})
	app.FlushCommands()

	ActivateTarget(cmd, "relay_a", 0)
	app.FlushCommands()
	if comps := cmd.GetAllComponents(relay); len(comps) != 0 {
		t.Fatalf("expected remove-on-fire relay to be removed, got %+v", comps)
	}
	if comps := cmd.GetAllComponents(obsolete); len(comps) != 0 {
		t.Fatalf("expected killtarget to remove obsolete target, got %+v", comps)
	}
	var doorOpen bool
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		if brush.TargetName == "door_a" {
			doorOpen = brush.Open
		}
		return true
	})
	if doorOpen {
		t.Fatal("expected delayed relay target to wait")
	}

	targetEventSystem(cmd, &Time{Dt: 0.25})
	app.FlushCommands()
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		if brush.TargetName == "door_a" {
			doorOpen = brush.Open
			if brush.ActivationCount != 1 {
				t.Fatalf("expected relay to activate door once, got %+v", brush)
			}
		}
		return true
	})
	if !doorOpen {
		t.Fatal("expected relay triggerstate on to open door")
	}
}

func TestTargetActivationControlsDamageVolumes(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	volume := cmd.AddEntity(&DamageVolumeComponent{
		TargetName: "acid_a",
		Enabled:    false,
	})
	app.FlushCommands()

	ActivateTargetWithState(cmd, "acid_a", 0, 1)
	app.FlushCommands()
	damage := cmd.GetComponent(volume, reflect.TypeOf(DamageVolumeComponent{})).(*DamageVolumeComponent)
	if damage == nil || !damage.Enabled || damage.ActivationCount != 1 {
		t.Fatalf("expected target state on to enable damage volume, got %+v", damage)
	}

	ActivateTargetWithState(cmd, "acid_a", 0, 0)
	app.FlushCommands()
	if damage.Enabled || damage.ActivationCount != 2 {
		t.Fatalf("expected target state off to disable damage volume, got %+v", damage)
	}

	ActivateTargetWithState(cmd, "acid_a", 0, 2)
	app.FlushCommands()
	if !damage.Enabled || damage.ActivationCount != 3 {
		t.Fatalf("expected target toggle to enable damage volume, got %+v", damage)
	}

	KillTarget(cmd, "acid_a")
	app.FlushCommands()
	if comps := cmd.GetAllComponents(volume); len(comps) != 0 {
		t.Fatalf("expected killtarget to remove damage volume, got %+v", comps)
	}
}

func TestTargetActivationControlsChangeLevelVolumes(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	volume := cmd.AddEntity(&ChangeLevelVolumeComponent{
		TargetName: "exit_a",
		TargetMap:  "c1a1",
		Enabled:    false,
	})
	app.FlushCommands()

	ActivateTargetWithState(cmd, "exit_a", 0, 1)
	app.FlushCommands()
	change := cmd.GetComponent(volume, reflect.TypeOf(ChangeLevelVolumeComponent{})).(*ChangeLevelVolumeComponent)
	if change == nil || !change.Enabled || change.ActivationCount != 1 {
		t.Fatalf("expected target state on to enable changelevel volume, got %+v", change)
	}

	ActivateTargetWithState(cmd, "exit_a", 0, 0)
	app.FlushCommands()
	if change.Enabled || change.ActivationCount != 2 {
		t.Fatalf("expected target state off to disable changelevel volume, got %+v", change)
	}

	KillTarget(cmd, "exit_a")
	app.FlushCommands()
	if comps := cmd.GetAllComponents(volume); len(comps) != 0 {
		t.Fatalf("expected killtarget to remove changelevel volume, got %+v", comps)
	}
}

func TestTargetActivationControlsChargers(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	charger := cmd.AddEntity(&ChargerComponent{
		TargetName: "charger_a",
		ChargeKind: "health",
		Enabled:    false,
	})
	app.FlushCommands()

	ActivateTargetWithState(cmd, "charger_a", 0, 1)
	app.FlushCommands()
	component := cmd.GetComponent(charger, reflect.TypeOf(ChargerComponent{})).(*ChargerComponent)
	if component == nil || !component.Enabled || component.ActivationCount != 1 {
		t.Fatalf("expected target state on to enable charger, got %+v", component)
	}

	ActivateTargetWithState(cmd, "charger_a", 0, 0)
	app.FlushCommands()
	if component.Enabled || component.ActivationCount != 2 {
		t.Fatalf("expected target state off to disable charger, got %+v", component)
	}

	KillTarget(cmd, "charger_a")
	app.FlushCommands()
	if comps := cmd.GetAllComponents(charger); len(comps) != 0 {
		t.Fatalf("expected killtarget to remove charger, got %+v", comps)
	}
}

func TestMovingBrushFollowsPathNodes(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	brush := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{},
		&MovingBrushComponent{
			Kind:       "hl1_func_train",
			MotionKind: "path",
			PathTarget: "corner_a",
			Speed:      2,
			Open:       true,
		},
	)
	cmd.AddEntity(&PathNodeComponent{
		TargetName: "corner_a",
		Target:     "corner_b",
		Position:   mgl32.Vec3{2, 0, 0},
	})
	cmd.AddEntity(&PathNodeComponent{
		TargetName: "corner_b",
		Position:   mgl32.Vec3{2, 0, -2},
	})
	app.FlushCommands()

	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	app.FlushCommands()
	tr := cmd.GetComponent(brush, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	moving := cmd.GetComponent(brush, reflect.TypeOf(MovingBrushComponent{})).(*MovingBrushComponent)
	if tr.Position != (mgl32.Vec3{2, 0, 0}) || moving.PathTarget != "corner_b" || !moving.Open {
		t.Fatalf("expected train to reach first path node, tr=%+v brush=%+v", tr, moving)
	}

	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	app.FlushCommands()
	if tr.Position != (mgl32.Vec3{2, 0, -2}) || moving.PathTarget != "" || moving.Open {
		t.Fatalf("expected train to stop at last path node, tr=%+v brush=%+v", tr, moving)
	}
}

func TestMovingBrushRotatesToOpenAngle(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	brush := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{},
		&MovingBrushComponent{
			Kind:              "hl1_func_door_rotating",
			MotionKind:        "rotate",
			BoundsCenter:      mgl32.Vec3{1, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{0.5, 1, 0.1},
			RotationOrigin:    mgl32.Vec3{0, 0, 0},
			RotationAxis:      mgl32.Vec3{0, 1, 0},
			OpenAngle:         90,
			Speed:             90,
			Open:              true,
		},
	)
	app.FlushCommands()

	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	app.FlushCommands()
	tr := cmd.GetComponent(brush, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	moving := cmd.GetComponent(brush, reflect.TypeOf(MovingBrushComponent{})).(*MovingBrushComponent)
	if absf(moving.CurrentAngle-90) > 0.001 || tr.Position.Sub(mgl32.Vec3{0, 0, -1}).Len() > 0.001 || moving.BoundsCenter.Sub(mgl32.Vec3{0, 0, -1}).Len() > 0.001 || moving.BoundsHalfExtents.Sub(mgl32.Vec3{0.1, 1, 0.5}).Len() > 0.001 || !MovingBrushFullyOpen(moving) {
		t.Fatalf("expected rotating door to reach open angle, tr=%+v brush=%+v", tr, moving)
	}
}

func TestBreakableDamageAndTargetActivation(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	breakable := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&BreakableComponent{
			Health:      30,
			MaxHealth:   30,
			TargetName:  "crate_a",
			Target:      "door_a",
			SpawnObject: "4",
			SpawnFlags:  1,
		},
	)
	cmd.AddEntity(&MovingBrushComponent{TargetName: "door_a"})
	app.FlushCommands()

	handled, broken := DamageBreakableEntity(cmd, breakable, 30, 42)
	if !handled || broken {
		t.Fatalf("expected only-trigger breakable to handle but ignore weapon damage, handled=%v broken=%v", handled, broken)
	}
	got := cmd.GetComponent(breakable, reflect.TypeOf(BreakableComponent{})).(*BreakableComponent)
	if got.Health != 30 {
		t.Fatalf("expected weapon damage to be ignored, got %+v", got)
	}

	ActivateTarget(cmd, "crate_a", 0)
	app.FlushCommands()
	if comps := cmd.GetAllComponents(breakable); len(comps) != 0 {
		t.Fatalf("expected targeted breakable to be removed, got %+v", comps)
	}
	var doorOpen bool
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		doorOpen = brush.Open
		return true
	})
	if !doorOpen {
		t.Fatal("expected break target to open linked door")
	}
	var pickupCount int
	MakeQuery1[PickupComponent](cmd).Map(func(_ EntityId, pickup *PickupComponent) bool {
		pickupCount++
		if pickup.ClassName != "ammo_9mmclip" || pickup.Category != "ammo" || pickup.Item != "9mmclip" || pickup.Amount != 17 {
			t.Fatalf("unexpected spawned pickup: %+v", pickup)
		}
		return true
	})
	if pickupCount != 1 {
		t.Fatalf("expected one pickup spawned from breakable, got %d", pickupCount)
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

func TestMovingBrushReturnsAfterOpenWait(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	closed := mgl32.Vec3{1, 1, 1}
	eid := cmd.AddEntity(
		&TransformComponent{Position: closed, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&MovingBrushComponent{ClosedPosition: closed, OpenOffset: mgl32.Vec3{0, 2, 0}, Speed: 2, Wait: 3, TargetName: "lift"},
	)
	app.FlushCommands()

	ActivateTarget(cmd, "lift", 0)
	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	if tr := transformForEntityMust(t, cmd, eid); tr.Position != (mgl32.Vec3{1, 3, 1}) {
		t.Fatalf("expected lift at open position, got %v", tr.Position)
	}
	movingBrushMotionSystem(cmd, &Time{Dt: 2})
	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	movingBrushMotionSystem(cmd, &Time{Dt: 1})
	if tr := transformForEntityMust(t, cmd, eid); tr.Position != closed {
		t.Fatalf("expected lift to return to closed position, got %v", tr.Position)
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
