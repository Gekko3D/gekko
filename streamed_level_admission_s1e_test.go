package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// Reflection lets the pre-S1e implementation reach admission behavior failures.
// The public configuration and metric types are required separately below.
func s1eLimit(t *testing.T, config *StreamedLevelRuntimeConfig, limit int, required bool) {
	t.Helper()
	field := reflect.ValueOf(config).Elem().FieldByName("MaxStreamingWorkItems")
	if !field.IsValid() {
		if required {
			t.Fatal("missing public configuration MaxStreamingWorkItems")
		}
		return
	}
	if field.Kind() != reflect.Int {
		t.Fatal("MaxStreamingWorkItems must have type int")
	}
	field.SetInt(int64(limit))
}

type s1eWorkMetrics struct {
	count, max, over, carry int
	blocked                 uint64
}

func s1eMetrics(t *testing.T, state *StreamedLevelRuntimeState) s1eWorkMetrics {
	t.Helper()
	value := reflect.ValueOf(state.Metrics)
	var result s1eWorkMetrics
	for name, target := range map[string]*int{
		"StreamingWorkCount": &result.count, "StreamingWorkMaxCount": &result.max,
		"StreamingWorkOverBudgetCount": &result.over, "StreamingWorkCarryoverCount": &result.carry,
	} {
		field := value.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.Int {
			t.Fatalf("public metric %s must have type int", name)
		}
		*target = int(field.Int())
	}
	field := value.FieldByName("StreamingWorkAdmissionBlockedCount")
	if !field.IsValid() || field.Kind() != reflect.Uint64 {
		t.Fatal("StreamingWorkAdmissionBlockedCount must have type uint64")
	}
	result.blocked = field.Uint()
	return result
}

func s1eWork(t *testing.T, state *StreamedLevelRuntimeState, count, max, over, carry int) s1eWorkMetrics {
	t.Helper()
	refreshStreamedRuntimeMetricsCounts(state)
	got := s1eMetrics(t, state)
	if got.count != count || got.max != max || got.over != over || got.carry != carry {
		t.Fatalf("streaming work = %+v, want count=%d max=%d over=%d carry=%d", got, count, max, over, carry)
	}
	return got
}

func s1eRuntime(t *testing.T, specs []s1dChunkSpec, limit, radius int, renderer bool, editWorld ...func(*content.ImportedWorldDef)) (*streamedRenderHarness, EntityId) {
	t.Helper()
	cmd, state, assets := s1dRuntime(t, specs, radius, radius, radius, editWorld...)
	f := &streamedRenderHarness{t: t, app: cmd.app, cmd: cmd, assets: assets, runtime: state}
	for _, spec := range specs {
		f.coords = append(f.coords, spec.coord)
	}
	if renderer {
		f.renderer = newVoxelRtStateTest()
		cmd.AddResources(f.renderer)
		cmd.app.FlushCommands()
	}
	config := state.Config
	s1eLimit(t, &config, limit, false)
	if err := RestartStreamedLevelRuntime(cmd, assets, config); err != nil {
		t.Fatal(err)
	}
	observer := s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	return f, observer
}

func s1eQueued(t *testing.T, state *StreamedLevelRuntimeState, count int) {
	t.Helper()
	s2bUntil(t, func() bool {
		return len(state.PreparedLoads)+len(state.PreparedProxyLoads) == count && streamedActivePrepareJobCounts(state) == 0
	})
}

func s1eDispatches(t *testing.T, state *StreamedLevelRuntimeState, want uint64) {
	t.Helper()
	got, _, _ := s1dDispatchMetric(t, state, true)
	if got != want {
		t.Fatalf("actual preparation dispatches = %d, want %d", got, want)
	}
}

func s1eImported(t *testing.T, f *streamedRenderHarness, coord ChunkCoord) EntityId {
	t.Helper()
	loaded := f.runtime.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("real full chunk %v did not commit one imported target", coord)
	}
	for entity := range loaded.ImportedWorldEntities {
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
		geometry, ok := ResolveVoxelGeometryMap(f.assets, &model)
		if !ok || geometry == nil || geometry.GetVoxelCount() == 0 {
			t.Fatal("committed imported target has no usable geometry")
		}
		return entity
	}
	panic("unreachable")
}

