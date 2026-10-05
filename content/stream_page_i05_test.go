package content

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func i05Clone[T any](t *testing.T, v T) T {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out T
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func i05Bounds(lo, hi float32) StreamPageBounds {
	return StreamPageBounds{Min: [3]float32{lo, lo, lo}, Max: [3]float32{hi, hi, hi}}
}
func i05Page(level uint8, lo, hi float32) StreamPageDef {
	return StreamPageDef{Level: level, BoundsMin: [3]float32{lo, lo, lo}, BoundsMax: [3]float32{hi, hi, hi}, Payload: StreamPagePayloadDef{Kind: ImportedWorldChunkPayloadSparseJSONV1, Path: "payload.gkchunk", WorldOrigin: [3]float32{lo, lo, lo}, ChunkSize: 1, VoxelResolution: 1, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 16}, Tags: []string{"page"}}
}
func i05ForestFixture() ([]StreamPageDef, []uint32, []StreamPageLeaf, []StreamPageBounds) {
	pages := []StreamPageDef{i05Page(StreamPageLevelLeaf, 0, 4), i05Page(StreamPageLevelRegional, 4, 8), i05Page(StreamPageLevelRoot, 0, 16)}
	pages[0].LeafEntryIndices = []uint32{0}
	pages[1].LeafEntryIndices = []uint32{1}
	pages[2].ChildPageIndices = []uint32{0, 1}
	leaves := []StreamPageLeaf{{Bounds: i05Bounds(0, 2), NonEmpty: true}, {Bounds: i05Bounds(4, 6), NonEmpty: true}, {Bounds: i05Bounds(8, 9)}}
	return pages, []uint32{2}, leaves, []StreamPageBounds{i05Bounds(0, 3), i05Bounds(4, 7), i05Bounds(0, 16)}
}

func TestI05StreamPageForestIndexesAndQualifiedHeightCoverage(t *testing.T) {
	if StreamPageLevelLeaf != 0 || StreamPageLevelRegional != 1 || StreamPageLevelMacro != 2 || StreamPageLevelRoot != 3 {
		t.Fatal("documented levels")
	}
	pages, roots, leaves, payload := i05ForestFixture()
	before := i05Clone(t, pages)
	index, e := ValidateStreamPageForest(pages, roots, leaves, payload)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(index.ParentPageIndices, []int{2, 2, -1}) || !reflect.DeepEqual(index.LeafOwnerPageIndices, []int{0, 1, -1}) || !reflect.DeepEqual(pages, before) {
		t.Fatalf("indexes/source: %+v", index)
	}
	empty, e := ValidateStreamPageForest(nil, nil, nil, nil)
	if e != nil || len(empty.ParentPageIndices) != 0 || len(empty.LeafOwnerPageIndices) != 0 {
		t.Fatalf("empty forest %v", e)
	}
	background := i05Page(StreamPageLevelRegional, 0, 4)
	if _, e := ValidateStreamPageForest([]StreamPageDef{background}, []uint32{0}, nil, []StreamPageBounds{i05Bounds(0, 4)}); e != nil {
		t.Fatalf("payload-only background: %v", e)
	}
	if got, e := ValidateStreamPageForest(nil, nil, []StreamPageLeaf{{Bounds: i05Bounds(0, 1)}}, nil); e != nil || !reflect.DeepEqual(got.LeafOwnerPageIndices, []int{-1}) {
		t.Fatalf("unowned empty leaves: %v", e)
	}
	// Qualified height coverage has its own vertical range; sample spacing must not imply a cubic Y extent.
	pages[0].Payload.Kind = TerrainHeightTilePayloadKind
	pages[0].Payload.VoxelResolution = 0
	pages[0].Payload.SampleSpacing = 2
	pages[0].Payload.ChunkSize = 128
	if _, e := ValidateStreamPageForest(pages, roots, leaves, payload); e != nil {
		t.Fatalf("qualified height payload: %v", e)
	}
	payload[0].Min[1] = 1
	payload[0].Max[1] = 1
	leaves[0].Bounds.Min[1] = 1
	leaves[0].Bounds.Max[1] = 1
	if _, e := ValidateStreamPageForest(pages, roots, leaves, payload); e != nil {
		t.Fatalf("constant-height plane coverage: %v", e)
	}
}

func TestI05StreamPageForestRejectsMalformedOwnershipAndBounds(t *testing.T) {
	cases := map[string]func(*[]StreamPageDef, *[]uint32, *[]StreamPageLeaf, *[]StreamPageBounds){
		"duplicate-root": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { *r = append(*r, 2) },
		"root-oob":       func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { (*r)[0] = 3 },
		"unreachable": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].ChildPageIndices = []uint32{0}
		},
		"cycle": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[0].LeafEntryIndices = nil
			(*p)[0].ChildPageIndices = []uint32{2}
		},
		"shared-child": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[1].LeafEntryIndices = nil
			(*p)[1].ChildPageIndices = []uint32{0}
		},
		"both-reference-kinds": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].LeafEntryIndices = []uint32{0}
		},

		"child-oob": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].ChildPageIndices = []uint32{0, 99}
		},
		"duplicate-child": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].ChildPageIndices = []uint32{0, 0, 1}
		},
		"leaf-oob": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[0].LeafEntryIndices = []uint32{99}
		},
		"duplicate-leaf": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[0].LeafEntryIndices = []uint32{0, 0}
		},
		"shared-leaf": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[1].LeafEntryIndices = []uint32{0, 1}
		},
		"unowned-nonempty": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*l)[2].NonEmpty = true
		},
		"not-coarser": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].Level = StreamPageLevelRegional
		},
		"unknown-level":          func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { (*p)[2].Level = 4 },
		"payload-bound-count":    func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { *b = (*b)[:2] },
		"inverted-payload-bound": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { (*b)[0].Min[1] = 4 },
		"inverted-leaf-bound": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*l)[0].Bounds.Min[1] = 3
		},
		"payload-outside": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) { (*b)[0].Max[0] = 5 },
		"leaf-outside": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*l)[0].Bounds.Min[0] = -1
		},
		"child-outside": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[2].BoundsMax[0] = 7
		},
		"zero-page-bound": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*p)[0].BoundsMax[1] = 0
		},
		"nan-leaf-bound": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*l)[0].Bounds.Min[2] = float32(math.NaN())
		},
		"inf-payload-bound": func(p *[]StreamPageDef, r *[]uint32, l *[]StreamPageLeaf, b *[]StreamPageBounds) {
			(*b)[0].Max[2] = float32(math.Inf(1))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p, r, l, b := i05ForestFixture()
			mutate(&p, &r, &l, &b)
			if got, e := ValidateStreamPageForest(p, r, l, b); e == nil || got != nil {
				t.Fatalf("accepted malformed forest %+v %v", got, e)
			}
		})
	}
	for name, mutate := range map[string]func(*StreamPagePayloadDef){"kind": func(p *StreamPagePayloadDef) { p.Kind = "unknown" }, "path": func(p *StreamPagePayloadDef) { p.Path = " " }, "hash": func(p *StreamPagePayloadDef) { p.PayloadHash = "bad" }, "zero-size": func(p *StreamPagePayloadDef) { p.PayloadSizeBytes = 0 }, "negative-sector-cost": func(p *StreamPagePayloadDef) { p.OccupiedSectorCount = -1 }, "negative-brick-cost": func(p *StreamPagePayloadDef) { p.OccupiedBrickCount = -1 }, "origin": func(p *StreamPagePayloadDef) { p.WorldOrigin[0] = float32(math.NaN()) }, "grid": func(p *StreamPagePayloadDef) { p.ChunkSize = 0 }, "spacing": func(p *StreamPagePayloadDef) { p.VoxelResolution = 0 }} {
		t.Run(name, func(t *testing.T) {
			p, r, l, b := i05ForestFixture()
			mutate(&p[0].Payload)
			if _, e := ValidateStreamPageForest(p, r, l, b); e == nil {
				t.Fatal("accepted invalid payload")
			}
		})
	}
}

