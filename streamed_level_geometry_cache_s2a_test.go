package gekko

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// These tests use the cache's owner API, its storage metrics, and AssetServer
// lookup. Fixture charges are measured through admission rather than hardcoded
// Go struct sizes, map bucket sizes, or allocator assumptions.
func s2aGeometry(bricks int) *volume.XBrickMap {
	geometry := volume.NewXBrickMap()
	for i := 0; i < bricks; i++ {
		geometry.SetVoxel(i*volume.BrickSize, 0, 0, 1)
	}
	geometry.ComputeAABB()
	geometry.ClearDirty()
	return geometry
}

func s2aCharge(t *testing.T, geometry *volume.XBrickMap) int64 {
	t.Helper()
	cache := newStreamedPreparedGeometryCache(256, 1<<40)
	cache.getOrBuild("measure", func() *volume.XBrickMap { return geometry })
	charge := cache.snapshot().PreparedBytes
	if charge <= 0 {
		t.Fatalf("nonempty geometry has no prepared storage charge: %d", charge)
	}
	cache.close(nil)
	return charge
}

func s2aAssertAsset(t *testing.T, assets *AssetServer, id AssetId, present bool) {
	t.Helper()
	geometry, ok := assets.GetVoxelGeometry(id)
	if ok != present {
		t.Fatalf("asset %s present=%t, want %t", id, ok, present)
	}
	if present {
		if geometry.XBrickMap == nil {
			t.Fatalf("asset %s no longer has usable geometry", id)
		}
		occupied, value := geometry.XBrickMap.GetVoxel(0, 0, 0)
		if !occupied || value != 1 {
			t.Fatalf("asset %s no longer has usable geometry", id)
		}
	}
}

func s2aWait[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case result := <-channel:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("cache operation did not make progress")
		var zero T
		return zero
	}
}

func s2aBuildBarrier(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	blocked := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(blocked) }) }
	t.Cleanup(release)
	return blocked, release
}

func s2aWaitForBuildWaits(t *testing.T, cache *streamedPreparedGeometryCache, want int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for cache.snapshot().BuildWaits < want {
		select {
		case <-deadline.C:
			t.Fatalf("same-key caller did not join the active build: %+v", cache.snapshot())
		default:
			runtime.Gosched()
		}
	}
}

func TestS2aPreparedCacheStorageCharge(t *testing.T) {
	t.Run("defaults and int64 budget", func(t *testing.T) {
		for _, cache := range []*streamedPreparedGeometryCache{
			newStreamedPreparedGeometryCache(8), newStreamedPreparedGeometryCache(8, 0),
		} {
			if stats := cache.snapshot(); stats.MaxBytes != 128<<20 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
				t.Fatalf("unexpected default byte metrics: %+v", stats)
			}
		}
		const largeBudget int64 = 1 << 40
		if stats := newStreamedPreparedGeometryCache(8, largeBudget).snapshot(); stats.MaxBytes != largeBudget {
			t.Fatalf("byte budget was truncated: %+v", stats)
		}
	})

	t.Run("dense CPU payload survives uniform compression", func(t *testing.T) {
		geometry := s2aGeometry(1)
		brick := geometry.Sectors[[3]int{}].GetBrick(0, 0, 0)
		brick.Expand(1)
		before := s2aCharge(t, geometry)
		if !brick.TryCompress() {
			t.Fatal("fixture did not become uniform")
		}
		after := s2aCharge(t, geometry)
		if before != after || after < int64(volume.BrickSize*volume.BrickSize*volume.BrickSize) {
			t.Fatalf("CPU payload lost its charge on compression: before=%d after=%d", before, after)
		}
	})

	t.Run("auxiliary and packed pointer capacity", func(t *testing.T) {
		plain := s2aGeometry(1)
		withAux := plain.Copy()
		withAux.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = make([]byte, 7, 8192)
		if delta := s2aCharge(t, withAux) - s2aCharge(t, plain); delta != 8192 {
			t.Fatalf("aux capacity charge=%d, want 8192", delta)
		}
		withPointers := plain.Copy()
		sector := withPointers.Sectors[[3]int{}]
		packed := make([]*volume.Brick, len(sector.PackedBricks), 64)
		copy(packed, sector.PackedBricks)
		sector.PackedBricks = packed
		if s2aCharge(t, withPointers) <= s2aCharge(t, plain) {
			t.Fatal("unused packed-brick pointer capacity was not charged")
		}
	})

	t.Run("logical dirty and revision maps", func(t *testing.T) {
		clean := s2aGeometry(1)
		dirty := clean.Copy()
		dirty.DirtySectors[[3]int{}] = true
		dirty.DirtyBricks[[6]int{}] = true
		if s2aCharge(t, dirty) <= s2aCharge(t, clean) {
			t.Fatal("logical dirty map entries were not charged")
		}
		withoutRevision := clean.Copy()
		withoutRevision.SectorRevisions = nil
		if s2aCharge(t, withoutRevision) >= s2aCharge(t, clean) {
			t.Fatal("logical sector revision entries were not charged")
		}
	})

	t.Run("actual registered copy", func(t *testing.T) {
		assets := newSpawnTestAssetServer()
		geometry := s2aGeometry(1)
		geometry.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = make([]byte, 7, 8192)
		cache := newStreamedPreparedGeometryCache(8, 1<<40)
		cache.getOrBuild("copy", func() *volume.XBrickMap { return geometry })
		id, _ := cache.acquireAsset(assets, "copy", geometry)
		registered, ok := assets.GetVoxelGeometry(id)
		if !ok || registered.XBrickMap == geometry {
			t.Fatal("fixture did not create a separate registered map")
		}
		preparedCharge, assetCharge := s2aCharge(t, geometry), s2aCharge(t, registered.XBrickMap)
		stats := cache.snapshot()
		if stats.PreparedBytes != preparedCharge || stats.AssetBytes != assetCharge || stats.Bytes != preparedCharge+assetCharge || stats.PinnedBytes != stats.Bytes {
			t.Fatalf("registered storage not charged exactly once: %+v prepared=%d asset=%d", stats, preparedCharge, assetCharge)
		}
		if assetCharge >= preparedCharge {
			t.Fatal("fixture must distinguish copied aux length from prepared aux capacity")
		}
		cache.close(assets)
	})
}

