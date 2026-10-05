package gpu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/cogentcore/webgpu/wgpu"
)

const materialBlockCapacity = 256
const payloadBytesPerBrick = volume.BrickSize * volume.BrickSize * volume.BrickSize
const DefaultRetainedVoxelMapBudgetSectors = 4096

func materialTableHasTransparency(table []core.Material) bool {
	for i, mat := range table {
		if i == 0 {
			continue
		}
		if mat.Transparency > 0.001 || mat.Transmission > 0.001 {
			return true
		}
	}
	return false
}

func materialTableIdentity(table []core.Material) (uintptr, int) {
	if len(table) == 0 {
		return 0, 0
	}
	return uintptr(unsafe.Pointer(&table[0])), len(table)
}

func buildMaterialData(table []core.Material) []byte {
	if len(table) == 0 {
		return make([]byte, materialBlockCapacity*64)
	}
	if len(table) > materialBlockCapacity {
		table = table[:materialBlockCapacity]
	}

	materials := make([]byte, 0, len(table)*64)
	for _, mat := range table {
		materials = append(materials, rgbaToVec4(mat.BaseColor)...)
		materials = append(materials, rgbaToVec4(mat.Emissive)...)
		materials = append(materials, float32ToBytes(mat.Roughness)...)
		materials = append(materials, float32ToBytes(mat.Metalness)...)
		materials = append(materials, float32ToBytes(mat.IOR)...)
		materials = append(materials, float32ToBytes(mat.Transparency)...)
		materials = append(materials, vec4ToBytes([4]float32{mat.Emission, mat.Transmission, mat.Density, mat.Refraction})...)
	}
	return materials
}

func (m *GpuBufferManager) UpdateVoxelData(scene *core.Scene) bool {
	return m.updateVoxelData(scene, nativeVoxelBackend{m})
}

func (m *GpuBufferManager) updateVoxelData(scene *core.Scene, backend voxelNativeBackend) bool {
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	m.voxelNative = backend
	m.ensureBrickRecordRanges()
	m.promotePackedDirtySectors(scene)
	m.voxelGPUWorkStats = VoxelGPUWorkStats{}
	m.voxelWorkAdvanced = false
	recreated := false
	m.ensureRetainedVoxelMaps()
	m.VoxelUniformSparseBricks = 0
	m.VoxelPayloadSparseBricks = 0
	m.VoxelPayloadUploadsSkipped = 0
	m.VoxelPayloadBytesAvoided = 0
	m.VoxelRuntimeNormalBakeDuration = 0
	stageFailed := false
	if m.voxelGrowth != nil {
		published, err := m.advanceVoxelGPUStage()
		recreated = published
		if err != nil && !errors.Is(err, errVoxelGPUWorkPending) {
			m.recordVoxelGPUAllocationFailure(err)
			stageFailed = true
		}
	}
	resources := m.currentVoxelGPUResources()
	if m.prepareVoxelGPUAdmission(scene, &resources, func(next voxelGPUResources) error {
		if stageFailed || m.voxelGrowth != nil {
			return errVoxelGPUWorkPending
		}
		// The existing atomic native bootstrap supplies mandatory bindings and
		// payload atlases before resumable post-bootstrap growth is possible.
		bootstrap := false
		if m.Device != nil {
			for i := uint32(0); i < m.VoxelPayloadPageCount; i++ {
				if m.VoxelPayloadTex[i] == nil {
					bootstrap = true
					break
				}
			}
		}
		if bootstrap {
			return m.growVoxelGPUResources(&resources, next)
		}
		err := m.startVoxelGPUStage(next)
		resources = m.currentVoxelGPUResources()
		if err == nil {
			return errVoxelGPUStagePublished
		}
		return err
	}) {
		recreated = true
	}
	// Tree64 belongs to the optional legacy representation, outside voxel budget.
	if m.Device != nil && m.ensureBuffer("Tree64Buf", &m.Tree64Buf, nil, wgpu.BufferUsageStorage, 64) {
		recreated = true
	}

	normalBakeContext := m.prepareVoxelNormalBakeContext(scene)

	m.serviceVoxelUploads(scene, func(work voxelUploadWork) bool {
		return m.executeVoxelUpload(normalBakeContext, work)
	})
	// Upload completion can assign new sectors, aux records and payload slots.
	activeMaps := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		activeMaps[target.mapRef] = true
	}
	m.evictRetainedVoxelMaps(activeMaps)
	m.voxelGPUWorkStats.Pending = m.voxelGrowth != nil
	m.refreshVoxelGPUAdmissionStats(m.currentVoxelGPUResources())

	return recreated
}

