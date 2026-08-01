package gekko

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func finishStreamedNavigationOverlay(t *testing.T, state *StreamedLevelRuntimeState) {
	t.Helper()
	state.jobs.Wait()
	streamedLevelNavigationSystem(state)
	if state.InitErr != nil {
		t.Fatal(state.InitErr)
	}
}

func BenchmarkStreamedNavigationCommit(b *testing.B) {
	state := &StreamedLevelRuntimeState{}
	result := streamedNavigationOverlayResult{
		LoadGeneration:     1,
		DisabledTraversals: map[string]struct{}{},
		OpenDoors:          map[string]struct{}{},
		Blockers:           map[string]content.NavBlockerDef{},
	}
	b.ReportAllocs()
	for b.Loop() {
		commitStreamedNavigationOverlay(state, result)
	}
}

func TestStreamedNavigationPublishesResidencyWhileOverlayMoves(t *testing.T) {
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	source := content.NavSourceTileDef{
		NavID: "pending", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: 4,
		SourceHash: "source", DependencyHash: "dependency",
		Spans: []content.NavSpanDef{{
			ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2,
			Headroom: 2, ClearanceRadius: 1, Area: "ground",
		}},
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, Generation: 3,
		BaseNavManifest:    &content.NavGraphManifestDef{ChunkSize: 4, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{profile}},
		NavigationRevision: 5, navigationLoadedGen: 1, navigationRequestedGen: 2, navigationLoadActive: true,
		navigationDesired:          make(map[content.TerrainChunkCoordDef]struct{}),
		navigationLoads:            make(chan streamedNavigationLoadResult, 2),
		navigationDisabled:         make(map[string]struct{}),
		navigationOpenDoors:        make(map[string]struct{}),
		navigationBlockers:         make(map[string]content.NavBlockerDef),
		navigationOverlayDisabled:  map[string]struct{}{"broken-ladder": {}},
		navigationOverlayOpenDoors: make(map[string]struct{}),
		navigationOverlayBlockers:  make(map[string]content.NavBlockerDef),
	}
	state.navigationLoads <- streamedNavigationLoadResult{
		RuntimeGeneration: 3, Generation: 2,
		Sources: []content.NavSourceTileDef{source}, Graphs: []content.NavGraphTileDef{built.Graph},
	}

	streamedLevelNavigationSystem(state)
	if state.navigationPendingGen != 2 || state.navigationLoadActive || !state.navigationOverlayActive {
		t.Fatalf("residency was not retained for overlay build: pending=%d load=%t overlay=%t", state.navigationPendingGen, state.navigationLoadActive, state.navigationOverlayActive)
	}
	setStreamedNavigationOverlayDesired(
		state,
		map[string]struct{}{"moving-ladder": {}},
		make(map[string]struct{}),
		make(map[string]content.NavBlockerDef),
	)
	state.jobs.Wait()
	streamedLevelNavigationSystem(state)
	if state.navigationPendingGen != 0 || state.navigationLoadedGen != 2 || state.navigationLoadActive || !state.navigationOverlayActive {
		t.Fatalf("moving overlay starved residency: pending=%d loaded=%d load=%t overlay=%t", state.navigationPendingGen, state.navigationLoadedGen, state.navigationLoadActive, state.navigationOverlayActive)
	}
	setStreamedNavigationOverlayDesired(
		state,
		map[string]struct{}{"latest-ladder": {}},
		make(map[string]struct{}),
		make(map[string]content.NavBlockerDef),
	)
	state.jobs.Wait()
	streamedLevelNavigationSystem(state)
	if state.NavigationRevision != 7 || state.navigationLoadActive || !state.navigationOverlayActive {
		t.Fatalf("second moving overlay was not published and coalesced: revision=%d load=%t overlay=%t", state.NavigationRevision, state.navigationLoadActive, state.navigationOverlayActive)
	}
	finishStreamedNavigationOverlay(t, state)
	if state.NavigationRevision != 8 || state.navigationLoadedGen != 2 || state.navigationPendingGen != 0 {
		t.Fatalf("latest residency was not published: revision=%d graph=%d pending=%d", state.NavigationRevision, state.navigationLoadedGen, state.navigationPendingGen)
	}
	if _, installed := state.navigationDisabled["latest-ladder"]; !installed {
		t.Fatal("latest overlay was not installed")
	}
}

