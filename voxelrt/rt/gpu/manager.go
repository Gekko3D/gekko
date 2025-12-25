package gpu

import (
	"encoding/binary"
	"math"
	"sort"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"

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

	// G-Buffer Textures
	GBufferDepth    *wgpu.Texture
	GBufferNormal   *wgpu.Texture
	GBufferMaterial *wgpu.Texture
	GBufferPosition *wgpu.Texture

	// G-Buffer Views
	DepthView    *wgpu.TextureView
	NormalView   *wgpu.TextureView
	MaterialView *wgpu.TextureView
	PositionView *wgpu.TextureView

	// Shadow Map Resources
	ShadowMapArray *wgpu.Texture
	ShadowMapView  *wgpu.TextureView

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

	MapOffsets    map[*volume.XBrickMap][3]uint32 // sectorBase, brickBase, payloadBase
	AllocatedMaps map[*volume.XBrickMap]bool      // Track which maps have been fully uploaded

	// Batch update tracking
	BatchMode      bool                       // Enable batching of updates within a frame
	PendingUpdates map[*volume.XBrickMap]bool // Maps with pending updates in current batch

	// Cached Sector indices for deterministic updates
	SectorIndices map[*volume.Sector]SectorGpuInfo
}

type SectorGpuInfo struct {
	SectorIndex     uint32
	FirstBrickIndex uint32
}

