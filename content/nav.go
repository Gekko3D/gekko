package content

const (
	CurrentNavManifestSchemaVersion = 1
	CurrentNavTileSchemaVersion     = 1

	DefaultNavBuilderVersion   = "voxel_nav_v2"
	DefaultNavAgentProfileID   = "hl1_standing"
	NavTilePayloadJSONV1       = "nav_tile_json_v1"
	NavTraversalWalk           = "walk"
	NavTraversalCrouch         = "crouch"
	NavTraversalJump           = "jump"
	NavTraversalDrop           = "drop"
	NavTraversalLadder         = "ladder"
	NavTraversalSwim           = "swim"
	NavTraversalDoor           = "door"
	NavTraversalMovingPlatform = "moving_platform"
)

type NavManifestDef struct {
	NavID           string               `json:"nav_id"`
	SchemaVersion   int                  `json:"schema_version"`
	LevelID         string               `json:"level_id,omitempty"`
	SourceWorldID   string               `json:"source_world_id,omitempty"`
	SourceLevelHash string               `json:"source_level_hash,omitempty"`
	BuilderVersion  string               `json:"builder_version,omitempty"`
	ChunkSize       int                  `json:"chunk_size"`
	VoxelResolution float32              `json:"voxel_resolution"`
	AgentProfiles   []NavAgentProfileDef `json:"agent_profiles,omitempty"`
	Tiles           []NavTileEntryDef    `json:"tiles,omitempty"`
	Sectors         []NavSectorEntryDef  `json:"sectors,omitempty"`
	Tags            []string             `json:"tags,omitempty"`
}

type NavAgentProfileDef struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	Radius          float32  `json:"radius"`
	Height          float32  `json:"height"`
	CrouchHeight    float32  `json:"crouch_height,omitempty"`
	StepHeight      float32  `json:"step_height"`
	MaxSlopeDegrees float32  `json:"max_slope_degrees"`
	MaxDropHeight   float32  `json:"max_drop_height,omitempty"`
	MaxJumpUp       float32  `json:"max_jump_up,omitempty"`
	MaxJumpDown     float32  `json:"max_jump_down,omitempty"`
	MaxJumpDistance float32  `json:"max_jump_distance,omitempty"`
	CanCrouch       bool     `json:"can_crouch,omitempty"`
	CanClimb        bool     `json:"can_climb,omitempty"`
	CanSwim         bool     `json:"can_swim,omitempty"`
	CanFly          bool     `json:"can_fly,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type NavTileEntryDef struct {
	Coord             TerrainChunkCoordDef `json:"coord"`
	AgentProfileID    string               `json:"agent_profile_id"`
	TilePath          string               `json:"tile_path"`
	PayloadKind       string               `json:"payload_kind,omitempty"`
	SourcePayloadHash string               `json:"source_payload_hash,omitempty"`
	SourceDeltaHash   string               `json:"source_delta_hash,omitempty"`
	NavBuildHash      string               `json:"nav_build_hash,omitempty"`
	BoundsMin         [3]float32           `json:"bounds_min"`
	BoundsMax         [3]float32           `json:"bounds_max"`
	Tags              []string             `json:"tags,omitempty"`
}

type NavSectorEntryDef struct {
	Coord              TerrainChunkCoordDef   `json:"coord"`
	BoundsMin          [3]float32             `json:"bounds_min"`
	BoundsMax          [3]float32             `json:"bounds_max"`
	AdjacentSectorRefs []TerrainChunkCoordDef `json:"adjacent_sector_refs,omitempty"`
	VisibleSectorRefs  []TerrainChunkCoordDef `json:"visible_sector_refs,omitempty"`
	Links              []NavSectorLinkDef     `json:"links,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
}

