package gekko

import (
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type voxelWorldDirtyChunkKey struct {
	WorldID string
	Coord   content.TerrainChunkCoordDef
}

type VoxelWorldDirtyChunks struct {
	mu       sync.Mutex
	Imported map[voxelWorldDirtyChunkKey]*content.ImportedWorldChunkDef
}

type streamedNavigationLoadResult struct {
	RuntimeGeneration uint64
	Generation        uint64
	Sources           []content.NavSourceTileDef
	Graphs            []content.NavGraphTileDef
	Err               error
}

type streamedNavigationOverlayResult struct {
	RuntimeGeneration  uint64
	GraphRevision      uint64
	LoadGeneration     uint64
	OverlayGeneration  uint64
	Sources            []content.NavSourceTileDef
	Graphs             []content.NavGraphTileDef
	Query              *content.NavGraphQuery
	DisabledTraversals map[string]struct{}
	OpenDoors          map[string]struct{}
	Blockers           map[string]content.NavBlockerDef
	Err                error
}

type streamedNavigationRebuildResult struct {
	RuntimeGeneration uint64
	EditGeneration    uint64
	Delta             content.WorldDeltaDef
	Result            content.NavGraphDeltaBakeResult
	Err               error
}

type streamedNavigationEditAnalysisItem struct {
	Edit            runtimeVoxelEdit
	WorldID         string
	Coord           content.TerrainChunkCoordDef
	ChunkSize       int
	VoxelResolution float32
	VoxelMap        *volume.XBrickMap
	Snapshot        *content.ImportedWorldChunkDef
	Backing         *VoxelBackingComponent
}

type streamedNavigationEditAnalysisResultItem struct {
	Input    streamedNavigationEditAnalysisItem
	Snapshot *content.ImportedWorldChunkDef
	Override content.ImportedWorldChunkOverrideDef
	Reason   string
}

type streamedNavigationEditAnalysisResult struct {
	RuntimeGeneration      uint64
	GraphGeneration        uint64
	GraphRequestGeneration uint64
	Items                  []streamedNavigationEditAnalysisResultItem
	IgnoredRemovals        map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}
	Err                    error
}

type streamedWorldDeltaSaveResult struct {
	RuntimeGeneration uint64
	Generation        uint64
	Err               error
}

type navigationVoxelSnapshot struct {
	Source   *volume.XBrickMap
	Revision uint64
	Snapshot *volume.XBrickMap
}

type navigationQueuedEdit struct {
	Generation uint64
	Snapshot   *content.ImportedWorldChunkDef
}

type navigationEditBlocker struct {
	Generation uint64
	Blocker    content.NavBlockerDef
}

const (
	navigationRebuildQuietPeriod = 100 * time.Millisecond
	navigationRebuildMaxDelay    = 250 * time.Millisecond
	navigationEditAnalysisDelay  = 50 * time.Millisecond
	navigationEditAnalysisMaxAge = 250 * time.Millisecond
)

type RuntimeNavigationService struct {
	Sources                 []content.NavSourceTileDef
	Graphs                  []content.NavGraphTileDef
	ResidencyPending        bool
	ChunkSize               int
	VoxelResolution         float32
	NavigationRevision      uint64
	NavigationGraphRevision uint64
	query                   *content.NavGraphQuery
	profiles                []content.NavAgentProfileDef
	carriers                []content.NavCarrierDef
	disabledTraversals      map[string]struct{}
	openDoors               map[string]struct{}
	blockers                map[string]content.NavBlockerDef
}

// NavigationBlockerComponent opts an entity's world-space AABB into runtime
// navigation blocking. ID must stay unique and stable while entity exists.
type NavigationBlockerComponent struct {
	ID       string
	Disabled bool
}

func configureStreamedNavigationManifest(state *StreamedLevelRuntimeState, level *content.LevelDef, cfg StreamedLevelRuntimeConfig) error {
	if state == nil || level == nil {
		return nil
	}
	path := strings.TrimSpace(cfg.NavigationManifestPath)
	if path == "" && level.Navigation != nil {
		path = strings.TrimSpace(level.Navigation.ManifestPath)
	}
	if path == "" {
		return nil
	}
	path = content.ResolveDocumentPath(path, cfg.LevelPath)
	manifest, err := content.LoadNavGraphManifest(path)
	if err != nil {
		return err
	}
	if state.BaseWorldID != "" && manifest.SourceWorldID != "" && manifest.SourceWorldID != state.BaseWorldID {
		return fmt.Errorf("navigation source world %q does not match base world %q", manifest.SourceWorldID, state.BaseWorldID)
	}
	if manifest.ChunkSize != level.ChunkSize || absf(manifest.VoxelResolution-level.VoxelResolution) > 1e-4 {
		return fmt.Errorf("navigation graph chunk metrics do not match level")
	}
	state.BaseNavManifestPath = path
	state.BaseNavManifest = manifest
	return nil
}

func RuntimeNavigationServiceFromStreamedLevelState(state *StreamedLevelRuntimeState) RuntimeNavigationService {
	if state == nil || state.BaseNavManifest == nil {
		return RuntimeNavigationService{}
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return RuntimeNavigationService{
		Sources: append([]content.NavSourceTileDef(nil), state.NavigationSources...),
		Graphs:  append([]content.NavGraphTileDef(nil), state.NavigationGraphs...),
		ResidencyPending: state.navigationLoadedGen != state.navigationRequestedGen ||
			state.navigationLoadActive || state.navigationPendingGen != 0,
		ChunkSize: state.BaseNavManifest.ChunkSize, VoxelResolution: state.BaseNavManifest.VoxelResolution,
		NavigationRevision:      state.NavigationRevision,
		NavigationGraphRevision: state.navigationLoadedGen,
		query:                   state.navigationQuery,
		profiles:                append([]content.NavAgentProfileDef(nil), state.BaseNavManifest.AgentProfiles...),
		carriers:                append([]content.NavCarrierDef(nil), state.BaseNavManifest.Carriers...),
		disabledTraversals:      copyNavigationTraversalSet(state.navigationDisabled),
		openDoors:               copyNavigationTraversalSet(state.navigationOpenDoors),
		blockers:                copyNavigationBlockers(state.navigationBlockers),
	}
}

func (s RuntimeNavigationService) FindRoute(start, goal content.Vec3) (content.NavRouteResult, error) {
	if s.query != nil {
		route, err := s.query.FindRoute(start, goal)
		route.NavigationRevision = s.NavigationRevision
		return route, err
	}
	if len(s.Sources) == 0 || len(s.Graphs) == 0 {
		return content.NavRouteResult{FailureReason: "navigation_unavailable", NavigationRevision: s.NavigationRevision}, nil
	}
	profileID := s.Graphs[0].AgentProfileID
	graphs := make([]content.NavGraphTileDef, 0, len(s.Graphs))
	for _, graph := range s.Graphs {
		if graph.AgentProfileID == profileID {
			graphs = append(graphs, graph)
		}
	}
	route, err := content.FindNavGraphRoute(s.Sources, graphs, s.ChunkSize, s.VoxelResolution, start, goal)
	route.NavigationRevision = s.NavigationRevision
	return route, err
}

// FindRouteAvoidingTraversal resolves a temporary interaction detour without
// changing global navigation state.
func (s RuntimeNavigationService) FindRouteAvoidingTraversal(start, goal content.Vec3, traversalID string) (content.NavRouteResult, error) {
	constrained, err := s.WithTraversalDisabled(traversalID)
	if err != nil {
		return content.NavRouteResult{}, err
	}
	return constrained.FindRoute(start, goal)
}

// WithTraversalDisabled returns an immutable query view for route detours.
func (s RuntimeNavigationService) WithTraversalDisabled(traversalID string) (RuntimeNavigationService, error) {
	if strings.TrimSpace(traversalID) == "" {
		return s, nil
	}
	disabled := copyNavigationTraversalSet(s.disabledTraversals)
	disabled[traversalID] = struct{}{}
	// ponytail: rebuild once per interaction plan; cache constrained views only
	// if controller-heavy maps make this measurable.
	query, err := buildRuntimeNavigationQueryWithDoorOverlays(s.Sources, s.Graphs, s.ChunkSize, s.VoxelResolution, s.profiles, disabled, s.openDoors, s.blockers, s.carriers)
	if err != nil {
		return RuntimeNavigationService{}, err
	}
	s.query, s.disabledTraversals = query.WithInheritedTileEpochs(s.query), disabled
	return s, nil
}

// WithSpanTransitionDisabled returns an immutable query view that avoids one
// directed walk edge.
func (s RuntimeNavigationService) WithSpanTransitionDisabled(from, to content.NavSpanRef) (RuntimeNavigationService, error) {
	graphs := append([]content.NavGraphTileDef(nil), s.Graphs...)
	found := false
	for i := range graphs {
		if graphs[i].Coord != from.Tile {
			continue
		}
		kept := make([]content.NavSpanTransitionDef, 0, len(graphs[i].SpanTransitions))
		for _, transition := range graphs[i].SpanTransitions {
			if transition.From == from.Span && transition.To == to && transition.Kind == content.NavTransitionWalk {
				found = true
				continue
			}
			kept = append(kept, transition)
		}
		graphs[i].SpanTransitions = kept
	}
	if !found {
		return s, nil
	}
	query, err := buildRuntimeNavigationQueryWithDoorOverlays(s.Sources, graphs, s.ChunkSize, s.VoxelResolution, s.profiles, s.disabledTraversals, s.openDoors, s.blockers, s.carriers)
	if err != nil {
		return RuntimeNavigationService{}, err
	}
	s.Graphs, s.query = graphs, query.WithInheritedTileEpochs(s.query)
	return s, nil
}

// RequiresSupportHandoff reports whether a directed route edge changes the
// walkable support height without invoking an explicit traversal action.
func (s RuntimeNavigationService) RequiresSupportHandoff(from, to content.NavSpanRef) bool {
	for _, graph := range s.Graphs {
		if graph.Coord != from.Tile {
			continue
		}
		for _, transition := range graph.SpanTransitions {
			if transition.From == from.Span && transition.To == to {
				return transition.Kind == content.NavTransitionWalk && absf(transition.StepDelta) > 1e-4
			}
		}
	}
	return false
}

func (s RuntimeNavigationService) ProjectPoint(point content.Vec3, maxDistance float32) (content.NavPointResult, error) {
	if s.query != nil {
		return s.query.ProjectPoint(point, maxDistance)
	}
	if len(s.Sources) == 0 || len(s.Graphs) == 0 {
		return content.NavPointResult{}, nil
	}
	profileID := s.Graphs[0].AgentProfileID
	graphs := make([]content.NavGraphTileDef, 0, len(s.Graphs))
	for _, graph := range s.Graphs {
		if graph.AgentProfileID == profileID {
			graphs = append(graphs, graph)
		}
	}
	return content.FindNearestNavGraphPoint(s.Sources, graphs, s.ChunkSize, s.VoxelResolution, point, maxDistance)
}

// Locate binds an actor world position to the same span contract used by
// route starts. It repairs at most one voxel of 3D drift.
func (s RuntimeNavigationService) Locate(point content.Vec3) (content.NavPointResult, error) {
	if s.query != nil {
		return s.query.Locate(point)
	}
	if len(s.Sources) == 0 || len(s.Graphs) == 0 {
		return content.NavPointResult{}, nil
	}
	profileID := s.Graphs[0].AgentProfileID
	graphs := make([]content.NavGraphTileDef, 0, len(s.Graphs))
	for _, graph := range s.Graphs {
		if graph.AgentProfileID == profileID {
			graphs = append(graphs, graph)
		}
	}
	return content.FindNavGraphLocation(s.Sources, graphs, s.ChunkSize, s.VoxelResolution, point)
}

// IsRouteSupport reports whether live motor support remains inside the swept
// active route segment or its capsule-scale recent tail. WaypointSpans are
// steering checkpoint identities, not a complete list of traversed spans.
func (s RuntimeNavigationService) IsRouteSupport(route content.NavRouteResult, waypoint int, support content.NavPointResult, radius, waypointTolerance float32) bool {
	if !route.Found || !route.StartLocation.Found || !support.Found ||
		waypoint < 0 || waypoint >= len(route.Waypoints) ||
		!s.IsRouteCurrent(route) ||
		!runtimeNavigationFinite(radius) || radius < 0 ||
		!runtimeNavigationFinite(waypointTolerance) || waypointTolerance < 0 ||
		!s.IsSpanActive(support.Ref) {
		return false
	}
	tolerance := radius + waypointTolerance + max(s.VoxelResolution, 0.05)
	backtrack := float32(0)
	for segment := waypoint; segment >= 0; segment-- {
		start := route.StartLocation.Point
		if segment > 0 {
			start = route.Waypoints[segment-1]
		}
		end := route.Waypoints[segment]
		if runtimeNavigationPointSegmentDistance(support.Point, start, end) <= tolerance {
			return true
		}
		if segment == 0 {
			break
		}
		backtrack += runtimeNavigationVecDistance(start, end)
		if backtrack > tolerance {
			break
		}
	}
	return false
}

// RouteDependencyStatus validates a route against the current per-tile
// topology epochs. Routes without dependency metadata retain legacy revision
// behavior for callers constructing results by hand.
func (s RuntimeNavigationService) RouteDependencyStatus(route content.NavRouteResult) (bool, string) {
	if len(route.TileDependencies) != 0 && s.query != nil {
		return s.query.RouteDependencyStatus(route)
	}
	if route.NavigationRevision != 0 && route.NavigationRevision != s.NavigationRevision {
		return false, "navigation_revision_changed"
	}
	return true, ""
}

func (s RuntimeNavigationService) IsRouteCurrent(route content.NavRouteResult) bool {
	valid, _ := s.RouteDependencyStatus(route)
	return valid
}

func runtimeNavigationPointSegmentDistance(point, start, end content.Vec3) float32 {
	segment := content.Vec3{end[0] - start[0], end[1] - start[1], end[2] - start[2]}
	offset := content.Vec3{point[0] - start[0], point[1] - start[1], point[2] - start[2]}
	lengthSquared := segment[0]*segment[0] + segment[1]*segment[1] + segment[2]*segment[2]
	t := float32(0)
	if lengthSquared > 0 {
		t = max(float32(0), min(float32(1), (offset[0]*segment[0]+offset[1]*segment[1]+offset[2]*segment[2])/lengthSquared))
	}
	closest := content.Vec3{start[0] + segment[0]*t, start[1] + segment[1]*t, start[2] + segment[2]*t}
	return runtimeNavigationVecDistance(point, closest)
}

func runtimeNavigationVecDistance(a, b content.Vec3) float32 {
	x, y, z := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(x*x + y*y + z*z)))
}

