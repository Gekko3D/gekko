package gekko

import (
	"fmt"
	"testing"

	"github.com/gekko3d/gekko/content"
)

// A single walkable corridor makes overlay effects observable without depending
// on revision counts or worker timing.
func newNavigationPublicationState(t *testing.T) *StreamedLevelRuntimeState {
	t.Helper()
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.1, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	source := content.NavSourceTileDef{
		NavID: "publication", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: 7,
		SourceHash: "source", DependencyHash: "dependency",
	}
	for x := range 7 {
		source.Spans = append(source.Spans, content.NavSpanDef{
			ID: uint32(x), X: x, Z: 0, SupportHeight: 0, CeilingHeight: 3,
			Headroom: 3, ClearanceRadius: 1, Area: "ground",
		})
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, Generation: 1,
		BaseNavManifest:   &content.NavGraphManifestDef{ChunkSize: 7, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{profile}},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: []content.NavGraphTileDef{built.Graph},
		NavigationRevision: 1, navigationLoadedGen: 1, navigationRequestedGen: 1, navigationQuery: query,
		navigationDisabled: map[string]struct{}{}, navigationOpenDoors: map[string]struct{}{}, navigationBlockers: map[string]content.NavBlockerDef{},
		navigationOverlayDisabled: map[string]struct{}{}, navigationOverlayOpenDoors: map[string]struct{}{}, navigationOverlayBlockers: map[string]content.NavBlockerDef{},
		navigationLoads: make(chan streamedNavigationLoadResult, 1), navigationOverlays: make(chan streamedNavigationOverlayResult, 1),
	}
	t.Cleanup(state.jobs.Wait)
	return state
}

func publicationBlocker(id string, x int) content.NavBlockerDef {
	return content.NavBlockerDef{ID: id, Min: content.Vec3{float32(x) + 0.25, 0, 0}, Max: content.Vec3{float32(x) + 0.75, 1, 1}}
}

func requirePublicationRoute(t *testing.T, service RuntimeNavigationService, from, to int, found bool) content.NavRouteResult {
	t.Helper()
	route, err := service.FindRoute(content.Vec3{float32(from) + 0.5, 0, 0.5}, content.Vec3{float32(to) + 0.5, 0, 0.5})
	if err != nil || route.Found != found {
		t.Fatalf("route from %d to %d: found=%t, want %t, route=%+v err=%v", from, to, route.Found, found, route, err)
	}
	return route
}

func requirePublicationSpan(t *testing.T, service RuntimeNavigationService, x int, blocked bool) {
	t.Helper()
	ref := content.NavSpanRef{Span: uint32(x)}
	if service.IsSpanActive(ref) == blocked || service.IsSpanBlocked(ref) != blocked {
		t.Fatalf("span %d: active=%t blocked=%t, want active=%t blocked=%t", x, service.IsSpanActive(ref), service.IsSpanBlocked(ref), !blocked, blocked)
	}
}

func requirePublicationSupport(t *testing.T, service RuntimeNavigationService) {
	t.Helper()
	point, err := service.ProjectPoint(content.Vec3{0.5, 0.1, 0.5}, 0.2)
	if err != nil || !point.Found || point.Ref != (content.NavSpanRef{Span: 0}) {
		t.Fatalf("published corridor cannot project its start: point=%+v err=%v", point, err)
	}
	requirePublicationRoute(t, service, 0, 2, true)
	goal := content.NavSpanRef{Span: 2}
	if _, reachable := service.ReachableSpans(point.Ref, []content.NavSpanRef{goal})[goal]; !reachable {
		t.Fatal("published corridor cannot reach its unblocked local goal")
	}
}

