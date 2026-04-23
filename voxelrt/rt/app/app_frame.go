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
	if a.BufferManager.UpdateScene(a.Scene, a.Camera, aspect) {
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
	}

	// Update Camera Uniforms
	a.BufferManager.UpdateCamera(viewProj, invView, invProj, a.Camera.Position, lightPos, a.Scene.AmbientLight, sunIntensity, a.Scene.SkyAmbientMix, a.Camera.FarPlane(), a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)), a.Config.Width, a.Config.Height, lightingQuality)
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
	a.TextItems = a.TextItems[:0]
	a.TextVertexCount = 0
}

func (a *App) DrawText(text string, x, y float32, scale float32, color [4]float32) {
	a.TextItems = append(a.TextItems, core.TextItem{
		Text:     text,
		Position: [2]float32{x, y},
		Scale:    scale,
		Color:    color,
	})
}

func (a *App) MeasureText(text string, scale float32) (float32, float32) {
	if a.TextRenderer == nil {
		return 0, 0
	}
	return a.TextRenderer.MeasureText(text, scale)
}

func (a *App) GetLineHeight(scale float32) float32 {
	if a.TextRenderer == nil {
		return 0
	}
	return a.TextRenderer.GetLineHeight(scale)
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

	a.runFeatureCommandStage("Feature Pre-GBuffer", FeatureCommandStagePreGBuffer, encoder)
	a.runFeatureCommandStage("Feature Pre-GBuffer Volumes", FeatureCommandStagePreGBufferVolumes, encoder)

	// Compute Pass
	a.Profiler.BeginScope("G-Buffer")
	cPass := encoder.BeginComputePass(nil)
	cPass.SetPipeline(a.GBufferPipeline)
	cPass.SetBindGroup(0, a.BufferManager.GBufferBindGroup0, nil)
	cPass.SetBindGroup(1, a.BufferManager.GBufferBindGroup, nil)
	cPass.SetBindGroup(2, a.BufferManager.GBufferBindGroup2, nil)

	// Dispatch
	wgX := (a.Config.Width + 7) / 8
	wgY := (a.Config.Height + 7) / 8
	cPass.DispatchWorkgroups(wgX, wgY, 1)
	err = cPass.End()
	if err != nil {
		fmt.Printf("ERROR: G-Buffer pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("G-Buffer")

	// Hi-Z Pass
	a.Profiler.BeginScope("Hi-Z Generation")
	a.BufferManager.DispatchHiZ(encoder, a.BufferManager.DepthView)
	a.Profiler.EndScope("Hi-Z Generation")

	a.runFeatureCommandStage("Feature Post-GBuffer", FeatureCommandStagePostGBuffer, encoder)

	// Shadow Pass
	a.Profiler.BeginScope("Shadows")
	shadowCameraMotion := a.hasShadowCameraMotion()
	shadowUpdates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, shadowCameraMotion)
	a.BufferManager.PrepareShadowLights(a.Scene, shadowUpdates)

	a.BufferManager.DispatchShadowPass(encoder, shadowUpdates)
	a.BufferManager.RecordShadowUpdates(shadowUpdates, a.RenderFrameIndex, a.Scene.StructureRevision)
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
	a.Profiler.SetCount("ShadowUpdates", len(shadowUpdates))
	a.Profiler.SetCount("ShadowPointUpdates", shadowPointUpdates)
	a.Profiler.SetCount("ShadowSpotUpdates", shadowSpotUpdates)
	a.Profiler.SetCount("ShadowDirectionalUpdates", shadowDirectionalUpdates)
	a.ShadowUpdateSummary = formatShadowUpdateSummary(shadowUpdates)
	a.Profiler.EndScope("Shadows")

	hasLocalLights := a.BufferManager.HasLocalLights(a.Scene)
	hasSceneLights := len(a.Scene.Lights) > 0
	needsAccumulation := a.hasPassStageWork(FeaturePassStageAccumulation)
	a.Profiler.SetCount("LocalLights", boolToCount(hasLocalLights))
	a.Profiler.SetCount("SceneLights", boolToCount(hasSceneLights))
	a.Profiler.SetCount("AccumulationActive", boolToCount(needsAccumulation))

	a.runFeatureCommandStage("Feature Pre-Lighting", FeatureCommandStagePreLighting, encoder)

	// Lighting Pass
	a.Profiler.BeginScope("Tile Light Cull")
	if hasLocalLights {
		a.BufferManager.DispatchTiledLightCull(encoder, a.TiledLightCullPipeline)
	} else {
		a.BufferManager.ResetTiledLightCullState(encoder)
	}
	a.Profiler.EndScope("Tile Light Cull")

	a.Profiler.BeginScope("Lighting")
	lPass := encoder.BeginComputePass(nil)
	lPass.SetPipeline(a.LightingPipeline)
	lPass.SetBindGroup(0, a.BufferManager.LightingBindGroup, nil)
	lPass.SetBindGroup(1, a.BufferManager.LightingBindGroup2, nil)
	lPass.SetBindGroup(2, a.BufferManager.LightingBindGroupMaterial, nil) // For materials/sectors
	lPass.SetBindGroup(3, a.BufferManager.LightingTileBindGroup, nil)
	lPass.DispatchWorkgroups(wgX, wgY, 1)
	err = lPass.End()
	if err != nil {
		fmt.Printf("ERROR: Lighting pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Lighting")

	a.runFeatureCommandStage("Feature Post-Lighting", FeatureCommandStagePostLighting, encoder)

	// Debug Pass
	if a.DebugMode && a.Camera != nil && core.DebugMode(a.Camera.DebugMode) == core.DebugModeScene {
		dPass := encoder.BeginComputePass(nil)
		dPass.SetPipeline(a.DebugComputePipeline)
		dPass.SetBindGroup(0, a.BufferManager.DebugBindGroup0, nil)
		dPass.SetBindGroup(1, a.BindGroup1Debug, nil)
		dPass.DispatchWorkgroups(wgX, wgY, 1)
		err = dPass.End()
		if err != nil {
			fmt.Printf("ERROR: Debug pass End failed: %v\n", err)
		}
	}

	// Accumulation Pass (Transparent overlay + Particles) -> WBOIT targets
	a.Profiler.BeginScope("Accumulation")
	if needsAccumulation || a.HadAccumulationPass {
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
		if needsAccumulation {
			if err := a.renderPassStage(FeaturePassStageAccumulation, accPass); err != nil {
				fmt.Printf("ERROR: Feature accumulation render failed: %v\n", err)
			}
		}
		err = accPass.End()
		if err != nil {
			fmt.Printf("ERROR: Accumulation pass End failed: %v\n", err)
		}
	}
	a.HadAccumulationPass = needsAccumulation
	a.Profiler.EndScope("Accumulation")

	a.runFeatureCommandStage("Feature Pre-Resolve", FeatureCommandStagePreResolve, encoder)

	// Resolve Pass -> Swapchain (composite opaque + accum/weight)
	a.Profiler.BeginScope("Resolve")
	rPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
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

	err = rPass.End()
	if err != nil {
		fmt.Printf("ERROR: Resolve pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Resolve")

	a.runFeatureScreenStage("Feature Post-Resolve", FeatureScreenStagePostResolve, encoder, view)

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
