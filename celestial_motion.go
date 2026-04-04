package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type CelestialMotionComponent struct {
	OrbitCenter       mgl32.Vec3
	OrbitAroundEntity bool
	OrbitCenterEntity EntityId
	OrbitCenterOffset mgl32.Vec3
	OrbitAxis         mgl32.Vec3
	OrbitRadius       float32
	OrbitPhase        float32
	OrbitAngularSpeed float32

	SelfAxis         mgl32.Vec3
	SelfPhase        float32
	SelfAngularSpeed float32

	AxialTiltDeg  float32
	AxialTiltAxis mgl32.Vec3
}

type CelestialMotionModule struct{}

func (CelestialMotionModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(celestialMotionSystem).
			InStage(Update).
			RunAlways(),
	)
}

func celestialMotionSystem(time *Time, cmd *Commands) {
	if cmd == nil {
		return
	}

	dt := float32(0.0)
	if time != nil && time.Dt > 0 {
		dt = float32(time.Dt)
	}

	MakeQuery1[CelestialMotionComponent](cmd).Map(func(_ EntityId, motion *CelestialMotionComponent) bool {
		if motion == nil {
			return true
		}
		if dt > 0 {
			motion.OrbitPhase += motion.OrbitAngularSpeed * dt
			motion.SelfPhase += motion.SelfAngularSpeed * dt
		}
		return true
	})

	applyCelestialMotionPass(cmd, false)
	for pass := 0; pass < 4; pass++ {
		if !applyCelestialMotionPass(cmd, true) {
			break
		}
	}
}

func CelestialMotionPosition(motion CelestialMotionComponent) (mgl32.Vec3, bool) {
	return CelestialMotionPositionWithCenter(motion, motion.OrbitCenter)
}

func CelestialMotionPositionWithCenter(motion CelestialMotionComponent, center mgl32.Vec3) (mgl32.Vec3, bool) {
	if motion.OrbitRadius <= 0 {
		return mgl32.Vec3{}, false
	}
	axis := normalizedCelestialAxis(motion.OrbitAxis, mgl32.Vec3{0, 1, 0})
	basisX, basisY := celestialOrbitBasis(axis)
	orbitOffset := basisX.Mul(float32(math.Cos(float64(motion.OrbitPhase)))).Add(
		basisY.Mul(float32(math.Sin(float64(motion.OrbitPhase)))),
	)
	return center.Add(orbitOffset.Mul(motion.OrbitRadius)), true
}

func CelestialMotionRotation(motion CelestialMotionComponent) (mgl32.Quat, bool) {
	selfAxis := normalizedCelestialAxis(motion.SelfAxis, mgl32.Vec3{0, 1, 0})
	tiltAxis := motion.AxialTiltAxis
	if tiltAxis.Len() <= 1e-5 {
		tiltAxis = mgl32.Vec3{0, 0, 1}
	}
	tiltAxis = tiltAxis.Normalize()

	hasSpin := float32(math.Abs(float64(motion.SelfPhase))+math.Abs(float64(motion.SelfAngularSpeed))) > 1e-5
	hasTilt := float32(math.Abs(float64(motion.AxialTiltDeg))) > 1e-5
	if !hasSpin && !hasTilt {
		return mgl32.QuatIdent(), false
	}

	tilt := mgl32.QuatIdent()
	if hasTilt {
		tilt = mgl32.QuatRotate(mgl32.DegToRad(motion.AxialTiltDeg), tiltAxis)
	}
	spin := mgl32.QuatRotate(motion.SelfPhase, selfAxis)
	return tilt.Mul(spin).Normalize(), true
}

func normalizedCelestialAxis(axis, fallback mgl32.Vec3) mgl32.Vec3 {
	if axis.Len() > 1e-5 {
		return axis.Normalize()
	}
	return fallback.Normalize()
}

func celestialOrbitBasis(axis mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	reference := mgl32.Vec3{0, 0, 1}
	if float32(math.Abs(float64(axis.Dot(reference)))) > 0.98 {
		reference = mgl32.Vec3{1, 0, 0}
	}
	basisX := axis.Cross(reference).Normalize()
	basisY := axis.Cross(basisX).Normalize()
	return basisX, basisY
}

func applyCelestialMotionPass(cmd *Commands, targeted bool) bool {
	if cmd == nil {
		return false
	}
	changed := false
	MakeQuery2[TransformComponent, CelestialMotionComponent](cmd).Map(func(_ EntityId, tr *TransformComponent, motion *CelestialMotionComponent) bool {
		if tr == nil || motion == nil {
			return true
		}
		hasTarget := motion.OrbitAroundEntity
		if hasTarget != targeted {
			return true
		}

		center := motion.OrbitCenter
		if hasTarget {
			targetTransform, ok := transformForEntity(cmd, motion.OrbitCenterEntity)
			if !ok || targetTransform == nil {
				return true
			}
			center = targetTransform.Position.Add(motion.OrbitCenterOffset)
		}

		if pos, ok := CelestialMotionPositionWithCenter(*motion, center); ok && tr.Position != pos {
			tr.Position = pos
			changed = true
		}
		if rot, ok := CelestialMotionRotation(*motion); ok && tr.Rotation != rot {
			tr.Rotation = rot
			changed = true
		}
		return true
	})
	return changed
}
