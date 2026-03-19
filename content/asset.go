package content

import "github.com/google/uuid"

const CurrentAssetSchemaVersion = 1

type Vec3 [3]float32
type Vec4 [4]float32
type Quat [4]float32
type Range2 [2]float32

type AssetSourceKind string

const (
	AssetSourceKindVoxModel            AssetSourceKind = "vox_model"
	AssetSourceKindVoxSceneNode        AssetSourceKind = "vox_scene_node"
	AssetSourceKindProceduralPrimitive AssetSourceKind = "procedural_primitive"
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

type AssetDef struct {
	ID            string            `json:"id"`
	SchemaVersion int               `json:"schema_version"`
	Name          string            `json:"name"`
	Tags          []string          `json:"tags,omitempty"`
	Parts         []AssetPartDef    `json:"parts,omitempty"`
	Lights        []AssetLightDef   `json:"lights,omitempty"`
	Emitters      []AssetEmitterDef `json:"emitters,omitempty"`
	Markers       []AssetMarkerDef  `json:"markers,omitempty"`
}

type AssetPartDef struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ParentID   string            `json:"parent_id,omitempty"`
	Source     AssetSourceDef    `json:"source"`
	Transform  AssetTransformDef `json:"transform"`
	ModelScale float32           `json:"model_scale,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
}

type AssetLightDef struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	ParentID  string            `json:"parent_id,omitempty"`
	Transform AssetTransformDef `json:"transform"`
	Type      AssetLightType    `json:"type"`
	Color     [3]float32        `json:"color,omitempty"`
	Intensity float32           `json:"intensity,omitempty"`
	Range     float32           `json:"range,omitempty"`
	ConeAngle float32           `json:"cone_angle,omitempty"`
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

type AssetTransformDef struct {
	Position Vec3 `json:"position,omitempty"`
	Rotation Quat `json:"rotation,omitempty"`
	Scale    Vec3 `json:"scale,omitempty"`
	Pivot    Vec3 `json:"pivot,omitempty"`
}

type AssetSourceDef struct {
	Kind       AssetSourceKind    `json:"kind"`
	Path       string             `json:"path,omitempty"`
	ModelIndex int                `json:"model_index,omitempty"`
	NodeName   string             `json:"node_name,omitempty"`
	Primitive  string             `json:"primitive,omitempty"`
	Params     map[string]float32 `json:"params,omitempty"`
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

func newID() string {
	return uuid.NewString()
}
