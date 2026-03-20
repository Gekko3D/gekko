package gpu

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) UpdateVoxelData(scene *core.Scene) bool {
	recreated := false
	var err error

	// Cleanup orphan allocations
	activeMaps := make(map[*volume.XBrickMap]bool)
	for _, obj := range scene.Objects {
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
			// Free material block
			m.MaterialAlloc.FreeSlot(alloc.MaterialOffset / 256)
			delete(m.Allocations, xbm)
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
	if m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, nil, wgpu.BufferUsageStorage, int(requiredBricks+2048)*16) {
		recreated = true
	}
	// Payload is now a 3D Texture
	if m.VoxelPayloadTex == nil {
		fmt.Printf("Initializing Voxel Atlas Texture: %dx%dx%d\n", AtlasSize, AtlasSize, AtlasSize)
		m.VoxelPayloadTex, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label: "VoxelPayloadAtlas",
			Size: wgpu.Extent3D{
				Width:              AtlasSize,
				Height:             AtlasSize,
				DepthOrArrayLayers: AtlasSize,
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
		m.VoxelPayloadView, err = m.VoxelPayloadTex.CreateView(nil)
		if err != nil {
			panic(err)
		}
		recreated = true
	}
	if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, int(m.MaterialAlloc.Tail*256*64)) {
		recreated = true
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

		// Update Materials
		materials := []byte{}
		for _, mat := range obj.MaterialTable {
			materials = append(materials, rgbaToVec4(mat.BaseColor)...)
			materials = append(materials, rgbaToVec4(mat.Emissive)...)
			materials = append(materials, float32ToBytes(mat.Roughness)...)
			materials = append(materials, float32ToBytes(mat.Metalness)...)
			materials = append(materials, float32ToBytes(mat.IOR)...)
			materials = append(materials, float32ToBytes(mat.Transparency)...)
			materials = append(materials, vec4ToBytes([4]float32{mat.Emission, 0.0, 0.0, 0.0})...)
		}
		if len(materials) == 0 {
			materials = make([]byte, 256*64)
		}
		mCount := uint32(len(materials) / 64)
		if !exists || mCount > alloc.MaterialCapacity {
			if exists && alloc.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(alloc.MaterialOffset / 256)
			}
			pSlot := m.MaterialAlloc.Alloc()
			alloc.MaterialOffset = pSlot * 256
			alloc.MaterialCapacity = 256 // Fixed size blocks for simplicity
			if mCount > 256 {
				// Special case: if object needs more than 256 materials, we'd need a multi-block allocator.
				// For now, we cap at 256 as it's the standard for this engine.
				materials = materials[:256*64]
				fmt.Printf("WARNING: Object has %d materials, capping to 256\n", mCount)
			}

			if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, int(m.MaterialAlloc.Tail*256*64)) {
				recreated = true
			}
			m.Device.GetQueue().WriteBuffer(m.MaterialBuf, uint64(alloc.MaterialOffset*64), materials)
		} else {
			// Just upload if modified
			m.Device.GetQueue().WriteBuffer(m.MaterialBuf, uint64(alloc.MaterialOffset*64), materials)
		}

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
					m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64((info.BrickTableIndex+uint32(i))*16), make([]byte, 16))
				}
			}
			delete(xbm.DirtySectors, sKey)
			sectorsInFrame++
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
				m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64((info.BrickTableIndex+uint32(bx+by*4+bz*16))*16), make([]byte, 16))
			}
			delete(xbm.DirtyBricks, bKey)
			bricksInFrame++
		}

		// Materials: For now, re-upload if new.
	}

	// Update ObjectParams for visible objects
	objParams := []byte{}
	for _, obj := range scene.VisibleObjects {
		alloc := m.Allocations[obj.XBrickMap]
		objParams = append(objParams, buildObjectParamsBytes(obj, alloc)...)
	}

	if len(objParams) == 0 {
		objParams = make([]byte, objectParamsSizeBytes)
	}

	if m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, objParams, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	return recreated
}

const objectParamsSizeBytes = 64

func buildObjectParamsBytes(obj *core.VoxelObject, alloc *ObjectGpuAllocation) []byte {
	pBuf := make([]byte, objectParamsSizeBytes)
	if obj == nil || obj.XBrickMap == nil || alloc == nil {
		return pBuf
	}
	// sector_table_base is used as the stable map ID for hash lookup.
	binary.LittleEndian.PutUint32(pBuf[0:4], obj.XBrickMap.ID)
	binary.LittleEndian.PutUint32(pBuf[4:8], 0) // brick_table_base - now internal to sector
	binary.LittleEndian.PutUint32(pBuf[8:12], 0)
	binary.LittleEndian.PutUint32(pBuf[12:16], alloc.MaterialOffset*4)
	binary.LittleEndian.PutUint32(pBuf[16:20], ^uint32(0))
	binary.LittleEndian.PutUint32(pBuf[20:24], math.Float32bits(obj.LODThreshold))
	binary.LittleEndian.PutUint32(pBuf[24:28], uint32(len(obj.XBrickMap.Sectors)))
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
	return pBuf
}

func (m *GpuBufferManager) releaseBrickSlot(brick *volume.Brick) {
	slot, exists := m.BrickToSlot[brick]
	if !exists {
		return
	}
	delete(m.BrickToSlot, brick)
	m.PayloadAlloc.FreeSlot(slot)
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
	if brick.Flags&volume.BrickFlagSolid != 0 {
		gpuAtlasOffset = brick.AtlasOffset
		m.releaseBrickSlot(brick)
	} else {
		pSlot, exists := m.BrickToSlot[brick]
		if !exists {
			pSlot = m.PayloadAlloc.Alloc()
			if pSlot >= AtlasBricksPerSide*AtlasBricksPerSide*AtlasBricksPerSide {
				panic("Voxel payload atlas full!")
			}
			m.BrickToSlot[brick] = pSlot
		}

		// Calculate 3D coordinates in the atlas
		ax := (pSlot % AtlasBricksPerSide) * volume.BrickSize
		ay := ((pSlot / AtlasBricksPerSide) % AtlasBricksPerSide) * volume.BrickSize
		az := (pSlot / (AtlasBricksPerSide * AtlasBricksPerSide)) * volume.BrickSize

		// 10 bits per axis for 1024^3 atlas
		gpuAtlasOffset = (ax << 20) | (ay << 10) | az

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
				Texture:  m.VoxelPayloadTex,
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

	bbuf := make([]byte, 16)
	binary.LittleEndian.PutUint32(bbuf[0:4], gpuAtlasOffset)
	binary.LittleEndian.PutUint32(bbuf[4:8], uint32(brick.OccupancyMask64))
	binary.LittleEndian.PutUint32(bbuf[8:12], uint32(brick.OccupancyMask64>>32))
	binary.LittleEndian.PutUint32(bbuf[12:16], brick.Flags)
	m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64(slotIdx*16), bbuf)
}