func TestStreamedNavigationIgnoresStaleLoadError(t *testing.T) {
	state := &StreamedLevelRuntimeState{
		Initialized: true, Generation: 3,
		BaseNavManifest:     &content.NavGraphManifestDef{ChunkSize: 4, VoxelResolution: 1},
		navigationLoadedGen: 1, navigationRequestedGen: 2, navigationLoadActive: true,
		navigationDesired:          make(map[content.TerrainChunkCoordDef]struct{}),
		navigationLoads:            make(chan streamedNavigationLoadResult, 2),
		navigationDisabled:         make(map[string]struct{}),
		navigationOpenDoors:        make(map[string]struct{}),
		navigationBlockers:         make(map[string]content.NavBlockerDef),
		navigationOverlayDisabled:  make(map[string]struct{}),
		navigationOverlayOpenDoors: make(map[string]struct{}),
		navigationOverlayBlockers:  make(map[string]content.NavBlockerDef),
	}
	state.navigationLoads <- streamedNavigationLoadResult{
		RuntimeGeneration: 3, Generation: 1, Err: errors.New("stale load failed"),
	}

	streamedLevelNavigationSystem(state)
	if state.InitErr != nil || !state.navigationLoadActive {
		t.Fatalf("stale error became fatal or current load was not started: err=%v active=%t", state.InitErr, state.navigationLoadActive)
	}
	state.jobs.Wait()
	streamedLevelNavigationSystem(state)
	finishStreamedNavigationOverlay(t, state)
	if state.navigationLoadedGen != 2 {
		t.Fatalf("current empty residency was not published: %d", state.navigationLoadedGen)
	}
}

func TestStreamedNavigationRejectsStaleDestructionBatch(t *testing.T) {
	state := &StreamedLevelRuntimeState{
		Initialized: true, Generation: 4,
		BaseNavManifest:          &content.NavGraphManifestDef{ChunkSize: 4, VoxelResolution: 1},
		WorldDelta:               &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion},
		navigationEditGeneration: 2, navigationRebuildActive: true,
		navigationRebuilds: make(chan streamedNavigationRebuildResult, 1),
		navigationLoads:    make(chan streamedNavigationLoadResult, 1),
		navigationOverlays: make(chan streamedNavigationOverlayResult, 1),
	}
	state.navigationRebuilds <- streamedNavigationRebuildResult{
		RuntimeGeneration: 4, EditGeneration: 1,
		Delta: content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, NavigationSourceOverrides: []content.NavigationSourceOverrideDef{{NavID: "stale"}}},
	}
	streamedLevelNavigationSystem(state)
	if state.navigationRebuildActive || len(state.WorldDelta.NavigationSourceOverrides) != 0 || state.navigationRequestedGen != 0 {
		t.Fatalf("stale rebuild published: active=%t delta=%+v load=%d", state.navigationRebuildActive, state.WorldDelta.NavigationSourceOverrides, state.navigationRequestedGen)
	}
}

