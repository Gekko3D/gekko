package app

import "github.com/cogentcore/webgpu/wgpu"

// WaterFeature owns dedicated stylized water surface accumulation rendering.
type WaterFeature struct{}

func (f *WaterFeature) Name() string {
	return "water"
}

func (f *WaterFeature) Enabled(*App) bool {
	return true
}

func (f *WaterFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupWaterPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupWaterPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.WaterBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *WaterFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.WaterPipeline = nil
}

func (f *WaterFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.WaterPipeline != nil &&
		a.BufferManager.HasWaterContribution()
}

func (f *WaterFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.WaterPipeline == nil || !a.BufferManager.HasWaterContribution() {
		return nil
	}
	if a.BufferManager.WaterBG0 == nil || a.BufferManager.WaterBG1 == nil || a.BufferManager.WaterBG2 == nil {
		return nil
	}

	pass.SetPipeline(a.WaterPipeline)
	pass.SetBindGroup(0, a.BufferManager.WaterBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.WaterBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.WaterBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *WaterFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.WaterPipeline == nil {
		return
	}
	a.BufferManager.StorageView = a.StorageView
	a.BufferManager.CreateWaterBindGroups(a.WaterPipeline)
}
