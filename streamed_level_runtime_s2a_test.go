package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s2aStartRuntime(t *testing.T, config StreamedLevelRuntimeConfig) (*App, *Commands, *StreamedLevelRuntimeState, *AssetServer) {
	t.Helper()
	if config.LevelPath == "" {
		config.LevelPath = filepath.Join(t.TempDir(), "s2a.gklevel")
		if err := content.SaveLevel(config.LevelPath, content.NewLevelDef("s2a")); err != nil {
			t.Fatal(err)
		}
	}
	app, cmd, state := newStreamedRuntimeHarness(t)
	assets := assetServerFromApp(app)
	if err := StartStreamedLevelRuntime(cmd, assets, config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := StopStreamedLevelRuntime(cmd); err != nil {
			t.Errorf("runtime cleanup: %v", err)
		}
	})
	return app, cmd, state, assets
}

func s2aCommitImported(t *testing.T, cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, coord ChunkCoord, key string) (EntityId, AssetId) {
	t.Helper()
	state.BaseWorldID = "s2a-world"
	chunk := &content.ImportedWorldChunkDef{
		WorldID: "s2a-world", Coord: content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
		ChunkSize: 16, VoxelResolution: 1,
		Voxels: []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}}, NonEmptyVoxelCount: 1,
	}
	geometry, _ := state.PreparedGeometryCache.getOrBuild(key, func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunk)
	})
	if _, err := commitPreparedStreamedChunk(cmd, assets, state, streamedPreparedChunk{
		Generation: state.Generation, Coord: coord, ImportedWorldChunk: chunk,
		PreparedImportedWorldGeometry: geometry, PreparedImportedWorldGeometryCacheKey: key,
	}); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	loaded := state.LoadedChunks[coord]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatalf("imported commit produced no unique entity: %+v", loaded)
	}
	for entity := range loaded.ImportedWorldEntities {
		model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		s2aAssertAsset(t, assets, model.GeometryAsset(), true)
		return entity, model.GeometryAsset()
	}
	panic("unreachable")
}

func s2aAssertRuntimeByteMetrics(t *testing.T, state *StreamedLevelRuntimeState) {
	t.Helper()
	stats := state.PreparedGeometryCache.snapshot()
	metrics := state.Metrics
	if metrics.PreparedGeometryCacheBytes != stats.Bytes ||
		metrics.PreparedGeometryCachePreparedBytes != stats.PreparedBytes ||
		metrics.PreparedGeometryCacheAssetBytes != stats.AssetBytes ||
		metrics.PreparedGeometryCachePinnedBytes != stats.PinnedBytes ||
		metrics.PreparedGeometryCacheMaxBytes != stats.MaxBytes ||
		metrics.PreparedGeometryCacheOverBudgetBytes != stats.OverBudgetBytes ||
		metrics.PreparedGeometryCacheBuildWaits != stats.BuildWaits ||
		metrics.PreparedGeometryCacheOversizedBypasses != stats.OversizedBypasses {
		t.Fatalf("runtime cache metrics do not match current owner pressure: metrics=%+v cache=%+v", metrics, stats)
	}
}

func TestS2aRuntimeCacheConfiguration(t *testing.T) {
	for _, config := range []struct {
		name    string
		entries int
		bytes   int64
		budget  int64
		warm    bool
	}{
		{"defaults", 0, 0, 128 << 20, true},
		{"large int64 budget", 4, 1 << 40, 1 << 40, true},
		{"entries disabled", -1, 1 << 40, 1 << 40, false},
		{"bytes disabled", 4, -1, 0, false},
	} {
		t.Run(config.name, func(t *testing.T) {
			app, _, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
				MaxPreparedGeometryCacheEntries: config.entries,
				MaxPreparedGeometryCacheBytes:   config.bytes,
			})
			geometry := s2aGeometry(1)
			state.PreparedGeometryCache.getOrBuild("config", func() *volume.XBrickMap { return geometry })
			_, hit := state.PreparedGeometryCache.getOrBuild("config", func() *volume.XBrickMap { return geometry.Copy() })
			if hit != config.warm {
				t.Fatalf("configured warm retention=%t, want %t", hit, config.warm)
			}
			app.callSystems(0, execute, DynamicUpdate)
			if stats := state.PreparedGeometryCache.snapshot(); stats.MaxBytes != config.budget {
				t.Fatalf("configured byte budget=%d, want %d", stats.MaxBytes, config.budget)
			}
			s2aAssertRuntimeByteMetrics(t, state)
		})
	}
}

