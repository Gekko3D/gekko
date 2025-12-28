package core

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestFrustumCulling(t *testing.T) {
	// Setup a simple camera at origin looking down -Z
	// Perspective: 90 deg FOV, Aspect 1.0, Near 1, Far 100
	proj := mgl32.Perspective(mgl32.DegToRad(90), 1.0, 1.0, 100.0)
	view := mgl32.LookAtV(
		mgl32.Vec3{0, 0, 0},  // Eye
		mgl32.Vec3{0, 0, -1}, // Center
		mgl32.Vec3{0, 1, 0},  // Up
	)
	viewProj := proj.Mul4(view)

	cam := &CameraState{}
	planes := cam.ExtractFrustum(viewProj)

	tests := []struct {
		name     string
		aabbMin  mgl32.Vec3
		aabbMax  mgl32.Vec3
		expected bool
	}{
		{
			name:     "Inside (center)",
			aabbMin:  mgl32.Vec3{-1, -1, -10},
			aabbMax:  mgl32.Vec3{1, 1, -5},
			expected: true,
		},
		{
			name:     "Outside (Left)",
			aabbMin:  mgl32.Vec3{-20, -1, -10},
			aabbMax:  mgl32.Vec3{-15, 1, -5},
			expected: false,
		},
		{
			name:     "Outside (Right)",
			aabbMin:  mgl32.Vec3{15, -1, -10},
			aabbMax:  mgl32.Vec3{20, 1, -5},
			expected: false,
		},
		{
			name:     "Outside (Behind/Near)",
			aabbMin:  mgl32.Vec3{-1, -1, 2},
			aabbMax:  mgl32.Vec3{1, 1, 5},
			expected: false,
		},
		{
			name:     "Outside (Far)",
			aabbMin:  mgl32.Vec3{-1, -1, -200},
			aabbMax:  mgl32.Vec3{1, 1, -150},
			expected: false,
		},
		{
			name:     "Intersecting (Left Plane)",
			aabbMin:  mgl32.Vec3{-15, -1, -10}, // Left edge is at roughly -10 (tan(45)*10)
			aabbMax:  mgl32.Vec3{-5, 1, -5},
			expected: true,
		},
		{
			name:     "Encompassing (Huge box)",
			aabbMin:  mgl32.Vec3{-1000, -1000, -1000},
			aabbMax:  mgl32.Vec3{1000, 1000, 1000},
			expected: true,
		},
	}

	for _, tc := range tests {
		aabb := [2]mgl32.Vec3{tc.aabbMin, tc.aabbMax}
		visible := AABBInFrustum(aabb, planes)
		if visible != tc.expected {
			t.Errorf("Test %s failed: expected %v, got %v", tc.name, tc.expected, visible)
			// Debug info
			t.Logf("Planes:")
			for i, p := range planes {
				// Calculate dist of center
				center := tc.aabbMin.Add(tc.aabbMax).Mul(0.5)
				dist := p.Dot(center.Vec4(1.0))
				t.Logf("  P%d: %v, Dist(Center)=%f", i, p, dist)
			}
		}
	}
}

// Test extracting from Identity - Ortho box
func TestFrustumOrtho(t *testing.T) {
	// Ortho -1..1
	// Looking down -Z? mgl32.Ortho default logic.
	// Let's assume GL depth -1..1
	// Left=-1, Right=1, Bottom=-1, Top=1, Near=-1, Far=1?
	// Usually Ortho(left, right, bottom, top, near, far)
	proj := mgl32.Ortho(-10, 10, -10, 10, 0, 20)
	// View at origin looking down -Z
	view := mgl32.LookAtV(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, -1}, mgl32.Vec3{0, 1, 0})

	vp := proj.Mul4(view)
	cam := &CameraState{}
	planes := cam.ExtractFrustum(vp)

	// AABB at 0,0,-5 should be inside
	aabb := [2]mgl32.Vec3{{-1, -1, -6}, {1, 1, -4}}
	if !AABBInFrustum(aabb, planes) {
		t.Error("Ortho: AABB should be inside")
	}

	// AABB at 0,0,-25 should be outside (Far is 20)
	// Note: View is looking down -Z. mgl32.LookAtV flips Z.
	// So -Z points into screen.
	// Near=0 => Z=0. Far=20 => Z=-20.
	// So -25 is beyond far plane.
	aabbFar := [2]mgl32.Vec3{{-1, -1, -26}, {1, 1, -24}}
	if AABBInFrustum(aabbFar, planes) {
		t.Error("Ortho: AABB at -25 should be outside (Far=20 => Z=-20)")
	}
}
