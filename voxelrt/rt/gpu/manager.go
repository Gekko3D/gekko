package gpu

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

const (
	HeadroomPayload = 4 * 1024 * 1024
	HeadroomTables  = 64 * 1024

	MaxUpdatesPerFrame  = 1024               // Cap voxel/sector updates per frame
	SafeBufferSizeLimit = 1024 * 1024 * 1024 // 1GB Warning/Compaction Limit

	// Texture Atlas Constants
	AtlasBricksPerSide = 128                                   // 128^3 = 2,097,152 bricks (1GB at 512 bytes per brick)
	AtlasSize          = AtlasBricksPerSide * volume.BrickSize // 1024 voxels per side if BrickSize is 8
)

type GpuBufferManager struct {
	Device *wgpu.Device

	CameraBuf        *wgpu.Buffer
	InstancesBuf     *wgpu.Buffer
	BVHNodesBuf      *wgpu.Buffer
	LightsBuf        *wgpu.Buffer
	ShadowIndicesBuf *wgpu.Buffer

	MaterialBuf         *wgpu.Buffer
	SectorTableBuf      *wgpu.Buffer
	BrickTableBuf       *wgpu.Buffer
	VoxelPayloadTex     *wgpu.Texture
	VoxelPayloadView    *wgpu.TextureView
	ObjectParamsBuf     *wgpu.Buffer
	Tree64Buf           *wgpu.Buffer
	SectorGridBuf       *wgpu.Buffer
	SectorGridParamsBuf *wgpu.Buffer

	// G-Buffer Textures
	GBufferDepth    *wgpu.Texture
	GBufferNormal   *wgpu.Texture
	GBufferMaterial *wgpu.Texture
	GBufferPosition *wgpu.Texture

	// Transparent Accumulation Targets (WBOIT)
	TransparentAccumTex  *wgpu.Texture // RGBA16Float, accum premultiplied color
	TransparentWeightTex *wgpu.Texture // R16Float, accum weight

	// G-Buffer Views
	DepthView    *wgpu.TextureView
	NormalView   *wgpu.TextureView
	MaterialView *wgpu.TextureView
	PositionView *wgpu.TextureView

	// Transparent Accumulation Views
	TransparentAccumView  *wgpu.TextureView
	TransparentWeightView *wgpu.TextureView

	// Shadow Map Resources
	ShadowMapArray *wgpu.Texture
	ShadowMapView  *wgpu.TextureView

	// Hi-Z Occlusion
	HiZTexture     *wgpu.Texture
	HiZViews       []*wgpu.TextureView // Mip views
	ReadbackBuffer *wgpu.Buffer
	HiZPipeline    *wgpu.ComputePipeline
	HiZBindGroups  []*wgpu.BindGroup // One per mip transition

	HiZReadbackLevel   uint32
	HiZReadbackWidth   uint32
	HiZReadbackHeight  uint32
	HiZState           int // 0: Idle, 1: Copy (GPU), 2: Mapping (Wait GPU), 3: Mapped (Read CPU)
	StateMu            sync.Mutex
	LastHiZData        []float32
	LastHiZW, LastHiZH uint32

	// Bind Groups for new passes
	GBufferBindGroup          *wgpu.BindGroup
	GBufferBindGroup0         *wgpu.BindGroup
	GBufferBindGroup2         *wgpu.BindGroup
	LightingBindGroup         *wgpu.BindGroup
	LightingBindGroup2        *wgpu.BindGroup // For G-Buffer inputs and output
	LightingBindGroupMaterial *wgpu.BindGroup // For Group 2 voxel data

	// Shadow Map Bind Groups
	ShadowPipeline   *wgpu.ComputePipeline
	ShadowBindGroup0 *wgpu.BindGroup
	ShadowBindGroup1 *wgpu.BindGroup
	ShadowBindGroup2 *wgpu.BindGroup

	DebugBindGroup0 *wgpu.BindGroup

	// Particles (rendered after lighting)
	ParticleInstancesBuf *wgpu.Buffer
	ParticlesBindGroup0  *wgpu.BindGroup // camera + instances
	ParticlesBindGroup1  *wgpu.BindGroup // gbuffer depth
	ParticleCount        uint32

	// Transparent overlay (single-layer transparency over lit image)
	TransparentBG0 *wgpu.BindGroup // camera + instances + BVH
	TransparentBG1 *wgpu.BindGroup // voxel data buffers
	TransparentBG2 *wgpu.BindGroup // gbuffer depth

	// Batch update tracking
	BatchMode      bool                       // Enable batching of updates within a frame
	PendingUpdates map[*volume.XBrickMap]bool // Maps with pending updates in current batch

	// Allocators for global pools
	SectorAlloc  SlotAllocator
	BrickAlloc   SlotAllocator // Allocates blocks of 64 bricks
	PayloadAlloc SlotAllocator // Allocates bricks (512 bytes each)

	// Mapping from volume objects to GPU slots
	SectorToInfo map[*volume.Sector]SectorGpuInfo
	BrickToSlot  map[*volume.Brick]uint32

	MaterialTail uint32

	Allocations map[*volume.XBrickMap]*ObjectGpuAllocation

	// Smooth streaming state
	SectorsPerFrame  uint32
	lastTotalSectors int
	gridDataPool     []byte
}

