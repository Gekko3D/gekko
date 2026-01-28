package app

import (
	"fmt"
	"os"
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/editor"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

type App struct {
	Window   *glfw.Window
	Instance *wgpu.Instance
	Adapter  *wgpu.Adapter
	Device   *wgpu.Device
	Queue    *wgpu.Queue
	Surface  *wgpu.Surface
	Config   *wgpu.SurfaceConfiguration

	DebugComputePipeline *wgpu.ComputePipeline

	RenderPipeline      *wgpu.RenderPipeline
	ParticlesPipeline   *wgpu.RenderPipeline
	TransparentPipeline *wgpu.RenderPipeline
	ResolvePipeline     *wgpu.RenderPipeline

	ResolveBG *wgpu.BindGroup

	// Deferred Rendering Pipelines
	GBufferPipeline  *wgpu.ComputePipeline
	LightingPipeline *wgpu.ComputePipeline

	StorageTexture *wgpu.Texture
	StorageView    *wgpu.TextureView
	Sampler        *wgpu.Sampler

	BindGroup1Debug *wgpu.BindGroup // Output texture for debug
	RenderBG        *wgpu.BindGroup // Blit

	BufferManager *gpu.GpuBufferManager
	Scene         *core.Scene
	Camera        *core.CameraState
	Editor        *editor.Editor

	TextRenderer     *core.TextRenderer
	TextPipeline     *wgpu.RenderPipeline
	TextAtlasView    *wgpu.TextureView
	TextBindGroup    *wgpu.BindGroup
	TextVertexBuffer *wgpu.Buffer
	TextItems        []core.TextItem
	TextVertexCount  uint32

	GizmoPass *gpu.GizmoRenderPass

	LastViewProj   mgl32.Mat4
	LastTime       float64
	LastRenderTime float64
	MouseCaptured  bool
	MouseX, MouseY float64
	DebugMode      bool
	RenderMode     uint32
	FontPath       string

	FrameCount         int
	FPS                float64
	FPSTime            float64
	ShadowUpdateOffset int

	Profiler *Profiler
}

func NewApp(window *glfw.Window) *App {
	return &App{
		Window:   window,
		Camera:   core.NewCameraState(),
		Scene:    core.NewScene(),
		Editor:   editor.NewEditor(),
		Profiler: NewProfiler(),
	}
}

func (a *App) Init() error {
	// WebGPU Init
	a.Instance = wgpu.CreateInstance(nil)

	surface := a.Instance.CreateSurface(GetSurfaceDescriptor(a.Window))
	a.Surface = surface

	adapter, err := a.Instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
		PowerPreference:   wgpu.PowerPreferenceHighPerformance,
	})
	if err != nil {
		return err
	}
	a.Adapter = adapter

	a.Device, err = adapter.RequestDevice(nil)
	if err != nil {
		return err
	}
	a.Queue = a.Device.GetQueue()

	// Config
	width, height := a.Window.GetFramebufferSize()
	caps := surface.GetCapabilities(adapter)
	format := caps.Formats[0]

	a.Config = &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      format,
		Width:       uint32(width),
		Height:      uint32(height),
		PresentMode: wgpu.PresentModeFifo,
		AlphaMode:   caps.AlphaModes[0],
	}
	surface.Configure(adapter, a.Device, a.Config)

	// Shaders
	fsModule, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Fullscreen VS/FS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.FullscreenWGSL},
	})

	// Debug Compute Pipeline
	debugModule, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Debug CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.DebugWGSL},
	})
	a.DebugComputePipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "Debug Pipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     debugModule,
			EntryPoint: "main",
		},
	})
	if err != nil {
		return err
	}

	// G-Buffer Pipeline
	gbMod, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "GBuffer CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.GBufferWGSL},
	})
	a.GBufferPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "GBuffer Pipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     gbMod,
			EntryPoint: "main",
		},
	})
	if err != nil {
		return err
	}

	// Deferred Lighting Pipeline
	lightMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Lighting CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.DeferredLightingWGSL},
	})
	if err != nil {
		return err
	}

	// Group 0: Camera + Lights
	lightBGL0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Lighting BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   256, // CameraData size
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0, // Runtime array
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	// Group 1: G-Buffer Input + Output + Shadow
	lightBGL1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Lighting BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			// Depth (RGBA32Float - Sampled texture)
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Normal (RGBA16Float - Sampled texture)
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Material (RGBA32Float - Sampled texture)
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Position (RGBA32Float - Sampled texture)
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Output Color
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageCompute,
				StorageTexture: wgpu.StorageTextureBindingLayout{
					Access:        wgpu.StorageTextureAccessWriteOnly,
					Format:        wgpu.TextureFormatRGBA8Unorm,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Shadow Maps (RGBA32Float Array)
			{
				Binding:    5,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2DArray,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	// Group 2: Materials
	lightBGL2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Lighting BGL2",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    3, // Matches mod_vox_rt binding
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	lightPipelineLayout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{lightBGL0, lightBGL1, lightBGL2},
	})
	if err != nil {
		return err
	}

	a.LightingPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Lighting Pipeline",
		Layout: lightPipelineLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     lightMod,
			EntryPoint: "main",
		},
	})
	if err != nil {
		return err
	}

	// Render Pipeline
	a.RenderPipeline, err = a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label: "Blit Pipeline",
		Vertex: wgpu.VertexState{
			Module:     fsModule,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     fsModule,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    format,
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		return err
	}

	//Resources
	a.BufferManager = gpu.NewGpuBufferManager(a.Device)
	var samplerErr error
	a.Sampler, samplerErr = a.Device.CreateSampler(&wgpu.SamplerDescriptor{
		MinFilter:     wgpu.FilterModeLinear,
		MagFilter:     wgpu.FilterModeLinear,
		MaxAnisotropy: 1,
	})
	if samplerErr != nil {
		panic(samplerErr)
	}

	// Text Rendering Setup
	fontPath := a.FontPath
	if fontPath == "" {
		// Try multiple potential locations relative to working directory
		candidates := []string{
			"gekko/voxelrt/rt/fonts/Roboto-Medium.ttf",    // Root
			"../gekko/voxelrt/rt/fonts/Roboto-Medium.ttf", // From subfolders like actiongame
			"assets/Roboto-Medium.ttf",                    // Local assets
		}

		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				fontPath = c
				break
			}
		}

		if fontPath == "" {
			fontPath = "Roboto-Medium.ttf" // Final fallback
		}
	}
	a.TextRenderer, err = core.NewTextRenderer(fontPath, 32)
	if err != nil {
		fmt.Printf("WARNING: Failed to initialize text renderer: %v\n", err)
	} else {
		a.setupTextResources()
	}

	a.setupTextures(width, height)

	// Default Camera Setup
	view := mgl32.Ident4()
	proj := mgl32.Ident4()
	invView := mgl32.Ident4()
	invProj := mgl32.Ident4()
	a.BufferManager.UpdateCamera(view, proj, invView, invProj, a.Camera.Position, mgl32.Vec3{10, 20, 10}, a.Scene.AmbientLight, a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)))

	// Ensure scene buffers are created (even if empty) before bind groups
	a.BufferManager.UpdateScene(a.Scene)

	// Bind groups creation
	a.setupBindGroups()
	a.BufferManager.CreateDebugBindGroups(a.DebugComputePipeline)

	// Shadow Pipeline
	err = a.BufferManager.CreateShadowPipeline(shaders.ShadowMapWGSL)
	if err != nil {
		return err
	}

	// G-Buffer resources
	a.BufferManager.CreateGBufferTextures(uint32(width), uint32(height))
	a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
	a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
	a.BufferManager.CreateShadowBindGroups()

	// Initialize particle instance buffer with a minimal allocation
	a.BufferManager.UpdateParticles([]core.ParticleInstance{})
	// Create particle render pipeline (accumulates into WBOIT targets)
	a.setupParticlesPipeline()
	// Create transparent overlay pipeline (accumulate into WBOIT targets)
	a.setupTransparentOverlayPipeline()
	// Create resolve pipeline to composite opaque + transparent accum onto swapchain
	a.setupResolvePipeline()

	// Gizmo Pipeline
	var gizmoErr error
	a.GizmoPass, gizmoErr = gpu.NewGizmoRenderPass(a.Device, format)
	if gizmoErr != nil {
		fmt.Printf("ERROR: Failed to create Gizmo pass: %v\n", gizmoErr)
	} else {
		// Create specific BindGroup for Gizmos
		// We need to access CameraBuf from BufferManager
		if a.BufferManager != nil {
			a.GizmoPass.BindGroup, err = a.GizmoPass.CreateBindGroup(a.BufferManager.CameraBuf)
			if err != nil {
				fmt.Printf("ERROR: Failed to create Gizmo BindGroup: %v\n", err)
			}
		}
	}

	// Initialize Hi-Z Occlusion
	hizMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "Hi-Z Shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaders.HiZWGSL,
		},
	})
	if err == nil {
		a.BufferManager.SetupHiZ(a.Config.Width, a.Config.Height, hizMod)
	} else {
		fmt.Printf("ERROR: Failed to create Hi-Z shader module: %v\n", err)
	}

	a.LastViewProj = mgl32.Ident4()
	a.LastTime = glfw.GetTime()

	return nil
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
		Format:        wgpu.TextureFormatRGBA8Unorm,
		Usage:         wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding,
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
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
		// Ensure shadow bind groups remain valid after any resource changes
		a.BufferManager.CreateShadowBindGroups()

		// Recreate particle pipeline for new swapchain format
		a.setupParticlesPipeline()
		// Recreate resolve pipeline/bind group (depends on textures/swapchain)
		a.setupResolvePipeline()
	}
}