func TestStreamedNavigationPublishesResidencyWhileOverlayMoves(t *testing.T) {
	state := newNavigationPublicationState(t)
	before := RuntimeNavigationServiceFromStreamedLevelState(state)
	beforeRoute := requirePublicationRoute(t, before, 0, 6, true)
	sources, graphs := state.NavigationSources, state.NavigationGraphs
	state.NavigationSources, state.NavigationGraphs, state.navigationQuery = nil, nil, nil
	state.navigationLoadedGen = 0
	state.navigationLoadActive = true
	captured := publicationBlocker("captured", 3)
	newer := publicationBlocker("newer", 5)
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{"broken-ladder": {}}, map[string]struct{}{}, map[string]content.NavBlockerDef{captured.ID: captured})
	state.navigationLoads <- streamedNavigationLoadResult{RuntimeGeneration: state.Generation, Generation: state.navigationRequestedGen, Sources: sources, Graphs: graphs}
	streamedLevelNavigationSystem(state)

	// A later request must survive publication of the complete captured snapshot.
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{"moving-ladder": {}}, map[string]struct{}{"opening-door": {}}, map[string]content.NavBlockerDef{captured.ID: captured, newer.ID: newer})
	finishStreamedNavigationOverlay(t, state)
	published := RuntimeNavigationServiceFromStreamedLevelState(state)
	if published.ResidencyPending {
		t.Fatal("moving overlay starved publication of current residency")
	}
	requirePublicationSupport(t, published)
	requirePublicationSpan(t, published, 3, true)
	newerWasBlocked := published.IsSpanBlocked(content.NavSpanRef{Span: 5})
	requirePublicationRoute(t, published, 0, 6, false)
	goal := content.NavSpanRef{Span: 6}
	if _, reachable := published.ReachableSpans(content.NavSpanRef{}, []content.NavSpanRef{goal})[goal]; reachable {
		t.Fatal("captured blocker was bypassed by reachability")
	}

	// Churn again while the follow-up build captures the newer blocker.
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{"latest-ladder": {}}, map[string]struct{}{"latest-door": {}}, map[string]content.NavBlockerDef{captured.ID: captured, newer.ID: newer})
	finishStreamedNavigationOverlay(t, state)
	followUp := RuntimeNavigationServiceFromStreamedLevelState(state)
	requirePublicationSupport(t, followUp)
	requirePublicationSpan(t, followUp, 3, true)
	requirePublicationSpan(t, followUp, 5, true)
	requirePublicationRoute(t, followUp, 4, 6, false)
	finishStreamedNavigationOverlay(t, state)
	latest := RuntimeNavigationServiceFromStreamedLevelState(state)
	requirePublicationSpan(t, latest, 3, true)
	requirePublicationSpan(t, latest, 5, true)

	// Previously handed-out services retain their query and dependencies.
	requirePublicationSpan(t, published, 5, newerWasBlocked)
	requirePublicationRoute(t, before, 0, 6, true)
	requirePublicationSpan(t, before, 3, false)
	requirePublicationSpan(t, before, 5, false)
	if valid, reason := before.RouteDependencyStatus(beforeRoute); !valid {
		t.Fatalf("old service changed its route dependencies: %s", reason)
	}
	if valid, _ := latest.RouteDependencyStatus(beforeRoute); valid {
		t.Fatal("new blocker did not invalidate the old route dependency")
	}
}

func TestStreamedNavigationPublishesOverlayProgressDuringRepeatedChurn(t *testing.T) {
	state := newNavigationPublicationState(t)
	fixed := publicationBlocker("captured", 3)
	movingX := 5
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, map[string]content.NavBlockerDef{fixed.ID: fixed, "moving": publicationBlocker("moving", movingX)})
	startStreamedNavigationOverlayBuild(state)
	for step := range 4 {
		nextX := 9 - movingX // Alternate between spans 4 and 5.
		setStreamedNavigationOverlayDesired(state, map[string]struct{}{fmt.Sprintf("ladder-%d", step): {}}, map[string]struct{}{fmt.Sprintf("door-%d", step): {}}, map[string]content.NavBlockerDef{fixed.ID: fixed, "moving": publicationBlocker("moving", nextX)})
		finishStreamedNavigationOverlay(t, state)
		published := RuntimeNavigationServiceFromStreamedLevelState(state)
		requirePublicationSupport(t, published)
		requirePublicationSpan(t, published, 3, true)
		requirePublicationSpan(t, published, movingX, true)
		movingX = nextX
	}
	finishStreamedNavigationOverlay(t, state)
	latest := RuntimeNavigationServiceFromStreamedLevelState(state)
	requirePublicationSpan(t, latest, 3, true)
	requirePublicationSpan(t, latest, movingX, true)
	requirePublicationSpan(t, latest, 9-movingX, false)
}

