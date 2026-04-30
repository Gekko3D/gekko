package app

import "github.com/cogentcore/webgpu/wgpu"

// AstronomicalFeature owns far-field angular celestial rendering.
type AstronomicalFeature struct{}

func (f *AstronomicalFeature) Name() string {
	return "astronomical"
}

func (f *AstronomicalFeature) Enabled(*App) bool {
	return true
}

func (f *AstronomicalFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupAstronomicalPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupAstronomicalPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.AstronomicalBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *AstronomicalFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.AstronomicalPipeline = nil
}

func (f *AstronomicalFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.AstronomicalPipeline != nil &&
		a.StorageView != nil &&
		a.BufferManager.DepthView != nil
}

func (f *AstronomicalFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil || a.AstronomicalPipeline == nil {
		return nil
	}
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:    a.StorageView,
				LoadOp:  wgpu.LoadOpLoad,
				StoreOp: wgpu.StoreOpStore,
			},
		},
	})
	if a.BufferManager.AstronomicalBodyCount > 0 && a.BufferManager.AstronomicalBG0 != nil && a.BufferManager.AstronomicalBG1 != nil && a.BufferManager.AstronomicalBG2 != nil {
		pass.SetPipeline(a.AstronomicalPipeline)
		pass.SetBindGroup(0, a.BufferManager.AstronomicalBG0, nil)
		pass.SetBindGroup(1, a.BufferManager.AstronomicalBG1, nil)
		pass.SetBindGroup(2, a.BufferManager.AstronomicalBG2, nil)
		pass.Draw(3, 1, 0, 0)
	}
	return pass.End()
}

func (f *AstronomicalFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.AstronomicalPipeline == nil {
		return
	}
	a.BufferManager.CreateAstronomicalBindGroups(a.AstronomicalPipeline)
}
