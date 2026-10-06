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

func i07TerrainManifest() *TerrainChunkManifestDef {
	d := &TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "terrain-pages", SourceHash: strings.Repeat("a", 64), ChunkSize: 128, VoxelResolution: 2, Entries: []TerrainChunkEntryDef{{Coord: TerrainChunkCoordDef{X: -1}, WorldOrigin: [3]float32{-256, 0, 0}, TerrainID: "terrain-pages", SourceHash: strings.Repeat("a", 64), ChunkSize: 128, VoxelResolution: 2, ChunkPath: "source.gkchunk", PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 32768, HeightOffset: 10, HeightScale: 80}}}
	add := func(level uint8, origin [3]float32, side int, spacing, res float32, children []uint32) {
		span := float32(side) * spacing
		d.Pages = append(d.Pages, StreamPageDef{Level: level, BoundsMin: [3]float32{origin[0], 10, origin[2]}, BoundsMax: [3]float32{origin[0] + span, 90, origin[2] + span}, Payload: StreamPagePayloadDef{Kind: TerrainHeightTilePayloadKind, Path: "page" + string(rune('0'+len(d.Pages))) + ".gkchunk", WorldOrigin: origin, ChunkSize: side, SampleSpacing: spacing, VoxelResolution: res, HeightOffset: 10, HeightScale: 80, PayloadHash: strings.Repeat("b", 64), PayloadSizeBytes: side * side * 2}, ChildPageIndices: children, Tags: []string{"owned"}})
	}
	for _, z := range []float32{0, 128} {
		for _, x := range []float32{-256, -128} {
			add(StreamPageLevelRegional, [3]float32{x, 0, z}, 64, 2, 1, nil)
		}
	}
	add(StreamPageLevelMacro, [3]float32{-512, 0, 0}, 128, 4, 4, []uint32{0, 1, 2, 3})
	add(StreamPageLevelRoot, [3]float32{-2048, 0, 0}, 128, 16, 16, []uint32{4})
	d.RootPageIndices = []uint32{5}
	return d
}
func i07Clone(t *testing.T, d *TerrainChunkManifestDef) *TerrainChunkManifestDef {
	t.Helper()
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	var out TerrainChunkManifestDef
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return &out
}

func TestI07TerrainSourceAndVisualOwnershipAreIndependent(t *testing.T) {
	d := i07TerrainManifest()
	before := i07Clone(t, d)
	index, e := ValidateTerrainPageManifest(d)
	if e != nil {
		t.Fatal(e)
	}
	if index.LegacyDistance || index.SourceOnly || len(index.Forest.LeafOwnerPageIndices) != 0 || index.EntryIndexByCoord[TerrainChunkCoordDef{X: -1}] != 0 || !reflect.DeepEqual(index.Forest.ParentPageIndices, []int{4, 4, 4, 4, 5, -1}) {
		t.Fatalf("incorrect independent terrain index: %+v", index)
	}
	for _, p := range index.Pages {
		if len(p.LeafEntryIndices) != 0 {
			t.Fatal("backing source tile gained visual ownership")
		}
	}
	index.Pages[0].Tags[0] = "changed"
	index.Pages[4].ChildPageIndices[0] = 99
	index.RootPageIndices[0] = 99
	index.EntryIndexByCoord[TerrainChunkCoordDef{X: -1}] = 99
	if !reflect.DeepEqual(d, before) {
		t.Fatal("normalized index aliases authored manifest")
	}
	path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	if e = SaveTerrainChunkManifest(path, d); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadTerrainChunkManifest(path)
	if e != nil || loaded.PageIndex == nil || loaded.PageIndex.SourceOnly {
		t.Fatalf("paged metadata load: %v", e)
	}
	loaded.PageIndex = nil
	if !reflect.DeepEqual(loaded, d) {
		t.Fatal("wire roundtrip changed backing/visual references")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "PageIndex") || strings.Contains(string(raw), "page_index") {
		t.Fatal("derived index serialized")
	}
}

