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

type narrowPhaseContact struct {
	normal      mgl32.Vec3
	penetration float32
	point       mgl32.Vec3
}

type voxelPrimitiveRangeIterator interface {
	ForEachPrimitiveInRange(minX, minY, minZ, maxX, maxY, maxZ int, fn func(localCenter, halfExtents mgl32.Vec3) bool) bool
}

func collectNarrowPhaseContacts(bodyA, bodyB *internalBody, pointInOBBEpsilon float32, contacts []narrowPhaseContact) []narrowPhaseContact {
	if bodyA == nil || bodyB == nil {
		return contacts
	}

	if isPrimitiveVoxelPair(bodyA, bodyB) {
		voxelContacts, handled := checkPrimitiveVoxelCollision(bodyA, bodyB)
		if handled {
			for _, contact := range voxelContacts {
				contacts = append(contacts, narrowPhaseContact{
					normal:      contact.normal,
					penetration: contact.penetration,
					point:       contact.point,
				})
			}
			return contacts
		}
	}

	if bodyA.model.Grid != nil || bodyB.model.Grid != nil {
		voxelContacts, handled := checkVoxelCollision(bodyA, bodyB, pointInOBBEpsilon)
		if handled {
			for _, contact := range voxelContacts {
				contacts = append(contacts, narrowPhaseContact{
					normal:      contact.normal,
					penetration: contact.penetration,
					point:       contact.point,
				})
			}
			return contacts
		}
	}

	if usesPrimitive(bodyA) || usesPrimitive(bodyB) {
		return collectPrimitiveContacts(bodyA, bodyB, contacts)
	}

	for _, boxA := range bodyA.boxes {
		for _, boxB := range bodyB.boxes {
			if collision, normal, penetration, contactPoint := checkSingleOBBCollision(bodyA.pos, bodyA.rot, boxA.Box, bodyB.pos, bodyB.rot, boxB.Box, pointInOBBEpsilon); collision {
				contacts = append(contacts, narrowPhaseContact{
					normal:      normal,
					penetration: penetration,
					point:       contactPoint,
				})
			}
		}
	}
	return contacts
}

type capsulePrimitive struct {
	a      mgl32.Vec3
	b      mgl32.Vec3
	radius float32
}

func usesPrimitive(body *internalBody) bool {
	return body != nil && (body.shape == ShapeSphere || body.shape == ShapeCapsule)
}

func isPrimitiveVoxelPair(bodyA, bodyB *internalBody) bool {
	return validPrimitiveBody(bodyA) && bodyB != nil && bodyB.model.Grid != nil ||
		validPrimitiveBody(bodyB) && bodyA != nil && bodyA.model.Grid != nil
}

func validPrimitiveBody(body *internalBody) bool {
	return validSphereBody(body) || validCapsuleBody(body)
}

func validSphereBody(body *internalBody) bool {
	return body != nil && body.shape == ShapeSphere && body.radius > 0
}

func validCapsuleBody(body *internalBody) bool {
	return body != nil && body.shape == ShapeCapsule && body.radius > 0 && body.capsuleHalfHeight >= 0
}

func capsuleFromBody(body *internalBody) (capsulePrimitive, bool) {
	if !validCapsuleBody(body) {
		return capsulePrimitive{}, false
	}
	a, b := capsuleSegmentEndpoints(body.pos, body.rot, body.capsuleHalfHeight)
	return capsulePrimitive{
		a:      a,
		b:      b,
		radius: body.radius,
	}, true
}

func capsuleSegmentEndpoints(pos mgl32.Vec3, rot mgl32.Quat, halfHeight float32) (mgl32.Vec3, mgl32.Vec3) {
	offset := rot.Rotate(mgl32.Vec3{0, halfHeight, 0})
	return pos.Sub(offset), pos.Add(offset)
}

func vec3Min(a, b mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{minf(a.X(), b.X()), minf(a.Y(), b.Y()), minf(a.Z(), b.Z())}
}

func vec3Max(a, b mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{maxf(a.X(), b.X()), maxf(a.Y(), b.Y()), maxf(a.Z(), b.Z())}
}

