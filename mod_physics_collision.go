package gekko

import (
	"math"
	"sort"

	rootphysics "github.com/gekko3d/gekko/physics"
	"github.com/go-gl/mathgl/mgl32"
)

type voxelCollisionContact struct {
	normal      mgl32.Vec3
	penetration float32
	point       mgl32.Vec3
}

type voxelPrimitiveRangeIterator interface {
	ForEachPrimitiveInRange(minX, minY, minZ, maxX, maxY, maxZ int, fn func(localCenter, halfExtents mgl32.Vec3) bool) bool
}

func checkSingleOBBCollision(posA mgl32.Vec3, rotA mgl32.Quat, boxA CollisionBox, posB mgl32.Vec3, rotB mgl32.Quat, boxB CollisionBox, pointInOBBEpsilon float32) (bool, mgl32.Vec3, float32, mgl32.Vec3) {
	worldPosA := posA.Add(rotA.Rotate(boxA.LocalOffset))
	worldPosB := posB.Add(rotB.Rotate(boxB.LocalOffset))

	matA := rotA.Mat4()
	matB := rotB.Mat4()

	axesA := [3]mgl32.Vec3{matA.Col(0).Vec3(), matA.Col(1).Vec3(), matA.Col(2).Vec3()}
	axesB := [3]mgl32.Vec3{matB.Col(0).Vec3(), matB.Col(1).Vec3(), matB.Col(2).Vec3()}

	L := worldPosB.Sub(worldPosA)
	minOverlap := float32(math.MaxFloat32)
	var collisionNormal mgl32.Vec3

	var testAxes [15]mgl32.Vec3
	axisCount := 0
	for i := 0; i < 3; i++ {
		testAxes[axisCount] = axesA[i]
		axisCount++
		testAxes[axisCount] = axesB[i]
		axisCount++
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			cross := axesA[i].Cross(axesB[j])
			if cross.LenSqr() > 1e-4 {
				testAxes[axisCount] = cross.Normalize()
				axisCount++
			}
		}
	}

	for _, axis := range testAxes[:axisCount] {
		projectionA := float32(0)
		for i := 0; i < 3; i++ {
			projectionA += absf(axesA[i].Dot(axis)) * boxA.HalfExtents[i]
		}
		projectionB := float32(0)
		for i := 0; i < 3; i++ {
			projectionB += absf(axesB[i].Dot(axis)) * boxB.HalfExtents[i]
		}
		distance := absf(L.Dot(axis))
		overlap := projectionA + projectionB - distance
		if overlap <= 0 {
			return false, mgl32.Vec3{}, 0, mgl32.Vec3{}
		}
		if overlap < minOverlap {
			minOverlap = overlap
			collisionNormal = axis
		}
	}

	if L.Dot(collisionNormal) > 0 {
		collisionNormal = collisionNormal.Mul(-1)
	}

	// Contact point
	cornersA := getCorners(worldPosA, axesA, boxA.HalfExtents)
	cornersB := getCorners(worldPosB, axesB, boxB.HalfExtents)
	var contactPoints [16]mgl32.Vec3
	contactCount := 0
	for _, p := range cornersA {
		if isPointInOBB(p, worldPosB, axesB, boxB.HalfExtents, pointInOBBEpsilon) {
			contactPoints[contactCount] = p
			contactCount++
		}
	}
	for _, p := range cornersB {
		if isPointInOBB(p, worldPosA, axesA, boxA.HalfExtents, pointInOBBEpsilon) {
			contactPoints[contactCount] = p
			contactCount++
		}
	}

	var cp mgl32.Vec3
	if contactCount == 0 {
		cp = worldPosA.Add(worldPosB).Mul(0.5)
	} else {
		for _, p := range contactPoints[:contactCount] {
			cp = cp.Add(p)
		}
		cp = cp.Mul(1.0 / float32(contactCount))
	}

	return true, collisionNormal, minOverlap, cp
}