func NewGpuBufferManager(device *wgpu.Device) *GpuBufferManager {
	return &GpuBufferManager{
		Device:         device,
		MapOffsets:     make(map[*volume.XBrickMap][3]uint32),
		AllocatedMaps:  make(map[*volume.XBrickMap]bool),
		PendingUpdates: make(map[*volume.XBrickMap]bool),
		BatchMode:      false,
		SectorIndices:  make(map[*volume.Sector]SectorGpuInfo),
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

func (m *GpuBufferManager) UpdateCamera(view, proj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor mgl32.Vec3, debugMode uint32, renderMode uint32) {
	// Struct CameraData {
	//   view_proj: mat4x4<f32>;   --  64
	//   inv_view: mat4x4<f32>;    -- 128
	//   inv_proj: mat4x4<f32>;    -- 192
	//   cam_pos: vec4<f32>;       -- 208
	//   light_pos: vec4<f32>;     -- 224
	//   ambient_color: vec4<f32>; -- 240
	//   debug_mode: u32;          -- 244 (offset 240)
	//   render_mode: u32;         -- 248 (offset 244)
	// } -> 256 bytes (padded)

	buf := make([]byte, 256)

	// Helper to write matrix
	writeMat := func(offset int, mat mgl32.Mat4) {
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

	// Debug + Render Mode
	binary.LittleEndian.PutUint32(buf[240:], debugMode)
	binary.LittleEndian.PutUint32(buf[244:], renderMode)

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
	m.UpdateLights(scene)
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
		// Calculate ViewProj if it's not already set or needs refresh
		// In a real engine this might be done in a separate system,
		// but here we can ensure it's up to date.

		// Pos (16)
		lightsData = append(lightsData, vec4ToBytes(l.Position)...)
		// Dir (16)
		lightsData = append(lightsData, vec4ToBytes(l.Direction)...)
		// Color (16)
		lightsData = append(lightsData, vec4ToBytes(l.Color)...)
		// Params (16)
		lightsData = append(lightsData, vec4ToBytes(l.Params)...)
		// ViewProj (64)
		lightsData = append(lightsData, mat4ToBytes(l.ViewProj)...)
		// InvViewProj (64)
		lightsData = append(lightsData, mat4ToBytes(l.InvViewProj)...)
	}

	if len(lightsData) == 0 {
		lightsData = make([]byte, 192) // dummy (one full light struct)
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
			// Directional light needs an ortho projection
			// We center the ortho projection around the scene or a reasonable area.
			// For now, let's use a conservative fixed box.
			// For now, fixed large size to cover typical demo scene
			size := float32(500.0)
			proj = mgl32.Ortho(-size, size, -size, size, 0.1, 2000.0)

			// View matrix: look from light position in light direction
			target := pos.Add(dir)
			view = mgl32.LookAtV(pos, target, up)
		} else if lightType == 2 { // Spot
			// Spot light needs a perspective projection
			fov := math.Acos(float64(l.Params[1])) * 2.0 // Params[1] is cos(cone_angle)
			proj = mgl32.Perspective(float32(fov), 1.0, 0.1, l.Params[0])

			target := pos.Add(dir)
			view = mgl32.LookAtV(pos, target, up)
		} else { // Point (Type 0)
			// Point lights are tricky with 2D shadow maps.
			// For now, use a default perspective looking forward.
			proj = mgl32.Perspective(mgl32.DegToRad(90), 1.0, 0.1, l.Params[0])
			view = mgl32.LookAtV(pos, pos.Add(mgl32.Vec3{0, 0, 1}), up)
		}

		vp := proj.Mul4(view)
		l.ViewProj = [16]float32(vp)
		l.InvViewProj = [16]float32(vp.Inv())
	}
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

			// Pre-allocate space for this map's payload chunk
			// Use NextAtlasOffset as the size required for this map
			// Note: NextAtlasOffset is in bytes.
			currentMapPayloadSize := xbm.NextAtlasOffset
			// Ensure we have enough space (sometimes NextAtlasOffset might be lagging if slots were freed?)
			// Actually, FreeAtlasSlot doesn't reduce NextAtlasOffset.
			// But if we just loaded from VOX, NextAtlasOffset might be 0 if we didn't update it?
			// The VOX loader uses SetVoxel which allocates slots, so NextAtlasOffset should be correct.
			// Wait, NewXBrickMap initializes NextAtlasOffset to 0.
			// AllocateAtlasSlot increments it.

			// Pad payload with zeros for this map
			zeros := make([]byte, currentMapPayloadSize)
			payload = append(payload, zeros...)

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
				for i := 0; i < 64; i++ {
					if (sector.BrickMask64 & (1 << i)) != 0 {
						bx, by, bz := i%4, (i/4)%4, i/16
						brick := sector.GetBrick(bx, by, bz)
						// Brick Record (16 bytes)
						// atlas (4), lo (4), hi (4), flags (4)

						// Calculate Atlas Offset / Palette Index
						var gpuAtlasOffset uint32
						if brick.Flags&volume.BrickFlagSolid != 0 {
							gpuAtlasOffset = brick.AtlasOffset
						} else {
							// Non-solid brick: Use the assigned AtlasOffset
							// This offset is relative to the start of this map's payload chunk.
							gpuAtlasOffset = brick.AtlasOffset

							// Write payload to the pre-allocated buffer
							// The absolute position is offsets[2] + gpuAtlasOffset
							// We appended `zeros` to payload, so payload[offsets[2]:] is the chunk for this map.
							baseIdx := int(offsets[2] + gpuAtlasOffset)
							// Safety: if NextAtlasOffset underestimates required space (or is zero for fresh maps),
							// grow the payload slice to fit this brick's 512-byte payload.
							required := baseIdx + 512
							if required > len(payload) {
								payload = append(payload, make([]byte, required-len(payload))...)
							}

							// Serialize payload (512 bytes)
							// Direct write into slice
							localIdx := 0
							for z := 0; z < 8; z++ {
								for y := 0; y < 8; y++ {
									for x := 0; x < 8; x++ {
										payload[baseIdx+localIdx] = brick.Payload[x][y][z]
										localIdx++
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
			{Binding: 2, Buffer: m.VoxelPayloadBuf, Size: wgpu.WholeSize},
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
func (m *GpuBufferManager) UpdateParticles(instances []core.ParticleInstance) {
	// Ensure non-zero buffer size for bind group validity
	minSize := uint64(32)
	size := uint64(len(instances)) * 32
	if size == 0 {
		size = minSize
	}
	// Create/resize buffer if needed
	if m.ParticleInstancesBuf == nil || m.ParticleInstancesBuf.GetSize() < size {
		if m.ParticleInstancesBuf != nil {
			m.ParticleInstancesBuf.Release()
		}
		buf, err := m.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "ParticleInstances",
			Size:  size,
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			panic(err)
		}
		m.ParticleInstancesBuf = buf
	}
	// Upload if we have any instances
	if len(instances) > 0 {
		vSize := uint64(len(instances)) * 32
		bytes := unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), vSize)
		m.Device.GetQueue().WriteBuffer(m.ParticleInstancesBuf, 0, bytes)
	}
	m.ParticleCount = uint32(len(instances))
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

	// Group 0: Scene + Lights
	m.ShadowBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
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
			{Binding: 2, Buffer: m.VoxelPayloadBuf, Size: wgpu.WholeSize},
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

func (m *GpuBufferManager) DispatchShadowPass(encoder *wgpu.CommandEncoder, numLights uint32) {
	if m.ShadowPipeline == nil || m.ShadowBindGroup0 == nil {
		return
	}

	cPass := encoder.BeginComputePass(nil)
	cPass.SetPipeline(m.ShadowPipeline)
	cPass.SetBindGroup(0, m.ShadowBindGroup0, nil)
	cPass.SetBindGroup(1, m.ShadowBindGroup1, nil)
	cPass.SetBindGroup(2, m.ShadowBindGroup2, nil)

	// Dispatch for 1024x1024 shadow maps
	wgX := (1024 + 7) / 8
	wgY := (1024 + 7) / 8
	cPass.DispatchWorkgroups(uint32(wgX), uint32(wgY), numLights)
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
