package main

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func pairedReport(mode string) report {
	ticks := []uint64{100, 120, 200, 240, 300, 360}
	summary := sampleSummary{Count: 3, MedianTicks: 20, P95Ticks: 30}
	if mode == "packed" {
		ticks = []uint64{100, 110, 200, 220, 300, 330}
		summary = sampleSummary{Count: 3, MedianTicks: 10, P95Ticks: 15}
	}
	return report{
		Version: 1, Mode: mode, Settings: settings{Workload: "sparse", Width: 64, Height: 64, Samples: 3, Warmup: 0, Batch: 2},
		Adapter: "same-adapter", Backend: "same-backend", Fixture: "deterministic-fixture", Shader: "same-shader",
		InitialGeometry: geometryStats{4096, 64, "initial-content"}, FinalGeometry: geometryStats{4096, 64, "final-content"},
		InitialCapture: capture{"initial-depth", "initial-normal", "initial-material", 64},
		FinalCapture:   capture{"final-depth", "final-normal", "final-material", 64},
		GPUAvailable:   true, GPUUnit: "raw_gpu_ticks", RawTicks: ticks, GPU: &summary,
	}
}

func TestCompareReportsUsesPackedToDenseGPUTickRatio(t *testing.T) {
	for _, first := range []string{"dense", "packed"} {
		t.Run(first, func(t *testing.T) {
			second := "dense"
			if first == "dense" {
				second = "packed"
			}
			a, b := pairedReport(first), pairedReport(second)
			// CPU wall time and upload size are independent costs, never ratio inputs.
			a.Initial.UpdateNS, b.Initial.UpdateNS = 999999, 1
			a.CPU, b.CPU = []cpuCost{{UpdateNS: 9999}}, []cpuCost{{UpdateNS: 1}}
			a.AuxiliaryCapacityBytes, b.AuxiliaryCapacityBytes = 1000, 1
			ratio, err := compareReports(a, b)
			if err != nil || ratio == nil || *ratio != .5 {
				t.Fatalf("ratio=%v error=%v, want packed/dense=0.5", ratio, err)
			}
		})
	}
}

func TestCompareReportsRejectsIncomparableEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*report)
	}{
		{"version", func(r *report) { r.Version++ }},
		{"same mode", func(r *report) { r.Mode = "dense" }},
		{"invalid mode", func(r *report) { r.Mode = "automatic" }},
		{"adapter", func(r *report) { r.Adapter = "other" }},
		{"backend", func(r *report) { r.Backend = "other" }},
		{"workload", func(r *report) { r.Settings.Workload = "dense" }},
		{"width", func(r *report) { r.Settings.Width++ }},
		{"height", func(r *report) { r.Settings.Height++ }},
		{"samples", func(r *report) { r.Settings.Samples++ }},
		{"warmup", func(r *report) { r.Settings.Warmup++ }},
		{"batch", func(r *report) { r.Settings.Batch++ }},
		{"fixture", func(r *report) { r.Fixture = "other" }},
		{"shader", func(r *report) { r.Shader = "other" }},
		{"initial geometry", func(r *report) { r.InitialGeometry.ContentSHA256 = "other" }},
		{"final geometry", func(r *report) { r.FinalGeometry.OccupiedVoxels++ }},
		{"initial depth", func(r *report) { r.InitialCapture.Depth = "other" }},
		{"initial normal", func(r *report) { r.InitialCapture.Normal = "other" }},
		{"initial material", func(r *report) { r.InitialCapture.Material = "other" }},
		{"initial hits", func(r *report) { r.InitialCapture.HitPixels++ }},
		{"final depth", func(r *report) { r.FinalCapture.Depth = "other" }},
		{"final normal", func(r *report) { r.FinalCapture.Normal = "other" }},
		{"final material", func(r *report) { r.FinalCapture.Material = "other" }},
		{"final hits", func(r *report) { r.FinalCapture.HitPixels++ }},
		{"raw count", func(r *report) { r.RawTicks = append(r.RawTicks, 400, 410) }},
		{"missing raw", func(r *report) { r.RawTicks = nil }},
		{"invalid raw", func(r *report) { r.RawTicks[1] = r.RawTicks[0] }},
		{"wrong unit", func(r *report) { r.GPUUnit = "nanoseconds" }},
		{"missing summary", func(r *report) { r.GPU = nil }},
		{"tampered summary count", func(r *report) { r.GPU.Count++ }},
		{"tampered summary median", func(r *report) { r.GPU.MedianTicks++ }},
		{"tampered summary p95", func(r *report) { r.GPU.P95Ticks++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := pairedReport("dense"), pairedReport("packed")
			tt.mutate(&b)
			for _, pair := range [][2]report{{a, b}, {b, a}} {
				ratio, err := compareReports(pair[0], pair[1])
				if err == nil || ratio != nil {
					t.Errorf("ratio=%v error=%v, want rejected evidence in both orientations", ratio, err)
				}
			}
		})
	}
}

