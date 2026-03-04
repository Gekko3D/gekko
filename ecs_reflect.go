package gekko

import (
	"reflect"

	rooteecs "github.com/gekko3d/gekko/ecs"
)

type AnySlice = rooteecs.AnySlice

func MakeAnySlice(slice any) AnySlice {
	return rooteecs.MakeAnySlice(slice)
}

func reflectSliceMake(elem reflect.Type) any {
	return rooteecs.ReflectSliceMake(elem)
}

func reflectSliceGet(slice any, idx int) reflect.Value {
	return rooteecs.ReflectSliceGet(slice, idx)
}

func reflectSliceSet(slice any, idx int, val reflect.Value) {
	rooteecs.ReflectSliceSet(slice, idx, val)
}

func reflectSliceAppend(slice any, val reflect.Value) any {
	return rooteecs.ReflectSliceAppend(slice, val)
}

func reflectSliceLen(slice any) int {
	return rooteecs.ReflectSliceLen(slice)
}
