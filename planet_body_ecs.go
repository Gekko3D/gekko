package gekko

import "github.com/go-gl/mathgl/mgl32"

const (
	planetBodyBakedSurfaceFaceCount     = 6
	PlanetBodyMaxBakedSurfaceResolution = 1024
)

type PlanetBakedSurfaceSample struct {
	Height       float32
	NormalOctX   float32
	NormalOctY   float32
	MaterialBand float32
}

// PlanetBodyComponent describes an analytic far-body planet rendered by the
// voxel RT renderer. Values are authored in local world units before transform
// scale is applied.
type PlanetBodyComponent struct {
	Disabled bool

	Radius                 float32
	OceanRadius            float32
	AtmosphereRadius       float32
	AtmosphereRimWidth     float32
	HeightAmplitude        float32
	NoiseScale             float32
	BlockSize              float32
	HeightSteps            int
	HandoffNearAlt         float32
	HandoffFarAlt          float32
	Seed                   uint32
	BiomeMix               float32
	BakedSurfaceResolution int
	BakedSurfaceSamples    []PlanetBakedSurfaceSample
	BandColors             [6][3]float32

	AmbientStrength  float32
	DiffuseStrength  float32
	SpecularStrength float32
	RimStrength      float32
	EmissionStrength float32

	TerrainLowColor     [3]float32
	TerrainHighColor    [3]float32
	RockColor           [3]float32
	OceanDeepColor      [3]float32
	OceanShallowColor   [3]float32
	AtmosphereTintColor [3]float32
}

func (p *PlanetBodyComponent) Enabled() bool {
	return p != nil && !p.Disabled && p.NormalizedRadius() > 0
}

func (p *PlanetBodyComponent) NormalizedRadius() float32 {
	if p == nil || p.Radius <= 0 {
		return 0
	}
	return p.Radius
}

func (p *PlanetBodyComponent) NormalizedOceanRadius() float32 {
	if p == nil || p.OceanRadius <= 0 {
		return 0
	}
	ocean := p.OceanRadius
	if ocean < p.NormalizedRadius() {
		ocean = p.NormalizedRadius()
	}
	maxOcean := p.NormalizedRadius() + p.NormalizedHeightAmplitude()
	if maxOcean > 0 && ocean > maxOcean {
		ocean = maxOcean
	}
	return ocean
}

func (p *PlanetBodyComponent) NormalizedAtmosphereRadius() float32 {
	if p == nil || p.AtmosphereRadius <= 0 {
		return 0
	}
	atmosphere := p.AtmosphereRadius
	minAtmosphere := max(p.NormalizedOceanRadius(), p.NormalizedRadius()+p.NormalizedHeightAmplitude())
	if atmosphere < minAtmosphere {
		atmosphere = minAtmosphere
	}
	return atmosphere
}

func (p *PlanetBodyComponent) NormalizedAtmosphereRimWidth() float32 {
	if p == nil || p.AtmosphereRimWidth <= 0 {
		return 0
	}
	maxWidth := p.NormalizedHeightAmplitude() * 1.5
	if maxWidth <= 0 {
		maxWidth = p.NormalizedRadius() * 0.12
	}
	if maxWidth <= 0 {
		return p.AtmosphereRimWidth
	}
	if p.AtmosphereRimWidth > maxWidth {
		return maxWidth
	}
	return p.AtmosphereRimWidth
}

func (p *PlanetBodyComponent) NormalizedHeightAmplitude() float32 {
	if p == nil || p.HeightAmplitude <= 0 {
		return 0
	}
	maxHeight := p.NormalizedRadius() * 0.6
	if maxHeight <= 0 {
		return 0
	}
	if p.HeightAmplitude > maxHeight {
		return maxHeight
	}
	return p.HeightAmplitude
}

func (p *PlanetBodyComponent) NormalizedNoiseScale() float32 {
	if p == nil || p.NoiseScale <= 0 {
		return 2.2
	}
	return p.NoiseScale
}

