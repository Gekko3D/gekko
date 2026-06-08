package content

import (
	"math"
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
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Palette:         []ImportedWorldPaletteColor{{0, 0, 0, 0}, {10, 20, 30, 255}},
		Materials: []ImportedWorldMaterialDef{{
			ID:                42,
			PaletteIndex:      1,
			SourceTextureName: "LIGHT01",
			BaseColor:         ImportedWorldPaletteColor{10, 20, 30, 255},
			Kind:              "emissive",
			CollisionKind:     "solid",
			EmitsLight:        true,
			Emissive:          2.5,
			Roughness:         0.35,
			Metallic:          0.2,
			Tags:              []string{"material:emissive"},
		}},
		SourceMaterials: []ImportedWorldMaterialDef{{
			ID:                7,
			PaletteIndex:      7,
			SourceTextureName: "METAL01",
			BaseColor:         ImportedWorldPaletteColor{80, 90, 100, 255},
			Kind:              "metal",
			CollisionKind:     "solid",
			Roughness:         0.5,
			Metallic:          0.85,
			Tags:              []string{"material:metal"},
		}},
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
	if len(loaded.Materials) != 1 || !loaded.Materials[0].EmitsLight || loaded.Materials[0].Emissive != 2.5 {
		t.Fatalf("expected imported world materials to round-trip, got %+v", loaded.Materials)
	}
	if loaded.Materials[0].Roughness != 0.35 || loaded.Materials[0].Metallic != 0.2 || len(loaded.Materials[0].Tags) != 1 {
		t.Fatalf("expected imported world material metadata to round-trip, got %+v", loaded.Materials[0])
	}
	if len(loaded.SourceMaterials) != 1 || loaded.SourceMaterials[0].Kind != "metal" || loaded.SourceMaterials[0].Metallic != 0.85 {
		t.Fatalf("expected source materials to round-trip, got %+v", loaded.SourceMaterials)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].ChunkPath != def.Entries[0].ChunkPath {
		t.Fatalf("expected imported world entries to round-trip, got %+v", loaded.Entries)
	}
	if len(loaded.Sectors) != 1 || len(loaded.Sectors[0].FullChunkRefs) != 1 || loaded.Sectors[0].FullChunkRefs[0] != def.Entries[0].Coord {
		t.Fatalf("expected imported world sectors to reference chunk entries, got %+v", loaded.Sectors)
	}
}

func TestImportedWorldRoundTripPreservesSectorVisibilityRefs(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPathA := filepath.Join(root, "worlds", "demo_chunks", "0_0_0.gkchunk")
	chunkPathB := filepath.Join(root, "worlds", "demo_chunks", "1_0_0.gkchunk")

	for _, chunk := range []struct {
		path  string
		coord TerrainChunkCoordDef
	}{
		{path: chunkPathA, coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}},
		{path: chunkPathB, coord: TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}},
	} {
		if err := SaveImportedWorldChunk(chunk.path, &ImportedWorldChunkDef{
			WorldID:         "world-a",
			Coord:           chunk.coord,
			ChunkSize:       16,
			VoxelResolution: 1,
			Voxels:          []ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 1}},
		}); err != nil {
			t.Fatalf("SaveImportedWorldChunk failed: %v", err)
		}
	}

	def := &ImportedWorldDef{
		WorldID:         "world-a",
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, ChunkPath: AuthorDocumentPath(chunkPathA, manifestPath), NonEmptyVoxelCount: 1},
			{Coord: TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}, ChunkPath: AuthorDocumentPath(chunkPathB, manifestPath), NonEmptyVoxelCount: 1},
		},
		Sectors: []ImportedWorldSectorDef{{
			Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin:          [3]float32{0, 0, 0},
			BoundsMax:          [3]float32{16, 16, 16},
			FullChunkRefs:      []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
			VisibilityID:       "hl1_leaf:4",
			SourceLeafIDs:      []int{4, 8},
			VisibleSectorRefs:  []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}},
			AdjacentSectorRefs: []TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
		}, {
			Coord:              TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			BoundsMin:          [3]float32{16, 0, 0},
			BoundsMax:          [3]float32{32, 16, 16},
			FullChunkRefs:      []TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
			VisibleSectorRefs:  []TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
			AdjacentSectorRefs: []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		}},
	}
	if err := SaveImportedWorld(manifestPath, def); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}
	loaded, err := LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if validation := ValidateImportedWorld(loaded, ImportedWorldValidationOptions{DocumentPath: manifestPath}); validation.HasErrors() {
		t.Fatalf("ValidateImportedWorld failed: %s", validation.Error())
	}
	sector := loaded.Sectors[0]
	if sector.VisibilityID != "hl1_leaf:4" || len(sector.SourceLeafIDs) != 2 || len(sector.VisibleSectorRefs) != 2 || len(sector.AdjacentSectorRefs) != 1 {
		t.Fatalf("expected sector visibility metadata to round-trip, got %+v", sector)
	}
}

