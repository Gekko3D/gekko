package gekko

import "github.com/go-gl/mathgl/mgl32"

type AnalyticMediumShape uint32

const (
	AnalyticMediumShapeSphere AnalyticMediumShape = iota
	AnalyticMediumShapeBox
)

// AnalyticMediumComponent describes a bounded non-voxel medium rendered by a
// dedicated analytic accumulation pass. Radii are expressed in world units.
type AnalyticMediumComponent struct {
	Disabled bool
	Shape    AnalyticMediumShape

	OuterRadius float32
	InnerRadius float32
	BoxExtents  [3]float32

	Density                   float32
	Falloff                   float32
	EdgeSoftness              float32
	PhaseG                    float32
	LightStrength             float32
	AmbientStrength           float32
	LimbStrength              float32
	LimbExponent              float32
	DiskHazeStrength          float32
	DiskHazeTintMix           float32
	OpaqueExtinctionScale     float32
	BackgroundExtinctionScale float32
	BoundaryFadeStart         float32
	BoundaryFadeEnd           float32
	OpaqueAlphaScale          float32
	BackgroundAlphaScale      float32
	OpaqueRevealScale         float32
	BackgroundRevealScale     float32

	Color           [3]float32
	AbsorptionColor [3]float32
	EmissionColor   [3]float32

	NoiseScale    float32
	NoiseStrength float32
	SampleCount   int

	CloudBlockSize     float32
	CloudThreshold     float32
	CloudSpeed         float32
	CloudAltitudeSteps float32
}

func (m *AnalyticMediumComponent) Enabled() bool {
	return m != nil && !m.Disabled && m.HasValidBounds() && m.Density > 0
}

func (m *AnalyticMediumComponent) NormalizedShape() AnalyticMediumShape {
	if m == nil {
		return AnalyticMediumShapeSphere
	}
	switch m.Shape {
	case AnalyticMediumShapeSphere:
		return m.Shape
	case AnalyticMediumShapeBox:
		return m.Shape
	default:
		return AnalyticMediumShapeSphere
	}
}

func (m *AnalyticMediumComponent) HasValidBounds() bool {
	if m == nil {
		return false
	}
	switch m.NormalizedShape() {
	case AnalyticMediumShapeBox:
		ext := m.NormalizedBoxExtents()
		return ext[0] > 0 && ext[1] > 0 && ext[2] > 0
	default:
		return m.OuterRadius > 0
	}
}

func (m *AnalyticMediumComponent) NormalizedOuterRadius() float32 {
	if m == nil || m.OuterRadius <= 0 {
		return 0
	}
	return m.OuterRadius
}

func (m *AnalyticMediumComponent) NormalizedInnerRadius() float32 {
	if m == nil {
		return 0
	}
	if m.NormalizedShape() != AnalyticMediumShapeSphere {
		return 0
	}
	outer := m.NormalizedOuterRadius()
	if outer <= 0 {
		return 0
	}
	inner := m.InnerRadius
	if inner < 0 {
		inner = 0
	}
	maxInner := outer - 1e-3
	if maxInner < 0 {
		maxInner = 0
	}
	if inner > maxInner {
		inner = maxInner
	}
	return inner
}

func (m *AnalyticMediumComponent) NormalizedBoxExtents() [3]float32 {
	if m == nil {
		return [3]float32{}
	}
	ext := m.BoxExtents
	for i := range ext {
		if ext[i] < 0 {
			ext[i] = 0
		}
	}
	return ext
}

func (m *AnalyticMediumComponent) NormalizedFalloff() float32 {
	if m == nil || m.Falloff <= 0 {
		return 1.35
	}
	return m.Falloff
}

func (m *AnalyticMediumComponent) NormalizedEdgeSoftness() float32 {
	if m == nil {
		return 0.02
	}
	if m.EdgeSoftness > 0 {
		return m.EdgeSoftness
	}
	thickness := float32(0)
	if m.NormalizedShape() == AnalyticMediumShapeBox {
		ext := m.NormalizedBoxExtents()
		thickness = min(min(ext[0], ext[1]), ext[2]) * 0.18
	} else {
		thickness = m.NormalizedOuterRadius() - m.NormalizedInnerRadius()
	}
	if thickness <= 0 {
		return 0.02
	}
	softness := thickness
	if m.NormalizedShape() != AnalyticMediumShapeBox {
		softness = thickness * 0.18
	}
	if softness < 0.02 {
		softness = 0.02
	}
	return softness
}

func (m *AnalyticMediumComponent) NormalizedPhaseG() float32 {
	if m == nil {
		return 0
	}
	if m.PhaseG < -0.85 {
		return -0.85
	}
	if m.PhaseG > 0.85 {
		return 0.85
	}
	return m.PhaseG
}

func (m *AnalyticMediumComponent) NormalizedLightStrength() float32 {
	if m == nil || m.LightStrength <= 0 {
		return 1
	}
	return m.LightStrength
}

func (m *AnalyticMediumComponent) NormalizedAmbientStrength() float32 {
	if m == nil || m.AmbientStrength <= 0 {
		return 0.42
	}
	return m.AmbientStrength
}

func (m *AnalyticMediumComponent) NormalizedLimbStrength() float32 {
	if m == nil || m.LimbStrength < 0 {
		return 0
	}
	if m.LimbStrength > 4 {
		return 4
	}
	return m.LimbStrength
}

