package gekko

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

const (
	defaultStreamedPreparedGeometryCacheEntries       = 256
	defaultStreamedPreparedGeometryCacheBytes   int64 = 128 << 20
)

type streamedPreparedGeometryCache struct {
	mu             sync.Mutex
	enabled        bool
	closed         bool
	maxEntries     int
	maxBytes       int64
	unpinnedOldest *streamedPreparedGeometryCacheEntry
	unpinnedNewest *streamedPreparedGeometryCacheEntry
	entries        map[string]*streamedPreparedGeometryCacheEntry
	owned          map[*streamedPreparedGeometryCacheEntry]struct{}
	assets         map[AssetId]*streamedPreparedGeometryCacheEntry
	builds         map[string]*streamedPreparedGeometryBuild
	storage        streamedGeometryStorageLedger
	stats          streamedPreparedGeometryCacheStats
}

type streamedPreparedGeometryCacheEntry struct {
	key             string
	geometry        *volume.XBrickMap
	preparedStorage *streamedGeometryStorageNode
	assetStorage    *streamedGeometryStorageNode
	asset           AssetId
	assetServer     *AssetServer
	refCount        int
	voxelCount      int
	unpinnedPrev    *streamedPreparedGeometryCacheEntry
	unpinnedNext    *streamedPreparedGeometryCacheEntry
	noWarm          bool
}

type streamedPreparedGeometryBuild struct {
	done       chan struct{}
	geometry   *volume.XBrickMap
	panicValue any
	hit        bool
}

type streamedPreparedGeometryCacheStats struct {
	Entries                 int
	Voxels                  int
	Hits                    int
	Misses                  int
	Evictions               int
	AssetRegisters          int
	AssetReuses             int
	Bytes                   int64
	PreparedBytes           int64
	AssetBytes              int64
	PinnedBytes             int64
	MaxBytes                int64
	OverBudgetBytes         int64
	BuildWaits              int
	OversizedBypasses       int
	EvictionCandidateVisits int
}

func streamedPreparedGeometryCacheMaxEntries(configured int) int {
	if configured < 0 {
		return 0
	}
	if configured == 0 {
		return defaultStreamedPreparedGeometryCacheEntries
	}
	return configured
}

func newStreamedPreparedGeometryCache(maxEntries int, byteBudget ...int64) *streamedPreparedGeometryCache {
	maxEntries = streamedPreparedGeometryCacheMaxEntries(maxEntries)
	maxBytes := defaultStreamedPreparedGeometryCacheBytes
	if len(byteBudget) > 0 {
		if byteBudget[0] < 0 {
			maxBytes = 0
		} else if byteBudget[0] > 0 {
			maxBytes = byteBudget[0]
		}
	}
	return &streamedPreparedGeometryCache{
		enabled:    maxEntries > 0 && maxBytes > 0,
		maxEntries: maxEntries, maxBytes: maxBytes,
		entries: make(map[string]*streamedPreparedGeometryCacheEntry),
		owned:   make(map[*streamedPreparedGeometryCacheEntry]struct{}),
		assets:  make(map[AssetId]*streamedPreparedGeometryCacheEntry),
		builds:  make(map[string]*streamedPreparedGeometryBuild),
	}
}

func (c *streamedPreparedGeometryCache) getOrBuild(key string, build func() *volume.XBrickMap) (*volume.XBrickMap, bool) {
	key = strings.TrimSpace(key)
	if c == nil || key == "" {
		if build == nil {
			return nil, false
		}
		return build(), false
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		if build == nil {
			return nil, false
		}
		return build(), false
	}
	if entry := c.entries[key]; c.enabled && entry != nil {
		if entry.refCount == 0 {
			c.refreshUnpinnedLocked(entry)
		}
		c.stats.Hits++
		c.mu.Unlock()
		return entry.geometry, true
	}
	if pending := c.builds[key]; pending != nil {
		c.stats.BuildWaits++
		c.mu.Unlock()
		<-pending.done
		if pending.panicValue != nil {
			panic(pending.panicValue)
		}
		return pending.geometry, pending.hit
	}
	c.stats.Misses++
	if build == nil {
		c.mu.Unlock()
		return nil, false
	}
	pending := &streamedPreparedGeometryBuild{done: make(chan struct{})}
	c.builds[key] = pending
	c.mu.Unlock()

	// Builders and same-key waits run outside the mutex. Every outcome is
	// published to all joined callers before the in-flight slot is removed.
	func() {
		defer func() { pending.panicValue = recover() }()
		pending.geometry = build()
	}()
	c.mu.Lock()
	if pending.geometry != nil && pending.panicValue == nil && c.enabled && !c.closed {
		if entry := c.entries[key]; entry != nil {
			if entry.refCount == 0 {
				c.refreshUnpinnedLocked(entry)
			}
			pending.geometry, pending.hit = entry.geometry, true
			c.stats.Hits++
		} else {
			entry := c.admitLocked(key, pending.geometry)
			if streamedGeometryStorageCharge(entry.preparedStorage) > c.maxBytes {
				c.stats.OversizedBypasses++
				c.removeLocked(entry, false)
			} else {
				// Workers can drop prepared-only storage. An older asset victim must
				// wait for engine-thread maintenance rather than evicting newer data.
				c.evictLocked(false)
			}
		}
	}
	delete(c.builds, key)
	close(pending.done)
	c.mu.Unlock()
	if pending.panicValue != nil {
		panic(pending.panicValue)
	}
	return pending.geometry, pending.hit
}

