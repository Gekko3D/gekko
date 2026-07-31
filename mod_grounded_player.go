package gekko

import (
	"math"
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type GroundedCharacterMotorConfig struct {
	Height           float32
	EyeHeight        float32
	CrouchHeight     float32
	CrouchEyeHeight  float32
	CrouchSpeedScale float32
	Radius           float32
	Speed            float32
	SwimSpeed        float32
	SprintMultiplier float32
	Sensitivity      float32
	JumpSpeed        float32
	Gravity          float32
	StepHeight       float32
	GroundProbe      float32
}

type GroundedPlayerControllerConfig = GroundedCharacterMotorConfig

type GroundedCharacterMotorDefaults struct {
	Config GroundedCharacterMotorConfig
}

type GroundedPlayerControllerDefaults = GroundedCharacterMotorDefaults

// GroundedCharacterMotorModule installs camera-free character movement.
// Games write GroundedCharacterIntentComponent from human or bot intent.
type GroundedCharacterMotorModule struct {
	Config GroundedCharacterMotorConfig
}

type GroundedPlayerControllerModule struct {
	Config GroundedPlayerControllerConfig
}

type GroundedCharacterIntentComponent struct {
	MoveDirection  mgl32.Vec3
	MaxDistance    float32
	AimDirection   mgl32.Vec3
	LadderMovement float32
	// ForceLadder is a compatibility bridge. New authored traversal starts a
	// CharacterTraversalLadder request instead.
	ForceLadder bool
	Jump        bool
	Crouch      bool
	Sprint      bool
	SwimUp      bool
}

type GroundedCharacterMotorComponent struct {
	// Height and EyeHeight are active capsule/camera dimensions. StandingHeight
	// and StandingEyeHeight preserve the configured values while crouched.
	Height            float32
	EyeHeight         float32
	StandingHeight    float32
	StandingEyeHeight float32
	CrouchHeight      float32
	CrouchEyeHeight   float32
	CrouchSpeedScale  float32
	Radius            float32
	Speed             float32
	SwimSpeed         float32
	SprintMultiplier  float32
	Sensitivity       float32
	JumpSpeed         float32
	Gravity           float32
	StepHeight        float32
	GroundProbe       float32

	MoveInput          mgl32.Vec2
	LookInput          mgl32.Vec2
	JumpQueued         bool
	CrouchRequested    bool
	SprintRequested    bool
	SwimUpRequested    bool
	Crouching          bool
	Swimming           bool
	WaterEntity        EntityId
	VerticalVelocity   float32
	Grounded           bool
	GroundPoint        mgl32.Vec3
	HasGroundPoint     bool
	GroundContacts     [CharacterGroundContactCapacity]CharacterGroundContact
	GroundContactCount int
	NeedsGroundSnap    bool
	OnLadder           bool
	LadderEntity       EntityId
	LadderClimbSpeed   float32
	ActualVelocity     mgl32.Vec3
	MotionMode         CharacterMotionMode
	Traversal          CharacterTraversalComponent
	LadderAvailable    bool
	AvailableLadder    EntityId
	// ScriptedMovement is a compatibility bridge for consumers not yet moved
	// to CharacterMotionKinematic.
	ScriptedMovement bool
	// CollisionIgnoredEntity is a presentation subtree excluded from this
	// controller's movement and ground probes.
	CollisionIgnoredEntity EntityId
}

type GroundedPlayerControllerComponent = GroundedCharacterMotorComponent

// CharacterControllerIgnoredComponent excludes an entity from grounded
// character-controller raycasts without changing its rendering or physics use.
type CharacterControllerIgnoredComponent struct{}

func DefaultGroundedCharacterMotorConfig() GroundedCharacterMotorConfig {
	return GroundedCharacterMotorConfig{
		Height:           1.8,
		EyeHeight:        1.7,
		CrouchHeight:     1.0,
		CrouchEyeHeight:  0.9,
		CrouchSpeedScale: 0.5,
		Radius:           0.35,
		Speed:            5.5,
		SwimSpeed:        3.5,
		SprintMultiplier: 1.6,
		Sensitivity:      0.1,
		JumpSpeed:        5.5,
		Gravity:          18.0,
		StepHeight:       0.6,
		GroundProbe:      0.15,
	}
}

func DefaultGroundedPlayerControllerConfig() GroundedPlayerControllerConfig {
	return DefaultGroundedCharacterMotorConfig()
}

func effectiveGroundedPlayerControllerConfig(cfg GroundedPlayerControllerConfig) GroundedPlayerControllerConfig {
	defaults := DefaultGroundedPlayerControllerConfig()
	if cfg.Height != 0 {
		defaults.Height = cfg.Height
	}
	if cfg.EyeHeight != 0 {
		defaults.EyeHeight = cfg.EyeHeight
	}
	if cfg.CrouchHeight != 0 {
		defaults.CrouchHeight = cfg.CrouchHeight
	}
	if cfg.CrouchEyeHeight != 0 {
		defaults.CrouchEyeHeight = cfg.CrouchEyeHeight
	}
	if cfg.CrouchSpeedScale != 0 {
		defaults.CrouchSpeedScale = cfg.CrouchSpeedScale
	}
	if cfg.Radius != 0 {
		defaults.Radius = cfg.Radius
	}
	if cfg.Speed != 0 {
		defaults.Speed = cfg.Speed
	}
	if cfg.SwimSpeed != 0 {
		defaults.SwimSpeed = cfg.SwimSpeed
	}
	if cfg.SprintMultiplier != 0 {
		defaults.SprintMultiplier = cfg.SprintMultiplier
	}
	if cfg.Sensitivity != 0 {
		defaults.Sensitivity = cfg.Sensitivity
	}
	if cfg.JumpSpeed != 0 {
		defaults.JumpSpeed = cfg.JumpSpeed
	}
	if cfg.Gravity != 0 {
		defaults.Gravity = cfg.Gravity
	}
	if cfg.StepHeight != 0 {
		defaults.StepHeight = cfg.StepHeight
	}
	if cfg.GroundProbe != 0 {
		defaults.GroundProbe = cfg.GroundProbe
	}
	return defaults
}

func installGroundedCharacterMotor(app *App, cmd *Commands, cfg GroundedCharacterMotorConfig) {
	if app != nil {
		if _, ok := app.resources[reflect.TypeOf(GroundedPlayerControllerDefaults{})]; !ok {
			cmd.AddResources(&GroundedPlayerControllerDefaults{
				Config: effectiveGroundedPlayerControllerConfig(cfg),
			})
		}
	}
	app.UseSystem(System(groundedCharacterMotorSystem).InStage(Update).RunAlways())
	app.UseSystem(System(triggerVolumeTouchSystem).InStage(Update).RunAlways())
	app.UseSystem(System(targetEventSystem).InStage(Update).RunAlways())
	app.UseSystem(System(movingBrushMotionSystem).InStage(Update).RunAlways())
}

func (mod GroundedCharacterMotorModule) Install(app *App, cmd *Commands) {
	installGroundedCharacterMotor(app, cmd, mod.Config)
}

func (mod GroundedPlayerControllerModule) Install(app *App, cmd *Commands) {
	app.UseSystem(System(groundedPlayerInputSystem).InStage(Update).RunAlways())
	installGroundedCharacterMotor(app, cmd, mod.Config)
	app.UseSystem(System(groundedPlayerUseSystem).InStage(Update).RunAlways())
}

func SpawnGroundedPlayerAtMarker(cmd *Commands, marker content.LevelMarkerDef) EntityId {
	return SpawnGroundedPlayerAtMarkerWithConfig(cmd, marker, groundedPlayerConfigFromApp(cmd.app))
}

func SpawnGroundedPlayerAtMarkerWithConfig(cmd *Commands, marker content.LevelMarkerDef, cfg GroundedPlayerControllerConfig) EntityId {
	transform := levelTransformToComponent(marker.Transform)
	transform.Scale = mgl32.Vec3{1, 1, 1}
	forward := forwardFromYawPitch(0, 0)
	cfg = effectiveGroundedPlayerControllerConfig(cfg)
	ctrl := GroundedPlayerControllerComponent{
		Height:            cfg.Height,
		EyeHeight:         cfg.EyeHeight,
		StandingHeight:    cfg.Height,
		StandingEyeHeight: cfg.EyeHeight,
		CrouchHeight:      cfg.CrouchHeight,
		CrouchEyeHeight:   cfg.CrouchEyeHeight,
		CrouchSpeedScale:  cfg.CrouchSpeedScale,
		Radius:            cfg.Radius,
		Speed:             cfg.Speed,
		SwimSpeed:         cfg.SwimSpeed,
		SprintMultiplier:  cfg.SprintMultiplier,
		Sensitivity:       cfg.Sensitivity,
		JumpSpeed:         cfg.JumpSpeed,
		Gravity:           cfg.Gravity,
		StepHeight:        cfg.StepHeight,
		GroundProbe:       cfg.GroundProbe,
		Grounded:          true,
		NeedsGroundSnap:   true,
	}
	local := LocalTransformComponent{
		Position: transform.Position,
		Rotation: transform.Rotation,
		Scale:    transform.Scale,
	}
	return cmd.AddEntity(
		&transform,
		&local,
		&CameraComponent{
			Position: transform.Position.Add(mgl32.Vec3{0, ctrl.EyeHeight, 0}),
			LookAt:   transform.Position.Add(mgl32.Vec3{0, ctrl.EyeHeight, 0}).Add(forward),
			Up:       mgl32.Vec3{0, 1, 0},
			Yaw:      0,
			Pitch:    0,
			Fov:      75,
			Aspect:   16.0 / 9.0,
			Near:     0.05,
			Far:      1000,
		},
		&ctrl,
		&StreamedLevelObserverComponent{Radius: 0},
	)
}

func groundedPlayerConfigFromApp(app *App) GroundedPlayerControllerConfig {
	if app != nil {
		if resource, ok := app.resources[reflect.TypeOf(GroundedPlayerControllerDefaults{})]; ok {
			if defaults, ok := resource.(*GroundedPlayerControllerDefaults); ok && defaults != nil {
				return defaults.Config
			}
		}
	}
	return DefaultGroundedPlayerControllerConfig()
}

func groundedPlayerInputSystem(input *Input, cmd *Commands) {
	if input == nil {
		return
	}
	if input.JustPressed[KeyTab] {
		input.MouseCaptured = !input.MouseCaptured
	}
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, _ *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		ctrl.MoveInput = mgl32.Vec2{}
		if input.Pressed[KeyA] {
			ctrl.MoveInput[0] -= 1
		}
		if input.Pressed[KeyD] {
			ctrl.MoveInput[0] += 1
		}
		if input.Pressed[KeyW] {
			ctrl.MoveInput[1] += 1
		}
		if input.Pressed[KeyS] {
			ctrl.MoveInput[1] -= 1
		}
		ctrl.LookInput = mgl32.Vec2{}
		if input.MouseCaptured {
			ctrl.LookInput[0] = float32(input.MouseDeltaX)
			ctrl.LookInput[1] = float32(input.MouseDeltaY)
		}
		ctrl.JumpQueued = input.JustPressed[KeySpace]
		ctrl.CrouchRequested = input.Pressed[KeyControl]
		ctrl.SprintRequested = input.Pressed[KeyShift]
		ctrl.SwimUpRequested = input.Pressed[KeySpace]
		return true
	})
}

