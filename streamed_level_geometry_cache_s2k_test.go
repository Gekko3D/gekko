package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func s2kCaptures(t *testing.T, cache *streamedPreparedGeometryCache) int {
	t.Helper()
	field := reflect.ValueOf(cache.snapshot()).FieldByName("StorageCaptureVisits")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("cache diagnostic StorageCaptureVisits must have type int")
	}
	return int(field.Int())
}

func s2kRuntimeCaptures(t *testing.T, state *StreamedLevelRuntimeState) int {
	t.Helper()
	refreshStreamedRuntimeMetricsCounts(state)
	field := reflect.ValueOf(state.Metrics).FieldByName("PreparedGeometryCacheStorageCaptureVisits")
	if !field.IsValid() || field.Kind() != reflect.Int {
		t.Fatal("public metric PreparedGeometryCacheStorageCaptureVisits must have type int")
	}
	visits := int(field.Int())
	if visits != s2kCaptures(t, state.PreparedGeometryCache) {
		t.Fatal("public storage capture metric differs from its cache owner")
	}
	return visits
}

func TestS2kImportedWorkerRegistrationAvoidsRecaptureAndKeepsActualIdentity(t *testing.T) {
	f, _, auxiliary := p5aRuntime(t, true, true)
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	full, proxy := <-f.runtime.PreparedLoads, <-f.runtime.PreparedProxyLoads
	f.runtime.PreparedLoads <- full
	f.runtime.PreparedProxyLoads <- proxy
	preparedCharge := s2aCharge(t, full.PreparedImportedWorldGeometry) + s2aCharge(t, proxy.PreparedGeometry)
	before := s2kRuntimeCaptures(t, f.runtime)
	if before <= 0 {
		t.Fatal("worker source admission did not describe any cache storage")
	}
	f.commitStage()
	f.commitStage()
	fullID, fullAsset := p5aAsset(t, f, false)
	proxyID, proxyAsset := p5aAsset(t, f, true)
	fullAssetCharge, proxyAssetCharge := s2aCharge(t, fullAsset.XBrickMap), s2aCharge(t, proxyAsset.XBrickMap)
	stats := f.runtime.PreparedGeometryCache.snapshot()
	if stats.PreparedBytes != preparedCharge || stats.AssetBytes != fullAssetCharge+proxyAssetCharge ||
		stats.Bytes != preparedCharge+fullAssetCharge+proxyAssetCharge || stats.PinnedBytes != stats.Bytes {
		t.Fatalf("worker descriptor changed exact physical attribution/pins: %+v", stats)
	}
	if got := s2kRuntimeCaptures(t, f.runtime); got != before {
		t.Fatalf("registered worker maps were recaptured on commit: before=%d after=%d", before, got)
	}
	for _, geometry := range []*VoxelGeometryAsset{&fullAsset, &proxyAsset} {
		p5aGeometry(t, geometry.XBrickMap, auxiliary)
	}
	if fullAsset.XBrickMap == full.PreparedImportedWorldGeometry || proxyAsset.XBrickMap == proxy.PreparedGeometry || fullAsset.XBrickMap == proxyAsset.XBrickMap {
		t.Fatal("registration descriptor changed independent geometry ownership")
	}
	// The installed graph must join the same physical identity ledger when its
	// registered map is borrowed immutably as a later prepared cache alias.
	f.runtime.PreparedGeometryCache.getOrBuild("registered-alias", func() *volume.XBrickMap { return fullAsset.XBrickMap })
	aliased := f.runtime.PreparedGeometryCache.snapshot()
	if aliased.Bytes != stats.Bytes || aliased.PreparedBytes != preparedCharge+fullAssetCharge || aliased.AssetBytes != proxyAssetCharge || aliased.PinnedBytes != stats.PinnedBytes {
		t.Fatalf("registered alias lost identity or independent pin attribution: before=%+v after=%+v", stats, aliased)
	}
	if got := s2kRuntimeCaptures(t, f.runtime); got != before {
		t.Fatalf("already installed immutable map was described again: %d", got)
	}
	derivative := volume.NewXBrickMap()
	for key, sector := range fullAsset.XBrickMap.Sectors {
		derivative.Sectors[key] = sector
	}
	derivative.ComputeAABB()
	derivative.ClearDirty()
	// Measure only the distinct root's increment through the established
	// ordinary owner seam. Its sector/brick storage is physically shared.
	measure := newStreamedPreparedGeometryCache(8, 1<<40)
	measure.getOrBuild("registered", func() *volume.XBrickMap { return fullAsset.XBrickMap })
	sharedCharge := measure.snapshot().Bytes
	measure.getOrBuild("derivative", func() *volume.XBrickMap { return derivative })
	rootCharge := measure.snapshot().Bytes - sharedCharge
	measure.close(nil)
	if rootCharge <= 0 || rootCharge >= s2aCharge(t, derivative) {
		t.Fatal("derivative fixture did not isolate distinct root metadata")
	}
	f.runtime.PreparedGeometryCache.getOrBuild("registered-derivative", func() *volume.XBrickMap { return derivative })
	derived := f.runtime.PreparedGeometryCache.snapshot()
	if derived.Bytes != aliased.Bytes+rootCharge || derived.PreparedBytes != aliased.PreparedBytes+rootCharge ||
		derived.AssetBytes != aliased.AssetBytes || derived.PinnedBytes != aliased.PinnedBytes {
		t.Fatalf("descriptor interior identity or attribution lost in derivative: before=%+v after=%+v root=%d", aliased, derived, rootCharge)
	}
	before++ // Only the new derivative root is a new cache description.
	if got := s2kRuntimeCaptures(t, f.runtime); got != before {
		t.Fatalf("derivative recaptured installed sector/brick identities: got=%d want=%d", got, before)
	}
	p5aGeometry(t, derivative, auxiliary)
	removeStreamedChunk(f.cmd, f.runtime, ChunkCoord{})
	unloadStreamedSectorProxy(f.cmd, f.runtime, ChunkCoord{})
	f.app.FlushCommands()
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	f.commitStage()
	f.commitStage()
	reusedFull, _ := p5aAsset(t, f, false)
	reusedProxy, _ := p5aAsset(t, f, true)
	if reusedFull != fullID || reusedProxy != proxyID || p5aAdoptions(t, f.runtime) != 2 || s2kRuntimeCaptures(t, f.runtime) != before || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("warm reuse recaptured storage, readopted discarded descriptors or retained pending ownership")
	}
	f.runtime.PreparedGeometryCache.trim(f.assets)
	if s2kRuntimeCaptures(t, f.runtime) != before {
		t.Fatal("read/maintenance captured storage")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if stats := f.runtime.PreparedGeometryCache.snapshot(); stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("terminal descriptor ownership left charged storage: %+v", stats)
	}
	for _, id := range []AssetId{fullID, proxyID} {
		if _, present := f.assets.GetVoxelGeometry(id); present {
			t.Fatal("terminal cache close retained exact adopted asset")
		}
	}
	p5aGeometry(t, fullAsset.XBrickMap, auxiliary)
	p5aGeometry(t, proxyAsset.XBrickMap, auxiliary)
	// Generic mutable/shared registrations still describe their actual maps.
	fallback := newStreamedPreparedGeometryCache(8, 1<<40)
	defer fallback.close(f.assets)
	id, _ := fallback.acquireAsset(f.assets, "ordinary", s2aGeometry(2))
	if s2kCaptures(t, fallback) <= 0 {
		t.Fatal("generic registration incorrectly bypassed ordinary storage capture")
	}
	if registered, ok := f.assets.GetVoxelGeometry(id); !ok || registered.XBrickMap.GetVoxelCount() != 2 {
		t.Fatal("generic registration lost usable geometry")
	}
}

