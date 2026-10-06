package content

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func i08Level() *LevelDef {
	return &LevelDef{ID: "index-only", ChunkSize: 256, VoxelResolution: 1}
}

func i08LegacyTerrain() *TerrainChunkManifestDef {
	return &TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "legacy-terrain", SourceHash: "terrain-generation", ChunkSize: 4, VoxelResolution: 2,
		Entries: []TerrainChunkEntryDef{
			{Coord: TerrainChunkCoordDef{X: 2}, ChunkPath: "does-not-exist-positive.gkchunk", NonEmptyVoxelCount: 3},
			{Coord: TerrainChunkCoordDef{X: -2, Y: 1}, ChunkPath: "does-not-exist-negative.gkchunk", NonEmptyVoxelCount: 10},
			{Coord: TerrainChunkCoordDef{X: 4}, ChunkPath: "does-not-exist-empty.gkchunk"},
		}}
}

func i08LegacyPOI() *ImportedWorldDef {
	positive, negative := TerrainChunkCoordDef{X: 3}, TerrainChunkCoordDef{X: -2}
	return &ImportedWorldDef{SchemaVersion: 2, WorldID: "poi", SourceHash: "poi-generation", ChunkSize: 4, VoxelResolution: .1,
		Entries: []ImportedWorldChunkEntryDef{{Coord: positive, ChunkPath: "missing-positive.gkchunk", NonEmptyVoxelCount: 1}, {Coord: negative, ChunkPath: "missing-negative.gkchunk", NonEmptyVoxelCount: 1}},
		Sectors: []ImportedWorldSectorDef{
			{Coord: positive, BoundsMin: [3]float32{1.2, 0, 0}, BoundsMax: [3]float32{1.6, .4, .4}, FullChunkRefs: []TerrainChunkCoordDef{positive}},
			{Coord: negative, BoundsMin: [3]float32{-.8, 0, 0}, BoundsMax: [3]float32{-.4, .4, .4}, FullChunkRefs: []TerrainChunkCoordDef{negative}},
		}}
}

func i08Build(t *testing.T, level *LevelDef, terrain *TerrainChunkManifestDef, poi *ImportedWorldDef) *LevelStreamingIndex {
	t.Helper()
	i, err := BuildLevelStreamingIndex(level, terrain, poi)
	if err != nil || i == nil {
		t.Fatalf("build metadata index: %v", err)
	}
	return i
}

func i08Query(t *testing.T, index *LevelStreamingBoundsIndex, min, max [3]float32, want ...uint32) {
	t.Helper()
	got, err := index.Query(min, max)
	if err != nil || len(got) != len(want) {
		t.Fatalf("query %v..%v: got %v, err %v, want %v", min, max, got, err, want)
	}
	for n := range want {
		if got[n] != want[n] {
			t.Fatalf("query indices got %v want %v", got, want)
		}
	}
}

