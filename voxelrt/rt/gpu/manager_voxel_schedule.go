package gpu

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// VoxelUploadBudget caps content writes per UpdateVoxelData invocation. Zero
// pauses the corresponding resource. Allocation and lookup writes are separate.
type VoxelUploadBudget struct {
	MaxBytes   uint64
	MaxSectors uint32
	MaxBricks  uint32
}

func DefaultVoxelUploadBudget() VoxelUploadBudget {
	return VoxelUploadBudget{4 * 1024 * 1024, MaxUpdatesPerFrame, 65536}
}

func (m *GpuBufferManager) SetVoxelUploadBudget(budget VoxelUploadBudget) {
	m.VoxelUploadBytesPerFrame = budget.MaxBytes
	m.SectorsPerFrame = budget.MaxSectors
	m.VoxelUploadBricksPerFrame = budget.MaxBricks
}

func (m *GpuBufferManager) VoxelUploadBudget() VoxelUploadBudget {
	return VoxelUploadBudget{m.VoxelUploadBytesPerFrame, m.SectorsPerFrame, m.VoxelUploadBricksPerFrame}
}

type voxelUploadKind uint8

const (
	voxelUploadMaterial voxelUploadKind = iota
	voxelUploadSector
	voxelUploadBrick
)

type voxelUploadWork struct {
	kind              voxelUploadKind
	object            *core.VoxelObject
	sectorKey         [3]int
	brickKey          [6]int
	bytes             uint64
	sectors, bricks   uint32
	target            *volume.XBrickMap
	targetRevision    uint64
	pendingGeneration uint64
}

// Legacy helper work literals resolve their target at use; production queues
// capture the selected map and revision before sorting or invoking an executor.
func (w voxelUploadWork) targetMap() *volume.XBrickMap {
	if w.target != nil {
		return w.target
	}
	return w.object.RenderVoxelMap()
}

func (w voxelUploadWork) targetCurrent() bool {
	target := w.targetMap()
	if target == nil || (w.target != nil && target.Revision != w.targetRevision) {
		return false
	}
	if w.pendingGeneration != 0 {
		return w.object.PendingFullUploadMap() == target && w.object.PendingFullUploadGeneration() == w.pendingGeneration
	}
	return w.object.RenderVoxelMap() == target
}

func (w voxelUploadWork) uploadOrder() uint64 {
	if w.object.VoxelUploadOrder != 0 {
		return w.object.VoxelUploadOrder
	}
	return uint64(w.targetMap().ID)
}

// Identities retain coordinates and first service frame, never encoded payloads.
// Materials also include the current map so detachment resets their age.
type voxelUploadIdentity struct {
	kind       voxelUploadKind
	xbm        *volume.XBrickMap
	object     *core.VoxelObject
	coordinate [6]int
}

func (w voxelUploadWork) identity() voxelUploadIdentity {
	id := voxelUploadIdentity{kind: w.kind, xbm: w.targetMap()}
	switch w.kind {
	case voxelUploadMaterial:
		id.object = w.object
	case voxelUploadSector:
		copy(id.coordinate[:3], w.sectorKey[:])
	case voxelUploadBrick:
		id.coordinate = w.brickKey
	}
	return id
}

func materialUploadRows(length int) int {
	if length == 0 {
		return materialBlockCapacity
	}
	return min(length, materialBlockCapacity)
}

func brickUploadBytes(brick *volume.Brick) uint64 {
	bytes := uint64(BrickRecordSize)
	if brick == nil {
		return bytes
	}
	mode := resolveBrickUploadMode(brick.Flags)
	if mode.usesAux {
		bytes += VoxelAuxRecordBytes
	}
	if mode.usesPayload {
		bytes += payloadBytesPerBrick
	}
	return bytes
}

func voxelUploadOrder(obj *core.VoxelObject) uint64 {
	if obj.VoxelUploadOrder != 0 {
		return obj.VoxelUploadOrder
	}
	return uint64(obj.RenderVoxelMap().ID)
}

