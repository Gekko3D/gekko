package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func s1fRuntime(t *testing.T, specs []s1dChunkSpec, radius, prefetch int) (*streamedRenderHarness, EntityId) {
	t.Helper()
	cmd, state, assets := s1dRuntime(t, specs, radius, prefetch, prefetch)
	observer := s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	// Observe real authored demand without dispatching it: each test controls
	// worker completion order, independently of S1d's admission order.
	updateStreamedObserverSelection(cmd, state)
	return &streamedRenderHarness{t: t, app: cmd.app, cmd: cmd, assets: assets, runtime: state}, observer
}

func s1fStartFull(f *streamedRenderHarness, coord ChunkCoord) {
	f.runtime.PendingLoads[coord] = struct{}{}
	startStreamedChunkPrepareJob(f.runtime, buildStreamedChunkLoadJob(f.runtime, coord))
}

func s1fStartProxy(f *streamedRenderHarness, coord ChunkCoord) {
	f.runtime.PendingProxyLoads[coord] = struct{}{}
	startStreamedSectorProxyPrepareJob(f.runtime, buildStreamedSectorProxyLoadJob(f.runtime, coord, f.runtime.ImportedWorldSectors[coord].LODs[0]))
}

func s1fPrepared(t *testing.T, f *streamedRenderHarness, coord ChunkCoord, proxy bool) {
	t.Helper()
	full, proxies := len(f.runtime.PreparedLoads), len(f.runtime.PreparedProxyLoads)
	if proxy {
		s1fStartProxy(f, coord)
		proxies++
	} else {
		s1fStartFull(f, coord)
		full++
	}
	// This observes publication only, before any new commit-system capture.
	s2bUntil(t, func() bool {
		return len(f.runtime.PreparedLoads) == full && len(f.runtime.PreparedProxyLoads) == proxies && streamedActivePrepareJobCounts(f.runtime) == 0
	})
}

func s1fCommitted(t *testing.T, f *streamedRenderHarness, want s1dAdmission) {
	t.Helper()
	f.commitStage()
	metrics := f.runtime.Metrics
	if metrics.ChunksCommittedLastFrame != 1 || metrics.LastCommitCoord != want.coord ||
		(metrics.ProxyChunksCommittedLastFrame == 1) != (want.kind == "proxy") {
		t.Fatalf("commit = coord %v full=%d proxy=%d, want %v", metrics.LastCommitCoord, metrics.FullChunksCommittedLastFrame, metrics.ProxyChunksCommittedLastFrame, want)
	}
	var entity EntityId
	if want.kind == "full" {
		entity = s1eImported(t, f, want.coord)
	} else {
		loaded := f.runtime.LoadedSectorProxies[want.coord]
		if loaded == nil {
			t.Fatal("real prepared proxy did not publish runtime residency")
		}
		entity = loaded.Entity
	}
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	if geometry, ok := ResolveVoxelGeometryMap(f.assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() != 1 {
		t.Fatal("ordered commit did not retain its real prepared geometry")
	}
}

func TestS1fCommitSignedFullTiesIgnoreWorkerCompletionOrder(t *testing.T) {
	want := []ChunkCoord{{X: -1, Y: -1, Z: -1}, {X: -1, Y: -1, Z: 1}, {X: -1, Y: 1, Z: -1}, {X: 1, Y: -1, Z: -1}}
	for _, order := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}} {
		var specs []s1dChunkSpec
		for _, index := range order {
			specs = append(specs, s1dChunkSpec{coord: want[index]})
		}
		f, _ := s1fRuntime(t, specs, 1, 1)
		for _, index := range order {
			s1fPrepared(t, f, want[index], false)
		}
		for _, coord := range want {
			s1fCommitted(t, f, s1dAdmission{coord, "full"})
		}
		if f.runtime.Metrics.PreparedQueueDepth != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
			t.Fatal("complete ordered frontier retained prepared work")
		}
	}
}

