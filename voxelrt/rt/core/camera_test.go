package core

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestCameraProjectionMatrixUsesConfiguredParameters(t *testing.T) {
	camera := &CameraState{
		Fov:  45,
		Near: 0.5,
		Far:  250,
	}

	got := camera.ProjectionMatrix(16.0 / 9.0)
	want := mgl32.Perspective(mgl32.DegToRad(45), 16.0/9.0, 0.5, 250)

	for i := 0; i < 16; i++ {
		if diff := abs32(got[i] - want[i]); diff > 1e-5 {
			t.Fatalf("matrix mismatch at %d: got %.6f want %.6f", i, got[i], want[i])
		}
	}
}

func TestCameraScreenToWorldRayRespondsToFov(t *testing.T) {
	camera := &CameraState{
		Position: mgl32.Vec3{0, 0, 0},
		Yaw:      0,
		Pitch:    0,
		Fov:      30,
	}

	narrow := camera.ScreenToWorldRay(1919, 540, 1920, 1080)
	camera.Fov = 90
	wide := camera.ScreenToWorldRay(1919, 540, 1920, 1080)

	forward := mgl32.Vec3{0, 0, -1}
	narrowAngle := float32(math.Acos(float64(clampDot(narrow.Direction.Dot(forward)))))
	wideAngle := float32(math.Acos(float64(clampDot(wide.Direction.Dot(forward)))))
	if wideAngle <= narrowAngle {
		t.Fatalf("expected wider FOV ray to diverge more from forward, got narrow=%.6f wide=%.6f", narrowAngle, wideAngle)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampDot(v float32) float32 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}
