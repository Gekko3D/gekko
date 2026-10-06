package content

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These fixtures deliberately have different level, terrain and POI grids.
func i08ValidationFixture(t *testing.T) (*LevelDef, string, *TerrainChunkManifestDef, *ImportedWorldDef) {
	t.Helper()
	dir := t.TempDir()
	levelPath := filepath.Join(dir, "map.gklevel")
	tile := &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: "terrain-i08", SourceHash: strings.Repeat("a", 64), SampleWidth: 2, SampleHeight: 2, SampleSpacing: 2, HeightOffset: 10, HeightScale: 20, HeightSamples: []uint16{0, 100, 200, 300}}
	saved, e := SaveTerrainHeightTile(filepath.Join(dir, "source.gkchunk"), tile)
	if e != nil {
		t.Fatal(e)
	}
	terrain := &TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: tile.TerrainID, SourceHash: tile.SourceHash, ChunkSize: 2, VoxelResolution: 2, Entries: []TerrainChunkEntryDef{{TerrainID: tile.TerrainID, SourceHash: tile.SourceHash, ChunkSize: 2, VoxelResolution: 2, ChunkPath: "source.gkchunk", PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, HeightOffset: 10, HeightScale: 20}}}
	if e = SaveTerrainChunkManifest(filepath.Join(dir, "terrain.gkterrainmanifest"), terrain); e != nil {
		t.Fatal(e)
	}
	chunk := &ImportedWorldChunkDef{WorldID: "poi-i08", ChunkSize: 4, VoxelResolution: .1, Voxels: []ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}}, NonEmptyVoxelCount: 1}
	if e = SaveImportedWorldChunk(filepath.Join(dir, "poi.gkchunk"), chunk); e != nil {
		t.Fatal(e)
	}
	poi := &ImportedWorldDef{SchemaVersion: 2, WorldID: chunk.WorldID, Kind: ImportedWorldKindVoxelWorld, ChunkSize: 4, VoxelResolution: .1, Entries: []ImportedWorldChunkEntryDef{{ChunkPath: "poi.gkchunk", NonEmptyVoxelCount: 1}}, Sectors: []ImportedWorldSectorDef{{BoundsMin: [3]float32{0, 0, 0}, BoundsMax: [3]float32{.4, .4, .4}, FullChunkRefs: []TerrainChunkCoordDef{{}}}}}
	if e = SaveImportedWorld(filepath.Join(dir, "poi.gkworld"), poi); e != nil {
		t.Fatal(e)
	}
	level := NewLevelDef("i08")
	level.ChunkSize = 8
	level.VoxelResolution = 1
	level.Terrain = &LevelTerrainDef{Kind: TerrainKindHeightfield, ManifestPath: "terrain.gkterrainmanifest"}
	level.BaseWorld = &LevelBaseWorldDef{Kind: ImportedWorldKindVoxelWorld, ManifestPath: "poi.gkworld"}
	return level, levelPath, terrain, poi
}
func i08Validate(t *testing.T, l *LevelDef, p string) LevelValidationResult {
	t.Helper()
	return ValidateLevel(l, LevelValidationOptions{DocumentPath: p})
}
func i08RequireValid(t *testing.T, l *LevelDef, p string) {
	t.Helper()
	r := i08Validate(t, l, p)
	if r.HasErrors() {
		t.Fatalf("valid independent layers rejected: %+v", r.Issues)
	}
}