func TestNavigationEditBlockersRetireOnlyCoveredGenerations(t *testing.T) {
	manifest := &content.NavGraphManifestDef{ChunkSize: 4, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{{Radius: .5, Height: 2}}}
	state := &StreamedLevelRuntimeState{
		BaseNavManifest:           manifest,
		navigationEditBlockers:    map[string]navigationEditBlocker{},
		navigationOverlayDisabled: map[string]struct{}{}, navigationOverlayOpenDoors: map[string]struct{}{}, navigationOverlayBlockers: map[string]content.NavBlockerDef{},
	}
	first := navigationEditBlockerForChunk(manifest, content.TerrainChunkCoordDef{}, 1)
	second := navigationEditBlockerForChunk(manifest, content.TerrainChunkCoordDef{X: 1}, 2)
	state.navigationEditBlockers[first.Blocker.ID] = first
	state.navigationEditBlockers[second.Blocker.ID] = second
	retireNavigationEditBlockers(state, 1)
	if _, exists := state.navigationEditBlockers[first.Blocker.ID]; exists {
		t.Fatal("covered edit blocker was retained")
	}
	if _, exists := state.navigationEditBlockers[second.Blocker.ID]; !exists {
		t.Fatal("newer edit blocker was retired by an older batch")
	}
}

func TestBrokenLadderDisablesRuntimeTraversalWithoutRebake(t *testing.T) {
	const chunkSize = 4
	source := content.NavSourceTileDef{
		NavID: "ladder-runtime", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
		Spans: []content.NavSpanDef{
			{ID: 0, X: 0, Z: 0, SupportHeight: 0, CeilingHeight: 2, Headroom: 2, ClearanceRadius: 1, Area: "ground"},
			{ID: 1, X: 0, Y: 3, Z: 0, SupportHeight: 3, CeilingHeight: 6, Headroom: 3, ClearanceRadius: 1, Area: "ground"},
		},
	}
	profile := content.NavAgentProfileDef{
		ID: "climber", Radius: 0.4, Height: 1.5, StepHeight: 0.5, MaxSlopeDegrees: 45,
		Capabilities: []string{content.NavCapabilityClimbLadder},
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	bottom, top := content.Vec3{0.5, 0, 0.5}, content.Vec3{0.5, 3, 0.5}
	ladder := content.LevelLadderVolumeDef{
		ID: "ladder-1", BoundsCenter: content.Vec3{0.5, 1.5, 0.5}, BoundsHalfExtents: content.Vec3{0.5, 1.5, 0.5},
		MountBottom: &bottom, MountTop: &top, Health: 50,
	}
	graphs, _, err := content.ConnectNavGraphLadders([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, []content.LevelLadderVolumeDef{ladder}, profile, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, graphs, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, LevelID: "level", BaseNavManifest: &content.NavGraphManifestDef{
			ChunkSize: chunkSize, VoxelResolution: 1, LadderVolumes: []content.LevelLadderVolumeDef{ladder},
		},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: graphs,
		NavigationRevision: 1, navigationQuery: query, navigationDisabled: make(map[string]struct{}), navigationLoadedGen: 7,
	}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0.5, 1.5, 0.5}},
		&LadderVolumeComponent{},
		&AuthoredLevelLadderVolumeRefComponent{LevelID: "level", LadderVolumeID: ladder.ID},
		&BreakableComponent{Kind: "ladder", Health: ladder.Health, MaxHealth: ladder.Health},
	)
	app.FlushCommands()
	service := RuntimeNavigationServiceFromStreamedLevelState(state)
	if service.NavigationGraphRevision != 7 {
		t.Fatalf("navigation graph revision = %d", service.NavigationGraphRevision)
	}
	before, err := service.FindRoute(bottom, top)
	if err != nil || !before.Found {
		t.Fatalf("expected live ladder route: route=%+v err=%v", before, err)
	}
	bottomRef, topRef := content.NavSpanRef{Span: 0}, content.NavSpanRef{Span: 1}
	if _, ok := service.ReachableSpans(bottomRef, []content.NavSpanRef{topRef})[topRef]; !ok {
		t.Fatal("live ladder traversal was not reachable")
	}
	if handled, broken := DamageBreakableEntity(cmd, entity, 100, 0); !handled || !broken {
		t.Fatalf("ladder damage was not handled: handled=%v broken=%v", handled, broken)
	}
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	if !state.navigationOverlayActive || state.NavigationRevision != 1 {
		t.Fatalf("overlay was not deferred: active=%t revision=%d", state.navigationOverlayActive, state.NavigationRevision)
	}
	finishStreamedNavigationOverlay(t, state)
	service = RuntimeNavigationServiceFromStreamedLevelState(state)
	if service.NavigationGraphRevision != 7 {
		t.Fatalf("overlay changed navigation graph revision: %d", service.NavigationGraphRevision)
	}
	after, err := service.FindRoute(bottom, top)
	if err != nil || after.Found || after.FailureReason != content.NavRouteNoRoute || after.NavigationRevision != 2 {
		t.Fatalf("broken ladder traversal remained enabled: route=%+v err=%v", after, err)
	}
	if _, ok := service.ReachableSpans(bottomRef, []content.NavSpanRef{topRef})[topRef]; ok {
		t.Fatal("reachable span query crossed broken ladder traversal")
	}
}