func (a *App) Update() {
	// Gather stats
	a.Profiler.SetCount("Objects", len(a.Scene.Objects))
	a.Profiler.SetCount("Visible", len(a.Scene.VisibleObjects))
	a.Profiler.SetCount("Lights", len(a.Scene.Lights))
	a.Profiler.SetCount("Particles", int(a.BufferManager.ParticleCount))

	if a.DebugMode {
		stats := fmt.Sprintf("FPS: %.1f\n%s", a.FPS, a.Profiler.GetStatsString())
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
	proj := mgl32.Perspective(mgl32.DegToRad(60), aspect, 0.1, 1000.0)

	// Update Editor (Batched scaling)
	a.Editor.Update(a.Scene, glfw.GetTime())

	// Combined
	viewProj := proj.Mul4(view)
	invView := view.Inv()
	invProj := proj.Inv()

	// Readback Hi-Z from previous frame (cheap latency)
	hizData, hizW, hizH := a.BufferManager.ReadbackHiZ()

	// Commit scene changes from ECS sync
	a.Profiler.BeginScope("Scene Commit")
	planes := a.Camera.ExtractFrustum(viewProj)
	a.Scene.Commit(planes, hizData, hizW, hizH, a.LastViewProj)
	a.Profiler.EndScope("Scene Commit")

	// Store current view-proj for next frame's Hi-Z reprojection
	a.LastViewProj = viewProj

	// Update Buffers
	a.Profiler.BeginScope("Buffer Update")
	recreated := a.BufferManager.UpdateScene(a.Scene)
	a.Profiler.EndScope("Buffer Update")

	if recreated {
		// New buffers mean we need new bind groups
		a.BufferManager.CreateDebugBindGroups(a.DebugComputePipeline)

		// Also update G-Buffer and Lighting Bind Groups
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)

		// Shadow pass also depends on storage buffers (instances/nodes/sectors/bricks/etc),
		// so we must rebind shadow bind groups when buffers are recreated.
		a.BufferManager.CreateShadowBindGroups()

		// Transparent pass too
		if a.TransparentPipeline != nil {
			a.BufferManager.CreateTransparentOverlayBindGroups(a.TransparentPipeline)
		}

		// Gizmo BindGroup
		if a.GizmoPass != nil && a.BufferManager.CameraBuf != nil {
			var gErr error
			a.GizmoPass.BindGroup, gErr = a.GizmoPass.CreateBindGroup(a.BufferManager.CameraBuf)
			if gErr != nil {
				fmt.Printf("ERROR: Failed to recreate Gizmo BindGroup: %v\n", gErr)
			}
		}
	}

	// Update Camera Uniforms
	a.BufferManager.UpdateCamera(viewProj, proj, invView, invProj, a.Camera.Position, lightPos, a.Scene.AmbientLight, a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)))

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

	var shadowIndices []uint32
	if len(a.Scene.Lights) > 0 {
		type lightInfo struct {
			Index int
			Dist  float32
		}

		sortedLights := make([]lightInfo, 0, len(a.Scene.Lights))
		camPos := a.Camera.Position

		shadowIndices = append(shadowIndices, 0) // Light 0 (Sun) always

		for i := 1; i < len(a.Scene.Lights); i++ {
			l := a.Scene.Lights[i]
			d := float32(0.0)
			if l.Params[2] != 1.0 { // Not directional
				p := mgl32.Vec3{l.Position[0], l.Position[1], l.Position[2]}
				d = p.Sub(camPos).Len()
			}
			sortedLights = append(sortedLights, lightInfo{i, d})
		}

		sort.Slice(sortedLights, func(i, j int) bool {
			return sortedLights[i].Dist < sortedLights[j].Dist
		})

		numPrioritized := 4
		updatesPerFrame := 2

		for i := 0; i < len(sortedLights) && i < numPrioritized; i++ {
			shadowIndices = append(shadowIndices, uint32(sortedLights[i].Index))
		}

		remainingStart := numPrioritized
		if remainingStart < len(sortedLights) {
			remainingCount := len(sortedLights) - remainingStart
			for i := 0; i < updatesPerFrame; i++ {
				offset := (a.ShadowUpdateOffset + i) % remainingCount
				idx := sortedLights[remainingStart+offset].Index
				shadowIndices = append(shadowIndices, uint32(idx))
			}
			a.ShadowUpdateOffset = (a.ShadowUpdateOffset + updatesPerFrame) % remainingCount
		}
	}

	a.BufferManager.DispatchShadowPass(encoder, shadowIndices)
	a.Profiler.EndScope("Shadows")

	// Lighting Pass
	a.Profiler.BeginScope("Lighting")
	lPass := encoder.BeginComputePass(nil)
	lPass.SetPipeline(a.LightingPipeline)
	lPass.SetBindGroup(0, a.BufferManager.LightingBindGroup, nil)
	lPass.SetBindGroup(1, a.BufferManager.LightingBindGroup2, nil)
	lPass.SetBindGroup(2, a.BufferManager.LightingBindGroupMaterial, nil) // For materials/sectors
	lPass.DispatchWorkgroups(wgX, wgY, 1)
	err = lPass.End()
	if err != nil {
		fmt.Printf("ERROR: Lighting pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Lighting")

	// Debug Pass
	if a.DebugMode {
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
				ClearValue: wgpu.Color{0, 0, 0, 0},
			},
			{
				View:       a.BufferManager.TransparentWeightView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{0, 0, 0, 0},
			},
		},
	})
	if a.TransparentPipeline != nil {
		accPass.SetPipeline(a.TransparentPipeline)
		if a.BufferManager.TransparentBG0 != nil && a.BufferManager.TransparentBG1 != nil && a.BufferManager.TransparentBG2 != nil {
			accPass.SetBindGroup(0, a.BufferManager.TransparentBG0, nil)
			accPass.SetBindGroup(1, a.BufferManager.TransparentBG1, nil)
			accPass.SetBindGroup(2, a.BufferManager.TransparentBG2, nil)
			accPass.Draw(3, 1, 0, 0)
		}
	}
	if a.ParticlesPipeline != nil && a.BufferManager.ParticleCount > 0 {
		accPass.SetPipeline(a.ParticlesPipeline)
		if a.BufferManager.ParticlesBindGroup0 != nil && a.BufferManager.ParticlesBindGroup1 != nil {
			accPass.SetBindGroup(0, a.BufferManager.ParticlesBindGroup0, nil)
			accPass.SetBindGroup(1, a.BufferManager.ParticlesBindGroup1, nil)
			accPass.Draw(6, a.BufferManager.ParticleCount, 0, 0)
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
			ClearValue: wgpu.Color{0, 0, 0, 1},
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
		a.GizmoPass.Draw(rPass, a.GizmoPass.BindGroup)
	}

	err = rPass.End()
	if err != nil {
		fmt.Printf("ERROR: Resolve/Gizmo pass End failed: %v\n", err)
	}
	a.Profiler.EndScope("Resolve")

	cmd, err := encoder.Finish(nil)
	if err != nil {
		fmt.Printf("ERROR: Encoder Finish failed: %v\n", err)
		return
	}
	a.Queue.Submit(cmd)
	a.BufferManager.ResolveHiZReadback()
	a.Surface.Present()
	a.Device.Poll(false, nil)

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
}