func TestS1fCommitSharedLivePriorityAndObsoleteCleanup(t *testing.T) {
	fallback, collision, current, prefetch, obsolete := ChunkCoord{X: 100}, ChunkCoord{}, ChunkCoord{X: 2}, ChunkCoord{X: -3}, ChunkCoord{X: 4}
	f, _ := s1fRuntime(t, []s1dChunkSpec{{coord: prefetch}, {coord: current}, {coord: collision}, {coord: fallback, proxy: true}, {coord: obsolete}}, 2, 3)
	for _, coord := range []ChunkCoord{obsolete, prefetch, current, collision} {
		s1fPrepared(t, f, coord, false)
	}
	s1fPrepared(t, f, fallback, true)
	s1eWork(t, f.runtime, 5, 32, 0, 0)
	s1fCommitted(t, f, s1dAdmission{fallback, "proxy"})
	if f.runtime.LoadedChunks[obsolete] != nil {
		t.Fatal("out-of-demand result unexpectedly committed")
	}
	// Normal obsolete cleanup runs before the valid frontier consumes the
	// count budget. Its payload and admission ownership must already be gone.
	s1eWork(t, f.runtime, 3, 32, 0, 0)
	for _, coord := range []ChunkCoord{collision, current, prefetch} {
		s1fCommitted(t, f, s1dAdmission{coord, "full"})
	}
	s1eWork(t, f.runtime, 0, 32, 0, 0)
}

func TestS1fCommitAgedDetailBeatsContinuouslyArrivingFallbacks(t *testing.T) {
	detail := ChunkCoord{X: 2}
	specs := []s1dChunkSpec{{coord: detail}}
	for index := range 17 {
		specs = append(specs, s1dChunkSpec{coord: ChunkCoord{X: 100 + index}, proxy: true})
	}
	f, _ := s1fRuntime(t, specs, 2, 2)
	s1fPrepared(t, f, detail, false)
	for index := range 17 {
		fallback := ChunkCoord{X: 100 + index}
		s1fPrepared(t, f, fallback, true)
		f.commitStage()
		if f.runtime.LoadedChunks[detail] != nil {
			s1eImported(t, f, detail)
			return
		}
		if f.runtime.Metrics.ChunksCommittedLastFrame != 1 || f.runtime.LoadedSectorProxies[fallback] == nil {
			t.Fatal("fresh competing fallback did not commit exactly once")
		}
	}
	t.Fatal("old current-detail result starved through 17 fresh fallback arrivals")
}

func TestS1fCommitSpawnHookArrivalWaitsForNextFrontier(t *testing.T) {
	f, _ := s1fRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 1, 1)
	// Use a real authored terrain chunk to enter the production spawn hook.
	terrainLevelPath := s2bWorldPath(t)
	terrainLevel, err := content.LoadLevel(terrainLevelPath)
	if err != nil {
		t.Fatal(err)
	}
	level, err := content.LoadLevel(f.runtime.LevelPath)
	if err != nil {
		t.Fatal(err)
	}
	level.Terrain = terrainLevel.Terrain
	level.Terrain.SourcePath = content.ResolveDocumentPath(level.Terrain.SourcePath, terrainLevelPath)
	level.Terrain.ManifestPath = content.ResolveDocumentPath(level.Terrain.ManifestPath, terrainLevelPath)
	if err := content.SaveLevel(f.runtime.LevelPath, level); err != nil {
		t.Fatal(err)
	}
	config := f.runtime.Config
	config.MaxChunkCommitsPerFrame = 2
	if err := RestartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	updateStreamedObserverSelection(f.cmd, f.runtime)
	first, later := ChunkCoord{}, ChunkCoord{X: 1}
	s1fPrepared(t, f, first, false)
	job := buildStreamedChunkLoadJob(f.runtime, later)
	path := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
	_, release, done := s2eHoldDecode(t, job.Loader, path, nil)
	f.runtime.PendingLoads[later] = struct{}{}
	startStreamedChunkPrepareJob(f.runtime, job)
	s2bUntil(t, func() bool { return job.Loader.Stats().LoadWaits == 1 })
	hooks := 0
	f.runtime.Config.TerrainHooks = []PostSpawnTerrainHook{func(_ *Commands, _ PostSpawnTerrainContext) {
		hooks++
		release()
		if result := s2bWait(t, done); result.err != nil {
			t.Fatal(result.err)
		}
		s2bUntil(t, func() bool {
			return len(f.runtime.PreparedLoads) == 1 && streamedActivePrepareJobCounts(f.runtime) == 0
		})
	}}
	f.commitStage()
	if hooks != 1 || f.runtime.LoadedChunks[first] == nil || len(f.runtime.LoadedChunks[first].TerrainEntities) != 1 {
		t.Fatal("frontier fixture did not commit real terrain through its spawn hook")
	}
	s1eImported(t, f, first)
	if f.runtime.LoadedChunks[later] != nil || f.runtime.Metrics.ChunksCommittedLastFrame != 1 || f.runtime.Metrics.PreparedChunkQueueDepth != 1 {
		t.Fatal("arrival during spawn hook entered the already captured commit frontier")
	}
	s1eWork(t, f.runtime, 1, 32, 0, 0)
	f.commitStage()
	s1eImported(t, f, later)
	if f.runtime.Metrics.PreparedQueueDepth != 0 {
		t.Fatal("next update did not consume the later worker arrival")
	}
}

