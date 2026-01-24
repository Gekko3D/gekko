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

const (
	SkyboxBlendAlpha SkyboxBlendMode = iota
	SkyboxBlendAdd
	SkyboxBlendMultiply
)

type SkyboxLayerComponent struct {
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