// Plan capacity from structural inputs without assigning slots or changing maps.
// Clean allocated maps retain their current capacity and need no sector walk.
func (m *GpuBufferManager) voxelAllocationRequirements(scene *core.Scene) (requiredSectors, requiredBricks uint32) {
	if m == nil {
		return 0, 0
	}
	m.VoxelCapacityPlanningSectorVisitsLastUpdate = 0
	requiredSectors = m.SectorAlloc.Tail
	ranges := m.brickRanges.clone()
	if !m.brickRangesInitialized {
		ranges.tail = uint64(m.BrickAlloc.Tail) * 64
		for _, info := range m.SectorToInfo {
			if info.packed == nil {
				ranges.tail = max(ranges.tail, uint64(info.BrickTableIndex)+64)
			}
		}
	}
	plan := voxelAdmissionPlan{recordRanges: ranges, packedReservations: make(map[*volume.Sector]plannedBrickRange)}
	requiredBricks = uint32(min(ranges.tail, uint64(^uint32(0))))
	if scene == nil {
		return requiredSectors, requiredBricks
	}
	var newSectors uint32
	seenMaps := make(map[*volume.XBrickMap]bool)
	seenSectors := make(map[*volume.Sector]bool)
	for _, target := range voxelServiceTargets(scene) {
		xbm := target.mapRef
		alloc, exists := m.Allocations[xbm]
		if seenMaps[xbm] {
			continue
		}
		seenMaps[xbm] = true
		plan.reservePackedMap(m, xbm)
		if exists && !xbm.StructureDirty {
			continue
		}
		for sKey, sector := range xbm.Sectors {
			m.VoxelCapacityPlanningSectorVisitsLastUpdate++
			if alloc == nil || alloc.Sectors[sKey] != sector {
				if _, hasInfo := m.SectorToInfo[sector]; !hasInfo && !seenSectors[sector] {
					seenSectors[sector] = true
					newSectors++
				}
			}
		}
	}
	return requiredSectors + newSectors, uint32(min(plan.recordRanges.tail, uint64(^uint32(0))))
}

