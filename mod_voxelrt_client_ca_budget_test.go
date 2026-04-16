package gekko

import (
	"testing"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestClampCAVolumeResolutionReducesAxisAndCellCount(t *testing.T) {
	cfg := gpu_rt.CAVolumeBudgetConfig{
		MaxResolutionAxis: 32,
		MaxCellsPerVolume: 32000,
	}.WithDefaults()

	got, clamped := clampCAVolumeResolution([3]uint32{80, 64, 48}, cfg)
	if !clamped {
		t.Fatalf("expected resolution clamp to trigger")
	}
	if got[0] > uint32(cfg.MaxResolutionAxis) || got[1] > uint32(cfg.MaxResolutionAxis) || got[2] > uint32(cfg.MaxResolutionAxis) {
		t.Fatalf("expected per-axis clamp to respect max %d, got %v", cfg.MaxResolutionAxis, got)
	}
	if cells := caVolumeCellCount(got); cells > uint64(cfg.MaxCellsPerVolume) {
		t.Fatalf("expected clamped volume to fit cell budget %d, got %d", cfg.MaxCellsPerVolume, cells)
	}
}

func TestBudgetCAVolumesDropsLowPriorityAndPreservesDeferredSteps(t *testing.T) {
	nearVolume := &CellularVolumeComponent{_gpuStepsPending: 4}
	farVolume := &CellularVolumeComponent{_gpuStepsPending: 3}
	cfg := gpu_rt.CAVolumeBudgetConfig{
		MaxManagedVolumes:     1,
		MaxResolutionAxis:     64,
		MaxCellsPerVolume:     64000,
		MaxAtlasCells:         64000,
		MaxStepsPerVolume:     4,
		MaxTotalStepsPerFrame: 2,
		StepReduceDistance:    50,
		StepSuspendDistance:   100,
		BehindCameraDot:       -0.1,
	}.WithDefaults()

	candidates := []caVolumeBudgetCandidate{
		{
			host: gpu_rt.CAVolumeHost{
				EntityID:     1,
				Resolution:   [3]uint32{16, 24, 16},
				Intensity:    1,
				StepsPending: 4,
			},
			volume:         nearVolume,
			rawSteps:       4,
			scheduledSteps: 4,
			visible:        true,
			behindCamera:   false,
			priority:       100,
		},
		{
			host: gpu_rt.CAVolumeHost{
				EntityID:     2,
				Resolution:   [3]uint32{16, 24, 16},
				Intensity:    0.5,
				StepsPending: 3,
			},
			volume:         farVolume,
			rawSteps:       3,
			scheduledSteps: 1,
			visible:        false,
			behindCamera:   true,
			priority:       10,
		},
	}

	hosts, dropped, deferred, suspended, totalSteps := budgetCAVolumes(candidates, cfg)
	if len(hosts) != 1 || hosts[0].EntityID != 1 {
		t.Fatalf("expected only high-priority volume to remain, got %+v", hosts)
	}
	if dropped != 1 {
		t.Fatalf("expected one dropped volume, got %d", dropped)
	}
	if deferred != 1 {
		t.Fatalf("expected one deferred volume due to step cap, got %d", deferred)
	}
	if suspended != 0 {
		t.Fatalf("expected no suspended selected volumes, got %d", suspended)
	}
	if totalSteps != 2 {
		t.Fatalf("expected total scheduled steps capped to 2, got %d", totalSteps)
	}
	if hosts[0].StepsPending != 2 {
		t.Fatalf("expected surviving volume to upload 2 scheduled steps, got %v", hosts[0].StepsPending)
	}
	if nearVolume._gpuStepsPending != 2 {
		t.Fatalf("expected surviving volume to retain 2 deferred steps, got %d", nearVolume._gpuStepsPending)
	}
	if farVolume._gpuStepsPending != 3 {
		t.Fatalf("expected dropped volume to keep its pending steps, got %d", farVolume._gpuStepsPending)
	}
}
