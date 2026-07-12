package content

import (
	"reflect"
	"testing"
)

func TestNavSpanClearanceAndProfileSupport(t *testing.T) {
	tests := []struct {
		name            string
		solid           [][3]int
		blocked         [][3]int
		wantSourceSpans int
		wantClearance   float32
		small           NavAgentProfileDef
		large           NavAgentProfileDef
		wantSmall       []uint32
		wantLarge       []uint32
		wantLargeReason string
	}{
		{
			name:  "open span",
			solid: [][3]int{{2, 0, 2}}, wantSourceSpans: 1, wantClearance: 2.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 2, Height: 3},
			wantSmall: []uint32{0}, wantLarge: []uint32{0},
		},
		{
			name:  "solid wall",
			solid: [][3]int{{2, 0, 2}, {3, 1, 2}, {3, 2, 2}, {3, 3, 2}, {3, 4, 2}}, wantSourceSpans: 1, wantClearance: 0.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.6, Height: 1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedClearance,
		},
		{
			name:  "metadata blocker subtracts only",
			solid: [][3]int{{2, 0, 2}}, blocked: [][3]int{{3, 1, 2}}, wantSourceSpans: 1, wantClearance: 0.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 1},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.6, Height: 1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedClearance,
		},
		{
			name:  "low ceiling",
			solid: [][3]int{{2, 0, 2}, {2, 3, 2}}, wantSourceSpans: 2, wantClearance: 2.5,
			small:     NavAgentProfileDef{ID: "small", Radius: 0.5, Height: 2},
			large:     NavAgentProfileDef{ID: "large", Radius: 0.5, Height: 2.1},
			wantSmall: []uint32{0}, wantLargeReason: NavSpanRejectedHeadroom,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			built, err := BuildNavSourceSpans(NavSpanBuildInput{
				NavID: "test", BuilderVersion: "test", SourceHash: "source",
				ChunkSize: 5, VoxelResolution: 1,
				Center: NavSpanBuildChunk{Known: true, SolidVoxels: test.solid, BlockedVoxels: test.blocked},
			})
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			if len(built.Source.Spans) != test.wantSourceSpans {
				t.Fatalf("source spans: want %d, got %+v", test.wantSourceSpans, built.Source.Spans)
			}
			if got := built.Source.Spans[0].ClearanceRadius; got != test.wantClearance {
				t.Fatalf("clearance: want %v, got %v", test.wantClearance, got)
			}

			// Isolate target floor when fixture also emits a supported ceiling top.
			built.Source.Spans = built.Source.Spans[:1]
			small, err := FilterNavSpansForProfile(built.Source, test.small)
			if err != nil {
				t.Fatalf("small profile failed: %v", err)
			}
			large, err := FilterNavSpansForProfile(built.Source, test.large)
			if err != nil {
				t.Fatalf("large profile failed: %v", err)
			}
			if !reflect.DeepEqual(small.Graph.SpanIDs, test.wantSmall) {
				t.Fatalf("small spans: want %v, got %v", test.wantSmall, small.Graph.SpanIDs)
			}
			if !reflect.DeepEqual(large.Graph.SpanIDs, test.wantLarge) {
				t.Fatalf("large spans: want %v, got %v", test.wantLarge, large.Graph.SpanIDs)
			}
			if test.wantLargeReason != "" && !reflect.DeepEqual(large.Diagnostics, []NavSpanProfileDiagnostic{{Code: test.wantLargeReason, Span: 0}}) {
				t.Fatalf("large rejection: want %s, got %+v", test.wantLargeReason, large.Diagnostics)
			}
			for _, id := range large.Graph.SpanIDs {
				if !containsNavSpanID(small.Graph.SpanIDs, id) {
					t.Fatalf("larger profile gained span %d absent from smaller profile", id)
				}
			}
		})
	}
}

