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

func i05LegacyWorld() *ImportedWorldDef {
	a, b, c := TerrainChunkCoordDef{X: 1}, TerrainChunkCoordDef{}, TerrainChunkCoordDef{X: -1}
	s0, s1 := TerrainChunkCoordDef{}, TerrainChunkCoordDef{X: -1}
	return &ImportedWorldDef{WorldID: "legacy", SchemaVersion: 2, ChunkSize: 4, VoxelResolution: 1, Tags: []string{"world"}, Entries: []ImportedWorldChunkEntryDef{{Coord: a, ChunkPath: "a.gkchunk", NonEmptyVoxelCount: 1}, {Coord: b, ChunkPath: "b.gkchunk", NonEmptyVoxelCount: 2}, {Coord: c, ChunkPath: "c.gkchunk", NonEmptyVoxelCount: 3}}, Sectors: []ImportedWorldSectorDef{{Coord: s0, BoundsMin: [3]float32{0, 0, 0}, BoundsMax: [3]float32{1, 1, 1}, FullChunkRefs: []TerrainChunkCoordDef{a, b}, VisibleSectorRefs: []TerrainChunkCoordDef{s1, s0}, AdjacentSectorRefs: []TerrainChunkCoordDef{s1}, VisibilityID: "visibility0", SourceLeafIDs: []int{8, 4}, Tags: []string{"sector0"}, LODs: []ImportedWorldLODDef{{Level: 3, Kind: "voxel_proxy", ChunkPath: "first.gkchunk", ChunkSize: 12, VoxelResolution: 1, NonEmptyVoxelCount: 2, Tags: []string{"first"}}, {Level: 1, Kind: "voxel_proxy", ChunkPath: "second.gkchunk", ChunkSize: 4, VoxelResolution: 1, NonEmptyVoxelCount: 1}}}, {Coord: s1, BoundsMin: [3]float32{-1, 0, 0}, BoundsMax: [3]float32{0, 1, 1}, FullChunkRefs: []TerrainChunkCoordDef{c}, Tags: []string{"sector1"}}}}
}

func TestI05LegacyPagesPreserveIndexedOrderAndOwnership(t *testing.T) {
	d := i05LegacyWorld()
	before := i05Clone(t, d)
	index, e := NormalizeImportedWorldPages(d)
	if e != nil {
		t.Fatal(e)
	}
	if !index.LegacyDistance || len(index.Pages) != 2 || !reflect.DeepEqual(index.RootPageIndices, []uint32{0, 1}) || !reflect.DeepEqual(index.ProxylessPageIndices, []uint32{1}) {
		t.Fatalf("compatibility index %+v", index)
	}
	if !reflect.DeepEqual(index.Forest.ParentPageIndices, []int{-1, -1}) || !reflect.DeepEqual(index.Forest.LeafOwnerPageIndices, []int{0, 0, 1}) {
		t.Fatalf("forest %+v", index.Forest)
	}
	for i, entry := range d.Entries {
		if index.EntryIndexByCoord[entry.Coord] != i {
			t.Fatal("entry array order lost")
		}
	}
	for i, sector := range d.Sectors {
		if index.SectorIndexByCoord[sector.Coord] != i || index.Sectors[i].BoundsMin != sector.BoundsMin || index.Sectors[i].BoundsMax != sector.BoundsMax || !reflect.DeepEqual(index.Sectors[i].Tags, sector.Tags) {
			t.Fatal("authored sector identity/bounds lost")
		}
	}
	if !reflect.DeepEqual(index.Pages[0].LeafEntryIndices, []uint32{0, 1}) || !reflect.DeepEqual(index.Sectors[0].FullChunkIndices, []uint32{0, 1}) || !reflect.DeepEqual(index.Sectors[0].VisibleSectorIndices, []uint32{1, 0}) || !reflect.DeepEqual(index.Sectors[0].AdjacentSectorIndices, []uint32{1}) || !reflect.DeepEqual(index.Sectors[0].SourceLeafIDs, []int{8, 4}) {
		t.Fatal("reference order changed")
	}
	page := index.Pages[0]
	if page.Level != StreamPageLevelLeaf || page.Payload.Path != "first.gkchunk" || page.Payload.Kind != ImportedWorldChunkPayloadSparseJSONV1 || page.Payload.ChunkSize != 12 || page.Payload.VoxelResolution != 1 || page.Payload.PayloadHash != "" || page.Payload.PayloadSizeBytes != 0 {
		t.Fatalf("firstLOD/optional metadata %+v", page.Payload)
	}
	if page.BoundsMin != ([3]float32{}) || page.BoundsMax != ([3]float32{12, 12, 12}) || index.Pages[1].BoundsMin[0] != -4 || index.Pages[1].BoundsMax[0] != 0 {
		t.Fatalf("coverage union %+v", index.Pages)
	}
	// Runtime indexes are independent of authored slices/maps in both directions.
	index.Pages[0].Tags[0] = "mutated"
	index.Pages[0].LeafEntryIndices[0] = 99
	index.Sectors[0].Tags[0] = "mutated"
	index.Sectors[0].SourceLeafIDs[0] = 99
	index.EntryIndexByCoord[d.Entries[0].Coord] = 99
	if !reflect.DeepEqual(d, before) {
		t.Fatal("normalized output aliases source")
	}
	d.Sectors[0].Tags[0] = "source-change"
	d.Sectors[0].FullChunkRefs[0] = TerrainChunkCoordDef{X: 99}
	if index.Sectors[0].Tags[0] == "source-change" || index.Sectors[0].FullChunkIndices[0] != 0 {
		t.Fatal("source mutation leaked into index")
	}
}