func TestS2aPreparedCacheSharedStorage(t *testing.T) {
	t.Run("one map under multiple keys and distinct assets", func(t *testing.T) {
		assets := newSpawnTestAssetServer()
		geometry := s2aGeometry(2)
		charge := s2aCharge(t, geometry)
		cache := newStreamedPreparedGeometryCache(2, 1<<40)
		for _, key := range []string{"a", "b"} {
			cache.getOrBuild(key, func() *volume.XBrickMap { return geometry })
		}
		if stats := cache.snapshot(); stats.PreparedBytes != charge {
			t.Fatalf("same map charged twice across keys: %+v want=%d", stats, charge)
		}
		a, _ := cache.acquireAsset(assets, "a", geometry)
		b, _ := cache.acquireAsset(assets, "b", geometry)
		if a == b {
			t.Fatal("distinct keys must have separately owned registered assets")
		}
		registeredA, _ := assets.GetVoxelGeometry(a)
		registeredB, _ := assets.GetVoxelGeometry(b)
		assetCharge := s2aCharge(t, registeredA.XBrickMap) + s2aCharge(t, registeredB.XBrickMap)
		if stats := cache.snapshot(); stats.PreparedBytes != charge || stats.AssetBytes != assetCharge || stats.PinnedBytes != charge+assetCharge {
			t.Fatalf("shared prepared and distinct copies charged incorrectly: %+v", stats)
		}
		cache.releaseAsset(assets, "a")
		cache.getOrBuild("third", func() *volume.XBrickMap { return geometry.Copy() })
		cache.trim(assets)
		s2aAssertAsset(t, assets, a, false)
		s2aAssertAsset(t, assets, b, true)
		if got, hit := cache.getOrBuild("b", nil); !hit || got != geometry {
			t.Fatal("eviction of one alias removed the remaining prepared map")
		}
		if stats := cache.snapshot(); stats.PinnedBytes != charge+s2aCharge(t, registeredB.XBrickMap) {
			t.Fatalf("remaining lease lost shared storage charge: %+v", stats)
		}
		cache.close(assets)
		if stats := cache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 {
			t.Fatalf("close left shared storage ownership: %+v", stats)
		}
	})

	t.Run("immutable derivative shares unchanged sectors", func(t *testing.T) {
		mutable := s2aGeometry(1)
		mutable.SetVoxel(volume.SectorSize, 0, 0, 1)
		mutable.ComputeAABB()
		mutable.ClearDirty()
		previous := mutable.Copy()
		previous.ClearDirty()
		since := mutable.Revision
		mutable.SetVoxel(volume.SectorSize+1, 0, 0, 2)
		derivative := mutable.CopyChangedSectors(previous, since)
		independent := derivative.Copy()
		independent.ClearDirty()
		cache := newStreamedPreparedGeometryCache(2, 1<<40)
		cache.getOrBuild("previous", func() *volume.XBrickMap { return previous })
		cache.getOrBuild("derivative", func() *volume.XBrickMap { return derivative })
		sharedCharge := cache.snapshot().PreparedBytes
		if sharedCharge <= s2aCharge(t, previous) || sharedCharge >= s2aCharge(t, previous)+s2aCharge(t, independent) {
			t.Fatalf("immutable derivative shared sectors not charged once: shared=%d", sharedCharge)
		}
		third := s2aGeometry(1)
		cache.getOrBuild("third", func() *volume.XBrickMap { return third })
		if got, hit := cache.getOrBuild("derivative", nil); !hit || got != derivative {
			t.Fatal("evicting the previous copy removed the derivative's shared sectors")
		}
		if stats := cache.snapshot(); stats.PreparedBytes != s2aCharge(t, derivative)+s2aCharge(t, third) {
			t.Fatalf("partial shared-sector eviction left incorrect storage charge: %+v", stats)
		}
		cache.close(nil)
		if stats := cache.snapshot(); stats.Bytes != 0 {
			t.Fatalf("shared sector ownership survived close: %+v", stats)
		}
	})

	t.Run("distinct sectors can share one brick", func(t *testing.T) {
		a := s2aGeometry(1)
		b := s2aGeometry(1)
		b.Sectors[[3]int{}].PackedBricks[0] = a.Sectors[[3]int{}].PackedBricks[0]
		cache := newStreamedPreparedGeometryCache(2, 1<<40)
		cache.getOrBuild("a", func() *volume.XBrickMap { return a })
		cache.getOrBuild("b", func() *volume.XBrickMap { return b })
		if stats := cache.snapshot(); stats.PreparedBytes >= s2aCharge(t, a)+s2aCharge(t, b) || stats.PreparedBytes <= s2aCharge(t, a) {
			t.Fatalf("shared brick charge did not preserve distinct map/sector storage: %+v", stats)
		}
		third := s2aGeometry(1)
		cache.getOrBuild("third", func() *volume.XBrickMap { return third })
		if got, hit := cache.getOrBuild("b", nil); !hit || got != b {
			t.Fatal("shared brick disappeared when its other owner was evicted")
		}
		if stats := cache.snapshot(); stats.PreparedBytes != s2aCharge(t, b)+s2aCharge(t, third) {
			t.Fatalf("partial shared-brick eviction left incorrect storage charge: %+v", stats)
		}
	})
}

