package gpu

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/gekko3d/gekko/voxelrt/rt/bvh"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

const lightSizeBytes = 496
const forceHashLookupEnv = "GEKKO_XBM_FORCE_HASH_LOOKUP"
const maxSpotShadowFOV = math.Pi - 1e-4

type directSectorLookupMetadata struct {
	LookupMode uint32
	Origin     [3]int32
	Extent     [3]uint32
	TableBase  uint32
}

func defaultDirectSectorLookupMetadata() directSectorLookupMetadata {
	return directSectorLookupMetadata{
		LookupMode: LookupModeHash,
		TableBase:  DirectSectorLookupInvalid,
	}
}

func directSectorLookupQualified(boundsVolume int64, liveSectors int) bool {
	if liveSectors <= 0 || boundsVolume <= 0 {
		return false
	}
	if boundsVolume > DirectSectorLookupMaxCells {
		return false
	}
	return boundsVolume <= int64(DirectSectorLookupDensityMax)*int64(liveSectors)
}

func flattenDirectSectorLookupIndex(local [3]uint32, extent [3]uint32) uint32 {
	return local[0] + local[1]*extent[0] + local[2]*extent[0]*extent[1]
}

func buildDirectSectorLookupForMap(xbm *volume.XBrickMap, sectorToInfo map[*volume.Sector]SectorGpuInfo) (directSectorLookupMetadata, []uint32, bool) {
	meta := defaultDirectSectorLookupMetadata()
	if xbm == nil || len(xbm.Sectors) == 0 {
		return meta, nil, false
	}
	if os.Getenv(forceHashLookupEnv) == "1" {
		return meta, nil, false
	}

	first := true
	var minCoord [3]int32
	var maxCoord [3]int32
	for sKey := range xbm.Sectors {
		coord := [3]int32{int32(sKey[0]), int32(sKey[1]), int32(sKey[2])}
		if first {
			minCoord = coord
			maxCoord = coord
			first = false
			continue
		}
		for axis := 0; axis < 3; axis++ {
			if coord[axis] < minCoord[axis] {
				minCoord[axis] = coord[axis]
			}
			if coord[axis] > maxCoord[axis] {
				maxCoord[axis] = coord[axis]
			}
		}
	}

	extentX := int64(maxCoord[0]-minCoord[0]) + 1
	extentY := int64(maxCoord[1]-minCoord[1]) + 1
	extentZ := int64(maxCoord[2]-minCoord[2]) + 1
	boundsVolume := extentX * extentY * extentZ
	if !directSectorLookupQualified(boundsVolume, len(xbm.Sectors)) {
		return meta, nil, false
	}

	extent := [3]uint32{uint32(extentX), uint32(extentY), uint32(extentZ)}
	table := make([]uint32, int(boundsVolume))
	for i := range table {
		table[i] = DirectSectorLookupInvalid
	}

	for sKey, sector := range xbm.Sectors {
		info, ok := sectorToInfo[sector]
		if !ok {
			return meta, nil, false
		}
		local := [3]uint32{
			uint32(int32(sKey[0]) - minCoord[0]),
			uint32(int32(sKey[1]) - minCoord[1]),
			uint32(int32(sKey[2]) - minCoord[2]),
		}
		table[flattenDirectSectorLookupIndex(local, extent)] = info.SlotIndex
	}

	meta.LookupMode = LookupModeDirect
	meta.Origin = minCoord
	meta.Extent = extent
	return meta, table, true
}

