package gpu

import (
	"encoding/binary"
	"fmt"
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
	recreated := false
	uploadedAny := false
	materialBufRecreated := false
	m.ensureRetainedVoxelMaps()
	m.VoxelSectorsUploaded = 0
	m.VoxelBricksUploaded = 0
	m.VoxelDirtySectorsPending = 0
	m.VoxelDirtyBricksPending = 0
	m.VoxelUniformSparseBricks = 0
	m.VoxelPayloadSparseBricks = 0
	m.VoxelPayloadUploadsSkipped = 0
	m.VoxelPayloadBytesAvoided = 0
	// Cleanup orphan allocations
	activeMaps := make(map[*volume.XBrickMap]bool)
	activeObjects := make(map[*core.VoxelObject]bool)
	for _, obj := range scene.Objects {
		if obj == nil {
			continue
		}
		activeObjects[obj] = true
		activeMaps[obj.XBrickMap] = true
	}
	m.evictRetainedVoxelMaps(activeMaps)
	for xbm, alloc := range m.Allocations {
		if !activeMaps[xbm] {
			if _, retain := m.retainedVoxelMaps[xbm]; retain {
				continue
			}
			m.releaseVoxelMapAllocation(xbm, alloc)
		}
	}
	for obj, alloc := range m.MaterialAllocations {
		if !activeObjects[obj] {
			if alloc != nil && alloc.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(alloc.MaterialOffset / 256)
			}
			delete(m.MaterialAllocations, obj)
		}
	}

	requiredSectors := m.SectorAlloc.Tail
	requiredBricks := m.BrickAlloc.Tail * 64

	// 1. Pre-scan: Count how many NEW allocations we need in this frame
	// Only scan if any object has structural changes or is new.
	needsScan := false
	for _, obj := range scene.Objects {
		if obj.XBrickMap == nil {
			continue
		}
		_, exists := m.Allocations[obj.XBrickMap]
		if !exists || obj.XBrickMap.StructureDirty {
			needsScan = true
			break
		}
	}

	if needsScan {
		newSectors := 0
		for _, obj := range scene.Objects {
			xbm := obj.XBrickMap
			if xbm == nil {
				continue
			}
			alloc := m.Allocations[xbm]
			for sKey, sector := range xbm.Sectors {
				if alloc == nil || alloc.Sectors[sKey] != sector {
					if _, hasInfo := m.SectorToInfo[sector]; !hasInfo {
						newSectors++
					}
				}
			}
		}

		// Update Tails if we have brand new allocations coming
		requiredSectors = m.SectorAlloc.Tail + uint32(newSectors)
		requiredBricks = m.BrickAlloc.Tail*64 + uint32(newSectors)*64
	}

	// Ensure all global buffers exist (even if empty) to avoid bind group panics.
	// We use the calculated requirements if needsScan was true, otherwise current tail.
	// Headroom: Reduced frequency of full-buffer reallocations by using larger buffers initially and geometric growth.
	if m.ensureBuffer("SectorTableBuf", &m.SectorTableBuf, nil, wgpu.BufferUsageStorage, int(requiredSectors+512)*32) {
		recreated = true
	}
	if m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, nil, wgpu.BufferUsageStorage, int(requiredBricks+2048)*BrickRecordSize) {
		recreated = true
	}
	if m.ensureBuffer("DenseOccupancyBuf", &m.DenseOccupancyBuf, nil, wgpu.BufferUsageStorage, int(requiredBricks+2048)*VoxelAuxRecordBytes) {
		recreated = true
	}
	if m.ensureVoxelPayloadPages() {
		recreated = true
	}
	requiredMaterialBlocks := m.MaterialAlloc.Tail
	if activeCount := uint32(len(activeObjects)); activeCount > requiredMaterialBlocks {
		requiredMaterialBlocks = activeCount
	}
	if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, int(maxMaterialSlots(requiredMaterialBlocks, 1)*materialBlockCapacity*64)) {
		recreated = true
		materialBufRecreated = true
		m.MaterialBufferGeneration++
	}
	if m.ensureBuffer("Tree64Buf", &m.Tree64Buf, nil, wgpu.BufferUsageStorage, 64) {
		recreated = true
	}

	m.prepareVoxelStructureDirtyState(scene)
	normalBakeContext := newVoxelNormalBakeContext(scene)
	markCrossObjectNormalHaloDirty(scene, normalBakeContext)

	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		alloc := m.Allocations[xbm]
		if alloc == nil {
			continue
		}

		// Update per-object materials independently from shared geometry.
		matAlloc, hasMatAlloc := m.MaterialAllocations[obj]
		if !hasMatAlloc {
			matAlloc = &MaterialGpuAllocation{}
			m.MaterialAllocations[obj] = matAlloc
		}
		tablePtr, tableLen := materialTableIdentity(obj.MaterialTable)
		materialCount := uint32(tableLen)
		if materialCount == 0 {
			materialCount = materialBlockCapacity
		}
		if !hasMatAlloc || materialCount > matAlloc.MaterialCapacity {
			if hasMatAlloc && matAlloc.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(matAlloc.MaterialOffset / 256)
			}
			pSlot := m.MaterialAlloc.Alloc()
			matAlloc.MaterialOffset = pSlot * materialBlockCapacity
			matAlloc.MaterialCapacity = materialBlockCapacity
			if materialCount > materialBlockCapacity {
				// Special case: if object needs more than 256 materials, we'd need a multi-block allocator.
				// For now, we cap at 256 as it's the standard for this engine.
				fmt.Printf("WARNING: Object has %d materials, capping to 256\n", materialCount)
			}
		}
		needsMaterialUpload := materialBufRecreated ||
			!hasMatAlloc ||
			matAlloc.BufferGeneration != m.MaterialBufferGeneration ||
			matAlloc.MaterialTablePtr != tablePtr ||
			matAlloc.MaterialTableLen != tableLen

		if needsMaterialUpload {
			materials := buildMaterialData(obj.MaterialTable)
			m.Device.GetQueue().WriteBuffer(m.MaterialBuf, uint64(matAlloc.MaterialOffset*64), materials)
			matAlloc.MaterialTablePtr = tablePtr
			matAlloc.MaterialTableLen = tableLen
			matAlloc.BufferGeneration = m.MaterialBufferGeneration
		}
		matAlloc.HasTransparency = materialTableHasTransparency(obj.MaterialTable)

		// Upload dirty sectors with budgeting
		sectorsInFrame := uint32(0)
		for sKey, isDirty := range xbm.DirtySectors {
			if !isDirty {
				delete(xbm.DirtySectors, sKey)
				continue
			}
			if sectorsInFrame >= m.SectorsPerFrame {
				break
			}
			sector, ok := xbm.Sectors[sKey]
			if !ok {
				delete(xbm.DirtySectors, sKey)
				continue
			}
			info := m.SectorToInfo[sector]
			m.writeSectorRecord(sector, info)

			// Also upload all bricks of this sector if it's considered "new/dirty structure"
			for i := 0; i < 64; i++ {
				if (sector.BrickMask64 & (1 << i)) != 0 {
					bx, by, bz := i%4, (i/4)%4, i/16
					brick := sector.GetBrick(bx, by, bz)
					if bPtrs, has := alloc.Bricks[sKey]; has {
						bPtrs[i] = brick // Sync pointer
					}
					m.uploadBrick(normalBakeContext, obj, brick, info.BrickTableIndex+uint32(i), brickOriginForSectorIndex(sKey, i))
				} else {
					// Clear brick record in GPU to 0
					if bPtrs, has := alloc.Bricks[sKey]; has {
						if oldBrick := bPtrs[i]; oldBrick != nil {
							m.releaseBrickSlot(oldBrick)
							m.releaseVoxelAuxSlot(oldBrick)
						}
						bPtrs[i] = nil
					}
					m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64((info.BrickTableIndex+uint32(i))*BrickRecordSize), make([]byte, BrickRecordSize))
				}
			}
			delete(xbm.DirtySectors, sKey)
			sectorsInFrame++
			m.VoxelSectorsUploaded++
			uploadedAny = true
		}

		// Upload individual dirty bricks (e.g. from small edits)
		maxBricks := m.SectorsPerFrame * 4 // Loose budget for individual bricks
		bricksInFrame := uint32(0)
		for bKey, isDirty := range xbm.DirtyBricks {
			if !isDirty {
				delete(xbm.DirtyBricks, bKey)
				continue
			}
			if bricksInFrame >= maxBricks {
				break
			}
			sx, sy, sz := bKey[0], bKey[1], bKey[2]
			bx, by, bz := bKey[3], bKey[4], bKey[5]
			sector, ok := xbm.Sectors[[3]int{sx, sy, sz}]
			if !ok {
				delete(xbm.DirtyBricks, bKey)
				continue
			}
			info := m.SectorToInfo[sector]
			brick := sector.GetBrick(bx, by, bz)
			bPtrs, hasPtrs := alloc.Bricks[[3]int{sx, sy, sz}]
			if brick != nil {
				if hasPtrs {
					oldBrick := bPtrs[bx+by*4+bz*16]
					if oldBrick != nil && oldBrick != brick {
						// Brick changed! Release old one
						m.releaseBrickSlot(oldBrick)
						m.releaseVoxelAuxSlot(oldBrick)
					}
					bPtrs[bx+by*4+bz*16] = brick
				}
				origin := [3]int{
					sx*volume.SectorSize + bx*volume.BrickSize,
					sy*volume.SectorSize + by*volume.BrickSize,
					sz*volume.SectorSize + bz*volume.BrickSize,
				}
				m.uploadBrick(normalBakeContext, obj, brick, info.BrickTableIndex+uint32(bx+by*4+bz*16), origin)
			} else {
				// Clear brick record in GPU to 0
				if hasPtrs {
					if oldBrick := bPtrs[bx+by*4+bz*16]; oldBrick != nil {
						m.releaseBrickSlot(oldBrick)
						m.releaseVoxelAuxSlot(oldBrick)
					}
					bPtrs[bx+by*4+bz*16] = nil
				}
				m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64((info.BrickTableIndex+uint32(bx+by*4+bz*16))*BrickRecordSize), make([]byte, BrickRecordSize))
			}
			delete(xbm.DirtyBricks, bKey)
			bricksInFrame++
			m.VoxelBricksUploaded++
			uploadedAny = true
		}

		for _, isDirty := range xbm.DirtySectors {
			if isDirty {
				m.VoxelDirtySectorsPending++
			}
		}
		for _, isDirty := range xbm.DirtyBricks {
			if isDirty {
				m.VoxelDirtyBricksPending++
			}
		}

		// Materials: For now, re-upload if new.
	}

	if uploadedAny {
		m.VoxelUploadRevision++
	}

	return recreated
}