func (c *streamedPreparedGeometryCache) admitLocked(key string, geometry *volume.XBrickMap, preparedVoxelCount ...int) *streamedPreparedGeometryCacheEntry {
	var voxelCount int
	if len(preparedVoxelCount) > 0 {
		voxelCount = preparedVoxelCount[0]
	} else {
		voxelCount = geometry.GetVoxelCount()
	}
	entry := &streamedPreparedGeometryCacheEntry{
		key: key, geometry: geometry, voxelCount: voxelCount,
		noWarm: !c.enabled || key == "",
	}
	entry.preparedStorage = c.storage.admit(geometry, streamedGeometryPrepared)
	if key != "" {
		c.entries[key] = entry
	}
	c.owned[entry] = struct{}{}
	c.refreshUnpinnedLocked(entry)
	c.stats.Entries++
	c.stats.Voxels += entry.voxelCount
	return entry
}

// acquireAsset, releaseAssetID, trim and close are engine-thread operations.
// Cleanup uses the registering AssetServer even if a direct caller supplies
// a different server later.
func (c *streamedPreparedGeometryCache) acquireAsset(assets *AssetServer, key string, geometry *volume.XBrickMap) (AssetId, bool) {
	id, reused, _ := c.acquirePreparedAsset(assets, key, geometry, nil)
	return id, reused
}

func (c *streamedPreparedGeometryCache) acquirePreparedAsset(assets *AssetServer, key string, geometry *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool, bool) {
	defer registration.release()
	if assets == nil || geometry == nil {
		return AssetId{}, false, false
	}
	if c == nil {
		if id, adopted := assets.adoptStreamedVoxelGeometry(registration, geometry); adopted {
			return id, false, true
		}
		return assets.RegisterSharedVoxelGeometry(geometry, ""), false, false
	}
	key = strings.TrimSpace(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return AssetId{}, false, false
	}
	var entry *streamedPreparedGeometryCacheEntry
	if key != "" {
		entry = c.entries[key]
	}
	if entry == nil {
		if count, matches := registration.countFor(geometry); matches {
			entry = c.admitLocked(key, geometry, count)
		} else {
			entry = c.admitLocked(key, geometry)
		}
	}
	if entry.refCount == 0 {
		c.unlinkUnpinnedLocked(entry)
		c.storage.adjust(entry.preparedStorage, streamedGeometryPinned, 1)
		c.storage.adjust(entry.assetStorage, streamedGeometryPinned, 1)
	}
	entry.refCount++
	if entry.asset != (AssetId{}) {
		c.stats.AssetReuses++
		c.evictLocked(true)
		return entry.asset, true, false
	}
	var adopted bool
	entry.asset, adopted = assets.adoptStreamedVoxelGeometry(registration, entry.geometry)
	if !adopted {
		entry.asset = assets.RegisterSharedVoxelGeometry(entry.geometry, "")
	}
	entry.assetServer = assets
	registered, _ := assets.GetVoxelGeometry(entry.asset)
	entry.assetStorage = c.storage.admit(registered.XBrickMap, streamedGeometryAsset)
	c.storage.adjust(entry.assetStorage, streamedGeometryPinned, 1)
	c.assets[entry.asset] = entry
	c.stats.AssetRegisters++
	if c.enabled && streamedGeometryStorageCharge(entry.preparedStorage, entry.assetStorage) > c.maxBytes {
		entry.noWarm = true
	}
	c.evictLocked(true)
	return entry.asset, false, adopted
}

// Preserve the keyed release seam for callers that already balance by key.
func (c *streamedPreparedGeometryCache) releaseAsset(assets *AssetServer, key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseLocked(c.entries[strings.TrimSpace(key)])
}

