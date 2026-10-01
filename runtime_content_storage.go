package gekko

import (
	"math"
	"reflect"
	"sync"
)

// Storage estimates count inline object sizes plus referenced decoded storage.
// Identical backing views deduplicate; different overlapping views remain
// conservative. Map bucket slack, allocator and cache overhead are excluded.
type runtimeContentStorageIdentity struct {
	kind reflect.Kind
	typ  reflect.Type
	ptr  uintptr
	size int
}
type runtimeContentStorageEstimate struct {
	seen map[runtimeContentStorageIdentity]struct{}
}

var runtimeContentReferenceTypes sync.Map

func runtimeContentTypeHasReferences(t reflect.Type) bool {
	if cached, ok := runtimeContentReferenceTypes.Load(t); ok {
		return cached.(bool)
	}
	has := false
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.String, reflect.Interface:
		has = true
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if runtimeContentTypeHasReferences(t.Field(i).Type) {
				has = true
				break
			}
		}
	case reflect.Array:
		has = runtimeContentTypeHasReferences(t.Elem())
	}
	runtimeContentReferenceTypes.Store(t, has)
	return has
}
func runtimeContentGraphCharge(roots ...any) int64 {
	estimate := runtimeContentStorageEstimate{seen: make(map[runtimeContentStorageIdentity]struct{})}
	var bytes int64
	for _, root := range roots {
		v := reflect.ValueOf(root)
		if v.IsValid() {
			bytes = runtimeContentChargeSum(bytes, int64(v.Type().Size()), estimate.references(v))
		}
	}
	return bytes
}
func (s *runtimeContentStorageEstimate) first(kind reflect.Kind, t reflect.Type, ptr uintptr, size int) bool {
	id := runtimeContentStorageIdentity{kind, t, ptr, size}
	if _, ok := s.seen[id]; ok {
		return false
	}
	s.seen[id] = struct{}{}
	return true
}
func (s *runtimeContentStorageEstimate) references(v reflect.Value) int64 {
	if !v.IsValid() || !runtimeContentTypeHasReferences(v.Type()) {
		return 0
	}
	var bytes int64
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || !s.first(v.Kind(), v.Type(), v.Pointer(), 0) {
			return 0
		}
		bytes = runtimeContentChargeSum(int64(v.Type().Elem().Size()), s.references(v.Elem()))
	case reflect.Interface:
		if !v.IsNil() {
			bytes = runtimeContentChargeSum(int64(v.Elem().Type().Size()), s.references(v.Elem()))
		}
	case reflect.String:
		if v.Len() > 0 && s.first(v.Kind(), v.Type(), v.Pointer(), v.Len()) {
			bytes = int64(v.Len())
		}
	case reflect.Slice:
		if v.Cap() == 0 || !s.first(v.Kind(), v.Type(), v.Pointer(), v.Cap()) {
			return 0
		}
		bytes = runtimeContentChargeProduct(int64(v.Cap()), int64(v.Type().Elem().Size()))
		if runtimeContentTypeHasReferences(v.Type().Elem()) {
			full := v.Slice(0, v.Cap())
			for i := 0; i < full.Len(); i++ {
				bytes = runtimeContentChargeSum(bytes, s.references(full.Index(i)))
			}
		}
	case reflect.Map:
		if v.IsNil() || !s.first(v.Kind(), v.Type(), v.Pointer(), 0) {
			return 0
		}
		bytes = runtimeContentChargeProduct(int64(v.Len()), runtimeContentChargeSum(int64(v.Type().Key().Size()), int64(v.Type().Elem().Size())))
		if runtimeContentTypeHasReferences(v.Type().Key()) || runtimeContentTypeHasReferences(v.Type().Elem()) {
			it := v.MapRange()
			for it.Next() {
				bytes = runtimeContentChargeSum(bytes, s.references(it.Key()), s.references(it.Value()))
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			bytes = runtimeContentChargeSum(bytes, s.references(v.Field(i)))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			bytes = runtimeContentChargeSum(bytes, s.references(v.Index(i)))
		}
	}
	return bytes
}

// Saturate conservative estimates instead of letting an overflow become a
// negative admission cost. Production allocations are much smaller.
func runtimeContentChargeSum(costs ...int64) int64 {
	var total int64
	for _, cost := range costs {
		if cost < 0 || cost > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += cost
	}
	return total
}
func runtimeContentChargeProduct(count, size int64) int64 {
	if count < 0 || size < 0 || (size > 0 && count > math.MaxInt64/size) {
		return math.MaxInt64
	}
	return count * size
}
