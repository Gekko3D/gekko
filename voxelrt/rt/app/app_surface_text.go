package app

import (
	"fmt"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
)

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

func (a *App) createParticleSimPipelines(mod *wgpu.ShaderModule) {
	var err error

	// BG0: Simulation State
	bg0Entries := []wgpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform}},
		{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
		{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
		{Binding: 3, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
		{Binding: 4, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
		{Binding: 5, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
	}
	simBGL0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "Particle Sim BGL0", Entries: bg0Entries})
	if err != nil {
		panic(err)
	}

	// BG1: Emitters & Spawn Requests
	bg1Entries := []wgpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}}, // Emitters
		{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}}, // Requests
	}
	simBGL1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "Particle Sim BGL1", Entries: bg1Entries})
	if err != nil {
		panic(err)
	}

	// BG2: Voxel Data (Shared with Shadow/GBuffer)
	bg2Entries := appendVoxelPayloadTextureLayoutEntries([]wgpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // Sectors
		{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // Bricks
		{Binding: 6, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // Materials
		{Binding: 7, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // ObjectParams
		{Binding: 8, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // Instances
		{Binding: 9, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},  // Grid
		{Binding: 10, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}}, // GridParams
	}, 2, wgpu.ShaderStageCompute)
	simBGL2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "Particle Sim BGL2", Entries: bg2Entries})
	if err != nil {
		panic(err)
	}

	simLayout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "Particle Sim Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{simBGL0, simBGL1, simBGL2},
	})
	if err != nil {
		panic(err)
	}

	a.ParticleInitPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Particle Init Pipeline",
		Layout: simLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "init_draw_args",
		},
	})
	if err != nil {
		panic(err)
	}

	a.ParticleSimPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Particle Sim Pipeline",
		Layout: simLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "simulate",
		},
	})
	if err != nil {
		panic(err)
	}

	a.ParticleSpawnPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Particle Spawn Pipeline",
		Layout: simLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "spawn",
		},
	})
	if err != nil {
		panic(err)
	}

	a.ParticleFinalizePipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "Particle Finalize Pipeline",
		Layout: simLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "finalize_draw_args",
		},
	})
	if err != nil {
		panic(err)
	}

	// Also update BufferManager with one of them to get bind group layouts
	a.BufferManager.ParticleSimPipeline = a.ParticleSimPipeline
	a.BufferManager.CreateParticleSimBindGroups()
}
