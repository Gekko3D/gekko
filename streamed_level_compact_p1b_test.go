package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Restart the authored fixture through the supported config before dispatching.
func p1bRuntime(t *testing.T, entries int, bytes int64) *streamedRenderHarness {
	t.Helper()
	f, _, _ := p5aRuntime(t, true, true)
	cfg := f.runtime.Config
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	cfg.CompactPreparedGeometry = true
	cfg.MaxPreparedGeometryCacheEntries, cfg.MaxPreparedGeometryCacheBytes = entries, bytes
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, cfg); err != nil {
		t.Fatal(err)
	}
	updateStreamedObserverSelection(f.cmd, f.runtime)
	return f
}

func p1bGeometry(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got == nil || !reflect.DeepEqual(got.Sectors, want.Sectors) ||
		got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) ||
		got.GetVoxelCount() != want.GetVoxelCount() || got.CachedMin != want.CachedMin || got.CachedMax != want.CachedMax || got.AABBDirty != want.AABBDirty ||
		got.StructureDirty != want.StructureDirty ||
		!reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) {
		t.Fatal("compact preparation changed authoritative geometry, metadata, bounds or clean state")
	}
}

func TestP1bCompactMixedMaterialAndHistoryParity(t *testing.T) {
	f := p1bRuntime(t, 8, 1<<30)
	sparse := []volume.VoxelWrite{
		{X: -32, Y: -32, Z: -32, Value: 255}, {X: -31, Y: -32, Z: -32, Value: 7},
		{X: -25, Y: -25, Z: -25, Value: 9}, {X: -32, Y: -32, Z: -32, Value: 255},
		{X: -31, Y: -32, Z: -32}, {X: -31, Y: -32, Z: -32, Value: 8},
		{X: -33, Y: -1, Z: -8, Value: 6}, {X: -33, Y: -1, Z: -8},
		{X: -33, Y: -1, Z: -8, Value: 10},
		{X: 96, Y: 96, Z: 96, Value: 11}, {X: 96, Y: 96, Z: 96},
	}
	source := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for x := 0; x < volume.BrickSize; x++ {
			for y := 0; y < volume.BrickSize; y++ {
				for z := 0; z < volume.BrickSize; z++ {
					if !yield(volume.VoxelWrite{X: x, Y: y, Z: z, Value: 13}) {
						return
					}
				}
			}
		}
		for _, write := range sparse {
			if !yield(write) {
				return
			}
		}
	})
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = []byte{0, 255, 3, 19, 0, 42}
	source.ComputeAABB()
	source.ClearDirty()
	want := source.Copy()
	want.ClearDirty()
	cache := f.runtime.PreparedGeometryCache
	cache.getOrBuildSource("mixed materials", true, func() *volume.XBrickMap { return source })
	if bytes := cache.snapshot().PreparedBytes; bytes <= 0 || bytes >= s2aCharge(t, source) {
		t.Fatal("compact qualified uniform/mixed source did not reduce retained storage")
	}
	got, hit := cache.getOrBuild("mixed materials", nil)
	if !hit {
		t.Fatal("qualified mixed source was not retained")
	}
	p1bGeometry(t, got, want)
	for _, point := range [][3]int{{-32, -32, -32}, {-31, -32, -32}, {-25, -25, -25}, {-33, -1, -8}, {96, 96, 96}, {7, 7, 7}, {-30, -32, -32}} {
		occupied, value := got.GetVoxel(point[0], point[1], point[2])
		wantOccupied, wantValue := want.GetVoxel(point[0], point[1], point[2])
		if occupied != wantOccupied || value != wantValue {
			t.Fatalf("promoted cell %v = (%t,%d), want (%t,%d)", point, occupied, value, wantOccupied, wantValue)
		}
	}
}

