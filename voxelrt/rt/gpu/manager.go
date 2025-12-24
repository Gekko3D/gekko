package gpu

import (
	"encoding/binary"
	"math"
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/cogentcore/webgpu/wgpu"
)

const (
	HeadroomPayload = 4 * 1024 * 1024
	HeadroomTables  = 64 * 1024
)

type GpuBufferManager struct {
	Device *wgpu.Device

	CameraBuf    *wgpu.Buffer
	InstancesBuf *wgpu.Buffer
	BVHNodesBuf  *wgpu.Buffer
	LightsBuf    *wgpu.Buffer

	MaterialBuf         *wgpu.Buffer
	SectorTableBuf      *wgpu.Buffer
	BrickTableBuf       *wgpu.Buffer
	VoxelPayloadBuf     *wgpu.Buffer
	ObjectParamsBuf     *wgpu.Buffer
	Tree64Buf           *wgpu.Buffer
	SectorGridBuf       *wgpu.Buffer
	SectorGridParamsBuf *wgpu.Buffer

	BindGroup0      *wgpu.BindGroup
	DebugBindGroup0 *wgpu.BindGroup
	BindGroup2      *wgpu.BindGroup

	MapOffsets    map[*volume.XBrickMap][3]uint32 // sectorBase, brickBase, payloadBase
	AllocatedMaps map[*volume.XBrickMap]bool      // Track which maps have been fully uploaded

	// Batch update tracking
	BatchMode      bool                       // Enable batching of updates within a frame
	PendingUpdates map[*volume.XBrickMap]bool // Maps with pending updates in current batch

	// GPU voxel editing
	EditCommandBuf   *wgpu.Buffer
	EditParamsBuf    *wgpu.Buffer
	EditPipeline     *wgpu.ComputePipeline
	EditBindGroup0   *wgpu.BindGroup
	EditBindGroup1   *wgpu.BindGroup
	EditBindGroup2   *wgpu.BindGroup
	PendingEdits     []EditCommand
	MaxEditsPerFrame int

	// Brick pool for GPU allocation
	BrickPoolParamsBuf  *wgpu.Buffer
	BrickPoolBuf        *wgpu.Buffer
	BrickPoolPayloadBuf *wgpu.Buffer
	SectorExpansionBuf  *wgpu.Buffer
	ExpansionCounterBuf *wgpu.Buffer

	// Pool configuration
	BrickPoolSize uint32
	BrickPoolUsed uint32

	// Compression pipeline
	CompressionPipeline   *wgpu.ComputePipeline
	CompressionParamsBuf  *wgpu.Buffer
	PayloadFreeQueueBuf   *wgpu.Buffer
	FreeQueueCounterBuf   *wgpu.Buffer
	CompressionBindGroup0 *wgpu.BindGroup
	CompressionBindGroup1 *wgpu.BindGroup
	CompressionBindGroup2 *wgpu.BindGroup

	// Payload free list
	PayloadFreeList   []uint32
	DirtyBrickIndices []uint32 // Track which bricks were edited

	// Cached Sector indices for deterministic updates
	SectorIndices map[*volume.Sector]SectorGpuInfo
}

type SectorGpuInfo struct {
	SectorIndex     uint32
	FirstBrickIndex uint32
}

// EditCommand represents a single voxel edit operation
type EditCommand struct {
	Position [3]int32
	Value    uint32
}

func NewGpuBufferManager(device *wgpu.Device) *GpuBufferManager {
	return &GpuBufferManager{
		Device:            device,
		MapOffsets:        make(map[*volume.XBrickMap][3]uint32),
		AllocatedMaps:     make(map[*volume.XBrickMap]bool),
		PendingUpdates:    make(map[*volume.XBrickMap]bool),
		BatchMode:         false,
		PendingEdits:      make([]EditCommand, 0, 16384),
		MaxEditsPerFrame:  16384,
		BrickPoolSize:     65536,
		BrickPoolUsed:     0,
		PayloadFreeList:   make([]uint32, 0, 1024),
		DirtyBrickIndices: make([]uint32, 0, 256),
		SectorIndices:     make(map[*volume.Sector]SectorGpuInfo),
	}
}