func (m *GpuBufferManager) prepareVoxelStructureDirtyState(scene *core.Scene) {
	if m == nil || scene == nil {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	topologyChanged := false
	if m.Allocations == nil {
		m.Allocations = make(map[*volume.XBrickMap]*ObjectGpuAllocation)
	}
	if m.SectorToInfo == nil {
		m.SectorToInfo = make(map[*volume.Sector]SectorGpuInfo)
	}
	if m.BrickToSlot == nil {
		m.BrickToSlot = make(map[*volume.Brick]PayloadSlot)
	}
	if m.BrickToAuxSlot == nil {
		m.BrickToAuxSlot = make(map[*volume.Brick]uint32)
	}
	m.voxelPreparationSectors = make(map[*volume.Sector]bool)
	m.voxelPreparationBricks = make(map[*volume.Brick]bool)
	defer func() { m.voxelPreparationSectors = nil; m.voxelPreparationBricks = nil }()
	preparationMaps := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		xbm := target.mapRef
		if preparationMaps[xbm] {
			continue
		}
		preparationMaps[xbm] = true
		if !m.voxelMapAdmitted(xbm) {
			continue
		}
		if m.Allocations[xbm] != nil && !xbm.StructureDirty {
			continue
		}
		for _, sector := range xbm.Sectors {
			m.voxelPreparationSectors[sector] = true
			if info := m.SectorToInfo[sector]; info.packed != nil {
				for _, brick := range info.packed.pointers {
					if brick != nil {
						m.voxelPreparationBricks[brick] = true
					}
				}
			}
		}
	}
	seenMaps := make(map[*volume.XBrickMap]bool)
	var changedMaps []*volume.XBrickMap
	for _, target := range voxelServiceTargets(scene) {
		if !m.voxelMapAdmitted(target.mapRef) {
			continue
		}
		if seenMaps[target.mapRef] {
			continue
		}
		xbm := target.mapRef
		seenMaps[xbm] = true
		alloc, exists := m.Allocations[xbm]
		if !exists {
			alloc = &ObjectGpuAllocation{
				Sectors: make(map[[3]int]*volume.Sector),
				Bricks:  make(map[[3]int]*[64]*volume.Brick),
				DirectLookup: directSectorLookupMetadata{
					TableBase: DirectSectorLookupInvalid,
				},
			}
			m.Allocations[xbm] = alloc
			m.trackVoxelAllocation(xbm, alloc)
			if m.voxelAdmissionActive {
				alloc.lookupAdmissionKnown = true
				alloc.lookupAdmitted = m.voxelLookupMaps[xbm]
			}
		}
		if !xbm.StructureDirty && exists {
			continue
		}
		m.markRetainedVoxelMapAccountingDirty(xbm)

		// 1. Detect removed sectors or pointer changes.
		for k, oldSector := range alloc.Sectors {
			newSector, stillExists := xbm.Sectors[k]
			if stillExists && newSector == oldSector {
				continue
			}
			topologyChanged = true
			pointers := alloc.Bricks[k]
			delete(alloc.Sectors, k)
			delete(alloc.Bricks, k)
			if info := m.SectorToInfo[oldSector]; info.packed != nil {
				delete(info.packed.owners, packedSectorOwner{xbm, k})
			}
			m.removeVoxelSnapshotEdges(oldSector, pointers)
			m.releaseUnreferencedSector(oldSector)
			m.releaseUnreferencedBricks(pointers)
		}

		changedMaps = append(changedMaps, xbm)
	}
	// Every safe removal precedes every addition, matching capacity reuse even
	// when scene insertion order differs from deterministic admission priority.
	for _, xbm := range changedMaps {
		alloc := m.Allocations[xbm]

		// 2. Identify new sectors and mark their bricks dirty before cross-object
		// normal halo propagation runs.
		for sKey, sector := range xbm.Sectors {
			if _, ok := alloc.Sectors[sKey]; ok {
				continue
			}
			topologyChanged = true
			info, hasInfo := m.SectorToInfo[sector]
			if !hasInfo {
				sSlot := m.SectorAlloc.Alloc()
				state := &packedSectorRange{owners: make(map[packedSectorOwner]bool)}
				info = SectorGpuInfo{
					pending:   true,
					SlotIndex: sSlot,
					packed:    state,
				}
				m.SectorToInfo[sector] = info
			}
			alloc.Sectors[sKey] = sector
			alloc.Bricks[sKey] = &[64]*volume.Brick{}
			if info.packed != nil {
				info.packed.owners[packedSectorOwner{xbm, sKey}] = true
				if info.packed.published {
					*alloc.Bricks[sKey] = info.packed.pointers
					if !m.voxelOwnership.legacy {
						for _, b := range info.packed.pointers {
							if b != nil {
								m.voxelOwnership.bricks[b]++
							}
						}
					}
				}
			}
			if !m.voxelOwnership.legacy {
				m.voxelOwnership.sectors[sector]++
			}
			xbm.DirtySectors[sKey] = true
			for bz := 0; bz < volume.SectorBricks; bz++ {
				for by := 0; by < volume.SectorBricks; by++ {
					for bx := 0; bx < volume.SectorBricks; bx++ {
						xbm.DirtyBricks[[6]int{sKey[0], sKey[1], sKey[2], bx, by, bz}] = true
					}
				}
			}
		}
		xbm.StructureDirty = false
		alloc.directCells = directSectorLookupCells(alloc.Sectors)
		alloc.directCellsValid = true
	}
	if topologyChanged {
		m.sectorTopologyRevision++
	}
}

func (m *GpuBufferManager) ensureRetainedVoxelMaps() {
	if m == nil {
		return
	}
	if m.retainedVoxelMaps == nil {
		m.retainedVoxelMaps = make(map[*volume.XBrickMap]*retainedVoxelMapEntry)
	}
}