func TestP1bCompactFullProxyParityRetentionAndWarmReuse(t *testing.T) {
	f := p1bRuntime(t, 8, 1<<30)
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	full, proxy := <-f.runtime.PreparedLoads, <-f.runtime.PreparedProxyLoads
	wantFull := prepareImportedWorldChunkGeometry(full.ImportedWorldChunk, full.ImportedWorldAux)
	wantProxy := prepareImportedWorldChunkGeometry(proxy.Chunk, proxy.Aux)
	stats := f.runtime.PreparedGeometryCache.snapshot()
	denseBytes := s2aCharge(t, wantFull) + s2aCharge(t, wantProxy)
	if stats.PreparedBytes <= 0 || stats.PreparedBytes >= denseBytes || stats.AssetBytes != 0 {
		t.Fatalf("opt-in did not reduce retained prepared storage: %+v dense=%d", stats, denseBytes)
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.PendingPreparedBytes <= denseBytes {
		t.Fatal("pending packets did not charge their registration copies and retained sources")
	}
	f.runtime.PreparedLoads <- full
	f.runtime.PreparedProxyLoads <- proxy
	f.commitStage()
	f.commitStage()
	fullID, fullAsset := p5aAsset(t, f, false)
	proxyID, proxyAsset := p5aAsset(t, f, true)
	p1bGeometry(t, fullAsset.XBrickMap, wantFull)
	p1bGeometry(t, proxyAsset.XBrickMap, wantProxy)
	stats = f.runtime.PreparedGeometryCache.snapshot()
	if stats.PreparedBytes >= denseBytes || stats.AssetBytes != s2aCharge(t, fullAsset.XBrickMap)+s2aCharge(t, proxyAsset.XBrickMap) || stats.PinnedBytes != stats.Bytes {
		t.Fatalf("compact and independently registered dense storage lost attribution or pins: %+v", stats)
	}
	if p5aAdoptions(t, f.runtime) != 2 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("qualified full/proxy copies were not adopted and consumed")
	}
	fullAsset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[7] ^= 0x80
	p1bGeometry(t, proxyAsset.XBrickMap, wantProxy)
	s1fPrepared(t, f, ChunkCoord{}, false)
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	pendingBeforePromotion := f.runtime.Metrics.PendingPreparedBytes
	// The compatibility getter exposes an independent, authoritative source.
	source, hit := f.runtime.PreparedGeometryCache.getOrBuild(full.PreparedImportedWorldGeometryCacheKey, nil)
	if !hit || source == fullAsset.XBrickMap {
		t.Fatal("dense exposure lost the cache source or aliased live geometry")
	}
	p1bGeometry(t, source, wantFull)
	stats = f.runtime.PreparedGeometryCache.snapshot()
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if stats.PinnedBytes != stats.Bytes || stats.PreparedBytes < s2aCharge(t, source) ||
		f.runtime.Metrics.PendingPreparedBytes != pendingBeforePromotion || pendingBeforePromotion == 0 {
		t.Fatal("promotion lost acquired pins or released a captured pending source")
	}
	f.commitStage()
	if id, _ := p5aAsset(t, f, false); id != fullID {
		t.Fatal("promotion while acquired changed the live asset ID")
	}
	removeStreamedChunk(f.cmd, f.runtime, ChunkCoord{})
	unloadStreamedSectorProxy(f.cmd, f.runtime, ChunkCoord{})
	f.app.FlushCommands()
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	f.commitStage()
	f.commitStage()
	warmFull, _ := p5aAsset(t, f, false)
	warmProxy, _ := p5aAsset(t, f, true)
	if warmFull != fullID || warmProxy != proxyID || p5aAdoptions(t, f.runtime) != 2 {
		t.Fatal("warm compact/dense entries changed asset identity or adopted unused copies")
	}
}

func TestP1bDensePromotionRejectsCapturedStaleCandidate(t *testing.T) {
	f := p1bRuntime(t, 8, 1<<30)
	s1fPrepared(t, f, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	want := prepareImportedWorldChunkGeometry(prepared.ImportedWorldChunk, prepared.ImportedWorldAux)
	cache, key := f.runtime.PreparedGeometryCache, prepared.PreparedImportedWorldGeometryCacheKey
	source, hit := cache.getOrBuild(key, nil)
	if !hit {
		t.Fatal("dense exposure did not find compact source")
	}
	p1bGeometry(t, source, want)
	// Raw public writes deliberately bypass Revision and material metadata repair.
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[1][2][3] = 255
	if again, hit := cache.getOrBuild(key, nil); !hit || again != source {
		t.Fatal("promotion did not retain one stable dense authority")
	}
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	_, asset := p5aAsset(t, f, false)
	if _, value := asset.XBrickMap.GetVoxel(1, 2, 3); value != 255 || asset.XBrickMap == source {
		t.Fatal("stale compact registration replaced current dense raw edits or lost copy isolation")
	}
	if p5aAdoptions(t, f.runtime) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("mismatched candidate was adopted or retained after current-source fallback")
	}
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[1][2][3] = 0
	if _, value := asset.XBrickMap.GetVoxel(1, 2, 3); value != 255 {
		t.Fatal("editing promoted source changed registered independent geometry")
	}
}

