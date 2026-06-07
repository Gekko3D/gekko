package common

type Vec3 struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	Z float32 `json:"z"`
}

type Bounds struct {
	Min Vec3 `json:"min"`
	Max Vec3 `json:"max"`
}

type SourceInfo struct {
	Kind            string   `json:"kind"`
	GameDir         string   `json:"game_dir,omitempty"`
	MapName         string   `json:"map_name,omitempty"`
	BSPPath         string   `json:"bsp_path,omitempty"`
	WADPaths        []string `json:"wad_paths,omitempty"`
	BSPHash         string   `json:"bsp_hash,omitempty"`
	ImporterName    string   `json:"importer_name,omitempty"`
	ImporterVersion string   `json:"importer_version,omitempty"`
}

type Material struct {
	ID                int       `json:"id"`
	PaletteIndex      uint8     `json:"palette_index,omitempty"`
	SourceTextureName string    `json:"source_texture_name,omitempty"`
	BaseColor         [4]uint8  `json:"base_color,omitempty"`
	Kind              string    `json:"kind,omitempty"`
	CollisionKind     string    `json:"collision_kind,omitempty"`
	Transparent       bool      `json:"transparent,omitempty"`
	EmitsLight        bool      `json:"emits_light,omitempty"`
	Emissive          float32   `json:"emissive,omitempty"`
	Roughness         float32   `json:"roughness,omitempty"`
	Metallic          float32   `json:"metallic,omitempty"`
	Transparency      float32   `json:"transparency,omitempty"`
	SourceWAD         string    `json:"source_wad,omitempty"`
	Size              [2]uint32 `json:"size,omitempty"`
	Tags              []string  `json:"tags,omitempty"`
}

type Voxel struct {
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Z          int    `json:"z"`
	Palette    uint8  `json:"palette"`
	MaterialID int    `json:"material_id,omitempty"`
	SolidKind  string `json:"solid_kind,omitempty"`
}

type Entity struct {
	ClassName        string            `json:"classname"`
	KeyValues        map[string]string `json:"key_values,omitempty"`
	SourceOrigin     Vec3              `json:"source_origin,omitempty"`
	WorldPosition    Vec3              `json:"world_position,omitempty"`
	SourceAngles     Vec3              `json:"source_angles,omitempty"`
	BrushModelID     int               `json:"brush_model_id,omitempty"`
	BrushWorldBounds Bounds            `json:"brush_world_bounds,omitempty"`
}

type Light struct {
	Name      string   `json:"name,omitempty"`
	Position  Vec3     `json:"position"`
	Color     [3]uint8 `json:"color,omitempty"`
	Intensity float32  `json:"intensity,omitempty"`
	Range     float32  `json:"range,omitempty"`
	Style     string   `json:"style,omitempty"`
}

type Trigger struct {
	Kind      string `json:"kind"`
	Bounds    Bounds `json:"bounds,omitempty"`
	TargetMap string `json:"target_map,omitempty"`
	Landmark  string `json:"landmark,omitempty"`
}

type MapImport struct {
	Source      SourceInfo   `json:"source"`
	Bounds      Bounds       `json:"bounds,omitempty"`
	Materials   []Material   `json:"materials,omitempty"`
	Voxels      []Voxel      `json:"voxels,omitempty"`
	Entities    []Entity     `json:"entities,omitempty"`
	Lights      []Light      `json:"lights,omitempty"`
	Triggers    []Trigger    `json:"triggers,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}
