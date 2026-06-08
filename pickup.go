package gekko

type PickupComponent struct {
	Kind       string
	AssetPath  string
	Category   string
	Item       string
	Amount     int
	ClassName  string
	TargetName string
	SpawnFlags int
	SourceTag  string
	Tags       []string
}
