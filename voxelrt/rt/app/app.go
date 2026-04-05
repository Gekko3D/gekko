package app

import (
	"fmt"

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

	RenderPipeline         *wgpu.RenderPipeline
	ParticlesPipeline      *wgpu.RenderPipeline
	SpritesPipeline        *wgpu.RenderPipeline
	TransparentPipeline    *wgpu.RenderPipeline
	CAVolumePipeline       *wgpu.RenderPipeline
	AnalyticMediumPipeline *wgpu.RenderPipeline
	ResolvePipeline        *wgpu.RenderPipeline

	ResolveBG *wgpu.BindGroup

	// Deferred Rendering Pipelines
	GBufferPipeline        *wgpu.ComputePipeline
	TiledLightCullPipeline *wgpu.ComputePipeline
	LightingPipeline       *wgpu.ComputePipeline

	// Particle Sim Pipelines
	ParticleSimPipeline      *wgpu.ComputePipeline
	ParticleSpawnPipeline    *wgpu.ComputePipeline
	ParticleInitPipeline     *wgpu.ComputePipeline
	ParticleFinalizePipeline *wgpu.ComputePipeline
	CAVolumeSimPipeline      *wgpu.ComputePipeline
	CAVolumeBoundsPipeline   *wgpu.ComputePipeline

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

	LastViewProj       mgl32.Mat4
	LastTime           float64
	LastRenderTime     float64
	LastCameraPos      mgl32.Vec3
	LastCameraYaw      float32
	LastCameraPitch    float32
	HasLastCameraState bool
	MouseCaptured      bool
	MouseX, MouseY     float64
	DebugMode          bool
	RenderMode         uint32
	QualityPreset      core.LightingQualityPreset
	LightingQuality    core.LightingQualityConfig
	OcclusionMode      core.OcclusionMode
	FontPath           string
	UIFontSize         float64

	FrameCount            int
	FPS                   float64
	FPSTime               float64
	RenderFrameIndex      uint64
	ShadowUpdateOffset    int
	ShadowUpdateSummary   string
	HadAccumulationPass   bool
	HadCAVolumePass       bool
	PreviousProfilerStats string

	Profiler *core.Profiler

	ParticleSpawnCount uint32
	ParticleAtlasData  []byte // If set before Init, uses this instead of embedded
	FeatureConfig      AppFeatureConfig

	features                  []Feature
	defaultFeaturesRegistered bool
}

const DefaultUIFontSize = 26.0

type AppFeatureFlags struct {
	Text          bool
	Gizmos        bool
	Skybox        bool
	CAVolumes     bool
	AnalyticMedia bool
	Transparency  bool
	Particles     bool
	Sprites       bool
}

type AppFeatureConfig struct {
	// AutoRegisterDefaults controls whether built-in optional features are auto-registered.
	// When false, the app starts with no default features and callers can register their own.
	AutoRegisterDefaults bool
	Defaults             AppFeatureFlags
}

func DefaultFeatureFlags() AppFeatureFlags {
	return AppFeatureFlags{
		Text:          true,
		Gizmos:        true,
		Skybox:        true,
		CAVolumes:     true,
		AnalyticMedia: true,
		Transparency:  true,
		Particles:     true,
		Sprites:       true,
	}
}

func DefaultFeatureConfig() AppFeatureConfig {
	return AppFeatureConfig{
		AutoRegisterDefaults: true,
		Defaults:             DefaultFeatureFlags(),
	}
}

func NewApp(window *glfw.Window) *App {
	app := &App{
		Window:          window,
		Camera:          core.NewCameraState(),
		Scene:           core.NewScene(),
		Profiler:        core.NewProfiler(),
		QualityPreset:   core.LightingQualityPresetBalanced,
		LightingQuality: core.DefaultLightingQualityConfig(),
		OcclusionMode:   core.OcclusionOff,
		FeatureConfig:   DefaultFeatureConfig(),
	}
	return app
}

func (a *App) EffectiveLightingQuality() core.LightingQualityConfig {
	cfg := a.LightingQuality
	if a.QualityPreset != "" {
		cfg.Preset = a.QualityPreset
	}
	return cfg.WithDefaults()
}

func (a *App) effectiveUIFontSize() float64 {
	if a.UIFontSize > 0 {
		return a.UIFontSize
	}
	return DefaultUIFontSize
}

