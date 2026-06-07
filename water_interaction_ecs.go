package gekko

import (
	"math"
	"sort"

	"github.com/go-gl/mathgl/mgl32"
)

const (
	maxWaterInteractionRipples = 64
)

type WaterDisturbanceKind uint32

const (
	WaterDisturbanceImpact WaterDisturbanceKind = iota
	WaterDisturbanceSkim
	WaterDisturbanceWake
)

type WaterImpactEvent struct {
	WaterEntity EntityId
	BodyEntity  EntityId
	Position    mgl32.Vec3
	Velocity    mgl32.Vec3
	Speed       float32
	Strength    float32
	Radius      float32
	Kind        WaterDisturbanceKind
}

type WaterRipple struct {
	WaterEntity        EntityId
	Position           mgl32.Vec3
	Strength           float32
	Age                float32
	Lifetime           float32
	Radius             float32
	HorizontalVelocity [2]float32
	Foam               float32
	Kind               WaterDisturbanceKind
}

type waterOccupancyKey struct {
	WaterEntity EntityId
	BodyEntity  EntityId
}

type waterInteractionBody struct {
	Entity      EntityId
	Center      mgl32.Vec3
	HalfExtents [2]float32
	SurfaceY    float32
	BottomY     float32
}

type WaterInteractionState struct {
	impactBuffer      []WaterImpactEvent
	activeRipples     []WaterRipple
	previousPositions map[EntityId]mgl32.Vec3
	occupancy         map[waterOccupancyKey]bool
	wakeTimers        map[waterOccupancyKey]float32
}

func (s *WaterInteractionState) ImpactEvents() []WaterImpactEvent {
	if s == nil || len(s.impactBuffer) == 0 {
		return nil
	}
	return append([]WaterImpactEvent(nil), s.impactBuffer...)
}

func (s *WaterInteractionState) ActiveRipples() []WaterRipple {
	if s == nil || len(s.activeRipples) == 0 {
		return nil
	}
	return append([]WaterRipple(nil), s.activeRipples...)
}

func (s *WaterInteractionState) ensureMaps() {
	if s.previousPositions == nil {
		s.previousPositions = make(map[EntityId]mgl32.Vec3)
	}
	if s.occupancy == nil {
		s.occupancy = make(map[waterOccupancyKey]bool)
	}
	if s.wakeTimers == nil {
		s.wakeTimers = make(map[waterOccupancyKey]float32)
	}
}

func waterInteractionSystem(cmd *Commands, time *Time, state *WaterInteractionState) {
	if cmd == nil || time == nil || state == nil {
		return
	}

	state.ensureMaps()
	state.advanceRipples(float32(time.Dt))

	waters := collectWaterInteractionBodies(cmd)
	if len(waters) == 0 {
		state.clearInactiveBodies(nil, nil)
		return
	}

	activeBodies := make(map[EntityId]struct{})
	activePairs := make(map[waterOccupancyKey]struct{})

	MakeQuery3[TransformComponent, RigidBodyComponent, ColliderComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, rb *RigidBodyComponent, col *ColliderComponent) bool {
		if tr == nil || rb == nil || col == nil || rb.BodyMode != BodyModeDynamic {
			return true
		}

		activeBodies[eid] = struct{}{}
		currentPos := tr.Position
		prevPos, hadPrev := state.previousPositions[eid]
		probeRadius := waterInteractionProbeRadius(tr, col)
		horizontalSpeed := waterInteractionHorizontalSpeed(rb.Velocity)

		for _, water := range waters {
			key := waterOccupancyKey{WaterEntity: water.Entity, BodyEntity: eid}
			activePairs[key] = struct{}{}

			isInside := waterInteractionBodyInside(currentPos, probeRadius, water)
			wasInside := state.occupancy[key]

			if hadPrev && !wasInside {
				impactPos, entered := detectWaterInteractionImpact(prevPos, currentPos, probeRadius, water)
				if entered {
					speed := maxf(-rb.Velocity.Y(), 0)
					if speed >= 2.0 {
						kind := classifyWaterDisturbance(speed, horizontalSpeed)
						strength := clampWaterFloat((speed+horizontalSpeed*0.35)/9.0, 0.35, 1.6)
						foam := clampWaterFloat(0.24+strength*0.28+probeRadius*0.12, 0.25, 0.9)
						state.impactBuffer = append(state.impactBuffer, WaterImpactEvent{
							WaterEntity: water.Entity,
							BodyEntity:  eid,
							Position:    impactPos,
							Velocity:    rb.Velocity,
							Speed:       speed,
							Strength:    strength,
							Radius:      probeRadius,
							Kind:        kind,
						})
						state.spawnRipple(water.Entity, impactPos, strength, probeRadius, rb.Velocity, foam, kind)
						isInside = true
					}
				}
			}

			if wasInside && isInside && horizontalSpeed > 1.2 {
				state.wakeTimers[key] += float32(time.Dt)
				interval := clampWaterFloat(0.34-probeRadius*0.05, 0.16, 0.34)
				if state.wakeTimers[key] >= interval {
					state.wakeTimers[key] = 0
					wakePos := currentPos
					wakePos[1] = water.SurfaceY + 0.02
					wakeStrength := clampWaterFloat(horizontalSpeed/9.0, 0.18, 0.8)
					wakeFoam := clampWaterFloat(wakeStrength*0.28, 0.08, 0.35)
					state.spawnRipple(water.Entity, wakePos, wakeStrength, probeRadius, rb.Velocity, wakeFoam, WaterDisturbanceWake)
				}
			} else if !isInside {
				delete(state.wakeTimers, key)
			}

			state.occupancy[key] = isInside
		}

		state.previousPositions[eid] = currentPos
		return true
	})

	state.clearInactiveBodies(activeBodies, activePairs)
}