func (p *PlanetBodyComponent) NormalizedBlockSize() float32 {
	if p == nil {
		return 1
	}
	if p.BlockSize > 0 {
		return p.BlockSize
	}
	derived := p.NormalizedHeightAmplitude() * 0.32
	if derived < 1 {
		derived = 1
	}
	return derived
}

func (p *PlanetBodyComponent) NormalizedHeightSteps() int {
	if p == nil || p.HeightSteps <= 1 {
		return 6
	}
	if p.HeightSteps > 64 {
		return 64
	}
	return p.HeightSteps
}

func (p *PlanetBodyComponent) NormalizedHandoffNearAlt() float32 {
	if p == nil {
		return 0
	}
	if p.HandoffNearAlt > 0 {
		return p.HandoffNearAlt
	}
	return max(p.NormalizedHeightAmplitude()*0.7, p.NormalizedRadius()*0.06)
}

func (p *PlanetBodyComponent) NormalizedHandoffFarAlt() float32 {
	near := p.NormalizedHandoffNearAlt()
	if p == nil {
		return near + 1
	}
	if p.HandoffFarAlt > near {
		return p.HandoffFarAlt
	}
	return near + max(p.NormalizedHeightAmplitude()*2.2, p.NormalizedRadius()*0.025)
}

func (p *PlanetBodyComponent) NormalizedBiomeMix() float32 {
	if p == nil {
		return 0.5
	}
	return clamp01(p.BiomeMix)
}

func (p *PlanetBodyComponent) NormalizedBakedSurfaceResolution() int {
	if p == nil || p.BakedSurfaceResolution < 2 {
		return 0
	}
	if p.BakedSurfaceResolution > PlanetBodyMaxBakedSurfaceResolution {
		return PlanetBodyMaxBakedSurfaceResolution
	}
	return p.BakedSurfaceResolution
}

func (p *PlanetBodyComponent) NormalizedBakedSurfaceSampleCount() int {
	resolution := p.NormalizedBakedSurfaceResolution()
	if resolution == 0 {
		return 0
	}
	return resolution * resolution * planetBodyBakedSurfaceFaceCount
}

func (p *PlanetBodyComponent) NormalizedBandColors() [6][3]float32 {
	if p == nil {
		return defaultPlanetBandColors()
	}
	colors := p.BandColors
	hasExplicit := false
	for i := range colors {
		if colors[i] != ([3]float32{}) {
			hasExplicit = true
			break
		}
	}
	if !hasExplicit {
		colors = [6][3]float32{
			p.NormalizedOceanDeepColor(),
			p.NormalizedOceanShallowColor(),
			p.NormalizedTerrainLowColor(),
			blendPlanetColor(p.NormalizedTerrainLowColor(), p.NormalizedTerrainHighColor(), 0.5),
			p.NormalizedTerrainHighColor(),
			p.NormalizedRockColor(),
		}
	}
	for i := range colors {
		for j := range colors[i] {
			if colors[i][j] < 0 {
				colors[i][j] = 0
			}
			if colors[i][j] > 1 {
				colors[i][j] = 1
			}
		}
	}
	return colors
}

func (p *PlanetBodyComponent) NormalizedAmbientStrength() float32 {
	if p == nil || p.AmbientStrength <= 0 {
		return 0.22
	}
	return clamp01(p.AmbientStrength)
}

func (p *PlanetBodyComponent) NormalizedDiffuseStrength() float32 {
	if p == nil || p.DiffuseStrength <= 0 {
		return 1.0
	}
	if p.DiffuseStrength > 4 {
		return 4
	}
	return p.DiffuseStrength
}

func (p *PlanetBodyComponent) NormalizedSpecularStrength() float32 {
	if p == nil || p.SpecularStrength < 0 {
		return 0.08
	}
	if p.SpecularStrength > 4 {
		return 4
	}
	return p.SpecularStrength
}

func (p *PlanetBodyComponent) NormalizedRimStrength() float32 {
	if p == nil || p.RimStrength < 0 {
		return 0.35
	}
	if p.RimStrength > 4 {
		return 4
	}
	return p.RimStrength
}

func (p *PlanetBodyComponent) NormalizedEmissionStrength() float32 {
	if p == nil || p.EmissionStrength <= 0 {
		return 0
	}
	if p.EmissionStrength > 4 {
		return 4
	}
	return p.EmissionStrength
}

