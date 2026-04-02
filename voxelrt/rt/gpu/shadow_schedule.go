package gpu

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

type shadowCandidate struct {
	Update      core.ShadowUpdate
	Distance    float32
	Invalidated bool
}

type pointShadowFaceCandidate struct {
	Update    core.ShadowUpdate
	State     shadowCacheState
	Available bool
}

func shadowTierBudget(tier uint32) int {
	switch tier {
	case core.ShadowTierHero:
		return 4
	case core.ShadowTierNear:
		return 3
	case core.ShadowTierMedium:
		return 2
	case core.ShadowTierFar:
		return 1
	default:
		return 1
	}
}

func (m *GpuBufferManager) shadowNeedsRefresh(layer ShadowLayerParams, sceneRevision, frameIndex uint64) (invalidated bool, cadenceDue bool) {
	if int(layer.Layer) >= len(m.shadowCacheStates) {
		return true, true
	}
	state := m.shadowCacheStates[layer.Layer]
	if !state.Initialized {
		return true, true
	}
	if state.LastLightSignature != layer.LightSignature ||
		state.LastSceneRevision != sceneRevision ||
		state.LastVoxelUploadRevision != m.VoxelUploadRevision {
		return true, true
	}
	if layer.CadenceFrames == 0 {
		return false, false
	}
	return false, frameIndex-state.LastUpdatedFrame >= uint64(layer.CadenceFrames)
}

func spotLightDistance(light core.Light, camPos mgl32.Vec3) float32 {
	lightPos := mgl32.Vec3{light.Position[0], light.Position[1], light.Position[2]}
	return lightPos.Sub(camPos).Len()
}

func localLightShadowUpdates(light core.Light, params []ShadowLayerParams, cacheStates []shadowCacheState) []core.ShadowUpdate {
	updates := make([]core.ShadowUpdate, 0, light.ShadowMeta[1])
	baseLayer := light.ShadowMeta[0]
	lightType := uint32(light.Params[2])
	if lightType == core.LightTypePoint {
		faceCandidates := make([]pointShadowFaceCandidate, 0, light.ShadowMeta[1])
		for layerOffset := uint32(0); layerOffset < light.ShadowMeta[1]; layerOffset++ {
			layer := baseLayer + layerOffset
			if int(layer) >= len(params) {
				continue
			}
			layerParams := params[layer]
			faceCandidate := pointShadowFaceCandidate{
				Update: core.ShadowUpdate{
					LightIndex:   layerParams.LightIndex,
					ShadowLayer:  layerParams.Layer,
					CascadeIndex: layerParams.CascadeIndex,
					Kind:         layerParams.Kind,
					Tier:         layerParams.Tier,
					Resolution:   layerParams.EffectiveResolution,
				},
			}
			if int(layer) < len(cacheStates) {
				faceCandidate.State = cacheStates[layer]
				faceCandidate.Available = true
			}
			faceCandidates = append(faceCandidates, faceCandidate)
		}
		sort.Slice(faceCandidates, func(i, j int) bool {
			left := faceCandidates[i]
			right := faceCandidates[j]
			if left.Available != right.Available {
				return left.Available
			}
			if left.State.Initialized != right.State.Initialized {
				return !left.State.Initialized
			}
			if left.State.LastUpdatedFrame != right.State.LastUpdatedFrame {
				return left.State.LastUpdatedFrame < right.State.LastUpdatedFrame
			}
			return left.Update.CascadeIndex < right.Update.CascadeIndex
		})
		faceBudget := pointShadowFacesPerFrame(params[baseLayer].Tier)
		if faceBudget > len(faceCandidates) {
			faceBudget = len(faceCandidates)
		}
		for i := 0; i < faceBudget; i++ {
			updates = append(updates, faceCandidates[i].Update)
		}
		sort.Slice(updates, func(i, j int) bool {
			return updates[i].CascadeIndex < updates[j].CascadeIndex
		})
		return updates
	}
	for layerOffset := uint32(0); layerOffset < light.ShadowMeta[1]; layerOffset++ {
		layer := baseLayer + layerOffset
		if int(layer) >= len(params) {
			continue
		}
		layerParams := params[layer]
		updates = append(updates, core.ShadowUpdate{
			LightIndex:   layerParams.LightIndex,
			ShadowLayer:  layerParams.Layer,
			CascadeIndex: layerParams.CascadeIndex,
			Kind:         layerParams.Kind,
			Tier:         layerParams.Tier,
			Resolution:   layerParams.EffectiveResolution,
		})
	}
	return updates
}

