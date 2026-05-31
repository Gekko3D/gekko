package app

import "github.com/cogentcore/webgpu/wgpu"

type FarPlanetRingFeature struct{}

func (f *FarPlanetRingFeature) Name() string {
	return "far_planet_ring"
}

func (f *FarPlanetRingFeature) Enabled(*App) bool {
	return true
}

func (f *FarPlanetRingFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupFarPlanetRingPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupFarPlanetRingPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.FarPlanetRingBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *FarPlanetRingFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.FarPlanetRingPipeline = nil
}

func (f *FarPlanetRingFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return false
}

func (f *FarPlanetRingFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	return nil
}

func (f *FarPlanetRingFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.FarPlanetRingPipeline != nil &&
		a.BufferManager.DepthView != nil &&
		a.BufferManager.PlanetDepthView != nil &&
		a.BufferManager.TransparentAccumView != nil &&
		a.BufferManager.TransparentWeightView != nil &&
		a.BufferManager.FarPlanetRingCount > 0 &&
		a.BufferManager.FarPlanetRingBG0 != nil &&
		a.BufferManager.FarPlanetRingBG1 != nil &&
		a.BufferManager.FarPlanetRingBG2 != nil
}

func (f *FarPlanetRingFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil || a.FarPlanetRingPipeline == nil {
		return nil
	}
	if a.BufferManager.FarPlanetRingCount == 0 {
		return nil
	}
	if a.BufferManager.FarPlanetRingBG0 == nil || a.BufferManager.FarPlanetRingBG1 == nil || a.BufferManager.FarPlanetRingBG2 == nil {
		return nil
	}
	pass.SetPipeline(a.FarPlanetRingPipeline)
	pass.SetBindGroup(0, a.BufferManager.FarPlanetRingBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.FarPlanetRingBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.FarPlanetRingBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *FarPlanetRingFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.FarPlanetRingPipeline == nil {
		return
	}
	a.BufferManager.CreateFarPlanetRingBindGroups(a.FarPlanetRingPipeline)
}
