package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func naiveTransformedOverlapBounds(overlapMin, overlapMax, bodyPos mgl32.Vec3, invRot mgl32.Quat, center mgl32.Vec3, voxelScale mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	localCorner1 := vec3DivComponents(invRot.Rotate(overlapMin.Sub(bodyPos)).Add(center), voxelScale)
	localCorner2 := vec3DivComponents(invRot.Rotate(overlapMax.Sub(bodyPos)).Add(center), voxelScale)

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

func bruteForceTransformedOverlapBounds(overlapMin, overlapMax, bodyPos mgl32.Vec3, invRot mgl32.Quat, center mgl32.Vec3, voxelScale mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
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

		local := vec3DivComponents(invRot.Rotate(corner.Sub(bodyPos)).Add(center), voxelScale)
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
	voxelScale := mgl32.Vec3{1, 1, 1}

	gotMin, gotMax := transformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, voxelScale)
	wantMin, wantMax := bruteForceTransformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, voxelScale)
	naiveMin, _ := naiveTransformedOverlapBounds(overlapMin, overlapMax, bodyPos, invRot, center, voxelScale)

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

func TestVoxelCollisionHonorsNonUniformVoxelScale(t *testing.T) {
	voxelScale := mgl32.Vec3{0.2, 0.1, 0.1}
	voxelGrid := &voxelGridSnapshot{
		xbm:        nil,
		vSize:      voxelScale.X(),
		voxelScale: voxelScale,
		cachedMin:  mgl32.Vec3{0, 0, 0},
		cachedMax:  mgl32.Vec3{1, 1, 1},
	}
	voxelGrid.xbm = nil

	// One occupied voxel at grid (0,0,0).
	xbmBody := makeVoxelGridSnapshot([][3]int{{0, 0, 0}}, voxelScale.X())
	xbmBody.voxelScale = voxelScale
	voxelGrid = xbmBody

	voxelCenter := voxelScale.Mul(0.5)
	voxelBody := &internalBody{
		Eid:      2,
		pos:      voxelCenter,
		rot:      mgl32.QuatIdent(),
		isStatic: true,
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: voxelCenter,
			},
		}},
		model: PhysicsModel{
			CenterOffset: voxelCenter,
			Boxes: []CollisionBox{{
				HalfExtents: voxelCenter,
			}},
			Grid: voxelGrid,
		},
	}

	boxBody := &internalBody{
		Eid: 1,
		pos: mgl32.Vec3{0.175, 0.05, 0.05},
		rot: mgl32.QuatIdent(),
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: mgl32.Vec3{0.03, 0.03, 0.03},
			},
		}},
		model: PhysicsModel{
			Boxes: []CollisionBox{{
				HalfExtents: mgl32.Vec3{0.03, 0.03, 0.03},
			}},
		},
	}

	voxelBody.updateAABB()
	boxBody.updateAABB()

	contacts, handled := checkVoxelCollision(boxBody, voxelBody, 0.01)
	if !handled {
		t.Fatal("expected voxel path to handle non-uniform voxel grid collision")
	}
	if len(contacts) == 0 {
		t.Fatal("expected collision that depends on stretched X voxel scale to be detected")
	}
}

func TestVoxelCollisionHandlesLargeOverlapWithoutFallback(t *testing.T) {
	voxelScale := mgl32.Vec3{VoxelSize, VoxelSize, VoxelSize}
	grid := testSolidGrid{
		size:   [3]int{40, 2, 40},
		vScale: voxelScale,
	}
	center := mgl32.Vec3{
		float32(grid.size[0]) * voxelScale.X() * 0.5,
		float32(grid.size[1]) * voxelScale.Y() * 0.5,
		float32(grid.size[2]) * voxelScale.Z() * 0.5,
	}

	bodyA := &internalBody{
		Eid: 1,
		pos: center,
		rot: mgl32.QuatIdent(),
		model: PhysicsModel{
			CenterOffset: center,
			Boxes: []CollisionBox{{
				HalfExtents: center,
			}},
			Grid: grid,
		},
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: center,
			},
		}},
	}
	bodyB := &internalBody{
		Eid: 2,
		pos: center.Add(mgl32.Vec3{0, voxelScale.Y() * 0.25, 0}),
		rot: mgl32.QuatIdent(),
		model: PhysicsModel{
			CenterOffset: center,
			Boxes: []CollisionBox{{
				HalfExtents: center,
			}},
			Grid: grid,
		},
		boxes: []InternalBox{{
			Box: CollisionBox{
				HalfExtents: center,
			},
		}},
	}

	bodyA.updateAABB()
	bodyB.updateAABB()

	contacts, handled := checkVoxelCollision(bodyA, bodyB, 0.01)
	if !handled {
		t.Fatal("expected large voxel overlap to stay on voxel path instead of falling back")
	}
	if len(contacts) == 0 {
		t.Fatal("expected large voxel overlap to produce contacts")
	}
}
