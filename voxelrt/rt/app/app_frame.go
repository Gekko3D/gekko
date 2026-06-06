package app

import (
	"fmt"
	"strings"

	"github.com/gekko3d/gekko/voxelrt/rt/core"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	occlusionFastCameraTranslationThreshold = 0.75
	occlusionFastCameraRotationThreshold    = 0.12
)

func renderModeLabel(mode uint32) string {
	switch mode {
	case 0:
		return "Lit"
	case 1:
		return "Albedo"
	case 2:
		return "Normals"
	case 3:
		return "G-Buffer"
	case 4:
		return "Direct"
	case 5:
		return "Indirect"
	case 6:
		return "Light Density"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}

func (a *App) setupTextures(w, h int) {
	if w == 0 || h == 0 {
		return
	}

	if a.StorageTexture != nil {
		a.StorageTexture.Release()
	}

	var err error
	a.StorageTexture, err = a.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Storage Tex",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA16Float,
		Usage:         wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment,
		SampleCount:   1,
	})
	if err != nil {
		panic(err)
	}
	a.StorageView, err = a.StorageTexture.CreateView(nil)
	if err != nil {
		panic(err)
	}
}

func (a *App) setupBindGroups() {
	var err error

	// Bind Group 1 Debug
	a.BindGroup1Debug, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.DebugComputePipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.StorageView},
		},
	})
	if err != nil {
		panic(err)
	}

	// Render BG for fullscreen blit
	a.RenderBG, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.RenderPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.StorageView},
			{Binding: 1, Sampler: a.Sampler},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (a *App) Resize(w, h int) {
	if w > 0 && h > 0 {
		a.Config.Width = uint32(w)
		a.Config.Height = uint32(h)
		a.Surface.Configure(a.Adapter, a.Device, a.Config)
		a.rebuildCoreSwapchainResources(w, h)
		a.rebuildCoreSceneBindings()

		if err := a.resizeFeatures(uint32(w), uint32(h)); err != nil {
			fmt.Printf("ERROR: Feature resize failed: %v\n", err)
		}
		if err := a.resizeRenderGraphNodes(uint32(w), uint32(h)); err != nil {
			fmt.Printf("ERROR: Render graph resize failed: %v\n", err)
		}
	}
}

