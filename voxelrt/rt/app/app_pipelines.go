package app

import (
	"fmt"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"github.com/cogentcore/webgpu/wgpu"
)

func appendVoxelPayloadTextureLayoutEntries(entries []wgpu.BindGroupLayoutEntry, startBinding uint32, visibility wgpu.ShaderStage) []wgpu.BindGroupLayoutEntry {
	for i := uint32(0); i < gpu_rt.MaxVoxelAtlasPages; i++ {
		entries = append(entries, wgpu.BindGroupLayoutEntry{
			Binding:    startBinding + i,
			Visibility: visibility,
			Texture: wgpu.TextureBindingLayout{
				SampleType:    wgpu.TextureSampleTypeUint,
				ViewDimension: wgpu.TextureViewDimension3D,
			},
		})
	}
	return entries
}

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
					MinBindingSize:   288, // CameraData size
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
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize:   0,
					HasDynamicOffset: false,
				},
			},
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageFragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingTypeFiltering,
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

func (a *App) setupSpritesPipeline() {
	spriteMod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Sprites Billboard",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.SpritesWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create sprite shader module: %v\n", err)
		return
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Sprites BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: 288,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeFloat,
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
		fmt.Printf("ERROR: Failed to create sprites BGL0: %v\n", err)
		return
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Sprites BGL1",
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
		fmt.Printf("ERROR: Failed to create sprites BGL1: %v\n", err)
		return
	}

	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create sprites pipeline layout: %v\n", err)
		return
	}

	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Sprites Pipeline",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     spriteMod,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     spriteMod,
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
		fmt.Printf("ERROR: Failed to create sprite render pipeline: %v\n", err)
		return
	}
	a.SpritesPipeline = pipeline
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
					MinBindingSize:   288,
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
			{
				Binding:    4,
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
		Entries: appendVoxelPayloadTextureLayoutEntries([]wgpu.BindGroupLayoutEntry{
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
			{ // Materials (packed vec4 table; transparency in pbr_params.w)
				Binding:    6,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // ObjectParams
				Binding:    7,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // Tree64
				Binding:    8,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // SectorGrid
				Binding:    9,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{ // SectorGridParams
				Binding:    10,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
		}, 2, wgpu.ShaderStageFragment),
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay BGL1: %v\n", err)
		return
	}

	// Group 2: GBuffer inputs + shadow maps + lit opaque color for refraction
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
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2DArray,
				},
			},
			{
				Binding:    3,
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

	bgl3, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "TransparentOverlay BGL3",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: 32,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 0,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create overlay BGL3: %v\n", err)
		return
	}

	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2, bgl3},
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

	// Group 0: opaque lit color, accum color, weight, volumetric color, volumetric depth, scene depth
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
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    5,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
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
	a.createResolveBindGroup(bgl0)
}

func (a *App) createResolveBindGroup(layout *wgpu.BindGroupLayout) {
	if a == nil || a.BufferManager == nil || layout == nil {
		return
	}
	if a.StorageView == nil || a.BufferManager.TransparentAccumView == nil || a.BufferManager.TransparentWeightView == nil || a.BufferManager.CurrentVolumetricView() == nil || a.BufferManager.CurrentVolumetricDepthView() == nil || a.BufferManager.DepthView == nil {
		// Views not ready yet (e.g., during early init/resize), skip creating BG
		return
	}
	var err error
	a.ResolveBG, err = a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.StorageView},
			{Binding: 1, TextureView: a.BufferManager.TransparentAccumView},
			{Binding: 2, TextureView: a.BufferManager.TransparentWeightView},
			{Binding: 3, TextureView: a.BufferManager.CurrentVolumetricView()},
			{Binding: 4, TextureView: a.BufferManager.CurrentVolumetricDepthView()},
			{Binding: 5, TextureView: a.BufferManager.DepthView},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create resolve bind group: %v\n", err)
		return
	}
}
