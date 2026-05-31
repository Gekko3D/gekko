package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const MaxFarPlanetRings = 128

type FarPlanetRingHost struct {
	BandID                           string
	ParentBodyID                     string
	CenterCameraRelativeMeters       mgl32.Vec3
	NormalCameraRelative             mgl32.Vec3
	TangentUCameraRelative           mgl32.Vec3
	TangentVCameraRelative           mgl32.Vec3
	InnerRadiusMeters                float32
	OuterRadiusMeters                float32
	HalfThicknessMeters              float32
	Tint                             [3]float32
	Opacity                          float32
	DustHazeOpacity                  float32
	DustHazeMaxAlpha                 float32
	DustHazeThicknessScale           float32
	DustHazeMinHalfThicknessMeters   float32
	DustHazeRadialEdgeFadeFraction   float32
	DustHazeVerticalCoreFraction     float32
	DustHazeSampleCount              float32
	DustHazeForwardScatterStrength   float32
	DustHazeShadowStrength           float32
	Seed                             uint32
	RadialOpacityProfile             [32]float32
	ParentCenterCameraRelativeMeters mgl32.Vec3
	ParentRadiusMeters               float32
	ParentDepthMeters                float32
	LightDirectionViewSpace          mgl32.Vec3
}

type farPlanetRingRecord struct {
	CenterOpacity    [4]float32
	NormalThickness  [4]float32
	TangentUInner    [4]float32
	TangentVOuter    [4]float32
	TintSeed         [4]float32
	ParentRadius     [4]float32
	ParentDepthLight [4]float32
	DustHazeParams   [4]float32
	DustHazeLighting [4]float32
	Profile0         [4]float32
	Profile1         [4]float32
	Profile2         [4]float32
	Profile3         [4]float32
	Profile4         [4]float32
	Profile5         [4]float32
	Profile6         [4]float32
	Profile7         [4]float32
}

type farPlanetRingParamsUniform struct {
	RingCount uint32
	Pad0      uint32
	Pad1      uint32
	Pad2      uint32
}

func buildFarPlanetRingRecords(hosts []FarPlanetRingHost) ([]farPlanetRingRecord, farPlanetRingParamsUniform) {
	if len(hosts) > MaxFarPlanetRings {
		hosts = hosts[:MaxFarPlanetRings]
	}
	records := make([]farPlanetRingRecord, MaxFarPlanetRings)
	for i, host := range hosts {
		normal := normalizedVec3Or(host.NormalCameraRelative, mgl32.Vec3{0, 1, 0})
		tangentU := normalizedVec3Or(host.TangentUCameraRelative, mgl32.Vec3{1, 0, 0})
		tangentV := normalizedVec3Or(host.TangentVCameraRelative, mgl32.Vec3{0, 0, -1})
		lightDir := normalizedVec3Or(host.LightDirectionViewSpace, mgl32.Vec3{0, 1, 0})
		records[i] = farPlanetRingRecord{
			CenterOpacity: [4]float32{
				host.CenterCameraRelativeMeters.X(),
				host.CenterCameraRelativeMeters.Y(),
				host.CenterCameraRelativeMeters.Z(),
				clamp01f(host.Opacity),
			},
			NormalThickness: [4]float32{
				normal.X(),
				normal.Y(),
				normal.Z(),
				clampNonNegative(host.HalfThicknessMeters),
			},
			TangentUInner: [4]float32{
				tangentU.X(),
				tangentU.Y(),
				tangentU.Z(),
				clampNonNegative(host.InnerRadiusMeters),
			},
			TangentVOuter: [4]float32{
				tangentV.X(),
				tangentV.Y(),
				tangentV.Z(),
				clampNonNegative(host.OuterRadiusMeters),
			},
			TintSeed: [4]float32{
				clamp01f(host.Tint[0]),
				clamp01f(host.Tint[1]),
				clamp01f(host.Tint[2]),
				math.Float32frombits(host.Seed),
			},
			ParentRadius: [4]float32{
				host.ParentCenterCameraRelativeMeters.X(),
				host.ParentCenterCameraRelativeMeters.Y(),
				host.ParentCenterCameraRelativeMeters.Z(),
				clampNonNegative(host.ParentRadiusMeters),
			},
			ParentDepthLight: [4]float32{
				clamp01f(host.DustHazeOpacity),
				lightDir.X(),
				lightDir.Y(),
				lightDir.Z(),
			},
			DustHazeParams: [4]float32{
				clamp01f(host.DustHazeMaxAlpha),
				clampNonNegative(host.DustHazeThicknessScale),
				clampNonNegative(host.DustHazeMinHalfThicknessMeters),
				clamp01f(host.DustHazeRadialEdgeFadeFraction),
			},
			DustHazeLighting: [4]float32{
				clamp01f(host.DustHazeVerticalCoreFraction),
				clampNonNegative(host.DustHazeSampleCount),
				clamp01f(host.DustHazeForwardScatterStrength),
				clamp01f(host.DustHazeShadowStrength),
			},
			Profile0: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 0),
			Profile1: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 4),
			Profile2: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 8),
			Profile3: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 12),
			Profile4: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 16),
			Profile5: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 20),
			Profile6: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 24),
			Profile7: packFarPlanetRingProfileQuad(host.RadialOpacityProfile, 28),
		}
		records[i].TintSeed[3] = math.Float32frombits(host.Seed)
		records[i].ParentRadius[3] = clampNonNegative(host.ParentRadiusMeters)
	}
	return records, farPlanetRingParamsUniform{RingCount: uint32(len(hosts))}
}

func packFarPlanetRingProfileQuad(profile [32]float32, offset int) [4]float32 {
	return [4]float32{
		clamp01f(profile[offset]),
		clamp01f(profile[offset+1]),
		clamp01f(profile[offset+2]),
		clamp01f(profile[offset+3]),
	}
}

func (m *GpuBufferManager) UpdateFarPlanetRings(hosts []FarPlanetRingHost) bool {
	records, params := buildFarPlanetRingRecords(hosts)
	recreated := false
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(farPlanetRingRecord{})))
	if m.ensureBuffer("FarPlanetRingBuf", &m.FarPlanetRingBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("FarPlanetRingParamsBuf", &m.FarPlanetRingParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.FarPlanetRingCount = params.RingCount
	m.FarPlanetRingBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreateFarPlanetRingBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.FarPlanetRingParamsBuf == nil || m.FarPlanetRingBuf == nil || m.DepthView == nil || m.PlanetDepthView == nil {
		return
	}

	var err error
	m.FarPlanetRingBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.FarPlanetRingBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.FarPlanetRingParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.FarPlanetRingBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
	m.FarPlanetRingBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.PlanetDepthView},
		},
	})
	if err != nil {
		panic(err)
	}
	m.FarPlanetRingBindingsDirty = false
}

func normalizedVec3Or(v, fallback mgl32.Vec3) mgl32.Vec3 {
	if v.LenSqr() <= 1e-8 {
		return fallback
	}
	return v.Normalize()
}