func (a *App) Update() {
	a.PreviousProfilerStats = a.Profiler.GetStatsString()

	// Reset profiler timestamps for the upcoming render passes
	a.Profiler.Reset()

	// We assume a default light position or sync it if needed.
	// Sync with scene light 0 if available
	lightPos := mgl32.Vec3{500, 1000, 500}
	sunIntensity := float32(1.0)
	if len(a.Scene.Lights) > 0 {
		lp := a.Scene.Lights[0].Position
		lightPos = mgl32.Vec3{lp[0], lp[1], lp[2]}
		sunIntensity = a.Scene.Lights[0].Color[3]
		if sunIntensity < 0 {
			sunIntensity = 0
		}
	}

	// Matrices
	view := a.Camera.GetViewMatrix()
	aspect := float32(a.Config.Width) / float32(a.Config.Height)
	if aspect == 0 {
		aspect = 1.0
	}
	proj := a.Camera.ProjectionMatrix(aspect)

	// Combined
	viewProj := proj.Mul4(view)
	invView := view.Inv()
	invProj := proj.Inv()
	renderOrigin := a.Camera.Position
	fastCameraMotion := a.hasFastCameraMotion()

	// Readback Hi-Z from previous frame (cheap latency)
	hizData, hizW, hizH := a.BufferManager.ReadbackHiZ()

	// Commit scene changes from ECS sync
	a.Profiler.BeginScope("Scene Commit")
	planes := a.Camera.ExtractFrustum(viewProj)
	prevViewProj := a.LastViewProj
	a.Scene.Commit(planes, core.SceneCommitOptions{
		OcclusionMode:    a.OcclusionMode,
		HiZData:          hizData,
		HiZW:             hizW,
		HiZH:             hizH,
		LastViewProj:     a.LastViewProj,
		CameraPosition:   a.Camera.Position,
		FastCameraMotion: fastCameraMotion,
		Profiler:         a.Profiler,
	})
	a.Profiler.EndScope("Scene Commit")

	// Store current view-proj for next frame's Hi-Z reprojection
	a.LastViewProj = viewProj

	shadowGroupedVisible := 0
	visibleTerrainChunks := 0
	for _, obj := range a.Scene.VisibleObjects {
		if obj == nil {
			continue
		}
		if obj.ShadowGroupID != 0 {
			shadowGroupedVisible++
		}
		if obj.IsTerrainChunk {
			visibleTerrainChunks++
		}
	}
	a.Profiler.SetCount("Objects", len(a.Scene.Objects))
	a.Profiler.SetCount("Visible", len(a.Scene.VisibleObjects))
	a.Profiler.SetCount("FrustumVisible", a.Scene.OcclusionStats.FrustumVisible)
	a.Profiler.SetCount("HiZEligible", a.Scene.OcclusionStats.HiZEligible)
	a.Profiler.SetCount("HiZCulled", a.Scene.OcclusionStats.HiZCulled)
	a.Profiler.SetCount("HiZHysteresis", a.Scene.OcclusionStats.HiZHysteresisKept)
	a.Profiler.SetCount("HiZMotionDisabled", a.Scene.OcclusionStats.HiZMotionDisabled)
	a.Profiler.SetCount("Lights", len(a.Scene.Lights))
	a.Profiler.SetCount("VisibleLights", len(a.Scene.Lights))
	a.Profiler.SetCount("LightListEntriesAvg", a.BufferManager.TileLightAvgCount)
	a.Profiler.SetCount("LightListEntriesMax", a.BufferManager.TileLightMaxCount)
	a.Profiler.SetCount("Particles", int(a.BufferManager.ParticleCount))
	a.Profiler.SetCount("ShadowGrouped", shadowGroupedVisible)
	a.Profiler.SetCount("ShadowCasters", len(a.Scene.ShadowObjects))
	a.Profiler.SetCount("TerrainChunks", visibleTerrainChunks)
	a.Profiler.SetCount("CAVolumes", int(a.BufferManager.CAVolumeCount))
	a.Profiler.SetCount("CARequested", int(a.BufferManager.CARequestedVolumeCount))
	a.Profiler.SetCount("CAVisible", int(a.BufferManager.CAVolumeVisibleCount))
	a.Profiler.SetCount("CADropped", int(a.BufferManager.CADroppedVolumeCount))
	a.Profiler.SetCount("CASuspended", int(a.BufferManager.CASuspendedVolumeCount))
	a.Profiler.SetCount("CAResClamp", int(a.BufferManager.CAResolutionClampedCount))
	a.Profiler.SetCount("CAStepDefer", int(a.BufferManager.CADeferredStepVolumeCount))
	a.Profiler.SetCount("CASteps", int(a.BufferManager.CATotalScheduledSteps))
	a.Profiler.SetCount("CAAtlasW", int(a.BufferManager.CAAtlasWidth))
	a.Profiler.SetCount("CAAtlasH", int(a.BufferManager.CAAtlasHeight))
	a.Profiler.SetCount("CAAtlasD", int(a.BufferManager.CAAtlasDepth))
	a.Profiler.SetCount("CAAtlasCells", int(a.BufferManager.CAAtlasCellCount))
	a.Profiler.SetCount("CAAtlasBytes", int(a.BufferManager.CAAtlasByteCount))

	// Update Buffers
	lightingQuality := a.EffectiveLightingQuality()
	a.BufferManager.LightingQuality = lightingQuality
	a.Profiler.BeginScope("Buffer Update")
	recreated := false
	if a.BufferManager.UpdateScene(a.Scene, a.Camera, aspect, renderOrigin) {
		recreated = true
	}
	if a.BufferManager.UpdateTiledLightingResources(a.Config.Width, a.Config.Height) {
		recreated = true
	}
	a.Profiler.SetCount("VoxelSecUp", a.BufferManager.VoxelSectorsUploaded)
	a.Profiler.SetCount("VoxelBrkUp", a.BufferManager.VoxelBricksUploaded)
	a.Profiler.SetCount("VoxelSecPend", a.BufferManager.VoxelDirtySectorsPending)
	a.Profiler.SetCount("VoxelBrkPend", a.BufferManager.VoxelDirtyBricksPending)
	a.Profiler.SetCount("VoxelUniBrk", a.BufferManager.VoxelUniformSparseBricks)
	a.Profiler.SetCount("VoxelPayBrk", a.BufferManager.VoxelPayloadSparseBricks)
	a.Profiler.SetCount("VoxelPaySkip", a.BufferManager.VoxelPayloadUploadsSkipped)
	a.Profiler.SetCount("VoxelPayBytes", a.BufferManager.VoxelPayloadBytesAvoided)
	a.Profiler.EndScope("Buffer Update")
	a.Profiler.SetCount("ShadowCasters", len(a.Scene.ShadowObjects))

	if recreated {
		a.rebuildCoreSceneBindings()

		if err := a.sceneBuffersRecreatedFeatures(); err != nil {
			fmt.Printf("ERROR: Feature scene-buffer recreation failed: %v\n", err)
		}
		if err := a.sceneBuffersRecreatedRenderGraphNodes(); err != nil {
			fmt.Printf("ERROR: Render graph scene-buffer recreation failed: %v\n", err)
		}
	}

	// Update Camera Uniforms
	a.BufferManager.UpdateCamera(viewProj, invView, invProj, a.Camera.Position, lightPos, a.Scene.AmbientLight, renderOrigin, sunIntensity, a.Scene.SkyAmbientMix, a.Camera.FarPlane(), a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)), a.Config.Width, a.Config.Height, lightingQuality)
	a.BufferManager.BeginVolumetricFrame()
	historyBlend := float32(0.7)
	if fastCameraMotion {
		historyBlend = 0.12
	}
	a.BufferManager.UpdateVolumetricHistoryParams(prevViewProj, a.LastCameraPos, historyBlend, a.HasLastCameraState)
	a.updateTiledLightMetrics(viewProj, invView, a.Camera.Position)
	a.Profiler.SetCount("LightListEntriesAvg", a.BufferManager.TileLightAvgCount)
	a.Profiler.SetCount("LightListEntriesMax", a.BufferManager.TileLightMaxCount)
	if a.ResolvePipeline != nil {
		a.createResolveBindGroup(a.ResolvePipeline.GetBindGroupLayout(0))
	}
	if err := a.updateFeatures(); err != nil {
		fmt.Printf("ERROR: Feature update failed: %v\n", err)
	}
	if err := a.updateRenderGraphNodes(); err != nil {
		fmt.Printf("ERROR: Render graph update failed: %v\n", err)
	}
}

