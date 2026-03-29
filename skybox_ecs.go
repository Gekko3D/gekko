package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

type SkyboxNoiseType uint32

const (
	SkyboxNoisePerlin SkyboxNoiseType = iota
	SkyboxNoiseSimplex
)

type SkyboxBlendMode uint32
type SkyboxLayerType uint32

const (
	SkyboxLayerNoise SkyboxLayerType = iota
	SkyboxLayerStars
	SkyboxLayerNebula
	SkyboxLayerGradient
)

const (
	SkyboxBlendAlpha SkyboxBlendMode = iota
	SkyboxBlendAdd
	SkyboxBlendMultiply
)

type SkyboxLayerComponent struct {
	// Layer type
	LayerType SkyboxLayerType

	// Noise type
	NoiseType SkyboxNoiseType

	// Noise parameters
	Seed        int64
	Scale       float32
	Octaves     int
	Persistence float32
	Lacunarity  float32
	Smooth      bool // Use linear filtering if true, nearest if false
	Resolution  [2]int

	// Visual parameters
	ColorA    mgl32.Vec3
	ColorB    mgl32.Vec3
	Threshold float32
	Invert    bool

	// Blending parameters
	BlendMode SkyboxBlendMode
	Opacity   float32
	Priority  int

	// Animation parameters
	WindSpeed mgl32.Vec3 // Offset per second
	Offset    mgl32.Vec3

	// Flags for internal use
	_dirty bool
}

func (s *SkyboxLayerComponent) SetDirty() {
	s._dirty = true
}

type SkyboxSunComponent struct {
	Direction              mgl32.Vec3
	Intensity              float32
	HaloColor              mgl32.Vec3
	CoreGlowStrength       float32
	CoreGlowExponent       float32
	AtmosphereExponent     float32
	AtmosphereGlowStrength float32
	DiskColor              mgl32.Vec3
	DiskStrength           float32
	DiskStart              float32
	DiskEnd                float32
}

type SkyAmbientComponent struct {
	SkyMix float32
}