func runtimeNavigationFinite(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

// IsSpanBlocked reports whether a runtime blocker removed a baked span.
func (s RuntimeNavigationService) IsSpanBlocked(ref content.NavSpanRef) bool {
	return s.query != nil && s.query.IsSpanBlocked(ref)
}

// IsSpanActive reports whether a span remains in the resident runtime graph.
func (s RuntimeNavigationService) IsSpanActive(ref content.NavSpanRef) bool {
	if s.query != nil {
		return s.query.IsSpanActive(ref)
	}
	for _, graph := range s.Graphs {
		if graph.Coord != ref.Tile {
			continue
		}
		for _, span := range graph.SpanIDs {
			if span == ref.Span {
				return true
			}
		}
	}
	return false
}

// ReachableSpans returns target spans reachable through the current runtime
// navigation overlay.
func (s RuntimeNavigationService) ReachableSpans(start content.NavSpanRef, targets []content.NavSpanRef) map[content.NavSpanRef]uint8 {
	if s.query != nil {
		return s.query.ReachableSpans(start, targets)
	}
	result := make(map[content.NavSpanRef]uint8, len(targets))
	if len(s.Sources) == 0 || len(s.Graphs) == 0 {
		return result
	}
	query, err := buildRuntimeNavigationQueryWithDoorOverlays(
		s.Sources, s.Graphs, s.ChunkSize, s.VoxelResolution,
		s.profiles, s.disabledTraversals, s.openDoors, s.blockers, s.carriers,
	)
	if err != nil {
		return result
	}
	return query.ReachableSpans(start, targets)
}

func requestStreamedNavigationResidency(state *StreamedLevelRuntimeState, desired map[ChunkCoord]struct{}) {
	if state == nil || state.BaseNavManifest == nil {
		return
	}
	next := make(map[content.TerrainChunkCoordDef]struct{})
	available := make(map[content.TerrainChunkCoordDef]struct{}, len(state.BaseNavManifest.SourceTiles))
	for _, entry := range state.BaseNavManifest.SourceTiles {
		available[entry.Coord] = struct{}{}
	}
	if state.WorldDelta != nil {
		for _, override := range state.WorldDelta.NavigationSourceOverrides {
			if override.NavID == state.BaseNavManifest.NavID {
				available[override.ChunkCoord] = struct{}{}
			}
		}
	}
	for coord := range desired {
		navCoord := terrainCoordFromChunk(coord)
		if _, ok := available[navCoord]; ok {
			next[navCoord] = struct{}{}
		}
	}
	if navCoordSetsEqual(next, state.navigationDesired) {
		return
	}
	state.navigationDesired = next
	reloadStreamedNavigationResidency(state)
}

func reloadStreamedNavigationResidency(state *StreamedLevelRuntimeState) {
	if state == nil || state.BaseNavManifest == nil {
		return
	}
	state.navigationRequestedGen++
	if !state.navigationLoadActive {
		startStreamedNavigationLoad(state)
	}
}

func startStreamedNavigationLoad(state *StreamedLevelRuntimeState) {
	if state == nil || state.BaseNavManifest == nil {
		return
	}
	generation := state.navigationRequestedGen
	desired := copyTerrainCoordSet(state.navigationDesired)
	manifest := copyNavGraphManifest(state.BaseNavManifest)
	delta := copyWorldDeltaForNav(state.WorldDelta)
	manifestPath, deltaPath := state.BaseNavManifestPath, state.WorldDeltaPath
	runtimeGeneration := state.Generation
	state.navigationLoadActive = true
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		sources, graphs, err := loadStreamedNavigationResidency(manifest, manifestPath, &delta, deltaPath, desired)
		state.navigationLoads <- streamedNavigationLoadResult{
			RuntimeGeneration: runtimeGeneration, Generation: generation,
			Sources: sources, Graphs: graphs, Err: err,
		}
	}()
}

func buildRuntimeNavigationQuery(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithDisabledTraversals(sources, graphs, chunkSize, voxelResolution, nil)
}

func buildRuntimeNavigationQueryWithDisabledTraversals(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, disabled map[string]struct{}) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithOverlays(sources, graphs, chunkSize, voxelResolution, nil, disabled, nil)
}

func buildRuntimeNavigationQueryWithOverlays(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, profiles []content.NavAgentProfileDef, disabled map[string]struct{}, blockers map[string]content.NavBlockerDef) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithDoorOverlays(sources, graphs, chunkSize, voxelResolution, profiles, disabled, nil, blockers, nil)
}

func buildRuntimeNavigationQueryWithDoorOverlays(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, profiles []content.NavAgentProfileDef, disabled, openDoors map[string]struct{}, blockers map[string]content.NavBlockerDef, carriers []content.NavCarrierDef) (*content.NavGraphQuery, error) {
	if len(sources) == 0 || len(graphs) == 0 {
		return nil, nil
	}
	profileID := graphs[0].AgentProfileID
	var profile *content.NavAgentProfileDef
	for i := range profiles {
		if profiles[i].ID == profileID {
			profile = &profiles[i]
			break
		}
	}
	if len(profiles) != 0 && profile == nil {
		return nil, fmt.Errorf("navigation runtime requires agent profile %q", profileID)
	}
	profileGraphs := make([]content.NavGraphTileDef, 0, len(graphs))
	for _, graph := range graphs {
		if graph.AgentProfileID == profileID {
			profileGraphs = append(profileGraphs, navGraphWithRuntimeTraversals(graph, disabled, openDoors, profile))
		}
	}
	if len(blockers) != 0 || len(carriers) != 0 {
		if profile != nil {
			carrierBlockers := content.NavCarrierBlockers(carriers, *profile)
			return content.NewNavGraphQueryWithBlockers(sources, profileGraphs, chunkSize, voxelResolution, *profile, append(sortedNavigationBlockers(blockers), carrierBlockers...))
		}
		return nil, fmt.Errorf("navigation blocker or carrier overlay requires agent profile %q", profileID)
	}
	return content.NewNavGraphQuery(sources, profileGraphs, chunkSize, voxelResolution)
}

func navGraphWithoutDisabledTraversals(graph content.NavGraphTileDef, disabled map[string]struct{}) content.NavGraphTileDef {
	return navGraphWithRuntimeTraversals(graph, disabled, nil, nil)
}

func navGraphWithRuntimeTraversals(graph content.NavGraphTileDef, disabled, openDoors map[string]struct{}, profile *content.NavAgentProfileDef) content.NavGraphTileDef {
	if len(disabled) == 0 && len(openDoors) == 0 && profile == nil {
		return graph
	}
	copy := graph
	copy.SpanTransitions = make([]content.NavSpanTransitionDef, 0, len(graph.SpanTransitions))
	for _, transition := range graph.SpanTransitions {
		if !navTransitionDisabled(transition.Traversal, transition.Gate, disabled) &&
			(profile == nil || content.NavTraversalSupportedByProfile(*profile, transition.Kind, transition.Traversal)) {
			transition.Gate, transition.Cost = runtimeDoorGate(transition.Gate, transition.Cost, openDoors)
			copy.SpanTransitions = append(copy.SpanTransitions, transition)
		}
	}
	copy.Transitions = make([]content.NavRegionTransitionDef, 0, len(graph.Transitions))
	for _, transition := range graph.Transitions {
		if navTransitionDisabled(transition.Traversal, transition.Gate, disabled) ||
			profile != nil && !content.NavTraversalSupportedByProfile(*profile, transition.Kind, transition.Traversal) {
			continue
		}
		transition.Gate, transition.Cost = runtimeDoorGate(transition.Gate, transition.Cost, openDoors)
		transition.ID = uint32(len(copy.Transitions))
		copy.Transitions = append(copy.Transitions, transition)
	}
	return copy
}

func runtimeDoorGate(gate *content.NavTransitionGateDef, cost float32, openDoors map[string]struct{}) (*content.NavTransitionGateDef, float32) {
	if gate == nil || gate.Kind != content.NavGateDoor {
		return gate, cost
	}
	if _, open := openDoors[gate.ID]; open {
		return nil, maxf(cost-content.NavDoorClosedCostPenalty, 0.001)
	}
	return gate, cost
}

func navTransitionDisabled(traversal *content.NavTraversalDef, gate *content.NavTransitionGateDef, disabled map[string]struct{}) bool {
	if traversal != nil {
		if _, found := disabled[traversal.StableLinkID()]; found {
			return true
		}
		if _, found := disabled[traversal.Owner()]; found {
			return true
		}
	}
	if gate != nil {
		_, found := disabled[gate.ID]
		return found
	}
	return false
}

