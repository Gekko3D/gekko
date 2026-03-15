package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func powf(a, b float32) float32 {
	return float32(math.Pow(float64(a), float64(b)))
}

func vec3MulComponents(a, b mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{
		a.X() * b.X(),
		a.Y() * b.Y(),
		a.Z() * b.Z(),
	}
}

func vec3DivComponents(a, b mgl32.Vec3) mgl32.Vec3 {
	var out mgl32.Vec3
	if b.X() != 0 {
		out[0] = a.X() / b.X()
	}
	if b.Y() != 0 {
		out[1] = a.Y() / b.Y()
	}
	if b.Z() != 0 {
		out[2] = a.Z() / b.Z()
	}
	return out
}
