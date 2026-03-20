package core

type Material struct {
	BaseColor    [4]uint8 // RGBA
	Emissive     [4]uint8 // RGBA
	Emission     float32
	Roughness    float32
	Metalness    float32
	IOR          float32
	Transparency float32
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
		Roughness:    1.0,
		Metalness:    0.0,
		IOR:          1.5,
		Transparency: 0.0,
	}
}
