package app

import (
	"fmt"

	"github.com/gekko3d/gekko/voxelrt/rt/shaders"

	"github.com/cogentcore/webgpu/wgpu"
)

func (a *App) createCAVolumeSimPipeline() error {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "CA Volume Sim CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.CAVolumeSimWGSL},
	})
	if err != nil {
		return err
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume Sim BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 48}},
			{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},
			{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},
		},
	})
	if err != nil {
		return err
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume Sim BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension3D,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				StorageTexture: wgpu.StorageTextureBindingLayout{
					Access:        wgpu.StorageTextureAccessWriteOnly,
					Format:        wgpu.TextureFormatRGBA16Float,
					ViewDimension: wgpu.TextureViewDimension3D,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "CA Volume Sim Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		return err
	}

	a.CAVolumeSimPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "CA Volume Sim Pipeline",
		Layout: layout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "simulate",
		},
	})
	if err != nil {
		return err
	}
	a.BufferManager.CAVolumeSimPipeline = a.CAVolumeSimPipeline
	a.BufferManager.CreateCAVolumeSimBindGroups()
	return nil
}

func (a *App) createCAVolumeBoundsPipeline() error {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "CA Volume Bounds CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.CAVolumeBoundsWGSL},
	})
	if err != nil {
		return err
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume Bounds BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 48}},
			{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}},
			{Binding: 2, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
		},
	})
	if err != nil {
		return err
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume Bounds BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension3D,
				},
			},
		},
	})
	if err != nil {
		return err
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "CA Volume Bounds Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		return err
	}

	a.CAVolumeBoundsPipeline, err = a.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:  "CA Volume Bounds Pipeline",
		Layout: layout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "compute_bounds",
		},
	})
	if err != nil {
		return err
	}
	a.BufferManager.CAVolumeBoundsPipeline = a.CAVolumeBoundsPipeline
	a.BufferManager.CreateCAVolumeBoundsBindGroups()
	return nil
}

func (a *App) setupCAVolumePipeline() {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "CA Volume Render",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.CAVolumeRenderWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create CA volume shader module: %v\n", err)
		return
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume BGL0",
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
		fmt.Printf("ERROR: Failed to create CA volume BGL0: %v\n", err)
		return
	}

	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 48},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension3D,
				},
			},
			{
				Binding:    3,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
			{
				Binding:    4,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create CA volume BGL1: %v\n", err)
		return
	}

	bgl2, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CA Volume BGL2",
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
		fmt.Printf("ERROR: Failed to create CA volume BGL2: %v\n", err)
		return
	}

	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "CA Volume Layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1, bgl2},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create CA volume pipeline layout: %v\n", err)
		return
	}

	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "CA Volume Pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{Module: mod, EntryPoint: "vs_main"},
		Fragment: &wgpu.FragmentState{
			Module:     mod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format: wgpu.TextureFormatRGBA16Float,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorSrcAlpha, Operation: wgpu.BlendOperationAdd},
						Alpha: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorZero, DstFactor: wgpu.BlendFactorSrcAlpha, Operation: wgpu.BlendOperationAdd},
					},
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
		fmt.Printf("ERROR: Failed to create CA volume pipeline: %v\n", err)
		return
	}

	a.CAVolumePipeline = pipeline
	a.BufferManager.CreateCAVolumeRenderBindGroups(a.CAVolumePipeline)
}