func (a *App) hasFastCameraMotion() bool {
	if a == nil || a.Camera == nil {
		return false
	}
	if !a.HasLastCameraState {
		return false
	}
	if a.Camera.Position.Sub(a.LastCameraPos).Len() > occlusionFastCameraTranslationThreshold {
		return true
	}
	if absf(a.Camera.Yaw-a.LastCameraYaw) > occlusionFastCameraRotationThreshold {
		return true
	}
	if absf(a.Camera.Pitch-a.LastCameraPitch) > occlusionFastCameraRotationThreshold {
		return true
	}
	return false
}

func (a *App) hasShadowCameraMotion() bool {
	if a == nil || a.Camera == nil {
		return false
	}
	if !a.HasLastCameraState {
		return true
	}
	if a.Camera.Position.Sub(a.LastCameraPos).Len() > 0.001 {
		return true
	}
	if absf(a.Camera.Yaw-a.LastCameraYaw) > 0.0005 {
		return true
	}
	if absf(a.Camera.Pitch-a.LastCameraPitch) > 0.0005 {
		return true
	}
	return false
}

func (a *App) recordCameraState() {
	if a == nil || a.Camera == nil {
		return
	}
	a.LastCameraPos = a.Camera.Position
	a.LastCameraYaw = a.Camera.Yaw
	a.LastCameraPitch = a.Camera.Pitch
	a.HasLastCameraState = true
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func boolToCount(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (a *App) updateTiledLightMetrics(viewProj, invView mgl32.Mat4, camPos mgl32.Vec3) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.EstimateTiledLightMetrics(a.Scene, viewProj, invView, camPos)
	if !a.BufferManager.HasLocalLights(a.Scene) {
		a.BufferManager.ResetTiledLightCullState(nil)
	}
}

func (a *App) ClearText() {
	textResources := a.ensureTextResources()
	textResources.Items = textResources.Items[:0]
	textResources.RectItems = textResources.RectItems[:0]
	textResources.VertexCount = 0
}

func (a *App) DrawText(text string, x, y float32, scale float32, color [4]float32) {
	textResources := a.ensureTextResources()
	textResources.Items = append(textResources.Items, core.TextItem{
		Text:     text,
		Position: [2]float32{x, y},
		Scale:    scale,
		Color:    color,
	})
}

func (a *App) DrawRect(x, y, w, h float32, color [4]float32) {
	textResources := a.ensureTextResources()
	textResources.RectItems = append(textResources.RectItems, core.RectItem{
		X:     x,
		Y:     y,
		W:     w,
		H:     h,
		Color: color,
	})
}

func (a *App) MeasureText(text string, scale float32) (float32, float32) {
	textRenderer := a.textRenderer()
	if textRenderer == nil {
		return 0, 0
	}
	return textRenderer.MeasureText(text, scale)
}

func (a *App) GetLineHeight(scale float32) float32 {
	textRenderer := a.textRenderer()
	if textRenderer == nil {
		return 0
	}
	return textRenderer.GetLineHeight(scale)
}

func (a *App) GetTextAscent(scale float32) float32 {
	textRenderer := a.textRenderer()
	if textRenderer == nil {
		return 0
	}
	return textRenderer.GetAscent(scale)
}

func (a *App) Render() {
	a.Profiler.BeginScope("Swapchain Wait")
	nextTexture, err := a.Surface.GetCurrentTexture()
	a.Profiler.EndScope("Swapchain Wait")
	if err != nil {
		fmt.Printf("ERROR: GetCurrentTexture failed: %v\n", err)
		return
	}
	defer nextTexture.Release()

	view, err := nextTexture.CreateView(nil)
	if err != nil {
		fmt.Printf("ERROR: CreateView failed: %v\n", err)
		return
	}
	defer view.Release()

	encoder, err := a.Device.CreateCommandEncoder(nil)
	if err != nil {
		fmt.Printf("ERROR: CreateCommandEncoder failed: %v\n", err)
		return
	}

	wgX := (a.Config.Width + 7) / 8
	wgY := (a.Config.Height + 7) / 8
	frame := &FrameContext{
		Width:         a.Config.Width,
		Height:        a.Config.Height,
		SwapchainView: view,
		WorkgroupsX:   wgX,
		WorkgroupsY:   wgY,
	}

	a.recordRenderFrameMetrics()
	a.recordRenderGraph(encoder, frame)

	a.Profiler.BeginScope("Submit/Present")
	cmd, err := encoder.Finish(nil)
	if err != nil {
		a.Profiler.EndScope("Submit/Present")
		fmt.Printf("ERROR: Encoder Finish failed: %v\n", err)
		return
	}
	a.Queue.Submit(cmd)
	a.BufferManager.ResolveHiZReadback()
	a.Surface.Present()
	a.Device.Poll(false, nil)
	a.BufferManager.AdvanceRetiredBuffers()
	a.Profiler.EndScope("Submit/Present")

	// Update FPS
	now := glfw.GetTime()
	if a.LastRenderTime > 0 {
		a.FrameCount++
		a.FPSTime += now - a.LastRenderTime
		if a.FPSTime >= 1.0 {
			a.FPS = float64(a.FrameCount) / a.FPSTime
			a.FrameCount = 0
			a.FPSTime = 0
		}
	}
	a.LastRenderTime = now
	a.BufferManager.CommitVolumetricFrame(a.BufferManager.AnalyticMediumCount > 0)
	a.recordCameraState()
	a.RenderFrameIndex++
}

func (a *App) recordRenderFrameMetrics() {
	if a == nil || a.Profiler == nil {
		return
	}
	hasLocalLights := false
	if a.BufferManager != nil {
		hasLocalLights = a.BufferManager.HasLocalLights(a.Scene)
	}
	hasSceneLights := a.Scene != nil && len(a.Scene.Lights) > 0
	needsAccumulation := a.hasPassStageWorkForRenderGraph(FeaturePassStageAccumulation)
	a.Profiler.SetCount("LocalLights", boolToCount(hasLocalLights))
	a.Profiler.SetCount("SceneLights", boolToCount(hasSceneLights))
	a.Profiler.SetCount("AccumulationActive", boolToCount(needsAccumulation))
}

func (a *App) recordRenderGraph(encoder *wgpu.CommandEncoder, frame *FrameContext) {
	if a == nil {
		return
	}
	if a.RenderGraph == nil {
		for _, nodeName := range runtimeRenderGraphNodeSequence() {
			a.runLegacyRenderGraphFeatureNode(nodeName, encoder, frame)
		}
		return
	}
	if err := a.RenderGraph.Record(a, encoder, frame); err != nil {
		fmt.Printf("ERROR: Render graph failed: %v\n", err)
	}
}

func runtimeRenderGraphNodesBeforeLightingMetrics() []string {
	return []string{
		RenderNodeFeatureParticlesSim,
		RenderNodeFeaturePreGBuffer,
		RenderNodeFeatureCAVolumesSim,
		RenderNodeFeaturePreGBufferVolumes,
		RenderNodeCoreGBuffer,
		RenderNodeCoreHiZ,
		RenderNodeFeaturePostGBuffer,
		RenderNodeCoreShadows,
	}
}

func runtimeRenderGraphNodesAfterLightingMetrics() []string {
	return []string{
		RenderNodeFeaturePreLighting,
		RenderNodeFeatureSkyboxUpdate,
		RenderNodeCoreTiledLightCull,
		RenderNodeCoreLighting,
		RenderNodeFeaturePostLighting,
		RenderNodeFeatureCAVolumesRender,
		RenderNodeFeatureAstronomical,
		RenderNodeFeaturePlanetBodies,
		RenderNodeFeatureAnalyticMedia,
		RenderNodeCoreDebugScene,
		RenderNodeCoreAccumulation,
		RenderNodeFeaturePreResolve,
		RenderNodeCoreResolve,
		RenderNodeFeatureTextOverlay,
		RenderNodeFeatureGizmosOverlay,
		RenderNodeFeaturePostResolve,
	}
}

func runtimeRenderGraphNodeSequence() []string {
	nodes := runtimeRenderGraphNodesBeforeLightingMetrics()
	nodes = append(nodes, runtimeRenderGraphNodesAfterLightingMetrics()...)
	return nodes
}

func (a *App) runLegacyRenderGraphFeatureNode(name string, encoder *wgpu.CommandEncoder, frame *FrameContext) {
	switch name {
	case RenderNodeFeatureParticlesSim:
		if err := a.recordParticlesSimulationPass(encoder); err != nil {
			fmt.Printf("ERROR: Particle simulation pass failed: %v\n", err)
		}
	case RenderNodeFeaturePreGBuffer:
		a.runFeatureCommandStage("Feature Pre-GBuffer", FeatureCommandStagePreGBuffer, encoder)
	case RenderNodeFeatureCAVolumesSim:
		if err := a.recordCAVolumeSimulationPass(encoder); err != nil {
			fmt.Printf("ERROR: CA volume simulation pass failed: %v\n", err)
		}
	case RenderNodeFeaturePreGBufferVolumes:
		a.runFeatureCommandStage("Feature Pre-GBuffer Volumes", FeatureCommandStagePreGBufferVolumes, encoder)
	case RenderNodeCoreGBuffer:
		if err := a.recordGBufferPass(encoder, frame); err != nil {
			fmt.Printf("ERROR: G-Buffer pass failed: %v\n", err)
		}
	case RenderNodeFeaturePostGBuffer:
		a.runFeatureCommandStage("Feature Post-GBuffer", FeatureCommandStagePostGBuffer, encoder)
	case RenderNodeCoreHiZ:
		if err := a.recordHiZPass(encoder); err != nil {
			fmt.Printf("ERROR: Hi-Z pass failed: %v\n", err)
		}
	case RenderNodeCoreShadows:
		if err := a.recordShadowPass(encoder); err != nil {
			fmt.Printf("ERROR: Shadow pass failed: %v\n", err)
		}
	case RenderNodeFeaturePreLighting:
		a.runFeatureCommandStage("Feature Pre-Lighting", FeatureCommandStagePreLighting, encoder)
	case RenderNodeFeatureSkyboxUpdate:
		// Skybox input is applied during graph update before render recording.
	case RenderNodeCoreTiledLightCull:
		if err := a.recordTiledLightCullPass(encoder); err != nil {
			fmt.Printf("ERROR: Tile light cull failed: %v\n", err)
		}
	case RenderNodeCoreLighting:
		if err := a.recordLightingPass(encoder, frame); err != nil {
			fmt.Printf("ERROR: Lighting pass failed: %v\n", err)
		}
	case RenderNodeFeaturePostLighting:
		a.runFeatureCommandStage("Feature Post-Lighting", FeatureCommandStagePostLighting, encoder)
	case RenderNodeFeatureCAVolumesRender:
		if err := a.recordCAVolumeRenderPass(encoder); err != nil {
			fmt.Printf("ERROR: CA volume render pass failed: %v\n", err)
		}
	case RenderNodeFeatureAstronomical:
		if err := a.recordAstronomicalPass(encoder); err != nil {
			fmt.Printf("ERROR: Astronomical pass failed: %v\n", err)
		}
	case RenderNodeFeaturePlanetBodies:
		if err := a.recordPlanetBodiesPass(encoder); err != nil {
			fmt.Printf("ERROR: Planet bodies pass failed: %v\n", err)
		}
	case RenderNodeFeatureAnalyticMedia:
		if err := a.recordAnalyticMediumPass(encoder); err != nil {
			fmt.Printf("ERROR: Analytic media pass failed: %v\n", err)
		}
	case RenderNodeCoreDebugScene:
		if err := a.recordDebugScenePass(encoder, frame); err != nil {
			fmt.Printf("ERROR: Debug pass failed: %v\n", err)
		}
	case RenderNodeCoreAccumulation:
		if err := a.recordAccumulationPass(encoder); err != nil {
			fmt.Printf("ERROR: Accumulation pass failed: %v\n", err)
		}
	case RenderNodeFeaturePreResolve:
		a.runFeatureCommandStage("Feature Pre-Resolve", FeatureCommandStagePreResolve, encoder)
	case RenderNodeCoreResolve:
		if err := a.recordResolvePass(encoder, frame); err != nil {
			fmt.Printf("ERROR: Resolve pass failed: %v\n", err)
		}
	case RenderNodeFeatureTextOverlay:
		if err := a.recordTextOverlayPass(encoder, frame); err != nil {
			fmt.Printf("ERROR: Text overlay pass failed: %v\n", err)
		}
	case RenderNodeFeatureGizmosOverlay:
		if err := a.recordGizmosOverlayPass(encoder, frame); err != nil {
			fmt.Printf("ERROR: Gizmos overlay pass failed: %v\n", err)
		}
	case RenderNodeFeaturePostResolve:
		var target *wgpu.TextureView
		if frame != nil {
			target = frame.SwapchainView
		}
		a.runFeatureScreenStage("Feature Post-Resolve", FeatureScreenStagePostResolve, encoder, target)
	}
}

func (a *App) tiledLightCullPassEnabled() bool {
	return a != nil && a.BufferManager != nil
}

func (a *App) gBufferPassEnabled() bool {
	return a != nil && a.BufferManager != nil
}

func (a *App) recordGBufferPass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.gBufferPassEnabled() {
		return nil
	}

	manager := a.BufferManager
	a.Profiler.SetCount("GBufferGraphNode", 1)
	a.Profiler.SetCount("GBufferPipelineReady", boolToCount(a.GBufferPipeline != nil))
	a.Profiler.SetCount("GBufferBG0Ready", boolToCount(manager.GBufferBindGroup0 != nil))
	a.Profiler.SetCount("GBufferBG1Ready", boolToCount(manager.GBufferBindGroup != nil))
	a.Profiler.SetCount("GBufferBG2Ready", boolToCount(manager.GBufferBindGroup2 != nil))

	if encoder == nil {
		return fmt.Errorf("g-buffer command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("g-buffer frame context is nil")
	}

	workgroupsX := frame.WorkgroupsX
	workgroupsY := frame.WorkgroupsY
	if workgroupsX == 0 && frame.Width > 0 {
		workgroupsX = (frame.Width + 7) / 8
	}
	if workgroupsY == 0 && frame.Height > 0 {
		workgroupsY = (frame.Height + 7) / 8
	}
	a.Profiler.SetCount("GBufferWorkgroupsX", int(workgroupsX))
	a.Profiler.SetCount("GBufferWorkgroupsY", int(workgroupsY))

	if workgroupsX == 0 || workgroupsY == 0 {
		return fmt.Errorf("g-buffer workgroup count is zero")
	}
	if a.GBufferPipeline == nil {
		return fmt.Errorf("g-buffer pipeline is nil")
	}
	if manager.GBufferBindGroup0 == nil {
		return fmt.Errorf("g-buffer bind group 0 is nil")
	}
	if manager.GBufferBindGroup == nil {
		return fmt.Errorf("g-buffer bind group 1 is nil")
	}
	if manager.GBufferBindGroup2 == nil {
		return fmt.Errorf("g-buffer bind group 2 is nil")
	}

	a.Profiler.BeginScope("G-Buffer")
	defer a.Profiler.EndScope("G-Buffer")

	cPass := encoder.BeginComputePass(nil)
	cPass.SetPipeline(a.GBufferPipeline)
	cPass.SetBindGroup(0, manager.GBufferBindGroup0, nil)
	cPass.SetBindGroup(1, manager.GBufferBindGroup, nil)
	cPass.SetBindGroup(2, manager.GBufferBindGroup2, nil)
	cPass.DispatchWorkgroups(workgroupsX, workgroupsY, 1)
	if err := cPass.End(); err != nil {
		return fmt.Errorf("g-buffer pass End failed: %w", err)
	}
	return nil
}

func (a *App) recordTiledLightCullPass(encoder *wgpu.CommandEncoder) error {
	if !a.tiledLightCullPassEnabled() {
		return nil
	}

	manager := a.BufferManager
	hasLocalLights := manager.HasLocalLights(a.Scene)
	a.Profiler.SetCount("TiledCullGraphNode", 1)
	a.Profiler.SetCount("TiledCullLocalLights", boolToCount(hasLocalLights))
	a.Profiler.SetCount("TiledCullPipelineReady", boolToCount(a.TiledLightCullPipeline != nil))
	a.Profiler.SetCount("TiledCullBG0Ready", boolToCount(manager.TiledLightCullBindGroup0 != nil))
	a.Profiler.SetCount("TiledCullBG1Ready", boolToCount(manager.TiledLightCullBindGroup1 != nil))
	a.Profiler.SetCount("TiledCullTilesX", int(manager.TileLightTilesX))
	a.Profiler.SetCount("TiledCullTilesY", int(manager.TileLightTilesY))

	a.Profiler.BeginScope("Tile Light Cull")
	defer a.Profiler.EndScope("Tile Light Cull")

	if !hasLocalLights {
		manager.ResetTiledLightCullState(encoder)
		return nil
	}
	if encoder == nil {
		return fmt.Errorf("tile light cull command encoder is nil")
	}
	if a.TiledLightCullPipeline == nil {
		return fmt.Errorf("tile light cull pipeline is nil")
	}
	if manager.TiledLightCullBindGroup0 == nil {
		return fmt.Errorf("tile light cull bind group 0 is nil")
	}
	if manager.TiledLightCullBindGroup1 == nil {
		return fmt.Errorf("tile light cull bind group 1 is nil")
	}
	if manager.TileLightTilesX == 0 || manager.TileLightTilesY == 0 {
		return fmt.Errorf("tile light cull tile dimensions are zero")
	}

	manager.DispatchTiledLightCull(encoder, a.TiledLightCullPipeline)
	return nil
}

func (a *App) lightingPassEnabled() bool {
	return a != nil && a.BufferManager != nil
}

func (a *App) hiZPassEnabled() bool {
	return a != nil && a.BufferManager != nil && a.OcclusionMode == core.OcclusionConservative
}

func (a *App) shadowPassEnabled() bool {
	return a != nil && a.BufferManager != nil
}

func (a *App) recordShadowPass(encoder *wgpu.CommandEncoder) error {
	if !a.shadowPassEnabled() {
		return nil
	}
	if a.Scene == nil {
		return fmt.Errorf("shadow scene is nil")
	}

	manager := a.BufferManager
	a.Profiler.BeginScope("Shadows")
	defer a.Profiler.EndScope("Shadows")

	shadowCameraMotion := a.hasShadowCameraMotion()
	shadowUpdates := manager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, shadowCameraMotion)
	manager.PrepareShadowLights(a.Scene, shadowUpdates)

	shadowPointUpdates := 0
	shadowSpotUpdates := 0
	shadowDirectionalUpdates := 0
	for _, update := range shadowUpdates {
		switch update.Kind {
		case core.ShadowUpdateKindDirectional:
			shadowDirectionalUpdates++
		case core.ShadowUpdateKindPoint:
			shadowPointUpdates++
		case core.ShadowUpdateKindSpot:
			shadowSpotUpdates++
		}
	}
	a.Profiler.SetCount("ShadowGraphNode", 1)
	a.Profiler.SetCount("ShadowCameraMotion", boolToCount(shadowCameraMotion))
	a.Profiler.SetCount("ShadowUpdates", len(shadowUpdates))
	a.Profiler.SetCount("ShadowPointUpdates", shadowPointUpdates)
	a.Profiler.SetCount("ShadowSpotUpdates", shadowSpotUpdates)
	a.Profiler.SetCount("ShadowDirectionalUpdates", shadowDirectionalUpdates)
	a.Profiler.SetCount("ShadowPipelineReady", boolToCount(manager.ShadowPipeline != nil))
	a.Profiler.SetCount("ShadowBG0Ready", boolToCount(manager.ShadowBindGroup0 != nil))
	a.Profiler.SetCount("ShadowBG1Ready", boolToCount(manager.ShadowBindGroup1 != nil))
	a.Profiler.SetCount("ShadowBG2Ready", boolToCount(manager.ShadowBindGroup2 != nil))
	a.Profiler.SetCount("ShadowLayerCount", len(manager.ShadowLayerParams))
	a.ShadowUpdateSummary = formatShadowUpdateSummary(shadowUpdates)

	if len(shadowUpdates) == 0 {
		manager.RecordShadowUpdates(shadowUpdates, a.RenderFrameIndex, a.Scene.StructureRevision)
		return nil
	}
	if encoder == nil {
		return fmt.Errorf("shadow command encoder is nil")
	}
	if manager.ShadowPipeline == nil {
		return fmt.Errorf("shadow pipeline is nil")
	}
	if manager.ShadowBindGroup0 == nil {
		return fmt.Errorf("shadow bind group 0 is nil")
	}
	if manager.ShadowBindGroup1 == nil {
		return fmt.Errorf("shadow bind group 1 is nil")
	}
	if manager.ShadowBindGroup2 == nil {
		return fmt.Errorf("shadow bind group 2 is nil")
	}

	manager.DispatchShadowPass(encoder, shadowUpdates)
	manager.RecordShadowUpdates(shadowUpdates, a.RenderFrameIndex, a.Scene.StructureRevision)
	return nil
}