func TestS1eStreamingWorkConfiguration(t *testing.T) {
	for _, limit := range []int{0, 1, 3, -1} {
		app, cmd, _ := newStreamedRuntimeHarness(t)
		path := filepath.Join(t.TempDir(), "level.gklevel")
		if err := content.SaveLevel(path, content.NewLevelDef("s1e-config")); err != nil {
			t.Fatal(err)
		}
		config := StreamedLevelRuntimeConfig{LevelPath: path}
		s1eLimit(t, &config, limit, true)
		err := StartStreamedLevelRuntime(cmd, assetServerFromApp(app), config)
		if limit < 0 {
			if err == nil {
				t.Fatal("negative streaming work limit was accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := limit
		if want == 0 {
			want = 32
		}
		s1eWork(t, streamedLevelRuntimeStateFromApp(app), 0, want, 0, 0)
		if err := StopStreamedLevelRuntime(cmd); err != nil {
			t.Fatal(err)
		}
	}
}

func TestS1eStreamingWorkRunningAndQueuedBackpressure(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(map[bool]string{true: "running", false: "queued-worker-free"}[running], func(t *testing.T) {
			f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}, {coord: ChunkCoord{X: 2}}}, 1, 2, false)
			if running {
				f.runtime.Config.MaxPrepareJobs = 2
				manifest := content.ResolveDocumentPath(f.runtime.Level.BaseWorld.ManifestPath, f.runtime.LevelPath)
				path := content.ResolveImportedWorldChunkPath(f.runtime.ImportedWorldEntries[ChunkCoord{}], manifest)
				_, release, done := s2eHoldDecode(t, f.runtime.Loader, path, nil)
				s2eDispatchHeld(t, f.cmd, f.runtime)
				// Another worker is available, so only the combined allowance can
				// explain retaining all three eligible chunks behind one attempt.
				s1eDispatches(t, f.runtime, 1)
				s1eWork(t, f.runtime, 1, 1, 0, 0)
				release()
				if result := s2bWait(t, done); result.err != nil {
					t.Fatal(result.err)
				}
			} else {
				f.observerStage()
			}
			s1eQueued(t, f.runtime, 1)
			var before uint64
			if field := reflect.ValueOf(f.runtime.Metrics).FieldByName("StreamingWorkAdmissionBlockedCount"); field.IsValid() {
				before = field.Uint()
			}
			for update := uint64(1); update <= 2; update++ {
				f.observerStage()
				// A completed worker does not release its queued result's item.
				s1eDispatches(t, f.runtime, 1)
				if got := s1eWork(t, f.runtime, 1, 1, 0, 0).blocked; got != before+update {
					t.Fatalf("blocked observer updates = %d, want %d", got, before+update)
				}
			}
			f.commitStage()
			s1eImported(t, f, ChunkCoord{})
			s1eWork(t, f.runtime, 0, 1, 0, 0)
			f.observerStage()
			s1eDispatches(t, f.runtime, 2)
			s1eQueued(t, f.runtime, 1)
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			s1eWork(t, f.runtime, 0, 1, 0, 0)
		})
	}
}