func loadStreamedNavigationResidency(manifest *content.NavGraphManifestDef, manifestPath string, delta *content.WorldDeltaDef, deltaPath string, desired map[content.TerrainChunkCoordDef]struct{}) ([]content.NavSourceTileDef, []content.NavGraphTileDef, error) {
	coords := sortedTerrainCoords(desired)
	sources := make([]content.NavSourceTileDef, 0, len(coords))
	graphs := make([]content.NavGraphTileDef, 0, len(coords)*len(manifest.AgentProfiles))
	for _, coord := range coords {
		source, err := content.LoadEffectiveNavSourceTile(manifest, manifestPath, delta, deltaPath, coord)
		if err != nil {
			return nil, nil, err
		}
		if !source.Found || source.Empty || source.Tile == nil {
			continue
		}
		sources = append(sources, *source.Tile)
		for _, profile := range manifest.AgentProfiles {
			graph, err := content.LoadEffectiveNavGraphTile(manifest, manifestPath, delta, deltaPath, coord, profile.ID)
			if err != nil {
				return nil, nil, err
			}
			if graph.Found && !graph.Empty && graph.Tile != nil {
				graphs = append(graphs, *graph.Tile)
			}
		}
	}
	trimNavGraphResidency(graphs)
	return sources, graphs, nil
}

func trimNavGraphResidency(graphs []content.NavGraphTileDef) {
	type key struct {
		Coord   content.TerrainChunkCoordDef
		Profile string
	}
	resident := make(map[key]struct{}, len(graphs))
	for _, graph := range graphs {
		resident[key{graph.Coord, graph.AgentProfileID}] = struct{}{}
	}
	for i := range graphs {
		graph := &graphs[i]
		spans := graph.SpanTransitions[:0]
		for _, transition := range graph.SpanTransitions {
			if transition.To.Tile == graph.Coord {
				spans = append(spans, transition)
				continue
			}
			if _, ok := resident[key{transition.To.Tile, graph.AgentProfileID}]; ok {
				spans = append(spans, transition)
			}
		}
		graph.SpanTransitions = spans
		regions := graph.Transitions[:0]
		for _, transition := range graph.Transitions {
			if transition.ToTile == graph.Coord {
				regions = append(regions, transition)
				continue
			}
			if _, ok := resident[key{transition.ToTile, graph.AgentProfileID}]; ok {
				regions = append(regions, transition)
			}
		}
		graph.Transitions = regions
		for id := range graph.Transitions {
			graph.Transitions[id].ID = uint32(id)
		}
	}
}

func streamedLevelNavigationSystem(state *StreamedLevelRuntimeState) {
	if state == nil || !state.Initialized {
		return
	}
	commitStreamedWorldDeltaSave(state)
	if state.InitErr != nil {
		return
	}
	select {
	case result := <-state.navigationLoads:
		if result.RuntimeGeneration != state.Generation {
			break
		}
		state.navigationLoadActive = false
		if result.Generation != state.navigationRequestedGen {
			break
		}
		if result.Err != nil {
			state.InitErr = result.Err
			return
		}
		state.navigationPendingSources = result.Sources
		state.navigationPendingGraphs = result.Graphs
		state.navigationPendingGen = result.Generation
	default:
	}
	select {
	case result := <-state.navigationRebuilds:
		if result.RuntimeGeneration != state.Generation {
			break
		}
		state.navigationRebuildActive = false
		state.Metrics.NavigationRebuildActive = false
		if result.EditGeneration != state.navigationEditGeneration {
			state.Metrics.NavigationRebuildDiscardedCount++
			state.Metrics.NavigationRebuildDiscardedRevision = result.EditGeneration
			state.Metrics.NavigationRebuildTerminalReason = "superseded"
			break
		}
		state.Metrics.NavigationRebuildCompletedCount++
		state.Metrics.NavigationRebuildCompletedRevision = result.EditGeneration
		if result.Err != nil {
			state.Metrics.NavigationRebuildTerminalReason = "error"
			state.InitErr = result.Err
			return
		}
		state.WorldDelta.NavigationSourceOverrides = append([]content.NavigationSourceOverrideDef(nil), result.Delta.NavigationSourceOverrides...)
		state.WorldDelta.NavigationGraphOverrides = append([]content.NavigationGraphOverrideDef(nil), result.Delta.NavigationGraphOverrides...)
		requestStreamedWorldDeltaSave(state)
		for coord, edit := range state.navigationQueuedEdits {
			if edit.Generation <= result.EditGeneration {
				delete(state.navigationQueuedEdits, coord)
			}
		}
		state.Metrics.NavigationRebuildQueuedTileCount = len(state.navigationQueuedEdits)
		if len(state.navigationQueuedEdits) == 0 {
			state.navigationEditQueuedSince = time.Time{}
			state.navigationEditLastQueuedAt = time.Time{}
		}
		if !navigationRebuildChangesResidentTopology(state, result.Result) {
			state.Metrics.NavigationRebuildDiscardedCount++
			state.Metrics.NavigationRebuildDiscardedRevision = result.EditGeneration
			state.Metrics.NavigationRebuildTerminalReason = "topology_unchanged"
			retireNavigationEditBlockers(state, result.EditGeneration)
			break
		}
		state.Metrics.NavigationRebuildPublishedCount++
		state.Metrics.NavigationRebuildPublishedRevision = result.EditGeneration
		state.Metrics.NavigationRebuildTerminalReason = "published"
		for _, override := range result.Result.SourceOverrides {
			delete(state.navigationIgnoredRemovals, override.ChunkCoord)
		}
		reloadStreamedNavigationResidency(state)
		if state.navigationRetireAtLoad == nil {
			state.navigationRetireAtLoad = make(map[uint64]uint64)
		}
		state.navigationRetireAtLoad[state.navigationRequestedGen] = result.EditGeneration
	default:
	}
	select {
	case result := <-state.navigationOverlays:
		state.navigationOverlayActive = false
		if result.RuntimeGeneration != state.Generation {
			break
		}
		if result.LoadGeneration != 0 {
			if result.LoadGeneration != state.navigationRequestedGen {
				break
			}
			if result.Err != nil {
				if result.OverlayGeneration == state.navigationOverlayRequestedGen {
					state.InitErr = result.Err
					return
				}
				break
			}
			commitStreamedNavigationOverlay(state, result)
			state.navigationPendingSources = nil
			state.navigationPendingGraphs = nil
			state.navigationPendingGen = 0
			if generation := state.navigationRetireAtLoad[result.LoadGeneration]; generation != 0 {
				retireNavigationEditBlockers(state, generation)
				delete(state.navigationRetireAtLoad, result.LoadGeneration)
			}
			break
		}
		if result.GraphRevision != state.navigationLoadedGen {
			break
		}
		if result.Err != nil {
			if result.OverlayGeneration == state.navigationOverlayRequestedGen {
				state.InitErr = result.Err
				return
			}
			break
		}
		commitStreamedNavigationOverlay(state, result)
	default:
	}
	if !state.navigationLoadActive &&
		state.navigationLoadedGen != state.navigationRequestedGen &&
		state.navigationPendingGen != state.navigationRequestedGen {
		startStreamedNavigationLoad(state)
	}
	startStreamedNavigationOverlayBuild(state)
}

func requestStreamedWorldDeltaSave(state *StreamedLevelRuntimeState) {
	if state == nil || state.WorldDelta == nil || strings.TrimSpace(state.WorldDeltaPath) == "" {
		return
	}
	state.worldDeltaSaveRequestedGen++
	copy := copyWorldDeltaForNav(state.WorldDelta)
	state.worldDeltaSavePending = &copy
	if !state.worldDeltaSaveActive {
		startStreamedWorldDeltaSave(state)
	}
}

func startStreamedWorldDeltaSave(state *StreamedLevelRuntimeState) {
	if state == nil || state.worldDeltaSaveActive || state.worldDeltaSavePending == nil {
		return
	}
	if state.worldDeltaSaves == nil {
		state.worldDeltaSaves = make(chan streamedWorldDeltaSaveResult, 2)
	}
	generation, runtimeGeneration := state.worldDeltaSaveRequestedGen, state.Generation
	delta, path := state.worldDeltaSavePending, state.WorldDeltaPath
	state.worldDeltaSavePending = nil
	state.worldDeltaSaveActive = true
	state.worldDeltaSaveActiveGen = generation
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		state.worldDeltaSaves <- streamedWorldDeltaSaveResult{
			RuntimeGeneration: runtimeGeneration, Generation: generation, Err: content.SaveWorldDelta(path, delta),
		}
	}()
}

func commitStreamedWorldDeltaSave(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	select {
	case result := <-state.worldDeltaSaves:
		if result.RuntimeGeneration != state.Generation {
			return
		}
		if result.Generation == state.worldDeltaSaveActiveGen {
			state.worldDeltaSaveActive = false
			state.worldDeltaSaveActiveGen = 0
		}
		if result.Err != nil {
			state.InitErr = result.Err
			return
		}
		startStreamedWorldDeltaSave(state)
	default:
	}
}

func saveStreamedWorldDeltaNow(state *StreamedLevelRuntimeState) error {
	if state == nil || state.WorldDelta == nil || strings.TrimSpace(state.WorldDeltaPath) == "" {
		return nil
	}
	if state.worldDeltaSaveActive {
		result := <-state.worldDeltaSaves
		state.worldDeltaSaveActive = false
		state.worldDeltaSaveActiveGen = 0
		if result.RuntimeGeneration == state.Generation && result.Err != nil {
			return result.Err
		}
	}
	state.worldDeltaSavePending = nil
	return content.SaveWorldDelta(state.WorldDeltaPath, state.WorldDelta)
}

func commitStreamedNavigationOverlay(state *StreamedLevelRuntimeState, result streamedNavigationOverlayResult) {
	state.mu.Lock()
	if result.LoadGeneration != 0 {
		state.NavigationSources = result.Sources
		state.NavigationGraphs = result.Graphs
		state.navigationLoadedGen = result.LoadGeneration
	}
	state.navigationQuery = result.Query
	state.navigationDisabled = result.DisabledTraversals
	state.navigationOpenDoors = result.OpenDoors
	state.navigationBlockers = result.Blockers
	state.NavigationRevision++
	state.mu.Unlock()
}

func startStreamedNavigationOverlayBuild(state *StreamedLevelRuntimeState) {
	if state == nil || state.BaseNavManifest == nil || state.navigationOverlayActive {
		return
	}
	pending := state.navigationPendingGen != 0 && state.navigationPendingGen == state.navigationRequestedGen
	if !pending &&
		navigationTraversalSetsEqual(state.navigationDisabled, state.navigationOverlayDisabled) &&
		navigationTraversalSetsEqual(state.navigationOpenDoors, state.navigationOverlayOpenDoors) &&
		navigationBlockersEqual(state.navigationBlockers, state.navigationOverlayBlockers) {
		return
	}
	var sources []content.NavSourceTileDef
	var graphs []content.NavGraphTileDef
	var graphRevision, loadGeneration uint64
	state.mu.RLock()
	previousQuery := state.navigationQuery
	if pending {
		sources = state.navigationPendingSources
		graphs = state.navigationPendingGraphs
		loadGeneration = state.navigationPendingGen
	} else {
		sources = state.NavigationSources
		graphs = state.NavigationGraphs
		graphRevision = state.navigationLoadedGen
	}
	state.mu.RUnlock()
	if !pending && (len(sources) == 0 || len(graphs) == 0) {
		return
	}
	// Resident navigation data is immutable, and runtime shutdown waits for jobs.
	manifest := state.BaseNavManifest
	disabled := copyNavigationTraversalSet(state.navigationOverlayDisabled)
	openDoors := copyNavigationTraversalSet(state.navigationOverlayOpenDoors)
	blockers := copyNavigationBlockers(state.navigationOverlayBlockers)
	runtimeGeneration := state.Generation
	overlayGeneration := state.navigationOverlayRequestedGen
	if state.navigationOverlays == nil {
		state.navigationOverlays = make(chan streamedNavigationOverlayResult, 1)
	}
	state.navigationOverlayActive = true
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		query, err := buildRuntimeNavigationQueryWithDoorOverlays(
			sources, graphs, manifest.ChunkSize, manifest.VoxelResolution,
			manifest.AgentProfiles, disabled, openDoors, blockers, manifest.Carriers,
		)
		if err == nil {
			query = query.WithUpdatedTileEpochs(previousQuery)
		}
		state.navigationOverlays <- streamedNavigationOverlayResult{
			RuntimeGeneration: runtimeGeneration, GraphRevision: graphRevision, LoadGeneration: loadGeneration,
			OverlayGeneration: overlayGeneration,
			Sources:           sources, Graphs: graphs,
			Query: query, DisabledTraversals: disabled, OpenDoors: openDoors, Blockers: blockers, Err: err,
		}
	}()
}

