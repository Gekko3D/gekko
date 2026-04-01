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

const lightSizeBytes = 496

func appendUint32LE(dst []byte, v uint32) []byte {
	n := len(dst)
	dst = append(dst, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(dst[n:], v)
	return dst
}

func appendFloat32LE(dst []byte, v float32) []byte {
	return appendUint32LE(dst, math.Float32bits(v))
}

func appendMat4LE(dst []byte, m [16]float32) []byte {
	for _, v := range m {
		dst = appendFloat32LE(dst, v)
	}
	return dst
}

func appendVec3PaddedLE(dst []byte, v [3]float32) []byte {
	dst = appendFloat32LE(dst, v[0])
	dst = appendFloat32LE(dst, v[1])
	dst = appendFloat32LE(dst, v[2])
	return appendUint32LE(dst, 0)
}

func appendVec4LE(dst []byte, v [4]float32) []byte {
	dst = appendFloat32LE(dst, v[0])
	dst = appendFloat32LE(dst, v[1])
	dst = appendFloat32LE(dst, v[2])
	return appendFloat32LE(dst, v[3])
}

func appendUVec4LE(dst []byte, v [4]uint32) []byte {
	dst = appendUint32LE(dst, v[0])
	dst = appendUint32LE(dst, v[1])
	dst = appendUint32LE(dst, v[2])
	return appendUint32LE(dst, v[3])
}

func writeObjectParamsData(dst []byte, obj *core.VoxelObject, alloc *ObjectGpuAllocation, matAlloc *MaterialGpuAllocation) {
	if len(dst) < objectParamsSizeBytes || obj == nil || obj.XBrickMap == nil || alloc == nil {
		return
	}
	binary.LittleEndian.PutUint32(dst[0:4], obj.XBrickMap.ID)
	binary.LittleEndian.PutUint32(dst[4:8], 0)
	binary.LittleEndian.PutUint32(dst[8:12], 0)
	if matAlloc != nil {
		binary.LittleEndian.PutUint32(dst[12:16], matAlloc.MaterialOffset*4)
	}
	binary.LittleEndian.PutUint32(dst[16:20], ^uint32(0))
	binary.LittleEndian.PutUint32(dst[20:24], math.Float32bits(obj.LODThreshold))
	binary.LittleEndian.PutUint32(dst[24:28], uint32(len(obj.XBrickMap.Sectors)))
	binary.LittleEndian.PutUint32(dst[32:36], obj.ShadowGroupID)
	binary.LittleEndian.PutUint32(dst[36:40], math.Float32bits(obj.ShadowSeamWorldEpsilon))
	if obj.IsTerrainChunk {
		binary.LittleEndian.PutUint32(dst[40:44], 1)
	}
	binary.LittleEndian.PutUint32(dst[44:48], obj.TerrainGroupID)
	binary.LittleEndian.PutUint32(dst[48:52], uint32(obj.TerrainChunkCoord[0]))
	binary.LittleEndian.PutUint32(dst[52:56], uint32(obj.TerrainChunkCoord[1]))
	binary.LittleEndian.PutUint32(dst[56:60], uint32(obj.TerrainChunkCoord[2]))
	binary.LittleEndian.PutUint32(dst[60:64], uint32(obj.TerrainChunkSize))
}

func buildInstanceData(objects []*core.VoxelObject) []byte {
	if len(objects) == 0 {
		return make([]byte, 208)
	}

	instData := make([]byte, 0, len(objects)*208)
	for i, obj := range objects {
		o2w := obj.Transform.ObjectToWorld()
		w2o := obj.Transform.WorldToObject()

		instData = appendMat4LE(instData, o2w)
		instData = appendMat4LE(instData, w2o)

		minB, maxB := [3]float32{}, [3]float32{}
		if obj.WorldAABB != nil {
			minB = obj.WorldAABB[0]
			maxB = obj.WorldAABB[1]
		}
		instData = appendVec3PaddedLE(instData, minB)
		instData = appendVec3PaddedLE(instData, maxB)

		lMin, lMax := obj.XBrickMap.ComputeAABB()
		instData = appendVec3PaddedLE(instData, [3]float32{lMin.X(), lMin.Y(), lMin.Z()})
		instData = appendVec3PaddedLE(instData, [3]float32{lMax.X(), lMax.Y(), lMax.Z()})

		instData = appendUint32LE(instData, uint32(i))
		instData = append(instData, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	return instData
}

func buildObjectParamsData(objects []*core.VoxelObject, allocations map[*volume.XBrickMap]*ObjectGpuAllocation, materialAllocations map[*core.VoxelObject]*MaterialGpuAllocation) []byte {
	if len(objects) == 0 {
		return make([]byte, objectParamsSizeBytes)
	}

	objParams := make([]byte, len(objects)*objectParamsSizeBytes)
	for i, obj := range objects {
		geomAlloc := allocations[obj.XBrickMap]
		matAlloc := materialAllocations[obj]
		writeObjectParamsData(objParams[i*objectParamsSizeBytes:], obj, geomAlloc, matAlloc)
	}
	return objParams
}

func buildLightsData(lights []core.Light) []byte {
	if len(lights) == 0 {
		return make([]byte, lightSizeBytes)
	}

	lightsData := make([]byte, 0, len(lights)*lightSizeBytes)
	for _, l := range lights {
		lightsData = appendVec4LE(lightsData, l.Position)
		lightsData = appendVec4LE(lightsData, l.Direction)
		lightsData = appendVec4LE(lightsData, l.Color)
		lightsData = appendVec4LE(lightsData, l.Params)
		lightsData = appendUVec4LE(lightsData, l.ShadowMeta)
		lightsData = appendMat4LE(lightsData, l.ViewProj)
		lightsData = appendMat4LE(lightsData, l.InvViewProj)
		for _, cascade := range l.DirectionalCascades {
			lightsData = appendMat4LE(lightsData, cascade.ViewProj)
			lightsData = appendMat4LE(lightsData, cascade.InvViewProj)
			lightsData = appendVec4LE(lightsData, cascade.Params)
		}
	}
	return lightsData
}

func (m *GpuBufferManager) buildLightsDataForGPU(lights []core.Light) []byte {
	gpuLights := make([]core.Light, len(lights))
	copy(gpuLights, lights)
	for i := range gpuLights {
		light := &gpuLights[i]
		if uint32(light.Params[2]) != core.LightTypeDirectional || light.ShadowMeta[1] == 0 {
			continue
		}
		baseLayer := light.ShadowMeta[0]
		for cascadeIdx := uint32(0); cascadeIdx < light.ShadowMeta[2]; cascadeIdx++ {
			layer := baseLayer + cascadeIdx
			if int(layer) >= len(m.shadowCacheStates) || int(layer) >= len(m.shadowCachedCascades) {
				continue
			}
			if m.shadowCacheStates[layer].Initialized {
				light.DirectionalCascades[cascadeIdx] = m.shadowCachedCascades[layer]
			}
		}
	}
	return buildLightsData(gpuLights)
}

func totalShadowLayers(lights []core.Light) uint32 {
	var total uint32
	for _, light := range lights {
		total += light.ShadowMeta[1]
	}
	return total
}

func expectedShadowLayers(lights []core.Light, hasCamera bool) uint32 {
	var total uint32
	for _, light := range lights {
		lightType := uint32(light.Params[2])
		if lightType == core.LightTypePoint && !light.CastsShadows {
			continue
		}
		switch lightType {
		case core.LightTypeDirectional:
			if hasCamera {
				total += core.DirectionalShadowCascadeCount
			}
		case core.LightTypePoint:
			total += core.PointShadowFaceCount
		case core.LightTypeSpot:
			total++
		}
	}
	return total
}

func (m *GpuBufferManager) UpdateScene(scene *core.Scene, camera *core.CameraState, aspect float32) bool {
	recreated := false

	// 1. Instances
	m.Profiler.BeginScope("Scene: Instances")
	instData := buildInstanceData(scene.VisibleObjects)
	if m.ensureBuffer("InstancesBuf", &m.InstancesBuf, instData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Instances")

	// 2. BVH
	bvhData := scene.BVHNodesBytes
	if len(bvhData) == 0 {
		bvhData = make([]byte, 64)
	}
	if m.ensureBuffer("BVHNodesBuf", &m.BVHNodesBuf, bvhData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 3. Light metadata drives shadow-only caster selection.
	m.Profiler.BeginScope("Scene: Lights")
	m.UpdateLights(scene, camera, aspect)
	m.Profiler.EndScope("Scene: Lights")

	// Update shadow acceleration structures from scene data.
	shadowInstData := buildInstanceData(scene.ShadowObjects)
	if m.ensureBuffer("ShadowInstancesBuf", &m.ShadowInstancesBuf, shadowInstData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	shadowBVHData := scene.ShadowBVHNodesBytes
	if len(shadowBVHData) == 0 {
		shadowBVHData = make([]byte, 64)
	}
	if m.ensureBuffer("ShadowBVHNodesBuf", &m.ShadowBVHNodesBuf, shadowBVHData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// 4. Lights
	lightsData := m.buildLightsDataForGPU(scene.Lights)
	if m.ensureBuffer("LightsBuf", &m.LightsBuf, lightsData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.ensureBuffer("ShadowLayerParamsBuf", &m.ShadowLayerParamsBuf, buildShadowLayerParamsData(m.ShadowLayerParams), wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.EnsureShadowMapCapacity(totalShadowLayers(scene.Lights)) {
		recreated = true
	}

	// 5. Voxel Data (Incremental / Paged)
	m.Profiler.BeginScope("Scene: Voxel")
	if m.UpdateVoxelData(scene) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Voxel")

	m.Profiler.BeginScope("Scene: Params")
	if m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, buildObjectParamsData(scene.VisibleObjects, m.Allocations, m.MaterialAllocations), wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.ensureBuffer("ShadowObjectParamsBuf", &m.ShadowObjectParamsBuf, buildObjectParamsData(scene.ShadowObjects, m.Allocations, m.MaterialAllocations), wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Params")

	// 6. Sector Hash Grid
	m.Profiler.BeginScope("Scene: Grid")
	if m.updateSectorGrid(scene) {
		recreated = true
	}
	if m.updateTerrainChunkLookup(scene) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Grid")
	_ = recreated
	return recreated
}

func buildCameraUniformData(viewProj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor mgl32.Vec3, skyAmbientMix float32, debugMode uint32, renderMode uint32, numLights uint32, screenW, screenH uint32, lightingQuality core.LightingQualityConfig) []byte {
	buf := make([]byte, 288)
	lightingQuality = lightingQuality.WithDefaults()

	writeMat := func(offset int, mat mgl32.Mat4) {
		for i, v := range mat {
			binary.LittleEndian.PutUint32(buf[offset+i*4:], math.Float32bits(v))
		}
	}

	writeMat(0, viewProj)
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
	binary.LittleEndian.PutUint32(buf[236:], math.Float32bits(skyAmbientMix))

	binary.LittleEndian.PutUint32(buf[240:], debugMode)
	binary.LittleEndian.PutUint32(buf[244:], renderMode)
	binary.LittleEndian.PutUint32(buf[248:], numLights)
	binary.LittleEndian.PutUint32(buf[252:], 0) // pad1

	binary.LittleEndian.PutUint32(buf[256:], math.Float32bits(float32(screenW)))
	binary.LittleEndian.PutUint32(buf[260:], math.Float32bits(float32(screenH)))
	binary.LittleEndian.PutUint32(buf[264:], 0) // pad2.x
	binary.LittleEndian.PutUint32(buf[268:], 0) // pad2.y
	binary.LittleEndian.PutUint32(buf[272:], math.Float32bits(float32(lightingQuality.AmbientOcclusion.SampleCount)))
	binary.LittleEndian.PutUint32(buf[276:], math.Float32bits(lightingQuality.AmbientOcclusion.Radius))
	binary.LittleEndian.PutUint32(buf[280:], math.Float32bits(lightingQuality.Shadow.DirectionalShadowSoftness))
	binary.LittleEndian.PutUint32(buf[284:], math.Float32bits(lightingQuality.Shadow.SpotShadowSoftness))

	return buf
}

func (m *GpuBufferManager) UpdateCamera(viewProj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor mgl32.Vec3, skyAmbientMix float32, debugMode uint32, renderMode uint32, numLights uint32, screenW, screenH uint32, lightingQuality core.LightingQualityConfig) {
	buf := buildCameraUniformData(viewProj, invView, invProj, camPos, lightPos, ambientColor, skyAmbientMix, debugMode, renderMode, numLights, screenW, screenH, lightingQuality)

	if m.CameraBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "CameraUB",
			Size:  288,
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

func (m *GpuBufferManager) UpdateLights(scene *core.Scene, camera *core.CameraState, aspect float32) {
	m.shadowDirectionalVolumes = m.shadowDirectionalVolumes[:0]
	m.shadowSpotVolumes = m.shadowSpotVolumes[:0]
	m.shadowPointVolumes = m.shadowPointVolumes[:0]
	m.ensureShadowCacheCapacity(expectedShadowLayers(scene.Lights, camera != nil))
	lightingQuality := m.LightingQuality.WithDefaults()
	cascadeDistances := lightingQuality.Shadow.DirectionalCascadeDistances
	spotBands := lightingQuality.Shadow.SpotShadowDistanceBands

	nextShadowLayer := uint32(0)
	for i := range scene.Lights {
		l := &scene.Lights[i]
		lightType := uint32(l.Params[2])
		pos := mgl32.Vec3{l.Position[0], l.Position[1], l.Position[2]}
		dir := mgl32.Vec3{l.Direction[0], l.Direction[1], l.Direction[2]}
		l.ShadowMeta = [4]uint32{}
		l.ViewProj = [16]float32{}
		l.InvViewProj = [16]float32{}
		for c := range l.DirectionalCascades {
			l.DirectionalCascades[c] = core.DirectionalShadowCascade{}
		}
		if lightType == core.LightTypePoint && !l.CastsShadows {
			continue
		}

		if lightType == core.LightTypeDirectional {
			if dir.Len() < 1e-4 {
				dir = mgl32.Vec3{0, -1, 0}
			}
			dir = dir.Normalize()
			l.Direction[0], l.Direction[1], l.Direction[2] = dir.X(), dir.Y(), dir.Z()
			if camera != nil {
				l.ShadowMeta[0] = nextShadowLayer
				l.ShadowMeta[1] = core.DirectionalShadowCascadeCount
				l.ShadowMeta[2] = core.DirectionalShadowCascadeCount

				splitNear := float32(0.0)
				for cascadeIdx := 0; cascadeIdx < core.DirectionalShadowCascadeCount; cascadeIdx++ {
					splitFar := cascadeDistances[cascadeIdx]
					tier := directionalCascadeTier(uint32(cascadeIdx))
					effectiveResolution := shadowAtlasLayerResolution
					cascade, volume := buildDirectionalShadowCascade(camera, aspect, dir, splitNear, splitFar, effectiveResolution)
					l.DirectionalCascades[cascadeIdx] = cascade
					layer := nextShadowLayer + uint32(cascadeIdx)
					m.ShadowLayerParams[layer] = ShadowLayerParams{
						Layer:               layer,
						LightIndex:          uint32(i),
						CascadeIndex:        uint32(cascadeIdx),
						Kind:                core.ShadowUpdateKindDirectional,
						Tier:                tier,
						EffectiveResolution: effectiveResolution,
						CadenceFrames:       shadowTierCadence(tier),
						UVScale: [2]float32{
							float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
							float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
						},
						LightSignature: hashMat4Signature(cascade.ViewProj,
							math.Float32bits(dir.X()),
							math.Float32bits(dir.Y()),
							math.Float32bits(dir.Z()),
							uint32(cascadeIdx),
							effectiveResolution,
						),
					}
					if cascadeIdx == core.DirectionalShadowCascadeCount-1 {
						m.shadowDirectionalVolumes = append(m.shadowDirectionalVolumes, volume)
					}
					splitNear = splitFar
				}
				nextShadowLayer += core.DirectionalShadowCascadeCount
			}
		} else if lightType == core.LightTypeSpot {
			up := shadowUpVector(dir)
			if dir.Len() < 1e-4 {
				dir = mgl32.Vec3{0, -1, 0}
			}
			dir = dir.Normalize()
			l.Direction[0], l.Direction[1], l.Direction[2] = dir.X(), dir.Y(), dir.Z()
			fov := math.Acos(float64(l.Params[1])) * 2.0
			proj := mgl32.Perspective(float32(fov), 1.0, 0.1, l.Params[0])
			view := mgl32.LookAtV(pos, pos.Add(dir), up)
			vp := proj.Mul4(view)
			tier := core.ShadowTierFar
			if camera != nil {
				tier = classifySpotShadowTier(camera.Position, pos, spotBands)
			}
			effectiveResolution := shadowTierResolution(tier)
			l.ViewProj = [16]float32(vp)
			l.InvViewProj = [16]float32(vp.Inv())
			l.ShadowMeta[0] = nextShadowLayer
			l.ShadowMeta[1] = 1
			m.ShadowLayerParams[nextShadowLayer] = ShadowLayerParams{
				Layer:               nextShadowLayer,
				LightIndex:          uint32(i),
				CascadeIndex:        0,
				Kind:                core.ShadowUpdateKindSpot,
				Tier:                tier,
				EffectiveResolution: effectiveResolution,
				CadenceFrames:       shadowTierCadence(tier),
				UVScale: [2]float32{
					float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
					float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
				},
				LightSignature: hashMat4Signature(l.ViewProj,
					math.Float32bits(pos.X()),
					math.Float32bits(pos.Y()),
					math.Float32bits(pos.Z()),
					math.Float32bits(dir.X()),
					math.Float32bits(dir.Y()),
					math.Float32bits(dir.Z()),
					effectiveResolution,
				),
			}
			nextShadowLayer++
			m.shadowSpotVolumes = append(m.shadowSpotVolumes, spotShadowCullVolume{
				Position: pos,
				Dir:      dir,
				Range:    l.Params[0],
				CosCone:  l.Params[1],
			})
		} else if lightType == core.LightTypePoint {
			tier := core.ShadowTierFar
			if camera != nil {
				tier = classifySpotShadowTier(camera.Position, pos, spotBands)
			}
			effectiveResolution := pointShadowTierResolution(tier)
			l.ShadowMeta[0] = nextShadowLayer
			l.ShadowMeta[1] = core.PointShadowFaceCount
			for face := uint32(0); face < core.PointShadowFaceCount; face++ {
				layer := nextShadowLayer + face
				m.ShadowLayerParams[layer] = ShadowLayerParams{
					Layer:               layer,
					LightIndex:          uint32(i),
					CascadeIndex:        face,
					Kind:                core.ShadowUpdateKindPoint,
					Tier:                tier,
					EffectiveResolution: effectiveResolution,
					CadenceFrames:       shadowTierCadence(tier),
					UVScale: [2]float32{
						float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
						float32(effectiveResolution) / float32(shadowAtlasLayerResolution),
					},
					LightSignature: hashShadowSignature(
						math.Float32bits(pos.X()),
						math.Float32bits(pos.Y()),
						math.Float32bits(pos.Z()),
						math.Float32bits(l.Params[0]),
						face,
						effectiveResolution,
					),
				}
			}
			nextShadowLayer += core.PointShadowFaceCount
			m.shadowPointVolumes = append(m.shadowPointVolumes, pointShadowCullVolume{
				Position: pos,
				Range:    l.Params[0],
			})
		}
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