func (a *App) recordHiZPass(encoder *wgpu.CommandEncoder) error {
	if !a.hiZPassEnabled() {
		return nil
	}

	manager := a.BufferManager
	a.Profiler.SetCount("HiZGraphNode", 1)
	a.Profiler.SetCount("HiZPipelineReady", boolToCount(manager.HiZPipeline != nil))
	a.Profiler.SetCount("HiZTextureReady", boolToCount(manager.HiZTexture != nil))
	a.Profiler.SetCount("HiZDepthReady", boolToCount(manager.DepthView != nil))
	a.Profiler.SetCount("HiZMipViews", len(manager.HiZViews))
	a.Profiler.SetCount("HiZBindGroups", len(manager.HiZBindGroups))
	a.Profiler.SetCount("HiZCameraBufReady", boolToCount(manager.CameraBuf != nil))
	a.Profiler.SetCount("HiZReadbackReady", boolToCount(manager.ReadbackBuffer != nil))

	if encoder == nil {
		return fmt.Errorf("hi-z command encoder is nil")
	}
	if manager.HiZPipeline == nil {
		return fmt.Errorf("hi-z pipeline is nil")
	}
	if manager.HiZTexture == nil {
		return fmt.Errorf("hi-z texture is nil")
	}
	if manager.DepthView == nil {
		return fmt.Errorf("hi-z source depth view is nil")
	}
	if len(manager.HiZViews) == 0 || manager.HiZViews[0] == nil {
		return fmt.Errorf("hi-z mip 0 view is nil")
	}
	if len(manager.HiZBindGroups) < len(manager.HiZViews) {
		return fmt.Errorf("hi-z bind group count %d is less than mip view count %d", len(manager.HiZBindGroups), len(manager.HiZViews))
	}
	if manager.CameraBuf == nil {
		return fmt.Errorf("hi-z camera buffer is nil")
	}
	if manager.ReadbackBuffer == nil {
		return fmt.Errorf("hi-z readback buffer is nil")
	}
	if manager.HiZReadbackWidth == 0 || manager.HiZReadbackHeight == 0 {
		return fmt.Errorf("hi-z readback dimensions are zero")
	}

	a.Profiler.BeginScope("Hi-Z Generation")
	defer a.Profiler.EndScope("Hi-Z Generation")

	manager.DispatchHiZ(encoder, manager.DepthView)
	return nil
}