func TestI08LevelStreamingBoundsWire(t *testing.T) {
	l := NewLevelDef("bounds")
	raw, e := json.Marshal(l)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "streaming_bounds") {
		t.Fatal("optional bounds serialized when absent")
	}
	l.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-10, -20, -30}, BoundsMax: [3]float32{10, 20, 30}}
	p := filepath.Join(t.TempDir(), "bounds.gklevel")
	if e = SaveLevel(p, l); e != nil {
		t.Fatal(e)
	}
	got, e := LoadLevel(p)
	if e != nil || !reflect.DeepEqual(got.StreamingBounds, l.StreamingBounds) {
		t.Fatalf("bounds roundtrip %v %+v", e, got)
	}
	raw, e = os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(raw), `"streaming_bounds"`) || !strings.Contains(string(raw), `"bounds_min"`) {
		t.Fatal("bounds wire fields missing")
	}
	for name, b := range map[string]LevelStreamingBoundsDef{"flat": {BoundsMax: [3]float32{1, 0, 1}}, "inverted": {BoundsMin: [3]float32{1, 0, 0}, BoundsMax: [3]float32{0, 1, 1}}, "nan": {BoundsMin: [3]float32{float32(math.NaN()), 0, 0}, BoundsMax: [3]float32{1, 1, 1}}, "infinite": {BoundsMax: [3]float32{1, float32(math.Inf(1)), 1}}} {
		t.Run(name, func(t *testing.T) {
			l.StreamingBounds = &b
			if !i08Validate(t, l, "").HasErrors() {
				t.Fatal("invalid streaming bounds accepted")
			}
		})
	}
}
func TestI08LevelValidationManifestFirstIndependentLayers(t *testing.T) {
	for _, provenance := range []string{"", "not-present.gkterrain", "unreadable-provenance.gkterrain"} {
		t.Run(provenance, func(t *testing.T) {
			l, p, _, _ := i08ValidationFixture(t)
			l.Terrain.SourcePath = provenance
			if strings.HasPrefix(provenance, "unreadable") {
				if e := os.Mkdir(filepath.Join(filepath.Dir(p), provenance), 0755); e != nil {
					t.Fatal(e)
				}
			}
			i08RequireValid(t, l, p)
		})
	}
}
func TestI08LevelValidationManifestFailureDoesNotFallBack(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "payload-missing", "payload-corrupt"} {
		t.Run(kind, func(t *testing.T) {
			l, p, _, _ := i08ValidationFixture(t)
			dir := filepath.Dir(p)
			source := NewTerrainSourceDef("valid fallback")
			source.SampleWidth = 2
			source.SampleHeight = 2
			source.HeightSamples = []uint16{0, 0, 0, 0}
			source.ChunkSize = l.ChunkSize
			source.VoxelResolution = l.VoxelResolution
			if e := SaveTerrainSource(filepath.Join(dir, "fallback.gkterrain"), source); e != nil {
				t.Fatal(e)
			}
			l.Terrain.SourcePath = "fallback.gkterrain"
			switch kind {
			case "missing":
				l.Terrain.ManifestPath = "missing.gkterrainmanifest"
			case "malformed":
				if e := os.WriteFile(filepath.Join(dir, l.Terrain.ManifestPath), []byte(`{"schema_version":3,"entries":`), 0600); e != nil {
					t.Fatal(e)
				}
			case "payload-missing":
				if e := os.Remove(filepath.Join(dir, "source.gkchunk")); e != nil {
					t.Fatal(e)
				}
			case "payload-corrupt":
				if e := os.WriteFile(filepath.Join(dir, "source.gkchunk"), []byte("broken"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if !i08Validate(t, l, p).HasErrors() {
				t.Fatal("invalid authoritative terrain manifest/payload accepted through provenance")
			}
		})
	}
}
func TestI08LevelValidationLegacyGridPolicyPreserved(t *testing.T) {
	l, p, _, _ := i08ValidationFixture(t)
	l.Terrain = nil
	r := i08Validate(t, l, p)
	assertHasLevelValidationCode(t, r, "base_world_chunk_size_mismatch")
	assertHasLevelValidationCode(t, r, "base_world_voxel_resolution_mismatch")
	l.BaseWorld = nil
	src := NewTerrainSourceDef("legacy")
	src.SampleWidth = 2
	src.SampleHeight = 2
	src.HeightSamples = []uint16{0, 0, 0, 0}
	src.ChunkSize = 4
	src.VoxelResolution = 2
	if e := SaveTerrainSource(filepath.Join(filepath.Dir(p), "legacy.gkterrain"), src); e != nil {
		t.Fatal(e)
	}
	l.Terrain = &LevelTerrainDef{Kind: TerrainKindHeightfield, SourcePath: "legacy.gkterrain"}
	r = i08Validate(t, l, p)
	assertHasLevelValidationCode(t, r, "terrain_chunk_size_mismatch")
	assertHasLevelValidationCode(t, r, "terrain_voxel_resolution_mismatch")
}
func TestI08LevelValidationNavigationUsesImportedOwnerGrid(t *testing.T) {
	l, p, _, poi := i08ValidationFixture(t)
	nav := &NavGraphManifestDef{NavID: "nav-i08", BuilderVersion: "i08", SourceWorldID: poi.WorldID, ChunkSize: poi.ChunkSize, VoxelResolution: poi.VoxelResolution}
	navPath := filepath.Join(filepath.Dir(p), "map.gknav")
	if e := SaveNavGraphManifest(navPath, nav); e != nil {
		t.Fatal(e)
	}
	l.Navigation = &LevelNavigationDef{ManifestPath: "map.gknav"}
	i08RequireValid(t, l, p)
	nav.SourceWorldID = "wrong"
	if e := SaveNavGraphManifest(navPath, nav); e != nil {
		t.Fatal(e)
	}
	assertHasLevelValidationCode(t, i08Validate(t, l, p), "navigation_source_world_id_mismatch")
	nav.SourceWorldID = poi.WorldID
	nav.ChunkSize = l.ChunkSize
	nav.VoxelResolution = l.VoxelResolution
	if e := SaveNavGraphManifest(navPath, nav); e != nil {
		t.Fatal(e)
	}
	r := i08Validate(t, l, p)
	assertHasLevelValidationCode(t, r, "navigation_chunk_size_mismatch")
	assertHasLevelValidationCode(t, r, "navigation_voxel_resolution_mismatch")
	l.Terrain = nil
	r = i08Validate(t, l, p)
	for _, issue := range r.Issues {
		if issue.Code == "navigation_chunk_size_mismatch" || issue.Code == "navigation_voxel_resolution_mismatch" {
			t.Fatalf("legacy navigation changed grid policy: %+v", r.Issues)
		}
	}
	assertHasLevelValidationCode(t, r, "base_world_chunk_size_mismatch")
}
func TestI08LevelValidationStreamingBoundsContainFullCoverage(t *testing.T) {
	l, p, _, poi := i08ValidationFixture(t)
	l.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-1, -1, -1}, BoundsMax: [3]float32{4, 30, 4}}
	i08RequireValid(t, l, p)
	for _, axis := range []int{0, 1, 2} {
		t.Run(string(rune('x'+axis)), func(t *testing.T) {
			b := *l.StreamingBounds
			b.BoundsMax[axis] -= .01
			l.StreamingBounds = &b
			if !i08Validate(t, l, p).HasErrors() {
				t.Fatal("partial source coverage accepted")
			}
			l.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-1, -1, -1}, BoundsMax: [3]float32{4, 30, 4}}
		})
	}
	poi.Sectors[0].BoundsMax = [3]float32{5, 1, 1}
	if e := SaveImportedWorld(filepath.Join(filepath.Dir(p), "poi.gkworld"), poi); e != nil {
		t.Fatal(e)
	}
	if !i08Validate(t, l, p).HasErrors() {
		t.Fatal("sector outside streaming bounds accepted")
	}
}

