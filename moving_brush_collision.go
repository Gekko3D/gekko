package gekko

import (
	"math"
	"reflect"

	"github.com/go-gl/mathgl/mgl32"
)

// MovingBrushCollisionQuery exposes all runtime moving-brush bounds to the
// shared character-controller collision path.
func MovingBrushCollisionQuery(cmd *Commands) CharacterCollisionQuery {
	if environment := activeGroundedCharacterEnvironment(cmd); environment != nil {
		if len(environment.movingBrushes) == 0 {
			return nil
		}
		return environment.movingBrushQuery
	}
	if cmd == nil {
		return nil
	}
	return func(origin, dir mgl32.Vec3, maxDistance float32, acceptEntity func(EntityId, bool) bool) RaycastHit {
		if maxDistance <= 0 {
			return RaycastHit{}
		}
		best := RaycastHit{}
		MakeQuery2[TransformComponent, MovingBrushComponent](cmd).Map(func(eid EntityId, _ *TransformComponent, brush *MovingBrushComponent) bool {
			if brush == nil || (acceptEntity != nil && !acceptEntity(eid, true)) {
				return true
			}
			t, normal, ok := rayAABBHit(origin, dir, brush.BoundsCenter.Sub(brush.BoundsHalfExtents), brush.BoundsCenter.Add(brush.BoundsHalfExtents), maxDistance)
			if !ok || (best.Hit && best.T <= t) {
				return true
			}
			best = RaycastHit{Hit: true, T: t, Normal: normal, Entity: eid}
			return true
		})
		return best
	}
}

type movingBrushCollisionBounds struct {
	Entity EntityId
	Min    mgl32.Vec3
	Max    mgl32.Vec3
}

func collectMovingBrushCollisionBounds(cmd *Commands, bounds []movingBrushCollisionBounds) []movingBrushCollisionBounds {
	bounds = bounds[:0]
	if cmd == nil {
		return bounds
	}
	MakeQuery2[TransformComponent, MovingBrushComponent](cmd).Map(func(eid EntityId, _ *TransformComponent, brush *MovingBrushComponent) bool {
		if brush != nil {
			bounds = append(bounds, movingBrushCollisionBounds{
				Entity: eid,
				Min:    brush.BoundsCenter.Sub(brush.BoundsHalfExtents),
				Max:    brush.BoundsCenter.Add(brush.BoundsHalfExtents),
			})
		}
		return true
	})
	return bounds
}

func raycastMovingBrushCollisionBounds(bounds []movingBrushCollisionBounds, origin, dir mgl32.Vec3, maxDistance float32, acceptEntity func(EntityId, bool) bool) RaycastHit {
	if maxDistance <= 0 {
		return RaycastHit{}
	}
	best := RaycastHit{}
	for _, brush := range bounds {
		if acceptEntity != nil && !acceptEntity(brush.Entity, true) {
			continue
		}
		t, normal, ok := rayAABBHit(origin, dir, brush.Min, brush.Max, maxDistance)
		if !ok || (best.Hit && best.T <= t) {
			continue
		}
		best = RaycastHit{Hit: true, T: t, Normal: normal, Entity: brush.Entity}
	}
	return best
}

func rayAABBHit(origin, dir, minB, maxB mgl32.Vec3, maxDistance float32) (float32, mgl32.Vec3, bool) {
	if dir.LenSqr() <= 1e-8 || maxDistance <= 0 {
		return 0, mgl32.Vec3{}, false
	}
	dir = dir.Normalize()
	tMin, tMax := float32(0), maxDistance
	normal := mgl32.Vec3{}
	for axis := 0; axis < 3; axis++ {
		if math.Abs(float64(dir[axis])) < 1e-6 {
			if origin[axis] < minB[axis] || origin[axis] > maxB[axis] {
				return 0, mgl32.Vec3{}, false
			}
			continue
		}
		t0 := (minB[axis] - origin[axis]) / dir[axis]
		t1 := (maxB[axis] - origin[axis]) / dir[axis]
		nearNormal := mgl32.Vec3{}
		nearNormal[axis] = -1
		if t0 > t1 {
			t0, t1 = t1, t0
			nearNormal[axis] = 1
		}
		if t0 >= tMin {
			tMin = t0
			normal = nearNormal
		}
		tMax = minf(tMax, t1)
		if tMax < tMin {
			return 0, mgl32.Vec3{}, false
		}
	}
	if normal.LenSqr() <= 1e-8 {
		normal = dir.Mul(-1)
	}
	return tMin, normal, true
}