func (a *App) recordLightingPass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.lightingPassEnabled() {
		return nil
	}

	manager := a.BufferManager
	a.Profiler.SetCount("LightingGraphNode", 1)
	a.Profiler.SetCount("LightingPipelineReady", boolToCount(a.LightingPipeline != nil))
	a.Profiler.SetCount("LightingBG0Ready", boolToCount(manager.LightingBindGroup != nil))
	a.Profiler.SetCount("LightingBG1Ready", boolToCount(manager.LightingBindGroup2 != nil))
	a.Profiler.SetCount("LightingBG2Ready", boolToCount(manager.LightingBindGroupMaterial != nil))
	a.Profiler.SetCount("LightingBG3Ready", boolToCount(manager.LightingTileBindGroup != nil))

	if encoder == nil {
		return fmt.Errorf("lighting command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("lighting frame context is nil")
	}

	workgroupsX := frame.WorkgroupsX
	workgroupsY := frame.WorkgroupsY
	if workgroupsX == 0 && frame.Width > 0 {
		workgroupsX = (frame.Width + 7) / 8
	}
	if workgroupsY == 0 && frame.Height > 0 {
		workgroupsY = (frame.Height + 7) / 8
	}
	a.Profiler.SetCount("LightingWorkgroupsX", int(workgroupsX))
	a.Profiler.SetCount("LightingWorkgroupsY", int(workgroupsY))

	if workgroupsX == 0 || workgroupsY == 0 {
		return fmt.Errorf("lighting workgroup count is zero")
	}
	if a.LightingPipeline == nil {
		return fmt.Errorf("lighting pipeline is nil")
	}
	if manager.LightingBindGroup == nil {
		return fmt.Errorf("lighting bind group 0 is nil")
	}
	if manager.LightingBindGroup2 == nil {
		return fmt.Errorf("lighting bind group 1 is nil")
	}
	if manager.LightingBindGroupMaterial == nil {
		return fmt.Errorf("lighting bind group 2 is nil")
	}
	if manager.LightingTileBindGroup == nil {
		return fmt.Errorf("lighting bind group 3 is nil")
	}

	a.Profiler.BeginScope("Lighting")
	defer a.Profiler.EndScope("Lighting")

	lPass := encoder.BeginComputePass(nil)
	lPass.SetPipeline(a.LightingPipeline)
	lPass.SetBindGroup(0, manager.LightingBindGroup, nil)
	lPass.SetBindGroup(1, manager.LightingBindGroup2, nil)
	lPass.SetBindGroup(2, manager.LightingBindGroupMaterial, nil)
	lPass.SetBindGroup(3, manager.LightingTileBindGroup, nil)
	lPass.DispatchWorkgroups(workgroupsX, workgroupsY, 1)
	if err := lPass.End(); err != nil {
		return fmt.Errorf("lighting pass End failed: %w", err)
	}
	return nil
}

