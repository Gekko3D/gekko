package gpu

import (
	"encoding/binary"
	"math"

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

	MaterialBuf     *wgpu.Buffer
	SectorTableBuf  *wgpu.Buffer
	BrickTableBuf   *wgpu.Buffer
	VoxelPayloadBuf *wgpu.Buffer
	ObjectParamsBuf *wgpu.Buffer
	Tree64Buf       *wgpu.Buffer

	BindGroup0      *wgpu.BindGroup
	DebugBindGroup0 *wgpu.BindGroup
	BindGroup2      *wgpu.BindGroup

	MapOffsets map[*volume.XBrickMap][3]uint32 // sectorBase, brickBase, payloadBase
}

func NewGpuBufferManager(device *wgpu.Device) *GpuBufferManager {
	return &GpuBufferManager{
		Device:     device,
		MapOffsets: make(map[*volume.XBrickMap][3]uint32),
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

	// 3. XBrickMap
	if m.updateXBrickMap(scene) {
		recreated = true
	}
	return recreated
}

func (m *GpuBufferManager) updateXBrickMap(scene *core.Scene) bool {
	recreated := false
	materials := []byte{}
	sectors := []byte{}
	bricks := []byte{}
	payload := []byte{}
	objParams := []byte{}
	tree64 := []byte{}

	m.MapOffsets = make(map[*volume.XBrickMap][3]uint32)

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

		if off, found := m.MapOffsets[xbm]; found {
			offsets = off
		} else {
			offsets[0] = uint32(len(sectors) / 32) // Sector Base (indices)
			offsets[1] = uint32(len(bricks) / 16)  // Brick Base (indices)
			offsets[2] = uint32(len(payload) / 4)  // Payload Base (U32 INDICES! Python used byte offset but shader uses u32 array)
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

			for sKey, sector := range xbm.Sectors {
				// We need current Sector index?
				// No, the sector table is just a list. Shader linearly scans sector table?
				// Shader: `find_sector` loops from `sector_base` to end?
				// `for (var i = sector_base; i < num_sectors; i++)`
				// Wait! If we have multiple objects, we only scan THIS object's sectors.
				// But we don't know "num_sectors" for this object easily unless we check `sector_id`?
				// No, loop `i` goes from `sector_base`.
				// Where does it stop?
				// Shader: `i < num_sectors`. This means it checks ALL sectors after base.
				// If we have Object A then Object B.
				// Object A check will check Object B's sectors too if they follow?
				// Yes, unless we break. But `find_sector` just looks for coords.
				// If coords match, it returns.
				// Sectors are unique per object usually (different coordinate spaces).
				// So it finds the first match.
				// It works as long as sectors don't collide between objects, or we don't care.
				// Collision: Object A has sector (0,0,0). Object B has sector (0,0,0).
				// Object A base=0. Object B base=10.
				// Scanning for A (base 0) might find B's sector (0,0,0) at index 10?
				// Yes. This seems to be a flaw or simplification in the shader reference.
				// `find_sector` doesn't know "count" of sectors.
				// But `find_sector` usually returns correct index.
				// Ideally we should limit the search scope. The shader doesn't have `sector_count` in ObjectParams.
				// I'll stick to the reference logic.

				firstBrickIdx := uint32(len(bricks)/16) - offsets[1] // Relative to brick base

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

						relPayloadOffset := uint32(len(payload)) - offsets[2]

						// If we want to support partial updates later, we need to respect existing atlas map?
						// For now, full rebuild is safer.

						// Serialize payload (512 bytes)
						for z := 0; z < 8; z++ {
							for y := 0; y < 8; y++ {
								for x := 0; x < 8; x++ {
									payload = append(payload, brick.Payload[x][y][z])
								}
							}
						}
						// Pad payload? It's u8s. shader `voxel_payload` is u32 array.
						// `load_u8` handles byte access.
						// So 512 bytes is fine.

						buf := make([]byte, 16)
						binary.LittleEndian.PutUint32(buf[0:4], relPayloadOffset)
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
		// s_base, b_base, p_base, mat_base, tree_base, lod, pad
		pBuf := make([]byte, 32)
		binary.LittleEndian.PutUint32(pBuf[0:4], offsets[0])
		binary.LittleEndian.PutUint32(pBuf[4:8], offsets[1])
		binary.LittleEndian.PutUint32(pBuf[8:12], offsets[2])
		binary.LittleEndian.PutUint32(pBuf[12:16], matBase)
		binary.LittleEndian.PutUint32(pBuf[16:20], tree64Base)
		binary.LittleEndian.PutUint32(pBuf[20:24], math.Float32bits(obj.LODThreshold))
		objParams = append(objParams, pBuf...)
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

	return recreated
}

func (m *GpuBufferManager) CreateBindGroups(pipeline *wgpu.ComputePipeline) {
	// Group 0
	entries0 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
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

	// Group 2
	entries2 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.VoxelPayloadBuf, Size: wgpu.WholeSize},
		{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
		{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
		{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize}, // Empty but bound
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
