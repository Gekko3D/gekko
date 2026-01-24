package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type FlyingCameraModule struct{}

func (m FlyingCameraModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(FlyingCameraSystem).
			InStage(Update).
			RunAlways(),
	)
}

type FlyingCameraComponent struct {
	Speed       float32
	Sensitivity float32
	Yaw         float32
	Pitch       float32
}

func FlyingCameraSystem(cmd *Commands, input *Input, time *Time) {
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}

	MakeQuery2[CameraComponent, FlyingCameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent, fly *FlyingCameraComponent) bool {
		// 1. Rotation
		if fly.Sensitivity == 0 {
			fly.Sensitivity = 0.1
		}

		if input.MouseCaptured {
			fly.Yaw += float32(input.MouseDeltaX) * fly.Sensitivity
			fly.Pitch -= float32(input.MouseDeltaY) * fly.Sensitivity
		}

		// Clamp pitch
		if fly.Pitch > 89.0 {
			fly.Pitch = 89.0
		}
		if fly.Pitch < -89.0 {
			fly.Pitch = -89.0
		}

		yawRad := mgl32.DegToRad(fly.Yaw)
		pitchRad := mgl32.DegToRad(fly.Pitch)

		// Forward Vector
		forward := mgl32.Vec3{
			float32(math.Sin(float64(yawRad)) * math.Cos(float64(pitchRad))),
			float32(math.Sin(float64(pitchRad))),
			float32(-math.Cos(float64(yawRad)) * math.Cos(float64(pitchRad))),
		}.Normalize()

		// Right Vector
		right := forward.Cross(mgl32.Vec3{0, 1, 0}).Normalize()
		up := right.Cross(forward).Normalize()

		// 2. Movement
		if fly.Speed == 0 {
			fly.Speed = 5.0
		}

		move := mgl32.Vec3{0, 0, 0}
		if input.Pressed[KeyW] {
			move = move.Add(forward)
		}
		if input.Pressed[KeyS] {
			move = move.Sub(forward)
		}
		if input.Pressed[KeyA] {
			move = move.Sub(right)
		}
		if input.Pressed[KeyD] {
			move = move.Add(right)
		}
		if input.Pressed[KeyE] {
			move = move.Add(up)
		}
		if input.Pressed[KeyQ] {
			move = move.Sub(up)
		}

		if move.Len() > 0 {
			cam.Position = cam.Position.Add(move.Normalize().Mul(fly.Speed * dt))
		}

		// 3. LookAt Sync
		cam.LookAt = cam.Position.Add(forward)
		cam.Up = mgl32.Vec3{0, 1, 0}

		return true
	})
}
