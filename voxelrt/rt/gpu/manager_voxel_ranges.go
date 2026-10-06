package gpu

import (
	"math/bits"
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type voxelIndexInterval struct{ base, size uint64 }
type voxelIndexRanges struct {
	tail   uint64
	free   []voxelIndexInterval
	shared bool
}

// Shared interval mechanics use caller-selected index coordinates.
// Brick records and auxiliary words own independent allocator instances.
type brickRecordRanges = voxelIndexRanges

func (a *voxelIndexRanges) clone() voxelIndexRanges { a.shared = true; b := *a; return b }
func (a *voxelIndexRanges) writable() {
	if a.shared {
		a.free = append([]voxelIndexInterval(nil), a.free...)
		a.shared = false
	}
}
func (a *voxelIndexRanges) alloc(size uint32, limit uint64) (uint32, bool) {
	if size == 0 {
		return 0, true
	}
	for i, r := range a.free {
		if r.size >= uint64(size) && r.base+uint64(size) <= limit && r.base+uint64(size) <= uint64(^uint32(0)) {
			a.writable()
			base := r.base
			a.free[i].base += uint64(size)
			a.free[i].size -= uint64(size)
			if a.free[i].size == 0 {
				a.free = append(a.free[:i], a.free[i+1:]...)
			}
			return uint32(base), true
		}
		// A completed suffix and unused physical tail are one contiguous span.
		// Sorted intervals keep earlier adequate free spans preferred.
		if r.base+r.size == a.tail && r.size < uint64(size) && r.base+uint64(size) <= limit && r.base+uint64(size) <= uint64(^uint32(0)) {
			a.writable()
			a.free = append(a.free[:i], a.free[i+1:]...)
			a.tail = r.base + uint64(size)
			return uint32(r.base), true
		}
	}
	if a.tail+uint64(size) > limit || a.tail+uint64(size) > uint64(^uint32(0)) {
		return 0, false
	}
	base := a.tail
	a.tail += uint64(size)
	return uint32(base), true
}
func (a *voxelIndexRanges) release(base, size uint32) {
	if size == 0 {
		return
	}
	a.writable()
	a.free = append(a.free, voxelIndexInterval{uint64(base), uint64(size)})
	sort.Slice(a.free, func(i, j int) bool { return a.free[i].base < a.free[j].base })
	out := a.free[:0]
	for _, r := range a.free {
		if len(out) > 0 && out[len(out)-1].base+out[len(out)-1].size == r.base {
			out[len(out)-1].size += r.size
		} else {
			out = append(out, r)
		}
	}
	a.free = out
}
func packedBrickCapacity(mask uint64) uint32 {
	n := uint32(bits.OnesCount64(mask))
	if n == 0 {
		return 0
	}
	return uint32(1) << bits.Len32(n-1)
}
func packedBrickRank(mask uint64, index int) uint32 {
	return uint32(bits.OnesCount64(mask & ((uint64(1) << index) - 1)))
}

type packedSectorOwner struct {
	xbm *volume.XBrickMap
	key [3]int
}
type packedSectorRange struct {
	base, capacity uint32
	mask           uint64
	published      bool
	pointers       [64]*volume.Brick
	owners         map[packedSectorOwner]bool
}
type retiredBrickRange struct {
	base, capacity uint32
	queue          *wgpu.Queue
	submission     uint64
}

func (m *GpuBufferManager) ensureBrickRecordRanges() bool {
	prefix := uint64(m.BrickAlloc.Tail) * 64
	if !m.brickRangesInitialized {
		for _, info := range m.SectorToInfo {
			if info.packed == nil {
				prefix = max(prefix, uint64(info.BrickTableIndex)+64)
			}
		}
		m.brickLegacyPrefix = prefix
		m.brickRanges.tail = prefix
		m.brickRangesInitialized = true
	}
	if prefix > m.brickLegacyPrefix {
		if !m.brickPackedLeased {
			m.brickLegacyPrefix = prefix
			m.brickRanges.tail = max(m.brickRanges.tail, prefix)
			return true
		}
		return false
	}
	return true
}
func (m *GpuBufferManager) retireBrickRange(base, capacity uint32) {
	if capacity != 0 {
		m.retiredBrickRanges = append(m.retiredBrickRanges, retiredBrickRange{base: base, capacity: capacity})
	}
}
func (m *GpuBufferManager) advanceRetiredBrickRanges() {
	out := m.retiredBrickRanges[:0]
	for _, r := range m.retiredBrickRanges {
		done := false
		if r.queue != nil {
			if poll, ok := m.voxelNative.(interface {
				SubmissionComplete(*wgpu.Queue, uint64) bool
			}); ok {
				done = poll.SubmissionComplete(r.queue, r.submission)
			} else if m.Device != nil {
				done = m.Device.Poll(false, &wgpu.WrappedSubmissionIndex{Queue: r.queue, SubmissionIndex: wgpu.SubmissionIndex(r.submission)})
			}
		}
		if done {
			m.brickRanges.release(r.base, r.capacity)
		} else {
			out = append(out, r)
		}
	}
	for i := len(out); i < len(m.retiredBrickRanges); i++ {
		m.retiredBrickRanges[i] = retiredBrickRange{}
	}
	m.retiredBrickRanges = out
}

// plannedBrickRange is a placement promise, never an assigned lease.
type plannedBrickRange struct {
	base, capacity uint32
	mask           uint64
}

// claim removes exactly the planned span without depending on service order.
// A later tail claim leaves earlier deferred placements as ordinary free gaps.
func (a *voxelIndexRanges) claim(base, size uint32, limit uint64) bool {
	if size == 0 {
		return true
	}
	start, end := uint64(base), uint64(base)+uint64(size)
	if end > limit || end > uint64(^uint32(0)) {
		return false
	}
	if start >= a.tail {
		if start > a.tail {
			a.release(uint32(a.tail), uint32(start-a.tail))
		}
		a.tail = end
		return true
	}
	for i, r := range a.free {
		if start >= r.base && start < a.tail && r.base+r.size == a.tail && end > a.tail {
			a.writable()
			if start > r.base {
				a.free[i].size = start - r.base
			} else {
				a.free = append(a.free[:i], a.free[i+1:]...)
			}
			a.tail = end
			return true
		}
		if start < r.base || end > r.base+r.size {
			continue
		}
		a.writable()
		left, right := start-r.base, r.base+r.size-end
		if left > 0 && right > 0 {
			a.free[i] = voxelIndexInterval{r.base, left}
			a.free = append(a.free, voxelIndexInterval{end, right})
			sort.Slice(a.free, func(i, j int) bool { return a.free[i].base < a.free[j].base })
		} else if left > 0 {
			a.free[i].size = left
		} else if right > 0 {
			a.free[i] = voxelIndexInterval{end, right}
		} else {
			a.free = append(a.free[:i], a.free[i+1:]...)
		}
		return true
	}
	return false
}

// Selection and execution share this exact placement qualification. Late demand
// may use spare space only after excluding every other pending virtual span.
func (m *GpuBufferManager) packedUploadLease(sector *volume.Sector, mask uint64) (uint32, uint32, bool) {
	state := m.SectorToInfo[sector].packed
	limit := uint64(^uint32(0))
	if m.voxelNative != nil {
		limit = m.voxelNative.BufferSize(m.BrickTableBuf) / BrickRecordSize
	}
	if state.published && uint64(state.base)+uint64(state.capacity) > limit {
		return 0, 0, false
	}
	if state.published && state.mask == mask {
		return state.base, state.capacity, true
	}
	capacity := packedBrickCapacity(mask)
	trial := m.brickRanges.clone()
	if own, ok := m.plannedBrickRanges[sector]; ok && own.capacity >= capacity && trial.claim(own.base, capacity, limit) {
		return own.base, capacity, true
	}
	trial = m.brickRanges.clone()
	for other, reserved := range m.plannedBrickRanges {
		if other != sector && !trial.claim(reserved.base, reserved.capacity, limit) {
			return 0, 0, false
		}
	}
	base, ok := trial.alloc(capacity, limit)
	return base, capacity, ok
}
func (m *GpuBufferManager) packedUploadFits(w voxelUploadWork) bool {
	if !m.ensureBrickRecordRanges() {
		return false
	}
	sector := w.targetMap().Sectors[w.sectorCoordinate()]
	info := m.SectorToInfo[sector]
	if info.packed == nil {
		return uint64(info.BrickTableIndex)+64 <= m.brickLegacyPrefix
	}
	mask := info.packed.mask
	if w.kind == voxelUploadSector {
		mask = sector.BrickMask64
	}
	_, _, ok := m.packedUploadLease(sector, mask)
	return ok
}
func (m *GpuBufferManager) assignPackedUpload(s *voxelSectorUploadSnapshot) bool {
	state := s.info.packed
	if state == nil {
		return true
	}
	if state.published && state.mask == s.mask {
		return true
	}
	base, capacity, ok := m.packedUploadLease(s.sector, s.mask)
	if !ok {
		return false
	}
	limit := uint64(^uint32(0))
	if m.voxelNative != nil {
		limit = m.voxelNative.BufferSize(m.BrickTableBuf) / BrickRecordSize
	}
	if !m.brickRanges.claim(base, capacity, limit) {
		return false
	}
	if capacity != 0 {
		m.brickPackedLeased = true
	}
	s.info.BrickTableIndex = base
	s.newRange = true
	return true
}

// Receipts follow the captured physical write, including source edits during writes.
func (m *GpuBufferManager) publishPackedUpload(w voxelUploadWork, actor *ObjectGpuAllocation) {
	sector := w.targetMap().Sectors[w.sectorCoordinate()]
	info, ok := m.SectorToInfo[sector]
	if w.sectorSnapshot != nil {
		sector = w.sectorSnapshot.sector
		info, ok = m.SectorToInfo[sector]
	}
	if !ok || info.packed == nil {
		return
	}
	state := info.packed
	old := state.pointers
	if w.sectorSnapshot != nil {
		s := w.sectorSnapshot
		if info.SlotIndex != s.info.SlotIndex || info.packed != s.info.packed {
			return
		}
		if s.newRange && state.published {
			m.retireBrickRange(state.base, state.capacity)
		}
		state.base = s.info.BrickTableIndex
		state.capacity = packedBrickCapacity(s.mask)
		state.mask = s.mask
		state.published = true
		state.pointers = s.desired
		delete(m.plannedBrickRanges, sector)
		info.BrickTableIndex = state.base
		m.SectorToInfo[sector] = info
	} else {
		start, end := w.brickRange()
		for i := start; i < end; i++ {
			state.pointers[i] = actor.Bricks[w.sectorCoordinate()][i]
		}
	}
	touched := map[*ObjectGpuAllocation]bool{actor: true}
	for owner := range state.owners {
		alloc := m.Allocations[owner.xbm]
		if alloc == nil || alloc.Sectors[owner.key] != sector {
			delete(state.owners, owner)
			continue
		}
		rows := alloc.Bricks[owner.key]
		if rows == nil {
			continue
		}
		before := *rows
		*rows = state.pointers
		if m.ownsVoxelAllocation(owner.xbm, alloc) {
			for i, b := range before {
				if b == rows[i] {
					continue
				}
				if b != nil {
					decrementVoxelReference(m.voxelOwnership.bricks, b)
				}
				if rows[i] != nil {
					m.voxelOwnership.bricks[rows[i]]++
				}
			}
		}
		if !touched[alloc] {
			alloc.shadowUploadEpoch++
			touched[alloc] = true
		}
		m.refreshSectorLookupInventory(owner.xbm, owner.key)
		m.markRetainedVoxelMapAccountingDirty(owner.xbm)
	}
	for _, brick := range old {
		if brick != nil && !m.voxelBrickReferenced(brick) {
			m.releaseBrickSlot(brick)
			m.releaseVoxelAuxSlot(brick)
		}
	}
}

func (m *GpuBufferManager) promotePackedDirtySectors(scene *core.Scene) {
	if scene == nil {
		return
	}
	seen := map[*volume.XBrickMap]bool{}
	for _, target := range m.voxelServiceTargets(scene) {
		xbm := target.mapRef
		if seen[xbm] {
			continue
		}
		seen[xbm] = true
		for key, dirty := range xbm.DirtyBricks {
			if !dirty {
				continue
			}
			sk := [3]int{key[0], key[1], key[2]}
			sector := xbm.Sectors[sk]
			if sector == nil {
				continue
			}
			info := m.SectorToInfo[sector]
			if info.packed != nil && (!info.packed.published || info.packed.mask != sector.BrickMask64) {
				xbm.DirtySectors[sk] = true
			}
		}
	}
}
func (p *voxelAdmissionPlan) reservePackedMap(m *GpuBufferManager, xbm *volume.XBrickMap) {
	keys := map[[3]int]bool{}
	alloc := m.Allocations[xbm]
	if managed := m.managedGPUMaps[xbm]; managed != nil {
		for _, key := range managed.activeFrontier() {
			keys[key] = true
		}
	} else if alloc == nil || xbm.StructureDirty {
		for key := range xbm.Sectors {
			keys[key] = true
		}
	}
	for key, dirty := range xbm.DirtySectors {
		if dirty {
			keys[key] = true
		}
	}
	for key, dirty := range xbm.DirtyBricks {
		if dirty {
			keys[[3]int{key[0], key[1], key[2]}] = true
		}
	}
	ordered := make([][3]int, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		for a := 0; a < 3; a++ {
			if ordered[i][a] != ordered[j][a] {
				return ordered[i][a] < ordered[j][a]
			}
		}
		return false
	})
	for _, key := range ordered {
		sector := xbm.Sectors[key]
		if sector == nil {
			continue
		}
		if _, reserved := p.packedReservations[sector]; reserved {
			continue
		}
		info, exists := m.SectorToInfo[sector]
		if exists && info.packed == nil {
			continue
		}
		if exists && info.packed.published && info.packed.mask == sector.BrickMask64 {
			continue
		}
		p.packedReservationKeys = append(p.packedReservationKeys, sector)
		capacity := packedBrickCapacity(sector.BrickMask64)
		base, ok := p.recordRanges.alloc(capacity, uint64(^uint32(0)))
		p.packedReservations[sector] = plannedBrickRange{base, capacity, sector.BrickMask64}
		if !ok {
			p.recordRangeInvalid = true
		}
	}
}