func TestS2aRuntimeFrameTrimsWithoutChunkCommits(t *testing.T) {
	geometry := s2aGeometry(1)
	app, _, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		MaxPreparedGeometryCacheEntries: 1,
		MaxPreparedGeometryCacheBytes:   2 * s2aCharge(t, geometry),
	})
	cache := state.PreparedGeometryCache
	id, _ := cache.acquireAsset(assets, "old", geometry)
	cache.releaseAsset(assets, "old")
	done := make(chan struct{})
	go func() {
		defer close(done)
		cache.getOrBuild("worker-new", func() *volume.XBrickMap { return geometry.Copy() })
	}()
	s2aWait(t, done)
	s2aAssertAsset(t, assets, id, true)
	beforeCommits := state.Metrics.CommittedChunkCount
	app.callSystems(0, execute, DynamicUpdate)
	if state.Metrics.CommittedChunkCount != beforeCommits || state.Metrics.ChunksCommittedLastFrame != 0 {
		t.Fatal("fixture unexpectedly committed a chunk during maintenance")
	}
	s2aAssertAsset(t, assets, id, false)
	if stats := cache.snapshot(); stats.Bytes > stats.MaxBytes || stats.Entries > 1 || stats.OverBudgetBytes != 0 {
		t.Fatalf("frame without commits left deferred cache pressure: %+v", stats)
	}
	s2aAssertRuntimeByteMetrics(t, state)
}

func TestS2aRuntimeImportedUsersAndCollisionSurviveTinyBudget(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	state.BaseWorldCollisionEnabled = true
	firstCoord, secondCoord := ChunkCoord{}, ChunkCoord{X: 1}
	state.CollisionChunks[firstCoord], state.CollisionChunks[secondCoord] = struct{}{}, struct{}{}
	first, firstAsset := s2aCommitImported(t, cmd, assets, state, firstCoord, "same-full")
	second, secondAsset := s2aCommitImported(t, cmd, assets, state, secondCoord, "same-full")
	if firstAsset != secondAsset {
		t.Fatal("live same-key imported users did not share their registered asset")
	}
	state.PreparedGeometryCache.getOrBuild("pressure", func() *volume.XBrickMap { return s2aGeometry(8) })
	state.PreparedGeometryCache.trim(assets)
	for _, entity := range []EntityId{first, second} {
		if !hasComponentOfType[RigidBodyComponent](cmd, entity) || !hasComponentOfType[ColliderComponent](cmd, entity) || !hasComponentOfType[AABBComponent](cmd, entity) {
			t.Fatalf("live imported entity %d lost its CPU collision contract", entity)
		}
		model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		physics, ok := buildFallbackPhysicsModelFromVoxel(assets, nil, &model)
		if !ok || physics.Grid == nil {
			t.Fatalf("live imported entity %d cannot resolve a collision grid", entity)
		}
		if occupied, value := physics.Grid.GetVoxel(0, 0, 0); !occupied || value != 1 {
			t.Fatalf("live imported entity %d has unusable CPU collision geometry", entity)
		}
	}
	if err := unloadStreamedChunk(cmd, state, firstCoord); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	state.PreparedGeometryCache.trim(assets)
	s2aAssertAsset(t, assets, secondAsset, true)
	if err := unloadStreamedChunk(cmd, state, secondCoord); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	state.PreparedGeometryCache.trim(assets)
	s2aAssertAsset(t, assets, secondAsset, false)
	if len(cmd.GetAllComponents(first)) != 0 || len(cmd.GetAllComponents(second)) != 0 {
		t.Fatal("unload left imported entities alive")
	}
	if stats := state.PreparedGeometryCache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("last imported unload left cache ownership: %+v", stats)
	}
}