func movingBrushDoorObstructed(cmd *Commands, previous, next *MovingBrushComponent) bool {
	if cmd == nil || previous == nil || next == nil ||
		previous.BoundsCenter == next.BoundsCenter && previous.BoundsHalfExtents == next.BoundsHalfExtents &&
			(previous.MotionKind != "rotate" || previous.CurrentAngle == next.CurrentAngle) {
		return false
	}
	delta := next.BoundsCenter.Sub(previous.BoundsCenter)
	blocked := false
	MakeQuery2[TransformComponent, GroundedCharacterMotorComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, ctrl *GroundedCharacterMotorComponent) bool {
		if tr == nil || ctrl == nil {
			return true
		}
		cam, _ := cmd.GetComponent(eid, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
		base := groundedPlayerBasePosition(cmd, eid, cam, ctrl)
		radius := defaulted(ctrl.Radius, 0.35)
		height := defaulted(ctrl.Height, 1.8)
		characterMin := base.Add(mgl32.Vec3{-radius, 0, -radius})
		characterMax := base.Add(mgl32.Vec3{radius, height, radius})
		previousDepth, previousOverlap := movingBrushAABBOverlapDepth(
			previous.BoundsCenter.Sub(previous.BoundsHalfExtents), previous.BoundsCenter.Add(previous.BoundsHalfExtents), characterMin, characterMax,
		)
		if previous.MotionKind == "rotate" {
			angleDelta := next.CurrentAngle - previous.CurrentAngle
			if angleDelta > 360 {
				angleDelta = 360
			} else if angleDelta < -360 {
				angleDelta = -360
			}
			steps := min(72, max(1, int(math.Ceil(math.Abs(float64(angleDelta))/5))))
			for step := 1; step <= steps && !blocked; step++ {
				angle := previous.CurrentAngle + angleDelta*float32(step)/float32(steps)
				center, halfExtents := movingBrushRotatedBounds(next, angle)
				depth, overlap := movingBrushAABBOverlapDepth(center.Sub(halfExtents), center.Add(halfExtents), characterMin, characterMax)
				blocked = overlap && (!previousOverlap || depth > previousDepth+0.005)
			}
			return !blocked
		}
		nextDepth, nextOverlap := movingBrushAABBOverlapDepth(
			next.BoundsCenter.Sub(next.BoundsHalfExtents), next.BoundsCenter.Add(next.BoundsHalfExtents), characterMin, characterMax,
		)
		if previousOverlap {
			blocked = nextOverlap && nextDepth > previousDepth+0.005
		} else if nextOverlap {
			blocked = true
		} else if delta.LenSqr() > 1e-8 {
			_, _, blocked = rayAABBHit(
				previous.BoundsCenter, delta,
				characterMin.Sub(previous.BoundsHalfExtents), characterMax.Add(previous.BoundsHalfExtents), delta.Len(),
			)
		}
		return !blocked
	})
	return blocked
}

func movingBrushAABBOverlapDepth(aMin, aMax, bMin, bMax mgl32.Vec3) (float32, bool) {
	depth := float32(math.MaxFloat32)
	for axis := 0; axis < 3; axis++ {
		overlap := minf(aMax[axis], bMax[axis]) - maxf(aMin[axis], bMin[axis])
		if overlap < 0 {
			return 0, false
		}
		depth = minf(depth, overlap)
	}
	return depth, true
}

func movingBrushRidersCanMove(cmd *Commands, brush EntityId, delta mgl32.Vec3) bool {
	if cmd == nil || delta.LenSqr() <= 1e-8 {
		return true
	}
	canMove := true
	MakeQuery2[TransformComponent, GroundedCharacterMotorComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, ctrl *GroundedCharacterMotorComponent) bool {
		if tr == nil || !movingBrushCarriesGroundedCharacter(ctrl, brush) {
			return true
		}
		cam, _ := cmd.GetComponent(eid, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
		base := groundedPlayerBasePosition(cmd, eid, cam, ctrl)
		_, canMove = movingBrushCarriedPosition(cmd, brush, base, delta, ctrl)
		return canMove
	})
	return canMove
}

