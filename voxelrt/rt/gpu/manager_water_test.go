package gpu

import (
	"math"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildWaterSurfaceRecordsPacksVisualCellSizeAndDisturbanceRanges(t *testing.T) {
	records := buildWaterSurfaceRecords([]WaterSurfaceHost{
		{
			EntityID:             7,
			Position:             mgl32.Vec3{1, 2, 3},
			HalfExtents:          [2]float32{4, 5},
			Depth:                6,
			Color:                [3]float32{0.1, 0.2, 0.3},
			AbsorptionColor:      [3]float32{0.4, 0.5, 0.6},
			Opacity:              0.7,
			Roughness:            0.8,
			Refraction:           0.9,
			DirectLightOcclusion: 0.6,
			FlowDirection:        [2]float32{0, 1},
			FlowSpeed:            2,
			WaveAmplitude:        0.25,
			VisualCellSize:       0.35,
			EdgeMask:             0b1010,
			ShapeKind:            1,
		},
		{EntityID: 8},
	}, []WaterRippleHost{
		{WaterIndex: 0, Strength: 0.4, Lifetime: 1},
		{WaterIndex: 0, Strength: 0.5, Lifetime: 1},
		{WaterIndex: 1, Strength: 0.6, Lifetime: 1},
	})

	if len(records) != 2 {
		t.Fatalf("expected two water records, got %d", len(records))
	}
	if got := records[0].Flow[3]; got != 0.35 {
		t.Fatalf("expected visual cell size packed into flow.w, got %v", got)
	}
	if got := records[0].Lighting[0]; math.Abs(float64(got-0.4)) > 1e-6 {
		t.Fatalf("expected direct light exposure 0.4 packed into lighting.x, got %v", got)
	}
	if got := records[0].Disturbance; got != ([4]uint32{0, 2, 0b1010, 1}) {
		t.Fatalf("expected first water disturbance range [0 2], got %v", got)
	}
	if got := records[1].Disturbance; got != ([4]uint32{2, 1, 0, 0}) {
		t.Fatalf("expected second water disturbance range [2 1], got %v", got)
	}
}

func TestBuildWaterSurfaceRecordsKeepsDummyRecordForEmptyInput(t *testing.T) {
	records := buildWaterSurfaceRecords(nil, nil)
	if len(records) != 1 {
		t.Fatalf("expected one dummy water record, got %d", len(records))
	}
}

func TestBuildBoundedWaterDisturbanceRecordsCapsAndPrioritizesPerSurface(t *testing.T) {
	waters := []WaterSurfaceHost{{EntityID: 1}, {EntityID: 2}}
	ripples := make([]WaterRippleHost, 0, MaxWaterDisturbancesPerSurface+3)
	for i := 0; i < MaxWaterDisturbancesPerSurface+2; i++ {
		ripples = append(ripples, WaterRippleHost{
			WaterIndex: 0,
			Position:   mgl32.Vec3{float32(i), 0, 0},
			Strength:   0.1,
			Age:        0.2,
			Lifetime:   2,
			Radius:     0.1,
		})
	}
	ripples = append(ripples,
		WaterRippleHost{
			WaterIndex: 0,
			Position:   mgl32.Vec3{99, 0, 0},
			Strength:   1.4,
			Age:        0.05,
			Lifetime:   2,
			Radius:     1,
			Foam:       0.7,
		},
		WaterRippleHost{
			WaterIndex: 1,
			Position:   mgl32.Vec3{7, 0, 0},
			Strength:   0.3,
			Age:        0.1,
			Lifetime:   2,
		},
		WaterRippleHost{
			WaterIndex: 3,
			Position:   mgl32.Vec3{8, 0, 0},
			Strength:   0.3,
			Age:        0.1,
			Lifetime:   2,
		},
	)

	surfaces, packed, packedCount, dropped := buildBoundedWaterDisturbanceRecords(waters, ripples)
	if got := surfaces[0].Disturbance; got != ([4]uint32{0, MaxWaterDisturbancesPerSurface, 0, 0}) {
		t.Fatalf("expected first water capped range, got %v", got)
	}
	if got := surfaces[1].Disturbance; got != ([4]uint32{MaxWaterDisturbancesPerSurface, 1, 0, 0}) {
		t.Fatalf("expected second water single range after packed first range, got %v", got)
	}
	if len(packed) != MaxWaterDisturbancesPerSurface+1 {
		t.Fatalf("expected capped packed ripple records, got %d", len(packed))
	}
	if packedCount != MaxWaterDisturbancesPerSurface+1 {
		t.Fatalf("expected logical packed ripple count, got %d", packedCount)
	}
	if dropped != 4 {
		t.Fatalf("expected three over-budget drops and one invalid water drop, got %d", dropped)
	}

	foundHighPriority := false
	for i := 0; i < MaxWaterDisturbancesPerSurface; i++ {
		if packed[i].PositionAge[0] == 99 {
			foundHighPriority = true
			break
		}
	}
	if !foundHighPriority {
		t.Fatal("expected high-priority ripple retained in capped first-water range")
	}
}

func TestBuildWaterRippleRecordsPacksDisturbanceMetadata(t *testing.T) {
	records := buildWaterRippleRecords([]WaterRippleHost{
		{
			WaterIndex:         2,
			Position:           mgl32.Vec3{1, 2, 3},
			Strength:           0.8,
			Age:                0.25,
			Lifetime:           1.5,
			Radius:             0.65,
			HorizontalVelocity: [2]float32{4, -2},
			Foam:               0.45,
			DisturbanceKind:    1,
		},
	})
	if records[0].Params != ([4]float32{0.8, 1.5, 2, 1}) {
		t.Fatalf("unexpected ripple params: %v", records[0].Params)
	}
	if records[0].Motion != ([4]float32{4, -2, 0.65, 0.45}) {
		t.Fatalf("unexpected ripple motion: %v", records[0].Motion)
	}
}

func TestWaterDirtyStateFollowsResourceLifetime(t *testing.T) {
	manager := &GpuBufferManager{
		WaterBG0: &wgpu.BindGroup{},
		WaterBG1: &wgpu.BindGroup{},
		WaterBG2: &wgpu.BindGroup{},
		WaterBG3: &wgpu.BindGroup{},
	}
	if manager.WaterBindGroupsMissing() {
		t.Fatal("expected seeded water bind groups to satisfy readiness")
	}
	if manager.WaterShouldDirtyBindings(false) {
		t.Fatal("expected stable water buffers and bind groups to avoid dirtying bind groups")
	}
	if !manager.WaterShouldDirtyBindings(true) {
		t.Fatal("expected recreated water buffers to dirty bind groups")
	}

	manager.WaterBG3 = nil
	if !manager.WaterShouldDirtyBindings(false) {
		t.Fatal("expected missing water bind group to dirty bind groups")
	}
}
