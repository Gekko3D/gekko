package gekko

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// These tests observe the runtime's ECS/renderer boundary. Status fixtures stand
// for the preceding PreRender stage; streaming must never write a transition.
type streamedRenderHarness struct {
	t        *testing.T
	app      *App
	cmd      *Commands
	assets   *AssetServer
	runtime  *StreamedLevelRuntimeState
	renderer *VoxelRtState
	sector   ChunkCoord
	coords   []ChunkCoord
}

func newStreamedRenderHarness(t *testing.T, renderer *VoxelRtState) *streamedRenderHarness {
	t.Helper()
	app, cmd, state := newStreamedRuntimeHarness(t)
	f := &streamedRenderHarness{t: t, app: app, cmd: cmd, assets: assetServerFromApp(app), runtime: state, renderer: renderer, sector: ChunkCoord{}, coords: []ChunkCoord{{}, {X: 1}}}
	if renderer != nil {
		cmd.AddResources(renderer)
	}
	state.Initialized, state.Generation = true, 41
	state.LevelID, state.BaseWorldID, state.TerrainID = "render-level", "render-world", "render-terrain"
	state.ChunkSize = 16
	state.DestructionChunks = make(map[ChunkCoord]struct{})
	state.LevelRoot = cmd.AddEntity(&AuthoredLevelRootComponent{LevelID: state.LevelID})
	state.Config.MaxPrepareJobs = 1
	// Prevent asynchronous I/O: prepared results are explicitly driven here.
	for _, coord := range f.coords {
		state.DesiredChunks[coord], state.KeepChunks[coord] = struct{}{}, struct{}{}
		state.PendingLoads[coord] = struct{}{}
		state.ImportedChunkSector[coord] = f.sector
		state.ImportedWorldEntries[coord] = content.ImportedWorldChunkEntryDef{Coord: terrainCoordFromChunk(coord), NonEmptyVoxelCount: 1, ChunkPath: "full.chunk.json"}
	}
	state.DesiredSectors[f.sector], state.KeepSectors[f.sector] = struct{}{}, struct{}{}
	state.DesiredProxySectors[f.sector], state.KeepProxySectors[f.sector] = struct{}{}, struct{}{}
	state.PendingProxyLoads[f.sector] = struct{}{}
	state.ImportedWorldSectors[f.sector] = content.ImportedWorldSectorDef{
		Coord: terrainCoordFromChunk(f.sector), FullChunkRefs: []content.TerrainChunkCoordDef{{}, {X: 1}},
		LODs: []content.ImportedWorldLODDef{{Level: 1, Kind: "voxel_proxy", ChunkPath: "proxy.chunk.json", NonEmptyVoxelCount: 1}},
	}
	app.FlushCommands()
	return f
}

func (f *streamedRenderHarness) prepared(coord ChunkCoord) streamedPreparedChunk {
	return streamedPreparedChunk{Generation: f.runtime.Generation, Coord: coord, ImportedWorldChunk: &content.ImportedWorldChunkDef{
		WorldID: f.runtime.BaseWorldID, Coord: terrainCoordFromChunk(coord), ChunkSize: 16, VoxelResolution: 1,
		Voxels: []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}}, NonEmptyVoxelCount: 1,
	}}
}

func (f *streamedRenderHarness) full(coord ChunkCoord) EntityId {
	f.t.Helper()
	if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, f.prepared(coord)); err != nil {
		f.t.Fatal(err)
	}
	loaded := f.runtime.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		f.t.Fatalf("expected one committed full entity at %v", coord)
	}
	for entity := range loaded.ImportedWorldEntities {
		return entity
	}
	return 0
}

func (f *streamedRenderHarness) proxy() EntityId {
	f.t.Helper()
	prepared := streamedPreparedSectorProxyForTest(f.sector)
	prepared.Generation = f.runtime.Generation
	if _, err := commitPreparedStreamedSectorProxy(f.cmd, f.assets, f.runtime, prepared); err != nil {
		f.t.Fatal(err)
	}
	loaded := f.runtime.LoadedSectorProxies[f.sector]
	if loaded == nil || loaded.Entity == 0 {
		f.t.Fatal("required fallback was not committed")
	}
	return loaded.Entity
}

func (f *streamedRenderHarness) marker(entity EntityId) StreamedVoxelRenderComponent {
	f.t.Helper()
	var marker StreamedVoxelRenderComponent
	found := false
	MakeQuery1[StreamedVoxelRenderComponent](f.cmd).Map(func(id EntityId, value *StreamedVoxelRenderComponent) bool {
		if id == entity {
			marker, found = *value, true
		}
		return true
	})
	if !found || marker.Ticket == 0 || marker.Generation != f.runtime.Generation {
		f.t.Fatalf("entity %d requires a nonzero current-generation render ticket before its first spawn flush; marker=%+v found=%v", entity, marker, found)
	}
	return marker
}

func (f *streamedRenderHarness) status(entity EntityId, state StreamedVoxelRenderState) uint64 {
	f.t.Helper()
	marker := f.marker(entity)
	if f.renderer.streamedVoxelTickets == nil {
		f.renderer.streamedVoxelTickets = make(map[uint64]*streamedVoxelTicket)
	}
	f.renderer.streamedVoxelTickets[marker.Ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: state, Entity: entity, Generation: marker.Generation}}
	return marker.Ticket
}

