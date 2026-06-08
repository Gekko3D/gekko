package content

const CurrentLevelSchemaVersion = 3

const DefaultLevelBrushLayerName = "Layer 1"

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

type LevelBrushKind string

const (
	LevelBrushKindProcedural LevelBrushKind = "procedural"
	LevelBrushKindVoxelShape LevelBrushKind = "voxel_shape"
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
	ID               string                  `json:"id"`
	SchemaVersion    int                     `json:"schema_version"`
	Name             string                  `json:"name"`
	Tags             []string                `json:"tags,omitempty"`
	ChunkSize        int                     `json:"chunk_size,omitempty"`
	VoxelResolution  float32                 `json:"voxel_resolution,omitempty"`
	Materials        []LevelMaterialDef      `json:"materials,omitempty"`
	BrushLayers      []LevelBrushLayerDef    `json:"brush_layers,omitempty"`
	Brushes          []LevelBrushDef         `json:"brushes,omitempty"`
	Terrain          *LevelTerrainDef        `json:"terrain,omitempty"`
	BaseWorld        *LevelBaseWorldDef      `json:"base_world,omitempty"`
	Player           *LevelPlayerDef         `json:"player,omitempty"`
	Placements       []LevelPlacementDef     `json:"placements,omitempty"`
	PlacementVolumes []PlacementVolumeDef    `json:"placement_volumes,omitempty"`
	Environment      *LevelEnvironmentDef    `json:"environment,omitempty"`
	Lights           []LevelLightDef         `json:"lights,omitempty"`
	WaterBodies      []LevelWaterBodyDef     `json:"water_bodies,omitempty"`
	LadderVolumes    []LevelLadderVolumeDef  `json:"ladder_volumes,omitempty"`
	MovingBrushes    []LevelMovingBrushDef   `json:"moving_brushes,omitempty"`
	PathNodes        []LevelPathNodeDef      `json:"path_nodes,omitempty"`
	UseTriggers      []LevelUseTriggerDef    `json:"use_triggers,omitempty"`
	TriggerVolumes   []LevelTriggerVolumeDef `json:"trigger_volumes,omitempty"`
	DamageVolumes    []LevelDamageVolumeDef  `json:"damage_volumes,omitempty"`
	ChangeLevels     []LevelChangeLevelDef   `json:"change_levels,omitempty"`
	Chargers         []LevelChargerDef       `json:"chargers,omitempty"`
	TargetRelays     []LevelTargetRelayDef   `json:"target_relays,omitempty"`
	MultiTargets     []LevelMultiTargetDef   `json:"multi_targets,omitempty"`
	Breakables       []LevelBreakableDef     `json:"breakables,omitempty"`
	Pickups          []LevelPickupDef        `json:"pickups,omitempty"`
	Markers          []LevelMarkerDef        `json:"markers,omitempty"`
}

type LevelMaterialDef = AssetMaterialDef
type LevelLightType = AssetLightType

const (
	LevelLightTypePoint       LevelLightType = AssetLightTypePoint
	LevelLightTypeDirectional LevelLightType = AssetLightTypeDirectional
	LevelLightTypeSpot        LevelLightType = AssetLightTypeSpot
	LevelLightTypeAmbient     LevelLightType = AssetLightTypeAmbient
)

type LevelBrushLayerDef struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	EditorHidden bool            `json:"editor_hidden,omitempty"`
	EditorLocked bool            `json:"editor_locked,omitempty"`
	Brushes      []LevelBrushDef `json:"brushes,omitempty"`
}

type LevelBrushDef struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Kind         LevelBrushKind      `json:"kind,omitempty"`
	Primitive    string              `json:"primitive"`
	Params       map[string]float32  `json:"params,omitempty"`
	VoxelShape   *AssetVoxelShapeDef `json:"voxel_shape,omitempty"`
	Transform    LevelTransformDef   `json:"transform"`
	MaterialID   string              `json:"material_id,omitempty"`
	Operation    AssetShapeOperation `json:"operation,omitempty"`
	EditorHidden bool                `json:"editor_hidden,omitempty"`
	EditorLocked bool                `json:"editor_locked,omitempty"`
	Tags         []string            `json:"tags,omitempty"`
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

