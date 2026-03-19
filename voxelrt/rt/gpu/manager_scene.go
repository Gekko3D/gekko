package gpu

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

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
	if m.EnsureShadowMapCapacity(uint32(len(scene.Lights))) {
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
	if m.updateTerrainChunkLookup(scene) {
		recreated = true
	}
	_ = recreated
	return recreated
}

func (m *GpuBufferManager) UpdateCamera(view, proj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor mgl32.Vec3, debugMode uint32, renderMode uint32, numLights uint32, screenW, screenH uint32) {
	buf := make([]byte, 272)

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
	binary.LittleEndian.PutUint32(buf[252:], 0) // pad1

	binary.LittleEndian.PutUint32(buf[256:], math.Float32bits(float32(screenW)))
	binary.LittleEndian.PutUint32(buf[260:], math.Float32bits(float32(screenH)))
	binary.LittleEndian.PutUint32(buf[264:], 0) // pad2.x
	binary.LittleEndian.PutUint32(buf[268:], 0) // pad2.y

	if m.CameraBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "CameraUB",
			Size:  272,
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
	m.BatchMode = false
	// Note: PendingUpdates will be processed by the next UpdateScene(scene) call,
	// which has the necessary scene context to access all objects.
}

func (m *GpuBufferManager) UpdateLights(scene *core.Scene) {
	for i := range scene.Lights {
		l := &scene.Lights[i]
		lightType := uint32(l.Params[2])
		pos := mgl32.Vec3{l.Position[0], l.Position[1], l.Position[2]}
		dir := mgl32.Vec3{l.Direction[0], l.Direction[1], l.Direction[2]}
		var view, proj mgl32.Mat4
		up := mgl32.Vec3{0, 1, 0}
		if float64(absf(dir.Y())) > 0.99 {
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
	totalSectors := 0
	for _, obj := range scene.Objects {
		if xbm := obj.XBrickMap; xbm != nil {
			totalSectors += len(xbm.Sectors)
		}
	}

	// Optimization: Skip rebuild if nothing structurally changed and count is the same
	// We use the new Scene.StructureRevision to detect any Add/Remove object operations,
	// even if the exact number of sectors happens to exactly offset between despawn & spawn.
	if totalSectors == m.lastTotalSectors && uint64(scene.StructureRevision) == m.lastSceneRevision && m.SectorGridBuf != nil {
		return false
	}
	m.lastTotalSectors = totalSectors
	m.lastSceneRevision = uint64(scene.StructureRevision)
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
		binary.LittleEndian.PutUint32(m.gridDataPool[i*32+20:], 0xFFFFFFFF) // sector_idx = -1
	}

	hash := func(x, y, z int32, base uint32) uint32 {
		h := uint32(x)*73856093 ^ uint32(y)*19349663 ^ uint32(z)*83492791 ^ base*99999989
		return h % uint32(gridSize)
	}

	processedMaps := make(map[*volume.XBrickMap]bool)
	for _, obj := range scene.Objects {
		xbm := obj.XBrickMap
		if xbm == nil || processedMaps[xbm] {
			continue
		}
		processedMaps[xbm] = true
		baseIdx := obj.XBrickMap.ID

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
				sectorIdx := binary.LittleEndian.Uint32(m.gridDataPool[probeIdx*32+20:])
				if sectorIdx == 0xFFFFFFFF {
					// Found empty slot
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+0:], uint32(sx))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+4:], uint32(sy))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+8:], uint32(sz))
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+12:], 0) // Padding for vec4
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+16:], baseIdx)
					binary.LittleEndian.PutUint32(m.gridDataPool[probeIdx*32+20:], info.SlotIndex)
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