func TestS1fCommitDeferredCancellationRetainsOwnershipThroughFailedStop(t *testing.T) {
	f, observer := s1fRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 1, 1)
	metadata := f.runtime.Loader.Stats().PinnedBytes
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		s1fPrepared(t, f, coord, false)
	}
	s1fCommitted(t, f, s1dAdmission{ChunkCoord{}, "full"})
	bytes := f.runtime.Metrics.PendingPreparedBytes
	if f.runtime.Metrics.PreparedChunkQueueDepth != 1 || bytes <= 0 || f.runtime.Loader.Stats().PinnedBytes <= metadata {
		t.Fatal("deferred result lost prepared bytes or decoded scope")
	}
	s1eWork(t, f.runtime, 1, 32, 0, 0)
	s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
	f.observerStage()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	path, generation := f.runtime.WorldDeltaPath, f.runtime.Generation
	f.runtime.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
	defer func() { f.runtime.WorldDeltaPath = path }()
	if err := StopStreamedLevelRuntime(f.cmd); err == nil {
		t.Fatal("fixture did not fail Stop before terminal acknowledgement")
	}
	f.runtime.WorldDeltaPath = path
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if !f.runtime.Initialized || f.runtime.Generation != generation || f.runtime.Metrics.PreparedChunkQueueDepth != 1 ||
		f.runtime.Metrics.PendingPreparedBytes != bytes || f.runtime.Loader.Stats().PinnedBytes <= metadata {
		t.Fatal("failed Stop discarded cancelled deferred result ownership")
	}
	s1eWork(t, f.runtime, 1, 32, 0, 0)
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if f.runtime.Metrics.PreparedQueueDepth != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.DecodedContentCachePinnedBytes != 0 {
		t.Fatal("successful Stop did not drain retained and transport ownership")
	}
	s1eWork(t, f.runtime, 0, 32, 0, 0)
}

func TestS1fCommitBoundedPreparedFrontierWithContinuingPublishers(t *testing.T) {
	var specs []s1dChunkSpec
	for index := range 8 {
		specs = append(specs, s1dChunkSpec{coord: ChunkCoord{X: index}}, s1dChunkSpec{coord: ChunkCoord{X: 100 + index}, proxy: true})
	}
	f, _ := s1fRuntime(t, specs, 7, 7)
	f.runtime.PreparedLoads = make(chan streamedPreparedChunk, 2)
	f.runtime.PreparedProxyLoads = make(chan streamedPreparedSectorProxy, 2)
	for index := range 8 {
		s1fStartFull(f, ChunkCoord{X: index})
		s1fStartProxy(f, ChunkCoord{X: 100 + index})
	}
	s2bUntil(t, func() bool { return len(f.runtime.PreparedLoads) == 2 && len(f.runtime.PreparedProxyLoads) == 2 })
	// Blocked publishers keep supplying the small transport channels while
	// each update consumes one commit from the bounded prepared frontier.
	for frame := range 16 {
		s2bUntil(t, func() bool {
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			return f.runtime.Metrics.PreparedQueueDepth > 0
		})
		f.commitStage()
		if frame < 2 {
			s2bUntil(t, func() bool { return len(f.runtime.PreparedLoads) == 2 && len(f.runtime.PreparedProxyLoads) == 2 })
			refreshStreamedRuntimeMetricsCounts(f.runtime)
		}
		metrics := f.runtime.Metrics
		if metrics.PreparedQueueDepth > 8 || metrics.PreparedQueueDepth != metrics.PreparedChunkQueueDepth+metrics.PreparedProxyQueueDepth {
			t.Fatal("prepared ownership exceeded four transport plus four retained results")
		}
		if metrics.ChunksCommittedLastFrame != 1 {
			t.Fatal("continuing publication escaped the one-chunk commit budget")
		}
	}
	f.runtime.jobs.Wait()
	for index := range 8 {
		s1eImported(t, f, ChunkCoord{X: index})
		loaded := f.runtime.LoadedSectorProxies[ChunkCoord{X: 100 + index}]
		if loaded == nil || !f.cmd.EntityExists(loaded.Entity) {
			t.Fatal("bounded frontier starved a publication lane")
		}
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.PreparedQueueDepth != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("bounded frontier retained completed payload ownership")
	}
	s1eWork(t, f.runtime, 0, 32, 0, 0)
}
