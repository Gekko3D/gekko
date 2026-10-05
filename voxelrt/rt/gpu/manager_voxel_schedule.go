package gpu

import (
	"bytes"
	"math/bits"
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
	packedAuxiliarySnapshot *packedAuxiliaryUploadSnapshot
	brickSnapshot           *voxelBrickUploadSnapshot
	sectorSnapshot          *voxelSectorUploadSnapshot
	recordMask              uint64
	sparseRecordSet         bool
	material                *MaterialGpuAllocation
	materialBlock           *materialGPUBlock
	materialData            []byte
	materialRows            []core.Material
	materialPtr             uintptr
	materialLen             int
	materialGeneration      uint64
	kind                    voxelUploadKind
	object                  *core.VoxelObject
	sectorKey               [3]int
	brickKey                [6]int
	bytes                   uint64
	sectors, bricks         uint32
	target                  *volume.XBrickMap
	targetRevision          uint64
	pendingGeneration       uint64
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

func (m *GpuBufferManager) materialWorkCurrent(w voxelUploadWork) bool {
	if w.material == nil {
		return w.targetCurrent()
	}
	if m.MaterialAllocations[w.object] != w.material || m.MaterialBufferGeneration != w.materialGeneration {
		return false
	}
	if w.materialBlock != nil {
		table := w.object.ImmutableMaterialTable()
		return table != nil && table.Identity() == w.materialBlock.table.Identity() && w.material.block == w.materialBlock
	}
	ptr, length := materialTableIdentity(w.object.MaterialTable)
	return w.materialPtr == ptr && w.materialLen == length && bytes.Equal(w.materialData, buildMaterialData(w.object.MaterialTable))
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
	kind          voxelUploadKind
	xbm           *volume.XBrickMap
	object        *core.VoxelObject
	materialBlock *materialGPUBlock
	coordinate    [6]int
}

func (w voxelUploadWork) identity() voxelUploadIdentity {
	id := voxelUploadIdentity{kind: w.kind, xbm: w.targetMap()}
	switch w.kind {
	case voxelUploadMaterial:
		if w.materialBlock != nil {
			id.materialBlock = w.materialBlock
			id.xbm = nil
		} else {
			id.object = w.object
		}
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

// Admission charges every buffer write, including mirrors already created in
// staging. Payload textures are shared and remain one write per uploaded brick.
func (m *GpuBufferManager) voxelBrickUploadBytes(brick *volume.Brick) uint64 {
	bytes := brickUploadBytes(brick)
	auxBytes := uint64(VoxelAuxRecordBytes)
	if m.usesVoxelAuxiliaryPackets() && brick != nil && resolveBrickUploadMode(brick.Flags).usesAux {
		auxBytes = uint64(m.auxiliaryPacketWordCount(brick)) * 4
		bytes = bytes - VoxelAuxRecordBytes + auxBytes
		if m.packedVoxelMaterials && resolveBrickUploadMode(brick.Flags).usesPayload {
			bytes -= payloadBytesPerBrick
		}
	}
	if m.voxelBufferMirrored(1) {
		bytes += BrickRecordSize
	}
	if brick != nil && resolveBrickUploadMode(brick.Flags).usesAux && m.voxelBufferMirrored(2) {
		bytes += auxBytes
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
func (m *GpuBufferManager) serviceVoxelUploads(scene *core.Scene, execute func(voxelUploadWork) bool, contexts ...func() voxelNormalBakeContext) {
	m.observeShadowUploadRevision()
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
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
	var materialObjects []*core.VoxelObject
	for _, target := range voxelServiceTargets(scene) {
		if target.pendingGeneration == 0 && m.voxelMapAdmitted(target.mapRef) && m.voxelObjectAdmitted(target.object) {
			materialObjects = append(materialObjects, target.object)
		}
	}
	m.prepareMaterialAttachments(materialObjects)
	materialQueue := make(map[*materialGPUBlock]int)
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
		ptr, length := materialTableIdentity(obj.MaterialTable)
		source := mat
		if mat.block != nil {
			source = &mat.block.allocation
		}
		if source.MaterialTableLen < 0 || (mat.block == nil && (mat.MaterialTablePtr != ptr || mat.MaterialTableLen != length)) || source.BufferGeneration != m.MaterialBufferGeneration {
			var rows []core.Material
			var data []byte
			if mat.block != nil {
				rows, data = mat.block.rows, mat.block.data
			} else {
				rows = append([]core.Material(nil), obj.MaterialTable...)
				data = buildMaterialData(rows)
			}
			bytes := uint64(len(data))
			if m.voxelBufferMirrored(3) {
				bytes *= 2
			}
			work := voxelUploadWork{kind: voxelUploadMaterial, object: obj, target: xbm, targetRevision: xbm.Revision, bytes: bytes, material: mat, materialBlock: mat.block, materialData: data, materialRows: rows, materialPtr: ptr, materialLen: length, materialGeneration: m.MaterialBufferGeneration}
			if mat.block != nil {
				if index, exists := materialQueue[mat.block]; exists {
					previous := queue[index]
					if obj.VoxelUploadPriority < previous.object.VoxelUploadPriority || (obj.VoxelUploadPriority == previous.object.VoxelUploadPriority && work.uploadOrder() < previous.uploadOrder()) {
						queue[index] = work
					}
					continue
				}
				materialQueue[mat.block] = len(queue)
			}
			queue = append(queue, work)
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
			work := voxelUploadWork{kind: voxelUploadSector, object: obj, target: xbm, targetRevision: xbm.Revision, pendingGeneration: target.pendingGeneration, sectorKey: key, sectors: 1}
			queue = append(queue, m.qualifySectorUpload(work))
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
			work := voxelUploadWork{kind: voxelUploadBrick, object: obj, target: xbm, targetRevision: xbm.Revision, pendingGeneration: target.pendingGeneration, brickKey: key, bytes: m.voxelBrickUploadBytes(sector.GetBrick(key[3], key[4], key[5])), bricks: 1}
			if info := m.SectorToInfo[sector]; info.packed != nil && sector.GetBrick(key[3], key[4], key[5]) == nil {
				work.bytes = 0
				work.bricks = 0
			}
			queue = append(queue, work)
		}
	}
	ages := make(map[voxelUploadIdentity]uint64, len(queue))
	// Deferred live work retains its first service frame. Detachment still
	// removes identities, without needing to encode or enqueue denied work.
	for id, first := range m.voxelUploadAges {
		if id.materialBlock != nil && m.materialBlocks[id.materialBlock.table.Identity()] != id.materialBlock {
			continue
		}
		if (id.materialBlock == nil && !liveMaps[id.xbm]) || (id.object != nil && !liveObjects[id.object]) {
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
	completedPhysical := make(map[*volume.Sector]voxelUploadWork)
	var context func() voxelNormalBakeContext
	if len(contexts) != 0 {
		context = contexts[0]
	}
	for _, w := range queue {
		if !w.targetCurrent() || (w.kind == voxelUploadMaterial && !m.materialWorkCurrent(w)) {
			continue
		}
		if w.kind != voxelUploadMaterial {
			key := w.sectorCoordinate()
			sector := w.targetMap().Sectors[key]
			allocation := m.Allocations[w.targetMap()]
			if sector == nil || allocation == nil || allocation.Sectors[key] != sector || allocation.Bricks[key] == nil {
				continue
			}
			if _, ok := m.SectorToInfo[sector]; !ok {
				continue
			}
		}
		if w.kind != voxelUploadMaterial {
			sector := w.targetMap().Sectors[w.sectorCoordinate()]
			info := m.SectorToInfo[sector]
			if w.kind == voxelUploadBrick && info.packed != nil && (!info.packed.published || info.packed.mask != sector.BrickMask64) {
				w.kind = voxelUploadSector
				w.sectorKey = [3]int{w.brickKey[0], w.brickKey[1], w.brickKey[2]}
				w.sectors = 1
				w.targetMap().DirtySectors[w.sectorKey] = true
			}
		}
		if w.kind == voxelUploadBrick {
			sector := w.targetMap().Sectors[w.sectorCoordinate()]
			info := m.SectorToInfo[sector]
			start, _ := w.brickRange()
			if info.packed != nil && sector.BrickMask64&(uint64(1)<<start) == 0 {
				delete(w.targetMap().DirtyBricks, w.brickKey)
				delete(ages, w.identity())
				continue
			}
			w.bytes = m.voxelBrickUploadBytes(sector.GetBrick(start%4, (start/4)%4, start/16))
			w.bricks = 1
		}
		if w.kind == voxelUploadSector {
			w = m.qualifySectorUpload(w)
		}
		if (!m.usesVoxelAuxiliaryPackets() || w.kind == voxelUploadMaterial) && (w.bytes > remaining.MaxBytes || w.sectors > remaining.MaxSectors || w.bricks > remaining.MaxBricks) {
			continue
		}
		if !m.usesVoxelAuxiliaryPackets() && w.kind != voxelUploadMaterial && (!m.voxelUploadPayloadFits(w) || !m.voxelUploadAuxiliaryFits(w) || !m.packedUploadFits(w)) {
			continue
		}
		if w.kind == voxelUploadSector {
			w.sectorSnapshot = m.captureSectorUpload(w)
		}
		if w.kind == voxelUploadBrick {
			w.brickSnapshot = m.captureBrickUpload(w)
		}
		var auxiliaryBefore voxelIndexRanges
		auxiliaryClaimed := false
		if m.usesVoxelAuxiliaryPackets() && w.kind != voxelUploadMaterial {
			w = m.capturePackedAuxiliaryUpload(w, context)
			previous, completed := completedPhysical[w.packedAuxiliarySnapshot.sector]
			if completed && samePackedPhysicalUpload(previous, w) {
				if w.targetCurrent() && w.packedAuxiliarySnapshot.current() && (w.sectorSnapshot == nil || w.sectorSnapshot.current(w)) && (w.brickSnapshot == nil || w.brickSnapshot.current(w)) {
					acknowledgeVoxelGeometryWork(w)
					delete(ages, w.identity())
				}
				continue
			}
			if w.bytes > remaining.MaxBytes || w.sectors > remaining.MaxSectors || w.bricks > remaining.MaxBricks {
				continue
			}
			if !m.voxelUploadPayloadFits(w) || !m.packedUploadFits(w) {
				continue
			}
			claimed, ok := m.planCapturedAuxiliaryUpload(w)
			if !ok {
				continue
			}
			auxiliaryBefore = m.auxiliaryRanges.clone()
			m.auxiliaryRanges = claimed
			auxiliaryClaimed = true
		}
		brickBefore := m.brickRanges.clone()
		if w.sectorSnapshot != nil && !m.assignPackedUpload(w.sectorSnapshot) {
			if auxiliaryClaimed {
				m.auxiliaryRanges = auxiliaryBefore
			}
			continue
		}
		snapshot := m.captureVoxelUploadSnapshot(w)
		geometry := m.Allocations[w.targetMap()]
		material := m.MaterialAllocations[w.object]
		sector := w.targetMap().Sectors[w.sectorCoordinate()]
		sectorInfo, hasSectorInfo := m.SectorToInfo[sector]
		var opacity materialShadowOpacity
		if w.kind == voxelUploadMaterial {
			if w.materialBlock != nil {
				opacity = captureMaterialShadowOpacity(nil, w.materialRows)
				opacity.writtenRows = materialBlockCapacity
			} else {
				opacity = captureMaterialShadowOpacity(material, w.object.MaterialTable)
			}
		}
		if execute == nil || !execute(w) {
			if auxiliaryClaimed {
				m.auxiliaryRanges = auxiliaryBefore
			}
			if w.sectorSnapshot != nil && w.sectorSnapshot.newRange {
				if auxiliaryClaimed {
					m.brickRanges = brickBefore
				} else {
					m.brickRanges.release(w.sectorSnapshot.info.BrickTableIndex, packedBrickCapacity(w.sectorSnapshot.mask))
				}
			}
			continue
		}
		// First publication belongs to the exact captured physical allocation.
		if w.kind == voxelUploadSector && hasSectorInfo && sectorInfo.pending {
			if current, ok := m.SectorToInfo[sector]; ok && current == sectorInfo {
				current.pending = false
				m.SectorToInfo[sector] = current
				m.sectorTopologyRevision++
			}
		}
		// Epochs describe written allocations, even when selection changed during execution.
		if w.kind == voxelUploadMaterial {
			if w.materialBlock != nil {
				material = &w.materialBlock.allocation
			}
			if material != nil {
				if material.shadowOpacity != opacity {
					material.shadowUploadEpoch++
				}
				material.shadowOpacity = opacity
			}
		} else if geometry != nil {
			geometry.shadowUploadEpoch++
		}
		m.commitVoxelUploadSnapshot(snapshot)
		if w.kind != voxelUploadMaterial {
			m.publishPackedUpload(w, geometry)
			if w.packedAuxiliarySnapshot != nil {
				m.auxiliaryPackedLeased = true
				m.publishAuxiliaryUpload(w)
				physical := w.packedAuxiliarySnapshot.sector
				_, tracked := completedPhysical[physical]
				if info := m.SectorToInfo[physical]; tracked || (info.packed != nil && len(info.packed.owners) > 1) {
					completedPhysical[physical] = w
				}
			}
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
		// A shared write acknowledges its captured block even if its representative
		// changed; only still-current attachments may inherit that acknowledgement.
		if w.kind == voxelUploadMaterial && w.materialBlock != nil {
			block := w.materialBlock
			block.allocation.MaterialTableLen = len(w.materialRows)
			block.allocation.BufferGeneration = w.materialGeneration
			block.allocation.HasTransparency = materialTableHasTransparency(w.materialRows)
			for obj, a := range block.attachments {
				if table := obj.ImmutableMaterialTable(); table != nil && table.Identity() == block.table.Identity() {
					m.acknowledgeMaterialAttachment(obj, a, block)
				}
			}
		} else {
			if !w.targetCurrent() || (w.kind == voxelUploadMaterial && !m.materialWorkCurrent(w)) || (w.sectorSnapshot != nil && !w.sectorSnapshot.current(w)) || (w.brickSnapshot != nil && !w.brickSnapshot.current(w)) || (w.packedAuxiliarySnapshot != nil && !w.packedAuxiliarySnapshot.current()) {
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
		}
		delete(ages, w.identity())
	}
	// Executors may remove objects while a packet is being queued. Reconcile
	// removals once after service, before any readiness/scene publication.
	if scene != nil {
		clear(liveObjects)
		for _, obj := range scene.Objects {
			liveObjects[obj] = true
		}
		for obj := range m.MaterialAllocations {
			if !liveObjects[obj] {
				m.releaseMaterialAllocation(obj)
			}
		}
		for obj := range m.managedMaterialObjects {
			if !liveObjects[obj] {
				delete(m.managedMaterialObjects, obj)
			}
		}
	}
	m.voxelUploadAges = ages
	for _, xbm := range maps {
		m.VoxelDirtySectorsPending += len(xbm.DirtySectors)
		m.VoxelDirtyBricksPending += len(xbm.DirtyBricks)
	}
	// Catch unknown writes made by an executor before attributing our own increment.
	m.observeShadowUploadRevision()
	if m.VoxelUploadBytes != 0 {
		m.VoxelUploadRevision++
		m.shadowObservedUploadRevision = m.VoxelUploadRevision
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
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	payload, auxiliary = make(map[*volume.Brick]bool), make(map[*volume.Brick]bool)
	key := w.sectorCoordinate()
	sector := w.targetMap().Sectors[key]
	pointers := m.Allocations[w.targetMap()].Bricks[key]
	targets := make(map[*volume.Brick]bool)
	for i := 0; i < 64; i++ {
		if b := w.desiredBrick(sector, i); b != nil {
			targets[b] = true
		}
	}
	start, end := w.brickRange()
	for i := start; i < end; i++ {
		current := w.desiredBrick(sector, i)
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
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	key := w.sectorCoordinate()
	start, end := w.brickRange()
	if !m.voxelOwnership.legacy && brick != nil {
		outside := m.voxelOwnership.bricks[brick]
		alloc := m.Allocations[w.targetMap()]
		if !m.ownsVoxelAllocation(w.targetMap(), alloc) {
			m.invalidateVoxelOwnership()
		} else {
			if pointers := alloc.Bricks[key]; pointers != nil {
				for i := start; i < end; i++ {
					if pointers[i] == brick {
						outside--
					}
				}
			}
			return outside > 0
		}
	}
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
	if m.packedVoxelMaterials {
		return true
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
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

// qualifySectorUpload rechecks the complete unit immediately before admission to
// the frame budget. Raw edits need not have changed the map revision.
func (m *GpuBufferManager) qualifySectorUpload(w voxelUploadWork) voxelUploadWork {
	xbm := w.targetMap()
	sector := xbm.Sectors[w.sectorKey]
	allocation := m.Allocations[xbm]
	w.recordMask = ^uint64(0)
	w.sparseRecordSet = m.ownsVoxelAllocation(xbm, allocation) && allocation.Sectors[w.sectorKey] == sector && allocation.Bricks[w.sectorKey] != nil
	if info := m.SectorToInfo[sector]; info.packed != nil {
		w.recordMask = sector.BrickMask64
		w.sparseRecordSet = true
	} else if w.sparseRecordSet {
		w.recordMask = 0
		previous := allocation.Bricks[w.sectorKey]
		for i := 0; i < 64; i++ {
			if previous[i] != nil || sector.GetBrick(i%4, (i/4)%4, i/16) != nil {
				w.recordMask |= uint64(1) << i
			}
		}
	}
	w.bytes = 32
	if m.voxelBufferMirrored(0) {
		w.bytes *= 2
	}
	for i := 0; i < 64; i++ {
		if w.recordMask&(uint64(1)<<i) != 0 {
			w.bytes += m.voxelBrickUploadBytes(sector.GetBrick(i%4, (i/4)%4, i/16))
		}
	}
	w.bricks = uint32(bits.OnesCount64(w.recordMask))
	return w
}

func acknowledgeVoxelGeometryWork(w voxelUploadWork) {
	xbm := w.targetMap()
	if w.kind == voxelUploadSector {
		delete(xbm.DirtySectors, w.sectorKey)
		for index := 0; index < 64; index++ {
			delete(xbm.DirtyBricks, [6]int{w.sectorKey[0], w.sectorKey[1], w.sectorKey[2], index % 4, (index / 4) % 4, index / 16})
		}
	} else if w.kind == voxelUploadBrick {
		delete(xbm.DirtyBricks, w.brickKey)
	}
}