// ObjectGpuAllocation tracks the GPU memory regions assigned to a specific object.
type ObjectGpuAllocation struct {
	Sectors          map[[3]int]*volume.Sector // Track which sector is at which coordinate
	MaterialOffset   uint32                    // In elements (64 bytes each)
	MaterialCapacity uint32                    // In elements
}

type SectorGpuInfo struct {
	SlotIndex       uint32
	BrickTableIndex uint32 // Index into global BrickTableBuf (64 slots per sector)
}

type SlotAllocator struct {
	Tail uint32
	Free []uint32
}

func (a *SlotAllocator) Alloc() uint32 {
	if len(a.Free) > 0 {
		idx := a.Free[len(a.Free)-1]
		a.Free = a.Free[:len(a.Free)-1]
		return idx
	}
	idx := a.Tail
	a.Tail++
	return idx
}

func (a *SlotAllocator) FreeSlot(idx uint32) {
	a.Free = append(a.Free, idx)
}

func NewGpuBufferManager(device *wgpu.Device) *GpuBufferManager {
	return &GpuBufferManager{
		Device:          device,
		Allocations:     make(map[*volume.XBrickMap]*ObjectGpuAllocation),
		PendingUpdates:  make(map[*volume.XBrickMap]bool),
		BatchMode:       false,
		SectorToInfo:    make(map[*volume.Sector]SectorGpuInfo),
		BrickToSlot:     make(map[*volume.Brick]uint32),
		SectorsPerFrame: 64,
	}
}

