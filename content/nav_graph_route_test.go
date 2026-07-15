package content

import "testing"

func TestFindNavGraphRoute(t *testing.T) {
	const chunkSize = 3
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.5, Height: 1.5, StepHeight: 0.6, MaxSlopeDegrees: 30}
	buildWorld := func(tileCount int) ([]NavSourceTileDef, []NavGraphTileDef) {
		t.Helper()
		sources := make([]NavSourceTileDef, tileCount)
		for tile := range sources {
			for x := 0; x < chunkSize; x++ {
				sources[tile].Spans = append(sources[tile].Spans, NavSpanDef{
					ID: uint32(x), X: x, Z: 0, SupportHeight: 0, CeilingHeight: 3,
					Headroom: 3, ClearanceRadius: 1, Area: "ground",
				})
			}
			sources[tile].NavID = "test"
			sources[tile].SchemaVersion = CurrentNavSourceTileSchemaVersion
			sources[tile].ChunkSize = chunkSize
			sources[tile].Coord = TerrainChunkCoordDef{X: tile}
			sources[tile].BuilderVersion = "test"
			sources[tile].SourceHash = TerrainChunkKey(sources[tile].Coord)
		}
		hashes := make(map[TerrainChunkCoordDef]string, len(sources))
		for _, source := range sources {
			hashes[source.Coord] = source.SourceHash
		}
		for i := range sources {
			sources[i].DependencyHash = navGraphDependencyHash(sources[i].Coord, func(coord TerrainChunkCoordDef) (string, bool) {
				hash, ok := hashes[coord]
				return hash, ok
			})
		}
		graphs := make([]NavGraphTileDef, len(sources))
		for i, source := range sources {
			built, err := BuildNavSpanGraph(source, profile, 1)
			if err != nil {
				t.Fatalf("build failed: %v", err)
			}
			graphs[i] = built.Graph
		}
		connectedSources, connectedGraphs, err := ConnectNavGraphTiles(sources, graphs, profile, chunkSize, 1)
		if err != nil {
			t.Fatalf("connect failed: %v", err)
		}
		return connectedSources, connectedGraphs
	}

	t.Run("long route stays hierarchical", func(t *testing.T) {
		sources, graphs := buildWorld(4)
		route, err := FindNavGraphRoute(sources, graphs, chunkSize, 1, Vec3{0.1, 0.2, 0.1}, Vec3{11.9, 0.2, 0.1})
		if err != nil {
			t.Fatalf("route failed: %v", err)
		}
		if !route.Found || len(route.Steps) != 4 || route.FailureReason != "" {
			t.Fatalf("hierarchical route mismatch: %+v", route)
		}
		for i, step := range route.Steps {
			if step.Tile.X != i || step.Region != 0 || i > 0 && step.RequiredAction != NavTransitionWalk {
				t.Fatalf("step %d mismatch: %+v", i, step)
			}
		}
		if got := route.Waypoints[len(route.Waypoints)-1]; got != (Vec3{11.9, 0, 0.1}) {
			t.Fatalf("goal was not projected to support: %v", got)
		}
		if len(route.Waypoints) >= 4*chunkSize {
			t.Fatalf("long route expanded every span: %d waypoints", len(route.Waypoints))
		}
	})

	t.Run("stacked floor resolution", func(t *testing.T) {
		source := NavSourceTileDef{
			NavID: "stacked", SchemaVersion: CurrentNavSourceTileSchemaVersion,
			BuilderVersion: "test", SourceHash: "source", DependencyHash: "dependencies",
			ChunkSize: chunkSize,
			Spans: []NavSpanDef{
				{ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1},
				{ID: 1, X: 0, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1},
				{ID: 2, X: 1, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1},
				{ID: 3, X: 1, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1},
			},
		}
		built, err := BuildNavSpanGraph(source, profile, 1)
		if err != nil {
			t.Fatalf("build failed: %v", err)
		}
		route, err := FindNavGraphRoute([]NavSourceTileDef{source}, []NavGraphTileDef{built.Graph}, chunkSize, 1, Vec3{0.1, 3.2, 0.1}, Vec3{1.9, 3.2, 0.1})
		if err != nil || !route.Found || route.Steps[0].Region != 1 {
			t.Fatalf("upper floor route mismatch: route=%+v err=%v", route, err)
		}
		if got := route.Waypoints[len(route.Waypoints)-1][1]; got != 3 {
			t.Fatalf("goal resolved to wrong floor: %v", got)
		}
	})

	t.Run("intermediate region follows its spans", func(t *testing.T) {
		areas := []string{"start", "middle", "middle", "middle", "middle", "middle", "goal"}
		cells := [][2]int{{0, 0}, {1, 0}, {1, 1}, {2, 1}, {3, 0}, {3, 1}, {4, 0}}
		source := NavSourceTileDef{
			NavID: "turn", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "test",
			ChunkSize: 5, SourceHash: "source", DependencyHash: "dependency",
		}
		for i, cell := range cells {
			source.Spans = append(source.Spans, NavSpanDef{
				ID: uint32(i), X: cell[0], Z: cell[1], SupportHeight: 0, CeilingHeight: 3,
				Headroom: 3, ClearanceRadius: 1, Area: areas[i],
			})
		}
		built, err := BuildNavSpanGraph(source, profile, 1)
		if err != nil {
			t.Fatal(err)
		}
		route, err := FindNavGraphRoute([]NavSourceTileDef{source}, []NavGraphTileDef{built.Graph}, 5, 1, Vec3{0.1, 0.2, 0.1}, Vec3{4.9, 0.2, 0.1})
		if err != nil || !route.Found {
			t.Fatalf("route failed: route=%+v err=%v", route, err)
		}
		for _, waypoint := range route.Waypoints {
			if waypoint[2] == 1.5 {
				return
			}
		}
		t.Fatalf("route cut across unsupported middle region: %v", route.Waypoints)
	})

	t.Run("clear diagonal stays direct", func(t *testing.T) {
		source := NavSourceTileDef{
			NavID: "open", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: "test",
			ChunkSize: 5, SourceHash: "source", DependencyHash: "dependency",
		}
		for x := 0; x < source.ChunkSize; x++ {
			for z := 0; z < source.ChunkSize; z++ {
				source.Spans = append(source.Spans, NavSpanDef{
					ID: uint32(len(source.Spans)), X: x, Z: z, SupportHeight: 0, CeilingHeight: 3,
					Headroom: 3, ClearanceRadius: 1, Area: "ground",
				})
			}
		}
		built, err := BuildNavSpanGraph(source, profile, 1)
		if err != nil {
			t.Fatal(err)
		}
		route, err := FindNavGraphRoute([]NavSourceTileDef{source}, []NavGraphTileDef{built.Graph}, 5, 1, Vec3{0.1, 0.2, 0.1}, Vec3{4.9, 0.2, 4.9})
		if err != nil || !route.Found {
			t.Fatalf("route failed: route=%+v err=%v", route, err)
		}
		if len(route.Waypoints) != 2 || route.Waypoints[0] != (Vec3{4.5, 0, 4.5}) {
			t.Fatalf("clear diagonal was not simplified: %v", route.Waypoints)
		}
	})

	t.Run("explicit failures", func(t *testing.T) {
		sources, graphs := buildWorld(1)
		route, err := FindNavGraphRoute(sources, graphs, chunkSize, 1, Vec3{-0.1, 0, 0}, Vec3{1, 0, 0})
		if err != nil || route.Found || route.FailureReason != NavRouteStartUnsupported || route.FailureTile.X != -1 {
			t.Fatalf("unsupported start mismatch: route=%+v err=%v", route, err)
		}
		route, err = FindNavGraphRoute(sources, graphs, chunkSize, 1, Vec3{1, 0, 0}, Vec3{3.1, 0, 0})
		if err != nil || route.Found || route.FailureReason != NavRouteGoalUnsupported || route.FailureTile.X != 1 {
			t.Fatalf("unsupported goal mismatch: route=%+v err=%v", route, err)
		}

		disconnectedSources, disconnectedGraphs := buildWorld(2)
		disconnectedGraphs[0].SpanTransitions = keepLocalNavSpanTransitions(disconnectedGraphs[0].Coord, disconnectedGraphs[0].SpanTransitions)
		disconnectedGraphs[0].Transitions = keepLocalNavRegionTransitions(disconnectedGraphs[0].Coord, disconnectedGraphs[0].Transitions)
		route, err = FindNavGraphRoute(disconnectedSources, disconnectedGraphs, chunkSize, 1, Vec3{0.1, 0, 0}, Vec3{3.1, 0, 0})
		if err != nil || route.Found || route.FailureReason != NavRouteNoRoute {
			t.Fatalf("disconnected route mismatch: route=%+v err=%v", route, err)
		}
	})
}

func TestFindNearestNavGraphPointChoosesSupportedStackedSpan(t *testing.T) {
	source := NavSourceTileDef{
		NavID: "nearest", SchemaVersion: CurrentNavSourceTileSchemaVersion, BuilderVersion: CurrentNavGraphBuilderVersion,
		Coord: TerrainChunkCoordDef{}, ChunkSize: 2, SourceHash: "source", DependencyHash: "dependency",
		Spans: []NavSpanDef{
			{ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1},
			{ID: 1, X: 0, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1},
		},
	}
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.25, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 45}
	built, err := BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	point, err := FindNearestNavGraphPoint([]NavSourceTileDef{source}, []NavGraphTileDef{built.Graph}, 2, 1, Vec3{1.2, 2.9, 0.5}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !point.Found || point.Ref.Span != 1 || point.Point != (Vec3{1, 3, 0.5}) || point.Region != 1 {
		t.Fatalf("unexpected nearest supported point: %+v", point)
	}
}
