package app

import "github.com/cogentcore/webgpu/wgpu"

// AnalyticMediumFeature owns the bounded analytic medium accumulation pass.
type AnalyticMediumFeature struct{}

func analyticDepthClearValue(a *App) float64 {
	if a != nil && a.Camera != nil {
		far := float64(a.Camera.FarPlane())
		if far > 0 {
			return far
		}
	}
	return 1e20
}

func (f *AnalyticMediumFeature) Name() string {
	return "analytic-media"
}

func (f *AnalyticMediumFeature) Enabled(*App) bool {
	return true
}

func (f *AnalyticMediumFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupAnalyticMediumPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupAnalyticMediumPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.AnalyticMediumBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *AnalyticMediumFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.AnalyticMediumPipeline = nil
}

func (f *AnalyticMediumFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.AnalyticMediumPipeline != nil &&
		a.BufferManager.CurrentVolumetricView() != nil &&
		a.BufferManager.CurrentVolumetricDepthView() != nil
}

func (f *AnalyticMediumFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	if a.AnalyticMediumPipeline == nil {
		return nil
	}
	if a.BufferManager.AnalyticMediumBG0 == nil || a.BufferManager.AnalyticMediumBG1 == nil || a.BufferManager.AnalyticMediumBG2 == nil {
		if a.BufferManager.AnalyticMediumCount == 0 {
			// Still clear the current volumetric targets so stale history is not reused.
			pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
				ColorAttachments: []wgpu.RenderPassColorAttachment{
					{
						View:       a.BufferManager.CurrentVolumetricView(),
						LoadOp:     wgpu.LoadOpClear,
						StoreOp:    wgpu.StoreOpStore,
						ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
					},
					{
						View:       a.BufferManager.CurrentVolumetricDepthView(),
						LoadOp:     wgpu.LoadOpClear,
						StoreOp:    wgpu.StoreOpStore,
						ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
					},
				},
			})
			return pass.End()
		}
		return nil
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       a.BufferManager.CurrentVolumetricView(),
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
			},
			{
				View:       a.BufferManager.CurrentVolumetricDepthView(),
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
			},
		},
	})
	pass.SetPipeline(a.AnalyticMediumPipeline)
	pass.SetBindGroup(0, a.BufferManager.AnalyticMediumBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.AnalyticMediumBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.AnalyticMediumBG2, nil)
	if a.BufferManager.AnalyticMediumCount > 0 {
		pass.Draw(3, 1, 0, 0)
	}
	return pass.End()
}

func (f *AnalyticMediumFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.AnalyticMediumPipeline == nil {
		return
	}
	a.BufferManager.CreateAnalyticMediumBindGroups(a.AnalyticMediumPipeline)
}
