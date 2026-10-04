package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

func voxelBufferSize(buffer *wgpu.Buffer) uint64 {
	if buffer == nil {
		return 0
	}
	return buffer.GetSize()
}
func (m *GpuBufferManager) currentVoxelGPUResources() voxelGPUResources {
	backend := m.voxelNative
	if backend == nil {
		backend = nativeVoxelBackend{m}
	}
	r := voxelGPUResources{SectorTable: backend.BufferSize(m.SectorTableBuf), BrickTable: backend.BufferSize(m.BrickTableBuf), Auxiliary: backend.BufferSize(m.DenseOccupancyBuf), Material: backend.BufferSize(m.MaterialBuf), SectorGrid: backend.BufferSize(m.SectorGridBuf), DirectLookup: backend.BufferSize(m.DirectSectorLookupBuf), SectorGridParams: backend.BufferSize(m.SectorGridParamsBuf), StagingBytes: m.stagingVoxelGPUBytes()}
	for _, retired := range m.retiredBuffers {
		r.RetiredBytes = addRetainedVoxelBytes(r.RetiredBytes, retired.VoxelBytes)
	}
	pageBytes := voxelMul(voxelMul(uint64(m.VoxelPayloadPageSize), uint64(m.VoxelPayloadPageSize)), uint64(m.VoxelPayloadPageSize))
	for i := uint32(0); i < m.VoxelPayloadPageCount; i++ {
		if m.VoxelPayloadTex[i] != nil {
			r.AtlasBytes = addRetainedVoxelBytes(r.AtlasBytes, pageBytes)
		}
	}
	r.MaxBufferBytes, r.MaxStorageBytes, r.MaxUniformBytes = backend.Limits()
	return r
}

// Create every buffer/atlas/view and encode every migration before publication.
// Unpublished native resources are released on error; current owners survive.
func (m *GpuBufferManager) growVoxelGPUResources(resources *voxelGPUResources, next voxelGPUResources) (err error) {
	destinations := [7]**wgpu.Buffer{&m.SectorTableBuf, &m.BrickTableBuf, &m.DenseOccupancyBuf, &m.MaterialBuf, &m.SectorGridBuf, &m.DirectSectorLookupBuf, &m.SectorGridParamsBuf}
	labels := [7]string{"SectorTableBuf", "BrickTableBuf", "DenseOccupancyBuf", "MaterialBuf", "SectorGridBuf", "DirectSectorLookupBuf", "SectorGridParamsBuf"}
	var buffers [7]*wgpu.Buffer
	var textures [MaxVoxelAtlasPages]*wgpu.Texture
	var views [MaxVoxelAtlasPages]*wgpu.TextureView
	var encoder *wgpu.CommandEncoder
	var commands *wgpu.CommandBuffer
	published := false
	defer func() {
		if commands != nil {
			commands.Release()
		}
		if encoder != nil {
			encoder.Release()
		}
		if !published {
			for _, buffer := range buffers {
				if buffer != nil {
					buffer.Release()
				}
			}
			for _, view := range views {
				if view != nil {
					view.Release()
				}
			}
			for _, texture := range textures {
				if texture != nil {
					texture.Release()
				}
			}
		}
	}()
	old := voxelResourceSizes(*resources)
	for i, size := range voxelResourceSizes(next) {
		if size <= old[i] {
			continue
		}
		usage := wgpu.BufferUsageStorage
		if i == 6 {
			usage = wgpu.BufferUsageUniform
		}
		buffers[i], err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: labels[i], Size: size, Usage: usage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst})
		if err != nil {
			return fmt.Errorf("voxel %s allocation: %w", labels[i], err)
		}
		m.voxelGPUWorkStats.Creates++
		m.voxelGPUWorkStats.CreatedBytes = addRetainedVoxelBytes(m.voxelGPUWorkStats.CreatedBytes, size)
	}
	if m.VoxelPayloadPageCount > MaxVoxelAtlasPages {
		return fmt.Errorf("voxel atlas page count %d exceeds %d", m.VoxelPayloadPageCount, MaxVoxelAtlasPages)
	}
	for i := uint32(0); i < m.VoxelPayloadPageCount; i++ {
		if m.VoxelPayloadTex[i] != nil {
			continue
		}
		textures[i], err = m.Device.CreateTexture(&wgpu.TextureDescriptor{Label: fmt.Sprintf("VoxelPayloadAtlas%d", i), Size: wgpu.Extent3D{Width: m.VoxelPayloadPageSize, Height: m.VoxelPayloadPageSize, DepthOrArrayLayers: m.VoxelPayloadPageSize}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension3D, Format: wgpu.TextureFormatR8Uint, Usage: wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst})
		if err != nil {
			return fmt.Errorf("voxel atlas %d allocation: %w", i, err)
		}
		views[i], err = textures[i].CreateView(nil)
		if err != nil {
			return fmt.Errorf("voxel atlas %d view: %w", i, err)
		}
	}
	for i, buffer := range buffers {
		previous := *destinations[i]
		if buffer == nil || previous == nil {
			continue
		}
		if encoder == nil {
			encoder, err = m.Device.CreateCommandEncoder(nil)
			if err != nil {
				return fmt.Errorf("voxel migration encoder: %w", err)
			}
		}
		if err = encoder.CopyBufferToBuffer(previous, 0, buffer, 0, previous.GetSize()); err != nil {
			return fmt.Errorf("voxel %s migration: %w", labels[i], err)
		}
	}
	if encoder != nil {
		commands, err = encoder.Finish(nil)
		if err != nil {
			return fmt.Errorf("voxel migration finish: %w", err)
		}
		m.Device.GetQueue().Submit(commands)
		for i, buffer := range buffers {
			if buffer != nil && *destinations[i] != nil {
				m.voxelGPUWorkStats.CopiedBytes = addRetainedVoxelBytes(m.voxelGPUWorkStats.CopiedBytes, old[i])
			}
		}
	}
	for i, buffer := range buffers {
		if buffer == nil {
			continue
		}
		previous := *destinations[i]
		if previous != nil {
			m.retiredBuffers = append(m.retiredBuffers, retiredBuffer{Buffer: previous, VoxelBytes: previous.GetSize(), FramesLeft: RetiredBufferFrameDelay})
		}
		*destinations[i] = buffer
		if i == 3 {
			m.MaterialBufferGeneration++
		}
	}
	for i, texture := range textures {
		if texture != nil {
			m.VoxelPayloadTex[i], m.VoxelPayloadView[i] = texture, views[i]
		}
	}
	published = true
	*resources = m.currentVoxelGPUResources()
	return nil
}