func collectPrimitiveContacts(bodyA, bodyB *internalBody, contacts []narrowPhaseContact) []narrowPhaseContact {
	sphereA := bodyA.shape == ShapeSphere && bodyA.radius > 0
	sphereB := bodyB.shape == ShapeSphere && bodyB.radius > 0
	capsuleA, validCapsuleA := capsuleFromBody(bodyA)
	capsuleB, validCapsuleB := capsuleFromBody(bodyB)

	switch {
	case sphereA && sphereB:
		if contact, ok := checkSphereSphereCollision(bodyA.pos, bodyA.radius, bodyB.pos, bodyB.radius); ok {
			contacts = append(contacts, contact)
		}
	case validCapsuleA && sphereB:
		if contact, ok := checkCapsuleSphereCollision(capsuleA, bodyB.pos, bodyB.radius); ok {
			contacts = append(contacts, contact)
		}
	case sphereA && validCapsuleB:
		if contact, ok := checkCapsuleSphereCollision(capsuleB, bodyA.pos, bodyA.radius); ok {
			contact.normal = contact.normal.Mul(-1)
			contacts = append(contacts, contact)
		}
	case validCapsuleA && validCapsuleB:
		if contact, ok := checkCapsuleCapsuleCollision(capsuleA, capsuleB); ok {
			contacts = append(contacts, contact)
		}
	case validCapsuleA:
		for _, boxB := range bodyB.boxes {
			if contact, ok := checkCapsuleOBBCollision(capsuleA, bodyB.pos, bodyB.rot, boxB.Box); ok {
				contacts = append(contacts, contact)
			}
		}
	case validCapsuleB:
		for _, boxA := range bodyA.boxes {
			if contact, ok := checkCapsuleOBBCollision(capsuleB, bodyA.pos, bodyA.rot, boxA.Box); ok {
				contact.normal = contact.normal.Mul(-1)
				contacts = append(contacts, contact)
			}
		}
	case sphereA:
		for _, boxB := range bodyB.boxes {
			if contact, ok := checkSphereOBBCollision(bodyA.pos, bodyA.radius, bodyB.pos, bodyB.rot, boxB.Box); ok {
				contacts = append(contacts, contact)
			}
		}
	case sphereB:
		for _, boxA := range bodyA.boxes {
			if contact, ok := checkSphereOBBCollision(bodyB.pos, bodyB.radius, bodyA.pos, bodyA.rot, boxA.Box); ok {
				contact.normal = contact.normal.Mul(-1)
				contacts = append(contacts, contact)
			}
		}
	}

	return contacts
}

