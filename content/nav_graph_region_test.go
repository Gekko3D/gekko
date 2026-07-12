package content

import "testing"

func TestCompressNavGraphRegions(t *testing.T) {
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 20}
	build := func(spans []NavSpanDef) NavGraphTileDef {
		t.Helper()
		source := NavSourceTileDef{
			NavID: "test", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "test",
			SourceHash: "source", DependencyHash: "dependencies", ChunkSize: 4, Spans: spans,
		}
		built, err := BuildNavSpanGraph(source, profile, 1)
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		assertNavRegionReachability(t, built.Graph)
		return built.Graph
	}
	span := func(id uint32, x, z int, height float32, area string) NavSpanDef {
		return NavSpanDef{ID: id, X: x, Y: int(height * 10), Z: z, SupportHeight: height, CeilingHeight: 3, Headroom: 3 - height, ClearanceRadius: 1, Area: area}
	}

	t.Run("flat field compresses", func(t *testing.T) {
		var spans []NavSpanDef
		for x := 0; x < 4; x++ {
			for z := 0; z < 4; z++ {
				spans = append(spans, span(uint32(len(spans)), x, z, 0, "ground"))
			}
		}
		graph := build(spans)
		if len(graph.Regions) != 1 || len(graph.Transitions) != 0 || len(graph.Regions[0].SpanRuns) != 1 || graph.Regions[0].SpanRuns[0].Count != 16 {
			t.Fatalf("flat field did not compress: %+v", graph)
		}
	})

	t.Run("area boundary becomes contiguous runs", func(t *testing.T) {
		graph := build([]NavSpanDef{
			span(0, 0, 0, 0, "ground"), span(1, 0, 1, 0, "ground"),
			span(2, 1, 0, 0, "mud"), span(3, 1, 1, 0, "mud"),
		})
		if len(graph.Regions) != 2 || graph.Regions[0].Area != "ground" || graph.Regions[1].Area != "mud" {
			t.Fatalf("area regions mismatch: %+v", graph.Regions)
		}
		if len(graph.Transitions) != 2 {
			t.Fatalf("want two directed runs, got %+v", graph.Transitions)
		}
		for _, transition := range graph.Transitions {
			if transition.Width != 2 || transition.CrossingStart[0] != 1 || transition.CrossingStart[2] != 0 || transition.CrossingEnd[2] != 2 {
				t.Fatalf("transition was not merged across boundary: %+v", transition)
			}
		}
	})

	t.Run("traversal class splits regions", func(t *testing.T) {
		graph := build([]NavSpanDef{
			span(0, 0, 0, 0, "ground"),
			span(1, 1, 0, 0, "ground"),
			span(2, 2, 0, 0.5, "ground"),
		})
		if len(graph.Regions) != 2 || len(graph.Transitions) != 2 {
			t.Fatalf("walk/step boundary mismatch: regions=%+v transitions=%+v", graph.Regions, graph.Transitions)
		}
		for _, transition := range graph.Transitions {
			if transition.Kind != NavTransitionStep {
				t.Fatalf("want step region transition, got %+v", transition)
			}
		}
	})

	t.Run("one way edge stays explicit", func(t *testing.T) {
		spans := []NavSpanDef{span(0, 0, 0, 0, "ground"), span(1, 1, 0, 0, "ground")}
		graph := build(spans)
		graph.SpanTransitions = graph.SpanTransitions[:1]
		source := NavSourceTileDef{
			NavID: "test", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "test",
			SourceHash: "source", DependencyHash: "dependencies", ChunkSize: 4, Spans: spans,
		}
		graph, err := CompressNavGraphRegions(source, graph, 1)
		if err != nil {
			t.Fatalf("compress failed: %v", err)
		}
		assertNavRegionReachability(t, graph)
		if len(graph.Regions) != 2 || len(graph.Transitions) != 1 || graph.Transitions[0].FromRegion != 0 || graph.Transitions[0].ToRegion != 1 {
			t.Fatalf("one-way transition mismatch: %+v", graph)
		}
	})
}

func TestBuildNavRegionCenterStaysInsideBoundsForLargeRegion(t *testing.T) {
	const count = 100_000
	members := make([]uint32, count)
	spans := make(map[uint32]NavSpanDef, count)
	for i := range count {
		id := uint32(i)
		members[i] = id
		spans[id] = NavSpanDef{ID: id, X: 7, Z: 9, SupportHeight: -51.1}
	}
	region := buildNavRegion(0, members, spans, 0.1)
	validation := NavGraphValidationResult{}
	validateRegionBounds(&validation, region)
	if validation.HasErrors() {
		t.Fatalf("large region center drifted outside bounds: center=%v bounds=%v..%v", region.Center, region.BoundsMin, region.BoundsMax)
	}
}

func assertNavRegionReachability(t *testing.T, graph NavGraphTileDef) {
	t.Helper()
	spanRegion := make(map[uint32]uint32)
	for _, region := range graph.Regions {
		for _, run := range region.SpanRuns {
			for offset := uint32(0); offset < run.Count; offset++ {
				spanRegion[run.Start+offset] = region.ID
			}
		}
	}
	spanEdges := make(map[uint32][]uint32)
	for _, edge := range graph.SpanTransitions {
		if edge.To.Tile == graph.Coord {
			spanEdges[edge.From] = append(spanEdges[edge.From], edge.To.Span)
		}
	}
	regionEdges := make(map[uint32][]uint32)
	for _, edge := range graph.Transitions {
		if edge.ToTile == graph.Coord {
			regionEdges[edge.FromRegion] = append(regionEdges[edge.FromRegion], edge.ToRegion)
		}
	}
	for _, start := range graph.SpanIDs {
		spanReachable := navRegionTestReachable(start, spanEdges)
		regionReachable := navRegionTestReachable(spanRegion[start], regionEdges)
		for _, goal := range graph.SpanIDs {
			if spanReachable[goal] != regionReachable[spanRegion[goal]] {
				t.Fatalf("reachability changed from span %d to %d", start, goal)
			}
		}
	}
}

func navRegionTestReachable[T comparable](start T, edges map[T][]T) map[T]bool {
	seen := map[T]bool{start: true}
	queue := []T{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range edges[current] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}