/*
*

	setupParticlesPipeline creates the additive billboard particle pipeline targeting the swapchain format.
*/
func (a *App) setupParticlesPipeline() {
	// Build shader module
	partMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Particles Billboard",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.ParticlesBillboardWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particle shader module: %v\n", err)
		return
	}

	// Explicit bind group layouts to allow sampling unfilterable float (RGBA32Float) GBuffer depth
	// Group 0: camera (uniform) + instances (storage read)
	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Particles BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   256, // CameraData size
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particles BGL0: %v\n", err)
		return
	}

	// Group 1: GBuffer depth (RGBA32Float), must be sampled as UnfilterableFloat
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Particles BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particles BGL1: %v\n", err)
		return
	}

	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particles pipeline layout: %v\n", err)
		return
	}

	// Create render pipeline
	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Particles Pipeline",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     partMod,
			EntryPoint: "vs_main",
			Buffers:    nil, // no vertex buffers; VS expands a unit quad per-instance
		},
		Fragment: &wgpu.FragmentState{
			Module:     partMod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format: wgpu.TextureFormatRGBA16Float,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
						Alpha: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
					},
					WriteMask: wgpu.ColorWriteMaskAll,
				},
				{
					Format: wgpu.TextureFormatR16Float,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
						Alpha: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
					},
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create particle render pipeline: %v\n", err)
		return
	}
	a.ParticlesPipeline = pipeline
}

