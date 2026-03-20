package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestBakeImportedWorldFromVoxBuildsDeterministicChunkedWorld(t *testing.T) {
	voxFile := &VoxFile{
		Models: []VoxModel{{
			SizeX: 40,
			SizeY: 4,
			SizeZ: 4,
			Voxels: []Voxel{
				{X: 0, Y: 0, Z: 0, ColorIndex: 1},
				{X: 31, Y: 0, Z: 0, ColorIndex: 2},
				{X: 32, Y: 0, Z: 0, ColorIndex: 3},
				{X: 39, Y: 3, Z: 3, ColorIndex: 4},
			},
		}},
	}

	result, err := BakeImportedWorldFromVox(voxFile, ImportedWorldBakeConfig{
		WorldID:           "station",
		ChunkSize:         32,
		VoxelResolution:   1,
		NormalizeToOrigin: true,
	})
	if err != nil {
		t.Fatalf("BakeImportedWorldFromVox failed: %v", err)
	}

	if result.Manifest == nil {
		t.Fatal("expected manifest")
	}
	if got := len(result.Manifest.Entries); got != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", got)
	}
	if result.TotalVoxelCount != 4 {
		t.Fatalf("expected 4 baked voxels, got %d", result.TotalVoxelCount)
	}

	first := result.Chunks[ChunkCoord{X: 0, Y: 0, Z: 0}]
	second := result.Chunks[ChunkCoord{X: 1, Y: 0, Z: 0}]
	if first == nil || second == nil {
		t.Fatalf("expected chunks at 0 and 1, got %+v", result.Chunks)
	}
	if first.NonEmptyVoxelCount != 2 {
		t.Fatalf("expected first chunk to contain 2 voxels, got %d", first.NonEmptyVoxelCount)
	}
	if second.NonEmptyVoxelCount != 2 {
		t.Fatalf("expected second chunk to contain 2 voxels, got %d", second.NonEmptyVoxelCount)
	}
	if second.Voxels[0].X != 0 {
		t.Fatalf("expected second chunk local X to reset at boundary, got %+v", second.Voxels[0])
	}
}

func TestSaveImportedWorldBakeWritesManifestAndChunkFiles(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "worlds", "station.gkworld")

	result, err := BakeImportedWorldFromVox(&VoxFile{
		Models: []VoxModel{{
			SizeX: 2,
			SizeY: 2,
			SizeZ: 2,
			Voxels: []Voxel{
				{X: 0, Y: 0, Z: 0, ColorIndex: 9},
				{X: 1, Y: 0, Z: 0, ColorIndex: 7},
			},
		}},
	}, ImportedWorldBakeConfig{
		WorldID:           "station",
		ChunkSize:         32,
		VoxelResolution:   1,
		NormalizeToOrigin: true,
	})
	if err != nil {
		t.Fatalf("BakeImportedWorldFromVox failed: %v", err)
	}
	if err := SaveImportedWorldBake(manifestPath, result); err != nil {
		t.Fatalf("SaveImportedWorldBake failed: %v", err)
	}

	manifest, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if manifest.WorldID != "station" {
		t.Fatalf("expected station world id, got %q", manifest.WorldID)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("expected one chunk entry, got %d", len(manifest.Entries))
	}
	chunkPath := content.ResolveImportedWorldChunkPath(manifest.Entries[0], manifestPath)
	if _, err := os.Stat(chunkPath); err != nil {
		t.Fatalf("expected chunk file %s: %v", chunkPath, err)
	}
	chunk, err := content.LoadImportedWorldChunk(chunkPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if chunk.NonEmptyVoxelCount != 2 {
		t.Fatalf("expected two chunk voxels, got %d", chunk.NonEmptyVoxelCount)
	}
}
