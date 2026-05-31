package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const MaxDebrisMidfieldCells = 256

type DebrisMidfieldHost struct {
	BandID               string
	CellID               string
	AsteroidID           string
	RadialIndex          int32
	AngularIndex         int32
	VerticalIndex        int32
	PositionViewSpace    mgl32.Vec3
	PlaneNormalViewSpace mgl32.Vec3
	InnerRadiusMeters    float32
	OuterRadiusMeters    float32
	Seed                 uint32
	Tint                 [3]float32
	Opacity              float32
	DensityScale         float32
	ApproachFade         float32
	DistanceMeters       float32
	GapInnerRadius       float32
	GapOuterRadius       float32
	LightDirViewSpace    mgl32.Vec3
	ActiveHandoff        bool
	HandoffExact         bool
	HandoffRadiusMeters  float32
}

type debrisMidfieldRecord struct {
	PositionOpacity [4]float32
	NormalSeed      [4]float32
	RadiiGaps       [4]float32
	TintPad         [4]float32
	LightDirPad     [4]float32
	HandoffPad      [4]float32
}

type debrisMidfieldParamsUniform struct {
	CellCount uint32
	Pad0      uint32
	Pad1      uint32
	Pad2      uint32
}

func buildDebrisMidfieldRecords(hosts []DebrisMidfieldHost) ([]debrisMidfieldRecord, debrisMidfieldParamsUniform) {
	if len(hosts) > MaxDebrisMidfieldCells {
		hosts = hosts[:MaxDebrisMidfieldCells]
	}
	// Always allocate a fixed size of MaxDebrisMidfieldCells (512) to prevent WebGPU buffer/bindgroup recreation
	records := make([]debrisMidfieldRecord, MaxDebrisMidfieldCells)
	for i, host := range hosts {
		activeHandoff := float32(0)
		if host.ActiveHandoff {
			activeHandoff = 1
		}
		handoffExact := float32(0)
		if host.HandoffExact {
			handoffExact = 1
		}
		records[i] = debrisMidfieldRecord{
			PositionOpacity: [4]float32{
				host.PositionViewSpace.X(),
				host.PositionViewSpace.Y(),
				host.PositionViewSpace.Z(),
				host.Opacity,
			},
			NormalSeed: [4]float32{
				host.PlaneNormalViewSpace.X(),
				host.PlaneNormalViewSpace.Y(),
				host.PlaneNormalViewSpace.Z(),
				math.Float32frombits(host.Seed),
			},
			RadiiGaps: [4]float32{
				host.InnerRadiusMeters,
				host.OuterRadiusMeters,
				host.GapInnerRadius,
				host.GapOuterRadius,
			},
			TintPad: [4]float32{
				host.Tint[0],
				host.Tint[1],
				host.Tint[2],
				host.DensityScale,
			},
			LightDirPad: [4]float32{
				host.LightDirViewSpace.X(),
				host.LightDirViewSpace.Y(),
				host.LightDirViewSpace.Z(),
				host.ApproachFade,
			},
			HandoffPad: [4]float32{
				activeHandoff,
				handoffExact,
				host.HandoffRadiusMeters,
				0,
			},
		}
	}
	return records, debrisMidfieldParamsUniform{CellCount: uint32(len(hosts))}
}

func (m *GpuBufferManager) UpdateDebrisMidfieldCells(hosts []DebrisMidfieldHost) bool {
	records, params := buildDebrisMidfieldRecords(hosts)
	recreated := false

	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(debrisMidfieldRecord{})))
	if m.ensureBuffer("DebrisMidfieldBuf", &m.DebrisMidfieldBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("DebrisMidfieldParamsBuf", &m.DebrisMidfieldParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.DebrisMidfieldCount = params.CellCount
	m.DebrisMidfieldBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreateDebrisMidfieldBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.DebrisMidfieldParamsBuf == nil || m.DebrisMidfieldBuf == nil || m.DepthView == nil || m.PlanetDepthView == nil {
		return
	}

	var err error
	m.DebrisMidfieldBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.DebrisMidfieldBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.DebrisMidfieldParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.DebrisMidfieldBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.DebrisMidfieldBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.PlanetDepthView},
		},
	})
	if err != nil {
		panic(err)
	}
	m.DebrisMidfieldBindingsDirty = false
}