type NavSectorLinkDef struct {
	ID          string               `json:"id,omitempty"`
	To          TerrainChunkCoordDef `json:"to"`
	Kind        string               `json:"kind,omitempty"`
	Cost        float32              `json:"cost,omitempty"`
	TargetName  string               `json:"target_name,omitempty"`
	Openable    bool                 `json:"openable,omitempty"`
	RequiresTag string               `json:"requires_tag,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
}

type NavTileDef struct {
	NavID             string               `json:"nav_id"`
	SchemaVersion     int                  `json:"schema_version"`
	Coord             TerrainChunkCoordDef `json:"coord"`
	AgentProfileID    string               `json:"agent_profile_id"`
	BuilderVersion    string               `json:"builder_version,omitempty"`
	PayloadKind       string               `json:"payload_kind,omitempty"`
	SourcePayloadHash string               `json:"source_payload_hash,omitempty"`
	SourceDeltaHash   string               `json:"source_delta_hash,omitempty"`
	NavBuildHash      string               `json:"nav_build_hash,omitempty"`
	BoundsMin         [3]float32           `json:"bounds_min"`
	BoundsMax         [3]float32           `json:"bounds_max"`
	Vertices          []Vec3               `json:"vertices,omitempty"`
	Polygons          []NavPolygonDef      `json:"polygons,omitempty"`
	OffMeshLinks      []NavOffMeshLinkDef  `json:"off_mesh_links,omitempty"`
	Tags              []string             `json:"tags,omitempty"`
}

type NavPolygonDef struct {
	ID        string   `json:"id,omitempty"`
	Vertices  []int    `json:"vertices,omitempty"`
	Area      string   `json:"area,omitempty"`
	Flags     []string `json:"flags,omitempty"`
	Neighbors []string `json:"neighbors,omitempty"`
}

type NavOffMeshLinkDef struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Start         Vec3     `json:"start"`
	End           Vec3     `json:"end"`
	Radius        float32  `json:"radius,omitempty"`
	Cost          float32  `json:"cost,omitempty"`
	Bidirectional bool     `json:"bidirectional,omitempty"`
	TargetName    string   `json:"target_name,omitempty"`
	Openable      bool     `json:"openable,omitempty"`
	RequiresTag   string   `json:"requires_tag,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

func NewNavManifestDef(navID string) *NavManifestDef {
	def := &NavManifestDef{
		NavID:           navID,
		SchemaVersion:   CurrentNavManifestSchemaVersion,
		BuilderVersion:  DefaultNavBuilderVersion,
		ChunkSize:       32,
		VoxelResolution: 1,
		AgentProfiles:   []NavAgentProfileDef{DefaultHL1NavAgentProfile()},
	}
	EnsureNavManifestDefaults(def)
	return def
}

func DefaultHL1NavAgentProfile() NavAgentProfileDef {
	return NavAgentProfileDef{
		ID:              DefaultNavAgentProfileID,
		Name:            "HL1 standing hull",
		Radius:          0.4064,
		Height:          1.8288,
		CrouchHeight:    0.9144,
		StepHeight:      0.4572,
		MaxSlopeDegrees: 45,
		MaxDropHeight:   0.9144,
		CanCrouch:       true,
		CanClimb:        true,
		CanSwim:         true,
	}
}

func EnsureNavManifestDefaults(def *NavManifestDef) {
	if def == nil {
		return
	}
	if def.NavID == "" {
		def.NavID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavManifestSchemaVersion
	}
	if def.BuilderVersion == "" {
		def.BuilderVersion = DefaultNavBuilderVersion
	}
	if def.ChunkSize == 0 {
		def.ChunkSize = 32
	}
	if def.VoxelResolution == 0 {
		def.VoxelResolution = 1
	}
	for i := range def.AgentProfiles {
		EnsureNavAgentProfileDefaults(&def.AgentProfiles[i])
	}
	for i := range def.Tiles {
		EnsureNavTileEntryDefaults(&def.Tiles[i])
	}
	for i := range def.Sectors {
		EnsureNavSectorDefaults(&def.Sectors[i])
	}
}

func EnsureNavAgentProfileDefaults(profile *NavAgentProfileDef) {
	if profile == nil {
		return
	}
	if profile.ID == "" {
		profile.ID = DefaultNavAgentProfileID
	}
	defaults := DefaultHL1NavAgentProfile()
	if profile.Radius == 0 {
		profile.Radius = defaults.Radius
	}
	if profile.Height == 0 {
		profile.Height = defaults.Height
	}
	if profile.CrouchHeight == 0 && profile.CanCrouch {
		profile.CrouchHeight = defaults.CrouchHeight
	}
	if profile.StepHeight == 0 {
		profile.StepHeight = defaults.StepHeight
	}
	if profile.MaxSlopeDegrees == 0 {
		profile.MaxSlopeDegrees = defaults.MaxSlopeDegrees
	}
}

func EnsureNavTileEntryDefaults(entry *NavTileEntryDef) {
	if entry == nil {
		return
	}
	if entry.AgentProfileID == "" {
		entry.AgentProfileID = DefaultNavAgentProfileID
	}
	if entry.PayloadKind == "" {
		entry.PayloadKind = NavTilePayloadJSONV1
	}
}

func EnsureNavSectorDefaults(sector *NavSectorEntryDef) {
	if sector == nil {
		return
	}
	for i := range sector.Links {
		if sector.Links[i].Kind == "" {
			sector.Links[i].Kind = NavTraversalWalk
		}
	}
}

func EnsureNavTileDefaults(def *NavTileDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavTileSchemaVersion
	}
	if def.AgentProfileID == "" {
		def.AgentProfileID = DefaultNavAgentProfileID
	}
	if def.BuilderVersion == "" {
		def.BuilderVersion = DefaultNavBuilderVersion
	}
	if def.PayloadKind == "" {
		def.PayloadKind = NavTilePayloadJSONV1
	}
	for i := range def.OffMeshLinks {
		if def.OffMeshLinks[i].Kind == "" {
			def.OffMeshLinks[i].Kind = NavTraversalWalk
		}
	}
}
