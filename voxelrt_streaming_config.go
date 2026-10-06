package gekko

import "github.com/gekko3d/gekko/voxelrt/rt/gpu"

// VoxelRtStreamingConfig installs explicit streaming policies once at renderer
// initialization. Optional policies leave the manager's existing values intact.
type VoxelRtStreamingConfig struct {
	ManagedAdmission gpu.ManagedGeometryAdmissionBudget
	ManagedFrame     gpu.ManagedGeometryFrameBudget
	SectorLookup     gpu.SectorLookupFrameBudget
	Upload           *gpu.VoxelUploadBudget
	NativeWork       *gpu.VoxelGPUWorkBudget
	NativeAdmission  *gpu.VoxelGPUAdmissionBudget
}

// Apply copies policy values; nil config or manager preserves existing behavior.
// Enabled zero budgets retain the individual manager policies' pause semantics.
func (config *VoxelRtStreamingConfig) Apply(manager *gpu.GpuBufferManager) {
	if config == nil || manager == nil {
		return
	}
	manager.SetManagedGeometryAdmissionBudget(config.ManagedAdmission)
	manager.SetManagedGeometryFrameBudget(config.ManagedFrame)
	manager.SetSectorLookupFrameBudget(config.SectorLookup)
	if config.Upload != nil {
		manager.SetVoxelUploadBudget(*config.Upload)
	}
	if config.NativeWork != nil {
		manager.SetVoxelGPUWorkBudget(*config.NativeWork)
	}
	if config.NativeAdmission != nil {
		manager.SetVoxelGPUAdmissionBudget(*config.NativeAdmission)
	}
}

// DefaultVoxelRtStreamingConfig is an explicit bounded service preset. Native
// work limits are configuration defaults, not measured performance targets.
// It does not choose a device-specific physical resource admission cap.
func DefaultVoxelRtStreamingConfig() VoxelRtStreamingConfig {
	upload := gpu.DefaultVoxelUploadBudget()
	work := gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreates: 2, MaxCreateBytes: 16 << 20, MaxCopyBytes: 4 << 20}
	return VoxelRtStreamingConfig{
		ManagedAdmission: gpu.DefaultManagedGeometryAdmissionBudget(),
		ManagedFrame:     gpu.DefaultManagedGeometryFrameBudget(),
		SectorLookup:     gpu.DefaultSectorLookupFrameBudget(),
		Upload:           &upload,
		NativeWork:       &work,
	}
}
