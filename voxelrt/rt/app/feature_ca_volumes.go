package app

import "github.com/cogentcore/webgpu/wgpu"

// CAVolumeFeature owns CA volume simulation, bounds, and accumulation rendering.
type CAVolumeFeature struct{}

func (f *CAVolumeFeature) Name() string {
	return "ca-volumes"
}

func (f *CAVolumeFeature) Enabled(*App) bool {
	return true
}

func (f *CAVolumeFeature) Setup(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	if err := a.createCAVolumeSimPipeline(); err != nil {
		return err
	}
	if err := a.createCAVolumeBoundsPipeline(); err != nil {
		return err
	}
	a.BufferManager.UpdateCAVolumes(nil)
	a.BufferManager.UpdateCAParams(0)
	a.setupCAVolumePipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupCAVolumePipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.CAVolumeBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *CAVolumeFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.CAVolumePipeline = nil
	a.CAVolumeSimPipeline = nil
	a.CAVolumeBoundsPipeline = nil
}

func (f *CAVolumeFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePreGBufferVolumes {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.DispatchCAVolumeSim(encoder, a.CAVolumeSimPipeline)
	a.BufferManager.DispatchCAVolumeBounds(encoder, a.CAVolumeBoundsPipeline)
	return nil
}

func (f *CAVolumeFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.CAVolumePipeline == nil || !a.BufferManager.HasCAVolumeContribution() {
		return nil
	}
	if a.BufferManager.CAVolumeRenderBG0 == nil || a.BufferManager.CAVolumeRenderBG2 == nil || a.BufferManager.CurrentCAVolumeRenderBG1() == nil {
		return nil
	}

	pass.SetPipeline(a.CAVolumePipeline)
	pass.SetBindGroup(0, a.BufferManager.CAVolumeRenderBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.CurrentCAVolumeRenderBG1(), nil)
	pass.SetBindGroup(2, a.BufferManager.CAVolumeRenderBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *CAVolumeFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil {
		return
	}
	if a.CAVolumeSimPipeline != nil {
		a.BufferManager.CreateCAVolumeSimBindGroups()
	}
	if a.CAVolumeBoundsPipeline != nil {
		a.BufferManager.CreateCAVolumeBoundsBindGroups()
	}
	if a.CAVolumePipeline != nil {
		a.BufferManager.CreateCAVolumeRenderBindGroups(a.CAVolumePipeline)
	}
}
