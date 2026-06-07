package gekko

import "github.com/go-gl/mathgl/mgl32"

type MovingBrushComponent struct {
	Kind               string
	BoundsCenter       mgl32.Vec3
	BoundsHalfExtents  mgl32.Vec3
	MoveDirection      mgl32.Vec3
	ClosedPosition     mgl32.Vec3
	ClosedBoundsCenter mgl32.Vec3
	OpenOffset         mgl32.Vec3
	Speed              float32
	Wait               float32
	Lip                float32
	TargetName         string
	Target             string
	SourceTag          string
	Tags               []string
	Open               bool
	ActivationCount    int
}

func (m *MovingBrushComponent) TargetPosition() mgl32.Vec3 {
	if m == nil {
		return mgl32.Vec3{}
	}
	if m.Open {
		return m.ClosedPosition.Add(m.OpenOffset)
	}
	return m.ClosedPosition
}

func (m *MovingBrushComponent) TargetBoundsCenter() mgl32.Vec3 {
	if m == nil {
		return mgl32.Vec3{}
	}
	if m.Open {
		return m.ClosedBoundsCenter.Add(m.OpenOffset)
	}
	return m.ClosedBoundsCenter
}

type UseTriggerComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	TargetName        string
	Target            string
	SourceTag         string
	Tags              []string
	ActivationCount   int
}

func movingBrushMotionSystem(cmd *Commands, time *Time) {
	if cmd == nil || time == nil || time.Dt <= 0 {
		return
	}
	dt := float32(time.Dt)
	MakeQuery2[TransformComponent, MovingBrushComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, brush *MovingBrushComponent) bool {
		if tr == nil || brush == nil {
			return true
		}
		if brush.ClosedPosition == (mgl32.Vec3{}) {
			brush.ClosedPosition = tr.Position
		}
		if brush.ClosedBoundsCenter == (mgl32.Vec3{}) {
			brush.ClosedBoundsCenter = brush.BoundsCenter
		}
		target := brush.TargetPosition()
		delta := target.Sub(tr.Position)
		if delta.LenSqr() <= 1e-8 {
			tr.Position = target
		} else {
			speed := brush.Speed
			if speed <= 0 {
				speed = 2
			}
			step := speed * dt
			if step >= delta.Len() {
				tr.Position = target
			} else {
				tr.Position = tr.Position.Add(delta.Normalize().Mul(step))
			}
		}
		progress := tr.Position.Sub(brush.ClosedPosition)
		brush.BoundsCenter = brush.ClosedBoundsCenter.Add(progress)
		if local, ok := localTransformForEntity(cmd, eid); ok {
			local.Position = tr.Position
			local.Rotation = tr.Rotation
			local.Scale = tr.Scale
		}
		return true
	})
}
