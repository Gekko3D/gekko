package core

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestCameraProjectionMatrixUsesConfiguredParameters(t *testing.T) {
	camera := &CameraState{
		Fov:       45,
		Near:      0.5,
		Far:       250,
		DepthMode: DepthModeStandard,
	}

	got := camera.ProjectionMatrix(16.0 / 9.0)
	want := mgl32.Perspective(mgl32.DegToRad(45), 16.0/9.0, 0.5, 250)

	for i := 0; i < 16; i++ {
		if diff := abs32(got[i] - want[i]); diff > 1e-5 {
			t.Fatalf("matrix mismatch at %d: got %.6f want %.6f", i, got[i], want[i])
		}
	}
}

func TestCameraProjectionMatrixReverseZMapsNearAndFarToExpectedClipDepth(t *testing.T) {
	camera := &CameraState{
		Fov:       45,
		Near:      0.5,
		Far:       250,
		DepthMode: DepthModeReverseZ,
	}

	proj := camera.ProjectionMatrix(16.0 / 9.0)

	nearClip := proj.Mul4x1(mgl32.Vec4{0, 0, -camera.Near, 1})
	farClip := proj.Mul4x1(mgl32.Vec4{0, 0, -camera.Far, 1})
	nearNDC := nearClip.Mul(1.0 / nearClip.W())
	farNDC := farClip.Mul(1.0 / farClip.W())

	if diff := abs32(nearNDC.Z() - 1.0); diff > 1e-5 {
		t.Fatalf("expected reverse-z near plane to map to +1, got %.6f", nearNDC.Z())
	}
	if diff := abs32(farNDC.Z() + 1.0); diff > 1e-4 {
		t.Fatalf("expected reverse-z far plane to map to -1, got %.6f", farNDC.Z())
	}
}

func TestCameraProjectionMatrixDepthModesProduceDistinctMatrices(t *testing.T) {
	camera := &CameraState{
		Fov:  60,
		Near: 0.1,
		Far:  4200,
	}

	camera.DepthMode = DepthModeStandard
	standard := camera.ProjectionMatrix(16.0 / 9.0)
	camera.DepthMode = DepthModeReverseZ
	reverse := camera.ProjectionMatrix(16.0 / 9.0)

	same := true
	for i := 0; i < 16; i++ {
		if abs32(standard[i]-reverse[i]) > 1e-6 {
			same = false
			break
		}
	}
	if same {
		t.Fatal("expected standard and reverse-z projection matrices to differ")
	}
}

func TestNewCameraStateDefaultsToStandardDepthMode(t *testing.T) {
	camera := NewCameraState()
	if camera.DepthMode != DepthModeStandard {
		t.Fatalf("expected default depth mode %q, got %q", DepthModeStandard, camera.DepthMode)
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

func TestCameraViewMatrixUsesLookAtAndUpFrame(t *testing.T) {
	camera := &CameraState{
		Position: mgl32.Vec3{2, 3, 4},
		LookAt:   mgl32.Vec3{2, 4, 4},
		Up:       mgl32.Vec3{0, 0, 1},
	}

	got := camera.GetViewMatrix()
	want := mgl32.LookAtV(camera.Position, camera.LookAt, camera.Up)

	for i := 0; i < 16; i++ {
		if diff := abs32(got[i] - want[i]); diff > 1e-5 {
			t.Fatalf("matrix mismatch at %d: got %.6f want %.6f", i, got[i], want[i])
		}
	}
}

func TestCameraScreenToWorldRayUsesCameraFrame(t *testing.T) {
	camera := &CameraState{
		Position: mgl32.Vec3{0, 0, 0},
		LookAt:   mgl32.Vec3{0, 1, 0},
		Up:       mgl32.Vec3{0, 0, 1},
		Fov:      60,
	}

	ray := camera.ScreenToWorldRay(960, 540, 1920, 1080)
	want := mgl32.Vec3{0, 1, 0}
	if ray.Direction.Sub(want).Len() > 1e-5 {
		t.Fatalf("expected center ray to follow LookAt frame, got %v", ray.Direction)
	}
}

func TestCameraClipPointVisibleAcceptsReverseZVisibleDepth(t *testing.T) {
	camera := &CameraState{
		Fov:       60,
		Near:      0.1,
		Far:       1000,
		DepthMode: DepthModeReverseZ,
	}

	clip := camera.ProjectionMatrix(1.0).Mul4x1(mgl32.Vec4{0, 0, -5, 1})
	if !camera.ClipPointVisible(clip) {
		t.Fatal("expected reverse-z clip point inside frustum to be visible")
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