func groundedCharacterMotorSystem(cmd *Commands, time *Time, voxRt *VoxelRtState) {
	groundedPlayerControlSystem(cmd, time, nil, voxRt)
}

func groundedPlayerControlSystem(cmd *Commands, time *Time, input *Input, voxRt *VoxelRtState) {
	if time == nil {
		return
	}
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}
	MakeQuery1[GroundedCharacterMotorComponent](cmd).Map(func(eid EntityId, ctrl *GroundedCharacterMotorComponent) bool {
		cam, _ := cmd.GetComponent(eid, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
		intent, _ := cmd.GetComponent(eid, reflect.TypeOf(GroundedCharacterIntentComponent{})).(*GroundedCharacterIntentComponent)
		if cam != nil && intent == nil {
			applyGroundedLook(cam, ctrl)
		}
		basePos := groundedPlayerBasePosition(cmd, eid, cam, ctrl)
		startPos := basePos
		if !ctrl.Traversal.Running() && (ctrl.ScriptedMovement || ctrl.MotionMode == CharacterMotionKinematic) {
			ctrl.JumpQueued = false
			ctrl.SwimUpRequested = false
			ctrl.VerticalVelocity = 0
			ctrl.ActualVelocity = mgl32.Vec3{}
			if intent != nil {
				intent.Jump = false
			}
			groundedPlayerApplyTransform(cmd, eid, cam, ctrl, basePos)
			return true
		}
		if !ctrl.Traversal.Running() && ctrl.MotionMode != CharacterMotionNormal {
			ctrl.MotionMode = CharacterMotionNormal
		}
		if intent != nil {
			ctrl.JumpQueued = ctrl.JumpQueued || intent.Jump
			ctrl.CrouchRequested = intent.Crouch
			ctrl.SwimUpRequested = intent.SwimUp
		}
		collisionFilter := groundedPlayerCollisionRaycastFilter(cmd, ctrl)
		groundedPlayerUpdateStance(cmd, voxRt, basePos, ctrl, collisionFilter)
		waterEntity, _, swimming := findGroundedPlayerWaterBody(cmd, basePos, ctrl)
		ctrl.Swimming = swimming
		if swimming {
			ctrl.WaterEntity = waterEntity
		} else {
			ctrl.WaterEntity = 0
		}

		flatForward := mgl32.Vec3{0, 0, -1}
		if cam != nil {
			flatForward = forwardFromYawPitch(cam.Yaw, 0)
		} else if intent != nil && intent.AimDirection.LenSqr() > 1e-8 {
			flatForward = intent.AimDirection
			flatForward[1] = 0
			if flatForward.LenSqr() > 1e-8 {
				flatForward = flatForward.Normalize()
			} else {
				flatForward = mgl32.Vec3{0, 0, -1}
			}
		}
		right := flatForward.Cross(mgl32.Vec3{0, 1, 0}).Normalize()
		speed := defaulted(ctrl.Speed, 5.5)
		if ctrl.Swimming {
			speed = defaulted(ctrl.SwimSpeed, 3.5)
		} else if ctrl.Crouching {
			speed *= defaulted(ctrl.CrouchSpeedScale, 0.5)
		} else if (intent != nil && intent.Sprint) || (intent == nil && ctrl.SprintRequested) {
			speed *= defaulted(ctrl.SprintMultiplier, 1.6)
		}
		move := right.Mul(ctrl.MoveInput[0]).Add(flatForward.Mul(ctrl.MoveInput[1]))
		ladderMovement := ctrl.MoveInput[1]
		if intent != nil {
			move = intent.MoveDirection
			move[1] = 0
			ladderMovement = intent.LadderMovement
		}
		if move.Len() > 1 {
			move = move.Normalize()
		}
		horizontalMove := move.Mul(speed * dt)
		if intent != nil && intent.MaxDistance > 0 && horizontalMove.Len() > intent.MaxDistance {
			horizontalMove = horizontalMove.Normalize().Mul(intent.MaxDistance)
		}
		ladderHorizontalMove := right.Mul(move.Dot(right) * speed * 0.5 * dt)
		if intent != nil && intent.MaxDistance > 0 && ladderHorizontalMove.Len() > intent.MaxDistance {
			ladderHorizontalMove = ladderHorizontalMove.Normalize().Mul(intent.MaxDistance)
		}
		ladderEntity, ladder, onLadder := findGroundedPlayerLadderVolume(cmd, basePos, ctrl)
		forcedLadder := intent != nil && intent.ForceLadder && intent.LadderMovement != 0
		ctrl.LadderAvailable, ctrl.AvailableLadder = onLadder, ladderEntity
		if !ctrl.Traversal.Running() && !ctrl.Swimming && ladderMovement != 0 && (onLadder || forcedLadder) {
			speed := DefaultLadderClimbSpeed
			if onLadder {
				speed = ladder.NormalizedClimbSpeed()
			}
			CharacterBeginTraversal(ctrl, CharacterTraversalRequest{
				Kind: CharacterTraversalLadder, Start: basePos, Entry: basePos,
				Speed: speed, LadderEntity: ladderEntity, Manual: true,
			})
		}
		if ctrl.Traversal.Running() {
			advanceGroundedCharacterTraversal(
				cmd, voxRt, &basePos, ctrl, ladderHorizontalMove, ladderMovement, dt,
				collisionFilter, ladderEntity, onLadder, forcedLadder,
			)
			ctrl.ActualVelocity = basePos.Sub(startPos).Mul(1 / dt)
			if intent != nil {
				intent.Jump = false
			}
			groundedPlayerApplyTransform(cmd, eid, cam, ctrl, basePos)
			return true
		}

		if ctrl.Swimming {
			ctrl.OnLadder = false
			ctrl.LadderEntity = 0
			ctrl.LadderClimbSpeed = 0
			basePos = tryGroundedHorizontalMove(cmd, voxRt, basePos, horizontalMove, ctrl, collisionFilter)
			resolveGroundedSwimMovement(cmd, voxRt, &basePos, ctrl, dt, collisionFilter)
		} else {
			ctrl.OnLadder = false
			ctrl.LadderEntity = 0
			ctrl.LadderClimbSpeed = 0
			basePos = CharacterGroundedMove(voxRt, basePos, horizontalMove, CharacterGroundedMoveOptions{
				CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
				GroundConfig:    groundedPlayerGroundProbeConfig(ctrl),
				AcceptEntity:    collisionFilter,
			}).Position
			resolveGroundedVertical(cmd, voxRt, &basePos, ctrl, dt, collisionFilter)
		}

		ctrl.ActualVelocity = basePos.Sub(startPos).Mul(1 / dt)
		if intent != nil {
			intent.Jump = false
		}
		groundedPlayerApplyTransform(cmd, eid, cam, ctrl, basePos)
		return true
	})
}

