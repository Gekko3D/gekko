package gpu

import (
	"errors"
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// VoxelGPUAdmissionBudget limits optional physical voxel resource growth.
// Zero disables the soft cap. Existing and required owners may expose pressure.
type VoxelGPUAdmissionBudget struct{ MaxBytes uint64 }

type VoxelGPUAdmissionStats struct {
	CurrentBufferBytes, StagingBytes, RetiredBufferBytes, AtlasBytes, TotalBytes, MaxBytes, PressureBytes uint64
	DeferredMaps, HardLimitDeferredMaps                                                                   int
	AllocationFailures                                                                                    uint64
	LastError                                                                                             string
}

// A transaction snapshot uses bytes, never logical assigned-slot accounting.
// A successful backend publishes all resources together and charges replacements.
type voxelGPUResources struct {
	SectorTable, BrickTable, Auxiliary, Material, SectorGrid, DirectLookup, SectorGridParams uint64
	AtlasBytes, StagingBytes, RetiredBytes, MaxBufferBytes, MaxStorageBytes, MaxUniformBytes uint64
}

func (m *GpuBufferManager) SetVoxelGPUAdmissionBudget(budget VoxelGPUAdmissionBudget) {
	if m != nil {
		m.voxelGPUAdmissionBudget = budget
	}
}
func (m *GpuBufferManager) VoxelGPUAdmissionStats() VoxelGPUAdmissionStats {
	if m == nil {
		return VoxelGPUAdmissionStats{}
	}
	return m.voxelGPUAdmissionStats
}
func voxelResourceSizes(r voxelGPUResources) [7]uint64 {
	return [7]uint64{r.SectorTable, r.BrickTable, r.Auxiliary, r.Material, r.SectorGrid, r.DirectLookup, r.SectorGridParams}
}
func voxelResourceBufferBytes(r voxelGPUResources) uint64 {
	var n uint64
	for _, size := range voxelResourceSizes(r) {
		n = addRetainedVoxelBytes(n, size)
	}
	return n
}
func voxelResourceTotalBytes(r voxelGPUResources) uint64 {
	return addRetainedVoxelBytes(addRetainedVoxelBytes(addRetainedVoxelBytes(voxelResourceBufferBytes(r), r.StagingBytes), r.RetiredBytes), r.AtlasBytes)
}
func voxelResourcePeakBytes(current, next voxelGPUResources) uint64 {
	n := voxelResourceTotalBytes(current)
	old := voxelResourceSizes(current)
	for i, size := range voxelResourceSizes(next) {
		if size > old[i] {
			n = addRetainedVoxelBytes(n, size)
		}
	}
	if next.AtlasBytes > current.AtlasBytes {
		n = addRetainedVoxelBytes(n, next.AtlasBytes-current.AtlasBytes)
	}
	return n
}
func voxelResourcesGrow(current, next voxelGPUResources) bool {
	old := voxelResourceSizes(current)
	for i, size := range voxelResourceSizes(next) {
		if size > old[i] {
			return true
		}
	}
	return next.AtlasBytes > current.AtlasBytes
}
func voxelMul(a, b uint64) uint64 {
	if b != 0 && a > ^uint64(0)/b {
		return ^uint64(0)
	}
	return a * b
}
func voxelAlign(size uint64) uint64 {
	if size < 256 {
		return 256
	}
	if size > ^uint64(0)-255 {
		return ^uint64(0)
	}
	return (size + 255) &^ uint64(255)
}

// Use geometric growth only when legal. Slack never makes fitting content fail
// a device limit. A zero limit is a mandatory limit, not an unlimited sentinel.
func voxelCapacity(current, required, preferred, limit uint64) (uint64, bool) {
	minimum := voxelAlign(required)
	if minimum > limit {
		return current, false
	}
	if current >= minimum {
		return current, current <= limit
	}
	desired := voxelAlign(preferred)
	if desired > limit {
		desired = minimum
	}
	growth := voxelAlign(addRetainedVoxelBytes(current, current/2))
	if growth > desired && growth <= limit {
		desired = growth
	}
	return desired, true
}

func (m *GpuBufferManager) voxelMapAdmitted(xbm *volume.XBrickMap) bool {
	return !m.voxelAdmissionActive || m.voxelAdmissionMaps[xbm]
}
func (m *GpuBufferManager) voxelObjectAdmitted(obj *core.VoxelObject) bool {
	return !m.voxelAdmissionActive || m.voxelAdmissionObjects[obj]
}

// Cleanup belongs to the same admission boundary as slot reuse. Retention's
// physical capacities do not shrink; freeing inactive owners only supplies slots.
func (m *GpuBufferManager) cleanupVoxelAdmissionOwners(scene *core.Scene) map[*volume.XBrickMap]bool {
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	m.ensureRetainedVoxelMaps()
	activeMaps := make(map[*volume.XBrickMap]bool)
	activeObjects := make(map[*core.VoxelObject]bool)
	if scene != nil {
		for _, obj := range scene.Objects {
			if obj != nil {
				activeObjects[obj] = true
			}
		}
	}
	for _, target := range m.voxelServiceTargets(scene) {
		activeMaps[target.mapRef] = true
	}
	m.evictRetainedVoxelMaps(activeMaps)
	for xbm, alloc := range m.Allocations {
		if !activeMaps[xbm] && m.managedGPUMaps[xbm] == nil {
			if _, retain := m.retainedVoxelMaps[xbm]; !retain {
				m.releaseVoxelMapAllocation(xbm, alloc)
			}
		}
	}
	for obj := range m.MaterialAllocations {
		if !activeObjects[obj] {
			m.releaseMaterialAllocation(obj)
		}
	}
	for obj := range m.managedMaterialObjects {
		if !activeObjects[obj] {
			delete(m.managedMaterialObjects, obj)
		}
	}
	return activeMaps
}

type voxelAdmissionCandidate struct {
	target   voxelServiceTarget
	index    int
	required bool
}

type voxelAdmissionPlan struct {
	auxiliaryWordRanges            voxelIndexRanges
	auxiliaryWordRangeInvalid      bool
	auxiliaryPacketReservations    map[auxiliaryPacketLocation]auxiliaryPacketLease
	auxiliaryPacketReservationKeys []auxiliaryPacketLocation

	auxiliaryDemand                     *auxiliaryDemandInventory
	auxiliaryTail, auxiliaryFree        uint64
	auxiliaryReserved                   map[*volume.Brick]bool
	auxiliaryReservationJournal         []*volume.Brick
	auxiliaryRemovedReferences          map[*volume.Brick]int
	auxiliaryReferenceJournal           []auxiliaryReferenceChange
	auxiliaryReleased                   map[*volume.Brick]bool
	auxiliaryReleaseJournal             []*volume.Brick
	materialKeys                        map[string]bool
	materialKeyJournal                  []string
	recordRanges                        brickRecordRanges
	recordRangeInvalid                  bool
	packedReservations                  map[*volume.Sector]plannedBrickRange
	packedReservationKeys               []*volume.Sector
	sectorTail, brickTail, materialTail uint64
	sectorFree, brickFree, materialFree uint64
	sectors                             map[*volume.Sector]bool
	maps                                map[*volume.XBrickMap]bool
	objects                             map[*core.VoxelObject]bool
	hashSectors, directCells            uint64
	sectorKeys                          []*volume.Sector
	mapKeys                             []*volume.XBrickMap
	objectKeys                          []*core.VoxelObject
	lookup                              map[*volume.XBrickMap]bool
	lookupKeys                          []*volume.XBrickMap
	removedSectors                      map[*volume.Sector]bool
	removedKeys                         []*volume.Sector
}

// Candidate trials share admitted sets and journal only their new entries.
// Refusal rolls back deltas; no resident-sector set is cloned or enumerated.
func (p *voxelAdmissionPlan) clone() voxelAdmissionPlan {
	b := *p
	b.recordRanges = p.recordRanges.clone()
	b.auxiliaryWordRanges = p.auxiliaryWordRanges.clone()
	return b
}
func (p *voxelAdmissionPlan) rollback(previous voxelAdmissionPlan) {
	for _, location := range p.auxiliaryPacketReservationKeys[len(previous.auxiliaryPacketReservationKeys):] {
		delete(p.auxiliaryPacketReservations, location)
	}

	for _, sector := range p.packedReservationKeys[len(previous.packedReservationKeys):] {
		delete(p.packedReservations, sector)
	}
	for _, brick := range p.auxiliaryReservationJournal[len(previous.auxiliaryReservationJournal):] {
		delete(p.auxiliaryReserved, brick)
	}
	for _, brick := range p.auxiliaryReleaseJournal[len(previous.auxiliaryReleaseJournal):] {
		delete(p.auxiliaryReleased, brick)
	}
	for i := len(p.auxiliaryReferenceJournal) - 1; i >= len(previous.auxiliaryReferenceJournal); i-- {
		change := p.auxiliaryReferenceJournal[i]
		if change.existed {
			p.auxiliaryRemovedReferences[change.brick] = change.previous
		} else {
			delete(p.auxiliaryRemovedReferences, change.brick)
		}
	}
	for _, key := range p.materialKeyJournal[len(previous.materialKeyJournal):] {
		delete(p.materialKeys, key)
	}
	for _, xbm := range p.lookupKeys[len(previous.lookupKeys):] {
		delete(p.lookup, xbm)
	}
	for _, sector := range p.removedKeys[len(previous.removedKeys):] {
		delete(p.removedSectors, sector)
	}
	for _, sector := range p.sectorKeys[len(previous.sectorKeys):] {
		delete(p.sectors, sector)
	}
	for _, xbm := range p.mapKeys[len(previous.mapKeys):] {
		delete(p.maps, xbm)
	}
	for _, obj := range p.objectKeys[len(previous.objectKeys):] {
		delete(p.objects, obj)
	}
}
func allocationDirectCells(alloc *ObjectGpuAllocation) uint64 {
	if !alloc.directCellsValid {
		alloc.directCells = directSectorLookupCells(alloc.Sectors)
		alloc.directCellsValid = true
	}
	return alloc.directCells
}
func consumeVoxelSlots(tail, free *uint64, count uint64) {
	reused := min(*free, count)
	*free -= reused
	*tail = addRetainedVoxelBytes(*tail, count-reused)
}
func directSectorLookupCells(sectors map[[3]int]*volume.Sector) uint64 {
	if len(sectors) == 0 {
		return 0
	}
	first := true
	var lo, hi [3]int64
	for key := range sectors {
		for axis := 0; axis < 3; axis++ {
			value := int64(int32(key[axis]))
			if first || value < lo[axis] {
				lo[axis] = value
			}
			if first || value > hi[axis] {
				hi[axis] = value
			}
		}
		first = false
	}
	cells := uint64(1)
	for axis := 0; axis < 3; axis++ {
		cells = voxelMul(cells, uint64(hi[axis]-lo[axis]+1))
	}
	if cells > DirectSectorLookupMaxCells || cells > voxelMul(uint64(len(sectors)), DirectSectorLookupDensityMax) {
		return 0
	}
	return cells
}
func (p *voxelAdmissionPlan) addMap(m *GpuBufferManager, xbm *volume.XBrickMap) {
	if p.maps[xbm] {
		return
	}
	p.maps[xbm] = true
	p.mapKeys = append(p.mapKeys, xbm)
	alloc := m.Allocations[xbm]
	managed := m.managedGPUMaps[xbm]
	sectors := xbm.Sectors
	if alloc != nil && !xbm.StructureDirty {
		sectors = alloc.Sectors
	}
	if alloc != nil && p.lookup[xbm] {
		p.hashSectors -= uint64(len(alloc.Sectors))
		p.directCells -= allocationDirectCells(alloc)
	}
	if !p.lookup[xbm] {
		p.lookup[xbm] = true
		p.lookupKeys = append(p.lookupKeys, xbm)
	}
	sectorCount := uint64(len(sectors))
	if managed != nil {
		// Managed desired topology is a sampling map. Only accepted coordinates
		// and this bounded frontier can consume physical lookup/sector resources.
		sectorCount = uint64(len(alloc.Sectors))
		for _, key := range managed.activeFrontier() {
			if sector := xbm.Sectors[key]; sector != nil && alloc.Sectors[key] == nil {
				sectorCount++
			}
		}
		// Managed direct demand is cached from bounded target construction;
		// admission never scans its private sampling topology.
	}
	p.hashSectors = addRetainedVoxelBytes(p.hashSectors, sectorCount)
	cells := uint64(0)
	if managed != nil {
		cells = allocationDirectCells(alloc)
	} else {
		if alloc != nil && !xbm.StructureDirty {
			cells = allocationDirectCells(alloc)
		} else {
			cells = directSectorLookupCells(sectors)
		}
	}
	p.directCells = addRetainedVoxelBytes(p.directCells, cells)
	var newSectors uint64
	reserveSector := func(sector *volume.Sector) {
		m.VoxelCapacityPlanningSectorVisitsLastUpdate++
		if sector != nil && !p.sectors[sector] {
			// Existing mappings still need a reservation: a later replacement may
			// remove their final old snapshot while this incoming owner needs them.
			p.sectors[sector] = true
			p.sectorKeys = append(p.sectorKeys, sector)
			if _, exists := m.SectorToInfo[sector]; exists && !p.removedSectors[sector] {
				return
			}
			newSectors++
		}
	}
	if managed != nil {
		for _, key := range managed.activeFrontier() {
			reserveSector(xbm.Sectors[key])
		}
	} else if alloc == nil || xbm.StructureDirty {
		for _, sector := range sectors {
			reserveSector(sector)
		}
	}
	// Structural preparation removes old snapshots before allocation. Model
	// only slots whose final references disappear in this exact candidate.
	if managed == nil && alloc != nil && xbm.StructureDirty {
		removed := make(map[*volume.Sector]bool)
		for key, sector := range alloc.Sectors {
			if sectors[key] != sector {
				removed[sector] = true
			}
		}
		for sector := range removed {
			if !m.voxelSectorSurvivesStructure(xbm, sectors, sector) && !p.removedSectors[sector] && !p.sectors[sector] {
				p.removedSectors[sector] = true
				p.removedKeys = append(p.removedKeys, sector)
				p.sectorFree++
				if m.SectorToInfo[sector].packed == nil {
					p.brickFree++
				}
			}
		}
	}
	consumeVoxelSlots(&p.sectorTail, &p.sectorFree, newSectors)
	p.reservePackedMap(m, xbm)
	if m.usesVoxelAuxiliaryPackets() {
		p.reservePackedAuxiliaryMap(m, xbm)
	} else {
		p.addAuxiliary(m, xbm)
	}
}
func (p *voxelAdmissionPlan) addMaterial(m *GpuBufferManager, obj *core.VoxelObject) {
	if p.objects[obj] {
		return
	}
	p.objects[obj] = true
	p.objectKeys = append(p.objectKeys, obj)
	alloc := m.MaterialAllocations[obj]
	table := obj.ImmutableMaterialTable()
	replacing := alloc != nil && !materialAttachmentMatches(alloc, table)
	// Shared abandonment is credited once globally, before candidate trials.
	// Private slots retain their per-object replacement accounting.
	if replacing && alloc.MaterialCapacity > 0 && alloc.block == nil {
		p.materialFree++
	}
	if table != nil {
		key := table.Identity()
		if p.materialKeys[key] || m.materialBlocks[key] != nil {
			return
		}
		p.materialKeys[key] = true
		p.materialKeyJournal = append(p.materialKeyJournal, key)
		consumeVoxelSlots(&p.materialTail, &p.materialFree, 1)
		return
	}
	if alloc == nil || replacing || alloc.MaterialCapacity < uint32(materialUploadRows(len(obj.MaterialTable))) {
		if !replacing && alloc != nil && alloc.MaterialCapacity > 0 {
			p.materialFree++
		}
		consumeVoxelSlots(&p.materialTail, &p.materialFree, 1)
	}
}
func voxelHashGridSize(sectors uint64) uint64 {
	if sectors == 0 {
		return 0
	}
	size := uint64(1024)
	need := voxelMul(sectors, 8)
	for size < need {
		if size > ^uint64(0)/2 {
			return ^uint64(0)
		}
		size *= 2
	}
	return size
}
func (p *voxelAdmissionPlan) resources(m *GpuBufferManager, current voxelGPUResources) (voxelGPUResources, bool) {
	next := current
	storageLimit := min(current.MaxBufferBytes, current.MaxStorageBytes)
	uniformLimit := min(current.MaxBufferBytes, current.MaxUniformBytes)
	// Owned paths reserve distinct actual demand while retaining allocator high
	// water. Unknown public allocation headers keep the conservative legacy bound.
	auxRows := p.auxiliaryTail
	if p.auxiliaryDemand == nil || p.auxiliaryDemand.legacy {
		auxRows = max(voxelMul(p.brickTail, 64), uint64(m.VoxelAuxAlloc.Tail))
	}
	sizes := [7]uint64{voxelMul(p.sectorTail, 32), voxelMul(max(voxelMul(p.brickTail, 64), p.recordRanges.tail), BrickRecordSize), voxelMul(auxRows, VoxelAuxRecordBytes), voxelMul(p.materialTail, materialBlockCapacity*64), voxelMul(voxelHashGridSize(p.hashSectors), 32), voxelMul(p.directCells, 4), 16}
	if m.usesVoxelAuxiliaryPackets() {
		sizes[2] = voxelMul(p.auxiliaryWordRanges.tail, 4)
	}
	fields := []*uint64{&next.SectorTable, &next.BrickTable, &next.Auxiliary, &next.Material, &next.SectorGrid, &next.DirectLookup, &next.SectorGridParams}
	for i, dest := range fields {
		limit := storageLimit
		if i == 6 {
			limit = uniformLimit
		}
		preferred := sizes[i]
		switch i {
		case 0:
			preferred = voxelMul(addRetainedVoxelBytes(p.sectorTail, 512), 32)
		}
		// Packet words require four-byte alignment; an existing bound pool can
		// satisfy exact demand without rounding its capacity up for a new allocation.
		if i == 2 && m.usesVoxelAuxiliaryPackets() && *dest >= sizes[i] && *dest <= limit {
			continue
		}
		value, ok := voxelCapacity(*dest, sizes[i], preferred, limit)
		if !ok {
			return current, false
		}
		*dest = value
	}
	// Shader indices and allocators remain 32-bit even on large devices.
	if p.auxiliaryWordRangeInvalid || (m.usesVoxelAuxiliaryPackets() && p.auxiliaryWordRanges.tail > uint64(^uint32(0))) || p.recordRangeInvalid || p.recordRanges.tail > uint64(^uint32(0)) || p.sectorTail > uint64(^uint32(0)) || p.brickTail > uint64(^uint32(0))/64 || p.materialTail > uint64(^uint32(0))/(materialBlockCapacity*4) || auxRows > uint64(^uint32(0))/volume.VoxelAuxWordCount || p.directCells > uint64(^uint32(0)) || voxelHashGridSize(p.hashSectors) > uint64(^uint32(0)) {
		return current, false
	}
	return next, true
}

// prepareVoxelGPUAdmission is shared by native and headless execution. Backend
// refusal cannot publish geometry/material ownership or acknowledge dirty work.
func (m *GpuBufferManager) prepareVoxelGPUAdmission(scene *core.Scene, resources *voxelGPUResources, grow func(voxelGPUResources) error) bool {
	if m == nil || resources == nil {
		return false
	}
	m.beginVoxelAdmissionFrame()
	return m.prepareVoxelGPUAdmissionCurrentFrame(scene, resources, grow)
}

func (m *GpuBufferManager) prepareVoxelGPUAdmissionCurrentFrame(scene *core.Scene, resources *voxelGPUResources, grow func(voxelGPUResources) error) bool {
	active := m.cleanupVoxelAdmissionOwners(scene)
	wasActive := m.voxelAdmissionActive
	previousLookup := m.voxelLookupMaps
	m.voxelAdmissionActive = true
	m.VoxelCapacityPlanningSectorVisitsLastUpdate = 0
	candidates := make([]voxelAdmissionCandidate, 0)
	requiredMaps := make(map[*volume.XBrickMap]bool)
	for index, target := range m.voxelServiceTargets(scene) {
		required := m.Allocations[target.mapRef] != nil || (target.pendingGeneration == 0 && !target.object.VoxelGPUAdmissionOptional)
		if managed := m.managedGPUMaps[target.mapRef]; managed != nil {
			// A private structural header is CPU ownership, not required GPU
			// residency. Preserve current content independently of its successor.
			required = managed.content || target.mapRef == target.object.RenderVoxelMap()
		}
		if required {
			requiredMaps[target.mapRef] = true
		}
		candidates = append(candidates, voxelAdmissionCandidate{target, index, required})
	}
	for i := range candidates {
		candidates[i].required = requiredMaps[candidates[i].target.mapRef]
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return voxelAdmissionRawLess(candidates[i], candidates[j])
	})
	// Geometry priority and its first required material owner are independent.
	// An optional alias cannot advance fresh required geometry past a refused
	// first required material allocation.
	firstRequiredMaterial := make(map[*volume.XBrickMap]*core.VoxelObject)
	representatives := make(map[*volume.XBrickMap]voxelAdmissionCandidate)
	for _, candidate := range candidates {
		target := candidate.target
		if _, exists := representatives[target.mapRef]; !exists {
			representatives[target.mapRef] = candidate
		}
		if target.pendingGeneration == 0 && voxelAdmissionMaterialRequired(m, target.object) && firstRequiredMaterial[target.mapRef] == nil {
			firstRequiredMaterial[target.mapRef] = target.object
		}
	}
	queue, ages := m.voxelAdmissionSchedule(candidates)

	desiredMaterialKeys := make(map[string]bool)
	if scene != nil {
		for _, obj := range scene.Objects {
			if obj != nil {
				if table := obj.ImmutableMaterialTable(); table != nil {
					desiredMaterialKeys[table.Identity()] = true
				}
			}
		}
	}
	for _, candidate := range candidates {
		if table := candidate.target.object.ImmutableMaterialTable(); table != nil {
			desiredMaterialKeys[table.Identity()] = true
		}
	}
	abandonedMaterialBlocks := make(map[*materialGPUBlock]bool)
	for key, block := range m.materialBlocks {
		if !desiredMaterialKeys[key] {
			abandonedMaterialBlocks[block] = true
		}
	}
	abandonmentAllowed := true
	abandonedCredits := uint64(len(abandonedMaterialBlocks))
	ordinaryMaterialFree := uint64(len(m.MaterialAlloc.Free))
	rangesValid := m.ensureBrickRecordRanges()
	auxiliaryValid := true
	if m.usesVoxelAuxiliaryPackets() {
		auxiliaryValid = m.ensureAuxiliaryWordRanges()
	}
	initial := voxelAdmissionPlan{auxiliaryWordRanges: m.auxiliaryRanges.clone(), auxiliaryWordRangeInvalid: !auxiliaryValid, auxiliaryPacketReservations: make(map[auxiliaryPacketLocation]auxiliaryPacketLease), recordRanges: m.brickRanges.clone(), recordRangeInvalid: !rangesValid, packedReservations: make(map[*volume.Sector]plannedBrickRange), auxiliaryDemand: m.auxiliaryDemand(scene), auxiliaryTail: uint64(m.VoxelAuxAlloc.Tail), auxiliaryFree: uint64(len(m.VoxelAuxAlloc.Free)), auxiliaryReserved: make(map[*volume.Brick]bool), auxiliaryRemovedReferences: make(map[*volume.Brick]int), auxiliaryReleased: make(map[*volume.Brick]bool), materialKeys: make(map[string]bool), sectorTail: uint64(m.SectorAlloc.Tail), brickTail: uint64(m.BrickAlloc.Tail), materialTail: uint64(m.MaterialAlloc.Tail), sectorFree: uint64(len(m.SectorAlloc.Free)), brickFree: uint64(len(m.BrickAlloc.Free)), materialFree: uint64(len(m.MaterialAlloc.Free)), sectors: make(map[*volume.Sector]bool), maps: make(map[*volume.XBrickMap]bool), objects: make(map[*core.VoxelObject]bool), lookup: make(map[*volume.XBrickMap]bool), removedSectors: make(map[*volume.Sector]bool)}

	for xbm := range active {
		if alloc := m.Allocations[xbm]; alloc != nil && (previousLookup[xbm] || (!wasActive && m.Device == nil)) {
			initial.lookup[xbm] = true
			initial.hashSectors = addRetainedVoxelBytes(initial.hashSectors, uint64(len(alloc.Sectors)))
			initial.directCells = addRetainedVoxelBytes(initial.directCells, allocationDirectCells(alloc))
		}
	}
	current := *resources
	atlasBytes := current.AtlasBytes
	if m.Device != nil {
		atlasBytes = voxelMul(voxelMul(voxelMul(uint64(m.VoxelPayloadPageSize), uint64(m.VoxelPayloadPageSize)), uint64(m.VoxelPayloadPageSize)), uint64(m.VoxelPayloadPageCount))
	}
	denied, hard := make(map[*volume.XBrickMap]bool), make(map[*volume.XBrickMap]bool)
	plan := initial.clone()
	next := current
	build := func(allowGrowth bool) {
		initial.materialFree = ordinaryMaterialFree
		if abandonmentAllowed {
			initial.materialFree += abandonedCredits
		}
		clear(initial.packedReservations)
		clear(initial.auxiliaryPacketReservations)
		clear(initial.auxiliaryReserved)
		clear(initial.auxiliaryRemovedReferences)
		clear(initial.auxiliaryReleased)
		clear(initial.materialKeys)
		clear(initial.sectors)
		clear(initial.maps)
		clear(initial.objects)
		clear(initial.lookup)
		clear(initial.removedSectors)
		for xbm := range active {
			if m.Allocations[xbm] != nil && (previousLookup[xbm] || (!wasActive && m.Device == nil)) {
				initial.lookup[xbm] = true
			}
		}
		plan = initial.clone()
		next = current
		next.AtlasBytes = atlasBytes
		clear(denied)
		clear(hard)
		admitMap := func(xbm *volume.XBrickMap) bool {
			if plan.maps[xbm] {
				return true
			}
			// Preserve the raw representative's joint owner even when another
			// live alias supplied the best aged scheduling key for this map.
			candidate := representatives[xbm]
			target := candidate.target
			obj := target.object
			trial := plan.clone()
			trial.addMap(m, xbm)
			var jointObject *core.VoxelObject
			if m.Allocations[xbm] == nil {
				jointObject = firstRequiredMaterial[xbm]
			}
			if jointObject == nil && !candidate.required && target.pendingGeneration == 0 {
				jointObject = obj
			}
			if jointObject != nil {
				trial.addMaterial(m, jointObject)
			}
			desired, legal := trial.resources(m, current)
			desired.AtlasBytes = atlasBytes
			if !legal {
				trial.rollback(plan)
				denied[xbm] = true
				hard[xbm] = true
				return false
			}
			growth := voxelResourcesGrow(next, desired)
			if (!allowGrowth && voxelResourcesGrow(current, desired)) || (!candidate.required && growth && m.voxelGPUAdmissionBudget.MaxBytes != 0 && voxelResourcePeakBytes(current, desired) > m.voxelGPUAdmissionBudget.MaxBytes) {
				trial.rollback(plan)
				denied[xbm] = true
				return false
			}
			plan, next = trial, desired
			delete(denied, xbm)
			delete(hard, xbm)
			return true
		}
		for _, work := range queue {
			target := work.candidate.target
			xbm, obj := target.mapRef, target.object
			if !admitMap(xbm) {
				continue
			}
			if work.material && !plan.objects[obj] {
				trial := plan.clone()
				trial.addMaterial(m, obj)
				desired, legal := trial.resources(m, current)
				desired.AtlasBytes = atlasBytes
				materialRequired := voxelAdmissionMaterialRequired(m, obj)
				if !legal {
					trial.rollback(plan)
					denied[xbm], hard[xbm] = true, true
					continue
				}
				// Use the committed baseline for zero-growth decisions: growth already
				// approved for another owner cannot give an optional material a free ride.
				materialGrowth := desired.Material > next.Material
				if (!allowGrowth && voxelResourcesGrow(current, desired)) || (!materialRequired && materialGrowth && m.voxelGPUAdmissionBudget.MaxBytes != 0 && voxelResourcePeakBytes(current, desired) > m.voxelGPUAdmissionBudget.MaxBytes) {
					trial.rollback(plan)
					denied[xbm] = true
					continue
				}
				plan, next = trial, desired
			}
		}
	}
	build(true)
	recreated := false
	if voxelResourcesGrow(current, next) {
		if grow == nil {
			build(false)
		} else if err := grow(next); err != nil {
			if errors.Is(err, errVoxelGPUStagePublished) {
				// Physical publication never commits the earlier logical plan.
				// Replan live demand without advancing its waiting clock twice.
				m.prepareVoxelGPUAdmissionCurrentFrame(scene, resources, grow)
				return true
			}
			*resources = current
			abandonmentAllowed = false
			if !errors.Is(err, errVoxelGPUWorkPending) {
				m.recordVoxelGPUAllocationFailure(err)
			}
			build(false)
		} else {
			recreated = true
		}
	}
	// A failed physical transaction cannot publish speculative abandonment,
	// including replacements that might otherwise reuse a different resident key.
	if !abandonmentAllowed {
		for obj := range plan.objects {
			if a := m.MaterialAllocations[obj]; a != nil && abandonedMaterialBlocks[a.block] && !materialAttachmentMatches(a, obj.ImmutableMaterialTable()) {
				delete(plan.objects, obj)
				denied[obj.RenderVoxelMap()] = true
			}
		}
	}

	m.finishVoxelAdmissionAges(ages, plan)
	m.plannedBrickRanges = plan.packedReservations
	m.plannedAuxiliaryRanges = plan.auxiliaryPacketReservations
	m.voxelAdmissionMaps, m.voxelAdmissionObjects = plan.maps, plan.objects
	m.voxelLookupMaps = plan.lookup
	for xbm, alloc := range m.Allocations {
		alloc.lookupAdmissionKnown = true
		alloc.lookupAdmitted = plan.lookup[xbm]
	}
	m.prepareVoxelStructureDirtyState(scene)
	// Material slots are prepared now, after all physical resources succeed.
	if m.MaterialAllocations == nil {
		m.MaterialAllocations = make(map[*core.VoxelObject]*MaterialGpuAllocation)
	}
	var materialObjects []*core.VoxelObject
	for _, candidate := range candidates {
		if plan.objects[candidate.target.object] {
			materialObjects = append(materialObjects, candidate.target.object)
		}
	}
	// Retire every stale attachment before reusing any credited physical slot.
	// No admitted demand, or demand served by ordinary free slots, needs this
	// global retirement. A fully refused plan preserves bindings; partial reuse
	// also removes denied stale attachments so they cannot alias new content.
	if abandonmentAllowed && len(plan.objects) > 0 && plan.materialFree < abandonedCredits {
		for block := range abandonedMaterialBlocks {
			for obj := range block.attachments {
				m.releaseMaterialAllocation(obj)
			}
		}
	}

	m.prepareMaterialAttachments(materialObjects)
	m.refreshVoxelGPUAdmissionStats(*resources)
	stats := m.voxelGPUAdmissionStats
	stats.DeferredMaps = len(denied)
	stats.HardLimitDeferredMaps = len(hard)
	m.voxelGPUAdmissionStats = stats
	return recreated
}

