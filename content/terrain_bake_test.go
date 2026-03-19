package content

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBakeTerrainChunkSetIsDeterministic(t *testing.T) {
	def := testBakeTerrainDef()
	manifestPath := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")

	manifestA, chunksA, err := BakeTerrainChunkSet(def, manifestPath)
	if err != nil {
		t.Fatalf("BakeTerrainChunkSet failed: %v", err)
	}
	manifestB, chunksB, err := BakeTerrainChunkSet(def, manifestPath)
	if err != nil {
		t.Fatalf("BakeTerrainChunkSet failed: %v", err)
	}

	if !reflect.DeepEqual(manifestA, manifestB) {
		t.Fatalf("expected deterministic manifests, got A=%+v B=%+v", manifestA, manifestB)
	}
	if !reflect.DeepEqual(chunksA, chunksB) {
		t.Fatalf("expected deterministic chunks, got A=%+v B=%+v", chunksA, chunksB)
	}
}

func TestBakeTerrainChunksReturnsOnlyRequestedDirtyCoords(t *testing.T) {
	def := testBakeTerrainDef()
	manifestPath := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	dirty := []TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 1}}

	manifest, chunks, err := BakeTerrainChunks(def, manifestPath, dirty)
	if err != nil {
		t.Fatalf("BakeTerrainChunks failed: %v", err)
	}

	if len(manifest.Entries) != len(dirty) {
		t.Fatalf("expected %d entries, got %d", len(dirty), len(manifest.Entries))
	}
	if len(chunks) != len(dirty) {
		t.Fatalf("expected %d chunks, got %d", len(dirty), len(chunks))
	}
	for _, coord := range dirty {
		if _, ok := chunks[TerrainChunkKey(coord)]; !ok {
			t.Fatalf("missing dirty chunk %s", TerrainChunkKey(coord))
		}
	}
}

func TestBakeTerrainChunkManifestUsesRelativeChunkPaths(t *testing.T) {
	def := testBakeTerrainDef()
	manifestPath := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")

	manifest, _, err := BakeTerrainChunkSet(def, manifestPath)
	if err != nil {
		t.Fatalf("BakeTerrainChunkSet failed: %v", err)
	}

	if manifest.SourceHash == "" {
		t.Fatal("expected source hash")
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("expected manifest entries")
	}
	entry := manifest.Entries[0]
	if entry.ChunkPath == "" || filepath.IsAbs(entry.ChunkPath) {
		t.Fatalf("expected relative chunk path, got %q", entry.ChunkPath)
	}
	if entry.TerrainID != def.ID {
		t.Fatalf("expected terrain id %q, got %q", def.ID, entry.TerrainID)
	}
}

func TestTerrainChunkCoordsAreCenteredAroundOrigin(t *testing.T) {
	def := testBakeTerrainDef()

	coords := TerrainChunkCoords(def)
	if len(coords) != 4 {
		t.Fatalf("expected 4 chunk coords, got %d", len(coords))
	}

	want := []TerrainChunkCoordDef{
		{X: -1, Y: 0, Z: -1},
		{X: 0, Y: 0, Z: -1},
		{X: -1, Y: 0, Z: 0},
		{X: 0, Y: 0, Z: 0},
	}
	if !reflect.DeepEqual(coords, want) {
		t.Fatalf("expected centered chunk coords %+v, got %+v", want, coords)
	}
}

func TestTerrainChunkManifestAndChunkRoundTrip(t *testing.T) {
	def := testBakeTerrainDef()
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "terrain.gkterrainmanifest")

	manifest, chunks, err := BakeTerrainChunkSet(def, manifestPath)
	if err != nil {
		t.Fatalf("BakeTerrainChunkSet failed: %v", err)
	}
	if err := SaveTerrainChunkManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveTerrainChunkManifest failed: %v", err)
	}
	for _, entry := range manifest.Entries {
		chunk := chunks[TerrainChunkKey(entry.Coord)]
		if chunk == nil {
			t.Fatalf("missing chunk for entry %+v", entry.Coord)
		}
		if err := SaveTerrainChunk(ResolveTerrainChunkPath(entry, manifestPath), chunk); err != nil {
			t.Fatalf("SaveTerrainChunk failed: %v", err)
		}
	}

	loadedManifest, err := LoadTerrainChunkManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadTerrainChunkManifest failed: %v", err)
	}
	if !reflect.DeepEqual(manifest, loadedManifest) {
		t.Fatalf("expected manifest round-trip, got want=%+v got=%+v", manifest, loadedManifest)
	}
	loadedChunk, err := LoadTerrainChunk(ResolveTerrainChunkPath(loadedManifest.Entries[0], manifestPath))
	if err != nil {
		t.Fatalf("LoadTerrainChunk failed: %v", err)
	}
	if !reflect.DeepEqual(chunks[TerrainChunkKey(loadedManifest.Entries[0].Coord)], loadedChunk) {
		t.Fatalf("expected chunk round-trip, got want=%+v got=%+v", chunks[TerrainChunkKey(loadedManifest.Entries[0].Coord)], loadedChunk)
	}
}

func TestBakeTerrainChunkSetDoesNotAddPositiveEdgeOverlapForNeighboringChunks(t *testing.T) {
	def := testBakeTerrainDef()
	manifestPath := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")

	_, chunks, err := BakeTerrainChunkSet(def, manifestPath)
	if err != nil {
		t.Fatalf("BakeTerrainChunkSet failed: %v", err)
	}

	left := chunks[TerrainChunkKey(TerrainChunkCoordDef{X: -1, Y: 0, Z: -1})]
	right := chunks[TerrainChunkKey(TerrainChunkCoordDef{X: 0, Y: 0, Z: -1})]
	if left == nil || right == nil {
		t.Fatalf("expected neighboring chunks, got left=%+v right=%+v", left, right)
	}

	if terrainChunkHasColumnAt(left, def.ChunkSize, 0) {
		t.Fatalf("did not expect left chunk to include extra overlap column at x=%d", def.ChunkSize)
	}
	if terrainChunkHasColumnAt(right, def.ChunkSize, 0) {
		t.Fatalf("did not expect right chunk to include extra overlap column at x=%d", def.ChunkSize)
	}
}

func testBakeTerrainDef() *TerrainSourceDef {
	return &TerrainSourceDef{
		ID:              "terrain-bake-test",
		SchemaVersion:   CurrentTerrainSchemaVersion,
		Name:            "terrain",
		Kind:            TerrainKindHeightfield,
		SampleWidth:     4,
		SampleHeight:    4,
		HeightSamples:   []uint16{0, 4096, 8192, 12288, 4096, 8192, 16384, 20480, 8192, 16384, 24576, 32768, 12288, 20480, 32768, 40960},
		WorldSize:       Vec2{32, 32},
		HeightScale:     16,
		VoxelResolution: 1,
		ChunkSize:       16,
	}
}

func terrainChunkHasColumnAt(chunk *TerrainChunkDef, x, z int) bool {
	for _, column := range chunk.Columns {
		if column.X == x && column.Z == z {
			return true
		}
	}
	return false
}
