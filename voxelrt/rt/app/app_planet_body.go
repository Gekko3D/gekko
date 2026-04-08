package app

import (
	"fmt"

	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"github.com/cogentcore/webgpu/wgpu"
)

func (a *App) setupPlanetBodyPipeline() {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Planet Body Render",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.PlanetBodyWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create planet body shader module: %v\n", err)
		return
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "PlanetBody BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 288},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create planet body BGL0: %v\n", err)
		return
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "PlanetBody BGL1",
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
		fmt.Printf("ERROR: Failed to create planet body BGL1: %v\n", err)
		return
	}

	bgl2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "PlanetBody BGL2",
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
		fmt.Printf("ERROR: Failed to create planet body BGL2: %v\n", err)
		return
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "Planet Body Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create planet body layout: %v\n", err)
		return
	}

	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Planet Body Pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{Module: mod, EntryPoint: "vs_main"},
		Fragment: &wgpu.FragmentState{
			Module:     mod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    wgpu.TextureFormatRGBA16Float,
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create planet body pipeline: %v\n", err)
		return
	}

	a.PlanetBodyPipeline = pipeline
	a.BufferManager.CreatePlanetBodyBindGroups(a.PlanetBodyPipeline)
}
