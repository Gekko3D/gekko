package content

import "testing"

func TestConnectNavGraphLaddersAddsCapabilityGatedRoutes(t *testing.T) {
	const chunkSize = 4
	source := NavSourceTileDef{
		NavID: "ladder", SchemaVersion: CurrentNavSourceTileSchemaVersion,
		BuilderVersion: CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
		Spans: []NavSpanDef{
			{ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1, Area: "ground"},
			{ID: 1, X: 0, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
			{ID: 2, X: 3, Z: 3, SupportHeight: 0, CeilingHeight: 3, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
		},
	}
	profile := NavAgentProfileDef{
		ID: "climber", Radius: 0.4, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 45,
		Capabilities: []string{NavCapabilityClimbLadder},
	}
	built, err := BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	bottom, top := Vec3{0.5, 0, 0.5}, Vec3{0.5, 3, 0.5}
	ladder := LevelLadderVolumeDef{
		ID: "ladder-1", BoundsCenter: Vec3{0.5, 1.5, 0.5}, BoundsHalfExtents: Vec3{0.2, 1.5, 0.1},
		MountBottom: &bottom, MountTop: &top, ClimbSpeed: 2,
	}

	graphs, diagnostics, err := ConnectNavGraphLadders([]NavSourceTileDef{source}, []NavGraphTileDef{built.Graph}, []LevelLadderVolumeDef{ladder}, profile, chunkSize, 1)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("connect ladder failed: diagnostics=%+v err=%v", diagnostics, err)
	}
	if len(graphs[0].Transitions) != 2 || len(graphs[0].SpanTransitions) != 2 {
		t.Fatalf("expected two directed ladder transitions, got regions=%+v spans=%+v", graphs[0].Transitions, graphs[0].SpanTransitions)
	}
	for _, test := range []struct {
		name        string
		start, goal Vec3
	}{
		{name: "up", start: bottom, goal: top},
		{name: "down", start: top, goal: bottom},
	} {
		t.Run(test.name, func(t *testing.T) {
			route, err := FindNavGraphRoute([]NavSourceTileDef{source}, graphs, chunkSize, 1, test.start, test.goal)
			if err != nil || !route.Found || len(route.Steps) != 2 {
				t.Fatalf("ladder route failed: route=%+v err=%v", route, err)
			}
			step := route.Steps[1]
			if step.RequiredAction != NavTransitionLadder || step.Traversal == nil || step.Traversal.ID != ladder.ID || step.TraversalWaypoint < 0 {
				t.Fatalf("route lost ladder action binding: %+v", step)
			}
		})
	}
	query, err := NewNavGraphQueryWithBlockers([]NavSourceTileDef{source}, graphs, chunkSize, 1, profile, []NavBlockerDef{{
		ID: "remote-crate", Min: Vec3{3.4, 0, 3.4}, Max: Vec3{3.6, 1, 3.6},
	}})
	if err != nil {
		t.Fatal(err)
	}
	route, err := query.FindRoute(bottom, top)
	if err != nil || !route.Found || len(route.Steps) != 2 || route.Steps[1].Traversal == nil || route.Steps[1].TraversalWaypoint < 0 {
		t.Fatalf("blocker overlay lost ladder action binding: route=%+v err=%v", route, err)
	}

	walker := profile
	walker.ID, walker.Capabilities = "walker", nil
	walkBuilt, err := BuildNavSpanGraph(source, walker, 1)
	if err != nil {
		t.Fatal(err)
	}
	walkGraphs, _, err := ConnectNavGraphLadders([]NavSourceTileDef{source}, []NavGraphTileDef{walkBuilt.Graph}, []LevelLadderVolumeDef{ladder}, walker, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	route, err = FindNavGraphRoute([]NavSourceTileDef{source}, walkGraphs, chunkSize, 1, bottom, top)
	if err != nil || route.Found || route.FailureReason != NavRouteNoRoute {
		t.Fatalf("walker unexpectedly used ladder: route=%+v err=%v", route, err)
	}
}