func (f *streamedRenderHarness) visibility(entity EntityId, hidden bool) {
	f.t.Helper()
	if got := VoxelEntityRenderHidden(f.cmd, entity); got != hidden {
		f.t.Fatalf("entity %d hidden=%v, want %v", entity, got, hidden)
	}
}

func (f *streamedRenderHarness) commitStage() {
	f.t.Helper()
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr != nil {
		f.t.Fatalf("commit stage failed: %v", f.runtime.InitErr)
	}
}

func (f *streamedRenderHarness) observerStage() {
	f.t.Helper()
	updateStreamedLevelObserverSystem(f.cmd, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr != nil {
		f.t.Fatalf("observer stage failed: %v", f.runtime.InitErr)
	}
}

func TestStreamedRenderCommitStagesEveryOwnedTargetBeforeSpawnFlush(t *testing.T) {
	for _, tc := range []struct {
		name     string
		renderer *VoxelRtState
	}{
		{"renderer app absent", &VoxelRtState{}}, {"GPU manager absent", newVoxelRtStateTest()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamedRenderHarness(t, tc.renderer)
			proxy := f.proxy()
			full := f.full(f.coords[0])
			terrainCoord := ChunkCoord{X: 3}
			terrain := &content.TerrainChunkDef{TerrainID: f.runtime.TerrainID, Coord: terrainCoordFromChunk(terrainCoord), ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}, NonEmptyVoxelCount: 1}
			if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, streamedPreparedChunk{Generation: f.runtime.Generation, Coord: terrainCoord, TerrainChunk: terrain}); err != nil {
				t.Fatal(err)
			}
			seen := make(map[uint64]bool)
			entities := []EntityId{proxy, full}
			for entity := range f.runtime.LoadedChunks[terrainCoord].TerrainEntities {
				entities = append(entities, entity)
			}
			if len(entities) != 3 {
				t.Fatal("terrain commit did not create target")
			}
			for _, entity := range entities {
				marker := f.marker(entity)
				if seen[marker.Ticket] {
					t.Fatal("owned targets share a ticket")
				}
				seen[marker.Ticket] = true
				f.visibility(entity, true)
				if _, known := f.renderer.StreamedVoxelStatus(marker.Ticket); known {
					t.Fatal("streaming wrote renderer status before bridge observation")
				}
			}
			if f.marker(proxy).Priority != StreamedVoxelPriorityFallback || f.marker(full).Priority != StreamedVoxelPriorityVisible {
				t.Fatal("commit priorities do not match fallback/visual demand")
			}
			f.commitStage()
			for _, entity := range entities {
				f.visibility(entity, true)
			}
		})
	}
}

func TestStreamedRenderCPUOnlyCommitKeepsExistingVisibility(t *testing.T) {
	f := newStreamedRenderHarness(t, nil)
	proxy := f.proxy()
	f.full(f.coords[0])
	f.runtime.PreparedLoads <- f.prepared(f.coords[1])
	f.commitStage()
	f.visibility(proxy, true)
	for _, coord := range f.coords {
		for entity := range f.runtime.LoadedChunks[coord].ImportedWorldEntities {
			f.visibility(entity, false)
			if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, entity) {
				t.Fatal("CPU-only runtime unexpectedly owns GPU tickets")
			}
		}
	}
}

func TestStreamedRenderRefinementPublishesCompleteCohortThroughBridge(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	parentTicket := f.marker(proxy).Ticket
	f.status(proxy, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, false)
	left := f.full(f.coords[0])
	f.status(left, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	// The second spawn has an internal flush. A ready sibling must stay hidden.
	right := f.full(f.coords[1])
	f.visibility(left, true)
	f.visibility(right, true)
	f.visibility(proxy, false)
	f.status(right, StreamedVoxelRenderUploading)
	f.commitStage()
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	scene := f.renderer.RtApp.Scene
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	parentObject := f.renderer.GetVoxelObject(proxy)
	if len(scene.Objects) != 3 || len(scene.VisibleObjects) != 1 || scene.VisibleObjects[0] != parentObject {
		t.Fatal("partial cohort must leave only fallback visible and all three objects resident")
	}
	f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, true)
	f.visibility(left, false)
	f.visibility(right, false)
	if f.marker(proxy).Ticket != parentTicket {
		t.Fatal("handoff discarded resident ready fallback ticket")
	}
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if len(scene.Objects) != 3 || len(scene.VisibleObjects) != 2 {
		t.Fatal("complete handoff must expose exactly two full objects and retain hidden proxy")
	}
	for _, object := range scene.VisibleObjects {
		if object == parentObject {
			t.Fatal("proxy remained visible beside full cohort")
		}
	}
}

func TestStreamedRenderCommitSystemKeepsReadySiblingHiddenDuringPendingSpawn(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left := f.full(f.coords[0])
	f.status(left, StreamedVoxelRenderReady)
	f.runtime.PreparedLoads <- f.prepared(f.coords[1])
	f.commitStage()
	right := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{1, 0, 0})
	if right == 0 {
		t.Fatal("prepared system did not commit pending full target")
	}
	f.marker(right)
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
}

