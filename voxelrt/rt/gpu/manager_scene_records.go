package gpu

import (
	"encoding/binary"
	"math"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

const sceneInstanceSizeBytes = 208

// Retain caller ordering and storage on the ordinary path. Invalid selected
// geometry cannot enter any GPU row, even from stale precomputed pass inputs.
func filterRenderObjects(objects []*core.VoxelObject) []*core.VoxelObject {
	for i, obj := range objects {
		if obj != nil && obj.Transform != nil && obj.RenderVoxelMap() != nil {
			continue
		}
		filtered := make([]*core.VoxelObject, 0, len(objects)-1)
		filtered = append(filtered, objects[:i]...)
		for _, candidate := range objects[i+1:] {
			if candidate != nil && candidate.Transform != nil && candidate.RenderVoxelMap() != nil {
				filtered = append(filtered, candidate)
			}
		}
		return filtered
	}
	return objects
}

// These are borrowed, read-only views until the next preparation or reset.
type sceneRecordBatch struct{ instances, bvh, params []byte }
type sceneRecordBatches struct{ visible, transparent, shadow sceneRecordBatch }

type sceneBoundsKey struct {
	present  bool
	min, max [3]uint32
}

type sceneInstanceKey struct {
	objectToWorld, worldToObject [16]uint32
	localMin, localMax           [3]uint32
	world                        sceneBoundsKey
	origin                       [3]uint32
}

// Snapshot encoded inputs, including allocation presence. Float bits make
// unchanged NaNs stable and distinguish signed zero without serializing rows.
type sceneParamsKey struct {
	admissionRejected                         bool
	geometry, material                        bool
	mapID, sectors, materialOffset            uint32
	lod, ao, shadowGroup, shadowEpsilon       uint32
	terrain                                   bool
	terrainGroup                              uint32
	terrainCoord                              [3]int
	terrainSize                               int
	planet                                    bool
	planetGroup, emitter                      uint32
	planetFace, planetLevel, planetX, planetY int
	direct                                    directSectorLookupMetadata
}

type sceneObjectRecords struct {
	seen, initialized                bool
	instanceKey                      sceneInstanceKey
	paramsKey                        sceneParamsKey
	instance                         [sceneInstanceSizeBytes]byte
	params                           [objectParamsSizeBytes]byte
	instanceRevision, paramsRevision uint64
}

type scenePassMember struct {
	object                           *core.VoxelObject
	instanceRevision, paramsRevision uint64
	world                            sceneBoundsKey
}

type sceneRecordBuffer struct {
	data                []byte
	revision            uint64
	uploadedRevision    uint64
	uploadedDestination *wgpu.Buffer
}

type sceneRecordPass struct {
	members                []scenePassMember
	origin                 [3]uint32
	instances, bvh, params sceneRecordBuffer
}

type sceneRecordOwner struct {
	objects                      map[*core.VoxelObject]*sceneObjectRecords
	visible, transparent, shadow sceneRecordPass
}

// InvalidateSceneRecords discards prepared records and their publication latch.
// Call on the main thread after an external overwrite or an explicit reset.
// Operational counters remain cumulative for the manager's lifetime.
func (m *GpuBufferManager) InvalidateSceneRecords() {
	if m == nil {
		return
	}
	m.sceneRecords = sceneRecordOwner{}
	m.SceneRecordObjectCount = 0
}

func sceneVecBits(v mgl32.Vec3) [3]uint32 {
	return [3]uint32{math.Float32bits(v[0]), math.Float32bits(v[1]), math.Float32bits(v[2])}
}

func sceneMatBits(v mgl32.Mat4) (bits [16]uint32) {
	for i := range v {
		bits[i] = math.Float32bits(v[i])
	}
	return
}

func sceneWorldBoundsKey(obj *core.VoxelObject) sceneBoundsKey {
	if obj.RenderWorldBounds() == nil {
		return sceneBoundsKey{}
	}
	return sceneBoundsKey{present: true, min: sceneVecBits(obj.RenderWorldBounds()[0]), max: sceneVecBits(obj.RenderWorldBounds()[1])}
}

func (m *GpuBufferManager) sceneObjectParamsKey(obj *core.VoxelObject) sceneParamsKey {
	if !materialPublicationReady(obj, m.Allocations[obj.RenderVoxelMap()], m.MaterialAllocations[obj]) {
		return sceneParamsKey{admissionRejected: true}
	}
	if m.voxelAdmissionActive && (m.Allocations[obj.RenderVoxelMap()] == nil || m.MaterialAllocations[obj] == nil || !m.voxelLookupMaps[obj.RenderVoxelMap()]) {
		return sceneParamsKey{admissionRejected: true}
	}
	alloc := m.Allocations[obj.RenderVoxelMap()]
	if alloc == nil {
		// Missing geometry is the canonical zero row, regardless of metadata.
		return sceneParamsKey{}
	}
	key := sceneParamsKey{
		geometry: true,
		mapID:    obj.RenderVoxelMap().ID, sectors: uint32(len(obj.RenderVoxelMap().Sectors)),
		lod: math.Float32bits(obj.LODThreshold), ao: uint32(obj.AmbientOcclusionMode),
		shadowGroup: obj.ShadowGroupID, shadowEpsilon: math.Float32bits(obj.ShadowSeamWorldEpsilon),
		terrain: obj.IsTerrainChunk, terrainGroup: obj.TerrainGroupID,
		terrainCoord: obj.TerrainChunkCoord, terrainSize: obj.TerrainChunkSize,
		planet: obj.IsPlanetTile, planetGroup: obj.PlanetTileGroupID, emitter: obj.EmitterLinkID,
		planetFace: obj.PlanetTileFace, planetLevel: obj.PlanetTileLevel,
		planetX: obj.PlanetTileX, planetY: obj.PlanetTileY,
		direct: alloc.DirectLookup,
	}
	if alloc.Sectors != nil {
		key.sectors = uint32(len(alloc.Sectors))
	}
	if material := m.MaterialAllocations[obj]; material != nil {
		key.material = true
		key.materialOffset = material.MaterialOffset
	}
	return key
}

func (m *GpuBufferManager) prepareSceneObject(obj *core.VoxelObject, origin mgl32.Vec3) {
	owner := &m.sceneRecords
	row := owner.objects[obj]
	if row == nil {
		row = &sceneObjectRecords{}
		owner.objects[obj] = row
	}
	if row.seen {
		return
	}
	row.seen = true
	o2w, w2o := obj.RenderObjectToWorld(), obj.RenderWorldToObject()
	localMin, localMax := obj.RenderVoxelMap().ComputeAABB()
	instanceKey := sceneInstanceKey{
		objectToWorld: sceneMatBits(o2w), worldToObject: sceneMatBits(w2o),
		localMin: sceneVecBits(localMin), localMax: sceneVecBits(localMax),
		world: sceneWorldBoundsKey(obj), origin: sceneVecBits(origin),
	}
	if !row.initialized || row.instanceKey != instanceKey {
		// Encode an index-neutral row once; each pass patches its own copy.
		data := row.instance[:0]
		data = appendMat4LE(data, renderRelativeObjectToWorld(o2w, origin))
		data = appendMat4LE(data, renderRelativeWorldToObject(w2o, origin))
		minB, maxB := mgl32.Vec3{}, mgl32.Vec3{}
		if obj.RenderWorldBounds() != nil {
			minB, maxB = obj.RenderWorldBounds()[0].Sub(origin), obj.RenderWorldBounds()[1].Sub(origin)
		}
		data = appendVec3PaddedLE(data, minB)
		data = appendVec3PaddedLE(data, maxB)
		data = appendVec3PaddedLE(data, localMin)
		data = appendVec3PaddedLE(data, localMax)
		clear(row.instance[len(data):])
		row.instanceKey = instanceKey
		row.instanceRevision++
		m.SceneInstanceRecordBuildCount++
	}
	paramsKey := m.sceneObjectParamsKey(obj)
	if !row.initialized || row.paramsKey != paramsKey {
		// The existing writer leaves false flags and missing offsets untouched.
		clear(row.params[:])
		writeObjectParamsData(row.params[:], obj, m.Allocations[obj.RenderVoxelMap()], m.MaterialAllocations[obj])
		if paramsKey.admissionRejected {
			// Zero extent makes every direct lookup reject before a table read,
			// including map ID zero and denied materials sharing required maps.
			writeRejectedObjectParams(row.params[:])
		}
		row.paramsKey = paramsKey
		row.paramsRevision++
		m.SceneObjectParamRecordBuildCount++
	}
	row.initialized = true
}

func resizeSceneRecordBytes(data []byte, size int) []byte {
	if cap(data) < size {
		return make([]byte, size)
	}
	if size < len(data) {
		clear(data[size:])
	}
	return data[:size]
}

func (m *GpuBufferManager) prepareSceneRecordPass(pass *sceneRecordPass, objects []*core.VoxelObject, origin mgl32.Vec3) sceneRecordBatch {
	instChanged := pass.instances.data == nil || len(pass.members) != len(objects)
	paramsChanged := pass.params.data == nil || len(pass.members) != len(objects)
	bvhChanged := pass.bvh.data == nil || len(pass.members) != len(objects)
	originBits := sceneVecBits(origin)
	if len(objects) > 0 && pass.origin != originBits {
		bvhChanged = true
	}
	for i, obj := range objects {
		row := m.sceneRecords.objects[obj]
		if i >= len(pass.members) || pass.members[i].object != obj {
			instChanged, paramsChanged, bvhChanged = true, true, true
			continue
		}
		previous := pass.members[i]
		instChanged = instChanged || previous.instanceRevision != row.instanceRevision
		paramsChanged = paramsChanged || previous.paramsRevision != row.paramsRevision
		bvhChanged = bvhChanged || previous.world != row.instanceKey.world
	}
	if len(objects) == 0 {
		// Release peak arrays and references when a pass empties. Keep only the
		// ABI sentinels and cumulative revision/publication state.
		if instChanged {
			pass.instances.data = make([]byte, sceneInstanceSizeBytes)
			pass.instances.revision++
		}
		if paramsChanged {
			pass.params.data = make([]byte, objectParamsSizeBytes)
			pass.params.revision++
		}
		if bvhChanged {
			pass.bvh.data = make([]byte, 64)
			pass.bvh.revision++
		}
		clear(pass.members)
		pass.members = nil
		pass.origin = [3]uint32{}
	} else {
		if instChanged {
			pass.instances.data = resizeSceneRecordBytes(pass.instances.data, len(objects)*sceneInstanceSizeBytes)
			for i, obj := range objects {
				dst := pass.instances.data[i*sceneInstanceSizeBytes : (i+1)*sceneInstanceSizeBytes]
				copy(dst, m.sceneRecords.objects[obj].instance[:])
				binary.LittleEndian.PutUint32(dst[192:], uint32(i))
			}
			pass.instances.revision++
		}
		if paramsChanged {
			pass.params.data = resizeSceneRecordBytes(pass.params.data, len(objects)*objectParamsSizeBytes)
			for i, obj := range objects {
				copy(pass.params.data[i*objectParamsSizeBytes:], m.sceneRecords.objects[obj].params[:])
			}
			pass.params.revision++
		}
		if bvhChanged {
			clear(pass.bvh.data)
			pass.bvh.data = buildRenderBVHData(objects, origin)
			pass.bvh.revision++
			m.SceneBVHBuildCount++
		}
		if len(objects) < len(pass.members) {
			clear(pass.members[len(objects):])
		}
		if cap(pass.members) < len(objects) {
			pass.members = make([]scenePassMember, len(objects))
		} else {
			pass.members = pass.members[:len(objects)]
		}
		for i, obj := range objects {
			row := m.sceneRecords.objects[obj]
			pass.members[i] = scenePassMember{obj, row.instanceRevision, row.paramsRevision, row.instanceKey.world}
		}
		pass.origin = originBits
	}
	return sceneRecordBatch{pass.instances.data, pass.bvh.data, pass.params.data}
}

func (m *GpuBufferManager) prepareSceneRecords(scene *core.Scene, origin mgl32.Vec3) sceneRecordBatches {
	owner := &m.sceneRecords
	if owner.objects == nil {
		owner.objects = make(map[*core.VoxelObject]*sceneObjectRecords)
	}
	for _, row := range owner.objects {
		row.seen = false
	}
	var visible, transparent, shadow []*core.VoxelObject
	if scene != nil {
		visible, transparent, shadow = filterRenderObjects(scene.VisibleObjects), filterRenderObjects(scene.TransparentVisibleObjects), filterRenderObjects(scene.ShadowObjects)
	}
	for _, objects := range [][]*core.VoxelObject{visible, transparent, shadow} {
		for _, obj := range objects {
			m.prepareSceneObject(obj, origin)
		}
	}
	batches := sceneRecordBatches{
		visible:     m.prepareSceneRecordPass(&owner.visible, visible, origin),
		transparent: m.prepareSceneRecordPass(&owner.transparent, transparent, origin),
		shadow:      m.prepareSceneRecordPass(&owner.shadow, shadow, origin),
	}
	for obj, row := range owner.objects {
		if !row.seen {
			delete(owner.objects, obj)
		}
	}
	m.SceneRecordObjectCount = len(owner.objects)
	if len(owner.objects) == 0 {
		owner.objects = nil
	}
	return batches
}

func (m *GpuBufferManager) publishSceneRecordBuffer(name string, destination **wgpu.Buffer, record *sceneRecordBuffer) bool {
	if m.Device == nil {
		// CPU inspection never acknowledges a GPU publication.
		return false
	}
	recreated := record.uploadedDestination != *destination
	// A non-nil empty slice requests capacity without copying obsolete data or
	// invoking ensureBuffer's unchecked write path. Publication is checked below.
	if m.ensureBuffer(name, destination, record.data[:0], wgpu.BufferUsageStorage, len(record.data)) {
		recreated = true
	}
	if record.uploadedDestination == *destination && record.uploadedRevision == record.revision {
		return recreated
	}
	// Match voxel writes' fail-fast policy. Do not advance ownership/counters
	// until the native queue has accepted the write.
	if err := m.Device.GetQueue().WriteBuffer(*destination, 0, record.data); err != nil {
		panic(err)
	}
	record.uploadedDestination = *destination
	record.uploadedRevision = record.revision
	m.SceneRecordUploadCount++
	return recreated
}

func (m *GpuBufferManager) publishSceneRecords() bool {
	owner := &m.sceneRecords
	recreated := false
	for _, item := range []struct {
		name        string
		destination **wgpu.Buffer
		record      *sceneRecordBuffer
	}{
		{"InstancesBuf", &m.InstancesBuf, &owner.visible.instances},
		{"BVHNodesBuf", &m.BVHNodesBuf, &owner.visible.bvh},
		{"ObjectParamsBuf", &m.ObjectParamsBuf, &owner.visible.params},
		{"TransparentInstancesBuf", &m.TransparentInstancesBuf, &owner.transparent.instances},
		{"TransparentBVHNodesBuf", &m.TransparentBVHNodesBuf, &owner.transparent.bvh},
		{"TransparentObjectParamsBuf", &m.TransparentObjectParamsBuf, &owner.transparent.params},
		{"ShadowInstancesBuf", &m.ShadowInstancesBuf, &owner.shadow.instances},
		{"ShadowBVHNodesBuf", &m.ShadowBVHNodesBuf, &owner.shadow.bvh},
		{"ShadowObjectParamsBuf", &m.ShadowObjectParamsBuf, &owner.shadow.params},
	} {
		if m.publishSceneRecordBuffer(item.name, item.destination, item.record) {
			recreated = true
		}
	}
	return recreated
}
