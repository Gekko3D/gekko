package volume

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBakedVoxelNormalFitsThreeToOneStairStepSurface(t *testing.T) {
	occupied := map[[3]int]bool{}
	for z := -3; z <= 3; z++ {
		for x := 0; x <= 8; x++ {
			occupied[[3]int{x, x / 3, z}] = true
		}
	}

	opts := VoxelNormalBakeOptions{
		SampleOccupancy: func(voxel [3]int) bool { return occupied[voxel] },
	}
	normal, valid, twoSided := BakedVoxelNormal(opts, [3]int{4, 1, 0})
	if !valid || !twoSided {
		t.Fatalf("expected a valid two-sided normal, got valid=%t twoSided=%t", valid, twoSided)
	}
	want := mgl32.Vec3{-1, 3, 0}.Normalize()
	if normal.Dot(want) < 0.98 {
		t.Fatalf("expected 3:1 stair normal near %v, got %v", want, normal)
	}
}