func (m *GpuBufferManager) ActivateRetainedVoxelMap(xbm *volume.XBrickMap) bool {
	if m == nil || xbm == nil {
		return false
	}
	m.ensureRetainedVoxelMaps()
	stamp := m.nextRetainedVoxelMapUse()
	m.retainedVoxelMapStats.Activations++
	if entry := m.retainedVoxelMaps[xbm]; entry != nil {
		m.touchRetainedVoxelMap(entry, stamp)
		if _, ok := m.Allocations[xbm]; ok {
			m.retainedVoxelMapStats.Hits++
			return true
		}
		m.retainedVoxelMapStats.Misses++
		return false
	}
	m.retainedVoxelMapStats.Misses++
	return false
}

func (m *GpuBufferManager) RetainVoxelMap(xbm *volume.XBrickMap) bool {
	if m == nil || xbm == nil {
		return false
	}
	m.ensureRetainedVoxelMaps()
	stamp := m.nextRetainedVoxelMapUse()
	m.retainedVoxelMapStats.RetainRequests++
	allocated := false
	if _, ok := m.Allocations[xbm]; ok {
		allocated = true
		m.retainedVoxelMapStats.RetainRequestsAllocated++
	}
	entry := m.retainedVoxelMaps[xbm]
	if entry == nil {
		entry = &retainedVoxelMapEntry{mapRef: xbm, heapIndex: -1}
		m.retainedVoxelMaps[xbm] = entry
	}
	entry.SectorCount = len(xbm.Sectors)
	m.touchRetainedVoxelMap(entry, stamp)
	m.makeRetainedVoxelMapInactive(entry)
	entry.Bytes = m.retainedVoxelMapBytes(xbm)
	entry.AccountingDirty = false
	return allocated
}

func (m *GpuBufferManager) ReleaseRetainedVoxelMap(xbm *volume.XBrickMap) {
	if m == nil || xbm == nil || len(m.retainedVoxelMaps) == 0 {
		return
	}
	m.removeRetainedVoxelMapEntry(xbm)
	m.retainedVoxelMapPruned = true
	if len(m.retainedVoxelMaps) == 0 {
		m.compactRetainedVoxelMaps()
	}
}

func (m *GpuBufferManager) RetainedVoxelMapStats() RetainedVoxelMapStats {
	if m == nil {
		return RetainedVoxelMapStats{}
	}
	stats := m.retainedVoxelMapStats
	stats.Entries, stats.Sectors = 0, 0
	stats.Bytes, stats.PinnedBytes, stats.MaxBytes, stats.PressureBytes = 0, 0, 0, 0
	if m.RetainedVoxelMapBudgetBytes > 0 {
		stats.MaxBytes = uint64(m.RetainedVoxelMapBudgetBytes)
	}
	for _, entry := range m.retainedVoxelMaps {
		if entry == nil {
			continue
		}
		stats.Entries++
		stats.Sectors += entry.SectorCount
		stats.Bytes = addRetainedVoxelBytes(stats.Bytes, entry.Bytes)
		if entry.Pinned {
			stats.PinnedBytes = addRetainedVoxelBytes(stats.PinnedBytes, entry.Bytes)
		}
	}
	if stats.MaxBytes != 0 && stats.PinnedBytes > stats.MaxBytes {
		stats.PressureBytes = stats.PinnedBytes - stats.MaxBytes
	}
	return stats
}

const objectParamsSizeBytes = 128

func buildObjectParamsBytes(obj *core.VoxelObject, alloc *ObjectGpuAllocation, matAlloc *MaterialGpuAllocation) []byte {
	pBuf := make([]byte, objectParamsSizeBytes)
	writeObjectParamsData(pBuf, obj, alloc, matAlloc)
	return pBuf
}