func (a *App) accumulationPassEnabled() bool {
	return a != nil && a.BufferManager != nil
}

func (a *App) recordAccumulationPass(encoder *wgpu.CommandEncoder) error {
	if !a.accumulationPassEnabled() {
		return nil
	}

	needsAccumulation := a.hasPassStageWorkForRenderGraph(FeaturePassStageAccumulation)
	hadAccumulation := a.hadAccumulationPass()
	a.Profiler.SetCount("AccumulationGraphNode", 1)
	a.Profiler.SetCount("AccumulationActive", boolToCount(needsAccumulation))
	a.Profiler.SetCount("AccumulationHadPrevious", boolToCount(hadAccumulation))
	defer func() {
		a.setHadAccumulationPass(needsAccumulation)
	}()

	a.Profiler.BeginScope("Accumulation")
	defer a.Profiler.EndScope("Accumulation")

	if !needsAccumulation && !hadAccumulation {
		return nil
	}
	if encoder == nil {
		return fmt.Errorf("accumulation command encoder is nil")
	}
	if a.BufferManager.TransparentAccumView == nil {
		return fmt.Errorf("transparent accumulation view is nil")
	}
	if a.BufferManager.TransparentWeightView == nil {
		return fmt.Errorf("transparent weight view is nil")
	}

	accPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       a.BufferManager.TransparentAccumView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 0},
			},
			{
				View:       a.BufferManager.TransparentWeightView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 0},
			},
		},
	})
	var featureErr error
	if needsAccumulation {
		featureErr = a.renderPassStageForRenderGraph(FeaturePassStageAccumulation, accPass)
	}
	if err := accPass.End(); err != nil {
		if featureErr != nil {
			return fmt.Errorf("accumulation pass End failed after feature render error %v: %w", featureErr, err)
		}
		return fmt.Errorf("accumulation pass End failed: %w", err)
	}
	if featureErr != nil {
		return fmt.Errorf("feature accumulation render failed: %w", featureErr)
	}
	return nil
}