func checkVoxelCollision(bodyA *internalBody, bodyB *internalBody, pointInOBBEpsilon float32) ([]voxelCollisionContact, bool) {
	// Ensure bodyB is the one with the grid if only one has it.
	swapped := false
	if bodyB.model.Grid == nil && bodyA.model.Grid != nil {
		bodyA, bodyB = bodyB, bodyA
		swapped = true
	}

	gridB := bodyB.model.Grid
	if gridB == nil {
		return nil, false
	}

	// Overlap in world space
	overlapMin := mgl32.Vec3{
		maxf(bodyA.aabbMin.X(), bodyB.aabbMin.X()),
		maxf(bodyA.aabbMin.Y(), bodyB.aabbMin.Y()),
		maxf(bodyA.aabbMin.Z(), bodyB.aabbMin.Z()),
	}
	overlapMax := mgl32.Vec3{
		minf(bodyA.aabbMax.X(), bodyB.aabbMax.X()),
		minf(bodyA.aabbMax.Y(), bodyB.aabbMax.Y()),
		minf(bodyA.aabbMax.Z(), bodyB.aabbMax.Z()),
	}

	if overlapMin.X() >= overlapMax.X() || overlapMin.Y() >= overlapMax.Y() || overlapMin.Z() >= overlapMax.Z() {
		return nil, true
	}

	voxelScaleB := gridB.VoxelScale()
	invRotB := bodyB.rot.Inverse()
	centerB := bodyB.model.CenterOffset
	voxelBoxB := CollisionBox{
		HalfExtents: voxelScaleB.Mul(0.5),
	}

	// Transform the full overlap box into B's voxel grid coordinates.
	lMin, lMax := transformedOverlapBounds(overlapMin, overlapMax, bodyB.pos, invRotB, centerB, voxelScaleB)

	minX, minY, minZ := int(math.Floor(float64(lMin.X()))), int(math.Floor(float64(lMin.Y()))), int(math.Floor(float64(lMin.Z())))
	maxX, maxY, maxZ := int(math.Ceil(float64(lMax.X()))), int(math.Ceil(float64(lMax.Y()))), int(math.Ceil(float64(lMax.Z())))

	gridA := bodyA.model.Grid
	invRotA := bodyA.rot.Inverse()
	centerA := bodyA.model.CenterOffset
	contacts := make([]voxelCollisionContact, 0, 16)

	boxesA := bodyA.boxes
	if len(boxesA) == 0 && len(bodyA.model.Boxes) > 0 {
		boxesA = make([]InternalBox, len(bodyA.model.Boxes))
		for i, box := range bodyA.model.Boxes {
			boxesA[i].Box = box
		}
	}

	handled := forEachVoxelPrimitiveInRange(gridB, minX, minY, minZ, maxX, maxY, maxZ, func(localCenterB, halfExtentsB mgl32.Vec3) bool {
		worldPosB := bodyB.rot.Rotate(localCenterB.Sub(centerB)).Add(bodyB.pos)
		voxelBoxB.HalfExtents = halfExtentsB

		if gridA != nil {
			voxelScaleA := gridA.VoxelScale()
			voxelBoxA := CollisionBox{
				HalfExtents: voxelScaleA.Mul(0.5),
			}
			halfExtentsInALocal := transformedCubeHalfExtentsInLocal(invRotA, bodyB.rot, halfExtentsB)
			localA := vec3DivComponents(invRotA.Rotate(worldPosB.Sub(bodyA.pos)).Add(centerA), voxelScaleA)
			voxelHalfExtentsInAGrid := vec3DivComponents(halfExtentsInALocal, voxelScaleA)
			minAX := int(math.Floor(float64(localA.X() - voxelHalfExtentsInAGrid.X())))
			minAY := int(math.Floor(float64(localA.Y() - voxelHalfExtentsInAGrid.Y())))
			minAZ := int(math.Floor(float64(localA.Z() - voxelHalfExtentsInAGrid.Z())))
			maxAX := int(math.Ceil(float64(localA.X() + voxelHalfExtentsInAGrid.X())))
			maxAY := int(math.Ceil(float64(localA.Y() + voxelHalfExtentsInAGrid.Y())))
			maxAZ := int(math.Ceil(float64(localA.Z() + voxelHalfExtentsInAGrid.Z())))
			for az := minAZ; az < maxAZ; az++ {
				for ay := minAY; ay < maxAY; ay++ {
					for ax := minAX; ax < maxAX; ax++ {
						if foundA, _ := gridA.GetVoxel(ax, ay, az); !foundA {
							continue
						}

						gridPosA := vec3MulComponents(mgl32.Vec3{float32(ax) + 0.5, float32(ay) + 0.5, float32(az) + 0.5}, voxelScaleA)
						worldPosA := bodyA.rot.Rotate(gridPosA.Sub(centerA)).Add(bodyA.pos)
						if collision, normal, penetration, point := checkSingleOBBCollision(worldPosA, bodyA.rot, voxelBoxA, worldPosB, bodyB.rot, voxelBoxB, pointInOBBEpsilon); collision {
							if swapped {
								normal = normal.Mul(-1)
							}
							contacts = append(contacts, voxelCollisionContact{
								normal:      normal,
								penetration: penetration,
								point:       point,
							})
						}
					}
				}
			}
			return true
		}

		halfExtentsInALocal := transformedCubeHalfExtentsInLocal(invRotA, bodyB.rot, halfExtentsB)
		for _, boxA := range boxesA {
			localA := invRotA.Rotate(worldPosB.Sub(bodyA.pos))
			relA := localA.Sub(boxA.Box.LocalOffset)
			if absf(relA.X()) > boxA.Box.HalfExtents.X()+halfExtentsInALocal.X() ||
				absf(relA.Y()) > boxA.Box.HalfExtents.Y()+halfExtentsInALocal.Y() ||
				absf(relA.Z()) > boxA.Box.HalfExtents.Z()+halfExtentsInALocal.Z() {
				continue
			}
			if collision, normal, penetration, point := checkSingleOBBCollision(bodyA.pos, bodyA.rot, boxA.Box, worldPosB, bodyB.rot, voxelBoxB, pointInOBBEpsilon); collision {
				if swapped {
					normal = normal.Mul(-1)
				}
				contacts = append(contacts, voxelCollisionContact{
					normal:      normal,
					penetration: penetration,
					point:       point,
				})
			}
		}
		return true
	})
	if !handled {
		return nil, false
	}

	if len(contacts) == 0 {
		return nil, true
	}

	return reduceVoxelContacts(contacts, 6), true
}

