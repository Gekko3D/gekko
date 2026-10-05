package gpu

import (
	"container/heap"
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// DefaultRetainedVoxelMapBudgetBytes is an assigned-slot retention budget.
// It excludes physical buffer/atlas capacity, lookup/object/material buffers,
// CPU geometry and temporary accounting; it is not a VRAM/process ceiling.
const DefaultRetainedVoxelMapBudgetBytes = 128 << 20

// Inactive owners only; scene membership and accounting remain main-thread work.
type retainedVoxelMapHeap []*retainedVoxelMapEntry

func (h retainedVoxelMapHeap) Len() int           { return len(h) }
func (h retainedVoxelMapHeap) Less(i, j int) bool { return h[i].LastUse < h[j].LastUse }
func (h retainedVoxelMapHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex, h[j].heapIndex = i, j
}
func (h *retainedVoxelMapHeap) Push(value any) {
	entry := value.(*retainedVoxelMapEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}
func (h *retainedVoxelMapHeap) Pop() any {
	last := len(*h) - 1
	entry := (*h)[last]
	(*h)[last] = nil
	*h = (*h)[:last]
	entry.heapIndex = -1
	return entry
}

func (m *GpuBufferManager) unlinkRetainedVoxelMapInactive(entry *retainedVoxelMapEntry) {
	if entry.heapIndex >= 0 {
		heap.Remove(&m.retainedVoxelMapInactive, entry.heapIndex)
	}
}

func (m *GpuBufferManager) makeRetainedVoxelMapInactive(entry *retainedVoxelMapEntry) {
	if !entry.Pinned && entry.mapRef != nil && entry.heapIndex < 0 {
		heap.Push(&m.retainedVoxelMapInactive, entry)
	}
}

func (m *GpuBufferManager) touchRetainedVoxelMap(entry *retainedVoxelMapEntry, stamp uint64) {
	entry.LastUse = stamp
	if entry.heapIndex >= 0 {
		heap.Fix(&m.retainedVoxelMapInactive, entry.heapIndex)
	}
}

func (m *GpuBufferManager) removeRetainedVoxelMapEntry(xbm *volume.XBrickMap) {
	if entry := m.retainedVoxelMaps[xbm]; entry != nil {
		m.unlinkRetainedVoxelMapInactive(entry)
		entry.mapRef = nil
	}
	delete(m.retainedVoxelMaps, xbm)
}

func addRetainedVoxelBytes(a, b uint64) uint64 {
	if b > ^uint64(0)-a {
		return ^uint64(0)
	}
	return a + b
}

// Inspect allocation snapshots and actual mappings, never mutable CPU map
// contents or current brick flags. Each assigned slot is charged once within
// this exact map; nested pointer sharing across maps gains no new ownership.
func (m *GpuBufferManager) retainedVoxelMapBytes(xbm *volume.XBrickMap) uint64 {
	bytes := uint64(256) // Retention entry and conservative owner metadata.
	alloc := m.Allocations[xbm]
	if alloc == nil {
		return bytes
	}
	sectorSlots, tableSlots := map[uint32]bool{}, map[uint32]bool{}
	for _, sector := range alloc.Sectors {
		if info, present := m.SectorToInfo[sector]; present {
			if !sectorSlots[info.SlotIndex] {
				sectorSlots[info.SlotIndex] = true
				bytes = addRetainedVoxelBytes(bytes, 32)
			}
			block := info.BrickTableIndex
			capacity := uint32(64)
			if info.packed != nil {
				capacity = info.packed.capacity
			}
			if capacity != 0 && !tableSlots[block] {
				tableSlots[block] = true
				bytes = addRetainedVoxelBytes(bytes, uint64(capacity)*BrickRecordSize)
			}
		}
	}
	auxSlots, payloadSlots := map[uint32]bool{}, map[PayloadSlot]bool{}
	for _, bricks := range alloc.Bricks {
		if bricks == nil {
			continue
		}
		for _, brick := range bricks {
			if brick == nil {
				continue
			}
			if slot, present := m.BrickToAuxSlot[brick]; present && !auxSlots[slot] {
				auxSlots[slot] = true
				bytes = addRetainedVoxelBytes(bytes, VoxelAuxRecordBytes)
			}
			if slot, present := m.BrickToSlot[brick]; present && !payloadSlots[slot] {
				payloadSlots[slot] = true
				bytes = addRetainedVoxelBytes(bytes, payloadBytesPerBrick)
			}
		}
	}
	return bytes
}

func (m *GpuBufferManager) markRetainedVoxelMapAccountingDirty(xbm *volume.XBrickMap) {
	if entry := m.retainedVoxelMaps[xbm]; entry != nil {
		entry.AccountingDirty = true
	}
}

func (m *GpuBufferManager) nextRetainedVoxelMapUse() uint64 {
	if m.retainedVoxelMapClock == ^uint64(0) {
		keys := make([]*volume.XBrickMap, 0, len(m.retainedVoxelMaps))
		for xbm, entry := range m.retainedVoxelMaps {
			if entry != nil {
				keys = append(keys, xbm)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			return m.retainedVoxelMaps[keys[i]].LastUse < m.retainedVoxelMaps[keys[j]].LastUse
		})
		for i, xbm := range keys {
			m.retainedVoxelMaps[xbm].LastUse = uint64(i) + 1
		}
		m.retainedVoxelMapClock = uint64(len(keys))
		heap.Init(&m.retainedVoxelMapInactive)
	}
	m.retainedVoxelMapClock++
	return m.retainedVoxelMapClock
}

func (m *GpuBufferManager) compactRetainedVoxelMaps() {
	m.retainedVoxelMapPruned = false
	if len(m.retainedVoxelMaps) == 0 {
		m.retainedVoxelMaps = nil
		for len(m.retainedVoxelMapInactive) > 0 {
			heap.Pop(&m.retainedVoxelMapInactive).(*retainedVoxelMapEntry).mapRef = nil
		}
		m.retainedVoxelMapInactive = nil
		return
	}
	retained := make(map[*volume.XBrickMap]*retainedVoxelMapEntry, len(m.retainedVoxelMaps))
	for xbm, entry := range m.retainedVoxelMaps {
		retained[xbm] = entry
	}
	m.retainedVoxelMaps = retained
}

func (m *GpuBufferManager) evictRetainedVoxelMaps(activeMaps map[*volume.XBrickMap]bool) {
	if m == nil {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	if m.retainedVoxelMapPruned {
		m.compactRetainedVoxelMaps()
	}
	if len(m.retainedVoxelMaps) == 0 {
		m.compactRetainedVoxelMaps()
		return
	}
	stamp := m.nextRetainedVoxelMapUse()
	var bytes uint64
	sectors := 0
	for xbm, entry := range m.retainedVoxelMaps {
		if entry == nil {
			continue
		}
		entry.Pinned = activeMaps[xbm]
		if entry.Pinned {
			m.unlinkRetainedVoxelMapInactive(entry)
			entry.LastUse = stamp // Maintenance is usage, never an activation hit.
		} else {
			m.makeRetainedVoxelMapInactive(entry)
		}
		if entry.AccountingDirty {
			entry.Bytes = m.retainedVoxelMapBytes(xbm)
			entry.AccountingDirty = false
		}
		bytes = addRetainedVoxelBytes(bytes, entry.Bytes)
		sectors += entry.SectorCount
	}
	overBudget := func() bool {
		return (m.RetainedVoxelMapBudgetBytes > 0 && bytes > uint64(m.RetainedVoxelMapBudgetBytes)) ||
			(m.RetainedVoxelMapBudgetSectors > 0 && sectors > m.RetainedVoxelMapBudgetSectors)
	}
	if !overBudget() {
		return
	}
	evicted := false
	saturated := bytes == ^uint64(0)
	for len(m.retainedVoxelMapInactive) > 0 {
		if !saturated && !overBudget() {
			break
		}
		victim := m.retainedVoxelMapInactive[0]
		m.retainedVoxelMapStats.EvictionCandidateVisits++
		xbm := victim.mapRef
		m.removeRetainedVoxelMapEntry(xbm)
		if alloc := m.Allocations[xbm]; alloc != nil {
			m.releaseVoxelMapAllocation(xbm, alloc)
		}
		if !saturated {
			bytes -= victim.Bytes
		}
		sectors -= victim.SectorCount
		m.retainedVoxelMapStats.Evictions++
		evicted = true
	}
	if evicted {
		// Saturated totals conservatively release every inactive entry; pin
		// pressure remains visible through fresh, saturating stats observations.
		m.compactRetainedVoxelMaps()
	}
}
