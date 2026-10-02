package gpu

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// DefaultRetainedVoxelMapBudgetBytes is an assigned-slot retention budget.
// It excludes physical buffer/atlas capacity, lookup/object/material buffers,
// CPU geometry and temporary accounting; it is not a VRAM/process ceiling.
const DefaultRetainedVoxelMapBudgetBytes = 128 << 20

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
			block := info.BrickTableIndex / 64
			if !tableSlots[block] {
				tableSlots[block] = true
				bytes = addRetainedVoxelBytes(bytes, 64*BrickRecordSize)
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
	}
	m.retainedVoxelMapClock++
	return m.retainedVoxelMapClock
}

func (m *GpuBufferManager) compactRetainedVoxelMaps() {
	m.retainedVoxelMapPruned = false
	if len(m.retainedVoxelMaps) == 0 {
		m.retainedVoxelMaps = nil
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
	if m.retainedVoxelMapPruned {
		m.compactRetainedVoxelMaps()
	}
	if len(m.retainedVoxelMaps) == 0 {
		m.retainedVoxelMaps = nil
		return
	}
	stamp := m.nextRetainedVoxelMapUse()
	type inactiveEntry struct {
		mapRef *volume.XBrickMap
		entry  *retainedVoxelMapEntry
		bytes  uint64
	}
	var bytes uint64
	sectors := 0
	for xbm, entry := range m.retainedVoxelMaps {
		if entry == nil {
			continue
		}
		entry.Pinned = activeMaps[xbm]
		if entry.Pinned {
			entry.LastUse = stamp // Maintenance is usage, never an activation hit.
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
	var inactive []inactiveEntry
	for xbm, entry := range m.retainedVoxelMaps {
		if entry != nil && !entry.Pinned && xbm != nil {
			inactive = append(inactive, inactiveEntry{xbm, entry, entry.Bytes})
		}
	}
	sort.Slice(inactive, func(i, j int) bool { return inactive[i].entry.LastUse < inactive[j].entry.LastUse })
	evicted := false
	saturated := bytes == ^uint64(0)
	for _, victim := range inactive {
		if !saturated && !overBudget() {
			break
		}
		delete(m.retainedVoxelMaps, victim.mapRef)
		if alloc := m.Allocations[victim.mapRef]; alloc != nil {
			m.releaseVoxelMapAllocation(victim.mapRef, alloc)
		}
		if !saturated {
			bytes -= victim.bytes
		}
		sectors -= victim.entry.SectorCount
		m.retainedVoxelMapStats.Evictions++
		evicted = true
	}
	if evicted {
		// Saturated totals conservatively release every inactive entry; pin
		// pressure remains visible through fresh, saturating stats observations.
		m.compactRetainedVoxelMaps()
	}
}
