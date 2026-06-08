package gekko

import (
	"math"
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type GroundedPlayerControllerConfig struct {
	Height           float32
	EyeHeight        float32
	Radius           float32
	Speed            float32
	SprintMultiplier float32
	Sensitivity      float32
	JumpSpeed        float32
	Gravity          float32
	StepHeight       float32
	GroundProbe      float32
}

type GroundedPlayerControllerDefaults struct {
	Config GroundedPlayerControllerConfig
}

type GroundedPlayerControllerModule struct {
	Config GroundedPlayerControllerConfig
}

type GroundedPlayerControllerComponent struct {
	Height           float32
	EyeHeight        float32
	Radius           float32
	Speed            float32
	SprintMultiplier float32
	Sensitivity      float32
	JumpSpeed        float32
	Gravity          float32
	StepHeight       float32
	GroundProbe      float32

	MoveInput        mgl32.Vec2
	LookInput        mgl32.Vec2
	JumpQueued       bool
	VerticalVelocity float32
	Grounded         bool
	NeedsGroundSnap  bool
	OnLadder         bool
	LadderEntity     EntityId
	LadderClimbSpeed float32
}

func DefaultGroundedPlayerControllerConfig() GroundedPlayerControllerConfig {
	return GroundedPlayerControllerConfig{
		Height:           1.8,
		EyeHeight:        1.7,
		Radius:           0.35,
		Speed:            5.5,
		SprintMultiplier: 1.6,
		Sensitivity:      0.1,
		JumpSpeed:        5.5,
		Gravity:          18.0,
		StepHeight:       0.6,
		GroundProbe:      0.15,
	}
}

func effectiveGroundedPlayerControllerConfig(cfg GroundedPlayerControllerConfig) GroundedPlayerControllerConfig {
	defaults := DefaultGroundedPlayerControllerConfig()
	if cfg.Height != 0 {
		defaults.Height = cfg.Height
	}
	if cfg.EyeHeight != 0 {
		defaults.EyeHeight = cfg.EyeHeight
	}
	if cfg.Radius != 0 {
		defaults.Radius = cfg.Radius
	}
	if cfg.Speed != 0 {
		defaults.Speed = cfg.Speed
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

func (mod GroundedPlayerControllerModule) Install(app *App, cmd *Commands) {
	if app != nil {
		if _, ok := app.resources[reflect.TypeOf(GroundedPlayerControllerDefaults{})]; !ok {
			cmd.AddResources(&GroundedPlayerControllerDefaults{
				Config: effectiveGroundedPlayerControllerConfig(mod.Config),
			})
		}
	}
	app.UseSystem(System(groundedPlayerInputSystem).InStage(Update).RunAlways())
	app.UseSystem(System(groundedPlayerControlSystem).InStage(Update).RunAlways())
	app.UseSystem(System(groundedPlayerUseSystem).InStage(Update).RunAlways())
	app.UseSystem(System(triggerVolumeTouchSystem).InStage(Update).RunAlways())
	app.UseSystem(System(targetEventSystem).InStage(Update).RunAlways())
	app.UseSystem(System(movingBrushMotionSystem).InStage(Update).RunAlways())
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
		Height:           cfg.Height,
		EyeHeight:        cfg.EyeHeight,
		Radius:           cfg.Radius,
		Speed:            cfg.Speed,
		SprintMultiplier: cfg.SprintMultiplier,
		Sensitivity:      cfg.Sensitivity,
		JumpSpeed:        cfg.JumpSpeed,
		Gravity:          cfg.Gravity,
		StepHeight:       cfg.StepHeight,
		GroundProbe:      cfg.GroundProbe,
		Grounded:         true,
		NeedsGroundSnap:  true,
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
	MakeQuery1[GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, ctrl *GroundedPlayerControllerComponent) bool {
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
		return true
	})
}

func groundedPlayerControlSystem(cmd *Commands, time *Time, input *Input, voxRt *VoxelRtState) {
	if time == nil {
		return
	}
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(eid EntityId, cam *CameraComponent, ctrl *GroundedPlayerControllerComponent) bool {
		applyGroundedLook(cam, ctrl)
		basePos := cam.Position.Sub(mgl32.Vec3{0, maxf(ctrl.EyeHeight, 0.01), 0})

		flatForward := forwardFromYawPitch(cam.Yaw, 0)
		right := flatForward.Cross(mgl32.Vec3{0, 1, 0}).Normalize()
		speed := defaulted(ctrl.Speed, 5.5)
		if input != nil && input.Pressed[KeyShift] {
			speed *= defaulted(ctrl.SprintMultiplier, 1.6)
		}

		if ladderEntity, ladder, ok := findGroundedPlayerLadderVolume(cmd, basePos, ctrl); ok {
			ctrl.OnLadder = true
			ctrl.LadderEntity = ladderEntity
			ctrl.LadderClimbSpeed = ladder.NormalizedClimbSpeed()
			lateralMove := right.Mul(ctrl.MoveInput[0] * speed * 0.5 * dt)
			basePos = tryGroundedHorizontalMove(voxRt, basePos, lateralMove, ctrl)
			resolveGroundedLadderMovement(voxRt, &basePos, ctrl, dt)
		} else {
			ctrl.OnLadder = false
			ctrl.LadderEntity = 0
			ctrl.LadderClimbSpeed = 0
			move := right.Mul(ctrl.MoveInput[0]).Add(flatForward.Mul(ctrl.MoveInput[1]))
			if move.Len() > 0 {
				move = move.Normalize()
			}
			horizontalMove := move.Mul(speed * dt)
			basePos = tryGroundedHorizontalMove(voxRt, basePos, horizontalMove, ctrl)
			resolveGroundedVertical(voxRt, &basePos, ctrl, dt)
		}

		cam.Position = basePos.Add(mgl32.Vec3{0, maxf(ctrl.EyeHeight, 0.01), 0})
		cam.LookAt = cam.Position.Add(forwardFromYawPitch(cam.Yaw, cam.Pitch))
		cam.Up = mgl32.Vec3{0, 1, 0}
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
		return true
	})
}

func groundedPlayerUseSystem(cmd *Commands, input *Input) {
	if cmd == nil || input == nil || !input.JustPressed[KeyE] {
		return
	}
	MakeQuery2[CameraComponent, GroundedPlayerControllerComponent](cmd).Map(func(_ EntityId, cam *CameraComponent, _ *GroundedPlayerControllerComponent) bool {
		origin := cam.Position
		dir := forwardFromYawPitch(cam.Yaw, cam.Pitch)
		if hit, ok := findUseTriggerHit(cmd, origin, dir, 2.2); ok {
			hit.Trigger.ActivationCount++
			activateMovingBrushAtBounds(cmd, hit.Trigger.BoundsCenter, hit.Trigger.BoundsHalfExtents)
			ActivateTarget(cmd, hit.Trigger.Target, 0)
			return false
		}
		if hit, ok := findMovingBrushUseHit(cmd, origin, dir, 2.2); ok {
			hit.Brush.ActivationCount++
			hit.Brush.Open = !hit.Brush.Open
			if hit.Brush.Target != "" {
				ActivateTarget(cmd, hit.Brush.Target, 0)
			}
			return false
		}
		return true
	})
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
		brush.Open = !brush.Open
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

func resolveGroundedLadderMovement(voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, dt float32) {
	if basePos == nil || ctrl == nil {
		return
	}
	if ctrl.JumpQueued {
		ctrl.OnLadder = false
		ctrl.LadderEntity = 0
		ctrl.Grounded = false
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = defaulted(ctrl.JumpSpeed, 5.5) * 0.5
		nextBase, blocked := tryGroundedVerticalMove(voxRt, *basePos, ctrl.VerticalVelocity*dt, ctrl)
		*basePos = nextBase
		if blocked {
			ctrl.VerticalVelocity = 0
		}
		ctrl.JumpQueued = false
		return
	}
	*basePos, _ = tryGroundedVerticalMove(voxRt, *basePos, ctrl.MoveInput[1]*defaulted(ctrl.LadderClimbSpeed, DefaultLadderClimbSpeed)*dt, ctrl)
	ctrl.VerticalVelocity = 0
	ctrl.Grounded = false
	ctrl.NeedsGroundSnap = false
	ctrl.JumpQueued = false
}

func tryGroundedVerticalMove(voxRt *VoxelRtState, basePos mgl32.Vec3, deltaY float32, ctrl *GroundedPlayerControllerComponent) (mgl32.Vec3, bool) {
	if voxRt == nil || ctrl == nil || math.Abs(float64(deltaY)) <= 1e-5 {
		return basePos.Add(mgl32.Vec3{0, deltaY, 0}), false
	}
	height := defaulted(ctrl.Height, 1.8)
	radius := defaulted(ctrl.Radius, 0.35)
	dirY := float32(1)
	originY := height
	if deltaY < 0 {
		dirY = -1
		originY = 0.02
	}
	dir := mgl32.Vec3{0, dirY, 0}
	distance := float32(math.Abs(float64(deltaY)))
	clearance := float32(0.03)
	allowed := distance
	for _, offset := range groundedVerticalCollisionOffsets(radius) {
		origin := basePos.Add(offset).Add(mgl32.Vec3{0, originY, 0})
		hit := voxRt.Raycast(origin, dir, distance+clearance)
		if !hit.Hit || hit.T > distance+clearance {
			continue
		}
		allowed = minf(allowed, maxf(hit.T-clearance, 0))
	}
	if allowed < distance {
		return basePos.Add(mgl32.Vec3{0, dirY * allowed, 0}), true
	}
	return basePos.Add(mgl32.Vec3{0, deltaY, 0}), false
}

func groundedVerticalCollisionOffsets(radius float32) []mgl32.Vec3 {
	r := maxf(radius*0.85, 0)
	if r <= 1e-5 {
		return []mgl32.Vec3{{0, 0, 0}}
	}
	return []mgl32.Vec3{
		{0, 0, 0},
		{r, 0, 0},
		{-r, 0, 0},
		{0, 0, r},
		{0, 0, -r},
	}
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

func tryGroundedHorizontalMove(voxRt *VoxelRtState, basePos mgl32.Vec3, move mgl32.Vec3, ctrl *GroundedPlayerControllerComponent) mgl32.Vec3 {
	if move.Len() <= 0 {
		return basePos
	}
	target := basePos.Add(mgl32.Vec3{move.X(), 0, move.Z()})
	if !groundedMovementBlocked(voxRt, basePos, mgl32.Vec3{move.X(), 0, move.Z()}, ctrl) {
		return target
	}
	xOnly := mgl32.Vec3{move.X(), 0, 0}
	if math.Abs(float64(xOnly.X())) > 1e-5 && !groundedMovementBlocked(voxRt, basePos, xOnly, ctrl) {
		basePos = basePos.Add(xOnly)
	}
	zOnly := mgl32.Vec3{0, 0, move.Z()}
	if math.Abs(float64(zOnly.Z())) > 1e-5 && !groundedMovementBlocked(voxRt, basePos, zOnly, ctrl) {
		basePos = basePos.Add(zOnly)
	}
	return basePos
}

func groundedMovementBlocked(voxRt *VoxelRtState, basePos, move mgl32.Vec3, ctrl *GroundedPlayerControllerComponent) bool {
	if voxRt == nil || move.Len() <= 0 {
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
			hit := voxRt.Raycast(origin, dir, dist)
			if hit.Hit && hit.T <= dist {
				return true
			}
		}
	}
	return false
}

func resolveGroundedVertical(voxRt *VoxelRtState, basePos *mgl32.Vec3, ctrl *GroundedPlayerControllerComponent, dt float32) {
	if basePos == nil || ctrl == nil {
		return
	}
	height := defaulted(ctrl.Height, 1.8)
	stepHeight := defaulted(ctrl.StepHeight, 0.6)
	groundProbe := defaulted(ctrl.GroundProbe, 0.15)

	if ctrl.Grounded && ctrl.JumpQueued {
		ctrl.Grounded = false
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = defaulted(ctrl.JumpSpeed, 5.5)
	}
	ctrl.JumpQueued = false

	if !ctrl.Grounded {
		ctrl.VerticalVelocity -= defaulted(ctrl.Gravity, 18.0) * dt
		basePos[1] += ctrl.VerticalVelocity * dt
	}

	if voxRt == nil {
		return
	}
	probeOrigin := basePos.Add(mgl32.Vec3{0, height + stepHeight, 0})
	fallDistance := maxf(-ctrl.VerticalVelocity*dt, 0)
	probeDistance := height + stepHeight + groundProbe + fallDistance + groundProbe
	hit := voxRt.Raycast(probeOrigin, mgl32.Vec3{0, -1, 0}, probeDistance)
	if ctrl.NeedsGroundSnap {
		if hit.Hit && hit.Normal.Y() > 0.35 {
			basePos[1] = probeOrigin.Y() - hit.T
			ctrl.VerticalVelocity = 0
			ctrl.Grounded = true
			ctrl.NeedsGroundSnap = false
		}
		return
	}
	if hit.Hit && hit.Normal.Y() > 0.35 {
		floorY := probeOrigin.Y() - hit.T
		if ctrl.VerticalVelocity <= 0 && basePos.Y()-floorY <= stepHeight+groundProbe {
			basePos[1] = floorY
			ctrl.VerticalVelocity = 0
			ctrl.Grounded = true
			return
		}
	}
	ctrl.Grounded = false
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
