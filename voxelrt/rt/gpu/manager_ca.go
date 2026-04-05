package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	caVolumeRecordSize       = 224
	caVolumeBoundsRecordSize = 32
	caParamsUniformSize      = 64
	CAPresetDataSize         = 128
)

type caVolumeRecord struct {
	LocalToWorld    [16]float32
	WorldToLocal    [16]float32
	SimParams       [4]float32
	RenderParams    [4]float32
	ScatterColor    [4]float32
	ShadowTint      [4]float32
	AbsorptionColor [4]float32
	Grid            [4]float32
}

type caVolumeBoundsRecord struct {
	Min [4]uint32
	Max [4]uint32
}

type caParamsUniform struct {
	Dt           float32
	Elapsed      float32
	VolumeCount  uint32
	AtlasWidth   uint32
	AtlasHeight  uint32
	AtlasDepth   uint32
	RenderWidth  uint32
	RenderHeight uint32
	Pad1         uint32
	Pad2         uint32
	Pad3         uint32
	Pad4         uint32
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
	m.caVolumes = append(m.caVolumes[:0], volumes...)
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
				float32((v.Preset << 3) | (v.Type & 0x7)),
				v.StepsPending,
			},
			ScatterColor: [4]float32{
				v.ScatterColor[0],
				v.ScatterColor[1],
				v.ScatterColor[2],
				v.StepDt,
			},
			ShadowTint: [4]float32{
				v.ShadowTint[0],
				v.ShadowTint[1],
				v.ShadowTint[2],
				v.Intensity,
			},
			AbsorptionColor: [4]float32{
				v.AbsorptionColor[0],
				v.AbsorptionColor[1],
				v.AbsorptionColor[2],
				0.0,
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
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(caVolumeRecord{})))
	if m.ensureBuffer("CAVolumeBuf", &m.CAVolumeBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	bounds := make([]caVolumeBoundsRecord, max(1, len(volumes)))
	if m.ensureBuffer("CABoundsBuf", &m.CABoundsBuf, unsafe.Slice((*byte)(unsafe.Pointer(&bounds[0])), len(bounds)*int(unsafe.Sizeof(caVolumeBoundsRecord{}))), wgpu.BufferUsageStorage, 0) {
		recreated = true
	}

	m.CAVolumeCount = uint32(len(volumes))
	m.CAVolumeBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CurrentCAVolumes() []CAVolumeHost {
	if m == nil {
		return nil
	}
	return m.caVolumes
}

func (m *GpuBufferManager) UpdateCAPresets() {
	presets := make([]CAPresetData, 8)

	// Preset 0: Default
	presets[0] = CAPresetData{
		SmokeSeed:       0.02,
		FireSeed:        0.08,
		SmokeInject:     0.14,
		FireInject:      0.45,
		Diffusion:       0.12,
		Buoyancy:        0.85,
		Cooling:         0.08,
		Dissipation:     0.02,
		SmokeDensityCut: 0.14,
		FireHeatCut:     0.04,
		SigmaTSmoke:     1.0,
		SigmaTFire:      0.32,
		AlphaScaleSmoke: 1.35,
		AlphaScaleFire:  1.35,
		AbsorptionScale: 1.0,
		ScatterScale:    1.0,
		EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
		FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
	}

	// Preset 1: Torch
	presets[1] = CAPresetData{
		SmokeSeed:       0.014,
		FireSeed:        0.12,
		SmokeInject:     0.08,
		FireInject:      0.55,
		Diffusion:       0.06,
		Buoyancy:        1.15,
		Cooling:         0.14,
		Dissipation:     0.04,
		SmokeDensityCut: 0.035,
		FireHeatCut:     0.04,
		SigmaTSmoke:     1.0,
		SigmaTFire:      0.32,
		AlphaScaleSmoke: 1.35,
		AlphaScaleFire:  1.35,
		AbsorptionScale: 1.0,
		ScatterScale:    1.0,
		EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
		FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
	}

	// Preset 2: Campfire
	presets[2] = CAPresetData{
		SmokeSeed:       0.022,
		FireSeed:        0.06,
		SmokeInject:     0.12,
		FireInject:      0.38,
		Diffusion:       0.14,
		Buoyancy:        0.72,
		Cooling:         0.06,
		Dissipation:     0.015,
		SmokeDensityCut: 0.02,
		FireHeatCut:     0.04,
		SigmaTSmoke:     1.0,
		SigmaTFire:      0.32,
		AlphaScaleSmoke: 1.35,
		AlphaScaleFire:  1.35,
		AbsorptionScale: 1.0,
		ScatterScale:    1.0,
		EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
		FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
	}

	// Preset 3: Jet Flame
	presets[3] = CAPresetData{
		SmokeSeed:       0.002,
		FireSeed:        0.18,
		SmokeInject:     0.04,
		FireInject:      1.15,
		Diffusion:       0.02,
		Buoyancy:        2.4,
		Cooling:         0.22,
		Dissipation:     0.08,
		SmokeDensityCut: 0.025,
		FireHeatCut:     0.015,
		SigmaTSmoke:     1.0,
		SigmaTFire:      1.02,
		AlphaScaleSmoke: 1.35,
		AlphaScaleFire:  1.65,
		AbsorptionScale: 0.38,
		ScatterScale:    0.1,
		EmberTint:       [4]float32{0.24, 0.34, 0.7, 1.0},
		FireCoreTint:    [4]float32{1.0, 1.0, 1.0, 1.0},
	}

	// Preset 4: Explosion
	presets[4] = CAPresetData{
		SmokeSeed:       0.015,
		FireSeed:        0.16,
		SmokeInject:     0.28,
		FireInject:      0.65,
		Diffusion:       0.14,
		Buoyancy:        1.45,
		Cooling:         0.12,
		Dissipation:     0.028,
		SmokeDensityCut: 0.012,
		FireHeatCut:     0.05,
		SigmaTSmoke:     1.1,
		SigmaTFire:      0.28,
		AlphaScaleSmoke: 1.0,
		AlphaScaleFire:  1.0,
		AbsorptionScale: 1.0,
		ScatterScale:    1.0,
		EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
		FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
	}

	m.ensureBuffer("CAPresetBuf", &m.CAPresetBuf, unsafe.Slice((*byte)(unsafe.Pointer(&presets[0])), len(presets)*int(unsafe.Sizeof(CAPresetData{}))), wgpu.BufferUsageStorage, 0)
}
func (m *GpuBufferManager) UpdateCAParams(dt float32) {
	if dt > 0 {
		m.CAElapsedTime += dt
	}
	params := caParamsUniform{
		Dt:           dt,
		Elapsed:      m.CAElapsedTime,
		VolumeCount:  m.CAVolumeCount,
		AtlasWidth:   m.CAAtlasWidth,
		AtlasHeight:  m.CAAtlasHeight,
		AtlasDepth:   m.CAAtlasDepth,
		RenderWidth:  m.VolumetricWidth,
		RenderHeight: m.VolumetricHeight,
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
			{Binding: 2, Buffer: m.CAPresetBuf, Size: wgpu.WholeSize},
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

func (m *GpuBufferManager) CreateCAVolumeBoundsBindGroups() {
	if m.CAVolumeBoundsPipeline == nil || m.CAParamsBuf == nil || m.CAVolumeBuf == nil || m.CABoundsBuf == nil || m.CAFieldViewA == nil || m.CAFieldViewB == nil {
		return
	}
	var err error
	m.CAVolumeBoundsBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeBoundsPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CAParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.CAVolumeBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.CABoundsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeBoundsBG1A, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeBoundsPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.CAFieldViewA},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeBoundsBG1B, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: m.CAVolumeBoundsPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.CAFieldViewB},
		},
	})
	if err != nil {
		panic(err)
	}
	m.CAVolumeBindingsDirty = false
}

