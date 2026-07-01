package content

const (
	CurrentNavBuildSourceSchemaVersion         = 1
	CurrentNavClearanceSourceTileSchemaVersion = 1

	NavBuildSourceKindGeneric           = "generic_nav_build_source"
	NavClearanceSourceTilePayloadJSONV1 = "nav_clearance_source_tile_json_v1"
	NavClearanceSourceKindVoxelSpans    = "voxel_span_clearance"

	NavBuildSurfaceWalkable         = "walkable"
	NavBuildSurfaceClearanceBlocker = "clearance_blocker"

	NavBuildVolumeLadder = "ladder"
	NavBuildVolumeWater  = "water"

	NavBuildConnectorDoor           = "door"
	NavBuildConnectorJump           = "jump"
	NavBuildConnectorDrop           = "drop"
	NavBuildConnectorMovingPlatform = "moving_platform"
)

type NavBuildSourceDef struct {
	SourceID      string                 `json:"source_id,omitempty"`
	SchemaVersion int                    `json:"schema_version"`
	Kind          string                 `json:"kind,omitempty"`
	SourceWorldID string                 `json:"source_world_id,omitempty"`
	SourceHash    string                 `json:"source_hash,omitempty"`
	BoundsMin     Vec3                   `json:"bounds_min,omitempty"`
	BoundsMax     Vec3                   `json:"bounds_max,omitempty"`
	Surfaces      []NavBuildSurfaceDef   `json:"surfaces,omitempty"`
	Volumes       []NavBuildVolumeDef    `json:"volumes,omitempty"`
	Connectors    []NavBuildConnectorDef `json:"connectors,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
}

type NavClearanceSourceTileDef struct {
	NavID              string                      `json:"nav_id"`
	SchemaVersion      int                         `json:"schema_version"`
	Coord              TerrainChunkCoordDef        `json:"coord"`
	Kind               string                      `json:"kind,omitempty"`
	BuilderVersion     string                      `json:"builder_version,omitempty"`
	PayloadKind        string                      `json:"payload_kind,omitempty"`
	SourcePayloadHash  string                      `json:"source_payload_hash,omitempty"`
	SourceDeltaHash    string                      `json:"source_delta_hash,omitempty"`
	NavBuildHash       string                      `json:"nav_build_hash,omitempty"`
	BoundsMin          [3]float32                  `json:"bounds_min"`
	BoundsMax          [3]float32                  `json:"bounds_max"`
	VoxelResolution    float32                     `json:"voxel_resolution"`
	MaxClearanceRadius float32                     `json:"max_clearance_radius,omitempty"`
	Cells              []NavClearanceSourceCellDef `json:"cells,omitempty"`
	Tags               []string                    `json:"tags,omitempty"`
}

type NavClearanceSourceCellDef struct {
	X               int      `json:"x"`
	Y               int      `json:"y"`
	Z               int      `json:"z"`
	Position        Vec3     `json:"position"`
	Headroom        float32  `json:"headroom"`
	ClearanceRadius float32  `json:"clearance_radius"`
	SlopeDegrees    float32  `json:"slope_degrees,omitempty"`
	Area            string   `json:"area,omitempty"`
	Flags           []string `json:"flags,omitempty"`
}

type NavBuildSurfaceDef struct {
	ID        string   `json:"id,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Vertices  []Vec3   `json:"vertices,omitempty"`
	Indices   []int    `json:"indices,omitempty"`
	Normal    Vec3     `json:"normal,omitempty"`
	Area      string   `json:"area,omitempty"`
	Flags     []string `json:"flags,omitempty"`
	SourceTag string   `json:"source_tag,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

type NavBuildExplicitSurfaceInput struct {
	ID        string   `json:"id,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Vertices  []Vec3   `json:"vertices,omitempty"`
	Normal    Vec3     `json:"normal,omitempty"`
	Area      string   `json:"area,omitempty"`
	Flags     []string `json:"flags,omitempty"`
	SourceTag string   `json:"source_tag,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

type NavBuildVolumeDef struct {
	ID          string   `json:"id,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	BoundsMin   Vec3     `json:"bounds_min,omitempty"`
	BoundsMax   Vec3     `json:"bounds_max,omitempty"`
	TargetName  string   `json:"target_name,omitempty"`
	RequiresTag string   `json:"requires_tag,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type NavBuildConnectorDef struct {
	ID            string   `json:"id,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Start         Vec3     `json:"start,omitempty"`
	End           Vec3     `json:"end,omitempty"`
	Radius        float32  `json:"radius,omitempty"`
	Cost          float32  `json:"cost,omitempty"`
	Bidirectional bool     `json:"bidirectional,omitempty"`
	TargetName    string   `json:"target_name,omitempty"`
	Openable      bool     `json:"openable,omitempty"`
	RequiresTag   string   `json:"requires_tag,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

func EnsureNavBuildSourceDefaults(def *NavBuildSourceDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavBuildSourceSchemaVersion
	}
	if def.Kind == "" {
		def.Kind = NavBuildSourceKindGeneric
	}
	for i := range def.Surfaces {
		EnsureNavBuildSurfaceDefaults(&def.Surfaces[i])
	}
	for i := range def.Connectors {
		if def.Connectors[i].Kind == "" {
			def.Connectors[i].Kind = NavBuildConnectorDoor
		}
	}
}

func EnsureNavClearanceSourceTileDefaults(def *NavClearanceSourceTileDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentNavClearanceSourceTileSchemaVersion
	}
	if def.Kind == "" {
		def.Kind = NavClearanceSourceKindVoxelSpans
	}
	if def.BuilderVersion == "" {
		def.BuilderVersion = DefaultNavBuilderVersion
	}
	if def.PayloadKind == "" {
		def.PayloadKind = NavClearanceSourceTilePayloadJSONV1
	}
	for i := range def.Cells {
		if def.Cells[i].Area == "" {
			def.Cells[i].Area = NavTraversalWalk
		}
	}
}

func EnsureNavBuildSurfaceDefaults(surface *NavBuildSurfaceDef) {
	if surface == nil {
		return
	}
	if surface.Kind == "" {
		surface.Kind = NavBuildSurfaceWalkable
	}
	if surface.Area == "" {
		surface.Area = NavTraversalWalk
	}
}

func NavClearanceSourceCellSupportsAgent(cell NavClearanceSourceCellDef, profile NavAgentProfileDef) bool {
	EnsureNavAgentProfileDefaults(&profile)
	return navClearanceSourceCellSupportsNormalizedAgent(cell, profile)
}

func navClearanceSourceCellSupportsNormalizedAgent(cell NavClearanceSourceCellDef, profile NavAgentProfileDef) bool {
	return cell.Headroom+1e-4 >= profile.Height && cell.ClearanceRadius+1e-4 >= profile.Radius && cell.SlopeDegrees <= profile.MaxSlopeDegrees+1e-4
}