func (m *GpuBufferManager) prepareVoxelStructureDirtyState(scene *core.Scene) {
	if m == nil || scene == nil {
		return
	}
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
	for _, obj := range scene.Objects {
		if obj == nil || obj.XBrickMap == nil {
			continue
		}
		xbm := obj.XBrickMap
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
		}
		if !xbm.StructureDirty && exists {
			continue
		}

		// 1. Detect removed sectors or pointer changes.
		for k, oldSector := range alloc.Sectors {
			newSector, stillExists := xbm.Sectors[k]
			if stillExists && newSector == oldSector {
				continue
			}
			if info, ok := m.SectorToInfo[oldSector]; ok {
				if bPtrs, has := alloc.Bricks[k]; has {
					for i := 0; i < 64; i++ {
						if brick := bPtrs[i]; brick != nil {
							m.releaseBrickSlot(brick)
							m.releaseVoxelAuxSlot(brick)
						}
					}
					delete(alloc.Bricks, k)
				}
				m.SectorAlloc.FreeSlot(info.SlotIndex)
				m.BrickAlloc.FreeSlot(info.BrickTableIndex / 64)
				delete(m.SectorToInfo, oldSector)
			}
			delete(alloc.Sectors, k)
		}

		// 2. Identify new sectors and mark their bricks dirty before cross-object
		// normal halo propagation runs.
		for sKey, sector := range xbm.Sectors {
			if _, ok := alloc.Sectors[sKey]; ok {
				continue
			}
			info, hasInfo := m.SectorToInfo[sector]
			if !hasInfo {
				sSlot := m.SectorAlloc.Alloc()
				bSlot := m.BrickAlloc.Alloc()
				info = SectorGpuInfo{
					SlotIndex:       sSlot,
					BrickTableIndex: bSlot * 64,
				}
				m.SectorToInfo[sector] = info
			}
			alloc.Sectors[sKey] = sector
			alloc.Bricks[sKey] = &[64]*volume.Brick{}
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
	m.retainedVoxelMapClock++
	m.retainedVoxelMapStats.Activations++
	if entry := m.retainedVoxelMaps[xbm]; entry != nil {
		entry.LastUse = m.retainedVoxelMapClock
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
	m.retainedVoxelMapClock++
	m.retainedVoxelMapStats.RetainRequests++
	allocated := false
	if _, ok := m.Allocations[xbm]; ok {
		allocated = true
		m.retainedVoxelMapStats.RetainRequestsAllocated++
	}
	entry := m.retainedVoxelMaps[xbm]
	if entry == nil {
		entry = &retainedVoxelMapEntry{}
		m.retainedVoxelMaps[xbm] = entry
	}
	entry.SectorCount = len(xbm.Sectors)
	entry.LastUse = m.retainedVoxelMapClock
	return allocated
}

func (m *GpuBufferManager) ReleaseRetainedVoxelMap(xbm *volume.XBrickMap) {
	if m == nil || xbm == nil || len(m.retainedVoxelMaps) == 0 {
		return
	}
	delete(m.retainedVoxelMaps, xbm)
}

func (m *GpuBufferManager) RetainedVoxelMapStats() RetainedVoxelMapStats {
	if m == nil {
		return RetainedVoxelMapStats{}
	}
	m.ensureRetainedVoxelMaps()
	stats := m.retainedVoxelMapStats
	for _, entry := range m.retainedVoxelMaps {
		if entry == nil {
			continue
		}
		stats.Entries++
		stats.Sectors += entry.SectorCount
	}
	return stats
}

func (m *GpuBufferManager) evictRetainedVoxelMaps(activeMaps map[*volume.XBrickMap]bool) {
	if m == nil || len(m.retainedVoxelMaps) == 0 || m.RetainedVoxelMapBudgetSectors <= 0 {
		return
	}
	for {
		stats := m.RetainedVoxelMapStats()
		if stats.Sectors <= m.RetainedVoxelMapBudgetSectors {
			return
		}
		var victimMap *volume.XBrickMap
		var victimEntry *retainedVoxelMapEntry
		for xbm, entry := range m.retainedVoxelMaps {
			if xbm == nil || entry == nil {
				continue
			}
			if activeMaps != nil && activeMaps[xbm] {
				continue
			}
			if victimEntry == nil || entry.LastUse < victimEntry.LastUse {
				victimMap = xbm
				victimEntry = entry
			}
		}
		if victimMap == nil {
			return
		}
		delete(m.retainedVoxelMaps, victimMap)
		if alloc := m.Allocations[victimMap]; alloc != nil && (activeMaps == nil || !activeMaps[victimMap]) {
			m.releaseVoxelMapAllocation(victimMap, alloc)
		}
		m.retainedVoxelMapStats.Evictions++
	}
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
	// Free slots based on what was actually allocated to the GPU, not the
	// current CPU state, which may already have been cleared or replaced.
	for sKey, sector := range alloc.Sectors {
		if info, ok := m.SectorToInfo[sector]; ok {
			m.SectorAlloc.FreeSlot(info.SlotIndex)
			m.BrickAlloc.FreeSlot(info.BrickTableIndex / 64)
			delete(m.SectorToInfo, sector)
		}
		if bPtrs, has := alloc.Bricks[sKey]; has {
			for i := 0; i < 64; i++ {
				if brick := bPtrs[i]; brick != nil {
					m.releaseBrickSlot(brick)
					m.releaseVoxelAuxSlot(brick)
				}
			}
		}
	}
	delete(m.Allocations, xbm)
	delete(m.retainedVoxelMaps, xbm)
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

func (m *GpuBufferManager) writeSectorRecord(sector *volume.Sector, info SectorGpuInfo) {
	sData := make([]byte, 32)
	ox, oy, oz := int32(sector.Coords[0]*32), int32(sector.Coords[1]*32), int32(sector.Coords[2]*32)
	binary.LittleEndian.PutUint32(sData[0:4], uint32(ox))
	binary.LittleEndian.PutUint32(sData[4:8], uint32(oy))
	binary.LittleEndian.PutUint32(sData[8:12], uint32(oz))
	binary.LittleEndian.PutUint32(sData[12:16], 0) // padding

	binary.LittleEndian.PutUint32(sData[16:20], info.BrickTableIndex)
	binary.LittleEndian.PutUint32(sData[20:24], uint32(sector.BrickMask64))
	binary.LittleEndian.PutUint32(sData[24:28], uint32(sector.BrickMask64>>32))
	// 28:32 padding

	m.Device.GetQueue().WriteBuffer(m.SectorTableBuf, uint64(info.SlotIndex*32), sData)
}

func (m *GpuBufferManager) uploadBrick(ctx voxelNormalBakeContext, obj *core.VoxelObject, brick *volume.Brick, slotIdx uint32, brickOrigin [3]int) {
	if brick == nil {
		return
	}
	mode := resolveBrickUploadMode(brick.Flags)
	m.recordVoxelUploadStats(mode)
	var payloadOffset uint32
	var payloadPage uint32
	auxWordBase := VoxelAuxInvalidWordBase
	if !mode.usesPayload {
		m.releaseBrickSlot(brick)
	} else {
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
					payload[idx] = brick.Payload[x][y][z]
					idx++
				}
			}
		}

		m.Device.GetQueue().WriteTexture(
			&wgpu.ImageCopyTexture{
				Texture:  m.VoxelPayloadTex[payloadPage],
				MipLevel: 0,
				Origin:   wgpu.Origin3D{X: uint32(ax), Y: uint32(ay), Z: uint32(az)},
				Aspect:   wgpu.TextureAspectAll,
			},
			payload,
			&wgpu.TextureDataLayout{
				Offset:       0,
				BytesPerRow:  8,
				RowsPerImage: 8,
			},
			&wgpu.Extent3D{
				Width:              8,
				Height:             8,
				DepthOrArrayLayers: 8,
			},
		)
	}

	if mode.usesAux {
		auxSlot, exists := m.BrickToAuxSlot[brick]
		if !exists {
			auxSlot = m.VoxelAuxAlloc.Alloc()
			m.BrickToAuxSlot[brick] = auxSlot
		}
		auxWordBase = voxelAuxWordBase(auxSlot)
		m.Device.GetQueue().WriteBuffer(m.DenseOccupancyBuf, uint64(auxSlot*VoxelAuxRecordBytes), buildVoxelAuxBytes(ctx, obj, brick, brickOrigin))
	} else {
		m.releaseVoxelAuxSlot(brick)
	}

	record := buildGpuBrickRecord(brick, mode, payloadOffset, payloadPage, auxWordBase)
	bbuf := encodeGpuBrickRecord(record)
	m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64(slotIdx*BrickRecordSize), bbuf)
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