func TestS2kDeferredWorkerDescriptorChargeAndTerminalRelease(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "stop"}[stop], func(t *testing.T) {
			f, observer, auxiliary := p5aRuntime(t, false, true)
			for _, coord := range []ChunkCoord{{}, {X: 1}} {
				s1fPrepared(t, f, coord, false)
			}
			first, deferred := <-f.runtime.PreparedLoads, <-f.runtime.PreparedLoads
			f.runtime.PreparedLoads <- first
			f.runtime.PreparedLoads <- deferred
			// The established charge seam measures the decoded envelope/source
			// without its registration owner. Add the exact defensive-copy charge;
			// a sealed descriptor must retain additional accounted metadata.
			baseline := deferred
			baseline.registration = nil
			copy := deferred.PreparedImportedWorldGeometry.Copy()
			copy.ClearDirty()
			geometryAndEnvelope := streamedPreparedChunkCharge(baseline) + s2aCharge(t, copy)
			before := s2kRuntimeCaptures(t, f.runtime)
			f.commitStage()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			retained := f.runtime.Metrics.PendingPreparedBytes
			if f.runtime.Metrics.PreparedChunkQueueDepth != 1 || retained <= geometryAndEnvelope {
				t.Fatalf("deferred descriptor metadata uncharged: pending=%d source/copy/envelope=%d", retained, geometryAndEnvelope)
			}
			if s2kRuntimeCaptures(t, f.runtime) != before {
				t.Fatal("first descriptor installation recaptured its registered map")
			}
			if stop {
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			} else {
				s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
				f.observerStage()
				if f.runtime.Metrics.PendingPreparedBytes != retained {
					t.Fatal("cancellation released descriptor charge before acknowledgement")
				}
				f.commitStage()
			}
			if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 {
				t.Fatal("terminal consumption retained worker descriptor ownership")
			}
			p5aGeometry(t, deferred.PreparedImportedWorldGeometry, auxiliary)
		})
	}
}