// serviceVoxelUploads is the shared production admission/completion path.
// An executor may refuse only before writing. Successful execution means writes
// were queued, not that GPU execution has completed.
func (m *GpuBufferManager) serviceVoxelUploads(scene *core.Scene, execute func(voxelUploadWork) bool) {
	m.VoxelUploadBytes = 0
	m.VoxelSectorsUploaded = 0
	m.VoxelBricksUploaded = 0
	m.VoxelMaterialsUploaded = 0
	m.VoxelDirtySectorsPending = 0
	m.VoxelDirtyBricksPending = 0
	m.voxelUploadFrame++
	if m.MaterialAllocations == nil {
		m.MaterialAllocations = make(map[*core.VoxelObject]*MaterialGpuAllocation)
	}
	var queue []voxelUploadWork
	liveMaps := make(map[*volume.XBrickMap]bool)
	liveObjects := make(map[*core.VoxelObject]bool)
	best := make(map[*volume.XBrickMap]voxelServiceTarget)
	objectOrder := make(map[*core.VoxelObject]int)
	var maps []*volume.XBrickMap
	for index, target := range voxelServiceTargets(scene) {
		obj, xbm := target.object, target.mapRef
		liveMaps[xbm], liveObjects[obj] = true, true
		_, seenObject := objectOrder[obj]
		if !seenObject {
			objectOrder[obj] = index
		}
		previous, exists := best[xbm]
		if !exists {
			maps = append(maps, xbm)
		}
		order := func(t voxelServiceTarget) uint64 {
			if t.object.VoxelUploadOrder != 0 {
				return t.object.VoxelUploadOrder
			}
			return uint64(t.mapRef.ID)
		}
		if !exists || obj.VoxelUploadPriority < previous.object.VoxelUploadPriority ||
			(obj.VoxelUploadPriority == previous.object.VoxelUploadPriority &&
				(order(target) < order(previous) || (order(target) == order(previous) && target.pendingGeneration == 0 && previous.pendingGeneration != 0))) {
			best[xbm] = target
		}
		// Materials are anchored to display and allocated only once per object.
		if target.pendingGeneration != 0 || seenObject || !m.voxelMapAdmitted(xbm) || !m.voxelObjectAdmitted(obj) {
			continue
		}

		mat := m.MaterialAllocations[obj]
		if mat == nil {
			mat = &MaterialGpuAllocation{MaterialTableLen: -1}
			m.MaterialAllocations[obj] = mat
		}
		rows := materialUploadRows(len(obj.MaterialTable))
		if mat.MaterialCapacity < uint32(rows) {
			if mat.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(mat.MaterialOffset / materialBlockCapacity)
			}
			mat.MaterialOffset = m.MaterialAlloc.Alloc() * materialBlockCapacity
			mat.MaterialCapacity = materialBlockCapacity
			mat.MaterialTableLen = -1
		}
		ptr, length := materialTableIdentity(obj.MaterialTable)
		if mat.MaterialTablePtr != ptr || mat.MaterialTableLen != length || mat.BufferGeneration != m.MaterialBufferGeneration {
			queue = append(queue, voxelUploadWork{kind: voxelUploadMaterial, object: obj, target: xbm, targetRevision: xbm.Revision, bytes: uint64(rows) * 64})
		}
	}
	for _, xbm := range maps {
		if !m.voxelMapAdmitted(xbm) {
			continue
		}
		target := best[xbm]
		obj := target.object
		for key, dirty := range xbm.DirtySectors {
			sector := xbm.Sectors[key]
			if !dirty || sector == nil {
				delete(xbm.DirtySectors, key)
				continue
			}
			bytes := uint64(32)
			for i := 0; i < 64; i++ {
				bytes += brickUploadBytes(sector.GetBrick(i%4, (i/4)%4, i/16))
			}
			queue = append(queue, voxelUploadWork{kind: voxelUploadSector, object: obj, target: xbm, targetRevision: xbm.Revision, pendingGeneration: target.pendingGeneration, sectorKey: key, bytes: bytes, sectors: 1, bricks: 64})
		}
		for key, dirty := range xbm.DirtyBricks {
			sKey := [3]int{key[0], key[1], key[2]}
			sector := xbm.Sectors[sKey]
			if !dirty || sector == nil || key[3] < 0 || key[3] >= 4 || key[4] < 0 || key[4] >= 4 || key[5] < 0 || key[5] >= 4 {
				delete(xbm.DirtyBricks, key)
				continue
			}
			if xbm.DirtySectors[sKey] {
				continue
			}
			queue = append(queue, voxelUploadWork{kind: voxelUploadBrick, object: obj, target: xbm, targetRevision: xbm.Revision, pendingGeneration: target.pendingGeneration, brickKey: key, bytes: brickUploadBytes(sector.GetBrick(key[3], key[4], key[5])), bricks: 1})
		}
	}
	ages := make(map[voxelUploadIdentity]uint64, len(queue))
	// Deferred live work retains its first service frame. Detachment still
	// removes identities, without needing to encode or enqueue denied work.
	for id, first := range m.voxelUploadAges {
		if !liveMaps[id.xbm] || (id.object != nil && !liveObjects[id.object]) {
			continue
		}
		if !m.voxelMapAdmitted(id.xbm) || (id.kind == voxelUploadMaterial && !m.voxelObjectAdmitted(id.object)) {
			ages[id] = first
		}
	}
	for _, w := range queue {
		id := w.identity()
		first, exists := m.voxelUploadAges[id]
		if !exists {
			first = m.voxelUploadFrame
		}
		ages[id] = first
	}
	effectivePriority := func(w voxelUploadWork) uint64 {
		priority := uint64(w.object.VoxelUploadPriority)
		promotion := (m.voxelUploadFrame - ages[w.identity()]) / 8
		return priority - min(priority, promotion)
	}
	sort.SliceStable(queue, func(i, j int) bool {
		a, b := queue[i], queue[j]
		if pa, pb := effectivePriority(a), effectivePriority(b); pa != pb {
			return pa < pb
		}
		if fa, fb := ages[a.identity()], ages[b.identity()]; fa != fb {
			return fa < fb
		}
		if (a.pendingGeneration == 0) != (b.pendingGeneration == 0) {
			return a.pendingGeneration == 0
		}
		if oa, ob := a.uploadOrder(), b.uploadOrder(); oa != ob {
			return oa < ob
		}
		if ia, ib := a.targetMap().ID, b.targetMap().ID; ia != ib {
			return ia < ib
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		ca, cb := a.identity().coordinate, b.identity().coordinate
		for k := range ca {
			if ca[k] != cb[k] {
				return ca[k] < cb[k]
			}
		}
		return objectOrder[a.object] < objectOrder[b.object]
	})
	remaining := m.VoxelUploadBudget()
	for _, w := range queue {
		if !w.targetCurrent() {
			continue
		}
		if w.bytes > remaining.MaxBytes || w.sectors > remaining.MaxSectors || w.bricks > remaining.MaxBricks {
			continue
		}
		if w.kind != voxelUploadMaterial && !m.voxelUploadPayloadFits(w) {
			continue
		}
		if execute == nil || !execute(w) {
			continue
		}
		remaining.MaxBytes -= w.bytes
		remaining.MaxSectors -= w.sectors
		remaining.MaxBricks -= w.bricks
		m.VoxelUploadBytes += w.bytes
		m.VoxelSectorsUploaded += int(w.sectors)
		m.VoxelBricksUploaded += int(w.bricks)
		if w.kind == voxelUploadMaterial {
			m.VoxelMaterialsUploaded++
		}
		// Written bytes consume admission even if execution changed selection or
		// revision. Such work cannot acknowledge any newer dirty queues.
		if !w.targetCurrent() {
			continue
		}
		xbm := w.targetMap()
		switch w.kind {
		case voxelUploadMaterial:
			mat := m.MaterialAllocations[w.object]
			mat.MaterialTablePtr, mat.MaterialTableLen = materialTableIdentity(w.object.MaterialTable)
			mat.BufferGeneration = m.MaterialBufferGeneration
			mat.HasTransparency = materialTableHasTransparency(w.object.MaterialTable)
		case voxelUploadSector:
			delete(xbm.DirtySectors, w.sectorKey)
			for i := 0; i < 64; i++ {
				delete(xbm.DirtyBricks, [6]int{w.sectorKey[0], w.sectorKey[1], w.sectorKey[2], i % 4, (i / 4) % 4, i / 16})
			}
		case voxelUploadBrick:
			delete(xbm.DirtyBricks, w.brickKey)
		}
		delete(ages, w.identity())
	}
	m.voxelUploadAges = ages
	for _, xbm := range maps {
		m.VoxelDirtySectorsPending += len(xbm.DirtySectors)
		m.VoxelDirtyBricksPending += len(xbm.DirtyBricks)
	}
	if m.VoxelUploadBytes != 0 {
		m.VoxelUploadRevision++
	}
}

