package core

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type CameraState struct {
	Position    mgl32.Vec3
	Yaw         float32
	Pitch       float32
	Speed       float32
	Sensitivity float32
}

func NewCameraState() *CameraState {
	return &CameraState{
		Position:    mgl32.Vec3{0, 2, 20},
		Yaw:         0,
		Pitch:       0,
		Speed:       10.0,
		Sensitivity: 0.003,
	}
}

func (c *CameraState) GetForward() mgl32.Vec3 {
	return mgl32.Vec3{
		float32(math.Sin(float64(c.Yaw)) * math.Cos(float64(c.Pitch))),
		float32(math.Sin(float64(c.Pitch))),
		float32(-math.Cos(float64(c.Yaw)) * math.Cos(float64(c.Pitch))),
	}
}

func (c *CameraState) GetRight() mgl32.Vec3 {
	return mgl32.Vec3{
		float32(math.Cos(float64(c.Yaw))),
		0,
		float32(math.Sin(float64(c.Yaw))),
	}
}

func (c *CameraState) GetViewMatrix() mgl32.Mat4 {
	forward := c.GetForward()
	eye := c.Position
	target := eye.Add(forward)
	up := mgl32.Vec3{0, 1, 0} // Approximate up is fine for LookAt
	return mgl32.LookAtV(eye, target, up)
}
