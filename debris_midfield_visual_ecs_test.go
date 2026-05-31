package gekko

import (
	"fmt"
	"math"
	"testing"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestBuildDebrisMidfieldHostsPreservesValidRecord(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	record := testDebrisMidfieldRecord("ring-a", "ring-a-1-2-0", 10)
	cmd.AddEntity(&DebrisMidfieldVisualComponent{Cells: []DebrisMidfieldCellRecord{record}})
	app.FlushCommands()

	hosts := buildDebrisMidfieldHosts(cmd)
	if len(hosts) != 1 {
		t.Fatalf("expected one host, got %d", len(hosts))
	}
	host := hosts[0]
	if host.BandID != record.BandID || host.CellID != record.CellID {
		t.Fatalf("expected semantic identity preserved, got %+v", host)
	}
	if host.DensityScale != record.DensityScale || host.ApproachFade != record.ApproachFade {
		t.Fatalf("expected fade/density preserved, got density=%.3f fade=%.3f", host.DensityScale, host.ApproachFade)
	}
	if host.ActiveHandoff != record.ActiveHandoff {
		t.Fatalf("expected active handoff flag preserved, got %v", host.ActiveHandoff)
	}
	if host.HandoffExact != record.HandoffExact || host.HandoffRadiusMeters != record.HandoffRadiusMeters {
		t.Fatalf("expected exact handoff metadata preserved, got exact=%v radius=%.3f", host.HandoffExact, host.HandoffRadiusMeters)
	}
	if host.PlaneNormalViewSpace.LenSqr() < 0.99 || host.LightDirViewSpace.LenSqr() < 0.99 {
		t.Fatalf("expected normalized vectors, got normal=%v light=%v", host.PlaneNormalViewSpace, host.LightDirViewSpace)
	}
}

func TestBuildDebrisMidfieldHostsSkipsDisabledAndInvalidRecords(t *testing.T) {
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

	if hosts := buildDebrisMidfieldHosts(cmd); len(hosts) != 0 {
		t.Fatalf("expected disabled/invalid records to be skipped, got %+v", hosts)
	}
}

func TestBuildDebrisMidfieldHostsSortsDeduplicatesAndTruncates(t *testing.T) {
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

	hosts := buildDebrisMidfieldHosts(cmd)
	if len(hosts) != gpu_rt.MaxDebrisMidfieldCells {
		t.Fatalf("expected max hosts %d, got %d", gpu_rt.MaxDebrisMidfieldCells, len(hosts))
	}
	if hosts[0].BandID != "ring-a" || hosts[0].CellID != "ring-a-cell" {
		t.Fatalf("expected strongest/nearest record first, got %+v", hosts[0])
	}
	seen := map[string]bool{}
	for _, host := range hosts {
		identity := host.CellID
		if host.AsteroidID != "" {
			identity = host.AsteroidID
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
