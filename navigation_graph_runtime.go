package gekko

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

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
	RuntimeGeneration  uint64
	Generation         uint64
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
	Delta             content.WorldDeltaDef
	Result            content.NavGraphDeltaBakeResult
	Err               error
}

type RuntimeNavigationService struct {
	Sources            []content.NavSourceTileDef
	Graphs             []content.NavGraphTileDef
	ChunkSize          int
	VoxelResolution    float32
	NavigationRevision uint64
	query              *content.NavGraphQuery
	profiles           []content.NavAgentProfileDef
	disabledTraversals map[string]struct{}
	openDoors          map[string]struct{}
	blockers           map[string]content.NavBlockerDef
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
		Sources:   append([]content.NavSourceTileDef(nil), state.NavigationSources...),
		Graphs:    append([]content.NavGraphTileDef(nil), state.NavigationGraphs...),
		ChunkSize: state.BaseNavManifest.ChunkSize, VoxelResolution: state.BaseNavManifest.VoxelResolution,
		NavigationRevision: state.NavigationRevision,
		query:              state.navigationQuery,
		profiles:           append([]content.NavAgentProfileDef(nil), state.BaseNavManifest.AgentProfiles...),
		disabledTraversals: copyNavigationTraversalSet(state.navigationDisabled),
		openDoors:          copyNavigationTraversalSet(state.navigationOpenDoors),
		blockers:           copyNavigationBlockers(state.navigationBlockers),
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
	query, err := buildRuntimeNavigationQueryWithDoorOverlays(s.Sources, s.Graphs, s.ChunkSize, s.VoxelResolution, s.profiles, disabled, s.openDoors, s.blockers)
	if err != nil {
		return RuntimeNavigationService{}, err
	}
	s.query, s.disabledTraversals = query, disabled
	return s, nil
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
	disabledTraversals := copyNavigationTraversalSet(state.navigationDisabled)
	openDoors := copyNavigationTraversalSet(state.navigationOpenDoors)
	blockers := copyNavigationBlockers(state.navigationBlockers)
	manifestPath, deltaPath := state.BaseNavManifestPath, state.WorldDeltaPath
	runtimeGeneration := state.Generation
	state.navigationLoadActive = true
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		sources, graphs, err := loadStreamedNavigationResidency(manifest, manifestPath, &delta, deltaPath, desired)
		var query *content.NavGraphQuery
		if err == nil {
			query, err = buildRuntimeNavigationQueryWithDoorOverlays(sources, graphs, manifest.ChunkSize, manifest.VoxelResolution, manifest.AgentProfiles, disabledTraversals, openDoors, blockers)
		}
		state.navigationLoads <- streamedNavigationLoadResult{RuntimeGeneration: runtimeGeneration, Generation: generation, Sources: sources, Graphs: graphs, Query: query, DisabledTraversals: disabledTraversals, OpenDoors: openDoors, Blockers: blockers, Err: err}
	}()
}

func buildRuntimeNavigationQuery(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithDisabledTraversals(sources, graphs, chunkSize, voxelResolution, nil)
}

func buildRuntimeNavigationQueryWithDisabledTraversals(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, disabled map[string]struct{}) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithOverlays(sources, graphs, chunkSize, voxelResolution, nil, disabled, nil)
}

func buildRuntimeNavigationQueryWithOverlays(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, profiles []content.NavAgentProfileDef, disabled map[string]struct{}, blockers map[string]content.NavBlockerDef) (*content.NavGraphQuery, error) {
	return buildRuntimeNavigationQueryWithDoorOverlays(sources, graphs, chunkSize, voxelResolution, profiles, disabled, nil, blockers)
}

