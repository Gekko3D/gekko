package content

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorldDeltaRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.gkworlddelta")
	def := &WorldDeltaDef{
		LevelID: "level-a",
		PlacementTransformOverrides: []PlacementTransformOverrideDef{{
			PlacementID: "ship",
			Transform: LevelTransformDef{
				Position: Vec3{1, 2, 3},
				Rotation: Quat{0, 0, 0, 1},
				Scale:    Vec3{4, 5, 6},
			},
		}},
		PlacementDeletions: []PlacementDeletionDef{{PlacementID: "deleted"}},
		TerrainChunkOverrides: []TerrainChunkOverrideDef{{
			TerrainID:    "terrain-a",
			ChunkCoord:   TerrainChunkCoordDef{X: 3, Y: 0, Z: -2},
			SnapshotPath: "demo.gkworlddelta_data/terrain_3_0_-2.gkchunk",
		}},
		VoxelObjectOverrides: []VoxelObjectOverrideDef{{
			PlacementID:  "ship",
			ItemID:       "hull",
			SnapshotPath: "demo.gkworlddelta_data/ship_hull.gkvoxobj",
		}},
	}

	if err := SaveWorldDelta(path, def); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}
	loaded, err := LoadWorldDelta(path)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if !reflect.DeepEqual(def, loaded) {
		t.Fatalf("expected round-trip match, want=%+v got=%+v", def, loaded)
	}
}

func TestTerrainChunkOverrideRefRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terrain_override.gkworlddelta")
	def := &WorldDeltaDef{
		LevelID: "level-a",
		TerrainChunkOverrides: []TerrainChunkOverrideDef{{
			TerrainID:    "terrain-a",
			ChunkCoord:   TerrainChunkCoordDef{X: -1, Y: 0, Z: 4},
			SnapshotPath: "terrain_override.gkworlddelta_data/chunk_-1_0_4.gkchunk",
		}},
	}

	if err := SaveWorldDelta(path, def); err != nil {
		t.Fatalf("SaveWorldDelta failed: %v", err)
	}
	loaded, err := LoadWorldDelta(path)
	if err != nil {
		t.Fatalf("LoadWorldDelta failed: %v", err)
	}
	if !reflect.DeepEqual(def.TerrainChunkOverrides, loaded.TerrainChunkOverrides) {
		t.Fatalf("expected terrain override round-trip, want=%+v got=%+v", def.TerrainChunkOverrides, loaded.TerrainChunkOverrides)
	}
}

func TestVoxelObjectSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "obj.gkvoxobj")
	def := &VoxelObjectSnapshotDef{
		Voxels: []VoxelObjectVoxelDef{
			{X: -2, Y: 1, Z: 9, Value: 3},
			{X: 4, Y: 5, Z: -6, Value: 7},
		},
	}

	if err := SaveVoxelObjectSnapshot(path, def); err != nil {
		t.Fatalf("SaveVoxelObjectSnapshot failed: %v", err)
	}
	loaded, err := LoadVoxelObjectSnapshot(path)
	if err != nil {
		t.Fatalf("LoadVoxelObjectSnapshot failed: %v", err)
	}
	if !reflect.DeepEqual(def, loaded) {
		t.Fatalf("expected voxel snapshot round-trip, want=%+v got=%+v", def, loaded)
	}
}

func TestDefaultWorldDeltaPaths(t *testing.T) {
	levelPath := filepath.Join("/tmp", "worlds", "demo.gklevel")
	deltaPath := DefaultWorldDeltaPath(levelPath)
	if deltaPath != filepath.Join("/tmp", "worlds", "demo.gkworlddelta") {
		t.Fatalf("unexpected delta path %q", deltaPath)
	}
	if got := DefaultWorldDeltaDataDir(deltaPath); got != filepath.Join("/tmp", "worlds", "demo.gkworlddelta_data") {
		t.Fatalf("unexpected data dir %q", got)
	}
}

func TestLoadWorldDeltaMissingFile(t *testing.T) {
	_, err := LoadWorldDelta(filepath.Join(t.TempDir(), "missing.gkworlddelta"))
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected missing file error, got %v", err)
	}
}