func (m *AnalyticMediumComponent) NormalizedLimbExponent() float32 {
	if m == nil || m.LimbExponent <= 0 {
		return 2
	}
	if m.LimbExponent < 0.25 {
		return 0.25
	}
	if m.LimbExponent > 8 {
		return 8
	}
	return m.LimbExponent
}

func (m *AnalyticMediumComponent) NormalizedDiskHazeStrength() float32 {
	if m == nil || m.DiskHazeStrength < 0 {
		return 0
	}
	if m.DiskHazeStrength > 4 {
		return 4
	}
	return m.DiskHazeStrength
}

func (m *AnalyticMediumComponent) NormalizedDiskHazeTintMix() float32 {
	if m == nil {
		return 0
	}
	return clamp01(m.DiskHazeTintMix)
}

func (m *AnalyticMediumComponent) NormalizedOpaqueExtinctionScale() float32 {
	if m == nil || m.OpaqueExtinctionScale <= 0 {
		return 1
	}
	return m.OpaqueExtinctionScale
}

func (m *AnalyticMediumComponent) NormalizedBackgroundExtinctionScale() float32 {
	if m == nil || m.BackgroundExtinctionScale <= 0 {
		return 1
	}
	return m.BackgroundExtinctionScale
}

func (m *AnalyticMediumComponent) NormalizedBoundaryFadeStart() float32 {
	if m == nil {
		return 1
	}
	return clamp01(m.BoundaryFadeStart)
}

func (m *AnalyticMediumComponent) NormalizedBoundaryFadeEnd() float32 {
	if m == nil {
		return 1
	}
	end := clamp01(m.BoundaryFadeEnd)
	start := m.NormalizedBoundaryFadeStart()
	if end <= start {
		return start
	}
	return end
}

func (m *AnalyticMediumComponent) NormalizedOpaqueAlphaScale() float32 {
	if m == nil || m.OpaqueAlphaScale <= 0 {
		return 1.35
	}
	return m.OpaqueAlphaScale
}

func (m *AnalyticMediumComponent) NormalizedBackgroundAlphaScale() float32 {
	if m == nil || m.BackgroundAlphaScale <= 0 {
		return 0.6
	}
	return m.BackgroundAlphaScale
}

func (m *AnalyticMediumComponent) NormalizedOpaqueRevealScale() float32 {
	if m == nil || m.OpaqueRevealScale <= 0 {
		return 0.2
	}
	return m.OpaqueRevealScale
}

func (m *AnalyticMediumComponent) NormalizedBackgroundRevealScale() float32 {
	if m == nil || m.BackgroundRevealScale <= 0 {
		return 0.08
	}
	return m.BackgroundRevealScale
}

func (m *AnalyticMediumComponent) NormalizedColor() [3]float32 {
	if m == nil {
		return [3]float32{1, 1, 1}
	}
	if m.Color == [3]float32{} {
		return [3]float32{1, 1, 1}
	}
	return [3]float32{
		clamp01(m.Color[0]),
		clamp01(m.Color[1]),
		clamp01(m.Color[2]),
	}
}

func (m *AnalyticMediumComponent) NormalizedAbsorptionColor() [3]float32 {
	if m == nil || m.AbsorptionColor == [3]float32{} {
		c := m.NormalizedColor()
		return [3]float32{
			clamp01(c[0]*0.42 + 0.08),
			clamp01(c[1]*0.42 + 0.08),
			clamp01(c[2]*0.42 + 0.08),
		}
	}
	return [3]float32{
		clamp01(m.AbsorptionColor[0]),
		clamp01(m.AbsorptionColor[1]),
		clamp01(m.AbsorptionColor[2]),
	}
}

func (m *AnalyticMediumComponent) NormalizedEmissionColor() [3]float32 {
	if m == nil {
		return [3]float32{}
	}
	if m.EmissionColor == [3]float32{} {
		return [3]float32{}
	}
	return [3]float32{
		clamp01(m.EmissionColor[0]),
		clamp01(m.EmissionColor[1]),
		clamp01(m.EmissionColor[2]),
	}
}

func (m *AnalyticMediumComponent) NormalizedNoiseScale() float32 {
	if m == nil || m.NoiseScale <= 0 {
		return 0
	}
	return m.NoiseScale
}

func (m *AnalyticMediumComponent) NormalizedNoiseStrength() float32 {
	if m == nil || m.NoiseStrength <= 0 {
		return 0
	}
	if m.NoiseStrength > 1 {
		return 1
	}
	return m.NoiseStrength
}

func (m *AnalyticMediumComponent) NormalizedSampleCount() int {
	if m == nil || m.SampleCount <= 0 {
		return 12
	}
	if m.SampleCount < 4 {
		return 4
	}
	if m.SampleCount > 48 {
		return 48
	}
	return m.SampleCount
}

func (m *AnalyticMediumComponent) WorldCenter(tr *TransformComponent) mgl32.Vec3 {
	if tr == nil {
		return mgl32.Vec3{}
	}
	return tr.Position
}

func (m *AnalyticMediumComponent) WorldRotation(tr *TransformComponent) mgl32.Quat {
	if tr == nil {
		return mgl32.QuatIdent()
	}
	if tr.Rotation.Len() <= 1e-6 {
		return mgl32.QuatIdent()
	}
	return tr.Rotation.Normalize()
}