func TestStreamedRenderFaultStatusCannotAuthorizeRefinement(t *testing.T) {
	for _, fault := range []string{"unknown", "uploading", "failed", "cancelled", "wrong entity", "wrong generation", "marker generation", "entity removed", "marker removed"} {
		t.Run(fault, func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			proxy := f.proxy()
			f.status(proxy, StreamedVoxelRenderReady)
			left, right := f.full(f.coords[0]), f.full(f.coords[1])
			f.status(left, StreamedVoxelRenderReady)
			ticket := f.marker(right).Ticket
			switch fault {
			case "uploading":
				f.status(right, StreamedVoxelRenderUploading)
			case "failed":
				f.status(right, StreamedVoxelRenderFailed)
			case "cancelled":
				f.status(right, StreamedVoxelRenderCancelled)
			case "wrong entity":
				f.status(right, StreamedVoxelRenderReady)
				f.renderer.streamedVoxelTickets[ticket].status.Entity = left
			case "wrong generation":
				f.status(right, StreamedVoxelRenderReady)
				f.renderer.streamedVoxelTickets[ticket].status.Generation--
			case "marker generation":
				f.status(right, StreamedVoxelRenderReady)
				MakeQuery1[StreamedVoxelRenderComponent](f.cmd).Map(func(id EntityId, value *StreamedVoxelRenderComponent) bool {
					if id == right {
						value.Generation--
					}
					return true
				})
			case "entity removed":
				f.status(right, StreamedVoxelRenderReady)
				f.cmd.RemoveEntity(right)
			case "marker removed":
				f.status(right, StreamedVoxelRenderReady)
				f.cmd.RemoveComponents(right, &StreamedVoxelRenderComponent{})
			}
			f.app.FlushCommands()
			f.commitStage()
			f.visibility(proxy, false)
			f.visibility(left, true)
			if fault != "entity removed" {
				f.visibility(right, true)
			}
		})
	}
}

func TestStreamedRenderCoverageRequiresActualTargetsAndHonorsSourceEmptyContent(t *testing.T) {
	for _, mode := range []string{"genuinely empty", "missing loaded", "loaded without target", "empty with override", "empty with backing"} {
		t.Run(mode, func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			proxy := f.proxy()
			f.status(proxy, StreamedVoxelRenderReady)
			left := f.full(f.coords[0])
			f.status(left, StreamedVoxelRenderReady)
			entry := f.runtime.ImportedWorldEntries[f.coords[1]]
			if mode != "missing loaded" && mode != "loaded without target" {
				entry.NonEmptyVoxelCount = 0
				f.runtime.ImportedWorldEntries[f.coords[1]] = entry
			}
			switch mode {
			case "loaded without target":
				f.runtime.LoadedChunks[f.coords[1]] = &streamedLoadedChunk{}
			case "empty with override":
				f.runtime.importedWorldOverrideMap[importedWorldChunkRuntimeKey(f.runtime.BaseWorldID, entry.Coord)] = content.ImportedWorldChunkOverrideDef{WorldID: f.runtime.BaseWorldID, ChunkCoord: entry.Coord, SnapshotPath: "override.chunk.json"}
			case "empty with backing":
				f.runtime.BaseWorldBacking = NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
			}
			f.commitStage()
			complete := mode == "genuinely empty"
			f.visibility(proxy, complete)
			f.visibility(left, !complete)
		})
	}
}

func TestStreamedRenderExplicitEmptyPreparedOverrideCompletesCoverage(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left := f.full(f.coords[0])
	f.status(left, StreamedVoxelRenderReady)
	coord := f.coords[1]
	key := importedWorldChunkRuntimeKey(f.runtime.BaseWorldID, terrainCoordFromChunk(coord))
	f.runtime.importedWorldOverrideMap[key] = content.ImportedWorldChunkOverrideDef{WorldID: f.runtime.BaseWorldID, ChunkCoord: terrainCoordFromChunk(coord), SnapshotPath: "empty.chunk.json"}
	prepared := f.prepared(coord)
	prepared.ImportedWorldChunk.NonEmptyVoxelCount, prepared.ImportedWorldChunk.Voxels = 0, nil
	// A stale empty completion is not authoritative for this world.
	stale := prepared
	stale.Generation--
	f.runtime.PreparedLoads <- stale
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	loaded := f.runtime.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 0 {
		t.Fatal("true empty override must commit coverage without resurrecting source geometry")
	}
	f.visibility(proxy, true)
	f.visibility(left, false)
	// Repeated stages must preserve explicit emptiness rather than infer missing
	// target or revive old proxy mass.
	for range 3 {
		f.commitStage()
		f.visibility(proxy, true)
		f.visibility(left, false)
	}
}

func TestStreamedRenderTerrainAndProxylessTargetsRevealIndependently(t *testing.T) {
	for _, mode := range []string{"no proxy", "disabled proxy", "terrain"} {
		t.Run(mode, func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			var entity EntityId
			if mode == "terrain" {
				prepared := streamedPreparedChunk{Generation: f.runtime.Generation, Coord: f.coords[0], TerrainChunk: &content.TerrainChunkDef{TerrainID: f.runtime.TerrainID, ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}, NonEmptyVoxelCount: 1}}
				if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, prepared); err != nil {
					t.Fatal(err)
				}
				for id := range f.runtime.LoadedChunks[f.coords[0]].TerrainEntities {
					entity = id
				}
			} else {
				if mode == "disabled proxy" {
					f.runtime.Config.DisableSectorProxies = true
				} else {
					sector := f.runtime.ImportedWorldSectors[f.sector]
					sector.LODs = nil
					f.runtime.ImportedWorldSectors[f.sector] = sector
				}
				entity = f.full(f.coords[0])
			}
			f.marker(entity)
			f.commitStage()
			f.visibility(entity, true)
			f.status(entity, StreamedVoxelRenderReady)
			f.commitStage()
			f.visibility(entity, false)
		})
	}
}

