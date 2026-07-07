package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type CharacterCollisionConfig struct {
	Radius                  float32
	Height                  float32
	StepHeight              float32
	SkinWidth               float32
	ContactEpsilon          float32
	MaxDepenetration        float32
	DepenetrationIterations int
}

type CharacterCollisionHit struct {
	RaycastHit
	SampleY float32
	Offset  mgl32.Vec3
	Stage   string
}

type CharacterDepenetrationResult struct {
	Position     mgl32.Vec3
	Offset       mgl32.Vec3
	Iterations   int
	Depenetrated bool
	Hit          CharacterCollisionHit
}

type CharacterKinematicMoveOptions struct {
	CollisionConfig      CharacterCollisionConfig
	AcceptEntity         func(EntityId, bool) bool
	DisableDepenetration bool
	DisableSlide         bool
}

type CharacterKinematicMoveResult struct {
	Start         mgl32.Vec3
	Position      mgl32.Vec3
	RequestedMove mgl32.Vec3
	AppliedMove   mgl32.Vec3
	Hit           CharacterCollisionHit
	Depenetration CharacterDepenetrationResult
	Blocked       bool
	Slid          bool
	Depenetrated  bool
}

// CharacterGroundedMoveOptions describes the collision portion of a grounded
// character move. Input, gravity, camera, and game policy remain with the
// caller.
type CharacterGroundedMoveOptions struct {
	CollisionConfig CharacterCollisionConfig
	GroundConfig    CharacterGroundProbeConfig
	AcceptEntity    func(EntityId, bool) bool
}

type CharacterGroundedMoveResult struct {
	Position      mgl32.Vec3
	Hit           CharacterCollisionHit
	Blocked       bool
	Slid          bool
	Stepped       bool
	Depenetrated  bool
	Depenetration CharacterDepenetrationResult
}

func CharacterKinematicMove(voxRt *VoxelRtState, position, move mgl32.Vec3, opts CharacterKinematicMoveOptions) CharacterKinematicMoveResult {
	move[1] = 0
	result := CharacterKinematicMoveResult{
		Start:         position,
		Position:      position,
		RequestedMove: move,
	}
	if move.LenSqr() <= 1e-8 {
		return result
	}
	cfg := effectiveCharacterCollisionConfig(opts.CollisionConfig)
	if !opts.DisableDepenetration {
		depen := CharacterDepenetrateInitialContacts(voxRt, position, move, cfg, opts.AcceptEntity)
		if depen.Depenetrated {
			position = depen.Position
			result.Position = position
			result.Depenetration = depen
			result.Depenetrated = true
		}
	}
	if hit, blocked := CharacterMovementBlockHit(voxRt, position, move, cfg, opts.AcceptEntity); !blocked {
		result.Position = position.Add(move)
		result.AppliedMove = result.Position.Sub(result.Start)
		return result
	} else {
		result.Hit = hit
		result.Blocked = true
		if opts.DisableSlide {
			result.AppliedMove = result.Position.Sub(result.Start)
			return result
		}
		slide, ok := characterKinematicSlideMove(move, hit.Normal)
		if ok {
			if slideHit, slideBlocked := CharacterMovementBlockHit(voxRt, position, slide, cfg, opts.AcceptEntity); !slideBlocked {
				result.Position = position.Add(slide)
				result.AppliedMove = result.Position.Sub(result.Start)
				result.Slid = true
				return result
			} else if slideHit.Hit {
				result.Hit = slideHit
			}
		}
	}
	result.AppliedMove = result.Position.Sub(result.Start)
	return result
}

