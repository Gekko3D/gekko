package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

func checkSingleOBBCollision(posA mgl32.Vec3, rotA mgl32.Quat, boxA CollisionBox, posB mgl32.Vec3, rotB mgl32.Quat, boxB CollisionBox) (bool, mgl32.Vec3, float32, mgl32.Vec3) {
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
		if isPointInOBB(p, worldPosB, axesB, boxB.HalfExtents) {
			contactPoints[contactCount] = p
			contactCount++
		}
	}
	for _, p := range cornersB {
		if isPointInOBB(p, worldPosA, axesA, boxA.HalfExtents) {
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

func isPointInOBB(p, pos mgl32.Vec3, axes [3]mgl32.Vec3, halfExtents mgl32.Vec3) bool {
	d := p.Sub(pos)
	for i := 0; i < 3; i++ {
		dist := absf(d.Dot(axes[i]))
		if dist > halfExtents[i]+0.01 { // Small epsilon
			return false
		}
	}
	return true
}