func TestI07TerrainStrictPageManifestRejectsMalformedContracts(t *testing.T) {
	cases := map[string]func(*TerrainChunkManifestDef){
		"source-generation": func(d *TerrainChunkManifestDef) {
			d.SourceHash = "legacy-identity"
			d.Entries[0].SourceHash = d.SourceHash
		},
		"source-offset-only":           func(d *TerrainChunkManifestDef) { d.Entries[0].HeightOffset = 10; d.Entries[0].HeightScale = 0 },
		"source-calibration-nonfinite": func(d *TerrainChunkManifestDef) { d.Entries[0].HeightScale = float32(math.Inf(1)) },
		"voxel-payload-kind":           func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.Kind = ImportedWorldChunkPayloadDenseRLEBinaryV1 },
		"negative-sector-cost":         func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.OccupiedSectorCount = -1 },
		"negative-brick-cost":          func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.OccupiedBrickCount = -1 },
		"source-calibration":           func(d *TerrainChunkManifestDef) { d.Entries[0].HeightScale = 0 },
		"source-owned-as-leaf":         func(d *TerrainChunkManifestDef) { d.Pages[0].LeafEntryIndices = []uint32{0} },
		"leaf-level":                   func(d *TerrainChunkManifestDef) { d.Pages[0].Level = StreamPageLevelLeaf },
		"root-level":                   func(d *TerrainChunkManifestDef) { d.RootPageIndices = []uint32{4} },
		"missing-roots":                func(d *TerrainChunkManifestDef) { d.RootPageIndices = nil },
		"duplicate-roots":              func(d *TerrainChunkManifestDef) { d.RootPageIndices = []uint32{5, 5} },
		"orphan":                       func(d *TerrainChunkManifestDef) { d.Pages[4].ChildPageIndices = d.Pages[4].ChildPageIndices[:3] },
		"shared-child":                 func(d *TerrainChunkManifestDef) { d.Pages[5].ChildPageIndices = append(d.Pages[5].ChildPageIndices, 0) },
		"child-oob":                    func(d *TerrainChunkManifestDef) { d.Pages[4].ChildPageIndices[0] = 99 },
		"payload-height-bounds":        func(d *TerrainChunkManifestDef) { d.Pages[0].BoundsMax[1] = 89 },
		"child-bounds":                 func(d *TerrainChunkManifestDef) { d.Pages[4].BoundsMin[0] = -128 },
		"spacing":                      func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.SampleSpacing = 0 },
		"target":                       func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.VoxelResolution = 0 },
		"target-finer-ratio":           func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.VoxelResolution = .75 },
		"scale":                        func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.HeightScale = 0 },
		"offset-nonfinite":             func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.HeightOffset = float32(math.NaN()) },
		"origin-lattice":               func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.WorldOrigin[0]++ },
		"origin-y":                     func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.WorldOrigin[1] = 1 },
		"oversized-grid":               func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.ChunkSize = 129 },
		"hash":                         func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.PayloadHash = "wrong" },
		"body-size":                    func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.PayloadSizeBytes = 1 },
		"aux":                          func(d *TerrainChunkManifestDef) { d.Pages[0].Payload.Aux = &ImportedWorldChunkAuxRefDef{} },
		"duplicate-source":             func(d *TerrainChunkManifestDef) { d.Entries = append(d.Entries, d.Entries[0]) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := i07TerrainManifest()
			mutate(d)
			if e := ValidateTerrainHeightTileManifest(d); e == nil {
				t.Fatal("source manifest validator bypassed invalid visual forest")
			}
			if index, e := ValidateTerrainPageManifest(d); e == nil || index != nil {
				t.Fatal("accepted invalid paged terrain")
			}
		})
	}
}

