package gekko

func getMapValues[K comparable, V any](kvs map[K]V) []V {
	var vals []V
	for _, v := range kvs {
		vals = append(vals, v)
	}
	return vals
}