func TestStreamedRenderCoarseningWaitsForCurrentReadyFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state StreamedVoxelRenderState
	}{
		{"unknown", StreamedVoxelRenderPendingBridge},
		{"pending bridge", StreamedVoxelRenderPendingBridge},
		{"uploading", StreamedVoxelRenderUploading},
		{"cancelled", StreamedVoxelRenderCancelled},
		{"failed", StreamedVoxelRenderFailed},
		{"wrong entity", StreamedVoxelRenderReady},
		{"wrong generation", StreamedVoxelRenderReady},
		{"ready", StreamedVoxelRenderReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			proxy := f.proxy()
			left, right := f.full(f.coords[0]), f.full(f.coords[1])
			f.status(left, StreamedVoxelRenderReady)
			f.status(right, StreamedVoxelRenderReady)
			ticket := f.status(proxy, tc.state)
			switch tc.name {
			case "unknown":
				delete(f.renderer.streamedVoxelTickets, ticket)
			case "wrong entity":
				f.renderer.streamedVoxelTickets[ticket].status.Entity = left
			case "wrong generation":
				f.renderer.streamedVoxelTickets[ticket].status.Generation--
			}
			// Complete full coverage can settle beside an unfinished hidden parent.
			// Do not regress a Ready ticket: S1a terminal states are latched.
			f.commitStage()
			f.visibility(proxy, true)
			f.visibility(left, false)
			f.visibility(right, false)
			f.cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{160, 0, 0}}, &StreamedLevelObserverComponent{})
			f.app.FlushCommands()
			f.observerStage()
			if tc.name == "ready" {
				if f.runtime.LoadedChunks[f.coords[0]] != nil || f.runtime.LoadedChunks[f.coords[1]] != nil {
					t.Fatal("ready fallback did not release out-of-keep full cohort")
				}
				f.visibility(proxy, false)
			} else {
				if f.runtime.LoadedChunks[f.coords[0]] == nil || f.runtime.LoadedChunks[f.coords[1]] == nil {
					t.Fatal("loaded but unready fallback authorized full eviction")
				}
				f.visibility(left, false)
				f.visibility(right, false)
				if _, requested := f.runtime.DesiredProxySectors[f.sector]; !requested {
					t.Fatal("unfinished fallback demand was lost")
				}
			}
		})
	}
}

func TestStreamedRenderObserverFinalDecisionSurvivesRemovalBeforeAdditionFlush(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(left, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderReady)
	// Initial markers remain hidden. In this PreUpdate both full tickets are ready
	// but observer movement chooses coarsening before any reveal is published.
	f.cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{160, 0, 0}}, &StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	f.visibility(proxy, false)
	if f.runtime.LoadedChunks[f.coords[0]] != nil || f.runtime.LoadedChunks[f.coords[1]] != nil {
		t.Fatal("observer failed to finish reverse handoff")
	}
}

