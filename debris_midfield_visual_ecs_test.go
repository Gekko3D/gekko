package gekko

import (
	"fmt"
	"math"
	"testing"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestBuildDebrisMidfieldInputsPreservesValidRecord(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	record := testDebrisMidfieldRecord("ring-a", "ring-a-1-2-0", 10)
	cmd.AddEntity(&DebrisMidfieldVisualComponent{Cells: []DebrisMidfieldCellRecord{record}})
	app.FlushCommands()

	inputs := buildDebrisMidfieldInputs(cmd)
	if len(inputs) != 1 {
		t.Fatalf("expected one input, got %d", len(inputs))
	}
	input := inputs[0]
	if input.BandID != record.BandID || input.CellID != record.CellID {
		t.Fatalf("expected semantic identity preserved, got %+v", input)
	}
	if input.DensityScale != record.DensityScale || input.ApproachFade != record.ApproachFade {
		t.Fatalf("expected fade/density preserved, got density=%.3f fade=%.3f", input.DensityScale, input.ApproachFade)
	}
	if input.ActiveHandoff != record.ActiveHandoff {
		t.Fatalf("expected active handoff flag preserved, got %v", input.ActiveHandoff)
	}
	if input.HandoffExact != record.HandoffExact || input.HandoffRadiusMeters != record.HandoffRadiusMeters {
		t.Fatalf("expected exact handoff metadata preserved, got exact=%v radius=%.3f", input.HandoffExact, input.HandoffRadiusMeters)
	}
	if input.PlaneNormalViewSpace.LenSqr() < 0.99 || input.LightDirViewSpace.LenSqr() < 0.99 {
		t.Fatalf("expected normalized vectors, got normal=%v light=%v", input.PlaneNormalViewSpace, input.LightDirViewSpace)
	}
}

func TestBuildDebrisMidfieldInputsSkipsDisabledAndInvalidRecords(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	valid := testDebrisMidfieldRecord("ring-a", "ring-a-1-2-0", 10)
	invalidRadius := valid
	invalidRadius.CellID = "bad-radius"
	invalidRadius.OuterRadiusMeters = invalidRadius.InnerRadiusMeters
	nanRecord := valid
	nanRecord.CellID = "nan"
	nanRecord.PositionViewSpace[0] = float32(math.NaN())
	zeroNormal := valid
	zeroNormal.CellID = "zero-normal"
	zeroNormal.PlaneNormalViewSpace = [3]float32{}
	badFade := valid
	badFade.CellID = "bad-fade"
	badFade.ApproachFade = 1.2

	cmd.AddEntity(&DebrisMidfieldVisualComponent{Disabled: true, Cells: []DebrisMidfieldCellRecord{valid}})
	cmd.AddEntity(&DebrisMidfieldVisualComponent{Cells: []DebrisMidfieldCellRecord{invalidRadius, nanRecord, zeroNormal, badFade}})
	app.FlushCommands()

	if inputs := buildDebrisMidfieldInputs(cmd); len(inputs) != 0 {
		t.Fatalf("expected disabled/invalid records to be skipped, got %+v", inputs)
	}
}

func TestBuildDebrisMidfieldInputsSortsDeduplicatesAndTruncates(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	records := make([]DebrisMidfieldCellRecord, 0, gpu_rt.MaxDebrisMidfieldCells+4)
	for i := gpu_rt.MaxDebrisMidfieldCells + 3; i >= 0; i-- {
		record := testDebrisMidfieldRecord("ring-z", fmt.Sprintf("ring-z-cell-%03d", i), float32(i))
		record.RadialIndex = i
		record.DistanceMeters = float32(i)
		record.Opacity = 0.5
		if i == 0 {
			record.BandID = "ring-a"
			record.CellID = "ring-a-cell"
			record.Opacity = 0.9
		}
		records = append(records, record)
	}
	duplicate := records[len(records)-1]
	duplicate.Opacity = 0.1
	records = append(records, duplicate)
	cmd.AddEntity(&DebrisMidfieldVisualComponent{Cells: records})
	app.FlushCommands()

	inputs := buildDebrisMidfieldInputs(cmd)
	if len(inputs) != gpu_rt.MaxDebrisMidfieldCells {
		t.Fatalf("expected max inputs %d, got %d", gpu_rt.MaxDebrisMidfieldCells, len(inputs))
	}
	if inputs[0].BandID != "ring-a" || inputs[0].CellID != "ring-a-cell" {
		t.Fatalf("expected strongest/nearest record first, got %+v", inputs[0])
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		identity := input.CellID
		if input.AsteroidID != "" {
			identity = input.AsteroidID
		}
		if seen[identity] {
			t.Fatalf("expected duplicate render ids to be collapsed, saw %q twice", identity)
		}
		seen[identity] = true
	}
}

func testDebrisMidfieldRecord(bandID, cellID string, distance float32) DebrisMidfieldCellRecord {
	return DebrisMidfieldCellRecord{
		BandID:               bandID,
		CellID:               cellID,
		RadialIndex:          1,
		AngularIndex:         2,
		VerticalIndex:        0,
		PositionViewSpace:    [3]float32{distance, 0, -100},
		PlaneNormalViewSpace: [3]float32{0, 2, 0},
		InnerRadiusMeters:    100,
		OuterRadiusMeters:    200,
		Seed:                 99,
		Tint:                 [3]float32{0.4, 0.5, 0.6},
		Opacity:              0.7,
		DensityScale:         0.8,
		ApproachFade:         0.9,
		DistanceMeters:       distance,
		GapInnerRadius:       120,
		GapOuterRadius:       130,
		LightDirViewSpace:    [3]float32{0, 0, 1},
		ActiveHandoff:        true,
		HandoffExact:         true,
		HandoffRadiusMeters:  42,
		AsteroidID:           cellID + "-ast-0",
	}
}
