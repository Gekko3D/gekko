package gekko

import (
	"fmt"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS2fPreparedCacheMultiVictimBytePressure(t *testing.T) {
	smallCharge := s2aCharge(t, s2aGeometry(1))
	large := s2aGeometry(8)
	largeCharge := s2aCharge(t, large)
	if largeCharge < 3*smallCharge {
		t.Fatalf("fixture must force multiple victims: small=%d large=%d", smallCharge, largeCharge)
	}
	budget := largeCharge + smallCharge
	count := int(budget / smallCharge)
	cache := newStreamedPreparedGeometryCache(count+8, budget)
	t.Cleanup(func() { cache.close(nil) })
	for i := 0; i < count; i++ {
		cache.getOrBuild(fmt.Sprintf("small-%d", i), func() *volume.XBrickMap { return s2aGeometry(1) })
	}
	if _, hit := cache.getOrBuild("small-0", nil); !hit {
		t.Fatal("initial warm entry missing")
	}
	before := cache.snapshot()
	if before.Entries != count || before.Bytes != int64(count)*smallCharge || before.EvictionCandidateVisits != 0 {
		t.Fatalf("fixture unexpectedly evicted or visited candidates: %+v", before)
	}
	cache.getOrBuild("large", func() *volume.XBrickMap { return large })
	after := cache.snapshot()
	if after.Entries != 2 || after.Bytes != budget || after.OverBudgetBytes != 0 ||
		after.Evictions-before.Evictions != count-1 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != count-1 {
		t.Fatalf("multi-victim trim must visit only its victims: before=%+v after=%+v", before, after)
	}
	for i := 0; i < count; i++ {
		if _, hit := cache.getOrBuild(fmt.Sprintf("small-%d", i), nil); hit != (i == 0) {
			t.Fatalf("small-%d retained=%t, want %t", i, hit, i == 0)
		}
	}
	if got, hit := cache.getOrBuild("large", nil); !hit || got != large {
		t.Fatal("new large entry was not retained")
	}
	cache.trim(nil)
	if got := cache.snapshot().EvictionCandidateVisits; got != after.EvictionCandidateVisits {
		t.Fatalf("reads and no-pressure trim visited candidates: got=%d want=%d", got, after.EvictionCandidateVisits)
	}
}

func TestS2fPreparedCachePinnedLRUAndWorkerDeferral(t *testing.T) {
	const pinnedCount = 64
	assets, unrelated := newSpawnTestAssetServer(), newSpawnTestAssetServer()
	cache := newStreamedPreparedGeometryCache(pinnedCount+2, 1<<40)
	t.Cleanup(func() { cache.close(unrelated) })
	pinned := make([]AssetId, pinnedCount)
	for i := range pinned {
		pinned[i], _ = cache.acquireAsset(assets, fmt.Sprintf("pinned-%d", i), s2aGeometry(1))
	}
	warm, _ := cache.acquireAsset(assets, "warm", s2aGeometry(1))
	cache.releaseAssetID(assets, warm)
	cache.getOrBuild("prepared", func() *volume.XBrickMap { return s2aGeometry(1) })
	if _, hit := cache.getOrBuild("warm", nil); !hit {
		t.Fatal("warm hit fixture missing")
	}
	workerBuild := func(key string) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			defer close(done)
			cache.getOrBuild(key, func() *volume.XBrickMap { return s2aGeometry(1) })
		}()
		s2aWait(t, done)
	}
	before := cache.snapshot()
	workerBuild("hit-pressure")
	after := cache.snapshot()
	if after.Evictions-before.Evictions != 1 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 1 {
		t.Fatalf("hit must move warm asset behind prepared victim: before=%+v after=%+v", before, after)
	}
	if _, hit := cache.getOrBuild("prepared", nil); hit {
		t.Fatal("hit failed to make warm asset newer than prepared")
	}
	// Two leases keep the hit entry pinned until its final release.
	for i := 0; i < 2; i++ {
		if id, reused := cache.acquireAsset(assets, "warm", s2aGeometry(1)); !reused || id != warm {
			t.Fatal("warm acquisition did not reuse its asset")
		}
	}
	cache.releaseAssetID(assets, warm)
	before = cache.snapshot()
	workerBuild("first")
	after = cache.snapshot()
	if after.Evictions-before.Evictions != 1 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 1 {
		t.Fatalf("pinned population enlarged candidate work: before=%+v after=%+v", before, after)
	}
	if _, hit := cache.getOrBuild("hit-pressure", nil); hit {
		t.Fatal("prepared entry survived pressure while warm asset was still leased")
	}
	s2aAssertAsset(t, assets, warm, true)
	cache.releaseAssetID(assets, warm)
	before = cache.snapshot()
	workerBuild("second")
	after = cache.snapshot()
	if after.Evictions-before.Evictions != 1 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 1 {
		t.Fatalf("final release did not leave one older prepared victim: before=%+v after=%+v", before, after)
	}
	if _, hit := cache.getOrBuild("first", nil); hit {
		t.Fatal("final release failed to make warm asset newer than first")
	}
	before = cache.snapshot()
	workerBuild("third")
	after = cache.snapshot()
	if after.Evictions != before.Evictions || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 1 || after.Entries != pinnedCount+3 {
		t.Fatalf("worker must defer once at oldest asset: before=%+v after=%+v", before, after)
	}
	s2aAssertAsset(t, assets, warm, true)
	unrelatedID := unrelated.RegisterSharedVoxelGeometry(s2aGeometry(1), "unrelated")
	cache.trim(unrelated)
	trimmed := cache.snapshot()
	if trimmed.Evictions-after.Evictions != 1 || trimmed.EvictionCandidateVisits-after.EvictionCandidateVisits != 1 ||
		trimmed.Entries != pinnedCount+2 || trimmed.Bytes > trimmed.MaxBytes || trimmed.OverBudgetBytes != 0 {
		t.Fatalf("main trim failed budget or bounded visits: before=%+v after=%+v", after, trimmed)
	}
	s2aAssertAsset(t, assets, warm, false)
	s2aAssertAsset(t, unrelated, unrelatedID, true)
	for _, id := range pinned {
		s2aAssertAsset(t, assets, id, true)
	}
	for _, key := range []string{"second", "third"} {
		if _, hit := cache.getOrBuild(key, nil); !hit {
			t.Fatalf("worker skipped oldest asset and discarded newer %s", key)
		}
	}
	cache.trim(unrelated)
	if got := cache.snapshot().EvictionCandidateVisits; got != trimmed.EvictionCandidateVisits {
		t.Fatalf("no-pressure maintenance visited a candidate: got=%d want=%d", got, trimmed.EvictionCandidateVisits)
	}
}

func TestS2fRuntimeEvictionCandidateMetric(t *testing.T) {
	_, _, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		MaxPreparedGeometryCacheEntries: 1,
		MaxPreparedGeometryCacheBytes:   1 << 40,
	})
	for _, key := range []string{"old", "new"} {
		state.PreparedGeometryCache.getOrBuild(key, func() *volume.XBrickMap { return s2aGeometry(1) })
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if got := state.Metrics.PreparedGeometryCacheEvictionCandidateVisits; got != 1 {
		t.Fatalf("runtime candidate metric=%d, want 1", got)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if got := state.Metrics.PreparedGeometryCacheEvictionCandidateVisits; got != 1 {
		t.Fatalf("metrics refresh changed cumulative candidate visits: %d", got)
	}
}