func TestStreamedRenderHiddenFullKeepsCPUCollisionDestructionAndNavigation(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	f.runtime.BaseWorldCollisionEnabled = true
	f.runtime.CollisionChunks[f.coords[0]], f.runtime.DestructionChunks[f.coords[0]] = struct{}{}, struct{}{}
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	full := f.full(f.coords[0])
	f.marker(full)
	f.visibility(full, true)
	for _, component := range []any{RigidBodyComponent{}, ColliderComponent{}, AABBComponent{}, StreamedDestructionResidentComponent{}} {
		if f.cmd.GetComponent(full, reflect.TypeOf(component)) == nil {
			t.Fatalf("render-hidden target lost CPU component %T", component)
		}
	}
	if !StreamedLevelCollisionReadyInBounds(f.cmd, f.runtime, mgl32.Vec3{0.1, 0.1, 0.1}, mgl32.Vec3{0.9, 0.9, 0.9}) {
		t.Fatal("CPU collision readiness incorrectly waits for renderer")
	}
	f.runtime.NavigationRevision = 73
	f.runtime.NavigationSources = []content.NavSourceTileDef{{}}
	f.runtime.NavigationGraphs = []content.NavGraphTileDef{{}}
	sources := append([]content.NavSourceTileDef(nil), f.runtime.NavigationSources...)
	graphs := append([]content.NavGraphTileDef(nil), f.runtime.NavigationGraphs...)
	right := f.full(f.coords[1])
	f.status(full, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	if f.runtime.NavigationRevision != 73 || !reflect.DeepEqual(sources, f.runtime.NavigationSources) || !reflect.DeepEqual(graphs, f.runtime.NavigationGraphs) {
		t.Fatal("render handoff changed published navigation")
	}
	for _, component := range []any{RigidBodyComponent{}, ColliderComponent{}, AABBComponent{}, StreamedDestructionResidentComponent{}} {
		if f.cmd.GetComponent(full, reflect.TypeOf(component)) == nil {
			t.Fatalf("handoff removed %T", component)
		}
	}
}

func TestStreamedRenderHealthyPriorityChangesPreserveTickets(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	f.runtime.Config.DisableSectorProxies = true
	full := f.full(f.coords[0])
	ticket := f.marker(full).Ticket
	f.status(full, StreamedVoxelRenderUploading)
	f.runtime.BaseWorldCollisionEnabled = false
	for _, demand := range []struct {
		priority           StreamedVoxelPriority
		collision, desired bool
	}{
		{StreamedVoxelPriorityCollision, true, true}, {StreamedVoxelPriorityVisible, false, true}, {StreamedVoxelPriorityKeep, false, false},
	} {
		delete(f.runtime.CollisionChunks, f.coords[0])
		delete(f.runtime.DesiredChunks, f.coords[0])
		if demand.collision {
			f.runtime.CollisionChunks[f.coords[0]] = struct{}{}
		}
		if demand.desired {
			f.runtime.DesiredChunks[f.coords[0]] = struct{}{}
		}
		f.commitStage()
		marker := f.marker(full)
		if marker.Ticket != ticket || marker.Priority != demand.priority {
			t.Fatalf("priority refresh reticketed healthy work or ignored demand: %+v want ticket %d priority %v", marker, ticket, demand.priority)
		}
	}
}

func TestStreamedRenderFailedInvalidSourceDoesNotChurnAndRepairRenewsTicket(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(left, StreamedVoxelRenderReady)
	old := f.marker(right).Ticket
	model := mustVoxelModelForSpawnTest(t, f.cmd, right)
	MakeQuery1[VoxelModelComponent](f.cmd).Map(func(id EntityId, value *VoxelModelComponent) bool {
		if id == right {
			value.VoxelPalette = makeAssetId()
		}
		return true
	})
	f.status(right, StreamedVoxelRenderFailed)
	for range 3 {
		f.commitStage()
		if f.marker(right).Ticket != old {
			t.Fatal("invalid failed palette issued endless new tickets")
		}
		f.visibility(proxy, false)
	}
	MakeQuery1[VoxelModelComponent](f.cmd).Map(func(id EntityId, value *VoxelModelComponent) bool {
		if id == right {
			value.VoxelPalette = model.VoxelPalette
		}
		return true
	})
	f.commitStage()
	if f.marker(right).Ticket <= old || f.marker(left).Ticket == 0 {
		t.Fatal("repaired valid target needs fresh monotonic ticket")
	}
	if f.runtime.LoadedChunks[f.coords[0]] == nil || f.runtime.LoadedChunks[f.coords[1]] == nil {
		t.Fatal("retry reloaded healthy cohort")
	}
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
}

func TestStreamedRenderRetirementWaitsForBridgeWhileStoppedOrFailed(t *testing.T) {
	for _, lifecycle := range []string{"unload", "stop", "error"} {
		for _, status := range []StreamedVoxelRenderState{StreamedVoxelRenderReady, StreamedVoxelRenderUploading} {
			t.Run(lifecycle+string(rune('0'+status)), func(t *testing.T) {
				f := newStreamedRenderHarness(t, newVoxelRtStateTest())
				full := f.full(f.coords[0])
				ticket := f.status(full, status)
				if lifecycle == "stop" {
					if err := StopStreamedLevelRuntime(f.cmd); err != nil {
						t.Fatal(err)
					}
				} else {
					removeStreamedChunk(f.cmd, f.runtime, f.coords[0])
					if _, exists := f.renderer.StreamedVoxelStatus(ticket); !exists {
						t.Fatal("ticket forgotten before marker/entity removal flushed")
					}
					f.app.FlushCommands()
					if lifecycle == "error" {
						f.runtime.InitErr = errors.New("CPU prepare failure")
					}
				}
				if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, full) {
					t.Fatal("retirement left old marker live")
				}
				updateStreamedLevelObserverSystem(f.cmd, f.runtime)
				commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
				f.app.FlushCommands()
				_, exists := f.renderer.StreamedVoxelStatus(ticket)
				if status == StreamedVoxelRenderReady {
					if exists {
						t.Fatal("terminal retirement was not swept after ECS removal")
					}
				} else {
					if !exists {
						t.Fatal("unfinished ticket forgotten before bridge cancellation")
					}
					streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderUploading)
					voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
					streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderCancelled)
					updateStreamedLevelObserverSystem(f.cmd, f.runtime)
					commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
					f.app.FlushCommands()
					if _, exists := f.renderer.StreamedVoxelStatus(ticket); exists {
						t.Fatal("cancelled retirement not swept during stopped/error frames")
					}
				}
			})
		}
	}
}

func TestStreamedRenderTicketsAvoidExistingOwnersAndRemainMonotonicAcrossStart(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	f.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: 1, Generation: 12})
	f.renderer.streamedVoxelTickets = map[uint64]*streamedVoxelTicket{2: {status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderCancelled}}}
	f.app.FlushCommands()
	full := f.full(f.coords[0])
	first := f.marker(full).Ticket
	if first == 1 || first == 2 {
		t.Fatalf("allocator reused an existing marker/status ID: %d", first)
	}
	f.status(full, StreamedVoxelRenderReady)
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	levelPath := filepath.Join(t.TempDir(), "restart.level.json")
	if err := content.SaveLevel(levelPath, content.NewLevelDef("ticket restart")); err != nil {
		t.Fatal(err)
	}
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, StreamedLevelRuntimeConfig{LevelPath: levelPath}); err != nil {
		t.Fatal(err)
	}
	// Restart the same resource through its real entry point, then commit a new
	// runtime-owned imported target without relying on private ownership records.
	f.runtime.BaseWorldID = "next-world"
	next := f.full(ChunkCoord{X: 5})
	second := f.marker(next).Ticket
	if second <= first {
		t.Fatalf("stop/start reset ticket counter: first=%d second=%d", first, second)
	}
	f.renderer.streamedVoxelTickets[first] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: full, Generation: f.runtime.Generation - 1}}
	f.commitStage()
	f.visibility(next, true)
}

