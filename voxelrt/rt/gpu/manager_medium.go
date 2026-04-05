package gpu

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

type analyticMediumRecord struct {
	Bounds     [4]float32
	Shape0     [4]float32
	Shape1     [4]float32
	Scatter    [4]float32
	Absorption [4]float32
	Emission   [4]float32
	Params     [4]float32
	Noise      [4]float32
	Style0     [4]float32
	Style1     [4]float32
	Style2     [4]float32
}

type analyticMediumParamsUniform struct {
	MediumCount uint32
	Pad0        uint32
	Pad1        uint32
	Pad2        uint32
}

func (m *GpuBufferManager) UpdateAnalyticMedia(media []AnalyticMediumHost) bool {
	records := make([]analyticMediumRecord, max(1, len(media)))
	for i, medium := range media {
		records[i] = analyticMediumRecord{
			Bounds: [4]float32{
				medium.Position.X(),
				medium.Position.Y(),
				medium.Position.Z(),
				medium.OuterRadius,
			},
			Shape0: [4]float32{
				medium.BoxExtents[0],
				medium.BoxExtents[1],
				medium.BoxExtents[2],
				medium.InnerRadius,
			},
			Shape1: [4]float32{
				medium.Rotation.V[0],
				medium.Rotation.V[1],
				medium.Rotation.V[2],
				medium.Rotation.W,
			},
			Scatter: [4]float32{
				medium.Color[0],
				medium.Color[1],
				medium.Color[2],
				medium.Density,
			},
			Absorption: [4]float32{
				medium.AbsorptionColor[0],
				medium.AbsorptionColor[1],
				medium.AbsorptionColor[2],
				0,
			},
			Emission: [4]float32{
				medium.EmissionColor[0],
				medium.EmissionColor[1],
				medium.EmissionColor[2],
				medium.PhaseG,
			},
			Params: [4]float32{
				medium.Falloff,
				medium.EdgeSoftness,
				medium.LightStrength,
				medium.AmbientStrength,
			},
			Noise: [4]float32{
				medium.NoiseScale,
				medium.NoiseStrength,
				float32(medium.SampleCount),
				float32(medium.Shape),
			},
			Style0: [4]float32{
				medium.LimbStrength,
				medium.LimbExponent,
				medium.DiskHazeStrength,
				medium.DiskHazeTintMix,
			},
			Style1: [4]float32{
				medium.OpaqueExtinctionScale,
				medium.BackgroundExtinctionScale,
				medium.BoundaryFadeStart,
				medium.BoundaryFadeEnd,
			},
			Style2: [4]float32{
				medium.OpaqueAlphaScale,
				medium.BackgroundAlphaScale,
				medium.OpaqueRevealScale,
				medium.BackgroundRevealScale,
			},
		}
	}

	recreated := false
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(analyticMediumRecord{})))
	if m.ensureBuffer("AnalyticMediumBuf", &m.AnalyticMediumBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	params := analyticMediumParamsUniform{MediumCount: uint32(len(media))}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("AnalyticMediumParamsBuf", &m.AnalyticMediumParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.AnalyticMediumCount = uint32(len(media))
	m.AnalyticMediumBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreateAnalyticMediumBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.LightsBuf == nil || m.AnalyticMediumParamsBuf == nil || m.AnalyticMediumBuf == nil || m.DepthView == nil {
		return
	}

	var err error
	m.AnalyticMediumBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.AnalyticMediumBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.AnalyticMediumParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.AnalyticMediumBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.AnalyticMediumBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.AnalyticMediumBindingsDirty = false
}
