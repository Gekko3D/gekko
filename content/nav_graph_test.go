package content

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNavGraphContracts(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 2, Z: -1}
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.45, MaxSlopeDegrees: 45}
	source := &NavSourceTileDef{
		NavID:           "demo",
		SchemaVersion:   CurrentNavSourceTileSchemaVersion,
		Coord:           coord,
		BuilderVersion:  "voxel_graph_v1",
		SourceHash:      "source",
		DependencyHash:  "dependencies",
		ChunkSize:       32,
		VoxelResolution: 0.1,
		Spans: []NavSpanDef{
			{ID: 0, X: 0, Y: 1, Z: 0, SupportHeight: 1, CeilingHeight: 4, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
			{ID: 1, X: 1, Y: 1, Z: 0, SupportHeight: 1, CeilingHeight: 4, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
		},
	}
	graph := &NavGraphTileDef{
		NavID:           "demo",
		SchemaVersion:   CurrentNavGraphTileSchemaVersion,
		Coord:           coord,
		AgentProfileID:  profile.ID,
		BuilderVersion:  "voxel_graph_v1",
		SourceHash:      source.SourceHash,
		DependencyHash:  source.DependencyHash,
		SpanIDs:         []uint32{0, 1},
		SpanTransitions: []NavSpanTransitionDef{{From: 0, To: NavSpanRef{Tile: coord, Span: 1}, Kind: NavTransitionWalk, Width: 1, MinHeadroom: 1.8, MinClearance: 0.4, Cost: 1}},
		Regions: []NavRegionDef{{
			ID: 0, SpanRuns: []NavSpanRunDef{{Start: 0, Count: 2}}, BoundsMin: Vec3{0, 1, 0}, BoundsMax: Vec3{2, 1, 1}, Center: Vec3{1, 1, 0.5}, HeightMin: 1, HeightMax: 1, Area: "ground",
		}},
	}
	manifest := &NavGraphManifestDef{
		NavID: "demo", SchemaVersion: CurrentNavGraphManifestSchemaVersion, SourceWorldID: "world", BuilderVersion: "voxel_graph_v1", ChunkSize: 32, VoxelResolution: 0.1,
		AgentProfiles: []NavAgentProfileDef{profile},
		SourceTiles:   []NavSourceTileEntryDef{{Coord: coord, TilePath: "tiles/2_-1" + NavSourceTileExtension, SourceHash: source.SourceHash, DependencyHash: source.DependencyHash, ContentHash: strings.Repeat("0", 64), ByteSize: 1}},
		GraphTiles:    []NavGraphTileEntryDef{{Coord: coord, AgentProfileID: profile.ID, TilePath: "tiles/2_-1_walker" + NavGraphTileExtension, SourceHash: source.SourceHash, DependencyHash: source.DependencyHash, ContentHash: strings.Repeat("0", 64), ByteSize: 1}},
	}

	tests := []struct {
		name string
		path string
		save func(string) error
		load func(string) (any, error)
		want any
	}{
		{"manifest", filepath.Join(t.TempDir(), "demo"+NavGraphManifestExtension), func(path string) error { return SaveNavGraphManifest(path, manifest) }, func(path string) (any, error) { return LoadNavGraphManifest(path) }, manifest},
		{"source", filepath.Join(t.TempDir(), "tile"+NavSourceTileExtension), func(path string) error { return SaveNavSourceTile(path, source) }, func(path string) (any, error) { return LoadNavSourceTile(path) }, source},
		{"graph", filepath.Join(t.TempDir(), "tile"+NavGraphTileExtension), func(path string) error { return SaveNavGraphTile(path, graph) }, func(path string) (any, error) { return LoadNavGraphTile(path) }, graph},
	}
	for _, test := range tests {
		t.Run(test.name+" round trip", func(t *testing.T) {
			if err := test.save(test.path); err != nil {
				t.Fatalf("save failed: %v", err)
			}
			first, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.save(test.path); err != nil {
				t.Fatalf("repeated save failed: %v", err)
			}
			second, err := os.ReadFile(test.path)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatalf("save is not deterministic: %v", err)
			}
			got, err := test.load(test.path)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			if !reflect.DeepEqual(test.want, got) {
				t.Fatalf("round trip mismatch: want=%+v got=%+v", test.want, got)
			}
		})
	}

	t.Run("validation", func(t *testing.T) {
		badSource := *source
		badSource.Spans = append([]NavSpanDef(nil), source.Spans...)
		badSource.Spans[1].ID = 4
		badSource.Spans[1].X = -1
		badSource.Spans[1].CeilingHeight = float32(math.Inf(1))
		if got := ValidateNavSourceTile(&badSource); got.HardErrorCount < 3 {
			t.Fatalf("expected id, order, and numeric errors, got %+v", got)
		}

		badGraph := *graph
		badGraph.SpanTransitions = append([]NavSpanTransitionDef(nil), graph.SpanTransitions...)
		badGraph.SpanTransitions[0].To.Span = 9
		badGraph.Regions = append([]NavRegionDef(nil), graph.Regions...)
		badGraph.Regions[0].BoundsMax[0] = -1
		if got := ValidateNavGraphTile(&badGraph); got.HardErrorCount < 2 {
			t.Fatalf("expected reference and bounds errors, got %+v", got)
		}

		badManifest := *manifest
		badManifest.GraphTiles = append([]NavGraphTileEntryDef(nil), manifest.GraphTiles...)
		badManifest.GraphTiles[0].AgentProfileID = "missing"
		badManifest.GraphTiles[0].TilePath = "legacy.gknavtile"
		if got := ValidateNavGraphManifest(&badManifest); got.HardErrorCount < 2 {
			t.Fatalf("expected profile and extension errors, got %+v", got)
		}
	})
}

