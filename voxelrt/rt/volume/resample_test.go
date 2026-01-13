package volume

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestXBrickMap_Resample(t *testing.T) {
	x := NewXBrickMap()

	// Create a 4x4x4 block at 0,0,0
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				x.SetVoxel(i, j, k, 1)
			}
		}
	}

	x.ComputeAABB()
	minB, maxB := x.CachedMin, x.CachedMax
	if minB != (mgl32.Vec3{0, 0, 0}) {
		t.Errorf("Expected min 0,0,0, got %v", minB)
	}
	if maxB != (mgl32.Vec3{4, 4, 4}) {
		t.Errorf("Expected max 4,4,4, got %v", maxB)
	}

	// Scale up by 2.0
	// New block should be 8x8x8
	newX := x.Resample(2.0)
	newX.ComputeAABB()

	nMin, nMax := newX.CachedMin, newX.CachedMax
	if nMin.X() < -0.1 || nMin.Y() < -0.1 || nMin.Z() < -0.1 {
		t.Errorf("Expected new min near 0, got %v", nMin)
	}
	// We might lose a bit of precision or alignment depending on rounding, but roughly should be 8
	if nMax.X() < 7.9 || nMax.X() > 8.1 {
		t.Errorf("Expected new max near 8, got %v", nMax)
	}

	// Scale down by 0.5 (from original)
	newSmall := x.Resample(0.5)
	newSmall.ComputeAABB()
	sMin, sMax := newSmall.CachedMin, newSmall.CachedMax
	if sMin.X() < -0.1 {
		t.Errorf("Expected small min near 0, got %v", sMin)
	}
	if sMax.X() < 1.9 || sMax.X() > 2.1 {
		t.Errorf("Expected small max near 2, got %v", sMax)
	}
}
