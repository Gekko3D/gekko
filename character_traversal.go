package gekko

import "github.com/go-gl/mathgl/mgl32"

// CharacterTraversalConfig describes a validated climb over an obstacle. The
// caller owns input, action policy, and presentation; this helper owns only
// the collision queries shared by character controllers.
type CharacterTraversalConfig struct {
	CollisionConfig CharacterCollisionConfig
	GroundConfig    CharacterGroundProbeConfig
	MinHeight       float32
	MaxHeight       float32
	ForwardDistance float32
	HeightIncrement float32
}

type CharacterTraversalTarget struct {
	Start   mgl32.Vec3
	Lift    mgl32.Vec3
	Landing mgl32.Vec3
	Height  float32
}

// CharacterFindTraversalTarget finds a walkable, standing-clear landing past
// a blocking obstacle. It does not move the character. Callers must advance
// through Start -> Lift -> Landing with the normal collision helpers.
func CharacterFindTraversalTarget(voxRt *VoxelRtState, start, forward mgl32.Vec3, opts CharacterTraversalConfig, acceptEntity func(EntityId, bool) bool) (CharacterTraversalTarget, bool) {
	if voxRt == nil {
		return CharacterTraversalTarget{}, false
	}
	forward[1] = 0
	if forward.LenSqr() <= 1e-8 {
		return CharacterTraversalTarget{}, false
	}
	forward = forward.Normalize()
	collision := effectiveCharacterCollisionConfig(opts.CollisionConfig)
	ground := opts.GroundConfig
	if ground.Radius <= 0 {
		ground.Radius = collision.Radius
	}
	if ground.StepHeight <= 0 {
		ground.StepHeight = collision.StepHeight
	}
	minHeight := opts.MinHeight
	if minHeight <= 0 {
		minHeight = collision.StepHeight + 0.05
	}
	maxHeight := opts.MaxHeight
	if maxHeight <= 0 {
		maxHeight = collision.Height * 0.7
	}
	if maxHeight < minHeight {
		return CharacterTraversalTarget{}, false
	}
	probeDistance := opts.ForwardDistance
	if probeDistance <= 0 {
		probeDistance = collision.Radius*2 + 0.45
	}
	blockHit, blocked := CharacterMovementBlockHit(voxRt, start, forward.Mul(probeDistance), collision, acceptEntity)
	if !blocked || !blockHit.Hit {
		return CharacterTraversalTarget{}, false
	}
	forwardDistance := maxCharacterCollisionFloat(probeDistance, blockHit.T+collision.Radius+0.2)
	increment := opts.HeightIncrement
	if increment <= 0 {
		increment = 0.05
	}
	for rise := minHeight; rise <= maxHeight+1e-5; rise += increment {
		lift, blocked := CharacterVerticalMove(voxRt, start, rise, collision, acceptEntity)
		if blocked || lift.Y() < start.Y()+rise-1e-4 {
			continue
		}
		move := forward.Mul(forwardDistance)
		if _, blocked := CharacterMovementBlockHit(voxRt, lift, move, collision, acceptEntity); blocked {
			continue
		}
		candidate := lift.Add(move)
		floor, ok := CharacterGroundHitAtWithin(voxRt, candidate, ground, 0.05, maxHeight+defaultCharacterCollisionFloat(ground.GroundProbe, 0.15), acceptEntity)
		if !ok {
			continue
		}
		height := floor.Y - start.Y()
		if height < minHeight-0.05 || height > maxHeight+0.05 {
			continue
		}
		landing := candidate
		landing[1] = floor.Y
		if !CharacterHasStandingClearance(voxRt, landing, collision, acceptEntity) {
			continue
		}
		return CharacterTraversalTarget{Start: start, Lift: lift, Landing: landing, Height: height}, true
	}
	return CharacterTraversalTarget{}, false
}

// CharacterHasStandingClearance checks the capsule's vertical footprint at a
// proposed base position. It deliberately uses the same radius samples as the
// vertical controller sweep, so landing validation and movement agree.
func CharacterHasStandingClearance(voxRt *VoxelRtState, basePos mgl32.Vec3, cfg CharacterCollisionConfig, acceptEntity func(EntityId, bool) bool) bool {
	if voxRt == nil {
		return true
	}
	cfg = effectiveCharacterCollisionConfig(cfg)
	height := cfg.Height - 0.05
	if height <= 0 {
		return true
	}
	for _, offset := range CharacterVerticalCollisionOffsets(cfg.Radius) {
		origin := basePos.Add(offset).Add(mgl32.Vec3{0, 0.03, 0})
		hit := voxRt.RaycastFiltered(origin, mgl32.Vec3{0, 1, 0}, height, acceptEntity)
		if hit.Hit && hit.T <= height {
			return false
		}
	}
	return true
}
