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
	SpawnFlags int
	SourceTag  string
	Tags       []string
}

type NPCAnimationComponent struct {
	State            string
	FallbackClipID   string
	ExplicitClipID   string
	RequestID        uint64
	AppliedRequestID uint64
	ActiveClipID     string
	Completed        bool
	CrossedEvents    []AnimationEvent
}

// RequestNPCAnimationClip selects an exact authored clip. Set restart when a
// same-clip replay is intentional.
func RequestNPCAnimationClip(animation *NPCAnimationComponent, clipID string, restart bool) bool {
	if animation == nil || clipID == "" {
		return false
	}
	if animation.ExplicitClipID != clipID || restart {
		animation.ExplicitClipID = clipID
		animation.RequestID++
	}
	return true
}

func ClearNPCAnimationClip(animation *NPCAnimationComponent) {
	if animation != nil && animation.ExplicitClipID != "" {
		animation.ExplicitClipID = ""
		animation.RequestID++
	}
}
