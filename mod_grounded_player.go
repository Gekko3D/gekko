package gekko

import (
	"math"
	"reflect"
	"sort"

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

type groundedCharacterEnvironmentState struct {
	active           bool
	voxelRuntime     *VoxelRtState
	waters           []waterInteractionBody
	ladders          []groundedCharacterLadder
	movingBrushes    []movingBrushCollisionBounds
	movingBrushQuery CharacterCollisionQuery
}

type groundedCharacterLadder struct {
	Entity EntityId
	Volume LadderVolumeComponent
}

func (state *groundedCharacterEnvironmentState) raycastMovingBrushes(origin, dir mgl32.Vec3, maxDistance float32, acceptEntity func(EntityId, bool) bool) RaycastHit {
	return raycastMovingBrushCollisionBounds(state.movingBrushes, origin, dir, maxDistance, acceptEntity)
}

func activeGroundedCharacterEnvironment(cmd *Commands) *groundedCharacterEnvironmentState {
	if cmd == nil || cmd.app == nil {
		return nil
	}
	resource, _ := cmd.app.resources[reflect.TypeOf(groundedCharacterEnvironmentState{})].(*groundedCharacterEnvironmentState)
	if resource == nil || !resource.active {
		return nil
	}
	return resource
}

// GroundedCharacterMotorModule installs camera-free character movement.
// Games write GroundedCharacterIntentComponent from human or bot intent.
type GroundedCharacterMotorModule struct {
	Config GroundedCharacterMotorConfig
}

type GroundedPlayerControllerModule struct {
	Config GroundedPlayerControllerConfig
}

type GroundedCharacterIntentComponent struct {
	MoveDirection            mgl32.Vec3
	MaxDistance              float32
	AimDirection             mgl32.Vec3
	LadderMovement           float32
	LadderUse                bool
	LadderEntity             EntityId
	LadderStartDelay         float32
	LadderTopExitDuration    float32
	LadderBottomExitDuration float32
	LadderMountMotion        []CharacterTraversalMotionKey
	LadderTopExitMotion      []CharacterTraversalMotionKey
	LadderBottomExitMotion   []CharacterTraversalMotionKey
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
	ExternalVelocity   mgl32.Vec3 // Decaying horizontal velocity from gameplay impulses.
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
		if _, ok := app.resources[reflect.TypeOf(groundedCharacterEnvironmentState{})]; !ok {
			environment := &groundedCharacterEnvironmentState{}
			environment.movingBrushQuery = environment.raycastMovingBrushes
			cmd.AddResources(environment)
		}
	}
	app.UseSystem(System(groundedCharacterMotorSystem).ProfileCategory("motor_collision").InStage(Update).RunAlways())
	app.UseSystem(System(triggerVolumeTouchSystem).ProfileCategory("motor_collision").InStage(Update).RunAlways())
	app.UseSystem(System(targetEventSystem).ProfileCategory("motor_collision").InStage(Update).RunAlways())
	app.UseSystem(System(movingBrushMotionSystem).ProfileCategory("motor_collision").InStage(Update).RunAlways())
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

func groundedCharacterMotorSystem(cmd *Commands, time *Time, voxRt *VoxelRtState, environment *groundedCharacterEnvironmentState) {
	if time == nil || time.Dt <= 0 || environment == nil {
		return
	}
	environment.voxelRuntime = voxRt
	// movingBrushMotionSystem is registered after this system, so these bounds
	// remain current until every motor below has consumed them.
	environment.waters = collectWaterInteractionBodiesInto(cmd, environment.waters)
	environment.ladders = collectGroundedCharacterLadders(cmd, environment.ladders)
	environment.movingBrushes = collectMovingBrushCollisionBounds(cmd, environment.movingBrushes)
	environment.active = true
	defer func() { environment.active = false }()
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
		horizontalMove = horizontalMove.Add(mgl32.Vec3{ctrl.ExternalVelocity.X(), 0, ctrl.ExternalVelocity.Z()}.Mul(dt))
		ctrl.ExternalVelocity[0] *= maxf(0, 1-6*dt)
		ctrl.ExternalVelocity[2] *= maxf(0, 1-6*dt)
		ladderHorizontalMove := right.Mul(move.Dot(right) * speed * 0.5 * dt)
		if intent != nil && intent.MaxDistance > 0 && ladderHorizontalMove.Len() > intent.MaxDistance {
			ladderHorizontalMove = ladderHorizontalMove.Normalize().Mul(intent.MaxDistance)
		}
		ladderEntity, ladder, onLadder := findGroundedPlayerLadderVolume(cmd, basePos, ctrl)
		forcedLadder := intent != nil && intent.ForceLadder
		startedLadder := false
		ctrl.LadderAvailable, ctrl.AvailableLadder = onLadder, ladderEntity
		selectedLadderEntity, selectedLadder := ladderEntity, ladder
		if forcedLadder && intent != nil && intent.LadderEntity != 0 {
			if target, _ := cmd.GetComponent(intent.LadderEntity, reflect.TypeOf(LadderVolumeComponent{})).(*LadderVolumeComponent); target != nil {
				selectedLadderEntity, selectedLadder = intent.LadderEntity, *target
			}
		}
		if !ctrl.Traversal.Running() && !ctrl.Swimming && (forcedLadder || intent == nil && onLadder && ladderMovement != 0) {
			speed := DefaultLadderClimbSpeed
			if selectedLadderEntity != 0 {
				speed = selectedLadder.NormalizedClimbSpeed()
			}
			ladderTop := selectedLadder.BoundsCenter.Y() + selectedLadder.BoundsHalfExtents.Y()
			mountFromTop := basePos.Y() >= ladderTop-defaulted(ctrl.StepHeight, 0.6)
			var mountMotion []CharacterTraversalMotionKey
			if intent != nil {
				mountMotion = intent.LadderMountMotion
			}
			entry := groundedLadderMountEntry(basePos, selectedLadder, ctrl.Radius, mountFromTop)
			if mountFromTop {
				entry[1] = minf(entry.Y(), ladderTop-characterTraversalMotionVerticalDistance(mountMotion))
			}
			startedLadder = CharacterBeginTraversal(ctrl, CharacterTraversalRequest{
				Kind: CharacterTraversalLadder, Start: basePos, Entry: entry,
				Speed: speed, StartDelay: func() float32 {
					if intent != nil {
						return intent.LadderStartDelay
					}
					return 0
				}(), LadderEntity: selectedLadderEntity, Manual: true,
				TopExitDuration: func() float32 {
					if intent != nil {
						return intent.LadderTopExitDuration
					}
					return 0
				}(),
				BottomExitDuration: func() float32 {
					if intent != nil {
						return intent.LadderBottomExitDuration
					}
					return 0
				}(),
				MountMotion: mountMotion,
				TopExitMotion: func() []CharacterTraversalMotionKey {
					if intent != nil {
						return intent.LadderTopExitMotion
					}
					return nil
				}(),
				BottomExitMotion: func() []CharacterTraversalMotionKey {
					if intent != nil {
						return intent.LadderBottomExitMotion
					}
					return nil
				}(),
			})
		}
		if ctrl.Traversal.Running() {
			advanceGroundedCharacterTraversal(
				cmd, voxRt, &basePos, ctrl, ladderHorizontalMove, ladderMovement, dt,
				collisionFilter, ladder, forcedLadder,
				intent != nil && intent.LadderUse && !startedLadder, flatForward,
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
			if ctrl.Grounded {
				basePos = CharacterGroundedMove(voxRt, basePos, horizontalMove, CharacterGroundedMoveOptions{
					CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
					GroundConfig:    groundedPlayerGroundProbeConfig(ctrl),
					AcceptEntity:    collisionFilter,
				}).Position
			} else {
				basePos = tryGroundedHorizontalMove(cmd, voxRt, basePos, horizontalMove, ctrl, collisionFilter)
			}
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
	_, used := GroundedCharacterUseWithHit(cmd, activator, origin, direction, maxDistance)
	return used
}

// GroundedCharacterUseHit describes the authored interaction activated by a
// character-use ray.
type GroundedCharacterUseHit struct {
	Kind string
}

// GroundedCharacterUseWithHit activates the nearest authored interaction and
// reports its kind for presentation systems.
func GroundedCharacterUseWithHit(cmd *Commands, activator EntityId, origin, direction mgl32.Vec3, maxDistance float32) (GroundedCharacterUseHit, bool) {
	if cmd == nil || direction.LenSqr() <= 1e-8 || maxDistance <= 0 {
		return GroundedCharacterUseHit{}, false
	}
	if hit, ok := findUseTriggerHit(cmd, origin, direction, maxDistance); ok {
		ActivateUseTrigger(cmd, hit.Trigger, activator)
		return GroundedCharacterUseHit{Kind: hit.Trigger.Kind}, true
	}
	if hit, ok := findMovingBrushUseHit(cmd, origin, direction, maxDistance); ok {
		hit.Brush.ActivationCount++
		if !hit.Brush.DoesNotMove() {
			hit.Brush.Open = !hit.Brush.Open
		}
		if hit.Brush.Target != "" {
			ActivateTarget(cmd, hit.Brush.Target, activator)
		}
		return GroundedCharacterUseHit{Kind: hit.Brush.Kind}, true
	}
	return GroundedCharacterUseHit{}, false
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

func collectGroundedCharacterLadders(cmd *Commands, ladders []groundedCharacterLadder) []groundedCharacterLadder {
	ladders = ladders[:0]
	if cmd == nil {
		return ladders
	}
	MakeQuery1[LadderVolumeComponent](cmd).Map(func(eid EntityId, ladder *LadderVolumeComponent) bool {
		if ladder != nil {
			ladders = append(ladders, groundedCharacterLadder{Entity: eid, Volume: *ladder})
		}
		return true
	})
	sort.Slice(ladders, func(i, j int) bool { return ladders[i].Entity < ladders[j].Entity })
	return ladders
}

func findGroundedPlayerLadderVolume(cmd *Commands, basePos mgl32.Vec3, ctrl *GroundedPlayerControllerComponent) (EntityId, LadderVolumeComponent, bool) {
	if cmd == nil || ctrl == nil {
		return 0, LadderVolumeComponent{}, false
	}
	radius := defaulted(ctrl.Radius, 0.35)
	height := defaulted(ctrl.Height, 1.8)
	playerMin := basePos.Add(mgl32.Vec3{-radius, 0, -radius})
	playerMax := basePos.Add(mgl32.Vec3{radius, height, radius})
	var ladders []groundedCharacterLadder
	if environment := activeGroundedCharacterEnvironment(cmd); environment != nil {
		ladders = environment.ladders
	} else {
		ladders = collectGroundedCharacterLadders(cmd, nil)
	}
	for _, ladder := range ladders {
		center := ladder.Volume.BoundsCenter
		extents := ladder.Volume.BoundsHalfExtents.Add(mgl32.Vec3{0.05, 0.05, 0.05})
		ladderMin := center.Sub(extents)
		ladderMax := center.Add(extents)
		if aabbOverlap(playerMin, playerMax, ladderMin, ladderMax) {
			return ladder.Entity, ladder.Volume, true
		}
	}
	return 0, LadderVolumeComponent{}, false
}

func groundedLadderMountEntry(basePos mgl32.Vec3, ladder LadderVolumeComponent, radius float32, mountFromTop bool) mgl32.Vec3 {
	if ladder.BoundsHalfExtents == (mgl32.Vec3{}) {
		return basePos
	}
	entry := ladder.BoundsCenter
	entry[1] = maxf(ladder.BoundsCenter.Y()-ladder.BoundsHalfExtents.Y(), minf(basePos.Y(), ladder.BoundsCenter.Y()+ladder.BoundsHalfExtents.Y()))
	normalAxis := 2
	if ladder.BoundsHalfExtents.X() < ladder.BoundsHalfExtents.Z() {
		normalAxis = 0
	}
	tangentAxis := 0
	if normalAxis == 0 {
		tangentAxis = 2
	}
	entry[tangentAxis] = maxf(ladder.BoundsCenter[tangentAxis]-ladder.BoundsHalfExtents[tangentAxis], minf(basePos[tangentAxis], ladder.BoundsCenter[tangentAxis]+ladder.BoundsHalfExtents[tangentAxis]))
	side := float32(1)
	if basePos[normalAxis] < ladder.BoundsCenter[normalAxis] {
		side = -1
	}
	if mountFromTop {
		side = -side
	}
	// Keep the capsule clear of the ladder's solid backing. The interaction
	// volume has a 5 cm margin, so this still counts as being on the ladder.
	entry[normalAxis] += side * (ladder.BoundsHalfExtents[normalAxis] + defaulted(radius, 0.35) + 0.03)
	return entry
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
	if ctrl.CrouchRequested || ctrl.Traversal.Running() && ctrl.Traversal.Request.Kind == CharacterTraversalCrawl {
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
	var waters []waterInteractionBody
	if environment := activeGroundedCharacterEnvironment(cmd); environment != nil {
		waters = environment.waters
	} else {
		waters = collectWaterInteractionBodies(cmd)
	}
	for _, water := range waters {
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
	ladder LadderVolumeComponent,
	forceLadder bool,
	ladderUse bool,
	ladderExitDirection mgl32.Vec3,
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
		advanceGroundedLadderTraversal(cmd, voxRt, basePos, ctrl, horizontalMove, ladderMovement, dt, acceptEntity, ladder, forceLadder, ladderUse, ladderExitDirection)
	case CharacterTraversalVault, CharacterTraversalMantle, CharacterTraversalCrawl:
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
		collision := groundedPlayerCharacterCollisionConfig(cmd, ctrl)
		if request.LandingSupportEntity != 0 && !characterHasLandingSupport(voxRt, request.End, collision, acceptEntity, request.LandingSupportEntity) {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "landing_support_unavailable")
			return
		}
		footClearance := float32(0.03)
		if request.LandingSupportEntity != 0 {
			footClearance = characterLandingSupportTolerance(collision)
		}
		if !characterHasStandingClearance(voxRt, request.End, collision, acceptEntity, footClearance) {
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

	launchingDrop := request.Kind == CharacterTraversalDrop && !traversal.WasAirborne
	target := request.End
	if launchingDrop {
		target = CharacterDropLaunchTarget(request.Start, request.End, ctrl.Radius)
	}
	next, _, reached, blocked := moveGroundedTraversalHorizontalDistance(cmd, voxRt, *basePos, target, request.Speed*dt, ctrl, acceptEntity)
	*basePos = next
	resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
	if !ctrl.Grounded {
		traversal.Committed = true
		traversal.WasAirborne = true
	}
	if launchingDrop && !traversal.WasAirborne {
		directLandingTolerance := maxf(defaulted(ctrl.GroundProbe, 0.15), 0.05)
		if request.Start.Y()-basePos.Y() > groundedPlayerGroundSnapUpTolerance(ctrl) &&
			float32(math.Abs(float64(request.End.Y()-basePos.Y()))) <= directLandingTolerance &&
			groundedBallisticLandingMatches(ctrl, *basePos) {
			traversal.Committed = true
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
			return
		}
		if updateGroundedTraversalBlocked(traversal, blocked || reached, dt) {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "drop_launch_blocked")
		}
		return
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
	finishGroundedBallisticLanding(ctrl, *basePos)
}

func finishGroundedBallisticLanding(ctrl *GroundedCharacterMotorComponent, basePos mgl32.Vec3) {
	traversal := &ctrl.Traversal
	landingSupportOK := traversal.Request.LandingSupportEntity == 0 || groundedCharacterSupportedBy(ctrl, traversal.Request.LandingSupportEntity)
	if traversal.PendingReason != "" {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, traversal.PendingReason)
	} else if groundedBallisticLandingMatches(ctrl, basePos) {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalSucceeded, "")
	} else if !landingSupportOK {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "wrong_landing_support")
	} else {
		finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "missed_landing")
	}
}

func groundedBallisticLandingMatches(ctrl *GroundedCharacterMotorComponent, basePos mgl32.Vec3) bool {
	if ctrl == nil || !ctrl.Grounded {
		return false
	}
	request := ctrl.Traversal.Request
	horizontal := request.End.Sub(basePos)
	horizontal[1] = 0
	verticalTolerance := maxf(defaulted(ctrl.StepHeight, 0.6)+defaulted(ctrl.GroundProbe, 0.15), request.Acceptance)
	return horizontal.Len() <= request.Acceptance &&
		float32(math.Abs(float64(request.End.Y()-basePos.Y()))) <= verticalTolerance &&
		(request.LandingSupportEntity == 0 || groundedCharacterSupportedBy(ctrl, request.LandingSupportEntity))
}

func groundedCharacterSupportedBy(ctrl *GroundedCharacterMotorComponent, entity EntityId) bool {
	if ctrl == nil || entity == 0 || !ctrl.Grounded {
		return false
	}
	count := min(ctrl.GroundContactCount, len(ctrl.GroundContacts))
	for index := 0; index < count; index++ {
		if ctrl.GroundContacts[index].Entity == entity {
			return true
		}
	}
	return false
}

func advanceGroundedLadderTraversal(
	cmd *Commands,
	voxRt *VoxelRtState,
	basePos *mgl32.Vec3,
	ctrl *GroundedCharacterMotorComponent,
	horizontalMove mgl32.Vec3,
	ladderMovement, dt float32,
	acceptEntity func(EntityId, bool) bool,
	ladder LadderVolumeComponent,
	forceLadder bool,
	ladderUse bool,
	ladderExitDirection mgl32.Vec3,
) {
	traversal := &ctrl.Traversal
	request := traversal.Request
	ctrl.MotionMode = CharacterMotionLadder
	ctrl.OnLadder = traversal.Phase != CharacterTraversalPhaseAlign
	if ctrl.OnLadder {
		ctrl.LadderEntity = request.LadderEntity
		ctrl.LadderClimbSpeed = request.Speed
	} else {
		ctrl.LadderEntity, ctrl.LadderClimbSpeed = 0, 0
	}
	if traversal.Phase == CharacterTraversalPhaseSettle {
		settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
		return
	}
	if request.Manual {
		if request.LadderEntity != 0 {
			if selected, _ := cmd.GetComponent(request.LadderEntity, reflect.TypeOf(LadderVolumeComponent{})).(*LadderVolumeComponent); selected != nil {
				ladder = *selected
			}
		}
		ladderBottom := ladder.BoundsCenter.Y() - ladder.BoundsHalfExtents.Y()
		ladderTop := ladder.BoundsCenter.Y() + ladder.BoundsHalfExtents.Y()
		withinLadderHeight := basePos.Y() >= ladderBottom-0.05 && basePos.Y() <= ladderTop+0.05
		if traversal.Phase == CharacterTraversalPhaseClimb && request.Start.Y() > ladderTop+0.05 && basePos.Y() > ladderTop+0.05 {
			withinLadderHeight = true
		}
		if traversal.Phase == CharacterTraversalPhaseDismount {
			duration := maxf(request.DismountDuration, dt)
			traversal.PhaseElapsed = minf(traversal.PhaseElapsed+dt, duration)
			progress := traversal.PhaseElapsed / duration
			motion := request.TopExitMotion
			if traversal.PendingExit == CharacterTraversalExitLadderBottom {
				motion = request.BottomExitMotion
			}
			desired := CharacterTraversalMotionPosition(request.Entry, request.End, motion, progress)
			if traversal.PendingExit == CharacterTraversalExitLadderTop {
				// The validated landing ledge is the geometry crossed by the pull-up.
				*basePos = desired
			} else {
				next, blocked := moveGroundedTraversalSegment(cmd, voxRt, *basePos, desired, ctrl, acceptEntity)
				if blocked || next.Sub(desired).LenSqr() > 1e-4 {
					finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "dismount_blocked")
					return
				}
				*basePos = next
			}
			if traversal.PhaseElapsed < duration {
				return
			}
			exit := traversal.PendingExit
			topExit := exit == CharacterTraversalExitLadderTop
			finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, exit)
			if topExit {
				*basePos = request.End
				ctrl.Grounded, ctrl.NeedsGroundSnap = true, false
				return
			}
			ctrl.Grounded = false
			resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
			return
		}
		if ladderUse && traversal.Phase == CharacterTraversalPhaseClimb {
			if exit, ok := groundedLadderPlatformExit(cmd, voxRt, *basePos, ladder, ctrl, ladderExitDirection, 0, 0, acceptEntity); ok {
				*basePos = exit
				finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, CharacterTraversalExitLadderPlatform)
				ctrl.Grounded, ctrl.NeedsGroundSnap = true, false
				return
			}
			finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, CharacterTraversalExitLadderReleased)
			ctrl.Grounded = false
			groundedPlayerClearGroundSupport(ctrl)
			ctrl.NeedsGroundSnap = false
			resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
			return
		}
		ctrl.JumpQueued = false
		if traversal.Phase == CharacterTraversalPhaseAlign {
			desired := request.Entry
			if request.StartDelay > 0 {
				traversal.PhaseElapsed = minf(traversal.PhaseElapsed+dt, request.StartDelay)
				desired = CharacterTraversalMotionPosition(request.Start, request.Entry, request.MountMotion, traversal.PhaseElapsed/request.StartDelay)
			}
			blocked := false
			if request.Start.Y() > ladderTop && request.Entry.Y() < ladderTop-0.05 {
				// Reversed pull-up crosses the validated platform edge onto the ladder.
				*basePos = desired
			} else {
				next, hit := moveGroundedTraversalSegment(cmd, voxRt, *basePos, desired, ctrl, acceptEntity)
				*basePos, blocked = next, hit
			}
			delta := desired.Sub(*basePos)
			delta[1] = 0
			if delta.Len() > request.Acceptance {
				if blocked {
					finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "mount_blocked")
				}
				return
			}
			if request.StartDelay > 0 && traversal.PhaseElapsed < request.StartDelay {
				return
			}
			withinLadderHeight = basePos.Y() >= ladderBottom-0.05 && basePos.Y() <= ladderTop+0.05
			traversal.Phase, traversal.Committed = CharacterTraversalPhaseClimb, true
			traversal.PhaseElapsed = 0
			ctrl.OnLadder, ctrl.LadderEntity, ctrl.LadderClimbSpeed = true, request.LadderEntity, request.Speed
			if request.Start.Y() > ladderTop+0.05 && basePos.Y() > ladderTop+0.05 {
				withinLadderHeight = true
			}
		}
		topDismountStart := traversal.Phase == CharacterTraversalPhaseClimb && ladderMovement > 0 && request.TopExitDuration > 0 && basePos.Y() >= ladderTop-characterTraversalMotionVerticalDistance(request.TopExitMotion)-0.05
		if (!withinLadderHeight || topDismountStart) && !forceLadder {
			exitKind := CharacterTraversalExitNone
			if topDismountStart || basePos.Y() >= ladderTop-defaulted(ctrl.StepHeight, 0.6) {
				exitKind = CharacterTraversalExitLadderTop
			} else if basePos.Y() <= ladderBottom+defaulted(ctrl.StepHeight, 0.6) {
				exitKind = CharacterTraversalExitLadderBottom
			}
			if exitKind != CharacterTraversalExitNone {
				traversal.PendingExit = exitKind
				traversal.PhaseElapsed = 0
				if exitKind == CharacterTraversalExitLadderTop {
					probe := *basePos
					probe[1] = ladderTop + maxf(defaulted(ctrl.StepHeight, 0.6), 0.5)
					platformRange := defaulted(ctrl.Height, 1.7) + defaulted(ctrl.StepHeight, 0.6)
					preferred := request.Entry.Sub(ladder.BoundsCenter)
					if exit, ok := groundedLadderPlatformExit(cmd, voxRt, probe, ladder, ctrl, preferred, platformRange, platformRange, acceptEntity); ok {
						if exit.Y() < basePos.Y()-defaulted(ctrl.StepHeight, 0.6) {
							*basePos = exit
							finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, CharacterTraversalExitLadderPlatform)
							ctrl.Grounded, ctrl.NeedsGroundSnap = true, false
							return
						}
						traversal.Request.End = exit
						traversal.Request.Entry = *basePos
						traversal.Request.Apex = *basePos
						traversal.Request.Apex[1] = maxf(basePos.Y(), exit.Y()) + maxf(defaulted(ctrl.StepHeight, 0.6), 0.5)
						traversal.Request.DismountDuration = request.TopExitDuration
						if traversal.Request.DismountDuration > 0 {
							traversal.Phase = CharacterTraversalPhaseDismount
							return
						}
						*basePos = exit
						finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, exitKind)
						ctrl.Grounded, ctrl.NeedsGroundSnap = true, false
						return
					}
					traversal.PendingExit = CharacterTraversalExitNone
					exitKind = CharacterTraversalExitNone
				} else {
					traversal.Request.Entry, traversal.Request.End = *basePos, *basePos
					traversal.Request.DismountDuration = request.BottomExitDuration
				}
				if traversal.Request.DismountDuration > 0 {
					traversal.Phase = CharacterTraversalPhaseDismount
					return
				}
			}
			if exitKind != CharacterTraversalExitNone || !withinLadderHeight {
				finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, exitKind)
				ctrl.Grounded = false
				resolveGroundedVertical(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
				return
			}
		}
		*basePos = tryGroundedHorizontalMove(cmd, voxRt, *basePos, horizontalMove, ctrl, acceptEntity)
		beforeY := basePos.Y()
		*basePos, _ = tryGroundedVerticalMove(cmd, voxRt, *basePos, ladderMovement*request.Speed*dt, ctrl, acceptEntity)
		traversal.LadderDistance += basePos.Y() - beforeY
		ctrl.VerticalVelocity = 0
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
		ctrl.NeedsGroundSnap = false
		ctrl.JumpQueued = false
		return
	}

	if traversal.Phase == CharacterTraversalPhaseAlign && !traversal.HasLadderApproach {
		approach, backoff, ok := groundedLadderApproach(cmd, voxRt, *basePos, ctrl, acceptEntity)
		if !ok {
			*basePos = backoff
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "mount_blocked")
			return
		}
		traversal.LadderApproach, traversal.HasLadderApproach = approach, true
	}
	if traversal.Phase == CharacterTraversalPhaseAlign {
		delta := traversal.LadderApproach.Sub(*basePos)
		delta[1] = 0
		if delta.Len() > request.Acceptance {
			move := delta.Normalize().Mul(minf(delta.Len(), request.Speed*dt))
			start := *basePos
			result := CharacterGroundedMove(voxRt, start, move, CharacterGroundedMoveOptions{
				CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
				GroundConfig:    groundedPlayerGroundProbeConfig(ctrl),
				AcceptEntity:    acceptEntity,
			})
			*basePos = result.Position
			if result.Blocked {
				recordGroundedTraversalCollision(ctrl, "ladder_approach", start, move, result.Hit)
				finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "mount_blocked")
				return
			}
			return
		}
	}

	if traversal.Phase == CharacterTraversalPhaseAlign && request.StartDelay > 0 {
		traversal.PhaseElapsed = minf(traversal.PhaseElapsed+dt, request.StartDelay)
		desired := CharacterTraversalMotionPosition(traversal.LadderApproach, request.Entry, request.MountMotion, traversal.PhaseElapsed/request.StartDelay)
		next, blocked := moveGroundedTraversalSegment(cmd, voxRt, *basePos, desired, ctrl, acceptEntity)
		if blocked || next.Sub(desired).LenSqr() > 1e-4 {
			finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "mount_blocked")
			return
		}
		*basePos = next
		if traversal.PhaseElapsed < request.StartDelay {
			return
		}
		traversal.Phase, traversal.PhaseElapsed, traversal.Committed = CharacterTraversalPhaseClimb, 0, true
		ctrl.Grounded = false
		groundedPlayerClearGroundSupport(ctrl)
	}

	remaining := request.Speed * dt
	for remaining > 1e-5 && traversal.Running() {
		switch traversal.Phase {
		case CharacterTraversalPhaseAlign:
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
			beforeY := basePos.Y()
			next, used, reached, blocked := moveGroundedTraversalVertical(cmd, voxRt, *basePos, targetY, remaining, ctrl, acceptEntity)
			*basePos, remaining = next, maxf(0, remaining-used)
			traversal.LadderDistance += basePos.Y() - beforeY
			if blocked && !reached {
				failOrSettleGroundedCharacterTraversal(ctrl, "climb_blocked")
				return
			}
			if !reached {
				return
			}
			traversal.Request.Entry = *basePos
			traversal.PendingExit = CharacterTraversalExitLadderTop
			if request.End.Y() < request.Start.Y() {
				traversal.PendingExit = CharacterTraversalExitLadderBottom
			}
			traversal.Phase, traversal.PhaseElapsed = CharacterTraversalPhaseDismount, 0
			if request.DismountDuration > 0 {
				return
			}
		case CharacterTraversalPhaseDismount:
			if request.DismountDuration > 0 {
				traversal.PhaseElapsed = minf(traversal.PhaseElapsed+dt, request.DismountDuration)
				motion := request.TopExitMotion
				if traversal.PendingExit == CharacterTraversalExitLadderBottom {
					motion = request.BottomExitMotion
				}
				desired := CharacterTraversalMotionPosition(traversal.Request.Entry, request.End, motion, traversal.PhaseElapsed/request.DismountDuration)
				next, blocked := moveGroundedTraversalSegment(cmd, voxRt, *basePos, desired, ctrl, acceptEntity)
				if blocked || next.Sub(desired).LenSqr() > 1e-4 {
					if groundedLadderCanSettleEndpoint(cmd, voxRt, *basePos, request.End, ctrl, acceptEntity) {
						*basePos = request.End
						traversal.Phase = CharacterTraversalPhaseSettle
						ctrl.Grounded, ctrl.NeedsGroundSnap = false, true
						settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
						return
					}
					finishGroundedCharacterTraversal(ctrl, CharacterTraversalFailed, "dismount_blocked")
					return
				}
				*basePos = next
				if traversal.PhaseElapsed < request.DismountDuration {
					return
				}
				*basePos = request.End
				traversal.Phase = CharacterTraversalPhaseSettle
				ctrl.Grounded = false
				ctrl.NeedsGroundSnap = true
				settleGroundedCharacterTraversal(cmd, voxRt, basePos, ctrl, dt, acceptEntity)
				return
			}
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

func groundedLadderApproach(cmd *Commands, voxRt *VoxelRtState, current mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, mgl32.Vec3, bool) {
	request := ctrl.Traversal.Request
	direction := request.Entry.Sub(request.Start)
	direction[1] = 0
	if direction.LenSqr() <= 1e-8 {
		direction = request.End.Sub(request.Start)
		direction[1] = 0
	}
	if direction.LenSqr() > 1e-8 {
		direction = direction.Normalize()
	}
	radius := defaulted(ctrl.Radius, 0.35)
	side := mgl32.Vec3{-direction.Z(), 0, direction.X()}
	anchors := [...]mgl32.Vec3{
		request.Start,
		request.Start.Sub(direction.Mul(radius)),
		request.Start.Add(side.Mul(radius * 0.5)),
		request.Start.Sub(side.Mul(radius * 0.5)),
	}
	collision := groundedPlayerCharacterCollisionConfig(cmd, ctrl)
	ground := groundedPlayerGroundProbeConfig(ctrl)
	ground.DynamicCollisionQuery = collision.DynamicCollisionQuery
	backoff := current
	for index, anchor := range anchors {
		move := anchor.Sub(current)
		move[1] = 0
		result := CharacterGroundedMove(voxRt, current, move, CharacterGroundedMoveOptions{
			CollisionConfig: collision, GroundConfig: ground, AcceptEntity: acceptEntity,
		})
		if result.Blocked || actionHorizontalDistance(result.Position, anchor) > ctrl.Traversal.Request.Acceptance {
			continue
		}
		floor, supported := CharacterGroundHitAtWithin(voxRt, result.Position, ground, collision.StepHeight, collision.StepHeight+defaulted(ctrl.GroundProbe, 0.15), acceptEntity)
		if !supported || !CharacterAcceptsGroundY(current.Y(), floor.Y, collision.StepHeight, collision.StepHeight+defaulted(ctrl.GroundProbe, 0.15)) {
			continue
		}
		result.Position[1] = floor.Y
		if !CharacterHasStandingClearance(voxRt, result.Position, collision, acceptEntity) {
			continue
		}
		if index == 1 {
			backoff = result.Position
		}
		if groundedLadderMountSweepClear(cmd, voxRt, result.Position, request.Entry, request.StartDelay, request.MountMotion, ctrl, acceptEntity) {
			return result.Position, backoff, true
		}
	}
	return mgl32.Vec3{}, backoff, false
}

func groundedLadderMountSweepClear(cmd *Commands, voxRt *VoxelRtState, start, end mgl32.Vec3, duration float32, motion []CharacterTraversalMotionKey, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) bool {
	probe := *ctrl
	current := start
	if duration <= 0 {
		end[1] = start.Y()
	}
	const samples = 12
	for sample := 1; sample <= samples; sample++ {
		desired := CharacterTraversalMotionPosition(start, end, motion, float32(sample)/samples)
		next, blocked := moveGroundedTraversalSegment(cmd, voxRt, current, desired, &probe, acceptEntity)
		if blocked || next.Sub(desired).LenSqr() > 1e-4 {
			return false
		}
		current = next
	}
	return true
}

func groundedLadderCanSettleEndpoint(cmd *Commands, voxRt *VoxelRtState, current, end mgl32.Vec3, ctrl *GroundedCharacterMotorComponent, acceptEntity func(EntityId, bool) bool) bool {
	collision := groundedPlayerCharacterCollisionConfig(cmd, ctrl)
	ground := groundedPlayerGroundProbeConfig(ctrl)
	ground.DynamicCollisionQuery = collision.DynamicCollisionQuery
	floor, supported := CharacterGroundHitAtWithin(voxRt, end, ground, 0.05, defaulted(ctrl.GroundProbe, 0.15), acceptEntity)
	if !supported || float32(math.Abs(float64(floor.Y-end.Y()))) > 0.05 || !CharacterHasStandingClearance(voxRt, end, collision, acceptEntity) {
		return false
	}
	probe := *ctrl
	next, blocked := moveGroundedTraversalSegment(cmd, voxRt, current, end, &probe, acceptEntity)
	return !blocked && next.Sub(end).LenSqr() <= 1e-4
}

// groundedLadderPlatformExit checks the two sides of the current ladder only
// when the actor asks to leave it. It avoids a continuous world scan.
func groundedLadderPlatformExit(cmd *Commands, voxRt *VoxelRtState, basePos mgl32.Vec3, ladder LadderVolumeComponent, ctrl *GroundedCharacterMotorComponent, preferred mgl32.Vec3, maxRise, maxDrop float32, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, bool) {
	if ctrl == nil || ladder.BoundsHalfExtents == (mgl32.Vec3{}) {
		return mgl32.Vec3{}, false
	}
	normalAxis, tangentAxis := 2, 0
	if ladder.BoundsHalfExtents.X() < ladder.BoundsHalfExtents.Z() {
		normalAxis, tangentAxis = 0, 2
	}
	preferred[1] = 0
	sides := [2]float32{1, -1}
	if preferred[normalAxis] < 0 {
		sides[0], sides[1] = sides[1], sides[0]
	}
	collision := groundedPlayerCharacterCollisionConfig(cmd, ctrl)
	ground := groundedPlayerGroundProbeConfig(ctrl)
	ground.DynamicCollisionQuery = MovingBrushCollisionQuery(cmd)
	maxRise = defaulted(maxRise, collision.StepHeight)
	maxDrop = defaulted(maxDrop, collision.StepHeight+defaulted(ctrl.GroundProbe, 0.15))
	tangent := maxf(ladder.BoundsCenter[tangentAxis]-ladder.BoundsHalfExtents[tangentAxis], minf(basePos[tangentAxis], ladder.BoundsCenter[tangentAxis]+ladder.BoundsHalfExtents[tangentAxis]))
	clearances := []float32{0.03, collision.Radius, collision.Radius * 2, collision.Radius * 3, collision.Radius * 4}
	for _, side := range sides {
		for _, clearance := range clearances {
			for _, tangentOffset := range []float32{0, -collision.Radius, collision.Radius, -ladder.BoundsHalfExtents[tangentAxis] - collision.Radius, ladder.BoundsHalfExtents[tangentAxis] + collision.Radius} {
				candidate := basePos
				candidate[normalAxis] = ladder.BoundsCenter[normalAxis] + side*(ladder.BoundsHalfExtents[normalAxis]+collision.Radius+clearance)
				candidate[tangentAxis] = tangent + tangentOffset
				floor, ok := CharacterGroundHitAtWithin(voxRt, candidate, ground, maxRise, maxDrop, acceptEntity)
				if !ok || !CharacterAcceptsGroundY(basePos.Y(), floor.Y, maxRise, maxDrop) {
					continue
				}
				candidate[1] = floor.Y
				if !CharacterHasStandingClearance(voxRt, candidate, collision, acceptEntity) {
					continue
				}
				move := CharacterKinematicMove(voxRt, basePos, candidate.Sub(basePos), CharacterKinematicMoveOptions{CollisionConfig: collision, AcceptEntity: acceptEntity, DisableSlide: true})
				// Kinematic moves are horizontal; allow its skin-width stopping distance
				// and leave the vertical part to the dismount traversal.
				delta := move.Position.Sub(candidate)
				delta[1] = 0
				tolerance := effectiveCharacterCollisionConfig(collision).SkinWidth + 0.005
				if delta.LenSqr() <= tolerance*tolerance {
					return candidate, true
				}
			}
		}
	}
	return mgl32.Vec3{}, false
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
		if request.Kind != CharacterTraversalCrawl {
			apex[1] = maxf(request.Start.Y(), request.End.Y()) + maxf(defaulted(ctrl.StepHeight, 0.6), 0.5)
		}
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
	} else if ctrl.Traversal.Request.Kind == CharacterTraversalLadder && ctrl.Traversal.PendingExit != CharacterTraversalExitNone {
		finishGroundedLadderTraversal(ctrl, CharacterTraversalSucceeded, ctrl.Traversal.PendingExit)
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

func finishGroundedLadderTraversal(ctrl *GroundedCharacterMotorComponent, status CharacterTraversalStatus, exit CharacterTraversalExit) {
	if ctrl == nil {
		return
	}
	reason := string(exit)
	ctrl.Traversal.Exit = exit
	ctrl.Traversal.PendingExit = CharacterTraversalExitNone
	finishGroundedCharacterTraversal(ctrl, status, reason)
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
	if ctrl.Traversal.Request.Kind == CharacterTraversalDrop && !ctrl.Traversal.WasAirborne {
		acceptance = 0.01
	}
	if distance <= acceptance {
		return current, 0, true, false
	}
	move := delta.Normalize().Mul(minf(distance, maxf(maxDistance, 0)))
	result := CharacterKinematicMove(voxRt, current, move, CharacterKinematicMoveOptions{
		CollisionConfig: groundedPlayerCharacterCollisionConfig(cmd, ctrl),
		AcceptEntity:    acceptEntity, DisableSlide: true,
	})
	if result.Blocked {
		recordGroundedTraversalCollision(ctrl, "horizontal", current, move, result.Hit)
	}
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
	next, hit, blocked := characterVerticalMoveHit(voxRt, current, move, groundedPlayerCharacterCollisionConfig(cmd, ctrl), acceptEntity)
	if blocked {
		recordGroundedTraversalCollision(ctrl, "vertical", current, mgl32.Vec3{0, move, 0}, hit)
	}
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
		if result.Blocked {
			recordGroundedTraversalCollision(ctrl, "segment_horizontal", current, horizontal, result.Hit)
		}
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

func recordGroundedTraversalCollision(ctrl *GroundedCharacterMotorComponent, test string, start, move mgl32.Vec3, hit CharacterCollisionHit) {
	if ctrl == nil {
		return
	}
	ctrl.Traversal.CollisionTest, ctrl.Traversal.CollisionStart, ctrl.Traversal.CollisionMove, ctrl.Traversal.CollisionHit = test, start, move, hit
}

func CharacterTraversalMotionPosition(start, end mgl32.Vec3, keys []CharacterTraversalMotionKey, progress float32) mgl32.Vec3 {
	progress = maxf(0, minf(1, progress))
	if len(keys) < 2 {
		return start.Mul(1 - progress).Add(end.Mul(progress))
	}
	first := keys[0].Position
	sourceEnd := keys[len(keys)-1].Position.Sub(first)
	local := sampleCharacterTraversalMotion(keys, progress).Sub(first)
	target := end.Sub(start)

	sourceHorizontal := mgl32.Vec2{sourceEnd.X(), sourceEnd.Z()}
	targetHorizontal := mgl32.Vec2{target.X(), target.Z()}
	mappedHorizontal := targetHorizontal.Mul(progress)
	if sourceHorizontal.LenSqr() > 1e-8 && targetHorizontal.LenSqr() > 1e-8 {
		angle := float32(math.Atan2(float64(targetHorizontal.Y()), float64(targetHorizontal.X())) - math.Atan2(float64(sourceHorizontal.Y()), float64(sourceHorizontal.X())))
		cosAngle, sinAngle := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
		mappedHorizontal = mgl32.Vec2{
			local.X()*cosAngle - local.Z()*sinAngle,
			local.X()*sinAngle + local.Z()*cosAngle,
		}.Mul(targetHorizontal.Len() / sourceHorizontal.Len())
	}
	mappedY := local.Y()
	if absf(sourceEnd.Y()) > 1e-5 {
		mappedY *= target.Y() / sourceEnd.Y()
	}
	return start.Add(mgl32.Vec3{mappedHorizontal.X(), mappedY, mappedHorizontal.Y()})
}

func characterTraversalMotionVerticalDistance(keys []CharacterTraversalMotionKey) float32 {
	if len(keys) < 2 {
		return 0
	}
	return absf(keys[len(keys)-1].Position.Y() - keys[0].Position.Y())
}

func sampleCharacterTraversalMotion(keys []CharacterTraversalMotionKey, progress float32) mgl32.Vec3 {
	if len(keys) == 0 {
		return mgl32.Vec3{}
	}
	if progress <= keys[0].Progress {
		return keys[0].Position
	}
	for index := 1; index < len(keys); index++ {
		if progress > keys[index].Progress {
			continue
		}
		previous, next := keys[index-1], keys[index]
		span := next.Progress - previous.Progress
		if span <= 1e-6 {
			return next.Position
		}
		t := (progress - previous.Progress) / span
		return previous.Position.Mul(1 - t).Add(next.Position.Mul(t))
	}
	return keys[len(keys)-1].Position
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

// GroundedCharacterCollisionConfig returns the collision shape and runtime
// geometry query used by the grounded motor.
func GroundedCharacterCollisionConfig(cmd *Commands, ctrl *GroundedCharacterMotorComponent) CharacterCollisionConfig {
	return groundedPlayerCharacterCollisionConfig(cmd, ctrl)
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
