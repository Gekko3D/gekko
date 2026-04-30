package gpu

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const MaxAstronomicalBodies = 256

type AstronomicalBodyHost struct {
	Kind                      uint32
	DirectionViewSpace        mgl32.Vec3
	AngularRadiusRad          float32
	GlowAngularRadiusRad      float32
	RingInnerAngularRadiusRad float32
	RingOuterAngularRadiusRad float32
	PhaseLight01              float32
	BodyTint                  [3]float32
	EmissionStrength          float32
	Seed                      uint32
	OcclusionPriority         int32
}

type astronomicalBodyRecord struct {
	DirectionKind [4]float32
	Angular       [4]float32
	TintEmission  [4]float32
	Meta          [4]uint32
}

type astronomicalBodyParamsUniform struct {
	BodyCount uint32
	Pad0      uint32
	Pad1      uint32
	Pad2      uint32
}

func buildAstronomicalBodyRecords(hosts []AstronomicalBodyHost) ([]astronomicalBodyRecord, astronomicalBodyParamsUniform) {
	if len(hosts) > MaxAstronomicalBodies {
		hosts = hosts[:MaxAstronomicalBodies]
	}
	records := make([]astronomicalBodyRecord, max(1, len(hosts)))
	for i, host := range hosts {
		dir := host.DirectionViewSpace
		if dir.LenSqr() > 1e-6 {
			dir = dir.Normalize()
		} else {
			dir = mgl32.Vec3{0, 0, -1}
		}
		records[i] = astronomicalBodyRecord{
			DirectionKind: [4]float32{
				dir.X(),
				dir.Y(),
				dir.Z(),
				float32(host.Kind),
			},
			Angular: [4]float32{
				clampNonNegative(host.AngularRadiusRad),
				clampNonNegative(host.GlowAngularRadiusRad),
				clampNonNegative(host.RingInnerAngularRadiusRad),
				clampNonNegative(host.RingOuterAngularRadiusRad),
			},
			TintEmission: [4]float32{
				clamp01f(host.BodyTint[0]),
				clamp01f(host.BodyTint[1]),
				clamp01f(host.BodyTint[2]),
				maxAstronomicalFloat(host.EmissionStrength, 0),
			},
			Meta: [4]uint32{
				host.Seed,
				uint32(max(host.OcclusionPriority, 0)),
				uint32(clamp01f(host.PhaseLight01) * 65535.0),
				0,
			},
		}
	}
	return records, astronomicalBodyParamsUniform{BodyCount: uint32(len(hosts))}
}

func (m *GpuBufferManager) UpdateAstronomicalBodies(hosts []AstronomicalBodyHost) bool {
	records, params := buildAstronomicalBodyRecords(hosts)
	recreated := false

	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(astronomicalBodyRecord{})))
	if m.ensureBuffer("AstronomicalBodyBuf", &m.AstronomicalBodyBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("AstronomicalBodyParamsBuf", &m.AstronomicalBodyParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.AstronomicalBodyCount = params.BodyCount
	m.AstronomicalBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreateAstronomicalBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.AstronomicalBodyParamsBuf == nil || m.AstronomicalBodyBuf == nil || m.DepthView == nil {
		return
	}

	var err error
	m.AstronomicalBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.AstronomicalBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.AstronomicalBodyParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.AstronomicalBodyBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.AstronomicalBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}
	m.AstronomicalBindingsDirty = false
}

func clampNonNegative(v float32) float32 {
	if v < 0 {
		return 0
	}
	return v
}

func clamp01f(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxAstronomicalFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
