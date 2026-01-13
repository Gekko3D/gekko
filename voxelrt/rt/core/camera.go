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
	DebugMode   uint32
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
	// Z-up: Forward in XY plane, Z for pitch
	return mgl32.Vec3{
		float32(math.Cos(float64(c.Pitch)) * math.Sin(float64(c.Yaw))),
		float32(-math.Cos(float64(c.Pitch)) * math.Cos(float64(c.Yaw))),
		float32(math.Sin(float64(c.Pitch))),
	}
}

func (c *CameraState) GetRight() mgl32.Vec3 {
	// Z-up: Right in XY plane
	return mgl32.Vec3{
		float32(-math.Sin(float64(c.Yaw))),
		float32(math.Cos(float64(c.Yaw))),
		0,
	}
}

func (c *CameraState) GetViewMatrix() mgl32.Mat4 {
	forward := c.GetForward()
	eye := c.Position
	target := eye.Add(forward)
	up := mgl32.Vec3{0, 0, 1} // Z-up
	return mgl32.LookAtV(eye, target, up)
}

// ExtractFrustum extracts the 6 planes of the frustum from the view-projection matrix.
// Returns planes in order: Left, Right, Bottom, Top, Near, Far.
// Plane is Ax + By + Cz + D = 0.
func (c *CameraState) ExtractFrustum(vp mgl32.Mat4) [6]mgl32.Vec4 {
	var planes [6]mgl32.Vec4

	// Left plane: Row 3 + Row 0
	planes[0] = mgl32.Vec4{
		vp.At(3, 0) + vp.At(0, 0),
		vp.At(3, 1) + vp.At(0, 1),
		vp.At(3, 2) + vp.At(0, 2),
		vp.At(3, 3) + vp.At(0, 3),
	}
	// Right plane: Row 3 - Row 0
	planes[1] = mgl32.Vec4{
		vp.At(3, 0) - vp.At(0, 0),
		vp.At(3, 1) - vp.At(0, 1),
		vp.At(3, 2) - vp.At(0, 2),
		vp.At(3, 3) - vp.At(0, 3),
	}
	// Bottom plane: Row 3 + Row 1
	planes[2] = mgl32.Vec4{
		vp.At(3, 0) + vp.At(1, 0),
		vp.At(3, 1) + vp.At(1, 1),
		vp.At(3, 2) + vp.At(1, 2),
		vp.At(3, 3) + vp.At(1, 3),
	}
	// Top plane: Row 3 - Row 1
	planes[3] = mgl32.Vec4{
		vp.At(3, 0) - vp.At(1, 0),
		vp.At(3, 1) - vp.At(1, 1),
		vp.At(3, 2) - vp.At(1, 2),
		vp.At(3, 3) - vp.At(1, 3),
	}
	// Near plane: Row 3 + Row 2 (OpenGL-style -1..1)
	planes[4] = mgl32.Vec4{
		vp.At(3, 0) + vp.At(2, 0),
		vp.At(3, 1) + vp.At(2, 1),
		vp.At(3, 2) + vp.At(2, 2),
		vp.At(3, 3) + vp.At(2, 3),
	}
	// Far plane: Row 3 - Row 2
	planes[5] = mgl32.Vec4{
		vp.At(3, 0) - vp.At(2, 0),
		vp.At(3, 1) - vp.At(2, 1),
		vp.At(3, 2) - vp.At(2, 2),
		vp.At(3, 3) - vp.At(2, 3),
	}

	// Normalize planes
	for i := 0; i < 6; i++ {
		length := float32(math.Sqrt(float64(planes[i][0]*planes[i][0] + planes[i][1]*planes[i][1] + planes[i][2]*planes[i][2])))
		if length > 0 {
			planes[i] = planes[i].Mul(1.0 / length)
		}
	}

	return planes
}
