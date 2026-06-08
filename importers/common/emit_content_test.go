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
		{ID: 2, PaletteIndex: 2, BaseColor: [4]uint8{40, 50, 60, 255}, SourceTextureName: "LIGHT01", EmitsLight: true, Emissive: 2.5},
	}, ImportedWorldEmitOptions{
		WorldID:            "test_world",
		ChunkSize:          32,
		VoxelResolution:    0.1,
		ChunkDirectoryName: "chunks",
		SourceBuildVersion: "test_v1",
		SourceHash:         "hash",
		SourceMaterials: []Material{{
			ID:                7,
			PaletteIndex:      7,
			SourceTextureName: "METAL01",
			BaseColor:         [4]uint8{80, 90, 100, 255},
			Kind:              "metal",
			CollisionKind:     "solid",
			Roughness:         0.5,
			Metallic:          0.85,
			Tags:              []string{"material:metal"},
		}},
		Tags: []string{"source:test"},
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
	if len(emission.Manifest.Materials) != 2 || !emission.Manifest.Materials[1].EmitsLight || emission.Manifest.Materials[1].Emissive != 2.5 {
		t.Fatalf("materials = %+v", emission.Manifest.Materials)
	}
	if len(emission.Manifest.SourceMaterials) != 1 || emission.Manifest.SourceMaterials[0].Kind != "metal" || emission.Manifest.SourceMaterials[0].Metallic != 0.85 || len(emission.Manifest.SourceMaterials[0].Tags) == 0 {
		t.Fatalf("source materials = %+v", emission.Manifest.SourceMaterials)
	}
	if len(emission.Manifest.Sectors) != 2 {
		t.Fatalf("sectors = %+v", emission.Manifest.Sectors)
	}
	if len(emission.Manifest.Sectors[1].LODs) != 1 {
		t.Fatalf("expected sector lod metadata, got %+v", emission.Manifest.Sectors[1])
	}
	negativeChunk := emission.Chunks[[3]int{-1, 0, 0}]
	if negativeChunk == nil || len(negativeChunk.Voxels) != 1 || negativeChunk.Voxels[0].X != 31 {
		t.Fatalf("negative chunk = %+v", negativeChunk)
	}
}

