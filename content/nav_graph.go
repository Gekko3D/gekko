package content

import (
	"fmt"
	"math"
)

const (
	CurrentNavGraphManifestSchemaVersion = 5
	CurrentNavSourceTileSchemaVersion    = 3
	CurrentNavGraphTileSchemaVersion     = 5

	NavGraphManifestExtension = ".gknav"
	NavSourceTileExtension    = ".gkns"
	NavGraphTileExtension     = ".gkng"

	NavTransitionWalk    = "walk"
	NavTransitionStep    = "step"
	NavTransitionStair   = "stair"
	NavTransitionDrop    = "drop"
	NavTransitionJump    = "jump"
	NavTransitionLadder  = "ladder"
	NavTransitionVault   = "vault"
	NavTransitionMantle  = "mantle"
	NavTransitionCarrier = "carrier"
	NavGateDoor          = "door"

	NavigationRoleDoor    = "door"
	NavigationRoleCarrier = "carrier"
	NavCarrierBoardDrop   = "drop"

	NavCapabilityClimbLadder = "climb_ladder"
	NavCapabilityJump        = "jump"
	NavCapabilityVault       = "vault"
	NavCapabilityMantle      = "mantle"
)

type NavAgentProfileDef struct {
	ID                  string   `json:"id"`
	Radius              float32  `json:"radius"`
	Height              float32  `json:"height"`
	StepHeight          float32  `json:"step_height"`
	MaxSlopeDegrees     float32  `json:"max_slope_degrees"`
	MaxDropHeight       float32  `json:"max_drop_height,omitempty"`
	MaxJumpDistance     float32  `json:"max_jump_distance,omitempty"`
	MaxJumpRise         float32  `json:"max_jump_rise,omitempty"`
	MaxVaultHeight      float32  `json:"max_vault_height,omitempty"`
	MaxMantleHeight     float32  `json:"max_mantle_height,omitempty"`
	JumpSpeed           float32  `json:"jump_speed,omitempty"`
	JumpHorizontalSpeed float32  `json:"jump_horizontal_speed,omitempty"`
	Gravity             float32  `json:"gravity,omitempty"`
	Capabilities        []string `json:"capabilities,omitempty"`
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

// NavTraversalDef binds special movement to its authored owner and gives
// locomotion explicit world-space entry and exit points.
type NavTraversalDef struct {
	LinkID      string                  `json:"link_id,omitempty"`
	OwnerID     string                  `json:"owner_id,omitempty"`
	Start       Vec3                    `json:"start"`
	Apex        Vec3                    `json:"apex,omitempty"`
	End         Vec3                    `json:"end"`
	Speed       float32                 `json:"speed,omitempty"`
	LaunchSpeed float32                 `json:"launch_speed,omitempty"`
	Duration    float32                 `json:"duration,omitempty"`
	Carrier     *NavCarrierTraversalDef `json:"carrier,omitempty"`
}

// NavCarrierTraversalDef describes the moving-support portion of one directed
// station-to-station traversal. Start and End remain static graph points.
type NavCarrierTraversalDef struct {
	CarrierID        string `json:"carrier_id"`
	FromStop         string `json:"from_stop"`
	ToStop           string `json:"to_stop"`
	Board            Vec3   `json:"board"`
	BoardMode        string `json:"board_mode,omitempty"`
	CallControllerID string `json:"call_controller_id,omitempty"`
	ControllerID     string `json:"controller_id,omitempty"`
}

func (traversal NavTraversalDef) StableLinkID() string {
	return traversal.LinkID
}

func (traversal NavTraversalDef) Owner() string {
	return traversal.OwnerID
}

func navTraversalLinkID(kind, owner string, from, to NavSpanRef) string {
	return fmt.Sprintf("%s:%s:%s/%d>%s/%d", kind, owner, TerrainChunkKey(from.Tile), from.Span, TerrainChunkKey(to.Tile), to.Span)
}

// NavTraversalSupportedByProfile is the shared physical eligibility check
// used by bakers, validators, and runtime overlays.
func NavTraversalSupportedByProfile(profile NavAgentProfileDef, kind string, traversal *NavTraversalDef) bool {
	if traversal == nil {
		return !navTransitionRequiresTraversal(kind)
	}
	const epsilon = float32(1e-4)
	rise := traversal.End[1] - traversal.Start[1]
	horizontal := float32(math.Hypot(
		float64(traversal.End[0]-traversal.Start[0]),
		float64(traversal.End[2]-traversal.Start[2]),
	))
	switch kind {
	case NavTransitionDrop:
		drop := -rise
		return drop > epsilon && (profile.MaxDropHeight <= 0 || drop <= profile.MaxDropHeight+epsilon)
	case NavTransitionJump:
		if !navProfileHasCapability(profile, NavCapabilityJump) ||
			profile.MaxJumpDistance > 0 && horizontal > profile.MaxJumpDistance+epsilon ||
			profile.MaxJumpRise > 0 && rise > profile.MaxJumpRise+epsilon ||
			profile.MaxDropHeight > 0 && -rise > profile.MaxDropHeight+epsilon {
			return false
		}
		launchSpeed := profile.JumpSpeed
		if traversal.LaunchSpeed > 0 {
			if profile.JumpSpeed > 0 && traversal.LaunchSpeed > profile.JumpSpeed+epsilon {
				return false
			}
			launchSpeed = traversal.LaunchSpeed
		}
		if launchSpeed <= 0 || profile.Gravity <= 0 {
			return true
		}
		discriminant := launchSpeed*launchSpeed - 2*profile.Gravity*rise
		if discriminant < 0 {
			return false
		}
		duration := (launchSpeed + float32(math.Sqrt(float64(discriminant)))) / profile.Gravity
		requiredSpeed := horizontal / duration
		return (profile.JumpHorizontalSpeed <= 0 || requiredSpeed <= profile.JumpHorizontalSpeed+epsilon) &&
			(traversal.Speed <= 0 || profile.JumpHorizontalSpeed <= 0 || traversal.Speed <= profile.JumpHorizontalSpeed+epsilon)
	case NavTransitionVault, NavTransitionMantle:
		capability, limit := NavCapabilityVault, profile.MaxVaultHeight
		if kind == NavTransitionMantle {
			capability, limit = NavCapabilityMantle, profile.MaxMantleHeight
		}
		if !navProfileHasCapability(profile, capability) || traversal.Apex == (Vec3{}) {
			return false
		}
		height := max(float32(0), traversal.End[1]-traversal.Start[1], traversal.Apex[1]-traversal.Start[1])
		return limit <= 0 || height <= limit+epsilon
	default:
		return true
	}
}

// NavTransitionGateDef binds a movement transition to gameplay state that
// must become traversable before locomotion executes the movement.
type NavTransitionGateDef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type NavSpanTransitionDef struct {
	From          uint32                `json:"from"`
	To            NavSpanRef            `json:"to"`
	Kind          string                `json:"kind"`
	StepDelta     float32               `json:"step_delta"`
	Width         float32               `json:"width"`
	MinHeadroom   float32               `json:"min_headroom"`
	MinClearance  float32               `json:"min_clearance"`
	Cost          float32               `json:"cost"`
	RequiresFlags []string              `json:"requires_flags,omitempty"`
	Traversal     *NavTraversalDef      `json:"traversal,omitempty"`
	Gate          *NavTransitionGateDef `json:"gate,omitempty"`
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
	ID            uint32                `json:"id"`
	FromRegion    uint32                `json:"from_region"`
	ToTile        TerrainChunkCoordDef  `json:"to_tile"`
	ToRegion      uint32                `json:"to_region"`
	Kind          string                `json:"kind"`
	CrossingStart Vec3                  `json:"crossing_start"`
	CrossingEnd   Vec3                  `json:"crossing_end"`
	Width         float32               `json:"width"`
	MinHeadroom   float32               `json:"min_headroom"`
	MinClearance  float32               `json:"min_clearance"`
	Cost          float32               `json:"cost"`
	RequiresFlags []string              `json:"requires_flags,omitempty"`
	Traversal     *NavTraversalDef      `json:"traversal,omitempty"`
	Gate          *NavTransitionGateDef `json:"gate,omitempty"`
}

type NavSourceTileDef struct {
	NavID           string               `json:"nav_id"`
	SchemaVersion   int                  `json:"schema_version"`
	Coord           TerrainChunkCoordDef `json:"coord"`
	BuilderVersion  string               `json:"builder_version"`
	SourceHash      string               `json:"source_hash"`
	DependencyHash  string               `json:"dependency_hash"`
	ChunkSize       int                  `json:"chunk_size"`
	VoxelResolution float32              `json:"voxel_resolution"`
	SolidRuns       []NavVoxelRunDef     `json:"solid_runs,omitempty"`
	BlockedRuns     []NavVoxelRunDef     `json:"blocked_runs,omitempty"`
	Spans           []NavSpanDef         `json:"spans,omitempty"`
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
	ContentHash    string               `json:"content_hash"`
	ByteSize       int64                `json:"byte_size"`
}

type NavGraphTileEntryDef struct {
	Coord          TerrainChunkCoordDef `json:"coord"`
	AgentProfileID string               `json:"agent_profile_id"`
	TilePath       string               `json:"tile_path"`
	SourceHash     string               `json:"source_hash"`
	DependencyHash string               `json:"dependency_hash"`
	ContentHash    string               `json:"content_hash"`
	ByteSize       int64                `json:"byte_size"`
}

// NavDoorDef is the baked, format-neutral closed footprint for one authored
// moving brush that owns door traversal state at runtime.
type NavDoorDef struct {
	ID                string `json:"id"`
	Group             string `json:"group,omitempty"`
	BoundsCenter      Vec3   `json:"bounds_center"`
	BoundsHalfExtents Vec3   `json:"bounds_half_extents"`
	OpenOffset        Vec3   `json:"open_offset,omitempty"`
}

type NavCarrierControllerDef struct {
	ID                string `json:"id"`
	BoundsCenter      Vec3   `json:"bounds_center"`
	BoundsHalfExtents Vec3   `json:"bounds_half_extents"`
}

type NavCarrierStopDef struct {
	ID           string                    `json:"id"`
	BoundsCenter Vec3                      `json:"bounds_center"`
	Controllers  []NavCarrierControllerDef `json:"controllers,omitempty"`
}

// NavCarrierDef is a format-neutral moving support with discrete stops.
// Current moving brushes emit closed/open stops; the graph and runtime contract
// intentionally supports more stops without changing route execution.
type NavCarrierDef struct {
	ID                string              `json:"id"`
	Group             string              `json:"group,omitempty"`
	BoundsHalfExtents Vec3                `json:"bounds_half_extents"`
	Speed             float32             `json:"speed,omitempty"`
	Wait              float32             `json:"wait,omitempty"`
	Stops             []NavCarrierStopDef `json:"stops"`
}

type NavGraphManifestDef struct {
	NavID           string                  `json:"nav_id"`
	SchemaVersion   int                     `json:"schema_version"`
	SourceWorldID   string                  `json:"source_world_id,omitempty"`
	BuilderVersion  string                  `json:"builder_version"`
	ChunkSize       int                     `json:"chunk_size"`
	VoxelResolution float32                 `json:"voxel_resolution"`
	AgentProfiles   []NavAgentProfileDef    `json:"agent_profiles,omitempty"`
	LadderVolumes   []LevelLadderVolumeDef  `json:"ladder_volumes,omitempty"`
	Doors           []NavDoorDef            `json:"doors,omitempty"`
	Carriers        []NavCarrierDef         `json:"carriers,omitempty"`
	SourceTiles     []NavSourceTileEntryDef `json:"source_tiles,omitempty"`
	GraphTiles      []NavGraphTileEntryDef  `json:"graph_tiles,omitempty"`
}

type NavRouteStep struct {
	Tile              TerrainChunkCoordDef  `json:"tile"`
	Region            uint32                `json:"region"`
	EnterTransition   uint32                `json:"enter_transition"`
	Target            Vec3                  `json:"target"`
	RequiredAction    string                `json:"required_action,omitempty"`
	Traversal         *NavTraversalDef      `json:"traversal,omitempty"`
	Gate              *NavTransitionGateDef `json:"gate,omitempty"`
	TraversalWaypoint int                   `json:"traversal_waypoint,omitempty"`
}

type NavRouteTileDependency struct {
	Tile  TerrainChunkCoordDef `json:"tile"`
	Epoch uint64               `json:"epoch"`
}

type NavRouteResult struct {
	Found         bool           `json:"found"`
	StartLocation NavPointResult `json:"start_location"`
	GoalLocation  NavPointResult `json:"goal_location"`
	Steps         []NavRouteStep `json:"steps,omitempty"`
	Waypoints     []Vec3         `json:"waypoints,omitempty"`
	// WaypointSpans has the same order and length as Waypoints.
	WaypointSpans      []NavSpanRef             `json:"waypoint_spans,omitempty"`
	FailureReason      string                   `json:"failure_reason,omitempty"`
	FailureTile        TerrainChunkCoordDef     `json:"failure_tile"`
	NavigationRevision uint64                   `json:"navigation_revision"`
	TileDependencies   []NavRouteTileDependency `json:"tile_dependencies,omitempty"`
}

type NavPointResult struct {
	Found    bool       `json:"found"`
	Ref      NavSpanRef `json:"ref"`
	Region   uint32     `json:"region"`
	Point    Vec3       `json:"point"`
	Distance float32    `json:"distance"`
}
