package gekko

import (
	"container/heap"
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
	// EvictionCandidateVisits counts cumulative nonnil pressure victims.
	EvictionCandidateVisits uint64
}

type voxelMaterialTableEntry struct {
	key             materialTableCacheKey
	bytes, lastUsed uint64
	pinned          bool
	candidateIndex  int
}

// Entry identity survives owner-map compaction. Only inactive retained entries
// belong to this heap; becoming inactive keeps the completed frame's saved age.
type voxelMaterialTableCandidates []*voxelMaterialTableEntry

func (h voxelMaterialTableCandidates) Len() int { return len(h) }
func (h voxelMaterialTableCandidates) Less(i, j int) bool {
	return h[i].lastUsed < h[j].lastUsed
}
func (h voxelMaterialTableCandidates) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].candidateIndex, h[j].candidateIndex = i, j
}
func (h *voxelMaterialTableCandidates) Push(value any) {
	entry := value.(*voxelMaterialTableEntry)
	entry.candidateIndex = len(*h)
	*h = append(*h, entry)
}
func (h *voxelMaterialTableCandidates) Pop() any {
	last := len(*h) - 1
	entry := (*h)[last]
	(*h)[last] = nil
	entry.candidateIndex = -1
	if last == 0 {
		*h = nil
	} else {
		// A nonempty heap may keep peak capacity, but removed slots hold no refs.
		*h = (*h)[:last]
	}
	return entry
}

type voxelMaterialTableRetention struct {
	budgetBytes                  int64
	entries                      map[materialTableCacheKey]*voxelMaterialTableEntry
	bytes, pinnedBytes, sequence uint64
	builds, hits, evictions      uint64
	evictionCandidateVisits      uint64
	candidates                   voxelMaterialTableCandidates
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
		EvictionCandidateVisits: owner.evictionCandidateVisits,
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
		}
		heap.Init(&owner.candidates)
		owner.sequence = uint64(len(keys))
	}
	owner.sequence++
	return owner.sequence
}

func (s *VoxelRtState) touchMaterialTableCache(key materialTableCacheKey, table []core.Material) {
	owner := &s.materialTableRetention
	entry, present := owner.entries[key]
	stamp := s.nextMaterialTableUse()
	if !present {
		if owner.entries == nil {
			owner.entries = make(map[materialTableCacheKey]*voxelMaterialTableEntry)
		}
		entry = &voxelMaterialTableEntry{
			key: key, bytes: materialTableCharge(table), lastUsed: stamp,
			candidateIndex: -1,
		}
		owner.entries[key] = entry
		owner.bytes = addMaterialTableBytes(owner.bytes, entry.bytes)
		heap.Push(&owner.candidates, entry)
		return
	}
	entry.lastUsed = stamp
	if entry.candidateIndex >= 0 {
		heap.Fix(&owner.candidates, entry.candidateIndex)
	}
}

// Run only after the complete instance pass and removed-object/key cleanup.
// Active usage refresh does not count as a builder cache hit.
func (s *VoxelRtState) trimMaterialTableCache() {
	owner := &s.materialTableRetention
	if len(s.materialTableCache) == 0 {
		s.materialTableCache, owner.entries, owner.candidates = nil, nil, nil
		owner.bytes, owner.pinnedBytes = 0, 0
		return
	}
	// Gather the complete distinct current-key set before owner maintenance.
	// Hidden streamed objects participate through the same instance map.
	activeKeys := make(map[materialTableCacheKey]struct{})
	for _, obj := range s.instanceMap {
		if key, tracked := s.lastMaterialKeys[obj]; tracked {
			activeKeys[key] = struct{}{}
		}
	}
	stamp := s.nextMaterialTableUse()
	owner.pinnedBytes = 0
	for key, entry := range owner.entries {
		_, entry.pinned = activeKeys[key]
		if entry.pinned {
			if entry.candidateIndex >= 0 {
				heap.Remove(&owner.candidates, entry.candidateIndex)
			}
			entry.lastUsed = stamp
			owner.pinnedBytes = addMaterialTableBytes(owner.pinnedBytes, entry.bytes)
		} else if entry.candidateIndex < 0 {
			heap.Push(&owner.candidates, entry)
		}
	}
	maxBytes := owner.maxBytes()
	if owner.bytes <= maxBytes {
		return
	}
	evicted := false
	saturated := owner.bytes == ^uint64(0)
	for len(owner.candidates) != 0 && (saturated || owner.bytes > maxBytes) {
		entry := heap.Pop(&owner.candidates).(*voxelMaterialTableEntry)
		owner.evictionCandidateVisits++
		delete(s.materialTableCache, entry.key)
		delete(owner.entries, entry.key)
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
	var entries map[materialTableCacheKey]*voxelMaterialTableEntry
	if len(s.materialTableCache) != 0 {
		tables = make(map[materialTableCacheKey][]core.Material, len(s.materialTableCache))
		entries = make(map[materialTableCacheKey]*voxelMaterialTableEntry, len(owner.entries))
		for key, table := range s.materialTableCache {
			tables[key] = table
			entries[key] = owner.entries[key]
		}
	}
	s.materialTableCache, owner.entries = tables, entries
}