func (a *App) resolvePassEnabled() bool {
	return a != nil
}

func (a *App) recordResolvePass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.resolvePassEnabled() {
		return nil
	}

	a.Profiler.SetCount("ResolveGraphNode", 1)
	a.Profiler.SetCount("ResolvePipelineReady", boolToCount(a.ResolvePipeline != nil))
	a.Profiler.SetCount("ResolveBGReady", boolToCount(a.ResolveBG != nil))

	if encoder == nil {
		return fmt.Errorf("resolve command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("resolve frame context is nil")
	}
	if frame.SwapchainView == nil {
		return fmt.Errorf("resolve swapchain view is nil")
	}

	a.Profiler.BeginScope("Resolve")
	defer a.Profiler.EndScope("Resolve")

	rPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       frame.SwapchainView,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
		}},
	})
	if a.ResolvePipeline != nil && a.ResolveBG != nil {
		rPass.SetPipeline(a.ResolvePipeline)
		rPass.SetBindGroup(0, a.ResolveBG, nil)
		rPass.Draw(3, 1, 0, 0)
	}
	if err := rPass.End(); err != nil {
		return fmt.Errorf("resolve pass End failed: %w", err)
	}
	return nil
}

func (a *App) debugScenePassEnabled() bool {
	return a != nil && a.DebugMode && a.Camera != nil && core.DebugMode(a.Camera.DebugMode) == core.DebugModeScene
}