func TestI05SharedPageAndIndexedSectorJSONContract(t *testing.T) {
	page := i05Page(StreamPageLevelMacro, -4, 12)
	page.ChildPageIndices = []uint32{7, 2}
	page.CoverageGroup = "poi:stable-id"
	page.Payload.SampleSpacing = 2
	page.Payload.OccupiedSectorCount = 3
	page.Payload.OccupiedBrickCount = 5
	encoded, e := json.Marshal(page)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(encoded, &fields); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"level", "bounds_min", "bounds_max", "payload", "child_page_indices", "coverage_group", "tags"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing page JSON field %s", key)
		}
	}
	var payload map[string]json.RawMessage
	if e = json.Unmarshal(fields["payload"], &payload); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"kind", "path", "world_origin", "chunk_size", "voxel_resolution", "sample_spacing", "payload_hash", "payload_size_bytes", "occupied_sector_count", "occupied_brick_count"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing payload JSON field %s", key)
		}
	}
	var decoded StreamPageDef
	if e = json.Unmarshal(encoded, &decoded); e != nil || !reflect.DeepEqual(decoded, page) {
		t.Fatalf("page roundtrip %v", e)
	}
	sector := ImportedWorldSectorV3Def{Coord: TerrainChunkCoordDef{X: -1}, BoundsMin: [3]float32{-4, 0, 0}, BoundsMax: [3]float32{0, 4, 4}, FullChunkIndices: []uint32{7, 2}, VisibilityID: "visible", SourceLeafIDs: []int{8, 4}, VisibleSectorIndices: []uint32{2, 0}, AdjacentSectorIndices: []uint32{1}, Tags: []string{"indoor"}}
	fields = make(map[string]json.RawMessage)
	encoded, e = json.Marshal(sector)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(encoded, &fields); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"coord", "bounds_min", "bounds_max", "full_chunk_indices", "visibility_id", "source_leaf_ids", "visible_sector_indices", "adjacent_sector_indices", "tags"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing indexed sector field %s", key)
		}
	}
	var out ImportedWorldSectorV3Def
	if e = json.Unmarshal(encoded, &out); e != nil || !reflect.DeepEqual(out, sector) {
		t.Fatalf("sector roundtrip %v", e)
	}
}

