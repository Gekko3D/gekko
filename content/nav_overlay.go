package content

const (
	NavDynamicOverlayEntryKindLink       = "link"
	NavDynamicOverlayEntryKindTraversal  = "traversal"
	NavDynamicOverlayEntryKindSectorPair = "sector_pair"
	NavDynamicOverlayEntryKindBounds     = "bounds"
)

type NavDynamicOverlayDef struct {
	Entries []NavDynamicOverlayEntryDef
}

type NavDynamicOverlayEntryDef struct {
	ID             string
	Kind           string
	LinkID         string
	TargetName     string
	TraversalKind  string
	From           TerrainChunkCoordDef
	To             TerrainChunkCoordDef
	BoundsMin      Vec3
	BoundsMax      Vec3
	Blocked        bool
	CostAdd        float32
	CostMultiplier float32
	Action         string
	Reason         string
}

type NavTraversalActionDef struct {
	ID         string
	Kind       string
	LinkID     string
	TargetName string
	Action     string
	Reason     string
	From       TerrainChunkCoordDef
	To         TerrainChunkCoordDef
}
