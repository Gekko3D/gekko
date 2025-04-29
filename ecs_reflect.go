package gekko

import (
	"reflect"
)

func reflectSliceMake(elem reflect.Type) any {
	return reflect.MakeSlice(reflect.SliceOf(elem), 0, 1).Interface()
}

func reflectSliceGet(slice any, idx int) reflect.Value {
	return reflect.ValueOf(slice).Index(idx)
}

func reflectSliceSet(slice any, idx int, val reflect.Value) {
	reflect.ValueOf(slice).Index(idx).Set(val)
}

func reflectSliceAppend(slice any, val reflect.Value) any {
	return reflect.Append(
		reflect.ValueOf(slice),
		val,
	).Interface()
}

func reflectSliceLen(slice any) int {
	return reflect.ValueOf(slice).Len()
}