func checkPrimitiveVoxelCollision(bodyA, bodyB *internalBody) ([]voxelCollisionContact, bool) {
	swapped := false
	if !validPrimitiveBody(bodyA) && validPrimitiveBody(bodyB) {
		bodyA, bodyB = bodyB, bodyA
		swapped = true
	}
	if !validPrimitiveBody(bodyA) || bodyB == nil || bodyB.model.Grid == nil {
		return nil, false
	}

	gridB := bodyB.model.Grid
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
	lMin, lMax := transformedOverlapBounds(overlapMin, overlapMax, bodyB.pos, invRotB, centerB, voxelScaleB)

	minX, minY, minZ := int(math.Floor(float64(lMin.X()))), int(math.Floor(float64(lMin.Y()))), int(math.Floor(float64(lMin.Z())))
	maxX, maxY, maxZ := int(math.Ceil(float64(lMax.X()))), int(math.Ceil(float64(lMax.Y()))), int(math.Ceil(float64(lMax.Z())))

	voxelBoxB := CollisionBox{
		HalfExtents: voxelScaleB.Mul(0.5),
	}
	capsuleA, capsuleAOk := capsuleFromBody(bodyA)
	contacts := make([]voxelCollisionContact, 0, 16)
	handled := forEachVoxelPrimitiveInRange(gridB, minX, minY, minZ, maxX, maxY, maxZ, func(localCenterB, halfExtentsB mgl32.Vec3) bool {
		worldPosB := bodyB.rot.Rotate(localCenterB.Sub(centerB)).Add(bodyB.pos)
		voxelBoxB.HalfExtents = halfExtentsB
		var contact narrowPhaseContact
		var ok bool
		if capsuleAOk {
			contact, ok = checkCapsuleOBBCollision(capsuleA, worldPosB, bodyB.rot, voxelBoxB)
		} else {
			contact, ok = checkSphereOBBCollision(bodyA.pos, bodyA.radius, worldPosB, bodyB.rot, voxelBoxB)
		}
		if !ok {
			return true
		}
		if swapped {
			contact.normal = contact.normal.Mul(-1)
		}
		contacts = append(contacts, voxelCollisionContact{
			normal:      contact.normal,
			penetration: contact.penetration,
			point:       contact.point,
		})
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

func checkCapsuleSphereCollision(capsule capsulePrimitive, spherePos mgl32.Vec3, sphereRadius float32) (narrowPhaseContact, bool) {
	if capsule.radius <= 0 || sphereRadius <= 0 {
		return narrowPhaseContact{}, false
	}

	capsulePoint := closestPointOnSegment(spherePos, capsule.a, capsule.b)
	delta := capsulePoint.Sub(spherePos)
	radiusSum := capsule.radius + sphereRadius
	distSqr := delta.LenSqr()
	if distSqr >= radiusSum*radiusSum {
		return narrowPhaseContact{}, false
	}

	dist := float32(math.Sqrt(float64(distSqr)))
	normal := capsuleNormalFallback(capsule, spherePos)
	if dist > 1e-6 {
		normal = delta.Mul(1.0 / dist)
	}
	penetration := radiusSum - dist
	point := capsulePoint.Sub(normal.Mul(capsule.radius - penetration*0.5))
	return narrowPhaseContact{
		normal:      normal,
		penetration: penetration,
		point:       point,
	}, true
}

func checkCapsuleCapsuleCollision(capsuleA, capsuleB capsulePrimitive) (narrowPhaseContact, bool) {
	if capsuleA.radius <= 0 || capsuleB.radius <= 0 {
		return narrowPhaseContact{}, false
	}

	pointA, pointB := closestSegmentSegment(capsuleA.a, capsuleA.b, capsuleB.a, capsuleB.b)
	delta := pointA.Sub(pointB)
	radiusSum := capsuleA.radius + capsuleB.radius
	distSqr := delta.LenSqr()
	if distSqr >= radiusSum*radiusSum {
		return narrowPhaseContact{}, false
	}

	dist := float32(math.Sqrt(float64(distSqr)))
	normal := capsulePairNormalFallback(capsuleA, capsuleB)
	if dist > 1e-6 {
		normal = delta.Mul(1.0 / dist)
	}
	penetration := radiusSum - dist
	point := pointA.Sub(normal.Mul(capsuleA.radius - penetration*0.5))
	return narrowPhaseContact{
		normal:      normal,
		penetration: penetration,
		point:       point,
	}, true
}

func checkCapsuleOBBCollision(capsule capsulePrimitive, boxPos mgl32.Vec3, boxRot mgl32.Quat, box CollisionBox) (narrowPhaseContact, bool) {
	if capsule.radius <= 0 {
		return narrowPhaseContact{}, false
	}

	worldBoxPos := boxPos.Add(boxRot.Rotate(box.LocalOffset))
	invRot := boxRot.Inverse()
	localA := invRot.Rotate(capsule.a.Sub(worldBoxPos))
	localB := invRot.Rotate(capsule.b.Sub(worldBoxPos))
	localCapsulePoint, localBoxPoint := closestSegmentAABB(localA, localB, box.HalfExtents)
	worldCapsulePoint := worldBoxPos.Add(boxRot.Rotate(localCapsulePoint))
	worldBoxPoint := worldBoxPos.Add(boxRot.Rotate(localBoxPoint))
	delta := worldCapsulePoint.Sub(worldBoxPoint)
	distSqr := delta.LenSqr()
	if distSqr >= capsule.radius*capsule.radius {
		return narrowPhaseContact{}, false
	}

	if distSqr > 1e-8 {
		dist := float32(math.Sqrt(float64(distSqr)))
		normal := delta.Mul(1.0 / dist)
		return narrowPhaseContact{
			normal:      normal,
			penetration: capsule.radius - dist,
			point:       worldBoxPoint,
		}, true
	}

	normal, faceDistance := closestOBBInteriorNormal(localCapsulePoint, box.HalfExtents, boxRot)
	return narrowPhaseContact{
		normal:      normal,
		penetration: capsule.radius + faceDistance,
		point:       worldCapsulePoint.Sub(normal.Mul(capsule.radius)),
	}, true
}

func closestPointOnSegment(point, a, b mgl32.Vec3) mgl32.Vec3 {
	ab := b.Sub(a)
	denom := ab.LenSqr()
	if denom <= 1e-8 {
		return a
	}
	t := point.Sub(a).Dot(ab) / denom
	t = clampf(t, 0, 1)
	return a.Add(ab.Mul(t))
}

func closestSegmentSegment(p1, q1, p2, q2 mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	const epsilon = float32(1e-6)
	d1 := q1.Sub(p1)
	d2 := q2.Sub(p2)
	r := p1.Sub(p2)
	a := d1.Dot(d1)
	e := d2.Dot(d2)
	f := d2.Dot(r)

	var s, t float32
	if a <= epsilon && e <= epsilon {
		return p1, p2
	}
	if a <= epsilon {
		s = 0
		t = clampf(f/e, 0, 1)
	} else {
		c := d1.Dot(r)
		if e <= epsilon {
			t = 0
			s = clampf(-c/a, 0, 1)
		} else {
			b := d1.Dot(d2)
			denom := a*e - b*b
			if denom != 0 {
				s = clampf((b*f-c*e)/denom, 0, 1)
			} else {
				s = 0
			}
			t = (b*s + f) / e
			if t < 0 {
				t = 0
				s = clampf(-c/a, 0, 1)
			} else if t > 1 {
				t = 1
				s = clampf((b-c)/a, 0, 1)
			}
		}
	}

	return p1.Add(d1.Mul(s)), p2.Add(d2.Mul(t))
}

func capsuleNormalFallback(capsule capsulePrimitive, otherPoint mgl32.Vec3) mgl32.Vec3 {
	mid := capsule.a.Add(capsule.b).Mul(0.5)
	if delta := mid.Sub(otherPoint); delta.LenSqr() > 1e-8 {
		return delta.Normalize()
	}
	axis := capsule.b.Sub(capsule.a)
	if axis.LenSqr() > 1e-8 {
		n := axis.Cross(mgl32.Vec3{1, 0, 0})
		if n.LenSqr() <= 1e-8 {
			n = axis.Cross(mgl32.Vec3{0, 0, 1})
		}
		if n.LenSqr() > 1e-8 {
			return n.Normalize()
		}
	}
	return mgl32.Vec3{1, 0, 0}
}

func capsulePairNormalFallback(capsuleA, capsuleB capsulePrimitive) mgl32.Vec3 {
	midA := capsuleA.a.Add(capsuleA.b).Mul(0.5)
	midB := capsuleB.a.Add(capsuleB.b).Mul(0.5)
	if delta := midA.Sub(midB); delta.LenSqr() > 1e-8 {
		return delta.Normalize()
	}
	axisA := capsuleA.b.Sub(capsuleA.a)
	axisB := capsuleB.b.Sub(capsuleB.a)
	if cross := axisA.Cross(axisB); cross.LenSqr() > 1e-8 {
		return cross.Normalize()
	}
	return capsuleNormalFallback(capsuleA, midB)
}

func closestSegmentAABB(a, b, halfExtents mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	d := b.Sub(a)
	if d.LenSqr() <= 1e-8 {
		boxPoint := closestPointOnAABB(a, halfExtents)
		return a, boxPoint
	}

	lo := float32(0)
	hi := float32(1)
	for i := 0; i < 32; i++ {
		m1 := lo + (hi-lo)/3
		m2 := hi - (hi-lo)/3
		p1 := a.Add(d.Mul(m1))
		p2 := a.Add(d.Mul(m2))
		if sqDistPointAABB(p1, halfExtents) < sqDistPointAABB(p2, halfExtents) {
			hi = m2
		} else {
			lo = m1
		}
	}
	t := (lo + hi) * 0.5
	segmentPoint := a.Add(d.Mul(t))
	boxPoint := closestPointOnAABB(segmentPoint, halfExtents)
	return segmentPoint, boxPoint
}

func closestPointOnAABB(point, halfExtents mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{
		clampf(point.X(), -halfExtents.X(), halfExtents.X()),
		clampf(point.Y(), -halfExtents.Y(), halfExtents.Y()),
		clampf(point.Z(), -halfExtents.Z(), halfExtents.Z()),
	}
}

func sqDistPointAABB(point, halfExtents mgl32.Vec3) float32 {
	closest := closestPointOnAABB(point, halfExtents)
	return point.Sub(closest).LenSqr()
}

func checkSphereSphereCollision(posA mgl32.Vec3, radiusA float32, posB mgl32.Vec3, radiusB float32) (narrowPhaseContact, bool) {
	if radiusA <= 0 || radiusB <= 0 {
		return narrowPhaseContact{}, false
	}
	delta := posA.Sub(posB)
	radiusSum := radiusA + radiusB
	distSqr := delta.LenSqr()
	if distSqr >= radiusSum*radiusSum {
		return narrowPhaseContact{}, false
	}

	dist := float32(math.Sqrt(float64(distSqr)))
	normal := mgl32.Vec3{1, 0, 0}
	if dist > 1e-6 {
		normal = delta.Mul(1.0 / dist)
	}

	penetration := radiusSum - dist
	point := posA.Sub(normal.Mul(radiusA - penetration*0.5))
	return narrowPhaseContact{
		normal:      normal,
		penetration: penetration,
		point:       point,
	}, true
}

func checkSphereOBBCollision(spherePos mgl32.Vec3, sphereRadius float32, boxPos mgl32.Vec3, boxRot mgl32.Quat, box CollisionBox) (narrowPhaseContact, bool) {
	if sphereRadius <= 0 {
		return narrowPhaseContact{}, false
	}

	worldBoxPos := boxPos.Add(boxRot.Rotate(box.LocalOffset))
	invRot := boxRot.Inverse()
	localSphere := invRot.Rotate(spherePos.Sub(worldBoxPos))
	closestLocal := mgl32.Vec3{
		clampf(localSphere.X(), -box.HalfExtents.X(), box.HalfExtents.X()),
		clampf(localSphere.Y(), -box.HalfExtents.Y(), box.HalfExtents.Y()),
		clampf(localSphere.Z(), -box.HalfExtents.Z(), box.HalfExtents.Z()),
	}
	closestWorld := worldBoxPos.Add(boxRot.Rotate(closestLocal))
	delta := spherePos.Sub(closestWorld)
	distSqr := delta.LenSqr()
	if distSqr >= sphereRadius*sphereRadius {
		return narrowPhaseContact{}, false
	}

	if distSqr > 1e-8 {
		dist := float32(math.Sqrt(float64(distSqr)))
		normal := delta.Mul(1.0 / dist)
		return narrowPhaseContact{
			normal:      normal,
			penetration: sphereRadius - dist,
			point:       closestWorld,
		}, true
	}

	normal, faceDistance := closestOBBInteriorNormal(localSphere, box.HalfExtents, boxRot)
	return narrowPhaseContact{
		normal:      normal,
		penetration: sphereRadius + faceDistance,
		point:       spherePos.Sub(normal.Mul(sphereRadius)),
	}, true
}

func closestOBBInteriorNormal(localPoint, halfExtents mgl32.Vec3, rot mgl32.Quat) (mgl32.Vec3, float32) {
	bestAxis := 0
	bestDistance := halfExtents.X() - absf(localPoint.X())
	for axis := 1; axis < 3; axis++ {
		distance := halfExtents[axis] - absf(localPoint[axis])
		if distance < bestDistance {
			bestAxis = axis
			bestDistance = distance
		}
	}
	if bestDistance < 0 {
		bestDistance = 0
	}

	localNormal := mgl32.Vec3{}
	if localPoint[bestAxis] < 0 {
		localNormal[bestAxis] = -1
	} else {
		localNormal[bestAxis] = 1
	}
	return rot.Rotate(localNormal), bestDistance
}

func clampf(v, minV, maxV float32) float32 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
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

	dominantNormal := mgl32.Vec3{}
	deepest := contacts[0]
	for _, contact := range contacts {
		weight := maxf(contact.penetration, 0.001)
		dominantNormal = dominantNormal.Add(contact.normal.Mul(weight))
		if contact.penetration > deepest.penetration {
			deepest = contact
		}
	}
	if dominantNormal.LenSqr() > 1e-6 {
		dominantNormal = dominantNormal.Normalize()
	} else {
		dominantNormal = deepest.normal
	}

	filtered := make([]voxelCollisionContact, 0, len(contacts))
	for _, contact := range contacts {
		if contact.normal.Dot(dominantNormal) >= 0.5 {
			filtered = append(filtered, contact)
		}
	}
	if len(filtered) >= maxContacts {
		contacts = filtered
	} else if len(filtered) > 0 {
		contacts = filtered
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