func TestI08LevelValidationBoundsIncludeVisualPagesAndRejectUnknownSectors(t *testing.T) {
	t.Run("visual-page", func(t *testing.T) {
		l, p, terrain, _ := i08ValidationFixture(t)
		entry := terrain.Entries[0]
		// A qualified root covering more than the backing tile must also fit the level.
		pageTile := &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: terrain.TerrainID, SourceHash: terrain.SourceHash, SampleWidth: 4, SampleHeight: 4, SampleSpacing: 2, HeightOffset: 10, HeightScale: 20, HeightSamples: make([]uint16, 16)}
		saved, e := SaveTerrainHeightTile(filepath.Join(filepath.Dir(p), "visual.gkchunk"), pageTile)
		if e != nil {
			t.Fatal(e)
		}
		terrain.Pages = []StreamPageDef{{Level: StreamPageLevelRoot, BoundsMin: [3]float32{0, 10, 0}, BoundsMax: [3]float32{8, 30, 8}, Payload: StreamPagePayloadDef{Kind: entry.PayloadKind, Path: "visual.gkchunk", ChunkSize: 4, SampleSpacing: 2, VoxelResolution: 2, HeightOffset: 10, HeightScale: 20, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes}}}
		terrain.RootPageIndices = []uint32{0}
		if e = SaveTerrainChunkManifest(filepath.Join(filepath.Dir(p), l.Terrain.ManifestPath), terrain); e != nil {
			t.Fatal(e)
		}
		l.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-1, -1, -1}, BoundsMax: [3]float32{8, 30, 8}}
		i08RequireValid(t, l, p)
		for _, corruption := range []string{"header", "body"} {
			t.Run(corruption, func(t *testing.T) {
				path := filepath.Join(filepath.Dir(p), "visual.gkchunk")
				if corruption == "header" {
					bad := *pageTile
					bad.SourceHash = strings.Repeat("c", 64)
					if _, err := SaveTerrainHeightTile(path, &bad); err != nil {
						t.Fatal(err)
					}
				} else {
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					raw[len(raw)-1] ^= 1
					if err = os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if !i08Validate(t, l, p).HasErrors() {
					t.Fatal("corrupt qualified page accepted")
				}
				if _, err := SaveTerrainHeightTile(path, pageTile); err != nil {
					t.Fatal(err)
				}
			})
		}
		l.StreamingBounds.BoundsMax[0] = 7.99
		if !i08Validate(t, l, p).HasErrors() {
			t.Fatal("visual coverage outside level accepted")
		}
	})
	t.Run("unknown-sector", func(t *testing.T) {
		l, p, _, poi := i08ValidationFixture(t)
		poi.Sectors[0].BoundsMin = [3]float32{}
		poi.Sectors[0].BoundsMax = [3]float32{}
		if e := SaveImportedWorld(filepath.Join(filepath.Dir(p), l.BaseWorld.ManifestPath), poi); e != nil {
			t.Fatal(e)
		}
		i08RequireValid(t, l, p) // Compatibility metadata without declared level coverage remains admissible.
		l.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-1, -1, -1}, BoundsMax: [3]float32{100, 100, 100}}
		if !i08Validate(t, l, p).HasErrors() {
			t.Fatal("unknown sector height fabricated to satisfy explicit bounds")
		}
	})
}