func groundedPlayerApplyTransform(cmd *Commands, eid EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent, basePos mgl32.Vec3) {
	if cmd == nil || ctrl == nil {
		return
	}
	if cam != nil {
		cam.Position = basePos.Add(mgl32.Vec3{0, maxf(ctrl.EyeHeight, 0.01), 0})
		cam.LookAt = cam.Position.Add(forwardFromYawPitch(cam.Yaw, cam.Pitch))
		cam.Up = mgl32.Vec3{0, 1, 0}
	}
	if tr, ok := transformForEntity(cmd, eid); ok {
		tr.Position = basePos
		tr.Rotation = mgl32.QuatIdent()
		tr.Scale = mgl32.Vec3{1, 1, 1}
	}
	if local, ok := localTransformForEntity(cmd, eid); ok {
		local.Position = basePos
		local.Rotation = mgl32.QuatIdent()
		local.Scale = mgl32.Vec3{1, 1, 1}
	}
}

// groundedPlayerBasePosition keeps physics owned by the controller transform.
// The camera can follow a presentation anchor without moving the capsule.
func groundedPlayerBasePosition(cmd *Commands, eid EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) mgl32.Vec3 {
	if tr, ok := transformForEntity(cmd, eid); ok && tr != nil {
		return tr.Position
	}
	if cam != nil {
		return cam.Position.Sub(mgl32.Vec3{0, maxf(ctrl.EyeHeight, 0.01), 0})
	}
	return mgl32.Vec3{}
}

func groundedPlayerUseSystem(cmd *Commands, input *Input) {
	if cmd == nil || input == nil || !input.JustPressed[KeyE] {
		return
	}
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(entity EntityId, cam *CameraComponent, _ *GroundedPlayerControllerComponent) bool {
		return !GroundedCharacterUse(cmd, entity, cam.Position, forwardFromYawPitch(cam.Yaw, cam.Pitch), 2.2)
	})
}

// GroundedCharacterUse activates the nearest authored interaction along an
// actor-owned aim ray. It does not require or create a render camera.
func GroundedCharacterUse(cmd *Commands, activator EntityId, origin, direction mgl32.Vec3, maxDistance float32) bool {
	if cmd == nil || direction.LenSqr() <= 1e-8 || maxDistance <= 0 {
		return false
	}
	if hit, ok := findUseTriggerHit(cmd, origin, direction, maxDistance); ok {
		ActivateUseTrigger(cmd, hit.Trigger, activator)
		return true
	}
	if hit, ok := findMovingBrushUseHit(cmd, origin, direction, maxDistance); ok {
		hit.Brush.ActivationCount++
		if !hit.Brush.DoesNotMove() {
			hit.Brush.Open = !hit.Brush.Open
		}
		if hit.Brush.Target != "" {
			ActivateTarget(cmd, hit.Brush.Target, activator)
		}
		return true
	}
	return false
}

// GroundedLocalPlayerUseSystem preserves camera-aimed authored interaction for
// games that provide their own actor input adapter around the shared motor.
func GroundedLocalPlayerUseSystem(cmd *Commands, input *Input) {
	groundedPlayerUseSystem(cmd, input)
}

// ActivateUseTrigger applies the same authored interaction for players, NPCs,
// and other gameplay systems.
func ActivateUseTrigger(cmd *Commands, trigger *UseTriggerComponent, activator EntityId) {
	ActivateUseTriggerWithState(cmd, trigger, activator, 2)
}

// ActivateUseTriggerWithState preserves trigger/button side effects while
// requesting an idempotent target state instead of blind toggling.
func ActivateUseTriggerWithState(cmd *Commands, trigger *UseTriggerComponent, activator EntityId, triggerState int) {
	if cmd == nil || trigger == nil {
		return
	}
	trigger.ActivationCount++
	activateMovingBrushAtBounds(cmd, trigger.BoundsCenter, trigger.BoundsHalfExtents)
	ActivateTargetWithState(cmd, trigger.Target, activator, triggerState)
}

type useTriggerHit struct {
	Entity  EntityId
	Trigger *UseTriggerComponent
	T       float32
}

type movingBrushUseHit struct {
	Entity EntityId
	Brush  *MovingBrushComponent
	T      float32
}

func findUseTriggerHit(cmd *Commands, origin, dir mgl32.Vec3, maxDistance float32) (useTriggerHit, bool) {
	var best useTriggerHit
	MakeQuery1[UseTriggerComponent](cmd).Map(func(eid EntityId, trigger *UseTriggerComponent) bool {
		if trigger == nil {
			return true
		}
		t, ok := rayAABB(origin, dir, trigger.BoundsCenter.Sub(trigger.BoundsHalfExtents), trigger.BoundsCenter.Add(trigger.BoundsHalfExtents), maxDistance)
		if !ok {
			return true
		}
		if best.Trigger == nil || t < best.T {
			best = useTriggerHit{Entity: eid, Trigger: trigger, T: t}
		}
		return true
	})
	return best, best.Trigger != nil
}