func streamedLevelNavigationOverlaySystem(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || state == nil || !state.Initialized || state.InitErr != nil || state.BaseNavManifest == nil {
		return
	}
	expected := make(map[string]struct{}, len(state.BaseNavManifest.LadderVolumes))
	for _, ladder := range state.BaseNavManifest.LadderVolumes {
		expected[ladder.ID] = struct{}{}
	}
	live := make(map[string]struct{}, len(expected))
	MakeQuery2[LadderVolumeComponent, AuthoredLevelLadderVolumeRefComponent](cmd).Map(func(_ EntityId, _ *LadderVolumeComponent, ref *AuthoredLevelLadderVolumeRefComponent) bool {
		if ref != nil && ref.LevelID == state.LevelID {
			live[ref.LadderVolumeID] = struct{}{}
		}
		return true
	})
	disabled := make(map[string]struct{})
	for id := range expected {
		if _, found := live[id]; !found {
			disabled[id] = struct{}{}
		}
	}
	expectedDoors := make(map[string]struct{}, len(state.BaseNavManifest.Doors))
	for _, door := range state.BaseNavManifest.Doors {
		expectedDoors[door.ID] = struct{}{}
	}
	liveDoors := make(map[string]struct{}, len(expectedDoors))
	openDoors := make(map[string]struct{}, len(expectedDoors))
	MakeQuery2[MovingBrushComponent, AuthoredLevelMovingBrushRefComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent, ref *AuthoredLevelMovingBrushRefComponent) bool {
		if brush == nil || ref == nil || ref.LevelID != state.LevelID {
			return true
		}
		if _, expected := expectedDoors[ref.MovingBrushID]; !expected {
			return true
		}
		liveDoors[ref.MovingBrushID] = struct{}{}
		if MovingBrushFullyOpen(brush) {
			openDoors[ref.MovingBrushID] = struct{}{}
		}
		return true
	})
	for id := range expectedDoors {
		if _, found := liveDoors[id]; !found {
			disabled[id] = struct{}{}
		}
	}
	expectedCarriers := make(map[string]struct{}, len(state.BaseNavManifest.Carriers))
	for _, carrier := range state.BaseNavManifest.Carriers {
		expectedCarriers[carrier.ID] = struct{}{}
	}
	liveCarriers := make(map[string]struct{}, len(expectedCarriers))
	MakeQuery2[MovingBrushComponent, AuthoredLevelMovingBrushRefComponent](cmd).Map(func(_ EntityId, brush *MovingBrushComponent, ref *AuthoredLevelMovingBrushRefComponent) bool {
		if brush == nil || ref == nil || ref.LevelID != state.LevelID {
			return true
		}
		if _, expected := expectedCarriers[ref.MovingBrushID]; expected {
			liveCarriers[ref.MovingBrushID] = struct{}{}
		}
		return true
	})
	for id := range expectedCarriers {
		if _, found := liveCarriers[id]; !found {
			disabled[id] = struct{}{}
		}
	}
	blockers, err := runtimeNavigationBlockers(cmd)
	if err != nil {
		state.InitErr = err
		return
	}
	for id, blocker := range state.navigationEditBlockers {
		blockers[id] = blocker.Blocker
	}
	setStreamedNavigationOverlayDesired(state, disabled, openDoors, blockers)
	startStreamedNavigationOverlayBuild(state)
}

func setStreamedNavigationOverlayDesired(state *StreamedLevelRuntimeState, disabled, openDoors map[string]struct{}, blockers map[string]content.NavBlockerDef) {
	if state == nil ||
		navigationTraversalSetsEqual(disabled, state.navigationOverlayDisabled) &&
			navigationTraversalSetsEqual(openDoors, state.navigationOverlayOpenDoors) &&
			navigationBlockersEqual(blockers, state.navigationOverlayBlockers) {
		return
	}
	state.navigationOverlayDisabled = disabled
	state.navigationOverlayOpenDoors = openDoors
	state.navigationOverlayBlockers = blockers
	state.navigationOverlayRequestedGen++
}

func runtimeNavigationBlockers(cmd *Commands) (map[string]content.NavBlockerDef, error) {
	blockers := make(map[string]content.NavBlockerDef)
	var scanErr error
	MakeQuery2[AABBComponent, NavigationBlockerComponent](cmd).Map(func(_ EntityId, bounds *AABBComponent, marker *NavigationBlockerComponent) bool {
		if bounds == nil || marker == nil || marker.Disabled {
			return true
		}
		id := strings.TrimSpace(marker.ID)
		if id == "" {
			scanErr = fmt.Errorf("navigation blocker id is required")
			return false
		}
		if _, exists := blockers[id]; exists {
			scanErr = fmt.Errorf("duplicate navigation blocker id %q", id)
			return false
		}
		blockers[id] = content.NavBlockerDef{ID: id, Min: content.Vec3(bounds.Min), Max: content.Vec3(bounds.Max)}
		return true
	})
	return blockers, scanErr
}

func streamedLevelRuntimeEditedNavigationSystem(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || state == nil || !state.Initialized || state.InitErr != nil || state.BaseNavManifest == nil {
		return
	}
	commitStreamedNavigationEditAnalysis(state)
	if state.InitErr != nil {
		return
	}
	for _, snapshot := range takeVoxelWorldDirtyChunks(cmd.app, state.BaseWorldID) {
		if snapshot != nil {
			queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
				WorldID: snapshot.WorldID, Coord: snapshot.Coord, ChunkSize: snapshot.ChunkSize,
				VoxelResolution: snapshot.VoxelResolution, Snapshot: snapshot,
			})
		}
	}
	backingDeltaDirty := false
	rt := voxelRtStateFromApp(cmd.app)
	if rt != nil {
		for _, loaded := range state.LoadedChunks {
			for eid := range loaded.ImportedWorldEntities {
				revision, edit, edited := rt.runtimeEditedVoxelEdit(eid)
				if !edited || revision == 0 || state.navigationEditRevisions[eid] >= revision {
					continue
				}
				if backing, ok := voxelBackingForEntity(cmd, eid); ok && backing.Dirty {
					state.recordVoxelBackingRemoval(backing)
					backingDeltaDirty = true
				}
				if item, ok := captureStreamedNavigationEdit(cmd, state, eid, edit); ok {
					queueStreamedNavigationEditAnalysis(state, item)
					state.navigationEditRevisions[eid] = revision
					rt.clearRuntimeEditedVoxelEdit(eid, revision)
				}
			}
		}
	}
	if backingDeltaDirty {
		state.WorldDelta.TerrainChunkOverrides = mapTerrainOverrides(state.terrainOverrideMap)
		state.WorldDelta.ImportedWorldChunkOverrides = mapImportedWorldOverrides(state.importedWorldOverrideMap)
		state.WorldDelta.VoxelBackingRemovals = mapVoxelBackingRemovals(state.voxelBackingRemovalMap)
	}
	now := time.Now()
	if !state.navigationEditAnalysisActive && len(state.navigationEditAnalysisPending) > 0 &&
		(state.navigationEditAnalysisAt.IsZero() || now.Sub(state.navigationEditAnalysisAt) >= navigationEditAnalysisDelay ||
			!state.navigationEditAnalysisSince.IsZero() && now.Sub(state.navigationEditAnalysisSince) >= navigationEditAnalysisMaxAge) {
		startStreamedNavigationEditAnalysis(state)
	}
	if !state.navigationRebuildActive && navigationRebuildReady(state, now) {
		startStreamedNavigationRebuild(cmd, state)
	}
}

func captureStreamedNavigationEdit(cmd *Commands, state *StreamedLevelRuntimeState, eid EntityId, edit runtimeVoxelEdit) (streamedNavigationEditAnalysisItem, bool) {
	ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
	if !ok {
		return streamedNavigationEditAnalysisItem{}, false
	}
	xbm, _, exists := currentVoxelMapForEntity(cmd, eid)
	vmc, vmcOK := voxelModelComponentForEntity(cmd, eid)
	if !exists || !vmcOK || vmc.TerrainChunkSize <= 0 || xbm == nil {
		return streamedNavigationEditAnalysisItem{}, false
	}
	immutable := immutableNavigationVoxelMap(cmd, state, eid, xbm)
	if immutable == nil {
		return streamedNavigationEditAnalysisItem{}, false
	}
	var backing *VoxelBackingComponent
	if live, backed := voxelBackingForEntity(cmd, eid); backed {
		backing = copyNavigationVoxelBacking(live)
	}
	return streamedNavigationEditAnalysisItem{
		Edit: edit, WorldID: ref.WorldID,
		Coord: terrainCoordFromArray(ref.ChunkCoord), ChunkSize: vmc.TerrainChunkSize,
		VoxelResolution: voxelResolutionForEntity(cmd, eid), VoxelMap: immutable, Backing: backing,
	}, true
}

func immutableNavigationVoxelMap(cmd *Commands, state *StreamedLevelRuntimeState, eid EntityId, source *volume.XBrickMap) *volume.XBrickMap {
	if state.navigationVoxelSnapshots == nil {
		state.navigationVoxelSnapshots = make(map[EntityId]navigationVoxelSnapshot)
	}
	if cmd != nil && cmd.app != nil {
		if resource, ok := cmd.app.resources[reflect.TypeOf(VoxelGridCache{})]; ok {
			if cache, ok := resource.(*VoxelGridCache); ok {
				stamp, stamped := cache.BuildStamps[eid]
				if snapshot := cache.Snapshots[eid]; stamped && snapshot != nil && stamp.MapPtr == source && stamp.MapRevision == source.Revision {
					state.navigationVoxelSnapshots[eid] = navigationVoxelSnapshot{Source: source, Revision: source.Revision, Snapshot: snapshot.xbm}
					return snapshot.xbm
				}
			}
		}
	}
	previous := state.navigationVoxelSnapshots[eid]
	var snapshot *volume.XBrickMap
	if previous.Source == source {
		snapshot = source.CopyChangedSectors(previous.Snapshot, previous.Revision)
	} else {
		snapshot = source.CopyChangedSectors(nil, 0)
	}
	state.navigationVoxelSnapshots[eid] = navigationVoxelSnapshot{Source: source, Revision: source.Revision, Snapshot: snapshot}
	return snapshot
}

func copyNavigationVoxelBacking(source *VoxelBackingComponent) *VoxelBackingComponent {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Removals = make(map[[3]int][16]uint32, len(source.Removals))
	for coord, bits := range source.Removals {
		copy.Removals[coord] = bits
	}
	copy.Materials = make(map[[3]int]uint8, len(source.Materials))
	for coord, material := range source.Materials {
		copy.Materials[coord] = material
	}
	copy.surfaceSupportSeeds = nil
	return &copy
}

func queueStreamedNavigationEditAnalysis(state *StreamedLevelRuntimeState, item streamedNavigationEditAnalysisItem) {
	if state == nil || strings.TrimSpace(item.WorldID) == "" {
		return
	}
	if state.navigationEditAnalysisPending == nil {
		state.navigationEditAnalysisPending = make(map[content.TerrainChunkCoordDef]streamedNavigationEditAnalysisItem)
	}
	if previous, found := state.navigationEditAnalysisPending[item.Coord]; found {
		merged := previous.Edit
		merged.include(item.Edit)
		item.Edit = merged
	}
	state.navigationEditAnalysisPending[item.Coord] = item
	now := time.Now()
	if state.navigationEditAnalysisSince.IsZero() {
		state.navigationEditAnalysisSince = now
	}
	state.navigationEditAnalysisAt = now
}