func TestCompareReportsUnavailableTimingHasNoRatio(t *testing.T) {
	for _, unavailable := range []int{0, 1, 2} {
		a, b := pairedReport("dense"), pairedReport("packed")
		if unavailable != 1 {
			a.GPUAvailable = false
			a.GPU = nil
			a.RawTicks = nil
		}
		if unavailable != 0 {
			b.GPUAvailable = false
			b.GPU = nil
			b.RawTicks = nil
		}
		ratio, err := compareReports(a, b)
		if err != nil || ratio != nil {
			t.Fatalf("unavailable=%d ratio=%v error=%v, want no ratio", unavailable, ratio, err)
		}
	}
}

func frontDepth(o *core.VoxelObject, x, y int) int {
	for z := 31; z >= 0; z-- {
		if occupied, _ := o.XBrickMap.GetVoxel(x, y, z); occupied {
			return z
		}
	}
	return -1
}

func TestFixturesHaveDeterministicDensityAndMixedMaterialContent(t *testing.T) {
	for _, workload := range []string{"sparse", "dense", "edited"} {
		t.Run(workload, func(t *testing.T) {
			_, a := makeFixture(workload)
			_, b := makeFixture(workload)
			if a == b || a.XBrickMap == b.XBrickMap {
				t.Fatal("paired fixtures share mutable voxel storage")
			}
			if geometry(a) != geometry(b) || !reflect.DeepEqual(a.MaterialTable, b.MaterialTable) {
				t.Fatal("paired fixture content differs")
			}
			const total = 32 * 32 * 32
			count := geometry(a).OccupiedVoxels
			if workload == "dense" {
				if count*100 < total*99 {
					t.Fatalf("dense fixture has %d/%d occupied voxels", count, total)
				}
				// Each brick must contain empty cells and both materials, avoiding the
				// solid and uniform material paths in the dense traversal comparison.
				for bz := 0; bz < 32; bz += 8 {
					for by := 0; by < 32; by += 8 {
						for bx := 0; bx < 32; bx += 8 {
							empty := false
							materials := map[uint8]bool{}
							for z := bz; z < bz+8; z++ {
								for y := by; y < by+8; y++ {
									for x := bx; x < bx+8; x++ {
										occ, value := a.XBrickMap.GetVoxel(x, y, z)
										if occ {
											materials[value] = true
										} else {
											empty = true
										}
									}
								}
							}
							if !empty || len(materials) < 2 {
								t.Fatalf("brick at %d,%d,%d does not exercise nonsolid mixed materials", bx, by, bz)
							}
						}
					}
				}
			} else if count <= 0 || count*4 > total {
				t.Fatalf("sparse fixture has %d/%d occupied voxels", count, total)
			}
		})
	}
}

func TestEditedFixturePreservesBoundsAndCountWithVisibleChanges(t *testing.T) {
	_, a := makeFixture("edited")
	_, b := makeFixture("edited")
	initial := geometry(a)
	lo, hi := a.XBrickMap.ComputeAABB()
	initialDepth := frontDepth(a, 12, 12)
	var priorDepth int
	for phase := 0; phase < 2; phase++ {
		editFixture(a, phase)
		editFixture(b, phase)
		got := geometry(a)
		newLo, newHi := a.XBrickMap.ComputeAABB()
		if got.OccupiedVoxels != initial.OccupiedVoxels || newLo != lo || newHi != hi {
			t.Fatalf("phase %d changed bounds or occupancy: %+v bounds %v,%v", phase, got, newLo, newHi)
		}
		if got.ContentSHA256 == initial.ContentSHA256 || got != geometry(b) {
			t.Fatalf("phase %d geometry unchanged or paired content differs", phase)
		}
		depth := frontDepth(a, 12, 12)
		if depth < 0 || depth == initialDepth || (phase > 0 && depth == priorDepth) {
			t.Fatalf("phase %d exposed patch depth=%d initial=%d previous=%d", phase, depth, initialDepth, priorDepth)
		}
		priorDepth = depth
	}
}