// setupTransparentOverlayPipeline creates a fullscreen render pipeline to alpha-blend
// a single transparent voxel surface per pixel over the lit image.
func (a *App) setupTransparentOverlayPipeline() {
	overlayMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Transparent Overlay",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.TransparentOverlayWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create transparent overlay shader module: %v\n", err)
		return
	}

	// Group 0: camera (uniform) + instances (storage) + BVH nodes (storage) + lights (storage)
	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "TransparentOverlay BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   256,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay BGL0: %v\n", err)
		return
	}

	// Group 1: voxel data (sector, brick, payload, object params, tree, sector grid)
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "TransparentOverlay BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{ // SectorTable
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // BrickTable
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // VoxelPayload
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUint,
					ViewDimension: wgpu.TextureViewDimension3D,
				},
			},
			{ // Materials (packed vec4 table; transparency in pbr_params.w)
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // ObjectParams
				Binding:    4,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // Tree64
				Binding:    5,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // SectorGrid
				Binding:    6,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // SectorGridParams
				Binding:    7,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay BGL1: %v\n", err)
		return
	}

	// Group 2: GBuffer inputs (Depth RGBA32F, Material RGBA32F)
	bgl2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "TransparentOverlay BGL2",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay BGL2: %v\n", err)
		return
	}

	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay pipeline layout: %v\n", err)
		return
	}

	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Transparent Overlay Pipeline",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     overlayMod,
			EntryPoint: "vs_main",
			Buffers:    nil, // fullscreen triangle from vertex_id
		},
		Fragment: &wgpu.FragmentState{
			Module:     overlayMod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format: wgpu.TextureFormatRGBA16Float,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
						Alpha: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
					},
					WriteMask: wgpu.ColorWriteMaskAll,
				},
				{
					Format: wgpu.TextureFormatR16Float,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
						Alpha: wgpu.BlendComponent{
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOne,
							Operation: wgpu.BlendOperationAdd,
						},
					},
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create transparent overlay pipeline: %v\n", err)
		return
	}
	a.TransparentPipeline = pipeline
}