func startStreamedNavigationEditAnalysis(state *StreamedLevelRuntimeState) {
	if state == nil || state.navigationEditAnalysisActive || len(state.navigationEditAnalysisPending) == 0 {
		return
	}
	if state.navigationEditAnalyses == nil {
		state.navigationEditAnalyses = make(chan streamedNavigationEditAnalysisResult, 2)
	}
	coords := make([]content.TerrainChunkCoordDef, 0, len(state.navigationEditAnalysisPending))
	for coord := range state.navigationEditAnalysisPending {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainChunkCoordLessForRuntime(coords[i], coords[j]) })
	items := make([]streamedNavigationEditAnalysisItem, 0, len(coords))
	for _, coord := range coords {
		items = append(items, state.navigationEditAnalysisPending[coord])
	}
	state.navigationEditAnalysisPending = make(map[content.TerrainChunkCoordDef]streamedNavigationEditAnalysisItem)
	state.navigationEditAnalysisSince = time.Time{}
	state.navigationEditAnalysisAt = time.Time{}
	ignored := copyNavigationIgnoredRemovals(state.navigationIgnoredRemovals)
	runtimeGeneration, graphGeneration, graphRequestGeneration := state.Generation, state.navigationLoadedGen, state.navigationRequestedGen
	manifest, sources, graphs, query := state.BaseNavManifest, state.NavigationSources, state.NavigationGraphs, state.navigationQuery
	worldDataDir, worldDeltaPath := state.WorldDataDir, state.WorldDeltaPath
	persistenceMu := &state.runtimeEditPersistenceMu
	state.navigationEditAnalysisActive = true
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		analysisState := &StreamedLevelRuntimeState{
			BaseNavManifest: manifest, NavigationSources: sources, NavigationGraphs: graphs,
			navigationQuery: query, navigationIgnoredRemovals: ignored,
		}
		result := streamedNavigationEditAnalysisResult{
			RuntimeGeneration: runtimeGeneration, GraphGeneration: graphGeneration, GraphRequestGeneration: graphRequestGeneration,
		}
		for _, item := range items {
			snapshot := item.Snapshot
			if snapshot == nil {
				snapshot = importedWorldChunkDefFromXBrickMap(item.WorldID, item.Coord, item.ChunkSize, item.VoxelResolution, item.VoxelMap)
			}
			override := content.ImportedWorldChunkOverrideDef{}
			if item.Backing == nil || item.Backing.OwnerKind != content.VoxelBackingOwnerImportedWorld {
				snapshotPath := filepath.Join(worldDataDir, fmt.Sprintf("imported_%s_%d_%d_%d.gkchunk", sanitizePathSegment(item.WorldID), item.Coord.X, item.Coord.Y, item.Coord.Z))
				persistenceMu.Lock()
				if err := content.SaveImportedWorldChunk(snapshotPath, snapshot); err != nil {
					persistenceMu.Unlock()
					result.Err = err
					break
				}
				persistenceMu.Unlock()
				override = content.ImportedWorldChunkOverrideDef{WorldID: item.WorldID, ChunkCoord: item.Coord, SnapshotPath: content.AuthorDocumentPath(snapshotPath, worldDeltaPath)}
			}
			reason := ""
			switch {
			case !item.Edit.Valid:
				reason = "unknown_edit"
			case item.Edit.Added:
				reason = "voxel_addition"
			default:
				reason = navigationRemovalImpact(analysisState, snapshot, item.Backing, item.Edit)
			}
			if reason != "" && (!item.Edit.Valid || item.Edit.Added) {
				delete(analysisState.navigationIgnoredRemovals, item.Coord)
			}
			result.Items = append(result.Items, streamedNavigationEditAnalysisResultItem{
				Input: item, Snapshot: snapshot, Reason: reason, Override: override,
			})
		}
		result.IgnoredRemovals = analysisState.navigationIgnoredRemovals
		state.navigationEditAnalyses <- result
	}()
}

func commitStreamedNavigationEditAnalysis(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	select {
	case result := <-state.navigationEditAnalyses:
		if result.RuntimeGeneration != state.Generation {
			return
		}
		state.navigationEditAnalysisActive = false
		if result.Err != nil {
			state.InitErr = result.Err
			return
		}
		if state.importedWorldOverrideMap == nil {
			state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
		}
		for _, item := range result.Items {
			if item.Override.WorldID != "" {
				state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(item.Override.WorldID, item.Override.ChunkCoord)] = item.Override
			}
		}
		if state.WorldDelta != nil {
			state.WorldDelta.ImportedWorldChunkOverrides = mapImportedWorldOverrides(state.importedWorldOverrideMap)
			requestStreamedWorldDeltaSave(state)
		}
		if result.GraphGeneration != state.navigationLoadedGen || result.GraphRequestGeneration != state.navigationRequestedGen {
			for _, item := range result.Items {
				input := item.Input
				input.VoxelMap, input.Snapshot = nil, item.Snapshot
				queueStreamedNavigationEditAnalysis(state, input)
			}
			return
		}
		state.navigationIgnoredRemovals = result.IgnoredRemovals
		impactful := make([]streamedNavigationEditAnalysisResultItem, 0, len(result.Items))
		lastReason := ""
		for _, item := range result.Items {
			reason := item.Reason
			if _, alreadyQueued := state.navigationQueuedEdits[item.Snapshot.Coord]; alreadyQueued {
				reason = state.Metrics.NavigationRebuildLastReason
				if reason == "" {
					reason = "coalesced_edit"
				}
			}
			if reason == "" {
				state.Metrics.NavigationEditIgnoredCount++
				continue
			}
			item.Reason = reason
			impactful = append(impactful, item)
			lastReason = reason
		}
		if len(impactful) == 0 {
			return
		}
		state.navigationEditGeneration++
		state.Metrics.NavigationRebuildRequestedRevision = state.navigationEditGeneration
		state.Metrics.NavigationRebuildLastReason = lastReason
		generation, now := state.navigationEditGeneration, time.Now()
		if state.navigationEditQueuedSince.IsZero() {
			state.navigationEditQueuedSince = now
		}
		state.navigationEditLastQueuedAt = now
		for _, item := range impactful {
			state.navigationQueuedEdits[item.Snapshot.Coord] = navigationQueuedEdit{Generation: generation, Snapshot: item.Snapshot}
			if item.Input.Edit.Valid {
				if item.Input.Edit.Added {
					blocker := navigationEditBlockerForBounds(item.Input.Edit, generation)
					state.navigationEditBlockers[blocker.Blocker.ID] = blocker
				}
			} else {
				blocker := navigationEditBlockerForChunk(state.BaseNavManifest, item.Snapshot.Coord, generation)
				state.navigationEditBlockers[blocker.Blocker.ID] = blocker
			}
		}
		state.Metrics.NavigationRebuildQueuedTileCount = len(state.navigationQueuedEdits)
		installNavigationEditBlockers(state)
	default:
	}
}

func copyNavigationIgnoredRemovals(source map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}) map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{} {
	copy := make(map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}, len(source))
	for coord, voxels := range source {
		set := make(map[navigationRemovedVoxel]struct{}, len(voxels))
		for voxel := range voxels {
			set[voxel] = struct{}{}
		}
		copy[coord] = set
	}
	return copy
}

type navigationRemovedVoxel struct{ X, Y, Z int }
type navigationRemovalColumn struct{ X, Z int }
type navigationAcceptedSpan struct {
	cell navigationRemovedVoxel
	ref  content.NavSpanRef
}

type navigationRemovalOccupancy struct {
	chunkSize int
	edited    content.TerrainChunkCoordDef
	snapshot  *content.ImportedWorldChunkDef
	backing   *VoxelBackingComponent
	sources   map[content.TerrainChunkCoordDef]content.NavSourceTileDef
	removed   map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}
}

func newNavigationRemovalOccupancy(sources []content.NavSourceTileDef, snapshot *content.ImportedWorldChunkDef, backing *VoxelBackingComponent, removed map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}) navigationRemovalOccupancy {
	byCoord := make(map[content.TerrainChunkCoordDef]content.NavSourceTileDef, len(sources))
	for _, source := range sources {
		byCoord[source.Coord] = source
	}
	return navigationRemovalOccupancy{chunkSize: snapshot.ChunkSize, edited: snapshot.Coord, snapshot: snapshot, backing: backing, sources: byCoord, removed: removed}
}

func (occupancy navigationRemovalOccupancy) voxel(x, y, z int, blocked bool) (bool, bool) {
	coord := content.TerrainChunkCoordDef{
		X: floorDivVoxelBacking(x, occupancy.chunkSize),
		Y: floorDivVoxelBacking(y, occupancy.chunkSize),
		Z: floorDivVoxelBacking(z, occupancy.chunkSize),
	}
	source, known := occupancy.sources[coord]
	if !known {
		return false, false
	}
	localX, localY, localZ := x-coord.X*occupancy.chunkSize, y-coord.Y*occupancy.chunkSize, z-coord.Z*occupancy.chunkSize
	if !blocked {
		if _, removed := occupancy.removed[coord][navigationRemovedVoxel{x, y, z}]; removed {
			return false, true
		}
	}
	if !blocked && coord == occupancy.edited {
		return navigationSnapshotSolid(occupancy.snapshot, occupancy.backing, localX, localY, localZ), true
	}
	runs := source.SolidRuns
	if blocked {
		runs = source.BlockedRuns
	}
	return navigationVoxelRunsContain(runs, localX, localY, localZ), true
}

func navigationVoxelRunsContain(runs []content.NavVoxelRunDef, x, y, z int) bool {
	start := sort.Search(len(runs), func(i int) bool { return runs[i].X >= x })
	for _, run := range runs[start:] {
		if run.X != x {
			break
		}
		if run.Z < z {
			continue
		}
		if run.Z > z {
			break
		}
		if y >= run.Y && y < run.Y+run.Count {
			return true
		}
	}
	return false
}

// navigationRemovalImpact keeps removal-only edits out of the bake
// queue while old spans retain footprint support and no capsule-sized local
// bridge connects previously unreachable graph areas.
func navigationRemovalImpact(state *StreamedLevelRuntimeState, snapshot *content.ImportedWorldChunkDef, backing *VoxelBackingComponent, edit runtimeVoxelEdit) string {
	if state == nil || state.BaseNavManifest == nil || snapshot == nil || !edit.Valid || edit.Added {
		return "removal_unknown"
	}
	for axis := range 3 {
		if !runtimeNavigationFinite(edit.Min[axis]) || !runtimeNavigationFinite(edit.Max[axis]) || edit.Min[axis] > edit.Max[axis] {
			return "removal_unknown"
		}
	}
	manifest := state.BaseNavManifest
	if manifest.ChunkSize <= 0 || manifest.VoxelResolution <= 0 || snapshot.ChunkSize != manifest.ChunkSize ||
		math.Abs(float64(snapshot.VoxelResolution-manifest.VoxelResolution)) > 1e-6 {
		return "removal_unknown"
	}
	var editedSource *content.NavSourceTileDef
	for i := range state.NavigationSources {
		if state.NavigationSources[i].Coord == snapshot.Coord {
			editedSource = &state.NavigationSources[i]
			break
		}
	}
	if editedSource == nil {
		return "removal_unknown"
	}
	removed := navigationRemovedVoxels(*editedSource, snapshot, backing, edit)
	if len(removed) == 0 {
		return ""
	}
	if state.navigationIgnoredRemovals == nil {
		state.navigationIgnoredRemovals = make(map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{})
	}
	allRemoved := state.navigationIgnoredRemovals[snapshot.Coord]
	if allRemoved == nil {
		allRemoved = make(map[navigationRemovedVoxel]struct{}, len(removed))
		state.navigationIgnoredRemovals[snapshot.Coord] = allRemoved
	}
	for _, voxel := range removed {
		allRemoved[voxel] = struct{}{}
	}
	removed = navigationConnectedRemovedVoxels(state.navigationIgnoredRemovals, snapshot.ChunkSize, removed)
	occupancy := newNavigationRemovalOccupancy(state.NavigationSources, snapshot, backing, state.navigationIgnoredRemovals)
	graphs := make(map[navigationRuntimeGraphKey]*content.NavGraphTileDef, len(state.NavigationGraphs))
	for i := range state.NavigationGraphs {
		graph := &state.NavigationGraphs[i]
		graphs[navigationRuntimeGraphKey{graph.Coord, graph.AgentProfileID}] = graph
	}
	for _, profile := range manifest.AgentProfiles {
		if navigationRemovalBreaksSupport(state.NavigationSources, graphs, occupancy, removed, profile, manifest.VoxelResolution) {
			return "removal_support_lost"
		}
		if navigationRemovalOpensTraversal(state.NavigationSources, graphs, state.navigationQuery, occupancy, removed, profile, manifest.VoxelResolution) {
			return "removal_new_connection"
		}
	}
	if len(manifest.AgentProfiles) == 0 {
		return "removal_unknown"
	}
	return ""
}

