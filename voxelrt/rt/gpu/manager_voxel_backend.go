package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type voxelBufferCopy struct {
	src, dst             *wgpu.Buffer
	srcOffset, dstOffset uint64
	size                 uint64
}

// Both native updates and byte-array verification use this boundary. Geometry,
// admission, encoding and completion stay on the same production path.
type voxelNativeBackend interface {
	BufferSize(*wgpu.Buffer) uint64
	Limits() (buffer, storage, uniform uint64)
	CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error)
	CopyBuffers([]voxelBufferCopy) error
	WriteBuffer(*wgpu.Buffer, uint64, []byte) error
	WritePayload(page uint32, origin [3]uint32, data []byte) error
	ReleaseBuffer(*wgpu.Buffer)
}

type nativeVoxelBackend struct{ manager *GpuBufferManager }

func (b nativeVoxelBackend) BufferSize(buffer *wgpu.Buffer) uint64 { return voxelBufferSize(buffer) }
func (b nativeVoxelBackend) Limits() (uint64, uint64, uint64) {
	limits := b.manager.Device.GetLimits().Limits
	return limits.MaxBufferSize, limits.MaxStorageBufferBindingSize, limits.MaxUniformBufferBindingSize
}
func (b nativeVoxelBackend) CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error) {
	usage := wgpu.BufferUsageStorage
	if uniform {
		usage = wgpu.BufferUsageUniform
	}
	return b.manager.Device.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: size, Usage: usage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst})
}
func (b nativeVoxelBackend) CopyBuffers(copies []voxelBufferCopy) error {
	if len(copies) == 0 {
		return nil
	}
	encoder, err := b.manager.Device.CreateCommandEncoder(nil)
	if err != nil {
		return fmt.Errorf("voxel migration encoder: %w", err)
	}
	defer encoder.Release()
	for _, copy := range copies {
		if err := encoder.CopyBufferToBuffer(copy.src, copy.srcOffset, copy.dst, copy.dstOffset, copy.size); err != nil {
			return fmt.Errorf("voxel migration copy: %w", err)
		}
	}
	commands, err := encoder.Finish(nil)
	if err != nil {
		return fmt.Errorf("voxel migration finish: %w", err)
	}
	defer commands.Release()
	b.manager.Device.GetQueue().Submit(commands)
	return nil
}
func (b nativeVoxelBackend) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	return b.manager.Device.GetQueue().WriteBuffer(buffer, offset, data)
}
func (b nativeVoxelBackend) WritePayload(page uint32, origin [3]uint32, data []byte) error {
	return b.manager.Device.GetQueue().WriteTexture(
		&wgpu.ImageCopyTexture{Texture: b.manager.VoxelPayloadTex[page], Origin: wgpu.Origin3D{X: origin[0], Y: origin[1], Z: origin[2]}, Aspect: wgpu.TextureAspectAll},
		data, &wgpu.TextureDataLayout{BytesPerRow: volume.BrickSize, RowsPerImage: volume.BrickSize},
		&wgpu.Extent3D{Width: volume.BrickSize, Height: volume.BrickSize, DepthOrArrayLayers: volume.BrickSize})
}
func (b nativeVoxelBackend) ReleaseBuffer(buffer *wgpu.Buffer) { buffer.Release() }

var voxelBufferLabels = [7]string{"SectorTableBuf", "BrickTableBuf", "DenseOccupancyBuf", "MaterialBuf", "SectorGridBuf", "DirectSectorLookupBuf", "SectorGridParamsBuf"}

func (m *GpuBufferManager) voxelBufferDestinations() [7]**wgpu.Buffer {
	return [7]**wgpu.Buffer{&m.SectorTableBuf, &m.BrickTableBuf, &m.DenseOccupancyBuf, &m.MaterialBuf, &m.SectorGridBuf, &m.DirectSectorLookupBuf, &m.SectorGridParamsBuf}
}

func (m *GpuBufferManager) writeVoxelBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if err := m.voxelNative.WriteBuffer(buffer, offset, data); err != nil {
		return err
	}
	if stage := m.voxelGrowth; stage != nil {
		for i, source := range stage.sources {
			if source == buffer && stage.buffers[i] != nil {
				// Even a refused native operation may have queued work. Retirement
				// conservatively waits for the next render fence covering mirrors.
				stage.used[i] = true
				if err := stage.backend.WriteBuffer(stage.buffers[i], offset, data); err != nil {
					m.abortVoxelGPUStage()
					return err
				}
				break
			}
		}
	}
	return nil
}

func (m *GpuBufferManager) voxelBufferMirrored(index int) bool {
	return m.voxelGrowth != nil && m.voxelGrowth.buffers[index] != nil
}