func TestS2aRuntimeHiddenProxyPinsGeometryUntilUnload(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	sectorCoord := ChunkCoord{}
	chunkCoord := ChunkCoord{X: 1}
	state.ImportedWorldSectors[sectorCoord] = content.ImportedWorldSectorDef{
		FullChunkRefs: []content.TerrainChunkCoordDef{{X: chunkCoord.X}},
	}
	state.ImportedWorldEntries[chunkCoord] = content.ImportedWorldChunkEntryDef{NonEmptyVoxelCount: 1}
	proxy := streamedPreparedSectorProxyForTest(sectorCoord)
	proxy.PreparedGeometryCacheKey = "proxy"
	proxy.PreparedGeometry, _ = state.PreparedGeometryCache.getOrBuild("proxy", func() *volume.XBrickMap { return proxy.PreparedGeometry })
	if count, err := commitPreparedStreamedSectorProxy(cmd, assets, state, proxy); err != nil || count != 1 {
		t.Fatalf("proxy commit count=%d error=%v", count, err)
	}
	proxyEntity := state.LoadedSectorProxies[sectorCoord].Entity
	proxyModel := mustVoxelModelComponentForLevelTest(t, cmd, proxyEntity)
	proxyAsset := proxyModel.GeometryAsset()
	s2aCommitImported(t, cmd, assets, state, chunkCoord, "full")
	reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
	cmd.app.FlushCommands()
	if !VoxelEntityRenderHidden(cmd, proxyEntity) {
		t.Fatal("fixture did not hide its resident fallback proxy after full coverage")
	}
	state.PreparedGeometryCache.getOrBuild("pressure", func() *volume.XBrickMap { return s2aGeometry(8) })
	state.PreparedGeometryCache.trim(assets)
	s2aAssertAsset(t, assets, proxyAsset, true)
	if hasComponentOfType[ColliderComponent](cmd, proxyEntity) {
		t.Fatal("proxy unexpectedly received full-chunk collision")
	}
	unloadStreamedSectorProxy(cmd, state, sectorCoord)
	cmd.app.FlushCommands()
	state.PreparedGeometryCache.trim(assets)
	s2aAssertAsset(t, assets, proxyAsset, false)
}

func TestS2aRuntimeUncachedGeometryReleasesOnUnload(t *testing.T) {
	for _, mode := range []string{"entry-disabled", "byte-disabled", "blank-key", "nil-cache"} {
		t.Run(mode, func(t *testing.T) {
			config := StreamedLevelRuntimeConfig{}
			if mode == "entry-disabled" {
				config.MaxPreparedGeometryCacheEntries = -1
			}
			if mode == "byte-disabled" {
				config.MaxPreparedGeometryCacheBytes = -1
			}
			_, cmd, state, assets := s2aStartRuntime(t, config)
			if mode == "nil-cache" {
				state.PreparedGeometryCache = nil
			}
			key := "full"
			if mode == "blank-key" {
				key = ""
			}
			unrelated := assets.RegisterSharedVoxelGeometry(s2aGeometry(1), "unrelated")
			coord := ChunkCoord{}
			entity, id := s2aCommitImported(t, cmd, assets, state, coord, key)
			model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
			if geometry, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &model); !ok || geometry == nil || geometry.GetVoxelCount() != 1 {
				t.Fatal("imported entity did not have a usable source before unload")
			}
			if err := unloadStreamedChunk(cmd, state, coord); err != nil {
				t.Fatal(err)
			}
			cmd.app.FlushCommands()
			s2aAssertAsset(t, assets, id, false)
			s2aAssertAsset(t, assets, unrelated, true)

			proxy := streamedPreparedSectorProxyForTest(coord)
			proxy.PreparedGeometryCacheKey = key
			if _, err := commitPreparedStreamedSectorProxy(cmd, assets, state, proxy); err != nil {
				t.Fatal(err)
			}
			loaded := state.LoadedSectorProxies[coord]
			if loaded == nil {
				t.Fatal("uncached proxy did not commit")
			}
			proxyModel := mustVoxelModelComponentForLevelTest(t, cmd, loaded.Entity)
			proxyID := proxyModel.GeometryAsset()
			s2aAssertAsset(t, assets, proxyID, true)
			if geometry, ok := ResolveVoxelGeometryMap(assetServerFromApp(cmd.app), &proxyModel); !ok || geometry == nil || geometry.GetVoxelCount() != 1 {
				t.Fatal("proxy did not have a usable source before unload")
			}
			unloadStreamedSectorProxy(cmd, state, coord)
			cmd.app.FlushCommands()
			s2aAssertAsset(t, assets, proxyID, false)
			s2aAssertAsset(t, assets, unrelated, true)
		})
	}
}