func TestS2aPreparedCacheByteBudgetAndLRU(t *testing.T) {
	t.Run("exact boundary and heterogeneous eviction below entry ceiling", func(t *testing.T) {
		small, large := s2aGeometry(1), s2aGeometry(8)
		budget := s2aCharge(t, large)
		cache := newStreamedPreparedGeometryCache(16, budget)
		cache.getOrBuild("small", func() *volume.XBrickMap { return small })
		cache.getOrBuild("large", func() *volume.XBrickMap { return large })
		if stats := cache.snapshot(); stats.Bytes != budget || stats.Entries != 1 || stats.OverBudgetBytes != 0 {
			t.Fatalf("exact byte boundary did not fit: %+v budget=%d", stats, budget)
		}
		if got, hit := cache.getOrBuild("large", nil); !hit || got != large {
			t.Fatal("exact-boundary entry was not retained")
		}
		if got, hit := cache.getOrBuild("small", nil); hit || got != nil {
			t.Fatal("smaller old entry survived byte pressure below the entry limit")
		}
	})

	t.Run("hit refreshes LRU", func(t *testing.T) {
		geometry := s2aGeometry(1)
		cache := newStreamedPreparedGeometryCache(16, 2*s2aCharge(t, geometry))
		for _, key := range []string{"a", "b"} {
			copy := geometry.Copy()
			cache.getOrBuild(key, func() *volume.XBrickMap { return copy })
		}
		if _, hit := cache.getOrBuild("a", nil); !hit {
			t.Fatal("missing initial LRU fixture")
		}
		cache.getOrBuild("c", func() *volume.XBrickMap { return geometry.Copy() })
		if _, hit := cache.getOrBuild("a", nil); !hit {
			t.Fatal("recently hit entry was evicted")
		}
		if _, hit := cache.getOrBuild("b", nil); hit {
			t.Fatal("least recently used entry survived")
		}
	})

	t.Run("release refreshes LRU and both ceilings apply", func(t *testing.T) {
		assets := newSpawnTestAssetServer()
		cache := newStreamedPreparedGeometryCache(2, 1<<40)
		geometry := s2aGeometry(1)
		a, _ := cache.acquireAsset(assets, "a", geometry)
		cache.releaseAsset(assets, "a")
		b, _ := cache.acquireAsset(assets, "b", geometry.Copy())
		cache.releaseAsset(assets, "b")
		cache.acquireAsset(assets, "a", geometry)
		cache.releaseAsset(assets, "a")
		cache.getOrBuild("c", func() *volume.XBrickMap { return geometry.Copy() })
		cache.trim(assets)
		s2aAssertAsset(t, assets, a, true)
		s2aAssertAsset(t, assets, b, false)
		if stats := cache.snapshot(); stats.Entries > 2 {
			t.Fatalf("entry ceiling was ignored: %+v", stats)
		}
	})
}