func (m *GpuBufferManager) ensureBuffer(name string, buf **wgpu.Buffer, data []byte, usage wgpu.BufferUsage, headroom int) bool {
	neededSize := uint64(len(data) + headroom)
	if neededSize%4 != 0 {
		neededSize += 4 - (neededSize % 4)
	}

	current := *buf
	if current == nil || current.GetSize() < neededSize {
		if current != nil {
			current.Release()
		}

		desc := &wgpu.BufferDescriptor{
			Label:            name,
			Size:             neededSize,
			Usage:            usage | wgpu.BufferUsageCopyDst,
			MappedAtCreation: false,
		}
		newBuf, err := m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
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

func (m *GpuBufferManager) UpdateCamera(view, proj, invView, invProj [16]float32, camPos, lightPos, ambientColor [3]float32, debugMode bool) {
	// Struct CameraData {
	//   view_proj: mat4x4<f32>; -- 64
	//   inv_view: mat4x4<f32>;  -- 128
	//   inv_proj: mat4x4<f32>;  -- 192
	//   cam_pos: vec4<f32>;     -- 208
	//   light_pos: vec4<f32>;   -- 224
	//   ambient_color: vec4<f32>; -- 240
	//   debug_mode: u32;        -- 244
	// } -> 256 bytes (padded)

	buf := make([]byte, 256)

	// Helper to write matrix
	writeMat := func(offset int, mat [16]float32) {
		for i, v := range mat {
			binary.LittleEndian.PutUint32(buf[offset+i*4:], math.Float32bits(v))
		}
	}

	writeMat(0, view)      // view_proj
	writeMat(64, invView)  // inv_view
	writeMat(128, invProj) // inv_proj

	// Cam Pos
	binary.LittleEndian.PutUint32(buf[192:], math.Float32bits(camPos[0]))
	binary.LittleEndian.PutUint32(buf[196:], math.Float32bits(camPos[1]))
	binary.LittleEndian.PutUint32(buf[200:], math.Float32bits(camPos[2]))
	binary.LittleEndian.PutUint32(buf[204:], 0)

	// Light Pos
	binary.LittleEndian.PutUint32(buf[208:], math.Float32bits(lightPos[0]))
	binary.LittleEndian.PutUint32(buf[212:], math.Float32bits(lightPos[1]))
	binary.LittleEndian.PutUint32(buf[216:], math.Float32bits(lightPos[2]))
	binary.LittleEndian.PutUint32(buf[220:], 0)

	// Ambient Color
	binary.LittleEndian.PutUint32(buf[224:], math.Float32bits(ambientColor[0]))
	binary.LittleEndian.PutUint32(buf[228:], math.Float32bits(ambientColor[1]))
	binary.LittleEndian.PutUint32(buf[232:], math.Float32bits(ambientColor[2]))
	binary.LittleEndian.PutUint32(buf[236:], 0)

	// Debug Mode
	debugVal := uint32(0)
	if debugMode {
		debugVal = 1
	}
	binary.LittleEndian.PutUint32(buf[240:], debugVal)

	// ensureBuffer handles creation if nil
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

// BeginBatch starts accumulating updates without immediate GPU writes
func (m *GpuBufferManager) BeginBatch() {
	m.BatchMode = true
	m.PendingUpdates = make(map[*volume.XBrickMap]bool)
}

// EndBatch processes all accumulated updates in one operation
func (m *GpuBufferManager) EndBatch() {
	if !m.BatchMode {
		return
	}

	// Process all pending updates
	for xbm := range m.PendingUpdates {
		// Process dirty bricks
		for bKey := range xbm.DirtyBricks {
			sKey := [3]int{bKey[0], bKey[1], bKey[2]}
			sector, ok := xbm.Sectors[sKey]
			if !ok {
				continue
			}

			bx, by, bz := bKey[3], bKey[4], bKey[5]
			brick := sector.GetBrick(bx, by, bz)
			if brick != nil {
				m.UpdateBrickPayload(xbm, bKey, brick)
				m.UpdateBrickRecord(xbm, bKey, brick)
			}
		}

		// Process dirty sectors
		for sKey := range xbm.DirtySectors {
			sector, ok := xbm.Sectors[sKey]
			if ok {
				m.UpdateSectorRecord(xbm, sKey, sector)
			}
		}

		// Clear dirty flags
		xbm.ClearDirty()
	}

	m.BatchMode = false
	m.PendingUpdates = make(map[*volume.XBrickMap]bool)
}

func (m *GpuBufferManager) UpdateScene(scene *core.Scene) bool {
	recreated := false
	// 1. Instances
	instData := []byte{}
	for i, obj := range scene.Objects {
		// Instance struct (176 bytes)
		// object_to_world (64)
		// world_to_object (64)
		// aabb_min (16)
		// aabb_max (16)
		// local_aabb_min (16)
		// local_aabb_max (16)
		// object_id (4), padding (12)
		// Total: 208 bytes

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

		// Local AABB
		lMin, lMax := obj.XBrickMap.ComputeAABB()
		instData = append(instData, vec3ToBytesPadded([3]float32{lMin.X(), lMin.Y(), lMin.Z()})...)
		instData = append(instData, vec3ToBytesPadded([3]float32{lMax.X(), lMax.Y(), lMax.Z()})...)

		// ID
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

	// 2. BVH (Placeholder)
	bvhData := scene.BVHNodesBytes
	if len(bvhData) == 0 {
		bvhData = make([]byte, 64)
	}
	if m.ensureBuffer("BVHNodesBuf", &m.BVHNodesBuf, bvhData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 3. Lights
	lightsData := []byte{}
	// Light header: count (u32), pad (12 bytes)
	// Actually typical structure for array<Light> is just data, but we need to know count.
	// We can put count in CameraData or a separate uniform?
	// Or use a storage buffer with a header?
	// Let's use storage buffer `array<Light>` and pass count in Camera structure or ObjectParams?
	// Camera structure has padding. Let's use `pad0` in CameraData for light count.
	// Wait, CameraData is Uniform buffer.
	// In the plan I said "Add lights storage buffer binding (Group 0, Binding 3)".
	// Shader usually needs to know length. `arrayLength` works for runtime sized arrays in storage buffers.
	// So we just dump lights.

	for _, l := range scene.Lights {
		// Pos (16)
		lightsData = append(lightsData, vec4ToBytes(l.Position)...)
		// Dir (16)
		lightsData = append(lightsData, vec4ToBytes(l.Direction)...)
		// Color (16)
		lightsData = append(lightsData, vec4ToBytes(l.Color)...)
		// Params (16)
		lightsData = append(lightsData, vec4ToBytes(l.Params)...)
	}

	if len(lightsData) == 0 {
		lightsData = make([]byte, 64) // dummy
	}

	if m.ensureBuffer("LightsBuf", &m.LightsBuf, lightsData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 4. XBrickMap
	if m.updateXBrickMap(scene) {
		recreated = true
	}
	return recreated
}

func (m *GpuBufferManager) updateXBrickMap(scene *core.Scene) bool {
	recreated := false

	// Check if we can do sparse updates
	canDoSparseUpdate := true
	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		if _, allocated := m.AllocatedMaps[xbm]; !allocated {
			canDoSparseUpdate = false
			break
		}
		if xbm.StructureDirty {
			canDoSparseUpdate = false
			break
		}
	}

	// Attempt sparse update if possible
	if canDoSparseUpdate && m.VoxelPayloadBuf != nil && m.BrickTableBuf != nil && m.SectorTableBuf != nil {
		for _, obj := range scene.Objects {
			xbm := obj.XBrickMap
			if len(xbm.DirtyBricks) > 0 || len(xbm.DirtySectors) > 0 {

				// If in batch mode, just mark as pending and skip immediate update
				if m.BatchMode {
					m.PendingUpdates[xbm] = true
					continue
				}

				// Update dirty bricks
				for bKey := range xbm.DirtyBricks {
					sKey := [3]int{bKey[0], bKey[1], bKey[2]}
					sector, ok := xbm.Sectors[sKey]
					if !ok {
						continue
					}

					bx, by, bz := bKey[3], bKey[4], bKey[5]
					brick := sector.GetBrick(bx, by, bz)
					if brick != nil {
						m.UpdateBrickPayload(xbm, bKey, brick)
						m.UpdateBrickRecord(xbm, bKey, brick)
					}
				}

				// Update dirty sectors
				for sKey := range xbm.DirtySectors {
					sector, ok := xbm.Sectors[sKey]
					if ok {
						m.UpdateSectorRecord(xbm, sKey, sector)
					}
				}

				// Clear dirty flags
				xbm.ClearDirty()
			}
		}

		return false // No recreation needed
	}

	// Full rebuild path (initial upload or major changes)
	materials := []byte{}
	sectors := []byte{}
	bricks := []byte{}
	payload := []byte{}
	objParams := []byte{}
	tree64 := []byte{}

	m.MapOffsets = make(map[*volume.XBrickMap][3]uint32)
	m.SectorIndices = make(map[*volume.Sector]SectorGpuInfo)

	// Deduplicate logic
	// But wait, Python code just overwrote map_offsets every time update_xbrickmap was called.

	for _, obj := range scene.Objects {
		// Materials
		matBase := uint32(len(materials) / 16) // vec4 array
		for _, mat := range obj.MaterialTable {
			// Base Color (Vec4)
			materials = append(materials, rgbaToVec4(mat.BaseColor)...)
			// Emissive (Vec4)
			materials = append(materials, rgbaToVec4(mat.Emissive)...)
			// PBR Params (Vec4)
			materials = append(materials, float32ToBytes(mat.Roughness)...)
			materials = append(materials, float32ToBytes(mat.Metalness)...)
			materials = append(materials, float32ToBytes(mat.IOR)...)
			materials = append(materials, float32ToBytes(mat.Transparency)...)
			// Padding/Reserved (Vec4)
			materials = append(materials, make([]byte, 16)...)
		}

		// XBrickMap
		xbm := obj.XBrickMap
		var offsets [3]uint32
		var sectorCount uint32

		if off, found := m.MapOffsets[xbm]; found {
			offsets = off
			sectorCount = uint32(len(xbm.Sectors))
		} else {
			offsets[0] = uint32(len(sectors) / 32) // Sector Base (indices)
			offsets[1] = uint32(len(bricks) / 16)  // Brick Base (indices)
			offsets[2] = uint32(len(payload))      // Payload Base (bytes)
			sectorCount = uint32(len(xbm.Sectors))
			// Wait, python shader used `voxel_payload: array<u32>`.
			// `load_u8` did `voxel_payload[word_idx]`.
			// `params.payload_base` in Python was byte offset?
			// Python: `payload_base = len(payload_data)`.
			// `actual_atlas_offset = params.payload_base + brick.atlas_offset + voxel_idx`.
			// If payload_base is bytes, and load_u8 divides by 4...
			// Yes. Python `payload_base` was bytes.
			// BUT `brick.atlas_offset` was also bytes (512 bytes chunks).
			// `voxel_idx` is [0..511].
			// So `byte_offset` = bytes + bytes + bytes.
			// Correct.

			payloadByteOffset := uint32(len(payload))
			offsets[2] = payloadByteOffset // Keep as bytes for consistent arithmetic

			// Pack sectors
			// Sort sectors by coordinate? No, map order is random.
			// Need a stable order? Python iteration order.
			// Just iterate.

			// Sector table relies on `sector_id` referring to first brick index absolute.
			// So we need to reserve space first?
			// No, we append linearly.

			// Pack sectors
			// Sort sectors by coordinate for deterministic layout
			type sectorEntry struct {
				Key    [3]int
				Sector *volume.Sector
			}
			sortedSectors := make([]sectorEntry, 0, len(xbm.Sectors))
			for k, v := range xbm.Sectors {
				sortedSectors = append(sortedSectors, sectorEntry{k, v})
			}
			sort.Slice(sortedSectors, func(i, j int) bool {
				a, b := sortedSectors[i].Key, sortedSectors[j].Key
				if a[2] != b[2] {
					return a[2] < b[2]
				}
				if a[1] != b[1] {
					return a[1] < b[1]
				}
				return a[0] < b[0]
			})

			for i, entry := range sortedSectors {
				sKey := entry.Key
				sector := entry.Sector

				// Calculate absolute indices for cache
				absSectorIndex := offsets[0] + uint32(i)
				absBrickIndex := uint32(len(bricks) / 16)

				m.SectorIndices[sector] = SectorGpuInfo{
					SectorIndex:     absSectorIndex,
					FirstBrickIndex: absBrickIndex,
				}

				firstBrickIdx := absBrickIndex - offsets[1] // Relative to brick base

				// Sector Record (32 bytes)
				// origin (16), id (4), lo (4), hi (4), pad (4)
				ox, oy, oz := int32(sKey[0]*32), int32(sKey[1]*32), int32(sKey[2]*32)
				sectors = append(sectors, int3ToBytesPadded([3]int32{ox, oy, oz})...) // origin

				buf := make([]byte, 16)
				binary.LittleEndian.PutUint32(buf[0:4], firstBrickIdx)
				binary.LittleEndian.PutUint32(buf[4:8], uint32(sector.BrickMask64))      // lo
				binary.LittleEndian.PutUint32(buf[8:12], uint32(sector.BrickMask64>>32)) // hi
				sectors = append(sectors, buf...)

				// Bricks
				// We need to iterate bricks in bit order to match `popcnt` logic?
				// Standard `popcnt` implies packed bricks are in bit order.

				for i := 0; i < 64; i++ {
					if (sector.BrickMask64 & (1 << i)) != 0 {
						bx, by, bz := i%4, (i/4)%4, i/16
						brick := sector.GetBrick(bx, by, bz)
						// Brick Record (16 bytes)
						// atlas (4), lo (4), hi (4), flags (4)

						// Important: Brick Payload
						// We need to sync atlas.
						// If key in `BrickAtlasMap`, use offset (relative to payload base? or absolute?).
						// Python: `brick.atlas_offset` (relative to payload_base of THE map?)
						// Python logic: `payload_offset = len(payload_data) - payload_base`.
						// Then stores `brick.atlas_offset = payload_offset`.
						// In shader: `actual = payload_base + brick.atlas_offset`.
						// So `brick.atlas_offset` is relative to that map's start.

						// But where is the payload data?
						// In Go port, we are rebuilding the buffer every time (for now).
						// So we append payload linearly and update atlas offset.

						// Prepare Atlas Offset / Palette Index
						var gpuAtlasOffset uint32
						if brick.Flags&volume.BrickFlagSolid != 0 {
							gpuAtlasOffset = brick.AtlasOffset
						} else {
							gpuAtlasOffset = uint32(len(payload)) - offsets[2]

							// Serialize payload (512 bytes)
							for z := 0; z < 8; z++ {
								for y := 0; y < 8; y++ {
									for x := 0; x < 8; x++ {
										payload = append(payload, brick.Payload[x][y][z])
									}
								}
							}
						}

						buf := make([]byte, 16)
						binary.LittleEndian.PutUint32(buf[0:4], gpuAtlasOffset)
						binary.LittleEndian.PutUint32(buf[4:8], uint32(brick.OccupancyMask64))
						binary.LittleEndian.PutUint32(buf[8:12], uint32(brick.OccupancyMask64>>32))
						binary.LittleEndian.PutUint32(buf[12:16], brick.Flags)
						bricks = append(bricks, buf...)
					}
				}
			}

			m.MapOffsets[xbm] = offsets
		}

		// Tree64 TODO
		tree64Base := ^uint32(0) // -1

		// ObjectParams (32 bytes)
		// s_base, b_base, p_base, mat_base, tree_base, lod, sector_count, pad
		pBuf := make([]byte, 32)
		binary.LittleEndian.PutUint32(pBuf[0:4], offsets[0])
		binary.LittleEndian.PutUint32(pBuf[4:8], offsets[1])
		binary.LittleEndian.PutUint32(pBuf[8:12], offsets[2])
		binary.LittleEndian.PutUint32(pBuf[12:16], matBase)
		binary.LittleEndian.PutUint32(pBuf[16:20], tree64Base)
		binary.LittleEndian.PutUint32(pBuf[20:24], math.Float32bits(obj.LODThreshold))
		binary.LittleEndian.PutUint32(pBuf[24:28], sectorCount)
		objParams = append(objParams, pBuf...)

		// Mark map as allocated for future sparse updates
		m.AllocatedMaps[xbm] = true
		xbm.StructureDirty = false
	}

	// WebGPU requires non-zero size for buffers
	if len(materials) == 0 {
		materials = make([]byte, 64)
	}
	if len(sectors) == 0 {
		sectors = make([]byte, 64)
	}
	if len(bricks) == 0 {
		bricks = make([]byte, 64)
	}
	if len(payload) == 0 {
		payload = make([]byte, 64)
	}
	if len(objParams) == 0 {
		objParams = make([]byte, 64)
	}
	if len(tree64) == 0 {
		tree64 = make([]byte, 64)
	}

	// Ensure buffers
	if m.ensureBuffer("MaterialBuf", &m.MaterialBuf, materials, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.ensureBuffer("SectorTableBuf", &m.SectorTableBuf, sectors, wgpu.BufferUsageStorage, HeadroomTables) {
		recreated = true
	}
	if m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, bricks, wgpu.BufferUsageStorage, HeadroomTables) {
		recreated = true
	}
	if m.ensureBuffer("VoxelPayloadBuf", &m.VoxelPayloadBuf, payload, wgpu.BufferUsageStorage, HeadroomPayload) {
		recreated = true
	}
	if m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, objParams, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.ensureBuffer("Tree64Buf", &m.Tree64Buf, tree64, wgpu.BufferUsageStorage, HeadroomTables) {
		recreated = true
	}

	// 5. Sector Hash Grid
	if m.updateSectorGrid(scene) {
		recreated = true
	}

	return recreated
}

func (m *GpuBufferManager) updateSectorGrid(scene *core.Scene) bool {
	// Count total sectors
	totalSectors := 0
	for _, obj := range scene.Objects {
		totalSectors += len(obj.XBrickMap.Sectors)
	}

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

	// Hash grid size: next power of 2, 2x occupancy
	gridSize := 1
	for gridSize < totalSectors*2 {
		gridSize <<= 1
	}
	if gridSize < 1024 {
		gridSize = 1024
	}

	// Grid entry: [sx, sy, sz, base_idx, sector_idx, pad, pad, pad] (8x i32 = 32 bytes)
	// We'll use a simple open-addressing scheme.
	// Empty slot: sector_idx = -1
	gridData := make([]byte, gridSize*32)
	for i := 0; i < gridSize; i++ {
		binary.LittleEndian.PutUint32(gridData[i*32+16:], 0xFFFFFFFF) // sector_idx = -1
	}

	hash := func(x, y, z int32, base uint32) uint32 {
		h := uint32(x)*73856093 ^ uint32(y)*19349663 ^ uint32(z)*83492791 ^ base*99999989
		return h % uint32(gridSize)
	}

	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		baseIdx := m.MapOffsets[xbm][0]

		for sKey, sector := range xbm.Sectors {
			sx, sy, sz := int32(sKey[0]), int32(sKey[1]), int32(sKey[2])
			info, ok := m.SectorIndices[sector]
			if !ok {
				continue
			}

			h := hash(sx, sy, sz, baseIdx)
			for {
				sectorIdx := binary.LittleEndian.Uint32(gridData[h*32+16:])
				if sectorIdx == 0xFFFFFFFF {
					// Found empty slot
					binary.LittleEndian.PutUint32(gridData[h*32+0:], uint32(sx))
					binary.LittleEndian.PutUint32(gridData[h*32+4:], uint32(sy))
					binary.LittleEndian.PutUint32(gridData[h*32+8:], uint32(sz))
					binary.LittleEndian.PutUint32(gridData[h*32+12:], baseIdx)
					binary.LittleEndian.PutUint32(gridData[h*32+16:], info.SectorIndex)
					break
				}
				h = (h + 1) % uint32(gridSize)
			}
		}
	}

	recreated := false
	if m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, gridData, wgpu.BufferUsageStorage, 0) {
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

// UpdateBrickPayload updates a single brick's payload in the GPU buffer
func (m *GpuBufferManager) UpdateBrickPayload(xbm *volume.XBrickMap, brickKey [6]int, brick *volume.Brick) {
	if m.VoxelPayloadBuf == nil {
		return // Buffer not yet allocated
	}

	offsets, ok := m.MapOffsets[xbm]
	if !ok {
		return // Map not yet uploaded
	}

	// Check if brick is solid (no payload to upload)
	if brick.Flags&volume.BrickFlagSolid != 0 {
		return
	}

	// Calculate byte offset in payload buffer
	payloadBase := offsets[2]
	atlasOffset := brick.AtlasOffset
	byteOffset := payloadBase + atlasOffset

	// Serialize brick payload (512 bytes)
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

	// Write directly to GPU buffer
	m.Device.GetQueue().WriteBuffer(m.VoxelPayloadBuf, uint64(byteOffset), payload)
}

// UpdateBrickRecord updates a single brick's metadata in the brick table
func (m *GpuBufferManager) UpdateBrickRecord(xbm *volume.XBrickMap, brickKey [6]int, brick *volume.Brick) {
	if m.BrickTableBuf == nil {
		return
	}

	_, ok := m.MapOffsets[xbm]
	if !ok {
		return
	}

	// Find brick index in the sector
	sx, sy, sz := brickKey[0], brickKey[1], brickKey[2]
	bx, by, bz := brickKey[3], brickKey[4], brickKey[5]
	sKey := [3]int{sx, sy, sz}

	sector, ok := xbm.Sectors[sKey]
	if !ok {
		return
	}

	flatIdx := bx + by*4 + bz*16
	if (sector.BrickMask64 & (1 << flatIdx)) == 0 {
		return // Brick doesn't exist
	}

	packedIdx := sector.GetPackedIndex(flatIdx)
	// Use cached indices for O(1) deterministic access
	sectorInfo, cached := m.SectorIndices[sector]
	if !cached {
		return // Sector not implicitly allocated in current buffer
	}

	recordOffset := (sectorInfo.FirstBrickIndex + uint32(packedIdx)) * 16

	// Prepare brick record (16 bytes)
	var gpuAtlasOffset uint32
	if brick.Flags&volume.BrickFlagSolid != 0 {
		gpuAtlasOffset = brick.AtlasOffset
	} else {
		gpuAtlasOffset = brick.AtlasOffset
	}

	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], gpuAtlasOffset)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(brick.OccupancyMask64))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(brick.OccupancyMask64>>32))
	binary.LittleEndian.PutUint32(buf[12:16], brick.Flags)

	m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64(recordOffset), buf)
}