func TestRuntimeDoorOverlayChangesActionWithoutRebake(t *testing.T) {
	const chunkSize = 6
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	source := content.NavSourceTileDef{
		NavID: "door-runtime", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
	}
	for x := range chunkSize {
		source.Spans = append(source.Spans, content.NavSpanDef{
			ID: uint32(x), X: x, Z: 0, SupportHeight: 0, CeilingHeight: 3,
			Headroom: 3, ClearanceRadius: 1, Area: "ground",
		})
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	door := content.NavDoorDef{ID: "door-1", BoundsCenter: content.Vec3{3, 1, 0.5}, BoundsHalfExtents: content.Vec3{0.1, 1, 0.5}}
	graphs, _, err := content.ConnectNavGraphDoors([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, []content.NavDoorDef{door}, profile, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, graphs, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, LevelID: "level", BaseNavManifest: &content.NavGraphManifestDef{
			ChunkSize: chunkSize, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{profile}, Doors: []content.NavDoorDef{door},
		},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: graphs,
		NavigationRevision: 1, navigationQuery: query, navigationDisabled: make(map[string]struct{}), navigationOpenDoors: make(map[string]struct{}), navigationBlockers: make(map[string]content.NavBlockerDef),
	}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&MovingBrushComponent{
			NavigationRole: content.NavigationRoleDoor,
			BoundsCenter:   mgl32.Vec3(door.BoundsCenter), ClosedBoundsCenter: mgl32.Vec3(door.BoundsCenter),
			BoundsHalfExtents: mgl32.Vec3(door.BoundsHalfExtents), OpenOffset: mgl32.Vec3{0, 2, 0},
		},
		&AuthoredLevelMovingBrushRefComponent{LevelID: "level", MovingBrushID: door.ID},
	)
	app.FlushCommands()
	start, goal := content.Vec3{0.5, 0, 0.5}, content.Vec3{5.5, 0, 0.5}
	closed, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !closed.Found || !routeRequiresGate(closed, content.NavGateDoor) {
		t.Fatalf("closed door route = %+v err=%v", closed, err)
	}
	avoiding, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRouteAvoidingTraversal(start, goal, door.ID)
	if err != nil || avoiding.Found || avoiding.FailureReason != content.NavRouteNoRoute {
		t.Fatalf("door-avoiding route = %+v err=%v", avoiding, err)
	}

	brush := cmd.GetComponent(entity, reflect.TypeOf(MovingBrushComponent{})).(*MovingBrushComponent)
	brush.Open = true
	brush.BoundsCenter = brush.ClosedBoundsCenter.Add(brush.OpenOffset)
	streamedLevelNavigationOverlaySystem(cmd, state)
	finishStreamedNavigationOverlay(t, state)
	open, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !open.Found || routeRequiresGate(open, content.NavGateDoor) || open.NavigationRevision != 2 {
		t.Fatalf("open door route = %+v err=%v", open, err)
	}

	cmd.RemoveEntity(entity)
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	finishStreamedNavigationOverlay(t, state)
	missing, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || missing.Found || missing.FailureReason != content.NavRouteNoRoute || missing.NavigationRevision != 3 {
		t.Fatalf("missing door route = %+v err=%v", missing, err)
	}
}

func routeRequiresGate(route content.NavRouteResult, kind string) bool {
	for _, step := range route.Steps {
		if step.Gate != nil && step.Gate.Kind == kind {
			return true
		}
	}
	return false
}

func TestRuntimeNavigationBlockerAddMoveRemove(t *testing.T) {
	const chunkSize = 7
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.1, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	source := content.NavSourceTileDef{
		NavID: "blocker-runtime", SchemaVersion: content.CurrentNavSourceTileSchemaVersion,
		BuilderVersion: content.CurrentNavGraphBuilderVersion, ChunkSize: chunkSize,
		SourceHash: "source", DependencyHash: "dependency",
	}
	for x := 0; x < chunkSize; x++ {
		for z := 0; z < chunkSize; z++ {
			source.Spans = append(source.Spans, content.NavSpanDef{
				ID: uint32(len(source.Spans)), X: x, Z: z, SupportHeight: 0, CeilingHeight: 3,
				Headroom: 3, ClearanceRadius: 1, Area: "ground",
			})
		}
	}
	built, err := content.BuildNavSpanGraph(source, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery([]content.NavSourceTileDef{source}, []content.NavGraphTileDef{built.Graph}, chunkSize, 1)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, BaseNavManifest: &content.NavGraphManifestDef{
			ChunkSize: chunkSize, VoxelResolution: 1, AgentProfiles: []content.NavAgentProfileDef{profile},
		},
		NavigationSources: []content.NavSourceTileDef{source}, NavigationGraphs: []content.NavGraphTileDef{built.Graph},
		NavigationRevision: 1, navigationQuery: query, navigationDisabled: make(map[string]struct{}), navigationBlockers: make(map[string]content.NavBlockerDef),
	}
	start, goal := content.Vec3{0.1, 0.2, 2.5}, content.Vec3{6.9, 0.2, 2.5}
	baseline, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !baseline.Found {
		t.Fatalf("baseline route=%+v err=%v", baseline, err)
	}
	app := NewApp()
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&AABBComponent{Min: mgl32.Vec3{3.25, 0, 2.25}, Max: mgl32.Vec3{3.75, 1, 2.75}},
		&NavigationBlockerComponent{ID: "crate"},
	)
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	finishStreamedNavigationOverlay(t, state)
	route, err := RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !route.Found || route.NavigationRevision != 2 || len(route.Waypoints) < 2 {
		t.Fatalf("placed blocker did not reroute: route=%+v err=%v", route, err)
	}
	if valid, reason := RuntimeNavigationServiceFromStreamedLevelState(state).RouteDependencyStatus(baseline); valid || reason != "dependency_epoch_changed" {
		t.Fatalf("blocked baseline route status=%t/%q", valid, reason)
	}

	MakeQuery1[AABBComponent](cmd).Map(func(_ EntityId, bounds *AABBComponent) bool {
		bounds.Min, bounds.Max = mgl32.Vec3{3.25, 0, 0}, mgl32.Vec3{3.75, 1, 7}
		return true
	})
	streamedLevelNavigationOverlaySystem(cmd, state)
	finishStreamedNavigationOverlay(t, state)
	route, err = RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || route.Found || route.FailureReason != content.NavRouteNoRoute || route.NavigationRevision != 3 {
		t.Fatalf("moved blocker did not split route: route=%+v err=%v", route, err)
	}

	cmd.RemoveEntity(entity)
	app.FlushCommands()
	streamedLevelNavigationOverlaySystem(cmd, state)
	finishStreamedNavigationOverlay(t, state)
	route, err = RuntimeNavigationServiceFromStreamedLevelState(state).FindRoute(start, goal)
	if err != nil || !route.Found || route.NavigationRevision != 4 || len(route.Waypoints) != 1 {
		t.Fatalf("removed blocker did not restore route: route=%+v err=%v", route, err)
	}
}

