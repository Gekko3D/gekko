package ecs

import "reflect"

func ReflectSliceMake(elem reflect.Type) any {
	return reflect.MakeSlice(reflect.SliceOf(elem), 0, 1).Interface()
}

func ReflectSliceGet(slice any, idx int) reflect.Value {
	return reflect.ValueOf(slice).Index(idx)
}

func ReflectSliceSet(slice any, idx int, val reflect.Value) {
	reflect.ValueOf(slice).Index(idx).Set(val)
}

func ReflectSliceAppend(slice any, val reflect.Value) any {
	return reflect.Append(
		reflect.ValueOf(slice),
		val,
	).Interface()
}

func ReflectSliceLen(slice any) int {
	return reflect.ValueOf(slice).Len()
}