func (m *GpuBufferManager) BuildShadowUpdates(scene *core.Scene, camera *core.CameraState, frameIndex uint64, forceDirectionalRefresh bool) []core.ShadowUpdate {
	updates := make([]core.ShadowUpdate, 0, len(m.ShadowLayerParams))
	if len(m.ShadowLayerParams) == 0 {
		return updates
	}

	for _, layer := range m.ShadowLayerParams {
		if layer.Kind != core.ShadowUpdateKindDirectional {
			continue
		}
		invalidated, cadenceDue := m.shadowNeedsRefresh(layer, scene.StructureRevision, frameIndex)
		if !forceDirectionalRefresh && !invalidated && !cadenceDue {
			continue
		}
		updates = append(updates, core.ShadowUpdate{
			LightIndex:   layer.LightIndex,
			ShadowLayer:  layer.Layer,
			CascadeIndex: layer.CascadeIndex,
			Kind:         layer.Kind,
			Tier:         layer.Tier,
			Resolution:   layer.EffectiveResolution,
		})
	}

	camPos := mgl32.Vec3{}
	if camera != nil {
		camPos = camera.Position
	}
	invalidatedByTier := [shadowTierCount][]shadowCandidate{}
	dueByTier := [shadowTierCount][]shadowCandidate{}
	for lightIndex, light := range scene.Lights {
		lightType := uint32(light.Params[2])
		if lightType != core.LightTypeSpot && lightType != core.LightTypePoint {
			continue
		}
		layerCount := light.ShadowMeta[1]
		if layerCount == 0 {
			continue
		}
		baseLayer := light.ShadowMeta[0]
		if int(baseLayer) >= len(m.ShadowLayerParams) {
			continue
		}
		tier := m.ShadowLayerParams[baseLayer].Tier
		invalidated := false
		cadenceDue := false
		for layerOffset := uint32(0); layerOffset < layerCount; layerOffset++ {
			layer := baseLayer + layerOffset
			if int(layer) >= len(m.ShadowLayerParams) {
				continue
			}
			layerInvalidated, layerCadenceDue := m.shadowNeedsRefresh(m.ShadowLayerParams[layer], scene.StructureRevision, frameIndex)
			invalidated = invalidated || layerInvalidated
			cadenceDue = cadenceDue || layerCadenceDue
		}
		if !invalidated && !cadenceDue {
			continue
		}
		candidate := shadowCandidate{
			Update: core.ShadowUpdate{
				LightIndex: uint32(lightIndex),
				Tier:       tier,
			},
			Distance:    spotLightDistance(light, camPos),
			Invalidated: invalidated,
		}
		if invalidated {
			invalidatedByTier[tier] = append(invalidatedByTier[tier], candidate)
		} else if cadenceDue {
			dueByTier[tier] = append(dueByTier[tier], candidate)
		}
	}

	for tier := uint32(0); tier < shadowTierCount; tier++ {
		sort.Slice(invalidatedByTier[tier], func(i, j int) bool {
			return invalidatedByTier[tier][i].Distance < invalidatedByTier[tier][j].Distance
		})
		sort.Slice(dueByTier[tier], func(i, j int) bool {
			return dueByTier[tier][i].Distance < dueByTier[tier][j].Distance
		})
		budget := shadowTierBudget(tier)
		for _, candidate := range invalidatedByTier[tier] {
			if budget == 0 {
				break
			}
			light := scene.Lights[candidate.Update.LightIndex]
			updates = append(updates, localLightShadowUpdates(light, m.ShadowLayerParams, m.shadowCacheStates)...)
			budget--
		}
		if budget == 0 || len(dueByTier[tier]) == 0 {
			continue
		}
		offset := 0
		if len(dueByTier[tier]) > 0 {
			offset = m.shadowTierOffsets[tier] % len(dueByTier[tier])
		}
		selected := 0
		for selected < budget && selected < len(dueByTier[tier]) {
			idx := (offset + selected) % len(dueByTier[tier])
			light := scene.Lights[dueByTier[tier][idx].Update.LightIndex]
			updates = append(updates, localLightShadowUpdates(light, m.ShadowLayerParams, m.shadowCacheStates)...)
			selected++
		}
		m.shadowTierOffsets[tier] = offset + selected
	}

	return updates
}

func (m *GpuBufferManager) RecordShadowUpdates(updates []core.ShadowUpdate, frameIndex, sceneRevision uint64) {
	for _, update := range updates {
		layer := update.ShadowLayer
		if int(layer) >= len(m.shadowCacheStates) || int(layer) >= len(m.ShadowLayerParams) {
			continue
		}
		state := &m.shadowCacheStates[layer]
		state.Initialized = true
		state.LastUpdatedFrame = frameIndex
		state.LastLightSignature = m.ShadowLayerParams[layer].LightSignature
		state.LastSceneRevision = sceneRevision
		state.LastVoxelUploadRevision = m.VoxelUploadRevision
	}
}

func (m *GpuBufferManager) PrepareShadowLights(scene *core.Scene, updates []core.ShadowUpdate) {
	if m.LightsBuf == nil || len(scene.Lights) == 0 {
		return
	}
	updatedDirectional := false
	for _, update := range updates {
		if update.Kind != core.ShadowUpdateKindDirectional {
			continue
		}
		if int(update.LightIndex) >= len(scene.Lights) || int(update.ShadowLayer) >= len(m.shadowCachedCascades) {
			continue
		}
		light := scene.Lights[update.LightIndex]
		if int(update.CascadeIndex) >= len(light.DirectionalCascades) {
			continue
		}
		m.shadowCachedCascades[update.ShadowLayer] = light.DirectionalCascades[update.CascadeIndex]
		updatedDirectional = true
	}
	if !updatedDirectional {
		return
	}
	lightsData := m.buildLightsDataForGPU(scene.Lights)
	m.Device.GetQueue().WriteBuffer(m.LightsBuf, 0, lightsData)
}