func TestI05LegacyFirstProxyEligibilityAndActualGridOrigin(t *testing.T) {
	for _, mutate := range []func(*ImportedWorldLODDef){func(l *ImportedWorldLODDef) { l.Kind = "other" }, func(l *ImportedWorldLODDef) { l.NonEmptyVoxelCount = 0 }, func(l *ImportedWorldLODDef) { l.ChunkPath = "" }} {
		d := i05LegacyWorld()
		mutate(&d.Sectors[0].LODs[0])
		index, e := NormalizeImportedWorldPages(d)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(index.ProxylessPageIndices, []uint32{0, 1}) || index.Pages[0].Payload.Path == "second.gkchunk" {
			t.Fatal("skipped ineligible firstLOD")
		}
	}
	d := i05LegacyWorld()
	d.Sectors[1].LODs = []ImportedWorldLODDef{{Kind: "voxel_proxy", ChunkPath: "negative.gkchunk", ChunkSize: 3, VoxelResolution: 2, NonEmptyVoxelCount: 1}}
	index, e := NormalizeImportedWorldPages(d)
	if e != nil {
		t.Fatal(e)
	}
	if index.Pages[1].Payload.WorldOrigin != ([3]float32{-6, 0, 0}) || index.Pages[1].BoundsMin[0] != -6 || index.Pages[1].BoundsMax[1] != 6 {
		t.Fatalf("actual proxy grid placement %+v", index.Pages[1])
	}
	d.Sectors[0].LODs[0].ChunkSize = 0
	d.Sectors[0].LODs[0].VoxelResolution = 0
	index, e = NormalizeImportedWorldPages(d)
	if e != nil {
		t.Fatal(e)
	}
	p := index.Pages[0].Payload
	if p.ChunkSize != 0 || p.VoxelResolution != 0 || p.WorldOrigin != ([3]float32{}) || index.Pages[0].BoundsMax[0] != 8 {
		t.Fatalf("unknown optional proxy grid invented %+v", index.Pages[0])
	}
}

