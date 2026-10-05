package gpu

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
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
	cascadeParams               [4]uint32
	assignment                  [3]uint32
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

func (m *GpuBufferManager) prepareShadowDependencies(scene *core.Scene) {
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
		m.directionalShadowDependencies = nil
		clear(m.localShadowCasters)
		m.localShadowCasters = nil
		return
	}
	// Prepare each selected caster's scalar inputs once for all shadow layers.
	membershipChanged := false
	n := 0
	if len(m.ShadowLayerParams) > 0 {
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
		m.refreshShadowDependency(owner, key, func(caster localShadowCasterKey) bool { return localShadowIntersects(caster.object, light) })
	}
	m.prepareDirectionalShadowDependencies(scene)
}

func (m *GpuBufferManager) refreshShadowDependency(owner *localShadowDependency, key localShadowLightKey, intersects func(localShadowCasterKey) bool) {
	changed := !owner.active || owner.light != key || owner.unknownEpoch != m.shadowUnknownUploadEpoch
	// Volume membership depends only on the light and the ordered caster
	// identities/world bounds. All other live scalar inputs still compare below.
	if !owner.active || owner.light != key || owner.membershipRevision != m.localShadowMembershipRevision {
		n := 0
		for index, caster := range m.localShadowCasters {
			if !intersects(caster) {
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

func (m *GpuBufferManager) localShadowLayerValid(layer ShadowLayerParams, state shadowCacheState) bool {
	if int(layer.LightIndex) >= len(m.localShadowDependencies) {
		return false
	}
	owner := m.localShadowDependencies[layer.LightIndex]
	return state.Initialized && owner.active && owner.unknownEpoch == m.shadowUnknownUploadEpoch && state.LastLocalGeneration == owner.generation
}

// An affine inverse projection describes parallel rays with fixed clip XY.
// Shader rays start at clip z=-1 and continue indefinitely toward increasing z;
// the projection's far plane limits stored depth, not traversal.
type directionalShadowRayPrism struct {
	worldToRay mgl64.Mat4
	supported  bool
}

func buildDirectionalShadowRayPrism(inverse [16]float32) directionalShadowRayPrism {
	var matrix mgl64.Mat4
	for i, value := range inverse {
		matrix[i] = float64(value)
		if math.IsNaN(matrix[i]) || math.IsInf(matrix[i], 0) {
			return directionalShadowRayPrism{}
		}
	}
	if matrix[3] != 0 || matrix[7] != 0 || matrix[11] != 0 || matrix[15] == 0 {
		return directionalShadowRayPrism{}
	}
	// Homogeneous w is constant. Normalize before inversion and reject poorly
	// conditioned axes rather than treating numerical uncertainty as exclusion.
	w := matrix[15]
	for i := range matrix {
		matrix[i] /= w
	}
	x, y, z := matrix.Col(0).Vec3(), matrix.Col(1).Vec3(), matrix.Col(2).Vec3()
	scale := x.Len() * y.Len() * z.Len()
	determinant := matrix.Det()
	if scale == 0 || math.IsNaN(scale) || math.IsInf(scale, 0) || math.Abs(determinant) <= scale*1e-6 {
		return directionalShadowRayPrism{}
	}
	worldToRay := matrix.Inv()
	for _, value := range worldToRay {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return directionalShadowRayPrism{}
		}
	}
	return directionalShadowRayPrism{worldToRay, true}
}

func (prism directionalShadowRayPrism) intersects(caster localShadowCasterKey) bool {
	if !prism.supported || !caster.instance.world.present {
		return true
	}
	var bounds [2]mgl32.Vec3
	for axis := 0; axis < 3; axis++ {
		bounds[0][axis] = math.Float32frombits(caster.instance.world.min[axis])
		bounds[1][axis] = math.Float32frombits(caster.instance.world.max[axis])
	}
	if !finiteShadowBounds(&bounds) {
		return true
	}
	minimum := mgl64.Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	maximum := mgl64.Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	guard := mgl64.Vec3{1e-4, 1e-4, 1e-4}
	for corner := 0; corner < 8; corner++ {
		p := mgl64.Vec4{0, 0, 0, 1}
		for axis := 0; axis < 3; axis++ {
			p[axis] = float64(bounds[(corner>>axis)&1][axis])
		}
		ray := prism.worldToRay.Mul4x1(p)
		for axis := 0; axis < 3; axis++ {
			if math.IsNaN(ray[axis]) || math.IsInf(ray[axis], 0) {
				return true
			}
			minimum[axis] = math.Min(minimum[axis], ray[axis])
			maximum[axis] = math.Max(maximum[axis], ray[axis])
			// Cover float32 shader arithmetic, including cancellation from large world
			// translations; contact with a boundary is always included.
			magnitude := 0.0
			for column := 0; column < 4; column++ {
				magnitude += math.Abs(prism.worldToRay[column*4+axis] * p[column])
			}
			guard[axis] = math.Max(guard[axis], magnitude*1e-4)
		}
	}
	return maximum[0] >= -1-guard[0] && minimum[0] <= 1+guard[0] && maximum[1] >= -1-guard[1] && minimum[1] <= 1+guard[1] && maximum[2] >= -1-guard[2]
}

func (m *GpuBufferManager) prepareDirectionalShadowDependencies(scene *core.Scene) {
	count := len(m.ShadowLayerParams)
	if count < len(m.directionalShadowDependencies) {
		clear(m.directionalShadowDependencies[count:])
	}
	if count > cap(m.directionalShadowDependencies) {
		entries := make([]localShadowDependency, count)
		copy(entries, m.directionalShadowDependencies)
		m.directionalShadowDependencies = entries
	} else {
		m.directionalShadowDependencies = m.directionalShadowDependencies[:count]
	}
	if count == 0 {
		m.directionalShadowDependencies = nil
		return
	}
	for i, layer := range m.ShadowLayerParams {
		owner := &m.directionalShadowDependencies[i]
		if layer.Kind != core.ShadowUpdateKindDirectional || int(layer.LightIndex) >= len(scene.Lights) || layer.CascadeIndex >= core.DirectionalShadowCascadeCount {
			*owner = localShadowDependency{}
			continue
		}
		light := scene.Lights[layer.LightIndex]
		cascade := light.DirectionalCascades[layer.CascadeIndex]
		key := localShadowLightKey{
			position: shadowVec4Bits(light.Position), direction: shadowVec4Bits(light.Direction), params: shadowVec4Bits(light.Params), meta: light.ShadowMeta,
			viewProj: sceneMatBits(mgl32.Mat4(cascade.ViewProj)), invViewProj: sceneMatBits(mgl32.Mat4(cascade.InvViewProj)), cascadeParams: shadowVec4Bits(cascade.Params),
			resolution: layer.EffectiveResolution, assignment: [3]uint32{layer.LightIndex, layer.CascadeIndex, layer.Layer},
		}
		var prism directionalShadowRayPrism
		if !owner.active || owner.light != key || owner.membershipRevision != m.localShadowMembershipRevision {
			prism = buildDirectionalShadowRayPrism(cascade.InvViewProj)
		}
		m.refreshShadowDependency(owner, key, prism.intersects)
	}
}

func (m *GpuBufferManager) directionalShadowLayerValid(layer ShadowLayerParams, state shadowCacheState) bool {
	if int(layer.Layer) >= len(m.directionalShadowDependencies) {
		return false
	}
	owner := m.directionalShadowDependencies[layer.Layer]
	return state.Initialized && owner.active && owner.unknownEpoch == m.shadowUnknownUploadEpoch && state.LastLocalGeneration == owner.generation
}
