package content

const (
	CurrentImportedWorldSchemaVersion      = 1
	CurrentImportedWorldChunkSchemaVersion = 1
)

type ImportedWorldKind string

const (
	ImportedWorldKindVoxelWorld ImportedWorldKind = "imported_voxel_world"
)

type ImportedWorldDef struct {
	WorldID            string                       `json:"world_id"`
	SchemaVersion      int                          `json:"schema_version"`
	Kind               ImportedWorldKind            `json:"kind"`
	ChunkSize          int                          `json:"chunk_size"`
	VoxelResolution    float32                      `json:"voxel_resolution"`
	SourceBuildVersion string                       `json:"source_build_version,omitempty"`
	SourceHash         string                       `json:"source_hash,omitempty"`
	Tags               []string                     `json:"tags,omitempty"`
	Entries            []ImportedWorldChunkEntryDef `json:"entries,omitempty"`
}

type ImportedWorldChunkEntryDef struct {
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkPath          string               `json:"chunk_path"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
}

type ImportedWorldChunkDef struct {
	WorldID            string                  `json:"world_id"`
	SchemaVersion      int                     `json:"schema_version"`
	Coord              TerrainChunkCoordDef    `json:"coord"`
	ChunkSize          int                     `json:"chunk_size"`
	VoxelResolution    float32                 `json:"voxel_resolution"`
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
}

func EnsureImportedWorldChunkDefaults(def *ImportedWorldChunkDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentImportedWorldChunkSchemaVersion
	}
	if def.NonEmptyVoxelCount == 0 {
		def.NonEmptyVoxelCount = len(def.Voxels)
	}
}
