package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestRuntimeContentLoaderCachesLevelAndAssetByPath(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "ship.gkasset")
	levelPath := filepath.Join(root, "levels", "demo.gklevel")

	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(levelPath), 0755); err != nil {
		t.Fatal(err)
	}

	assetDef := content.NewAssetDef("ship")
	assetDef.Parts = []content.AssetPartDef{{
		ID:     "root-part",
		Name:   "root-part",
		Source: testProceduralPartSource(),
	}}
	if err := content.SaveAsset(assetPath, assetDef); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	levelDef := content.NewLevelDef("demo")
	levelDef.Placements = []content.LevelPlacementDef{{
		ID:        "placement-1",
		AssetPath: filepath.Join("..", "assets", "ship.gkasset"),
	}}
	if err := content.SaveLevel(levelPath, levelDef); err != nil {
		t.Fatalf("SaveLevel failed: %v", err)
	}

	loader := NewRuntimeContentLoader()
	firstAsset, err := loader.LoadAsset(assetPath)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	secondAsset, err := loader.LoadAsset(assetPath)
	if err != nil {
		t.Fatalf("LoadAsset failed: %v", err)
	}
	if firstAsset != secondAsset {
		t.Fatal("expected asset loads to reuse cached pointer")
	}

	firstLevel, err := loader.LoadLevel(levelPath)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	secondLevel, err := loader.LoadLevel(levelPath)
	if err != nil {
		t.Fatalf("LoadLevel failed: %v", err)
	}
	if firstLevel != secondLevel {
		t.Fatal("expected level loads to reuse cached pointer")
	}
}

func TestRuntimeContentLoaderCachesTerrainManifestAndChunkByPath(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "terrain", "demo.gkterrainmanifest")
	chunkPath := filepath.Join(root, "terrain", "demo_chunks", "0_0_0.gkchunk")

	if err := os.MkdirAll(filepath.Dir(chunkPath), 0755); err != nil {
		t.Fatal(err)
	}

	chunkDef := &content.TerrainChunkDef{
		TerrainID:          "terrain-1",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          8,
		VoxelResolution:    1,
		SolidValue:         2,
		Columns:            []content.TerrainChunkColumnDef{{X: 1, Z: 2, FilledVoxels: 3}},
		NonEmptyVoxelCount: 3,
	}
	if err := content.SaveTerrainChunk(chunkPath, chunkDef); err != nil {
		t.Fatalf("SaveTerrainChunk failed: %v", err)
	}

	manifestDef := &content.TerrainChunkManifestDef{
		TerrainID:       "terrain-1",
		ChunkSize:       8,
		VoxelResolution: 1,
		Entries: []content.TerrainChunkEntryDef{{
			Coord:              chunkDef.Coord,
			ChunkSize:          chunkDef.ChunkSize,
			VoxelResolution:    chunkDef.VoxelResolution,
			TerrainID:          chunkDef.TerrainID,
			ChunkPath:          content.AuthorDocumentPath(chunkPath, manifestPath),
			NonEmptyVoxelCount: chunkDef.NonEmptyVoxelCount,
		}},
	}
	if err := content.SaveTerrainChunkManifest(manifestPath, manifestDef); err != nil {
		t.Fatalf("SaveTerrainChunkManifest failed: %v", err)
	}

	loader := NewRuntimeContentLoader()
	firstManifest, err := loader.LoadTerrainChunkManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadTerrainChunkManifest failed: %v", err)
	}
	secondManifest, err := loader.LoadTerrainChunkManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadTerrainChunkManifest failed: %v", err)
	}
	if firstManifest != secondManifest {
		t.Fatal("expected manifest loads to reuse cached pointer")
	}

	resolvedChunkPath := content.ResolveTerrainChunkPath(firstManifest.Entries[0], manifestPath)
	firstChunk, err := loader.LoadTerrainChunk(resolvedChunkPath)
	if err != nil {
		t.Fatalf("LoadTerrainChunk failed: %v", err)
	}
	secondChunk, err := loader.LoadTerrainChunk(resolvedChunkPath)
	if err != nil {
		t.Fatalf("LoadTerrainChunk failed: %v", err)
	}
	if firstChunk != secondChunk {
		t.Fatal("expected terrain chunk loads to reuse cached pointer")
	}
}
