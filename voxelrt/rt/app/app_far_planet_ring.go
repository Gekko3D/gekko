package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

func (a *App) setupFarPlanetRingPipeline() {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Far Planet Ring Render",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.FarPlanetRingWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create far planet-ring shader module: %v\n", err)
		return
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Far Planet Ring BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: gpu.CameraUniformSizeBytes},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create far planet-ring BGL0: %v\n", err)
		return
	}
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Far Planet Ring BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 16},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create far planet-ring BGL1: %v\n", err)
		return
	}
	bgl2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Far Planet Ring BGL2",
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
		fmt.Printf("ERROR: Failed to create far planet-ring BGL2: %v\n", err)
		return
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "Far Planet Ring Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create far planet-ring layout: %v\n", err)
		return
	}
	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Far Planet Ring Pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{Module: mod, EntryPoint: "vs_main"},
		Fragment: &wgpu.FragmentState{
			Module:     mod,
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
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create far planet-ring pipeline: %v\n", err)
		return
	}
	a.FarPlanetRingPipeline = pipeline
	a.BufferManager.CreateFarPlanetRingBindGroups(a.FarPlanetRingPipeline)
}
