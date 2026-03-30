package app

import (
	"fmt"
	"strings"
	"unsafe"

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
		a.setupTextures(w, h)
		a.setupBindGroups()

		// Resize G-Buffer
		a.BufferManager.CreateGBufferTextures(uint32(w), uint32(h))
		a.BufferManager.UpdateTiledLightingResources(uint32(w), uint32(h))
		a.BufferManager.CreateTiledLightCullBindGroups(a.TiledLightCullPipeline)
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
		a.BufferManager.StorageView = a.StorageView
		// Ensure shadow bind groups remain valid after any resource changes
		a.BufferManager.CreateShadowBindGroups()
		if a.TransparentPipeline != nil {
			a.BufferManager.CreateTransparentOverlayBindGroups(a.TransparentPipeline)
		}

		// Recreate particle pipeline for new swapchain format
		a.setupParticlesPipeline()
		a.setupSpritesPipeline()
		a.setupCAVolumePipeline()
		// Recreate resolve pipeline/bind group (depends on textures/swapchain)
		a.setupResolvePipeline()
	}
}

func (a *App) Update() {
	if a.DebugMode {
		stats := fmt.Sprintf(
			"FPS: %.1f\nRender Mode: %s\n%s",
			a.FPS,
			renderModeLabel(a.RenderMode),
			a.Profiler.GetStatsString(),
		)
		if a.ShadowUpdateSummary != "" {
			stats += fmt.Sprintf("\nShadow Refresh:\n  %s\n", a.ShadowUpdateSummary)
		}
		// Position at top-right (approx 260px width for text block)
		x := float32(a.Config.Width) - 260
		a.DrawText(stats, x, 10, 0.6, [4]float32{1, 1, 0, 1})
	}

	// Reset profiler timestamps for the upcoming render passes
	a.Profiler.Reset()

	// We assume a default light position or sync it if needed.
	// Sync with scene light 0 if available
	lightPos := mgl32.Vec3{500, 1000, 500}
	if len(a.Scene.Lights) > 0 {
		lp := a.Scene.Lights[0].Position
		lightPos = mgl32.Vec3{lp[0], lp[1], lp[2]}
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
	a.Scene.Commit(planes, core.SceneCommitOptions{
		OcclusionMode:    a.OcclusionMode,
		HiZData:          hizData,
		HiZW:             hizW,
		HiZH:             hizH,
		LastViewProj:     a.LastViewProj,
		CameraPosition:   a.Camera.Position,
		FastCameraMotion: fastCameraMotion,
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
	a.Profiler.EndScope("Buffer Update")
	a.Profiler.SetCount("ShadowCasters", len(a.Scene.ShadowObjects))

	if recreated {
		// New buffers mean we need new bind groups
		a.BufferManager.CreateDebugBindGroups(a.DebugComputePipeline)
		a.BufferManager.CreateTiledLightCullBindGroups(a.TiledLightCullPipeline)

		// Also update G-Buffer and Lighting Bind Groups
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)

		// Shadow pass also depends on storage buffers (instances/nodes/sectors/bricks/etc),
		// so we must rebind shadow bind groups when buffers are recreated.
		a.BufferManager.CreateShadowBindGroups()

		// Transparent pass too
		if a.TransparentPipeline != nil {
			a.BufferManager.StorageView = a.StorageView
			a.BufferManager.CreateTransparentOverlayBindGroups(a.TransparentPipeline)
		}
		if a.CAVolumePipeline != nil {
			a.BufferManager.CreateCAVolumeRenderBindGroups(a.CAVolumePipeline)
		}
		if a.CAVolumeSimPipeline != nil {
			a.BufferManager.CreateCAVolumeSimBindGroups()
		}
		if a.CAVolumeBoundsPipeline != nil {
			a.BufferManager.CreateCAVolumeBoundsBindGroups()
		}

		// Gizmo BindGroup
		if a.GizmoPass != nil && a.BufferManager.CameraBuf != nil {
			var gErr error
			a.GizmoPass.BindGroup, gErr = a.GizmoPass.CreateBindGroup(a.BufferManager.CameraBuf)
			if gErr != nil {
				fmt.Printf("ERROR: Failed to recreate Gizmo BindGroup: %v\n", gErr)
			}
			// Recreate Depth BindGroup
			a.GizmoPass.DepthBindGroup, gErr = a.GizmoPass.CreateDepthBindGroup(a.BufferManager.DepthView)
			if gErr != nil {
				fmt.Printf("ERROR: Failed to recreate Gizmo Depth BindGroup: %v\n", gErr)
			}
		}
		a.BufferManager.CreateParticleSimBindGroups()
	}

	// Update Camera Uniforms
	a.BufferManager.UpdateCamera(viewProj, invView, invProj, a.Camera.Position, lightPos, a.Scene.AmbientLight, a.Scene.SkyAmbientMix, a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)), a.Config.Width, a.Config.Height, lightingQuality)
	a.BufferManager.EstimateTiledLightMetrics(a.Scene, viewProj, invView, a.Camera.Position)
	a.Profiler.SetCount("LightListEntriesAvg", a.BufferManager.TileLightAvgCount)
	a.Profiler.SetCount("LightListEntriesMax", a.BufferManager.TileLightMaxCount)
	if a.BufferManager.CAVolumeBindingsDirty {
		a.BufferManager.CreateCAVolumeSimBindGroups()
		if a.CAVolumeBoundsPipeline != nil {
			a.BufferManager.CreateCAVolumeBoundsBindGroups()
		}
		if a.CAVolumePipeline != nil {
			a.BufferManager.CreateCAVolumeRenderBindGroups(a.CAVolumePipeline)
		}
	}

	// Update Text Buffers if needed
	if len(a.TextItems) > 0 && a.TextRenderer != nil {
		vertices := a.TextRenderer.BuildVertices(a.TextItems, int(a.Config.Width), int(a.Config.Height))
		if len(vertices) > 0 {
			vSize := uint64(len(vertices) * int(unsafe.Sizeof(core.TextVertex{})))
			if a.TextVertexBuffer == nil || a.TextVertexBuffer.GetSize() < vSize {
				if a.TextVertexBuffer != nil {
					a.TextVertexBuffer.Release()
				}
				a.TextVertexBuffer, _ = a.Device.CreateBuffer(&wgpu.BufferDescriptor{
					Label: "Text VB",
					Size:  vSize,
					Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
				})
			}
			a.Queue.WriteBuffer(a.TextVertexBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), vSize))
			a.TextVertexCount = uint32(len(vertices))
		}
	}

	// Update Gizmos
	if a.GizmoPass != nil {
		a.GizmoPass.Update(a.Queue, a.Scene.Gizmos)
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
	nextTexture, err := a.Surface.GetCurrentTexture()
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

	// Particle Simulation
	a.Profiler.BeginScope("Particles Sim")
	a.BufferManager.DispatchParticleSim(encoder, a.ParticleInitPipeline, a.ParticleSimPipeline)
	a.BufferManager.DispatchParticleSpawn(encoder, a.ParticleSpawnPipeline, a.ParticleFinalizePipeline, a.ParticleSpawnCount)
	a.Profiler.EndScope("Particles Sim")

	a.Profiler.BeginScope("CA Sim")
	a.BufferManager.DispatchCAVolumeSim(encoder, a.CAVolumeSimPipeline)
	a.BufferManager.DispatchCAVolumeBounds(encoder, a.CAVolumeBoundsPipeline)
	a.Profiler.EndScope("CA Sim")

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

	// Shadow Pass
	a.Profiler.BeginScope("Shadows")
	shadowCameraMotion := a.hasShadowCameraMotion()
	shadowUpdates := a.BufferManager.BuildShadowUpdates(a.Scene, a.Camera, a.RenderFrameIndex, shadowCameraMotion)
	a.BufferManager.PrepareShadowLights(a.Scene, shadowUpdates)

	a.BufferManager.DispatchShadowPass(encoder, shadowUpdates)
	a.BufferManager.RecordShadowUpdates(shadowUpdates, a.RenderFrameIndex, a.Scene.StructureRevision)
	shadowSpotUpdates := 0
	shadowDirectionalUpdates := 0
	for _, update := range shadowUpdates {
		switch update.Kind {
		case core.ShadowUpdateKindDirectional:
			shadowDirectionalUpdates++
		case core.ShadowUpdateKindSpot:
			shadowSpotUpdates++
		}
	}
	a.Profiler.SetCount("ShadowUpdates", len(shadowUpdates))
	a.Profiler.SetCount("ShadowSpotUpdates", shadowSpotUpdates)
	a.Profiler.SetCount("ShadowDirectionalUpdates", shadowDirectionalUpdates)
	a.ShadowUpdateSummary = formatShadowUpdateSummary(shadowUpdates)
	a.Profiler.EndScope("Shadows")

	// Lighting Pass
	a.Profiler.BeginScope("Tile Light Cull")
	a.BufferManager.DispatchTiledLightCull(encoder, a.TiledLightCullPipeline)
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
	if a.CAVolumePipeline != nil {
		accPass.SetPipeline(a.CAVolumePipeline)
		if a.BufferManager.CAVolumeRenderBG0 != nil && a.BufferManager.CurrentCAVolumeRenderBG1() != nil && a.BufferManager.CAVolumeRenderBG2 != nil {
			accPass.SetBindGroup(0, a.BufferManager.CAVolumeRenderBG0, nil)
			accPass.SetBindGroup(1, a.BufferManager.CurrentCAVolumeRenderBG1(), nil)
			accPass.SetBindGroup(2, a.BufferManager.CAVolumeRenderBG2, nil)
			accPass.Draw(3, 1, 0, 0)
		}
	}
	if a.TransparentPipeline != nil {
		accPass.SetPipeline(a.TransparentPipeline)
		if a.BufferManager.TransparentBG0 != nil && a.BufferManager.TransparentBG1 != nil && a.BufferManager.TransparentBG2 != nil && a.BufferManager.TransparentBG3 != nil {
			accPass.SetBindGroup(0, a.BufferManager.TransparentBG0, nil)
			accPass.SetBindGroup(1, a.BufferManager.TransparentBG1, nil)
			accPass.SetBindGroup(2, a.BufferManager.TransparentBG2, nil)
			accPass.SetBindGroup(3, a.BufferManager.TransparentBG3, nil)
			accPass.Draw(3, 1, 0, 0)
		}
	}
	if a.ParticlesPipeline != nil {
		accPass.SetPipeline(a.ParticlesPipeline)
		if a.BufferManager.ParticlesBindGroup0 != nil && a.BufferManager.ParticlesBindGroup1 != nil {
			accPass.SetBindGroup(0, a.BufferManager.ParticlesBindGroup0, nil)
			accPass.SetBindGroup(1, a.BufferManager.ParticlesBindGroup1, nil)
			accPass.DrawIndirect(a.BufferManager.ParticleIndirectBuf, 0)
		}
	}
	if a.SpritesPipeline != nil && a.BufferManager.SpriteCount > 0 {
		accPass.SetPipeline(a.SpritesPipeline)
		if a.BufferManager.SpritesBindGroup1 != nil {
			accPass.SetBindGroup(1, a.BufferManager.SpritesBindGroup1, nil)
			for _, batch := range a.BufferManager.SpriteBatches {
				if batch.BindGroup0 == nil || batch.InstanceCount == 0 {
					continue
				}
				accPass.SetBindGroup(0, batch.BindGroup0, nil)
				accPass.Draw(6, batch.InstanceCount, 0, batch.FirstInstance)
			}
		}
	}
	err = accPass.End()
	if err != nil {
		fmt.Printf("ERROR: Accumulation pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Accumulation")

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
	// Text Pass
	if len(a.TextItems) > 0 && a.TextVertexBuffer != nil && a.TextPipeline != nil {
		rPass.SetPipeline(a.TextPipeline)
		rPass.SetBindGroup(0, a.TextBindGroup, nil)
		rPass.SetVertexBuffer(0, a.TextVertexBuffer, 0, a.TextVertexBuffer.GetSize())
		rPass.Draw(a.TextVertexCount, 1, 0, 0)
	}

	// Draw Gizmos
	if a.GizmoPass != nil && a.GizmoPass.BindGroup != nil {
		a.GizmoPass.Draw(rPass, a.GizmoPass.BindGroup, a.GizmoPass.DepthBindGroup)
	}

	err = rPass.End()
	if err != nil {
		fmt.Printf("ERROR: Resolve/Gizmo pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Resolve")

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
	a.recordCameraState()
	a.RenderFrameIndex++
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
