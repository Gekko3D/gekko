package content

const (
	CurrentImportedWorldSchemaVersion      = 1
	CurrentImportedWorldChunkSchemaVersion = 1
)

type ImportedWorldKind string

const (
	ImportedWorldKindVoxelWorld ImportedWorldKind = "imported_voxel_world"

	ImportedWorldChunkPayloadSparseJSONV1     = "sparse_json_v1"
	ImportedWorldChunkPayloadDenseRLEBinaryV1 = "dense_rle_binary_v1"
)

type ImportedWorldDef struct {
	WorldID            string                       `json:"world_id"`
	SchemaVersion      int                          `json:"schema_version"`
	Kind               ImportedWorldKind            `json:"kind"`
	ChunkSize          int                          `json:"chunk_size"`
	VoxelResolution    float32                      `json:"voxel_resolution"`
	Palette            []ImportedWorldPaletteColor  `json:"palette,omitempty"`
	Materials          []ImportedWorldMaterialDef   `json:"materials,omitempty"`
	SourceMaterials    []ImportedWorldMaterialDef   `json:"source_materials,omitempty"`
	SourceBuildVersion string                       `json:"source_build_version,omitempty"`
	SourceHash         string                       `json:"source_hash,omitempty"`
	ChunkPayloadKind   string                       `json:"chunk_payload_kind,omitempty"`
	Tags               []string                     `json:"tags,omitempty"`
	Entries            []ImportedWorldChunkEntryDef `json:"entries,omitempty"`
}

type ImportedWorldPaletteColor [4]uint8

type ImportedWorldMaterialDef struct {
	ID                int                       `json:"id"`
	PaletteIndex      uint8                     `json:"palette_index,omitempty"`
	SourceTextureName string                    `json:"source_texture_name,omitempty"`
	BaseColor         ImportedWorldPaletteColor `json:"base_color,omitempty"`
	Kind              string                    `json:"kind,omitempty"`
	CollisionKind     string                    `json:"collision_kind,omitempty"`
	Transparent       bool                      `json:"transparent,omitempty"`
	EmitsLight        bool                      `json:"emits_light,omitempty"`
	Emissive          float32                   `json:"emissive,omitempty"`
	Roughness         float32                   `json:"roughness,omitempty"`
	Metallic          float32                   `json:"metallic,omitempty"`
	Transparency      float32                   `json:"transparency,omitempty"`
	SourceWAD         string                    `json:"source_wad,omitempty"`
	Size              [2]uint32                 `json:"size,omitempty"`
	Tags              []string                  `json:"tags,omitempty"`
}

type ImportedWorldChunkEntryDef struct {
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkPath          string               `json:"chunk_path"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count,omitempty"`
	PayloadKind        string               `json:"payload_kind,omitempty"`
	PayloadHash        string               `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int                  `json:"payload_size_bytes,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
}

type ImportedWorldChunkDef struct {
	WorldID            string                  `json:"world_id"`
	SchemaVersion      int                     `json:"schema_version"`
	Coord              TerrainChunkCoordDef    `json:"coord"`
	ChunkSize          int                     `json:"chunk_size"`
	VoxelResolution    float32                 `json:"voxel_resolution"`
	PayloadKind        string                  `json:"payload_kind,omitempty"`
	PayloadHash        string                  `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int                     `json:"payload_size_bytes,omitempty"`
	Voxels             []ImportedWorldVoxelDef `json:"voxels,omitempty"`
	NonEmptyVoxelCount int                     `json:"non_empty_voxel_count,omitempty"`
	Tags               []string                `json:"tags,omitempty"`
}

type ImportedWorldVoxelDef struct {
	X     int   `json:"x"`
	Y     int   `json:"y"`
	Z     int   `json:"z"`
	Value uint8 `json:"value"`
}

func EnsureImportedWorldDefaults(def *ImportedWorldDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentImportedWorldSchemaVersion
	}
	if def.Kind == "" {
		def.Kind = ImportedWorldKindVoxelWorld
	}
	if def.ChunkPayloadKind == "" {
		def.ChunkPayloadKind = ImportedWorldChunkPayloadSparseJSONV1
	}
}

func EnsureImportedWorldChunkDefaults(def *ImportedWorldChunkDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentImportedWorldChunkSchemaVersion
	}
	if def.PayloadKind == "" {
		def.PayloadKind = ImportedWorldChunkPayloadSparseJSONV1
	}
	if def.NonEmptyVoxelCount == 0 {
		def.NonEmptyVoxelCount = len(def.Voxels)
	}
}