func TestI07TerrainLegacyAndSourceOnlyNormalization(t *testing.T) {
	legacy := &TerrainChunkManifestDef{TerrainID: "legacy", ChunkSize: 4, VoxelResolution: 2, Entries: []TerrainChunkEntryDef{{Coord: TerrainChunkCoordDef{X: -2, Y: 1}, ChunkSize: 4, VoxelResolution: 2, ChunkPath: "a", NonEmptyVoxelCount: 10, PayloadHash: "optional", PayloadSizeBytes: 7}, {Coord: TerrainChunkCoordDef{}, ChunkSize: 4, VoxelResolution: 2, ChunkPath: "empty"}, {Coord: TerrainChunkCoordDef{X: 3}, ChunkSize: 4, VoxelResolution: 2, ChunkPath: "b", NonEmptyVoxelCount: 1}}}
	before := i07Clone(t, legacy)
	index, e := NormalizeTerrainPages(legacy)
	if e != nil {
		t.Fatal(e)
	}
	if !index.LegacyDistance || index.SourceOnly || !reflect.DeepEqual(index.Forest.LeafOwnerPageIndices, []int{0, -1, 1}) || !reflect.DeepEqual(index.RootPageIndices, []uint32{0, 1}) {
		t.Fatalf("legacy index: %+v", index)
	}
	if index.Pages[0].Payload.PayloadHash != "optional" || index.Pages[0].Payload.PayloadSizeBytes != 7 || index.Pages[0].BoundsMin != ([3]float32{-16, 8, 0}) || index.Pages[0].BoundsMax != ([3]float32{-8, 28, 8}) {
		t.Fatalf("legacy ordered metadata/column bounds: %+v", index.Pages[0])
	}
	if !reflect.DeepEqual(legacy, before) {
		t.Fatal("legacy normalization changed source defaults")
	}
	d := i07TerrainManifest()
	d.Pages = nil
	d.RootPageIndices = nil
	d.Entries[0].HeightOffset = 0
	d.Entries[0].HeightScale = 0
	d.SourceHash = "arbitrary-source-only-identity"
	d.Entries[0].SourceHash = d.SourceHash
	source, e := ValidateTerrainPageManifest(d)
	if e != nil || !source.SourceOnly || source.LegacyDistance || len(source.Pages) != 0 {
		t.Fatalf("existing source-only v3 changed: %v", e)
	}
	for _, version := range []int{-1, 1, 4} {
		legacy.SchemaVersion = version
		if got, e := NormalizeTerrainPages(legacy); e == nil || got != nil {
			t.Fatalf("accepted unsupported version %d", version)
		}
	}
}

func TestI07TerrainWireFailsClosedAndSavePreservesPreviousManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	old := []byte("previous manifest")
	if e := os.WriteFile(path, old, 0600); e != nil {
		t.Fatal(e)
	}
	d := i07TerrainManifest()
	d.RootPageIndices = []uint32{99}
	if e := SaveTerrainChunkManifest(path, d); e == nil {
		t.Fatal("saved invalid pages")
	}
	got, e := os.ReadFile(path)
	if e != nil || string(got) != string(old) {
		t.Fatal("invalid save replaced prior manifest")
	}
	for _, field := range []string{"pages", "root_page_indices"} {
		raw := `{"schema_version":2,"terrain_id":"legacy","chunk_size":4,"voxel_resolution":1,"` + field + `":[]}`
		if e = os.WriteFile(path, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if got, e := LoadTerrainChunkManifest(path); e == nil || got != nil {
			t.Fatal("legacy silently accepted page reference field")
		}
	}
	legacy := &TerrainChunkManifestDef{TerrainID: "legacy", ChunkSize: 4, VoxelResolution: 1}
	if e = SaveTerrainChunkManifest(path, legacy); e != nil {
		t.Fatal(e)
	}
	gotLegacy, e := LoadTerrainChunkManifest(path)
	if e != nil || gotLegacy.SchemaVersion != 2 || gotLegacy.PageIndex == nil || !gotLegacy.PageIndex.LegacyDistance {
		t.Fatalf("legacy default reader/writer: %v", e)
	}
}

