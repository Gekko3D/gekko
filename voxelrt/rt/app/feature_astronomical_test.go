package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestAstronomicalGPUHostsMapRendererInput(t *testing.T) {
	inputs := []AstronomicalBodyInput{
		{
			Kind:                      2,
			DirectionViewSpace:        mgl32.Vec3{0, 0, -1},
			LightDirectionViewSpace:   mgl32.Vec3{0, 1, 0},
			AngularRadiusRad:          0.1,
			GlowAngularRadiusRad:      0.2,
			RingInnerAngularRadiusRad: 0.3,
			RingOuterAngularRadiusRad: 0.4,
			PhaseLight01:              0.5,
			BodyTint:                  [3]float32{0.6, 0.7, 0.8},
			EmissionStrength:          0.9,
			Seed:                      10,
			OcclusionPriority:         11,
			RingNormalViewSpace:       mgl32.Vec3{0, 1, 0},
			RingInnerRadiusMeters:     12,
			RingOuterRadiusMeters:     13,
			RingDistanceMeters:        14,
			RingParentPlanetRadius:    15,
		},
	}

	hosts := astronomicalGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one astronomical host, got %d", len(hosts))
	}
	if hosts[0] != (gpu.AstronomicalBodyHost(inputs[0])) {
		t.Fatalf("unexpected astronomical host: %+v", hosts[0])
	}
	if len(astronomicalGPUHosts(nil)) != 0 {
		t.Fatal("expected empty astronomical host output")
	}
}

func TestClearAstronomicalInputClearsContributionCount(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{AstronomicalBodyCount: 2},
	}

	app.ClearAstronomicalInput()

	if got := app.BufferManager.AstronomicalBodyCount; got != 0 {
		t.Fatalf("expected astronomical body count cleared, got %d", got)
	}
}
