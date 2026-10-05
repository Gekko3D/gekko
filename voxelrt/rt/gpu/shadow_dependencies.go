package gpu

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Shadow rays consume only material PBR.w. Keep the actual uploaded values,
// including unwritten tail rows, independently of CPU palette identity.
type materialShadowOpacity struct {
	values      [materialBlockCapacity]uint32
	writtenRows uint16
	initialized bool
}

func captureMaterialShadowOpacity(allocation *MaterialGpuAllocation, table []core.Material) materialShadowOpacity {
	snapshot := materialShadowOpacity{}
	if allocation != nil {
		snapshot = allocation.shadowOpacity
	}
	snapshot.initialized = true
	if len(table) == 0 {
		clear(snapshot.values[:])
		snapshot.writtenRows = materialBlockCapacity
		return snapshot
	}
	count := min(len(table), materialBlockCapacity)
	snapshot.writtenRows = uint16(count)
	for i := 0; i < count; i++ {
		snapshot.values[i] = math.Float32bits(table[i].Transparency)
	}
	return snapshot
}

// Exact owned scalar inputs, with pointers used only as identities. No source
// light, transform, bounds, or material slices are retained.
type localShadowLightKey struct {
	position, direction, params [4]uint32
	viewProj, invViewProj       [16]uint32
	meta                        [4]uint32
	resolution                  uint32
}

type localShadowCasterKey struct {
	object                       *core.VoxelObject
	selected                     *volume.XBrickMap
	revision                     uint64
	instance                     sceneInstanceKey
	params                       sceneParamsKey
	geometry                     *ObjectGpuAllocation
	material                     *MaterialGpuAllocation
	geometryEpoch, materialEpoch uint64
}

type localShadowDependency struct {
	active                   bool
	light                    localShadowLightKey
	casters                  []localShadowCasterKey
	generation, unknownEpoch uint64
	membershipRevision       uint64
	memberIndices            []int
}

// Public revisions may also be advanced outside the scheduler. Observe before
// tracked service as well as preparation so a subsequent tracked write cannot
// accidentally attribute an earlier unknown change.
func (m *GpuBufferManager) observeShadowUploadRevision() {
	if m.shadowObservedUploadRevision != m.VoxelUploadRevision {
		m.shadowUnknownUploadEpoch++
		m.shadowObservedUploadRevision = m.VoxelUploadRevision
	}
}

func shadowVec4Bits(v [4]float32) (bits [4]uint32) {
	for i := range v {
		bits[i] = math.Float32bits(v[i])
	}
	return
}

func finiteShadowBounds(bounds *[2]mgl32.Vec3) bool {
	if bounds == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if bounds[0][i] > bounds[1][i] {
			return false
		}
		for j := 0; j < 2; j++ {
			value := float64(bounds[j][i])
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return false
			}
		}
	}
	return true
}

func localShadowIntersects(obj *core.VoxelObject, light core.Light) bool {
	bounds := obj.RenderWorldBounds()
	// Unknown bounds cannot prove exclusion.
	if !finiteShadowBounds(bounds) {
		return true
	}
	position := mgl32.Vec3{light.Position[0], light.Position[1], light.Position[2]}
	if uint32(light.Params[2]) == core.LightTypePoint {
		return intersectsPointShadowVolume(*bounds, pointShadowCullVolume{position, light.Params[0]})
	}
	direction := mgl32.Vec3{light.Direction[0], light.Direction[1], light.Direction[2]}
	return intersectsSpotShadowVolume(*bounds, spotShadowCullVolume{position, direction, light.Params[0], light.Params[1]})
}

func (m *GpuBufferManager) localShadowCaster(obj *core.VoxelObject) localShadowCasterKey {
	selected := obj.RenderVoxelMap()
	localMin, localMax := selected.ComputeAABB()
	key := localShadowCasterKey{
		object: obj, selected: selected, revision: selected.Revision,
		instance: sceneInstanceKey{
			objectToWorld: sceneMatBits(obj.RenderObjectToWorld()), worldToObject: sceneMatBits(obj.RenderWorldToObject()),
			localMin: sceneVecBits(localMin), localMax: sceneVecBits(localMax), world: sceneWorldBoundsKey(obj),
		},
		params: m.sceneObjectParamsKey(obj), geometry: m.Allocations[selected], material: m.MaterialAllocations[obj],
	}
	if key.geometry != nil {
		key.geometryEpoch = key.geometry.shadowUploadEpoch
	}
	if key.material != nil {
		key.materialEpoch = key.material.shadowUploadEpoch
	}
	return key
}