func movingBrushCarriesGroundedCharacter(ctrl *GroundedCharacterMotorComponent, brush EntityId) bool {
	if ctrl == nil || brush == 0 || !ctrl.Grounded {
		return false
	}
	carried := false
	count := min(ctrl.GroundContactCount, len(ctrl.GroundContacts))
	for index := 0; index < count; index++ {
		if ctrl.GroundContacts[index].Entity != brush {
			return false
		}
		carried = true
	}
	return carried
}

func movingBrushCarriedPosition(cmd *Commands, brush EntityId, base, delta mgl32.Vec3, ctrl *GroundedCharacterMotorComponent) (mgl32.Vec3, bool) {
	if ctrl == nil {
		return base, false
	}
	filter := groundedPlayerCollisionRaycastFilter(cmd, ctrl)
	acceptEntity := func(entity EntityId, known bool) bool {
		return (!known || entity != brush) && (filter == nil || filter(entity, known))
	}
	collision := groundedPlayerCharacterCollisionConfig(cmd, ctrl)
	var voxels *VoxelRtState
	if cmd != nil && cmd.app != nil {
		environment, _ := cmd.app.resources[reflect.TypeOf(groundedCharacterEnvironmentState{})].(*groundedCharacterEnvironmentState)
		if environment != nil {
			voxels = environment.voxelRuntime
		}
	}
	position := base
	horizontal := delta
	horizontal[1] = 0
	if horizontal.LenSqr() > 1e-8 {
		move := CharacterKinematicMove(voxels, position, horizontal, CharacterKinematicMoveOptions{
			CollisionConfig: collision, AcceptEntity: acceptEntity, DisableDepenetration: true, DisableSlide: true,
		})
		if move.Blocked || move.AppliedMove.Sub(horizontal).LenSqr() > 1e-6 {
			return base, false
		}
		position = move.Position
	}
	if absf(delta.Y()) > 1e-5 {
		moved, blocked := CharacterVerticalMove(voxels, position, delta.Y(), collision, acceptEntity)
		targetY := position.Y() + delta.Y()
		if blocked || absf(moved.Y()-targetY) > 1e-4 {
			target := position
			target[1] = targetY
			ground := groundedPlayerGroundProbeConfig(ctrl)
			ground.DynamicCollisionQuery = collision.DynamicCollisionQuery
			hit, landed := CharacterGroundHitAtWithin(voxels, target, ground, 0.05, 0.05, acceptEntity)
			if delta.Y() >= 0 || absf(moved.Y()-targetY) > 0.05 || !landed || absf(hit.Y-targetY) > 0.05 ||
				!CharacterHasStandingClearance(voxels, target, collision, acceptEntity) {
				return base, false
			}
			moved = target
		}
		position = moved
	}
	return position, true
}

func moveMovingBrushRiders(cmd *Commands, brush EntityId, previousCenter, previousHalfExtents, delta mgl32.Vec3) {
	if cmd == nil || delta.LenSqr() <= 1e-8 {
		return
	}
	MakeQuery2[TransformComponent, GroundedCharacterMotorComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, ctrl *GroundedCharacterMotorComponent) bool {
		if tr == nil || !movingBrushCarriesGroundedCharacter(ctrl, brush) {
			return true
		}
		cam, _ := cmd.GetComponent(eid, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
		ctrl.Grounded = true
		base := groundedPlayerBasePosition(cmd, eid, cam, ctrl)
		moved, ok := movingBrushCarriedPosition(cmd, brush, base, delta, ctrl)
		if !ok {
			return true
		}
		point := movingBrushSupportPoint(previousCenter.Add(delta), previousHalfExtents, moved)
		hit := CharacterGroundHit{Point: point, Y: point.Y(), Normal: mgl32.Vec3{0, 1, 0}, ContactCount: 1}
		hit.Contacts[0] = CharacterGroundContact{Point: point, Normal: hit.Normal, Entity: brush}
		groundedPlayerSetGroundSupport(ctrl, hit)
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = 0
		groundedPlayerApplyTransform(cmd, eid, cam, ctrl, moved)
		return true
	})
}

func movingBrushSupportPoint(center, halfExtents, basePos mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{
		maxf(center.X()-halfExtents.X(), minf(center.X()+halfExtents.X(), basePos.X())),
		center.Y() + halfExtents.Y(),
		maxf(center.Z()-halfExtents.Z(), minf(center.Z()+halfExtents.Z(), basePos.Z())),
	}
}