func forEachVoxelPrimitiveInRange(grid rootphysics.VoxelGrid, minX, minY, minZ, maxX, maxY, maxZ int, fn func(localCenter, halfExtents mgl32.Vec3) bool) bool {
	if iterator, ok := grid.(voxelPrimitiveRangeIterator); ok {
		return iterator.ForEachPrimitiveInRange(minX, minY, minZ, maxX, maxY, maxZ, fn)
	}

	if (maxX-minX) > 32 || (maxY-minY) > 32 || (maxZ-minZ) > 32 {
		return false
	}

	voxelScale := grid.VoxelScale()
	voxelHalfExtents := voxelScale.Mul(0.5)
	for vz := minZ; vz < maxZ; vz++ {
		for vy := minY; vy < maxY; vy++ {
			for vx := minX; vx < maxX; vx++ {
				if found, _ := grid.GetVoxel(vx, vy, vz); !found {
					continue
				}
				localCenter := vec3MulComponents(mgl32.Vec3{float32(vx) + 0.5, float32(vy) + 0.5, float32(vz) + 0.5}, voxelScale)
				if !fn(localCenter, voxelHalfExtents) {
					return true
				}
			}
		}
	}

	return true
}

func emitVoxelPrimitiveRange(minX, minY, minZ, maxX, maxY, maxZ int, voxelScale mgl32.Vec3, fn func(localCenter, halfExtents mgl32.Vec3) bool) bool {
	if minX >= maxX || minY >= maxY || minZ >= maxZ {
		return true
	}

	sizeInCells := mgl32.Vec3{
		float32(maxX - minX),
		float32(maxY - minY),
		float32(maxZ - minZ),
	}
	localCenter := vec3MulComponents(mgl32.Vec3{
		float32(minX) + sizeInCells.X()*0.5,
		float32(minY) + sizeInCells.Y()*0.5,
		float32(minZ) + sizeInCells.Z()*0.5,
	}, voxelScale)
	halfExtents := vec3MulComponents(sizeInCells, voxelScale).Mul(0.5)

	return fn(localCenter, halfExtents)
}