func TestI05LegacyDefaultsAndVersionedReaderRuntimeOnlyIndex(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			d := i05LegacyWorld()
			d.SchemaVersion = version
			d.Sectors = nil
			before := i05Clone(t, d)
			index, e := NormalizeImportedWorldPages(d)
			if e != nil || !index.LegacyDistance || len(index.Pages) == 0 || !reflect.DeepEqual(d, before) {
				t.Fatalf("legacy normalize %v", e)
			}
			defaults := i05Clone(t, d)
			EnsureImportedWorldDefaults(defaults)
			if len(index.Sectors) != len(defaults.Sectors) {
				t.Fatal("generated sector defaults differ")
			}
			for i, s := range defaults.Sectors {
				if index.Sectors[i].Coord != s.Coord || index.Sectors[i].BoundsMin != s.BoundsMin || index.Sectors[i].BoundsMax != s.BoundsMax {
					t.Fatal("generated sector bounds differ")
				}
				for j, c := range s.FullChunkRefs {
					if index.Sectors[i].FullChunkIndices[j] != uint32(index.EntryIndexByCoord[c]) {
						t.Fatal("generated refs")
					}
				}
			}
			p := filepath.Join(t.TempDir(), "legacy.gkworld")
			b, e := json.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			loaded, e := LoadImportedWorld(p)
			if e != nil || loaded.PageIndex == nil || loaded.SchemaVersion != 2 || !reflect.DeepEqual(loaded.PageIndex, index) {
				t.Fatalf("reader normalization %v", e)
			}
			loaded.SchemaVersion = 2
			if e = SaveImportedWorld(p, loaded); e != nil {
				t.Fatal(e)
			}
			b, e = os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(b, &fields); e != nil {
				t.Fatal(e)
			}
			for _, key := range []string{"PageIndex", "page_index", "pages", "root_page_indices"} {
				if _, ok := fields[key]; ok {
					t.Fatalf("runtime index serialized as %s", key)
				}
			}
			if string(fields["schema_version"]) != "2" || CurrentImportedWorldSchemaVersion != 2 || CurrentImportedWorldChunkSchemaVersion != 1 {
				t.Fatal("writer/chunk version advanced")
			}
		})
	}
	empty, e := NormalizeImportedWorldPages(&ImportedWorldDef{SchemaVersion: 2})
	if e != nil || len(empty.Pages) != 0 || len(empty.Sectors) != 0 {
		t.Fatalf("empty world %v", e)
	}
}

func TestI05LegacyNormalizationRejectsMalformedStructure(t *testing.T) {
	if got, e := NormalizeImportedWorldPages(nil); e == nil || got != nil {
		t.Fatal("nil accepted")
	}
	for name, mutate := range map[string]func(*ImportedWorldDef){"negative-version": func(d *ImportedWorldDef) { d.SchemaVersion = -1 }, "future-version": func(d *ImportedWorldDef) { d.SchemaVersion = 3 }, "duplicate-entry": func(d *ImportedWorldDef) { d.Entries = append(d.Entries, d.Entries[0]) }, "duplicate-sector": func(d *ImportedWorldDef) { d.Sectors = append(d.Sectors, d.Sectors[0]) }, "duplicate-full-ref": func(d *ImportedWorldDef) {
		d.Sectors[0].FullChunkRefs = append(d.Sectors[0].FullChunkRefs, d.Sectors[0].FullChunkRefs[0])
	}, "missing-full-ref": func(d *ImportedWorldDef) { d.Sectors[0].FullChunkRefs[0] = TerrainChunkCoordDef{X: 99} }, "shared-entry": func(d *ImportedWorldDef) {
		d.Sectors[1].FullChunkRefs = append(d.Sectors[1].FullChunkRefs, d.Entries[0].Coord)
	}, "unowned-entry": func(d *ImportedWorldDef) { d.Sectors[0].FullChunkRefs = d.Sectors[0].FullChunkRefs[:1] }, "missing-visible-sector": func(d *ImportedWorldDef) { d.Sectors[0].VisibleSectorRefs = []TerrainChunkCoordDef{{X: 99}} }, "duplicate-visible-sector": func(d *ImportedWorldDef) {
		d.Sectors[0].VisibleSectorRefs = append(d.Sectors[0].VisibleSectorRefs, d.Sectors[0].VisibleSectorRefs[0])
	}, "missing-adjacent-sector": func(d *ImportedWorldDef) { d.Sectors[0].AdjacentSectorRefs = []TerrainChunkCoordDef{{X: 99}} }, "nan-bounds": func(d *ImportedWorldDef) { d.Sectors[0].BoundsMin[0] = float32(math.NaN()) }, "inf-resolution": func(d *ImportedWorldDef) { d.VoxelResolution = float32(math.Inf(1)) }, "negative-lod-grid": func(d *ImportedWorldDef) { d.Sectors[0].LODs[0].ChunkSize = -1 }, "negative-count": func(d *ImportedWorldDef) { d.Entries[0].NonEmptyVoxelCount = -1 }} {
		t.Run(name, func(t *testing.T) {
			d := i05LegacyWorld()
			mutate(d)
			if got, e := NormalizeImportedWorldPages(d); e == nil || got != nil {
				t.Fatalf("accepted malformed source %+v %v", got, e)
			}
		})
	}
	for _, version := range []int{-1, 3, 99} {
		d := i05LegacyWorld()
		d.SchemaVersion = version
		b, _ := json.Marshal(d)
		p := filepath.Join(t.TempDir(), "future.gkworld")
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		if got, e := LoadImportedWorld(p); e == nil || got != nil || !strings.Contains(strings.ToLower(e.Error()), "version") {
			t.Fatalf("version reader %+v %v", got, e)
		}
	}
}

