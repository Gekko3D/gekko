package content

import (
	"path/filepath"
	"testing"
)

func TestImportedWorldRoundTripPreservesManifestFields(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "demo_chunks", "0_0_0.gkchunk")

	chunk := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          16,
		VoxelResolution:    1,
		Voxels:             []ImportedWorldVoxelDef{{X: 1, Y: 2, Z: 3, Value: 4}},
		NonEmptyVoxelCount: 1,
	}
	if err := SaveImportedWorldChunk(chunkPath, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}

	def := &ImportedWorldDef{
		WorldID:            "world-a",
		Kind:               ImportedWorldKindVoxelWorld,
		ChunkSize:          16,
		VoxelResolution:    1,
		Palette:            []ImportedWorldPaletteColor{{0, 0, 0, 0}, {10, 20, 30, 255}},
		SourceBuildVersion: "importer-1",
		SourceHash:         "abc123",
		Tags:               []string{"hl"},
		Entries: []ImportedWorldChunkEntryDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkPath:          AuthorDocumentPath(chunkPath, manifestPath),
			NonEmptyVoxelCount: 1,
			Tags:               []string{"solid"},
		}},
	}
	if err := SaveImportedWorld(manifestPath, def); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	loaded, err := LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentImportedWorldSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentImportedWorldSchemaVersion, loaded.SchemaVersion)
	}
	if loaded.WorldID != def.WorldID || loaded.Kind != def.Kind || loaded.SourceHash != def.SourceHash {
		t.Fatalf("unexpected imported world round-trip %+v", loaded)
	}
	if len(loaded.Palette) != len(def.Palette) || loaded.Palette[1] != def.Palette[1] {
		t.Fatalf("expected imported world palette to round-trip, got %+v", loaded.Palette)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].ChunkPath != def.Entries[0].ChunkPath {
		t.Fatalf("expected imported world entries to round-trip, got %+v", loaded.Entries)
	}
}

func TestImportedWorldChunkRoundTripPreservesSparseVoxels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk.gkchunk")
	def := &ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              TerrainChunkCoordDef{X: 1, Y: 2, Z: 3},
		ChunkSize:          16,
		VoxelResolution:    0.5,
		Voxels:             []ImportedWorldVoxelDef{{X: 4, Y: 5, Z: 6, Value: 7}},
		NonEmptyVoxelCount: 1,
		Tags:               []string{"walkable"},
	}
	if err := SaveImportedWorldChunk(path, def); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	loaded, err := LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		t.Fatalf("expected chunk schema version %d, got %d", CurrentImportedWorldChunkSchemaVersion, loaded.SchemaVersion)
	}
	if len(loaded.Voxels) != 1 || loaded.Voxels[0] != def.Voxels[0] {
		t.Fatalf("expected sparse voxels to round-trip, got %+v", loaded.Voxels)
	}
}

func TestValidateImportedWorldRejectsBrokenManifestEntries(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "broken.gkworld")
	def := &ImportedWorldDef{
		WorldID:         "",
		Kind:            ImportedWorldKind("bad"),
		ChunkSize:       0,
		VoxelResolution: 0,
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, ChunkPath: "missing.gkchunk"},
			{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, ChunkPath: "bad.txt"},
		},
	}

	result := ValidateImportedWorld(def, ImportedWorldValidationOptions{DocumentPath: manifestPath})
	if !result.HasErrors() {
		t.Fatal("expected imported world validation errors")
	}
	assertImportedWorldValidationCode(t, result, "empty_world_id")
	assertImportedWorldValidationCode(t, result, "invalid_world_kind")
	assertImportedWorldValidationCode(t, result, "invalid_chunk_size")
	assertImportedWorldValidationCode(t, result, "invalid_voxel_resolution")
	assertImportedWorldValidationCode(t, result, "missing_chunk_file")
	assertImportedWorldValidationCode(t, result, "duplicate_chunk_coord")
}

func assertImportedWorldValidationCode(t *testing.T, result ImportedWorldValidationResult, code string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected validation code %q in %+v", code, result.Issues)
}
