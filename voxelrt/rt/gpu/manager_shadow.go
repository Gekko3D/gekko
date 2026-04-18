package gpu

import (
	"encoding/binary"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func (m *GpuBufferManager) CreateShadowMapTextures(w, h, count uint32) {
	if count == 0 {
		count = 1
	}
	if m.ShadowMapView != nil {
		m.ShadowMapView.Release()
	}
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
	m.ShadowMapLayers = count
}

func (m *GpuBufferManager) CreateDirectionalShadowTextures(count uint32) {
	if count == 0 {
		count = 1
	}
	resolutions := [core.DirectionalShadowCascadeCount]uint32{512, 256}
	for i := 0; i < core.DirectionalShadowCascadeCount; i++ {
		if m.DirectionalShadowViews[i] != nil {
			m.DirectionalShadowViews[i].Release()
		}
		if m.DirectionalShadowArrays[i] != nil {
			m.DirectionalShadowArrays[i].Release()
		}
		var err error
		res := resolutions[i]
		m.DirectionalShadowArrays[i], err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label: "Directional Shadow Array",
			Size: wgpu.Extent3D{
				Width:              res,
				Height:             res,
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
		m.DirectionalShadowViews[i], err = m.DirectionalShadowArrays[i].CreateView(&wgpu.TextureViewDescriptor{
			Label:           "Directional Shadow View",
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
	m.DirectionalShadowLayers = count
}

func nextPow2U32(v uint32) uint32 {
	if v <= 1 {
		return 1
	}
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	return v + 1
}

func (m *GpuBufferManager) EnsureShadowMapCapacity(numLayers uint32) bool {
	required := nextPow2U32(numLayers)
	if m.ShadowMapView != nil && required <= m.ShadowMapLayers {
		return false
	}
	m.CreateShadowMapTextures(1024, 1024, required)
	return true
}

func (m *GpuBufferManager) EnsureDirectionalShadowCapacity(numLayers uint32) bool {
	required := nextPow2U32(numLayers)
	if m.DirectionalShadowViews[0] != nil && required <= m.DirectionalShadowLayers {
		return false
	}
	m.CreateDirectionalShadowTextures(required)
	return true
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

	// Ensure shadow update buffer exists
	m.ensureBuffer("ShadowUpdatesBuf", &m.ShadowUpdatesBuf, make([]byte, 16), wgpu.BufferUsageStorage, 0)

	// Group 0: Scene + Lights + Update Indices
	m.ShadowBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.ShadowPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.ShadowUpdatesBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.ShadowInstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.ShadowBVHNodesBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ShadowLayerParamsBuf, Size: wgpu.WholeSize},
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
		Entries: m.appendDenseOccupancyEntry(m.appendVoxelPayloadEntries([]wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ShadowObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 8, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
			{Binding: 11, Buffer: m.DirectSectorLookupBuf, Size: wgpu.WholeSize},
		}, 2), DenseOccupancyBinding),
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) DispatchShadowPass(encoder *wgpu.CommandEncoder, updates []core.ShadowUpdate) {
	if m.ShadowPipeline == nil || m.ShadowBindGroup0 == nil {
		return
	}

	if len(updates) == 0 {
		return
	}

	for _, resolution := range []uint32{512, 256, 128, shadowAtlasLayerResolution} {
		bucket := make([]core.ShadowUpdate, 0, len(updates))
		for _, update := range updates {
			if update.Resolution == resolution {
				bucket = append(bucket, update)
			}
		}
		if len(bucket) == 0 {
			continue
		}
		updateBytes := make([]byte, len(bucket)*24)
		for i, update := range bucket {
			offset := i * 24
			binary.LittleEndian.PutUint32(updateBytes[offset+0:], update.LightIndex)
			binary.LittleEndian.PutUint32(updateBytes[offset+4:], update.ShadowLayer)
			binary.LittleEndian.PutUint32(updateBytes[offset+8:], update.CascadeIndex)
			binary.LittleEndian.PutUint32(updateBytes[offset+12:], update.Kind)
			binary.LittleEndian.PutUint32(updateBytes[offset+16:], update.Tier)
			binary.LittleEndian.PutUint32(updateBytes[offset+20:], update.Resolution)
		}
		if m.ensureBuffer("ShadowUpdatesBuf", &m.ShadowUpdatesBuf, updateBytes, wgpu.BufferUsageStorage, 1024) {
			m.CreateShadowBindGroups()
		}

		cPass := encoder.BeginComputePass(nil)
		cPass.SetPipeline(m.ShadowPipeline)
		cPass.SetBindGroup(0, m.ShadowBindGroup0, nil)
		cPass.SetBindGroup(1, m.ShadowBindGroup1, nil)
		cPass.SetBindGroup(2, m.ShadowBindGroup2, nil)

		wgX := (resolution + 7) / 8
		wgY := (resolution + 7) / 8
		cPass.DispatchWorkgroups(uint32(wgX), uint32(wgY), uint32(len(bucket)))
		cPass.End()
	}
}

// Helpers
