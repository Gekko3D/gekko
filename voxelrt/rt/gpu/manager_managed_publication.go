package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
	"iter"
	"unsafe"
)

type ManagedGeometryFrameBudget struct {
	Enabled    bool
	MaxEntries uint32
}

func DefaultManagedGeometryFrameBudget() ManagedGeometryFrameBudget {
	return ManagedGeometryFrameBudget{true, 16}
}

type ManagedGeometryFrameStats struct {
	AttemptedEntries uint32
	Pending          bool
}
type ManagedGeometryGPUStatus struct {
	CurrentInput, StageInput           core.ManagedGeometryInput
	CurrentGeneration, StageGeneration uint64
	StageReady, Pending                bool
	RetiringEntries                    uint64
}

type managedCoordinateCharge struct{ input, copies uint64 }
type managedOccupiedBounds struct {
	minimum, maximum mgl32.Vec3
	occupied         bool
}
type managedCoordinateRecord struct {
	coordinate [3]int
	sector     *volume.Sector
	pointers   *[64]*volume.Brick
	charge     managedCoordinateCharge
	uploaded   *volume.Sector
	desired    *volume.Sector
	pending    bool
}

// Targets retain their own input and desired/uploaded snapshot charges. CPU
// admission cancellation cannot detach these roots or erase their ledger.
type managedGPUTarget struct {
	input                     core.ManagedGeometryInput
	mapRef                    *volume.XBrickMap
	root                      *managedGeometryStageNode
	generation                uint64
	cursor, prepared          int
	coords                    [][3]int
	minimum, maximum          mgl32.Vec3
	inputBytes, bytes         uint64
	metadata                  uint64
	rootBytes                 uint64
	bakeMinimum, bakeMaximum  mgl32.Vec3
	bakeContextValid          bool
	rebakeLimit               int
	occupiedBounds            []managedOccupiedBounds
	desired                   *volume.XBrickMap
	charges                   map[[3]int]managedCoordinateCharge
	uploaded                  map[[3]int]*volume.Sector
	syncCursor                int
	syncGeneration            uint64
	syncPending               bool
	retirement                *managedGPUTarget
	retirementBuilding        bool
	complete, ready, retiring bool
	retireCursor              int
	content                   bool
	changed                   map[[3]int]bool
	pending                   [][3]int
	frontierCount             int
	frontierAllowed           bool
	holdUntil                 uint64
}
type managedGPUOwner struct {
	object                    *core.VoxelObject
	current, stage, candidate *managedGPUTarget
	retired                   []*managedGPUTarget
	selection                 core.ManagedGeometryInput
	initialInput              core.ManagedGeometryInput
	initialInputBytes         uint64
	lastConsidered            uint64
	pending                   bool
	handoff                   bool
	journal                   [1024][3]int
	count                     int
	sweep, again              bool
	cursor                    int
	pass                      uint64
	covered                   uint64
	floor                     uint64
	repair                    bool
	metadata                  uint64
	retiringEntries           uint64
}