// CreateTransparentOverlayBindGroups wires the overlay pass bind groups:
// Group 0: camera (uniform) + instances (storage) + BVH nodes (storage)
// Group 1: voxel data buffers (sector, brick, payload, object params, tree64, sector grid, sector grid params)
// Group 2: gbuffer depth (RGBA32F)
func (m *GpuBufferManager) CreateTransparentOverlayBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}
	var err error

	// Group 0
	m.TransparentBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 1
	m.TransparentBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.VoxelPayloadView},
			{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 2
	m.TransparentBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.MaterialView},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) UpdateScene(scene *core.Scene) bool {
	recreated := false

	// 1. Instances
	instData := []byte{}
	for i, obj := range scene.VisibleObjects {
		o2w := obj.Transform.ObjectToWorld()
		w2o := obj.Transform.WorldToObject()

		instData = append(instData, mat4ToBytes(o2w)...)
		instData = append(instData, mat4ToBytes(w2o)...)

		minB, maxB := [3]float32{}, [3]float32{}
		if obj.WorldAABB != nil {
			minB = obj.WorldAABB[0]
			maxB = obj.WorldAABB[1]
		}
		instData = append(instData, vec3ToBytesPadded(minB)...)
		instData = append(instData, vec3ToBytesPadded(maxB)...)

		lMin, lMax := obj.XBrickMap.ComputeAABB()
		instData = append(instData, vec3ToBytesPadded([3]float32{lMin.X(), lMin.Y(), lMin.Z()})...)
		instData = append(instData, vec3ToBytesPadded([3]float32{lMax.X(), lMax.Y(), lMax.Z()})...)

		idBuf := make([]byte, 16)
		binary.LittleEndian.PutUint32(idBuf[0:4], uint32(i))
		instData = append(instData, idBuf...)
	}

	if len(instData) == 0 {
		instData = make([]byte, 208)
	}
	if m.ensureBuffer("InstancesBuf", &m.InstancesBuf, instData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 2. BVH
	bvhData := scene.BVHNodesBytes
	if len(bvhData) == 0 {
		bvhData = make([]byte, 64)
	}
	if m.ensureBuffer("BVHNodesBuf", &m.BVHNodesBuf, bvhData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 3. Lights
	m.UpdateLights(scene)
	lightsData := []byte{}
	for _, l := range scene.Lights {
		lightsData = append(lightsData, vec4ToBytes(l.Position)...)
		lightsData = append(lightsData, vec4ToBytes(l.Direction)...)
		lightsData = append(lightsData, vec4ToBytes(l.Color)...)
		lightsData = append(lightsData, vec4ToBytes(l.Params)...)
		lightsData = append(lightsData, mat4ToBytes(l.ViewProj)...)
		lightsData = append(lightsData, mat4ToBytes(l.InvViewProj)...)
	}
	if len(lightsData) == 0 {
		lightsData = make([]byte, 192)
	}
	if m.ensureBuffer("LightsBuf", &m.LightsBuf, lightsData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 4. Voxel Data (Incremental / Paged)
	if m.UpdateVoxelData(scene) {
		recreated = true
	}

	// 5. Sector Hash Grid
	if m.updateSectorGrid(scene) {
		recreated = true
	}
	_ = recreated
	return recreated
}

func (m *GpuBufferManager) UpdateVoxelData(scene *core.Scene) bool {
	recreated := false
	var err error

	// Cleanup orphan allocations
	activeMaps := make(map[*volume.XBrickMap]bool)
	for _, obj := range scene.Objects {
		activeMaps[obj.XBrickMap] = true
	}
	for xbm := range m.Allocations {
		if !activeMaps[xbm] {
			// Free slots
			for sKey, sector := range xbm.Sectors {
				if info, ok := m.SectorToInfo[sector]; ok {
					m.SectorAlloc.FreeSlot(info.SlotIndex)
					m.BrickAlloc.FreeSlot(info.BrickTableIndex / 64)
					delete(m.SectorToInfo, sector)
				}
				// Free bricks payloads
				for i := 0; i < 64; i++ {
					if (sector.BrickMask64 & (1 << i)) != 0 {
						bx, by, bz := i%4, (i/4)%4, i/16
						brick := sector.GetBrick(bx, by, bz)
						if slot, ok := m.BrickToSlot[brick]; ok {
							m.PayloadAlloc.FreeSlot(slot)
							delete(m.BrickToSlot, brick)
						}
					}
				}
				_ = sKey
			}
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
	if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, int(m.MaterialTail+1024)*64) {
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
			materials = append(materials, make([]byte, 16)...)
		}
		if len(materials) == 0 {
			materials = make([]byte, 256*64)
		}
		mCount := uint32(len(materials) / 64)
		if mCount > alloc.MaterialCapacity {
			alloc.MaterialOffset = m.MaterialTail
			alloc.MaterialCapacity = mCount + 64 // Add some headroom
			m.MaterialTail += alloc.MaterialCapacity
			if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, int(m.MaterialTail*64)) {
				recreated = true
			}
			m.Device.GetQueue().WriteBuffer(m.MaterialBuf, uint64(alloc.MaterialOffset*64), materials)
		} else {
			// Just upload if modified (in a real system we'd check mat dirty flags)
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
						for bz := 0; bz < 4; bz++ {
							for by := 0; by < 4; by++ {
								for bx := 0; bx < 4; bx++ {
									if brick := oldSector.GetBrick(bx, by, bz); brick != nil {
										if pSlot, ok := m.BrickToSlot[brick]; ok {
											m.PayloadAlloc.FreeSlot(pSlot)
											delete(m.BrickToSlot, brick)
										}
									}
								}
							}
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
					m.uploadBrick(brick, info.BrickTableIndex+uint32(i))
				} else {
					// Clear brick record in GPU to 0
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
			if brick != nil {
				m.uploadBrick(brick, info.BrickTableIndex+uint32(bx+by*4+bz*16))
			} else {
				// Clear brick record in GPU to 0
				m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64((info.BrickTableIndex+uint32(bx+by*4+bz*16))*16), make([]byte, 16))
			}
			delete(xbm.DirtyBricks, bKey)
			bricksInFrame++
		}

		if len(xbm.DirtySectors) == 0 && len(xbm.DirtyBricks) == 0 {
			xbm.AABBDirty = false
		}

		// Materials: For now, re-upload if new.
		// Need MaterialTail... I'll add m.MaterialTail back but keep it as a pool for objects.
	}

	// Update ObjectParams for visible objects
	objParams := []byte{}
	for _, obj := range scene.VisibleObjects {
		alloc := m.Allocations[obj.XBrickMap]
		// Need MaterialOffset!
		pBuf := make([]byte, 32)
		binary.LittleEndian.PutUint32(pBuf[0:4], 0) // sector_table_base is not used as index anymore, but for ID
		// Wait, the shader uses params.sector_table_base as ID for hash.
		// We can just use the memory address of xbm as a unique ID.
		binary.LittleEndian.PutUint32(pBuf[0:4], uint32(uintptr(unsafe.Pointer(obj.XBrickMap))))
		binary.LittleEndian.PutUint32(pBuf[4:8], 0)  // brick_table_base - now internal to sector
		binary.LittleEndian.PutUint32(pBuf[8:12], 0) // payload_base - now internal to brick
		binary.LittleEndian.PutUint32(pBuf[12:16], alloc.MaterialOffset*4)
		binary.LittleEndian.PutUint32(pBuf[16:20], ^uint32(0))
		binary.LittleEndian.PutUint32(pBuf[20:24], math.Float32bits(obj.LODThreshold))
		binary.LittleEndian.PutUint32(pBuf[24:28], uint32(len(obj.XBrickMap.Sectors)))
		objParams = append(objParams, pBuf...)
	}

	if len(objParams) == 0 {
		objParams = make([]byte, 32)
	}

	if m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, objParams, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	return recreated
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
	} else {
		// Needs payload slot
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

		gpuAtlasOffset = (ax << 20) | (ay << 10) | az // Pack coords for shader

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

func (m *GpuBufferManager) ensureBuffer(name string, buf **wgpu.Buffer, data []byte, usage wgpu.BufferUsage, headroom int) bool {
	neededSize := uint64(len(data) + headroom)
	if neededSize%4 != 0 {
		neededSize += 4 - (neededSize % 4)
	}

	current := *buf
	// Always add CopySrc/CopyDst to allow resizing copies and writes
	usage = usage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc

	if current == nil || current.GetSize() < neededSize {
		// Calculate new size
		var newSize uint64 = neededSize
		if current != nil {
			// Geometric growth: grow by 1.5x
			growthSize := uint64(float64(current.GetSize()) * 1.5)
			if growthSize > newSize {
				newSize = growthSize
			}
		}

		if newSize > SafeBufferSizeLimit {
			fmt.Printf("WARNING: Buffer %s allocation size %d exceeds safety limit %d\n", name, newSize, SafeBufferSizeLimit)
		}

		desc := &wgpu.BufferDescriptor{
			Label:            name,
			Size:             newSize,
			Usage:            usage,
			MappedAtCreation: false,
		}
		newBuf, err := m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
		}

		// If we are resizing an existing buffer AND not overwriting it strictly (data == nil),
		// we must preserve the old content.
		if current != nil && data == nil {
			encoder, err := m.Device.CreateCommandEncoder(nil)
			if err != nil {
				panic(err)
			}

			// Copy old content to new buffer
			// Size: Copy valid range. We can just copy the whole old buffer size.
			copySize := current.GetSize()
			encoder.CopyBufferToBuffer(current, 0, newBuf, 0, copySize)

			cmdBuf, err := encoder.Finish(nil)
			if err != nil {
				panic(err)
			}
			m.Device.GetQueue().Submit(cmdBuf)
		}

		if current != nil {
			current.Release()
		}

		*buf = newBuf

		if len(data) > 0 {
			m.Device.GetQueue().WriteBuffer(*buf, 0, data)
		}
		return true
	} else {
		if len(data) > 0 {
			m.Device.GetQueue().WriteBuffer(*buf, 0, data)
		}
		return false
	}
}

func (m *GpuBufferManager) UpdateCamera(view, proj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor mgl32.Vec3, debugMode uint32, renderMode uint32, numLights uint32) {
	buf := make([]byte, 256)

	writeMat := func(offset int, mat mgl32.Mat4) {
		for i, v := range mat {
			binary.LittleEndian.PutUint32(buf[offset+i*4:], math.Float32bits(v))
		}
	}

	writeMat(0, view)
	writeMat(64, invView)
	writeMat(128, invProj)

	binary.LittleEndian.PutUint32(buf[192:], math.Float32bits(camPos[0]))
	binary.LittleEndian.PutUint32(buf[196:], math.Float32bits(camPos[1]))
	binary.LittleEndian.PutUint32(buf[200:], math.Float32bits(camPos[2]))
	binary.LittleEndian.PutUint32(buf[204:], 0)

	binary.LittleEndian.PutUint32(buf[208:], math.Float32bits(lightPos[0]))
	binary.LittleEndian.PutUint32(buf[212:], math.Float32bits(lightPos[1]))
	binary.LittleEndian.PutUint32(buf[216:], math.Float32bits(lightPos[2]))
	binary.LittleEndian.PutUint32(buf[220:], 0)

	binary.LittleEndian.PutUint32(buf[224:], math.Float32bits(ambientColor[0]))
	binary.LittleEndian.PutUint32(buf[228:], math.Float32bits(ambientColor[1]))
	binary.LittleEndian.PutUint32(buf[232:], math.Float32bits(ambientColor[2]))
	binary.LittleEndian.PutUint32(buf[236:], 0)

	binary.LittleEndian.PutUint32(buf[240:], debugMode)
	binary.LittleEndian.PutUint32(buf[244:], renderMode)
	binary.LittleEndian.PutUint32(buf[248:], numLights)

	if m.CameraBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "CameraUB",
			Size:  256,
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		}
		var err error
		m.CameraBuf, err = m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
		}
	}
	m.Device.GetQueue().WriteBuffer(m.CameraBuf, 0, buf)
}

