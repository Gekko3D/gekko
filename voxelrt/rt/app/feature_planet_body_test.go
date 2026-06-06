package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestPlanetBodyGPUHostsMapRendererInput(t *testing.T) {
	samples := []PlanetBakedSurfaceSampleInput{{Height: 0.25, NormalOctX: 0.1, NormalOctY: 0.2, MaterialBand: 3}}
	inputs := []PlanetBodyInput{
		{
			EntityID:               7,
			Seed:                   8,
			Position:               mgl32.Vec3{1, 2, 3},
			Rotation:               mgl32.QuatIdent(),
			Radius:                 10,
			OceanRadius:            11,
			AtmosphereRadius:       12,
			AtmosphereRimWidth:     0.5,
			HeightAmplitude:        2,
			NoiseScale:             3,
			BlockSize:              4,
			HeightSteps:            5,
			HandoffNearAlt:         6,
			HandoffFarAlt:          7,
			BiomeMix:               0.75,
			BakedSurfaceResolution: 2,
			BakedSurfaceSamples:    samples,
			BakedSurfaceID:         99,
			BandColors:             [6][3]float32{{0.1, 0.2, 0.3}},
			AmbientStrength:        0.4,
			DiffuseStrength:        0.5,
			SpecularStrength:       0.6,
			RimStrength:            0.7,
			EmissionStrength:       0.8,
			TerrainLowColor:        [3]float32{0.1, 0.2, 0.3},
			TerrainHighColor:       [3]float32{0.4, 0.5, 0.6},
			RockColor:              [3]float32{0.7, 0.8, 0.9},
			OceanDeepColor:         [3]float32{0.2, 0.3, 0.4},
			OceanShallowColor:      [3]float32{0.5, 0.6, 0.7},
			AtmosphereColor:        [3]float32{0.8, 0.9, 1},
		},
	}

	hosts := planetBodyGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one planet-body host, got %d", len(hosts))
	}
	if hosts[0].EntityID != inputs[0].EntityID || hosts[0].Radius != inputs[0].Radius || hosts[0].BakedSurfaceID != inputs[0].BakedSurfaceID {
		t.Fatalf("unexpected planet-body host: %+v", hosts[0])
	}
	if len(hosts[0].BakedSurfaceSamples) != len(samples) || hosts[0].BakedSurfaceSamples[0] != (gpu.PlanetBakedSurfaceSampleHost(samples[0])) {
		t.Fatalf("unexpected baked surface samples: %+v", hosts[0].BakedSurfaceSamples)
	}
	if len(planetBodyGPUHosts(nil)) != 0 {
		t.Fatal("expected empty planet-body host output")
	}
}

func TestPlanetBodySurfaceGPUHostsMapRendererInput(t *testing.T) {
	samples := []PlanetBakedSurfaceSampleInput{{Height: 0.5}}
	inputs := []PlanetBodySurfaceInput{
		{BakedSurfaceResolution: 2, BakedSurfaceSamples: samples, BakedSurfaceID: 42},
	}

	hosts := planetBodySurfaceGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one planet-body surface host, got %d", len(hosts))
	}
	if hosts[0].BakedSurfaceResolution != 2 || hosts[0].BakedSurfaceID != 42 {
		t.Fatalf("unexpected planet-body surface host: %+v", hosts[0])
	}
	if len(hosts[0].BakedSurfaceSamples) != len(samples) || hosts[0].BakedSurfaceSamples[0].Height != 0.5 {
		t.Fatalf("unexpected planet-body surface samples: %+v", hosts[0].BakedSurfaceSamples)
	}
	if len(planetBodySurfaceGPUHosts(nil)) != 0 {
		t.Fatal("expected empty planet-body surface host output")
	}
}

func TestClearPlanetBodyInputClearsContributionCount(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{PlanetBodyCount: 2},
	}

	app.ClearPlanetBodyInput()

	if got := app.BufferManager.PlanetBodyCount; got != 0 {
		t.Fatalf("expected planet-body count cleared, got %d", got)
	}
}