// setupResolvePipeline creates a fullscreen resolve pass that composites the opaque lit
// color (StorageTexture) with the accumulated transparent color/weight textures onto the swapchain.
func (a *App) setupResolvePipeline() {
	// Build shader module
	resMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Resolve Transparency",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.ResolveTransparencyWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve shader module: %v\n", err)
		return
	}

	// Group 0: opaque lit color, accum color, weight, sampler
	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Resolve BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat, // RGBA8Unorm
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat, // RGBA16Float
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat, // R16Float
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve BGL0: %v\n", err)
		return
	}

	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve pipeline layout: %v\n", err)
		return
	}

	// Render pipeline to swapchain
	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Resolve Pipeline",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     resMod,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     resMod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    a.Config.Format,
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve render pipeline: %v\n", err)
		return
	}
	a.ResolvePipeline = pipeline

	// Create resolve bind group (opaque lit + accum + weight + sampler)
	if a.StorageView == nil || a.BufferManager.TransparentAccumView == nil || a.BufferManager.TransparentWeightView == nil {
		// Views not ready yet (e.g., during early init/resize), skip creating BG
		return
	}
	a.ResolveBG, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: bgl0,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.StorageView},
			{Binding: 1, TextureView: a.BufferManager.TransparentAccumView},
			{Binding: 2, TextureView: a.BufferManager.TransparentWeightView},
			{Binding: 3, Sampler: a.Sampler},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve bind group: %v\n", err)
		return
	}
}

