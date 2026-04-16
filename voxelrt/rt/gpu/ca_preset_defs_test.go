package gpu

import "testing"

func TestCAVolumePresetDefinitionForReturnsSharedSimData(t *testing.T) {
	def := CAVolumePresetDefinitionFor(3)
	if def.Sim.FireInject != 1.15 {
		t.Fatalf("expected jet flame FireInject 1.15, got %v", def.Sim.FireInject)
	}
	if def.Sim.ScatterScale != 0.1 {
		t.Fatalf("expected jet flame ScatterScale 0.1, got %v", def.Sim.ScatterScale)
	}
}

func TestCAVolumeRenderDefaultsForResolvesTypeSpecificCampfireValues(t *testing.T) {
	smoke := CAVolumeRenderDefaultsFor(2, 0)
	fire := CAVolumeRenderDefaultsFor(2, 1)

	if smoke.Emission != 0.0 {
		t.Fatalf("expected campfire smoke emission 0, got %v", smoke.Emission)
	}
	if smoke.ScatterColor != [3]float32{0.34, 0.35, 0.38} {
		t.Fatalf("unexpected campfire smoke scatter color: %+v", smoke.ScatterColor)
	}
	if fire.Emission != 7.2 {
		t.Fatalf("expected campfire fire emission 7.2, got %v", fire.Emission)
	}
	if fire.ScatterColor != [3]float32{1.0, 0.42, 0.1} {
		t.Fatalf("unexpected campfire fire scatter color: %+v", fire.ScatterColor)
	}
}

func TestCAVolumeRenderDefaultsForFallsBackToDefaultPreset(t *testing.T) {
	got := CAVolumeRenderDefaultsFor(99, 1)
	want := CAVolumeRenderDefaultsFor(0, 1)
	if got != want {
		t.Fatalf("expected unknown preset to fall back to default render values, got %+v want %+v", got, want)
	}
}