func TestI05LegacySynthesizedGridRejectsUnsafeArithmeticWithoutMutation(t *testing.T) {
	for _, v := range []struct {
		name    string
		coord   int
		spacing float32
	}{{"maximum-coordinate", int(^uint(0) >> 1), 1}, {"minimum-coordinate", -int(^uint(0)>>1) - 1, 1}, {"tiny-spacing", 0, math.SmallestNonzeroFloat32}} {
		t.Run(v.name, func(t *testing.T) {
			d := i05LegacyWorld()
			d.Sectors = nil
			d.Entries = d.Entries[:1]
			d.Entries[0].Coord.X = v.coord
			d.VoxelResolution = v.spacing
			before := i05Clone(t, d)
			if got, e := NormalizeImportedWorldPages(d); e == nil || got != nil {
				t.Fatalf("accepted unsafe synthesis %+v %v", got, e)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("failed synthesis mutated input")
			}
		})
	}
}

func TestI05LegacyOptionalMetadataCopiesAndLoadRejectsBrokenOwnership(t *testing.T) {
	d := i05LegacyWorld()
	d.Sectors[0].LODs[0].PayloadHash = "optional-legacy-identity"
	d.Sectors[0].LODs[0].PayloadSizeBytes = 123
	index, e := NormalizeImportedWorldPages(d)
	if e != nil {
		t.Fatal(e)
	}
	if index.Pages[0].Payload.PayloadHash != "optional-legacy-identity" || index.Pages[0].Payload.PayloadSizeBytes != 123 {
		t.Fatal("legacy metadata lost")
	}
	original := i05Clone(t, d)
	index.RootPageIndices[0] = 1
	index.Sectors[0].VisibleSectorIndices[0] = 0
	index.Sectors[0].AdjacentSectorIndices[0] = 0
	index.Sectors[0].FullChunkIndices[0] = 99
	index.SectorIndexByCoord[d.Sectors[0].Coord] = 99
	if !reflect.DeepEqual(d, original) {
		t.Fatal("runtime indexed refs alias authored input")
	}
	for name, mutate := range map[string]func(*ImportedWorldDef){"missing-adjacent": func(d *ImportedWorldDef) { d.Sectors[0].AdjacentSectorRefs = []TerrainChunkCoordDef{{X: 99}} }, "duplicate-adjacent": func(d *ImportedWorldDef) {
		d.Sectors[0].AdjacentSectorRefs = append(d.Sectors[0].AdjacentSectorRefs, d.Sectors[0].AdjacentSectorRefs[0])
	}, "shared-leaf": func(d *ImportedWorldDef) {
		d.Sectors[1].FullChunkRefs = append(d.Sectors[1].FullChunkRefs, d.Entries[0].Coord)
	}} {
		t.Run(name, func(t *testing.T) {
			d := i05LegacyWorld()
			mutate(d)
			if _, e := NormalizeImportedWorldPages(d); e == nil {
				t.Fatal("normalizer accepted malformed refs")
			}
			b, e := json.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(t.TempDir(), "bad.gkworld")
			if e = os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			if got, e := LoadImportedWorld(p); e == nil || got != nil {
				t.Fatalf("reader accepted malformed ownership %+v %v", got, e)
			}
		})
	}
}