func findMovingBrushUseHit(cmd *Commands, origin, dir mgl32.Vec3, maxDistance float32) (movingBrushUseHit, bool) {
	var best movingBrushUseHit
	MakeQuery1[MovingBrushComponent](cmd).Map(func(eid EntityId, brush *MovingBrushComponent) bool {
		if brush == nil {
			return true
		}
		t, ok := rayAABB(origin, dir, brush.BoundsCenter.Sub(brush.BoundsHalfExtents), brush.BoundsCenter.Add(brush.BoundsHalfExtents), maxDistance)
		if !ok {
			return true
		}
		if best.Brush == nil || t < best.T {
			best = movingBrushUseHit{Entity: eid, Brush: brush, T: t}
		}
		return true
	})
	return best, best.Brush != nil
}

func activateMovingBrushTarget(cmd *Commands, target string) {
	ActivateTarget(cmd, target, 0)
}

func activateMovingBrushAtBounds(cmd *Commands, center, halfExtents mgl32.Vec3) {
	if cmd == nil {
		return
	}
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		if brush == nil {
			return true
		}
		if !aabbOverlap(center.Sub(halfExtents), center.Add(halfExtents), brush.BoundsCenter.Sub(brush.BoundsHalfExtents), brush.BoundsCenter.Add(brush.BoundsHalfExtents)) {
			return true
		}
		if !brush.DoesNotMove() {
			brush.Open = !brush.Open
		}
		brush.ActivationCount++
		return false
	})
}

func rayAABB(origin, dir, minB, maxB mgl32.Vec3, maxDistance float32) (float32, bool) {
	if dir.LenSqr() <= 1e-8 || maxDistance <= 0 {
		return 0, false
	}
	dir = dir.Normalize()
	tMin := float32(0)
	tMax := maxDistance
	for axis := 0; axis < 3; axis++ {
		if math.Abs(float64(dir[axis])) < 1e-6 {
			if origin[axis] < minB[axis] || origin[axis] > maxB[axis] {
				return 0, false
			}
			continue
		}
		invD := 1 / dir[axis]
		t0 := (minB[axis] - origin[axis]) * invD
		t1 := (maxB[axis] - origin[axis]) * invD
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		tMin = maxf(tMin, t0)
		tMax = minf(tMax, t1)
		if tMax < tMin {
			return 0, false
		}
	}
	return tMin, true
}

func findGroundedPlayerLadderVolume(cmd *Commands, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent) (EntityId, LadderVolumeComponent, bool) {
	if cmd == nil || ctrl == nil {
		return 0, LadderVolumeComponent{}, false
	}
	radius := defaulted(ctrl.Radius, 0.35)
	height := defaulted(ctrl.Height, 1.8)
	playerMin := basePos.Add(mgl32.Vec3{-radius, 0, -radius})
	playerMax := basePos.Add(mgl32.Vec3{radius, height, radius})
	var foundEntity EntityId
	var foundLadder LadderVolumeComponent
	MakeQuery1[LadderVolumeComponent](cmd).Map(func(eid EntityId, ladder *LadderVolumeComponent) bool {
		if ladder == nil {
			return true
		}
		center := ladder.BoundsCenter
		extents := ladder.BoundsHalfExtents.Add(mgl32.Vec3{0.05, 0.05, 0.05})
		ladderMin := center.Sub(extents)
		ladderMax := center.Add(extents)
		if aabbOverlap(playerMin, playerMax, ladderMin, ladderMax) {
			foundEntity = eid
			foundLadder = *ladder
			return false
		}
		return true
	})
	return foundEntity, foundLadder, foundEntity != 0
}

func groundedPlayerUpdateStance(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool) {
	if ctrl == nil {
		return
	}
	standingHeight := defaulted(ctrl.StandingHeight, ctrl.Height)
	standingEyeHeight := defaulted(ctrl.StandingEyeHeight, ctrl.EyeHeight)
	if ctrl.StandingHeight == 0 {
		ctrl.StandingHeight = standingHeight
	}
	if ctrl.StandingEyeHeight == 0 {
		ctrl.StandingEyeHeight = standingEyeHeight
	}
	if ctrl.CrouchRequested {
		ctrl.Crouching = true
		ctrl.Height = minf(defaulted(ctrl.CrouchHeight, standingHeight*0.5), standingHeight)
		ctrl.EyeHeight = minf(defaulted(ctrl.CrouchEyeHeight, standingEyeHeight*0.5), standingEyeHeight)
		return
	}
	if !ctrl.Crouching || !groundedPlayerCanStand(cmd, voxRt, basePos, ctrl, acceptEntity) {
		return
	}
	ctrl.Crouching = false
	ctrl.Height = standingHeight
	ctrl.EyeHeight = standingEyeHeight
}

func groundedPlayerCanStand(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool) bool {
	if ctrl == nil {
		return true
	}
	standingHeight := defaulted(ctrl.StandingHeight, ctrl.Height)
	currentHeight := defaulted(ctrl.Height, standingHeight)
	clearanceHeight := standingHeight - currentHeight
	if clearanceHeight <= 1e-5 {
		return true
	}
	for _, offset := range CharacterVerticalCollisionOffsets(defaulted(ctrl.Radius, 0.35)) {
		origin := basePos.Add(offset).Add(mgl32.Vec3{0, currentHeight, 0})
		if hit := characterRaycastFiltered(voxRt, origin, mgl32.Vec3{0, 1, 0}, clearanceHeight+0.03, acceptEntity, MovingBrushCollisionQuery(cmd)); hit.Hit && hit.T <= clearanceHeight+0.03 {
			return false
		}
	}
	return true
}

func findGroundedPlayerWaterBody(cmd *Commands, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent) (EntityId, waterInteractionBody, bool) {
	if cmd == nil || ctrl == nil {
		return 0, waterInteractionBody{}, false
	}
	radius := defaulted(ctrl.Radius, 0.35)
	height := defaulted(ctrl.Height, 1.8)
	for _, water := range collectWaterInteractionBodies(cmd) {
		if basePos.X()+radius < water.Center.X()-water.HalfExtents[0] ||
			basePos.X()-radius > water.Center.X()+water.HalfExtents[0] ||
			basePos.Z()+radius < water.Center.Z()-water.HalfExtents[1] ||
			basePos.Z()-radius > water.Center.Z()+water.HalfExtents[1] {
			continue
		}
		if water.SurfaceY < basePos.Y()+height*0.5 || water.BottomY > basePos.Y()+height {
			continue
		}
		return water.Entity, water, true
	}
	return 0, waterInteractionBody{}, false
}

func resolveGroundedLadderMovement(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, movement, dt float32, acceptEntity func(EntityId, bool) bool) {
	if basePos == nil || ctrl == nil {
		return
	}
	if ctrl.JumpQueued {
		ctrl.OnLadder = false
		ctrl.LadderEntity = 0
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = defaulted(ctrl.JumpSpeed, 5.5) * 0.5
		nextBase, blocked := tryGroundedVerticalMove(cmd, voxRt, *basePos, ctrl.VerticalVelocity*dt, ctrl, acceptEntity)
		*basePos = nextBase
		if blocked {
			ctrl.VerticalVelocity = 0
		}
		ctrl.JumpQueued = false
		return
	}
	*basePos, _ = tryGroundedVerticalMove(cmd, voxRt, *basePos, movement*defaulted(ctrl.LadderClimbSpeed, DefaultLadderClimbSpeed)*dt, ctrl, acceptEntity)
	ctrl.VerticalVelocity = 0
	ctrl.Grounded = false
	groundedPlayerClearGroundSupport(ctrl)
	ctrl.NeedsGroundSnap = false
	ctrl.JumpQueued = false
}

func resolveGroundedSwimMovement(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, dt float32, acceptEntity func(EntityId, bool) bool) {
	if basePos == nil || ctrl == nil {
		return
	}
	vertical := float32(0)
	if ctrl.SwimUpRequested {
		vertical += defaulted(ctrl.SwimSpeed, 3.5)
	}
	if ctrl.CrouchRequested {
		vertical -= defaulted(ctrl.SwimSpeed, 3.5)
	}
	*basePos, _ = tryGroundedVerticalMove(cmd, voxRt, *basePos, vertical*dt, ctrl, acceptEntity)
	ctrl.VerticalVelocity = 0
	ctrl.Grounded = false
	groundedPlayerClearGroundSupport(ctrl)
	ctrl.NeedsGroundSnap = false
	ctrl.JumpQueued = false
}

