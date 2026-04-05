package gekko

import "github.com/go-gl/mathgl/mgl32"

func NewAtmosphereMedium(innerRadius, outerRadius float32, color [3]float32) *AnalyticMediumComponent {
	thickness := outerRadius - innerRadius
	if thickness < 0.02 {
		thickness = 0.02
	}
	edgeSoftness := thickness * 0.42
	if edgeSoftness < 0.03 {
		edgeSoftness = 0.03
	}
	return &AnalyticMediumComponent{
		Shape:                     AnalyticMediumShapeSphere,
		OuterRadius:               outerRadius,
		InnerRadius:               innerRadius,
		Density:                   0.08,
		Falloff:                   2.1,
		EdgeSoftness:              edgeSoftness,
		PhaseG:                    0.35,
		LightStrength:             0.35,
		AmbientStrength:           0.12,
		LimbStrength:              1.1,
		LimbExponent:              2.4,
		DiskHazeStrength:          0.38,
		DiskHazeTintMix:           0.3,
		OpaqueExtinctionScale:     0.42,
		BackgroundExtinctionScale: 0.12,
		BoundaryFadeStart:         0.58,
		BoundaryFadeEnd:           1.0,
		OpaqueAlphaScale:          1.35,
		BackgroundAlphaScale:      0.6,
		OpaqueRevealScale:         0.2,
		BackgroundRevealScale:     0.08,
		Color:                     color,
		AbsorptionColor: [3]float32{
			clamp01(color[0]*0.42 + 0.08),
			clamp01(color[1]*0.42 + 0.08),
			clamp01(color[2]*0.42 + 0.08),
		},
		NoiseScale:    4.5,
		NoiseStrength: 0.04,
		SampleCount:   8,
	}
}

func NewFogSphereMedium(radius float32, color [3]float32) *AnalyticMediumComponent {
	return &AnalyticMediumComponent{
		Shape:                     AnalyticMediumShapeSphere,
		OuterRadius:               radius,
		Density:                   0.12,
		Falloff:                   0.9,
		EdgeSoftness:              max(radius*0.18, 0.2),
		PhaseG:                    0.1,
		LightStrength:             0.55,
		AmbientStrength:           0.45,
		OpaqueExtinctionScale:     1.0,
		BackgroundExtinctionScale: 0.55,
		OpaqueAlphaScale:          1.0,
		BackgroundAlphaScale:      0.8,
		OpaqueRevealScale:         0.18,
		BackgroundRevealScale:     0.1,
		Color:                     color,
		AbsorptionColor: [3]float32{
			clamp01(color[0]*0.55 + 0.04),
			clamp01(color[1]*0.55 + 0.04),
			clamp01(color[2]*0.55 + 0.04),
		},
		NoiseScale:    0.7,
		NoiseStrength: 0.1,
		SampleCount:   8,
	}
}

func NewFogBoxMedium(halfExtents mgl32.Vec3, color [3]float32) *AnalyticMediumComponent {
	softness := min(min(halfExtents.X(), halfExtents.Y()), halfExtents.Z()) * 0.18
	if softness < 0.2 {
		softness = 0.2
	}
	return &AnalyticMediumComponent{
		Shape:                     AnalyticMediumShapeBox,
		BoxExtents:                [3]float32{halfExtents.X(), halfExtents.Y(), halfExtents.Z()},
		Density:                   0.12,
		Falloff:                   0.85,
		EdgeSoftness:              softness,
		PhaseG:                    0.08,
		LightStrength:             0.5,
		AmbientStrength:           0.48,
		OpaqueExtinctionScale:     1.0,
		BackgroundExtinctionScale: 0.65,
		OpaqueAlphaScale:          1.0,
		BackgroundAlphaScale:      0.85,
		OpaqueRevealScale:         0.16,
		BackgroundRevealScale:     0.1,
		Color:                     color,
		AbsorptionColor: [3]float32{
			clamp01(color[0]*0.58 + 0.04),
			clamp01(color[1]*0.58 + 0.04),
			clamp01(color[2]*0.58 + 0.04),
		},
		NoiseScale:    0.45,
		NoiseStrength: 0.08,
		SampleCount:   8,
	}
}