func waterInteractionCleanupSystem(state *WaterInteractionState) {
	if state == nil || len(state.impactBuffer) == 0 {
		return
	}
	state.impactBuffer = state.impactBuffer[:0]
}

func (s *WaterInteractionState) advanceRipples(dt float32) {
	if dt <= 0 || len(s.activeRipples) == 0 {
		return
	}

	dst := s.activeRipples[:0]
	for _, ripple := range s.activeRipples {
		ripple.Age += dt
		if ripple.Age < ripple.Lifetime {
			dst = append(dst, ripple)
		}
	}
	s.activeRipples = dst
}

func (s *WaterInteractionState) spawnRipple(waterEntity EntityId, pos mgl32.Vec3, strength, radius float32, velocity mgl32.Vec3, foam float32, kind WaterDisturbanceKind) {
	if radius <= 0 {
		radius = 0.25
	}
	s.activeRipples = append(s.activeRipples, WaterRipple{
		WaterEntity:        waterEntity,
		Position:           pos,
		Strength:           strength,
		Age:                0,
		Lifetime:           1.8 + 0.45*strength + 0.18*radius,
		Radius:             radius,
		HorizontalVelocity: [2]float32{velocity.X(), velocity.Z()},
		Foam:               foam,
		Kind:               kind,
	})
	if len(s.activeRipples) > maxWaterInteractionRipples {
		s.activeRipples = append([]WaterRipple(nil), s.activeRipples[len(s.activeRipples)-maxWaterInteractionRipples:]...)
	}
}

func (s *WaterInteractionState) clearInactiveBodies(activeBodies map[EntityId]struct{}, activePairs map[waterOccupancyKey]struct{}) {
	for eid := range s.previousPositions {
		if activeBodies == nil {
			delete(s.previousPositions, eid)
			continue
		}
		if _, ok := activeBodies[eid]; !ok {
			delete(s.previousPositions, eid)
		}
	}

	for key := range s.occupancy {
		if activePairs == nil {
			delete(s.occupancy, key)
			delete(s.wakeTimers, key)
			continue
		}
		if _, ok := activePairs[key]; !ok {
			delete(s.occupancy, key)
			delete(s.wakeTimers, key)
		}
	}
}

func waterInteractionHorizontalSpeed(velocity mgl32.Vec3) float32 {
	return float32(math.Hypot(float64(velocity.X()), float64(velocity.Z())))
}

func classifyWaterDisturbance(verticalSpeed, horizontalSpeed float32) WaterDisturbanceKind {
	if horizontalSpeed > verticalSpeed*0.75 && horizontalSpeed > 2.5 {
		return WaterDisturbanceSkim
	}
	return WaterDisturbanceImpact
}