func TestI08IndependentLayerWorldQueriesPreserveManifestIndices(t *testing.T) {
	terrain, poi := i07TerrainManifest(), i08LegacyPOI()
	i := i08Build(t, i08Level(), terrain, poi)
	if !i.IndependentLayers || i.Terrain.OwnerID != terrain.TerrainID || i.POI.OwnerID != poi.WorldID || i.Terrain.SourceHash != terrain.SourceHash || i.POI.SourceHash != poi.SourceHash || i.Terrain.ChunkSize != 128 || i.Terrain.VoxelResolution != 2 || i.POI.ChunkSize != 4 || i.POI.VoxelResolution != .1 || i.Terrain.LegacyDistance || !i.POI.LegacyDistance {
		t.Fatalf("layer identities or independent grids lost: %+v", i)
	}
	if len(i.Terrain.Forest.LeafOwnerPageIndices) != 0 || len(i.POI.Forest.LeafOwnerPageIndices) != 2 {
		t.Fatal("independent visual and backing ownership lost")
	}
	if i.POI.EntryIndexByCoord[poi.Entries[0].Coord] != 0 || i.POI.EntryIndexByCoord[poi.Entries[1].Coord] != 1 || i.POI.SectorIndexByCoord[poi.Sectors[1].Coord] != 1 {
		t.Fatal("sorted spatial storage renumbered original manifest references")
	}
	i08Query(t, i.POI.SourceIndex, [3]float32{-1, -1, -1}, [3]float32{2, 1, 1}, 0, 1)
	i08Query(t, i.POI.SourceIndex, [3]float32{-.6, .2, .2}, [3]float32{-.6, .2, .2}, 1)
	i08Query(t, i.POI.SourceIndex, [3]float32{.5, 0, 0}, [3]float32{1, 1, 1})
	i08Query(t, i.POI.SectorIndex, [3]float32{-.6, .2, .2}, [3]float32{-.6, .2, .2}, 1)
	// A closed edge belongs to both adjoining regional payloads; coarser
	// ancestors remain independently queryable under their original indices.
	i08Query(t, i.Terrain.PageIndex, [3]float32{-128, 20, 64}, [3]float32{-128, 20, 64}, 0, 1, 4, 5)
	i08Query(t, i.Terrain.SourceIndex, [3]float32{-200, 20, 20}, [3]float32{-200, 20, 20}, 0)
	i08Query(t, i.Terrain.SourceIndex, [3]float32{-200, 100, 20}, [3]float32{-200, 100, 20})
	for _, record := range i.Terrain.SourceBounds {
		if !record.Known || !record.FootprintKnown {
			t.Fatal("calibrated source coverage became unknown")
		}
	}
}

func TestI08LegacyTerrainCountBoundsAndEmptyBackingRecords(t *testing.T) {
	d := i08LegacyTerrain()
	i := i08Build(t, i08Level(), d, i08LegacyPOI())
	if i.IndependentLayers || !i.Terrain.LegacyDistance || len(i.Terrain.SourceBounds) != 3 || len(i.Terrain.Pages) != 2 {
		t.Fatal("legacy layer/empty backing semantics changed")
	}
	want := []LevelStreamingBoundsRecord{
		{ManifestIndex: 0, BoundsMin: [3]float32{16, 0, 0}, BoundsMax: [3]float32{24, 6, 8}, Known: true, FootprintKnown: true},
		{ManifestIndex: 1, BoundsMin: [3]float32{-16, 8, 0}, BoundsMax: [3]float32{-8, 28, 8}, Known: true, FootprintKnown: true},
		{ManifestIndex: 2, BoundsMin: [3]float32{32, 0, 0}, BoundsMax: [3]float32{40, 0, 8}, Known: true, FootprintKnown: true},
	}
	if !reflect.DeepEqual(i.Terrain.SourceBounds, want) || !reflect.DeepEqual(i.Terrain.Forest.LeafOwnerPageIndices, []int{0, 1, -1}) {
		t.Fatalf("count upper bounds/empty references: %v", i.Terrain.SourceBounds)
	}
	i08Query(t, i.Terrain.SourceIndex, [3]float32{-12, 20, 4}, [3]float32{-12, 20, 4}, 1)
	i08Query(t, i.Terrain.SourceIndex, [3]float32{36, 0, 4}, [3]float32{36, 0, 4}, 2)
	i08Query(t, i.Terrain.PageIndex, [3]float32{36, 0, 4}, [3]float32{36, 0, 4})
	// Constructor owns metadata, not the legacy runtime's shared-grid policy.
	if d.Entries[0].ChunkSize != 0 || d.Entries[0].VoxelResolution != 0 {
		t.Fatal("fallback defaults mutated source")
	}
}

