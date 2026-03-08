package gpu

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
