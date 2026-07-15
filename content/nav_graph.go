package content

const (
	CurrentNavGraphManifestSchemaVersion = 1
	CurrentNavSourceTileSchemaVersion    = 2
	CurrentNavGraphTileSchemaVersion     = 1

	NavGraphManifestExtension = ".gknav"
	NavSourceTileExtension    = ".gknavsource"
	NavGraphTileExtension     = ".gknavgraph"

	NavTransitionWalk  = "walk"
	NavTransitionStep  = "step"
	NavTransitionStair = "stair"
)

type NavAgentProfileDef struct {
	ID              string  `json:"id"`
	Radius          float32 `json:"radius"`
	Height          float32 `json:"height"`
	StepHeight      float32 `json:"step_height"`
	MaxSlopeDegrees float32 `json:"max_slope_degrees"`
}

type NavSpanDef struct {
	ID              uint32   `json:"id"`
	X               int      `json:"x"`
	Y               int      `json:"y"`
	Z               int      `json:"z"`
	SupportHeight   float32  `json:"support_height"`
	CeilingHeight   float32  `json:"ceiling_height"`
	Headroom        float32  `json:"headroom"`
	ClearanceRadius float32  `json:"clearance_radius"`
	Area            string   `json:"area,omitempty"`
	Flags           []string `json:"flags,omitempty"`
}

type NavSpanRef struct {
	Tile TerrainChunkCoordDef `json:"tile"`
	Span uint32               `json:"span"`
}

type NavSpanTransitionDef struct {
	From          uint32     `json:"from"`
	To            NavSpanRef `json:"to"`
	Kind          string     `json:"kind"`
	StepDelta     float32    `json:"step_delta"`
	Width         float32    `json:"width"`
	MinHeadroom   float32    `json:"min_headroom"`
	MinClearance  float32    `json:"min_clearance"`
	Cost          float32    `json:"cost"`
	RequiresFlags []string   `json:"requires_flags,omitempty"`
}

type NavSpanRunDef struct {
	Start uint32 `json:"start"`
	Count uint32 `json:"count"`
}

// NavVoxelRunDef stores one vertical run of occupied source voxels.
type NavVoxelRunDef struct {
	X     int `json:"x"`
	Y     int `json:"y"`
	Z     int `json:"z"`
	Count int `json:"count"`
}

type NavRegionDef struct {
	ID        uint32          `json:"id"`
	SpanRuns  []NavSpanRunDef `json:"span_runs,omitempty"`
	BoundsMin Vec3            `json:"bounds_min"`
	BoundsMax Vec3            `json:"bounds_max"`
	Center    Vec3            `json:"center"`
	HeightMin float32         `json:"height_min"`
	HeightMax float32         `json:"height_max"`
	Area      string          `json:"area,omitempty"`
}

type NavRegionTransitionDef struct {
	ID            uint32               `json:"id"`
	FromRegion    uint32               `json:"from_region"`
	ToTile        TerrainChunkCoordDef `json:"to_tile"`
	ToRegion      uint32               `json:"to_region"`
	Kind          string               `json:"kind"`
	CrossingStart Vec3                 `json:"crossing_start"`
	CrossingEnd   Vec3                 `json:"crossing_end"`
	Width         float32              `json:"width"`
	MinHeadroom   float32              `json:"min_headroom"`
	MinClearance  float32              `json:"min_clearance"`
	Cost          float32              `json:"cost"`
	RequiresFlags []string             `json:"requires_flags,omitempty"`
}

type NavSourceTileDef struct {
	NavID          string               `json:"nav_id"`
	SchemaVersion  int                  `json:"schema_version"`
	Coord          TerrainChunkCoordDef `json:"coord"`
	BuilderVersion string               `json:"builder_version"`
	SourceHash     string               `json:"source_hash"`
	DependencyHash string               `json:"dependency_hash"`
	ChunkSize      int                  `json:"chunk_size"`
	SolidRuns      []NavVoxelRunDef     `json:"solid_runs,omitempty"`
	BlockedRuns    []NavVoxelRunDef     `json:"blocked_runs,omitempty"`
	Spans          []NavSpanDef         `json:"spans,omitempty"`
}

type NavGraphTileDef struct {
	NavID           string                   `json:"nav_id"`
	SchemaVersion   int                      `json:"schema_version"`
	Coord           TerrainChunkCoordDef     `json:"coord"`
	AgentProfileID  string                   `json:"agent_profile_id"`
	BuilderVersion  string                   `json:"builder_version"`
	SourceHash      string                   `json:"source_hash"`
	DependencyHash  string                   `json:"dependency_hash"`
	SpanIDs         []uint32                 `json:"span_ids,omitempty"`
	SpanTransitions []NavSpanTransitionDef   `json:"span_transitions,omitempty"`
	Regions         []NavRegionDef           `json:"regions,omitempty"`
	Transitions     []NavRegionTransitionDef `json:"transitions,omitempty"`
}

type NavSourceTileEntryDef struct {
	Coord          TerrainChunkCoordDef `json:"coord"`
	TilePath       string               `json:"tile_path"`
	SourceHash     string               `json:"source_hash"`
	DependencyHash string               `json:"dependency_hash"`
}

type NavGraphTileEntryDef struct {
	Coord          TerrainChunkCoordDef `json:"coord"`
	AgentProfileID string               `json:"agent_profile_id"`
	TilePath       string               `json:"tile_path"`
	SourceHash     string               `json:"source_hash"`
	DependencyHash string               `json:"dependency_hash"`
}

type NavGraphManifestDef struct {
	NavID           string                  `json:"nav_id"`
	SchemaVersion   int                     `json:"schema_version"`
	SourceWorldID   string                  `json:"source_world_id,omitempty"`
	BuilderVersion  string                  `json:"builder_version"`
	ChunkSize       int                     `json:"chunk_size"`
	VoxelResolution float32                 `json:"voxel_resolution"`
	AgentProfiles   []NavAgentProfileDef    `json:"agent_profiles,omitempty"`
	SourceTiles     []NavSourceTileEntryDef `json:"source_tiles,omitempty"`
	GraphTiles      []NavGraphTileEntryDef  `json:"graph_tiles,omitempty"`
}

type NavRouteStep struct {
	Tile            TerrainChunkCoordDef `json:"tile"`
	Region          uint32               `json:"region"`
	EnterTransition uint32               `json:"enter_transition"`
	Target          Vec3                 `json:"target"`
	RequiredAction  string               `json:"required_action,omitempty"`
}

type NavRouteResult struct {
	Found              bool                 `json:"found"`
	Steps              []NavRouteStep       `json:"steps,omitempty"`
	Waypoints          []Vec3               `json:"waypoints,omitempty"`
	FailureReason      string               `json:"failure_reason,omitempty"`
	FailureTile        TerrainChunkCoordDef `json:"failure_tile"`
	NavigationRevision uint64               `json:"navigation_revision"`
}

type NavPointResult struct {
	Found    bool       `json:"found"`
	Ref      NavSpanRef `json:"ref"`
	Region   uint32     `json:"region"`
	Point    Vec3       `json:"point"`
	Distance float32    `json:"distance"`
}