func TestP1bEvictedCaptureUsesRebuiltAuthorityAndTerminalCleanup(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop pending", true: "rebuild and commit"}[commit], func(t *testing.T) {
			f := p1bRuntime(t, 1, 1<<30)
			s1fPrepared(t, f, ChunkCoord{}, false)
			prepared := <-f.runtime.PreparedLoads
			f.runtime.PreparedLoads <- prepared
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			pending := f.runtime.Metrics.PendingPreparedBytes
			cache, key := f.runtime.PreparedGeometryCache, prepared.PreparedImportedWorldGeometryCacheKey
			cache.getOrBuild("pressure", func() *volume.XBrickMap { return s2aGeometry(1) })
			if _, hit := cache.getOrBuild(key, nil); hit {
				t.Fatal("fixture did not evict the captured compact source")
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if pending == 0 || f.runtime.Metrics.PendingPreparedBytes != pending {
				t.Fatal("cache eviction released pending source/registration ownership")
			}
			var id AssetId
			if commit {
				current := prepareImportedWorldChunkGeometry(prepared.ImportedWorldChunk, prepared.ImportedWorldAux)
				current.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[1][2][3] = 42
				cache.getOrBuild(key, func() *volume.XBrickMap { return current })
				f.commitStage()
				var asset VoxelGeometryAsset
				id, asset = p5aAsset(t, f, false)
				if _, value := asset.XBrickMap.GetVoxel(1, 2, 3); value != 42 || p5aAdoptions(t, f.runtime) != 0 {
					t.Fatal("same-key stale captured candidate replaced rebuilt current authority")
				}
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			stats := cache.snapshot()
			if stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatalf("terminal cleanup retained compact, pending or asset owners: %+v", stats)
			}
			if commit {
				s2aAssertAsset(t, f.assets, id, false)
			}
		})
	}
}

func TestP1bCompactMixedSingleflightAndClose(t *testing.T) {
	for _, mode := range []string{"dense first", "compact first", "close compact build"} {
		t.Run(mode, func(t *testing.T) {
			f := p1bRuntime(t, 8, 1<<30)
			cache := f.runtime.PreparedGeometryCache
			entered := make(chan struct{})
			blocked, release := s2aBuildBarrier(t)
			geometry := s2aGeometry(1)
			type outcome struct{ geometry *volume.XBrickMap }
			results := make(chan outcome, 2)
			call := func(compact, first bool) {
				builder := func() *volume.XBrickMap {
					if !first {
						return s2aGeometry(2) // A second invocation changes observable content.
					}
					close(entered)
					<-blocked
					return geometry
				}
				if compact {
					cache.getOrBuildSource("mixed", true, builder)
					results <- outcome{}
				} else {
					got, _ := cache.getOrBuild("mixed", builder)
					results <- outcome{got}
				}
			}
			compactFirst := mode != "dense first"
			go call(compactFirst, true)
			s2aWait(t, entered)
			go call(!compactFirst, false)
			s2aWaitForBuildWaits(t, cache, 1)
			if mode == "close compact build" {
				cache.close(f.assets)
			}
			release()
			var dense *volume.XBrickMap
			for range 2 {
				if result := s2aWait(t, results); result.geometry != nil {
					dense = result.geometry
				}
			}
			if dense == nil || dense.GetVoxelCount() != geometry.GetVoxelCount() {
				t.Fatal("mixed same-key callers did not share one successful logical build")
			}
			if mode == "dense first" && dense != geometry {
				t.Fatal("dense-first qualification replaced the generic builder's exact map identity")
			}
			if mode == "close compact build" {
				if stats := cache.snapshot(); stats.Bytes != 0 || stats.Entries != 0 {
					t.Fatalf("blocked compact completion repopulated closed cache: %+v", stats)
				}
			} else if again, hit := cache.getOrBuild("mixed", nil); !hit || again != dense {
				t.Fatal("mixed callers did not retain stable promoted dense identity")
			}
		})
	}
}