func TestStreamedRenderCancelledTargetRenewsWithoutReloadingHealthyCohort(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	leftTicket := f.status(left, StreamedVoxelRenderReady)
	old := f.marker(right).Ticket
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, old, StreamedVoxelRenderUploading)
	object := f.renderer.GetVoxelObject(right)
	object.XBrickMap.SetVoxel(0, 0, 0, 2)
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, old, StreamedVoxelRenderCancelled)
	f.commitStage()
	if f.marker(right).Ticket <= old || f.marker(left).Ticket != leftTicket {
		t.Fatal("cancelled target must renew alone with a fresh ticket")
	}
	if importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{1, 0, 0}) != right {
		t.Fatal("ticket renewal reloaded the CPU entity")
	}
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
}

func TestStreamedRenderMissingFallbackIsRequestedAndFullCoverageRetained(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(left, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	unloadStreamedSectorProxy(f.cmd, f.runtime, f.sector)
	f.app.FlushCommands()
	f.cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{160, 0, 0}}, &StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	if f.runtime.LoadedChunks[f.coords[0]] == nil || f.runtime.LoadedChunks[f.coords[1]] == nil {
		t.Fatal("missing fallback authorized full eviction")
	}
	if _, requested := f.runtime.DesiredProxySectors[f.sector]; !requested {
		t.Fatal("missing required fallback was not requested")
	}
	f.visibility(left, false)
	f.visibility(right, false)
}

func TestStreamedRenderStaleKeepDemandCannotHideIncompleteCohortFallback(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	f.runtime.DestructionChunks[f.coords[0]] = struct{}{}
	left := f.full(f.coords[0])
	f.status(left, StreamedVoxelRenderReady)
	f.commitStage()
	delete(f.runtime.KeepProxySectors, f.sector)
	delete(f.runtime.KeepSectors, f.sector)
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	// The v2 observer globally desires usable sector proxies, so this checks the
	// final visibility decision after selection rather than an eviction branch.
	f.cmd.AddEntity(&TransformComponent{}, &StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	if !hasComponentOfType[VoxelModelComponent](f.cmd, left) {
		t.Fatal("observer removed the required resident full entity")
	}
	f.visibility(proxy, false)
	f.visibility(left, true)
}

func TestStreamedRenderCompleteEmptyReplacementNeverRevivesOldProxyMass(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	for _, coord := range f.coords {
		entity := f.full(coord)
		f.status(entity, StreamedVoxelRenderReady)
	}
	f.commitStage()
	f.visibility(proxy, true)
	for _, coord := range f.coords {
		removeStreamedChunk(f.cmd, f.runtime, coord)
		prepared := f.prepared(coord)
		prepared.ImportedWorldChunk.NonEmptyVoxelCount, prepared.ImportedWorldChunk.Voxels = 0, nil
		key := importedWorldChunkRuntimeKey(f.runtime.BaseWorldID, terrainCoordFromChunk(coord))
		f.runtime.importedWorldOverrideMap[key] = content.ImportedWorldChunkOverrideDef{WorldID: f.runtime.BaseWorldID, ChunkCoord: terrainCoordFromChunk(coord), SnapshotPath: "removed.chunk.json"}
		f.runtime.PreparedLoads <- prepared
	}
	f.app.FlushCommands()
	f.commitStage()
	for range 3 {
		f.commitStage()
		f.visibility(proxy, true)
		for _, coord := range f.coords {
			loaded := f.runtime.LoadedChunks[coord]
			if loaded == nil || len(loaded.ImportedWorldEntities) != 0 {
				t.Fatal("complete empty prepared coverage respawned old source geometry")
			}
		}
	}
}

func TestStreamedRenderInstalledRendererCannotDisappearIntoCPUReadyMode(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(left, StreamedVoxelRenderReady)
	f.commitStage()
	delete(f.app.resources, reflect.TypeOf(VoxelRtState{}))
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
}

func TestStreamedRenderAuthoredPlacementVisibilityRemainsWithPlacementOwner(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	assetPath := filepath.Join(t.TempDir(), "placement.gkasset")
	writeProceduralAssetForLevelTest(t, assetPath, "placement-asset")
	f.runtime.Loader = NewRuntimeContentLoader()
	prepared := f.prepared(f.coords[0])
	prepared.PlacementItems = []streamedPlacementInstance{{PlacementID: "placement", AssetPath: assetPath, Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}}
	if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, prepared); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	placement := placementItemEntityByIDForStreamedTest(f.cmd, "placement", "placement-asset-part")
	if placement == 0 {
		t.Fatal("placement fixture failed to spawn voxel item")
	}
	f.visibility(placement, false)
	if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, placement) {
		t.Fatal("streaming took render ticket ownership of authored placement")
	}
	f.commitStage()
	f.visibility(placement, false)
}

