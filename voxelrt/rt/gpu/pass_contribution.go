package gpu

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

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

func (m *GpuBufferManager) ResetTiledLightCullState(encoder *wgpu.CommandEncoder) {
	if m == nil {
		return
	}
	m.TileLightAvgCount = 0
	m.TileLightMaxCount = 0
	if encoder == nil {
		return
	}
	if m.TileLightHeadersBuf != nil && m.TileLightHeadersBuf.GetSize() > 0 {
		_ = encoder.ClearBuffer(m.TileLightHeadersBuf, 0, m.TileLightHeadersBuf.GetSize())
	}
	if m.TileLightIndicesBuf != nil && m.TileLightIndicesBuf.GetSize() > 0 {
		_ = encoder.ClearBuffer(m.TileLightIndicesBuf, 0, m.TileLightIndicesBuf.GetSize())
	}
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
		m.CAVolumeVisibleCount > 0 &&
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

func (m *GpuBufferManager) HasWaterContribution() bool {
	return m != nil &&
		m.WaterCount > 0 &&
		m.WaterBG0 != nil &&
		m.WaterBG1 != nil &&
		m.WaterBG2 != nil
}