func buildDirectSectorLookupData(scene *core.Scene, sectorToInfo map[*volume.Sector]SectorGpuInfo, allocations map[*volume.XBrickMap]*ObjectGpuAllocation, baseWordOffset uint32) []byte {
	if scene == nil {
		return make([]byte, 4)
	}

	tables := make([]uint32, 0)
	processedMaps := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		xbm := target.mapRef
		if processedMaps[xbm] {
			continue
		}
		processedMaps[xbm] = true

		alloc := allocations[xbm]
		if alloc == nil || (alloc.lookupAdmissionKnown && !alloc.lookupAdmitted) {
			continue
		}
		alloc.DirectLookup = defaultDirectSectorLookupMetadata()

		// Production allocation snapshots remain valid while CPU topology is
		// deferred. Nil snapshots retain old standalone helper fixture behavior.
		lookupMap := xbm
		if alloc.Sectors != nil {
			lookupMap = &volume.XBrickMap{Sectors: alloc.Sectors}
		}
		meta, table, ok := buildDirectSectorLookupForMap(lookupMap, sectorToInfo)
		if !ok {
			continue
		}
		meta.TableBase = baseWordOffset + uint32(len(tables))
		alloc.DirectLookup = meta
		tables = append(tables, table...)
	}

	if len(tables) == 0 {
		return make([]byte, 4)
	}

	buf := make([]byte, len(tables)*4)
	for i, sectorIdx := range tables {
		binary.LittleEndian.PutUint32(buf[i*4:], sectorIdx)
	}
	return buf
}

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
	if len(dst) < objectParamsSizeBytes || obj == nil || obj.RenderVoxelMap() == nil || alloc == nil {
		return
	}
	if !materialPublicationReady(obj, alloc, matAlloc) {
		writeRejectedObjectParams(dst)
		return
	}
	binary.LittleEndian.PutUint32(dst[0:4], obj.RenderVoxelMap().ID)
	binary.LittleEndian.PutUint32(dst[4:8], 0)
	binary.LittleEndian.PutUint32(dst[8:12], 0)
	if matAlloc != nil {
		binary.LittleEndian.PutUint32(dst[12:16], matAlloc.MaterialOffset*4)
	}
	binary.LittleEndian.PutUint32(dst[16:20], ^uint32(0))
	binary.LittleEndian.PutUint32(dst[20:24], math.Float32bits(obj.LODThreshold))
	sectorCount := len(obj.RenderVoxelMap().Sectors)
	if alloc.Sectors != nil {
		sectorCount = len(alloc.Sectors)
	}
	binary.LittleEndian.PutUint32(dst[24:28], uint32(sectorCount))
	binary.LittleEndian.PutUint32(dst[28:32], uint32(obj.AmbientOcclusionMode))
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
	if obj.IsPlanetTile {
		binary.LittleEndian.PutUint32(dst[64:68], 1)
	}
	binary.LittleEndian.PutUint32(dst[68:72], obj.PlanetTileGroupID)
	binary.LittleEndian.PutUint32(dst[72:76], obj.EmitterLinkID)
	binary.LittleEndian.PutUint32(dst[80:84], uint32(obj.PlanetTileFace))
	binary.LittleEndian.PutUint32(dst[84:88], uint32(obj.PlanetTileLevel))
	binary.LittleEndian.PutUint32(dst[88:92], uint32(obj.PlanetTileX))
	binary.LittleEndian.PutUint32(dst[92:96], uint32(obj.PlanetTileY))
	// The tail packs direct sector lookup metadata consumed by WGSL as:
	// direct_lookup_origin_mode: vec4<i32> and direct_lookup_extent_base: vec4<u32>.
	binary.LittleEndian.PutUint32(dst[96:100], uint32(alloc.DirectLookup.Origin[0]))
	binary.LittleEndian.PutUint32(dst[100:104], uint32(alloc.DirectLookup.Origin[1]))
	binary.LittleEndian.PutUint32(dst[104:108], uint32(alloc.DirectLookup.Origin[2]))
	binary.LittleEndian.PutUint32(dst[108:112], alloc.DirectLookup.LookupMode)
	binary.LittleEndian.PutUint32(dst[112:116], alloc.DirectLookup.Extent[0])
	binary.LittleEndian.PutUint32(dst[116:120], alloc.DirectLookup.Extent[1])
	binary.LittleEndian.PutUint32(dst[120:124], alloc.DirectLookup.Extent[2])
	binary.LittleEndian.PutUint32(dst[124:128], alloc.DirectLookup.TableBase)
}

