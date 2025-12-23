package gekko

type TextComponent struct {
	Text     string
	Position [2]float32 // Pixels, top-left
	Scale    float32
	Color    [4]float32
}
