package gpu

import (
	"math"
	"sort"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

const MaxWaterDisturbancesPerSurface = 8

type waterSurfaceRecord struct {
	Bounds      [4]float32
	Extents     [4]float32
	Color       [4]float32
	Absorption  [4]float32
	Flow        [4]float32
	Lighting    [4]float32
	Disturbance [4]uint32
}

type waterRippleRecord struct {
	PositionAge [4]float32
	Params      [4]float32
	Motion      [4]float32
}

type waterSurfaceParamsUniform struct {
	Header  [4]uint32
	Params0 [4]float32
}

func (m *GpuBufferManager) UpdateWaterSurfaces(waters []WaterSurfaceHost, ripples []WaterRippleHost, dt float32) bool {
	if dt > 0 {
		m.WaterElapsedTime += dt
	}
	records, rippleRecords, packedRippleCount, droppedRipples := buildBoundedWaterDisturbanceRecords(waters, ripples)

	recreated := false
	recBytes := unsafe.Slice((*byte)(unsafe.Pointer(&records[0])), len(records)*int(unsafe.Sizeof(waterSurfaceRecord{})))
	if m.ensureBuffer("WaterSurfaceBuf", &m.WaterSurfaceBuf, recBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	rippleBytes := unsafe.Slice((*byte)(unsafe.Pointer(&rippleRecords[0])), len(rippleRecords)*int(unsafe.Sizeof(waterRippleRecord{})))
	if m.ensureBuffer("WaterRippleBuf", &m.WaterRippleBuf, rippleBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	params := waterSurfaceParamsUniform{
		Header:  [4]uint32{uint32(len(waters)), uint32(packedRippleCount), uint32(droppedRipples), MaxWaterDisturbancesPerSurface},
		Params0: [4]float32{m.WaterElapsedTime, 0, 0, 0},
	}
	paramsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&params)), int(unsafe.Sizeof(params)))
	if m.ensureBuffer("WaterSurfaceParamsBuf", &m.WaterSurfaceParamsBuf, paramsBytes, wgpu.BufferUsageUniform, 0) {
		recreated = true
	}
	m.WaterCount = uint32(len(waters))
	m.WaterRippleCount = uint32(packedRippleCount)
	m.WaterRippleSourceCount = uint32(len(ripples))
	m.WaterRippleDroppedCount = uint32(droppedRipples)
	m.WaterSurfaces = append(m.WaterSurfaces[:0], waters...)
	m.WaterBindingsDirty = m.WaterShouldDirtyBindings(recreated)
	return recreated
}

func (m *GpuBufferManager) WaterBindGroupsMissing() bool {
	return m == nil || m.WaterBG0 == nil || m.WaterBG1 == nil || m.WaterBG2 == nil || m.WaterBG3 == nil
}

func (m *GpuBufferManager) WaterShouldDirtyBindings(recreated bool) bool {
	return recreated || m.WaterBindGroupsMissing()
}

type waterRippleCandidate struct {
	ripple  WaterRippleHost
	score   float32
	ordinal int
}

