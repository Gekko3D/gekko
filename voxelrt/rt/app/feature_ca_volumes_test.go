package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestCAVolumeGPUHostsMapRendererInput(t *testing.T) {
	input := CAVolumeInput{
		EntityID:        42,
		Type:            2,
		Preset:          3,
		Resolution:      [3]uint32{16, 24, 32},
		Position:        mgl32.Vec3{1, 2, 3},
		Rotation:        mgl32.QuatIdent(),
		VoxelScale:      mgl32.Vec3{0.5, 0.75, 1.25},
		Intensity:       0.8,
		Diffusion:       0.1,
		Buoyancy:        0.2,
		Cooling:         0.3,
		Dissipation:     0.4,
		Extinction:      0.5,
		Emission:        0.6,
		StepsPending:    2,
		StepDt:          1.0 / 30.0,
		ScatterColor:    [3]float32{0.7, 0.8, 0.9},
		ShadowTint:      [3]float32{0.2, 0.3, 0.4},
		AbsorptionColor: [3]float32{0.1, 0.15, 0.2},
	}

	hosts := caVolumeGPUHosts([]CAVolumeInput{input})
	if len(hosts) != 1 {
		t.Fatalf("expected one CA volume host, got %d", len(hosts))
	}
	got := hosts[0]
	if got.EntityID != input.EntityID ||
		got.Type != input.Type ||
		got.Preset != input.Preset ||
		got.Resolution != input.Resolution ||
		got.Position != input.Position ||
		got.Rotation != input.Rotation ||
		got.VoxelScale != input.VoxelScale ||
		got.Intensity != input.Intensity ||
		got.Diffusion != input.Diffusion ||
		got.Buoyancy != input.Buoyancy ||
		got.Cooling != input.Cooling ||
		got.Dissipation != input.Dissipation ||
		got.Extinction != input.Extinction ||
		got.Emission != input.Emission ||
		got.StepsPending != input.StepsPending ||
		got.StepDt != input.StepDt ||
		got.ScatterColor != input.ScatterColor ||
		got.ShadowTint != input.ShadowTint ||
		got.AbsorptionColor != input.AbsorptionColor {
		t.Fatalf("CA volume host = %+v, want fields from %+v", got, input)
	}
}

func TestClearCAVolumeInputClearsRendererState(t *testing.T) {
	app := &App{
		CAVolumeResources: &CAVolumeResources{HadPass: true},
		BufferManager: &gpu.GpuBufferManager{
			CAVolumeCount:             3,
			CAVolumeVisibleCount:      2,
			CARequestedVolumeCount:    4,
			CAResolutionClampedCount:  1,
			CADeferredStepVolumeCount: 2,
			CASuspendedVolumeCount:    1,
			CADroppedVolumeCount:      1,
			CATotalScheduledSteps:     5,
			CAAtlasCellCount:          128,
			CAAtlasByteCount:          2048,
		},
	}

	app.ClearCAVolumeInput()

	if app.HadCAVolumePass() {
		t.Fatal("expected previous CA volume pass state to be cleared")
	}
	if app.BufferManager.CAVolumeCount != 0 ||
		app.BufferManager.CAVolumeVisibleCount != 0 ||
		app.BufferManager.CARequestedVolumeCount != 0 ||
		app.BufferManager.CAResolutionClampedCount != 0 ||
		app.BufferManager.CADeferredStepVolumeCount != 0 ||
		app.BufferManager.CASuspendedVolumeCount != 0 ||
		app.BufferManager.CADroppedVolumeCount != 0 ||
		app.BufferManager.CATotalScheduledSteps != 0 ||
		app.BufferManager.CAAtlasCellCount != 0 ||
		app.BufferManager.CAAtlasByteCount != 0 {
		t.Fatalf("expected CA volume renderer counters to be cleared, got %+v", app.BufferManager)
	}
	if !app.BufferManager.CAVolumeBindingsDirty {
		t.Fatal("expected CA volume bindings to be marked dirty")
	}
}