func TestP1bCompactAcquiredOversizedOwnerSurvivesUntilStop(t *testing.T) {
	f := p1bRuntime(t, 8, 1)
	s1fPrepared(t, f, ChunkCoord{}, false)
	f.commitStage()
	id, asset := p5aAsset(t, f, false)
	stats := f.runtime.PreparedGeometryCache.snapshot()
	if stats.PinnedBytes <= 0 || stats.OverBudgetBytes <= 0 || stats.AssetBytes <= 0 {
		t.Fatalf("acquired oversized compact preparation lost its pressure/pinned owner: %+v", stats)
	}
	borrowed := asset.XBrickMap
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	s2aAssertAsset(t, f.assets, id, false)
	if stats := f.runtime.PreparedGeometryCache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("stop retained oversized compact ownership: %+v", stats)
	}
	if _, value := borrowed.GetVoxel(1, 2, 3); value != 1 {
		t.Fatal("owner cleanup invalidated an already borrowed dense map")
	}
}

func TestP1bCompactLateBackingMatchesDenseRemovalPath(t *testing.T) {
	var dense *volume.XBrickMap
	var denseRemoval content.VoxelBackingRemovalDef
	for _, compact := range []bool{false, true} {
		var f *streamedRenderHarness
		if compact {
			f = p1bRuntime(t, 8, 1<<30)
		} else {
			f, _, _ = p5aRuntime(t, true, true)
		}
		s1fPrepared(t, f, ChunkCoord{}, false)
		prepared := <-f.runtime.PreparedLoads
		wantSource := prepareImportedWorldChunkGeometry(prepared.ImportedWorldChunk, prepared.ImportedWorldAux)
		provider := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{
			ChunkSize: 16, SolidValue: 7, Columns: []content.TerrainChunkColumnDef{{X: 1, Z: 3, FilledVoxels: 3}},
		})
		removal := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, f.runtime.BaseWorldID, "late", [3]int{}, 16, provider, nil)
		center := mgl32.Vec3{1.5, 2.5, 3.5}
		removal.MaterializeSphere(wantSource.Copy(), center, 0.1)
		// Current backing and removal history can change after worker capture.
		f.runtime.BaseWorldBacking, f.runtime.BaseWorldBackingSourceHash = provider, "late"
		f.runtime.recordVoxelBackingRemoval(removal)
		f.runtime.PreparedLoads <- prepared
		f.commitStage()
		entity := s1eImported(t, f, ChunkCoord{})
		model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
		geometry, ok := ResolveVoxelGeometryMap(f.assets, &model)
		backing, hasBacking := voxelBackingForEntity(f.cmd, entity)
		if !ok || !hasBacking || p5aAdoptions(t, f.runtime) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
			t.Fatal("late backing lost private geometry/removal owner or retained/adopted the unused candidate")
		}
		if _, value := geometry.GetVoxel(9, 10, 11); value != 1 {
			t.Fatal("late backing overwrote authored material")
		}
		backing.MaterializeSphere(geometry, center, 0.1)
		if occupied, _ := geometry.GetVoxel(1, 2, 3); occupied {
			t.Fatal("late backing restored a captured removal")
		}
		source, hit := f.runtime.PreparedGeometryCache.getOrBuild(prepared.PreparedImportedWorldGeometryCacheKey, nil)
		if !hit || source == geometry {
			t.Fatal("backed live geometry lost private copy isolation")
		}
		p1bGeometry(t, source, wantSource)
		if compact {
			p1bGeometry(t, geometry, dense)
			if !reflect.DeepEqual(backing.RemovalDef(), denseRemoval) {
				t.Fatal("compact late-backing fallback changed removal materials/history")
			}
		} else {
			dense, denseRemoval = geometry, backing.RemovalDef()
		}
	}
}