func (c *streamedPreparedGeometryCache) releaseAssetID(assets *AssetServer, id AssetId) {
	if id == (AssetId{}) {
		return
	}
	if c == nil {
		if assets != nil {
			assets.DeleteVoxelGeometry(id)
		}
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseLocked(c.assets[id])
}

func (c *streamedPreparedGeometryCache) releaseLocked(entry *streamedPreparedGeometryCacheEntry) {
	if entry == nil || entry.refCount == 0 {
		return
	}
	entry.refCount--
	if entry.refCount == 0 {
		c.storage.adjust(entry.preparedStorage, streamedGeometryPinned, -1)
		c.storage.adjust(entry.assetStorage, streamedGeometryPinned, -1)
		if entry.noWarm {
			c.removeLocked(entry, true)
		} else {
			c.refreshUnpinnedLocked(entry)
		}
	}
	c.evictLocked(true)
}

func (c *streamedPreparedGeometryCache) snapshot() streamedPreparedGeometryCacheStats {
	if c == nil {
		return streamedPreparedGeometryCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	stats.Bytes = c.storage.bytes
	stats.PreparedBytes = c.storage.preparedBytes
	stats.AssetBytes = c.storage.assetBytes
	stats.PinnedBytes = c.storage.pinnedBytes
	stats.MaxBytes = c.maxBytes
	if stats.Bytes > c.maxBytes {
		stats.OverBudgetBytes = stats.Bytes - c.maxBytes
	}
	return stats
}

func (c *streamedPreparedGeometryCache) evictLocked(canDeleteAssets bool) {
	for len(c.owned) > c.maxEntries || c.storage.bytes > c.maxBytes {
		victim := c.unpinnedOldest
		if victim == nil {
			return
		}
		c.stats.EvictionCandidateVisits++
		if !canDeleteAssets && victim.asset != (AssetId{}) {
			return
		}
		c.removeLocked(victim, true)
	}
}

// Only unpinned entries participate in eviction order. All links are owned by
// the cache mutex; pinned entries and removed entries have no links.
func (c *streamedPreparedGeometryCache) unlinkUnpinnedLocked(entry *streamedPreparedGeometryCacheEntry) {
	if entry.unpinnedPrev == nil && entry.unpinnedNext == nil && c.unpinnedOldest != entry {
		return
	}
	if entry.unpinnedPrev != nil {
		entry.unpinnedPrev.unpinnedNext = entry.unpinnedNext
	} else {
		c.unpinnedOldest = entry.unpinnedNext
	}
	if entry.unpinnedNext != nil {
		entry.unpinnedNext.unpinnedPrev = entry.unpinnedPrev
	} else {
		c.unpinnedNewest = entry.unpinnedPrev
	}
	entry.unpinnedPrev, entry.unpinnedNext = nil, nil
}

func (c *streamedPreparedGeometryCache) refreshUnpinnedLocked(entry *streamedPreparedGeometryCacheEntry) {
	c.unlinkUnpinnedLocked(entry)
	entry.unpinnedPrev = c.unpinnedNewest
	if c.unpinnedNewest != nil {
		c.unpinnedNewest.unpinnedNext = entry
	} else {
		c.unpinnedOldest = entry
	}
	c.unpinnedNewest = entry
}

func (c *streamedPreparedGeometryCache) removeLocked(entry *streamedPreparedGeometryCacheEntry, eviction bool) {
	c.unlinkUnpinnedLocked(entry)
	if entry.asset != (AssetId{}) {
		entry.assetServer.DeleteVoxelGeometry(entry.asset)
		delete(c.assets, entry.asset)
	}
	if entry.refCount > 0 {
		c.storage.adjust(entry.preparedStorage, streamedGeometryPinned, -1)
		c.storage.adjust(entry.assetStorage, streamedGeometryPinned, -1)
	}
	c.storage.adjust(entry.preparedStorage, streamedGeometryPrepared, -1)
	c.storage.adjust(entry.assetStorage, streamedGeometryAsset, -1)
	delete(c.entries, entry.key)
	delete(c.owned, entry)
	c.stats.Entries--
	c.stats.Voxels -= entry.voxelCount
	if eviction {
		c.stats.Evictions++
	}
}

func (c *streamedPreparedGeometryCache) trim(assets *AssetServer) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked(true)
}

func (c *streamedPreparedGeometryCache) close(assets *AssetServer) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for entry := range c.owned {
		c.removeLocked(entry, false)
	}
}

func streamedImportedWorldGeometryCacheKey(kind, path, payloadHash string, payloadSizeBytes int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "imported"
	}
	payloadHash = strings.TrimSpace(payloadHash)
	if payloadHash != "" {
		return fmt.Sprintf("%s:%s:%s:%d", kind, path, payloadHash, payloadSizeBytes)
	}
	return fmt.Sprintf("%s:%s", kind, path)
}