func (m *GpuBufferManager) BeginBatch() {
	m.BatchMode = true
	m.PendingUpdates = make(map[*volume.XBrickMap]bool)
}

func (m *GpuBufferManager) EndBatch() {
	if !m.BatchMode {
		return
	}
	// With Paged Updates, dirty flags are handled inside updateXBrickMapPaged's loop.
	// But if EndBatch is called before UpdateScene, we might want to ensure consistency.
	// Actually, UpdateScene calls updateXBrickMapPaged which handles dirty flags.
	// So EndBatch just clears the mode.
	m.BatchMode = false
	m.PendingUpdates = make(map[*volume.XBrickMap]bool)
}

func (m *GpuBufferManager) UpdateLights(scene *core.Scene) {
	for i := range scene.Lights {
		l := &scene.Lights[i]
		lightType := uint32(l.Params[2])
		pos := mgl32.Vec3{l.Position[0], l.Position[1], l.Position[2]}
		dir := mgl32.Vec3{l.Direction[0], l.Direction[1], l.Direction[2]}
		var view, proj mgl32.Mat4
		up := mgl32.Vec3{0, 1, 0}
		if math.Abs(float64(dir.Y())) > 0.99 {
			up = mgl32.Vec3{1, 0, 0}
		}

		if lightType == 1 { // Directional
			size := float32(500.0)
			proj = mgl32.Ortho(-size, size, -size, size, 0.1, 2000.0)
			view = mgl32.LookAtV(pos, pos.Add(dir), up)
		} else if lightType == 2 { // Spot
			fov := math.Acos(float64(l.Params[1])) * 2.0
			proj = mgl32.Perspective(float32(fov), 1.0, 0.1, l.Params[0])
			view = mgl32.LookAtV(pos, pos.Add(dir), up)
		} else { // Point
			proj = mgl32.Perspective(mgl32.DegToRad(90), 1.0, 0.1, l.Params[0])
			view = mgl32.LookAtV(pos, pos.Add(mgl32.Vec3{0, 0, 1}), up)
		}
		vp := proj.Mul4(view)
		l.ViewProj = [16]float32(vp)
		l.InvViewProj = [16]float32(vp.Inv())
	}
}

