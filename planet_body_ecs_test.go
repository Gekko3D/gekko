package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestPlanetBodyComponentNormalizationAndWorldScaling(t *testing.T) {
	var nilPlanet *PlanetBodyComponent
	if nilPlanet.Enabled() {
		t.Fatal("expected nil planet body to be disabled")
	}

	planet := &PlanetBodyComponent{
		Radius:             10,
		OceanRadius:        9,
		AtmosphereRadius:   10.5,
		AtmosphereRimWidth: 1.5,
		HeightAmplitude:    3,
		NoiseScale:         0,
		BlockSize:          0,
		HeightSteps:        0,
		HandoffNearAlt:     8,
		HandoffFarAlt:      20,
		BiomeMix:           2,
		BandColors:         [6][3]float32{{2, -1, 0.5}},
		AmbientStrength:    2,
		DiffuseStrength:    5,
		SpecularStrength:   -1,
		RimStrength:        -1,
		EmissionStrength:   6,
	}
	tr := &TransformComponent{
		Position: mgl32.Vec3{4, 5, 6},
		Rotation: mgl32.QuatRotate(mgl32.DegToRad(20), mgl32.Vec3{0, 1, 0}),
		Scale:    mgl32.Vec3{2, 4, 6},
	}

	if !planet.Enabled() {
		t.Fatal("expected planet body to be enabled")
	}
	if got := planet.NormalizedOceanRadius(); got != 10 {
		t.Fatalf("expected ocean radius clamped to base radius, got %v", got)
	}
	if got := planet.NormalizedAtmosphereRadius(); got != 13 {
		t.Fatalf("expected atmosphere radius lifted above solid shell, got %v", got)
	}
	if got := planet.NormalizedNoiseScale(); got != 2.2 {
		t.Fatalf("expected default noise scale, got %v", got)
	}
	if got := planet.NormalizedBlockSize(); got < 1 {
		t.Fatalf("expected derived block size >= 1, got %v", got)
	}
	if got := planet.NormalizedHeightSteps(); got != 6 {
		t.Fatalf("expected default height steps, got %d", got)
	}
	if got := planet.NormalizedHandoffNearAlt(); got != 8 {
		t.Fatalf("expected explicit handoff near altitude 8, got %v", got)
	}
	if got := planet.NormalizedHandoffFarAlt(); got != 20 {
		t.Fatalf("expected explicit handoff far altitude 20, got %v", got)
	}
	if got := planet.NormalizedBiomeMix(); got != 1 {
		t.Fatalf("expected biome mix clamped to 1, got %v", got)
	}
	if got := planet.NormalizedBakedSurfaceResolution(); got != 0 {
		t.Fatalf("expected missing baked surface data to report resolution 0, got %d", got)
	}
	if got := planet.NormalizedBakedSurfaceSampleCount(); got != 0 {
		t.Fatalf("expected missing baked surface data to report zero samples, got %d", got)
	}
	bands := planet.NormalizedBandColors()
	if bands[0] != ([3]float32{1, 0, 0.5}) {
		t.Fatalf("expected first band color clamped, got %v", bands[0])
	}
	if got := planet.NormalizedAmbientStrength(); got != 1 {
		t.Fatalf("expected ambient strength clamped to 1, got %v", got)
	}
	if got := planet.NormalizedDiffuseStrength(); got != 4 {
		t.Fatalf("expected diffuse strength clamped to 4, got %v", got)
	}
	if got := planet.NormalizedSpecularStrength(); got != 0.08 {
		t.Fatalf("expected default specular strength, got %v", got)
	}
	if got := planet.NormalizedRimStrength(); got != 0.35 {
		t.Fatalf("expected default rim strength, got %v", got)
	}
	if got := planet.NormalizedEmissionStrength(); got != 4 {
		t.Fatalf("expected emission strength clamped to 4, got %v", got)
	}
	if got := planet.WorldCenter(tr); got != tr.Position {
		t.Fatalf("expected world center %v, got %v", tr.Position, got)
	}
	if got := planet.WorldRadius(tr); got != 40 {
		t.Fatalf("expected scaled radius 40, got %v", got)
	}
	if got := planet.WorldHeightAmplitude(tr); got != 12 {
		t.Fatalf("expected scaled height amplitude 12, got %v", got)
	}
	if got := planet.WorldHandoffNearAlt(tr); got != 32 {
		t.Fatalf("expected scaled handoff near altitude 32, got %v", got)
	}
	if got := planet.WorldHandoffFarAlt(tr); got != 80 {
		t.Fatalf("expected scaled handoff far altitude 80, got %v", got)
	}
	if got := planet.WorldAtmosphereRadius(tr); got != 52 {
		t.Fatalf("expected scaled atmosphere radius 52, got %v", got)
	}
	if got := planet.WorldAtmosphereRimWidth(tr); got != 6 {
		t.Fatalf("expected scaled atmosphere rim width 6, got %v", got)
	}
}

func TestPlanetBodyComponentNormalizedBakedSurfaceResolution(t *testing.T) {
	planet := &PlanetBodyComponent{BakedSurfaceResolution: 2048}
	if got := planet.NormalizedBakedSurfaceResolution(); got != PlanetBodyMaxBakedSurfaceResolution {
		t.Fatalf("expected baked surface resolution clamp to %d, got %d", PlanetBodyMaxBakedSurfaceResolution, got)
	}
	if got := planet.NormalizedBakedSurfaceSampleCount(); got != PlanetBodyMaxBakedSurfaceResolution*PlanetBodyMaxBakedSurfaceResolution*planetBodyBakedSurfaceFaceCount {
		t.Fatalf("unexpected baked surface sample count %d", got)
	}
}
