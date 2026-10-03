package gekko

import (
	"fmt"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS2gPreparedCacheRepeatedRootReferences(t *testing.T) {
	const owners = 32
	geometry := s2aGeometry(8)
	charge := s2aCharge(t, geometry)
	cache := newStreamedPreparedGeometryCache(owners, 1<<40)
	t.Cleanup(func() { cache.close(nil) })
	cache.getOrBuild("alias-0", func() *volume.XBrickMap { return geometry })
	before := cache.snapshot()
	for i := 1; i < owners; i++ {
		cache.getOrBuild(fmt.Sprintf("alias-%d", i), func() *volume.XBrickMap { return geometry })
	}
	after := cache.snapshot()
	if after.Bytes != charge || after.PreparedBytes != charge || after.AssetBytes != 0 || after.PinnedBytes != 0 ||
		after.StorageReferenceVisits-before.StorageReferenceVisits != owners-1 {
		t.Fatalf("additional root owners must only adjust the root: before=%+v after=%+v charge=%d", before, after, charge)
	}
	cache.getOrBuild("replacement", func() *volume.XBrickMap { return geometry })
	trimmed := cache.snapshot()
	if trimmed.Bytes != charge || trimmed.Entries != owners || trimmed.Evictions != 1 ||
		trimmed.StorageReferenceVisits-after.StorageReferenceVisits != 2 {
		t.Fatalf("partial root replacement walked shared children or changed charge: before=%+v after=%+v", after, trimmed)
	}
	if _, hit := cache.getOrBuild("alias-0", nil); hit {
		t.Fatal("old alias survived entry pressure")
	}
	for i := 1; i < owners; i++ {
		if got, hit := cache.getOrBuild(fmt.Sprintf("alias-%d", i), nil); !hit || got != geometry {
			t.Fatalf("partial eviction damaged alias-%d", i)
		}
	}
	if occupied, value := geometry.GetVoxel(0, 0, 0); !occupied || value != 1 {
		t.Fatal("shared geometry no longer usable")
	}
	cache.trim(nil)
	if got := cache.snapshot().StorageReferenceVisits; got != trimmed.StorageReferenceVisits {
		t.Fatalf("reads and no-pressure trim adjusted storage: got=%d want=%d", got, trimmed.StorageReferenceVisits)
	}
	cache.close(nil)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("last root owner left charged storage: %+v", stats)
	}
}

func TestS2gPreparedCacheSharedSectorDuplicateEdges(t *testing.T) {
	a := s2aGeometry(4)
	sector := a.Sectors[[3]int{}]
	// Two physical links from one immutable root reach the same sector.
	a.Sectors[[3]int{1, 0, 0}] = sector
	a.AABBDirty = true
	a.ComputeAABB()
	a.ClearDirty()
	b := volume.NewXBrickMap()
	b.Sectors[[3]int{}] = sector
	b.ComputeAABB()
	b.ClearDirty()
	aCharge, bCharge := s2aCharge(t, a), s2aCharge(t, b)
	cache := newStreamedPreparedGeometryCache(2, 1<<40)
	t.Cleanup(func() { cache.close(nil) })
	cache.getOrBuild("duplicate", func() *volume.XBrickMap { return a })
	before := cache.snapshot()
	cache.getOrBuild("shared", func() *volume.XBrickMap { return b })
	after := cache.snapshot()
	if after.Bytes <= aCharge || after.Bytes >= aCharge+bCharge || after.PreparedBytes != after.Bytes ||
		after.StorageReferenceVisits-before.StorageReferenceVisits != 2 {
		t.Fatalf("already-present shared sector must stop propagation: before=%+v after=%+v", before, after)
	}
	empty := volume.NewXBrickMap()
	emptyCharge := s2aCharge(t, empty)
	cache.getOrBuild("empty", func() *volume.XBrickMap { return empty })
	trimmed := cache.snapshot()
	// One new root plus the removed root and its two sector links. The shared
	// sector still has b's link, so neither removal traverses its bricks.
	if trimmed.Bytes != bCharge+emptyCharge || trimmed.PreparedBytes != trimmed.Bytes || trimmed.Evictions != 1 ||
		trimmed.StorageReferenceVisits-after.StorageReferenceVisits != 4 {
		t.Fatalf("duplicate-link partial eviction changed charge or walked bricks: before=%+v after=%+v", after, trimmed)
	}
	if got, hit := cache.getOrBuild("shared", nil); !hit || got != b {
		t.Fatal("shared sector owner disappeared with duplicate-link owner")
	}
	if occupied, value := b.GetVoxel(0, 0, 0); !occupied || value != 1 {
		t.Fatal("shared sector geometry no longer usable")
	}
	cache.close(nil)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("last shared sector owner left charged storage: %+v", stats)
	}
}