func TestStreamedNavigationResidencyRevisionSwapFollowsDelta(t *testing.T) {
	const chunkSize = 4
	coords := []content.TerrainChunkCoordDef{{}, {X: 1}}
	world := &content.ImportedWorldDef{
		WorldID: "runtime-nav", SchemaVersion: content.CurrentImportedWorldSchemaVersion,
		Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: chunkSize, VoxelResolution: 1,
	}
	chunks := make([]content.ImportedWorldChunkDef, 2)
	for i, coord := range coords {
		voxels := make([]content.ImportedWorldVoxelDef, 0, chunkSize*chunkSize)
		for x := range chunkSize {
			for z := range chunkSize {
				voxels = append(voxels, content.ImportedWorldVoxelDef{X: x, Z: z, Value: 1})
			}
		}
		world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: coord.String() + ".gkchunk", NonEmptyVoxelCount: len(voxels)})
		chunks[i] = content.ImportedWorldChunkDef{WorldID: world.WorldID, Coord: coord, ChunkSize: chunkSize, VoxelResolution: 1, Voxels: voxels}
	}
	content.EnsureImportedWorldSectors(world)
	profile := content.NavAgentProfileDef{ID: "walker", Radius: 0.4, Height: 1.8, StepHeight: 0.5, MaxSlopeDegrees: 45}
	bake, err := content.BakeNavGraphWorld(world, chunks, []content.NavAgentProfileDef{profile})
	if err != nil {
		t.Fatal(err)
	}
	query, err := buildRuntimeNavigationQuery(bake.SourceTiles, bake.GraphTiles, bake.Manifest.ChunkSize, bake.Manifest.VoxelResolution)
	if err != nil {
		t.Fatal(err)
	}
	prepared := RuntimeNavigationService{NavigationRevision: 7, query: query}
	preparedRoute, err := prepared.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || !preparedRoute.Found || preparedRoute.NavigationRevision != 7 {
		t.Fatalf("prepared runtime query failed: route=%+v err=%v", preparedRoute, err)
	}
	basePath := filepath.Join(t.TempDir(), "base"+content.NavGraphManifestExtension)
	if err := content.SaveNavGraphBake(basePath, &bake); err != nil {
		t.Fatal(err)
	}
	deltaPath := filepath.Join(t.TempDir(), "level.gkworlddelta")
	delta := &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "level"}
	desired := map[content.TerrainChunkCoordDef]struct{}{coords[0]: {}, coords[1]: {}}
	sources, graphs, err := loadStreamedNavigationResidency(&bake.Manifest, basePath, delta, deltaPath, desired)
	if err != nil {
		t.Fatal(err)
	}
	state := &StreamedLevelRuntimeState{
		Initialized: true, BaseNavManifest: &bake.Manifest, navigationRequestedGen: 1, navigationLoadActive: true,
		navigationLoads: make(chan streamedNavigationLoadResult, 1), navigationRebuilds: make(chan streamedNavigationRebuildResult, 1),
	}
	state.navigationLoads <- streamedNavigationLoadResult{Generation: 1, Sources: sources, Graphs: graphs}
	streamedLevelNavigationSystem(state)
	finishStreamedNavigationOverlay(t, state)
	before := RuntimeNavigationServiceFromStreamedLevelState(state)
	route, err := before.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || !route.Found || route.NavigationRevision != 1 {
		t.Fatalf("initial streamed route failed: route=%+v err=%v", route, err)
	}

	chunks[0].Voxels = nil
	effective := map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{coords[0]: &chunks[0], coords[1]: &chunks[1]}
	if _, err := content.SaveNavGraphDeltaForImportedWorldChunks(deltaPath, delta, &bake.Manifest, basePath, effective, []content.TerrainChunkCoordDef{coords[0]}); err != nil {
		t.Fatal(err)
	}
	sources, graphs, err = loadStreamedNavigationResidency(&bake.Manifest, basePath, delta, deltaPath, desired)
	if err != nil {
		t.Fatal(err)
	}
	state.navigationRequestedGen = 2
	state.navigationLoadActive = true
	state.navigationLoads <- streamedNavigationLoadResult{Generation: 2, Sources: sources, Graphs: graphs}
	streamedLevelNavigationSystem(state)
	finishStreamedNavigationOverlay(t, state)
	after := RuntimeNavigationServiceFromStreamedLevelState(state)
	route, err = after.FindRoute(content.Vec3{0.5, 1, 0.5}, content.Vec3{7.5, 1, 0.5})
	if err != nil || route.Found || route.NavigationRevision != 2 {
		t.Fatalf("delta revision did not remove route atomically: route=%+v err=%v", route, err)
	}
}

