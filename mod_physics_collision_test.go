package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func naiveTransformedOverlapBounds(overlapMin, overlapMax, bodyPos mgl32.Vec3, invRot mgl32.Quat, center mgl32.Vec3, invVoxelSize float32) (mgl32.Vec3, mgl32.Vec3) {
	localCorner1 := invRot.Rotate(overlapMin.Sub(bodyPos)).Add(center).Mul(invVoxelSize)
	localCorner2 := invRot.Rotate(overlapMax.Sub(bodyPos)).Add(center).Mul(invVoxelSize)

	return mgl32.Vec3{
			minf(localCorner1.X(), localCorner2.X()),
			minf(localCorner1.Y(), localCorner2.Y()),
			minf(localCorner1.Z(), localCorner2.Z()),
		}, mgl32.Vec3{
			maxf(localCorner1.X(), localCorner2.X()),
			maxf(localCorner1.Y(), localCorner2.Y()),
			maxf(localCorner1.Z(), localCorner2.Z()),
		}
}

func bruteForceTransformedOverlapBounds(overlapMin, overlapMax, bodyPos mgl32.Vec3, invRot mgl32.Quat, center mgl32.Vec3, invVoxelSize float32) (mgl32.Vec3, mgl32.Vec3) {
	minBound := mgl32.Vec3{float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)}
	maxBound := mgl32.Vec3{-float32(math.MaxFloat32), -float32(math.MaxFloat32), -float32(math.MaxFloat32)}

	for mask := 0; mask < 8; mask++ {
		corner := overlapMin
		if mask&1 != 0 {
			corner[0] = overlapMax.X()
		}
		if mask&2 != 0 {
			corner[1] = overlapMax.Y()
		}
		if mask&4 != 0 {
			corner[2] = overlapMax.Z()
		}

		local := invRot.Rotate(corner.Sub(bodyPos)).Add(center).Mul(invVoxelSize)
		for axis := 0; axis < 3; axis++ {
			minBound[axis] = minf(minBound[axis], local[axis])
			maxBound[axis] = maxf(maxBound[axis], local[axis])
		}
	}

	return minBound, maxBound
}

func vec3MaxDiff(a, b mgl32.Vec3) float32 {
	diff := float32(0)
	for i := 0; i < 3; i++ {
		axisDiff := absf(a[i] - b[i])
		if axisDiff > diff {
			diff = axisDiff
		}
	}
	return diff
}

func TestTransformedOverlapBoundsIncludesAllRotatedCorners(t *testing.T) {
	overlapMin := mgl32.Vec3{0, 0, 0}
	overlapMax := mgl32.Vec3{2, 1, 3}
	bodyPos := mgl32.Vec3{}
	center := mgl32.Vec3{1, 0.5, 1.5}
	invRot := mgl32.QuatRotate(float32(5*math.Pi/180), mgl32.Vec3{0, 1, 0}).Inverse()

	gotMin, gotMax := transformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, 1.0)
	wantMin, wantMax := bruteForceTransformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, 1.0)
	naiveMin, _ := naiveTransformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, 1.0)

	if diff := vec3MaxDiff(gotMin, wantMin); diff > 1e-5 {
		t.Fatalf("expected transformed min bound %v, got %v (diff %.6f)", wantMin, gotMin, diff)
	}
	if diff := vec3MaxDiff(gotMax, wantMax); diff > 1e-5 {
		t.Fatalf("expected transformed max bound %v, got %v (diff %.6f)", wantMax, gotMax, diff)
	}
	if naiveMin.X()-wantMin.X() <= 0.2 {
		t.Fatalf("expected regression scenario to expose the old two-corner underbound, got naive min %v and full min %v", naiveMin, wantMin)
	}
}
