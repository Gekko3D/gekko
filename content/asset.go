package content

import "github.com/google/uuid"

const (
	CurrentAssetSchemaVersion = 3
	DefaultAssetVoxelSize     = 0.1
)

type Vec3 [3]float32
type Vec4 [4]float32
type Vec2 [2]float32
type Quat [4]float32
type Range2 [2]float32

type AssetSourceKind string

const (
	AssetSourceKindGroup               AssetSourceKind = "group"
	AssetSourceKindVoxModel            AssetSourceKind = "vox_model"
	AssetSourceKindVoxSceneNode        AssetSourceKind = "vox_scene_node"
	AssetSourceKindProceduralPrimitive AssetSourceKind = "procedural_primitive"
	AssetSourceKindVoxelShape          AssetSourceKind = "voxel_shape"
)

type AssetLightType string

const (
	AssetLightTypePoint       AssetLightType = "point"
	AssetLightTypeDirectional AssetLightType = "directional"
	AssetLightTypeSpot        AssetLightType = "spot"
	AssetLightTypeAmbient     AssetLightType = "ambient"
)

type AssetAlphaMode string

const (
	AssetAlphaModeTexture   AssetAlphaMode = "texture"
	AssetAlphaModeLuminance AssetAlphaMode = "luminance"
)

type AssetShapeOperation string

const (
	AssetShapeOperationAdd      AssetShapeOperation = "add"
	AssetShapeOperationSubtract AssetShapeOperation = "subtract"
)

const (
	AssetMarkerKindMuzzle       = "muzzle"
	AssetMarkerKindHandMount    = "hand_mount"
	AssetMarkerKindEffectAnchor = "effect_anchor"
	AssetMarkerKindSpawnAnchor  = "spawn_anchor"
	AssetMarkerKindDockPort     = "dock_port"
	AssetMarkerKindWeaponSlot   = "weapon_slot"
)

func KnownAssetMarkerKinds() []string {
	return []string{
		AssetMarkerKindMuzzle,
		AssetMarkerKindDockPort,
		AssetMarkerKindWeaponSlot,
		AssetMarkerKindHandMount,
		AssetMarkerKindEffectAnchor,
		AssetMarkerKindSpawnAnchor,
	}
}

type AssetDef struct {
	ID            string             `json:"id"`
	SchemaVersion int                `json:"schema_version"`
	Name          string             `json:"name"`
	Tags          []string           `json:"tags,omitempty"`
	Materials     []AssetMaterialDef `json:"materials,omitempty"`
	Runtime       *AssetRuntimeDef   `json:"runtime,omitempty"`
	Parts         []AssetPartDef     `json:"parts,omitempty"`
	Lights        []AssetLightDef    `json:"lights,omitempty"`
	Emitters      []AssetEmitterDef  `json:"emitters,omitempty"`
	Markers       []AssetMarkerDef   `json:"markers,omitempty"`
}

type AssetMaterialDef struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Tags         []string `json:"tags,omitempty"`
	BaseColor    [4]uint8 `json:"base_color,omitempty"`
	Roughness    float32  `json:"roughness,omitempty"`
	Metallic     float32  `json:"metallic,omitempty"`
	Emissive     float32  `json:"emissive,omitempty"`
	IOR          float32  `json:"ior,omitempty"`
	Transparency float32  `json:"transparency,omitempty"`
}

type AssetRuntimeDef struct {
	CollapseVoxelParts bool    `json:"collapse_voxel_parts,omitempty"`
	CastsShadows       *bool   `json:"casts_shadows,omitempty"`
	ShadowMaxDistance  float32 `json:"shadow_max_distance,omitempty"`
}

