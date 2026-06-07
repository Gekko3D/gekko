package common

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestBuildImportedWorldEmissionPartitionsVoxels(t *testing.T) {
	emission, err := BuildImportedWorldEmission([]Voxel{
		{X: 0, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
		{X: 31, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
		{X: 32, Y: 0, Z: 0, Palette: 2, MaterialID: 2},
		{X: -1, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
		{X: 99, Y: 0, Z: 0, Palette: 0, MaterialID: 1},
	}, []Material{
		{ID: 1, PaletteIndex: 1, BaseColor: [4]uint8{10, 20, 30, 255}},
		{ID: 2, PaletteIndex: 2, BaseColor: [4]uint8{40, 50, 60, 255}},
	}, ImportedWorldEmitOptions{
		WorldID:            "test_world",
		ChunkSize:          32,
		VoxelResolution:    0.1,
		ChunkDirectoryName: "chunks",
		SourceBuildVersion: "test_v1",
		SourceHash:         "hash",
		Tags:               []string{"source:test"},
	})
	if err != nil {
		t.Fatalf("BuildImportedWorldEmission failed: %v", err)
	}
	if emission.TotalVoxelCount != 4 {
		t.Fatalf("total voxels = %d", emission.TotalVoxelCount)
	}
	if len(emission.Chunks) != 3 {
		t.Fatalf("chunks = %d", len(emission.Chunks))
	}
	if emission.Manifest.Palette[1] != (content.ImportedWorldPaletteColor{10, 20, 30, 255}) {
		t.Fatalf("palette[1] = %+v", emission.Manifest.Palette[1])
	}
	negativeChunk := emission.Chunks[[3]int{-1, 0, 0}]
	if negativeChunk == nil || len(negativeChunk.Voxels) != 1 || negativeChunk.Voxels[0].X != 31 {
		t.Fatalf("negative chunk = %+v", negativeChunk)
	}
}

func TestSaveImportedWorldEmissionRoundTripsAndValidates(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "worlds", "test_world.gkworld")
	emission, err := BuildImportedWorldEmission([]Voxel{
		{X: 0, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
	}, []Material{{ID: 1, PaletteIndex: 1}}, ImportedWorldEmitOptions{
		WorldID:         "test_world",
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportedWorldEmission failed: %v", err)
	}
	if err := SaveImportedWorldEmission(manifestPath, emission); err != nil {
		t.Fatalf("SaveImportedWorldEmission failed: %v", err)
	}
	loaded, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if validation := content.ValidateImportedWorld(loaded, content.ImportedWorldValidationOptions{DocumentPath: manifestPath}); validation.HasErrors() {
		t.Fatalf("ValidateImportedWorld failed: %s", validation.Error())
	}
	chunkPath := content.ResolveImportedWorldChunkPath(loaded.Entries[0], manifestPath)
	chunk, err := content.LoadImportedWorldChunk(chunkPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if chunk.NonEmptyVoxelCount != 1 {
		t.Fatalf("chunk voxels = %d", chunk.NonEmptyVoxelCount)
	}
}
