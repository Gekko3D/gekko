package content

import "testing"

func TestConnectNavGraphDoorsCreatesExplicitCrossing(t *testing.T) {
	const chunkSize = 8
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	sources, graphs := buildFlatNavRouteWorld(t, []TerrainChunkCoordDef{{}}, chunkSize, profile)
	door := NavDoorDef{
		ID: "door-1", BoundsCenter: Vec3{4, 1, 4}, BoundsHalfExtents: Vec3{0.1, 1, 1},
	}
	linked, diagnostics, err := ConnectNavGraphDoors(sources, graphs, []NavDoorDef{door}, profile, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("door diagnostics = %+v", diagnostics)
	}
	query, err := NewNavGraphQuery(sources, linked, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	route, err := query.FindRoute(Vec3{1.5, 0, 4.5}, Vec3{6.5, 0, 4.5})
	if err != nil || !route.Found {
		t.Fatalf("door route not found: route=%+v err=%v", route, err)
	}
	foundDoor := false
	for _, step := range route.Steps {
		if step.RequiredAction != NavTransitionWalk || step.Gate == nil || step.Gate.Kind != NavGateDoor || step.Gate.ID != door.ID {
			continue
		}
		foundDoor = step.Traversal != nil && step.Traversal.OwnerID == door.ID && step.Traversal.Start[0] < door.BoundsCenter[0] && step.Traversal.End[0] > door.BoundsCenter[0]
	}
	if !foundDoor {
		t.Fatalf("route omitted explicit door traversal: %+v", route)
	}
}

func TestConnectNavGraphDoorsCrossesTileSeam(t *testing.T) {
	const chunkSize = 4
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	sources, graphs := buildFlatNavRouteWorld(t, []TerrainChunkCoordDef{{}, {X: 1}}, chunkSize, profile)
	door := NavDoorDef{ID: "seam-door", BoundsCenter: Vec3{4, 1, 2}, BoundsHalfExtents: Vec3{0.1, 1, 1}}
	linked, _, err := ConnectNavGraphDoors(sources, graphs, []NavDoorDef{door}, profile, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := NewNavGraphQuery(sources, linked, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	route, err := query.FindRoute(Vec3{2.5, 0, 2.5}, Vec3{5.5, 0, 2.5})
	if err != nil || !route.Found || !navRouteHasDoor(route, door.ID) {
		t.Fatalf("tile-seam door route = %+v err=%v", route, err)
	}
}

func navRouteHasDoor(route NavRouteResult, id string) bool {
	for _, step := range route.Steps {
		if step.Gate != nil && step.Gate.Kind == NavGateDoor && step.Gate.ID == id {
			return true
		}
	}
	return false
}

func TestConnectNavGraphDoorsComposesHorizontalHatchWithMovement(t *testing.T) {
	source, graph, profile, door := buildHorizontalHatchGraph(t)
	linked, diagnostics, err := ConnectNavGraphDoors([]NavSourceTileDef{source}, []NavGraphTileDef{graph}, []NavDoorDef{door}, profile, 8, 1)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("link horizontal hatch: diagnostics=%+v err=%v", diagnostics, err)
	}
	query, err := NewNavGraphQuery([]NavSourceTileDef{source}, linked, 8, 1)
	if err != nil {
		t.Fatal(err)
	}
	down, err := query.FindRoute(Vec3{1.5, 3, 2.5}, Vec3{2.5, 0, 2.5})
	if err != nil || !down.Found || len(down.Steps) != 2 || down.Steps[1].RequiredAction != NavTransitionDrop || down.Steps[1].Gate == nil || down.Steps[1].Gate.ID != door.ID {
		t.Fatalf("hatch drop route = %+v err=%v", down, err)
	}
	up, err := query.FindRoute(Vec3{2.5, 0, 2.5}, Vec3{1.5, 3, 2.5})
	if err != nil || up.Found {
		t.Fatalf("hatch invented upward drop: route=%+v err=%v", up, err)
	}
}

func TestConnectNavGraphDoorsRejectsDropBlockedByOpenHatch(t *testing.T) {
	source, graph, profile, door := buildHorizontalHatchGraph(t)
	door.OpenOffset = Vec3{0, -2.5, 0}
	linked, diagnostics, err := connectNavGraphDoorGates(
		[]NavSourceTileDef{source}, []NavGraphTileDef{graph}, []NavDoorDef{door}, profile, 8, 1, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, transition := range linked[0].SpanTransitions {
		if transition.Kind == NavTransitionDrop {
			t.Fatalf("open hatch blocks inferred landing: %+v", transition)
		}
	}
	if len(diagnostics) != 1 || diagnostics[0] != (NavDoorDiagnostic{DoorID: door.ID, Code: NavDoorSkippedUnsupported}) {
		t.Fatalf("hatch diagnostics = %+v", diagnostics)
	}
}

func TestConnectNavGraphDoorsGatesLadderInsteadOfInferringDrop(t *testing.T) {
	source, graph, profile, door := buildHorizontalHatchGraph(t)
	regions := navGraphSpanRegions(graph)
	top := NavPointResult{Found: true, Ref: NavSpanRef{Span: 0}, Region: regions[0], Point: Vec3{1.5, 3, 2.5}}
	bottom := NavPointResult{Found: true, Ref: NavSpanRef{Span: 1}, Region: regions[1], Point: Vec3{2.5, 0, 2.5}}
	appendNavLadderDirection(&graph, "ladder-1", top, bottom, 1, 2, 1, 3)
	appendNavLadderDirection(&graph, "ladder-1", bottom, top, 1, 2, 1, 3)

	linked, diagnostics, err := ConnectNavGraphDoors([]NavSourceTileDef{source}, []NavGraphTileDef{graph}, []NavDoorDef{door}, profile, 8, 1)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("gate hatch ladder: diagnostics=%+v err=%v", diagnostics, err)
	}
	ladders, drops := 0, 0
	for _, transition := range linked[0].SpanTransitions {
		switch transition.Kind {
		case NavTransitionLadder:
			ladders++
			if transition.Gate == nil || transition.Gate.ID != door.ID {
				t.Fatalf("ladder omitted hatch gate: %+v", transition)
			}
		case NavTransitionDrop:
			drops++
		}
	}
	if ladders != 2 || drops != 0 {
		t.Fatalf("hatch movement kinds: ladders=%d drops=%d graph=%+v", ladders, drops, linked[0])
	}
}

func buildHorizontalHatchGraph(t *testing.T) (NavSourceTileDef, NavGraphTileDef, NavAgentProfileDef, NavDoorDef) {
	t.Helper()
	profile := NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45, Capabilities: []string{NavCapabilityClimbLadder}}
	source := NavSourceTileDef{
		NavID: "hatch", SchemaVersion: CurrentNavSourceTileSchemaVersion, Coord: TerrainChunkCoordDef{}, BuilderVersion: CurrentNavGraphBuilderVersion,
		SourceHash: "source", DependencyHash: "dependency", ChunkSize: 8,
		Spans: []NavSpanDef{
			{ID: 0, X: 1, Z: 2, SupportHeight: 3, CeilingHeight: 8, Headroom: 5, ClearanceRadius: 1},
			{ID: 1, X: 2, Z: 2, SupportHeight: 0, CeilingHeight: 8, Headroom: 8, ClearanceRadius: 1},
		},
	}
	graph := NavGraphTileDef{
		NavID: source.NavID, SchemaVersion: CurrentNavGraphTileSchemaVersion, Coord: source.Coord, AgentProfileID: profile.ID,
		BuilderVersion: source.BuilderVersion, SourceHash: source.SourceHash, DependencyHash: source.DependencyHash, SpanIDs: []uint32{0, 1},
	}
	var err error
	graph, err = CompressNavGraphRegions(source, graph, 1)
	if err != nil {
		t.Fatal(err)
	}
	door := NavDoorDef{ID: "hatch-1", Group: "hatch", BoundsCenter: Vec3{2.5, 2.9, 2.5}, BoundsHalfExtents: Vec3{1, 0.1, 1}}
	return source, graph, profile, door
}

func TestNavDoorGroupsKeepRemoteDoorsIndependent(t *testing.T) {
	doors := []NavDoorDef{
		{ID: "a", Group: "open_hatches", BoundsCenter: Vec3{0, 0, 0}, BoundsHalfExtents: Vec3{1, 0.1, 1}},
		{ID: "b", Group: "open_hatches", BoundsCenter: Vec3{2, 0, 0}, BoundsHalfExtents: Vec3{1, 0.1, 1}},
		{ID: "c", Group: "open_hatches", BoundsCenter: Vec3{10, 0, 0}, BoundsHalfExtents: Vec3{1, 0.1, 1}},
	}
	groups := navDoorGroups(doors)
	if len(groups) != 2 || len(groups[0].Doors) != 2 || len(groups[1].Doors) != 1 {
		t.Fatalf("physical hatch groups = %+v", groups)
	}
}
