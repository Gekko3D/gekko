package gekko

import (
	"math"
	"testing"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestBuildFarPlanetRingHostsPreservesValidRecord(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	record := testFarPlanetRingRecord("ring-a", "planet-a", 10)
	cmd.AddEntity(&FarPlanetRingVisualComponent{Rings: []FarPlanetRingVisualRecord{record}})
	app.FlushCommands()

	hosts := buildFarPlanetRingHosts(cmd)
	if len(hosts) != 1 {
		t.Fatalf("expected one host, got %d", len(hosts))
	}
	host := hosts[0]
	if host.BandID != record.BandID || host.ParentBodyID != record.ParentBodyID {
		t.Fatalf("expected ids preserved, got %+v", host)
	}
	if host.NormalCameraRelative.LenSqr() < 0.99 || host.TangentUCameraRelative.LenSqr() < 0.99 || host.TangentVCameraRelative.LenSqr() < 0.99 {
		t.Fatalf("expected normalized basis vectors, got %+v", host)
	}
	if host.RadialOpacityProfile != record.RadialOpacityProfile {
		t.Fatalf("expected profile preserved, got %v", host.RadialOpacityProfile)
	}
	if host.DustHazeOpacity != record.DustHazeOpacity {
		t.Fatalf("expected dust haze opacity preserved, got %f", host.DustHazeOpacity)
	}
	if host.DustHazeMaxAlpha != record.DustHazeMaxAlpha ||
		host.DustHazeThicknessScale != record.DustHazeThicknessScale ||
		host.DustHazeMinHalfThicknessMeters != record.DustHazeMinHalfThicknessMeters ||
		host.DustHazeRadialEdgeFadeFraction != record.DustHazeRadialEdgeFadeFraction ||
		host.DustHazeVerticalCoreFraction != record.DustHazeVerticalCoreFraction ||
		host.DustHazeSampleCount != record.DustHazeSampleCount ||
		host.DustHazeForwardScatterStrength != record.DustHazeForwardScatterStrength ||
		host.DustHazeShadowStrength != record.DustHazeShadowStrength {
		t.Fatalf("expected dust haze tuning preserved, got %+v", host)
	}
}

func TestBuildFarPlanetRingHostsSkipsDisabledAndInvalidRecords(t *testing.T) {
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

	if hosts := buildFarPlanetRingHosts(cmd); len(hosts) != 0 {
		t.Fatalf("expected disabled/invalid records to be skipped, got %+v", hosts)
	}
}

func TestBuildFarPlanetRingHostsSortsAndTruncatesDeterministically(t *testing.T) {
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

	hosts := buildFarPlanetRingHosts(cmd)
	if len(hosts) != gpu_rt.MaxFarPlanetRings {
		t.Fatalf("expected max hosts %d, got %d", gpu_rt.MaxFarPlanetRings, len(hosts))
	}
	if hosts[0].ParentDepthMeters != 0 || hosts[0].BandID != "ring-a" {
		t.Fatalf("expected nearest sorted record retained first, got %+v", hosts[0])
	}
	for i := 1; i < len(hosts); i++ {
		if hosts[i].ParentDepthMeters < hosts[i-1].ParentDepthMeters {
			t.Fatalf("expected stable ascending depth order at %d: %.1f < %.1f", i, hosts[i].ParentDepthMeters, hosts[i-1].ParentDepthMeters)
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
