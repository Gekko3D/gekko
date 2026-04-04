package app

import "github.com/cogentcore/webgpu/wgpu"

// TransparencyFeature owns transparent overlay pipeline lifecycle and accumulation rendering.
type TransparencyFeature struct{}

func (f *TransparencyFeature) Name() string {
	return "transparency"
}

func (f *TransparencyFeature) Enabled(*App) bool {
	return true
}

func (f *TransparencyFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupTransparentOverlayPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *TransparencyFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupTransparentOverlayPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *TransparencyFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *TransparencyFeature) Update(*App) error {
	return nil
}

func (f *TransparencyFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *TransparencyFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.TransparentPipeline = nil
}

func (f *TransparencyFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.TransparentPipeline == nil || !a.BufferManager.HasVisibleTransparentOverlay(a.Scene) {
		return nil
	}
	if a.BufferManager.TransparentBG0 == nil || a.BufferManager.TransparentBG1 == nil || a.BufferManager.TransparentBG2 == nil || a.BufferManager.TransparentBG3 == nil {
		return nil
	}

	pass.SetPipeline(a.TransparentPipeline)
	pass.SetBindGroup(0, a.BufferManager.TransparentBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.TransparentBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.TransparentBG2, nil)
	pass.SetBindGroup(3, a.BufferManager.TransparentBG3, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *TransparencyFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.TransparentPipeline == nil {
		return
	}
	a.BufferManager.StorageView = a.StorageView
	a.BufferManager.CreateTransparentOverlayBindGroups(a.TransparentPipeline)
}
