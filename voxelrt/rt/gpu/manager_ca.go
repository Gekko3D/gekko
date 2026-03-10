package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const caVolumeRecordSize = 192

type caVolumeRecord struct {
	LocalToWorld [16]float32
	WorldToLocal [16]float32
	SimParams    [4]float32
	RenderParams [4]float32
	ScatterColor [4]float32
	Grid         [4]float32
}

type caParamsUniform struct {
	Dt          float32
	Elapsed     float32
	VolumeCount uint32
	AtlasWidth  uint32
	AtlasHeight uint32
	AtlasDepth  uint32
	Pad1        uint32
	Pad2        uint32
	Pad3        uint32
	Pad4        uint32
	Pad5        uint32
	Pad6        uint32
}

func (m *GpuBufferManager) UpdateCAVolumes(volumes []CAVolumeHost) bool {
	layoutChanged := len(volumes) != len(m.caLayout)
	if !layoutChanged {
		for i, v := range volumes {
			prev := m.caLayout[i]
			if prev.EntityID != v.EntityID || prev.Type != v.Type || prev.Resolution != v.Resolution {
				layoutChanged = true
				break
			}
		}
	}

	atlasW, atlasH, atlasD := uint32(1), uint32(1), uint32(1)
	layout := make([]caVolumeLayout, len(volumes))
	records := make([]caVolumeRecord, len(volumes))
	zOffset := uint32(0)

	for i, v := range volumes {
		layout[i] = caVolumeLayout{EntityID: v.EntityID, Type: v.Type, Resolution: v.Resolution}
		if v.Resolution[0] > atlasW {
			atlasW = v.Resolution[0]
		}
		if v.Resolution[1] > atlasH {
			atlasH = v.Resolution[1]
		}
		atlasD += v.Resolution[2]

		scale := v.VoxelScale
		localToWorld := mgl32.Translate3D(v.Position.X(), v.Position.Y(), v.Position.Z()).
			Mul4(v.Rotation.Mat4()).
			Mul4(mgl32.Scale3D(scale.X(), scale.Y(), scale.Z()))
		worldToLocal := localToWorld.Inv()

		records[i] = caVolumeRecord{
			LocalToWorld: [16]float32(localToWorld),
			WorldToLocal: [16]float32(worldToLocal),
			SimParams: [4]float32{
				v.Diffusion,
				v.Buoyancy,
				v.Cooling,
				v.Dissipation,
			},
			RenderParams: [4]float32{
				v.Extinction,
				v.Emission,
				float32(v.Type),
				v.StepsPending,
			},
			ScatterColor: [4]float32{
				v.ScatterColor[0],
				v.ScatterColor[1],
				v.ScatterColor[2],
				v.StepDt,
			},
			Grid: [4]float32{
				float32(v.Resolution[0]),
				float32(v.Resolution[1]),
				float32(v.Resolution[2]),
				float32(zOffset),
			},
		}
		zOffset += v.Resolution[2]
	}

	recreated := false
	if layoutChanged || m.CAFieldTexA == nil || atlasW != m.CAAtlasWidth || atlasH != m.CAAtlasHeight || atlasD != m.CAAtlasDepth {
		m.caLayout = layout
		m.CAAtlasWidth = atlasW
		m.CAAtlasHeight = atlasH
		m.CAAtlasDepth = atlasD
		m.CAFieldIndex = 0
		m.createCAFieldTextures(atlasW, atlasH, atlasD)
		m.writeCASeedField(volumes, atlasW, atlasH, atlasD)
		recreated = true
	}

	if len(records) == 0 {
		records = []caVolumeRecord{{}}
	}
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*caVolumeRecordSize)
	if m.ensureBuffer("CAVolumeBuf", &m.CAVolumeBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	m.CAVolumeCount = uint32(len(volumes))
	m.CAVolumeBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) UpdateCAParams(dt float32) {
	if dt > 0 {
		m.CAElapsedTime += dt
	}
	params := caParamsUniform{
		Dt:          dt,
		Elapsed:     m.CAElapsedTime,
		VolumeCount: m.CAVolumeCount,
		AtlasWidth:  m.CAAtlasWidth,
		AtlasHeight: m.CAAtlasHeight,
		AtlasDepth:  m.CAAtlasDepth,
	}
	if m.ensureBuffer("CAParamsBuf", &m.CAParamsBuf, unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params))), wgpu.BufferUsageUniform, 0) {
		m.CAVolumeBindingsDirty = true
	}
}

func (m *GpuBufferManager) CreateCAVolumeSimBindGroups() {
	if m.CAVolumeSimPipeline == nil || m.CAParamsBuf == nil || m.CAVolumeBuf == nil || m.CAFieldViewA == nil || m.CAFieldViewB == nil {
		return
	}
	var err error
	m.CAVolumeSimBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeSimPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CAParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.CAVolumeBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeSimBG1A, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeSimPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.CAFieldViewA},
			{Binding: 1, TextureView: m.CAFieldViewB},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeSimBG1B, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeSimPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.CAFieldViewB},
			{Binding: 1, TextureView: m.CAFieldViewA},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeBindingsDirty = false
}

