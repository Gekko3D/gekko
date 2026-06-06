package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestAnalyticMediumGPUHostsMapRendererInput(t *testing.T) {
	inputs := []AnalyticMediumInput{
		{
			EntityID:                  7,
			Shape:                     1,
			Position:                  mgl32.Vec3{1, 2, 3},
			Rotation:                  mgl32.QuatIdent(),
			OuterRadius:               10,
			InnerRadius:               2,
			BoxExtents:                [3]float32{4, 5, 6},
			Density:                   0.25,
			Falloff:                   1.5,
			EdgeSoftness:              0.2,
			PhaseG:                    0.3,
			LightStrength:             4,
			AmbientStrength:           5,
			LimbStrength:              6,
			LimbExponent:              7,
			DiskHazeStrength:          8,
			DiskHazeTintMix:           0.4,
			OpaqueExtinctionScale:     0.5,
			BackgroundExtinctionScale: 0.6,
			BoundaryFadeStart:         0.7,
			BoundaryFadeEnd:           0.8,
			OpaqueAlphaScale:          0.9,
			BackgroundAlphaScale:      1.1,
			OpaqueRevealScale:         1.2,
			BackgroundRevealScale:     1.3,
			Color:                     [3]float32{0.1, 0.2, 0.3},
			AbsorptionColor:           [3]float32{0.4, 0.5, 0.6},
			EmissionColor:             [3]float32{0.7, 0.8, 0.9},
			NoiseScale:                11,
			NoiseStrength:             12,
			SampleCount:               24,
			CloudBlockSize:            13,
			CloudThreshold:            14,
			CloudTime:                 15,
			CloudAltitudeSteps:        16,
		},
	}

	hosts := analyticMediumGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one analytic medium host, got %d", len(hosts))
	}
	if hosts[0] != (gpu.AnalyticMediumHost(inputs[0])) {
		t.Fatalf("unexpected analytic medium host: %+v", hosts[0])
	}
	if len(analyticMediumGPUHosts(nil)) != 0 {
		t.Fatal("expected empty analytic medium host output")
	}
}

func TestClearAnalyticMediumInputClearsContributionCount(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{AnalyticMediumCount: 2},
	}

	app.ClearAnalyticMediumInput()

	if got := app.BufferManager.AnalyticMediumCount; got != 0 {
		t.Fatalf("expected analytic medium count cleared, got %d", got)
	}
}
