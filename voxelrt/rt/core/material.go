package core

func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type Material struct {
	BaseColor    [4]uint8 // RGBA
	Emissive     [4]uint8 // RGBA
	Emission     float32
	Transmission float32
	Density      float32
	Refraction   float32
	Roughness    float32
	Metalness    float32
	IOR          float32
	Transparency float32
}

func (m Material) HasTransparency() bool {
	return m.Transparency > 0.001 || m.Transmission > 0.001
}

// ApplyGameplaySeeThrough forces a non-refractive transparency mode suitable
// for gameplay readability, such as seeing a character through nearby cover.
func (m *Material) ApplyGameplaySeeThrough(transparency float32) {
	if m == nil {
		return
	}
	m.Transparency = clampUnit(transparency)
	m.Transmission = 0.0
	m.Density = 0.0
	m.Refraction = 0.0
}

func NewMaterial(baseColor [4]uint8, emissive [4]uint8) Material {
	emission := float32(0.0)
	if emissive[0] > 0 || emissive[1] > 0 || emissive[2] > 0 {
		emission = 1.0
	}
	return Material{
		BaseColor:    baseColor,
		Emissive:     emissive,
		Emission:     emission,
		Transmission: 0.0,
		Density:      0.0,
		Refraction:   0.0,
		Roughness:    1.0,
		Metalness:    0.0,
		IOR:          1.5,
		Transparency: 0.0,
	}
}

// Helper for default white
func DefaultMaterial() Material {
	return Material{
		BaseColor:    [4]uint8{255, 255, 255, 255},
		Emissive:     [4]uint8{0, 0, 0, 0},
		Emission:     0.0,
		Transmission: 0.0,
		Density:      0.0,
		Refraction:   0.0,
		Roughness:    1.0,
		Metalness:    0.0,
		IOR:          1.5,
		Transparency: 0.0,
	}
}

// NewGameplaySeeThroughMaterial returns a transparent but non-refractive
// material preset for gameplay convenience rendering.
func NewGameplaySeeThroughMaterial(baseColor [4]uint8, transparency float32) Material {
	mat := NewMaterial(baseColor, [4]uint8{0, 0, 0, 0})
	mat.ApplyGameplaySeeThrough(transparency)
	return mat
}
