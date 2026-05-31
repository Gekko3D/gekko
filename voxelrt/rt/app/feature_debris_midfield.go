package app

import "github.com/cogentcore/webgpu/wgpu"

type DebrisMidfieldFeature struct{}

func (f *DebrisMidfieldFeature) Name() string {
	return "debris_midfield"
}

func (f *DebrisMidfieldFeature) Enabled(a *App) bool {
	if a == nil {
		return false
	}
	return a.FeatureConfig.Defaults.Transparency
}

func (f *DebrisMidfieldFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupDebrisMidfieldPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupDebrisMidfieldPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.DebrisMidfieldBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *DebrisMidfieldFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *DebrisMidfieldFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.DebrisMidfieldPipeline = nil
}

func (f *DebrisMidfieldFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.DebrisMidfieldPipeline != nil &&
		a.BufferManager.DepthView != nil &&
		a.BufferManager.PlanetDepthView != nil &&
		a.BufferManager.DebrisMidfieldCount > 0 &&
		a.BufferManager.DebrisMidfieldBG0 != nil &&
		a.BufferManager.DebrisMidfieldBG1 != nil &&
		a.BufferManager.DebrisMidfieldBG2 != nil
}

func (f *DebrisMidfieldFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.DebrisMidfieldPipeline == nil || a.BufferManager.DebrisMidfieldCount == 0 || a.BufferManager.DepthView == nil || a.BufferManager.PlanetDepthView == nil {
		return nil
	}
	if a.BufferManager.DebrisMidfieldBG0 == nil || a.BufferManager.DebrisMidfieldBG1 == nil || a.BufferManager.DebrisMidfieldBG2 == nil {
		return nil
	}

	pass.SetPipeline(a.DebrisMidfieldPipeline)
	pass.SetBindGroup(0, a.BufferManager.DebrisMidfieldBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.DebrisMidfieldBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.DebrisMidfieldBG2, nil)

	pass.Draw(6, a.BufferManager.DebrisMidfieldCount*64, 0, 0)
	return nil
}

func (f *DebrisMidfieldFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.DebrisMidfieldPipeline == nil {
		return
	}
	a.BufferManager.CreateDebrisMidfieldBindGroups(a.DebrisMidfieldPipeline)
}
