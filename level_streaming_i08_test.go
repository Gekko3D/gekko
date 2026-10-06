package gekko

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i08RuntimeTerrain(t *testing.T) (*content.LevelDef, string, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "map.gklevel")
	tile := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: "runtime-i08", SourceHash: strings.Repeat("a", 64), SampleWidth: 2, SampleHeight: 2, SampleSpacing: 2, HeightOffset: 10, HeightScale: 20, HeightSamples: []uint16{0, 1, 2, 3}}
	saved, e := content.SaveTerrainHeightTile(filepath.Join(dir, "tile.gkchunk"), tile)
	if e != nil {
		t.Fatal(e)
	}
	m := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: tile.TerrainID, SourceHash: tile.SourceHash, ChunkSize: 2, VoxelResolution: 2, Entries: []content.TerrainChunkEntryDef{{TerrainID: tile.TerrainID, SourceHash: tile.SourceHash, ChunkSize: 2, VoxelResolution: 2, ChunkPath: "tile.gkchunk", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, HeightOffset: 10, HeightScale: 20}}}
	mp := filepath.Join(dir, "terrain.gkterrainmanifest")
	if e = content.SaveTerrainChunkManifest(mp, m); e != nil {
		t.Fatal(e)
	}
	l := content.NewLevelDef("runtime-i08")
	l.ChunkSize = 2
	l.VoxelResolution = 2
	l.Terrain = &content.LevelTerrainDef{Kind: content.TerrainKindHeightfield, ManifestPath: "terrain.gkterrainmanifest"}
	if e = content.SaveLevel(p, l); e != nil {
		t.Fatal(e)
	}
	return l, p, mp
}
func TestI08RuntimeLoaderKeepsV3GateBeforeCacheAdmission(t *testing.T) {
	_, _, mp := i08RuntimeTerrain(t)
	if _, e := content.LoadTerrainChunkManifest(mp); e != nil {
		t.Fatal(e)
	}
	loader := NewRuntimeContentLoader()
	for n := 0; n < 2; n++ {
		if _, e := loader.LoadTerrainChunkManifest(mp); e == nil || !strings.Contains(e.Error(), "resident height collision") {
			t.Fatalf("v3 runtime gate: %v", e)
		}
		s := loader.Stats()
		if s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 || s.Hits != 0 {
			t.Fatalf("rejected manifest entered runtime cache: %+v", s)
		}
	}
	legacyPath := filepath.Join(t.TempDir(), "legacy.gkterrainmanifest")
	if e := content.SaveTerrainChunkManifest(legacyPath, &content.TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "legacy-i08", ChunkSize: 2, VoxelResolution: 2}); e != nil {
		t.Fatal(e)
	}
	a, e := loader.LoadTerrainChunkManifest(legacyPath)
	if e != nil {
		t.Fatal(e)
	}
	b, e := loader.LoadTerrainChunkManifest(legacyPath)
	if e != nil || a != b || loader.Stats().Entries != 1 {
		t.Fatalf("legacy cache behavior changed: %v %+v", e, loader.Stats())
	}
}
func TestI08StreamedV3RejectionPublishesNoEntitiesOrResidency(t *testing.T) {
	_, p, _ := i08RuntimeTerrain(t)
	app, cmd, state := newStreamedRuntimeHarness(t)
	before := len(app.pendingAdditions)
	e := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: p, StreamingRadius: 0})
	if e == nil || !strings.Contains(e.Error(), "resident height collision") {
		t.Fatalf("must reject at explicit runtime gate, not authoring validation: %v", e)
	}
	if len(app.pendingAdditions) != before || len(state.LoadedChunks) != 0 || len(state.TerrainEntries) != 0 || state.Level != nil || state.LevelID != "" {
		t.Fatalf("rejected v3 published runtime state: pending=%d before=%d loaded=%d terrain=%d level=%v", len(app.pendingAdditions), before, len(state.LoadedChunks), len(state.TerrainEntries), state.Level)
	}
}
func TestI08EagerV3RejectionPublishesNoEntities(t *testing.T) {
	l, p, _ := i08RuntimeTerrain(t)
	app, cmd, _ := newStreamedRuntimeHarness(t)
	before := len(app.pendingAdditions)
	result, e := SpawnAuthoredLevel(cmd, newSpawnTestAssetServer(), NewRuntimeContentLoader(), l, AuthoredLevelSpawnOptions{LevelPath: p})
	if e == nil || !strings.Contains(e.Error(), "resident height collision") {
		t.Fatalf("must reject at explicit runtime gate: %v", e)
	}
	if len(app.pendingAdditions) != before || len(result.TerrainChunkEntities) != 0 {
		t.Fatalf("rejected v3 queued eager entities: pending=%d before=%d result=%+v", len(app.pendingAdditions), before, result)
	}
}

func TestI08StreamedPOI3RejectionPublishesNoEntitiesOrResidency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "poi.gkworld")
	chunk := &content.ImportedWorldChunkDef{WorldID: "poi-runtime-i08", ChunkSize: 4, VoxelResolution: .1, Voxels: []content.ImportedWorldVoxelDef{{Value: 1}}, NonEmptyVoxelCount: 1}
	saved, e := content.SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "poi.gkchunk"), chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if e != nil {
		t.Fatal(e)
	}
	m := &content.ImportedWorldDef{SchemaVersion: 3, WorldID: chunk.WorldID, SourceHash: strings.Repeat("b", 64), ChunkSize: 4, VoxelResolution: .1, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: "poi.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: saved.PayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1}}, Pages: []content.StreamPageDef{{Level: content.StreamPageLevelRoot, BoundsMax: [3]float32{.4, .4, .4}, LeafEntryIndices: []uint32{0}, Payload: content.StreamPagePayloadDef{Kind: saved.PayloadKind, Path: "poi.gkchunk", ChunkSize: 4, VoxelResolution: .1, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1}}}, RootPageIndices: []uint32{0}}
	if e = content.SaveImportedWorld(path, m); e != nil {
		t.Fatal(e)
	}
	loader := NewRuntimeContentLoader()
	if _, e = loader.LoadImportedWorld(path); e == nil || !strings.Contains(e.Error(), "indexed page registration") {
		t.Fatalf("POI v3 gate: %v", e)
	}
	if loader.Stats().Entries != 0 {
		t.Fatal("rejected POI admitted to cache")
	}
	l := content.NewLevelDef("poi-runtime-i08")
	l.ChunkSize = 4
	l.VoxelResolution = .1
	l.BaseWorld = &content.LevelBaseWorldDef{Kind: content.ImportedWorldKindVoxelWorld, ManifestPath: "poi.gkworld"}
	levelPath := filepath.Join(dir, "map.gklevel")
	if e = content.SaveLevel(levelPath, l); e != nil {
		t.Fatal(e)
	}
	app, cmd, state := newStreamedRuntimeHarness(t)
	before := len(app.pendingAdditions)
	e = StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 0})
	if e == nil || !strings.Contains(e.Error(), "indexed page registration") {
		t.Fatalf("POI explicit runtime gate: %v", e)
	}
	if len(app.pendingAdditions) != before || len(state.LoadedChunks) != 0 || len(state.ImportedWorldEntries) != 0 || state.BaseWorldManifest != nil {
		t.Fatalf("rejected POI published state: pending=%d before=%d loaded=%d sources=%d", len(app.pendingAdditions), before, len(state.LoadedChunks), len(state.ImportedWorldEntries))
	}
}