func TestS2aPreparedCacheWarmReacquisitionPinsFullStorage(t *testing.T) {
	assets := newSpawnTestAssetServer()
	geometry := s2aGeometry(2)
	cache := newStreamedPreparedGeometryCache(8, 1<<40)
	t.Cleanup(func() { cache.close(assets) })
	cache.getOrBuild("warm", func() *volume.XBrickMap { return geometry })
	id, reused := cache.acquireAsset(assets, "warm", geometry)
	initial := cache.snapshot()
	if reused || initial.PreparedBytes <= 0 || initial.AssetBytes <= 0 || initial.Bytes != initial.PreparedBytes+initial.AssetBytes || initial.PinnedBytes != initial.Bytes {
		t.Fatalf("fixture did not acquire complete prepared and registered storage: %+v", initial)
	}
	s2aAssertAsset(t, assets, id, true)
	cache.releaseAsset(assets, "warm")
	if stats := cache.snapshot(); stats.Bytes != initial.Bytes || stats.PinnedBytes != 0 {
		t.Fatalf("initial release did not leave unpinned warm storage: %+v", stats)
	}

	for cycle := range 3 {
		first, firstReused := cache.acquireAsset(assets, "warm", geometry)
		if !firstReused || first != id {
			t.Fatalf("cycle %d did not reuse the registered warm asset", cycle)
		}
		s2aAssertAsset(t, assets, first, true)
		if stats := cache.snapshot(); stats.Bytes != initial.Bytes || stats.PreparedBytes != initial.PreparedBytes || stats.AssetBytes != initial.AssetBytes || stats.PinnedBytes != initial.Bytes {
			t.Fatalf("cycle %d warm reacquisition must pin full prepared and registered storage without changing physical bytes: initial=%+v current=%+v", cycle, initial, stats)
		}
		second, secondReused := cache.acquireAsset(assets, "warm", geometry)
		if !secondReused || second != id {
			t.Fatalf("cycle %d second user did not share the live registered asset", cycle)
		}
		if stats := cache.snapshot(); stats.Bytes != initial.Bytes || stats.PinnedBytes != initial.Bytes {
			t.Fatalf("cycle %d second lease duplicated physical or pinned charge: %+v", cycle, stats)
		}
		cache.releaseAsset(assets, "warm")
		s2aAssertAsset(t, assets, second, true)
		if stats := cache.snapshot(); stats.Bytes != initial.Bytes || stats.PinnedBytes != initial.Bytes {
			t.Fatalf("cycle %d remaining lease must keep full storage pinned: %+v", cycle, stats)
		}
		cache.releaseAsset(assets, "warm")
		s2aAssertAsset(t, assets, id, true)
		if stats := cache.snapshot(); stats.Bytes != initial.Bytes || stats.PreparedBytes != initial.PreparedBytes || stats.AssetBytes != initial.AssetBytes || stats.PinnedBytes != 0 {
			t.Fatalf("cycle %d balanced releases did not restore unpinned warm storage: %+v", cycle, stats)
		}
	}
	cache.close(assets)
	s2aAssertAsset(t, assets, id, false)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
		t.Fatalf("close left ownership after repeated warm leases: %+v", stats)
	}
}

