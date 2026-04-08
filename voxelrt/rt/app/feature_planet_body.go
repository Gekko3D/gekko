package app

import "github.com/cogentcore/webgpu/wgpu"

// PlanetBodyFeature owns analytic far-body planet rendering.
type PlanetBodyFeature struct{}

func (f *PlanetBodyFeature) Name() string {
	return "planet-bodies"
}

func (f *PlanetBodyFeature) Enabled(*App) bool {
	return true
}

func (f *PlanetBodyFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupPlanetBodyPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupPlanetBodyPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.PlanetBodyBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *PlanetBodyFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.PlanetBodyPipeline = nil
}

func (f *PlanetBodyFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.PlanetBodyPipeline != nil &&
		a.BufferManager.PlanetBodyCount > 0 &&
		a.StorageView != nil
}

func (f *PlanetBodyFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil || a.PlanetBodyPipeline == nil {
		return nil
	}
	if a.BufferManager.PlanetBodyBG0 == nil || a.BufferManager.PlanetBodyBG1 == nil || a.BufferManager.PlanetBodyBG2 == nil {
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
	pass.SetPipeline(a.PlanetBodyPipeline)
	pass.SetBindGroup(0, a.BufferManager.PlanetBodyBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.PlanetBodyBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.PlanetBodyBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return pass.End()
}

func (f *PlanetBodyFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.PlanetBodyPipeline == nil {
		return
	}
	a.BufferManager.CreatePlanetBodyBindGroups(a.PlanetBodyPipeline)
}
