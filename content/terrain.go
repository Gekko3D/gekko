package content

const CurrentTerrainSchemaVersion = 1

type TerrainKind string

const (
	TerrainKindHeightfield TerrainKind = "heightfield"
)

type TerrainSourceDef struct {
	ID              string            `json:"id"`
	SchemaVersion   int               `json:"schema_version"`
	Name            string            `json:"name"`
	Kind            TerrainKind       `json:"kind"`
	SampleWidth     int               `json:"sample_width"`
	SampleHeight    int               `json:"sample_height"`
	HeightSamples   []uint16          `json:"height_samples,omitempty"`
	WorldSize       Vec2              `json:"world_size,omitempty"`
	HeightScale     float32           `json:"height_scale,omitempty"`
	VoxelResolution float32           `json:"voxel_resolution,omitempty"`
	ChunkSize       int               `json:"chunk_size,omitempty"`
	ImportSource    *TerrainImportDef `json:"import_source,omitempty"`
}

type TerrainImportDef struct {
	PNGPath    string `json:"png_path,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
}

func NewTerrainSourceDef(name string) *TerrainSourceDef {
	def := &TerrainSourceDef{
		ID:              newID(),
		SchemaVersion:   CurrentTerrainSchemaVersion,
		Name:            name,
		Kind:            TerrainKindHeightfield,
		WorldSize:       Vec2{256, 256},
		HeightScale:     64,
		VoxelResolution: 1,
		ChunkSize:       32,
	}
	EnsureTerrainSourceDefaults(def)
	return def
}

func EnsureTerrainSourceDefaults(def *TerrainSourceDef) {
	if def == nil {
		return
	}
	if def.ID == "" {
		def.ID = newID()
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentTerrainSchemaVersion
	}
	if def.Kind == "" {
		def.Kind = TerrainKindHeightfield
	}
	if def.WorldSize == (Vec2{}) {
		def.WorldSize = Vec2{256, 256}
	}
	if def.HeightScale == 0 {
		def.HeightScale = 64
	}
	if def.VoxelResolution == 0 {
		def.VoxelResolution = 1
	}
	if def.ChunkSize == 0 {
		def.ChunkSize = 32
	}
}