func TestS2aRuntimeStopClosesCacheAndRestartUsesFreshBudget(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1 << 40})
	path := state.LevelPath
	oldCache := state.PreparedGeometryCache
	unrelated := assets.RegisterSharedVoxelGeometry(s2aGeometry(1), "unrelated")
	_, live := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "live")
	_, warm := s2aCommitImported(t, cmd, assets, state, ChunkCoord{X: 1}, "warm")
	if err := unloadStreamedChunk(cmd, state, ChunkCoord{X: 1}); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	s2aAssertAsset(t, assets, warm, true)
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PreparedGeometryCacheBytes == 0 || state.Metrics.PreparedGeometryCachePinnedBytes == 0 {
		t.Fatal("fixture must publish live and warm cache storage before Stop")
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	s2aAssertAsset(t, assets, live, false)
	s2aAssertAsset(t, assets, warm, false)
	s2aAssertAsset(t, assets, unrelated, true)
	if stats := oldCache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
		t.Fatalf("stopped cache retained storage: %+v", stats)
	}
	// Stopped frame guards must not leave stale public pressure telemetry. Do
	// not refresh metrics manually after Stop, which would hide that defect.
	if state.Metrics.PreparedGeometryCacheBytes != 0 || state.Metrics.PreparedGeometryCachePreparedBytes != 0 ||
		state.Metrics.PreparedGeometryCacheAssetBytes != 0 || state.Metrics.PreparedGeometryCachePinnedBytes != 0 ||
		state.Metrics.PreparedGeometryCacheOverBudgetBytes != 0 || state.Metrics.PreparedGeometryCacheEntries != 0 {
		t.Fatalf("Stop left stale cache ownership metrics: %+v", state.Metrics)
	}
	if err := StartStreamedLevelRuntime(cmd, assets, StreamedLevelRuntimeConfig{LevelPath: path, MaxPreparedGeometryCacheBytes: 1}); err != nil {
		t.Fatal(err)
	}
	if state.PreparedGeometryCache == oldCache || state.PreparedGeometryCache.snapshot().MaxBytes != 1 {
		t.Fatal("restart reused discarded cache ownership or ignored new byte config")
	}
	_, restarted := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "restart")
	if err := unloadStreamedChunk(cmd, state, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	s2aAssertAsset(t, assets, restarted, false)
	if stats := state.PreparedGeometryCache.snapshot(); stats.Bytes != 0 {
		t.Fatalf("restarted tiny-budget cache retained warm geometry: %+v", stats)
	}
}

func TestS2aRuntimeFailedPersistenceKeepsLiveCacheOwnership(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	entity, id := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "live")
	cache, generation := state.PreparedGeometryCache, state.Generation
	before := cache.snapshot()
	originalDeltaPath := state.WorldDeltaPath
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	state.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
	defer func() { state.WorldDeltaPath = originalDeltaPath }()
	if err := StopStreamedLevelRuntime(cmd); err == nil {
		t.Fatal("fixture did not fail persistence")
	}
	if !state.Initialized || state.Generation != generation || state.PreparedGeometryCache != cache {
		t.Fatal("failed Stop abandoned the still-active world's owner")
	}
	s2aAssertAsset(t, assets, id, true)
	if !hasComponentOfType[VoxelModelComponent](cmd, entity) {
		t.Fatal("failed Stop removed a live imported entity")
	}
	if after := cache.snapshot(); after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes || after.Entries != before.Entries {
		t.Fatalf("failed persistence cleared live cache charge: before=%+v after=%+v", before, after)
	}
	if err := unloadStreamedChunk(cmd, state, ChunkCoord{}); err != nil {
		t.Fatal(fmt.Errorf("live world stopped being usable after failed Stop: %w", err))
	}
}
