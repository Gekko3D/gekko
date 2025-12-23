package core

// Light is the GPU representation of a light
type Light struct {
	Position  [4]float32 // xyz, pad/type? We need type. Let's pack type in w? Or use separate field.
	Direction [4]float32 // xyz, pad
	Color     [4]float32 // rgb, intensity
	Params    [4]float32 // range, cone_angle_cos, type, padding
}
