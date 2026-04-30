package core

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestFrustumCulling(t *testing.T) {
	view := mgl32.LookAtV(
		mgl32.Vec3{0, 0, 0},  // Eye
		mgl32.Vec3{0, 0, -1}, // Center
		mgl32.Vec3{0, 1, 0},  // Up
	)

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
			name:     "Intersecting (Left Plane)",
			aabbMin:  mgl32.Vec3{-15, -1, -10}, // Left edge is at roughly -10 (tan(45)*10)
			aabbMax:  mgl32.Vec3{-5, 1, -5},
			expected: true,
		},
	}

	for _, mode := range []DepthMode{DepthModeStandard, DepthModeReverseZ} {
		cam := &CameraState{
			Fov:       90,
			Near:      1,
			Far:       100,
			DepthMode: mode,
		}
		proj := cam.ProjectionMatrix(1.0)
		viewProj := proj.Mul4(view)
		planes := cam.ExtractFrustum(viewProj)

		for _, tc := range tests {
			aabb := [2]mgl32.Vec3{tc.aabbMin, tc.aabbMax}
			visible := AABBInFrustum(aabb, planes)
			if visible != tc.expected {
				t.Errorf("mode %s, test %s failed: expected %v, got %v", mode, tc.name, tc.expected, visible)
			}
		}
	}
}

// Test extracting from Identity - Ortho box
func TestFrustumOrtho(t *testing.T) {
	// Ortho -1..1
	proj := mgl32.Ortho(-10, 10, -10, 10, 0, 20)
	view := mgl32.LookAtV(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, -1}, mgl32.Vec3{0, 1, 0})

	vp := proj.Mul4(view)
	cam := &CameraState{}
	planes := cam.ExtractFrustum(vp)

	// AABB at 0,0,-5 should be inside
	aabb := [2]mgl32.Vec3{{-1, -1, -6}, {1, 1, -4}}
	if !AABBInFrustum(aabb, planes) {
		t.Error("Ortho: AABB should be inside")
	}
}

func TestOcclusion(t *testing.T) {
	// Mock Hi-Z Buffer
	// 4x4 buffer
	w, h := uint32(4), uint32(4)
	hiz := make([]float32, w*h)

	// Fill with "Near" depth (small distance) -> Occlusion
	// Fill with "Far" depth (large distance) -> Visible

	// Let's say max depth in tile is 10.
	for i := range hiz {
		hiz[i] = 10.0
	}

	view := mgl32.LookAtV(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, -1}, mgl32.Vec3{0, 1, 0})

	for _, mode := range []DepthMode{DepthModeStandard, DepthModeReverseZ} {
		cam := &CameraState{
			Fov:       90,
			Near:      1,
			Far:       100,
			DepthMode: mode,
		}
		vp := cam.ProjectionMatrix(1.0).Mul4(view)

		// 1. Object CLOSE (Z=-5). Dist = 5.
		aabbClose := [2]mgl32.Vec3{{-1, -1, -6}, {1, 1, -4}}
		if IsOccluded(aabbClose, hiz, w, h, vp, 0) {
			t.Errorf("mode %s: close object (dist 5) should NOT be occluded by wall at dist 10", mode)
		}

		// 2. Object FAR (Z=-20). Dist = 20.
		aabbFar := [2]mgl32.Vec3{{-1, -1, -21}, {1, 1, -19}}
		if !IsOccluded(aabbFar, hiz, w, h, vp, 0) {
			t.Errorf("mode %s: far object (dist 20) MUST be occluded by wall at dist 10", mode)
		}

		holeHiZ := append([]float32(nil), hiz...)
		holeHiZ[2*4+2] = 100.0
		holeHiZ[2*4+3] = 100.0
		holeHiZ[3*4+2] = 100.0
		holeHiZ[3*4+3] = 100.0
		if IsOccluded(aabbFar, holeHiZ, w, h, vp, 0) {
			t.Errorf("mode %s: far object should be visible through the hole (depth 100)", mode)
		}
	}
}

func TestOcclusionDepthSlack(t *testing.T) {
	w, h := uint32(4), uint32(4)
	hiz := make([]float32, w*h)
	for i := range hiz {
		hiz[i] = 18.0
	}

	view := mgl32.LookAtV(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, -1}, mgl32.Vec3{0, 1, 0})
	aabb := [2]mgl32.Vec3{{-1, -1, -21}, {1, 1, -19}}

	for _, mode := range []DepthMode{DepthModeStandard, DepthModeReverseZ} {
		cam := &CameraState{
			Fov:       90,
			Near:      1,
			Far:       100,
			DepthMode: mode,
		}
		vp := cam.ProjectionMatrix(1.0).Mul4(view)
		if !IsOccluded(aabb, hiz, w, h, vp, 0) {
			t.Fatalf("mode %s: expected object to be occluded without slack", mode)
		}
		if IsOccluded(aabb, hiz, w, h, vp, 2.5) {
			t.Fatalf("mode %s: expected depth slack to keep the object visible", mode)
		}
	}
}
