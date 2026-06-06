package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestFarPlanetRingGPUHostsMapRendererInput(t *testing.T) {
	profile := [32]float32{}
	profile[0] = 0.1
	profile[31] = 0.9
	inputs := []FarPlanetRingInput{
		{
			BandID:                           "ring-a",
			ParentBodyID:                     "planet-a",
			CenterCameraRelativeMeters:       mgl32.Vec3{1, 2, 3},
			NormalCameraRelative:             mgl32.Vec3{0, 1, 0},
			TangentUCameraRelative:           mgl32.Vec3{1, 0, 0},
			TangentVCameraRelative:           mgl32.Vec3{0, 0, 1},
			InnerRadiusMeters:                10,
			OuterRadiusMeters:                20,
			HalfThicknessMeters:              2,
			Tint:                             [3]float32{0.1, 0.2, 0.3},
			Opacity:                          0.4,
			DustHazeOpacity:                  0.5,
			DustHazeMaxAlpha:                 0.6,
			DustHazeThicknessScale:           7,
			DustHazeMinHalfThicknessMeters:   8,
			DustHazeRadialEdgeFadeFraction:   0.9,
			DustHazeVerticalCoreFraction:     0.11,
			DustHazeSampleCount:              5,
			DustHazeForwardScatterStrength:   0.12,
			DustHazeShadowStrength:           0.13,
			Seed:                             14,
			RadialOpacityProfile:             profile,
			ParentCenterCameraRelativeMeters: mgl32.Vec3{4, 5, 6},
			ParentRadiusMeters:               30,
			ParentDepthMeters:                40,
			LightDirectionViewSpace:          mgl32.Vec3{0, -1, 0},
		},
	}

	hosts := farPlanetRingGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one far planet-ring host, got %d", len(hosts))
	}
	if hosts[0] != (gpu.FarPlanetRingHost(inputs[0])) {
		t.Fatalf("unexpected far planet-ring host: %+v", hosts[0])
	}
	if len(farPlanetRingGPUHosts(nil)) != 0 {
		t.Fatal("expected empty far planet-ring host output")
	}
}

func TestClearFarPlanetRingInputClearsContributionCount(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{FarPlanetRingCount: 2},
	}

	app.ClearFarPlanetRingInput()

	if got := app.BufferManager.FarPlanetRingCount; got != 0 {
		t.Fatalf("expected far planet-ring count cleared, got %d", got)
	}
}
