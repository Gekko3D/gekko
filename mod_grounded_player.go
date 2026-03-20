package gekko

import (
	"math"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type GroundedPlayerControllerModule struct{}

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
}

func (GroundedPlayerControllerModule) Install(app *App, cmd *Commands) {
	app.UseSystem(System(groundedPlayerInputSystem).InStage(Update).RunAlways())
	app.UseSystem(System(groundedPlayerControlSystem).InStage(Update).RunAlways())
}

func SpawnGroundedPlayerAtMarker(cmd *Commands, marker content.LevelMarkerDef) EntityId {
	transform := levelTransformToComponent(marker.Transform)
	transform.Scale = mgl32.Vec3{1, 1, 1}
	transform.Position = transform.Position.Add(mgl32.Vec3{0, 0, 0})
	forward := forwardFromYawPitch(0, 0)
	ctrl := GroundedPlayerControllerComponent{
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
		move := right.Mul(ctrl.MoveInput[0]).Add(flatForward.Mul(ctrl.MoveInput[1]))
		if move.Len() > 0 {
			move = move.Normalize()
		}
		speed := defaulted(ctrl.Speed, 5.5)
		if input != nil && input.Pressed[KeyShift] {
			speed *= defaulted(ctrl.SprintMultiplier, 1.6)
		}
		horizontalMove := move.Mul(speed * dt)
		basePos = tryGroundedHorizontalMove(voxRt, basePos, horizontalMove, ctrl)

		resolveGroundedVertical(voxRt, &basePos, ctrl, dt)

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
	step := defaulted(ctrl.StepHeight, 0.6)
	samples := []float32{step * 0.5, height * 0.5, maxf(height-0.2, step)}
	for _, sampleY := range samples {
		origin := basePos.Add(mgl32.Vec3{0, sampleY, 0})
		hit := voxRt.Raycast(origin, dir, dist)
		if hit.Hit && hit.T <= dist {
			return true
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
	hit := voxRt.Raycast(probeOrigin, mgl32.Vec3{0, -1, 0}, height+stepHeight+groundProbe+maxf(ctrl.VerticalVelocity*dt, 0))
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
	if cmd == nil || eid == 0 {
		return nil, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(*TransformComponent); ok {
			return tr, true
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
