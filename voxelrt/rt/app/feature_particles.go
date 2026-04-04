package app

import "github.com/cogentcore/webgpu/wgpu"

// ParticlesFeature owns particle simulation/render pipeline lifecycle and accumulation rendering.
type ParticlesFeature struct{}

func (f *ParticlesFeature) Name() string {
	return "particles"
}

func (f *ParticlesFeature) Enabled(*App) bool {
	return true
}

func (f *ParticlesFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	if err := a.setupParticleSimulationPipelines(); err != nil {
		return err
	}
	a.setupParticlesPipeline()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupParticlesPipeline()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) OnSceneBuffersRecreated(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.CreateParticleSimBindGroups()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) Update(*App) error {
	return nil
}

func (f *ParticlesFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *ParticlesFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.ParticlesPipeline = nil
	a.ParticleInitPipeline = nil
	a.ParticleSimPipeline = nil
	a.ParticleSpawnPipeline = nil
	a.ParticleFinalizePipeline = nil
}

func (f *ParticlesFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePreGBuffer {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.DispatchParticleSim(encoder, a.ParticleInitPipeline, a.ParticleSimPipeline)
	a.BufferManager.DispatchParticleSpawn(encoder, a.ParticleSpawnPipeline, a.ParticleFinalizePipeline, a.ParticleSpawnCount)
	return nil
}

func (f *ParticlesFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.ParticlesPipeline == nil || !a.BufferManager.HasParticleContribution() {
		return nil
	}
	if a.BufferManager.ParticlesBindGroup0 == nil || a.BufferManager.ParticlesBindGroup1 == nil {
		return nil
	}

	pass.SetPipeline(a.ParticlesPipeline)
	pass.SetBindGroup(0, a.BufferManager.ParticlesBindGroup0, nil)
	pass.SetBindGroup(1, a.BufferManager.ParticlesBindGroup1, nil)
	pass.DrawIndirect(a.BufferManager.ParticleIndirectBuf, 0)
	return nil
}

func (f *ParticlesFeature) rebuildRenderBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.ParticlesPipeline == nil {
		return
	}
	m := a.BufferManager
	if m.CameraBuf == nil || m.ParticlePoolBuf == nil || m.ParticleAliveListBuf == nil {
		return
	}
	if m.ParticleAtlasView == nil || m.ParticleAtlasSampler == nil || m.DepthView == nil {
		return
	}
	m.CreateParticlesBindGroups(a.ParticlesPipeline)
}