func TestStreamedRenderCommittedBackingTargetStillRequiresItsOwnReadyTicket(t *testing.T) {
	for _, uploading := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "uploading"}[uploading], func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			proxy := f.proxy()
			f.status(proxy, StreamedVoxelRenderReady)
			left := f.full(f.coords[0])
			f.status(left, StreamedVoxelRenderReady)
			coord := f.coords[1]
			f.runtime.BaseWorldBacking = NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{Coord: terrainCoordFromChunk(coord), ChunkSize: 16, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
			f.runtime.DestructionChunks[coord] = struct{}{}
			entry := f.runtime.ImportedWorldEntries[coord]
			entry.NonEmptyVoxelCount = 0
			f.runtime.ImportedWorldEntries[coord] = entry
			prepared := f.prepared(coord)
			prepared.ImportedWorldChunk.NonEmptyVoxelCount, prepared.ImportedWorldChunk.Voxels = 0, nil
			if _, err := commitPreparedStreamedChunk(f.cmd, f.assets, f.runtime, prepared); err != nil {
				t.Fatal(err)
			}
			loaded := f.runtime.LoadedChunks[coord]
			if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
				t.Fatal("backed empty payload must retain its effective-content target")
			}
			var target EntityId
			for id := range loaded.ImportedWorldEntities {
				target = id
			}
			if !hasComponentOfType[VoxelBackingComponent](f.cmd, target) {
				t.Fatal("backing target fixture lost backing provider")
			}
			f.marker(target)
			if uploading {
				f.status(target, StreamedVoxelRenderUploading)
			}
			f.commitStage()
			f.visibility(proxy, false)
			f.visibility(left, true)
			f.visibility(target, true)
			f.status(target, StreamedVoxelRenderReady)
			f.commitStage()
			f.visibility(proxy, true)
			f.visibility(left, false)
			f.visibility(target, false)
		})
	}
}

func TestStreamedRenderPartialCommitFailureRetiresFlushedTargetOnStop(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status StreamedVoxelRenderState
	}{
		{"ready", StreamedVoxelRenderReady},
		{"uploading", StreamedVoxelRenderUploading},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamedRenderHarness(t, newVoxelRtStateTest())
			f.runtime.Loader = NewRuntimeContentLoader()
			prepared := f.prepared(f.coords[0])
			prepared.PlacementItems = []streamedPlacementInstance{{
				PlacementID: "missing-placement",
				AssetPath:   filepath.Join(t.TempDir(), "missing.gkasset"),
				Transform:   content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
			}}
			f.runtime.PreparedLoads <- prepared
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			if f.runtime.InitErr == nil {
				t.Fatal("invalid placement must fail after the imported voxel spawn")
			}
			// Imported spawning flushed before the later placement error. Observe
			// that live descendant without relying on a successful chunk record.
			entity := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{0, 0, 0})
			if entity == 0 || !hasComponentOfType[VoxelModelComponent](f.cmd, entity) {
				t.Fatal("fixture did not reach a flushed partial voxel commit")
			}
			f.marker(entity)
			f.visibility(entity, true)
			ticket := f.status(entity, tc.status)
			updateStreamedLevelObserverSystem(f.cmd, f.runtime)
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			streamedStatus(t, f.renderer, ticket, tc.status)
			f.visibility(entity, true)
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, entity) || hasComponentOfType[VoxelModelComponent](f.cmd, entity) {
				t.Fatal("stop left the partially committed descendant or its marker live")
			}
			updateStreamedLevelObserverSystem(f.cmd, f.runtime)
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			if tc.status == StreamedVoxelRenderUploading {
				streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderUploading)
				voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
				streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderCancelled)
				updateStreamedLevelObserverSystem(f.cmd, f.runtime)
				commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
				f.app.FlushCommands()
			}
			if _, exists := f.renderer.StreamedVoxelStatus(ticket); exists {
				t.Fatal("stopped runtime did not sweep the partial-commit terminal ticket")
			}
			voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
			if _, exists := f.renderer.StreamedVoxelStatus(ticket); exists {
				t.Fatal("bridge recreated a retired ticket from a surviving partial-commit marker")
			}
		})
	}
}

func TestStreamedRenderRemovedUploadingMarkerRenewsOnSameOwnedEntity(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	healthyTicket := f.status(left, StreamedVoxelRenderReady)
	oldTicket := f.marker(right).Ticket
	f.commitStage()
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, oldTicket, StreamedVoxelRenderUploading)
	f.cmd.RemoveComponents(right, &StreamedVoxelRenderComponent{})
	f.app.FlushCommands()
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, oldTicket, StreamedVoxelRenderCancelled)
	if !f.cmd.EntityExists(right) || !hasComponentOfType[VoxelModelComponent](f.cmd, right) {
		t.Fatal("external marker removal must leave the owned CPU target live")
	}
	f.commitStage()
	renewed := f.marker(right)
	if renewed.Ticket <= oldTicket || f.marker(left).Ticket != healthyTicket {
		t.Fatal("detached uploading marker must renew alone with a fresh current-generation ticket")
	}
	if importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{1, 0, 0}) != right {
		t.Fatal("marker recovery reloaded the CPU entity")
	}
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
	// Replacement is now flushed; a subsequent runtime sweep may forget the old
	// terminal ticket without allowing its old marker to be observed again.
	f.commitStage()
	if _, exists := f.renderer.StreamedVoxelStatus(oldTicket); exists {
		t.Fatal("recovered marker did not retire its cancelled predecessor")
	}
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, renewed.Ticket, StreamedVoxelRenderUploading)
	if _, exists := f.renderer.StreamedVoxelStatus(oldTicket); exists {
		t.Fatal("bridge recreated a retired ticket during marker recovery")
	}
}