// CharacterGroundedMove moves across walkable ground, including a single step
// no higher than CollisionConfig.StepHeight. It does not apply gravity.
func CharacterGroundedMove(voxRt *VoxelRtState, position, move mgl32.Vec3, opts CharacterGroundedMoveOptions) CharacterGroundedMoveResult {
	move[1] = 0
	result := CharacterGroundedMoveResult{Position: position}
	if move.LenSqr() <= 1e-8 {
		return result
	}
	cfg := effectiveCharacterCollisionConfig(opts.CollisionConfig)
	if depen := CharacterDepenetrateInitialContacts(voxRt, position, move, cfg, opts.AcceptEntity); depen.Depenetrated {
		position = depen.Position
		result.Position = position
		result.Depenetrated = true
		result.Depenetration = depen
	}
	if hit, blocked := CharacterMovementBlockHit(voxRt, position, move, cfg, opts.AcceptEntity); !blocked {
		result.Position = characterGroundedMoveLanding(voxRt, position, position.Add(move), cfg, opts)
		return result
	} else {
		result.Hit = hit
		if stepped, ok := characterGroundedStepMove(voxRt, position, move, cfg, opts); ok {
			result.Position = stepped
			result.Stepped = true
			return result
		}
	}

	kinematic := CharacterKinematicMove(voxRt, position, move, CharacterKinematicMoveOptions{
		CollisionConfig:      cfg,
		AcceptEntity:         opts.AcceptEntity,
		DisableDepenetration: true,
	})
	if kinematic.Slid {
		result.Position = characterGroundedMoveLanding(voxRt, position, kinematic.Position, cfg, opts)
		result.Slid = true
		return result
	}
	result.Blocked = true
	return result
}

func characterGroundedStepMove(voxRt *VoxelRtState, position, move mgl32.Vec3, cfg CharacterCollisionConfig, opts CharacterGroundedMoveOptions) (mgl32.Vec3, bool) {
	if voxRt == nil || cfg.StepHeight <= 0 {
		return mgl32.Vec3{}, false
	}
	raised, blocked := CharacterVerticalMove(voxRt, position, cfg.StepHeight, cfg, opts.AcceptEntity)
	if blocked || raised.Y() < position.Y()+cfg.StepHeight-1e-4 {
		return mgl32.Vec3{}, false
	}
	if _, blocked := CharacterMovementBlockHit(voxRt, raised, move, cfg, opts.AcceptEntity); blocked {
		return mgl32.Vec3{}, false
	}
	candidate := raised.Add(move)
	ground, ok := CharacterGroundHitAtWithin(voxRt, candidate, opts.GroundConfig, cfg.StepHeight, cfg.StepHeight+defaultCharacterCollisionFloat(opts.GroundConfig.GroundProbe, 0.15), opts.AcceptEntity)
	if !ok || !CharacterAcceptsGroundY(position.Y(), ground.Y, cfg.StepHeight, cfg.StepHeight+defaultCharacterCollisionFloat(opts.GroundConfig.GroundProbe, 0.15)) {
		return mgl32.Vec3{}, false
	}
	candidate[1] = ground.Y
	return candidate, true
}

func characterGroundedMoveLanding(voxRt *VoxelRtState, start, candidate mgl32.Vec3, cfg CharacterCollisionConfig, opts CharacterGroundedMoveOptions) mgl32.Vec3 {
	if voxRt == nil {
		return candidate
	}
	maxDown := cfg.StepHeight + defaultCharacterCollisionFloat(opts.GroundConfig.GroundProbe, 0.15)
	ground, ok := CharacterGroundHitAtWithin(voxRt, candidate, opts.GroundConfig, cfg.StepHeight, maxDown, opts.AcceptEntity)
	if ok && CharacterAcceptsGroundY(start.Y(), ground.Y, cfg.StepHeight, maxDown) {
		candidate[1] = ground.Y
	}
	return candidate
}

// CharacterVerticalMove sweeps the character footprint along Y and returns
// the reachable position. The collision flag reports a ceiling or floor hit.
func CharacterVerticalMove(voxRt *VoxelRtState, basePos mgl32.Vec3, deltaY float32, cfg CharacterCollisionConfig, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, bool) {
	if voxRt == nil || math.Abs(float64(deltaY)) <= 1e-5 {
		return basePos.Add(mgl32.Vec3{0, deltaY, 0}), false
	}
	cfg = effectiveCharacterCollisionConfig(cfg)
	dirY := float32(1)
	originY := cfg.Height
	if deltaY < 0 {
		dirY = -1
		originY = 0.02
	}
	distance := float32(math.Abs(float64(deltaY)))
	const clearance = float32(0.03)
	allowed := distance
	for _, offset := range CharacterVerticalCollisionOffsets(cfg.Radius) {
		origin := basePos.Add(offset).Add(mgl32.Vec3{0, originY, 0})
		hit := voxRt.RaycastFiltered(origin, mgl32.Vec3{0, dirY, 0}, distance+clearance, acceptEntity)
		if !hit.Hit || hit.T > distance+clearance {
			continue
		}
		allowed = minCharacterCollisionFloat(allowed, maxCharacterCollisionFloat(hit.T-clearance, 0))
	}
	if allowed < distance {
		return basePos.Add(mgl32.Vec3{0, dirY * allowed, 0}), true
	}
	return basePos.Add(mgl32.Vec3{0, deltaY, 0}), false
}

