package gekko

import (
	"math"
	"reflect"

	"github.com/go-gl/mathgl/mgl32"
)

// MovingBrushCollisionQuery exposes all runtime moving-brush bounds to the
// shared character-controller collision path.
func MovingBrushCollisionQuery(cmd *Commands) CharacterCollisionQuery {
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

func moveMovingBrushRiders(cmd *Commands, previousCenter, previousHalfExtents, delta mgl32.Vec3) {
	if cmd == nil || delta.LenSqr() <= 1e-8 {
		return
	}
	MakeQuery2[TransformComponent, GroundedCharacterMotorComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, ctrl *GroundedCharacterMotorComponent) bool {
		if tr == nil || ctrl == nil || !movingBrushSupportsBase(previousCenter, previousHalfExtents, tr.Position, defaulted(ctrl.Radius, 0.35)) {
			return true
		}
		cam, _ := cmd.GetComponent(eid, reflect.TypeOf(CameraComponent{})).(*CameraComponent)
		ctrl.Grounded = true
		ctrl.NeedsGroundSnap = false
		ctrl.VerticalVelocity = 0
		groundedPlayerApplyTransform(cmd, eid, cam, ctrl, tr.Position.Add(delta))
		return true
	})
	MakeQuery2[TransformComponent, NPCComponent](cmd).Without(GroundedCharacterMotorComponent{}).Map(func(eid EntityId, tr *TransformComponent, _ *NPCComponent) bool {
		if tr == nil || !movingBrushSupportsBase(previousCenter, previousHalfExtents, tr.Position, 0.3) {
			return true
		}
		tr.Position = tr.Position.Add(delta)
		if local, ok := localTransformForEntity(cmd, eid); ok {
			local.Position = tr.Position
		}
		return true
	})
}

func movingBrushSupportsBase(center, halfExtents, basePos mgl32.Vec3, radius float32) bool {
	top := center.Y() + halfExtents.Y()
	if math.Abs(float64(basePos.Y()-top)) > 0.08 {
		return false
	}
	if radius <= 0 {
		radius = 0.3
	}
	return basePos.X()+radius >= center.X()-halfExtents.X() &&
		basePos.X()-radius <= center.X()+halfExtents.X() &&
		basePos.Z()+radius >= center.Z()-halfExtents.Z() &&
		basePos.Z()-radius <= center.Z()+halfExtents.Z()
}
