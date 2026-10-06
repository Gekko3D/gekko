package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func s1oAssertStreamingConfig(t *testing.T, manager *gpu.GpuBufferManager, config VoxelRtStreamingConfig) {
	t.Helper()
	if got := manager.ManagedGeometryAdmissionBudget(); got != config.ManagedAdmission {
		t.Fatalf("managed admission = %+v, want %+v", got, config.ManagedAdmission)
	}
	if got := manager.ManagedGeometryFrameBudget(); got != config.ManagedFrame {
		t.Fatalf("managed frame = %+v, want %+v", got, config.ManagedFrame)
	}
	if got := manager.SectorLookupFrameBudget(); got != config.SectorLookup {
		t.Fatalf("sector lookup = %+v, want %+v", got, config.SectorLookup)
	}
	if config.Upload != nil && manager.VoxelUploadBudget() != *config.Upload {
		t.Fatalf("upload = %+v, want %+v", manager.VoxelUploadBudget(), *config.Upload)
	}
	if config.NativeWork != nil && manager.VoxelGPUWorkBudget() != *config.NativeWork {
		t.Fatalf("native work = %+v, want %+v", manager.VoxelGPUWorkBudget(), *config.NativeWork)
	}
	if config.NativeAdmission != nil && manager.VoxelGPUAdmissionBudget() != *config.NativeAdmission {
		t.Fatalf("native admission = %+v, want %+v", manager.VoxelGPUAdmissionBudget(), *config.NativeAdmission)
	}
}

func TestS1oStreamingConfigAppliesExactValuesAndCopiesOptionalBudgets(t *testing.T) {
	upload := gpu.VoxelUploadBudget{MaxBytes: 17, MaxSectors: 3, MaxBricks: 5}
	work := gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 23, MaxCreates: 2, MaxCopyBytes: 29}
	admission := gpu.VoxelGPUAdmissionBudget{MaxBytes: 31}
	config := VoxelRtStreamingConfig{
		ManagedAdmission: gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: 37, MaxCopiedStageBytes: 41},
		ManagedFrame:     gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 7},
		SectorLookup:     gpu.SectorLookupFrameBudget{Enabled: true, MaxEntries: 11, MaxUploadBytes: 43, MaxStageBytes: 47},
		Upload:           &upload, NativeWork: &work, NativeAdmission: &admission,
	}
	module := VoxelRtModule{StreamingConfig: &config}
	first, second := &gpu.GpuBufferManager{}, &gpu.GpuBufferManager{}
	module.StreamingConfig.Apply(first)
	module.StreamingConfig.Apply(second)
	s1oAssertStreamingConfig(t, first, config)
	s1oAssertStreamingConfig(t, second, config)
	wantUpload, wantWork, wantAdmission := upload, work, admission
	upload.MaxBytes, work.MaxCreates, admission.MaxBytes = 0, 0, 0
	config.ManagedFrame.MaxEntries = 0
	if first.VoxelUploadBudget() != wantUpload || second.VoxelUploadBudget() != wantUpload || first.VoxelGPUWorkBudget() != wantWork || first.VoxelGPUAdmissionBudget() != wantAdmission || first.ManagedGeometryFrameBudget().MaxEntries != 7 {
		t.Fatal("installed policy aliases caller-owned configuration")
	}
	first.SetManagedGeometryFrameBudget(gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 19})
	if second.ManagedGeometryFrameBudget().MaxEntries != 7 {
		t.Fatal("separate managers share installed budget state")
	}
}

func TestS1oNilStreamingConfigAndOptionalPoliciesPreserveExistingBudgets(t *testing.T) {
	if (VoxelRtModule{}).StreamingConfig != nil {
		t.Fatal("zero renderer module opted into managed streaming")
	}
	manager := &gpu.GpuBufferManager{}
	config := DefaultVoxelRtStreamingConfig()
	admission := gpu.VoxelGPUAdmissionBudget{MaxBytes: 73}
	config.NativeAdmission = &admission
	config.Apply(manager)
	var absent *VoxelRtStreamingConfig
	absent.Apply(manager)
	s1oAssertStreamingConfig(t, manager, config)
	config.Apply(nil)
	absent.Apply(nil)
	upload, work := manager.VoxelUploadBudget(), manager.VoxelGPUWorkBudget()
	managedOnly := VoxelRtStreamingConfig{ManagedAdmission: gpu.ManagedGeometryAdmissionBudget{Enabled: true}, ManagedFrame: gpu.ManagedGeometryFrameBudget{Enabled: true}, SectorLookup: gpu.SectorLookupFrameBudget{Enabled: true}}
	managedOnly.Apply(manager)
	s1oAssertStreamingConfig(t, manager, managedOnly)
	if manager.VoxelUploadBudget() != upload || manager.VoxelGPUWorkBudget() != work || manager.VoxelGPUAdmissionBudget() != admission {
		t.Fatal("nil optional policy reset an existing manager policy")
	}
}

func TestS1oStreamingConfigPreservesEnabledZeroAndIndependentPauseDimensions(t *testing.T) {
	for _, config := range []VoxelRtStreamingConfig{
		{},
		{ManagedAdmission: gpu.ManagedGeometryAdmissionBudget{Enabled: true}, ManagedFrame: gpu.ManagedGeometryFrameBudget{Enabled: true}, SectorLookup: gpu.SectorLookupFrameBudget{Enabled: true}, Upload: &gpu.VoxelUploadBudget{}, NativeWork: &gpu.VoxelGPUWorkBudget{Enabled: true}, NativeAdmission: &gpu.VoxelGPUAdmissionBudget{}},
		{ManagedAdmission: gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: 91}, ManagedFrame: gpu.ManagedGeometryFrameBudget{Enabled: true}, SectorLookup: gpu.SectorLookupFrameBudget{Enabled: true, MaxEntries: 2, MaxStageBytes: 97}, Upload: &gpu.VoxelUploadBudget{MaxSectors: 3}, NativeWork: &gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 101, MaxCopyBytes: 103}},
	} {
		manager := &gpu.GpuBufferManager{}
		preset := DefaultVoxelRtStreamingConfig()
		preset.Apply(manager)
		config.Apply(manager)
		s1oAssertStreamingConfig(t, manager, config)
	}
}

func TestS1oStreamingPresetUsesBoundedDefaultsAndIndependentOptionalValues(t *testing.T) {
	first, second := DefaultVoxelRtStreamingConfig(), DefaultVoxelRtStreamingConfig()
	if first.ManagedAdmission != gpu.DefaultManagedGeometryAdmissionBudget() || first.ManagedFrame != gpu.DefaultManagedGeometryFrameBudget() || first.SectorLookup != gpu.DefaultSectorLookupFrameBudget() || first.Upload == nil || *first.Upload != gpu.DefaultVoxelUploadBudget() {
		t.Fatal("explicit streaming preset does not compose the established managed/upload defaults")
	}
	if first.NativeWork == nil || !first.NativeWork.Enabled || first.NativeWork.MaxCreateBytes == 0 || first.NativeWork.MaxCreates == 0 || first.NativeWork.MaxCopyBytes < 4 {
		t.Fatal("streaming preset has no positive finite native creation/copy service")
	}
	if first.NativeAdmission != nil {
		t.Fatal("streaming preset invented a device-specific native admission cap")
	}
	wantUpload, wantWork := *second.Upload, *second.NativeWork
	first.Upload.MaxBytes, first.NativeWork.MaxCreates = 0, 0
	if *second.Upload != wantUpload || *second.NativeWork != wantWork {
		t.Fatal("fresh streaming presets alias optional policy storage")
	}
}
