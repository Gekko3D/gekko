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

func (m *GpuBufferManager) BuildShadowUpdates(scene *core.Scene, camera *core.CameraState, frameIndex uint64, forceDirectionalRefresh bool) []core.ShadowUpdate {
	_ = forceDirectionalRefresh
	updates := make([]core.ShadowUpdate, 0, len(m.ShadowLayerParams))
	if len(m.ShadowLayerParams) == 0 {
		return updates
	}

	for _, layer := range m.ShadowLayerParams {
		if layer.Kind != core.ShadowUpdateKindDirectional {
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
	for _, layer := range m.ShadowLayerParams {
		if layer.Kind != core.ShadowUpdateKindSpot {
			continue
		}
		invalidated, cadenceDue := m.shadowNeedsRefresh(layer, scene.StructureRevision, frameIndex)
		if !invalidated && !cadenceDue {
			continue
		}
		light := scene.Lights[layer.LightIndex]
		candidate := shadowCandidate{
			Update: core.ShadowUpdate{
				LightIndex:   layer.LightIndex,
				ShadowLayer:  layer.Layer,
				CascadeIndex: layer.CascadeIndex,
				Kind:         layer.Kind,
				Tier:         layer.Tier,
				Resolution:   layer.EffectiveResolution,
			},
			Distance:    spotLightDistance(light, camPos),
			Invalidated: invalidated,
		}
		if invalidated {
			invalidatedByTier[layer.Tier] = append(invalidatedByTier[layer.Tier], candidate)
		} else if cadenceDue {
			dueByTier[layer.Tier] = append(dueByTier[layer.Tier], candidate)
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
			updates = append(updates, candidate.Update)
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
			updates = append(updates, dueByTier[tier][idx].Update)
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
