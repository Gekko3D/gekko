package app

import (
	"fmt"

	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"github.com/cogentcore/webgpu/wgpu"
)

func (a *App) setupAnalyticMediumPipeline() {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Analytic Medium Render",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.AnalyticMediumWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create analytic medium shader module: %v\n", err)
		return
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "AnalyticMedium BGL0",
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
		fmt.Printf("ERROR: Failed to create analytic medium BGL0: %v\n", err)
		return
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "AnalyticMedium BGL1",
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
		fmt.Printf("ERROR: Failed to create analytic medium BGL1: %v\n", err)
		return
	}

	bgl2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "AnalyticMedium BGL2",
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
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 96},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create analytic medium BGL2: %v\n", err)
		return
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "Analytic Medium Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create analytic medium layout: %v\n", err)
		return
	}

	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Analytic Medium Pipeline",
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
				{
					Format:    wgpu.TextureFormatR16Float,
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create analytic medium pipeline: %v\n", err)
		return
	}

	a.AnalyticMediumPipeline = pipeline
	a.BufferManager.CreateAnalyticMediumBindGroups(a.AnalyticMediumPipeline)
}