func advanceGroundedCharacterTraversal(
	cmd *Commands,
	voxRt *VoxelRtState,
	basePos *mgl32.Vec3,
	ctrl *GroundedCharacterMotorComponent,
	horizontalMove mgl32.Vec3,
	ladderMovement, dt float32,
	acceptEntity func(EntityId, bool) bool,
	availableLadder EntityId,
	onLadder, forceLadder bool,
) {
	if basePos == nil || ctrl == nil || !ctrl.Traversal.Running() {
		return
	}
	traversal := &ctrl.Traversal
	traversal.Elapsed += dt
	traversal.Blocked = false
	if traversal.Request.MaxDuration > 0 && traversal.Elapsed > traversal.Request.MaxDuration && traversal.PendingReason == "" {
		if traversal.Committed || !ctrl.Grounded {
			traversal.PendingReason = "traversal_timeout"
			traversal.Phase = CharacterTraversalPhaseSettle
		} else {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "traversal_timeout")
			return
		}
	}
	switch traversal.Request.Kind {
	case CharacterTraversalDrop, CharacterTraversalJump:
		advanceGroundedBallisticTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	case CharacterTraversalLadder:
		advanceGroundedLadderTraversal(cmd, voxRt, basePos, ctrl, horizontalMove, ladderMovement, dt, acceptEntity, availableLadder, onLadder, forceLadder)
	case CharacterTraversalVault, CharacterTraversalMantle:
		advanceGroundedKinematicTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	default:
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "unsupported_traversal")
	}
}

func advanceGroundedBallisticTraversal(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, dt float32, acceptEntity func(EntityId, bool) bool) {
	traversal := &ctrl.Traversal
	request := traversal.Request
	ctrl.MotionMode = CharacterMotionBallistic
	ctrl.OnLadder = false
	ctrl.LadderEntity = 0

	if traversal.Phase == CharacterTraversalPhaseSettle {
		settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
		return
	}
	if traversal.Phase == CharacterTraversalPhaseAlign {
		delta := request.Start.Sub(*basePos)
		delta[1] = 0
		if delta.Len() > request.Acceptance {
			next, blocked := moveGroundedTraversalHorizontal(cmd, voxRt, *basePos, request.Start, request.Speed*dt, ctrl, acceptEntity)
			*basePos = next
			resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
			if updateGroundedTraversalBlocked(traversal, blocked, dt) {
				finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "entry_blocked")
			}
			return
		}
		traversal.Phase = CharacterTraversalPhaseTraverse
		traversal.PhaseElapsed = 0
		if !CharacterHasStandingClearance(voxRt, request.End, groundedPlayerCharacterCollisionConfig(cmd, ctrl), acceptEntity) {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "landing_blocked")
			return
		}
		if request.Kind == CharacterTraversalJump {
			if !groundedCharacterJumpReachable(request, defaulted(ctrl.Gravity, 18)) {
				finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "jump_unreachable")
				return
			}
			ctrl.Grounded = false
			groundedPlayerClearGroundSupport(ctrl)
			ctrl.NeedsGroundSnap = false
			ctrl.VerticalVelocity = request.LaunchSpeed
			traversal.Committed = true
			traversal.WasAirborne = true
		}
	}

	next, blocked := moveGroundedTraversalHorizontal(cmd, voxRt, *basePos, request.End, request.Speed*dt, ctrl, acceptEntity)
	*basePos = next
	resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	if !ctrl.Grounded {
		traversal.Committed = true
		traversal.WasAirborne = true
	}
	if blocked && !traversal.Committed && updateGroundedTraversalBlocked(traversal, true, dt) {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "entry_blocked")
		return
	}
	if !blocked {
		updateGroundedTraversalBlocked(traversal, false, dt)
	}
	if !traversal.WasAirborne || !ctrl.Grounded {
		return
	}
	horizontal := request.End.Sub(*basePos)
	horizontal[1] = 0
	verticalTolerance := maxf(defaulted(ctrl.StepHeight, 0.6)+defaulted(ctrl.GroundProbe, 0.15), request.Acceptance)
	if traversal.PendingReason != "" {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, traversal.PendingReason)
	} else if horizontal.Len() <= request.Acceptance && float32(math.Abs(float64(request.End.Y()-basePos.Y()))) <= verticalTolerance {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
	} else {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "missed_landing")
	}
}

func advanceGroundedLadderTraversal(
	cmd *Commands,
	voxRt *VoxelRtState,
	basePos *mgl32.Vec3,
	ctrl *GroundedCharacterMotorComponent,
	horizontalMove mgl32.Vec3,
	ladderMovement, dt float32,
	acceptEntity func(EntityId, bool) bool,
	availableLadder EntityId,
	onLadder, forceLadder bool,
) {
	traversal := &ctrl.Traversal
	request := traversal.Request
	ctrl.MotionMode = CharacterMotionLadder
	ctrl.OnLadder = true
	ctrl.LadderEntity = request.LadderEntity
	ctrl.LadderClimbSpeed = request.Speed
	if traversal.Phase == CharacterTraversalPhaseSettle {
		settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
		return
	}
	if request.Manual {
		sameLadder := request.LadderEntity == 0 || request.LadderEntity == availableLadder
		if ctrl.JumpQueued {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
			ctrl.Grounded = false
			groundedPlayerClearGroundSupport(ctrl)
			ctrl.NeedsGroundSnap = false
			ctrl.VerticalVelocity = defaulted(ctrl.JumpSpeed, 5.5) * 0.5
			*basePos, _ = tryGroundedVerticalMove(cmd, voxRt, *basePos, ctrl.VerticalVelocity*dt, ctrl, acceptEntity)
			ctrl.JumpQueued = false
			return
		}
		if (!onLadder || !sameLadder) && !forceLadder {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
			ctrl.Grounded = false
			resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
			return
		}
		*basePos = tryGroundedHorizontalMove(cmd, voxRt, *basePos, horizontalMove, ctrl, acceptEntity)
		*basePos, _ = tryGroundedVerticalMove(cmd, voxRt, *basePos, ladderMovement*request.Speed*dt, ctrl, acceptEntity)
		ctrl.VerticalVelocity = 0
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
		ctrl.NeedsGroundSnap = false
		ctrl.JumpQueued = false
		return
	}

	remaining := request.Speed * dt
	for remaining > 1e-5 && traversal.Running() {
		switch traversal.Phase {
		case CharacterTraversalPhaseAlign:
			if request.Start.Y() > request.End.Y() && basePos.Y() < request.Start.Y()+defaulted(ctrl.StepHeight, 0.6)-0.01 {
				next, used, reached, blocked := moveGroundedTraversalVertical(cmd, voxRt, *basePos, request.Start.Y()+defaulted(ctrl.StepHeight, 0.6), remaining, ctrl, acceptEntity)
				*basePos, remaining = next, maxf(0, remaining-used)
				if blocked && !reached {
					failOrSettleGroundedCharacterTraversal(ctrl, "mount_blocked")
					return
				}
				if !reached {
					return
				}
			}
			target := request.Entry
			target[1] = basePos.Y()
			next, used, reached, blocked := moveGroundedTraversalHorizontalDistance(cmd, voxRt, *basePos, target, remaining, ctrl, acceptEntity)
			*basePos, remaining = next, maxf(0, remaining-used)
			if blocked {
				failOrSettleGroundedCharacterTraversal(ctrl, "mount_blocked")
				return
			}
			if !reached {
				return
			}
			traversal.Phase = CharacterTraversalPhaseClimb
			traversal.Committed = true
			ctrl.Grounded = false
			groundedPlayerClearGroundSupport(ctrl)
			continue
		case CharacterTraversalPhaseClimb:
			targetY := request.End.Y()
			if request.End.Y() > request.Start.Y() {
				targetY += defaulted(ctrl.StepHeight, 0.6)
			}
			next, used, reached, blocked := moveGroundedTraversalVertical(cmd, voxRt, *basePos, targetY, remaining, ctrl, acceptEntity)
			*basePos, remaining = next, maxf(0, remaining-used)
			if blocked && !reached {
				failOrSettleGroundedCharacterTraversal(ctrl, "climb_blocked")
				return
			}
			if !reached {
				return
			}
			traversal.Phase = CharacterTraversalPhaseDismount
		case CharacterTraversalPhaseDismount:
			next, used, reached, blocked := moveGroundedTraversalHorizontalDistance(cmd, voxRt, *basePos, request.End, remaining, ctrl, acceptEntity)
			*basePos, remaining = next, maxf(0, remaining-used)
			if blocked {
				failOrSettleGroundedCharacterTraversal(ctrl, "dismount_blocked")
				return
			}
			if !reached {
				return
			}
			next, _, reached, blocked = moveGroundedTraversalVertical(cmd, voxRt, *basePos, request.End.Y(), remaining, ctrl, acceptEntity)
			*basePos = next
			if blocked && !reached {
				failOrSettleGroundedCharacterTraversal(ctrl, "dismount_blocked")
				return
			}
			if !reached {
				return
			}
			*basePos = request.End
			traversal.Phase = CharacterTraversalPhaseSettle
			ctrl.Grounded = false
			ctrl.NeedsGroundSnap = true
			settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
			return
		}
	}
	ctrl.VerticalVelocity = 0
	ctrl.NeedsGroundSnap = false
	ctrl.JumpQueued = false
}

