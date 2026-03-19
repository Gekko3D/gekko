package content

const (
	CurrentTerrainChunkManifestSchemaVersion = 2
	CurrentTerrainChunkSchemaVersion         = 2
	DefaultTerrainChunkSolidValue            = uint8(1)
)

type TerrainChunkCoordDef struct {
	X int `json:"x"`
	Y int `json:"y,omitempty"`
	Z int `json:"z"`
}

type TerrainChunkColumnDef struct {
	X            int `json:"x"`
	Z            int `json:"z"`
	FilledVoxels int `json:"filled_voxels"`
}

type TerrainChunkEntryDef struct {
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkSize          int                  `json:"chunk_size"`
	VoxelResolution    float32              `json:"voxel_resolution"`
	TerrainID          string               `json:"terrain_id"`
	SourceHash         string               `json:"source_hash"`
	ChunkPath          string               `json:"chunk_path"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count,omitempty"`
}

type TerrainChunkManifestDef struct {
	SchemaVersion   int                    `json:"schema_version"`
	TerrainID       string                 `json:"terrain_id"`
	SourceHash      string                 `json:"source_hash"`
	ChunkSize       int                    `json:"chunk_size"`
	VoxelResolution float32                `json:"voxel_resolution"`
	Entries         []TerrainChunkEntryDef `json:"entries,omitempty"`
}

type TerrainChunkDef struct {
	SchemaVersion      int                     `json:"schema_version"`
	TerrainID          string                  `json:"terrain_id"`
	SourceHash         string                  `json:"source_hash"`
	Coord              TerrainChunkCoordDef    `json:"coord"`
	ChunkSize          int                     `json:"chunk_size"`
	VoxelResolution    float32                 `json:"voxel_resolution"`
	SolidValue         uint8                   `json:"solid_value"`
	Columns            []TerrainChunkColumnDef `json:"columns,omitempty"`
	NonEmptyVoxelCount int                     `json:"non_empty_voxel_count,omitempty"`
}

func TerrainChunkKey(coord TerrainChunkCoordDef) string {
	return coord.String()
}

func (c TerrainChunkCoordDef) String() string {
	return itoa(c.X) + ":" + itoa(c.Y) + ":" + itoa(c.Z)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	return sign + string(buf[i:])
}
