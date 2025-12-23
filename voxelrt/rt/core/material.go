package core

type Material struct {
	BaseColor    [4]uint8 // RGBA
	Emissive     [4]uint8 // RGBA
	Roughness    float32
	Metalness    float32
	IOR          float32
	Transparency float32
}

func NewMaterial(baseColor [4]uint8, emissive [4]uint8) Material {
	return Material{
		BaseColor:    baseColor,
		Emissive:     emissive,
		Roughness:    1.0,
		Metalness:    0.0,
		IOR:          1.0,
		Transparency: 0.0,
	}
}

// Helper for default white
func DefaultMaterial() Material {
	return Material{
		BaseColor:    [4]uint8{255, 255, 255, 255},
		Emissive:     [4]uint8{0, 0, 0, 0},
		Roughness:    1.0,
		Metalness:    0.0,
		IOR:          1.0,
		Transparency: 0.0,
	}
}