func (m *GpuBufferManager) updateSectorGrid(scene *core.Scene) bool {
	// Count total sectors
	totalSectors := 0
	anyDirty := false
	for _, obj := range scene.Objects {
		if xbm := obj.XBrickMap; xbm != nil {
			totalSectors += len(xbm.Sectors)
			if xbm.StructureDirty {
				anyDirty = true
			}
		}
	}

	// Optimization: Skip rebuild if nothing structurally changed and count is the same
	if totalSectors == m.lastTotalSectors && !anyDirty && m.SectorGridBuf != nil {
		return false
	}
	m.lastTotalSectors = totalSectors

	// Always ensure buffers exist even if empty to avoid bind group panics
	if totalSectors == 0 {
		recreated := false
		if m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0) {
			recreated = true
		}
		if m.ensureBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0) {
			recreated = true
		}
		return recreated
	}

	// Hash grid size: next power of 2, 8x occupancy for minimal collisions
	gridSize := 1
	for gridSize < totalSectors*8 {
		gridSize <<= 1
	}
	if gridSize < 1024 {
		gridSize = 1024
	}

	// Re-use or resize pull to avoid GC pressure
	neededSize := gridSize * 32
	if cap(m.gridDataPool) < neededSize {
		m.gridDataPool = make([]byte, neededSize)
	} else {
		m.gridDataPool = m.gridDataPool[:neededSize]
		// Fast clear
		for i := range m.gridDataPool {
			m.gridDataPool[i] = 0
		}
	}

	// Grid entry: [sx, sy, sz, base_idx, sector_idx, pad, pad, pad] (8x i32 = 32 bytes)
	// We'll use a simple open-addressing scheme.
	// Empty slot: sector_idx = -1
	for i := 0; i < gridSize; i++ {
		binary.LittleEndian.PutUint32(m.gridDataPool[i*32+16:], 0xFFFFFFFF) // sector_idx = -1
	}

	hash := func(x, y, z int32, base uint32) uint32 {
		h := uint32(x)*73856093 ^ uint32(y)*19349663 ^ uint32(z)*83492791 ^ base*99999989
		return h % uint32(gridSize)
	}

	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		if xbm == nil {
			continue
		}
		baseIdx := uint32(uintptr(unsafe.Pointer(obj.XBrickMap)))

		for sKey, sector := range xbm.Sectors {
			sx, sy, sz := int32(sKey[0]), int32(sKey[1]), int32(sKey[2])
			info, ok := m.SectorToInfo[sector]
			if !ok {
				continue
			}

			h := hash(sx, sy, sz, baseIdx)
			inserted := false
			for i := 0; i < 128; i++ {
				probeIdx := (h + uint32(i)) % uint32(gridSize)
				sectorIdx := binary.LittleEndian.Uint32(m.gridDataPool[probeIdx*32+16:])
				if sectorIdx == 0xFFFFFFFF {
					// Found empty slot
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+0:], uint32(sx))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+4:], uint32(sy))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+8:], uint32(sz))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+12:], baseIdx)
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+16:], info.SlotIndex)
					inserted = true
					break
				}
			}
			if !inserted {
				fmt.Printf("WARNING: Sector Grid Overflow! Failed to insert sector [%d,%d,%d] base=%d after 128 probes. totalSectors=%d, gridSize=%d\n",
					sx, sy, sz, baseIdx, totalSectors, gridSize)
			}
		}
	}

	recreated := false
	if m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, m.gridDataPool, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	paramsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(paramsData[0:4], uint32(gridSize))
	binary.LittleEndian.PutUint32(paramsData[4:8], uint32(gridSize-1)) // mask if we used power of 2, but we use modulo just in case. Wait, h % gridSize is fine.

	if m.ensureBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, paramsData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	return recreated
}