func TestI05IndependentRootsAndCoverageValidation(t *testing.T) {
	p, r, l, b := i05ForestFixture()
	p[2].ChildPageIndices = nil
	r = []uint32{0, 1, 2}
	got, e := ValidateStreamPageForest(p, r, l, b)
	if e != nil || !reflect.DeepEqual(got.ParentPageIndices, []int{-1, -1, -1}) {
		t.Fatalf("independent roots %+v %v", got, e)
	}
	p, r, l, b = i05ForestFixture()
	r = append(r, 0)
	if got, e := ValidateStreamPageForest(p, r, l, b); e == nil || got != nil {
		t.Fatal("root also has parent")
	}
	p, r, l, b = i05ForestFixture()
	l[2].Bounds.Max[0] = float32(math.Inf(1))
	if _, e := ValidateStreamPageForest(p, r, l, b); e == nil {
		t.Fatal("unowned empty coverage must still be finite")
	}
	for name, mutate := range map[string]func(*StreamPagePayloadDef){"oversized": func(p *StreamPagePayloadDef) { p.ChunkSize = 129 }, "zero-spacing": func(p *StreamPagePayloadDef) { p.SampleSpacing = 0 }, "nan-spacing": func(p *StreamPagePayloadDef) { p.SampleSpacing = float32(math.NaN()) }, "inf-spacing": func(p *StreamPagePayloadDef) { p.SampleSpacing = float32(math.Inf(1)) }} {
		t.Run(name, func(t *testing.T) {
			p, r, l, b := i05ForestFixture()
			p[0].Payload.Kind = TerrainHeightTilePayloadKind
			p[0].Payload.ChunkSize = 128
			p[0].Payload.SampleSpacing = 2
			mutate(&p[0].Payload)
			if _, e := ValidateStreamPageForest(p, r, l, b); e == nil {
				t.Fatal("invalid height grid")
			}
		})
	}
}
