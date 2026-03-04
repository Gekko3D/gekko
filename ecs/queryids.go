package ecs

import "reflect"

func IdentifyOptionals(getID func(reflect.Type) uint32, components ...any) map[uint32]struct{} {
	res := make(map[uint32]struct{})
	for _, c := range components {
		cType := reflect.TypeOf(c)
		if cType.Kind() == reflect.Pointer {
			cType = cType.Elem()
		}
		res[getID(cType)] = struct{}{}
	}
	return res
}

func IdentifyComponents(getID func(reflect.Type) uint32, types ...reflect.Type) []uint32 {
	res := make([]uint32, len(types))
	for i, t := range types {
		res[i] = getID(t)
	}
	return res
}
