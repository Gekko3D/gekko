package gpu

import (
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/cogentcore/webgpu/wgpu"
)

const materialBlockCapacity = 256

func materialTableHasTransparency(table []core.Material) bool {
	for _, mat := range table {
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
	m.VoxelSectorsUploaded = 0
	m.VoxelBricksUploaded = 0
	m.VoxelDirtySectorsPending = 0
	m.VoxelDirtyBricksPending = 0

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
	for xbm, alloc := range m.Allocations {
		if !activeMaps[xbm] {
			// Free slots based on what was ACTUALLY allocated to the GPU,
			// not the current CPU state (which could have been cleared)
			for sKey, sector := range alloc.Sectors {
				if info, ok := m.SectorToInfo[sector]; ok {
					m.SectorAlloc.FreeSlot(info.SlotIndex)
					m.BrickAlloc.FreeSlot(info.BrickTableIndex / 64)
					delete(m.SectorToInfo, sector)
				}
				// Free bricks payloads
				if bPtrs, has := alloc.Bricks[sKey]; has {
					for i := 0; i < 64; i++ {
						if brick := bPtrs[i]; brick != nil {
							m.releaseBrickSlot(brick)
						}
					}
				}
			}
			delete(m.Allocations, xbm)
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

	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		alloc, exists := m.Allocations[xbm]
		if !exists {
			alloc = &ObjectGpuAllocation{
				Sectors: make(map[[3]int]*volume.Sector),
				Bricks:  make(map[[3]int]*[64]*volume.Brick),
			}
			m.Allocations[xbm] = alloc
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

		if xbm.StructureDirty || !exists {
			// 1. Detect removed sectors or pointer changes
			for k, oldSector := range alloc.Sectors {
				newSector, stillExists := xbm.Sectors[k]
				if !stillExists || newSector != oldSector {
					// Sector removed or replaced at this coordinate
					if info, ok := m.SectorToInfo[oldSector]; ok {
						// Free payload slots for all bricks in this sector
						if bPtrs, has := alloc.Bricks[k]; has {
							for i := 0; i < 64; i++ {
								if brick := bPtrs[i]; brick != nil {
									m.releaseBrickSlot(brick)
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
			}

			// 2. Identify new sectors
			for sKey, sector := range xbm.Sectors {
				if _, ok := alloc.Sectors[sKey]; !ok {
					// New sector at this coordinate (or replacement)
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
					alloc.Bricks[sKey] = &[64]*volume.Brick{} // New tracking entry
					// Force upload for new sectors and ALL their bricks
					xbm.DirtySectors[sKey] = true
					for bz := 0; bz < 4; bz++ {
						for by := 0; by < 4; by++ {
							for bx := 0; bx < 4; bx++ {
								xbm.DirtyBricks[[6]int{sKey[0], sKey[1], sKey[2], bx, by, bz}] = true
							}
						}
					}
				}
			}
			xbm.StructureDirty = false
		}
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
					m.uploadBrick(brick, info.BrickTableIndex+uint32(i))
				} else {
					// Clear brick record in GPU to 0
					if bPtrs, has := alloc.Bricks[sKey]; has {
						if oldBrick := bPtrs[i]; oldBrick != nil {
							m.releaseBrickSlot(oldBrick)
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
					}
					bPtrs[bx+by*4+bz*16] = brick
				}
				m.uploadBrick(brick, info.BrickTableIndex+uint32(bx+by*4+bz*16))
			} else {
				// Clear brick record in GPU to 0
				if hasPtrs {
					if oldBrick := bPtrs[bx+by*4+bz*16]; oldBrick != nil {
						m.releaseBrickSlot(oldBrick)
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

const objectParamsSizeBytes = 96

func buildObjectParamsBytes(obj *core.VoxelObject, alloc *ObjectGpuAllocation, matAlloc *MaterialGpuAllocation) []byte {
	pBuf := make([]byte, objectParamsSizeBytes)
	if obj == nil || obj.XBrickMap == nil || alloc == nil {
		return pBuf
	}
	// sector_table_base is used as the stable map ID for hash lookup.
	binary.LittleEndian.PutUint32(pBuf[0:4], obj.XBrickMap.ID)
	binary.LittleEndian.PutUint32(pBuf[4:8], 0) // brick_table_base - now internal to sector
	binary.LittleEndian.PutUint32(pBuf[8:12], 0)
	if matAlloc != nil {
		binary.LittleEndian.PutUint32(pBuf[12:16], matAlloc.MaterialOffset*4)
	}
	binary.LittleEndian.PutUint32(pBuf[16:20], ^uint32(0))
	binary.LittleEndian.PutUint32(pBuf[20:24], math.Float32bits(obj.LODThreshold))
	binary.LittleEndian.PutUint32(pBuf[24:28], uint32(len(obj.XBrickMap.Sectors)))
	binary.LittleEndian.PutUint32(pBuf[28:32], uint32(obj.AmbientOcclusionMode))
	binary.LittleEndian.PutUint32(pBuf[32:36], obj.ShadowGroupID)
	binary.LittleEndian.PutUint32(pBuf[36:40], math.Float32bits(obj.ShadowSeamWorldEpsilon))
	if obj.IsTerrainChunk {
		binary.LittleEndian.PutUint32(pBuf[40:44], 1)
	}
	binary.LittleEndian.PutUint32(pBuf[44:48], obj.TerrainGroupID)
	binary.LittleEndian.PutUint32(pBuf[48:52], uint32(obj.TerrainChunkCoord[0]))
	binary.LittleEndian.PutUint32(pBuf[52:56], uint32(obj.TerrainChunkCoord[1]))
	binary.LittleEndian.PutUint32(pBuf[56:60], uint32(obj.TerrainChunkCoord[2]))
	binary.LittleEndian.PutUint32(pBuf[60:64], uint32(obj.TerrainChunkSize))
	if obj.IsPlanetTile {
		binary.LittleEndian.PutUint32(pBuf[64:68], 1)
	}
	binary.LittleEndian.PutUint32(pBuf[68:72], obj.PlanetTileGroupID)
	binary.LittleEndian.PutUint32(pBuf[72:76], obj.EmitterLinkID)
	binary.LittleEndian.PutUint32(pBuf[80:84], uint32(obj.PlanetTileFace))
	binary.LittleEndian.PutUint32(pBuf[84:88], uint32(obj.PlanetTileLevel))
	binary.LittleEndian.PutUint32(pBuf[88:92], uint32(obj.PlanetTileX))
	binary.LittleEndian.PutUint32(pBuf[92:96], uint32(obj.PlanetTileY))
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

func (m *GpuBufferManager) uploadBrick(brick *volume.Brick, slotIdx uint32) {
	if brick == nil {
		return
	}
	var gpuAtlasOffset uint32
	var gpuAtlasPage uint32
	if brick.Flags&volume.BrickFlagSolid != 0 {
		gpuAtlasOffset = brick.AtlasOffset
		gpuAtlasPage = 0
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
		gpuAtlasPage = payloadSlot.Page

		// Calculate 3D coordinates in the atlas
		ax := (payloadSlot.Slot % m.VoxelPayloadBricks) * volume.BrickSize
		ay := ((payloadSlot.Slot / m.VoxelPayloadBricks) % m.VoxelPayloadBricks) * volume.BrickSize
		az := (payloadSlot.Slot / (m.VoxelPayloadBricks * m.VoxelPayloadBricks)) * volume.BrickSize

		gpuAtlasOffset = packVoxelAtlasOffset(ax, ay, az)

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
				Texture:  m.VoxelPayloadTex[gpuAtlasPage],
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

	bbuf := make([]byte, BrickRecordSize)
	binary.LittleEndian.PutUint32(bbuf[0:4], gpuAtlasOffset)
	binary.LittleEndian.PutUint32(bbuf[4:8], uint32(brick.OccupancyMask64))
	binary.LittleEndian.PutUint32(bbuf[8:12], uint32(brick.OccupancyMask64>>32))
	binary.LittleEndian.PutUint32(bbuf[12:16], gpuAtlasPage)
	binary.LittleEndian.PutUint32(bbuf[16:20], brick.Flags)
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