func (m *GpuBufferManager) prepareLocalShadowDependencies(scene *core.Scene) {
	m.observeShadowUploadRevision()
	count := 0
	if scene != nil {
		count = len(scene.Lights)
	}
	if count < len(m.localShadowDependencies) {
		clear(m.localShadowDependencies[count:])
	}
	if count > cap(m.localShadowDependencies) {
		entries := make([]localShadowDependency, count)
		copy(entries, m.localShadowDependencies)
		m.localShadowDependencies = entries
	} else {
		m.localShadowDependencies = m.localShadowDependencies[:count]
	}
	if count == 0 {
		m.localShadowDependencies = nil
		clear(m.localShadowCasters)
		m.localShadowCasters = nil
		return
	}
	// Prepare each selected caster's scalar inputs once for all local lights.
	membershipChanged := false
	n := 0
	hasLocal := false
	for _, layer := range m.ShadowLayerParams {
		if layer.Kind == core.ShadowUpdateKindPoint || layer.Kind == core.ShadowUpdateKindSpot {
			hasLocal = true
			break
		}
	}
	if hasLocal {
		for _, obj := range scene.ShadowObjects {
			if obj == nil || obj.Transform == nil || obj.RenderVoxelMap() == nil || !obj.RenderEnabled || !obj.CastsShadows {
				continue
			}
			caster := m.localShadowCaster(obj)
			if n >= len(m.localShadowCasters) {
				membershipChanged = true
				m.localShadowCasters = append(m.localShadowCasters, caster)
			} else {
				previous := m.localShadowCasters[n]
				membershipChanged = membershipChanged || previous.object != caster.object || previous.instance.world != caster.instance.world
				m.localShadowCasters[n] = caster
			}
			n++
		}
	}
	if n < len(m.localShadowCasters) {
		membershipChanged = true
		clear(m.localShadowCasters[n:])
		m.localShadowCasters = m.localShadowCasters[:n]
	}
	if n == 0 {
		m.localShadowCasters = nil
	}
	if membershipChanged {
		m.localShadowMembershipRevision++
	}
	for i, light := range scene.Lights {
		owner := &m.localShadowDependencies[i]
		kind := uint32(light.Params[2])
		base := light.ShadowMeta[0]
		if (kind != core.LightTypeSpot && kind != core.LightTypePoint) || light.ShadowMeta[1] == 0 || int(base) >= len(m.ShadowLayerParams) {
			*owner = localShadowDependency{}
			continue
		}
		layer := m.ShadowLayerParams[base]
		key := localShadowLightKey{
			position: shadowVec4Bits(light.Position), direction: shadowVec4Bits(light.Direction), params: shadowVec4Bits(light.Params),
			viewProj: sceneMatBits(mgl32.Mat4(light.ViewProj)), invViewProj: sceneMatBits(mgl32.Mat4(light.InvViewProj)), meta: light.ShadowMeta,
			resolution: layer.EffectiveResolution,
		}
		changed := !owner.active || owner.light != key || owner.unknownEpoch != m.shadowUnknownUploadEpoch
		// Volume membership depends only on the light and the ordered caster
		// identities/world bounds. All other live scalar inputs still compare below.
		if !owner.active || owner.light != key || owner.membershipRevision != m.localShadowMembershipRevision {
			n := 0
			for index, caster := range m.localShadowCasters {
				if !localShadowIntersects(caster.object, light) {
					continue
				}
				if n >= len(owner.memberIndices) {
					owner.memberIndices = append(owner.memberIndices, index)
				} else {
					owner.memberIndices[n] = index
				}
				n++
			}
			if n < len(owner.memberIndices) {
				clear(owner.memberIndices[n:])
				owner.memberIndices = owner.memberIndices[:n]
			}
			if n == 0 {
				owner.memberIndices = nil
			}
			owner.membershipRevision = m.localShadowMembershipRevision
		}
		for n, index := range owner.memberIndices {
			caster := m.localShadowCasters[index]
			if n >= len(owner.casters) {
				owner.casters = append(owner.casters, caster)
				changed = true
			} else if owner.casters[n] != caster {
				owner.casters[n] = caster
				changed = true
			}
		}
		n := len(owner.memberIndices)
		if n < len(owner.casters) {
			clear(owner.casters[n:])
			owner.casters = owner.casters[:n]
			changed = true
		}
		if n == 0 {
			owner.casters = nil
		}

		if changed {
			m.localShadowGeneration++
			owner.generation = m.localShadowGeneration
		}
		owner.active, owner.light, owner.unknownEpoch = true, key, m.shadowUnknownUploadEpoch
	}
}

func (m *GpuBufferManager) localShadowLayerValid(layer ShadowLayerParams, state shadowCacheState) bool {
	if int(layer.LightIndex) >= len(m.localShadowDependencies) {
		return false
	}
	owner := m.localShadowDependencies[layer.LightIndex]
	return state.Initialized && owner.active && owner.unknownEpoch == m.shadowUnknownUploadEpoch && state.LastLocalGeneration == owner.generation
}