func buildRuntimeNavigationQueryWithDoorOverlays(sources []content.NavSourceTileDef, graphs []content.NavGraphTileDef, chunkSize int, voxelResolution float32, profiles []content.NavAgentProfileDef, disabled, openDoors map[string]struct{}, blockers map[string]content.NavBlockerDef) (*content.NavGraphQuery, error) {
	if len(sources) == 0 || len(graphs) == 0 {
		return nil, nil
	}
	profileID := graphs[0].AgentProfileID
	profileGraphs := make([]content.NavGraphTileDef, 0, len(graphs))
	for _, graph := range graphs {
		if graph.AgentProfileID == profileID {
			profileGraphs = append(profileGraphs, navGraphWithRuntimeTraversals(graph, disabled, openDoors))
		}
	}
	if len(blockers) != 0 {
		for _, profile := range profiles {
			if profile.ID == profileID {
				return content.NewNavGraphQueryWithBlockers(sources, profileGraphs, chunkSize, voxelResolution, profile, sortedNavigationBlockers(blockers))
			}
		}
		return nil, fmt.Errorf("navigation blocker overlay requires agent profile %q", profileID)
	}
	return content.NewNavGraphQuery(sources, profileGraphs, chunkSize, voxelResolution)
}

func navGraphWithoutDisabledTraversals(graph content.NavGraphTileDef, disabled map[string]struct{}) content.NavGraphTileDef {
	return navGraphWithRuntimeTraversals(graph, disabled, nil)
}