func TestI08UnknownCoverageIsExplicitAndConservative(t *testing.T) {
	d := i07TerrainManifest()
	d.Pages, d.RootPageIndices = nil, nil
	d.SourceHash, d.Entries[0].SourceHash = "old-source-identity", "old-source-identity"
	d.Entries[0].HeightOffset, d.Entries[0].HeightScale = 0, 0
	poi := i08LegacyPOI()
	poi.Sectors[1].BoundsMin, poi.Sectors[1].BoundsMax = [3]float32{}, [3]float32{}
	i := i08Build(t, i08Level(), d, poi)
	if !i.Terrain.SourceOnly || len(i.Terrain.Pages) != 0 || i.Terrain.SourceBounds[0].Known || !i.Terrain.SourceBounds[0].FootprintKnown || i.POI.SectorBounds[1].Known || i.POI.SectorBounds[1].FootprintKnown {
		t.Fatal("unknown calibration or sector metadata invented known coverage")
	}
	i08Query(t, i.Terrain.SourceIndex, [3]float32{-200, 100000, 20}, [3]float32{-200, 100000, 20}, 0)
	i08Query(t, i.Terrain.SourceIndex, [3]float32{100, 100000, 20}, [3]float32{100, 100000, 20})
	i08Query(t, i.POI.SectorIndex, [3]float32{10000, 10000, 10000}, [3]float32{10000, 10000, 10000}, 1)
	i08Query(t, i.POI.PageIndex, [3]float32{10000, 10000, 10000}, [3]float32{10000, 10000, 10000})
	for _, inputs := range []struct {
		terrain *TerrainChunkManifestDef
		poi     *ImportedWorldDef
	}{{d, nil}, {nil, poi}} {
		level := i08Level()
		level.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-100000, -100000, -100000}, BoundsMax: [3]float32{100000, 100000, 100000}}
		if got, err := BuildLevelStreamingIndex(level, inputs.terrain, inputs.poi); err == nil || got != nil {
			t.Fatal("explicit full containment accepted unknown coverage")
		}
	}
}

func TestI08StreamingBoundsValidationContainmentAndWire(t *testing.T) {
	level := i08Level()
	level.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-3000, -1, -1}, BoundsMax: [3]float32{100, 100, 3000}}
	i := i08Build(t, level, i07TerrainManifest(), i06World())
	if i.StreamingBounds == level.StreamingBounds || !reflect.DeepEqual(i.StreamingBounds, level.StreamingBounds) {
		t.Fatal("authored padded world bounds not independently owned")
	}
	raw, err := json.Marshal(level)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	var bounds map[string][3]float32
	if err = json.Unmarshal(wire["streaming_bounds"], &bounds); err != nil || bounds["bounds_min"] != level.StreamingBounds.BoundsMin || bounds["bounds_max"] != level.StreamingBounds.BoundsMax {
		t.Fatalf("authored streaming bounds wire: %s (%v)", raw, err)
	}
	for name, mutate := range map[string]func(*LevelStreamingBoundsDef){
		"inverted":                func(b *LevelStreamingBoundsDef) { b.BoundsMax[0] = -4000 },
		"flat":                    func(b *LevelStreamingBoundsDef) { b.BoundsMax[1] = b.BoundsMin[1] },
		"nan":                     func(b *LevelStreamingBoundsDef) { b.BoundsMin[2] = float32(math.NaN()) },
		"infinity":                func(b *LevelStreamingBoundsDef) { b.BoundsMax[0] = float32(math.Inf(1)) },
		"omitted-source-coverage": func(b *LevelStreamingBoundsDef) { b.BoundsMin[0] = -200 },
		"omitted-page-coverage":   func(b *LevelStreamingBoundsDef) { b.BoundsMin[0] = -1000 },
		"omitted-poi-coverage":    func(b *LevelStreamingBoundsDef) { b.BoundsMax[0] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			l := i08Level()
			copied := *level.StreamingBounds
			l.StreamingBounds = &copied
			mutate(l.StreamingBounds)
			if got, err := BuildLevelStreamingIndex(l, i07TerrainManifest(), i06World()); err == nil || got != nil {
				t.Fatal("accepted invalid or incomplete authored bounds")
			}
		})
	}
}

