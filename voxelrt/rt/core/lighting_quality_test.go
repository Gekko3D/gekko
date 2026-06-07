package core

import "testing"

func TestLightingQualityDefaultsToBalancedPreset(t *testing.T) {
	cfg := (LightingQualityConfig{}).WithDefaults()
	if cfg.Preset != LightingQualityPresetBalanced {
		t.Fatalf("expected balanced preset, got %q", cfg.Preset)
	}
	if cfg.AmbientOcclusion.SampleCount != 10 {
		t.Fatalf("expected balanced AO sample count 10, got %d", cfg.AmbientOcclusion.SampleCount)
	}
	if got := cfg.Shadow.DirectionalCascadeDistances; got != [DirectionalShadowCascadeCount]float32{48.0, 160.0} {
		t.Fatalf("expected balanced cascade distances [48 160], got %v", got)
	}
	if cfg.Shadow.DirectionalShadowSoftness != 0 {
		t.Fatalf("expected ignored directional shadow softness to default to 0, got %v", cfg.Shadow.DirectionalShadowSoftness)
	}
	if cfg.Shadow.SpotShadowSoftness != 0 {
		t.Fatalf("expected ignored spot shadow softness to default to 0, got %v", cfg.Shadow.SpotShadowSoftness)
	}
}
