package gpu

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildPlanetBodyRecordsPreservesStableLayout(t *testing.T) {
	records, heights, params := buildPlanetBodyRecords([]PlanetBodyHost{
		{
			EntityID:           17,
			Seed:               99,
			Position:           mgl32.Vec3{1, 2, 3},
			Rotation:           mgl32.Quat{V: mgl32.Vec3{0.1, 0.2, 0.3}, W: 0.9},
			Radius:             10,
			OceanRadius:        11,
			AtmosphereRadius:   12,
			AtmosphereRimWidth: 2.5,
			HeightAmplitude:    2,
			NoiseScale:         3.5,
			BlockSize:          4,
			HeightSteps:        7,
			HandoffNearAlt:     13,
			HandoffFarAlt:      47,
			BiomeMix:           0.65,
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
			EmissionStrength: 1.75,
			AtmosphereColor:  [3]float32{0.77, 0.88, 0.99},
		},
	})

	if params.PlanetCount != 1 {
		t.Fatalf("expected planet count 1, got %d", params.PlanetCount)
	}
	if len(records) != 1 {
		t.Fatalf("expected one packed record, got %d", len(records))
	}
	if len(heights) != 1 {
		t.Fatalf("expected sentinel baked-height sample, got %d values", len(heights))
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
	if got := records[0].Emission[0]; got != 1.75 {
		t.Fatalf("expected emission strength 1.75, got %v", got)
	}
	if got := records[0].Band4[0]; got != 0.41 {
		t.Fatalf("expected band4 red 0.41, got %v", got)
	}
	if got := records[0].Atmosphere[3]; got != 2.5 {
		t.Fatalf("expected atmosphere rim width 2.5, got %v", got)
	}
	if got := records[0].Band5[3]; got != 47 {
		t.Fatalf("expected handoff far altitude 47, got %v", got)
	}
}

func TestBuildPlanetBodyRecordsPacksBakedSurfaceSamples(t *testing.T) {
	baked := []PlanetBakedSurfaceSampleHost{
		{Height: -2, NormalOctX: -2, NormalOctY: 2, MaterialBand: -3},
		{Height: -0.5, NormalOctX: -0.5, NormalOctY: 0.5, MaterialBand: 1},
		{Height: 0.25, NormalOctX: 0.25, NormalOctY: -0.25, MaterialBand: 2},
		{Height: 2, NormalOctX: 2, NormalOctY: -2, MaterialBand: 9},
		{Height: 0.1}, {Height: 0.2}, {Height: 0.3}, {Height: 0.4},
		{Height: 0.5}, {Height: 0.6}, {Height: 0.7}, {Height: 0.8},
		{Height: 0.9}, {Height: 1.0}, {Height: -1.0}, {Height: -0.9},
		{Height: -0.8}, {Height: -0.7}, {Height: -0.6}, {Height: -0.5},
		{Height: -0.4}, {Height: -0.3}, {Height: -0.2}, {Height: -0.1},
		{Height: 99},
	}
	records, heights, _ := buildPlanetBodyRecords([]PlanetBodyHost{
		{
			Radius:                 10,
			HeightAmplitude:        2,
			BakedSurfaceResolution: 2,
			BakedSurfaceSamples:    baked,
		},
	})

	if got := records[0].BakeMeta[0]; got != 2 {
		t.Fatalf("expected baked height resolution 2, got %d", got)
	}
	if got := records[0].BakeMeta[1]; got != 1 {
		t.Fatalf("expected baked height offset 1, got %d", got)
	}
	if got := records[0].BakeMeta[2]; got != 24 {
		t.Fatalf("expected baked height sample count 24, got %d", got)
	}
	if len(heights) != 25 {
		t.Fatalf("expected sentinel plus 24 baked surface samples, got %d values", len(heights))
	}
	if heights[1].Height != -1 || heights[4].Height != 1 {
		t.Fatalf("expected baked heights clamped into [-1,1], got first row %+v", heights[1:5])
	}
	if heights[1].NormalOctX != -1 || heights[1].NormalOctY != 1 {
		t.Fatalf("expected baked oct normals clamped into [-1,1], got %+v", heights[1])
	}
	if heights[1].MaterialBand != 0 || heights[4].MaterialBand != 5 {
		t.Fatalf("expected baked material bands clamped into [0,5], got %+v %+v", heights[1], heights[4])
	}
}

func TestBuildPlanetBodyRecordsIgnoresIncompleteBakedSurfaceSamples(t *testing.T) {
	records, heights, _ := buildPlanetBodyRecords([]PlanetBodyHost{
		{
			Radius:                 10,
			HeightAmplitude:        2,
			BakedSurfaceResolution: 2,
			BakedSurfaceSamples:    []PlanetBakedSurfaceSampleHost{{Height: 0.1}, {Height: 0.2}},
		},
	})

	if got := records[0].BakeMeta[0]; got != 0 {
		t.Fatalf("expected invalid baked height resolution to be disabled, got %d", got)
	}
	if got := records[0].BakeMeta[2]; got != 0 {
		t.Fatalf("expected invalid baked height sample count to be zero, got %d", got)
	}
	if len(heights) != 1 {
		t.Fatalf("expected only sentinel height sample when bake is invalid, got %d", len(heights))
	}
}
