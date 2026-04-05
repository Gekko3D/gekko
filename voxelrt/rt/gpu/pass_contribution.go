package gpu

import "github.com/gekko3d/gekko/voxelrt/rt/core"

func (m *GpuBufferManager) HasLocalLights(scene *core.Scene) bool {
	if scene == nil {
		return false
	}
	for _, light := range scene.Lights {
		lightType := uint32(light.Params[2])
		if lightType == core.LightTypeSpot || lightType == core.LightTypePoint {
			return true
		}
	}
	return false
}

func (m *GpuBufferManager) HasVisibleTransparentOverlay(scene *core.Scene) bool {
	if scene == nil {
		return false
	}
	return len(scene.TransparentVisibleObjects) > 0
}

func (m *GpuBufferManager) HasSpriteContribution() bool {
	if m == nil || m.SpriteCount == 0 || m.SpritesBindGroup1 == nil {
		return false
	}
	for _, batch := range m.SpriteBatches {
		if batch.BindGroup0 != nil && batch.InstanceCount > 0 {
			return true
		}
	}
	return false
}

func (m *GpuBufferManager) HasParticleContribution() bool {
	return m != nil && m.ParticleSystemActive && m.ParticlesBindGroup0 != nil && m.ParticlesBindGroup1 != nil
}

func (m *GpuBufferManager) HasCAVolumeContribution() bool {
	return m != nil &&
		m.CAVolumeCount > 0 &&
		m.CAVolumeRenderBG0 != nil &&
		m.CurrentCAVolumeRenderBG1() != nil &&
		m.CAVolumeRenderBG2 != nil
}

func (m *GpuBufferManager) HasAnalyticMediumContribution() bool {
	return m != nil &&
		m.AnalyticMediumCount > 0 &&
		m.AnalyticMediumBG0 != nil &&
		m.AnalyticMediumBG1 != nil &&
		m.AnalyticMediumBG2 != nil
}
