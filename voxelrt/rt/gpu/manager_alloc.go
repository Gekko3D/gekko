package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) ensureBuffer(name string, buf **wgpu.Buffer, data []byte, usage wgpu.BufferUsage, headroom int) bool {
	neededSize := alignGpuBufferAllocationSize(uint64(len(data) + headroom))

	current := *buf
	// Always add CopySrc/CopyDst to allow resizing copies and writes
	usage = usage | wgpu.BufferUsageCopyDst | wgpu.BufferUsageCopySrc

	if current == nil || current.GetSize() < neededSize {
		// Calculate new size
		var newSize uint64 = neededSize
		if current != nil {
			// Geometric growth: grow by 1.5x
			growthSize := uint64(float64(current.GetSize()) * 1.5)
			if growthSize > newSize {
				newSize = growthSize
			}
		}
		newSize = alignGpuBufferAllocationSize(newSize)

		if newSize > SafeBufferSizeLimit {
			fmt.Printf("WARNING: Buffer %s allocation size %d exceeds safety limit %d\n", name, newSize, SafeBufferSizeLimit)
		}

		desc := &wgpu.BufferDescriptor{
			Label:            name,
			Size:             newSize,
			Usage:            usage,
			MappedAtCreation: false,
		}
		newBuf, err := m.Device.CreateBuffer(desc)
		if err != nil {
			panic(err)
		}

		// If we are resizing an existing buffer AND not overwriting it strictly (data == nil),
		// we must preserve the old content.
		if current != nil && data == nil {
			encoder, err := m.Device.CreateCommandEncoder(nil)
			if err != nil {
				panic(err)
			}

			// Copy old content to new buffer
			// Size: Copy valid range. We can just copy the whole old buffer size.
			copySize := current.GetSize()
			encoder.CopyBufferToBuffer(current, 0, newBuf, 0, copySize)

			cmdBuf, err := encoder.Finish(nil)
			if err != nil {
				panic(err)
			}
			m.Device.GetQueue().Submit(cmdBuf)
		}

		m.retireBuffer(current)

		*buf = newBuf

		if len(data) > 0 {
			m.Device.GetQueue().WriteBuffer(*buf, 0, data)
		}
		return true
	} else {
		if len(data) > 0 {
			m.Device.GetQueue().WriteBuffer(*buf, 0, data)
		}
		return false
	}
}

func alignGpuBufferAllocationSize(size uint64) uint64 {
	if size < 256 {
		size = 256
	}
	if size%256 != 0 {
		size += 256 - (size % 256)
	}
	return size
}

func (m *GpuBufferManager) retireBuffer(buf *wgpu.Buffer) {
	if m == nil || buf == nil {
		return
	}
	m.retiredBuffers = append(m.retiredBuffers, retiredBuffer{
		Buffer:     buf,
		FramesLeft: RetiredBufferFrameDelay,
	})
}

func (m *GpuBufferManager) retireBindGroup(bindGroup *wgpu.BindGroup) {
	m.retireBindGroupWithBuffers(bindGroup)
}

func (m *GpuBufferManager) retireBindGroupWithBuffers(bindGroup *wgpu.BindGroup, buffers ...*wgpu.Buffer) {
	if m == nil || bindGroup == nil {
		return
	}
	pinned := nonNilBuffers(buffers)
	m.retiredBindGroups = append(m.retiredBindGroups, retiredBindGroup{
		BindGroup:  bindGroup,
		Buffers:    pinned,
		FramesLeft: RetiredBufferFrameDelay,
	})
}

func (m *GpuBufferManager) AdvanceRetiredBuffers() {
	if m == nil {
		return
	}
	m.advanceRetiredBindGroups()
	m.advanceRetiredBuffers()
}

func nonNilBuffers(buffers []*wgpu.Buffer) []*wgpu.Buffer {
	if len(buffers) == 0 {
		return nil
	}
	kept := buffers[:0]
	for _, buf := range buffers {
		if buf != nil {
			kept = append(kept, buf)
		}
	}
	return kept
}

func (m *GpuBufferManager) advanceRetiredBuffers() {
	if len(m.retiredBuffers) == 0 {
		return
	}
	kept := m.retiredBuffers[:0]
	for _, retired := range m.retiredBuffers {
		retired.FramesLeft--
		if retired.Queue != nil {
			done := false
			if m.Device != nil {
				done = m.Device.Poll(false, &wgpu.WrappedSubmissionIndex{
					Queue:           retired.Queue,
					SubmissionIndex: retired.SubmissionIndex,
				})
			}
			if done {
				if retired.Buffer != nil {
					if m.bufferReferencedByLiveBindGroup(retired.Buffer) {
						kept = append(kept, retired)
						continue
					}
					if m.bufferPinnedByRetiredBindGroup(retired.Buffer) {
						kept = append(kept, retired)
						continue
					}
					retired.Buffer.Release()
				}
				continue
			}
			kept = append(kept, retired)
			continue
		}
		if retired.FramesLeft <= 0 && retired.Queue == nil {
			if retired.Buffer != nil {
				if m.bufferReferencedByLiveBindGroup(retired.Buffer) {
					kept = append(kept, retired)
					continue
				}
				if m.bufferPinnedByRetiredBindGroup(retired.Buffer) {
					kept = append(kept, retired)
					continue
				}
				retired.Buffer.Release()
			}
			continue
		}
		kept = append(kept, retired)
	}
	for i := len(kept); i < len(m.retiredBuffers); i++ {
		m.retiredBuffers[i] = retiredBuffer{}
	}
	m.retiredBuffers = kept
}

