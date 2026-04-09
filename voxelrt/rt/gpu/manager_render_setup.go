package gpu

import "github.com/cogentcore/webgpu/wgpu"

func (m *GpuBufferManager) CreateDebugBindGroups(pipeline *wgpu.ComputePipeline) {
	entries0 := []wgpu.BindGroupEntry{
		{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
		{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
		{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
	}
	desc0 := &wgpu.BindGroupDescriptor{
		Layout:  pipeline.GetBindGroupLayout(0),
		Entries: entries0,
	}
	var err error
	m.DebugBindGroup0, err = m.Device.CreateBindGroup(desc0)
	if err != nil {
		panic(err)
	}

}
func (m *GpuBufferManager) CreateGBufferTextures(w, h uint32) {
	if w == 0 || h == 0 {
		return
	}

	setupTexture := func(tex **wgpu.Texture, view **wgpu.TextureView, label string, format wgpu.TextureFormat, usage wgpu.TextureUsage, tw, th uint32) {
		if *tex != nil {
			(*tex).Release()
		}
		var err error
		*tex, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         label,
			Size:          wgpu.Extent3D{Width: tw, Height: th, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			Dimension:     wgpu.TextureDimension2D,
			Format:        format,
			Usage:         usage,
			SampleCount:   1,
		})
		if err != nil {
			panic(err)
		}
		*view, err = (*tex).CreateView(nil)
		if err != nil {
			panic(err)
		}
	}

	setupTexture(&m.GBufferDepth, &m.DepthView, "GBuffer Depth", wgpu.TextureFormatRGBA32Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding, w, h)
	setupTexture(&m.GBufferNormal, &m.NormalView, "GBuffer Normal", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding, w, h)
	setupTexture(&m.GBufferMaterial, &m.MaterialView, "GBuffer Material", wgpu.TextureFormatRGBA32Float, wgpu.TextureUsageStorageBinding|wgpu.TextureUsageTextureBinding, w, h)
	setupTexture(&m.PlanetDepthTex, &m.PlanetDepthView, "Planet Depth", wgpu.TextureFormatR16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, w, h)

	// Transparent accumulation targets for WBOIT
	setupTexture(&m.TransparentAccumTex, &m.TransparentAccumView, "Transparent Accum", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, w, h)
	setupTexture(&m.TransparentWeightTex, &m.TransparentWeightView, "Transparent Weight", wgpu.TextureFormatR16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, w, h)

	volW := max(1, (w+1)/2)
	volH := max(1, (h+1)/2)
	for i := 0; i < 2; i++ {
		setupTexture(&m.VolumetricTex[i], &m.VolumetricView[i], "Volumetric Color", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, volW, volH)
		setupTexture(&m.VolumetricDepthTex[i], &m.VolumetricDepthView[i], "Volumetric Depth", wgpu.TextureFormatR16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, volW, volH)
	}
	setupTexture(&m.CAVolumeColorTex, &m.CAVolumeColorView, "CA Volume Color", wgpu.TextureFormatRGBA16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, volW, volH)
	setupTexture(&m.CAVolumeDepthTex, &m.CAVolumeDepthView, "CA Volume Depth", wgpu.TextureFormatR16Float, wgpu.TextureUsageRenderAttachment|wgpu.TextureUsageTextureBinding, volW, volH)
	m.VolumetricWidth = volW
	m.VolumetricHeight = volH
	m.VolumetricHistoryIdx = 0
	m.VolumetricRenderIdx = 1
	m.VolumetricHistoryValid = false

	m.CreateShadowMapTextures(1024, 1024, 1)
	m.CreateDirectionalShadowTextures(1)
}

func (m *GpuBufferManager) CreateGBufferBindGroups(gbPipeline, lightPipeline *wgpu.ComputePipeline) {
	var err error

	m.GBufferBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.NormalView},
			{Binding: 2, TextureView: m.MaterialView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.LightingBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.ShadowLayerParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.GBufferBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.GBufferBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: gbPipeline.GetBindGroupLayout(2),
		Entries: m.appendVoxelPayloadEntries([]wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 8, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
			{Binding: 11, Buffer: m.TerrainChunkLookupBuf, Size: wgpu.WholeSize},
			{Binding: 12, Buffer: m.PlanetTileLookupBuf, Size: wgpu.WholeSize},
		}, 2),
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateLightingBindGroups(lightPipeline *wgpu.ComputePipeline, outputView *wgpu.TextureView) {
	var err error
	m.LightingBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.NormalView},
			{Binding: 2, TextureView: m.MaterialView},
			{Binding: 3, TextureView: outputView},
			{Binding: 4, TextureView: m.ShadowMapView},
			{Binding: 5, TextureView: m.getSkyboxView()},
			{Binding: 6, Sampler: m.getSkyboxSampler()},
		},
	})
	if err != nil {
		panic(err)
	}

	m.LightingTileBindGroup, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(3),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.TileLightParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.TileLightHeadersBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.TileLightIndicesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Create materials bind group (group 2)
	m.LightingBindGroupMaterial, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: lightPipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 3, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

// UpdateParticles manages GPU particle buffers and state.
// If isGpuSim is true, it initializes/resizes buffers for GPU simulation.
