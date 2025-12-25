package app

import (
	"fmt"

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

	ComputePipeline      *wgpu.ComputePipeline
	DebugComputePipeline *wgpu.ComputePipeline

	RenderPipeline *wgpu.RenderPipeline

	// Deferred Rendering Pipelines
	GBufferPipeline  *wgpu.ComputePipeline
	LightingPipeline *wgpu.ComputePipeline

	StorageTexture *wgpu.Texture
	StorageView    *wgpu.TextureView
	Sampler        *wgpu.Sampler

	BindGroup1      *wgpu.BindGroup // Output texture
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

	AmbientLight   [3]float32
	LastTime       float64
	LastRenderTime float64
	MouseCaptured  bool
	DebugMode      bool

	FrameCount int
	FPS        float64
	FPSTime    float64
}

func NewApp(window *glfw.Window) *App {
	return &App{
		Window: window,
		Camera: core.NewCameraState(),
		Scene:  core.NewScene(),
		Editor: editor.NewEditor(),
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
	csModule, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Raytrace CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.RaytraceWGSL},
	})

	fsModule, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Fullscreen VS/FS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.FullscreenWGSL},
	})

	// Compute Pipeline
	// Layout auto
	a.ComputePipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "Raytrace Pipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     csModule,
			EntryPoint: "main",
		},
	})
	if err != nil {
		return err
	}

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
	lightMod, _ := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Lighting CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.DeferredLightingWGSL},
	})
	a.LightingPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "Lighting Pipeline",
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
	fontPath := "/Users/ddevidch/code/go/gekko3d/actiongame/assets/Roboto-Medium.ttf"
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
	a.BufferManager.UpdateCamera(view, proj, invView, invProj, a.Camera.Position, mgl32.Vec3{10, 20, 10}, a.AmbientLight, a.Camera.DebugMode)

	// Ensure scene buffers are created (even if empty) before bind groups
	a.BufferManager.UpdateScene(a.Scene)

	// Bind groups creation
	a.setupBindGroups()
	a.BufferManager.CreateBindGroups(a.ComputePipeline)
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

	// Initialize time
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

	// Bind Group 1 (Output) for compute shader
	a.BindGroup1, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.ComputePipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.StorageView},
		},
	})
	if err != nil {
		panic(err)
	}

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
	}
}

func (a *App) Update() {
	if a.DebugMode {
		a.DrawText(fmt.Sprintf("Renderer FPS: %.1f", a.FPS), 10, 10, 1.0, [4]float32{1, 1, 0, 1})
	}

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

	// Combine
	viewProj := proj.Mul4(view)
	invView := view.Inv()
	invProj := proj.Inv()

	// Commit scene changes from ECS sync
	a.Scene.Commit()

	// Update Buffers
	recreated := a.BufferManager.UpdateScene(a.Scene)
	if recreated {
		// New buffers mean we need new bind groups
		a.BufferManager.CreateBindGroups(a.ComputePipeline)
		a.BufferManager.CreateDebugBindGroups(a.DebugComputePipeline)

		// Also update G-Buffer and Lighting Bind Groups
		a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
		a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
	}

	// Update Camera Uniforms
	a.BufferManager.UpdateCamera(viewProj, proj, invView, invProj, a.Camera.Position, lightPos, a.AmbientLight, a.Camera.DebugMode)

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

	// Shadow Pass
	a.BufferManager.DispatchShadowPass(encoder, uint32(len(a.Scene.Lights)))

	// Lighting Pass
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

	// Render Pass (Blit)
	rPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{0, 0, 0, 1},
		}},
	})
	rPass.SetPipeline(a.RenderPipeline)
	rPass.SetBindGroup(0, a.RenderBG, nil)
	rPass.Draw(3, 1, 0, 0)

	// Text Pass
	if len(a.TextItems) > 0 && a.TextVertexBuffer != nil && a.TextPipeline != nil {
		rPass.SetPipeline(a.TextPipeline)
		rPass.SetBindGroup(0, a.TextBindGroup, nil)
		rPass.SetVertexBuffer(0, a.TextVertexBuffer, 0, a.TextVertexBuffer.GetSize())
		rPass.Draw(a.TextVertexCount, 1, 0, 0)
	}

	err = rPass.End()
	if err != nil {
		fmt.Printf("ERROR: Render pass End failed: %v\n", err)
	}

	cmd, err := encoder.Finish(nil)
	if err != nil {
		fmt.Printf("ERROR: Encoder Finish failed: %v\n", err)
		return
	}
	a.Queue.Submit(cmd)
	a.Surface.Present()

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

func (a *App) HandleClick(button int, action int) {
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
		a.Scene.Commit()
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
