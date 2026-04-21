package gpu

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

type PlanetBodyHost struct {
	EntityID               uint32
	Seed                   uint32
	Position               mgl32.Vec3
	Rotation               mgl32.Quat
	Radius                 float32
	OceanRadius            float32
	AtmosphereRadius       float32
	AtmosphereRimWidth     float32
	HeightAmplitude        float32
	NoiseScale             float32
	BlockSize              float32
	HeightSteps            uint32
	HandoffNearAlt         float32
	HandoffFarAlt          float32
	BiomeMix               float32
	BakedSurfaceResolution uint32
	BakedSurfaceSamples    []PlanetBakedSurfaceSampleHost
	BandColors             [6][3]float32
	AmbientStrength        float32
	DiffuseStrength        float32
	SpecularStrength       float32
	RimStrength            float32
	EmissionStrength       float32
	TerrainLowColor        [3]float32
	TerrainHighColor       [3]float32
	RockColor              [3]float32
	OceanDeepColor         [3]float32
	OceanShallowColor      [3]float32
	AtmosphereColor        [3]float32
}

type PlanetBakedSurfaceSampleHost struct {
	Height       float32
	NormalOctX   float32
	NormalOctY   float32
	MaterialBand float32
}

type planetBodyRecord struct {
	Bounds     [4]float32
	Rotation   [4]float32
	Surface    [4]float32
	Noise      [4]float32
	Style      [4]float32
	Emission   [4]float32
	BakeMeta   [4]uint32
	Band0      [4]float32
	Band1      [4]float32
	Band2      [4]float32
	Band3      [4]float32
	Band4      [4]float32
	Band5      [4]float32
	Atmosphere [4]float32
}

type planetBodyParamsUniform struct {
	PlanetCount uint32
	Pad0        uint32
	Pad1        uint32
	Pad2        uint32
}

const (
	planetBodyBakeMinResolution = 2
	planetBodyBakeMaxResolution = 1024
	planetBodyBakeFaceCount     = 6
)

func normalizePlanetBakedSurfaceData(resolution uint32, samples []PlanetBakedSurfaceSampleHost) (uint32, int) {
	if resolution < planetBodyBakeMinResolution {
		return 0, 0
	}
	if resolution > planetBodyBakeMaxResolution {
		resolution = planetBodyBakeMaxResolution
	}
	sampleCount := int(resolution) * int(resolution) * planetBodyBakeFaceCount
	if len(samples) < sampleCount {
		return 0, 0
	}
	return resolution, sampleCount
}

func appendPlanetBakedSurfaceSamples(dst []PlanetBakedSurfaceSampleHost, src []PlanetBakedSurfaceSampleHost, count int) []PlanetBakedSurfaceSampleHost {
	for i := 0; i < count; i++ {
		sample := src[i]
		if sample.Height < -1 {
			sample.Height = -1
		}
		if sample.Height > 1 {
			sample.Height = 1
		}
		if sample.NormalOctX < -1 {
			sample.NormalOctX = -1
		}
		if sample.NormalOctX > 1 {
			sample.NormalOctX = 1
		}
		if sample.NormalOctY < -1 {
			sample.NormalOctY = -1
		}
		if sample.NormalOctY > 1 {
			sample.NormalOctY = 1
		}
		if sample.MaterialBand < 0 {
			sample.MaterialBand = 0
		}
		if sample.MaterialBand > 5 {
			sample.MaterialBand = 5
		}
		dst = append(dst, sample)
	}
	return dst
}