func (m *GpuBufferManager) CreateCAVolumeRenderBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CAParamsBuf == nil || m.CAVolumeBuf == nil || m.CAFieldViewA == nil || m.CAFieldViewB == nil || m.DepthView == nil {
		return
	}
	var err error
	m.CAVolumeRenderBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeRenderBG1A, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CAParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.CAVolumeBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.CAFieldViewA},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeRenderBG1B, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CAParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.CAVolumeBuf, Size: wgpu.WholeSize},
			{Binding: 2, TextureView: m.CAFieldViewB},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeRenderBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeBindingsDirty = false
}

func (m *GpuBufferManager) DispatchCAVolumeSim(encoder *wgpu.CommandEncoder, pipeline *wgpu.ComputePipeline) {
	if pipeline == nil || m.CAVolumeCount == 0 || m.CAVolumeSimBG0 == nil || m.CAAtlasWidth == 0 || m.CAAtlasHeight == 0 || m.CAAtlasDepth == 0 {
		return
	}

	pass := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "CA Volume Sim"})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, m.CAVolumeSimBG0, nil)
	if m.CAFieldIndex == 0 {
		pass.SetBindGroup(1, m.CAVolumeSimBG1A, nil)
	} else {
		pass.SetBindGroup(1, m.CAVolumeSimBG1B, nil)
	}
	pass.DispatchWorkgroups((m.CAAtlasWidth+3)/4, (m.CAAtlasHeight+3)/4, (m.CAAtlasDepth+3)/4)
	pass.End()
	m.CAFieldIndex = 1 - m.CAFieldIndex
}

func (m *GpuBufferManager) CurrentCAVolumeRenderBG1() *wgpu.BindGroup {
	if m.CAFieldIndex == 0 {
		return m.CAVolumeRenderBG1A
	}
	return m.CAVolumeRenderBG1B
}

func (m *GpuBufferManager) createCAFieldTextures(w, h, d uint32) {
	createField := func(label string) (*wgpu.Texture, *wgpu.TextureView) {
		tex, err := m.Device.CreateTexture(&wgpu.TextureDescriptor{
			Label:         label,
			Size:          wgpu.Extent3D{Width: maxu32(w, 1), Height: maxu32(h, 1), DepthOrArrayLayers: maxu32(d, 1)},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     wgpu.TextureDimension3D,
			Format:        wgpu.TextureFormatRGBA32Float,
			Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageStorageBinding | wgpu.TextureUsageCopyDst,
		})
		if err != nil {
			panic(err)
		}
		view, err := tex.CreateView(nil)
		if err != nil {
			panic(err)
		}
		return tex, view
	}

	if m.CAFieldTexA != nil {
		m.CAFieldTexA.Release()
	}
	if m.CAFieldTexB != nil {
		m.CAFieldTexB.Release()
	}
	m.CAFieldTexA, m.CAFieldViewA = createField("CAFieldA")
	m.CAFieldTexB, m.CAFieldViewB = createField("CAFieldB")
	m.CAVolumeBindingsDirty = true
}

func (m *GpuBufferManager) writeCASeedField(volumes []CAVolumeHost, atlasW, atlasH, atlasD uint32) {
	total := int(maxu32(atlasW, 1) * maxu32(atlasH, 1) * maxu32(atlasD, 1) * 4)
	data := make([]float32, total)

	zOffset := uint32(0)
	for _, v := range volumes {
		radius := maxf32(2, float32(minu32(v.Resolution[0], v.Resolution[2]))*0.14)
		cx := float32(v.Resolution[0]) * 0.5
		cz := float32(v.Resolution[2]) * 0.5
		for z := uint32(0); z < v.Resolution[2]; z++ {
			for y := uint32(0); y < v.Resolution[1]; y++ {
				for x := uint32(0); x < v.Resolution[0]; x++ {
					idx := (((int(zOffset+z)*int(atlasH))+int(y))*int(atlasW) + int(x)) * 4
					dx := float32(x) - cx
					dz := float32(z) - cz
					dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
					if y <= 1 && dist <= radius {
						falloff := 1 - dist/maxf32(radius, 0.001)
						if falloff < 0 {
							falloff = 0
						}
						data[idx+0] = 0.35 + 0.65*falloff
						if v.Type == 1 {
							data[idx+1] = 0.9
						}
					}
				}
			}
		}
		zOffset += v.Resolution[2]
	}

	if len(data) == 0 {
		data = []float32{0, 0, 0, 0}
	}
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4)
	layout := &wgpu.TextureDataLayout{
		Offset:       0,
		BytesPerRow:  maxu32(atlasW, 1) * 16,
		RowsPerImage: maxu32(atlasH, 1),
	}
	extent := &wgpu.Extent3D{Width: maxu32(atlasW, 1), Height: maxu32(atlasH, 1), DepthOrArrayLayers: maxu32(atlasD, 1)}
	m.Device.GetQueue().WriteTexture(m.CAFieldTexA.AsImageCopy(), bytes, layout, extent)
	m.Device.GetQueue().WriteTexture(m.CAFieldTexB.AsImageCopy(), bytes, layout, extent)
}

func maxu32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func minu32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func maxf32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