func TestI07TerrainPagePayloadQualifiesHeaderAndSourceCalibration(t *testing.T) {
	d := i07TerrainManifest()
	path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	p := &d.Pages[0].Payload
	tile := &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: d.SourceHash, Coord: TerrainChunkCoordDef{X: -2}, WorldOrigin: p.WorldOrigin, SampleWidth: 64, SampleHeight: 64, SampleSpacing: 2, HeightOffset: 10, HeightScale: 80, HeightSamples: make([]uint16, 64*64)}
	tile.HeightSamples[0] = 1234
	tile.SurfaceMask = make([]byte, 512)
	tile.SurfaceMask[0] = 1
	payloadPath := filepath.Join(filepath.Dir(path), p.Path)
	result, e := SaveTerrainHeightTile(payloadPath, tile)
	if e != nil {
		t.Fatal(e)
	}
	p.PayloadHash = result.PayloadHash
	p.PayloadSizeBytes = result.PayloadSizeBytes
	got, e := LoadTerrainHeightPagePayload(d, path, 0)
	if e != nil || got.HeightSamples[0] != 1234 || !reflect.DeepEqual(got.SurfaceMask, tile.SurfaceMask) {
		t.Fatalf("qualified page payload: %v", e)
	}
	for name, mutate := range map[string]func(*TerrainHeightTileDef){"terrain": func(v *TerrainHeightTileDef) { v.TerrainID = "foreign" }, "source": func(v *TerrainHeightTileDef) { v.SourceHash = "foreign" }, "coord": func(v *TerrainHeightTileDef) { v.Coord.X++ }, "origin": func(v *TerrainHeightTileDef) { v.WorldOrigin[0]++ }, "spacing": func(v *TerrainHeightTileDef) { v.SampleSpacing = 4 }, "offset": func(v *TerrainHeightTileDef) { v.HeightOffset = 11 }, "scale": func(v *TerrainHeightTileDef) { v.HeightScale = 81 }} {
		t.Run(name, func(t *testing.T) {
			changed := *tile
			mutate(&changed)
			r, e := SaveTerrainHeightTile(payloadPath, &changed)
			if e != nil {
				t.Fatal(e)
			}
			if r.PayloadHash != result.PayloadHash {
				t.Fatal("header-only mutation changed body hash")
			}
			if got, e := LoadTerrainHeightPagePayload(d, path, 0); e == nil || got != nil {
				t.Fatal("accepted wrong page owner/grid/calibration")
			}
			if _, e = SaveTerrainHeightTile(payloadPath, tile); e != nil {
				t.Fatal(e)
			}
		})
	}
	if got, e := LoadTerrainHeightPagePayload(d, path, 99); e == nil || got != nil {
		t.Fatal("accepted out-of-range page")
	}
	source := &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: d.SourceHash, Coord: d.Entries[0].Coord, WorldOrigin: d.Entries[0].WorldOrigin, SampleWidth: 128, SampleHeight: 128, SampleSpacing: 2, HeightOffset: 10, HeightScale: 80, HeightSamples: make([]uint16, 128*128)}
	r, e := SaveTerrainHeightTile(filepath.Join(filepath.Dir(path), "source.gkchunk"), source)
	if e != nil {
		t.Fatal(e)
	}
	d.Entries[0].PayloadHash = r.PayloadHash
	d.Entries[0].PayloadSizeBytes = r.PayloadSizeBytes
	if e = ValidateTerrainHeightTileReference(d.Entries[0], source); e != nil {
		t.Fatal(e)
	}
	source.HeightScale = 81
	if e = ValidateTerrainHeightTileReference(d.Entries[0], source); e == nil {
		t.Fatal("optional declared source calibration ignored")
	}
	d.Entries[0].HeightOffset = 0
	d.Entries[0].HeightScale = 0
	if e = ValidateTerrainHeightTileReference(d.Entries[0], source); e != nil {
		t.Fatal("legacy unspecified calibration stopped working")
	}
	d.Entries[0].HeightOffset = 10
	if e = ValidateTerrainHeightTileReference(d.Entries[0], source); e == nil {
		t.Fatal("offset-only calibration accepted without positive scale")
	}
	d.Entries[0].HeightScale = float32(math.Inf(1))
	if e = ValidateTerrainHeightTileReference(d.Entries[0], source); e == nil {
		t.Fatal("nonfinite optional calibration accepted")
	}
}

func TestI07TerrainLegacyValidationAndEntryGridFallback(t *testing.T) {
	fixture := func() *TerrainChunkManifestDef {
		return &TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "legacy", ChunkSize: 4, VoxelResolution: 2, Entries: []TerrainChunkEntryDef{{Coord: TerrainChunkCoordDef{X: -2}, ChunkSize: 4, VoxelResolution: 2, ChunkPath: "legacy.gkchunk", NonEmptyVoxelCount: 10}}}
	}
	cases := map[string]func(*TerrainChunkManifestDef){
		"negative-top-grid":   func(d *TerrainChunkManifestDef) { d.VoxelResolution = -1 },
		"negative-entry-grid": func(d *TerrainChunkManifestDef) { d.Entries[0].VoxelResolution = -1 },
		"negative-entry-size": func(d *TerrainChunkManifestDef) { d.Entries[0].ChunkSize = -1 },
		"negative-count":      func(d *TerrainChunkManifestDef) { d.Entries[0].NonEmptyVoxelCount = -1 },
		"duplicate-coord":     func(d *TerrainChunkManifestDef) { d.Entries = append(d.Entries, d.Entries[0]) },
		"nonfinite":           func(d *TerrainChunkManifestDef) { d.Entries[0].VoxelResolution = float32(math.Inf(1)) },
		"overflow": func(d *TerrainChunkManifestDef) {
			d.VoxelResolution = math.MaxFloat32
			d.Entries[0].VoxelResolution = math.MaxFloat32
		},
		"collapsed": func(d *TerrainChunkManifestDef) { d.Entries[0].Coord.X = int(^uint(0) >> 1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := fixture()
			mutate(d)
			if got, e := NormalizeTerrainPages(d); e == nil || got != nil {
				t.Fatal("accepted invalid legacy grid, ownership or bounds")
			}
		})
	}
	d := fixture()
	d.Entries[0].ChunkSize = 0
	d.Entries[0].VoxelResolution = 0
	before := i07Clone(t, d)
	index, e := NormalizeTerrainPages(d)
	if e != nil {
		t.Fatal(e)
	}
	if index.Pages[0].Payload.ChunkSize != 4 || index.Pages[0].Payload.VoxelResolution != 2 || index.Pages[0].BoundsMin != ([3]float32{-16, 0, 0}) || index.Pages[0].BoundsMax != ([3]float32{-8, 20, 8}) {
		t.Fatal("zero legacy entry grid failed top-header fallback")
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("legacy fallback mutated authored grid")
	}
}

