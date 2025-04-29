package gekko

import (
	"reflect"
	"testing"
)

func TestEcsReflect_ReflectSliceMake(t *testing.T) {
	intSlice := reflectSliceMake(reflect.TypeOf(0))
	if reflect.TypeOf(intSlice).Kind() != reflect.Slice {
		t.Errorf("Expected a slice, got %v", reflect.TypeOf(intSlice).Kind())
	}

	if reflect.TypeOf(intSlice).Elem().Kind() != reflect.Int {
		t.Errorf("Expected slice of int, got %v", reflect.TypeOf(intSlice).Elem().Kind())
	}

	type myStruct struct{ A int }
	structSlice := reflectSliceMake(reflect.TypeOf(myStruct{}))
	if reflect.TypeOf(structSlice).Elem() != reflect.TypeOf(myStruct{}) {
		t.Errorf("Expected slice of myStruct, got %v", reflect.TypeOf(structSlice).Elem())
	}
}

func TestEcsReflect_ReflectSliceGet(t *testing.T) {
	slice := []int{10, 20, 30}
	val := reflectSliceGet(slice, 1)
	if val.Int() != 20 {
		t.Errorf("Expected 20, got %d", val.Int())
	}
}

func TestEcsReflect_ReflectSliceGet_PanicOnInvalidIndex(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic on invalid index")
		}
	}()
	slice := []int{1, 2}
	_ = reflectSliceGet(slice, 10) // Out of bounds
}

func TestEcsReflect_ReflectSliceSet(t *testing.T) {
	slice := []int{1, 2}
	val := reflect.ValueOf(99)
	reflectSliceSet(slice, 0, val)

	if slice[0] != 99 {
		t.Errorf("Expected 99 at index 0, got %d", slice[0])
	}
}

func TestEcsReflect_ReflectSliceSet_PanicOnTypeMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic on type mismatch")
		}
	}()
	slice := []int{1, 2}
	wrongVal := reflect.ValueOf("wrong type")
	reflectSliceSet(slice, 0, wrongVal)
}

func TestEcsReflect_ReflectSliceAppend(t *testing.T) {
	slice := []int{}
	val := reflect.ValueOf(42)
	newSlice := reflectSliceAppend(slice, val).([]int)

	if len(newSlice) != 1 || newSlice[0] != 42 {
		t.Errorf("Expected [42], got %v", newSlice)
	}
}

func TestEcsReflect_ReflectSliceAppend_Multiple(t *testing.T) {
	slice := []int{}
	for i := 0; i < 5; i++ {
		val := reflect.ValueOf(i)
		slice = reflectSliceAppend(slice, val).([]int)
	}
	if len(slice) != 5 {
		t.Errorf("Expected slice length 5, got %d", len(slice))
	}
	for i, v := range slice {
		if v != i {
			t.Errorf("Expected value %d at index %d, got %d", i, i, v)
		}
	}
}

func TestEcsReflect_ReflectSliceAppend_PanicOnWrongType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic on type mismatch append")
		}
	}()
	slice := []int{}
	val := reflect.ValueOf("string") // wrong type
	_ = reflectSliceAppend(slice, val)
}

func TestEcsReflect_ReflectSliceLen(t *testing.T) {
	slice := []int{1, 2, 3}
	if l := reflectSliceLen(slice); l != 3 {
		t.Errorf("Expected length 3, got %d", l)
	}

	emptySlice := []string{}
	if l := reflectSliceLen(emptySlice); l != 0 {
		t.Errorf("Expected length 0, got %d", l)
	}
}

func TestEcsReflect_ReflectSliceLen_PanicOnNonSlice(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic when input is not a slice")
		}
	}()
	_ = reflectSliceLen(123) // not a slice
}