func TestS2aPreparedCachePinnedLeasesAndOversize(t *testing.T) {
	t.Run("two leases survive pressure and release independently", func(t *testing.T) {
		assets := newSpawnTestAssetServer()
		geometry := s2aGeometry(1)
		budget := s2aCharge(t, geometry)
		cache := newStreamedPreparedGeometryCache(8, budget)
		cache.getOrBuild("live", func() *volume.XBrickMap { return geometry })
		a, reused := cache.acquireAsset(assets, "live", geometry)
		b, reusedAgain := cache.acquireAsset(assets, "live", geometry)
		if reused || !reusedAgain || a != b {
			t.Fatal("two acquired users did not share one asset")
		}
		cache.getOrBuild("pressure", func() *volume.XBrickMap { return s2aGeometry(4) })
		cache.trim(assets)
		stats := cache.snapshot()
		if stats.Bytes <= budget || stats.PinnedBytes != stats.Bytes || stats.OverBudgetBytes != stats.Bytes-budget {
			t.Fatalf("live asset pressure was not exposed: %+v budget=%d", stats, budget)
		}
		cache.releaseAsset(assets, "live")
		cache.trim(assets)
		s2aAssertAsset(t, assets, a, true)
		cache.releaseAsset(assets, "live")
		cache.trim(assets)
		s2aAssertAsset(t, assets, a, false)
		if stats := cache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.OverBudgetBytes != 0 {
			t.Fatalf("final release retained post-registration oversized entry: %+v", stats)
		}
		if _, hit := cache.getOrBuild("live", nil); hit {
			t.Fatal("post-registration oversized entry retained a warm hit")
		}
	})

	t.Run("oversized prepared result bypasses warm retention", func(t *testing.T) {
		geometry := s2aGeometry(8)
		cache := newStreamedPreparedGeometryCache(8, s2aCharge(t, geometry)-1)
		for range 2 {
			if got, hit := cache.getOrBuild("oversized", func() *volume.XBrickMap { return geometry }); got != geometry || hit {
				t.Fatal("oversized result must be usable without a warm hit")
			}
		}
		if stats := cache.snapshot(); stats.Bytes != 0 || stats.Entries != 0 || stats.OversizedBypasses != 2 {
			t.Fatalf("oversized results left warm storage: %+v", stats)
		}
	})

	t.Run("latest unpinned admission cannot keep excess storage", func(t *testing.T) {
		assets := newSpawnTestAssetServer()
		live := s2aGeometry(1)
		cache := newStreamedPreparedGeometryCache(8, 2*s2aCharge(t, live))
		id, _ := cache.acquireAsset(assets, "live", live)
		cache.getOrBuild("new", func() *volume.XBrickMap { return live.Copy() })
		cache.trim(assets)
		s2aAssertAsset(t, assets, id, true)
		if stats := cache.snapshot(); stats.Bytes > stats.MaxBytes || stats.OverBudgetBytes != 0 {
			t.Fatalf("new unpinned entry displaced budget protection: %+v", stats)
		}
	})
}

func TestS2aPreparedCacheDisabledBlankAndNilLifetime(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		cache *streamedPreparedGeometryCache
		key   string
	}{
		{"entries disabled", newStreamedPreparedGeometryCache(-1, 1<<40), "key"},
		{"bytes disabled", newStreamedPreparedGeometryCache(8, -1), "key"},
		{"blank key", newStreamedPreparedGeometryCache(8, 1<<40), "  "},
		{"nil cache", nil, "key"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			assets := newSpawnTestAssetServer()
			geometry := s2aGeometry(1)
			for range 2 {
				if got, hit := fixture.cache.getOrBuild(fixture.key, func() *volume.XBrickMap { return geometry }); got != geometry || hit {
					t.Fatal("nonretaining path failed to return caller geometry")
				}
			}
			a, _ := fixture.cache.acquireAsset(assets, fixture.key, geometry)
			b, _ := fixture.cache.acquireAsset(assets, "", geometry.Copy())
			if a == (AssetId{}) || b == (AssetId{}) || a == b {
				t.Fatal("independent uncached geometry did not receive usable assets")
			}
			fixture.cache.releaseAssetID(assets, a)
			s2aAssertAsset(t, assets, a, false)
			s2aAssertAsset(t, assets, b, true)
			fixture.cache.releaseAssetID(assets, b)
			fixture.cache.trim(assets)
			s2aAssertAsset(t, assets, b, false)
			if stats := fixture.cache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
				t.Fatalf("nonretaining leases left storage ownership: %+v", stats)
			}
		})
	}
}

