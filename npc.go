package gekko

const (
	NPCAnimationStateIdle   = "idle"
	NPCAnimationStateWalk   = "walk"
	NPCAnimationStateRun    = "run"
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
	State          string
	FallbackClipID string
	ActiveClipID   string
}