func TestStreamedNavigationRejectsObsoleteOverlaySnapshots(t *testing.T) {
	for _, obsolete := range []string{"runtime", "loaded topology", "requested residency"} {
		t.Run(obsolete, func(t *testing.T) {
			state := newNavigationPublicationState(t)
			before := RuntimeNavigationServiceFromStreamedLevelState(state)
			route := requirePublicationRoute(t, before, 0, 6, true)
			blocker := publicationBlocker("obsolete", 3)
			setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, map[string]content.NavBlockerDef{blocker.ID: blocker})
			if obsolete == "requested residency" {
				state.navigationPendingGen = state.navigationRequestedGen
				state.navigationPendingSources, state.navigationPendingGraphs = state.NavigationSources, state.NavigationGraphs
			}
			startStreamedNavigationOverlayBuild(state)
			state.jobs.Wait()
			switch obsolete {
			case "runtime":
				state.Generation++
			case "loaded topology":
				state.navigationLoadedGen++
				state.navigationRequestedGen = state.navigationLoadedGen
			case "requested residency":
				state.navigationRequestedGen++
				state.navigationLoadActive = true
			}
			streamedLevelNavigationSystem(state)
			if state.InitErr != nil {
				t.Fatal(state.InitErr)
			}
			after := RuntimeNavigationServiceFromStreamedLevelState(state)
			requirePublicationRoute(t, after, 0, 6, true)
			requirePublicationSpan(t, after, 3, false)
			if after.NavigationRevision != before.NavigationRevision {
				t.Fatal("obsolete snapshot replaced the published service")
			}
			if valid, reason := after.RouteDependencyStatus(route); !valid {
				t.Fatalf("obsolete snapshot invalidated a current route: %s", reason)
			}
		})
	}
}

func TestStreamedNavigationOverlayErrorsRespectSupersession(t *testing.T) {
	for _, loadBacked := range []bool{false, true} {
		for _, superseded := range []bool{false, true} {
			t.Run(fmt.Sprintf("load=%t/superseded=%t", loadBacked, superseded), func(t *testing.T) {
				state := newNavigationPublicationState(t)
				before := RuntimeNavigationServiceFromStreamedLevelState(state)
				beforeRoute := requirePublicationRoute(t, before, 0, 6, true)
				if loadBacked {
					state.navigationRequestedGen = 2
					state.navigationPendingGen = 2
					state.navigationPendingSources, state.navigationPendingGraphs = state.NavigationSources, state.NavigationGraphs
				}
				invalid := publicationBlocker("invalid", 3)
				invalid.Min[0] = invalid.Max[0] + 1
				setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, map[string]content.NavBlockerDef{invalid.ID: invalid})
				startStreamedNavigationOverlayBuild(state)
				valid := publicationBlocker("latest", 3)
				if superseded {
					setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, map[string]content.NavBlockerDef{valid.ID: valid})
				}
				state.jobs.Wait()
				streamedLevelNavigationSystem(state)
				retained := RuntimeNavigationServiceFromStreamedLevelState(state)
				requirePublicationRoute(t, retained, 0, 6, true)
				if current, reason := retained.RouteDependencyStatus(beforeRoute); !current {
					t.Fatalf("failed build replaced route dependencies: %s", reason)
				}
				if !superseded {
					if state.InitErr == nil {
						t.Fatal("current invalid overlay was not fatal")
					}
					return
				}
				if state.InitErr != nil {
					t.Fatalf("superseded invalid overlay became fatal: %v", state.InitErr)
				}
				finishStreamedNavigationOverlay(t, state)
				latest := RuntimeNavigationServiceFromStreamedLevelState(state)
				requirePublicationSpan(t, latest, 3, true)
				requirePublicationRoute(t, latest, 0, 6, false)
			})
		}
	}
}