func TestBuildImportedWorldSectorsGroupsChunksByTargetWorldSize(t *testing.T) {
	entries := []ImportedWorldChunkEntryDef{
		{Coord: TerrainChunkCoordDef{X: -1, Y: 0, Z: 0}, NonEmptyVoxelCount: 2},
		{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, NonEmptyVoxelCount: 3},
		{Coord: TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}, NonEmptyVoxelCount: 5},
	}

	sectors := BuildImportedWorldSectors(entries, 128, 0.1, DefaultImportedWorldSectorTargetWorldSize)

	if len(sectors) != 2 {
		t.Fatalf("expected negative and positive chunk groups, got %+v", sectors)
	}
	if sectors[0].Coord != (TerrainChunkCoordDef{X: -1, Y: 0, Z: 0}) || len(sectors[0].FullChunkRefs) != 1 || sectors[0].NonEmptyVoxelCount != 2 {
		t.Fatalf("unexpected negative sector %+v", sectors[0])
	}
	if sectors[1].Coord != (TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}) || len(sectors[1].FullChunkRefs) != 2 || sectors[1].NonEmptyVoxelCount != 8 {
		t.Fatalf("unexpected positive sector %+v", sectors[1])
	}
	if math.Abs(float64(sectors[1].BoundsMax[0]-sectors[1].BoundsMin[0]-25.6)) > 0.001 {
		t.Fatalf("expected two 12.8m chunks per sector, got bounds %+v..%+v", sectors[1].BoundsMin, sectors[1].BoundsMax)
	}
}

func TestBuildImportedWorldSectorProxyChunksDownsamplesFullChunks(t *testing.T) {
	sector := ImportedWorldSectorDef{
		Coord:              TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		BoundsMin:          [3]float32{0, 0, 0},
		BoundsMax:          [3]float32{32, 16, 16},
		FullChunkRefs:      []TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}},
		NonEmptyVoxelCount: 4,
	}
	chunks := map[TerrainChunkCoordDef]*ImportedWorldChunkDef{
		{X: 0, Y: 0, Z: 0}: {
			WorldID:         "world-a",
			Coord:           TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			ChunkSize:       16,
			VoxelResolution: 1,
			Voxels: []ImportedWorldVoxelDef{
				{X: 0, Y: 0, Z: 0, Value: 1},
				{X: 1, Y: 0, Z: 0, Value: 2},
				{X: 2, Y: 0, Z: 0, Value: 2},
				{X: 3, Y: 0, Z: 0, Value: 2},
			},
		},
		{X: 1, Y: 0, Z: 0}: {
			WorldID:         "world-a",
			Coord:           TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			ChunkSize:       16,
			VoxelResolution: 1,
			Voxels:          []ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 3}},
		},
	}

	sectors, proxies := BuildImportedWorldSectorProxyChunks([]ImportedWorldSectorDef{sector}, chunks, ImportedWorldSectorProxyOptions{
		WorldID:         "world-a",
		ChunkSize:       16,
		VoxelResolution: 1,
		Downsample:      4,
	})

	if len(sectors) != 1 || len(sectors[0].LODs) != 1 {
		t.Fatalf("expected one sector lod, got %+v", sectors)
	}
	proxy := proxies[sectors[0].LODs[0].ChunkPath]
	if proxy == nil {
		t.Fatalf("missing proxy chunk for %+v", sectors[0].LODs[0])
	}
	if proxy.ChunkSize != 8 || proxy.VoxelResolution != 4 {
		t.Fatalf("unexpected proxy scale: %+v", proxy)
	}
	if len(proxy.Voxels) != 2 {
		t.Fatalf("expected two proxy voxels after downsample, got %+v", proxy.Voxels)
	}
	if proxy.Voxels[0] != (ImportedWorldVoxelDef{X: 0, Y: 0, Z: 0, Value: 2}) {
		t.Fatalf("expected dominant material in first proxy cell, got %+v", proxy.Voxels)
	}
	if proxy.Voxels[1] != (ImportedWorldVoxelDef{X: 4, Y: 0, Z: 0, Value: 3}) {
		t.Fatalf("expected second chunk to map into sector-local proxy coord, got %+v", proxy.Voxels)
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

func TestImportedWorldChunkDenseRLEBinaryRoundTripPreservesSparseVoxels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chunk.gkchunk")
	def := &ImportedWorldChunkDef{
		WorldID:         "world-a",
		Coord:           TerrainChunkCoordDef{X: -1, Y: 2, Z: 3},
		ChunkSize:       8,
		VoxelResolution: 0.25,
		Voxels: []ImportedWorldVoxelDef{
			{X: 0, Y: 0, Z: 0, Value: 1},
			{X: 1, Y: 0, Z: 0, Value: 1},
			{X: 7, Y: 7, Z: 7, Value: 3},
			{X: 3, Y: 4, Z: 5, Value: 2},
		},
		Tags: []string{"binary"},
	}
	if err := SaveImportedWorldChunkWithOptions(path, def, ImportedWorldChunkSaveOptions{PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
		t.Fatalf("SaveImportedWorldChunkWithOptions failed: %v", err)
	}
	if def.PayloadKind != ImportedWorldChunkPayloadDenseRLEBinaryV1 || def.PayloadHash == "" || def.PayloadSizeBytes <= 0 {
		t.Fatalf("expected binary payload metadata, got %+v", def)
	}
	loaded, err := LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatalf("LoadImportedWorldChunk failed: %v", err)
	}
	if loaded.PayloadKind != ImportedWorldChunkPayloadDenseRLEBinaryV1 || loaded.PayloadHash != def.PayloadHash {
		t.Fatalf("expected binary payload metadata to round-trip, got %+v", loaded)
	}
	if loaded.WorldID != def.WorldID || loaded.Coord != def.Coord || loaded.ChunkSize != def.ChunkSize || loaded.VoxelResolution != def.VoxelResolution {
		t.Fatalf("expected chunk metadata to round-trip, got %+v", loaded)
	}
	if len(loaded.Voxels) != len(def.Voxels) {
		t.Fatalf("expected %d voxels, got %d: %+v", len(def.Voxels), len(loaded.Voxels), loaded.Voxels)
	}
	wantVoxels := map[ImportedWorldVoxelDef]struct{}{}
	for _, voxel := range def.Voxels {
		wantVoxels[voxel] = struct{}{}
	}
	for _, voxel := range loaded.Voxels {
		if _, ok := wantVoxels[voxel]; !ok {
			t.Fatalf("unexpected loaded voxel %+v from %+v", voxel, loaded.Voxels)
		}
	}
}