func (m *GpuBufferManager) CreateCAVolumeRenderBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CAParamsBuf == nil || m.CAVolumeBuf == nil || m.CABoundsBuf == nil || m.CAFieldViewA == nil || m.CAFieldViewB == nil || m.DepthView == nil {
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
			{Binding: 3, Buffer: m.CABoundsBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.CAPresetBuf, Size: wgpu.WholeSize},
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
			{Binding: 3, Buffer: m.CABoundsBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.CAPresetBuf, Size: wgpu.WholeSize},
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

func (m *GpuBufferManager) DispatchCAVolumeBounds(encoder *wgpu.CommandEncoder, pipeline *wgpu.ComputePipeline) {
	if pipeline == nil || m.CAVolumeCount == 0 || m.CAVolumeBoundsBG0 == nil {
		return
	}
	pass := encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: "CA Volume Bounds"})
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, m.CAVolumeBoundsBG0, nil)
	if m.CAFieldIndex == 0 {
		pass.SetBindGroup(1, m.CAVolumeBoundsBG1A, nil)
	} else {
		pass.SetBindGroup(1, m.CAVolumeBoundsBG1B, nil)
	}
	pass.DispatchWorkgroups(maxu32(m.CAVolumeCount, 1), 1, 1)
	pass.End()
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
			Format:        wgpu.TextureFormatRGBA16Float,
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
		cx := float32(v.Resolution[0]) * 0.5
		cz := float32(v.Resolution[2]) * 0.5
		for z := uint32(0); z < v.Resolution[2]; z++ {
			for y := uint32(0); y < v.Resolution[1]; y++ {
				for x := uint32(0); x < v.Resolution[0]; x++ {
					idx := (((int(zOffset+z)*int(atlasH))+int(y))*int(atlasW) + int(x)) * 4
					mask := caSeedMask(v.Preset, v.Type, float32(x)+0.5, float32(z)+0.5, cx, cz)
					if y <= 2 && mask > 0 {
						falloff := mask * (1.0 - float32(y)*0.24)
						if falloff <= 0 {
							continue
						}
						if v.Type == 1 {
							smokeSeed := float32(0.02)
							heatSeed := float32(0.75)
							switch v.Preset {
							case 1:
								smokeSeed = 0.014
								heatSeed = 0.95
							case 2:
								smokeSeed = 0.03
								heatSeed = 0.82
							case 4:
								smokeSeed = 0.01
								heatSeed = 1.0
							case 3:
								smokeSeed = 0.008
								heatSeed = 1.0
							}
							data[idx+0] = maxf32(data[idx+0], smokeSeed+falloff*0.08)
							data[idx+1] = maxf32(data[idx+1], heatSeed*falloff)
						} else {
							data[idx+0] = maxf32(data[idx+0], 0.3+0.7*falloff)
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

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func caSeedLobe(x, z, cx, cz, rx, rz float32) float32 {
	qx := (x - cx) / maxf32(rx, 0.001)
	qz := (z - cz) / maxf32(rz, 0.001)
	d := float32(math.Sqrt(float64(qx*qx + qz*qz)))
	return maxf32(0, 1-d)
}

func caSeedBox(x, z, cx, cz, hx, hz, feather float32) float32 {
	dx := absf32(x-cx) - hx
	dz := absf32(z-cz) - hz
	edge := maxf32(dx, dz)
	if edge <= 0 {
		return 1
	}
	return maxf32(0, 1-edge/maxf32(feather, 0.001))
}

func caSeedMask(preset, volumeType uint32, x, z, cx, cz float32) float32 {
	switch preset {
	case 1: // Torch
		wick := caSeedBox(x, z, cx, cz, 0.22, 0.46, 0.3)
		emberA := caSeedLobe(x, z, cx-0.22, cz+0.08, 0.28, 0.3)
		emberB := caSeedLobe(x, z, cx+0.18, cz-0.06, 0.24, 0.26)
		return maxf32(wick, maxf32(emberA, emberB))
	case 2: // Campfire
		if volumeType == 1 {
			logA := caSeedBox(x, z, cx-1.2, cz-0.35, 1.1, 0.24, 0.36)
			logB := caSeedBox(x, z, cx+1.0, cz+0.42, 0.95, 0.24, 0.36)
			logC := caSeedBox(x, z, cx+0.1, cz-1.05, 0.82, 0.22, 0.32)
			hotA := caSeedLobe(x, z, cx-0.75, cz-0.25, 0.32, 0.28)
			hotB := caSeedLobe(x, z, cx+0.72, cz+0.28, 0.28, 0.32)
			hotC := caSeedLobe(x, z, cx+0.05, cz-0.9, 0.26, 0.34)
			return maxf32(maxf32(logA, logB), maxf32(logC, maxf32(hotA, maxf32(hotB, hotC))))
		}
		ventA := caSeedBox(x, z, cx-0.7, cz-0.18, 0.58, 0.34, 0.38)
		ventB := caSeedBox(x, z, cx+0.62, cz+0.22, 0.52, 0.34, 0.36)
		ventC := caSeedBox(x, z, cx, cz-0.82, 0.4, 0.28, 0.32)
		ventD := caSeedLobe(x, z, cx+0.2, cz+0.64, 0.34, 0.34)
		return maxf32(maxf32(ventA, ventB), maxf32(ventC, ventD))
	case 3: // Jet
		slit := caSeedBox(x, z, cx, cz, 1.35, 0.12, 0.18)
		core := caSeedBox(x, z, cx, cz, 0.9, 0.08, 0.12)
		return maxf32(slit, core)
	case 4: // Explosion
		core := caSeedLobe(x, z, cx, cz, 0.7, 0.7)
		ringA := caSeedLobe(x, z, cx-0.35, cz+0.12, 0.48, 0.42)
		ringB := caSeedLobe(x, z, cx+0.28, cz-0.18, 0.44, 0.38)
		return maxf32(core, maxf32(ringA, ringB))
	default:
		radius := maxf32(2, float32(minu32(uint32(cx*2), uint32(cz*2)))*0.14)
		dx := x - cx
		dz := z - cz
		dist := float32(math.Sqrt(float64(dx*dx + dz*dz)))
		if dist > radius {
			return 0
		}
		return maxf32(0, 1-dist/maxf32(radius, 0.001))
	}
}