func TestStreamedNavigationPreservesQueuedDoorAndTraversalIntent(t *testing.T) {
	for _, intent := range []string{"open door", "disable traversal"} {
		t.Run(intent, func(t *testing.T) {
			state := newNavigationPublicationState(t)
			door := content.NavDoorDef{ID: "door", BoundsCenter: content.Vec3{1, 1, 0.5}, BoundsHalfExtents: content.Vec3{0.1, 1, 0.5}}
			profile := state.BaseNavManifest.AgentProfiles[0]
			graphs, _, err := content.ConnectNavGraphDoors(state.NavigationSources, state.NavigationGraphs, []content.NavDoorDef{door}, profile, 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			state.NavigationGraphs = graphs
			state.BaseNavManifest.Doors = []content.NavDoorDef{door}
			state.navigationQuery, err = buildRuntimeNavigationQuery(state.NavigationSources, graphs, 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			before := RuntimeNavigationServiceFromStreamedLevelState(state)
			closedRoute := requirePublicationRoute(t, before, 0, 2, true)
			if !routeRequiresGate(closedRoute, content.NavGateDoor) {
				t.Fatal("fixture route does not require its closed door")
			}
			blocker := publicationBlocker("captured", 3)
			blockers := map[string]content.NavBlockerDef{blocker.ID: blocker}
			setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, blockers)
			startStreamedNavigationOverlayBuild(state)
			disabled, open := map[string]struct{}{}, map[string]struct{}{}
			if intent == "open door" {
				open[door.ID] = struct{}{}
			} else {
				disabled[door.ID] = struct{}{}
			}
			setStreamedNavigationOverlayDesired(state, disabled, open, blockers)
			finishStreamedNavigationOverlay(t, state)
			requirePublicationSpan(t, RuntimeNavigationServiceFromStreamedLevelState(state), 3, true)
			finishStreamedNavigationOverlay(t, state)
			latest := RuntimeNavigationServiceFromStreamedLevelState(state)
			route := requirePublicationRoute(t, latest, 0, 2, intent == "open door")
			if intent == "open door" && routeRequiresGate(route, content.NavGateDoor) {
				t.Fatal("latest queued door opening still requires a door action")
			}
			goal := content.NavSpanRef{Span: 2}
			_, reachable := latest.ReachableSpans(content.NavSpanRef{}, []content.NavSpanRef{goal})[goal]
			if reachable != (intent == "open door") {
				t.Fatalf("latest queued %s reachability=%t", intent, reachable)
			}
			requirePublicationSpan(t, latest, 3, true)
			oldRoute := requirePublicationRoute(t, before, 0, 2, true)
			if !routeRequiresGate(oldRoute, content.NavGateDoor) {
				t.Fatal("old service lost its closed-door action")
			}
			if current, reason := before.RouteDependencyStatus(closedRoute); !current {
				t.Fatalf("old service lost its route dependencies: %s", reason)
			}
		})
	}
}

func TestStreamedNavigationPublicationRetiresOnlyCoveredEditBlockers(t *testing.T) {
	state := newNavigationPublicationState(t)
	first := navigationEditBlocker{Generation: 1, Blocker: publicationBlocker("__nav_edit:first", 2)}
	second := navigationEditBlocker{Generation: 2, Blocker: publicationBlocker("__nav_edit:second", 4)}
	state.navigationEditBlockers = map[string]navigationEditBlocker{first.Blocker.ID: first, second.Blocker.ID: second}
	state.navigationRequestedGen = 2
	state.navigationPendingGen = 2
	state.navigationPendingSources, state.navigationPendingGraphs = state.NavigationSources, state.NavigationGraphs
	state.navigationRetireAtLoad = map[uint64]uint64{2: 1}
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{}, map[string]struct{}{}, map[string]content.NavBlockerDef{first.Blocker.ID: first.Blocker, second.Blocker.ID: second.Blocker})
	startStreamedNavigationOverlayBuild(state)
	setStreamedNavigationOverlayDesired(state, map[string]struct{}{"moving-ladder": {}}, map[string]struct{}{}, copyNavigationBlockers(state.navigationOverlayBlockers))
	finishStreamedNavigationOverlay(t, state)
	captured := RuntimeNavigationServiceFromStreamedLevelState(state)
	if captured.ResidencyPending {
		t.Fatal("edit-blocked topology did not publish under overlay churn")
	}
	requirePublicationSpan(t, captured, 2, true)
	requirePublicationSpan(t, captured, 4, true)
	finishStreamedNavigationOverlay(t, state)
	latest := RuntimeNavigationServiceFromStreamedLevelState(state)
	requirePublicationSpan(t, latest, 2, false)
	requirePublicationSpan(t, latest, 4, true)
	requirePublicationRoute(t, latest, 0, 3, true)
	requirePublicationRoute(t, latest, 3, 6, false)
	requirePublicationSpan(t, captured, 2, true)
	requirePublicationSpan(t, captured, 4, true)
}