func navigationConnectedRemovedVoxels(all map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}, chunkSize int, seeds []navigationRemovedVoxel) []navigationRemovedVoxel {
	directions := [...]navigationRemovedVoxel{{X: -1}, {X: 1}, {Y: -1}, {Y: 1}, {Z: -1}, {Z: 1}}
	visited := make(map[navigationRemovedVoxel]struct{}, len(seeds))
	queue := append([]navigationRemovedVoxel(nil), seeds...)
	for _, seed := range queue {
		visited[seed] = struct{}{}
	}
	for len(queue) > 0 {
		voxel := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for _, direction := range directions {
			neighbor := navigationRemovedVoxel{voxel.X + direction.X, voxel.Y + direction.Y, voxel.Z + direction.Z}
			coord := content.TerrainChunkCoordDef{
				X: floorDivVoxelBacking(neighbor.X, chunkSize),
				Y: floorDivVoxelBacking(neighbor.Y, chunkSize),
				Z: floorDivVoxelBacking(neighbor.Z, chunkSize),
			}
			if _, removed := all[coord][neighbor]; !removed {
				continue
			}
			if _, seen := visited[neighbor]; seen {
				continue
			}
			visited[neighbor] = struct{}{}
			queue = append(queue, neighbor)
		}
	}
	result := make([]navigationRemovedVoxel, 0, len(visited))
	for voxel := range visited {
		result = append(result, voxel)
	}
	return result
}

func navigationRemovedVoxels(source content.NavSourceTileDef, snapshot *content.ImportedWorldChunkDef, backing *VoxelBackingComponent, edit runtimeVoxelEdit) []navigationRemovedVoxel {
	resolution := source.VoxelResolution
	origin := [3]float32{
		float32(source.Coord.X*source.ChunkSize) * resolution,
		float32(source.Coord.Y*source.ChunkSize) * resolution,
		float32(source.Coord.Z*source.ChunkSize) * resolution,
	}
	localMin, localMax := [3]int{}, [3]int{}
	for axis := range 3 {
		localMin[axis] = max(0, int(math.Floor(float64((edit.Min[axis]-origin[axis])/resolution)))-1)
		localMax[axis] = min(source.ChunkSize-1, int(math.Ceil(float64((edit.Max[axis]-origin[axis])/resolution)))+1)
	}
	removed := make([]navigationRemovedVoxel, 0, 16)
	first := sort.Search(len(source.SolidRuns), func(i int) bool { return source.SolidRuns[i].X >= localMin[0] })
	for _, run := range source.SolidRuns[first:] {
		if run.X > localMax[0] {
			break
		}
		if run.Z < localMin[2] || run.Z > localMax[2] {
			continue
		}
		start, end := max(run.Y, localMin[1]), min(run.Y+run.Count-1, localMax[1])
		for y := start; y <= end; y++ {
			if navigationSnapshotSolid(snapshot, backing, run.X, y, run.Z) {
				continue
			}
			removed = append(removed, navigationRemovedVoxel{
				X: source.Coord.X*source.ChunkSize + run.X,
				Y: source.Coord.Y*source.ChunkSize + y,
				Z: source.Coord.Z*source.ChunkSize + run.Z,
			})
		}
	}
	return removed
}

func navigationRemovalBreaksSupport(sources []content.NavSourceTileDef, graphs map[navigationRuntimeGraphKey]*content.NavGraphTileDef, occupancy navigationRemovalOccupancy, removed []navigationRemovedVoxel, profile content.NavAgentProfileDef, resolution float32) bool {
	probeRadius := max(profile.Radius*0.5, float32(0.05))
	margin := int(math.Ceil(float64(probeRadius/resolution))) + 1
	minX, maxX, minY, maxY, minZ, maxZ := removed[0].X, removed[0].X, removed[0].Y, removed[0].Y, removed[0].Z, removed[0].Z
	for _, voxel := range removed[1:] {
		minX, maxX = min(minX, voxel.X), max(maxX, voxel.X)
		minY, maxY = min(minY, voxel.Y), max(maxY, voxel.Y)
		minZ, maxZ = min(minZ, voxel.Z), max(maxZ, voxel.Z)
	}
	stepLayers := int(math.Floor(float64(profile.StepHeight / resolution)))
	for _, source := range sources {
		graph := graphs[navigationRuntimeGraphKey{source.Coord, profile.ID}]
		if graph == nil {
			continue
		}
		originX, originY, originZ := source.Coord.X*source.ChunkSize, source.Coord.Y*source.ChunkSize, source.Coord.Z*source.ChunkSize
		for _, span := range navigationSourceSpansInX(source, minX-margin, maxX+margin) {
			spanX, spanY, spanZ := originX+span.X, originY+span.Y, originZ+span.Z
			if spanX < minX-margin || spanX > maxX+margin || spanZ < minZ-margin || spanZ > maxZ+margin ||
				spanY-1 < minY-stepLayers || spanY-1 > maxY+stepLayers || !navigationGraphAcceptsSpan(*graph, span.ID) {
				continue
			}
			if solid, known := occupancy.voxel(spanX, spanY-1, spanZ, false); !known || solid {
				continue
			}
			supported := false
			for _, offset := range CharacterGroundProbeOffsets(profile.Radius) {
				x := int(math.Floor(float64(float32(spanX) + 0.5 + offset.X()/resolution)))
				z := int(math.Floor(float64(float32(spanZ) + 0.5 + offset.Z()/resolution)))
				for y := spanY - 1; y >= spanY-1-stepLayers; y-- {
					if solid, known := occupancy.voxel(x, y, z, false); known && solid {
						supported = true
						break
					}
				}
				if supported {
					break
				}
			}
			if !supported {
				return true
			}
		}
	}
	return false
}

func navigationRemovalOpensTraversal(sources []content.NavSourceTileDef, graphs map[navigationRuntimeGraphKey]*content.NavGraphTileDef, query *content.NavGraphQuery, occupancy navigationRemovalOccupancy, removed []navigationRemovedVoxel, profile content.NavAgentProfileDef, resolution float32) bool {
	radiusCells := int(math.Ceil(float64(profile.Radius/resolution))) + 1
	heightLayers := int(math.Ceil(float64(profile.Height / resolution)))
	stepLayers := int(math.Floor(float64(profile.StepHeight / resolution)))
	minX, maxX, minY, maxY, minZ, maxZ := removed[0].X, removed[0].X, removed[0].Y, removed[0].Y, removed[0].Z, removed[0].Z
	for _, voxel := range removed[1:] {
		minX, maxX = min(minX, voxel.X), max(maxX, voxel.X)
		minY, maxY = min(minY, voxel.Y), max(maxY, voxel.Y)
		minZ, maxZ = min(minZ, voxel.Z), max(maxZ, voxel.Z)
	}

	accepted := make(map[navigationRemovalColumn][]navigationAcceptedSpan)
	candidates := make(map[navigationRemovedVoxel]struct{}, len(removed)*2)
	for _, source := range sources {
		graph := graphs[navigationRuntimeGraphKey{source.Coord, profile.ID}]
		originX, originY, originZ := source.Coord.X*source.ChunkSize, source.Coord.Y*source.ChunkSize, source.Coord.Z*source.ChunkSize
		for _, span := range navigationSourceSpansInX(source, minX-radiusCells-1, maxX+radiusCells+1) {
			cell := navigationRemovedVoxel{X: originX + span.X, Y: originY + span.Y, Z: originZ + span.Z}
			if graph != nil && navigationGraphAcceptsSpan(*graph, span.ID) {
				if cell.X >= minX-radiusCells-1 && cell.X <= maxX+radiusCells+1 && cell.Z >= minZ-radiusCells-1 && cell.Z <= maxZ+radiusCells+1 &&
					cell.Y >= minY-heightLayers-stepLayers && cell.Y <= maxY+stepLayers {
					column := navigationRemovalColumn{cell.X, cell.Z}
					accepted[column] = append(accepted[column], navigationAcceptedSpan{cell: cell, ref: content.NavSpanRef{Tile: source.Coord, Span: span.ID}})
				}
				continue
			}
			if cell.X >= minX-radiusCells && cell.X <= maxX+radiusCells && cell.Z >= minZ-radiusCells && cell.Z <= maxZ+radiusCells &&
				cell.Y >= minY-heightLayers && cell.Y <= maxY {
				candidates[cell] = struct{}{}
			}
		}
	}
	for _, voxel := range removed {
		candidates[voxel] = struct{}{}
	}

	novel := make([]navigationRemovedVoxel, 0, len(candidates))
	byColumn := make(map[navigationRemovalColumn][]navigationRemovedVoxel)
	for cell := range candidates {
		if !navigationRemovalCellWalkable(occupancy, cell, profile, resolution) || navigationRemovalCoveredByAcceptedSpan(accepted[navigationRemovalColumn{cell.X, cell.Z}], cell.Y, stepLayers) {
			continue
		}
		novel = append(novel, cell)
		column := navigationRemovalColumn{cell.X, cell.Z}
		byColumn[column] = append(byColumn[column], cell)
	}
	if len(novel) == 0 {
		return false
	}

	directions := [...]navigationRemovalColumn{{X: -1}, {X: 1}, {Z: -1}, {Z: 1}}
	visited := make(map[navigationRemovedVoxel]struct{}, len(novel))
	for _, seed := range novel {
		if _, seen := visited[seed]; seen {
			continue
		}
		queue := []navigationRemovedVoxel{seed}
		visited[seed] = struct{}{}
		contacts := make(map[content.NavSpanRef]struct{})
		for len(queue) > 0 {
			cell := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			for _, direction := range directions {
				column := navigationRemovalColumn{cell.X + direction.X, cell.Z + direction.Z}
				for _, other := range byColumn[column] {
					if navigationAbsInt(other.Y-cell.Y) > stepLayers {
						continue
					}
					if _, seen := visited[other]; !seen {
						visited[other] = struct{}{}
						queue = append(queue, other)
					}
				}
				for _, old := range accepted[column] {
					if navigationAbsInt(old.cell.Y-cell.Y) <= stepLayers {
						contacts[old.ref] = struct{}{}
					}
				}
			}
		}
		if navigationRemovalConnectsSeparatedSpans(query, contacts) {
			return true
		}
	}
	return false
}