type LevelPlayerDef struct {
	SpawnKind        string   `json:"spawn_kind,omitempty"`
	Height           float32  `json:"height,omitempty"`
	EyeHeight        float32  `json:"eye_height,omitempty"`
	Radius           float32  `json:"radius,omitempty"`
	Speed            float32  `json:"speed,omitempty"`
	SprintMultiplier float32  `json:"sprint_multiplier,omitempty"`
	Sensitivity      float32  `json:"sensitivity,omitempty"`
	JumpSpeed        float32  `json:"jump_speed,omitempty"`
	Gravity          float32  `json:"gravity,omitempty"`
	StepHeight       float32  `json:"step_height,omitempty"`
	GroundProbe      float32  `json:"ground_probe,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

type LevelLightDef struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	Transform     LevelTransformDef `json:"transform"`
	Type          LevelLightType    `json:"type"`
	Color         [3]float32        `json:"color,omitempty"`
	Intensity     float32           `json:"intensity,omitempty"`
	Range         float32           `json:"range,omitempty"`
	ConeAngle     float32           `json:"cone_angle,omitempty"`
	CastsShadows  bool              `json:"casts_shadows,omitempty"`
	SourceRadius  float32           `json:"source_radius,omitempty"`
	EmitterLinkID uint32            `json:"emitter_link_id,omitempty"`
	SourceTag     string            `json:"source_tag,omitempty"`
	Style         string            `json:"style,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
}

type LevelWaterBodyMode string

const (
	LevelWaterBodyModeExplicitRect LevelWaterBodyMode = "ExplicitRect"
	LevelWaterBodyModeFitBounds    LevelWaterBodyMode = "FitBounds"
)

type LevelWaterBodyDef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`

	Mode LevelWaterBodyMode `json:"mode,omitempty"`

	SurfaceY float32 `json:"surface_y"`
	Depth    float32 `json:"depth"`

	RectHalfExtents   Vec2 `json:"rect_half_extents,omitempty"`
	BoundsCenter      Vec3 `json:"bounds_center,omitempty"`
	BoundsHalfExtents Vec3 `json:"bounds_half_extents,omitempty"`

	Inset       float32 `json:"inset,omitempty"`
	Overlap     float32 `json:"overlap,omitempty"`
	MinCellSize float32 `json:"min_cell_size,omitempty"`

	SourceTag       string `json:"source_tag,omitempty"`
	ContinuityGroup string `json:"continuity_group,omitempty"`
	EnableSkirt     *bool  `json:"enable_skirt,omitempty"`
	MaxPatchCount   uint32 `json:"max_patch_count,omitempty"`
	DebugName       string `json:"debug_name,omitempty"`

	Color           Vec3    `json:"color,omitempty"`
	AbsorptionColor Vec3    `json:"absorption_color,omitempty"`
	Opacity         float32 `json:"opacity,omitempty"`
	Roughness       float32 `json:"roughness,omitempty"`
	Refraction      float32 `json:"refraction,omitempty"`
	// DirectLightOcclusion attenuates global sun/moon lighting on water.
	// 0 keeps full direct light; 1 fully removes direct-light sparkle.
	DirectLightOcclusion *float32 `json:"direct_light_occlusion,omitempty"`
	FlowDirection        Vec2     `json:"flow_direction,omitempty"`
	FlowSpeed            float32  `json:"flow_speed,omitempty"`
	WaveAmplitude        float32  `json:"wave_amplitude,omitempty"`

	Transform LevelTransformDef `json:"transform,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
}

type LevelEnvironmentDef struct {
	Preset                  string   `json:"preset,omitempty"`
	DirectionalCastsShadows *bool    `json:"directional_casts_shadows,omitempty"`
	Tags                    []string `json:"tags,omitempty"`
}

type LevelLadderVolumeDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	ClimbSpeed        float32  `json:"climb_speed,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelMovingBrushDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	MotionKind        string   `json:"motion_kind,omitempty"`
	AssetPath         string   `json:"asset_path,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	VisualOrigin      Vec3     `json:"visual_origin,omitempty"`
	MoveDirection     Vec3     `json:"move_direction,omitempty"`
	MoveDistance      float32  `json:"move_distance,omitempty"`
	RotationOrigin    Vec3     `json:"rotation_origin,omitempty"`
	RotationAxis      Vec3     `json:"rotation_axis,omitempty"`
	OpenAngle         float32  `json:"open_angle,omitempty"`
	PathTarget        string   `json:"path_target,omitempty"`
	Speed             float32  `json:"speed,omitempty"`
	Wait              float32  `json:"wait,omitempty"`
	Lip               float32  `json:"lip,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	Target            string   `json:"target,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelPathNodeDef struct {
	ID         string   `json:"id"`
	Name       string   `json:"name,omitempty"`
	TargetName string   `json:"target_name,omitempty"`
	Target     string   `json:"target,omitempty"`
	Position   Vec3     `json:"position"`
	Wait       float32  `json:"wait,omitempty"`
	Speed      float32  `json:"speed,omitempty"`
	SpawnFlags int      `json:"spawn_flags,omitempty"`
	SourceTag  string   `json:"source_tag,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type LevelUseTriggerDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	TargetName        string   `json:"target_name,omitempty"`
	Target            string   `json:"target,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelTriggerVolumeDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	TargetName        string   `json:"target_name,omitempty"`
	Target            string   `json:"target,omitempty"`
	Delay             float32  `json:"delay,omitempty"`
	Wait              float32  `json:"wait,omitempty"`
	Once              bool     `json:"once,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelDamageVolumeDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	Damage            float32  `json:"damage,omitempty"`
	DamageInterval    float32  `json:"damage_interval,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	Target            string   `json:"target,omitempty"`
	Delay             float32  `json:"delay,omitempty"`
	SpawnFlags        int      `json:"spawn_flags,omitempty"`
	StartDisabled     bool     `json:"start_disabled,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelChangeLevelDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	TargetMap         string   `json:"target_map,omitempty"`
	Landmark          string   `json:"landmark,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	SpawnFlags        int      `json:"spawn_flags,omitempty"`
	StartDisabled     bool     `json:"start_disabled,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelChargerDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	ChargeKind        string   `json:"charge_kind,omitempty"`
	Capacity          float32  `json:"capacity,omitempty"`
	Rate              float32  `json:"rate,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	SpawnFlags        int      `json:"spawn_flags,omitempty"`
	StartDisabled     bool     `json:"start_disabled,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelTargetEventDef struct {
	Target string  `json:"target"`
	Delay  float32 `json:"delay,omitempty"`
}

type LevelMultiTargetDef struct {
	ID         string                `json:"id"`
	Name       string                `json:"name,omitempty"`
	TargetName string                `json:"target_name,omitempty"`
	Delay      float32               `json:"delay,omitempty"`
	Events     []LevelTargetEventDef `json:"events,omitempty"`
	SourceTag  string                `json:"source_tag,omitempty"`
	Tags       []string              `json:"tags,omitempty"`
}

type LevelTargetRelayDef struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	TargetName   string   `json:"target_name,omitempty"`
	Target       string   `json:"target,omitempty"`
	Delay        float32  `json:"delay,omitempty"`
	KillTarget   string   `json:"kill_target,omitempty"`
	TriggerState int      `json:"trigger_state,omitempty"`
	SpawnFlags   int      `json:"spawn_flags,omitempty"`
	SourceTag    string   `json:"source_tag,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