func CharacterVerticalCollisionOffsets(radius float32) []mgl32.Vec3 {
	r := maxCharacterCollisionFloat(radius*0.85, 0)
	if r <= 1e-5 {
		return []mgl32.Vec3{{0, 0, 0}}
	}
	return []mgl32.Vec3{{0, 0, 0}, {r, 0, 0}, {-r, 0, 0}, {0, 0, r}, {0, 0, -r}}
}

func CharacterMovementBlockHit(voxRt *VoxelRtState, basePos, move mgl32.Vec3, cfg CharacterCollisionConfig, acceptEntity func(EntityId, bool) bool) (CharacterCollisionHit, bool) {
	move[1] = 0
	if voxRt == nil || move.Len() <= 0 {
		return CharacterCollisionHit{}, false
	}
	cfg = effectiveCharacterCollisionConfig(cfg)
	dir := move.Normalize()
	dist := move.Len() + cfg.Radius
	for _, sampleY := range CharacterCollisionSampleHeights(cfg) {
		for _, offset := range CharacterCollisionSideOffsets(dir, cfg.Radius) {
			origin := basePos.Add(offset).Add(mgl32.Vec3{0, sampleY, 0})
			hit := voxRt.RaycastFiltered(origin, dir, dist, acceptEntity)
			if hit.Hit && hit.T <= dist && characterHorizontalHitBlocksMovement(hit.Normal) {
				return CharacterCollisionHit{
					RaycastHit: hit,
					SampleY:    sampleY,
					Offset:     offset,
				}, true
			}
		}
	}
	return CharacterCollisionHit{}, false
}

func characterHorizontalHitBlocksMovement(normal mgl32.Vec3) bool {
	return normal.Y() <= 0.35
}

func characterKinematicSlideMove(move, normal mgl32.Vec3) (mgl32.Vec3, bool) {
	move[1] = 0
	normal[1] = 0
	if move.LenSqr() <= 1e-8 || normal.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, false
	}
	normal = normal.Normalize()
	slide := move.Sub(normal.Mul(move.Dot(normal)))
	slide[1] = 0
	if slide.LenSqr() <= 1e-8 || slide.Dot(move) <= 0 {
		return mgl32.Vec3{}, false
	}
	if slide.Len() > move.Len() {
		slide = slide.Normalize().Mul(move.Len())
	}
	return slide, true
}

func CharacterDepenetrateInitialContacts(voxRt *VoxelRtState, basePos, intent mgl32.Vec3, cfg CharacterCollisionConfig, acceptEntity func(EntityId, bool) bool) CharacterDepenetrationResult {
	result := CharacterDepenetrationResult{Position: basePos}
	if voxRt == nil {
		return result
	}
	cfg = effectiveCharacterCollisionConfig(cfg)
	iterations := cfg.DepenetrationIterations
	if iterations <= 0 {
		iterations = 2
	}
	position := basePos
	total := mgl32.Vec3{}
	for i := 0; i < iterations; i++ {
		push, hit, ok := characterInitialContactPush(voxRt, position, intent, cfg, acceptEntity)
		if !ok || push.LenSqr() <= 1e-8 {
			break
		}
		if total.Add(push).Len() > cfg.MaxDepenetration {
			remaining := cfg.MaxDepenetration - total.Len()
			if remaining <= 0 {
				break
			}
			push = push.Normalize().Mul(remaining)
		}
		position = position.Add(push)
		total = total.Add(push)
		result.Hit = hit
		result.Iterations++
		result.Depenetrated = true
		if total.Len() >= cfg.MaxDepenetration {
			break
		}
	}
	result.Position = position
	result.Offset = total
	return result
}

func CharacterCollisionSampleHeights(cfg CharacterCollisionConfig) []float32 {
	cfg = effectiveCharacterCollisionConfig(cfg)
	return []float32{
		cfg.StepHeight * 0.5,
		cfg.Height * 0.5,
		maxCharacterCollisionFloat(cfg.Height-0.2, cfg.StepHeight),
	}
}