func (m *GpuBufferManager) CreateDebugBindGroups(pipeline *wgpu.ComputePipeline) {
	entries0 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
		{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
	}
	desc0 := &wgpu.BindGroupDescriptor{
		Layout:  pipeline.GetBindGroupLayout(0),
		Entries: entries0,
	}
	var err error
	m.DebugBindGroup0, err = m.Device.CreateBindGroup(desc0)
	if err != nil {
		panic(err)
	}

}
func (m *GpuBufferManager) CreateGBufferTextures(w, h uint32) {
	if w == 0 || h == 0 {
		return
	}

	setupTexture := func(tex **wgpu.Texture, view **wgpu.TextureView, label string, format wgpu.TextureFormat, usage wgpu.TextureUsage) {
		if *tex != nil {
			(*tex).Release()
		}
		var err error
		*tex, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         label,
			Size:          wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			Dimension:     wgpu.TextureDimension2D,
			Format:        format,
			Usage:         usage,
			SampleCount:   1,
		})
		if err != nil {
			panic(err)
		}
		*view, err = (*tex).CreateView(nil)
		if err != nil {
			panic(err)
		}
	}

	setupTexture(&m.GBufferDepth, &m.DepthView, "GBuffer Depth", wgpu.TextureFormatRGBA32Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding)
	setupTexture(&m.GBufferNormal, &m.NormalView, "GBuffer Normal", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding)
	setupTexture(&m.GBufferMaterial, &m.MaterialView, "GBuffer Material", wgpu.TextureFormatRGBA32Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding)
	setupTexture(&m.GBufferPosition, &m.PositionView, "GBuffer Position", wgpu.TextureFormatRGBA32Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding)

	// Transparent accumulation targets for WBOIT
	setupTexture(&m.TransparentAccumTex, &m.TransparentAccumView, "Transparent Accum", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding)
	setupTexture(&m.TransparentWeightTex, &m.TransparentWeightView, "Transparent Weight", wgpu.TextureFormatR16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding)

	m.CreateShadowMapTextures(1024, 1024, 16) // Default 1024x1024 for 16 lights
}

