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

	// The Hi-Z hierarchy starts at half the G-Buffer resolution.
	// G-Buffer: 1920x1080
	// Hi-Z Mip 0: 960x540
	// Hi-Z Mip 1: 480x270
	// ...
	hizW := width / 2
	hizH := height / 2
	if hizW < 1 {
		hizW = 1
	}
	if hizH < 1 {
		hizH = 1
	}

	mips := 0
	dim := hizW
	if hizH > dim {
		dim = hizH
	}
	for dim > 0 {
		mips++
		dim >>= 1
	}

	// Create Texture
	var err error
	m.HiZTexture, err = m.Device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "Hi-Z Texture",
		Size:          wgpu.Extent3D{Width: hizW, Height: hizH, DepthOrArrayLayers: 1},
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

	// Create Readback Buffer (target ~64 wide)
	targetW := uint32(64)
	readbackLevel := 0
	currW, currH := hizW, hizH
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

	m.StateMu.Lock()
	m.HiZState = 0
	m.StateMu.Unlock()

	// Create BindGroupLayout explicitly
	bgl, err := m.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Hi-Z BGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				StorageTexture: wgpu.StorageTextureBindingLayout{
					Access:        wgpu.StorageTextureAccessWriteOnly,
					Format:        wgpu.TextureFormatR32Float,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}

	// Create Pipeline
	if m.HiZPipeline == nil {
		layout, _ := m.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
			BindGroupLayouts: []*wgpu.BindGroupLayout{bgl},
		})
		m.HiZPipeline, err = m.Device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
			Label:  "Hi-Z Pipeline",
			Layout: layout,
			Compute: wgpu.ProgrammableStageDescriptor{
				Module:     hizModule,
				EntryPoint: "main",
			},
		})
		if err != nil {
			panic(err)
		}
	}

	// Cache Bind Groups
	m.HiZBindGroups = make([]*wgpu.BindGroup, mips)
	// We'll populate Pass 0 (source is external) dynamically,
	// but passes 1..N (internal) can be cached.
	for i := 0; i < mips-1; i++ {
		bg, _ := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Label:  fmt.Sprintf("HiZ Pass %d (Internal)", i+1),
			Layout: bgl,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, TextureView: m.HiZViews[i]},   // Source
				{Binding: 1, TextureView: m.HiZViews[i+1]}, // Dest
			},
		})
		m.HiZBindGroups[i+1] = bg
	}
}

// DispatchHiZ generates the Hi-Z mip chain.
func (m *GpuBufferManager) DispatchHiZ(encoder *wgpu.CommandEncoder, sourceDepthView *wgpu.TextureView) {
	if m.HiZPipeline == nil {
		return
	}

	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(m.HiZPipeline)

	hizW := m.HiZTexture.GetWidth()
	hizH := m.HiZTexture.GetHeight()
	mips := len(m.HiZViews)

	bgl := m.HiZPipeline.GetBindGroupLayout(0)
	if bgl == nil {
		fmt.Printf("HiZ Error: Failed to get BindGroupLayout from pipeline\n")
		pass.End()
		return
	}

	// Pass 0: G-Buffer Depth -> HiZ Mip 0 (Downsample)
	if sourceDepthView == nil || m.HiZViews[0] == nil {
		fmt.Printf("HiZ Error: sourceDepthView=%v, Mip0View=%v\n", sourceDepthView, m.HiZViews[0])
		pass.End()
		return
	}

	bg0, err := m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "HiZ Pass 0",
		Layout: bgl,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: sourceDepthView},
			{Binding: 1, TextureView: m.HiZViews[0]},
		},
	})
	if err != nil || bg0 == nil {
		fmt.Printf("HiZ Error: Failed to create Pass 0 BindGroup: %v\n", err)
		pass.End()
		return
	}
	pass.SetBindGroup(0, bg0, nil)
	pass.DispatchWorkgroups((hizW+7)/8, (hizH+7)/8, 1)

	// Subsequent passes: Mip K -> Mip K+1
	prevW, prevH := hizW, hizH
	for i := 0; i < mips-1; i++ {
		w := prevW >> 1
		h := prevH >> 1
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}

		bg := m.HiZBindGroups[i+1]
		if bg == nil {
			// Safety if cache failed
			bg, _ = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Label:  fmt.Sprintf("HiZ Pass %d", i+1),
				Layout: bgl,
				Entries: []wgpu.BindGroupEntry{
					{Binding: 0, TextureView: m.HiZViews[i]},
					{Binding: 1, TextureView: m.HiZViews[i+1]},
				},
			})
			m.HiZBindGroups[i+1] = bg
		}
		if bg == nil {
			fmt.Printf("HiZ Error: Nil BindGroup for pass %d\n", i+1)
			continue
		}
		pass.SetBindGroup(0, bg, nil)
		pass.DispatchWorkgroups((w+7)/8, (h+7)/8, 1)

		prevW, prevH = w, h
	}
	pass.End()

	m.StateMu.Lock()
	state := m.HiZState
	m.StateMu.Unlock()

	// Issue Copy to Readback ONLY if buffer is idle
	if state != 0 {
		return
	}

	m.StateMu.Lock()
	m.HiZState = 1 // 1: Copy (GPU)
	m.StateMu.Unlock()

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
	m.HiZState = 1 // 1: Copy (GPU)
}