func (p *PlanetBodyComponent) NormalizedTerrainLowColor() [3]float32 {
	return normalizePlanetColor(p, p.TerrainLowColor, [3]float32{0.26, 0.43, 0.24}, 1)
}

func (p *PlanetBodyComponent) NormalizedTerrainHighColor() [3]float32 {
	return normalizePlanetColor(p, p.TerrainHighColor, [3]float32{0.68, 0.61, 0.53}, 1)
}

func (p *PlanetBodyComponent) NormalizedRockColor() [3]float32 {
	return normalizePlanetColor(p, p.RockColor, [3]float32{0.78, 0.78, 0.78}, 1)
}

func (p *PlanetBodyComponent) NormalizedOceanDeepColor() [3]float32 {
	return normalizePlanetColor(p, p.OceanDeepColor, [3]float32{0.06, 0.19, 0.34}, 1)
}

func (p *PlanetBodyComponent) NormalizedOceanShallowColor() [3]float32 {
	return normalizePlanetColor(p, p.OceanShallowColor, [3]float32{0.21, 0.49, 0.67}, 1)
}

func (p *PlanetBodyComponent) NormalizedAtmosphereTintColor() [3]float32 {
	return normalizePlanetColor(p, p.AtmosphereTintColor, [3]float32{0.32, 0.58, 0.98}, 1)
}

func (p *PlanetBodyComponent) WorldCenter(tr *TransformComponent) mgl32.Vec3 {
	if tr == nil {
		return mgl32.Vec3{}
	}
	return tr.Position
}

func (p *PlanetBodyComponent) WorldRotation(tr *TransformComponent) mgl32.Quat {
	if tr == nil {
		return mgl32.QuatIdent()
	}
	return tr.Rotation
}

func (p *PlanetBodyComponent) WorldRadius(tr *TransformComponent) float32 {
	return p.NormalizedRadius() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldOceanRadius(tr *TransformComponent) float32 {
	return p.NormalizedOceanRadius() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldAtmosphereRadius(tr *TransformComponent) float32 {
	return p.NormalizedAtmosphereRadius() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldAtmosphereRimWidth(tr *TransformComponent) float32 {
	return p.NormalizedAtmosphereRimWidth() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldHeightAmplitude(tr *TransformComponent) float32 {
	return p.NormalizedHeightAmplitude() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldBlockSize(tr *TransformComponent) float32 {
	return p.NormalizedBlockSize() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldHandoffNearAlt(tr *TransformComponent) float32 {
	return p.NormalizedHandoffNearAlt() * planetUniformScale(tr)
}

func (p *PlanetBodyComponent) WorldHandoffFarAlt(tr *TransformComponent) float32 {
	return p.NormalizedHandoffFarAlt() * planetUniformScale(tr)
}

func planetUniformScale(tr *TransformComponent) float32 {
	if tr == nil {
		return 1
	}
	sx := absPlanetFloat(tr.Scale.X())
	sy := absPlanetFloat(tr.Scale.Y())
	sz := absPlanetFloat(tr.Scale.Z())
	return (sx + sy + sz) / 3.0
}

func absPlanetFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	if v == 0 {
		return 1
	}
	return v
}

func normalizePlanetColor(_ *PlanetBodyComponent, color [3]float32, fallback [3]float32, maxV float32) [3]float32 {
	if color == ([3]float32{}) {
		color = fallback
	}
	for i := range color {
		if color[i] < 0 {
			color[i] = 0
		}
		if color[i] > maxV {
			color[i] = maxV
		}
	}
	return color
}

func defaultPlanetBandColors() [6][3]float32 {
	return [6][3]float32{
		{0.06, 0.19, 0.34},
		{0.21, 0.49, 0.67},
		{0.26, 0.43, 0.24},
		{0.47, 0.46, 0.28},
		{0.68, 0.61, 0.53},
		{0.92, 0.94, 0.96},
	}
}

func blendPlanetColor(a, b [3]float32, t float32) [3]float32 {
	return [3]float32{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}
