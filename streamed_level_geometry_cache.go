package gekko

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

const defaultStreamedPreparedGeometryCacheEntries = 256

type streamedPreparedGeometryCache struct {
	mu         sync.Mutex
	enabled    bool
	maxEntries int
	clock      uint64
	entries    map[string]*streamedPreparedGeometryCacheEntry
	stats      streamedPreparedGeometryCacheStats
}

type streamedPreparedGeometryCacheEntry struct {
	key        string
	geometry   *volume.XBrickMap
	asset      AssetId
	refCount   int
	voxelCount int
	lastUse    uint64
}

type streamedPreparedGeometryCacheStats struct {
	Entries        int
	Voxels         int
	Hits           int
	Misses         int
	Evictions      int
	AssetRegisters int
	AssetReuses    int
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

func newStreamedPreparedGeometryCache(maxEntries int) *streamedPreparedGeometryCache {
	cache := &streamedPreparedGeometryCache{
		enabled:    maxEntries > 0,
		maxEntries: maxEntries,
		entries:    make(map[string]*streamedPreparedGeometryCacheEntry),
	}
	return cache
}

func (c *streamedPreparedGeometryCache) getOrBuild(key string, build func() *volume.XBrickMap) (*volume.XBrickMap, bool) {
	key = strings.TrimSpace(key)
	if c == nil || !c.enabled || key == "" {
		if build == nil {
			return nil, false
		}
		return build(), false
	}

	c.mu.Lock()
	c.clock++
	if entry := c.entries[key]; entry != nil && entry.geometry != nil {
		entry.lastUse = c.clock
		c.stats.Hits++
		c.mu.Unlock()
		return entry.geometry, true
	}
	c.stats.Misses++
	c.mu.Unlock()
	if build == nil {
		return nil, false
	}
	geometry := build()
	if geometry == nil {
		return nil, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if entry := c.entries[key]; entry != nil && entry.geometry != nil {
		entry.lastUse = c.clock
		c.stats.Hits++
		return entry.geometry, true
	}
	entry := &streamedPreparedGeometryCacheEntry{
		key:        key,
		geometry:   geometry,
		voxelCount: geometry.GetVoxelCount(),
		lastUse:    c.clock,
	}
	c.entries[key] = entry
	c.evictLocked(nil, key)
	return geometry, false
}

func (c *streamedPreparedGeometryCache) acquireAsset(assets *AssetServer, key string, geometry *volume.XBrickMap) (AssetId, bool) {
	key = strings.TrimSpace(key)
	if assets == nil || geometry == nil || c == nil || !c.enabled || key == "" {
		if assets == nil || geometry == nil {
			return AssetId{}, false
		}
		return assets.RegisterSharedVoxelGeometry(geometry, ""), false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	entry := c.entries[key]
	if entry == nil {
		entry = &streamedPreparedGeometryCacheEntry{
			key:        key,
			geometry:   geometry,
			voxelCount: geometry.GetVoxelCount(),
		}
		c.entries[key] = entry
	} else if entry.geometry == nil {
		entry.geometry = geometry
		entry.voxelCount = geometry.GetVoxelCount()
	}
	entry.lastUse = c.clock
	entry.refCount++
	if entry.asset != (AssetId{}) {
		c.stats.AssetReuses++
		return entry.asset, true
	}
	entry.asset = assets.RegisterSharedVoxelGeometry(entry.geometry, "")
	c.stats.AssetRegisters++
	c.evictLocked(assets, key)
	return entry.asset, false
}

func (c *streamedPreparedGeometryCache) releaseAsset(assets *AssetServer, key string) {
	key = strings.TrimSpace(key)
	if c == nil || !c.enabled || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if entry := c.entries[key]; entry != nil {
		if entry.refCount > 0 {
			entry.refCount--
		}
		entry.lastUse = c.clock
	}
	c.evictLocked(assets, "")
}

func (c *streamedPreparedGeometryCache) snapshot() streamedPreparedGeometryCacheStats {
	if c == nil {
		return streamedPreparedGeometryCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	for _, entry := range c.entries {
		stats.Entries++
		stats.Voxels += entry.voxelCount
	}
	return stats
}

func (c *streamedPreparedGeometryCache) evictLocked(assets *AssetServer, protectedKey string) {
	if c == nil || !c.enabled || c.maxEntries <= 0 {
		return
	}
	for len(c.entries) > c.maxEntries {
		var victim *streamedPreparedGeometryCacheEntry
		for _, entry := range c.entries {
			if entry == nil || entry.refCount > 0 {
				continue
			}
			if protectedKey != "" && entry.key == protectedKey {
				continue
			}
			if assets == nil && entry.asset != (AssetId{}) {
				continue
			}
			if victim == nil || entry.lastUse < victim.lastUse {
				victim = entry
			}
		}
		if victim == nil {
			return
		}
		if assets != nil && victim.asset != (AssetId{}) {
			assets.DeleteVoxelGeometry(victim.asset)
		}
		delete(c.entries, victim.key)
		c.stats.Evictions++
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