func maxMaterialSlots(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func (m *GpuBufferManager) releaseBrickSlot(brick *volume.Brick) {
	slot, exists := m.BrickToSlot[brick]
	if !exists {
		return
	}
	delete(m.BrickToSlot, brick)
	if slot.Page >= m.VoxelPayloadPageCount {
		return
	}
	m.PayloadAlloc[slot.Page].FreeSlot(slot.Slot)
}

func (m *GpuBufferManager) releaseVoxelMapAllocation(xbm *volume.XBrickMap, alloc *ObjectGpuAllocation) {
	if m == nil || xbm == nil || alloc == nil {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	if !m.voxelOwnership.legacy {
		if m.Allocations[xbm] != alloc || !m.ownsVoxelAllocation(xbm, alloc) {
			m.invalidateVoxelOwnership()
		} else {
			// Detach all this map's edges before checking surviving owners.
			for _, sector := range alloc.Sectors {
				decrementVoxelReference(m.voxelOwnership.sectors, sector)
			}
			for _, pointers := range alloc.Bricks {
				m.removeVoxelBrickEdges(pointers)
			}
			delete(m.voxelOwnership.headers, xbm)
		}
	}
	// Remove this owner before checking the surviving allocation snapshots.
	delete(m.Allocations, xbm)
	for sKey, sector := range alloc.Sectors {
		if info := m.SectorToInfo[sector]; info.packed != nil {
			delete(info.packed.owners, packedSectorOwner{xbm, sKey})
		}
		m.releaseUnreferencedSector(sector)
		m.releaseUnreferencedBricks(alloc.Bricks[sKey])
	}
	if _, retained := m.retainedVoxelMaps[xbm]; retained {
		m.retainedVoxelMapPruned = true
	}
	m.removeRetainedVoxelMapEntry(xbm)
}

func (m *GpuBufferManager) releaseUnreferencedSector(sector *volume.Sector) {
	if m.voxelPreparationSectors[sector] {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	if !m.voxelOwnership.legacy {
		if m.voxelOwnership.sectors[sector] != 0 {
			return
		}
	} else {
		for _, alloc := range m.Allocations {
			for _, reference := range alloc.Sectors {
				if reference == sector {
					return
				}
			}
		}
	}
	if info, ok := m.SectorToInfo[sector]; ok {
		m.SectorAlloc.FreeSlot(info.SlotIndex)
		if info.packed != nil {
			m.retireBrickRange(info.packed.base, info.packed.capacity)
		} else {
			m.BrickAlloc.FreeSlot(info.BrickTableIndex / 64)
		}
		delete(m.SectorToInfo, sector)
	}
}

func (m *GpuBufferManager) voxelBrickReferenced(brick *volume.Brick) bool {
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	if !m.voxelOwnership.legacy && brick != nil {
		return m.voxelOwnership.bricks[brick] != 0
	}
	for _, alloc := range m.Allocations {
		for _, pointers := range alloc.Bricks {
			if pointers == nil {
				continue
			}
			for _, reference := range pointers {
				if reference == brick {
					return true
				}
			}
		}
	}
	return false
}

func (m *GpuBufferManager) releaseUnreferencedBricks(pointers *[64]*volume.Brick) {
	if pointers == nil {
		return
	}
	m.beginVoxelOwnership()
	defer m.endVoxelOwnership()
	for _, brick := range pointers {
		if brick != nil && !m.voxelPreparationBricks[brick] && !m.voxelBrickReferenced(brick) {
			m.releaseBrickSlot(brick)
			m.releaseVoxelAuxSlot(brick)
		}
	}
}

func (m *GpuBufferManager) releaseVoxelAuxSlot(brick *volume.Brick) {
	slot, exists := m.BrickToAuxSlot[brick]
	if !exists {
		return
	}
	delete(m.BrickToAuxSlot, brick)
	m.VoxelAuxAlloc.FreeSlot(slot)
}

func voxelAuxWordBase(slot uint32) uint32 {
	return slot * volume.VoxelAuxWordCount
}

func brickOriginForSectorIndex(sKey [3]int, brickIdx int) [3]int {
	bx, by, bz := brickIdx%4, (brickIdx/4)%4, brickIdx/16
	return [3]int{
		sKey[0]*volume.SectorSize + bx*volume.BrickSize,
		sKey[1]*volume.SectorSize + by*volume.BrickSize,
		sKey[2]*volume.SectorSize + bz*volume.BrickSize,
	}
}

type brickUploadMode struct {
	usesPayload     bool
	usesAux         bool
	isUniformSparse bool
}

type gpuBrickRecord struct {
	materialIndex    uint32
	payloadOffset    uint32
	occupancyMaskLo  uint32
	occupancyMaskHi  uint32
	payloadPage      uint32
	flags            uint32
	voxelAuxWordBase uint32
}

func resolveBrickUploadMode(flags uint32) brickUploadMode {
	if flags&volume.BrickFlagSolid != 0 {
		return brickUploadMode{usesAux: true}
	}
	if flags&volume.BrickFlagUniformMaterial != 0 {
		return brickUploadMode{usesAux: true, isUniformSparse: true}
	}
	return brickUploadMode{usesPayload: true, usesAux: true}
}

func buildGpuBrickRecord(brick *volume.Brick, mode brickUploadMode, payloadOffset, payloadPage, auxWordBase uint32) gpuBrickRecord {
	record := gpuBrickRecord{
		occupancyMaskLo:  uint32(brick.OccupancyMask64),
		occupancyMaskHi:  uint32(brick.OccupancyMask64 >> 32),
		flags:            brick.Flags,
		voxelAuxWordBase: auxWordBase,
	}
	if mode.usesPayload {
		record.payloadOffset = payloadOffset
		record.payloadPage = payloadPage
	} else {
		record.materialIndex = brick.AtlasOffset
	}
	return record
}

func encodeGpuBrickRecord(record gpuBrickRecord) []byte {
	buf := make([]byte, BrickRecordSize)
	binary.LittleEndian.PutUint32(buf[0:4], record.materialIndex)
	binary.LittleEndian.PutUint32(buf[4:8], record.payloadOffset)
	binary.LittleEndian.PutUint32(buf[8:12], record.occupancyMaskLo)
	binary.LittleEndian.PutUint32(buf[12:16], record.occupancyMaskHi)
	binary.LittleEndian.PutUint32(buf[16:20], record.payloadPage)
	binary.LittleEndian.PutUint32(buf[20:24], record.flags)
	binary.LittleEndian.PutUint32(buf[24:28], record.voxelAuxWordBase)
	return buf
}

func (m *GpuBufferManager) recordVoxelUploadStats(mode brickUploadMode) {
	if mode.usesPayload {
		m.VoxelPayloadSparseBricks++
		return
	}
	if mode.isUniformSparse {
		m.VoxelUniformSparseBricks++
		m.VoxelPayloadUploadsSkipped++
		m.VoxelPayloadBytesAvoided += payloadBytesPerBrick
	}
}

func (m *GpuBufferManager) writeSectorRecord(coords [3]int, mask uint64, info SectorGpuInfo) {
	sData := make([]byte, 32)
	ox, oy, oz := int32(coords[0]*32), int32(coords[1]*32), int32(coords[2]*32)
	binary.LittleEndian.PutUint32(sData[0:4], uint32(ox))
	binary.LittleEndian.PutUint32(sData[4:8], uint32(oy))
	binary.LittleEndian.PutUint32(sData[8:12], uint32(oz))
	binary.LittleEndian.PutUint32(sData[12:16], 0) // padding

	binary.LittleEndian.PutUint32(sData[16:20], info.BrickTableIndex)
	binary.LittleEndian.PutUint32(sData[20:24], uint32(mask))
	binary.LittleEndian.PutUint32(sData[24:28], uint32(mask>>32))
	if info.packed != nil {
		binary.LittleEndian.PutUint32(sData[28:32], 1)
	}

	mustQueueVoxelWrite(m.writeVoxelBuffer(m.SectorTableBuf, uint64(info.SlotIndex)*32, sData))
}

func (m *GpuBufferManager) uploadBrick(context func() voxelNormalBakeContext, obj *core.VoxelObject, target *volume.XBrickMap, brick *volume.Brick, slotIdx uint32, brickOrigin [3]int) {
	if brick == nil {
		return
	}
	mode := resolveBrickUploadMode(brick.Flags)
	m.recordVoxelUploadStats(mode)
	var payloadOffset uint32
	var payloadPage uint32
	auxWordBase := VoxelAuxInvalidWordBase
	if mode.usesPayload {
		payloadSlot, exists := m.BrickToSlot[brick]
		if !exists {
			var ok bool
			payloadSlot, ok = m.allocPayloadSlot()
			if !ok {
				panic(fmt.Sprintf("voxel payload atlas full: pages=%d bricks_per_page=%d total_capacity=%d", m.VoxelPayloadPageCount, m.voxelPayloadCapacityPerPage(), m.voxelPayloadCapacityPerPage()*m.VoxelPayloadPageCount))
			}
			m.BrickToSlot[brick] = payloadSlot
		}
		payloadPage = payloadSlot.Page

		// Calculate 3D coordinates in the atlas
		ax := (payloadSlot.Slot % m.VoxelPayloadBricks) * volume.BrickSize
		ay := ((payloadSlot.Slot / m.VoxelPayloadBricks) % m.VoxelPayloadBricks) * volume.BrickSize
		az := (payloadSlot.Slot / (m.VoxelPayloadBricks * m.VoxelPayloadBricks)) * volume.BrickSize

		payloadOffset = packVoxelAtlasOffset(ax, ay, az)

		// Upload payload via WriteTexture
		payload := make([]byte, 512)
		idx := 0
		for z := 0; z < 8; z++ {
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					payload[idx] = brick.VoxelValue(x, y, z)
					idx++
				}
			}
		}

		mustQueueVoxelWrite(m.voxelNative.WritePayload(payloadPage, [3]uint32{uint32(ax), uint32(ay), uint32(az)}, payload))
	}

	if mode.usesAux {
		auxSlot, exists := m.BrickToAuxSlot[brick]
		if !exists {
			auxSlot = m.VoxelAuxAlloc.Alloc()
			m.BrickToAuxSlot[brick] = auxSlot
		}
		auxWordBase = voxelAuxWordBase(auxSlot)
		var auxBytes []byte
		if len(brick.PrecomputedAux) == VoxelAuxRecordBytes {
			auxBytes = brick.PrecomputedAux
		} else {
			start := time.Now()
			if target == obj.RenderVoxelMap() {
				auxBytes = buildVoxelAuxBytesWithContext(context, obj, brick, brickOrigin)
			} else {
				auxBytes = buildVoxelAuxBytesForTarget(voxelNormalBakeContext{}, obj, target, brick, brickOrigin)
			}
			m.VoxelRuntimeNormalBakeDuration += time.Since(start)
		}
		mustQueueVoxelWrite(m.writeVoxelBuffer(m.DenseOccupancyBuf, uint64(auxSlot)*VoxelAuxRecordBytes, auxBytes))
	} else {
		// Complete-unit reclamation owns releases, including alias guards.
	}

	record := buildGpuBrickRecord(brick, mode, payloadOffset, payloadPage, auxWordBase)
	bbuf := encodeGpuBrickRecord(record)
	mustQueueVoxelWrite(m.writeVoxelBuffer(m.BrickTableBuf, uint64(slotIdx)*BrickRecordSize, bbuf))
}

func (m *GpuBufferManager) ensureVoxelPayloadPages() bool {
	recreated := false
	for i := uint32(0); i < m.VoxelPayloadPageCount; i++ {
		if m.VoxelPayloadTex[i] != nil {
			continue
		}
		fmt.Printf("Initializing Voxel Atlas Texture Page %d: %dx%dx%d\n", i, m.VoxelPayloadPageSize, m.VoxelPayloadPageSize, m.VoxelPayloadPageSize)
		tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label: fmt.Sprintf("VoxelPayloadAtlas%d", i),
			Size: wgpu.Extent3D{
				Width:              m.VoxelPayloadPageSize,
				Height:             m.VoxelPayloadPageSize,
				DepthOrArrayLayers: m.VoxelPayloadPageSize,
			},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     wgpu.TextureDimension3D,
			Format:        wgpu.TextureFormatR8Uint,
			Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		})
		if err != nil {
			panic(err)
		}
		view, err := tex.CreateView(nil)
		if err != nil {
			panic(err)
		}
		m.VoxelPayloadTex[i] = tex
		m.VoxelPayloadView[i] = view
		recreated = true
	}
	return recreated
}

func (m *GpuBufferManager) voxelPayloadCapacityPerPage() uint32 {
	return m.VoxelPayloadBricks * m.VoxelPayloadBricks * m.VoxelPayloadBricks
}

func (m *GpuBufferManager) allocPayloadSlot() (PayloadSlot, bool) {
	capacity := m.voxelPayloadCapacityPerPage()
	for page := uint32(0); page < m.VoxelPayloadPageCount; page++ {
		alloc := &m.PayloadAlloc[page]
		if len(alloc.Free) > 0 || alloc.Tail < capacity {
			return PayloadSlot{Page: page, Slot: alloc.Alloc()}, true
		}
	}
	return PayloadSlot{}, false
}

func packVoxelAtlasOffset(ax, ay, az uint32) uint32 {
	return (ax << 20) | (ay << 10) | az
}