func TestI08QueryRejectsInvalidBoundsAndOwnsResults(t *testing.T) {
	i := i08Build(t, i08Level(), i07TerrainManifest(), i08LegacyPOI())
	for _, index := range []*LevelStreamingBoundsIndex{i.Terrain.SourceIndex, i.Terrain.PageIndex, i.Terrain.SectorIndex, i.POI.SourceIndex, i.POI.PageIndex, i.POI.SectorIndex} {
		for _, bounds := range []struct{ min, max [3]float32 }{
			{[3]float32{1, 0, 0}, [3]float32{}},
			{[3]float32{0, float32(math.NaN()), 0}, [3]float32{}},
			{[3]float32{}, [3]float32{0, 0, float32(math.Inf(1))}},
		} {
			if got, err := index.Query(bounds.min, bounds.max); err == nil || len(got) != 0 {
				t.Fatal("invalid query returned matches")
			}
		}
	}
	got, err := i.POI.SourceIndex.Query([3]float32{-1, -1, -1}, [3]float32{2, 1, 1})
	if err != nil || len(got) != 2 {
		t.Fatal(err)
	}
	got[0] = 99
	i08Query(t, i.POI.SourceIndex, [3]float32{-1, -1, -1}, [3]float32{2, 1, 1}, 0, 1)
}

func TestI08IndexDetachesInputAndPublicRecords(t *testing.T) {
	terrain, poi := i07TerrainManifest(), i06World()
	level := i08Level()
	level.StreamingBounds = &LevelStreamingBoundsDef{BoundsMin: [3]float32{-3000, -1, -1}, BoundsMax: [3]float32{100, 100, 3000}}
	i := i08Build(t, level, terrain, poi)
	terrain.Pages[0].Tags[0] = "input changed"
	terrain.Pages[4].ChildPageIndices[0] = 99
	terrain.Entries[0].WorldOrigin[0] = 99
	poi.Pages[0].LeafEntryIndices[0] = 99
	poi.IndexedSectors[0].BoundsMax[0] = 999
	level.StreamingBounds.BoundsMax[0] = 999
	if i.Terrain.Pages[0].Tags[0] != "owned" || i.Terrain.Pages[4].ChildPageIndices[0] != 0 || i.POI.Pages[0].LeafEntryIndices[0] != 0 || i.StreamingBounds.BoundsMax[0] != 100 {
		t.Fatal("index aliases inputs")
	}
	i.Terrain.SourceBounds[0].BoundsMin[0] = 999
	i.Terrain.PageBounds[0].BoundsMin[0] = 999
	i.POI.SectorBounds[0].BoundsMin[0] = 999
	i.Terrain.Pages[0].BoundsMin[0] = 999
	i.Terrain.RootPageIndices[0] = 99
	i.Terrain.Forest.ParentPageIndices[0] = 99
	i.POI.EntryIndexByCoord[TerrainChunkCoordDef{}] = 99
	i08Query(t, i.Terrain.SourceIndex, [3]float32{-200, 20, 20}, [3]float32{-200, 20, 20}, 0)
	i08Query(t, i.Terrain.PageIndex, [3]float32{-200, 20, 20}, [3]float32{-200, 20, 20}, 0, 4, 5)
	i08Query(t, i.POI.SectorIndex, [3]float32{2, 2, 2}, [3]float32{2, 2, 2}, 0)
	if terrain.Pages[0].BoundsMin[0] == 999 || poi.PageIndex != nil || terrain.PageIndex != nil {
		t.Fatal("output mutation changed input or installed caches")
	}
}