func (m *GpuBufferManager) CreateShadowMapTextures(w, h, count uint32) {
	if m.ShadowMapArray != nil {
		m.ShadowMapArray.Release()
	}

	var err error
	m.ShadowMapArray, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Shadow Map Array",
		Size: wgpu.Extent3D{
			Width:              w,
			Height:             h,
			DepthOrArrayLayers: count,
		},
		MipLevelCount: 1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA32Float,
		Usage:         wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding,
		SampleCount:   1,
	})
	if err != nil {
		panic(err)
	}

	m.ShadowMapView, err = m.ShadowMapArray.CreateView(&wgpu.TextureViewDescriptor{
		Label:           "Shadow Map View",
		Format:          wgpu.TextureFormatRGBA32Float,
		Dimension:       wgpu.TextureViewDimension2DArray,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: count,
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateGBufferBindGroups(gbPipeline, lightPipeline *wgpu.ComputePipeline) {
	var err error

	m.GBufferBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.NormalView},
			{Binding: 2, TextureView: m.MaterialView},
			{Binding: 3, TextureView: m.PositionView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.LightingBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.GBufferBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.GBufferBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.VoxelPayloadView},
			{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateLightingBindGroups(lightPipeline *wgpu.ComputePipeline, outputView *wgpu.TextureView) {
	var err error
	m.LightingBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.NormalView},
			{Binding: 2, TextureView: m.MaterialView},
			{Binding: 3, TextureView: m.PositionView},
			{Binding: 4, TextureView: outputView},
			{Binding: 5, TextureView: m.ShadowMapView},
		},
	})
	if err != nil {
		panic(err)
	}

	// Create materials bind group (group 2)
	m.LightingBindGroupMaterial, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

