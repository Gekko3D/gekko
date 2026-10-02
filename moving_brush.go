package gekko

import (
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type MovingBrushComponent struct {
	Kind               string
	MotionKind         string
	NavigationRole     string
	BoundsCenter       mgl32.Vec3
	BoundsHalfExtents  mgl32.Vec3
	ClosedHalfExtents  mgl32.Vec3
	MoveDirection      mgl32.Vec3
	ClosedPosition     mgl32.Vec3
	ClosedBoundsCenter mgl32.Vec3
	OpenOffset         mgl32.Vec3
	RotationOrigin     mgl32.Vec3
	RotationAxis       mgl32.Vec3
	ClosedRotation     mgl32.Quat
	OpenAngle          float32
	CurrentAngle       float32
	PathTarget         string
	PathWaitRemaining  float32
	OpenWaitRemaining  float32
	Speed              float32
	Wait               float32
	Lip                float32
	SpawnFlags         int
	TargetName         string
	Target             string
	SourceTag          string
	Tags               []string
	Open               bool
	ActivationCount    int
}

func (m *MovingBrushComponent) DoesNotMove() bool {
	return m != nil && m.Kind == "hl1_func_button" && m.SpawnFlags&1 != 0
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

func (m *MovingBrushComponent) TargetAngle() float32 {
	if m == nil {
		return 0
	}
	if m.Open {
		return m.OpenAngle
	}
	return 0
}

// MovingBrushFullyOpen reports physical clearance, not merely an open target.
func MovingBrushFullyOpen(m *MovingBrushComponent) bool {
	if m == nil || !m.Open {
		return false
	}
	if m.MotionKind == "rotate" {
		return absf(m.CurrentAngle-m.OpenAngle) <= 1e-3
	}
	return m.BoundsCenter.Sub(m.ClosedBoundsCenter.Add(m.OpenOffset)).LenSqr() <= 1e-6
}

// MovingBrushAtNavigationStop reports physical arrival at one of the
// discrete stops exposed by the current two-state moving-brush contract.
func MovingBrushAtNavigationStop(m *MovingBrushComponent, stop string) bool {
	if m == nil {
		return false
	}
	switch stop {
	case "closed":
		return !m.Open && m.BoundsCenter.Sub(m.ClosedBoundsCenter).LenSqr() <= 1e-6
	case "open":
		return MovingBrushFullyOpen(m)
	default:
		return false
	}
}

// RequestMovingBrushNavigationStop is idempotent: repeated calls cannot
// reverse a carrier while another system is waiting for the same stop.
func RequestMovingBrushNavigationStop(m *MovingBrushComponent, stop string) bool {
	if m == nil {
		return false
	}
	open := false
	switch stop {
	case "closed":
	case "open":
		open = true
	default:
		return false
	}
	if m.Open != open {
		m.Open = open
		m.OpenWaitRemaining = 0
		m.ActivationCount++
	}
	return true
}

// HoldMovingBrushNavigationStop keeps an auto-return carrier at its
// destination while a traversal is still exiting.
func HoldMovingBrushNavigationStop(m *MovingBrushComponent, stop string) bool {
	if !RequestMovingBrushNavigationStop(m, stop) {
		return false
	}
	if stop == "open" && m.Wait > 0 && MovingBrushFullyOpen(m) {
		m.OpenWaitRemaining = m.Wait
	}
	return true
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

type PathNodeComponent struct {
	TargetName string
	Target     string
	Position   mgl32.Vec3
	Wait       float32
	Speed      float32
	SpawnFlags int
	SourceTag  string
	Tags       []string
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

type DamageVolumeComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	Damage            float32
	DamageInterval    float32
	TargetName        string
	Target            string
	Delay             float32
	SpawnFlags        int
	Enabled           bool
	SourceTag         string
	Tags              []string
	CooldownRemaining float32
	ActivationCount   int
}

type ChangeLevelVolumeComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	TargetMap         string
	Landmark          string
	TargetName        string
	SpawnFlags        int
	Enabled           bool
	SourceTag         string
	Tags              []string
	ActivationCount   int
}

type ChargerComponent struct {
	Kind              string
	BoundsCenter      mgl32.Vec3
	BoundsHalfExtents mgl32.Vec3
	ChargeKind        string
	Capacity          float32
	Remaining         float32
	Rate              float32
	TargetName        string
	SpawnFlags        int
	Enabled           bool
	SourceTag         string
	Tags              []string
	ActivationCount   int
	ChargeRemainder   float32
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
	Target            string
	DelayRemaining    float32
	Activator         EntityId
	SourceTag         string
	TriggerState      int
	RuntimeGeneration uint64
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
	generation := uint64(0)
	if runtime := streamedLevelRuntimeStateFromApp(cmd.app); runtime != nil && runtime.Initialized {
		generation = runtime.Generation
	}
	cmd.AddEntity(&TargetEventComponent{
		Target:            target,
		DelayRemaining:    delay,
		Activator:         activator,
		SourceTag:         sourceTag,
		TriggerState:      triggerState,
		RuntimeGeneration: generation,
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
		if brush.DoesNotMove() {
			brush.ActivationCount++
			return true
		}
		switch triggerState {
		case 0:
			brush.Open = false
			brush.OpenWaitRemaining = 0
		case 1:
			brush.Open = true
			brush.OpenWaitRemaining = 0
		default:
			if brush.Wait > 0 {
				brush.Open = true
				brush.OpenWaitRemaining = 0
			} else {
				brush.Open = !brush.Open
			}
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
	MakeQuery1[DamageVolumeComponent](cmd).Map(func(_ EntityId, damage *DamageVolumeComponent) bool {
		if damage == nil || damage.TargetName != target {
			return true
		}
		switch triggerState {
		case 0:
			damage.Enabled = false
		case 1:
			damage.Enabled = true
		default:
			damage.Enabled = !damage.Enabled
		}
		damage.ActivationCount++
		return true
	})
	MakeQuery1[ChangeLevelVolumeComponent](cmd).Map(func(_ EntityId, volume *ChangeLevelVolumeComponent) bool {
		if volume == nil || volume.TargetName != target {
			return true
		}
		switch triggerState {
		case 0:
			volume.Enabled = false
		case 1:
			volume.Enabled = true
		default:
			volume.Enabled = !volume.Enabled
		}
		volume.ActivationCount++
		return true
	})
	MakeQuery1[ChargerComponent](cmd).Map(func(_ EntityId, charger *ChargerComponent) bool {
		if charger == nil || charger.TargetName != target {
			return true
		}
		switch triggerState {
		case 0:
			charger.Enabled = false
		case 1:
			charger.Enabled = true
		default:
			charger.Enabled = !charger.Enabled
		}
		charger.ActivationCount++
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
	MakeQuery1[DamageVolumeComponent](cmd).Map(func(eid EntityId, damage *DamageVolumeComponent) bool {
		if damage != nil && damage.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[ChangeLevelVolumeComponent](cmd).Map(func(eid EntityId, volume *ChangeLevelVolumeComponent) bool {
		if volume != nil && volume.TargetName == target {
			cmd.RemoveEntity(eid)
		}
		return true
	})
	MakeQuery1[ChargerComponent](cmd).Map(func(eid EntityId, charger *ChargerComponent) bool {
		if charger != nil && charger.TargetName == target {
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
		if event.RuntimeGeneration != 0 {
			runtime := streamedLevelRuntimeStateFromApp(cmd.app)
			if runtime == nil || !runtime.Initialized || runtime.Generation != event.RuntimeGeneration {
				cmd.RemoveEntity(eid)
				return true
			}
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
		if brush.DoesNotMove() {
			return true
		}
		previousPosition := tr.Position
		previousCenter := brush.BoundsCenter
		previousHalfExtents := brush.BoundsHalfExtents
		if brush.ClosedPosition == (mgl32.Vec3{}) {
			brush.ClosedPosition = tr.Position
		}
		if brush.ClosedHalfExtents == (mgl32.Vec3{}) {
			brush.ClosedHalfExtents = brush.BoundsHalfExtents
			if brush.ClosedBoundsCenter == (mgl32.Vec3{}) {
				brush.ClosedBoundsCenter = brush.BoundsCenter
			}
		}
		if brush.ClosedRotation.W == 0 {
			brush.ClosedRotation = tr.Rotation
			if brush.ClosedRotation.W == 0 {
				brush.ClosedRotation = mgl32.QuatIdent()
			}
		}
		nextTransform, nextBrush := *tr, *brush
		switch {
		case nextBrush.PathTarget != "":
			updateMovingBrushPath(cmd, &nextTransform, &nextBrush, dt)
		case nextBrush.MotionKind == "rotate":
			updateMovingBrushRotation(&nextTransform, &nextBrush, dt)
		default:
			updateMovingBrushLinear(&nextTransform, &nextBrush, dt)
		}
		updateMovingBrushOpenWait(&nextTransform, &nextBrush, dt)
		if nextBrush.MotionKind == "rotate" {
			nextBrush.BoundsCenter, nextBrush.BoundsHalfExtents = movingBrushRotatedBounds(&nextBrush, nextBrush.CurrentAngle)
		} else {
			progress := nextTransform.Position.Sub(nextBrush.ClosedPosition)
			nextBrush.BoundsCenter = nextBrush.ClosedBoundsCenter.Add(progress)
			nextBrush.BoundsHalfExtents = nextBrush.ClosedHalfExtents
		}
		delta := nextTransform.Position.Sub(previousPosition)
		if brush.NavigationRole == content.NavigationRoleDoor && movingBrushDoorObstructed(cmd, brush, &nextBrush) {
			if !brush.Open {
				RequestMovingBrushNavigationStop(brush, "open")
			}
			return true
		}
		if brush.NavigationRole == content.NavigationRoleCarrier && !movingBrushRidersCanMove(cmd, eid, delta) {
			return true
		}
		previousWorld := hierarchyBits(tr.Position, tr.Rotation, tr.Scale)
		*tr, *brush = nextTransform, nextBrush
		if hierarchyBits(tr.Position, tr.Rotation, tr.Scale) != previousWorld {
			cmd.MarkComponentChanged(eid, reflect.TypeOf(TransformComponent{}))
		}
		if local, ok := localTransformForEntity(cmd, eid); ok {
			previousLocal := hierarchyBits(local.Position, local.Rotation, local.Scale)
			local.Position = tr.Position
			local.Rotation = tr.Rotation
			local.Scale = tr.Scale
			if hierarchyBits(local.Position, local.Rotation, local.Scale) != previousLocal {
				cmd.MarkComponentChanged(eid, reflect.TypeOf(LocalTransformComponent{}))
			}
		}
		moveMovingBrushRiders(cmd, eid, previousCenter, previousHalfExtents, delta)
		return true
	})
}

func movingBrushRotatedBounds(brush *MovingBrushComponent, angle float32) (mgl32.Vec3, mgl32.Vec3) {
	axis := brush.RotationAxis
	if axis.LenSqr() <= 1e-6 {
		axis = mgl32.Vec3{0, 1, 0}
	} else {
		axis = axis.Normalize()
	}
	rotation := mgl32.QuatRotate(mgl32.DegToRad(angle), axis)
	center := brush.RotationOrigin.Add(rotation.Rotate(brush.ClosedBoundsCenter.Sub(brush.RotationOrigin)))
	return center, rotatedAABBHalfExtents(brush.ClosedHalfExtents, rotation)
}

func rotatedAABBHalfExtents(half mgl32.Vec3, rotation mgl32.Quat) mgl32.Vec3 {
	x := rotation.Rotate(mgl32.Vec3{half.X(), 0, 0})
	y := rotation.Rotate(mgl32.Vec3{0, half.Y(), 0})
	z := rotation.Rotate(mgl32.Vec3{0, 0, half.Z()})
	return mgl32.Vec3{
		absf(x.X()) + absf(y.X()) + absf(z.X()),
		absf(x.Y()) + absf(y.Y()) + absf(z.Y()),
		absf(x.Z()) + absf(y.Z()) + absf(z.Z()),
	}
}

func updateMovingBrushOpenWait(tr *TransformComponent, brush *MovingBrushComponent, dt float32) {
	atOpenPosition := tr.Position.Sub(brush.TargetPosition()).LenSqr() <= 1e-8
	if brush.MotionKind == "rotate" {
		atOpenPosition = absf(brush.CurrentAngle-brush.TargetAngle()) <= 1e-4
	}
	if brush.PathTarget != "" || !brush.Open || brush.Wait <= 0 || !atOpenPosition {
		if !brush.Open {
			brush.OpenWaitRemaining = 0
		}
		return
	}
	if brush.OpenWaitRemaining <= 0 {
		brush.OpenWaitRemaining = brush.Wait
		return
	}
	brush.OpenWaitRemaining -= dt
	if brush.OpenWaitRemaining <= 0 {
		brush.Open = false
		brush.OpenWaitRemaining = 0
	}
}

func updateMovingBrushLinear(tr *TransformComponent, brush *MovingBrushComponent, dt float32) {
	target := brush.TargetPosition()
	delta := target.Sub(tr.Position)
	if delta.LenSqr() <= 1e-8 {
		tr.Position = target
		return
	}
	speed := brush.Speed
	if speed <= 0 {
		speed = 2
	}
	step := speed * dt
	if step >= delta.Len() {
		tr.Position = target
		return
	}
	tr.Position = tr.Position.Add(delta.Normalize().Mul(step))
}

func updateMovingBrushRotation(tr *TransformComponent, brush *MovingBrushComponent, dt float32) {
	target := brush.TargetAngle()
	delta := target - brush.CurrentAngle
	if absf(delta) <= 1e-4 {
		brush.CurrentAngle = target
	} else {
		speed := brush.Speed
		if speed <= 0 {
			speed = 90
		}
		step := speed * dt
		if absf(delta) <= step {
			brush.CurrentAngle = target
		} else if delta > 0 {
			brush.CurrentAngle += step
		} else {
			brush.CurrentAngle -= step
		}
	}
	axis := brush.RotationAxis
	if axis.LenSqr() <= 1e-6 {
		axis = mgl32.Vec3{0, 1, 0}
	} else {
		axis = axis.Normalize()
	}
	rot := mgl32.QuatRotate(mgl32.DegToRad(brush.CurrentAngle), axis)
	tr.Rotation = brush.ClosedRotation.Mul(rot)
	tr.Position = brush.RotationOrigin.Add(rot.Rotate(brush.ClosedPosition.Sub(brush.RotationOrigin)))
}

func updateMovingBrushPath(cmd *Commands, tr *TransformComponent, brush *MovingBrushComponent, dt float32) {
	if !brush.Open {
		return
	}
	if brush.PathWaitRemaining > 0 {
		brush.PathWaitRemaining -= dt
		return
	}
	node, ok := findPathNodeByTargetName(cmd, brush.PathTarget)
	if !ok {
		brush.Open = false
		return
	}
	target := node.Position
	delta := target.Sub(tr.Position)
	speed := brush.Speed
	if node.Speed > 0 {
		speed = node.Speed
		brush.Speed = node.Speed
	}
	if speed <= 0 {
		speed = 2
	}
	if delta.LenSqr() <= 1e-8 || speed*dt >= delta.Len() {
		tr.Position = target
		brush.PathTarget = node.Target
		brush.PathWaitRemaining = node.Wait
		if brush.PathTarget == "" {
			brush.Open = false
		}
		return
	}
	tr.Position = tr.Position.Add(delta.Normalize().Mul(speed * dt))
}

func findPathNodeByTargetName(cmd *Commands, targetName string) (PathNodeComponent, bool) {
	if cmd == nil || targetName == "" {
		return PathNodeComponent{}, false
	}
	var out PathNodeComponent
	var found bool
	MakeQuery1[PathNodeComponent](cmd).Map(func(_ EntityId, node *PathNodeComponent) bool {
		if node == nil || node.TargetName != targetName {
			return true
		}
		out = *node
		found = true
		return false
	})
	return out, found
}
