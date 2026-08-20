package gekko

const (
	NPCAnimationStateIdle   = "idle"
	NPCAnimationStateWalk   = "walk"
	NPCAnimationStateRun    = "run"
	NPCAnimationStateLadder = "ladder"
	NPCAnimationStateAttack = "attack"
	NPCAnimationStatePain   = "pain"
	NPCAnimationStateDeath  = "death"
)

type NPCComponent struct {
	Kind       string
	AssetPath  string
	ClassName  string
	ModelRef   string
	Health     float32
	MaxHealth  float32
	TargetName string
	Target     string
	SquadName  string
	Weapons    int
	SpawnFlags int
	SourceTag  string
	Tags       []string
}

type NPCAnimationComponent struct {
	State                   string
	FallbackClipID          string
	ExplicitClipID          string
	ExplicitPlaybackSpeed   float32
	LocomotionClipID        string
	LocomotionPlaybackSpeed float32
	RequestID               uint64
	AppliedRequestID        uint64
	FailedRequestID         uint64
	FailedClipID            string
	FailureReason           string
	ActiveClipID            string
	IdleVariantClipID       string
	IdleVariantAnchorClipID string
	IdleVariantSequence     uint64
	BlendValue              float32
	HasBlendValue           bool
	Completed               bool
	CrossedEvents           []AnimationEvent
}

// SetNPCAnimationBlend changes the current clip pose without restarting its
// timeline. Clips without a one-dimensional blend ignore the value.
func SetNPCAnimationBlend(animation *NPCAnimationComponent, value float32) {
	if animation != nil {
		animation.BlendValue, animation.HasBlendValue = value, true
	}
}

// RequestNPCAnimationClip selects an exact authored clip. Set restart when a
// same-clip replay is intentional.
func RequestNPCAnimationClip(animation *NPCAnimationComponent, clipID string, restart bool) bool {
	return RequestNPCAnimationClipSpeed(animation, clipID, 1, restart)
}

// RequestNPCAnimationClipSpeed selects an exact authored clip at a specific
// speed. Negative playback begins at the end, which lets authored one-shots
// serve as their own reverse transition.
func RequestNPCAnimationClipSpeed(animation *NPCAnimationComponent, clipID string, speed float32, restart bool) bool {
	if animation == nil || clipID == "" {
		return false
	}
	if animation.ExplicitClipID != clipID || restart {
		animation.ExplicitClipID, animation.ExplicitPlaybackSpeed = clipID, speed
		animation.RequestID++
	} else {
		animation.ExplicitPlaybackSpeed = speed
	}
	return true
}

func ClearNPCAnimationClip(animation *NPCAnimationComponent) {
	if animation != nil {
		animation.HasBlendValue, animation.ExplicitPlaybackSpeed = false, 1
		if animation.ExplicitClipID != "" {
			animation.ExplicitClipID = ""
			animation.RequestID++
		}
	}
}