func TestValidateImportedWorldRejectsBrokenManifestEntries(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "broken.gkworld")
	def := &ImportedWorldDef{
		WorldID:          "",
		Kind:             ImportedWorldKind("bad"),
		ChunkSize:        0,
		VoxelResolution:  0,
		ChunkPayloadKind: "bad",
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, ChunkPath: "missing.gkchunk"},
			{Coord: TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}, ChunkPath: "bad.txt", PayloadKind: "also_bad"},
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
	assertImportedWorldValidationCode(t, result, "invalid_chunk_payload_kind")
	assertImportedWorldValidationCode(t, result, "missing_chunk_file")
	assertImportedWorldValidationCode(t, result, "duplicate_chunk_coord")
	assertImportedWorldValidationCode(t, result, "empty_sectors")
}

func TestImportedWorldMaterialLookupHelpers(t *testing.T) {
	def := &ImportedWorldDef{
		Materials: []ImportedWorldMaterialDef{{
			ID:                1,
			PaletteIndex:      7,
			SourceTextureName: "metalwall01",
			Kind:              "metal",
			Tags:              []string{"material:metal"},
		}},
		SourceMaterials: []ImportedWorldMaterialDef{{
			ID:                2,
			PaletteIndex:      9,
			SourceTextureName: "light01",
			Kind:              "emissive",
			Tags:              []string{"material:emissive"},
		}},
	}

	material, ok := FindImportedWorldMaterialByPaletteIndex(def, 7)
	if !ok || material.Kind != "metal" {
		t.Fatalf("expected palette material lookup, got %+v ok=%t", material, ok)
	}
	material, ok = FindImportedWorldMaterialByPaletteIndex(def, 9)
	if !ok || material.Kind != "emissive" {
		t.Fatalf("expected source material fallback lookup, got %+v ok=%t", material, ok)
	}
	material, ok = FindImportedWorldMaterialBySourceTexture(def, " LIGHT01 ")
	if !ok || !ImportedWorldMaterialHasTag(material, "material:emissive") {
		t.Fatalf("expected case-insensitive source texture lookup, got %+v ok=%t", material, ok)
	}
	if _, ok := FindImportedWorldMaterialByPaletteIndex(def, 0); ok {
		t.Fatal("palette index 0 must not resolve to a material")
	}
}

func TestImportedWorldChunkPaletteAt(t *testing.T) {
	chunk := &ImportedWorldChunkDef{
		ChunkSize: 4,
		Voxels: []ImportedWorldVoxelDef{
			{X: 1, Y: 2, Z: 3, Value: 8},
		},
	}
	if got, ok := ImportedWorldChunkPaletteAt(chunk, 1, 2, 3); !ok || got != 8 {
		t.Fatalf("expected palette 8 at voxel, got %d ok=%t", got, ok)
	}
	if _, ok := ImportedWorldChunkPaletteAt(chunk, 0, 0, 0); ok {
		t.Fatal("expected empty voxel lookup to miss")
	}
	if _, ok := ImportedWorldChunkPaletteAt(chunk, 4, 0, 0); ok {
		t.Fatal("expected out-of-bounds voxel lookup to miss")
	}
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
