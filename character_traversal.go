package gekko

import "github.com/go-gl/mathgl/mgl32"

type CharacterMotionMode string

const (
	CharacterMotionNormal    CharacterMotionMode = "normal"
	CharacterMotionBallistic CharacterMotionMode = "ballistic"
	CharacterMotionLadder    CharacterMotionMode = "ladder"
	CharacterMotionKinematic CharacterMotionMode = "kinematic"
)

type CharacterTraversalKind string

const (
	CharacterTraversalDrop   CharacterTraversalKind = "drop"
	CharacterTraversalJump   CharacterTraversalKind = "jump"
	CharacterTraversalLadder CharacterTraversalKind = "ladder"
	CharacterTraversalVault  CharacterTraversalKind = "vault"
	CharacterTraversalMantle CharacterTraversalKind = "mantle"
)

type CharacterTraversalPhase string

const (
	CharacterTraversalPhaseAlign    CharacterTraversalPhase = "align"
	CharacterTraversalPhaseTraverse CharacterTraversalPhase = "traverse"
	CharacterTraversalPhaseLift     CharacterTraversalPhase = "lift"
	CharacterTraversalPhaseCross    CharacterTraversalPhase = "cross"
	CharacterTraversalPhaseLand     CharacterTraversalPhase = "land"
	CharacterTraversalPhaseClimb    CharacterTraversalPhase = "climb"
	CharacterTraversalPhaseDismount CharacterTraversalPhase = "dismount"
	CharacterTraversalPhaseSettle   CharacterTraversalPhase = "settle"
)

type CharacterTraversalStatus string

const (
	CharacterTraversalInactive  CharacterTraversalStatus = ""
	CharacterTraversalActive    CharacterTraversalStatus = "active"
	CharacterTraversalSucceeded CharacterTraversalStatus = "succeeded"
	CharacterTraversalFailed    CharacterTraversalStatus = "failed"
)

type CharacterTraversalRequest struct {
	RequestID    uint64
	LinkID       string
	OwnerID      string
	Kind         CharacterTraversalKind
	Start        mgl32.Vec3
	Entry        mgl32.Vec3
	Apex         mgl32.Vec3
	End          mgl32.Vec3
	Speed        float32
	Duration     float32
	Acceptance   float32
	LaunchSpeed  float32
	MaxDuration  float32
	LadderEntity EntityId
	Manual       bool
}

// CharacterTraversalComponent is embedded by the grounded motor. It is the
// sole owner of special movement and retains one terminal result until the
// next request.
type CharacterTraversalComponent struct {
	Request        CharacterTraversalRequest
	Sequence       uint64
	Phase          CharacterTraversalPhase
	Status         CharacterTraversalStatus
	Reason         string
	PendingReason  string
	Elapsed        float32
	PhaseElapsed   float32
	BlockedElapsed float32
	Committed      bool
	WasAirborne    bool
	Blocked        bool
}

func (traversal CharacterTraversalComponent) Running() bool {
	return traversal.Status == CharacterTraversalActive
}

func CharacterBeginTraversal(ctrl *GroundedCharacterMotorComponent, request CharacterTraversalRequest) bool {
	if ctrl == nil || ctrl.Traversal.Running() {
		return false
	}
	switch request.Kind {
	case CharacterTraversalDrop, CharacterTraversalJump, CharacterTraversalLadder, CharacterTraversalVault, CharacterTraversalMantle:
	default:
		return false
	}
	if request.Kind == CharacterTraversalJump && !ctrl.Grounded {
		return false
	}
	sequence := ctrl.Traversal.Sequence + 1
	if sequence == 0 {
		sequence++
	}
	if request.RequestID == 0 {
		request.RequestID = sequence
	}
	if request.Acceptance <= 0 {
		request.Acceptance = maxCharacterCollisionFloat(ctrl.Radius, 0.25)
		if request.Kind == CharacterTraversalLadder {
			request.Acceptance = 0.01
		}
	}
	if request.Speed <= 0 {
		request.Speed = defaultCharacterCollisionFloat(ctrl.Speed, 1.8)
	}
	if request.MaxDuration <= 0 && !request.Manual {
		request.MaxDuration = maxCharacterCollisionFloat(8, request.End.Sub(request.Start).Len()/request.Speed*3+2)
	}
	if request.Kind == CharacterTraversalJump && request.LaunchSpeed <= 0 {
		request.LaunchSpeed = defaultCharacterCollisionFloat(ctrl.JumpSpeed, 5.5)
	}
	ctrl.Traversal = CharacterTraversalComponent{
		Request: request, Sequence: sequence, Phase: CharacterTraversalPhaseAlign,
		Status: CharacterTraversalActive,
	}
	switch request.Kind {
	case CharacterTraversalDrop, CharacterTraversalJump:
		ctrl.MotionMode = CharacterMotionBallistic
	case CharacterTraversalLadder:
		ctrl.MotionMode = CharacterMotionLadder
	default:
		ctrl.MotionMode = CharacterMotionKinematic
	}
	return true
}

// CharacterAbortTraversal is reserved for hard ownership changes such as
// death, teleport, or pausing the actor.
func CharacterAbortTraversal(ctrl *GroundedCharacterMotorComponent, reason string) {
	if ctrl == nil || !ctrl.Traversal.Running() {
		return
	}
	if reason == "" {
		reason = "aborted"
	}
	ctrl.Traversal.Status = CharacterTraversalFailed
	ctrl.Traversal.Reason = reason
	ctrl.MotionMode = CharacterMotionNormal
	ctrl.OnLadder = false
	ctrl.LadderEntity = 0
}

// CharacterTraversalConfig describes a validated climb over an obstacle.
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
// a blocking obstacle. CharacterBeginTraversal executes the returned target.
func CharacterFindTraversalTarget(voxRt *VoxelRtState, start, forward mgl32.Vec3, opts CharacterTraversalConfig, acceptEntity func(EntityId, bool) bool) (CharacterTraversalTarget, bool) {
	if !characterCollisionAvailable(voxRt, opts.CollisionConfig.DynamicCollisionQuery) {
		return CharacterTraversalTarget{}, false
	}
	forward[1] = 0
	if forward.LenSqr() <= 1e-8 {
		return CharacterTraversalTarget{}, false
	}
	forward = forward.Normalize()
	collision := effectiveCharacterCollisionConfig(opts.CollisionConfig)
	ground := opts.GroundConfig
	if ground.DynamicCollisionQuery == nil {
		ground.DynamicCollisionQuery = collision.DynamicCollisionQuery
	}
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
	if !characterCollisionAvailable(voxRt, cfg.DynamicCollisionQuery) {
		return true
	}
	cfg = effectiveCharacterCollisionConfig(cfg)
	height := cfg.Height - 0.05
	if height <= 0 {
		return true
	}
	for _, offset := range CharacterVerticalCollisionOffsets(cfg.Radius) {
		origin := basePos.Add(offset).Add(mgl32.Vec3{0, 0.03, 0})
		hit := characterRaycastFiltered(voxRt, origin, mgl32.Vec3{0, 1, 0}, height, acceptEntity, cfg.DynamicCollisionQuery)
		if hit.Hit && hit.T <= height {
			return false
		}
	}
	return true
}
