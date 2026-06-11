package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type CharacterGroundProbeConfig struct {
	Radius             float32
	StepHeight         float32
	GroundProbe        float32
	MinWalkableNormalY float32
}

type CharacterGroundHit struct {
	Y      float32
	Normal mgl32.Vec3
}

type CharacterGroundVisualConfig struct {
	SmoothingSpeed float32
	SnapDistance   float32
	Deadband       float32
}

type CharacterGroundVisualState struct {
	RawY        float32
	VisualY     float32
	Initialized bool
}

func DefaultCharacterGroundVisualConfig() CharacterGroundVisualConfig {
	return CharacterGroundVisualConfig{
		SmoothingSpeed: 8,
		SnapDistance:   0.75,
		Deadband:       0.025,
	}
}

func CharacterGroundHitAt(voxRt *VoxelRtState, basePos mgl32.Vec3, cfg CharacterGroundProbeConfig, maxSnapUp float32, acceptEntity func(EntityId, bool) bool) (CharacterGroundHit, bool) {
	groundProbe := defaultCharacterGroundFloat(cfg.GroundProbe, 0.15)
	return CharacterGroundHitAtWithin(voxRt, basePos, cfg, maxSnapUp, maxCharacterGroundFloat(groundProbe, 4.0), acceptEntity)
}

func CharacterGroundHitAtWithin(voxRt *VoxelRtState, basePos mgl32.Vec3, cfg CharacterGroundProbeConfig, maxSnapUp, maxSnapDown float32, acceptEntity func(EntityId, bool) bool) (CharacterGroundHit, bool) {
	if voxRt == nil {
		return CharacterGroundHit{}, false
	}
	radius := defaultCharacterGroundFloat(cfg.Radius, 0.35)
	stepHeight := defaultCharacterGroundFloat(cfg.StepHeight, 0.6)
	groundProbe := defaultCharacterGroundFloat(cfg.GroundProbe, 0.15)
	probeHeight := stepHeight + groundProbe
	probeDistance := probeHeight + maxCharacterGroundFloat(maxSnapDown, groundProbe)
	minNormalY := CharacterMinWalkableNormalY(cfg)
	best := CharacterGroundHit{}
	found := false
	for _, offset := range CharacterGroundProbeOffsets(radius) {
		probeOrigin := basePos.Add(offset).Add(mgl32.Vec3{0, probeHeight, 0})
		hit := voxRt.RaycastFiltered(probeOrigin, mgl32.Vec3{0, -1, 0}, probeDistance, acceptEntity)
		if !hit.Hit || hit.Normal.Y() < minNormalY {
			continue
		}
		y := probeOrigin.Y() - hit.T
		if y > basePos.Y()+maxSnapUp {
			continue
		}
		if !found || y > best.Y {
			best = CharacterGroundHit{Y: y, Normal: hit.Normal}
			found = true
		}
	}
	return best, found
}

func CharacterGroundSnapUpTolerance(cfg CharacterGroundProbeConfig) float32 {
	groundProbe := defaultCharacterGroundFloat(cfg.GroundProbe, 0.15)
	return minCharacterGroundFloat(maxCharacterGroundFloat(groundProbe*0.25, 0.01), 0.05)
}

func CharacterMinWalkableNormalY(cfg CharacterGroundProbeConfig) float32 {
	if cfg.MinWalkableNormalY > 0 {
		return minCharacterGroundFloat(maxCharacterGroundFloat(cfg.MinWalkableNormalY, 0.01), 1)
	}
	return 0.65
}

func CharacterAcceptsGroundY(baseY, floorY, maxSnapUp, maxSnapDown float32) bool {
	delta := baseY - floorY
	return delta >= -maxSnapUp && delta <= maxSnapDown
}

func CharacterGroundProbeOffsets(radius float32) []mgl32.Vec3 {
	r := maxCharacterGroundFloat(radius*0.5, 0.05)
	return []mgl32.Vec3{
		{r, 0, r},
		{-r, 0, r},
		{r, 0, -r},
		{-r, 0, -r},
		{0, 0, 0},
	}
}

func UpdateCharacterGroundVisualY(state *CharacterGroundVisualState, targetY float32, dt float32, cfg CharacterGroundVisualConfig) float32 {
	if state == nil {
		return targetY
	}
	cfg = effectiveCharacterGroundVisualConfig(cfg)
	state.RawY = targetY
	if !state.Initialized || dt <= 0 || float32(math.Abs(float64(targetY-state.VisualY))) >= cfg.SnapDistance {
		state.VisualY = targetY
		state.Initialized = true
		return state.VisualY
	}
	delta := targetY - state.VisualY
	if float32(math.Abs(float64(delta))) <= cfg.Deadband {
		return state.VisualY
	}
	alpha := float32(1 - math.Exp(float64(-cfg.SmoothingSpeed*dt)))
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}
	state.VisualY += delta * alpha
	return state.VisualY
}

func ResetCharacterGroundVisualY(state *CharacterGroundVisualState, y float32) {
	if state == nil {
		return
	}
	state.RawY = y
	state.VisualY = y
	state.Initialized = true
}

func ApplyCharacterVisualGroundOffsetToChildren(cmd *Commands, parent EntityId, offsetY float32) {
	if cmd == nil {
		return
	}
	MakeQuery2[Parent, LocalTransformComponent](cmd).Map(func(_ EntityId, childParent *Parent, local *LocalTransformComponent) bool {
		if childParent == nil || local == nil || childParent.Entity != parent {
			return true
		}
		local.Position[1] = offsetY
		return true
	})
}

func effectiveCharacterGroundVisualConfig(cfg CharacterGroundVisualConfig) CharacterGroundVisualConfig {
	defaults := DefaultCharacterGroundVisualConfig()
	if cfg.SmoothingSpeed > 0 {
		defaults.SmoothingSpeed = cfg.SmoothingSpeed
	}
	if cfg.SnapDistance > 0 {
		defaults.SnapDistance = cfg.SnapDistance
	}
	if cfg.Deadband > 0 {
		defaults.Deadband = cfg.Deadband
	}
	return defaults
}

func defaultCharacterGroundFloat(v, fallback float32) float32 {
	if v == 0 {
		return fallback
	}
	return v
}

func minCharacterGroundFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxCharacterGroundFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