func (m *GpuBufferManager) SetManagedGeometryFrameBudget(b ManagedGeometryFrameBudget) {
	if m != nil {
		m.managedFrameBudget = b
		if b.Enabled && b.MaxEntries > 0 {
			m.managedDrain = b.MaxEntries
		}
	}
}
func (m *GpuBufferManager) ManagedGeometryFrameBudget() ManagedGeometryFrameBudget {
	if m == nil {
		return ManagedGeometryFrameBudget{}
	}
	return m.managedFrameBudget
}
func (m *GpuBufferManager) ManagedGeometryFrameStats() ManagedGeometryFrameStats {
	if m == nil {
		return ManagedGeometryFrameStats{}
	}
	return m.managedFrameStats
}
func (m *GpuBufferManager) ManagedGeometryGPUStatus(obj *core.VoxelObject) (ManagedGeometryGPUStatus, bool) {
	if m == nil {
		return ManagedGeometryGPUStatus{}, false
	}
	o := m.managedGPUOwners[obj]
	if o == nil {
		return ManagedGeometryGPUStatus{}, false
	}
	s := ManagedGeometryGPUStatus{Pending: o.pending}
	if o.current != nil {
		s.CurrentInput = o.current.input
		s.CurrentGeneration = o.current.generation
	}
	if o.stage != nil {
		s.StageInput = o.stage.input
		s.StageGeneration = o.stage.generation
		s.StageReady = o.stage.ready
	}
	s.RetiringEntries = o.retiringEntries
	return s, true
}
func (m *GpuBufferManager) managedCharge(input, copies uint64) bool {
	st := &m.managedGeometryStats
	b := m.managedGeometryBudget
	i, ok := managedGeometryAdd(st.InputBytes, input)
	if !ok {
		return false
	}
	c, ok := managedGeometryAdd(st.TotalStageBytes, copies)
	if !ok || !b.Enabled || i > b.MaxInputBytes || c > b.MaxCopiedStageBytes {
		return false
	}
	st.InputBytes = i
	st.ReservedCopiedStageBytes += copies
	st.TotalStageBytes = c
	return true
}
func (m *GpuBufferManager) managedMetadata(bytes uint64) bool {
	if !m.managedCharge(0, bytes) {
		return false
	}
	m.managedGeometryStats.ReservedCopiedStageBytes -= bytes
	m.managedGeometryStats.OwnedMetadataBytes += bytes
	return true
}
func (m *GpuBufferManager) managedUncharge(t *managedGPUTarget) {
	st := &m.managedGeometryStats
	st.InputBytes -= t.inputBytes
	st.ReservedCopiedStageBytes -= t.bytes
	st.OwnedMetadataBytes -= t.metadata
	st.TotalStageBytes -= t.bytes + t.metadata
	t.occupiedBounds = nil
}
func (m *GpuBufferManager) attachManagedTarget(t *managedGPUTarget) {
	if m.managedGPUMaps == nil {
		m.managedGPUMaps = make(map[*volume.XBrickMap]*managedGPUTarget)
	}
	if m.Allocations == nil {
		m.Allocations = make(map[*volume.XBrickMap]*ObjectGpuAllocation)
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	m.managedGPUMaps[t.mapRef] = t
	a := &ObjectGpuAllocation{Sectors: make(map[[3]int]*volume.Sector), Bricks: make(map[[3]int]*[64]*volume.Brick), DirectLookup: defaultDirectSectorLookupMetadata(), directCellsValid: true}
	m.Allocations[t.mapRef] = a
	m.trackVoxelAllocation(t.mapRef, a)
}
func managedTargetMetadata(n int) (uint64, bool) {
	records, ok := managedGeometryMultiply(uint64(n), uint64(unsafe.Sizeof(managedCoordinateRecord{}))+uint64(unsafe.Sizeof([64]*volume.Brick{})))
	if !ok {
		return 0, false
	}
	tree, ok := ManagedGeometryStageStorageBytes(n)
	if !ok {
		return 0, false
	}
	total, ok := managedGeometryAdd(records, tree)
	if !ok {
		return 0, false
	}
	return managedGeometryAdd(total, uint64(unsafe.Sizeof(managedGPUTarget{}))+2*uint64(unsafe.Sizeof(volume.XBrickMap{}))+uint64(unsafe.Sizeof(ObjectGpuAllocation{})))
}

// Occupied bounds belong to normal baking; display and direct lookup keep
// their separately accumulated conservative coordinate bounds.
func managedOccupiedStorage(n int) (int, uint64, bool) {
	count, ok := managedGeometryMultiply(uint64(n), 4)
	if !ok || count > uint64(^uint(0)>>1) {
		return 0, 0, false
	}
	bytes, ok := managedGeometryMultiply(count, uint64(unsafe.Sizeof(managedOccupiedBounds{})))
	return int(count), bytes, ok
}
func (t *managedGPUTarget) updateOccupied(index, n int, c [3]int, sector *volume.Sector) {
	if n == 0 {
		return
	}
	leaf := managedOccupiedBounds{}
	if sector != nil {
		local := volume.XBrickMap{Sectors: map[[3]int]*volume.Sector{c: sector}, AABBDirty: true}
		leaf.minimum, leaf.maximum = local.ComputeAABB()
		leaf.occupied = leaf.minimum != leaf.maximum
	}
	var update func(int, int, int)
	update = func(node, lo, hi int) {
		if hi-lo == 1 {
			t.occupiedBounds[node] = leaf
			return
		}
		mid := lo + (hi-lo)/2
		if index < mid {
			update(node*2+1, lo, mid)
		} else {
			update(node*2+2, mid, hi)
		}
		a, b := t.occupiedBounds[node*2+1], t.occupiedBounds[node*2+2]
		if !a.occupied {
			t.occupiedBounds[node] = b
			return
		}
		if !b.occupied {
			t.occupiedBounds[node] = a
			return
		}
		for axis := 0; axis < 3; axis++ {
			a.minimum[axis] = min(a.minimum[axis], b.minimum[axis])
			a.maximum[axis] = max(a.maximum[axis], b.maximum[axis])
		}
		t.occupiedBounds[node] = a
	}
	update(0, 0, n)
}
func (t *managedGPUTarget) cacheOccupied(target *volume.XBrickMap) {
	bounds := managedOccupiedBounds{}
	if len(t.occupiedBounds) > 0 {
		bounds = t.occupiedBounds[0]
	}
	target.CachedMin, target.CachedMax = bounds.minimum, bounds.maximum
	target.AABBDirty = false
}
func (t *managedGPUTarget) occupiedOrdinal(c [3]int) int {
	lo, hi := 0, len(t.coords)
	for lo < hi {
		mid := lo + (hi-lo)/2
		key := t.coords[mid]
		less := key[0] < c[0] || key[0] == c[0] && (key[1] < c[1] || key[1] == c[1] && key[2] < c[2])
		if less {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
func (m *GpuBufferManager) newManagedTarget(input core.ManagedGeometryInput, root *managedGeometryStageNode, generation uint64, copiedBytes uint64) *managedGPUTarget {
	n := input.Geometry().Len()
	metadata, ok := managedTargetMetadata(n)
	if !ok {
		return nil
	}
	boundCount, boundBytes, boundsOK := managedOccupiedStorage(n)
	metadata, ok = managedGeometryAdd(metadata, boundBytes)
	if !ok || !boundsOK {
		return nil
	}
	copies, ok := managedGeometryMultiply(copiedBytes, 3)
	if !ok {
		return nil
	}
	if !m.managedCharge(input.Geometry().RetainedBytes(), copies) {
		return nil
	}
	if !m.managedMetadata(metadata) {
		m.managedGeometryStats.InputBytes -= input.Geometry().RetainedBytes()
		m.managedGeometryStats.ReservedCopiedStageBytes -= copies
		m.managedGeometryStats.TotalStageBytes -= copies
		return nil
	}
	t := &managedGPUTarget{input: input, root: root, occupiedBounds: make([]managedOccupiedBounds, boundCount), generation: generation, mapRef: volume.NewXBrickMap(), desired: volume.NewXBrickMap(), coords: make([][3]int, 0, n), bytes: copies, rootBytes: copiedBytes, metadata: metadata, inputBytes: input.Geometry().RetainedBytes(), changed: make(map[[3]int]bool), charges: make(map[[3]int]managedCoordinateCharge), uploaded: make(map[[3]int]*volume.Sector)}
	t.mapRef.StructureDirty = false
	t.desired.StructureDirty = false
	m.attachManagedTarget(t)
	return t
}
func (m *GpuBufferManager) newManagedContent(o *managedGPUOwner, g uint64) *managedGPUTarget {
	metadata, _ := managedTargetMetadata(0)
	metadata *= 2
	if !m.managedMetadata(metadata) {
		return nil
	}
	t := &managedGPUTarget{input: o.current.input, generation: g, mapRef: o.current.desired, complete: true, content: true, metadata: metadata, changed: make(map[[3]int]bool), charges: make(map[[3]int]managedCoordinateCharge), uploaded: make(map[[3]int]*volume.Sector), minimum: o.current.minimum, maximum: o.current.maximum}
	m.attachManagedTarget(t)
	m.cacheManagedDirect(t)
	return t
}
func (m *GpuBufferManager) cacheManagedDirect(t *managedGPUTarget) {
	a := m.Allocations[t.mapRef]
	if a == nil {
		return
	}
	cells := uint64(1)
	for axis := 0; axis < 3; axis++ {
		cells = voxelMul(cells, uint64((t.maximum[axis]-t.minimum[axis])/32))
	}
	if cells > DirectSectorLookupMaxCells || cells > voxelMul(uint64(len(t.mapRef.Sectors)), DirectSectorLookupDensityMax) {
		cells = 0
	}
	a.directCells = cells
	a.directCellsValid = true
}
func (m *GpuBufferManager) retireManaged(o *managedGPUOwner, t *managedGPUTarget) {
	if t != nil && !t.retiring {
		if t.retirement != nil {
			t.retirement.retirementBuilding = false
		}
		t.retiring = true
		t.ready = false
		o.retired = append(o.retired, t)
		o.retiringEntries += uint64(len(t.coords) - t.retireCursor)
	}
}
func (m *GpuBufferManager) releaseManagedCoordinate(t *managedGPUTarget, c [3]int) {
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	a := m.Allocations[t.mapRef]
	if a == nil {
		return
	}
	sector := a.Sectors[c]
	p := a.Bricks[c]
	delete(a.Sectors, c)
	delete(a.Bricks, c)
	delete(t.mapRef.Sectors, c)
	delete(t.uploaded, c)
	if t.desired != nil {
		delete(t.desired.Sectors, c)
	}
	charge := t.charges[c]
	delete(t.charges, c)
	t.inputBytes -= charge.input
	t.bytes -= charge.copies
	m.managedGeometryStats.InputBytes -= charge.input
	m.managedGeometryStats.ReservedCopiedStageBytes -= charge.copies
	m.managedGeometryStats.TotalStageBytes -= charge.copies
	if sector != nil {
		if info := m.SectorToInfo[sector]; info.packed != nil {
			delete(info.packed.owners, packedSectorOwner{t.mapRef, c})
		}
		m.removeVoxelSnapshotEdges(sector, p)
		m.releaseUnreferencedSector(sector)
		m.releaseUnreferencedBricks(p)
		m.sectorTopologyRevision++
	}
}
func (m *GpuBufferManager) drainManaged(o *managedGPUOwner, left *uint32) {
	for len(o.retired) > 0 {
		t := o.retired[0]
		if t.holdUntil > 0 && o.current != nil && !o.current.retiring && o.current.generation < t.holdUntil {
			return
		}
		if t.retireCursor < len(t.coords) {
			if *left == 0 {
				return
			}
			c := t.coords[t.retireCursor]
			m.releaseManagedCoordinate(t, c)
			t.retireCursor++
			o.retiringEntries--
			*left--
			m.managedFrameStats.AttemptedEntries++
			continue
		}
		if t.retirementBuilding {
			return
		}
		m.beginVoxelOwnership()
		delete(m.Allocations, t.mapRef)
		delete(m.voxelOwnership.headers, t.mapRef)
		delete(m.managedGPUMaps, t.mapRef)
		m.endVoxelOwnership()
		m.managedUncharge(t)
		o.retired = o.retired[1:]
	}
}
func (m *GpuBufferManager) buildManagedTarget(t *managedGPUTarget, left *uint32) {
	n := t.input.Geometry().Len()
	for t.cursor < n && *left > 0 {
		i := t.cursor
		c, _ := t.input.Geometry().Coord(i)
		leaf := managedGeometryLeaf(t.root, 0, n, i)
		if leaf == nil {
			return
		}
		sector := leaf.sector
		t.coords = append(t.coords, c)
		t.updateOccupied(i, n, c, sector)
		if leaf := managedGeometryLeaf(t.root, 0, n, i); leaf != nil {
			t.charges[c] = managedCoordinateCharge{copies: leaf.bytes * 2}
		}
		if sector != nil {
			t.mapRef.Sectors[c] = sector
			t.desired.Sectors[c] = sector
		}
		for axis := 0; axis < 3; axis++ {
			lo := float32(c[axis]) * 32
			hi := lo + 32
			if i == 0 || lo < t.minimum[axis] {
				t.minimum[axis] = lo
			}
			if i == 0 || hi > t.maximum[axis] {
				t.maximum[axis] = hi
			}
		}
		t.cursor++
		*left--
		m.managedFrameStats.AttemptedEntries++
	}
	if t.cursor == n {
		t.complete = true
		t.cacheOccupied(t.mapRef)
		t.cacheOccupied(t.desired)
		m.cacheManagedDirect(t)
	}
}

// Only the bounded coordinate frontier is exposed to native admission. Desired
// neighbor topology is already complete in the private sampling map.
func (m *GpuBufferManager) prepareManagedCoordinates(o *managedGPUOwner, t *managedGPUTarget, left *uint32) {
	if !t.complete {
		return
	}
	if len(t.pending) > 0 {
		n := min(len(t.pending), int(*left))
		*left -= uint32(n)
		m.managedFrameStats.AttemptedEntries += uint32(n)
		t.frontierCount = n
		t.frontierAllowed = n > 0
		return
	}
	if len(t.mapRef.DirtySectors)+len(t.mapRef.DirtyBricks) > 0 {
		return
	}
	for t.prepared < len(t.coords) && *left > 0 {
		*left--
		m.managedFrameStats.AttemptedEntries++
		i := t.prepared
		c := t.coords[i]
		if !t.content && i < t.rebakeLimit {
			leaf := managedGeometryLeaf(t.root, 0, t.input.Geometry().Len(), i)
			if leaf == nil {
				return
			}
			if !m.replaceManagedStageCoordinate(o, t, i, leaf.sector, leaf.bytes, true) {
				return
			}
		}
		if !t.content && !t.bakeContextValid {
			t.bakeMinimum, t.bakeMaximum = t.mapRef.ComputeAABB()
			t.bakeContextValid = true
		}
		t.prepared++
		if t.content && !t.changed[c] {
			continue
		}
		if t.mapRef.Sectors[c] != nil {
			t.pending = append(t.pending, c)
			t.frontierAllowed = true
			t.frontierCount = len(t.pending)
		}
	}
}
func (m *GpuBufferManager) installManagedFrontier(t *managedGPUTarget) {
	if !t.complete || !t.frontierAllowed || !m.voxelMapAdmitted(t.mapRef) {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	a := m.Allocations[t.mapRef]
	for _, c := range t.activeFrontier() {
		sector := t.mapRef.Sectors[c]
		if sector == nil || a.Sectors[c] == sector {
			continue
		}
		info, ok := m.SectorToInfo[sector]
		if !ok {
			info = SectorGpuInfo{pending: true, SlotIndex: m.SectorAlloc.Alloc(), packed: &packedSectorRange{owners: make(map[packedSectorOwner]bool)}}
			m.SectorToInfo[sector] = info
		}
		a.Sectors[c] = sector
		a.Bricks[c] = new([64]*volume.Brick)
		if info.packed != nil {
			info.packed.owners[packedSectorOwner{t.mapRef, c}] = true
		}
		if !m.voxelOwnership.legacy {
			m.voxelOwnership.sectors[sector]++
		}
		t.mapRef.DirtySectors[c] = true
		m.sectorTopologyRevision++
	}
	t.pending = t.pending[t.frontierCount:]
	t.frontierCount = 0
	t.frontierAllowed = false
	m.cacheManagedDirect(t)
}
func (m *GpuBufferManager) managedTargetCurrent(t *managedGPUTarget, obj *core.VoxelObject) bool {
	if t == nil || t.retiring || !t.complete || m.managedFrameBudget.Enabled && m.managedFrameBudget.MaxEntries == 0 {
		return false
	}
	o := m.managedGPUOwners[obj]
	if o == nil || o.stage != t && o.candidate != t && o.current != t {
		return false
	}
	if o.current == t {
		return obj.RenderVoxelMap() == t.mapRef
	}
	if o.handoff || !m.managedFrameBudget.Enabled || !m.managedGeometryBudget.Enabled {
		return false
	}
	g, ok := obj.CurrentManagedGeometryGeneration(t.input)
	return ok && g == t.generation
}
func (m *GpuBufferManager) managedReady(o *managedGPUOwner, t *managedGPUTarget) bool {
	if t.syncPending || !m.managedTargetCurrent(t, o.object) || t.prepared < len(t.coords) || len(t.pending) != 0 || len(t.mapRef.DirtySectors)+len(t.mapRef.DirtyBricks) != 0 || !materialBindingReady(o.object, m.MaterialAllocations[o.object], m.MaterialBufferGeneration) {
		return false
	}
	a := m.Allocations[t.mapRef]
	if a == nil {
		return false
	}
	// Prepared frontier and successful unit acknowledgements certify coverage;
	// dirty emptiness by itself cannot certify unallocated coordinates.
	if !t.content && len(a.Sectors) != len(t.mapRef.Sectors) {
		return false
	}
	if len(t.uploaded) != len(a.Sectors) {
		return false
	}
	if t.content && (o.sweep || o.count > 0) {
		return false
	}
	return true
}
func (m *GpuBufferManager) postManagedUploads() {
	for _, o := range m.managedGPUOwners {
		if o.stage != nil {
			o.stage.ready = m.managedReady(o, o.stage)
		}
		if o.candidate != nil {
			o.candidate.ready = m.managedReady(o, o.candidate)
		}
	}
}
func (m *GpuBufferManager) commitManagedContent(o *managedGPUOwner, left *uint32) {
	t := o.candidate
	if t == nil || !t.ready || !m.managedReady(o, t) {
		return
	}
	b := m.VoxelUploadBudget()
	if b.MaxSectors == 0 || b.MaxBytes == 0 {
		return
	}
	if t.retirement == nil {
		t.retirement = &managedGPUTarget{input: t.input, mapRef: volume.NewXBrickMap(), charges: make(map[[3]int]managedCoordinateCharge)}
		t.retirement.mapRef.StructureDirty = false
		m.attachManagedTarget(t.retirement)
		retirementMetadata, _ := managedTargetMetadata(0)
		t.retirement.metadata = retirementMetadata
		t.metadata -= t.retirement.metadata
		t.retirement.retirementBuilding = true
		m.retireManaged(o, t.retirement)
	}
	r := t.retirement
	for t.retireCursor < len(t.coords) && *left > 0 {
		c := t.coords[t.retireCursor]
		t.retireCursor++
		*left--
		m.managedFrameStats.AttemptedEntries++
		m.beginVoxelOwnership()
		oldA := m.Allocations[o.current.mapRef]
		newA := m.Allocations[t.mapRef]
		retA := m.Allocations[r.mapRef]
		old, oldP := oldA.Sectors[c], oldA.Bricks[c]
		incoming, incomingP := newA.Sectors[c], newA.Bricks[c]
		if info := m.SectorToInfo[old]; info.packed != nil {
			delete(info.packed.owners, packedSectorOwner{o.current.mapRef, c})
			info.packed.owners[packedSectorOwner{r.mapRef, c}] = true
		}
		if info := m.SectorToInfo[incoming]; info.packed != nil {
			delete(info.packed.owners, packedSectorOwner{t.mapRef, c})
			info.packed.owners[packedSectorOwner{o.current.mapRef, c}] = true
		}
		if old != nil {
			retA.Sectors[c] = old
			retA.Bricks[c] = oldP
			r.mapRef.Sectors[c] = old
		}
		delete(newA.Sectors, c)
		delete(newA.Bricks, c)
		delete(t.uploaded, c)
		if incoming != nil {
			oldA.Sectors[c] = incoming
			oldA.Bricks[c] = incomingP
			o.current.mapRef.Sectors[c] = incoming
			o.current.uploaded[c] = incoming
		} else {
			delete(oldA.Sectors, c)
			delete(oldA.Bricks, c)
			delete(o.current.mapRef.Sectors, c)
			delete(o.current.uploaded, c)
		}
		oldCharge := o.current.charges[c]
		newCharge := t.charges[c]
		retiredMetadata := uint64(unsafe.Sizeof(managedCoordinateRecord{})) + uint64(unsafe.Sizeof([64]*volume.Brick{}))
		t.metadata -= retiredMetadata
		r.metadata += retiredMetadata
		r.charges[c] = oldCharge
		r.inputBytes += oldCharge.input
		r.bytes += oldCharge.copies
		r.coords = append(r.coords, c)
		o.retiringEntries++
		o.current.inputBytes += newCharge.input
		o.current.inputBytes -= oldCharge.input
		o.current.bytes += newCharge.copies
		o.current.bytes -= oldCharge.copies
		o.current.charges[c] = newCharge
		t.inputBytes -= newCharge.input
		t.bytes -= newCharge.copies
		m.cacheManagedDirect(o.current)
		oldA.shadowUploadEpoch++
		m.sectorTopologyRevision++
		m.VoxelUploadRevision++
		m.endVoxelOwnership()
	}
	if t.retireCursor == len(t.coords) {
		o.current.cacheOccupied(o.current.mapRef)
		o.current.generation = t.generation
		o.covered = t.generation
		o.repair = false
		m.beginVoxelOwnership()
		delete(m.Allocations, t.mapRef)
		delete(m.voxelOwnership.headers, t.mapRef)
		delete(m.managedGPUMaps, t.mapRef)
		m.endVoxelOwnership()
		r.retirementBuilding = false
		m.managedUncharge(t)
		m.retireManaged(o, r)
		o.candidate = nil
	}
}
func (m *GpuBufferManager) NotifyManagedGeometryGPUContent(obj *core.VoxelObject, previous, current uint64, writes []volume.VoxelWrite) {
	if m == nil {
		return
	}
	o := m.managedGPUOwners[obj]
	if o == nil {
		return
	}
	if cpu := m.managedGeometryOwner(obj); cpu != nil {
		m.RecordManagedGeometryContentPublication(obj, cpu.accepted.input, previous, current)
		for _, w := range writes {
			for c := range managedWriteHalo(w) {
				m.QueueManagedGeometryContent(obj, cpu.accepted.input, c)
			}
		}
	}
	if o.current == nil {
		return
	}
	g, ok := obj.CurrentManagedGeometryGeneration(o.current.input)
	if !ok || g != current || current <= previous || previous != o.covered {
		o.repair = true
		if !o.sweep {
			o.requestSweep()
		}
	} else {
		o.covered = current
	}
	if current > o.floor {
		o.floor = current
	}
	for _, w := range writes {
		for c := range managedWriteHalo(w) {
			if !managedGeometryContentContains(o.current.input, c) {
				continue
			}
			found := false
			for i := 0; i < o.count; i++ {
				if o.journal[i] == c {
					found = true
					break
				}
			}
			if found {
				continue
			}
			if o.count == len(o.journal) {
				o.requestSweep()
				continue
			}
			o.journal[o.count] = c
			o.count++
		}
	}
	o.pending = true
}
func (o *managedGPUOwner) requestSweep() {
	o.count = 0
	if !o.sweep {
		o.sweep = true
		o.cursor = 0
		o.pass = 0
	} else if o.cursor > 0 {
		o.again = true
	}
}
func (m *GpuBufferManager) serviceManagedCurrent(o *managedGPUOwner, g uint64, left *uint32) {
	if o.current == nil {
		return
	}
	if g < o.floor || g != o.covered {
		o.repair = true
		if !o.sweep {
			o.requestSweep()
		}
	}
	if g > o.floor {
		o.floor = g
	}
	if o.candidate != nil && o.candidate.generation != g {
		m.detachManagedCandidate(o)
		m.retireManaged(o, o.candidate)
		o.candidate = nil
		if o.sweep {
			o.again = true
		}
	}
	if !o.sweep && o.count == 0 {
		if o.candidate != nil {
			m.prepareManagedCoordinates(o, o.candidate, left)
		} else if g == o.covered && !o.repair {
			o.current.generation = g
		}
		return
	}
	if o.candidate == nil {
		o.candidate = m.newManagedContent(o, g)
		if o.candidate == nil {
			return
		}
	}
	t := o.candidate
	n := len(o.current.coords)
	for *left > 0 && (o.sweep || o.count > 0) {
		var c [3]int
		if o.sweep {
			if n == 0 {
				o.sweep = false
				break
			}
			c = o.current.coords[o.cursor]
			if o.pass == 0 {
				o.pass = g
			}
			if o.pass != g {
				o.again = true
			}
		} else {
			c = o.journal[o.count-1]
		}
		*left--
		m.managedFrameStats.AttemptedEntries++
		in, ok := o.object.CaptureManagedGeometrySector(o.current.input, c)
		if !ok || in.Generation() != g {
			return
		}
		observed, qualified := o.object.CurrentManagedGeometryGeneration(o.current.input)
		if !qualified || observed != g {
			return
		}
		// Charge copied replacement and its retained frozen record independently.
		copyBytes := in.Sector().CopyBytes()
		doubled, valid := managedGeometryMultiply(copyBytes, 2)
		if !valid {
			return
		}
		retain := in.Sector().RetainedBytes()
		if !m.managedFrameBudget.Enabled || m.managedFrameBudget.MaxEntries == 0 || m.managedGPUOwners[o.object] != o || o.candidate != t {
			return
		}
		record := uint64(0)
		if !t.changed[c] {
			record = 2*uint64(unsafe.Sizeof(managedCoordinateRecord{})) + uint64(unsafe.Sizeof([64]*volume.Brick{}))
			if !m.managedMetadata(record) {
				return
			}
		}
		if !m.managedCharge(retain, doubled) {
			m.managedGeometryStats.OwnedMetadataBytes -= record
			m.managedGeometryStats.TotalStageBytes -= record
			return
		}
		sector, present := in.Sector().CopySector()
		if !present && in.Sector().Present() {
			m.managedGeometryStats.InputBytes -= retain
			m.managedGeometryStats.ReservedCopiedStageBytes -= doubled
			m.managedGeometryStats.TotalStageBytes -= doubled
			m.managedGeometryStats.OwnedMetadataBytes -= record
			m.managedGeometryStats.TotalStageBytes -= record
			return
		}

		o.current.updateOccupied(o.current.occupiedOrdinal(c), n, c, sector)
		o.current.cacheOccupied(t.mapRef)
		t.inputBytes += retain
		t.bytes += doubled
		if sector == nil {
			delete(t.mapRef.Sectors, c)
		} else {
			t.mapRef.Sectors[c] = sector
		}
		if !t.changed[c] {
			t.metadata += record
			t.coords = append(t.coords, c)
		}
		previousCharge := t.charges[c]
		m.managedGeometryStats.InputBytes -= previousCharge.input
		m.managedGeometryStats.ReservedCopiedStageBytes -= previousCharge.copies
		m.managedGeometryStats.TotalStageBytes -= previousCharge.copies
		t.inputBytes -= previousCharge.input
		t.bytes -= previousCharge.copies
		t.charges[c] = managedCoordinateCharge{retain, doubled}
		t.changed[c] = true
		if o.sweep {
			o.cursor++
			if o.cursor == n {
				o.cursor = 0
				if o.again {
					o.again = false
					o.pass = 0
				} else {
					o.sweep = false
					o.covered = g
					o.repair = false
				}
			}
		} else {
			o.count--
		}
	}
	if !o.sweep && o.count == 0 {
		m.prepareManagedCoordinates(o, t, left)
	}
}
func (m *GpuBufferManager) PrepareManagedGeometryFrame(scene *core.Scene) {
	if m == nil {
		return
	}
	previousPending := m.managedFrameStats.Pending
	m.managedFrameStats = ManagedGeometryFrameStats{}
	for _, t := range m.managedGPUMaps {
		t.frontierAllowed = false
	}
	b := m.managedFrameBudget
	if b.Enabled && b.MaxEntries == 0 {
		m.managedFrameStats.Pending = previousPending
		return
	}
	if len(m.managedGPUOwners) == 0 && (!b.Enabled || !m.managedGeometryBudget.Enabled) {
		return
	}
	left := b.MaxEntries
	if !b.Enabled {
		left = m.managedDrain
		if left == 0 {
			left = 16
		}
	}
	if m.managedGPUOwners == nil {
		m.managedGPUOwners = make(map[*core.VoxelObject]*managedGPUOwner)
	}
	live := make(map[*core.VoxelObject]bool)
	if scene != nil {
		for _, obj := range scene.Objects {
			if obj != nil {
				live[obj] = true
			}
		}
	}
	for obj, o := range m.managedGPUOwners {
		if !live[obj] {
			m.retireManaged(o, o.current)
			m.retireManaged(o, o.stage)
			m.retireManaged(o, o.candidate)
			o.current = nil
			o.stage = nil
			o.candidate = nil
			m.CancelManagedGeometryInputs(obj)
			obj.ClearManagedRenderGeometry(o.selection)
			m.drainManaged(o, &left)
			if len(o.retired) == 0 {
				m.managedGeometryStats.InputBytes -= o.initialInputBytes
				m.managedGeometryStats.OwnedMetadataBytes -= o.metadata
				m.managedGeometryStats.TotalStageBytes -= o.metadata
				delete(m.managedGPUOwners, obj)
			}
			continue
		}
		expected := o.initialInput
		if o.current != nil {
			expected = o.current.input
		} else if o.stage != nil {
			expected = o.stage.input
		}
		g, qualified := obj.CurrentManagedGeometryGeneration(expected)
		o.handoff = !m.managedFrameBudget.Enabled || !m.managedGeometryBudget.Enabled || !qualified
		if o.handoff {
			m.retireManaged(o, o.stage)
			m.retireManaged(o, o.candidate)
			o.stage = nil
			o.candidate = nil
			m.CancelManagedGeometryInputs(obj)
			ready := obj.XBrickMap == nil || o.current != nil && obj.RenderVoxelMap() != o.current.mapRef
			if obj.XBrickMap != nil {
				ordinaryReady, _, _ := m.voxelTargetAllocationReady(obj, obj.XBrickMap, len(obj.XBrickMap.DirtySectors), len(obj.XBrickMap.DirtyBricks))
				ready = ready || ordinaryReady
			}
			if ready {
				obj.ClearManagedRenderGeometry(o.selection)
				m.retireManaged(o, o.current)
				o.current = nil
			}
			m.drainManaged(o, &left)
			o.pending = o.current != nil || len(o.retired) > 0
			if !o.pending {
				m.managedGeometryStats.InputBytes -= o.initialInputBytes
				m.managedGeometryStats.OwnedMetadataBytes -= o.metadata
				m.managedGeometryStats.TotalStageBytes -= o.metadata
				delete(m.managedGPUOwners, obj)
			}
			continue
		}
		if o.stage != nil && o.stage.ready && m.managedReady(o, o.stage) {
			t := o.stage
			if obj.SetManagedRenderGeometry(t.input, t.mapRef, t.minimum, t.maximum) {
				m.retireManaged(o, o.current)
				o.current = t
				t.root = nil
				t.bytes -= t.rootBytes
				m.managedGeometryStats.ReservedCopiedStageBytes -= t.rootBytes
				m.managedGeometryStats.TotalStageBytes -= t.rootBytes
				t.rootBytes = 0
				o.stage = nil
				o.selection = t.input
				o.covered = t.generation
				o.floor = t.generation
				o.count = 0
				o.sweep = false
				if cpu := m.managedGeometryOwner(obj); cpu != nil && cpu.successor != nil {
					m.AdvanceManagedGeometryInput(obj, cpu.accepted.input)
				}
			}
		}
		m.commitManagedContent(o, &left)
		m.serviceManagedCurrent(o, g, &left)
	}
	if b.Enabled && m.managedGeometryBudget.Enabled && scene != nil {
		for _, obj := range scene.Objects {
			if obj == nil {
				continue
			}
			o := m.managedGPUOwners[obj]
			if o != nil && o.handoff {
				continue
			}
			var input core.ManagedGeometryInput
			var ok bool
			if o == nil {
				input, ok = obj.CaptureManagedGeometryInput()
			} else {
				expected := o.initialInput
				if o.current != nil {
					expected = o.current.input
				} else if o.stage != nil {
					expected = o.stage.input
				} else if cpu := m.managedGeometryOwner(obj); cpu != nil {
					expected = cpu.accepted.input
				}
				g, qualified := obj.CurrentManagedGeometryGeneration(expected)
				if qualified && g == o.lastConsidered && (o.current != nil || o.stage != nil || m.managedGeometryOwner(obj) != nil) {
					ok = false
				} else {
					input, ok = obj.CaptureManagedGeometryInput()
				}
			}
			if ok && m.managedFrameBudget.Enabled && m.managedFrameBudget.MaxEntries > 0 && m.managedGeometryBudget.Enabled && obj.MatchesManagedGeometrySource(input) {
				if o == nil {
					metadata := uint64(unsafe.Sizeof(managedGPUOwner{}))
					retained := input.Geometry().RetainedBytes()
					if !m.managedCharge(retained, 0) {
						continue
					}
					if !m.managedMetadata(metadata) {
						m.managedGeometryStats.InputBytes -= retained
						continue
					}
					o = &managedGPUOwner{object: obj, metadata: metadata, initialInput: input, initialInputBytes: retained}
					m.managedGPUOwners[obj] = o
				}
				if o.current != nil && input.SameSource(o.current.input) && input.Geometry().SameTopology(o.current.input.Geometry()) {
					o.lastConsidered = input.Generation()
				} else {
					result := m.admitManagedGeometryInput(obj, input)
					if result == ManagedGeometryAdmissionAccepted || result == ManagedGeometryAdmissionCoalesced || result == ManagedGeometryAdmissionUnchanged {

						o.lastConsidered = input.Generation()

						if o.current == nil && !o.selection.SameSource(o.selection) {
							obj.SetManagedRenderGeometry(input, nil, mgl32.Vec3{}, mgl32.Vec3{})
							o.selection = input
						}
					}
				}
			}
			if o == nil {
				continue
			}
			cpu := m.managedGeometryOwner(obj)
			if cpu != nil && cpu.successor != nil && o.current != nil && cpu.accepted.input.SameSource(o.current.input) && cpu.accepted.input.Geometry().SameTopology(o.current.input.Geometry()) && o.stage == nil {
				m.AdvanceManagedGeometryInput(obj, cpu.accepted.input)
				cpu = m.managedGeometryOwner(obj)
			}
			if cpu != nil && (o.current == nil || !cpu.accepted.input.Geometry().SameTopology(o.current.input.Geometry())) {
				used := m.ServiceManagedGeometryReconciliation(obj, cpu.accepted.input, int(left))
				left -= uint32(used)
				m.managedFrameStats.AttemptedEntries += uint32(used)
				status, yes := m.ManagedGeometryReconciliationStatus(obj)
				if yes && status.Coherent && m.managedFrameBudget.Enabled && m.managedFrameBudget.MaxEntries > 0 && m.managedGeometryBudget.Enabled {
					if o.stage != nil && (o.stage.generation != status.Generation || o.stage.root != cpu.accepted.root) {
						m.syncManagedStage(o, o.stage, cpu.accepted, status.Generation, &left)
					}
					if o.stage == nil {
						o.stage = m.newManagedTarget(cpu.accepted.input, cpu.accepted.root, status.Generation, cpu.accepted.copiedBytes)
					}
					if o.stage != nil {
						m.buildManagedTarget(o.stage, &left)
						if !o.stage.syncPending {
							m.prepareManagedCoordinates(o, o.stage, &left)
						}
					}
				}
			}
		}
	}
	for _, o := range m.managedGPUOwners {
		m.drainManaged(o, &left)
		o.pending = o.stage != nil || o.candidate != nil || o.sweep || o.count > 0 || len(o.retired) > 0 || o.current == nil || o.lastConsidered > o.current.generation
		if o.current != nil {
			if g, ok := o.object.CurrentManagedGeometryGeneration(o.current.input); ok && g > o.lastConsidered {
				o.pending = true
			}
		}
		if cpu := m.managedGeometryOwner(o.object); cpu != nil && cpu.successor != nil {
			o.pending = true
		}
		m.managedFrameStats.Pending = m.managedFrameStats.Pending || o.pending
	}
}

func managedWriteHalo(w volume.VoxelWrite) iter.Seq[[3]int] {
	return func(yield func([3]int) bool) { volume.VisitVoxelNormalHaloSectors(w.X, w.Y, w.Z, yield) }
}

// Reconcile the already prepared finite topology without discarding its cursor
// or unchanged successful upload units. A moving scalar requests another stable
// pass; only changed immutable leaf pointers enter the native frontier.
func (m *GpuBufferManager) syncManagedStage(o *managedGPUOwner, t *managedGPUTarget, gen *managedGeometryGeneration, g uint64, left *uint32) {
	if !t.syncPending {
		t.syncPending = true
		t.syncCursor = 0
		t.syncGeneration = g
		t.ready = false
	}
	n := gen.input.Geometry().Len()
	for t.syncCursor < n && *left > 0 {
		i := t.syncCursor
		c, _ := t.input.Geometry().Coord(i)
		leaf := managedGeometryLeaf(gen.root, 0, n, i)
		if leaf == nil {
			return
		}
		*left--
		m.managedFrameStats.AttemptedEntries++
		previous := managedGeometryLeaf(t.root, 0, n, i)
		if previous == nil || previous.sector != leaf.sector {
			if !m.replaceManagedStageCoordinate(o, t, i, leaf.sector, leaf.bytes, false) {
				return
			}
			t.updateOccupied(i, n, c, leaf.sector)
			t.cacheOccupied(t.mapRef)
			t.cacheOccupied(t.desired)
			if leaf.sector != nil && i < t.prepared {
				t.pending = append(t.pending, c)
			}
		}
		t.syncCursor++
	}
	if t.syncCursor == n {
		if t.syncGeneration != g {
			t.syncCursor = 0
			t.syncGeneration = g
			return
		}
		if !m.managedCharge(0, gen.copiedBytes) {
			return
		}
		m.managedGeometryStats.ReservedCopiedStageBytes -= t.rootBytes
		m.managedGeometryStats.TotalStageBytes -= t.rootBytes
		t.bytes = t.bytes - t.rootBytes + gen.copiedBytes
		t.rootBytes = gen.copiedBytes
		minimum, maximum := t.mapRef.ComputeAABB()
		if t.bakeContextValid && (minimum != t.bakeMinimum || maximum != t.bakeMaximum) {
			t.rebakeLimit = max(t.rebakeLimit, t.prepared)
			t.prepared = 0
		}
		if t.bakeContextValid {
			t.bakeMinimum, t.bakeMaximum = minimum, maximum
		}
		t.syncPending = false
		t.root = gen.root
		t.generation = g
	}
}

// An obsolete queued candidate keeps its captured allocation header. Detach the
// reusable desired dictionary in constant time; payload charges for its desired
// roots stay with current until qualified replacement visits them.
func (m *GpuBufferManager) detachManagedCandidate(o *managedGPUOwner) {
	t := o.candidate
	if t == nil || o.current == nil {
		return
	}
	desired := volume.NewXBrickMap()
	desired.Sectors = t.mapRef.Sectors
	o.current.cacheOccupied(desired)
	desired.StructureDirty = false
	o.current.desired = desired
	t.mapRef.Sectors = nil
	t.holdUntil = o.floor
	o.repair = true
	o.requestSweep()
}

func (t *managedGPUTarget) activeFrontier() [][3]int {
	if !t.frontierAllowed {
		return nil
	}
	return t.pending[:t.frontierCount]
}

// Replace one hidden coordinate after reserving both the incoming payload and
// the independent retirement header. Fresh copies give rebakes new physical
// identities without changing their immutable CPU source leaves.
func (m *GpuBufferManager) replaceManagedStageCoordinate(o *managedGPUOwner, t *managedGPUTarget, i int, sector *volume.Sector, bytes uint64, fresh bool) bool {
	n := t.input.Geometry().Len()
	c, _ := t.input.Geometry().Coord(i)
	oldCharge := t.charges[c]
	if i >= t.cursor {
		if original := managedGeometryLeaf(t.root, 0, n, i); original != nil {
			oldCharge.copies = original.bytes * 2
		}
	}
	incoming, ok := managedGeometryMultiply(bytes, 2)
	if !ok {
		return false
	}
	oldA := m.Allocations[t.mapRef]
	old := oldA.Sectors[c]
	metadata := uint64(0)
	if old != nil {
		metadata, ok = managedTargetMetadata(1)
		if !ok {
			return false
		}
		if !m.managedMetadata(metadata) {
			return false
		}
	}
	if !m.managedCharge(0, incoming) {
		m.managedGeometryStats.OwnedMetadataBytes -= metadata
		m.managedGeometryStats.TotalStageBytes -= metadata
		return false
	}
	if fresh && sector != nil {
		copied := *sector
		copied.PackedBricks = make([]*volume.Brick, len(sector.PackedBricks))
		for index, source := range sector.PackedBricks {
			brick := *source
			if source.PrecomputedAux != nil {
				brick.PrecomputedAux = make([]byte, len(source.PrecomputedAux), cap(source.PrecomputedAux))
				copy(brick.PrecomputedAux, source.PrecomputedAux)
			}
			copied.PackedBricks[index] = &brick
		}
		sector = &copied
	}
	if old != nil {
		r := &managedGPUTarget{input: t.input, mapRef: volume.NewXBrickMap(), coords: [][3]int{c}, bytes: oldCharge.copies, metadata: metadata, charges: map[[3]int]managedCoordinateCharge{c: oldCharge}}
		r.mapRef.StructureDirty = false
		m.attachManagedTarget(r)
		m.beginVoxelOwnership()
		a := m.Allocations[r.mapRef]
		a.Sectors[c] = old
		a.Bricks[c] = oldA.Bricks[c]
		r.mapRef.Sectors[c] = old
		delete(oldA.Sectors, c)
		delete(oldA.Bricks, c)
		if info := m.SectorToInfo[old]; info.packed != nil {
			delete(info.packed.owners, packedSectorOwner{t.mapRef, c})
			info.packed.owners[packedSectorOwner{r.mapRef, c}] = true
		}
		m.endVoxelOwnership()
		m.retireManaged(o, r)
	} else {
		m.managedGeometryStats.ReservedCopiedStageBytes -= oldCharge.copies
		m.managedGeometryStats.TotalStageBytes -= oldCharge.copies
	}
	t.bytes = t.bytes - oldCharge.copies + incoming
	t.charges[c] = managedCoordinateCharge{copies: incoming}
	delete(t.uploaded, c)
	if sector == nil {
		delete(t.mapRef.Sectors, c)
		delete(t.desired.Sectors, c)
	} else {
		t.mapRef.Sectors[c] = sector
		t.desired.Sectors[c] = sector
	}
	m.sectorTopologyRevision++
	return true
}
