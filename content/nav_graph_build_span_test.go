package content

import (
	"reflect"
	"testing"
)

func TestBuildNavSourceSpans(t *testing.T) {
	coord := TerrainChunkCoordDef{X: 3, Y: -1, Z: 4}
	build := func(size int, center [][3]int, halo ...NavSpanBuildChunk) NavSpanBuildResult {
		t.Helper()
		result, err := BuildNavSourceSpans(NavSpanBuildInput{
			NavID: "test", BuilderVersion: "test", SourceHash: "source",
			ChunkSize: size, VoxelResolution: 0.5,
			Center: NavSpanBuildChunk{Coord: coord, Known: true, SolidVoxels: center},
			Halo:   halo,
		})
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		if validation := ValidateNavSourceTile(&result.Source); validation.HasErrors() {
			t.Fatalf("invalid source tile: %+v", validation)
		}
		return result
	}
	span := func(id uint32, x, y, z int, support, ceiling float32) NavSpanDef {
		return NavSpanDef{ID: id, X: x, Y: y, Z: z, SupportHeight: support, CeilingHeight: ceiling, Headroom: ceiling - support, ClearanceRadius: 0.25}
	}

	tests := []struct {
		name            string
		size            int
		center          [][3]int
		halo            []NavSpanBuildChunk
		wantSpans       []NavSpanDef
		wantDiagnostics []NavSpanBuildDiagnostic
	}{
		{
			name:   "flat ground with hole",
			size:   3,
			center: [][3]int{{0, 0, 0}, {1, 0, 0}, {0, 0, 1}},
			wantSpans: []NavSpanDef{
				span(0, 0, 1, 0, -1, 0),
				span(1, 0, 1, 1, -1, 0),
				span(2, 1, 1, 0, -1, 0),
			},
			wantDiagnostics: []NavSpanBuildDiagnostic{
				{Code: NavSpanBuildTruncatedOpenInterval, X: 0, Y: 1, Z: 0},
				{Code: NavSpanBuildTruncatedOpenInterval, X: 0, Y: 1, Z: 1},
				{Code: NavSpanBuildTruncatedOpenInterval, X: 1, Y: 1, Z: 0},
			},
		},
		{
			name:   "stacked floors",
			size:   4,
			center: [][3]int{{0, 0, 0}, {0, 2, 0}},
			wantSpans: []NavSpanDef{
				span(0, 0, 1, 0, -1.5, -1),
				span(1, 0, 3, 0, -0.5, 0),
			},
			wantDiagnostics: []NavSpanBuildDiagnostic{{Code: NavSpanBuildTruncatedOpenInterval, X: 0, Y: 3, Z: 0}},
		},
		{
			name:   "known upper halo extends headroom",
			size:   2,
			center: [][3]int{{0, 1, 0}},
			halo: []NavSpanBuildChunk{{
				Coord: TerrainChunkCoordDef{X: coord.X, Y: coord.Y + 1, Z: coord.Z}, Known: true, SourceHash: "upper",
			}},
			wantSpans:       []NavSpanDef{span(0, 0, 2, 0, 0, 1)},
			wantDiagnostics: []NavSpanBuildDiagnostic{{Code: NavSpanBuildTruncatedOpenInterval, X: 0, Y: 2, Z: 0}},
		},
		{
			name:   "unknown upper halo rejects unsupported open interval",
			size:   2,
			center: [][3]int{{0, 1, 0}},
			halo: []NavSpanBuildChunk{{
				Coord: TerrainChunkCoordDef{X: coord.X, Y: coord.Y + 1, Z: coord.Z}, Known: false,
			}},
			wantDiagnostics: []NavSpanBuildDiagnostic{{Code: NavSpanBuildUnknownOpenInterval, Rejected: true, X: 0, Y: 2, Z: 0}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := build(test.size, test.center, test.halo...)
			if !reflect.DeepEqual(got.Source.Spans, test.wantSpans) {
				t.Fatalf("spans mismatch:\nwant: %+v\ngot:  %+v", test.wantSpans, got.Source.Spans)
			}
			if !reflect.DeepEqual(got.Diagnostics, test.wantDiagnostics) {
				t.Fatalf("diagnostics mismatch:\nwant: %+v\ngot:  %+v", test.wantDiagnostics, got.Diagnostics)
			}
		})
	}

	a := build(2, [][3]int{{0, 0, 0}}, NavSpanBuildChunk{Coord: TerrainChunkCoordDef{X: coord.X + 1, Y: coord.Y, Z: coord.Z}, Known: true, SourceHash: "east-a"})
	b := build(2, [][3]int{{0, 0, 0}}, NavSpanBuildChunk{Coord: TerrainChunkCoordDef{X: coord.X + 1, Y: coord.Y, Z: coord.Z}, Known: true, SourceHash: "east-b"})
	if a.Source.DependencyHash == b.Source.DependencyHash {
		t.Fatal("halo source change did not affect dependency hash")
	}
}
