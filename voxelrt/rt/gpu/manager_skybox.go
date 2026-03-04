package gpu

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) CreateSkyboxGenPipeline(wgsl string) {
	shader, err := m.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "SkyboxGenShader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: wgsl},
	})
	if err != nil {
		panic(err)
	}
	defer shader.Release()

	m.SkyboxGenPipeline, err = m.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "SkyboxGenPipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     shader,
			EntryPoint: "main",
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) UpdateSkyboxGPU(width, height uint32, layers []GpuSkyboxLayer, sunDir [4]float32, smooth bool, lightPipeline *wgpu.ComputePipeline, outputView *wgpu.TextureView) {
	if m.SkyboxGenPipeline == nil {
		return
	}

	samplerChanged := (m.SkyboxSmooth != smooth)
	m.SkyboxSmooth = smooth

	if m.SkyboxTex == nil || m.SkyboxTex.GetWidth() != width || m.SkyboxTex.GetHeight() != height {
		if m.SkyboxTex != nil {
			m.SkyboxTex.Release()
			m.SkyboxView.Release()
		}

		var err error
		m.SkyboxTex, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         "Skybox Texture",
			Size:          wgpu.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
			Dimension:     wgpu.TextureDimension2D,
			Format:        wgpu.TextureFormatRGBA16Float,
			Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageStorageBinding,
			SampleCount:   1,
			MipLevelCount: 1,
		})
		if err != nil {
			panic(err)
		}
		m.SkyboxView, err = m.SkyboxTex.CreateView(nil)
		if err != nil {
			panic(err)
		}
		samplerChanged = true // New view needs new bind group
		m.SkyboxGenBindGroup = nil
	}

	// Update Buffers
	if m.ensureBuffer("SkyboxLayersBuf", &m.SkyboxLayersBuf, nil, wgpu.BufferUsageStorage, len(layers)*int(unsafe.Sizeof(GpuSkyboxLayer{}))) {
		m.SkyboxGenBindGroup = nil
	}
	if len(layers) > 0 {
		m.Device.GetQueue().WriteBuffer(m.SkyboxLayersBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&layers[0])), len(layers)*int(unsafe.Sizeof(GpuSkyboxLayer{}))))
	}

	uniforms := GpuSkyboxUniforms{
		LayerCount: uint32(len(layers)),
		SunDir:     sunDir,
	}
	if m.ensureBuffer("SkyboxParamsBuf", &m.SkyboxParamsBuf, nil, wgpu.BufferUsageUniform, int(unsafe.Sizeof(uniforms))) {
		m.SkyboxGenBindGroup = nil
	}
	m.Device.GetQueue().WriteBuffer(m.SkyboxParamsBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&uniforms)), int(unsafe.Sizeof(uniforms))))

	if m.SkyboxGenBindGroup == nil {
		var err error
		m.SkyboxGenBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: m.SkyboxGenPipeline.GetBindGroupLayout(0),
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: m.SkyboxParamsBuf, Size: wgpu.WholeSize},
				{Binding: 1, Buffer: m.SkyboxLayersBuf, Size: wgpu.WholeSize},
				{Binding: 2, TextureView: m.SkyboxView},
			},
		})
		if err != nil {
			panic(err)
		}
	}

	// Dispatch Compute
	encoder, err := m.Device.CreateCommandEncoder(nil)
	if err != nil {
		panic(err)
	}
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(m.SkyboxGenPipeline)
	pass.SetBindGroup(0, m.SkyboxGenBindGroup, nil)
	pass.DispatchWorkgroups((width+7)/8, (height+7)/8, 1)
	err = pass.End()
	if err != nil {
		panic(err)
	}

	cmd, err := encoder.Finish(nil)
	if err != nil {
		panic(err)
	}
	m.Device.GetQueue().Submit(cmd)

	if samplerChanged && lightPipeline != nil && outputView != nil {
		m.CreateLightingBindGroups(lightPipeline, outputView)
	}
}

func (m *GpuBufferManager) UpdateSkybox(width, height uint32, data []byte, smooth bool, lightPipeline *wgpu.ComputePipeline, outputView *wgpu.TextureView) {
	samplerChanged := (m.SkyboxSmooth != smooth)
	m.SkyboxSmooth = smooth

	if m.SkyboxTex == nil || m.SkyboxTex.GetWidth() != width || m.SkyboxTex.GetHeight() != height {
		if m.SkyboxTex != nil {
			m.SkyboxTex.Release()
			m.SkyboxView.Release()
		}

		var err error
		m.SkyboxTex, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         "Skybox Texture",
			Size:          wgpu.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
			Dimension:     wgpu.TextureDimension2D,
			Format:        wgpu.TextureFormatRGBA8Unorm,
			Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
			SampleCount:   1,
			MipLevelCount: 1,
		})
		if err != nil {
			panic(err)
		}
		m.SkyboxView, err = m.SkyboxTex.CreateView(nil)
		if err != nil {
			panic(err)
		}
		samplerChanged = true // New view needs new bind group
	}

	m.Device.GetQueue().WriteTexture(
		&wgpu.ImageCopyTexture{
			Texture:  m.SkyboxTex,
			MipLevel: 0,
			Origin:   wgpu.Origin3D{X: 0, Y: 0, Z: 0},
		},
		data,
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  width * 4,
			RowsPerImage: height,
		},
		&wgpu.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
	)

	if samplerChanged && lightPipeline != nil && outputView != nil {
		m.CreateLightingBindGroups(lightPipeline, outputView)
	}
}
func (m *GpuBufferManager) getSkyboxView() *wgpu.TextureView {
	if m.SkyboxView != nil {
		return m.SkyboxView
	}
	// Create 1x1 black placeholder
	m.UpdateSkybox(1, 1, []byte{0, 0, 0, 255}, true, nil, nil)
	return m.SkyboxView
}

func (m *GpuBufferManager) getSkyboxSampler() *wgpu.Sampler {
	if m.SkyboxSmooth {
		if m.SkyboxSampler != nil {
			return m.SkyboxSampler
		}
		// Create default linear sampler
		var err error
		m.SkyboxSampler, err = m.Device.CreateSampler(&wgpu.SamplerDescriptor{
			AddressModeU:  wgpu.AddressModeRepeat,
			AddressModeV:  wgpu.AddressModeClampToEdge,
			MinFilter:     wgpu.FilterModeLinear,
			MagFilter:     wgpu.FilterModeLinear,
			MipmapFilter:  wgpu.MipmapFilterModeLinear,
			MaxAnisotropy: 1,
		})
		if err != nil {
			panic(err)
		}
		return m.SkyboxSampler
	} else {
		if m.SkyboxSamplerNearest != nil {
			return m.SkyboxSamplerNearest
		}
		// Create nearest sampler
		var err error
		m.SkyboxSamplerNearest, err = m.Device.CreateSampler(&wgpu.SamplerDescriptor{
			AddressModeU:  wgpu.AddressModeRepeat,
			AddressModeV:  wgpu.AddressModeClampToEdge,
			MinFilter:     wgpu.FilterModeNearest,
			MagFilter:     wgpu.FilterModeNearest,
			MipmapFilter:  wgpu.MipmapFilterModeNearest,
			MaxAnisotropy: 1,
		})
		if err != nil {
			panic(err)
		}
		return m.SkyboxSamplerNearest
	}
}