func TestBuildImportedWorldEmissionCreatesMaterialPaletteForTransparentVoxels(t *testing.T) {
	emission, err := BuildImportedWorldEmission([]Voxel{
		{X: 0, Y: 0, Z: 0, Palette: 4, SolidKind: "structural"},
		{X: 1, Y: 0, Z: 0, Palette: 4, SolidKind: "glass"},
	}, []Material{
		{ID: 4, PaletteIndex: 4, BaseColor: [4]uint8{120, 180, 220, 255}, Kind: "baked_texture", Roughness: 0.9},
	}, ImportedWorldEmitOptions{
		WorldID:         "test_world",
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportedWorldEmission failed: %v", err)
	}
	chunk := emission.Chunks[[3]int{0, 0, 0}]
	if chunk == nil || len(chunk.Voxels) != 2 {
		t.Fatalf("chunk = %+v", chunk)
	}
	opaque := chunk.Voxels[0]
	glass := chunk.Voxels[1]
	if opaque.Value != 4 || glass.Value != 4 {
		t.Fatalf("expected both voxels to preserve baked color value 4, got %+v", chunk.Voxels)
	}
	if content.ImportedWorldVoxelMaterialValue(opaque) == content.ImportedWorldVoxelMaterialValue(glass) {
		t.Fatalf("expected structural and glass material values to differ, got %+v", chunk.Voxels)
	}
	glassMaterial, ok := content.FindImportedWorldMaterialByPaletteIndex(emission.Manifest, content.ImportedWorldVoxelMaterialValue(glass))
	if !ok || glassMaterial.Kind != "glass" || !glassMaterial.Transparent || glassMaterial.Transparency < 0.5 {
		t.Fatalf("expected glass runtime material, got %+v ok=%t", glassMaterial, ok)
	}
	if emission.Manifest.MaterialPalette[content.ImportedWorldVoxelMaterialValue(glass)] != (content.ImportedWorldPaletteColor{120, 180, 220, 255}) {
		t.Fatalf("expected glass material palette color to preserve baked color, got %+v", emission.Manifest.MaterialPalette[content.ImportedWorldVoxelMaterialValue(glass)])
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
	if len(loaded.Sectors) != 1 || len(loaded.Sectors[0].LODs) != 1 {
		t.Fatalf("expected saved sector proxy lod, got %+v", loaded.Sectors)
	}
	proxyPath := content.ResolveDocumentPath(loaded.Sectors[0].LODs[0].ChunkPath, manifestPath)
	proxy, err := content.LoadImportedWorldChunk(proxyPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk proxy failed: %v", err)
	}
	if proxy.NonEmptyVoxelCount != 1 || proxy.VoxelResolution <= loaded.VoxelResolution {
		t.Fatalf("unexpected proxy chunk %+v", proxy)
	}
}

func TestSaveImportedWorldEmissionWritesMaterialDenseRLEWhenNeeded(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "worlds", "test_world.gkworld")
	emission, err := BuildImportedWorldEmission([]Voxel{
		{X: 0, Y: 0, Z: 0, Palette: 4, SolidKind: "structural"},
		{X: 1, Y: 0, Z: 0, Palette: 4, SolidKind: "glass"},
	}, []Material{{ID: 4, PaletteIndex: 4, BaseColor: [4]uint8{120, 180, 220, 255}}}, ImportedWorldEmitOptions{
		WorldID:         "test_world",
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportedWorldEmission failed: %v", err)
	}
	if err := SaveImportedWorldEmissionWithOptions(manifestPath, emission, ImportedWorldSaveOptions{
		ChunkPayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1,
	}); err != nil {
		t.Fatalf("SaveImportedWorldEmissionWithOptions failed: %v", err)
	}
	loaded, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if loaded.ChunkPayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
		t.Fatalf("manifest payload kind = %q", loaded.ChunkPayloadKind)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
		t.Fatalf("entry payload metadata = %+v", loaded.Entries)
	}
	chunk, err := content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(loaded.Entries[0], manifestPath))
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if chunk.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 || content.ImportedWorldVoxelMaterialValue(chunk.Voxels[0]) == content.ImportedWorldVoxelMaterialValue(chunk.Voxels[1]) {
		t.Fatalf("expected material-value chunk, got %+v", chunk)
	}
}

func TestSaveImportedWorldEmissionCanWriteDenseRLEBinaryChunks(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "worlds", "test_world.gkworld")
	emission, err := BuildImportedWorldEmission([]Voxel{
		{X: 0, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
		{X: 1, Y: 0, Z: 0, Palette: 1, MaterialID: 1},
		{X: 31, Y: 31, Z: 31, Palette: 2, MaterialID: 2},
	}, []Material{{ID: 1, PaletteIndex: 1}, {ID: 2, PaletteIndex: 2}}, ImportedWorldEmitOptions{
		WorldID:         "test_world",
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportedWorldEmission failed: %v", err)
	}
	if err := SaveImportedWorldEmissionWithOptions(manifestPath, emission, ImportedWorldSaveOptions{
		ChunkPayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1,
	}); err != nil {
		t.Fatalf("SaveImportedWorldEmissionWithOptions failed: %v", err)
	}
	loaded, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if loaded.ChunkPayloadKind != content.ImportedWorldChunkPayloadDenseRLEBinaryV1 {
		t.Fatalf("manifest payload kind = %q", loaded.ChunkPayloadKind)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].PayloadKind != content.ImportedWorldChunkPayloadDenseRLEBinaryV1 || loaded.Entries[0].PayloadHash == "" {
		t.Fatalf("entry payload metadata = %+v", loaded.Entries)
	}
	if len(loaded.Sectors) != 1 || len(loaded.Sectors[0].LODs) != 1 || loaded.Sectors[0].LODs[0].PayloadKind != content.ImportedWorldChunkPayloadDenseRLEBinaryV1 || loaded.Sectors[0].LODs[0].PayloadHash == "" {
		t.Fatalf("sector lod payload metadata = %+v", loaded.Sectors)
	}
	chunkPath := content.ResolveImportedWorldChunkPath(loaded.Entries[0], manifestPath)
	chunk, err := content.LoadImportedWorldChunk(chunkPath)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if chunk.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEBinaryV1 || chunk.NonEmptyVoxelCount != 3 || len(chunk.Voxels) != 3 {
		t.Fatalf("chunk = %+v", chunk)
	}
}