func TestI08CoverageGroupsRequireOverlappingCrossLayerMembers(t *testing.T) {
	terrain, poi := i07TerrainManifest(), i06World()
	terrain.Pages[1].CoverageGroup, poi.Pages[3].CoverageGroup = "poi-landmark", "poi-landmark"
	i08Build(t, i08Level(), terrain, poi) // Closed X=0 contact is an overlap.
	for name, mutate := range map[string]func(*TerrainChunkManifestDef, *ImportedWorldDef){
		"terrain-only":     func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.Pages[3].CoverageGroup = "" },
		"poi-only":         func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.Pages[1].CoverageGroup = "" },
		"different-poi-id": func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.Pages[3].CoverageGroup = "another-poi" },
		"disjoint-members": func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) {
			d.Pages[1].CoverageGroup = ""
			d.Pages[0].CoverageGroup = "poi-landmark"
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, p := i07TerrainManifest(), i06World()
			d.Pages[1].CoverageGroup, p.Pages[3].CoverageGroup = "poi-landmark", "poi-landmark"
			mutate(d, p)
			if got, err := BuildLevelStreamingIndex(i08Level(), d, p); err == nil || got != nil {
				t.Fatal("accepted unpaired coverage group")
			}
		})
	}
}

func TestI08ConstructorRejectsMalformedMetadata(t *testing.T) {
	if got, err := BuildLevelStreamingIndex(nil, nil, nil); err == nil || got != nil {
		t.Fatal("nil level accepted")
	}
	for name, mutate := range map[string]func(*TerrainChunkManifestDef, *ImportedWorldDef){
		"terrain-version":        func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.SchemaVersion = 1 },
		"terrain-duplicate":      func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.Entries = append(d.Entries, d.Entries[0]) },
		"terrain-negative-count": func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.Entries[0].NonEmptyVoxelCount = -1 },
		"terrain-negative-grid":  func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.Entries[0].ChunkSize = -1 },
		"terrain-nan":            func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.VoxelResolution = float32(math.NaN()) },
		"terrain-lost-progress":  func(d *TerrainChunkManifestDef, _ *ImportedWorldDef) { d.Entries[0].Coord.X = int(^uint(0) >> 1) },
		"poi-version":            func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.SchemaVersion = 4 },
		"poi-duplicate":          func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.Entries = append(p.Entries, p.Entries[0]) },
		"poi-negative-count":     func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.Entries[0].NonEmptyVoxelCount = -1 },
		"poi-infinity":           func(_ *TerrainChunkManifestDef, p *ImportedWorldDef) { p.VoxelResolution = float32(math.Inf(1)) },
	} {
		t.Run(name, func(t *testing.T) {
			d, p := i08LegacyTerrain(), i08LegacyPOI()
			mutate(d, p)
			if got, err := BuildLevelStreamingIndex(i08Level(), d, p); err == nil || got != nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	for _, bad := range []struct {
		terrain *TerrainChunkManifestDef
		poi     *ImportedWorldDef
	}{
		{func() *TerrainChunkManifestDef {
			d := i07TerrainManifest()
			d.Pages[4].ChildPageIndices[0] = 99
			return d
		}(), nil},
		{nil, func() *ImportedWorldDef { d := i06World(); d.Pages[0].LeafEntryIndices[0] = 99; return d }()},
	} {
		if got, err := BuildLevelStreamingIndex(i08Level(), bad.terrain, bad.poi); err == nil || got != nil {
			t.Fatal("constructor bypassed strict forest validation")
		}
	}
}

func TestI08AbsentAndEmptyLayers(t *testing.T) {
	i := i08Build(t, i08Level(), nil, nil)
	if i.IndependentLayers || i.Terrain != nil || i.POI != nil || i.StreamingBounds != nil {
		t.Fatal("absent layers fabricated")
	}
	i = i08Build(t, i08Level(), &TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "empty-terrain"}, &ImportedWorldDef{SchemaVersion: 2, WorldID: "empty-poi"})
	for _, layer := range []*LevelStreamingLayerIndex{i.Terrain, i.POI} {
		if len(layer.SourceBounds)+len(layer.PageBounds)+len(layer.SectorBounds) != 0 {
			t.Fatal("empty layer fabricated coverage")
		}
		for _, index := range []*LevelStreamingBoundsIndex{layer.SourceIndex, layer.PageIndex, layer.SectorIndex} {
			i08Query(t, index, [3]float32{}, [3]float32{})
		}
	}
}
