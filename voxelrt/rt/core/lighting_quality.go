package core

type LightingQualityPreset string

const (
	LightingQualityPresetPerformance LightingQualityPreset = "performance"
	LightingQualityPresetBalanced    LightingQualityPreset = "balanced"
	LightingQualityPresetQuality     LightingQualityPreset = "quality"
)

type AmbientOcclusionQuality struct {
	SampleCount uint32
	Radius      float32
}

type ShadowQualityConfig struct {
	DirectionalCascadeDistances [DirectionalShadowCascadeCount]float32
	SpotShadowDistanceBands     [3]float32
	DirectionalShadowSoftness   float32
	SpotShadowSoftness          float32
}

type LightingQualityConfig struct {
	Preset           LightingQualityPreset
	AmbientOcclusion AmbientOcclusionQuality
	Shadow           ShadowQualityConfig
}

func DefaultLightingQualityConfig() LightingQualityConfig {
	return LightingQualityPresetConfig(LightingQualityPresetBalanced)
}

func LightingQualityPresetConfig(preset LightingQualityPreset) LightingQualityConfig {
	switch preset {
	case LightingQualityPresetPerformance:
		return LightingQualityConfig{
			Preset: LightingQualityPresetPerformance,
			AmbientOcclusion: AmbientOcclusionQuality{
				SampleCount: 6,
				Radius:      1.0,
			},
			Shadow: ShadowQualityConfig{
				DirectionalCascadeDistances: [DirectionalShadowCascadeCount]float32{36.0, 112.0},
				SpotShadowDistanceBands:     [3]float32{18.0, 42.0, 88.0},
				DirectionalShadowSoftness:   0.35,
				SpotShadowSoftness:          0.20,
			},
		}
	case LightingQualityPresetQuality:
		return LightingQualityConfig{
			Preset: LightingQualityPresetQuality,
			AmbientOcclusion: AmbientOcclusionQuality{
				SampleCount: 13,
				Radius:      2.0,
			},
			Shadow: ShadowQualityConfig{
				DirectionalCascadeDistances: [DirectionalShadowCascadeCount]float32{64.0, 224.0},
				SpotShadowDistanceBands:     [3]float32{32.0, 72.0, 144.0},
				DirectionalShadowSoftness:   1.00,
				SpotShadowSoftness:          0.70,
			},
		}
	default:
		return LightingQualityConfig{
			Preset: LightingQualityPresetBalanced,
			AmbientOcclusion: AmbientOcclusionQuality{
				SampleCount: 10,
				Radius:      1.0,
			},
			Shadow: ShadowQualityConfig{
				DirectionalCascadeDistances: [DirectionalShadowCascadeCount]float32{48.0, 160.0},
				SpotShadowDistanceBands:     [3]float32{24.0, 56.0, 120.0},
				DirectionalShadowSoftness:   0.65,
				SpotShadowSoftness:          0.40,
			},
		}
	}
}

func (cfg LightingQualityConfig) WithDefaults() LightingQualityConfig {
	preset := cfg.Preset
	if preset == "" {
		preset = LightingQualityPresetBalanced
	}
	base := LightingQualityPresetConfig(preset)

	cfg.Preset = base.Preset
	if cfg.AmbientOcclusion.SampleCount == 0 {
		cfg.AmbientOcclusion.SampleCount = base.AmbientOcclusion.SampleCount
	}
	if cfg.AmbientOcclusion.SampleCount > 13 {
		cfg.AmbientOcclusion.SampleCount = 13
	}
	if cfg.AmbientOcclusion.Radius <= 0 {
		cfg.AmbientOcclusion.Radius = base.AmbientOcclusion.Radius
	}

	if cfg.Shadow.DirectionalCascadeDistances == [DirectionalShadowCascadeCount]float32{} {
		cfg.Shadow.DirectionalCascadeDistances = base.Shadow.DirectionalCascadeDistances
	}
	if cfg.Shadow.SpotShadowDistanceBands == [3]float32{} {
		cfg.Shadow.SpotShadowDistanceBands = base.Shadow.SpotShadowDistanceBands
	}
	if cfg.Shadow.DirectionalShadowSoftness <= 0 {
		cfg.Shadow.DirectionalShadowSoftness = base.Shadow.DirectionalShadowSoftness
	}
	if cfg.Shadow.SpotShadowSoftness <= 0 {
		cfg.Shadow.SpotShadowSoftness = base.Shadow.SpotShadowSoftness
	}
	if cfg.Shadow.DirectionalShadowSoftness > 1 {
		cfg.Shadow.DirectionalShadowSoftness = 1
	}
	if cfg.Shadow.SpotShadowSoftness > 1 {
		cfg.Shadow.SpotShadowSoftness = 1
	}

	return cfg
}
