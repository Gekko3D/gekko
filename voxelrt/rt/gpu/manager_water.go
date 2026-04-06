package gpu

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

type waterSurfaceRecord struct {
	Bounds     [4]float32
	Extents    [4]float32
	Color      [4]float32
	Absorption [4]float32
	Flow       [4]float32
}

type waterSurfaceParamsUniform struct {
	Header  [4]uint32
	Params0 [4]float32
}

func (m *GpuBufferManager) UpdateWaterSurfaces(waters []WaterSurfaceHost, dt float32) bool {
	if dt > 0 {
		m.WaterElapsedTime += dt
	}
	records := make([]waterSurfaceRecord, max(1, len(waters)))
	for i, water := range waters {
		records[i] = waterSurfaceRecord{
			Bounds: [4]float32{
				water.Position.X(),
				water.Position.Y(),
				water.Position.Z(),
				water.Depth,
			},
			Extents: [4]float32{
				water.HalfExtents[0],
				water.HalfExtents[1],
				water.Opacity,
				water.Roughness,
			},
			Color: [4]float32{
				water.Color[0],
				water.Color[1],
				water.Color[2],
				water.Refraction,
			},
			Absorption: [4]float32{
				water.AbsorptionColor[0],
				water.AbsorptionColor[1],
				water.AbsorptionColor[2],
				water.WaveAmplitude,
			},
			Flow: [4]float32{
				water.FlowDirection[0],
				water.FlowDirection[1],
				water.FlowSpeed,
				float32(water.EntityID),
			},
		}
	}

	recreated := false
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(waterSurfaceRecord{})))
	if m.ensureBuffer("WaterSurfaceBuf", &m.WaterSurfaceBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	params := waterSurfaceParamsUniform{
		Header:  [4]uint32{uint32(len(waters)), 0, 0, 0},
		Params0: [4]float32{m.WaterElapsedTime, 0, 0, 0},
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("WaterSurfaceParamsBuf", &m.WaterSurfaceParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.WaterCount = uint32(len(waters))
	m.WaterBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreateWaterBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.WaterSurfaceParamsBuf == nil || m.WaterSurfaceBuf == nil || m.DepthView == nil || m.StorageView == nil {
		return
	}

	var err error
	m.WaterBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.WaterBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.WaterSurfaceParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.WaterSurfaceBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.WaterBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.StorageView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.WaterBindingsDirty = false
}
