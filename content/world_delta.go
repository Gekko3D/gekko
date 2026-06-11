package content

import "path/filepath"

const (
	CurrentWorldDeltaSchemaVersion          = 1
	CurrentVoxelObjectSnapshotSchemaVersion = 1
)

type WorldDeltaDef struct {
	SchemaVersion               int                             `json:"schema_version"`
	LevelID                     string                          `json:"level_id"`
	PlacementTransformOverrides []PlacementTransformOverrideDef `json:"placement_transform_overrides,omitempty"`
	PlacementDeletions          []PlacementDeletionDef          `json:"placement_deletions,omitempty"`
	TerrainChunkOverrides       []TerrainChunkOverrideDef       `json:"terrain_chunk_overrides,omitempty"`
	ImportedWorldChunkOverrides []ImportedWorldChunkOverrideDef `json:"imported_world_chunk_overrides,omitempty"`
	NavigationTileOverrides     []NavigationTileOverrideDef     `json:"navigation_tile_overrides,omitempty"`
	VoxelObjectOverrides        []VoxelObjectOverrideDef        `json:"voxel_object_overrides,omitempty"`
}

type PlacementTransformOverrideDef struct {
	PlacementID string            `json:"placement_id"`
	Transform   LevelTransformDef `json:"transform"`
}

type PlacementDeletionDef struct {
	PlacementID string `json:"placement_id"`
}

type TerrainChunkOverrideDef struct {
	TerrainID    string               `json:"terrain_id"`
	ChunkCoord   TerrainChunkCoordDef `json:"chunk_coord"`
	SnapshotPath string               `json:"snapshot_path"`
}

type ImportedWorldChunkOverrideDef struct {
	WorldID      string               `json:"world_id"`
	ChunkCoord   TerrainChunkCoordDef `json:"chunk_coord"`
	SnapshotPath string               `json:"snapshot_path"`
}

type NavigationTileOverrideDef struct {
	NavID              string               `json:"nav_id"`
	AgentProfileID     string               `json:"agent_profile_id"`
	ChunkCoord         TerrainChunkCoordDef `json:"chunk_coord"`
	TilePath           string               `json:"tile_path,omitempty"`
	SourceDeltaHash    string               `json:"source_delta_hash,omitempty"`
	NavBuildHash       string               `json:"nav_build_hash,omitempty"`
	Empty              bool                 `json:"empty,omitempty"`
	SourceOverrideKind string               `json:"source_override_kind,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
}

type VoxelObjectOverrideDef struct {
	PlacementID  string `json:"placement_id"`
	ItemID       string `json:"item_id"`
	SnapshotPath string `json:"snapshot_path"`
}

type VoxelObjectSnapshotDef struct {
	SchemaVersion int                   `json:"schema_version"`
	Voxels        []VoxelObjectVoxelDef `json:"voxels,omitempty"`
}

type VoxelObjectVoxelDef struct {
	X     int   `json:"x"`
	Y     int   `json:"y"`
	Z     int   `json:"z"`
	Value uint8 `json:"value"`
}

func DefaultWorldDeltaPath(levelPath string) string {
	base := filepath.Base(levelPath)
	if base == "" {
		base = "level.gklevel"
	}
	return filepath.Join(filepath.Dir(levelPath), trimKnownSuffix(base, ".gklevel")+".gkworlddelta")
}

func DefaultWorldDeltaDataDir(deltaPath string) string {
	base := filepath.Base(deltaPath)
	if base == "" {
		base = "level.gkworlddelta"
	}
	return filepath.Join(filepath.Dir(deltaPath), base+"_data")
}