func TestS2kDescriptorRegisteredUnionBudgetReleasesOversizedAsset(t *testing.T) {
	f, _, _ := p5aRuntime(t, false, false)
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
	chunk, err := content.LoadImportedWorldChunk(content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	budget := s2aCharge(t, prepareImportedWorldChunkGeometry(chunk))
	// Configure the existing cache owner boundary before dispatching any jobs.
	// The previous default owner is empty and has no assets or live workers.
	f.runtime.PreparedGeometryCache.close(f.assets)
	f.runtime.PreparedGeometryCache = newStreamedPreparedGeometryCache(8, budget)
	s1fPrepared(t, f, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	f.runtime.PreparedLoads <- prepared
	before := s2kRuntimeCaptures(t, f.runtime)
	f.commitStage()
	id, asset := p5aAsset(t, f, false)
	p5aGeometry(t, asset.XBrickMap, nil)
	assetCharge := s2aCharge(t, asset.XBrickMap)
	stats := f.runtime.PreparedGeometryCache.snapshot()
	if stats.PreparedBytes != budget || stats.AssetBytes != assetCharge || stats.Bytes != budget+assetCharge ||
		stats.PinnedBytes != stats.Bytes || stats.OverBudgetBytes != assetCharge || stats.OversizedBypasses != 0 {
		t.Fatalf("fitting source plus oversized adopted union lost pinned pressure: %+v source=%d asset=%d", stats, budget, assetCharge)
	}
	if s2kRuntimeCaptures(t, f.runtime) != before {
		t.Fatal("small-budget descriptor adoption recaptured registered storage")
	}
	unrelated := f.assets.RegisterSharedVoxelGeometry(asset.XBrickMap, "unrelated")
	if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if stats := f.runtime.PreparedGeometryCache.snapshot(); stats.Entries != 0 || stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 || stats.OverBudgetBytes != 0 {
		t.Fatalf("oversized registered union retained warm ownership after final release: %+v", stats)
	}
	if _, present := f.assets.GetVoxelGeometry(id); present {
		t.Fatal("final unload retained exact oversized adopted asset")
	}
	if _, present := f.assets.GetVoxelGeometry(unrelated); !present {
		t.Fatal("oversized cleanup deleted unrelated public geometry")
	}
	if geometry, hit := f.runtime.PreparedGeometryCache.getOrBuild(prepared.PreparedImportedWorldGeometryCacheKey, nil); hit || geometry != nil {
		t.Fatal("oversized adopted union left a warm cache hit")
	}
	p5aGeometry(t, asset.XBrickMap, nil)
}
