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
	key                        string
	geometry                   *volume.XBrickMap
	source                     *streamedGeometrySource
	promotion                  *streamedGeometryPromotion
	preparedStorage            *streamedGeometryStorageNode
	assetStorage               *streamedGeometryStorageNode
	asset                      AssetId
	assetServer                *AssetServer
	refCount                   int
	voxelCount                 int
	unpinnedPrev               *streamedPreparedGeometryCacheEntry
	unpinnedNext               *streamedPreparedGeometryCacheEntry
	noWarm                     bool
	preparedStandaloneBytes    int64
	preparedStandaloneCaptured bool
}

type streamedPreparedGeometryBuild struct {
	done       chan struct{}
	source     *streamedGeometrySource
	promotion  *streamedGeometryPromotion
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
	StorageReferenceVisits  int
	StorageCaptureVisits    int
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
	source, promotion, hit := c.getOrBuildSourceRequest(key, false, build)
	return c.promoteSource(strings.TrimSpace(key), source, promotion), hit
}

func (c *streamedPreparedGeometryCache) getOrBuildSource(key string, compactEligible bool, build func() *volume.XBrickMap) (*streamedGeometrySource, bool) {
	source, _, hit := c.getOrBuildSourceRequest(key, compactEligible, build)
	return source, hit
}