func buildInstanceData(objects []*core.VoxelObject, renderOrigin mgl32.Vec3) []byte {
	objects = filterRenderObjects(objects)
	if len(objects) == 0 {
		return make([]byte, 208)
	}

	instData := make([]byte, 0, len(objects)*208)
	for i, obj := range objects {
		o2w := renderRelativeObjectToWorld(obj.RenderObjectToWorld(), renderOrigin)
		w2o := renderRelativeWorldToObject(obj.RenderWorldToObject(), renderOrigin)

		instData = appendMat4LE(instData, o2w)
		instData = appendMat4LE(instData, w2o)

		minB, maxB := [3]float32{}, [3]float32{}
		if obj.RenderWorldBounds() != nil {
			minB = obj.RenderWorldBounds()[0].Sub(renderOrigin)
			maxB = obj.RenderWorldBounds()[1].Sub(renderOrigin)
		}
		instData = appendVec3PaddedLE(instData, minB)
		instData = appendVec3PaddedLE(instData, maxB)

		lMin, lMax := obj.RenderVoxelMap().ComputeAABB()
		instData = appendVec3PaddedLE(instData, [3]float32{lMin.X(), lMin.Y(), lMin.Z()})
		instData = appendVec3PaddedLE(instData, [3]float32{lMax.X(), lMax.Y(), lMax.Z()})

		instData = appendUint32LE(instData, uint32(i))
		instData = append(instData, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	return instData
}

func renderRelativeObjectToWorld(objectToWorld mgl32.Mat4, renderOrigin mgl32.Vec3) mgl32.Mat4 {
	if renderOrigin == (mgl32.Vec3{}) {
		return objectToWorld
	}
	return mgl32.Translate3D(-renderOrigin.X(), -renderOrigin.Y(), -renderOrigin.Z()).Mul4(objectToWorld)
}

func renderRelativeWorldToObject(worldToObject mgl32.Mat4, renderOrigin mgl32.Vec3) mgl32.Mat4 {
	if renderOrigin == (mgl32.Vec3{}) {
		return worldToObject
	}
	return worldToObject.Mul4(mgl32.Translate3D(renderOrigin.X(), renderOrigin.Y(), renderOrigin.Z()))
}

func buildRenderBVHData(objects []*core.VoxelObject, renderOrigin mgl32.Vec3) []byte {
	objects = filterRenderObjects(objects)
	if len(objects) == 0 {
		return make([]byte, 64)
	}
	aabbs := make([][2]mgl32.Vec3, len(objects))
	for i, obj := range objects {
		if obj == nil || obj.RenderWorldBounds() == nil {
			continue
		}
		aabbs[i] = [2]mgl32.Vec3{
			obj.RenderWorldBounds()[0].Sub(renderOrigin),
			obj.RenderWorldBounds()[1].Sub(renderOrigin),
		}
	}
	builder := &bvh.TLASBuilder{}
	return builder.Build(aabbs)
}

func buildObjectParamsData(objects []*core.VoxelObject, allocations map[*volume.XBrickMap]*ObjectGpuAllocation, materialAllocations map[*core.VoxelObject]*MaterialGpuAllocation) []byte {
	objects = filterRenderObjects(objects)
	if len(objects) == 0 {
		return make([]byte, objectParamsSizeBytes)
	}

	objParams := make([]byte, len(objects)*objectParamsSizeBytes)
	for i, obj := range objects {
		geomAlloc := allocations[obj.RenderVoxelMap()]
		matAlloc := materialAllocations[obj]
		writeObjectParamsData(objParams[i*objectParamsSizeBytes:], obj, geomAlloc, matAlloc)
	}
	return objParams
}

func buildLightsData(lights []core.Light, renderOrigin mgl32.Vec3) []byte {
	if len(lights) == 0 {
		return make([]byte, lightSizeBytes)
	}

	lightsData := make([]byte, 0, len(lights)*lightSizeBytes)
	for _, l := range lights {
		l = renderRelativeLight(l, renderOrigin)
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

func renderRelativeLight(light core.Light, renderOrigin mgl32.Vec3) core.Light {
	if renderOrigin == (mgl32.Vec3{}) {
		return light
	}

	light.Position[0] -= renderOrigin.X()
	light.Position[1] -= renderOrigin.Y()
	light.Position[2] -= renderOrigin.Z()

	translateToAbsolute := mgl32.Translate3D(renderOrigin.X(), renderOrigin.Y(), renderOrigin.Z())
	translateToRelative := mgl32.Translate3D(-renderOrigin.X(), -renderOrigin.Y(), -renderOrigin.Z())
	relViewProj := mgl32.Mat4(light.ViewProj).Mul4(translateToAbsolute)
	relInvViewProj := translateToRelative.Mul4(mgl32.Mat4(light.InvViewProj))
	light.ViewProj = [16]float32(relViewProj)
	light.InvViewProj = [16]float32(relInvViewProj)
	for i := range light.DirectionalCascades {
		cascade := &light.DirectionalCascades[i]
		relCascadeViewProj := mgl32.Mat4(cascade.ViewProj).Mul4(translateToAbsolute)
		relCascadeInvViewProj := translateToRelative.Mul4(mgl32.Mat4(cascade.InvViewProj))
		cascade.ViewProj = [16]float32(relCascadeViewProj)
		cascade.InvViewProj = [16]float32(relCascadeInvViewProj)
	}
	return light
}

func (m *GpuBufferManager) shadowLayerCacheValid(layer uint32, shadowRevision uint64) bool {
	if int(layer) >= len(m.shadowCacheStates) || int(layer) >= len(m.ShadowLayerParams) {
		return false
	}
	state := m.shadowCacheStates[layer]
	params := m.ShadowLayerParams[layer]
	if params.Kind != core.ShadowUpdateKindDirectional {
		return m.localShadowLayerValid(params, state)
	}
	return m.directionalShadowLayerValid(params, state)
}

func (m *GpuBufferManager) localShadowCacheReady(light core.Light, shadowRevision uint64) bool {
	layerCount := light.ShadowMeta[1]
	if layerCount == 0 {
		return false
	}
	baseLayer := light.ShadowMeta[0]
	for layerOffset := uint32(0); layerOffset < layerCount; layerOffset++ {
		if !m.shadowLayerCacheValid(baseLayer+layerOffset, shadowRevision) {
			return false
		}
	}
	return true
}

func (m *GpuBufferManager) buildLightsDataForGPU(lights []core.Light, shadowRevision uint64) []byte {
	m.observeShadowUploadRevision()
	gpuLights := make([]core.Light, len(lights))
	copy(gpuLights, lights)
	for i := range gpuLights {
		light := &gpuLights[i]
		lightType := uint32(light.Params[2])
		if lightType != core.LightTypeDirectional && light.ShadowMeta[1] > 0 && !m.localShadowCacheReady(*light, shadowRevision) {
			light.Params[3] = 0
			light.ShadowMeta[1] = 0
			continue
		}
		if lightType != core.LightTypeDirectional || light.ShadowMeta[1] == 0 {
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
	return buildLightsData(gpuLights, m.RenderOrigin)
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
		if light.Params[3] <= 0.5 {
			continue
		}
		switch lightType {
		case core.LightTypeDirectional:
			if hasCamera {
				total += core.DirectionalShadowCascadeCount
			}
		case core.LightTypePoint:
			if !localLightRangeValid(light) {
				continue
			}
			total += core.PointShadowFaceCount
		case core.LightTypeSpot:
			if !localLightRangeValid(light) {
				continue
			}
			total++
		}
	}
	return total
}

func spotShadowFOVFromCosCone(cosCone float32) float64 {
	return math.Acos(float64(cosCone)) * 2.0
}

func localLightRangeValid(light core.Light) bool {
	return light.Params[0] > 0
}

func (m *GpuBufferManager) UpdateScene(scene *core.Scene, camera *core.CameraState, aspect float32, renderOrigin mgl32.Vec3) bool {
	recreated := false
	m.RenderOrigin = renderOrigin

	// Light metadata drives shadow-only caster selection on every frame.
	m.Profiler.BeginScope("Scene: Lights")
	m.UpdateLights(scene, camera, aspect)
	m.Profiler.EndScope("Scene: Lights")

	// Lights
	if m.EnsureShadowMapCapacity(totalShadowLayers(scene.Lights)) {
		m.invalidateShadowCache()
		recreated = true
	}

	// Voxel Data (Incremental / Paged)
	m.Profiler.BeginScope("Scene: Voxel")
	if m.UpdateVoxelData(scene) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Voxel")

	// Sector lookup structures
	m.Profiler.BeginScope("Scene: Grid")
	if m.updateSectorGrid(scene) {
		recreated = true
	}
	combinedLookup, planetLookup := m.prepareObjectLookupBytes(scene)
	if m.updateTerrainChunkLookup(combinedLookup) {
		recreated = true
	}
	if m.updatePlanetTileLookup(planetLookup) {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Grid")

	m.prepareShadowDependencies(scene)
	lightsData := m.buildLightsDataForGPU(scene.Lights, scene.ShadowRevision())
	if m.ensureBuffer("LightsBuf", &m.LightsBuf, lightsData, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	if m.ensureBuffer("ShadowLayerParamsBuf", &m.ShadowLayerParamsBuf, buildShadowLayerParamsData(m.ShadowLayerParams), wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	// Record inputs now include admitted material offsets and current lookup
	// metadata. Earlier maintenance does not consume these nine buffers.
	m.Profiler.BeginScope("Scene: Records")
	m.prepareSceneRecords(scene, renderOrigin)
	if m.publishSceneRecords() {
		recreated = true
	}
	m.Profiler.EndScope("Scene: Records")
	return recreated
}

const CameraUniformSizeBytes = 320

func buildCameraUniformData(viewProj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor, renderOrigin mgl32.Vec3, sunIntensity, skyAmbientMix, farPlane float32, debugMode uint32, renderMode uint32, numLights uint32, screenW, screenH uint32, lightingQuality core.LightingQualityConfig) []byte {
	buf := make([]byte, CameraUniformSizeBytes)
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
	binary.LittleEndian.PutUint32(buf[220:], math.Float32bits(sunIntensity))

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
	binary.LittleEndian.PutUint32(buf[280:], 0) // ao_quality.z: reserved shadow style control
	binary.LittleEndian.PutUint32(buf[284:], 0) // ao_quality.w: reserved shadow style control
	binary.LittleEndian.PutUint32(buf[288:], math.Float32bits(farPlane))
	binary.LittleEndian.PutUint32(buf[292:], math.Float32bits(farPlane))
	binary.LittleEndian.PutUint32(buf[296:], 0)
	binary.LittleEndian.PutUint32(buf[300:], 0)
	binary.LittleEndian.PutUint32(buf[304:], math.Float32bits(renderOrigin[0]))
	binary.LittleEndian.PutUint32(buf[308:], math.Float32bits(renderOrigin[1]))
	binary.LittleEndian.PutUint32(buf[312:], math.Float32bits(renderOrigin[2]))
	binary.LittleEndian.PutUint32(buf[316:], 0)

	return buf
}

func (m *GpuBufferManager) UpdateCamera(viewProj, invView, invProj mgl32.Mat4, camPos, lightPos, ambientColor, renderOrigin mgl32.Vec3, sunIntensity, skyAmbientMix, farPlane float32, debugMode uint32, renderMode uint32, numLights uint32, screenW, screenH uint32, lightingQuality core.LightingQualityConfig) {
	buf := buildCameraUniformData(viewProj, invView, invProj, camPos, lightPos, ambientColor, renderOrigin, sunIntensity, skyAmbientMix, farPlane, debugMode, renderMode, numLights, screenW, screenH, lightingQuality)

	if m.CameraBuf == nil {
		desc := &wgpu.BufferDescriptor{
			Label: "CameraUB",
			Size:  CameraUniformSizeBytes,
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
		emitterLinkID := l.ShadowMeta[3]
		l.ShadowMeta = [4]uint32{0, 0, 0, emitterLinkID}
		l.ViewProj = [16]float32{}
		l.InvViewProj = [16]float32{}
		for c := range l.DirectionalCascades {
			l.DirectionalCascades[c] = core.DirectionalShadowCascade{}
		}

		if lightType == core.LightTypeDirectional {
			if dir.Len() < 1e-4 {
				dir = mgl32.Vec3{0, -1, 0}
			}
			dir = dir.Normalize()
			l.Direction[0], l.Direction[1], l.Direction[2] = dir.X(), dir.Y(), dir.Z()
			if camera != nil && l.Params[3] > 0.5 {
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
			if dir.Len() < 1e-4 {
				dir = mgl32.Vec3{0, -1, 0}
			}
			dir = dir.Normalize()
			up := shadowUpVector(dir)
			l.Direction[0], l.Direction[1], l.Direction[2] = dir.X(), dir.Y(), dir.Z()
			if l.Params[3] <= 0.5 {
				continue
			}
			if !localLightRangeValid(*l) {
				continue
			}
			fov := spotShadowFOVFromCosCone(l.Params[1])
			if fov <= 0 || fov >= maxSpotShadowFOV || math.IsNaN(fov) || math.IsInf(fov, 0) {
				continue
			}
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
			if l.Params[3] <= 0.5 {
				continue
			}
			if !localLightRangeValid(*l) {
				continue
			}
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
	m.prepareShadowDependencies(scene)
}

type sectorGridMapIdentity struct {
	mapRef *volume.XBrickMap
	mapID  uint32
}

// Snapshot selected maps by value so equal-count swaps of already allocated or
// retained maps, and changes to map IDs, cannot reuse stale lookup tables.
// Ordering follows scene objects; no hash or iteration order defines identity.
// Removed entries release their references; an empty selection drops storage.
func (m *GpuBufferManager) sectorGridSelectionChanged(scene *core.Scene) bool {
	changed := false
	count := 0
	if scene != nil {
		for _, target := range voxelServiceTargets(scene) {
			xbm := target.mapRef
			if xbm == nil {
				continue
			}
			identity := sectorGridMapIdentity{mapRef: xbm, mapID: xbm.ID}
			if count >= len(m.lastSectorGridSelection) {
				m.lastSectorGridSelection = append(m.lastSectorGridSelection, identity)
				changed = true
			} else if m.lastSectorGridSelection[count] != identity {
				m.lastSectorGridSelection[count] = identity
				changed = true
			}
			count++
		}
	}
	if count < len(m.lastSectorGridSelection) {
		clear(m.lastSectorGridSelection[count:])
		m.lastSectorGridSelection = m.lastSectorGridSelection[:count]
		changed = true
	}
	if count == 0 {
		m.lastSectorGridSelection = nil
	}
	return changed
}

func (m *GpuBufferManager) updateSectorGrid(scene *core.Scene) bool {
	selectionChanged := m.sectorGridSelectionChanged(scene)
	totalSectors := 0
	for _, target := range voxelServiceTargets(scene) {
		if xbm := target.mapRef; xbm != nil {
			if alloc := m.Allocations[xbm]; alloc != nil && (!alloc.lookupAdmissionKnown || alloc.lookupAdmitted) {
				totalSectors += len(alloc.Sectors)
			}
		}
	}

	// Skip only when object and sector-allocation topology are unchanged.
	if !selectionChanged && totalSectors == m.lastTotalSectors &&
		uint64(scene.StructureRevision) == m.lastSceneRevision &&
		m.sectorTopologyRevision == m.lastSectorGridTopologyRevision &&
		m.SectorGridBuf != nil {
		return false
	}
	m.lastTotalSectors = totalSectors
	m.lastSceneRevision = uint64(scene.StructureRevision)
	m.lastSectorGridTopologyRevision = m.sectorTopologyRevision
	// Always ensure buffers exist even if empty to avoid bind group panics
	if totalSectors == 0 {
		recreated := false
		for _, target := range voxelServiceTargets(scene) {
			if alloc := m.Allocations[target.mapRef]; alloc != nil {
				alloc.DirectLookup = defaultDirectSectorLookupMetadata()
			}
		}
		if m.publishVoxelLookupBuffer("SectorGridBuf", &m.SectorGridBuf, make([]byte, 64), wgpu.BufferUsageStorage) {
			recreated = true
		}
		if m.publishVoxelLookupBuffer("DirectSectorLookupBuf", &m.DirectSectorLookupBuf, make([]byte, 4), wgpu.BufferUsageStorage) {
			recreated = true
		}
		if m.publishVoxelLookupBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, make([]byte, 16), wgpu.BufferUsageUniform) {
			recreated = true
		}
		return recreated
	}

	gridData, gridSize := m.buildSectorGridData(scene)

	directData := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)

	recreated := false
	if m.publishVoxelLookupBuffer("SectorGridBuf", &m.SectorGridBuf, gridData, wgpu.BufferUsageStorage) {
		recreated = true
	}
	if m.publishVoxelLookupBuffer("DirectSectorLookupBuf", &m.DirectSectorLookupBuf, directData, wgpu.BufferUsageStorage) {
		recreated = true
	}

	paramsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(paramsData[0:4], uint32(gridSize))
	binary.LittleEndian.PutUint32(paramsData[4:8], uint32(gridSize-1)) // gridSize is always power-of-two, so shaders can wrap with grid_mask.

	if m.publishVoxelLookupBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, paramsData, wgpu.BufferUsageUniform) {
		recreated = true
	}
	return recreated
}

// buildSectorGridData packs allocated snapshots, never unadmitted CPU maps.
func (m *GpuBufferManager) buildSectorGridData(scene *core.Scene) ([]byte, uint32) {
	totalSectors := 0
	processed := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		if processed[target.mapRef] {
			continue
		}
		processed[target.mapRef] = true
		if alloc := m.Allocations[target.mapRef]; alloc != nil && (!alloc.lookupAdmissionKnown || alloc.lookupAdmitted) {
			totalSectors += len(alloc.Sectors)
		}
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
		return h & uint32(gridSize-1)
	}

	processedMaps := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		xbm := target.mapRef
		if xbm == nil || processedMaps[xbm] {
			continue
		}
		processedMaps[xbm] = true
		baseIdx := xbm.ID

		alloc := m.Allocations[xbm]
		if alloc == nil || (alloc.lookupAdmissionKnown && !alloc.lookupAdmitted) {
			continue
		}
		for sKey, sector := range alloc.Sectors {
			sx, sy, sz := int32(sKey[0]), int32(sKey[1]), int32(sKey[2])
			info, ok := m.SectorToInfo[sector]
			if !ok {
				continue
			}

			h := hash(sx, sy, sz, baseIdx)
			inserted := false
			for i := 0; i < 128; i++ {
				probeIdx := (h + uint32(i)) & uint32(gridSize-1)
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

	return m.gridDataPool, uint32(gridSize)
}

// Capacity is owned by the admission transaction. Publication never allocates.
func (m *GpuBufferManager) publishVoxelLookupBuffer(name string, destination **wgpu.Buffer, data []byte, usage wgpu.BufferUsage) bool {
	if !m.voxelAdmissionActive {
		return m.ensureBuffer(name, destination, data, usage, 0)
	}
	current := *destination
	backend := m.voxelNative
	if backend == nil {
		backend = nativeVoxelBackend{m}
	}
	if current == nil || uint64(len(data)) > backend.BufferSize(current) {
		panic(fmt.Sprintf("voxel lookup %s exceeds admitted capacity", name))
	}
	bufferLimit, storageLimit, uniformLimit := backend.Limits()
	limit := min(bufferLimit, storageLimit)
	if usage&wgpu.BufferUsageUniform != 0 {
		limit = min(bufferLimit, uniformLimit)
	}
	if backend.BufferSize(current) > limit {
		panic(fmt.Sprintf("voxel lookup %s exceeds device limit", name))
	}
	mustQueueVoxelWrite(m.writeVoxelBuffer(current, 0, data))
	return false
}