func (w voxelUploadWork) sectorCoordinate() [3]int {
	if w.kind == voxelUploadSector {
		return w.sectorKey
	}
	return [3]int{w.brickKey[0], w.brickKey[1], w.brickKey[2]}
}

func (w voxelUploadWork) brickRange() (int, int) {
	if w.kind == voxelUploadSector {
		return 0, 64
	}
	i := w.brickKey[3] + w.brickKey[4]*4 + w.brickKey[5]*16
	return i, i + 1
}

// Compute exactly the releases that execution will perform before allocating.
// Protect current target pointers throughout the sector, including moved bricks.
func (m *GpuBufferManager) voxelUploadReleases(w voxelUploadWork) (payload, auxiliary map[*volume.Brick]bool) {
	payload, auxiliary = make(map[*volume.Brick]bool), make(map[*volume.Brick]bool)
	key := w.sectorCoordinate()
	sector := w.targetMap().Sectors[key]
	pointers := m.Allocations[w.targetMap()].Bricks[key]
	targets := make(map[*volume.Brick]bool)
	for i := 0; i < 64; i++ {
		if b := sector.GetBrick(i%4, (i/4)%4, i/16); b != nil {
			targets[b] = true
		}
	}
	start, end := w.brickRange()
	for i := start; i < end; i++ {
		current := sector.GetBrick(i%4, (i/4)%4, i/16)
		if pointers != nil {
			if old := pointers[i]; old != nil && !targets[old] {
				payload[old], auxiliary[old] = true, true
			}
		}
		if current != nil && !resolveBrickUploadMode(current.Flags).usesPayload {
			payload[current] = true
		}
	}
	// Other live records still address these slots. Reclaim only references
	// replaced by this complete upload unit, including aliases inside one map.
	for brick := range payload {
		if _, exists := m.BrickToSlot[brick]; !exists || m.voxelBrickReferencedOutsideUpload(brick, w) {
			delete(payload, brick)
		}
	}
	for brick := range auxiliary {
		if _, exists := m.BrickToAuxSlot[brick]; !exists || m.voxelBrickReferencedOutsideUpload(brick, w) {
			delete(auxiliary, brick)
		}
	}
	return payload, auxiliary
}