func collectWaterInteractionBodies(cmd *Commands) []waterInteractionBody {
	if cmd == nil {
		return nil
	}

	bodies := make([]waterInteractionBody, 0, 4)
	MakeQuery2[TransformComponent, WaterSurfaceComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, water *WaterSurfaceComponent) bool {
		if tr == nil || water == nil || !water.Enabled() {
			return true
		}

		center := water.WorldCenter(tr)
		extents := water.WorldHalfExtents(tr)
		depth := water.WorldDepth(tr)
		bodies = append(bodies, waterInteractionBody{
			Entity:      eid,
			Center:      center,
			HalfExtents: extents,
			SurfaceY:    center.Y(),
			BottomY:     center.Y() - depth,
		})
		return true
	})
	MakeQuery2[TransformComponent, ResolvedWaterPatchComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, patch *ResolvedWaterPatchComponent) bool {
		if tr == nil || patch == nil || !patch.Enabled() || patch.Kind != WaterPatchKindSurface {
			return true
		}
		bodies = append(bodies, waterInteractionBody{
			Entity:      eid,
			Center:      patch.Center,
			HalfExtents: patch.HalfExtents,
			SurfaceY:    patch.Center.Y(),
			BottomY:     patch.Center.Y() - patch.Depth,
		})
		return true
	})
	sort.Slice(bodies, func(i, j int) bool {
		return bodies[i].Entity < bodies[j].Entity
	})
	return bodies
}

func waterInteractionProbeRadius(tr *TransformComponent, col *ColliderComponent) float32 {
	if tr == nil || col == nil {
		return 0.35
	}

	switch col.Shape {
	case ShapeSphere:
		radiusScale := maxf(absf(tr.Scale.X()), maxf(absf(tr.Scale.Y()), absf(tr.Scale.Z())))
		return maxf(0.2, col.Radius*radiusScale)
	default:
		half := col.HalfExtents
		if half == (mgl32.Vec3{}) {
			half = col.AABBHalfExtents
		}
		scaled := mgl32.Vec3{
			absf(tr.Scale.X()) * half.X(),
			absf(tr.Scale.Y()) * half.Y(),
			absf(tr.Scale.Z()) * half.Z(),
		}
		return maxf(0.2, maxf(scaled.X(), maxf(scaled.Y(), scaled.Z())))
	}
}

func waterInteractionBodyInside(pos mgl32.Vec3, radius float32, water waterInteractionBody) bool {
	const horizontalMargin = 0.15
	return pos.X() >= water.Center.X()-water.HalfExtents[0]-radius-horizontalMargin &&
		pos.X() <= water.Center.X()+water.HalfExtents[0]+radius+horizontalMargin &&
		pos.Z() >= water.Center.Z()-water.HalfExtents[1]-radius-horizontalMargin &&
		pos.Z() <= water.Center.Z()+water.HalfExtents[1]+radius+horizontalMargin &&
		pos.Y()-radius <= water.SurfaceY+0.08 &&
		pos.Y()+radius >= water.BottomY-0.08
}

func detectWaterInteractionImpact(prevPos, currentPos mgl32.Vec3, radius float32, water waterInteractionBody) (mgl32.Vec3, bool) {
	prevBottom := prevPos.Y() - radius
	currentBottom := currentPos.Y() - radius
	if prevBottom <= water.SurfaceY || currentBottom > water.SurfaceY {
		return mgl32.Vec3{}, false
	}

	denom := prevBottom - currentBottom
	if math.Abs(float64(denom)) < 1e-5 {
		return mgl32.Vec3{}, false
	}

	t := (prevBottom - water.SurfaceY) / denom
	if t < 0 || t > 1 {
		return mgl32.Vec3{}, false
	}

	impactPos := prevPos.Add(currentPos.Sub(prevPos).Mul(t))
	const horizontalMargin = 0.15
	if impactPos.X() < water.Center.X()-water.HalfExtents[0]-radius-horizontalMargin ||
		impactPos.X() > water.Center.X()+water.HalfExtents[0]+radius+horizontalMargin ||
		impactPos.Z() < water.Center.Z()-water.HalfExtents[1]-radius-horizontalMargin ||
		impactPos.Z() > water.Center.Z()+water.HalfExtents[1]+radius+horizontalMargin {
		return mgl32.Vec3{}, false
	}

	impactPos[1] = water.SurfaceY + 0.02
	return impactPos, true
}