func TestVoxelWorldDirtyNotificationPreservesEmptyChunk(t *testing.T) {
	app := NewApp()
	app.resources[reflect.TypeOf(VoxelWorldDirtyChunks{})] = &VoxelWorldDirtyChunks{Imported: make(map[voxelWorldDirtyChunkKey]*content.ImportedWorldChunkDef)}
	cmd := app.Commands()
	entity := cmd.AddEntity(
		&VoxelModelComponent{TerrainChunkSize: 4, VoxelResolution: 1},
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&AuthoredImportedWorldChunkRefComponent{WorldID: "world", ChunkCoord: [3]int{2, 0, -1}},
	)
	app.FlushCommands()
	notifyImportedWorldChunkDirty(cmd, entity, volume.NewXBrickMap())
	snapshots := takeVoxelWorldDirtyChunks(app, "world")
	if len(snapshots) != 1 || snapshots[0].Coord != (content.TerrainChunkCoordDef{X: 2, Z: -1}) || snapshots[0].NonEmptyVoxelCount != 0 {
		t.Fatalf("empty dirty chunk notification lost: %+v", snapshots)
	}
}

func TestConfigureStreamedNavigationManifestUsesValidatedRuntimeOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime"+content.NavGraphManifestExtension)
	manifest := &content.NavGraphManifestDef{
		NavID: "runtime-nav", SchemaVersion: content.CurrentNavGraphManifestSchemaVersion,
		SourceWorldID: "world", BuilderVersion: content.CurrentNavGraphBuilderVersion,
		ChunkSize: 16, VoxelResolution: 0.25,
	}
	if err := content.SaveNavGraphManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	level := &content.LevelDef{ChunkSize: 16, VoxelResolution: 0.25, Navigation: &content.LevelNavigationDef{ManifestPath: "missing.gknav"}}
	state := &StreamedLevelRuntimeState{BaseWorldID: "world"}
	if err := configureStreamedNavigationManifest(state, level, StreamedLevelRuntimeConfig{LevelPath: filepath.Join(filepath.Dir(path), "level.gklevel"), NavigationManifestPath: path}); err != nil {
		t.Fatal(err)
	}
	if state.BaseNavManifestPath != path || state.BaseNavManifest == nil || state.BaseNavManifest.NavID != manifest.NavID {
		t.Fatalf("runtime navigation override was not selected: %+v", state.BaseNavManifest)
	}
	state.BaseWorldID = "other-world"
	if err := configureStreamedNavigationManifest(state, level, StreamedLevelRuntimeConfig{LevelPath: filepath.Join(filepath.Dir(path), "level.gklevel"), NavigationManifestPath: path}); err == nil {
		t.Fatal("navigation override with mismatched source world was accepted")
	}
}