type LevelBreakableDef struct {
	ID                string   `json:"id"`
	Name              string   `json:"name,omitempty"`
	Kind              string   `json:"kind,omitempty"`
	AssetPath         string   `json:"asset_path,omitempty"`
	BoundsCenter      Vec3     `json:"bounds_center"`
	BoundsHalfExtents Vec3     `json:"bounds_half_extents"`
	VisualOrigin      Vec3     `json:"visual_origin,omitempty"`
	Health            float32  `json:"health,omitempty"`
	Material          string   `json:"material,omitempty"`
	SpawnObject       string   `json:"spawn_object,omitempty"`
	SpawnFlags        int      `json:"spawn_flags,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	Target            string   `json:"target,omitempty"`
	Delay             float32  `json:"delay,omitempty"`
	SourceTag         string   `json:"source_tag,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

type LevelPickupDef struct {
	ID         string            `json:"id"`
	Name       string            `json:"name,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	AssetPath  string            `json:"asset_path,omitempty"`
	Category   string            `json:"category,omitempty"`
	Item       string            `json:"item,omitempty"`
	Amount     int               `json:"amount,omitempty"`
	ClassName  string            `json:"class_name,omitempty"`
	Transform  LevelTransformDef `json:"transform"`
	TargetName string            `json:"target_name,omitempty"`
	SpawnFlags int               `json:"spawn_flags,omitempty"`
	SourceTag  string            `json:"source_tag,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
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

func NewLevelBrushLayerDef(name string) LevelBrushLayerDef {
	if name == "" {
		name = DefaultLevelBrushLayerName
	}
	return LevelBrushLayerDef{
		ID:   newID(),
		Name: name,
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
	for i := range def.WaterBodies {
		if def.WaterBodies[i].ID == "" {
			def.WaterBodies[i].ID = newID()
		}
		if def.WaterBodies[i].Mode == "" {
			def.WaterBodies[i].Mode = LevelWaterBodyModeExplicitRect
		}
		if def.WaterBodies[i].Transform.Rotation == (Quat{}) {
			def.WaterBodies[i].Transform.Rotation = Quat{0, 0, 0, 1}
		}
		if def.WaterBodies[i].Transform.Scale == (Vec3{}) {
			def.WaterBodies[i].Transform.Scale = Vec3{1, 1, 1}
		}
	}
	for i := range def.LadderVolumes {
		if def.LadderVolumes[i].ID == "" {
			def.LadderVolumes[i].ID = newID()
		}
	}
	for i := range def.MovingBrushes {
		if def.MovingBrushes[i].ID == "" {
			def.MovingBrushes[i].ID = newID()
		}
	}
	for i := range def.PathNodes {
		if def.PathNodes[i].ID == "" {
			def.PathNodes[i].ID = newID()
		}
	}
	for i := range def.UseTriggers {
		if def.UseTriggers[i].ID == "" {
			def.UseTriggers[i].ID = newID()
		}
	}
	for i := range def.TriggerVolumes {
		if def.TriggerVolumes[i].ID == "" {
			def.TriggerVolumes[i].ID = newID()
		}
	}
	for i := range def.DamageVolumes {
		if def.DamageVolumes[i].ID == "" {
			def.DamageVolumes[i].ID = newID()
		}
	}
	for i := range def.ChangeLevels {
		if def.ChangeLevels[i].ID == "" {
			def.ChangeLevels[i].ID = newID()
		}
	}
	for i := range def.Chargers {
		if def.Chargers[i].ID == "" {
			def.Chargers[i].ID = newID()
		}
	}
	for i := range def.MultiTargets {
		if def.MultiTargets[i].ID == "" {
			def.MultiTargets[i].ID = newID()
		}
	}
	for i := range def.TargetRelays {
		if def.TargetRelays[i].ID == "" {
			def.TargetRelays[i].ID = newID()
		}
	}
	for i := range def.Breakables {
		if def.Breakables[i].ID == "" {
			def.Breakables[i].ID = newID()
		}
	}
	for i := range def.Pickups {
		if def.Pickups[i].ID == "" {
			def.Pickups[i].ID = newID()
		}
		if def.Pickups[i].Transform.Rotation == (Quat{}) {
			def.Pickups[i].Transform.Rotation = Quat{0, 0, 0, 1}
		}
		if def.Pickups[i].Transform.Scale == (Vec3{}) {
			def.Pickups[i].Transform.Scale = Vec3{1, 1, 1}
		}
	}
	for i := range def.Lights {
		if def.Lights[i].ID == "" {
			def.Lights[i].ID = newID()
		}
		if def.Lights[i].Transform.Rotation == (Quat{}) {
			def.Lights[i].Transform.Rotation = Quat{0, 0, 0, 1}
		}
		if def.Lights[i].Transform.Scale == (Vec3{}) {
			def.Lights[i].Transform.Scale = Vec3{1, 1, 1}
		}
		if def.Lights[i].Type == "" {
			def.Lights[i].Type = LevelLightTypePoint
		}
	}
	for i := range def.Materials {
		normalizeLevelMaterial(&def.Materials[i])
	}
	if len(def.BrushLayers) == 0 {
		def.BrushLayers = append(def.BrushLayers, NewLevelBrushLayerDef(DefaultLevelBrushLayerName))
	}
	for i := range def.BrushLayers {
		normalizeLevelBrushLayer(&def.BrushLayers[i], i)
	}
	for i := range def.Markers {
		if def.Markers[i].ID == "" {
			def.Markers[i].ID = newID()
		}
	}
}

func normalizeLevelMaterial(material *LevelMaterialDef) {
	normalizeAssetMaterial(material)
}

func normalizeLevelBrushLayer(layer *LevelBrushLayerDef, index int) {
	if layer == nil {
		return
	}
	if layer.ID == "" {
		layer.ID = newID()
	}
	if layer.Name == "" {
		if index == 0 {
			layer.Name = DefaultLevelBrushLayerName
		} else {
			layer.Name = defaultIndexedLevelBrushLayerName(index)
		}
	}
	for i := range layer.Brushes {
		normalizeLevelBrush(&layer.Brushes[i])
	}
}

func normalizeLevelBrush(brush *LevelBrushDef) {
	if brush == nil {
		return
	}
	if brush.ID == "" {
		brush.ID = newID()
	}
	if brush.Kind == "" {
		if brush.VoxelShape != nil {
			brush.Kind = LevelBrushKindVoxelShape
		} else {
			brush.Kind = LevelBrushKindProcedural
		}
	}
	if brush.Transform.Rotation == (Quat{}) {
		brush.Transform.Rotation = Quat{0, 0, 0, 1}
	}
	if brush.Transform.Scale == (Vec3{}) {
		brush.Transform.Scale = Vec3{1, 1, 1}
	}
}

func FindLevelMaterialByID(def *LevelDef, id string) (LevelMaterialDef, bool) {
	if def == nil || id == "" {
		return LevelMaterialDef{}, false
	}
	for _, material := range def.Materials {
		if material.ID == id {
			return material, true
		}
	}
	return LevelMaterialDef{}, false
}

func LevelBrushCount(def *LevelDef) int {
	if def == nil {
		return 0
	}
	count := 0
	for _, layer := range def.BrushLayers {
		count += len(layer.Brushes)
	}
	return count
}

func LevelBrushes(def *LevelDef) []LevelBrushDef {
	if def == nil {
		return nil
	}
	brushes := make([]LevelBrushDef, 0, LevelBrushCount(def))
	for _, layer := range def.BrushLayers {
		brushes = append(brushes, layer.Brushes...)
	}
	return brushes
}

func FindLevelBrushByID(def *LevelDef, id string) (LevelBrushDef, bool) {
	if def == nil || id == "" {
		return LevelBrushDef{}, false
	}
	for _, layer := range def.BrushLayers {
		for _, brush := range layer.Brushes {
			if brush.ID == id {
				return brush, true
			}
		}
	}
	return LevelBrushDef{}, false
}

func FindLevelBrushLayerByID(def *LevelDef, id string) (LevelBrushLayerDef, bool) {
	if def == nil || id == "" {
		return LevelBrushLayerDef{}, false
	}
	for _, layer := range def.BrushLayers {
		if layer.ID == id {
			return layer, true
		}
	}
	return LevelBrushLayerDef{}, false
}

func EffectiveLevelBrushOperation(brush LevelBrushDef) AssetShapeOperation {
	if brush.Operation == "" {
		return AssetShapeOperationAdd
	}
	return brush.Operation
}

func EffectiveLevelBrushKind(brush LevelBrushDef) LevelBrushKind {
	if brush.Kind != "" {
		return brush.Kind
	}
	if brush.VoxelShape != nil {
		return LevelBrushKindVoxelShape
	}
	return LevelBrushKindProcedural
}

func defaultIndexedLevelBrushLayerName(index int) string {
	if index <= 0 {
		return DefaultLevelBrushLayerName
	}
	return "Layer " + itoa(index+1)
}
