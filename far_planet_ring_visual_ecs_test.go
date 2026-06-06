package gekko

import (
	"math"
	"testing"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestBuildFarPlanetRingInputsPreservesValidRecord(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	record := testFarPlanetRingRecord("ring-a", "planet-a", 10)
	cmd.AddEntity(&FarPlanetRingVisualComponent{Rings: []FarPlanetRingVisualRecord{record}})
	app.FlushCommands()

	inputs := buildFarPlanetRingInputs(cmd)
	if len(inputs) != 1 {
		t.Fatalf("expected one input, got %d", len(inputs))
	}
	input := inputs[0]
	if input.BandID != record.BandID || input.ParentBodyID != record.ParentBodyID {
		t.Fatalf("expected ids preserved, got %+v", input)
	}
	if input.NormalCameraRelative.LenSqr() < 0.99 || input.TangentUCameraRelative.LenSqr() < 0.99 || input.TangentVCameraRelative.LenSqr() < 0.99 {
		t.Fatalf("expected normalized basis vectors, got %+v", input)
	}
	if input.RadialOpacityProfile != record.RadialOpacityProfile {
		t.Fatalf("expected profile preserved, got %v", input.RadialOpacityProfile)
	}
	if input.DustHazeOpacity != record.DustHazeOpacity {
		t.Fatalf("expected dust haze opacity preserved, got %f", input.DustHazeOpacity)
	}
	if input.DustHazeMaxAlpha != record.DustHazeMaxAlpha ||
		input.DustHazeThicknessScale != record.DustHazeThicknessScale ||
		input.DustHazeMinHalfThicknessMeters != record.DustHazeMinHalfThicknessMeters ||
		input.DustHazeRadialEdgeFadeFraction != record.DustHazeRadialEdgeFadeFraction ||
		input.DustHazeVerticalCoreFraction != record.DustHazeVerticalCoreFraction ||
		input.DustHazeSampleCount != record.DustHazeSampleCount ||
		input.DustHazeForwardScatterStrength != record.DustHazeForwardScatterStrength ||
		input.DustHazeShadowStrength != record.DustHazeShadowStrength {
		t.Fatalf("expected dust haze tuning preserved, got %+v", input)
	}
}

func TestBuildFarPlanetRingInputsSkipsDisabledAndInvalidRecords(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	valid := testFarPlanetRingRecord("valid", "planet", 1)
	invalid := valid
	invalid.BandID = "invalid"
	invalid.OuterRadiusMeters = invalid.InnerRadiusMeters
	nanRecord := valid
	nanRecord.BandID = "nan"
	nanRecord.CenterCameraRelativeMeters[0] = float32(math.NaN())
	zeroBasis := valid
	zeroBasis.BandID = "zero-basis"
	zeroBasis.NormalCameraRelative = [3]float32{}

	cmd.AddEntity(&FarPlanetRingVisualComponent{Disabled: true, Rings: []FarPlanetRingVisualRecord{valid}})
	cmd.AddEntity(&FarPlanetRingVisualComponent{Rings: []FarPlanetRingVisualRecord{invalid, nanRecord, zeroBasis}})
	app.FlushCommands()

	if inputs := buildFarPlanetRingInputs(cmd); len(inputs) != 0 {
		t.Fatalf("expected disabled/invalid records to be skipped, got %+v", inputs)
	}
}

func TestBuildFarPlanetRingInputsSortsAndTruncatesDeterministically(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	records := make([]FarPlanetRingVisualRecord, 0, gpu_rt.MaxFarPlanetRings+4)
	for i := gpu_rt.MaxFarPlanetRings + 3; i >= 0; i-- {
		record := testFarPlanetRingRecord("ring-"+string(rune('a'+i%26)), "planet", float32(i))
		record.BandID = "ring-z"
		if i == 0 {
			record.BandID = "ring-a"
		}
		record.ParentDepthMeters = float32(i)
		records = append(records, record)
	}
	cmd.AddEntity(&FarPlanetRingVisualComponent{Rings: records})
	app.FlushCommands()

	inputs := buildFarPlanetRingInputs(cmd)
	if len(inputs) != gpu_rt.MaxFarPlanetRings {
		t.Fatalf("expected max inputs %d, got %d", gpu_rt.MaxFarPlanetRings, len(inputs))
	}
	if inputs[0].ParentDepthMeters != 0 || inputs[0].BandID != "ring-a" {
		t.Fatalf("expected nearest sorted record retained first, got %+v", inputs[0])
	}
	for i := 1; i < len(inputs); i++ {
		if inputs[i].ParentDepthMeters < inputs[i-1].ParentDepthMeters {
			t.Fatalf("expected stable ascending depth order at %d: %.1f < %.1f", i, inputs[i].ParentDepthMeters, inputs[i-1].ParentDepthMeters)
		}
	}
}

func testFarPlanetRingRecord(bandID, parentID string, depth float32) FarPlanetRingVisualRecord {
	profile := [FarPlanetRingRadialProfileSampleCount]float32{}
	for i := range profile {
		profile[i] = 1 - float32(i%8)*0.08
	}
	return FarPlanetRingVisualRecord{
		BandID:                           bandID,
		ParentBodyID:                     parentID,
		CenterCameraRelativeMeters:       [3]float32{0, 0, -depth - 100},
		NormalCameraRelative:             [3]float32{0, 1, 0},
		TangentUCameraRelative:           [3]float32{1, 0, 0},
		TangentVCameraRelative:           [3]float32{0, 0, -1},
		InnerRadiusMeters:                100,
		OuterRadiusMeters:                200,
		HalfThicknessMeters:              3,
		Tint:                             [3]float32{0.4, 0.5, 0.6},
		Opacity:                          0.7,
		DustHazeOpacity:                  0.7,
		DustHazeMaxAlpha:                 0.22,
		DustHazeThicknessScale:           72,
		DustHazeMinHalfThicknessMeters:   30000,
		DustHazeRadialEdgeFadeFraction:   0.05,
		DustHazeVerticalCoreFraction:     0.24,
		DustHazeSampleCount:              6,
		DustHazeForwardScatterStrength:   0.42,
		DustHazeShadowStrength:           0.6,
		Seed:                             99,
		RadialOpacityProfile:             profile,
		ParentCenterCameraRelativeMeters: [3]float32{0, 0, -depth - 100},
		ParentRadiusMeters:               50,
		ParentDepthMeters:                depth,
		LightDirectionViewSpace:          [3]float32{0, 1, 0},
	}
}
