package gpu

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/cogentcore/webgpu/wgpu"
)

func (m *GpuBufferManager) SetupHiZ(width, height uint32, hizModule *wgpu.ShaderModule) {
	// Release old if any
	if m.HiZTexture != nil {
		m.HiZTexture.Release()
	}
	if m.ReadbackBuffer != nil {
		m.ReadbackBuffer.Release()
	}
	for _, v := range m.HiZViews {
		v.Release()
	}
	m.HiZViews = nil
	m.HiZBindGroups = nil

	// Compute mips count.
	mips := 0
	dim := width
	if height > dim {
		dim = height
	}
	for dim > 0 {
		mips++
		dim >>= 1
	}

	// Create Texture
	var err error
	m.HiZTexture, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Hi-Z Texture",
		Size:          wgpu.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
		MipLevelCount: uint32(mips),
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatR32Float,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageStorageBinding | wgpu.TextureUsageCopySrc,
	})
	if err != nil {
		panic(err)
	}

	// Create Mip Views
	m.HiZViews = make([]*wgpu.TextureView, mips)
	for i := 0; i < mips; i++ {
		m.HiZViews[i], err = m.HiZTexture.CreateView(&wgpu.TextureViewDescriptor{
			Label:           fmt.Sprintf("Hi-Z Mip %d", i),
			Format:          wgpu.TextureFormatR32Float,
			Dimension:       wgpu.TextureViewDimension2D,
			BaseMipLevel:    uint32(i),
			MipLevelCount:   1,
			BaseArrayLayer:  0,
			ArrayLayerCount: 1,
		})
		if err != nil {
			panic(err)
		}
	}

	// Create Readback Buffer
	// We want a mip level roughly 64 wide for CPU culling.
	targetW := uint32(64)
	readbackLevel := 0
	currW, currH := width, height
	for readbackLevel < mips-1 && currW > targetW {
		readbackLevel++
		currW >>= 1
		currH >>= 1
	}
	if currW < 1 {
		currW = 1
	}
	if currH < 1 {
		currH = 1
	}

	m.HiZReadbackLevel = uint32(readbackLevel)
	m.HiZReadbackWidth = currW
	m.HiZReadbackHeight = currH

	// Size: Width * Height * 4 bytes (R32F)
	// Aligned to 256 bytes per row for CopyTextureToBuffer
	bytesPerRow := (currW*4 + 255) & ^uint32(255)
	size := uint64(bytesPerRow * currH)

	m.ReadbackBuffer, err = m.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Hi-Z Readback",
		Size:  size,
		Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead,
	})
	if err != nil {
		panic(err)
	}

	// Create Pipeline if needed
	if m.HiZPipeline == nil {
		m.HiZPipeline, err = m.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
			Label: "Hi-Z Pipeline",
			Compute: wgpu.ProgrammableStageDescriptor{
				Module:     hizModule,
				EntryPoint: "main",
			},
		})
		if err != nil {
			panic(err)
		}
	}
}

// DispatchHiZ generates the Hi-Z mip chain.
func (m *GpuBufferManager) DispatchHiZ(encoder *wgpu.CommandEncoder, sourceDepthView *wgpu.TextureView) {
	if m.HiZPipeline == nil {
		return
	}

	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(m.HiZPipeline)

	width := m.HiZTexture.GetWidth()
	height := m.HiZTexture.GetHeight()
	mips := len(m.HiZViews)

	bgl := m.HiZPipeline.GetBindGroupLayout(0)

	// Pass 0
	bg0, _ := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "HiZ Pass 0",
		Layout: bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: sourceDepthView},
			{Binding: 1, TextureView: m.HiZViews[0]},
		},
	})
	pass.SetBindGroup(0, bg0, nil)
	pass.DispatchWorkgroups((width+7)/8, (height+7)/8, 1)

	// Subsequent passes
	prevW, prevH := width, height
	for i := 0; i < mips-1; i++ {
		src := m.HiZViews[i]   // MIP i
		dst := m.HiZViews[i+1] // MIP i+1

		// Downsample target size
		w := prevW >> 1
		h := prevH >> 1
		if w == 0 {
			w = 1
		}
		if h == 0 {
			h = 1
		}

		bg, _ := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  fmt.Sprintf("HiZ Pass %d", i+1),
			Layout: bgl,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, TextureView: src},
				{Binding: 1, TextureView: dst},
			},
		})
		pass.SetBindGroup(0, bg, nil)
		pass.DispatchWorkgroups((w+7)/8, (h+7)/8, 1)

		prevW, prevH = w, h
	}
	pass.End()

	// Issue Copy to Readback
	level := m.HiZReadbackLevel
	w := m.HiZReadbackWidth
	h := m.HiZReadbackHeight
	bytesPerRow := (w*4 + 255) & ^uint32(255)

	encoder.CopyTextureToBuffer(
		&wgpu.ImageCopyTexture{
			Texture:  m.HiZTexture,
			MipLevel: level,
			Origin:   wgpu.Origin3D{0, 0, 0},
		},
		&wgpu.ImageCopyBuffer{
			Buffer: m.ReadbackBuffer,
			Layout: wgpu.TextureDataLayout{
				Offset:       0,
				BytesPerRow:  bytesPerRow,
				RowsPerImage: h,
			},
		},
		&wgpu.Extent3D{Width: w, Height: h, DepthOrArrayLayers: 1},
	)
}

func (m *GpuBufferManager) ReadbackHiZ() ([]float32, uint32, uint32) {
	if m.ReadbackBuffer == nil {
		return nil, 0, 0
	}

	if !m.HiZMapped {
		// MapAsync expects uint64
		m.ReadbackBuffer.MapAsync(wgpu.MapModeRead, 0, m.ReadbackBuffer.GetSize(), func(status wgpu.BufferMapAsyncStatus) {
			if status == wgpu.BufferMapAsyncStatusSuccess {
				m.HiZMapped = true
			} else {
				fmt.Printf("HiZ MapAsync failed: %d\n", status)
			}
		})
	}

	m.Device.Poll(false, nil)

	if m.HiZMapped {
		size := m.ReadbackBuffer.GetSize()
		// GetMappedRange expects uint (based on error logs)
		// Or maybe offset is uint64 and size is uint?
		// "cannot use size (uint64) as uint" implies standard casting fixes it.
		data := m.ReadbackBuffer.GetMappedRange(0, uint(size))

		// Copy data out because Unmap invalidates it
		w := m.HiZReadbackWidth
		h := m.HiZReadbackHeight
		bytesPerRow := (w*4 + 255) & ^uint32(255)

		result := make([]float32, w*h)

		// Unpack rows
		for y := uint32(0); y < h; y++ {
			rowOffset := y * bytesPerRow
			for x := uint32(0); x < w; x++ {
				if uint64(rowOffset+x*4+4) <= size {
					valBits := binary.LittleEndian.Uint32(data[rowOffset+x*4 : rowOffset+x*4+4])
					result[y*w+x] = math.Float32frombits(valBits)
				}
			}
		}

		m.ReadbackBuffer.Unmap()
		m.HiZMapped = false
		return result, w, h
	}
	return nil, 0, 0
}