func (a *App) HandleClick(button int, action int) {
	if action == int(glfw.Press) && button == int(glfw.MouseButtonRight) {
		fmt.Printf("Right Click Detected. MouseCaptured: %v\n", a.MouseCaptured)
		if !a.MouseCaptured {
			// Try to select object
			fmt.Printf("Attempting Selection at %f, %f\n", a.MouseX, a.MouseY)
			w, h := a.Window.GetSize()
			ray := a.Editor.GetPickRay(a.MouseX, a.MouseY, w, h, a.Camera)
			fmt.Printf("Ray: Origin=%v Dir=%v\n", ray.Origin, ray.Direction)
			a.Editor.Select(a.Scene, ray)
			if a.Editor.SelectedObject != nil {
				fmt.Printf("Selected Object: %v\n", a.Editor.SelectedObject)
			} else {
				fmt.Println("Selection Missed")
			}
			return
		}
	}

	if a.MouseCaptured || action != int(glfw.Press) {
		return
	}

	x, y := a.Window.GetCursorPos()
	w, h := a.Window.GetSize()

	ray := a.Editor.GetPickRay(x, y, w, h, a.Camera)
	hit := a.Editor.Pick(a.Scene, ray)

	if hit != nil {
		// COW
		sharingCount := 0
		for _, obj := range a.Scene.Objects {
			if obj.XBrickMap == hit.Object.XBrickMap {
				sharingCount++
			}
		}

		if sharingCount > 1 {
			hit.Object.XBrickMap = hit.Object.XBrickMap.Copy()
		}

		// Apply brush
		oldVal := a.Editor.BrushValue
		if button == int(glfw.MouseButtonRight) {
			a.Editor.BrushValue = 0
		}
		a.Editor.ApplyBrush(hit.Object, hit.Coord, hit.Normal)
		a.Editor.BrushValue = oldVal

		// Mark scene as dirty - Update() will handle buffer sync
		a.Scene.Commit([6]mgl32.Vec4{}, nil, 0, 0, mgl32.Ident4())
		// DO NOT call UpdateScene or CreateBindGroups here!
		// This causes race condition with the render loop.
		// The Update() method will handle it on the next frame.
	}
}

func GetSurfaceDescriptor(w *glfw.Window) *wgpu.SurfaceDescriptor {
	return wgpuglfw.GetSurfaceDescriptor(w)
}

func (a *App) setupTextResources() {
	// Texture
	tr := a.TextRenderer
	w, h := tr.AtlasImage.Bounds().Dx(), tr.AtlasImage.Bounds().Dy()
	tex, err := a.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Text Atlas",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		Format:        wgpu.TextureFormatR8Unorm,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		Dimension:     wgpu.TextureDimension2D,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		panic(err)
	}
	a.Queue.WriteTexture(tex.AsImageCopy(), tr.AtlasImage.Pix, &wgpu.TextureDataLayout{
		Offset:       0,
		BytesPerRow:  uint32(w),
		RowsPerImage: uint32(h),
	}, &wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1})

	a.TextAtlasView, _ = tex.CreateView(nil)

	// Pipeline
	textMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Text Shader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.TextWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create text shader module: %v\n", err)
		return
	}

	a.TextPipeline, err = a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label: "Text Pipeline",
		Vertex: wgpu.VertexState{
			Module:     textMod,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(core.TextVertex{})),
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x2, Offset: 8, ShaderLocation: 1},
					{Format: wgpu.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 2},
				},
			}},
		},
		Fragment: &wgpu.FragmentState{
			Module:     textMod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: a.Config.Format,
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorSrcAlpha,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						Operation: wgpu.BlendOperationAdd,
					},
					Alpha: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOne,
						Operation: wgpu.BlendOperationAdd,
					},
				},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create text render pipeline: %v\n", err)
		return
	}

	a.TextBindGroup, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.TextPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.TextAtlasView},
			{Binding: 1, Sampler: a.Sampler},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create text bind group: %v\n", err)
		return
	}
}