type AssetPartDef struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	ParentID        string            `json:"parent_id,omitempty"`
	Source          AssetSourceDef    `json:"source"`
	Transform       AssetTransformDef `json:"transform"`
	VoxelResolution float32           `json:"voxel_resolution,omitempty"`
	ModelScale      float32           `json:"model_scale,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
}

type AssetLightDef struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	ParentID     string            `json:"parent_id,omitempty"`
	Transform    AssetTransformDef `json:"transform"`
	Type         AssetLightType    `json:"type"`
	Color        [3]float32        `json:"color,omitempty"`
	Intensity    float32           `json:"intensity,omitempty"`
	Range        float32           `json:"range,omitempty"`
	ConeAngle    float32           `json:"cone_angle,omitempty"`
	CastsShadows bool              `json:"casts_shadows,omitempty"`
}

type AssetEmitterDef struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	ParentID  string            `json:"parent_id,omitempty"`
	Transform AssetTransformDef `json:"transform"`
	Emitter   EmitterDef        `json:"emitter"`
}

type AssetMarkerDef struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	ParentID  string            `json:"parent_id,omitempty"`
	Transform AssetTransformDef `json:"transform"`
	Kind      string            `json:"kind"`
	Tags      []string          `json:"tags,omitempty"`
}

// AssetTransformDef is authored relative to the asset root for root items and
// relative to the parent item for child items. Pivot is stored in that same
// authored space and must round-trip without recomputing from world state.
type AssetTransformDef struct {
	Position Vec3 `json:"position,omitempty"`
	Rotation Quat `json:"rotation,omitempty"`
	Scale    Vec3 `json:"scale,omitempty"`
	Pivot    Vec3 `json:"pivot,omitempty"`
}

type AssetSourceDef struct {
	Kind       AssetSourceKind     `json:"kind"`
	Path       string              `json:"path,omitempty"`
	ModelIndex int                 `json:"model_index,omitempty"`
	NodeName   string              `json:"node_name,omitempty"`
	Primitive  string              `json:"primitive,omitempty"`
	Params     map[string]float32  `json:"params,omitempty"`
	VoxelShape *AssetVoxelShapeDef `json:"voxel_shape,omitempty"`
	MaterialID string              `json:"material_id,omitempty"`
	Operation  AssetShapeOperation `json:"operation,omitempty"`
}

type AssetVoxelShapeDef struct {
	Palette []AssetVoxelPaletteEntryDef `json:"palette,omitempty"`
	Voxels  []VoxelObjectVoxelDef       `json:"voxels,omitempty"`
}

type AssetVoxelPaletteEntryDef struct {
	Value      uint8  `json:"value"`
	MaterialID string `json:"material_id,omitempty"`
}

type EmitterDef struct {
	Enabled          bool           `json:"enabled"`
	MaxParticles     int            `json:"max_particles,omitempty"`
	SpawnRate        float32        `json:"spawn_rate,omitempty"`
	LifetimeRange    Range2         `json:"lifetime_range,omitempty"`
	StartSpeedRange  Range2         `json:"start_speed_range,omitempty"`
	StartSizeRange   Range2         `json:"start_size_range,omitempty"`
	StartColorMin    Vec4           `json:"start_color_min,omitempty"`
	StartColorMax    Vec4           `json:"start_color_max,omitempty"`
	Gravity          float32        `json:"gravity,omitempty"`
	Drag             float32        `json:"drag,omitempty"`
	ConeAngleDegrees float32        `json:"cone_angle_degrees,omitempty"`
	SpriteIndex      uint32         `json:"sprite_index,omitempty"`
	AtlasCols        uint32         `json:"atlas_cols,omitempty"`
	AtlasRows        uint32         `json:"atlas_rows,omitempty"`
	TexturePath      string         `json:"texture_path,omitempty"`
	AlphaMode        AssetAlphaMode `json:"alpha_mode,omitempty"`
}

func NewAssetDef(name string) *AssetDef {
	def := &AssetDef{
		ID:            newID(),
		SchemaVersion: CurrentAssetSchemaVersion,
		Name:          name,
	}
	EnsureAssetIDs(def)
	return def
}

func EnsureAssetIDs(def *AssetDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentAssetSchemaVersion
	}
	for i := range def.Parts {
		if def.Parts[i].ID == "" {
			def.Parts[i].ID = newID()
		}
		normalizeAssetPart(&def.Parts[i])
	}
	for i := range def.Materials {
		normalizeAssetMaterial(&def.Materials[i])
	}
	for i := range def.Lights {
		if def.Lights[i].ID == "" {
			def.Lights[i].ID = newID()
		}
	}
	for i := range def.Emitters {
		if def.Emitters[i].ID == "" {
			def.Emitters[i].ID = newID()
		}
	}
	for i := range def.Markers {
		if def.Markers[i].ID == "" {
			def.Markers[i].ID = newID()
		}
	}
}

func NormalizeAssetDef(def *AssetDef) {
	EnsureAssetIDs(def)
	if def == nil {
		return
	}
	def.SchemaVersion = CurrentAssetSchemaVersion
}

func AssetPartUsesVoxelSource(part AssetPartDef) bool {
	switch part.Source.Kind {
	case AssetSourceKindVoxModel, AssetSourceKindVoxSceneNode, AssetSourceKindProceduralPrimitive, AssetSourceKindVoxelShape:
		return true
	default:
		return false
	}
}

func normalizeAssetPart(part *AssetPartDef) {
	if part == nil {
		return
	}
	if part.ModelScale == 0 {
		part.ModelScale = 1.0
	}
	if AssetPartUsesVoxelSource(*part) && part.VoxelResolution == 0 {
		part.VoxelResolution = DefaultAssetVoxelSize
	}
}

func normalizeAssetMaterial(material *AssetMaterialDef) {
	if material == nil {
		return
	}
	if material.BaseColor == [4]uint8{} {
		material.BaseColor = [4]uint8{255, 255, 255, 255}
	}
	if material.Roughness == 0 {
		material.Roughness = 1.0
	}
	if material.IOR == 0 {
		material.IOR = 1.5
	}
}

func FindAssetMaterialByID(def *AssetDef, id string) (AssetMaterialDef, bool) {
	if def == nil || id == "" {
		return AssetMaterialDef{}, false
	}
	for _, material := range def.Materials {
		if material.ID == id {
			return material, true
		}
	}
	return AssetMaterialDef{}, false
}

func EffectiveAssetSourceOperation(source AssetSourceDef) AssetShapeOperation {
	if source.Operation == "" {
		return AssetShapeOperationAdd
	}
	return source.Operation
}

func newID() string {
	return uuid.NewString()
}