func advanceGroundedKinematicTraversal(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, dt float32, acceptEntity func(EntityId, bool) bool) {
	traversal := &ctrl.Traversal
	request := traversal.Request
	ctrl.MotionMode = CharacterMotionKinematic
	ctrl.OnLadder = false
	if traversal.Phase == CharacterTraversalPhaseSettle {
		settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
		return
	}
	if traversal.Phase == CharacterTraversalPhaseAlign {
		next, blocked := moveGroundedTraversalHorizontal(cmd, voxRt, *basePos, request.Start, request.Speed*dt, ctrl, acceptEntity)
		*basePos = next
		delta := request.Start.Sub(*basePos)
		delta[1] = 0
		if blocked {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "entry_blocked")
			return
		}
		if delta.Len() > request.Acceptance {
			return
		}
		traversal.Phase = CharacterTraversalPhaseLift
		traversal.PhaseElapsed = 0
	}
	if request.Duration <= 0 {
		request.Duration = 0.75
	}
	apex := request.Apex
	if apex == (mgl32.Vec3{}) {
		apex = request.Start
		apex[1] = maxf(request.Start.Y(), request.End.Y()) + maxf(defaulted(ctrl.StepHeight, 0.6), 0.5)
	}
	phaseDuration := request.Duration * 0.5
	start, target := request.Start, apex
	switch traversal.Phase {
	case CharacterTraversalPhaseLift:
		phaseDuration = request.Duration * 0.25
	case CharacterTraversalPhaseCross:
		start = apex
		target = mgl32.Vec3{request.End.X(), apex.Y(), request.End.Z()}
	case CharacterTraversalPhaseLand:
		phaseDuration = request.Duration * 0.25
		start = mgl32.Vec3{request.End.X(), apex.Y(), request.End.Z()}
		target = request.End
	}
	traversal.PhaseElapsed = minf(traversal.PhaseElapsed+dt, phaseDuration)
	progress := traversal.PhaseElapsed / maxf(phaseDuration, 0.001)
	progress = progress * progress * (3 - 2*progress)
	desired := start.Mul(1 - progress).Add(target.Mul(progress))
	next, blocked := moveGroundedTraversalSegment(cmd, voxRt, *basePos, desired, ctrl, acceptEntity)
	*basePos = next
	traversal.Committed = traversal.Committed || traversal.Phase != CharacterTraversalPhaseLift || progress > 0
	ctrl.Grounded = false
	groundedPlayerClearGroundSupport(ctrl)
	ctrl.VerticalVelocity = 0
	if blocked || next.Sub(desired).LenSqr() > 1e-5 {
		failOrSettleGroundedCharacterTraversal(ctrl, "traversal_blocked")
		return
	}
	if traversal.PhaseElapsed < phaseDuration {
		return
	}
	*basePos = target
	traversal.PhaseElapsed = 0
	switch traversal.Phase {
	case CharacterTraversalPhaseLift:
		traversal.Phase = CharacterTraversalPhaseCross
	case CharacterTraversalPhaseCross:
		traversal.Phase = CharacterTraversalPhaseLand
	default:
		traversal.Phase = CharacterTraversalPhaseSettle
		ctrl.NeedsGroundSnap = true
		settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	}
}

func settleGroundedCharacterTraversal(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, dt float32, acceptEntity func(EntityId, bool) bool) {
	ctrl.MotionMode = CharacterMotionBallistic
	ctrl.OnLadder = false
	resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	if !ctrl.Grounded {
		return
	}
	if ctrl.Traversal.PendingReason != "" {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, ctrl.Traversal.PendingReason)
	} else {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
	}
}

func failOrSettleGroundedCharacterTraversal(ctrl *GroundedCharacterMotorComponent, reason string) {
	if ctrl == nil {
		return
	}
	if ctrl.Traversal.Committed || !ctrl.Grounded {
		ctrl.Traversal.PendingReason = reason
		ctrl.Traversal.Phase = CharacterTraversalPhaseSettle
		ctrl.MotionMode = CharacterMotionBallistic
		ctrl.OnLadder = false
		return
	}
	finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, reason)
}

func finishGroundedCharacterTraversal(ctrl *GroundedCharacterMotorComponent, status CharacterTraversalStatus, reason string) {
	if ctrl == nil {
		return
	}
	ctrl.Traversal.Status = status
	ctrl.Traversal.Reason = reason
	ctrl.Traversal.PendingReason = ""
	ctrl.MotionMode = CharacterMotionNormal
	ctrl.OnLadder = false
	ctrl.LadderEntity = 0
	ctrl.LadderClimbSpeed = 0
	ctrl.JumpQueued = false
}

func updateGroundedTraversalBlocked(traversal *CharacterTraversalComponent, blocked bool, dt float32) bool {
	if traversal == nil {
		return false
	}
	traversal.Blocked = blocked
	if blocked {
		traversal.BlockedElapsed += dt
	} else {
		traversal.BlockedElapsed = 0
	}
	return traversal.BlockedElapsed >= 0.25
}

func moveGroundedTraversalHorizontal(cmd *Commands, voxRt *VoxelRtState, current, target mgl32.Vec3, maxDistance float32, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, bool) {
	next, _, _, blocked := moveGroundedTraversalHorizontalDistance(cmd, voxRt, current, target, maxDistance, ctrl, acceptEntity)
	return next, blocked
}

func moveGroundedTraversalHorizontalDistance(cmd *Commands, voxRt *VoxelRtState, current, target mgl32.Vec3, maxDistance float32, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, float32, bool, bool) {
	delta := target.Sub(current)
	delta[1] = 0
	distance := delta.Len()
	acceptance := maxf(ctrl.Traversal.Request.Acceptance, 0.01)
	if distance <= acceptance {
		return current, 0, true, false
	}
	move := delta.Normalize().Mul(minf(distance, maxf(maxDistance, 0)))
	result := CharacterKinematicMove(voxRt, current, move, CharacterKinematicMoveOptions{
		CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
		AcceptEntity:    acceptEntity, DisableSlide: true,
	})
	used := result.Position.Sub(current)
	used[1] = 0
	reached := !result.Blocked && actionHorizontalDistance(result.Position, target) <= acceptance
	return result.Position, used.Len(), reached, result.Blocked
}