func i08ValidationPOI3(t *testing.T, p string) *ImportedWorldDef {
	t.Helper()
	dir := filepath.Dir(p)
	chunk := &ImportedWorldChunkDef{WorldID: "poi-i08-v3", ChunkSize: 4, VoxelResolution: .1, Voxels: []ImportedWorldVoxelDef{{Value: 1}}, NonEmptyVoxelCount: 1}
	saved, e := SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "v3poi.gkchunk"), chunk, ImportedWorldChunkSaveOptions{PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if e != nil {
		t.Fatal(e)
	}
	d := &ImportedWorldDef{SchemaVersion: 3, WorldID: chunk.WorldID, SourceHash: strings.Repeat("b", 64), ChunkSize: 4, VoxelResolution: .1, Entries: []ImportedWorldChunkEntryDef{{ChunkPath: "v3poi.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: saved.PayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1}}, Pages: []StreamPageDef{{Level: StreamPageLevelRoot, BoundsMax: [3]float32{.4, .4, .4}, LeafEntryIndices: []uint32{0}, Payload: StreamPagePayloadDef{Kind: saved.PayloadKind, Path: "v3poi.gkchunk", ChunkSize: 4, VoxelResolution: .1, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, OccupiedSectorCount: 1, OccupiedBrickCount: 1}}}, RootPageIndices: []uint32{0}}
	if e = SaveImportedWorld(filepath.Join(dir, "v3poi.gkworld"), d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestI08LevelValidationPOI3SelectsIndependentContextForLegacyTerrain(t *testing.T) {
	l, p, _, _ := i08ValidationFixture(t)
	poi := i08ValidationPOI3(t, p)
	l.BaseWorld.ManifestPath = "v3poi.gkworld"
	source := NewTerrainSourceDef("legacy terrain")
	source.SampleWidth = 2
	source.SampleHeight = 2
	source.HeightSamples = []uint16{0, 1, 2, 3}
	source.ChunkSize = 2
	source.VoxelResolution = 2
	if e := SaveTerrainSource(filepath.Join(filepath.Dir(p), "legacy.gkterrain"), source); e != nil {
		t.Fatal(e)
	}
	l.Terrain = &LevelTerrainDef{Kind: TerrainKindHeightfield, SourcePath: "legacy.gkterrain"}
	i08RequireValid(t, l, p)
	l.Terrain = nil
	i08RequireValid(t, l, p)
	nav := &NavGraphManifestDef{NavID: "poi3-nav", BuilderVersion: "i08", SourceWorldID: poi.WorldID, ChunkSize: poi.ChunkSize, VoxelResolution: poi.VoxelResolution}
	if e := SaveNavGraphManifest(filepath.Join(filepath.Dir(p), "poi3.gknav"), nav); e != nil {
		t.Fatal(e)
	}
	l.Navigation = &LevelNavigationDef{ManifestPath: "poi3.gknav"}
	i08RequireValid(t, l, p)
}
func TestI08LevelValidationTerrainKindExtensionAndTileHeaderQualification(t *testing.T) {
	for _, bad := range []string{"kind", "extension", "header"} {
		t.Run(bad, func(t *testing.T) {
			l, p, _, _ := i08ValidationFixture(t)
			switch bad {
			case "kind":
				l.Terrain.Kind = TerrainKind("unsupported")
			case "extension":
				old := filepath.Join(filepath.Dir(p), l.Terrain.ManifestPath)
				newPath := filepath.Join(filepath.Dir(p), "terrain.json")
				if e := os.Rename(old, newPath); e != nil {
					t.Fatal(e)
				}
				l.Terrain.ManifestPath = "terrain.json"
			case "header":
				tile, e := LoadTerrainHeightTile(filepath.Join(filepath.Dir(p), "source.gkchunk"))
				if e != nil {
					t.Fatal(e)
				}
				tile.TerrainID = "wrong-owner"
				if _, e = SaveTerrainHeightTile(filepath.Join(filepath.Dir(p), "source.gkchunk"), tile); e != nil {
					t.Fatal(e)
				}
			}
			if !i08Validate(t, l, p).HasErrors() {
				t.Fatal("invalid terrain metadata/header accepted")
			}
		})
	}
}