func TestS1eStreamingWorkSharedProxyFullHiddenCohortAndQualifiedReady(t *testing.T) {
	left, right := ChunkCoord{X: 2}, ChunkCoord{X: 3}
	f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: left, proxy: true}, {coord: right}}, 1, 3, true, func(world *content.ImportedWorldDef) {
		world.Sectors[0].FullChunkRefs = append(world.Sectors[0].FullChunkRefs, world.Sectors[1].FullChunkRefs...)
		world.Sectors[0].BoundsMax = world.Sectors[1].BoundsMax
		world.Sectors[0].NonEmptyVoxelCount = 2
		world.Sectors = world.Sectors[:1]
	})
	f.sector = left
	if got := s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets); got != (s1dAdmission{left, "proxy"}) {
		t.Fatalf("first shared admission = %v, want fallback", got)
	}
	proxy := f.runtime.LoadedSectorProxies[left].Entity
	f.status(proxy, StreamedVoxelRenderUploading)
	f.observerStage()
	s1eDispatches(t, f.runtime, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.status(proxy, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	if got := s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets); got != (s1dAdmission{left, "full"}) {
		t.Fatalf("full admission = %v, want first cohort child", got)
	}
	child := s1eImported(t, f, left)
	f.visibility(proxy, false)
	f.visibility(child, true)
	// Wrong entity/generation terminal reports cannot free the current item.
	for _, bad := range []StreamedVoxelRenderStatus{
		{State: StreamedVoxelRenderReady, Entity: proxy, Generation: f.runtime.Generation},
		{State: StreamedVoxelRenderFailed, Entity: child, Generation: f.runtime.Generation - 1},
		{State: StreamedVoxelRenderCancelled, Entity: child, Generation: f.runtime.Generation},
	} {
		old := f.marker(child).Ticket
		f.renderer.streamedVoxelTickets[old] = &streamedVoxelTicket{status: bad}
		f.commitStage()
		if f.marker(child).Ticket == old {
			t.Fatal("stale/cancelled live target did not receive a replacement ticket")
		}
		// Even a delayed, matching Ready report on the predecessor cannot
		// complete the live replacement's initial GPU work.
		f.renderer.streamedVoxelTickets[old] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: child, Generation: f.runtime.Generation}}
		f.observerStage()
		s1eDispatches(t, f.runtime, 2)
		s1eWork(t, f.runtime, 1, 1, 0, 0)
		f.visibility(child, true)
	}
	old := f.status(child, StreamedVoxelRenderUploading)
	MakeQuery1[StreamedVoxelRenderComponent](f.cmd).Map(func(entity EntityId, marker *StreamedVoxelRenderComponent) bool {
		if entity == child {
			marker.Generation--
		}
		return true
	})
	f.commitStage()
	if f.marker(child).Ticket == old {
		t.Fatal("mismatched live marker did not renew its uploading ticket")
	}
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.status(child, StreamedVoxelRenderReady)
	f.commitStage()
	// Replacement completion cannot abandon an older unfinished renderer
	// capture, even though the replacement itself qualifies as Ready.
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.observerStage()
	s1eDispatches(t, f.runtime, 2)
	f.renderer.streamedVoxelTickets[old] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderCancelled, Entity: child, Generation: f.runtime.Generation}}
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	f.visibility(child, true)
	f.visibility(proxy, false)
	// Individual completion must release capacity while the larger cohort
	// stays hidden; otherwise a limit-one runtime deadlocks refinement.
	if got := s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets); got != (s1dAdmission{right, "full"}) {
		t.Fatalf("remaining cohort admission = %v", got)
	}
	last := s1eImported(t, f, right)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.status(last, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	f.visibility(proxy, true)
	f.visibility(child, false)
	f.visibility(last, false)
	before := s1eMetrics(t, f.runtime).blocked
	f.observerStage()
	if s1eMetrics(t, f.runtime).blocked != before {
		t.Fatal("satisfied demand counted an admission block")
	}
}