func navigationRemovalCellWalkable(occupancy navigationRemovalOccupancy, cell navigationRemovedVoxel, profile content.NavAgentProfileDef, resolution float32) bool {
	heightLayers := int(math.Ceil(float64(profile.Height / resolution)))
	stepLayers := int(math.Floor(float64(profile.StepHeight / resolution)))
	if solid, known := occupancy.voxel(cell.X, cell.Y-1, cell.Z, false); !known || !solid {
		return false
	}
	for y := cell.Y; y < cell.Y+heightLayers; y++ {
		if solid, known := occupancy.voxel(cell.X, y, cell.Z, false); !known || solid {
			return false
		}
	}
	radiusCells := int(math.Ceil(float64(profile.Radius/resolution))) + 1
	radiusSquared := profile.Radius * profile.Radius
	for dx := -radiusCells; dx <= radiusCells; dx++ {
		for dz := -radiusCells; dz <= radiusCells; dz++ {
			axisX, axisZ := max(0, 2*navigationAbsInt(dx)-1), max(0, 2*navigationAbsInt(dz)-1)
			distanceSquared := float32(axisX*axisX+axisZ*axisZ) * resolution * resolution * 0.25
			if distanceSquared+1e-6 >= radiusSquared {
				continue
			}
			for y := cell.Y + stepLayers; y < cell.Y+heightLayers; y++ {
				if solid, known := occupancy.voxel(cell.X+dx, y, cell.Z+dz, false); !known || solid {
					return false
				}
			}
			for y := cell.Y; y < cell.Y+heightLayers; y++ {
				if blocked, known := occupancy.voxel(cell.X+dx, y, cell.Z+dz, true); !known || blocked {
					return false
				}
			}
		}
	}
	return true
}

func navigationRemovalConnectsSeparatedSpans(query *content.NavGraphQuery, contacts map[content.NavSpanRef]struct{}) bool {
	if len(contacts) < 2 {
		return false
	}
	refs := make([]content.NavSpanRef, 0, len(contacts))
	for ref := range contacts {
		refs = append(refs, ref)
	}
	if query == nil || len(query.ReachableSpansIgnoringBlockers(refs[0], refs)) != len(refs) {
		return true
	}
	for _, ref := range refs[1:] {
		if _, reachable := query.ReachableSpansIgnoringBlockers(ref, refs[:1])[refs[0]]; !reachable {
			return true
		}
	}
	return false
}

func navigationRemovalCoveredByAcceptedSpan(spans []navigationAcceptedSpan, y, stepLayers int) bool {
	for _, span := range spans {
		if navigationAbsInt(span.cell.Y-y) <= stepLayers {
			return true
		}
	}
	return false
}

func navigationAbsInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func navigationSourceSpansInX(source content.NavSourceTileDef, globalMin, globalMax int) []content.NavSpanDef {
	originX := source.Coord.X * source.ChunkSize
	localMin, localMax := globalMin-originX, globalMax-originX
	start := sort.Search(len(source.Spans), func(i int) bool { return source.Spans[i].X >= localMin })
	end := start + sort.Search(len(source.Spans)-start, func(i int) bool { return source.Spans[start+i].X > localMax })
	return source.Spans[start:end]
}

func navigationGraphAcceptsSpan(graph content.NavGraphTileDef, id uint32) bool {
	index := sort.Search(len(graph.SpanIDs), func(i int) bool { return graph.SpanIDs[i] >= id })
	return index < len(graph.SpanIDs) && graph.SpanIDs[index] == id
}

func navigationSnapshotSolid(snapshot *content.ImportedWorldChunkDef, backing *VoxelBackingComponent, x, y, z int) bool {
	if snapshot == nil || x < 0 || y < 0 || z < 0 || x >= snapshot.ChunkSize || y >= snapshot.ChunkSize || z >= snapshot.ChunkSize {
		return false
	}
	if backing != nil && backing.removed([3]int{x, y, z}) {
		return false
	}
	index := sort.Search(len(snapshot.Voxels), func(i int) bool {
		voxel := snapshot.Voxels[i]
		return voxel.X > x || voxel.X == x && (voxel.Y > y || voxel.Y == y && voxel.Z >= z)
	})
	if index < len(snapshot.Voxels) && snapshot.Voxels[index].X == x && snapshot.Voxels[index].Y == y && snapshot.Voxels[index].Z == z && snapshot.Voxels[index].Value != 0 {
		return true
	}
	if backing == nil || backing.Provider == nil {
		return false
	}
	origin := [3]int{backing.ChunkCoord[0] * backing.ChunkSize, backing.ChunkCoord[1] * backing.ChunkSize, backing.ChunkCoord[2] * backing.ChunkSize}
	return backing.Provider.VoxelValue([3]int{origin[0] + x, origin[1] + y, origin[2] + z}) != 0
}

func navigationRebuildReady(state *StreamedLevelRuntimeState, now time.Time) bool {
	if state == nil || len(state.navigationQueuedEdits) == 0 {
		return false
	}
	if state.navigationEditQueuedSince.IsZero() || state.navigationEditLastQueuedAt.IsZero() {
		return true
	}
	return now.Sub(state.navigationEditLastQueuedAt) >= navigationRebuildQuietPeriod || now.Sub(state.navigationEditQueuedSince) >= navigationRebuildMaxDelay
}

type navigationRuntimeGraphKey struct {
	Coord   content.TerrainChunkCoordDef
	Profile string
}

func navigationRebuildChangesResidentTopology(state *StreamedLevelRuntimeState, result content.NavGraphDeltaBakeResult) bool {
	if state == nil || state.navigationLoadActive || state.navigationPendingGen != 0 || state.navigationLoadedGen != state.navigationRequestedGen {
		return true
	}

	sources := make(map[content.TerrainChunkCoordDef]content.NavSourceTileDef, len(state.NavigationSources))
	for _, source := range state.NavigationSources {
		sources[source.Coord] = source
	}
	newSources := make(map[content.TerrainChunkCoordDef]content.NavSourceTileDef, len(result.SourceTiles))
	for _, source := range result.SourceTiles {
		newSources[source.Coord] = source
	}
	for _, override := range result.SourceOverrides {
		if _, resident := state.navigationDesired[override.ChunkCoord]; !resident {
			continue
		}
		if override.Empty {
			delete(sources, override.ChunkCoord)
			continue
		}
		source, ok := newSources[override.ChunkCoord]
		if !ok {
			return true
		}
		sources[override.ChunkCoord] = source
	}

	graphs := make(map[navigationRuntimeGraphKey]content.NavGraphTileDef, len(state.NavigationGraphs))
	for _, graph := range state.NavigationGraphs {
		graphs[navigationRuntimeGraphKey{graph.Coord, graph.AgentProfileID}] = graph
	}
	newGraphs := make(map[navigationRuntimeGraphKey]content.NavGraphTileDef, len(result.GraphTiles))
	for _, graph := range result.GraphTiles {
		newGraphs[navigationRuntimeGraphKey{graph.Coord, graph.AgentProfileID}] = graph
	}
	for _, override := range result.GraphOverrides {
		if _, resident := state.navigationDesired[override.ChunkCoord]; !resident {
			continue
		}
		key := navigationRuntimeGraphKey{override.ChunkCoord, override.AgentProfileID}
		if override.Empty {
			delete(graphs, key)
			continue
		}
		graph, ok := newGraphs[key]
		if !ok {
			return true
		}
		graphs[key] = graph
	}

	if len(sources) != len(state.NavigationSources) || len(graphs) != len(state.NavigationGraphs) {
		return true
	}
	for _, old := range state.NavigationSources {
		candidate, ok := sources[old.Coord]
		if !ok || !navigationSliceEqual(old.Spans, candidate.Spans) {
			return true
		}
	}
	resident := make(map[navigationRuntimeGraphKey]struct{}, len(graphs))
	for key := range graphs {
		resident[key] = struct{}{}
	}
	for _, old := range state.NavigationGraphs {
		candidate, ok := graphs[navigationRuntimeGraphKey{old.Coord, old.AgentProfileID}]
		if !ok || !navigationRuntimeGraphsEqual(old, trimNavigationRuntimeGraph(candidate, resident)) {
			return true
		}
	}
	return false
}

func trimNavigationRuntimeGraph(graph content.NavGraphTileDef, resident map[navigationRuntimeGraphKey]struct{}) content.NavGraphTileDef {
	spans := make([]content.NavSpanTransitionDef, 0, len(graph.SpanTransitions))
	for _, transition := range graph.SpanTransitions {
		if transition.To.Tile == graph.Coord {
			spans = append(spans, transition)
			continue
		}
		if _, ok := resident[navigationRuntimeGraphKey{transition.To.Tile, graph.AgentProfileID}]; ok {
			spans = append(spans, transition)
		}
	}
	regions := make([]content.NavRegionTransitionDef, 0, len(graph.Transitions))
	for _, transition := range graph.Transitions {
		if transition.ToTile != graph.Coord {
			if _, ok := resident[navigationRuntimeGraphKey{transition.ToTile, graph.AgentProfileID}]; !ok {
				continue
			}
		}
		transition.ID = uint32(len(regions))
		regions = append(regions, transition)
	}
	graph.SpanTransitions, graph.Transitions = spans, regions
	return graph
}

func navigationRuntimeGraphsEqual(a, b content.NavGraphTileDef) bool {
	return navigationSliceEqual(a.SpanIDs, b.SpanIDs) &&
		navigationSliceEqual(a.SpanTransitions, b.SpanTransitions) &&
		navigationSliceEqual(a.Regions, b.Regions) &&
		navigationSliceEqual(a.Transitions, b.Transitions)
}

func navigationSliceEqual[T any](a, b []T) bool {
	return len(a) == len(b) && (len(a) == 0 || reflect.DeepEqual(a, b))
}

func startStreamedNavigationRebuild(cmd *Commands, state *StreamedLevelRuntimeState) {
	dirty := make([]content.TerrainChunkCoordDef, 0, len(state.navigationQueuedEdits))
	overrides := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(state.navigationQueuedEdits))
	for coord, edit := range state.navigationQueuedEdits {
		dirty = append(dirty, coord)
		overrides[coord] = edit.Snapshot
	}
	sort.Slice(dirty, func(i, j int) bool {
		if dirty[i].Y != dirty[j].Y {
			return dirty[i].Y < dirty[j].Y
		}
		if dirty[i].X != dirty[j].X {
			return dirty[i].X < dirty[j].X
		}
		return dirty[i].Z < dirty[j].Z
	})
	chunks, err := loadNavigationRebuildChunks(cmd, state, dirty, overrides)
	if err != nil {
		state.InitErr = err
		return
	}
	delta := copyWorldDeltaForNav(state.WorldDelta)
	manifest := copyNavGraphManifest(state.BaseNavManifest)
	runtimeGeneration := state.Generation
	editGeneration := state.navigationEditGeneration
	deltaPath, manifestPath := state.WorldDeltaPath, state.BaseNavManifestPath
	state.navigationRebuildActive = true
	state.Metrics.NavigationRebuildActive = true
	state.Metrics.NavigationRebuildStartedCount++
	state.Metrics.NavigationRebuildLastDirtyCount = len(dirty)
	state.navigationEditQueuedSince = time.Time{}
	state.navigationEditLastQueuedAt = time.Time{}
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		batchID := fmt.Sprintf("%d-%d", runtimeGeneration, editGeneration)
		result, err := content.SaveNavGraphDeltaBatchForImportedWorldChunks(deltaPath, &delta, manifest, manifestPath, chunks, dirty, batchID)
		if err != nil {
			err = fmt.Errorf("rebuild navigation graph delta: %w", err)
		}
		state.navigationRebuilds <- streamedNavigationRebuildResult{RuntimeGeneration: runtimeGeneration, EditGeneration: editGeneration, Delta: delta, Result: result, Err: err}
	}()
}

func navigationEditBlockerForChunk(manifest *content.NavGraphManifestDef, coord content.TerrainChunkCoordDef, generation uint64) navigationEditBlocker {
	tileSize := float32(manifest.ChunkSize) * manifest.VoxelResolution
	horizontal, vertical := float32(0), float32(0)
	for _, profile := range manifest.AgentProfiles {
		horizontal = maxf(horizontal, profile.Radius)
		for _, extent := range [...]float32{profile.Height, profile.StepHeight, profile.MaxDropHeight, profile.MaxJumpRise, profile.MaxVaultHeight, profile.MaxMantleHeight} {
			vertical = maxf(vertical, extent)
		}
	}
	minimum := content.Vec3{float32(coord.X)*tileSize - horizontal, float32(coord.Y)*tileSize - vertical, float32(coord.Z)*tileSize - horizontal}
	maximum := content.Vec3{float32(coord.X+1)*tileSize + horizontal, float32(coord.Y+1)*tileSize + vertical, float32(coord.Z+1)*tileSize + horizontal}
	id := fmt.Sprintf("__nav_edit:%d:%d:%d", coord.X, coord.Y, coord.Z)
	return navigationEditBlocker{Generation: generation, Blocker: content.NavBlockerDef{ID: id, Min: minimum, Max: maximum}}
}