func transformedOverlapBounds(overlapMin, overlapMax, bodyPos mgl32.Vec3, invRot mgl32.Quat, center mgl32.Vec3, voxelScale mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	lMin := mgl32.Vec3{float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)}
	lMax := mgl32.Vec3{-float32(math.MaxFloat32), -float32(math.MaxFloat32), -float32(math.MaxFloat32)}

	for mask := 0; mask < 8; mask++ {
		corner := mgl32.Vec3{
			overlapMin.X(),
			overlapMin.Y(),
			overlapMin.Z(),
		}
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
			lMin[axis] = minf(lMin[axis], local[axis])
			lMax[axis] = maxf(lMax[axis], local[axis])
		}
	}

	return lMin, lMax
}

func transformedCubeHalfExtentsInLocal(targetInvRot mgl32.Quat, sourceRot mgl32.Quat, halfExtents mgl32.Vec3) mgl32.Vec3 {
	relAxes := [3]mgl32.Vec3{
		targetInvRot.Rotate(sourceRot.Rotate(mgl32.Vec3{1, 0, 0})),
		targetInvRot.Rotate(sourceRot.Rotate(mgl32.Vec3{0, 1, 0})),
		targetInvRot.Rotate(sourceRot.Rotate(mgl32.Vec3{0, 0, 1})),
	}

	return mgl32.Vec3{
		absf(relAxes[0].X())*halfExtents.X() + absf(relAxes[1].X())*halfExtents.Y() + absf(relAxes[2].X())*halfExtents.Z(),
		absf(relAxes[0].Y())*halfExtents.X() + absf(relAxes[1].Y())*halfExtents.Y() + absf(relAxes[2].Y())*halfExtents.Z(),
		absf(relAxes[0].Z())*halfExtents.X() + absf(relAxes[1].Z())*halfExtents.Y() + absf(relAxes[2].Z())*halfExtents.Z(),
	}
}

func reduceVoxelContacts(contacts []voxelCollisionContact, maxContacts int) []voxelCollisionContact {
	if len(contacts) <= maxContacts {
		return contacts
	}

	sort.Slice(contacts, func(i, j int) bool {
		if contacts[i].penetration == contacts[j].penetration {
			return contacts[i].point.LenSqr() < contacts[j].point.LenSqr()
		}
		return contacts[i].penetration > contacts[j].penetration
	})

	selected := make([]voxelCollisionContact, 0, maxContacts)
	used := make([]bool, len(contacts))
	selected = append(selected, contacts[0])
	used[0] = true

	for len(selected) < maxContacts {
		bestIdx := -1
		bestScore := float32(-1)
		for i, contact := range contacts {
			if used[i] {
				continue
			}
			minDistSq := float32(math.MaxFloat32)
			for _, selectedContact := range selected {
				distSq := contact.point.Sub(selectedContact.point).LenSqr()
				if distSq < minDistSq {
					minDistSq = distSq
				}
			}
			score := minDistSq + contact.penetration*contact.penetration
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, contacts[bestIdx])
	}

	return selected
}

func getCorners(pos mgl32.Vec3, axes [3]mgl32.Vec3, halfExtents mgl32.Vec3) [8]mgl32.Vec3 {
	var corners [8]mgl32.Vec3
	for i := 0; i < 8; i++ {
		p := pos
		if i&1 != 0 {
			p = p.Add(axes[0].Mul(halfExtents.X()))
		} else {
			p = p.Sub(axes[0].Mul(halfExtents.X()))
		}
		if i&2 != 0 {
			p = p.Add(axes[1].Mul(halfExtents.Y()))
		} else {
			p = p.Sub(axes[1].Mul(halfExtents.Y()))
		}
		if i&4 != 0 {
			p = p.Add(axes[2].Mul(halfExtents.Z()))
		} else {
			p = p.Sub(axes[2].Mul(halfExtents.Z()))
		}
		corners[i] = p
	}
	return corners
}

func isPointInOBB(p, pos mgl32.Vec3, axes [3]mgl32.Vec3, halfExtents mgl32.Vec3, epsilon float32) bool {
	d := p.Sub(pos)
	for i := 0; i < 3; i++ {
		dist := absf(d.Dot(axes[i]))
		if dist > halfExtents[i]+epsilon {
			return false
		}
	}
	return true
}