func buildPlanetBodyRecords(planets []PlanetBodyHost) ([]planetBodyRecord, []PlanetBakedSurfaceSampleHost, planetBodyParamsUniform) {
	records := make([]planetBodyRecord, max(1, len(planets)))
	surfaceSamples := make([]PlanetBakedSurfaceSampleHost, 1)
	for i, planet := range planets {
		bakeResolution, bakeCount := normalizePlanetBakedSurfaceData(planet.BakedSurfaceResolution, planet.BakedSurfaceSamples)
		bakeOffset := uint32(0)
		if bakeResolution > 0 {
			bakeOffset = uint32(len(surfaceSamples))
			surfaceSamples = appendPlanetBakedSurfaceSamples(surfaceSamples, planet.BakedSurfaceSamples, bakeCount)
		}
		records[i] = planetBodyRecord{
			Bounds: [4]float32{
				planet.Position.X(),
				planet.Position.Y(),
				planet.Position.Z(),
				planet.Radius,
			},
			Rotation: [4]float32{
				planet.Rotation.V[0],
				planet.Rotation.V[1],
				planet.Rotation.V[2],
				planet.Rotation.W,
			},
			Surface: [4]float32{
				planet.OceanRadius,
				planet.AtmosphereRadius,
				planet.HeightAmplitude,
				planet.BlockSize,
			},
			Noise: [4]float32{
				planet.NoiseScale,
				float32(planet.HeightSteps),
				float32(planet.Seed),
				planet.BiomeMix,
			},
			Style: [4]float32{
				planet.AmbientStrength,
				planet.DiffuseStrength,
				planet.SpecularStrength,
				planet.RimStrength,
			},
			Emission: [4]float32{
				planet.EmissionStrength,
				0,
				0,
				0,
			},
			BakeMeta: [4]uint32{
				bakeResolution,
				bakeOffset,
				uint32(bakeCount),
				0,
			},
			Band0: [4]float32{planet.BandColors[0][0], planet.BandColors[0][1], planet.BandColors[0][2], 0},
			Band1: [4]float32{planet.BandColors[1][0], planet.BandColors[1][1], planet.BandColors[1][2], 0},
			Band2: [4]float32{planet.BandColors[2][0], planet.BandColors[2][1], planet.BandColors[2][2], 0},
			Band3: [4]float32{planet.BandColors[3][0], planet.BandColors[3][1], planet.BandColors[3][2], 0},
			Band4: [4]float32{planet.BandColors[4][0], planet.BandColors[4][1], planet.BandColors[4][2], 0},
			Band5: [4]float32{planet.BandColors[5][0], planet.BandColors[5][1], planet.BandColors[5][2], planet.HandoffFarAlt},
			Atmosphere: [4]float32{
				planet.AtmosphereColor[0],
				planet.AtmosphereColor[1],
				planet.AtmosphereColor[2],
				planet.AtmosphereRimWidth,
			},
		}
	}
	return records, surfaceSamples, planetBodyParamsUniform{PlanetCount: uint32(len(planets))}
}

func (m *GpuBufferManager) UpdatePlanetBodies(planets []PlanetBodyHost) bool {
	records, surfaceSamples, params := buildPlanetBodyRecords(planets)
	recreated := false

	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(planetBodyRecord{})))
	if m.ensureBuffer("PlanetBodyBuf", &m.PlanetBodyBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	surfaceBytes := unsafe.Slice((*byte)(unsafe.Pointer(&surfaceSamples[0])), len(surfaceSamples)*int(unsafe.Sizeof(PlanetBakedSurfaceSampleHost{})))
	if m.ensureBuffer("PlanetBodySurfaceBuf", &m.PlanetBodySurfaceBuf, surfaceBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("PlanetBodyParamsBuf", &m.PlanetBodyParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.PlanetBodyCount = uint32(len(planets))
	m.PlanetBodyBindingsDirty = true
	return recreated
}

func (m *GpuBufferManager) CreatePlanetBodyBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil || m.CameraBuf == nil || m.LightsBuf == nil || m.PlanetBodyParamsBuf == nil || m.PlanetBodyBuf == nil || m.PlanetBodySurfaceBuf == nil || m.DepthView == nil {
		return
	}

	var err error
	m.PlanetBodyBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.PlanetBodyBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.PlanetBodyParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.PlanetBodyBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.PlanetBodySurfaceBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.PlanetBodyBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.PlanetBodyBindingsDirty = false
}
