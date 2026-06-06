package gpu

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) ensureBuffer(name string, buf **wgpu.Buffer, data []byte, usage wgpu.BufferUsage, headroom int) bool {
	neededSize := uint64(len(data) + headroom)
	if neededSize < 256 {
		neededSize = 256
	}
	if neededSize%256 != 0 {
		neededSize += 256 - (neededSize % 256)
	}

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

func (m *GpuBufferManager) retireBuffer(buf *wgpu.Buffer) {
	if m == nil || buf == nil {
		return
	}
	m.retiredBuffers = append(m.retiredBuffers, retiredBuffer{
		Buffer:     buf,
		FramesLeft: RetiredBufferFrameDelay,
	})
}

func (m *GpuBufferManager) AdvanceRetiredBuffers() {
	if m == nil || len(m.retiredBuffers) == 0 {
		return
	}

	kept := m.retiredBuffers[:0]
	for _, retired := range m.retiredBuffers {
		retired.FramesLeft--
		if retired.FramesLeft <= 0 {
			if retired.Buffer != nil {
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