func TestS1eStreamingWorkCombinedTargetsFailureRepairAndReadyEdits(t *testing.T) {
	config := StreamedLevelRuntimeConfig{LevelPath: s2bWorldPath(t), DisableSectorProxies: true, MaxPrepareJobs: 1, MaxChunkCommitsPerFrame: 1}
	s1eLimit(t, &config, 1, false)
	app, cmd, state, assets := s2aStartRuntime(t, config)
	renderer := newVoxelRtStateTest()
	cmd.AddResources(renderer)
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	f := &streamedRenderHarness{t: t, app: app, cmd: cmd, runtime: state, assets: assets, renderer: renderer}
	f.observerStage()
	s1eQueued(t, state, 1)
	f.commitStage()
	loaded := state.LoadedChunks[ChunkCoord{}]
	if loaded == nil || len(loaded.TerrainEntities) != 1 || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatal("real authored full attempt did not commit both terrain and imported targets")
	}
	imported := s1eImported(t, f, ChunkCoord{})
	var terrain EntityId
	for entity := range loaded.TerrainEntities {
		terrain = entity
	}
	model := mustVoxelModelComponentForLevelTest(t, cmd, terrain)
	if geometry, ok := ResolveVoxelGeometryMap(assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() == 0 {
		t.Fatal("authored terrain target has no usable geometry")
	}
	s1eWork(t, state, 1, 1, 0, 0)
	f.status(terrain, StreamedVoxelRenderUploading)
	// Failed source cannot immediately repair itself; its qualified terminal
	// failure ends initial admission without proving renderer coverage.
	transform := *cmd.GetComponent(imported, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	cmd.RemoveComponents(imported, &TransformComponent{})
	app.FlushCommands()
	failedTicket := f.status(imported, StreamedVoxelRenderFailed)
	f.commitStage()
	s1eWork(t, state, 1, 1, 0, 0)
	f.visibility(imported, true)
	cmd.AddComponents(imported, &transform)
	app.FlushCommands()
	f.commitStage()
	if f.marker(imported).Ticket <= failedTicket {
		t.Fatal("repaired terminally failed source did not receive a fresh ticket")
	}
	s1eWork(t, state, 2, 1, 1, 0)
	// The failed initial target stays completed while its sibling is still
	// uploading; repair is independent compatibility work.
	f.status(terrain, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, state, 1, 1, 0, 0)
	f.status(imported, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, state, 0, 1, 0, 0)
	// Ordinary live edits stay with the renderer upload owner after Ready.
	ready := f.marker(imported)
	voxelRtSystem(nil, renderer, assets, &Time{Dt: 1.0 / 60}, cmd, nil)
	object := renderer.GetVoxelObject(imported)
	if object == nil || object.XBrickMap == nil {
		t.Fatal("ready edited target did not reach renderer scene")
	}
	object.XBrickMap.SetVoxel(0, 0, 0, 2)
	voxelRtSystem(nil, renderer, assets, &Time{Dt: 1.0 / 60}, cmd, nil)
	f.commitStage()
	if f.marker(imported).Ticket != ready.Ticket {
		t.Fatal("ordinary Ready edit reissued initial load ticket")
	}
	s1eWork(t, state, 0, 1, 0, 0)
}

func TestS1eStreamingWorkCancellationWaitsForAcknowledgement(t *testing.T) {
	f, observer := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}}, 1, 1, false)
	manifest := content.ResolveDocumentPath(f.runtime.Level.BaseWorld.ManifestPath, f.runtime.LevelPath)
	path := content.ResolveImportedWorldChunkPath(f.runtime.ImportedWorldEntries[ChunkCoord{}], manifest)
	_, release, done := s2eHoldDecode(t, f.runtime.Loader, path, nil)
	s2eDispatchHeld(t, f.cmd, f.runtime)
	s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
	f.observerStage()
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	s3aMove(f.cmd, observer, mgl32.Vec3{1, 1, 1})
	f.observerStage()
	s1eDispatches(t, f.runtime, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	if s1eMetrics(t, f.runtime).blocked != 0 {
		t.Fatal("worker/pending attempt stall counted as allowance blockage")
	}
	release()
	if result := s2bWait(t, done); result.err != nil {
		t.Fatal(result.err)
	}
	s1eQueued(t, f.runtime, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.commitStage()
	if f.runtime.LoadedChunks[ChunkCoord{}] != nil || f.runtime.Metrics.PrepareCancelledCount != 1 {
		t.Fatal("cancelled attempt committed or did not acknowledge cancellation")
	}
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets)
	s1eDispatches(t, f.runtime, 2)
	s1eWork(t, f.runtime, 0, 1, 0, 0)
}