func TestStreamedRenderExternallyRemovedProxyCanBePreparedAgainForCoarsening(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	oldTicket := f.status(proxy, StreamedVoxelRenderReady)
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	leftTicket := f.status(left, StreamedVoxelRenderReady)
	rightTicket := f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, true)
	f.visibility(left, false)
	f.visibility(right, false)
	// Remove directly through ECS, bypassing the streaming unload helper.
	f.cmd.RemoveEntity(proxy)
	f.app.FlushCommands()
	f.cmd.AddEntity(&TransformComponent{Position: mgl32.Vec3{160, 0, 0}}, &StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	if !f.cmd.EntityExists(left) || !f.cmd.EntityExists(right) {
		t.Fatal("externally removed fallback authorized full-cohort eviction")
	}
	f.visibility(left, false)
	f.visibility(right, false)
	if _, requested := f.runtime.DesiredProxySectors[f.sector]; !requested {
		t.Fatal("observer did not request replacement fallback")
	}
	prepared := streamedPreparedSectorProxyForTest(f.sector)
	prepared.Generation = f.runtime.Generation
	f.runtime.PreparedProxyLoads <- prepared
	f.commitStage()
	loaded := f.runtime.LoadedSectorProxies[f.sector]
	if loaded == nil || loaded.Entity == proxy || !f.cmd.EntityExists(loaded.Entity) {
		t.Fatal("prepared replacement was ignored because the old proxy entry remained stale")
	}
	replacement := loaded.Entity
	marker := f.marker(replacement)
	if marker.Ticket <= oldTicket || marker.Ticket == leftTicket || marker.Ticket == rightTicket {
		t.Fatal("replacement proxy did not receive a fresh unique ticket")
	}
	f.visibility(replacement, true)
	if _, known := f.renderer.StreamedVoxelStatus(marker.Ticket); known {
		t.Fatal("streaming authorized replacement readiness before bridge observation")
	}
	f.observerStage()
	if !f.cmd.EntityExists(left) || !f.cmd.EntityExists(right) {
		t.Fatal("unknown replacement fallback evicted the complete full cohort")
	}
	f.visibility(left, false)
	f.visibility(right, false)
	f.status(replacement, StreamedVoxelRenderReady)
	f.observerStage()
	if f.cmd.EntityExists(left) || f.cmd.EntityExists(right) {
		t.Fatal("current Ready replacement did not finish reverse handoff")
	}
	f.visibility(replacement, false)
}

func TestStreamedRenderLateRendererInstallationStagesExistingOwnedTargets(t *testing.T) {
	f := newStreamedRenderHarness(t, nil)
	proxy := f.proxy()
	left := f.full(f.coords[0])
	f.runtime.PreparedLoads <- f.prepared(f.coords[1])
	terrainCoord := ChunkCoord{X: 3}
	preparedTerrain := streamedPreparedChunk{Generation: f.runtime.Generation, Coord: terrainCoord, TerrainChunk: &content.TerrainChunkDef{
		TerrainID: f.runtime.TerrainID, Coord: terrainCoordFromChunk(terrainCoord), ChunkSize: 16, VoxelResolution: 1, SolidValue: 1,
		Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}, NonEmptyVoxelCount: 1,
	}}
	f.runtime.DesiredChunks[terrainCoord], f.runtime.KeepChunks[terrainCoord] = struct{}{}, struct{}{}
	f.runtime.PreparedLoads <- preparedTerrain
	f.commitStage()
	right := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{1, 0, 0})
	terrain := terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{3, 0, 0})
	if left == 0 || right == 0 || terrain == 0 {
		t.Fatal("CPU-only fixture did not commit all owned targets")
	}
	f.visibility(proxy, true)
	for _, entity := range []EntityId{left, right, terrain} {
		f.visibility(entity, false)
	}
	f.renderer = newVoxelRtStateTest()
	f.cmd.AddResources(f.renderer)
	f.commitStage()
	seen := make(map[uint64]bool)
	for _, entity := range []EntityId{proxy, left, right, terrain} {
		marker := f.marker(entity)
		if seen[marker.Ticket] {
			t.Fatal("late renderer adoption duplicated an owned ticket")
		}
		seen[marker.Ticket] = true
		f.visibility(entity, true)
		if _, known := f.renderer.StreamedVoxelStatus(marker.Ticket); known {
			t.Fatal("late renderer adoption wrote a renderer transition")
		}
	}
	f.status(proxy, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
	f.visibility(terrain, true)
	f.status(left, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	f.visibility(proxy, true)
	f.visibility(left, false)
	f.visibility(right, false)
	f.visibility(terrain, true)
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	scene := f.renderer.RtApp.Scene
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if len(scene.Objects) != 4 || len(scene.VisibleObjects) != 2 {
		t.Fatal("late renderer handoff failed to retain four objects and expose only complete full cohort")
	}
	for _, object := range scene.VisibleObjects {
		if object == f.renderer.GetVoxelObject(proxy) || object == f.renderer.GetVoxelObject(terrain) {
			t.Fatal("late renderer handoff exposed unready terrain or hidden proxy")
		}
	}
}
