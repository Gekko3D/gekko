package app

import (
	"fmt"
	"os"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	_ "image/png"

	"github.com/cogentcore/webgpu/wgpu"
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

	// Particle Sim Pipelines
	ParticleSimPipeline      *wgpu.ComputePipeline
	ParticleSpawnPipeline    *wgpu.ComputePipeline
	ParticleInitPipeline     *wgpu.ComputePipeline
	ParticleFinalizePipeline *wgpu.ComputePipeline

	StorageTexture *wgpu.Texture
	StorageView    *wgpu.TextureView
	Sampler        *wgpu.Sampler

	BindGroup1Debug *wgpu.BindGroup // Output texture for debug
	RenderBG        *wgpu.BindGroup // Blit

	BufferManager *gpu.GpuBufferManager
	Scene         *core.Scene
	Camera        *core.CameraState

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

	ParticleSpawnCount uint32
	ParticleAtlasData  []byte // If set before Init, uses this instead of embedded
}

func NewApp(window *glfw.Window) *App {
	return &App{
		Window:   window,
		Camera:   core.NewCameraState(),
		Scene:    core.NewScene(),
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
	a.BufferManager = gpu.NewGpuBufferManager(a.Device)

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
			// Output Color (HDR)
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageCompute,
				StorageTexture: wgpu.StorageTextureBindingLayout{
					Access:        wgpu.StorageTextureAccessWriteOnly,
					Format:        wgpu.TextureFormatRGBA16Float,
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
			// Skybox Texture (RGBA8Unorm)
			{
				Binding:    6,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Skybox Sampler
			{
				Binding:    7,
				Visibility: wgpu.ShaderStageCompute,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
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

	// Particle Simulation Pipelines
	simMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Particle Sim CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.ParticlesSimWGSL},
	})
	if err != nil {
		return err
	}

	a.BufferManager.UpdateParticles(1000000, nil)
	a.BufferManager.CreateDefaultParticleAtlas()

	a.createParticleSimPipelines(simMod)

	// Skybox Generation Pipeline
	a.BufferManager.CreateSkyboxGenPipeline(shaders.SkyboxWGSL)

	// G-Buffer resources
	a.BufferManager.CreateGBufferTextures(uint32(width), uint32(height))
	a.BufferManager.CreateGBufferBindGroups(a.GBufferPipeline, a.LightingPipeline)
	a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
	a.BufferManager.CreateShadowBindGroups()

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
			var gErr error
			a.GizmoPass.BindGroup, gErr = a.GizmoPass.CreateBindGroup(a.BufferManager.CameraBuf)
			if gErr != nil {
				fmt.Printf("ERROR: Failed to create Gizmo BindGroup: %v\n", gErr)
			}
			// Create Depth BindGroup
			a.GizmoPass.DepthBindGroup, gErr = a.GizmoPass.CreateDepthBindGroup(a.BufferManager.DepthView)
			if gErr != nil {
				fmt.Printf("ERROR: Failed to create Gizmo Depth BindGroup: %v\n", gErr)
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