func TestS1eStreamingWorkAuthoredEmptyOverrideReleasesAtCommit(t *testing.T) {
	f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 1, 1, true)
	config, levelID, worldID := f.runtime.Config, f.runtime.LevelID, f.runtime.BaseWorldID
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(filepath.Dir(config.LevelPath), "empty.gkchunk")
	if err := content.SaveImportedWorldChunk(snapshot, &content.ImportedWorldChunkDef{
		SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion, WorldID: worldID,
		ChunkSize: 16, VoxelResolution: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveWorldDelta(content.DefaultWorldDeltaPath(config.LevelPath), &content.WorldDeltaDef{
		SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: levelID,
		ImportedWorldChunkOverrides: []content.ImportedWorldChunkOverrideDef{{WorldID: worldID, SnapshotPath: "empty.gkchunk"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	f.observerStage()
	s1eQueued(t, f.runtime, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.commitStage()
	loaded := f.runtime.LoadedChunks[ChunkCoord{}]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 0 {
		t.Fatal("authored empty override did not establish empty resident coverage")
	}
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	f.observerStage()
	s1eDispatches(t, f.runtime, 2)
}

func TestS1eStreamingWorkRetryAndPreparationErrorRelease(t *testing.T) {
	t.Run("pending-byte-retry", func(t *testing.T) {
		f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 2, 1, false)
		config := f.runtime.Config
		config.MaxPendingPreparedBytes = 1
		if err := RestartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
			t.Fatal(err)
		}
		f.app.FlushCommands()
		for count := 1; count <= 2; count++ {
			f.observerStage()
			s1eQueued(t, f.runtime, count)
		}
		if f.runtime.Metrics.PendingPreparedAdmissionRetries == 0 {
			refreshStreamedRuntimeMetricsCounts(f.runtime)
		}
		if f.runtime.Metrics.PendingPreparedAdmissionRetries != 1 {
			t.Fatal("actual second prepared payload did not retry against retained byte pressure")
		}
		s1eWork(t, f.runtime, 2, 2, 0, 0)
		f.commitStage()
		s1eImported(t, f, ChunkCoord{})
		s1eWork(t, f.runtime, 1, 2, 0, 0)
		f.commitStage()
		s1eWork(t, f.runtime, 0, 2, 0, 0)
		if f.runtime.LoadedChunks[ChunkCoord{X: 1}] != nil {
			t.Fatal("byte-admission retry incorrectly committed")
		}
		f.observerStage()
		s1eDispatches(t, f.runtime, 3)
	})
	t.Run("preparation-error", func(t *testing.T) {
		f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}}, 1, 1, false)
		manifest := content.ResolveDocumentPath(f.runtime.Level.BaseWorld.ManifestPath, f.runtime.LevelPath)
		path := content.ResolveImportedWorldChunkPath(f.runtime.ImportedWorldEntries[ChunkCoord{}], manifest)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		f.observerStage()
		s1eQueued(t, f.runtime, 1)
		s1eWork(t, f.runtime, 1, 1, 0, 0)
		commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
		f.app.FlushCommands()
		if f.runtime.InitErr == nil || f.runtime.Metrics.PrepareErrorCount != 1 {
			t.Fatal("missing authored source did not reach normal preparation error policy")
		}
		s1eWork(t, f.runtime, 0, 1, 0, 0)
	})
}

func TestS1eStreamingWorkCompatibilityPressureSurvivesMetricsReset(t *testing.T) {
	f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}, {coord: ChunkCoord{X: 2}}}, 1, 2, false)
	for _, position := range []content.Vec3{{1, 1, 1}, {17, 1, 1}} {
		if err := ensureStreamedChunkLoadedForPosition(f.cmd, f.assets, f.runtime, position); err != nil {
			t.Fatal(err)
		}
	}
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	f.renderer = newVoxelRtStateTest()
	f.cmd.AddResources(f.renderer)
	f.app.FlushCommands()
	f.commitStage()
	left, right := s1eImported(t, f, ChunkCoord{}), s1eImported(t, f, ChunkCoord{X: 1})
	f.marker(left)
	f.marker(right)
	s1eWork(t, f.runtime, 2, 1, 1, 0)
	// Gameplay remains synchronous even when two adopted targets already
	// exceed the asynchronous ceiling.
	if err := ensureStreamedChunkLoadedForPosition(f.cmd, f.assets, f.runtime, content.Vec3{33, 1, 1}); err != nil {
		t.Fatal(err)
	}
	third := s1eImported(t, f, ChunkCoord{X: 2})
	s1eWork(t, f.runtime, 3, 1, 2, 0)
	// Withdraw the third owned chunk after its Ready latch. Its still-current
	// observer demand supplies otherwise eligible asynchronous work.
	f.status(third, StreamedVoxelRenderReady)
	f.commitStage()
	removeStreamedChunk(f.cmd, f.runtime, ChunkCoord{X: 2})
	f.app.FlushCommands()
	f.commitStage()
	s1eWork(t, f.runtime, 2, 1, 1, 0)
	f.observerStage()
	s1eDispatches(t, f.runtime, 0)
	if s1eMetrics(t, f.runtime).blocked != 1 {
		t.Fatal("over-budget compatibility work did not block new asynchronous demand")
	}
	f.runtime.Metrics = StreamedLevelRuntimeMetrics{}
	f.observerStage()
	s1eDispatches(t, f.runtime, 0)
	s1eWork(t, f.runtime, 2, 1, 1, 0)
	f.status(left, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.observerStage()
	s1eDispatches(t, f.runtime, 0)
	f.status(right, StreamedVoxelRenderReady)
	f.commitStage()
	f.observerStage()
	s1eDispatches(t, f.runtime, 1)
}

