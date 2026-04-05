package app

import "github.com/cogentcore/webgpu/wgpu"

// AnalyticMediumFeature owns the bounded analytic medium accumulation pass.
type AnalyticMediumFeature struct{}

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

func (f *AnalyticMediumFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.AnalyticMediumPipeline != nil &&
		a.BufferManager.HasAnalyticMediumContribution()
}

func (f *AnalyticMediumFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.AnalyticMediumPipeline == nil || !a.BufferManager.HasAnalyticMediumContribution() {
		return nil
	}
	if a.BufferManager.AnalyticMediumBG0 == nil || a.BufferManager.AnalyticMediumBG1 == nil || a.BufferManager.AnalyticMediumBG2 == nil {
		return nil
	}

	pass.SetPipeline(a.AnalyticMediumPipeline)
	pass.SetBindGroup(0, a.BufferManager.AnalyticMediumBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.AnalyticMediumBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.AnalyticMediumBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *AnalyticMediumFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.AnalyticMediumPipeline == nil {
		return
	}
	a.BufferManager.CreateAnalyticMediumBindGroups(a.AnalyticMediumPipeline)
}
