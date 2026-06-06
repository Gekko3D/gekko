package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
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
			host: app_rt.CAVolumeInput{
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
			host: app_rt.CAVolumeInput{
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

func TestBuildCAVolumeFrameInputBuildsTypedBridgeAdapter(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	volume := &CellularVolumeComponent{
		Resolution:            [3]int{80, 64, 48},
		Type:                  CellularFire,
		Preset:                CAVolumePresetCampfire,
		UseIntensity:          true,
		Intensity:             0.75,
		TickRate:              20,
		Diffusion:             0.1,
		Buoyancy:              0.2,
		Cooling:               0.3,
		Dissipation:           0.4,
		UseAppearanceOverride: true,
		ScatterColor:          [3]float32{0.6, 0.5, 0.4},
		Extinction:            0.7,
		Emission:              0.8,
		_gpuStepsPending:      3,
		_dirty:                true,
	}
	transform := &TransformComponent{
		Position: mgl32.Vec3{1, 2, 3},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{2, 3, 4},
	}
	entityID := cmd.AddEntity(
		transform,
		volume,
	)
	app.FlushCommands()

	cfg := gpu_rt.CAVolumeBudgetConfig{
		MaxManagedVolumes:     4,
		MaxResolutionAxis:     32,
		MaxCellsPerVolume:     32000,
		MaxAtlasCells:         64000,
		MaxStepsPerVolume:     4,
		MaxTotalStepsPerFrame: 2,
		StepReduceDistance:    50,
		StepSuspendDistance:   100,
		BehindCameraDot:       -0.1,
	}

	frame := buildCAVolumeFrameInput(cmd, nil, cfg, 1.0/60.0)
	if !frame.UpdatePresets {
		t.Fatal("expected CA frame input to request preset refresh")
	}
	if frame.DeltaTime != 1.0/60.0 {
		t.Fatalf("expected CA frame delta time to be forwarded, got %v", frame.DeltaTime)
	}
	if frame.RequestedVolumeCount != 1 {
		t.Fatalf("expected one requested CA volume, got %d", frame.RequestedVolumeCount)
	}
	if frame.ResolutionClampedCount != 1 {
		t.Fatalf("expected one clamped CA volume, got %d", frame.ResolutionClampedCount)
	}
	if frame.DeferredStepCount != 1 {
		t.Fatalf("expected CA frame to report deferred steps, got %d", frame.DeferredStepCount)
	}
	if frame.TotalScheduledSteps != 2 {
		t.Fatalf("expected scheduled steps capped to 2, got %d", frame.TotalScheduledSteps)
	}
	if len(frame.Volumes) != 1 {
		t.Fatalf("expected one typed CA volume input, got %d", len(frame.Volumes))
	}
	got := frame.Volumes[0]
	if got.EntityID != uint32(entityID) {
		t.Fatalf("expected CA volume entity id %d, got %d", entityID, got.EntityID)
	}
	if got.Type != uint32(CellularFire) || got.Preset != uint32(CAVolumePresetCampfire) {
		t.Fatalf("expected CA type/preset to be forwarded, got type=%d preset=%d", got.Type, got.Preset)
	}
	if got.Resolution[0] > 32 || got.Resolution[1] > 32 || got.Resolution[2] > 32 {
		t.Fatalf("expected clamped CA resolution, got %v", got.Resolution)
	}
	if got.Position != volume.VolumeOrigin(transform) {
		t.Fatalf("expected CA position from volume origin, got %v", got.Position)
	}
	if got.VoxelScale != (mgl32.Vec3{VoxelSize * 2, VoxelSize * 3, VoxelSize * 4}) {
		t.Fatalf("expected CA voxel scale from transform, got %v", got.VoxelScale)
	}
	if got.Intensity != 0.75 ||
		got.Diffusion != volume.Diffusion ||
		got.Buoyancy != volume.Buoyancy ||
		got.Cooling != volume.Cooling ||
		got.Dissipation != volume.Dissipation ||
		got.Extinction != volume.Extinction ||
		got.Emission != volume.Emission ||
		got.StepDt != 1.0/20.0 ||
		got.ScatterColor != volume.ScatterColor {
		t.Fatalf("expected CA renderer input fields to be forwarded, got %+v", got)
	}
	if got.StepsPending != 2 {
		t.Fatalf("expected CA upload steps capped to 2, got %v", got.StepsPending)
	}
	var storedVolume *CellularVolumeComponent
	MakeQuery1[CellularVolumeComponent](cmd).Map(func(_ EntityId, cv *CellularVolumeComponent) bool {
		storedVolume = cv
		return false
	})
	if storedVolume == nil {
		t.Fatal("expected CA adapter test volume to remain queryable")
	}
	if storedVolume._gpuStepsPending != 1 {
		t.Fatalf("expected one deferred CA step to remain on ECS component, got %d", storedVolume._gpuStepsPending)
	}
	if storedVolume._dirty {
		t.Fatal("expected CA adapter to clear dirty flag after building input")
	}
}
