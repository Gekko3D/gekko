package gekko

import (
	"sort"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// DefaultVoxelMaterialTableCacheBytes is the default retained CPU table budget.
// Active tables remain pinned under pressure; this is not a process/GPU limit.
const DefaultVoxelMaterialTableCacheBytes = 16 << 20

// VoxelMaterialTableCacheStats reports private cache accounting. Pins describe
// the last completed instance sync; changed budgets take effect in MaxBytes
// immediately and trim inactive tables only at the next complete sync.
type VoxelMaterialTableCacheStats struct {
	Entries       int
	Bytes         uint64
	PinnedBytes   uint64
	MaxBytes      uint64
	PressureBytes uint64
	Builds        uint64
	Hits          uint64
	Evictions     uint64
}

type voxelMaterialTableEntry struct {
	bytes, lastUsed uint64
	pinned          bool
}

type voxelMaterialTableRetention struct {
	budgetBytes                  int64
	entries                      map[materialTableCacheKey]voxelMaterialTableEntry
	bytes, pinnedBytes, sequence uint64
	builds, hits, evictions      uint64
}

// SetVoxelMaterialTableCacheBudgetBytes configures main-thread retention.
// Zero restores the default; negative disables warm retention. Active tables
// stay pinned. Configuration never evicts or changes borrowed material slices.
func (s *VoxelRtState) SetVoxelMaterialTableCacheBudgetBytes(maxBytes int64) {
	if s != nil {
		s.materialTableRetention.budgetBytes = maxBytes
	}
}

func (owner *voxelMaterialTableRetention) maxBytes() uint64 {
	if owner.budgetBytes < 0 {
		return 0
	}
	if owner.budgetBytes == 0 {
		return DefaultVoxelMaterialTableCacheBytes
	}
	return uint64(owner.budgetBytes)
}

// VoxelMaterialTableCacheStats is a pure main-thread observation. Reads do not
// allocate, trim, perform cache lookups or advance work counters.
func (s *VoxelRtState) VoxelMaterialTableCacheStats() VoxelMaterialTableCacheStats {
	if s == nil {
		return VoxelMaterialTableCacheStats{}
	}
	owner := &s.materialTableRetention
	maxBytes := owner.maxBytes()
	pressure := uint64(0)
	if owner.pinnedBytes > maxBytes {
		pressure = owner.pinnedBytes - maxBytes
	}
	return VoxelMaterialTableCacheStats{
		Entries: len(s.materialTableCache), Bytes: owner.bytes,
		PinnedBytes: owner.pinnedBytes, MaxBytes: maxBytes, PressureBytes: pressure,
		Builds: owner.builds, Hits: owner.hits, Evictions: owner.evictions,
	}
}

func materialTableCharge(table []core.Material) uint64 {
	// Charge CPU backing capacity, never GPU encoded-row size or object count.
	// Metadata covers both key maps, slice/entry headers and allocation overhead.
	const metadata = uint64(256)
	size := uint64(unsafe.Sizeof(core.Material{}))
	capacity := uint64(cap(table))
	if capacity > (^uint64(0)-metadata)/size {
		return ^uint64(0)
	}
	return metadata + capacity*size
}

func addMaterialTableBytes(a, b uint64) uint64 {
	if b > ^uint64(0)-a {
		return ^uint64(0)
	}
	return a + b
}

func (s *VoxelRtState) nextMaterialTableUse() uint64 {
	owner := &s.materialTableRetention
	if owner.sequence == ^uint64(0) {
		// Preserve LRU order while starting a compact sequence era. Equal-age
		// entries intentionally have no deterministic tie-breaking promise.
		keys := make([]materialTableCacheKey, 0, len(owner.entries))
		for key := range owner.entries {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return owner.entries[keys[i]].lastUsed < owner.entries[keys[j]].lastUsed
		})
		for i, key := range keys {
			entry := owner.entries[key]
			entry.lastUsed = uint64(i) + 1
			owner.entries[key] = entry
		}
		owner.sequence = uint64(len(keys))
	}
	owner.sequence++
	return owner.sequence
}

func (s *VoxelRtState) touchMaterialTableCache(key materialTableCacheKey, table []core.Material) {
	owner := &s.materialTableRetention
	entry, present := owner.entries[key]
	if !present {
		if owner.entries == nil {
			owner.entries = make(map[materialTableCacheKey]voxelMaterialTableEntry)
		}
		entry.bytes = materialTableCharge(table)
		owner.bytes = addMaterialTableBytes(owner.bytes, entry.bytes)
	}
	entry.lastUsed = s.nextMaterialTableUse()
	owner.entries[key] = entry
}

// Run only after the complete instance pass and removed-object/key cleanup.
// Active usage refresh does not count as a builder cache hit.
func (s *VoxelRtState) trimMaterialTableCache() {
	owner := &s.materialTableRetention
	if len(s.materialTableCache) == 0 {
		s.materialTableCache, owner.entries = nil, nil
		owner.bytes, owner.pinnedBytes = 0, 0
		return
	}
	stamp := s.nextMaterialTableUse()
	for key, entry := range owner.entries {
		entry.pinned = false
		owner.entries[key] = entry
	}
	owner.pinnedBytes = 0
	for _, obj := range s.instanceMap {
		key, tracked := s.lastMaterialKeys[obj]
		if !tracked {
			continue
		}
		entry, retained := owner.entries[key]
		if !retained || entry.pinned {
			continue
		}
		entry.pinned, entry.lastUsed = true, stamp
		owner.entries[key] = entry
		owner.pinnedBytes = addMaterialTableBytes(owner.pinnedBytes, entry.bytes)
	}
	maxBytes := owner.maxBytes()
	if owner.bytes <= maxBytes {
		return
	}
	var inactive []materialTableCacheKey
	for key, entry := range owner.entries {
		if !entry.pinned {
			inactive = append(inactive, key)
		}
	}
	sort.Slice(inactive, func(i, j int) bool {
		return owner.entries[inactive[i]].lastUsed < owner.entries[inactive[j]].lastUsed
	})
	evicted := false
	saturated := owner.bytes == ^uint64(0)
	for _, key := range inactive {
		if !saturated && owner.bytes <= maxBytes {
			break
		}
		entry := owner.entries[key]
		delete(s.materialTableCache, key)
		delete(owner.entries, key)
		if !saturated {
			owner.bytes -= entry.bytes
		}
		owner.evictions++
		evicted = true
	}
	if !evicted {
		return
	}
	if saturated {
		// A saturated aggregate cannot safely subtract back to an apparently
		// fitting total. Conservatively release all inactive keys and recount.
		owner.bytes = 0
		for _, entry := range owner.entries {
			owner.bytes = addMaterialTableBytes(owner.bytes, entry.bytes)
		}
	}
	// Eviction releases only cache references. Objects and external borrowers
	// keep valid backing; maps are rebuilt to release historical peak capacity.
	var tables map[materialTableCacheKey][]core.Material
	var entries map[materialTableCacheKey]voxelMaterialTableEntry
	if len(s.materialTableCache) != 0 {
		tables = make(map[materialTableCacheKey][]core.Material, len(s.materialTableCache))
		entries = make(map[materialTableCacheKey]voxelMaterialTableEntry, len(owner.entries))
		for key, table := range s.materialTableCache {
			tables[key] = table
			entries[key] = owner.entries[key]
		}
	}
	s.materialTableCache, owner.entries = tables, entries
}