func (m *GpuBufferManager) recordVoxelGPUAllocationFailure(err error) {
	m.voxelGPUAdmissionStats.AllocationFailures = addRetainedVoxelBytes(m.voxelGPUAdmissionStats.AllocationFailures, 1)
	message := err.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	m.voxelGPUAdmissionStats.LastError = message
}

func (m *GpuBufferManager) refreshVoxelGPUAdmissionStats(resources voxelGPUResources) {
	stats := m.voxelGPUAdmissionStats
	stats.CurrentBufferBytes = voxelResourceBufferBytes(resources)
	stats.StagingBytes = resources.StagingBytes
	stats.RetiredBufferBytes = resources.RetiredBytes
	stats.AtlasBytes = resources.AtlasBytes
	stats.TotalBytes = voxelResourceTotalBytes(resources)
	stats.MaxBytes = m.voxelGPUAdmissionBudget.MaxBytes
	stats.PressureBytes = 0
	if stats.MaxBytes != 0 && stats.TotalBytes > stats.MaxBytes {
		stats.PressureBytes = stats.TotalBytes - stats.MaxBytes
	}
	m.voxelGPUAdmissionStats = stats
}

// Final reference scan is limited to structural removals, not idle preparation.
func (m *GpuBufferManager) voxelSectorSurvivesStructure(owner *volume.XBrickMap, replacement map[[3]int]*volume.Sector, sector *volume.Sector) bool {
	for xbm, alloc := range m.Allocations {
		sectors := alloc.Sectors
		if xbm == owner {
			sectors = replacement
		}
		for _, reference := range sectors {
			if reference == sector {
				return true
			}
		}
	}
	return false
}