func CharacterCollisionSideOffsets(dir mgl32.Vec3, radius float32) []mgl32.Vec3 {
	offsets := []mgl32.Vec3{{0, 0, 0}}
	dir[1] = 0
	if dir.LenSqr() <= 1e-8 {
		return offsets
	}
	perp := mgl32.Vec3{-dir.Z(), 0, dir.X()}
	if perp.LenSqr() <= 1e-8 {
		return offsets
	}
	side := perp.Normalize().Mul(maxCharacterCollisionFloat(radius, 0))
	return append(offsets, side, side.Mul(-1))
}

func characterInitialContactPush(voxRt *VoxelRtState, basePos, intent mgl32.Vec3, cfg CharacterCollisionConfig, acceptEntity func(EntityId, bool) bool) (mgl32.Vec3, CharacterCollisionHit, bool) {
	directions := characterDepenetrationProbeDirections(intent)
	var push mgl32.Vec3
	var firstHit CharacterCollisionHit
	found := false
	for _, dir := range directions {
		move := dir.Mul(cfg.ContactEpsilon)
		hit, ok := CharacterMovementBlockHit(voxRt, basePos, move, cfg, acceptEntity)
		if !ok || hit.T > cfg.ContactEpsilon {
			continue
		}
		normal := hit.Normal
		normal[1] = 0
		if normal.LenSqr() <= 1e-8 {
			normal = dir.Mul(-1)
		}
		if normal.LenSqr() <= 1e-8 {
			continue
		}
		push = push.Add(normal.Normalize())
		if !found {
			firstHit = hit
			found = true
		}
	}
	if !found || push.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, CharacterCollisionHit{}, false
	}
	pushDistance := maxCharacterCollisionFloat(cfg.SkinWidth, cfg.Radius*0.12)
	pushDistance = minCharacterCollisionFloat(pushDistance, cfg.MaxDepenetration)
	return push.Normalize().Mul(pushDistance), firstHit, true
}

func characterDepenetrationProbeDirections(intent mgl32.Vec3) []mgl32.Vec3 {
	intent[1] = 0
	directions := make([]mgl32.Vec3, 0, 6)
	if intent.LenSqr() > 1e-8 {
		dir := intent.Normalize()
		directions = append(directions, dir, dir.Mul(-1))
		perp := mgl32.Vec3{-dir.Z(), 0, dir.X()}
		if perp.LenSqr() > 1e-8 {
			perp = perp.Normalize()
			directions = append(directions, perp, perp.Mul(-1))
		}
	}
	directions = append(directions,
		mgl32.Vec3{1, 0, 0},
		mgl32.Vec3{-1, 0, 0},
		mgl32.Vec3{0, 0, 1},
		mgl32.Vec3{0, 0, -1},
	)
	out := make([]mgl32.Vec3, 0, len(directions))
	for _, dir := range directions {
		if dir.LenSqr() <= 1e-8 {
			continue
		}
		dir = dir.Normalize()
		duplicate := false
		for _, existing := range out {
			if dir.Sub(existing).LenSqr() < 1e-6 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, dir)
		}
	}
	return out
}

func effectiveCharacterCollisionConfig(cfg CharacterCollisionConfig) CharacterCollisionConfig {
	if cfg.Radius <= 0 {
		cfg.Radius = 0.35
	}
	if cfg.Height <= 0 {
		cfg.Height = 1.8
	}
	if cfg.StepHeight <= 0 {
		cfg.StepHeight = 0.6
	}
	if cfg.SkinWidth <= 0 {
		cfg.SkinWidth = 0.04
	}
	if cfg.ContactEpsilon <= 0 {
		cfg.ContactEpsilon = 0.015
	}
	if cfg.MaxDepenetration <= 0 {
		cfg.MaxDepenetration = maxCharacterCollisionFloat(cfg.Radius*0.5, cfg.SkinWidth)
	}
	return cfg
}

func minCharacterCollisionFloat(a, b float32) float32 {
	return float32(math.Min(float64(a), float64(b)))
}

func maxCharacterCollisionFloat(a, b float32) float32 {
	return float32(math.Max(float64(a), float64(b)))
}

func defaultCharacterCollisionFloat(value, fallback float32) float32 {
	if value == 0 {
		return fallback
	}
	return value
}
