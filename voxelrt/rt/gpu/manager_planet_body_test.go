package gpu

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildPlanetBodyRecordsPreservesStableLayout(t *testing.T) {
	records, params := buildPlanetBodyRecords([]PlanetBodyHost{
		{
			EntityID:         17,
			Seed:             99,
			Position:         mgl32.Vec3{1, 2, 3},
			Rotation:         mgl32.Quat{V: mgl32.Vec3{0.1, 0.2, 0.3}, W: 0.9},
			Radius:           10,
			OceanRadius:      11,
			AtmosphereRadius: 12,
			HeightAmplitude:  2,
			NoiseScale:       3.5,
			BlockSize:        4,
			HeightSteps:      7,
			HandoffNearAlt:   13,
			HandoffFarAlt:    47,
			BiomeMix:         0.65,
			BandColors: [6][3]float32{
				{0.01, 0.02, 0.03},
				{0.11, 0.12, 0.13},
				{0.21, 0.22, 0.23},
				{0.31, 0.32, 0.33},
				{0.41, 0.42, 0.43},
				{0.51, 0.52, 0.53},
			},
			AmbientStrength:  0.2,
			DiffuseStrength:  1.3,
			SpecularStrength: 0.4,
			RimStrength:      0.5,
			AtmosphereColor:  [3]float32{0.77, 0.88, 0.99},
		},
	})

	if params.PlanetCount != 1 {
		t.Fatalf("expected planet count 1, got %d", params.PlanetCount)
	}
	if len(records) != 1 {
		t.Fatalf("expected one packed record, got %d", len(records))
	}
	if got := records[0].Bounds[3]; got != 10 {
		t.Fatalf("expected radius 10, got %v", got)
	}
	if got := records[0].Surface[0]; got != 11 {
		t.Fatalf("expected ocean radius 11, got %v", got)
	}
	if got := records[0].Noise[1]; got != 7 {
		t.Fatalf("expected height steps 7, got %v", got)
	}
	if got := records[0].Noise[2]; got != 99 {
		t.Fatalf("expected seed 99, got %v", got)
	}
	if got := records[0].Noise[3]; got != 0.65 {
		t.Fatalf("expected biome mix 0.65, got %v", got)
	}
	if got := records[0].Style[1]; got != 1.3 {
		t.Fatalf("expected diffuse strength 1.3, got %v", got)
	}
	if got := records[0].Style[3]; got != 0.5 {
		t.Fatalf("expected rim strength 0.5, got %v", got)
	}
	if got := records[0].Band4[0]; got != 0.41 {
		t.Fatalf("expected band4 red 0.41, got %v", got)
	}
	if got := records[0].Atmosphere[3]; got != 13 {
		t.Fatalf("expected handoff near altitude 13, got %v", got)
	}
	if got := records[0].Band5[3]; got != 47 {
		t.Fatalf("expected handoff far altitude 47, got %v", got)
	}
}