func TestS2aPreparedCacheDeferredTrimAndOwnerServer(t *testing.T) {
	assets, unrelated := newSpawnTestAssetServer(), newSpawnTestAssetServer()
	geometry := s2aGeometry(1)
	unrelatedID := unrelated.RegisterSharedVoxelGeometry(geometry, "unrelated")
	cache := newStreamedPreparedGeometryCache(1, 2*s2aCharge(t, geometry))
	id, _ := cache.acquireAsset(assets, "old", geometry)
	cache.releaseAsset(assets, "old")
	done := make(chan struct{})
	go func() {
		defer close(done)
		cache.getOrBuild("new", func() *volume.XBrickMap { return geometry.Copy() })
	}()
	s2aWait(t, done)
	s2aAssertAsset(t, assets, id, true)
	stats := cache.snapshot()
	if stats.AssetBytes <= 0 || stats.Bytes != stats.PreparedBytes+stats.AssetBytes {
		t.Fatalf("deferred registered storage disappeared from accounting: %+v", stats)
	}
	// Cleanup follows the registering owner even if its caller supplies a
	// different server argument. The cache still owns the original asset.
	cache.trim(unrelated)
	s2aAssertAsset(t, assets, id, false)
	s2aAssertAsset(t, unrelated, unrelatedID, true)
	if stats := cache.snapshot(); stats.Bytes > stats.MaxBytes || stats.Entries > 1 || stats.OverBudgetBytes != 0 {
		t.Fatalf("engine-thread maintenance did not restore both budgets: %+v", stats)
	}
	live, _ := cache.acquireAsset(assets, "live", geometry)
	cache.close(unrelated)
	s2aAssertAsset(t, assets, live, false)
	s2aAssertAsset(t, unrelated, unrelatedID, true)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
		t.Fatalf("close retained cache ownership: %+v", stats)
	}
}

func TestS2aPreparedCacheRepeatedTravelStaysBounded(t *testing.T) {
	assets := newSpawnTestAssetServer()
	geometry := s2aGeometry(1)
	cache := newStreamedPreparedGeometryCache(3, 4*s2aCharge(t, geometry))
	var ids []AssetId
	for i := range 24 {
		key := fmt.Sprintf("travel-%d", i)
		prepared, _ := cache.getOrBuild(key, func() *volume.XBrickMap { return s2aGeometry(1 + i%4) })
		id, _ := cache.acquireAsset(assets, key, prepared)
		ids = append(ids, id)
		s2aAssertAsset(t, assets, id, true)
		cache.releaseAsset(assets, key)
		cache.trim(assets)
		if stats := cache.snapshot(); stats.Bytes > stats.MaxBytes || stats.Entries > 3 || stats.PinnedBytes != 0 || stats.OverBudgetBytes != 0 {
			t.Fatalf("travel %d left unbounded storage: %+v", i, stats)
		}
	}
	cache.close(assets)
	for _, id := range ids {
		s2aAssertAsset(t, assets, id, false)
	}
}

func TestS2aPreparedCacheCoalescesOverlappingBuilds(t *testing.T) {
	for _, mode := range []struct {
		name   string
		budget int64
	}{
		{"warm", 1 << 40}, {"oversized", 1}, {"disabled", -1},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cache := newStreamedPreparedGeometryCache(8, mode.budget)
			entered := make(chan struct{})
			blocked, release := s2aBuildBarrier(t)
			result := make(chan *volume.XBrickMap, 3)
			geometry := s2aGeometry(1)
			var calls atomic.Int32
			go func() {
				got, _ := cache.getOrBuild("same", func() *volume.XBrickMap {
					calls.Add(1)
					close(entered)
					<-blocked
					return geometry
				})
				result <- got
			}()
			s2aWait(t, entered)
			for range 2 {
				go func() {
					got, _ := cache.getOrBuild("same", func() *volume.XBrickMap {
						calls.Add(1)
						return s2aGeometry(2)
					})
					result <- got
				}()
			}
			s2aWaitForBuildWaits(t, cache, 2)
			release()
			for range 3 {
				if got := s2aWait(t, result); got != geometry {
					t.Fatal("overlapping callers did not receive the same build result")
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("same key built %d times", calls.Load())
			}
		})
	}
}

