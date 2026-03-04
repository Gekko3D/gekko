package gpu

import (
	"encoding/binary"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) CreateShadowMapTextures(w, h, count uint32) {
	if m.ShadowMapArray != nil {
		m.ShadowMapArray.Release()
	}

	var err error
	m.ShadowMapArray, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Shadow Map Array",
		Size: wgpu.Extent3D{
			Width:              w,
			Height:             h,
			DepthOrArrayLayers: count,
		},
		MipLevelCount: 1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA32Float,
		Usage:         wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding,
		SampleCount:   1,
	})
	if err != nil {
		panic(err)
	}

	m.ShadowMapView, err = m.ShadowMapArray.CreateView(&wgpu.TextureViewDescriptor{
		Label:           "Shadow Map View",
		Format:          wgpu.TextureFormatRGBA32Float,
		Dimension:       wgpu.TextureViewDimension2DArray,
		BaseMipLevel:    0,
		MipLevelCount:   1,
		BaseArrayLayer:  0,
		ArrayLayerCount: count,
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) CreateShadowPipeline(code string) error {
	mod, err := m.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Shadow Map CS",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: code},
	})
	if err != nil {
		return err
	}
	defer mod.Release()

	m.ShadowPipeline, err = m.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label: "Shadow Pipeline",
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     mod,
			EntryPoint: "main",
		},
	})
	return err
}

func (m *GpuBufferManager) CreateShadowBindGroups() {
	var err error

	// Ensure indices buffer exists
	m.ensureBuffer("ShadowIndicesBuf", &m.ShadowIndicesBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0)

	// Group 0: Scene + Lights + Update Indices
	m.ShadowBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.ShadowIndicesBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 1: Output Shadow Maps
	m.ShadowBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.ShadowMapView},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 2: Voxel Data
	m.ShadowBindGroup2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.VoxelPayloadView},
			{Binding: 4, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 5, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) DispatchShadowPass(encoder *wgpu.CommandEncoder, indices []uint32) {
	if m.ShadowPipeline == nil || m.ShadowBindGroup0 == nil {
		return
	}

	if len(indices) == 0 {
		return
	}

	// Upload indices
	idxBytes := make([]byte, len(indices)*4)
	for i, v := range indices {
		binary.LittleEndian.PutUint32(idxBytes[i*4:], v)
	}

	// Ensure buffer size
	if m.ensureBuffer("ShadowIndicesBuf", &m.ShadowIndicesBuf, idxBytes, wgpu.BufferUsageStorage, 1024) {
		// If recreated, we must recreate the bind group immediately for it to take effect
		// This might be expensive if done every frame, but ensureBuffer only recreates on growth.
		m.CreateShadowBindGroups()
	} else {
		// Just write if not recreated (ensureBuffer writes data if buffer acts as update)
		// Actually ensureBuffer does write data.
		// If buffer wasn't recreated, we still need to write if we want to update content?
		// modify ensureBuffer behavior?
		// ensureBuffer writes data if passed.
	}
	// Wait, ensureBuffer implementation:
	// if len(data) > 0 { m.Device.GetQueue().WriteBuffer(*buf, 0, data) }
	// So data IS written.

	cPass := encoder.BeginComputePass(nil)
	cPass.SetPipeline(m.ShadowPipeline)
	cPass.SetBindGroup(0, m.ShadowBindGroup0, nil)
	cPass.SetBindGroup(1, m.ShadowBindGroup1, nil)
	cPass.SetBindGroup(2, m.ShadowBindGroup2, nil)

	// Dispatch for 1024x1024 shadow maps
	wgX := (1024 + 7) / 8
	wgY := (1024 + 7) / 8
	cPass.DispatchWorkgroups(uint32(wgX), uint32(wgY), uint32(len(indices)))
	cPass.End()
}

// Helpers
