package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type FlyingCameraModule struct{}

func (m FlyingCameraModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(FlyingCameraInputSystem).
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(FlyingCameraControlSystem).
			InStage(Update).
			RunAlways(),
	)
}

type FlyingCameraComponent struct {
	Speed       float32
	Sensitivity float32
	Move        mgl32.Vec3
	Look        mgl32.Vec2
}

func FlyingCameraInputSystem(input *Input, cmd *Commands) {
	if input.JustPressed[KeyTab] {
		input.MouseCaptured = !input.MouseCaptured
	}

	MakeQuery1[FlyingCameraComponent](cmd).Map(func(eid EntityId, fly *FlyingCameraComponent) bool {
		fly.Move = mgl32.Vec3{0, 0, 0}
		if input.Pressed[KeyW] {
			fly.Move[2] += 1
		}
		if input.Pressed[KeyS] {
			fly.Move[2] -= 1
		}
		if input.Pressed[KeyA] {
			fly.Move[0] -= 1
		}
		if input.Pressed[KeyD] {
			fly.Move[0] += 1
		}
		if input.Pressed[KeySpace] {
			fly.Move[1] += 1
		}
		if input.Pressed[KeyControl] {
			fly.Move[1] -= 1
		}

		if input.MouseCaptured {
			fly.Look[0] = float32(input.MouseDeltaX)
			fly.Look[1] = float32(input.MouseDeltaY)
		} else {
			fly.Look[0] = 0
			fly.Look[1] = 0
		}

		return true
	})
}

func FlyingCameraControlSystem(cmd *Commands, time *Time) {
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}

	MakeQuery2[CameraComponent, FlyingCameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent, fly *FlyingCameraComponent) bool {
		// 1. Rotation
		if fly.Sensitivity == 0 {
			fly.Sensitivity = 0.1
		}

		cam.Yaw += fly.Look[0] * fly.Sensitivity
		cam.Pitch -= fly.Look[1] * fly.Sensitivity

		// Clamp pitch
		if cam.Pitch > 89.0 {
			cam.Pitch = 89.0
		}
		if cam.Pitch < -89.0 {
			cam.Pitch = -89.0
		}

		yawRad := mgl32.DegToRad(cam.Yaw)
		pitchRad := mgl32.DegToRad(cam.Pitch)

		// Forward Vector
		forward := mgl32.Vec3{
			float32(math.Sin(float64(yawRad)) * math.Cos(float64(pitchRad))),
			float32(math.Sin(float64(pitchRad))),
			float32(-math.Cos(float64(yawRad)) * math.Cos(float64(pitchRad))),
		}.Normalize()

		// Right Vector
		right := forward.Cross(mgl32.Vec3{0, 1, 0}).Normalize()
		up := mgl32.Vec3{0, 1, 0}

		// 2. Movement
		if fly.Speed == 0 {
			fly.Speed = 5.0
		}

		moveDir := mgl32.Vec3{0, 0, 0}
		moveDir = moveDir.Add(right.Mul(fly.Move[0]))
		moveDir = moveDir.Add(up.Mul(fly.Move[1]))
		moveDir = moveDir.Add(forward.Mul(fly.Move[2]))

		if moveDir.Len() > 0 {
			cam.Position = cam.Position.Add(moveDir.Normalize().Mul(fly.Speed * dt))
		}

		// 3. LookAt Sync
		cam.LookAt = cam.Position.Add(forward)
		cam.Up = mgl32.Vec3{0, 1, 0}

		return true
	})
}
