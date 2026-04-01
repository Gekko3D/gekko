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
	if cfg.Shadow.DirectionalShadowSoftness != 0.65 {
		t.Fatalf("expected balanced directional shadow softness 0.65, got %v", cfg.Shadow.DirectionalShadowSoftness)
	}
	if cfg.Shadow.SpotShadowSoftness != 0.40 {
		t.Fatalf("expected balanced spot shadow softness 0.40, got %v", cfg.Shadow.SpotShadowSoftness)
	}
}