func navigationEditBlockerForBounds(edit runtimeVoxelEdit, generation uint64) navigationEditBlocker {
	id := fmt.Sprintf("__nav_edit:%d:%08x:%08x:%08x:%08x:%08x:%08x", generation,
		math.Float32bits(edit.Min[0]), math.Float32bits(edit.Min[1]), math.Float32bits(edit.Min[2]),
		math.Float32bits(edit.Max[0]), math.Float32bits(edit.Max[1]), math.Float32bits(edit.Max[2]))
	return navigationEditBlocker{Generation: generation, Blocker: content.NavBlockerDef{ID: id, Min: content.Vec3(edit.Min), Max: content.Vec3(edit.Max)}}
}

func installNavigationEditBlockers(state *StreamedLevelRuntimeState) {
	if state.navigationEditBlockers == nil {
		state.navigationEditBlockers = make(map[string]navigationEditBlocker)
	}
	blockers := copyNavigationBlockers(state.navigationOverlayBlockers)
	for id, blocker := range state.navigationEditBlockers {
		blockers[id] = blocker.Blocker
	}
	setStreamedNavigationOverlayDesired(state, copyNavigationTraversalSet(state.navigationOverlayDisabled), copyNavigationTraversalSet(state.navigationOverlayOpenDoors), blockers)
	startStreamedNavigationOverlayBuild(state)
}

func retireNavigationEditBlockers(state *StreamedLevelRuntimeState, generation uint64) {
	for id, blocker := range state.navigationEditBlockers {
		if blocker.Generation <= generation {
			delete(state.navigationEditBlockers, id)
		}
	}
	blockers := copyNavigationBlockers(state.navigationOverlayBlockers)
	for id := range blockers {
		if strings.HasPrefix(id, "__nav_edit:") {
			delete(blockers, id)
		}
	}
	for id, blocker := range state.navigationEditBlockers {
		blockers[id] = blocker.Blocker
	}
	setStreamedNavigationOverlayDesired(state, copyNavigationTraversalSet(state.navigationOverlayDisabled), copyNavigationTraversalSet(state.navigationOverlayOpenDoors), blockers)
}

func loadNavigationRebuildChunks(cmd *Commands, state *StreamedLevelRuntimeState, dirty []content.TerrainChunkCoordDef, snapshots map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) (map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, error) {
	coords := make(map[content.TerrainChunkCoordDef]struct{})
	for _, first := range content.ExpandNavGraphDirtyTileCoords(dirty) {
		for _, second := range content.ExpandNavGraphDirtyTileCoords([]content.TerrainChunkCoordDef{first}) {
			coords[second] = struct{}{}
		}
	}
	chunks := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef)
	for coord := range coords {
		if snapshot := snapshots[coord]; snapshot != nil {
			chunks[coord] = snapshot
			continue
		}
		if snapshot := loadedImportedWorldChunkSnapshotForNavigationCoord(cmd, state, state.BaseWorldID, coord); snapshot != nil {
			chunks[coord] = snapshot
			continue
		}
		if override, ok := state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(state.BaseWorldID, coord)]; ok {
			chunk, err := state.Loader.LoadImportedWorldChunk(content.ResolveDocumentPath(override.SnapshotPath, state.WorldDeltaPath))
			if err != nil {
				return nil, err
			}
			chunks[coord] = chunk
			continue
		}
		entry, ok := state.ImportedWorldEntries[chunkCoordFromTerrain(coord)]
		if !ok {
			continue
		}
		if entry.NonEmptyVoxelCount == 0 {
			chunks[coord] = &content.ImportedWorldChunkDef{WorldID: state.BaseWorldID, Coord: coord, ChunkSize: state.BaseNavManifest.ChunkSize, VoxelResolution: state.BaseNavManifest.VoxelResolution}
			continue
		}
		worldPath := content.ResolveDocumentPath(state.Level.BaseWorld.ManifestPath, state.LevelPath)
		chunk, err := state.Loader.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(entry, worldPath))
		if err != nil {
			return nil, err
		}
		chunks[coord] = chunk
	}
	return chunks, nil
}

func notifyImportedWorldChunkDirty(cmd *Commands, eid EntityId, xbm *volume.XBrickMap) {
	if cmd == nil || cmd.app == nil || xbm == nil {
		return
	}
	resource := voxelWorldDirtyChunksFromApp(cmd.app)
	if resource == nil {
		return
	}
	ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
	if !ok {
		return
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok || vmc.TerrainChunkSize <= 0 {
		return
	}
	coord := terrainCoordFromArray(ref.ChunkCoord)
	snapshot := importedWorldChunkDefFromXBrickMap(ref.WorldID, coord, vmc.TerrainChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
	resource.mu.Lock()
	resource.Imported[voxelWorldDirtyChunkKey{ref.WorldID, coord}] = snapshot
	resource.mu.Unlock()
}

func takeVoxelWorldDirtyChunks(app *App, worldID string) []*content.ImportedWorldChunkDef {
	resource := voxelWorldDirtyChunksFromApp(app)
	if resource == nil {
		return nil
	}
	resource.mu.Lock()
	defer resource.mu.Unlock()
	result := make([]*content.ImportedWorldChunkDef, 0, len(resource.Imported))
	for key, snapshot := range resource.Imported {
		if worldID != "" && key.WorldID != worldID {
			continue
		}
		result = append(result, snapshot)
		delete(resource.Imported, key)
	}
	return result
}

func voxelWorldDirtyChunksFromApp(app *App) *VoxelWorldDirtyChunks {
	if app == nil {
		return nil
	}
	resource, ok := app.resources[reflect.TypeOf(VoxelWorldDirtyChunks{})]
	if !ok {
		return nil
	}
	return resource.(*VoxelWorldDirtyChunks)
}

func loadedImportedWorldChunkSnapshotForNavigation(cmd *Commands, state *StreamedLevelRuntimeState, eid EntityId) *content.ImportedWorldChunkDef {
	ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
	if !ok {
		return nil
	}
	return loadedImportedWorldChunkSnapshotForNavigationCoord(cmd, state, ref.WorldID, terrainCoordFromArray(ref.ChunkCoord))
}

func loadedImportedWorldChunkSnapshotForNavigationCoord(cmd *Commands, state *StreamedLevelRuntimeState, worldID string, coord content.TerrainChunkCoordDef) *content.ImportedWorldChunkDef {
	loaded := state.LoadedChunks[chunkCoordFromTerrain(coord)]
	if loaded == nil {
		return nil
	}
	for eid := range loaded.ImportedWorldEntities {
		ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
		if !ok || ref.WorldID != worldID || terrainCoordFromArray(ref.ChunkCoord) != coord {
			continue
		}
		xbm, _, exists := currentVoxelMapForEntity(cmd, eid)
		vmc, vmcOK := voxelModelComponentForEntity(cmd, eid)
		if !exists || !vmcOK || vmc.TerrainChunkSize <= 0 {
			continue
		}
		return importedWorldChunkDefFromXBrickMap(worldID, coord, vmc.TerrainChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
	}
	return nil
}

func uniqueImportedWorldSnapshots(input []*content.ImportedWorldChunkDef) []*content.ImportedWorldChunkDef {
	byCoord := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(input))
	for _, snapshot := range input {
		if snapshot != nil {
			byCoord[snapshot.Coord] = snapshot
		}
	}
	coords := make([]content.TerrainChunkCoordDef, 0, len(byCoord))
	for coord := range byCoord {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainChunkCoordLessForRuntime(coords[i], coords[j]) })
	result := make([]*content.ImportedWorldChunkDef, 0, len(coords))
	for _, coord := range coords {
		result = append(result, byCoord[coord])
	}
	return result
}

func copyNavGraphManifest(source *content.NavGraphManifestDef) *content.NavGraphManifestDef {
	if source == nil {
		return nil
	}
	copy := *source
	copy.AgentProfiles = append([]content.NavAgentProfileDef(nil), source.AgentProfiles...)
	for i := range copy.AgentProfiles {
		copy.AgentProfiles[i].Capabilities = append([]string(nil), source.AgentProfiles[i].Capabilities...)
	}
	copy.LadderVolumes = append([]content.LevelLadderVolumeDef(nil), source.LadderVolumes...)
	copy.Doors = append([]content.NavDoorDef(nil), source.Doors...)
	copy.Carriers = append([]content.NavCarrierDef(nil), source.Carriers...)
	for i := range copy.Carriers {
		copy.Carriers[i].Stops = append([]content.NavCarrierStopDef(nil), source.Carriers[i].Stops...)
		for stop := range copy.Carriers[i].Stops {
			copy.Carriers[i].Stops[stop].Controllers = append([]content.NavCarrierControllerDef(nil), source.Carriers[i].Stops[stop].Controllers...)
		}
	}
	copy.SourceTiles = append([]content.NavSourceTileEntryDef(nil), source.SourceTiles...)
	copy.GraphTiles = append([]content.NavGraphTileEntryDef(nil), source.GraphTiles...)
	return &copy
}

func copyWorldDeltaForNav(source *content.WorldDeltaDef) content.WorldDeltaDef {
	if source == nil {
		return content.WorldDeltaDef{}
	}
	copy := *source
	copy.NavigationSourceOverrides = append([]content.NavigationSourceOverrideDef(nil), source.NavigationSourceOverrides...)
	copy.NavigationGraphOverrides = append([]content.NavigationGraphOverrideDef(nil), source.NavigationGraphOverrides...)
	return copy
}

func copyTerrainCoordSet(source map[content.TerrainChunkCoordDef]struct{}) map[content.TerrainChunkCoordDef]struct{} {
	copy := make(map[content.TerrainChunkCoordDef]struct{}, len(source))
	for coord := range source {
		copy[coord] = struct{}{}
	}
	return copy
}

func copyNavigationTraversalSet(source map[string]struct{}) map[string]struct{} {
	copy := make(map[string]struct{}, len(source))
	for id := range source {
		copy[id] = struct{}{}
	}
	return copy
}

func navigationTraversalSetsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if _, found := b[id]; !found {
			return false
		}
	}
	return true
}

func copyNavigationBlockers(source map[string]content.NavBlockerDef) map[string]content.NavBlockerDef {
	copy := make(map[string]content.NavBlockerDef, len(source))
	for id, blocker := range source {
		copy[id] = blocker
	}
	return copy
}

func navigationBlockersEqual(a, b map[string]content.NavBlockerDef) bool {
	if len(a) != len(b) {
		return false
	}
	for id, blocker := range a {
		if b[id] != blocker {
			return false
		}
	}
	return true
}

func sortedNavigationBlockers(source map[string]content.NavBlockerDef) []content.NavBlockerDef {
	ids := make([]string, 0, len(source))
	for id := range source {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	blockers := make([]content.NavBlockerDef, 0, len(ids))
	for _, id := range ids {
		blockers = append(blockers, source[id])
	}
	return blockers
}

func sortedTerrainCoords(source map[content.TerrainChunkCoordDef]struct{}) []content.TerrainChunkCoordDef {
	coords := make([]content.TerrainChunkCoordDef, 0, len(source))
	for coord := range source {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainChunkCoordLessForRuntime(coords[i], coords[j]) })
	return coords
}

func navCoordSetsEqual(a, b map[content.TerrainChunkCoordDef]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for coord := range a {
		if _, ok := b[coord]; !ok {
			return false
		}
	}
	return true
}
