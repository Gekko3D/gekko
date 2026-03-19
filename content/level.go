package content

const CurrentLevelSchemaVersion = 1

type LevelPlacementMode string

const (
	LevelPlacementModeSurfaceSnap LevelPlacementMode = "surface_snap"
	LevelPlacementModePlaneSnap   LevelPlacementMode = "plane_snap"
	LevelPlacementModeFree3D      LevelPlacementMode = "free_3d"
)

type LevelDef struct {
	ID              string               `json:"id"`
	SchemaVersion   int                  `json:"schema_version"`
	Name            string               `json:"name"`
	Tags            []string             `json:"tags,omitempty"`
	ChunkSize       int                  `json:"chunk_size,omitempty"`
	StreamingRadius int                  `json:"streaming_radius,omitempty"`
	Terrain         *LevelTerrainDef     `json:"terrain,omitempty"`
	Placements      []LevelPlacementDef  `json:"placements,omitempty"`
	Environment     *LevelEnvironmentDef `json:"environment,omitempty"`
	Markers         []LevelMarkerDef     `json:"markers,omitempty"`
}

type LevelPlacementDef struct {
	ID            string             `json:"id"`
	AssetPath     string             `json:"asset_path"`
	Transform     LevelTransformDef  `json:"transform"`
	PlacementMode LevelPlacementMode `json:"placement_mode,omitempty"`
	Tags          []string           `json:"tags,omitempty"`
}

type LevelTransformDef struct {
	Position Vec3 `json:"position,omitempty"`
	Rotation Quat `json:"rotation,omitempty"`
	Scale    Vec3 `json:"scale,omitempty"`
}

type LevelTerrainDef struct {
	Kind         string `json:"kind,omitempty"`
	SourcePath   string `json:"source_path,omitempty"`
	ManifestPath string `json:"manifest_path,omitempty"`
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
		StreamingRadius: 4,
	}
	EnsureLevelIDs(def)
	return def
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
	for i := range def.Placements {
		if def.Placements[i].ID == "" {
			def.Placements[i].ID = newID()
		}
		if def.Placements[i].PlacementMode == "" {
			def.Placements[i].PlacementMode = LevelPlacementModePlaneSnap
		}
	}
	for i := range def.Markers {
		if def.Markers[i].ID == "" {
			def.Markers[i].ID = newID()
		}
	}
}
