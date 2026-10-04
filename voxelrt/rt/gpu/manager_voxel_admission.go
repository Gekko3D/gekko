package gpu

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// VoxelGPUAdmissionBudget limits optional physical voxel resource growth.
// Zero disables the soft cap. Existing and required owners may expose pressure.
type VoxelGPUAdmissionBudget struct{ MaxBytes uint64 }

type VoxelGPUAdmissionStats struct {
	CurrentBufferBytes, RetiredBufferBytes, AtlasBytes, TotalBytes, MaxBytes, PressureBytes uint64
	DeferredMaps, HardLimitDeferredMaps                                                     int
	AllocationFailures                                                                      uint64
	LastError                                                                               string
}

// A transaction snapshot uses bytes, never logical assigned-slot accounting.
// A successful backend publishes all resources together and charges replacements.
type voxelGPUResources struct {
	SectorTable, BrickTable, Auxiliary, Material, SectorGrid, DirectLookup, SectorGridParams uint64
	AtlasBytes, RetiredBytes, MaxBufferBytes, MaxStorageBytes, MaxUniformBytes               uint64
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
	return addRetainedVoxelBytes(addRetainedVoxelBytes(voxelResourceBufferBytes(r), r.RetiredBytes), r.AtlasBytes)
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
	for _, target := range voxelServiceTargets(scene) {
		activeMaps[target.mapRef] = true
	}
	m.evictRetainedVoxelMaps(activeMaps)
	for xbm, alloc := range m.Allocations {
		if !activeMaps[xbm] {
			if _, retain := m.retainedVoxelMaps[xbm]; !retain {
				m.releaseVoxelMapAllocation(xbm, alloc)
			}
		}
	}
	for obj, alloc := range m.MaterialAllocations {
		if !activeObjects[obj] {
			if alloc != nil && alloc.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(alloc.MaterialOffset / materialBlockCapacity)
			}
			delete(m.MaterialAllocations, obj)
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
func (p *voxelAdmissionPlan) clone() voxelAdmissionPlan { return *p }
func (p *voxelAdmissionPlan) rollback(previous voxelAdmissionPlan) {
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
	p.hashSectors = addRetainedVoxelBytes(p.hashSectors, uint64(len(sectors)))
	cells := uint64(0)
	if alloc != nil && !xbm.StructureDirty {
		cells = allocationDirectCells(alloc)
	} else {
		cells = directSectorLookupCells(sectors)
	}
	p.directCells = addRetainedVoxelBytes(p.directCells, cells)
	var newSectors uint64
	if alloc == nil || xbm.StructureDirty {
		for _, sector := range sectors {
			m.VoxelCapacityPlanningSectorVisitsLastUpdate++
			if sector != nil && !p.sectors[sector] {
				// Existing mappings still need a reservation: a later replacement may
				// remove their final old snapshot while this incoming owner needs them.
				p.sectors[sector] = true
				p.sectorKeys = append(p.sectorKeys, sector)
				if _, exists := m.SectorToInfo[sector]; exists && !p.removedSectors[sector] {
					continue
				}
				newSectors++
			}
		}
	}
	// Structural preparation removes old snapshots before allocation. Model
	// only slots whose final references disappear in this exact candidate.
	if alloc != nil && xbm.StructureDirty {
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
				p.brickFree++
			}
		}
	}
	consumeVoxelSlots(&p.sectorTail, &p.sectorFree, newSectors)
	consumeVoxelSlots(&p.brickTail, &p.brickFree, newSectors)
}
func (p *voxelAdmissionPlan) addMaterial(m *GpuBufferManager, obj *core.VoxelObject) {
	if p.objects[obj] {
		return
	}
	p.objects[obj] = true
	p.objectKeys = append(p.objectKeys, obj)
	alloc := m.MaterialAllocations[obj]
	if alloc == nil || alloc.MaterialCapacity < uint32(materialUploadRows(len(obj.MaterialTable))) {
		if alloc != nil && alloc.MaterialCapacity > 0 {
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
	// Auxiliary records retain existing high-water capacity. Reserving all brick
	// rows keeps content service from allocating unplanned auxiliary slots later.
	auxRows := max(voxelMul(p.brickTail, 64), uint64(m.VoxelAuxAlloc.Tail))
	sizes := [7]uint64{voxelMul(p.sectorTail, 32), voxelMul(p.brickTail, 64*BrickRecordSize), voxelMul(auxRows, VoxelAuxRecordBytes), voxelMul(p.materialTail, materialBlockCapacity*64), voxelMul(voxelHashGridSize(p.hashSectors), 32), voxelMul(p.directCells, 4), 16}
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
		case 1:
			preferred = voxelMul(addRetainedVoxelBytes(voxelMul(p.brickTail, 64), 2048), BrickRecordSize)
		case 2:
			preferred = voxelMul(addRetainedVoxelBytes(auxRows, 2048), VoxelAuxRecordBytes)
		}
		value, ok := voxelCapacity(*dest, sizes[i], preferred, limit)
		if !ok {
			return current, false
		}
		*dest = value
	}
	// Shader indices and allocators remain 32-bit even on large devices.
	if p.sectorTail > uint64(^uint32(0)) || p.brickTail > uint64(^uint32(0))/64 || p.materialTail > uint64(^uint32(0))/(materialBlockCapacity*4) || auxRows > uint64(^uint32(0))/volume.VoxelAuxWordCount || p.directCells > uint64(^uint32(0)) || voxelHashGridSize(p.hashSectors) > uint64(^uint32(0)) {
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
	active := m.cleanupVoxelAdmissionOwners(scene)
	wasActive := m.voxelAdmissionActive
	previousLookup := m.voxelLookupMaps
	m.voxelAdmissionActive = true
	m.VoxelCapacityPlanningSectorVisitsLastUpdate = 0
	candidates := make([]voxelAdmissionCandidate, 0)
	requiredMaps := make(map[*volume.XBrickMap]bool)
	for index, target := range voxelServiceTargets(scene) {
		required := m.Allocations[target.mapRef] != nil || (target.pendingGeneration == 0 && !target.object.VoxelGPUAdmissionOptional)
		if required {
			requiredMaps[target.mapRef] = true
		}
		candidates = append(candidates, voxelAdmissionCandidate{target, index, required})
	}
	for i := range candidates {
		candidates[i].required = requiredMaps[candidates[i].target.mapRef]
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.required != b.required {
			return a.required
		}
		if a.target.object.VoxelUploadPriority != b.target.object.VoxelUploadPriority {
			return a.target.object.VoxelUploadPriority < b.target.object.VoxelUploadPriority
		}
		order := func(t voxelServiceTarget) uint64 {
			if t.object.VoxelUploadOrder != 0 {
				return t.object.VoxelUploadOrder
			}
			return uint64(t.mapRef.ID)
		}
		if order(a.target) != order(b.target) {
			return order(a.target) < order(b.target)
		}
		if a.target.mapRef.ID != b.target.mapRef.ID {
			return a.target.mapRef.ID < b.target.mapRef.ID
		}
		if (a.target.pendingGeneration == 0) != (b.target.pendingGeneration == 0) {
			return a.target.pendingGeneration == 0
		}
		// Required material users precede optional users of shared geometry.
		if a.target.object.VoxelGPUAdmissionOptional != b.target.object.VoxelGPUAdmissionOptional {
			return !a.target.object.VoxelGPUAdmissionOptional
		}
		return a.index < b.index
	})
	// Geometry priority and its first required material owner are independent.
	// An optional alias cannot advance fresh required geometry past a refused
	// first required material allocation.
	firstRequiredMaterial := make(map[*volume.XBrickMap]*core.VoxelObject)
	for _, candidate := range candidates {
		target := candidate.target
		if target.pendingGeneration == 0 && (!target.object.VoxelGPUAdmissionOptional || m.MaterialAllocations[target.object] != nil) && firstRequiredMaterial[target.mapRef] == nil {
			firstRequiredMaterial[target.mapRef] = target.object
		}
	}

	initial := voxelAdmissionPlan{sectorTail: uint64(m.SectorAlloc.Tail), brickTail: uint64(m.BrickAlloc.Tail), materialTail: uint64(m.MaterialAlloc.Tail), sectorFree: uint64(len(m.SectorAlloc.Free)), brickFree: uint64(len(m.BrickAlloc.Free)), materialFree: uint64(len(m.MaterialAlloc.Free)), sectors: make(map[*volume.Sector]bool), maps: make(map[*volume.XBrickMap]bool), objects: make(map[*core.VoxelObject]bool), lookup: make(map[*volume.XBrickMap]bool), removedSectors: make(map[*volume.Sector]bool)}

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
		for _, candidate := range candidates {
			target := candidate.target
			xbm, obj := target.mapRef, target.object
			if !plan.maps[xbm] {
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
					continue
				}
				growth := voxelResourcesGrow(next, desired)
				if (!allowGrowth && voxelResourcesGrow(current, desired)) || (!candidate.required && growth && m.voxelGPUAdmissionBudget.MaxBytes != 0 && voxelResourcePeakBytes(current, desired) > m.voxelGPUAdmissionBudget.MaxBytes) {
					trial.rollback(plan)
					denied[xbm] = true
					continue
				}
				plan, next = trial, desired
				delete(denied, xbm)
				delete(hard, xbm)
			}
			if target.pendingGeneration == 0 && !plan.objects[obj] {
				trial := plan.clone()
				trial.addMaterial(m, obj)
				desired, legal := trial.resources(m, current)
				desired.AtlasBytes = atlasBytes
				materialRequired := !obj.VoxelGPUAdmissionOptional || m.MaterialAllocations[obj] != nil
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
			*resources = current
			m.voxelGPUAdmissionStats.AllocationFailures = addRetainedVoxelBytes(m.voxelGPUAdmissionStats.AllocationFailures, 1)
			message := err.Error()
			if len(message) > 512 {
				message = message[:512]
			}
			m.voxelGPUAdmissionStats.LastError = message
			build(false)
		} else {
			recreated = true
		}
	}
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
	seenObjects := make(map[*core.VoxelObject]bool)
	for _, candidate := range candidates {
		obj := candidate.target.object
		if !plan.objects[obj] || seenObjects[obj] {
			continue
		}
		seenObjects[obj] = true
		mat := m.MaterialAllocations[obj]
		if mat == nil {
			mat = &MaterialGpuAllocation{MaterialTableLen: -1}
			m.MaterialAllocations[obj] = mat
		}
		if mat.MaterialCapacity < uint32(materialUploadRows(len(obj.MaterialTable))) {
			if mat.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(mat.MaterialOffset / materialBlockCapacity)
			}
			mat.MaterialOffset = m.MaterialAlloc.Alloc() * materialBlockCapacity
			mat.MaterialCapacity = materialBlockCapacity
			mat.MaterialTableLen = -1
		}
	}
	stats := m.voxelGPUAdmissionStats
	stats.CurrentBufferBytes = voxelResourceBufferBytes(*resources)
	stats.RetiredBufferBytes = resources.RetiredBytes
	stats.AtlasBytes = resources.AtlasBytes
	stats.TotalBytes = voxelResourceTotalBytes(*resources)
	stats.MaxBytes = m.voxelGPUAdmissionBudget.MaxBytes
	stats.PressureBytes = 0
	if stats.MaxBytes != 0 && stats.TotalBytes > stats.MaxBytes {
		stats.PressureBytes = stats.TotalBytes - stats.MaxBytes
	}
	stats.DeferredMaps = len(denied)
	stats.HardLimitDeferredMaps = len(hard)
	m.voxelGPUAdmissionStats = stats
	return recreated
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
