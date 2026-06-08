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

type TriggerVolumeComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	TargetName        string
	Target            string
	Delay             float32
	Wait              float32
	Once              bool
	SourceTag         string
	Tags              []string
	Fired             bool
	CooldownRemaining float32
	ActivationCount   int
}

type TargetEventDef struct {
	Target string
	Delay  float32
}

type MultiTargetComponent struct {
	TargetName      string
	Delay           float32
	Events          []TargetEventDef
	SourceTag       string
	Tags            []string
	ActivationCount int
}

type TargetRelayComponent struct {
	Kind            string
	TargetName      string
	Target          string
	Delay           float32
	KillTarget      string
	TriggerState    int
	SpawnFlags      int
	SourceTag       string
	Tags            []string
	ActivationCount int
	Fired           bool
}

type TargetEventComponent struct {
	Target         string
	DelayRemaining float32
	Activator      EntityId
	SourceTag      string
	TriggerState   int
}

func QueueTargetEvent(cmd *Commands, target string, delay float32, activator EntityId, sourceTag string) {
	QueueTargetEventWithState(cmd, target, delay, activator, sourceTag, 2)
}

func QueueTargetEventWithState(cmd *Commands, target string, delay float32, activator EntityId, sourceTag string, triggerState int) {
	if cmd == nil || target == "" {
		return
	}
	if delay <= 0 {
		ActivateTargetWithState(cmd, target, activator, triggerState)
		return
	}
	cmd.AddEntity(&TargetEventComponent{
		Target:         target,
		DelayRemaining: delay,
		Activator:      activator,
		SourceTag:      sourceTag,
		TriggerState:   triggerState,
	})
}

func ActivateTarget(cmd *Commands, target string, activator EntityId) {
	ActivateTargetWithState(cmd, target, activator, 2)
}

func ActivateTargetWithState(cmd *Commands, target string, activator EntityId, triggerState int) {
	if cmd == nil || target == "" {
		return
	}
	MakeQuery1[MovingBrushComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent) bool {
		if brush == nil || brush.TargetName != target {
			return true
		}
		switch triggerState {
		case 0:
			brush.Open = false
		case 1:
			brush.Open = true
		default:
			brush.Open = !brush.Open
		}
		brush.ActivationCount++
		return true
	})
	MakeQuery1[TargetRelayComponent](cmd).Map(func(eid EntityId, relay *TargetRelayComponent) bool {
		if relay == nil || relay.TargetName != target {
			return true
		}
		relay.ActivationCount++
		relay.Fired = true
		if relay.KillTarget != "" {
			KillTarget(cmd, relay.KillTarget)
		}
		if relay.Target != "" {
			QueueTargetEventWithState(cmd, relay.Target, relay.Delay, activator, relay.SourceTag, relay.TriggerState)
		}
		if relay.SpawnFlags&1 != 0 {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[MultiTargetComponent](cmd).Map(func(_ EntityId, multi *MultiTargetComponent) bool {
		if multi == nil || multi.TargetName != target {
			return true
		}
		multi.ActivationCount++
		for _, event := range multi.Events {
			QueueTargetEvent(cmd, event.Target, multi.Delay+event.Delay, activator, multi.SourceTag)
		}
		return true
	})
	MakeQuery1[BreakableComponent](cmd).Map(func(eid EntityId, breakable *BreakableComponent) bool {
		if breakable == nil || breakable.TargetName != target {
			return true
		}
		triggerBreakable(cmd, eid, breakable, activator)
		return true
	})
}

func KillTarget(cmd *Commands, target string) {
	if cmd == nil || target == "" {
		return
	}
	MakeQuery1[MovingBrushComponent](cmd).Map(func(eid EntityId, brush *MovingBrushComponent) bool {
		if brush != nil && brush.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[TargetRelayComponent](cmd).Map(func(eid EntityId, relay *TargetRelayComponent) bool {
		if relay != nil && relay.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[MultiTargetComponent](cmd).Map(func(eid EntityId, multi *MultiTargetComponent) bool {
		if multi != nil && multi.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[BreakableComponent](cmd).Map(func(eid EntityId, breakable *BreakableComponent) bool {
		if breakable != nil && breakable.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
}

func targetEventSystem(cmd *Commands, time *Time) {
	if cmd == nil {
		return
	}
	var dt float32
	if time != nil && time.Dt > 0 {
		dt = float32(time.Dt)
	}
	MakeQuery1[TargetEventComponent](cmd).Map(func(eid EntityId, event *TargetEventComponent) bool {
		if event == nil {
			return true
		}
		event.DelayRemaining -= dt
		if event.DelayRemaining > 0 {
			return true
		}
		ActivateTargetWithState(cmd, event.Target, event.Activator, event.TriggerState)
		cmd.RemoveEntity(eid)
		return true
	})
}

func triggerVolumeTouchSystem(cmd *Commands, time *Time) {
	if cmd == nil {
		return
	}
	var dt float32
	if time != nil && time.Dt > 0 {
		dt = float32(time.Dt)
	}
	MakeQuery2[TransformComponent, GroundedPlayerControllerComponent](cmd).Map(func(player EntityId, tr *TransformComponent, ctrl *GroundedPlayerControllerComponent) bool {
		if tr == nil || ctrl == nil {
			return true
		}
		playerHalf := mgl32.Vec3{ctrl.Radius, ctrl.Height * 0.5, ctrl.Radius}
		if playerHalf.X() <= 0 {
			playerHalf[0] = 0.35
		}
		if playerHalf.Y() <= 0 {
			playerHalf[1] = 0.9
		}
		if playerHalf.Z() <= 0 {
			playerHalf[2] = playerHalf.X()
		}
		playerCenter := tr.Position.Add(mgl32.Vec3{0, playerHalf.Y(), 0})
		playerMin := playerCenter.Sub(playerHalf)
		playerMax := playerCenter.Add(playerHalf)
		MakeQuery1[TriggerVolumeComponent](cmd).Map(func(_ EntityId, trigger *TriggerVolumeComponent) bool {
			if trigger == nil {
				return true
			}
			if trigger.CooldownRemaining > 0 {
				trigger.CooldownRemaining -= dt
				if trigger.CooldownRemaining > 0 {
					return true
				}
			}
			if trigger.Once && trigger.Fired {
				return true
			}
			triggerMin := trigger.BoundsCenter.Sub(trigger.BoundsHalfExtents)
			triggerMax := trigger.BoundsCenter.Add(trigger.BoundsHalfExtents)
			if !aabbOverlap(playerMin, playerMax, triggerMin, triggerMax) {
				return true
			}
			trigger.Fired = true
			trigger.ActivationCount++
			QueueTargetEvent(cmd, trigger.Target, trigger.Delay, player, trigger.SourceTag)
			wait := trigger.Wait
			if wait <= 0 {
				wait = 0.2
			}
			trigger.CooldownRemaining = wait
			return true
		})
		return true
	})
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