func (m *GpuBufferManager) voxelBrickReferencedOutsideUpload(brick *volume.Brick, w voxelUploadWork) bool {
	key := w.sectorCoordinate()
	start, end := w.brickRange()
	for xbm, alloc := range m.Allocations {
		for coordinate, pointers := range alloc.Bricks {
			if pointers == nil {
				continue
			}
			for index, reference := range pointers {
				if xbm == w.targetMap() && coordinate == key && index >= start && index < end {
					continue
				}
				if reference == brick {
					return true
				}
			}
		}
	}
	return false
}

func (m *GpuBufferManager) voxelUploadPayloadFits(w voxelUploadWork) bool {
	key := w.sectorCoordinate()
	alloc := m.Allocations[w.targetMap()]
	sector := w.targetMap().Sectors[key]
	if alloc == nil || alloc.Bricks[key] == nil || sector == nil {
		return false
	}
	if _, ok := m.SectorToInfo[sector]; !ok {
		return false
	}
	available := uint64(0)
	capacity := m.voxelPayloadCapacityPerPage()
	for page := uint32(0); page < m.VoxelPayloadPageCount; page++ {
		a := &m.PayloadAlloc[page]
		available += uint64(len(a.Free))
		if a.Tail < capacity {
			available += uint64(capacity - a.Tail)
		}
	}
	releases, _ := m.voxelUploadReleases(w)
	for brick := range releases {
		if slot, ok := m.BrickToSlot[brick]; ok && slot.Page < m.VoxelPayloadPageCount {
			available++
		}
	}
	needed := make(map[*volume.Brick]bool)
	start, end := w.brickRange()
	for i := start; i < end; i++ {
		brick := sector.GetBrick(i%4, (i/4)%4, i/16)
		if brick == nil || !resolveBrickUploadMode(brick.Flags).usesPayload {
			continue
		}
		if _, exists := m.BrickToSlot[brick]; !exists {
			needed[brick] = true
		}
	}
	return uint64(len(needed)) <= available
}
