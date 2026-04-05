package app

import "github.com/cogentcore/webgpu/wgpu"

// SpriteFeature owns sprite pipeline lifecycle and accumulation rendering.
type SpriteFeature struct{}

func (f *SpriteFeature) Name() string {
	return "sprites"
}

func (f *SpriteFeature) Enabled(*App) bool {
	return true
}

func (f *SpriteFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupSpritesPipeline()
	return nil
}

func (f *SpriteFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupSpritesPipeline()
	return nil
}

func (f *SpriteFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *SpriteFeature) Update(*App) error {
	return nil
}

func (f *SpriteFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *SpriteFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.SpritesPipeline = nil
}

func (f *SpriteFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.SpritesPipeline != nil &&
		a.BufferManager.HasSpriteContribution()
}

func (f *SpriteFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.SpritesPipeline == nil || !a.BufferManager.HasSpriteContribution() || a.BufferManager.SpritesBindGroup1 == nil {
		return nil
	}

	pass.SetPipeline(a.SpritesPipeline)
	pass.SetBindGroup(1, a.BufferManager.SpritesBindGroup1, nil)
	for _, batch := range a.BufferManager.SpriteBatches {
		if batch.BindGroup0 == nil || batch.InstanceCount == 0 {
			continue
		}
		pass.SetBindGroup(0, batch.BindGroup0, nil)
		pass.Draw(6, batch.InstanceCount, 0, batch.FirstInstance)
	}
	return nil
}