func (c *streamedPreparedGeometryCache) getOrBuildSourceRequest(key string, compactEligible bool, build func() *volume.XBrickMap) (*streamedGeometrySource, *streamedGeometryPromotion, bool) {
	key = strings.TrimSpace(key)
	bypass := func() (*streamedGeometrySource, *streamedGeometryPromotion, bool) {
		if build == nil {
			return nil, nil, false
		}
		return newStreamedGeometrySource(build(), compactEligible), &streamedGeometryPromotion{}, false
	}
	if c == nil || key == "" {
		return bypass()
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return bypass()
	}
	if entry := c.entries[key]; c.enabled && entry != nil {
		if entry.refCount == 0 {
			c.refreshUnpinnedLocked(entry)
		}
		c.stats.Hits++
		source, promotion := entry.source, entry.promotion
		c.mu.Unlock()
		return source, promotion, true
	}
	if pending := c.builds[key]; pending != nil {
		c.stats.BuildWaits++
		c.mu.Unlock()
		<-pending.done
		if pending.panicValue != nil {
			panic(pending.panicValue)
		}
		return pending.source, pending.promotion, pending.hit
	}
	c.stats.Misses++
	if build == nil {
		c.mu.Unlock()
		return nil, nil, false
	}
	pending := &streamedPreparedGeometryBuild{done: make(chan struct{}), promotion: &streamedGeometryPromotion{}}
	c.builds[key] = pending
	c.mu.Unlock()
	func() {
		defer func() { pending.panicValue = recover() }()
		pending.source = newStreamedGeometrySource(build(), compactEligible)
	}()
	c.mu.Lock()
	if pending.source != nil && pending.panicValue == nil && c.enabled && !c.closed {
		if entry := c.entries[key]; entry != nil {
			if entry.refCount == 0 {
				c.refreshUnpinnedLocked(entry)
			}
			pending.source, pending.promotion, pending.hit = entry.source, entry.promotion, true
			c.stats.Hits++
		} else {
			entry := c.admitSourceLocked(key, pending.source, pending.promotion)
			if entry.preparedStandaloneCharge() > c.maxBytes {
				c.stats.OversizedBypasses++
				c.removeLocked(entry, false)
			} else {
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
	return pending.source, pending.promotion, pending.hit
}

// The coordinator belongs to a build/cache owner, never to a pending snapshot.
// It shares uncached/evicted build materialization without retaining the old
// compact source after promotion. Work and waiter synchronization avoid c.mu.
func (c *streamedPreparedGeometryCache) promoteSource(key string, source *streamedGeometrySource, promotion *streamedGeometryPromotion) *volume.XBrickMap {
	if source == nil {
		return nil
	}
	if source.dense != nil {
		source.exposed.Store(true)
		return source.dense
	}
	promotion.once.Do(func() {
		defer func() { promotion.panicValue = recover() }()
		promotion.dense = source.materialize()
		if c == nil {
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		entry := c.entries[key]
		if c.closed || entry == nil || entry.source != source {
			return
		}
		if entry.refCount > 0 {
			c.storage.adjust(entry.preparedStorage, streamedGeometryPinned, -1)
		}
		c.storage.adjust(entry.preparedStorage, streamedGeometryPrepared, -1)
		entry.source = &streamedGeometrySource{dense: promotion.dense, qualified: true}
		entry.source.exposed.Store(true)
		entry.geometry = promotion.dense
		entry.preparedStorage = c.storage.admitSource(entry.source, streamedGeometryPrepared)
		if entry.refCount > 0 {
			c.storage.adjust(entry.preparedStorage, streamedGeometryPinned, 1)
		}
		entry.preparedStandaloneCaptured = false
		if streamedGeometryStorageCharge(entry.preparedStorage, entry.assetStorage) > c.maxBytes {
			entry.noWarm = true
		}
		c.evictLocked(false)
	})
	if promotion.panicValue != nil {
		panic(promotion.panicValue)
	}
	return promotion.dense
}

func (c *streamedPreparedGeometryCache) admitLocked(key string, geometry *volume.XBrickMap, preparedVoxelCount ...int) *streamedPreparedGeometryCacheEntry {
	return c.admitSourceLocked(key, newStreamedGeometrySource(geometry, false), &streamedGeometryPromotion{}, preparedVoxelCount...)
}

func (c *streamedPreparedGeometryCache) admitSourceLocked(key string, source *streamedGeometrySource, promotion *streamedGeometryPromotion, preparedVoxelCount ...int) *streamedPreparedGeometryCacheEntry {
	var voxelCount int
	if len(preparedVoxelCount) > 0 {
		voxelCount = preparedVoxelCount[0]
	} else {
		voxelCount = source.count()
	}
	entry := &streamedPreparedGeometryCacheEntry{
		key: key, source: source, promotion: promotion, geometry: source.dense, voxelCount: voxelCount,
		noWarm: !c.enabled || key == "",
	}
	entry.preparedStorage = c.storage.admitSource(source, streamedGeometryPrepared)
	if key != "" {
		c.entries[key] = entry
	}
	c.owned[entry] = struct{}{}
	c.refreshUnpinnedLocked(entry)
	c.stats.Entries++
	c.stats.Voxels += entry.voxelCount
	return entry
}

// Used under c.mu only when standalone warm-retention policy needs this value.
// Generic acquire-only paths keep their existing union traversal.
func (entry *streamedPreparedGeometryCacheEntry) preparedStandaloneCharge() int64 {
	if !entry.preparedStandaloneCaptured {
		entry.preparedStandaloneBytes = streamedGeometryStorageCharge(entry.preparedStorage)
		entry.preparedStandaloneCaptured = true
	}
	return entry.preparedStandaloneBytes
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
	if c.closed {
		c.mu.Unlock()
		return AssetId{}, false, false
	}
	var entry *streamedPreparedGeometryCacheEntry
	if key != "" {
		entry = c.entries[key]
	}
	if entry != nil && entry.geometry == nil {
		// A compact winner is independent of the caller's dense candidate.
		// Keep it compact and use the source path's outside-lock fallback and
		// current-identity/closed checks before publishing the asset.
		source := entry.source
		c.mu.Unlock()
		return c.acquirePreparedSourceAsset(assets, key, source, registration)
	}
	defer c.mu.Unlock()
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
	var descriptor *streamedGeometryStorageDescriptor
	entry.asset, descriptor, adopted = assets.adoptStreamedVoxelGeometryWithStorage(registration, entry.geometry)
	if !adopted {
		entry.asset = assets.RegisterSharedVoxelGeometry(entry.geometry, "")
	}
	entry.assetServer = assets
	registered, _ := assets.GetVoxelGeometry(entry.asset)
	entry.assetStorage = c.storage.installDescriptor(descriptor, registered.XBrickMap)
	qualifiedDescriptor := entry.assetStorage != nil
	if qualifiedDescriptor {
		c.storage.adjust(entry.assetStorage, streamedGeometryAsset, 1)
	} else {
		entry.assetStorage = c.storage.admit(registered.XBrickMap, streamedGeometryAsset)
	}
	c.storage.adjust(entry.assetStorage, streamedGeometryPinned, 1)
	c.assets[entry.asset] = entry
	c.stats.AssetRegisters++
	if c.enabled {
		var standaloneBytes int64
		if qualifiedDescriptor {
			// Copy owns distinct map/sector/brick objects, so this exact path can
			// add the two standalone charges. Generic/shared graphs need union.
			standaloneBytes = runtimeContentChargeSum(entry.preparedStandaloneCharge(), descriptor.geometryBytes)
		} else {
			standaloneBytes = streamedGeometryStorageCharge(entry.preparedStorage, entry.assetStorage)
		}
		if standaloneBytes > c.maxBytes {
			entry.noWarm = true
		}
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
	stats.StorageReferenceVisits = c.storage.referenceVisits
	stats.StorageCaptureVisits = c.storage.captureVisits
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

// Source-qualified packets use the same cache entries, pins, asset identities and
// cleanup. Registration work that requires dense fallback runs outside c.mu.
func (c *streamedPreparedGeometryCache) acquirePreparedSourceAsset(assets *AssetServer, key string, source *streamedGeometrySource, registration *streamedGeometryRegistration) (AssetId, bool, bool) {
	defer registration.release()
	if assets == nil || source == nil {
		return AssetId{}, false, false
	}
	if c == nil {
		if id, _, adopted := assets.adoptStreamedVoxelSourceWithStorage(registration, source); adopted {
			return id, false, true
		}
		return assets.RegisterSharedVoxelGeometry(source.materialize(), ""), false, false
	}
	key = strings.TrimSpace(key)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return AssetId{}, false, false
	}
	entry := c.entries[key]
	if entry == nil {
		if count, matches := registration.countForSource(source); matches {
			entry = c.admitSourceLocked(key, source, &streamedGeometryPromotion{}, count)
		} else {
			entry = c.admitSourceLocked(key, source, &streamedGeometryPromotion{})
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
		id := entry.asset
		c.mu.Unlock()
		return id, true, false
	}
	var adopted bool
	var descriptor *streamedGeometryStorageDescriptor
	for {
		current := entry.source
		if current == source {
			entry.asset, descriptor, adopted = assets.adoptStreamedVoxelSourceWithStorage(registration, current)
			if adopted {
				break
			}
		}
		c.mu.Unlock()
		// This snapshot is a fallback only; it never becomes a second retained cache
		// authority. A concurrent promotion is rechecked before asset publication.
		geometry := current.materialize()
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return AssetId{}, false, false
		}
		if entry.source != current {
			continue
		}
		entry.asset = assets.RegisterSharedVoxelGeometry(geometry, "")
		break
	}
	entry.assetServer = assets
	registered, _ := assets.GetVoxelGeometry(entry.asset)
	entry.assetStorage = c.storage.installDescriptor(descriptor, registered.XBrickMap)
	if entry.assetStorage != nil {
		c.storage.adjust(entry.assetStorage, streamedGeometryAsset, 1)
	} else {
		entry.assetStorage = c.storage.admit(registered.XBrickMap, streamedGeometryAsset)
	}
	c.storage.adjust(entry.assetStorage, streamedGeometryPinned, 1)
	c.assets[entry.asset] = entry
	c.stats.AssetRegisters++
	if c.enabled && streamedGeometryStorageCharge(entry.preparedStorage, entry.assetStorage) > c.maxBytes {
		entry.noWarm = true
	}
	c.evictLocked(true)
	id := entry.asset
	c.mu.Unlock()
	return id, false, adopted
}

// Used only when current backing/spawn actually needs a dense source. Generic
// exposure promotes through the shared coordinator and preserves current edits.
func (c *streamedPreparedGeometryCache) densePreparedSource(key string, captured *streamedGeometrySource) *volume.XBrickMap {
	if captured == nil {
		return nil
	}
	if c != nil {
		c.mu.Lock()
		if entry := c.entries[strings.TrimSpace(key)]; !c.closed && entry != nil {
			source, promotion := entry.source, entry.promotion
			c.mu.Unlock()
			return c.promoteSource(strings.TrimSpace(key), source, promotion)
		}
		c.mu.Unlock()
	}
	return captured.materialize()
}