func buildBoundedWaterDisturbanceRecords(waters []WaterSurfaceHost, ripples []WaterRippleHost) ([]waterSurfaceRecord, []waterRippleRecord, int, int) {
	records := make([]waterSurfaceRecord, max(1, len(waters)))
	if len(waters) == 0 {
		return records, buildWaterRippleRecords(nil), 0, len(ripples)
	}

	byWater := make([][]waterRippleCandidate, len(waters))
	dropped := 0
	for i, ripple := range ripples {
		if ripple.WaterIndex >= uint32(len(waters)) || ripple.Lifetime <= 0 || ripple.Age >= ripple.Lifetime {
			dropped++
			continue
		}
		candidate := waterRippleCandidate{
			ripple:  ripple,
			score:   waterRipplePriority(ripple),
			ordinal: i,
		}
		byWater[ripple.WaterIndex] = append(byWater[ripple.WaterIndex], candidate)
	}

	packedRipples := make([]WaterRippleHost, 0, min(len(ripples), len(waters)*MaxWaterDisturbancesPerSurface))
	for i, water := range waters {
		candidates := byWater[i]
		if len(candidates) > MaxWaterDisturbancesPerSurface {
			sort.SliceStable(candidates, func(a, b int) bool {
				if candidates[a].score == candidates[b].score {
					return candidates[a].ordinal < candidates[b].ordinal
				}
				return candidates[a].score > candidates[b].score
			})
			dropped += len(candidates) - MaxWaterDisturbancesPerSurface
			candidates = candidates[:MaxWaterDisturbancesPerSurface]
		}
		sort.SliceStable(candidates, func(a, b int) bool {
			if candidates[a].ripple.Age == candidates[b].ripple.Age {
				return candidates[a].ordinal < candidates[b].ordinal
			}
			return candidates[a].ripple.Age < candidates[b].ripple.Age
		})

		start := len(packedRipples)
		for _, candidate := range candidates {
			ripple := candidate.ripple
			ripple.WaterIndex = uint32(i)
			packedRipples = append(packedRipples, ripple)
		}
		records[i] = buildWaterSurfaceRecord(water, uint32(start), uint32(len(packedRipples)-start))
	}

	return records, buildWaterRippleRecords(packedRipples), len(packedRipples), dropped
}

func waterRipplePriority(ripple WaterRippleHost) float32 {
	lifetime := ripple.Lifetime
	if lifetime <= 0 {
		return 0
	}
	lifeT := clampf32(ripple.Age/lifetime, 0, 1)
	fade := (1 - lifeT) * (1 - lifeT)
	speed := float32(math.Hypot(float64(ripple.HorizontalVelocity[0]), float64(ripple.HorizontalVelocity[1])))
	return ripple.Strength*fade + ripple.Foam*0.45 + ripple.Radius*0.12 + clampf32(speed/12, 0, 1)*0.18
}

func clampf32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func buildWaterSurfaceRecords(waters []WaterSurfaceHost, ripples []WaterRippleHost) []waterSurfaceRecord {
	records, _, _, _ := buildBoundedWaterDisturbanceRecords(waters, ripples)
	return records
}

func buildWaterSurfaceRecord(water WaterSurfaceHost, disturbanceStart, disturbanceCount uint32) waterSurfaceRecord {
	return waterSurfaceRecord{
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
			water.VisualCellSize,
		},
		Lighting: [4]float32{
			1 - clampf32(water.DirectLightOcclusion, 0, 1),
			0,
			0,
			0,
		},
		Disturbance: [4]uint32{
			disturbanceStart,
			disturbanceCount,
			0,
			0,
		},
	}
}

func buildWaterRippleRecords(ripples []WaterRippleHost) []waterRippleRecord {
	rippleRecords := make([]waterRippleRecord, max(1, len(ripples)))
	for i, ripple := range ripples {
		rippleRecords[i] = waterRippleRecord{
			PositionAge: [4]float32{
				ripple.Position.X(),
				ripple.Position.Y(),
				ripple.Position.Z(),
				ripple.Age,
			},
			Params: [4]float32{
				ripple.Strength,
				ripple.Lifetime,
				float32(ripple.WaterIndex),
				float32(ripple.DisturbanceKind),
			},
			Motion: [4]float32{
				ripple.HorizontalVelocity[0],
				ripple.HorizontalVelocity[1],
				ripple.Radius,
				ripple.Foam,
			},
		}
	}
	return rippleRecords
}

func (m *GpuBufferManager) CreateWaterBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil ||
		m.CameraBuf == nil ||
		m.LightsBuf == nil ||
		m.WaterSurfaceParamsBuf == nil ||
		m.WaterSurfaceBuf == nil ||
		m.WaterRippleBuf == nil ||
		m.DepthView == nil ||
		m.StorageView == nil ||
		m.TileLightParamsBuf == nil ||
		m.TileLightHeadersBuf == nil ||
		m.TileLightIndicesBuf == nil {
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
			{Binding: 2, Buffer: m.WaterRippleBuf, Size: wgpu.WholeSize},
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

	m.WaterBG3, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(3),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.TileLightParamsBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.TileLightHeadersBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.TileLightIndicesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.WaterBindingsDirty = false
}