func (a *App) recordDebugScenePass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.debugScenePassEnabled() {
		return nil
	}
	if encoder == nil {
		return fmt.Errorf("debug scene command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("debug scene frame context is nil")
	}

	workgroupsX := frame.WorkgroupsX
	workgroupsY := frame.WorkgroupsY
	if workgroupsX == 0 && frame.Width > 0 {
		workgroupsX = (frame.Width + 7) / 8
	}
	if workgroupsY == 0 && frame.Height > 0 {
		workgroupsY = (frame.Height + 7) / 8
	}
	if workgroupsX == 0 || workgroupsY == 0 {
		return fmt.Errorf("debug scene workgroup count is zero")
	}

	dPass := encoder.BeginComputePass(nil)
	dPass.SetPipeline(a.DebugComputePipeline)
	dPass.SetBindGroup(0, a.BufferManager.DebugBindGroup0, nil)
	dPass.SetBindGroup(1, a.BindGroup1Debug, nil)
	dPass.DispatchWorkgroups(workgroupsX, workgroupsY, 1)
	if err := dPass.End(); err != nil {
		return fmt.Errorf("debug pass End failed: %w", err)
	}
	return nil
}

func (a *App) runFeatureCommandStage(scope string, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) {
	if a == nil || encoder == nil || !a.hasCommandStageWork(stage) {
		return
	}
	a.Profiler.BeginScope(scope)
	if err := a.dispatchCommandStage(stage, encoder); err != nil {
		fmt.Printf("ERROR: Feature command stage %d failed: %v\n", stage, err)
	}
	a.Profiler.EndScope(scope)
}

func (a *App) runFeatureScreenStage(scope string, stage FeatureScreenStage, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) {
	if a == nil || encoder == nil || target == nil || !a.hasScreenStageWork(stage) {
		return
	}
	a.Profiler.BeginScope(scope)
	if err := a.renderScreenStage(stage, encoder, target); err != nil {
		fmt.Printf("ERROR: Feature screen stage %d failed: %v\n", stage, err)
	}
	a.Profiler.EndScope(scope)
}

func formatShadowUpdateSummary(updates []core.ShadowUpdate) string {
	if len(updates) == 0 {
		return "none"
	}

	const maxEntries = 6
	capacity := len(updates)
	if capacity > maxEntries {
		capacity = maxEntries
	}
	parts := make([]string, 0, capacity)
	for i, update := range updates {
		if i >= maxEntries {
			break
		}
		switch update.Kind {
		case core.ShadowUpdateKindDirectional:
			parts = append(parts, fmt.Sprintf("D%d:%d@%d", update.LightIndex, update.CascadeIndex, update.Resolution))
		case core.ShadowUpdateKindPoint:
			parts = append(parts, fmt.Sprintf("P%d:%d@%d", update.LightIndex, update.CascadeIndex, update.Resolution))
		case core.ShadowUpdateKindSpot:
			parts = append(parts, fmt.Sprintf("S%d@%d", update.LightIndex, update.Resolution))
		default:
			parts = append(parts, fmt.Sprintf("U%d", update.LightIndex))
		}
	}
	if len(updates) > maxEntries {
		parts = append(parts, fmt.Sprintf("+%d more", len(updates)-maxEntries))
	}
	return strings.Join(parts, ", ")
}

/*
*

	setupParticlesPipeline creates the additive billboard particle pipeline targeting the swapchain format.
*/