func TestProfileClearanceTreatsReachableRampAsSupport(t *testing.T) {
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 50}
	for _, fixture := range []struct {
		name                  string
		resolution            float32
		chunkSize, minX, maxX int
		minZ, maxZ, routeZ    int
	}{
		{name: "fine", resolution: 0.1, chunkSize: 32, minX: 5, maxX: 24, minZ: 5, maxZ: 26, routeZ: 16},
		{name: "coarse", resolution: 0.2, chunkSize: 16, minX: 3, maxX: 12, minZ: 3, maxZ: 12, routeZ: 8},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var solids [][3]int
			for x := fixture.minX; x <= fixture.maxX; x++ {
				for z := fixture.minZ; z <= fixture.maxZ; z++ {
					for y := 0; y <= (x-fixture.minX)/2; y++ {
						solids = append(solids, [3]int{x, y, z})
					}
				}
			}
			built, err := BuildNavSourceSpans(NavSpanBuildInput{
				NavID: "ramp", BuilderVersion: "test", SourceHash: "source",
				ChunkSize: fixture.chunkSize, VoxelResolution: fixture.resolution,
				Center: NavSpanBuildChunk{Known: true, SolidVoxels: solids},
			})
			if err != nil {
				t.Fatalf("build source: %v", err)
			}
			spanAt := func(x int) NavSpanDef {
				t.Helper()
				for _, span := range built.Source.Spans {
					if span.X == x && span.Z == fixture.routeZ {
						return span
					}
				}
				t.Fatalf("missing ramp span at %d,%d", x, fixture.routeZ)
				return NavSpanDef{}
			}
			oldRejected := spanAt(fixture.minX + 5)
			if oldRejected.ClearanceRadius >= profile.Radius {
				t.Fatalf("fixture no longer reproduces raw-clearance bug: %+v", oldRejected)
			}

			graph, err := BuildNavSpanGraphWithContext(built.Source, []NavSourceTileDef{built.Source}, profile, fixture.resolution)
			if err != nil {
				t.Fatalf("build profile graph: %v", err)
			}
			start, goal := spanAt(fixture.minX+2), spanAt(fixture.maxX-2)
			if !containsNavSpanID(graph.Graph.SpanIDs, oldRejected.ID) || !containsNavSpanID(graph.Graph.SpanIDs, start.ID) || !containsNavSpanID(graph.Graph.SpanIDs, goal.ID) {
				t.Fatalf("reachable ramp spans rejected: old=%d start=%d goal=%d diagnostics=%+v", oldRejected.ID, start.ID, goal.ID, graph.SpanDiagnostics)
			}
			route, err := FindNavSpanRoute(built.Source, graph.Graph, fixture.resolution, start.ID, goal.ID)
			if err != nil || !route.Found {
				t.Fatalf("ramp route failed: route=%+v err=%v", route, err)
			}
		})
	}

	t.Run("tall wall still subtracts clearance", func(t *testing.T) {
		const chunkSize = 24
		var solids [][3]int
		for x := range chunkSize {
			for z := range chunkSize {
				solids = append(solids, [3]int{x, 0, z})
			}
		}
		for z := range chunkSize {
			for y := 1; y < 20; y++ {
				solids = append(solids, [3]int{12, y, z})
			}
		}
		built, err := BuildNavSourceSpans(NavSpanBuildInput{
			NavID: "wall", BuilderVersion: "test", SourceHash: "source", ChunkSize: chunkSize, VoxelResolution: 0.1,
			Center: NavSpanBuildChunk{Known: true, SolidVoxels: solids},
		})
		if err != nil {
			t.Fatalf("build source: %v", err)
		}
		graph, err := BuildNavSpanGraphWithContext(built.Source, []NavSourceTileDef{built.Source}, profile, 0.1)
		if err != nil {
			t.Fatalf("build profile graph: %v", err)
		}
		spanID := func(x int) uint32 {
			t.Helper()
			for _, span := range built.Source.Spans {
				if span.X == x && span.Z == 12 && span.Y == 1 {
					return span.ID
				}
			}
			t.Fatalf("missing floor span at x=%d", x)
			return 0
		}
		if containsNavSpanID(graph.Graph.SpanIDs, spanID(8)) {
			t.Fatal("span whose cylinder overlaps tall wall was accepted")
		}
		if !containsNavSpanID(graph.Graph.SpanIDs, spanID(7)) {
			t.Fatal("span outside wall radius was rejected")
		}
	})
}

func TestNavSpanSquaredIntervalDistanceField(t *testing.T) {
	const width, height = 4, 3
	for mask := 1; mask < 1<<(width*height); mask++ {
		field := make([]int64, width*height)
		for i := range field {
			field[i] = navSpanDistanceInfinity
			if mask&(1<<i) != 0 {
				field[i] = 0
			}
		}
		line := make([]int64, width)
		transformed := make([]int64, width)
		hull := make([]navSpanDistanceLine, width)
		navSpanSquaredIntervalDistanceField(field, width, height, line, transformed, hull)
		for z := range height {
			for x := range width {
				want := navSpanDistanceInfinity
				for obstacle := range width * height {
					if mask&(1<<obstacle) == 0 {
						continue
					}
					dx := absNavSpanInt(x - obstacle%width)
					dz := absNavSpanInt(z - obstacle/width)
					xDistance := max(2*dx-1, 0)
					zDistance := max(2*dz-1, 0)
					want = min(want, int64(xDistance*xDistance+zDistance*zDistance))
				}
				if got := field[x+width*z]; got != want {
					t.Fatalf("mask %x cell %d,%d: want %d, got %d", mask, x, z, want, got)
				}
			}
		}
	}
}

func containsNavSpanID(ids []uint32, want uint32) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