func TestS1eStreamingWorkRemovedUploadingTargetAndFailedStop(t *testing.T) {
	f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 1, 1, true)
	s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets)
	entity := s1eImported(t, f, ChunkCoord{})
	ticket := f.status(entity, StreamedVoxelRenderUploading)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	original, generation := f.runtime.WorldDeltaPath, f.runtime.Generation
	f.runtime.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
	defer func() { f.runtime.WorldDeltaPath = original }()
	if err := StopStreamedLevelRuntime(f.cmd); err == nil {
		t.Fatal("fixture did not fail Stop before current-world teardown")
	}
	f.runtime.WorldDeltaPath = original
	if !f.runtime.Initialized || f.runtime.Generation != generation || !f.cmd.EntityExists(entity) {
		t.Fatal("failed Stop lost active generation or live target")
	}
	f.observerStage()
	s1eDispatches(t, f.runtime, 1)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	removeStreamedChunk(f.cmd, f.runtime, ChunkCoord{})
	// Buffered removal still leaves the old marker observable this stage.
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.commitStage()
	f.commitStage()
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderUploading)
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	streamedStatus(t, f.renderer, ticket, StreamedVoxelRenderCancelled)
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 0)
	if _, known := f.renderer.StreamedVoxelStatus(ticket); known {
		t.Fatal("removed terminal target did not finish renderer retirement")
	}
}

func TestS1eStreamingWorkPartialCommitStopCarryoverDoesNotBlockRestart(t *testing.T) {
	f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}}, 1, 1, true)
	f.observerStage()
	s1eQueued(t, f.runtime, 1)
	prepared := <-f.runtime.PreparedLoads
	prepared.PlacementItems = []streamedPlacementInstance{{PlacementID: "missing", AssetPath: filepath.Join(t.TempDir(), "missing.gkasset"), Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}}
	f.runtime.PreparedLoads <- prepared
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr == nil {
		t.Fatal("missing later placement did not fail after imported spawn flush")
	}
	entity := importedWorldChunkEntityByCoordForStreamedTest(f.cmd, [3]int{})
	if entity == 0 || !hasComponentOfType[VoxelModelComponent](f.cmd, entity) {
		t.Fatal("partial failed commit did not retain its flushed GPU target")
	}
	ticket := f.status(entity, StreamedVoxelRenderUploading)
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	config, generation := f.runtime.Config, f.runtime.Generation
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 1)
	if f.cmd.EntityExists(entity) {
		t.Fatal("successful Stop retained partial-commit entity")
	}
	delete(f.app.resources, reflect.TypeOf(VoxelRtState{}))
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 1)
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if f.runtime.Generation <= generation {
		t.Fatal("restart did not advance current-world generation")
	}
	s1dPrepareAndCommit(t, f.cmd, f.runtime, f.assets)
	next := s1eImported(t, f, ChunkCoord{})
	s1eWork(t, f.runtime, 0, 1, 0, 1)
	if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, next) {
		t.Fatal("renderer-absent new world did not retain CPU-only commit behavior")
	}
	f.visibility(next, false)
	f.cmd.AddResources(f.renderer)
	f.app.FlushCommands()
	f.commitStage()
	if f.marker(next).Ticket <= ticket {
		t.Fatal("new generation reused a retiring GPU ticket")
	}
	s1eWork(t, f.runtime, 1, 1, 0, 1)
	f.visibility(next, true)
	f.renderer.streamedVoxelTickets[ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderCancelled, Entity: entity, Generation: generation}}
	f.commitStage()
	s1eWork(t, f.runtime, 1, 1, 0, 0)
	f.visibility(next, true)
	f.status(next, StreamedVoxelRenderReady)
	f.commitStage()
	s1eWork(t, f.runtime, 0, 1, 0, 0)
}