func moveGroundedTraversalVertical(cmd *Commands, voxRt *VoxelRtState, current mgl32.Vec3, targetY, maxDistance float32, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, float32, bool, bool) {
	delta := targetY - current.Y()
	if float32(math.Abs(float64(delta))) <= 0.04 {
		current[1] = targetY
		return current, 0, true, false
	}
	move := minf(float32(math.Abs(float64(delta))), maxf(maxDistance, 0))
	if delta < 0 {
		move = -move
	}
	next, blocked := tryGroundedVerticalMove(cmd, voxRt, current, move, ctrl, acceptEntity)
	used := float32(math.Abs(float64(next.Y() - current.Y())))
	reached := float32(math.Abs(float64(targetY-next.Y()))) <= 0.04
	if reached && (!blocked || delta < 0) {
		next[1] = targetY
		return next, used, true, false
	}
	return next, used, false, blocked
}

func moveGroundedTraversalSegment(cmd *Commands, voxRt *VoxelRtState, current, desired mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, bool) {
	delta := desired.Sub(current)
	if delta.Y() > 1e-5 {
		next, _, _, blocked := moveGroundedTraversalVertical(cmd, voxRt, current, desired.Y(), delta.Y(), ctrl, acceptEntity)
		current = next
		if blocked {
			return current, true
		}
	}
	horizontal := desired.Sub(current)
	horizontal[1] = 0
	if horizontal.LenSqr() > 1e-8 {
		result := CharacterKinematicMove(voxRt, current, horizontal, CharacterKinematicMoveOptions{
			CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
			AcceptEntity:    acceptEntity, DisableDepenetration: true, DisableSlide: true,
		})
		current = result.Position
		if result.Blocked {
			return current, true
		}
	}
	deltaY := desired.Y() - current.Y()
	if deltaY < -1e-5 {
		next, _, _, blocked := moveGroundedTraversalVertical(cmd, voxRt, current, desired.Y(), -deltaY, ctrl, acceptEntity)
		return next, blocked
	}
	return current, false
}

func actionHorizontalDistance(a, b mgl32.Vec3) float32 {
	delta := b.Sub(a)
	delta[1] = 0
	return delta.Len()
}

func groundedCharacterJumpReachable(request CharacterTraversalRequest, gravity float32) bool {
	if gravity <= 0 || request.LaunchSpeed <= 0 || request.Speed <= 0 {
		return false
	}
	dy := request.End.Y() - request.Start.Y()
	discriminant := request.LaunchSpeed*request.LaunchSpeed - 2*gravity*dy
	if discriminant < 0 {
		return false
	}
	flightTime := (request.LaunchSpeed + float32(math.Sqrt(float64(discriminant)))) / gravity
	return actionHorizontalDistance(request.Start, request.End) <= request.Speed*flightTime+request.Acceptance
}

func tryGroundedVerticalMove(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, deltaY float32, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, bool) {
	if ctrl == nil {
		return basePos.Add(mgl32.Vec3{0, deltaY, 0}), false
	}
	return CharacterVerticalMove(voxRt, basePos, deltaY, groundedPlayerCharacterCollisionConfig(cmd, ctrl), acceptEntity)
}

func aabbOverlap(aMin, aMax, bMin, bMax mgl32.Vec3) bool {
	return aMin.X() <= bMax.X() && aMax.X() >= bMin.X() &&
		aMin.Y() <= bMax.Y() && aMax.Y() >= bMin.Y() &&
		aMin.Z() <= bMax.Z() && aMax.Z() >= bMin.Z()
}

func applyGroundedLook(cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) {
	if cam == nil || ctrl == nil {
		return
	}
	cam.Yaw += ctrl.LookInput[0] * defaulted(ctrl.Sensitivity, 0.1)
	cam.Pitch -= ctrl.LookInput[1] * defaulted(ctrl.Sensitivity, 0.1)
	if cam.Pitch > 89 {
		cam.Pitch = 89
	}
	if cam.Pitch < -89 {
		cam.Pitch = -89
	}
}

func tryGroundedHorizontalMove(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, move mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool) mgl32.Vec3 {
	if move.Len() <= 0 {
		return basePos
	}
	target := basePos.Add(mgl32.Vec3{move.X(), 0, move.Z()})
	query := MovingBrushCollisionQuery(cmd)
	if !groundedMovementBlockedWithQuery(voxRt, basePos, mgl32.Vec3{move.X(), 0, move.Z()}, ctrl, acceptEntity, query) {
		return target
	}
	xOnly := mgl32.Vec3{move.X(), 0, 0}
	if math.Abs(float64(xOnly.X())) > 1e-5 && !groundedMovementBlockedWithQuery(voxRt, basePos, xOnly, ctrl, acceptEntity, query) {
		basePos = basePos.Add(xOnly)
	}
	zOnly := mgl32.Vec3{0, 0, move.Z()}
	if math.Abs(float64(zOnly.Z())) > 1e-5 && !groundedMovementBlockedWithQuery(voxRt, basePos, zOnly, ctrl, acceptEntity, query) {
		basePos = basePos.Add(zOnly)
	}
	return basePos
}

func groundedMovementBlocked(voxRt *VoxelRtState, basePos, move mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool) bool {
	return groundedMovementBlockedWithQuery(voxRt, basePos, move, ctrl, acceptEntity, nil)
}

func groundedMovementBlockedWithQuery(voxRt *VoxelRtState, basePos, move mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, acceptEntity func(EntityId, bool) bool, query CharacterCollisionQuery) bool {
	if !characterCollisionAvailable(voxRt, query) || move.Len() <= 0 {
		return false
	}
	dir := move.Normalize()
	dist := move.Len() + defaulted(ctrl.Radius, 0.35)
	height := defaulted(ctrl.Height, 1.8)
	radius := defaulted(ctrl.Radius, 0.35)
	step := defaulted(ctrl.StepHeight, 0.6)
	samples := []float32{step * 0.5, height * 0.5, maxf(height-0.2, step)}
	perp := mgl32.Vec3{-dir.Z(), 0, dir.X()}
	if perp.Len() > 1e-5 {
		perp = perp.Normalize()
	}
	offsets := []mgl32.Vec3{{0, 0, 0}}
	if perp.Len() > 0 {
		side := perp.Mul(radius)
		offsets = append(offsets, side, side.Mul(-1))
	}
	for _, sampleY := range samples {
		for _, offset := range offsets {
			origin := basePos.Add(offset).Add(mgl32.Vec3{0, sampleY, 0})
			hit := characterRaycastFiltered(voxRt, origin, dir, dist, acceptEntity, query)
			if hit.Hit && hit.T <= dist {
				return true
			}
		}
	}
	return false
}

