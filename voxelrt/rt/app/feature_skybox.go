package app

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

// SkyboxFeature owns skybox generation pipeline bootstrap.
type SkyboxFeature struct{}

func (f *SkyboxFeature) Name() string {
	return "skybox"
}

func (f *SkyboxFeature) Enabled(*App) bool {
	return true
}

func (f *SkyboxFeature) Setup(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.CreateSkyboxGenPipeline(shaders.SkyboxWGSL)
	return nil
}

func (f *SkyboxFeature) Resize(*App, uint32, uint32) error {
	return nil
}

func (f *SkyboxFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *SkyboxFeature) Update(*App) error {
	return nil
}

func (f *SkyboxFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *SkyboxFeature) Shutdown(a *App) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.SkyboxGenPipeline = nil
}