func TestI07TerrainPagePayloadRejectsNavigationAndOtherKinds(t *testing.T) {
	d := i07TerrainManifest()
	d.Pages = d.Pages[:1]
	d.Pages[0].Level = StreamPageLevelRoot
	d.RootPageIndices = []uint32{0}
	d.Pages[0].BoundsMin = [3]float32{-256, 10, 0}
	d.Pages[0].BoundsMax = [3]float32{0, 90, 256}
	p := &d.Pages[0].Payload
	p.ChunkSize = 128
	p.WorldOrigin = [3]float32{-256, 0, 0}
	p.PayloadSizeBytes = 32768
	path := filepath.Join(t.TempDir(), "terrain.gkterrainmanifest")
	tile := &TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: d.SourceHash, Coord: TerrainChunkCoordDef{X: -1}, WorldOrigin: p.WorldOrigin, SampleWidth: 128, SampleHeight: 128, SampleSpacing: 2, HeightOffset: 10, HeightScale: 80, HeightSamples: make([]uint16, 128*128)}
	payloadPath := filepath.Join(filepath.Dir(path), p.Path)
	saved, e := SaveTerrainHeightTile(payloadPath, tile)
	if e != nil {
		t.Fatal(e)
	}
	p.PayloadHash = saved.PayloadHash
	p.PayloadSizeBytes = saved.PayloadSizeBytes
	if _, e = LoadTerrainHeightPagePayload(d, path, 0); e != nil {
		t.Fatalf("valid no-navigation control: %v", e)
	}
	p.Kind = ImportedWorldChunkPayloadDenseRLEBinaryV1
	if got, e := LoadTerrainHeightPagePayload(d, path, 0); e == nil || got != nil {
		t.Fatal("height page loader accepted another payload kind")
	}
	p.Kind = TerrainHeightTilePayloadKind
	// Navigation is a valid backing-tile payload, but is forbidden on render pages.
	tile.OutdoorNavCellSize = 1
	tile.OutdoorNavExclusionMask = make([]byte, 8192)
	saved, e = SaveTerrainHeightTile(payloadPath, tile)
	if e != nil {
		t.Fatal(e)
	}
	p.PayloadHash = saved.PayloadHash
	p.PayloadSizeBytes = saved.PayloadSizeBytes
	if got, e := LoadTerrainHeightPagePayload(d, path, 0); e == nil || got != nil {
		t.Fatal("render page accepted navigation-owned backing data")
	}
}

func TestI07VoxelPageOwnerRejectsHeightCalibration(t *testing.T) {
	for name, mutate := range map[string]func(*StreamPagePayloadDef){"offset": func(p *StreamPagePayloadDef) { p.HeightOffset = 1 }, "scale": func(p *StreamPagePayloadDef) { p.HeightScale = 1 }} {
		t.Run(name, func(t *testing.T) {
			d := i06World()
			if _, e := ValidateImportedWorldV3(d); e != nil {
				t.Fatalf("valid voxel control: %v", e)
			}
			mutate(&d.Pages[0].Payload)
			if index, e := ValidateImportedWorldV3(d); e == nil || index != nil {
				t.Fatal("voxel payload accepted height-owner calibration")
			}
		})
	}
}