func resolveGroundedVertical(cmd *Commands, voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, dt float32, acceptEntity func(EntityId, bool) bool) {
	if basePos == nil || ctrl == nil {
		return
	}
	stepHeight := defaulted(ctrl.StepHeight, 0.6)
	groundProbe := defaulted(ctrl.GroundProbe, 0.15)
	maxSnapUp := groundedPlayerGroundSnapUpTolerance(ctrl)
	dynamicQuery := MovingBrushCollisionQuery(cmd)

	if ctrl.Grounded && ctrl.JumpQueued {
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = defaulted(ctrl.JumpSpeed, 5.5)
	}
	ctrl.JumpQueued = false

	if characterCollisionAvailable(voxRt, dynamicQuery) && ctrl.Grounded {
		if hit, ok := groundedPlayerGroundHitWithin(cmd, voxRt, *basePos, ctrl, maxSnapUp, stepHeight+groundProbe, acceptEntity); ok && CharacterAcceptsGroundY(basePos.Y(), hit.Y, maxSnapUp, stepHeight+groundProbe) {
			basePos[1] = hit.Y
			ctrl.VerticalVelocity = 0
			groundedPlayerSetGroundSupport(ctrl, hit)
			return
		}
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
	}

	if !ctrl.Grounded {
		ctrl.VerticalVelocity -= defaulted(ctrl.Gravity, 18.0) * dt
		deltaY := ctrl.VerticalVelocity * dt
		if next, blocked := tryGroundedVerticalMove(cmd, voxRt, *basePos, deltaY, ctrl, acceptEntity); blocked {
			*basePos = next
			ctrl.VerticalVelocity = 0
			if deltaY < 0 {
				ctrl.Grounded = true
				ctrl.NeedsGroundSnap = false
				if hit, ok := groundedPlayerGroundHitWithin(cmd, voxRt, *basePos, ctrl, maxSnapUp, stepHeight+groundProbe, acceptEntity); ok {
					groundedPlayerSetGroundSupport(ctrl, hit)
				}
				return
			}
		} else {
			*basePos = next
		}
	}

	if !characterCollisionAvailable(voxRt, dynamicQuery) {
		return
	}
	fallDistance := maxf(-ctrl.VerticalVelocity*dt, 0)
	if ctrl.NeedsGroundSnap {
		if hit, ok := groundedPlayerGroundHitWithin(cmd, voxRt, *basePos, ctrl, maxSnapUp, stepHeight+groundProbe+fallDistance, acceptEntity); ok {
			basePos[1] = hit.Y
			ctrl.VerticalVelocity = 0
			ctrl.Grounded = true
			groundedPlayerSetGroundSupport(ctrl, hit)
			ctrl.NeedsGroundSnap = false
		}
		return
	}
	if hit, ok := groundedPlayerGroundHitWithin(cmd, voxRt, *basePos, ctrl, maxSnapUp+fallDistance, stepHeight+groundProbe+fallDistance, acceptEntity); ok {
		if ctrl.VerticalVelocity <= 0 && CharacterAcceptsGroundY(basePos.Y(), hit.Y, maxSnapUp+fallDistance, stepHeight+groundProbe+fallDistance) {
			basePos[1] = hit.Y
			ctrl.VerticalVelocity = 0
			ctrl.Grounded = true
			groundedPlayerSetGroundSupport(ctrl, hit)
			return
		}
	}
	ctrl.Grounded = false
	groundedPlayerClearGroundSupport(ctrl)
}

func groundedPlayerSetGroundSupport(ctrl *GroundedCharacterMotorComponent, hit CharacterGroundHit) {
	if ctrl == nil {
		return
	}
	ctrl.GroundPoint, ctrl.HasGroundPoint = hit.Point, true
	ctrl.GroundContacts, ctrl.GroundContactCount = hit.Contacts, hit.ContactCount
}

func groundedPlayerClearGroundSupport(ctrl *GroundedCharacterMotorComponent) {
	if ctrl == nil {
		return
	}
	ctrl.HasGroundPoint = false
	ctrl.GroundContactCount = 0
}

func groundedPlayerCharacterCollisionConfig(cmd *Commands, ctrl *GroundedPlayerControllerComponent) CharacterCollisionConfig {
	if ctrl == nil {
		return CharacterCollisionConfig{}
	}
	return CharacterCollisionConfig{
		Radius:                defaulted(ctrl.Radius, 0.35),
		Height:                defaulted(ctrl.Height, 1.8),
		StepHeight:            defaulted(ctrl.StepHeight, 0.6),
		DynamicCollisionQuery: MovingBrushCollisionQuery(cmd),
	}
}

func groundedPlayerGroundHitWithin(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, maxSnapUp, maxSnapDown float32, acceptEntity func(EntityId, bool) bool) (CharacterGroundHit, bool) {
	if ctrl == nil {
		return CharacterGroundHit{}, false
	}
	ground := groundedPlayerGroundProbeConfig(ctrl)
	ground.DynamicCollisionQuery = MovingBrushCollisionQuery(cmd)
	return CharacterGroundHitAtWithin(voxRt, basePos, ground, maxSnapUp, maxSnapDown, acceptEntity)
}

func groundedPlayerCollisionRaycastFilter(cmd *Commands, ctrl *GroundedPlayerControllerComponent) func(EntityId, bool) bool {
	if cmd == nil || ctrl == nil {
		return nil
	}
	ignoredRoot := ctrl.CollisionIgnoredEntity
	return func(hitEntity EntityId, knownEntity bool) bool {
		return !knownEntity || (cmd.GetComponent(hitEntity, reflect.TypeOf(CharacterControllerIgnoredComponent{})) == nil &&
			(ignoredRoot == 0 || !groundedPlayerEntityDescendsFrom(cmd, hitEntity, ignoredRoot)))
	}
}

// GroundedPlayerCollisionRaycastFilter returns the controller's shared
// collision filter for gameplay actions that use Character* movement helpers.
func GroundedPlayerCollisionRaycastFilter(cmd *Commands, ctrl *GroundedPlayerControllerComponent) func(EntityId, bool) bool {
	return groundedPlayerCollisionRaycastFilter(cmd, ctrl)
}

func groundedPlayerEntityDescendsFrom(cmd *Commands, entity, ancestor EntityId) bool {
	for i := 0; cmd != nil && entity != 0 && i < 64; i++ {
		if entity == ancestor {
			return true
		}
		parent, _ := cmd.GetComponent(entity, reflect.TypeOf(Parent{})).(*Parent)
		if parent == nil {
			return false
		}
		entity = parent.Entity
	}
	return false
}

func groundedPlayerGroundSnapUpTolerance(ctrl *GroundedPlayerControllerComponent) float32 {
	return CharacterGroundSnapUpTolerance(groundedPlayerGroundProbeConfig(ctrl))
}

func groundedPlayerGroundProbeConfig(ctrl *GroundedPlayerControllerComponent) CharacterGroundProbeConfig {
	if ctrl == nil {
		return CharacterGroundProbeConfig{
			Radius:             0.35,
			StepHeight:         0.6,
			GroundProbe:        0.15,
			MinWalkableNormalY: 0.65,
		}
	}
	return CharacterGroundProbeConfig{
		Radius:             defaulted(ctrl.Radius, 0.35),
		StepHeight:         defaulted(ctrl.StepHeight, 0.6),
		GroundProbe:        defaulted(ctrl.GroundProbe, 0.15),
		MinWalkableNormalY: 0.65,
	}
}

func forwardFromYawPitch(yawDeg, pitchDeg float32) mgl32.Vec3 {
	yawRad := mgl32.DegToRad(yawDeg)
	pitchRad := mgl32.DegToRad(pitchDeg)
	return mgl32.Vec3{
		float32(math.Sin(float64(yawRad)) * math.Cos(float64(pitchRad))),
		float32(math.Sin(float64(pitchRad))),
		float32(-math.Cos(float64(yawRad)) * math.Cos(float64(pitchRad))),
	}.Normalize()
}

func defaulted(v, fallback float32) float32 {
	if v == 0 {
		return fallback
	}
	return v
}

func transformForEntity(cmd *Commands, eid EntityId) (*TransformComponent, bool) {
	if cmd == nil {
		return nil, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(*TransformComponent); ok {
			return tr, true
		}
		if tr, ok := comp.(TransformComponent); ok {
			tmp := tr
			return &tmp, true
		}
	}
	return nil, false
}

func localTransformForEntity(cmd *Commands, eid EntityId) (*LocalTransformComponent, bool) {
	if cmd == nil || eid == 0 {
		return nil, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(*LocalTransformComponent); ok {
			return tr, true
		}
	}
	return nil, false
}