func (m *GpuBufferManager) bufferPinnedByRetiredBindGroup(buffer *wgpu.Buffer) bool {
	if m == nil || buffer == nil {
		return false
	}
	for _, retired := range m.retiredBindGroups {
		for _, pinned := range retired.Buffers {
			if pinned == buffer {
				return true
			}
		}
	}
	return false
}

func (m *GpuBufferManager) bufferReferencedByLiveBindGroup(buffer *wgpu.Buffer) bool {
	if m == nil || buffer == nil {
		return false
	}
	liveBuffers := []*wgpu.Buffer{
		m.gBufferBG0Camera,
		m.gBufferBG0Instances,
		m.gBufferBG0BVHNodes,
		m.transparentBG0Camera,
		m.transparentBG0Instances,
		m.transparentBG0BVHNodes,
		m.transparentBG0Lights,
		m.transparentBG0ShadowLayerParam,
	}
	for _, live := range liveBuffers {
		if live == buffer {
			return true
		}
	}
	return false
}

func (m *GpuBufferManager) advanceRetiredBindGroups() {
	if len(m.retiredBindGroups) == 0 {
		return
	}
	kept := m.retiredBindGroups[:0]
	for _, retired := range m.retiredBindGroups {
		retired.FramesLeft--
		if retired.Queue != nil {
			done := false
			if m.Device != nil {
				done = m.Device.Poll(false, &wgpu.WrappedSubmissionIndex{
					Queue:           retired.Queue,
					SubmissionIndex: retired.SubmissionIndex,
				})
			}
			if done {
				if retired.BindGroup != nil {
					if m.bindGroupReferencedByLiveState(retired.BindGroup) {
						kept = append(kept, retired)
						continue
					}
					retired.BindGroup.Release()
				}
				continue
			}
			kept = append(kept, retired)
			continue
		}
		if retired.FramesLeft <= 0 && retired.Queue == nil {
			if retired.BindGroup != nil {
				if m.bindGroupReferencedByLiveState(retired.BindGroup) {
					kept = append(kept, retired)
					continue
				}
				retired.BindGroup.Release()
			}
			continue
		}
		kept = append(kept, retired)
	}
	for i := len(kept); i < len(m.retiredBindGroups); i++ {
		m.retiredBindGroups[i] = retiredBindGroup{}
	}
	m.retiredBindGroups = kept
}

func (m *GpuBufferManager) bindGroupReferencedByLiveState(bindGroup *wgpu.BindGroup) bool {
	if m == nil || bindGroup == nil {
		return false
	}
	liveBindGroups := []*wgpu.BindGroup{
		m.GBufferBindGroup0,
		m.GBufferBindGroup,
		m.GBufferBindGroup2,
		m.TransparentBG0,
		m.TransparentBG1,
		m.TransparentBG2,
		m.TransparentBG3,
		m.LightingBindGroup,
		m.LightingBindGroup2,
		m.LightingBindGroupMaterial,
		m.LightingTileBindGroup,
		m.TiledLightCullBindGroup0,
		m.TiledLightCullBindGroup1,
		m.ShadowBindGroup0,
		m.ShadowBindGroup1,
		m.ShadowBindGroup2,
		m.DebugBindGroup0,
	}
	for _, live := range liveBindGroups {
		if live == bindGroup {
			return true
		}
	}
	return false
}

func (m *GpuBufferManager) MarkRetiredBuffersSubmitted(queue *wgpu.Queue, submissionIndex wgpu.SubmissionIndex) {
	if m == nil || queue == nil {
		return
	}
	for i := range m.retiredBuffers {
		if m.retiredBuffers[i].Queue != nil {
			continue
		}
		m.retiredBuffers[i].Queue = queue
		m.retiredBuffers[i].SubmissionIndex = submissionIndex
	}
	for i := range m.retiredBindGroups {
		if m.retiredBindGroups[i].Queue != nil {
			continue
		}
		m.retiredBindGroups[i].Queue = queue
		m.retiredBindGroups[i].SubmissionIndex = submissionIndex
	}
}