func (a *App) Init() error {
	a.ensureDefaultFeatures()

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
	a.BufferManager = gpu.NewGpuBufferManager(a.Device, a.Profiler)

	// Config
	width, height := a.Window.GetFramebufferSize()
	caps := surface.GetCapabilities(adapter)
	format := caps.Formats[0]

	// Try to find a good present mode, default to Fifo
	presentMode := wgpu.PresentModeFifo
	for _, m := range caps.PresentModes {
		if m == wgpu.PresentModeMailbox {
			presentMode = m
			break
		}
	}

	// Safety: Some drivers report extension modes (e.g. 1000361000) that cause warnings.
	// Ensure we use a standard enum value.
	if uint32(presentMode) >= 1000 {
		presentMode = wgpu.PresentModeFifo
	}

	a.Config = &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      format,
		Width:       uint32(width),
		Height:      uint32(height),
		PresentMode: presentMode,
		AlphaMode:   caps.AlphaModes[0],
	}
	surface.Configure(adapter, a.Device, a.Config)

	a.OcclusionMode = core.OcclusionConservative

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
	tiledLightCullMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Tiled Light Cull CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.TiledLightCullWGSL},
	})
	if err != nil {
		return err
	}

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
					MinBindingSize:   288, // CameraData size
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
			{
				Binding:    2,
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

	tiledLightCullBGL0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Tiled Light Cull BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   288,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
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

	tiledLightCullBGL1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Tiled Light Cull BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   32,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	tiledLightCullLayout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{tiledLightCullBGL0, tiledLightCullBGL1},
	})
	if err != nil {
		return err
	}

	a.TiledLightCullPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Tiled Light Cull Pipeline",
		Layout: tiledLightCullLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     tiledLightCullMod,
			EntryPoint: "main",
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
			// Output Color (HDR)
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageCompute,
				StorageTexture: wgpu.StorageTextureBindingLayout{
					Access:        wgpu.StorageTextureAccessWriteOnly,
					Format:        wgpu.TextureFormatRGBA16Float,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Shadow Maps (RGBA32Float Array)
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2DArray,
				},
			},
			// Skybox Texture (RGBA8Unorm)
			{
				Binding:    5,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			// Skybox Sampler
			{
				Binding:    6,
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

	lightBGL3, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Lighting BGL3",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   32,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    2,
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
		BindGroupLayouts: []*wgpu.BindGroupLayout{lightBGL0, lightBGL1, lightBGL2, lightBGL3},
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
	a.BufferManager = gpu.NewGpuBufferManager(a.Device, a.Profiler)
	var samplerErr error
	a.Sampler, samplerErr = a.Device.CreateSampler(&wgpu.SamplerDescriptor{
		MinFilter:     wgpu.FilterModeLinear,
		MagFilter:     wgpu.FilterModeLinear,
		MaxAnisotropy: 1,
	})
	if samplerErr != nil {
		panic(samplerErr)
	}

	// Default Camera Setup
	view := mgl32.Ident4()
	invView := mgl32.Ident4()
	invProj := mgl32.Ident4()
	a.BufferManager.LightingQuality = a.EffectiveLightingQuality()
	a.BufferManager.UpdateCamera(view, invView, invProj, a.Camera.Position, mgl32.Vec3{10, 20, 10}, a.Scene.AmbientLight, 1.0, a.Scene.SkyAmbientMix, a.Camera.DebugMode, a.RenderMode, uint32(len(a.Scene.Lights)), uint32(width), uint32(height), a.EffectiveLightingQuality())

	// Ensure scene buffers are created (even if empty) before bind groups
	a.BufferManager.UpdateScene(a.Scene, a.Camera, float32(width)/float32(height))
	a.BufferManager.UpdateTiledLightingResources(uint32(width), uint32(height))

	// Shadow Pipeline
	err = a.BufferManager.CreateShadowPipeline(shaders.ShadowMapWGSL)
	if err != nil {
		return err
	}

	a.rebuildCoreSwapchainResources(width, height)
	a.rebuildCoreSceneBindings()

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

	if err := a.setupFeatures(); err != nil {
		return err
	}

	return nil
}

func (a *App) SetSpriteAtlas(data []byte, w, h uint32) {
	if a.BufferManager != nil {
		a.BufferManager.SetSpriteAtlas("", data, w, h, 0)
	}
}

func (a *App) Shutdown() {
	a.shutdownFeatures()
}
