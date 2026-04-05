package gekko

type CelestialBodyComponent struct {
	Radius               float32
	AtmosphereRadius     float32
	CloudRadius          float32
	DisableSurface       bool
	SurfaceColor         [3]float32
	AtmosphereColor      [3]float32
	CloudColor           [3]float32
	CloudCoverage        float32
	AtmosphereDensity    float32
	AtmosphereFalloff    float32
	AtmosphereGlow       float32
	CloudOpacity         float32
	CloudSharpness       float32
	CloudDriftSpeed      float32
	CloudBanding         float32
	SurfaceBiomeMix      float32
	CloudTintWarmth      float32
	NightSideFill        float32
	TerminatorSoftness   float32
	SurfaceOcclusionBias float32
	Emission             float32
	SurfaceSeed          float32
	SurfaceNoiseScale    float32
	CloudSeed            float32
	CloudNoiseScale      float32
}