func navGraphWithRuntimeTraversals(graph content.NavGraphTileDef, disabled, openDoors map[string]struct{}) content.NavGraphTileDef {
	if len(disabled) == 0 && len(openDoors) == 0 {
		return graph
	}
	copy := graph
	copy.SpanTransitions = make([]content.NavSpanTransitionDef, 0, len(graph.SpanTransitions))
	for _, transition := range graph.SpanTransitions {
		if !navTransitionDisabled(transition.Traversal, transition.Gate, disabled) {
			transition.Gate, transition.Cost = runtimeDoorGate(transition.Gate, transition.Cost, openDoors)
			copy.SpanTransitions = append(copy.SpanTransitions, transition)
		}
	}
	copy.Transitions = make([]content.NavRegionTransitionDef, 0, len(graph.Transitions))
	for _, transition := range graph.Transitions {
		if navTransitionDisabled(transition.Traversal, transition.Gate, disabled) {
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
		if _, found := disabled[traversal.ID]; found {
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
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	select {
	case result := <-state.navigationLoads:
		if result.RuntimeGeneration != state.Generation {
			break
		}
		state.navigationLoadActive = false
		if result.Err != nil {
			state.InitErr = result.Err
			return
		}
		if result.Generation == state.navigationRequestedGen {
			query := result.Query
			if !navigationTraversalSetsEqual(result.DisabledTraversals, state.navigationDisabled) || !navigationTraversalSetsEqual(result.OpenDoors, state.navigationOpenDoors) || !navigationBlockersEqual(result.Blockers, state.navigationBlockers) {
				query, result.Err = buildRuntimeNavigationQueryWithDoorOverlays(result.Sources, result.Graphs, state.BaseNavManifest.ChunkSize, state.BaseNavManifest.VoxelResolution, state.BaseNavManifest.AgentProfiles, state.navigationDisabled, state.navigationOpenDoors, state.navigationBlockers)
				if result.Err != nil {
					state.InitErr = result.Err
					return
				}
			}
			state.mu.Lock()
			state.NavigationSources = result.Sources
			state.NavigationGraphs = result.Graphs
			state.navigationQuery = query
			state.NavigationRevision++
			state.navigationLoadedGen = result.Generation
			state.mu.Unlock()
		}
	default:
	}
	select {
	case result := <-state.navigationRebuilds:
		if result.RuntimeGeneration != state.Generation {
			break
		}
		state.navigationRebuildActive = false
		if result.Err != nil {
			state.InitErr = result.Err
			return
		}
		state.WorldDelta.NavigationSourceOverrides = append([]content.NavigationSourceOverrideDef(nil), result.Delta.NavigationSourceOverrides...)
		state.WorldDelta.NavigationGraphOverrides = append([]content.NavigationGraphOverrideDef(nil), result.Delta.NavigationGraphOverrides...)
		if err := content.SaveWorldDelta(state.WorldDeltaPath, state.WorldDelta); err != nil {
			state.InitErr = err
			return
		}
		reloadStreamedNavigationResidency(state)
	default:
	}
	if !state.navigationLoadActive && state.navigationLoadedGen != state.navigationRequestedGen {
		startStreamedNavigationLoad(state)
	}
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
	blockers, err := runtimeNavigationBlockers(cmd)
	if err != nil {
		state.InitErr = err
		return
	}
	if navigationTraversalSetsEqual(disabled, state.navigationDisabled) && navigationTraversalSetsEqual(openDoors, state.navigationOpenDoors) && navigationBlockersEqual(blockers, state.navigationBlockers) {
		return
	}
	state.mu.RLock()
	sources := append([]content.NavSourceTileDef(nil), state.NavigationSources...)
	graphs := append([]content.NavGraphTileDef(nil), state.NavigationGraphs...)
	state.mu.RUnlock()
	query, err := buildRuntimeNavigationQueryWithDoorOverlays(sources, graphs, state.BaseNavManifest.ChunkSize, state.BaseNavManifest.VoxelResolution, state.BaseNavManifest.AgentProfiles, disabled, openDoors, blockers)
	if err != nil {
		state.InitErr = err
		return
	}
	state.mu.Lock()
	state.navigationDisabled = disabled
	state.navigationOpenDoors = openDoors
	state.navigationBlockers = blockers
	state.navigationQuery = query
	state.NavigationRevision++
	state.mu.Unlock()
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
	if cmd == nil || state == nil || !state.Initialized || state.InitErr != nil || state.BaseNavManifest == nil || state.navigationRebuildActive {
		return
	}
	snapshots := takeVoxelWorldDirtyChunks(cmd.app, state.BaseWorldID)
	rt := voxelRtStateFromApp(cmd.app)
	if rt != nil {
		for _, loaded := range state.LoadedChunks {
			for eid := range loaded.ImportedWorldEntities {
				revision, edited := rt.runtimeEditedVoxelRevision(eid)
				if !edited || revision == 0 || state.navigationEditRevisions[eid] >= revision {
					continue
				}
				if snapshot := loadedImportedWorldChunkSnapshotForNavigation(cmd, state, eid); snapshot != nil {
					snapshots = append(snapshots, snapshot)
					state.navigationEditRevisions[eid] = revision
				}
			}
		}
	}
	if len(snapshots) == 0 {
		return
	}
	snapshots = uniqueImportedWorldSnapshots(snapshots)
	if err := persistImportedWorldRuntimeEditSnapshots(state, snapshots); err != nil {
		state.InitErr = err
		return
	}
	dirty := make([]content.TerrainChunkCoordDef, 0, len(snapshots))
	overrides := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(snapshots))
	for _, snapshot := range snapshots {
		dirty = append(dirty, snapshot.Coord)
		overrides[snapshot.Coord] = snapshot
	}
	chunks, err := loadNavigationRebuildChunks(cmd, state, dirty, overrides)
	if err != nil {
		state.InitErr = err
		return
	}
	delta := copyWorldDeltaForNav(state.WorldDelta)
	manifest := copyNavGraphManifest(state.BaseNavManifest)
	runtimeGeneration := state.Generation
	deltaPath, manifestPath := state.WorldDeltaPath, state.BaseNavManifestPath
	state.navigationRebuildActive = true
	state.jobs.Add(1)
	go func() {
		defer state.jobs.Done()
		result, err := content.SaveNavGraphDeltaForImportedWorldChunks(deltaPath, &delta, manifest, manifestPath, chunks, dirty)
		if err != nil {
			err = fmt.Errorf("rebuild navigation graph delta: %w", err)
		}
		state.navigationRebuilds <- streamedNavigationRebuildResult{RuntimeGeneration: runtimeGeneration, Delta: delta, Result: result, Err: err}
	}()
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
