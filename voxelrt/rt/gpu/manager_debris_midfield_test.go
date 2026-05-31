package gpu

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildDebrisMidfieldRecordsZeroIsSafe(t *testing.T) {
	records, params := buildDebrisMidfieldRecords(nil)
	if len(records) != MaxDebrisMidfieldCells {
		t.Fatalf("expected fixed record buffer length %d, got %d", MaxDebrisMidfieldCells, len(records))
	}
	if params.CellCount != 0 {
		t.Fatalf("expected zero count, got %d", params.CellCount)
	}
}

func TestBuildDebrisMidfieldRecordsPacksHostFields(t *testing.T) {
	host := testDebrisMidfieldHost(7)
	records, params := buildDebrisMidfieldRecords([]DebrisMidfieldHost{host})
	if params.CellCount != 1 {
		t.Fatalf("expected one cell, got %d", params.CellCount)
	}
	record := records[0]
	if record.PositionOpacity != [4]float32{1, 2, -300, 0.75} {
		t.Fatalf("unexpected position/opacity pack: %v", record.PositionOpacity)
	}
	if math.Float32bits(record.NormalSeed[3]) != host.Seed {
		t.Fatalf("expected seed bits preserved, got %v", record.NormalSeed[3])
	}
	if record.RadiiGaps != [4]float32{100, 200, 120, 130} {
		t.Fatalf("expected radii/gaps packed, got %v", record.RadiiGaps)
	}
	if record.TintPad != [4]float32{0.2, 0.3, 0.4, 0.8} {
		t.Fatalf("expected tint/density packed, got %v", record.TintPad)
	}
	if record.LightDirPad != [4]float32{0, 0, 1, 0.9} {
		t.Fatalf("expected light/fade packed, got %v", record.LightDirPad)
	}
	if record.HandoffPad != [4]float32{1, 1, 42, 0} {
		t.Fatalf("expected active handoff flag packed, got %v", record.HandoffPad)
	}
}

func TestBuildDebrisMidfieldRecordsTruncatesToMax(t *testing.T) {
	hosts := make([]DebrisMidfieldHost, MaxDebrisMidfieldCells+3)
	for i := range hosts {
		hosts[i] = testDebrisMidfieldHost(uint32(i))
	}
	records, params := buildDebrisMidfieldRecords(hosts)
	if len(records) != MaxDebrisMidfieldCells || params.CellCount != MaxDebrisMidfieldCells {
		t.Fatalf("expected max records/count %d, got len=%d count=%d", MaxDebrisMidfieldCells, len(records), params.CellCount)
	}
	if math.Float32bits(records[MaxDebrisMidfieldCells-1].NormalSeed[3]) != uint32(MaxDebrisMidfieldCells-1) {
		t.Fatalf("expected last retained seed %d", MaxDebrisMidfieldCells-1)
	}
}

func testDebrisMidfieldHost(seed uint32) DebrisMidfieldHost {
	return DebrisMidfieldHost{
		BandID:               "ring",
		CellID:               "ring-1-2-0",
		RadialIndex:          1,
		AngularIndex:         2,
		VerticalIndex:        0,
		PositionViewSpace:    mgl32.Vec3{1, 2, -300},
		PlaneNormalViewSpace: mgl32.Vec3{0, 1, 0},
		InnerRadiusMeters:    100,
		OuterRadiusMeters:    200,
		Seed:                 seed,
		Tint:                 [3]float32{0.2, 0.3, 0.4},
		Opacity:              0.75,
		DensityScale:         0.8,
		ApproachFade:         0.9,
		DistanceMeters:       250,
		GapInnerRadius:       120,
		GapOuterRadius:       130,
		LightDirViewSpace:    mgl32.Vec3{0, 0, 1},
		ActiveHandoff:        true,
		HandoffExact:         true,
		HandoffRadiusMeters:  42,
	}
}
