package app

// rebuildCoreSwapchainResources recreates core render targets and swapchain-bound
// bind groups. Feature-owned resources are rebuilt via feature lifecycle hooks.
func (a *App) rebuildCoreSwapchainResources(width, height int) {
	if a == nil || a.BufferManager == nil || width <= 0 || height <= 0 {
		return
	}

	a.setupTextures(width, height)
	a.BufferManager.CreateGBufferTextures(uint32(width), uint32(height))
	a.BufferManager.UpdateTiledLightingResources(uint32(width), uint32(height))
	a.BufferManager.StorageView = a.StorageView
	a.setupBindGroups()
	a.setupResolvePipeline()
}

// rebuildCoreSceneBindings recreates bind groups backed by scene, lighting, or
// core render targets. Optional feature bindings are rebuilt separately.
func (a *App) rebuildCoreSceneBindings() {
	if a == nil || a.BufferManager == nil {
		return
	}

	if a.DebugComputePipeline != nil {
		a.BufferManager.CreateDebugBindGroups(a.DebugComputePipeline)
	}
	if a.TiledLightCullPipeline != nil {
		a.BufferManager.CreateTiledLightCullBindGroups(a.TiledLightCullPipeline)
	}
	if a.GBufferPipeline != nil && a.LightingPipeline != nil {
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
	}
	if a.LightingPipeline != nil && a.StorageView != nil {
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
	}
	a.BufferManager.CreateShadowBindGroups()
}
