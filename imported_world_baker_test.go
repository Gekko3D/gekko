package gekko

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSaveImportedWorldBakeReportWritesJSON(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "reports", "station_bake_report.json")

	result, err := BakeImportedWorldFromVox(&VoxFile{
		Models: []VoxModel{{
			SizeX: 1,
			SizeY: 1,
			SizeZ: 1,
			Voxels: []Voxel{
				{X: 0, Y: 0, Z: 0, ColorIndex: 5},
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
	if err := SaveImportedWorldBakeReport(reportPath, result); err != nil {
		t.Fatalf("SaveImportedWorldBakeReport failed: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"world_id": "station"`) {
		t.Fatalf("expected report to contain world id, got %s", body)
	}
	if !strings.Contains(body, `"total_voxel_count": 1`) {
		t.Fatalf("expected report to contain voxel count, got %s", body)
	}
}

func TestBakeImportedWorldFromScenePreservesAdjacentVoxelInstances(t *testing.T) {
	vox := &VoxFile{
		Models: []VoxModel{{
			SizeX:  1,
			SizeY:  1,
			SizeZ:  1,
			Voxels: []Voxel{{X: 0, Y: 0, Z: 0, ColorIndex: 1}},
		}},
		Nodes: map[int]VoxNode{
			0: {ID: 0, Type: VoxNodeGroup, ChildrenIDs: []int{1, 3}},
			1: {
				ID:      1,
				Type:    VoxNodeTransform,
				ChildID: 2,
				Frames:  []VoxTransformFrame{{Rotation: 0, LocalTrans: [3]float32{0, 0, 0}}},
			},
			2: {ID: 2, Type: VoxNodeShape, Models: []VoxShapeModel{{ModelID: 0}}},
			3: {
				ID:      3,
				Type:    VoxNodeTransform,
				ChildID: 4,
				Frames:  []VoxTransformFrame{{Rotation: 0, LocalTrans: [3]float32{1, 0, 0}}},
			},
			4: {ID: 4, Type: VoxNodeShape, Models: []VoxShapeModel{{ModelID: 0}}},
		},
	}

	result, err := BakeImportedWorldFromVox(vox, ImportedWorldBakeConfig{
		WorldID:         "scene_adjacent",
		ChunkSize:       32,
		VoxelResolution: 1,
	})
	if err != nil {
		t.Fatalf("BakeImportedWorldFromVox failed: %v", err)
	}

	chunk := result.Chunks[ChunkCoord{X: 0, Y: 0, Z: 0}]
	if chunk == nil {
		t.Fatalf("expected baked chunk at origin, got %+v", result.Chunks)
	}
	if len(chunk.Voxels) != 2 {
		t.Fatalf("expected 2 voxels in baked chunk, got %+v", chunk.Voxels)
	}
	if chunk.Voxels[0].X != 0 || chunk.Voxels[1].X != 1 {
		t.Fatalf("expected adjacent voxel x positions [0 1], got %+v", chunk.Voxels)
	}
}
