package gpu

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildFarPlanetRingRecordsZeroIsSafe(t *testing.T) {
	records, params := buildFarPlanetRingRecords(nil)
	if len(records) != MaxFarPlanetRings {
		t.Fatalf("expected fixed record buffer length %d, got %d", MaxFarPlanetRings, len(records))
	}
	if params.RingCount != 0 {
		t.Fatalf("expected zero count, got %d", params.RingCount)
	}
}

func TestBuildFarPlanetRingRecordsPacksHostFields(t *testing.T) {
	host := testFarPlanetRingHost(7)
	records, params := buildFarPlanetRingRecords([]FarPlanetRingHost{host})
	if params.RingCount != 1 {
		t.Fatalf("expected one ring, got %d", params.RingCount)
	}
	record := records[0]
	if record.CenterOpacity != [4]float32{1, 2, -300, 0.75} {
		t.Fatalf("unexpected center/opacity pack: %v", record.CenterOpacity)
	}
	if record.TangentUInner[3] != host.InnerRadiusMeters || record.TangentVOuter[3] != host.OuterRadiusMeters {
		t.Fatalf("expected radii packed, got %v %v", record.TangentUInner, record.TangentVOuter)
	}
	if math.Float32bits(record.TintSeed[3]) != host.Seed {
		t.Fatalf("expected seed bits preserved, got %v", record.TintSeed[3])
	}
	if record.ParentRadius != [4]float32{1, 2, -250, 50} || record.ParentDepthLight != [4]float32{0.5, 0, 1, 0} {
		t.Fatalf("expected parent/light fields packed, got %v / %v", record.ParentRadius, record.ParentDepthLight)
	}
	if record.DustHazeParams != [4]float32{0.22, 72, 30000, 0.05} {
		t.Fatalf("expected dust haze params packed, got %v", record.DustHazeParams)
	}
	if record.DustHazeLighting != [4]float32{0.24, 6, 0.42, 0.6} {
		t.Fatalf("expected dust haze lighting packed, got %v", record.DustHazeLighting)
	}
	if record.Profile0 != [4]float32{0, 0.25, 0.5, 0.75} ||
		record.Profile1 != [4]float32{1, 0.75, 0.5, 0.25} ||
		record.Profile7 != [4]float32{1, 0.75, 0.5, 0.25} {
		t.Fatalf("expected profile samples packed, got %v / %v / %v", record.Profile0, record.Profile1, record.Profile7)
	}
}

func TestBuildFarPlanetRingRecordsTruncatesToMax(t *testing.T) {
	hosts := make([]FarPlanetRingHost, MaxFarPlanetRings+3)
	for i := range hosts {
		hosts[i] = testFarPlanetRingHost(uint32(i))
	}
	records, params := buildFarPlanetRingRecords(hosts)
	if len(records) != MaxFarPlanetRings || params.RingCount != MaxFarPlanetRings {
		t.Fatalf("expected max records/count %d, got len=%d count=%d", MaxFarPlanetRings, len(records), params.RingCount)
	}
	if math.Float32bits(records[MaxFarPlanetRings-1].TintSeed[3]) != uint32(MaxFarPlanetRings-1) {
		t.Fatalf("expected last retained seed %d", MaxFarPlanetRings-1)
	}
}

func testFarPlanetRingHost(seed uint32) FarPlanetRingHost {
	profile := [32]float32{}
	pattern := []float32{0, 0.25, 0.5, 0.75, 1, 0.75, 0.5, 0.25}
	for i := range profile {
		profile[i] = pattern[i%len(pattern)]
	}
	return FarPlanetRingHost{
		BandID:                           "ring",
		ParentBodyID:                     "planet",
		CenterCameraRelativeMeters:       mgl32.Vec3{1, 2, -300},
		NormalCameraRelative:             mgl32.Vec3{0, 2, 0},
		TangentUCameraRelative:           mgl32.Vec3{2, 0, 0},
		TangentVCameraRelative:           mgl32.Vec3{0, 0, -2},
		InnerRadiusMeters:                100,
		OuterRadiusMeters:                200,
		HalfThicknessMeters:              4,
		Tint:                             [3]float32{0.2, 0.3, 0.4},
		Opacity:                          0.75,
		DustHazeOpacity:                  0.5,
		DustHazeMaxAlpha:                 0.22,
		DustHazeThicknessScale:           72,
		DustHazeMinHalfThicknessMeters:   30000,
		DustHazeRadialEdgeFadeFraction:   0.05,
		DustHazeVerticalCoreFraction:     0.24,
		DustHazeSampleCount:              6,
		DustHazeForwardScatterStrength:   0.42,
		DustHazeShadowStrength:           0.6,
		Seed:                             seed,
		RadialOpacityProfile:             profile,
		ParentCenterCameraRelativeMeters: mgl32.Vec3{1, 2, -250},
		ParentRadiusMeters:               50,
		ParentDepthMeters:                250,
		LightDirectionViewSpace:          mgl32.Vec3{0, 2, 0},
	}
}