func TestNavBinaryContractRejectsCorruptionAndWrongVersion(t *testing.T) {
	source := &NavSourceTileDef{
		NavID: "binary", SchemaVersion: CurrentNavSourceTileSchemaVersion,
		BuilderVersion: "test", SourceHash: "source", DependencyHash: "dependency",
		ChunkSize: 4, VoxelResolution: 1,
		Spans: []NavSpanDef{{ID: 0, Y: 1, SupportHeight: 1, CeilingHeight: 4, Headroom: 3, Area: "ground"}},
	}
	first, err := encodeNavSourceTile(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encodeNavSourceTile(source)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("navigation binary output is not deterministic: %v", err)
	}

	for name, mutate := range map[string]func([]byte) []byte{
		"binary version": func(data []byte) []byte { data[8]++; return data },
		"schema version": func(data []byte) []byte { data[14]++; return data },
		"truncated gzip": func(data []byte) []byte { return data[:len(data)-1] },
		"gzip checksum":  func(data []byte) []byte { data[len(data)-1] ^= 0xff; return data },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeNavSourceTile(mutate(append([]byte(nil), first...))); err == nil {
				t.Fatal("corrupt navigation binary was accepted")
			}
		})
	}
}

func TestNavTraversalLandingSupportBinaryRoundTrip(t *testing.T) {
	want := &NavTraversalDef{
		LinkID: "drop:lift", OwnerID: "lift", Start: Vec3{1, 4, 2}, End: Vec3{3, 0, 2},
		LandingSupport: &NavLandingSupportDef{ID: "lift", Stop: "closed", Point: Vec3{2, 0, 2}},
	}
	strings := makeNavStrings(map[string]struct{}{"drop:lift": {}, "lift": {}, "closed": {}})
	w := &navBinaryWriter{}
	writeNavTraversal(w, strings, want)
	r := newNavBinaryReader(w.Bytes())
	got := readNavTraversal(r, strings.values)
	if err := r.done(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("landing support round trip mismatch: got=%+v want=%+v", got, want)
	}
}