func TestS2gRuntimeStorageReferenceMetricAndAttribution(t *testing.T) {
	_, _, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		MaxPreparedGeometryCacheEntries: 2,
		MaxPreparedGeometryCacheBytes:   1 << 40,
	})
	cache := state.PreparedGeometryCache
	prepared := s2aGeometry(4)
	preparedCharge := s2aCharge(t, prepared)
	id, _ := cache.acquireAsset(assets, "live", prepared)
	registered, ok := assets.GetVoxelGeometry(id)
	if !ok {
		t.Fatal("registered geometry missing")
	}
	// Borrow the actual registered map immutably. Its physical storage now has
	// both asset and prepared attribution, while the original asset stays live.
	borrowed := registered.XBrickMap
	assetCharge := s2aCharge(t, borrowed)
	cache.getOrBuild("borrowed-1", func() *volume.XBrickMap { return borrowed })
	before := cache.snapshot()
	if before.Bytes != preparedCharge+assetCharge || before.PreparedBytes != before.Bytes || before.AssetBytes != 0 || before.PinnedBytes != before.Bytes {
		t.Fatalf("prepared attribution did not win over pinned asset attribution: %+v", before)
	}
	cache.getOrBuild("borrowed-2", func() *volume.XBrickMap { return borrowed })
	after := cache.snapshot()
	if after.Bytes != before.Bytes || after.PreparedBytes != before.PreparedBytes || after.AssetBytes != 0 || after.PinnedBytes != before.PinnedBytes ||
		after.StorageReferenceVisits-before.StorageReferenceVisits != 2 {
		t.Fatalf("prepared alias replacement walked descendants or changed independent pins: before=%+v after=%+v", before, after)
	}
	empty := volume.NewXBrickMap()
	cache.getOrBuild("empty", func() *volume.XBrickMap { return empty })
	stats := cache.snapshot()
	if stats.Bytes != preparedCharge+assetCharge+s2aCharge(t, empty) || stats.PreparedBytes != preparedCharge+s2aCharge(t, empty) ||
		stats.AssetBytes != assetCharge || stats.PinnedBytes != preparedCharge+assetCharge {
		t.Fatalf("last prepared alias did not restore independent live asset attribution: %+v", stats)
	}
	s2aAssertAsset(t, assets, id, true)
	refreshStreamedRuntimeMetricsCounts(state)
	visits := state.Metrics.PreparedGeometryCacheStorageReferenceVisits
	if visits <= 0 || visits != stats.StorageReferenceVisits {
		t.Fatalf("runtime storage visits=%d, cache visits=%d", visits, stats.StorageReferenceVisits)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PreparedGeometryCacheStorageReferenceVisits != visits || cache.snapshot().StorageReferenceVisits != visits {
		t.Fatal("metrics refresh adjusted storage references")
	}
	cache.close(assets)
	s2aAssertAsset(t, assets, id, false)
	if stats := cache.snapshot(); stats.Bytes != 0 || stats.PreparedBytes != 0 || stats.AssetBytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("last pinned/asset/prepared owners left charged storage: %+v", stats)
	}
}
