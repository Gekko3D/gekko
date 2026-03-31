package content

const CurrentLevelSchemaVersion = 1

type LevelPlacementMode string

const (
	LevelPlacementModeSurfaceSnap LevelPlacementMode = "surface_snap"
	LevelPlacementModePlaneSnap   LevelPlacementMode = "plane_snap"
	LevelPlacementModeFree3D      LevelPlacementMode = "free_3d"
)

type PlacementVolumeKind string

const (
	PlacementVolumeKindSphere PlacementVolumeKind = "sphere"
	PlacementVolumeKindBox    PlacementVolumeKind = "box"
)

type PlacementVolumeRuleMode string

const (
	PlacementVolumeRuleModeCount   PlacementVolumeRuleMode = "count"
	PlacementVolumeRuleModeDensity PlacementVolumeRuleMode = "density"
)

const (
	LevelTagShooter = "shooter"
)

const (
	LevelMarkerKindPlayerSpawn = "player_spawn"
	LevelMarkerKindAISpawn     = "ai_spawn"
	LevelMarkerKindPatrolPoint = "patrol_point"
	LevelMarkerKindObjective   = "objective"
	LevelMarkerKindExtract     = "extract_point"
)

type LevelDef struct {
	ID               string               `json:"id"`
	SchemaVersion    int                  `json:"schema_version"`
	Name             string               `json:"name"`
	Tags             []string             `json:"tags,omitempty"`
	ChunkSize        int                  `json:"chunk_size,omitempty"`
	VoxelResolution  float32              `json:"voxel_resolution,omitempty"`
	Terrain          *LevelTerrainDef     `json:"terrain,omitempty"`
	BaseWorld        *LevelBaseWorldDef   `json:"base_world,omitempty"`
	Placements       []LevelPlacementDef  `json:"placements,omitempty"`
	PlacementVolumes []PlacementVolumeDef `json:"placement_volumes,omitempty"`
	Environment      *LevelEnvironmentDef `json:"environment,omitempty"`
	Markers          []LevelMarkerDef     `json:"markers,omitempty"`
}

type LevelPlacementDef struct {
	ID            string             `json:"id"`
	AssetPath     string             `json:"asset_path"`
	Transform     LevelTransformDef  `json:"transform"`
	PlacementMode LevelPlacementMode `json:"placement_mode,omitempty"`
	Tags          []string           `json:"tags,omitempty"`
}

type PlacementVolumeRuleDef struct {
	Mode                 PlacementVolumeRuleMode `json:"mode,omitempty"`
	Count                int                     `json:"count,omitempty"`
	DensityPer1000Volume float32                 `json:"density_per_1000_volume,omitempty"`
}

type PlacementVolumeDef struct {
	ID                string                 `json:"id"`
	Kind              PlacementVolumeKind    `json:"kind"`
	AssetPath         string                 `json:"asset_path,omitempty"`
	AssetSetPath      string                 `json:"asset_set_path,omitempty"`
	CastsShadows      *bool                  `json:"casts_shadows,omitempty"`
	ShadowMaxDistance float32                `json:"shadow_max_distance,omitempty"`
	MaxShadowCasters  int                    `json:"max_shadow_casters,omitempty"`
	Transform         LevelTransformDef      `json:"transform"`
	Radius            float32                `json:"radius,omitempty"`
	Extents           Vec3                   `json:"extents,omitempty"`
	Rule              PlacementVolumeRuleDef `json:"rule"`
	RandomSeed        int64                  `json:"random_seed,omitempty"`
	Tags              []string               `json:"tags,omitempty"`
}

type LevelTransformDef struct {
	Position Vec3 `json:"position,omitempty"`
	Rotation Quat `json:"rotation,omitempty"`
	Scale    Vec3 `json:"scale,omitempty"`
}

type LevelTerrainDef struct {
	Kind         TerrainKind `json:"kind,omitempty"`
	SourcePath   string      `json:"source_path,omitempty"`
	ManifestPath string      `json:"manifest_path,omitempty"`
}

type LevelBaseWorldDef struct {
	Kind              ImportedWorldKind `json:"kind,omitempty"`
	ManifestPath      string            `json:"manifest_path,omitempty"`
	ReadOnlyByDefault bool              `json:"read_only_by_default,omitempty"`
	CollisionEnabled  bool              `json:"collision_enabled,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
}

type LevelEnvironmentDef struct {
	Preset string   `json:"preset,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type LevelMarkerDef struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Transform LevelTransformDef `json:"transform"`
	Tags      []string          `json:"tags,omitempty"`
}

func NewLevelDef(name string) *LevelDef {
	def := &LevelDef{
		ID:              newID(),
		SchemaVersion:   CurrentLevelSchemaVersion,
		Name:            name,
		ChunkSize:       32,
		VoxelResolution: 1,
	}
	EnsureLevelIDs(def)
	return def
}

func NewPlacementVolumeDef(kind PlacementVolumeKind) PlacementVolumeDef {
	return PlacementVolumeDef{
		ID:   newID(),
		Kind: kind,
		Transform: LevelTransformDef{
			Rotation: Quat{0, 0, 0, 1},
			Scale:    Vec3{1, 1, 1},
		},
		Radius:  8,
		Extents: Vec3{8, 8, 8},
		Rule: PlacementVolumeRuleDef{
			Mode:  PlacementVolumeRuleModeCount,
			Count: 16,
		},
		RandomSeed: 1,
	}
}

func EnsureLevelIDs(def *LevelDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentLevelSchemaVersion
	}
	if def.ChunkSize == 0 {
		def.ChunkSize = 32
	}
	if def.VoxelResolution == 0 {
		def.VoxelResolution = 1
	}
	for i := range def.Placements {
		if def.Placements[i].ID == "" {
			def.Placements[i].ID = newID()
		}
		if def.Placements[i].PlacementMode == "" {
			def.Placements[i].PlacementMode = LevelPlacementModePlaneSnap
		}
	}
	for i := range def.PlacementVolumes {
		if def.PlacementVolumes[i].ID == "" {
			def.PlacementVolumes[i].ID = newID()
		}
	}
	for i := range def.Markers {
		if def.Markers[i].ID == "" {
			def.Markers[i].ID = newID()
		}
	}
}