func TestS2aPreparedCacheDistinctAndBypassBuildsMakeProgress(t *testing.T) {
	for _, mode := range []string{"distinct keys", "blank keys", "nil cache"} {
		t.Run(mode, func(t *testing.T) {
			cache := newStreamedPreparedGeometryCache(8, 1<<40)
			firstKey, secondKey := "a", "b"
			if mode == "blank keys" {
				firstKey, secondKey = "", " "
			}
			if mode == "nil cache" {
				cache = nil
				secondKey = firstKey
			}
			entered, firstDone := make(chan struct{}), make(chan struct{})
			blocked, release := s2aBuildBarrier(t)
			go func() {
				defer close(firstDone)
				cache.getOrBuild(firstKey, func() *volume.XBrickMap {
					close(entered)
					<-blocked
					return s2aGeometry(1)
				})
			}()
			s2aWait(t, entered)
			secondDone := make(chan *volume.XBrickMap, 1)
			go func() {
				got, _ := cache.getOrBuild(secondKey, func() *volume.XBrickMap { return s2aGeometry(2) })
				secondDone <- got
			}()
			if got := s2aWait(t, secondDone); got == nil {
				t.Fatal("independent builder did not produce geometry")
			}
			release()
			s2aWait(t, firstDone)
		})
	}
}

func TestS2aPreparedCacheNilAndPanicBuildsWakeWaitersAndRetry(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%t", panics), func(t *testing.T) {
			cache := newStreamedPreparedGeometryCache(8, 1<<40)
			entered := make(chan struct{})
			blocked, release := s2aBuildBarrier(t)
			type outcome struct {
				geometry *volume.XBrickMap
				panic    any
			}
			results := make(chan outcome, 2)
			request := func(builder func() *volume.XBrickMap) {
				result := outcome{}
				defer func() {
					result.panic = recover()
					results <- result
				}()
				result.geometry, _ = cache.getOrBuild("retry", builder)
			}
			go request(func() *volume.XBrickMap {
				close(entered)
				<-blocked
				if panics {
					panic("s2a-builder-panic")
				}
				return nil
			})
			s2aWait(t, entered)
			go request(func() *volume.XBrickMap { return s2aGeometry(1) })
			s2aWaitForBuildWaits(t, cache, 1)
			release()
			for range 2 {
				result := s2aWait(t, results)
				if result.geometry != nil || (panics && result.panic != "s2a-builder-panic") || (!panics && result.panic != nil) {
					t.Fatalf("waiter did not receive failed build outcome: %+v", result)
				}
			}
			if stats := cache.snapshot(); stats.Bytes != 0 || stats.Entries != 0 {
				t.Fatalf("failed build created ownership: %+v", stats)
			}
			if got, hit := cache.getOrBuild("retry", nil); got != nil || hit {
				t.Fatal("nil builder created or found an entry after failed build")
			}
			geometry := s2aGeometry(1)
			if got, hit := cache.getOrBuild("retry", func() *volume.XBrickMap { return geometry }); got != geometry || hit {
				t.Fatal("later request could not retry a failed build")
			}
		})
	}
}

func TestS2aPreparedCacheCloseDuringBuildPreventsRepopulation(t *testing.T) {
	assets := newSpawnTestAssetServer()
	cache := newStreamedPreparedGeometryCache(8, 1<<40)
	geometry := s2aGeometry(1)
	id, _ := cache.acquireAsset(assets, "old", geometry)
	entered := make(chan struct{})
	blocked, release := s2aBuildBarrier(t)
	done := make(chan *volume.XBrickMap, 1)
	go func() {
		got, _ := cache.getOrBuild("blocked", func() *volume.XBrickMap {
			close(entered)
			<-blocked
			return geometry.Copy()
		})
		done <- got
	}()
	s2aWait(t, entered)
	cache.close(assets)
	s2aAssertAsset(t, assets, id, false)
	release()
	s2aWait(t, done)
	cache.trim(assets)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.Entries != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("outstanding build repopulated closed cache: %+v", stats)
	}
}
