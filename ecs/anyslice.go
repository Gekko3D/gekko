package ecs

import (
	"reflect"
	"unsafe"
)

type AnySlice struct {
	typ reflect.Type
	val reflect.Value
}

func MakeAnySlice(slice any) AnySlice {
	val := reflect.ValueOf(slice)
	if val.Kind() != reflect.Slice {
		panic("Not a slice")
	}

	return AnySlice{
		typ: reflect.TypeOf(slice),
		val: val,
	}
}

func (slice AnySlice) Len() int {
	return slice.val.Len()
}

func (slice AnySlice) Get(idx int) reflect.Value {
	return slice.val.Index(idx)
}

func (slice AnySlice) ElementSize() int {
	return int(slice.typ.Elem().Size())
}

func (slice AnySlice) DataPointer() unsafe.Pointer {
	return unsafe.Pointer(slice.Get(0).UnsafeAddr())
}