// ResolveHiZReadback is kept for compatibility but logic is now handled in ReadbackHiZ state machine
func (m *GpuBufferManager) ResolveHiZReadback() {}

func (m *GpuBufferManager) ReadbackHiZ() ([]float32, uint32, uint32) {
	if m.ReadbackBuffer == nil {
		return nil, 0, 0
	}

	m.StateMu.Lock()
	state := m.HiZState

	// If the GPU copy finished in a previous frame, start mapping now
	if state == 1 {
		m.HiZState = 2 // Transition to Mapping
		m.ReadbackBuffer.MapAsync(wgpu.MapModeRead, 0, m.ReadbackBuffer.GetSize(), func(status wgpu.BufferMapAsyncStatus) {
			m.StateMu.Lock()
			defer m.StateMu.Unlock()
			if status == wgpu.BufferMapAsyncStatusSuccess {
				m.HiZState = 3 // Mapped
			} else {
				m.HiZState = 0 // Error, back to idle
			}
		})
	}
	m.StateMu.Unlock()

	// Check if any mapping completed
	// m.Device.Poll(false, nil)
	m.StateMu.Lock()
	if m.HiZState == 3 {
		size := m.ReadbackBuffer.GetSize()
		data := m.ReadbackBuffer.GetMappedRange(0, uint(size))

		w := m.HiZReadbackWidth
		h := m.HiZReadbackHeight
		bytesPerRow := (w*4 + 255) & ^uint32(255)

		if len(m.LastHiZData) != int(w*h) {
			m.LastHiZData = make([]float32, w*h)
			for i := range m.LastHiZData {
				m.LastHiZData[i] = 60000.0 // Default to far
			}
		}
		m.LastHiZW = w
		m.LastHiZH = h

		// Unpack rows
		for y := uint32(0); y < h; y++ {
			rowOffset := y * bytesPerRow
			for x := uint32(0); x < w; x++ {
				if uint64(rowOffset+x*4+4) <= size {
					valBits := binary.LittleEndian.Uint32(data[rowOffset+x*4 : rowOffset+x*4+4])
					m.LastHiZData[y*w+x] = math.Float32frombits(valBits)
				}
			}
		}

		m.ReadbackBuffer.Unmap()
		m.HiZState = 0 // Transition to Idle
	}
	m.StateMu.Unlock()

	// Provide default "Far" data if none available yet
	if len(m.LastHiZData) == 0 && m.HiZReadbackWidth > 0 && m.HiZReadbackHeight > 0 {
		w, h := m.HiZReadbackWidth, m.HiZReadbackHeight
		m.LastHiZData = make([]float32, w*h)
		for i := range m.LastHiZData {
			m.LastHiZData[i] = 60000.0
		}
		m.LastHiZW = w
		m.LastHiZH = h
	}

	return m.LastHiZData, m.LastHiZW, m.LastHiZH
}