// UpdateParticles uploads particle instances into a storage buffer (read in VS)
func (m *GpuBufferManager) UpdateParticles(instances []core.ParticleInstance) bool {
	// Create byte slice from instances
	var bytes []byte
	if len(instances) > 0 {
		vSize := len(instances) * int(unsafe.Sizeof(core.ParticleInstance{}))
		bytes = unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), vSize)
	} else {
		bytes = []byte{}
	}

	// Use ensureBuffer with headroom for ~1024 particles (32KB)
	// This ensures we have space to grow without immediate reallocation
	// and benefits from the geometric growth logic in ensureBuffer.
	headroom := 1024 * 32
	recreated := m.ensureBuffer("ParticleInstancesBuf", &m.ParticleInstancesBuf, bytes, wgpu.BufferUsageStorage, headroom)
	m.ParticleCount = uint32(len(instances))
	return recreated
}

// CreateParticlesBindGroups wires camera + instances (group 0) and gbuffer depth (group 1)
func (m *GpuBufferManager) CreateParticlesBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}
	var err error
	m.ParticlesBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.ParticleInstancesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.ParticlesBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateShadowPipeline(code string) error {
	mod, err := m.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Shadow Map CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code},
	})
	if err != nil {
		return err
	}
	defer mod.Release()

	m.ShadowPipeline, err = m.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "Shadow Pipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "main",
		},
	})
	return err
}

func (m *GpuBufferManager) CreateShadowBindGroups() {
	var err error

	// Ensure indices buffer exists
	m.ensureBuffer("ShadowIndicesBuf", &m.ShadowIndicesBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0)

	// Group 0: Scene + Lights + Update Indices
	m.ShadowBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.ShadowIndicesBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 1: Output Shadow Maps
	m.ShadowBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.ShadowMapView},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 2: Voxel Data
	m.ShadowBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.VoxelPayloadView},
			{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) DispatchShadowPass(encoder *wgpu.CommandEncoder, indices []uint32) {
	if m.ShadowPipeline == nil || m.ShadowBindGroup0 == nil {
		return
	}

	if len(indices) == 0 {
		return
	}

	// Upload indices
	idxBytes := make([]byte, len(indices)*4)
	for i, v := range indices {
		binary.LittleEndian.PutUint32(idxBytes[i*4:], v)
	}

	// Ensure buffer size
	if m.ensureBuffer("ShadowIndicesBuf", &m.ShadowIndicesBuf, idxBytes, wgpu.BufferUsageStorage, 1024) {
		// If recreated, we must recreate the bind group immediately for it to take effect
		// This might be expensive if done every frame, but ensureBuffer only recreates on growth.
		m.CreateShadowBindGroups()
	} else {
		// Just write if not recreated (ensureBuffer writes data if buffer acts as update)
		// Actually ensureBuffer does write data.
		// If buffer wasn't recreated, we still need to write if we want to update content?
		// modify ensureBuffer behavior?
		// ensureBuffer writes data if passed.
	}
	// Wait, ensureBuffer implementation:
	// if len(data) > 0 { m.Device.GetQueue().WriteBuffer(*buf, 0, data) }
	// So data IS written.

	cPass := encoder.BeginComputePass(nil)
	cPass.SetPipeline(m.ShadowPipeline)
	cPass.SetBindGroup(0, m.ShadowBindGroup0, nil)
	cPass.SetBindGroup(1, m.ShadowBindGroup1, nil)
	cPass.SetBindGroup(2, m.ShadowBindGroup2, nil)

	// Dispatch for 1024x1024 shadow maps
	wgX := (1024 + 7) / 8
	wgY := (1024 + 7) / 8
	cPass.DispatchWorkgroups(uint32(wgX), uint32(wgY), uint32(len(indices)))
	cPass.End()
}

// Helpers
func mat4ToBytes(m [16]float32) []byte {
	buf := make([]byte, 64)
	for i, v := range m {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func vec3ToBytesPadded(v [3]float32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(v[2]))
	return buf
}

func int3ToBytesPadded(v [3]int32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(v[2]))
	return buf
}

func rgbaToVec4(c [4]uint8) []byte {
	buf := make([]byte, 16)
	for i, v := range c {
		f := float32(v) / 255.0
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func float32ToBytes(ff float32) []byte {
	bits := math.Float32bits(ff)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, bits)
	return buf
}

func vec4ToBytes(v [4]float32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(v[2]))
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(v[3]))
	return buf
}