// UpdateSectorRecord updates a single sector's metadata in the sector table
func (m *GpuBufferManager) UpdateSectorRecord(xbm *volume.XBrickMap, sectorKey [3]int, sector *volume.Sector) {
	if m.SectorTableBuf == nil {
		return
	}

	offsets, ok := m.MapOffsets[xbm]
	if !ok {
		return
	}

	// Use cached indices for O(1) deterministic access
	sectorInfo, cached := m.SectorIndices[sector]
	if !cached {
		return
	}

	recordOffset := sectorInfo.SectorIndex * 32
	firstBrickIdx := sectorInfo.FirstBrickIndex - offsets[1] // Relative to brick base of this map

	// Prepare sector record (32 bytes)
	ox, oy, oz := int32(sectorKey[0]*32), int32(sectorKey[1]*32), int32(sectorKey[2]*32)
	buf := make([]byte, 32)

	// Origin (16 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(ox))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(oy))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(oz))

	// ID, lo, hi (16 bytes)
	binary.LittleEndian.PutUint32(buf[16:20], firstBrickIdx)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(sector.BrickMask64))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sector.BrickMask64>>32))

	m.Device.GetQueue().WriteBuffer(m.SectorTableBuf, uint64(recordOffset), buf)
}

func (m *GpuBufferManager) CreateBindGroups(pipeline *wgpu.ComputePipeline) {
	// Group 0
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
	m.BindGroup0, err = m.Device.CreateBindGroup(desc0)
	if err != nil {
		panic(err)
	}

	// Ensure voxel scene buffers for Group 2
	if m.SectorTableBuf == nil {
		m.ensureBuffer("SectorTableBuf", &m.SectorTableBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.BrickTableBuf == nil {
		m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.VoxelPayloadBuf == nil {
		m.ensureBuffer("VoxelPayloadBuf", &m.VoxelPayloadBuf, make([]byte, 512), wgpu.BufferUsageStorage, 0)
	}
	if m.MaterialBuf == nil {
		m.ensureBuffer("MaterialBuf", &m.MaterialBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.ObjectParamsBuf == nil {
		m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.Tree64Buf == nil {
		m.ensureBuffer("Tree64Buf", &m.Tree64Buf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.SectorGridBuf == nil {
		m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, make([]byte, 64), wgpu.BufferUsageStorage, 0)
	}
	if m.SectorGridParamsBuf == nil {
		m.ensureBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0)
	}

	// Group 2
	entries2 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.VoxelPayloadBuf, Size: wgpu.WholeSize},
		{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
		{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
		{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
		{Binding: 6, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
		{Binding: 7, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
	}
	desc2 := &wgpu.BindGroupDescriptor{
		Layout:  pipeline.GetBindGroupLayout(2),
		Entries: entries2,
	}
	m.BindGroup2, err = m.Device.CreateBindGroup(desc2)
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateDebugBindGroups(pipeline *wgpu.ComputePipeline) {
	// Group 0 for debug
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
