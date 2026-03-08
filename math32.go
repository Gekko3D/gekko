package gekko

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
