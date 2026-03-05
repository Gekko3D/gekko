package gekko

import (
	"reflect"

	rooteecs "github.com/gekko3d/gekko/ecs"
)

// TO get more queries:
//  1. Uncomment QueryN
//  2. Uncomment identifyComponentsN
//  3. Copy MapN-1() and implement according to other Map() functions
//  4. Implement Command's MakeQuery to make it available in the user code
type Query1[A any] struct {
	ecs      *Ecs
	excludes []any
}

func (q Query1[A]) Without(excludes ...any) Query1[A] {
	q.excludes = append(q.excludes, excludes...)
	return q
}

type Query2[A, B any] struct {
	ecs      *Ecs
	excludes []any
}

func (q Query2[A, B]) Without(excludes ...any) Query2[A, B] {
	q.excludes = append(q.excludes, excludes...)
	return q
}

type Query3[A, B, C any] struct {
	ecs      *Ecs
	excludes []any
}

func (q Query3[A, B, C]) Without(excludes ...any) Query3[A, B, C] {
	q.excludes = append(q.excludes, excludes...)
	return q
}

type Query4[A, B, C, D any] struct {
	ecs      *Ecs
	excludes []any
}

func (q Query4[A, B, C, D]) Without(excludes ...any) Query4[A, B, C, D] {
	q.excludes = append(q.excludes, excludes...)
	return q
}

type Query5[A, B, C, D, E any] struct {
	ecs      *Ecs
	excludes []any
}

func (q Query5[A, B, C, D, E]) Without(excludes ...any) Query5[A, B, C, D, E] {
	q.excludes = append(q.excludes, excludes...)
	return q
}

func Type[T any]() any {
	var t T
	return t
}

/*type Query6[A, B, C, D, E, F any] struct{ ecs *Ecs }
type Query7[A, B, C, D, E, F, G any] struct{ ecs *Ecs }
type Query8[A, B, C, D, E, F, G, H any] struct{ ecs *Ecs }
*/

func MakeQuery1[A any](cmd *Commands) Query1[A]             { return Query1[A]{ecs: cmd.app.ecs} }
func MakeQuery2[A, B any](cmd *Commands) Query2[A, B]       { return Query2[A, B]{ecs: cmd.app.ecs} }
func MakeQuery3[A, B, C any](cmd *Commands) Query3[A, B, C] { return Query3[A, B, C]{ecs: cmd.app.ecs} }
func MakeQuery4[A, B, C, D any](cmd *Commands) Query4[A, B, C, D] {
	return Query4[A, B, C, D]{ecs: cmd.app.ecs}
}
func MakeQuery5[A, B, C, D, E any](cmd *Commands) Query5[A, B, C, D, E] {
	return Query5[A, B, C, D, E]{ecs: cmd.app.ecs}
}

type rootArchetypeView struct{ arch *archetype }

func (v rootArchetypeView) GetComponent(id uint32) (any, bool) {
	data, ok := v.arch.componentData[componentId(id)]
	return data, ok
}

func (v rootArchetypeView) EachEntity(fn func(rooteecs.EntityID, int) bool) {
	for entityID, r := range v.arch.entities {
		if !fn(rooteecs.EntityID(entityID), int(r)) {
			return
		}
	}
}

func toOptionalIDs(opt set[componentId]) map[uint32]struct{} {
	res := make(map[uint32]struct{}, len(opt))
	for id := range opt {
		res[uint32(id)] = struct{}{}
	}
	return res
}

func filterViewsForExcludes(views []rooteecs.ArchetypeView, ecs *Ecs, excludes []any) []rooteecs.ArchetypeView {
	if len(excludes) == 0 {
		return views
	}
	excIDs := identifyOptionals(ecs, excludes...)
	filtered := make([]rooteecs.ArchetypeView, 0, len(views))
	for _, v := range views {
		archView := v.(rootArchetypeView)
		excluded := false
		for excID := range excIDs {
			if _, ok := archView.arch.componentData[excID]; ok {
				excluded = true
				break
			}
		}
		if !excluded {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func (q Query1[A]) Map(m func(EntityId, *A) bool, optionals ...any) {
	id1 := identifyComponents1[A](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)
	views := filterViewsForExcludes(q.ecs.archetypeViews(), q.ecs, q.excludes)
	rooteecs.Map1(views, uint32(id1), toOptionalIDs(opt), func(id rooteecs.EntityID, a *A) bool {
		return m(EntityId(id), a)
	})
}

func (q Query2[A, B]) Map(m func(EntityId, *A, *B) bool, optionals ...any) {
	id1, id2 := identifyComponents2[A, B](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)
	views := filterViewsForExcludes(q.ecs.archetypeViews(), q.ecs, q.excludes)
	rooteecs.Map2(views, uint32(id1), uint32(id2), toOptionalIDs(opt), func(id rooteecs.EntityID, a *A, b *B) bool {
		return m(EntityId(id), a, b)
	})
}

func (q Query3[A, B, C]) Map(m func(EntityId, *A, *B, *C) bool, optionals ...any) {
	id1, id2, id3 := identifyComponents3[A, B, C](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)
	views := filterViewsForExcludes(q.ecs.archetypeViews(), q.ecs, q.excludes)
	rooteecs.Map3(views, uint32(id1), uint32(id2), uint32(id3), toOptionalIDs(opt), func(id rooteecs.EntityID, a *A, b *B, c *C) bool {
		return m(EntityId(id), a, b, c)
	})
}

func (q Query4[A, B, C, D]) Map(m func(EntityId, *A, *B, *C, *D) bool, optionals ...any) {
	id1, id2, id3, id4 := identifyComponents4[A, B, C, D](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)
	views := filterViewsForExcludes(q.ecs.archetypeViews(), q.ecs, q.excludes)
	rooteecs.Map4(views, uint32(id1), uint32(id2), uint32(id3), uint32(id4), toOptionalIDs(opt), func(id rooteecs.EntityID, a *A, b *B, c *C, d *D) bool {
		return m(EntityId(id), a, b, c, d)
	})
}

func (q Query5[A, B, C, D, E]) Map(m func(EntityId, *A, *B, *C, *D, *E) bool, optionals ...any) {
	id1, id2, id3, id4, id5 := identifyComponents5[A, B, C, D, E](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)
	views := filterViewsForExcludes(q.ecs.archetypeViews(), q.ecs, q.excludes)
	rooteecs.Map5(views, uint32(id1), uint32(id2), uint32(id3), uint32(id4), uint32(id5), toOptionalIDs(opt), func(id rooteecs.EntityID, a *A, b *B, c *C, d *D, e *E) bool {
		return m(EntityId(id), a, b, c, d, e)
	})
}

func identifyOptionals(ecs *Ecs, components ...any) set[componentId] {
	raw := rooteecs.IdentifyOptionals(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, components...)
	res := make(set[componentId], len(raw))
	for id := range raw {
		res[componentId(id)] = struct{}{}
	}
	return res
}

func identifyComponents1[A any](ecs *Ecs) componentId {
	var a A
	ids := rooteecs.IdentifyComponents(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, reflect.TypeOf(a))
	return componentId(ids[0])
}

func identifyComponents2[A, B any](ecs *Ecs) (componentId, componentId) {
	var a A
	var b B
	ids := rooteecs.IdentifyComponents(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, reflect.TypeOf(a), reflect.TypeOf(b))
	return componentId(ids[0]), componentId(ids[1])
}

func identifyComponents3[A, B, C any](ecs *Ecs) (componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	ids := rooteecs.IdentifyComponents(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, reflect.TypeOf(a), reflect.TypeOf(b), reflect.TypeOf(c))
	return componentId(ids[0]), componentId(ids[1]), componentId(ids[2])
}

func identifyComponents4[A, B, C, D any](ecs *Ecs) (componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	ids := rooteecs.IdentifyComponents(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, reflect.TypeOf(a), reflect.TypeOf(b), reflect.TypeOf(c), reflect.TypeOf(d))
	return componentId(ids[0]), componentId(ids[1]), componentId(ids[2]), componentId(ids[3])
}

func identifyComponents5[A, B, C, D, E any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	ids := rooteecs.IdentifyComponents(func(t reflect.Type) uint32 {
		return uint32(ecs.getComponentId(t))
	}, reflect.TypeOf(a), reflect.TypeOf(b), reflect.TypeOf(c), reflect.TypeOf(d), reflect.TypeOf(e))
	return componentId(ids[0]), componentId(ids[1]), componentId(ids[2]), componentId(ids[3]), componentId(ids[4])
}

/*
func identifyComponents6[A, B, C, D, E, F any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	var f F
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d)), ecs.getComponentId(reflect.TypeOf(e)), ecs.getComponentId(reflect.TypeOf(f))
}

func identifyComponents7[A, B, C, D, E, F, G any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	var f F
	var g G
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d)), ecs.getComponentId(reflect.TypeOf(e)), ecs.getComponentId(reflect.TypeOf(f)), ecs.getComponentId(reflect.TypeOf(g))
}

func identifyComponents8[A, B, C, D, E, F, G, H any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	var f F
	var g G
	var h H
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d)), ecs.getComponentId(reflect.TypeOf(e)), ecs.getComponentId(reflect.TypeOf(f)), ecs.getComponentId(reflect.TypeOf(g)), ecs.getComponentId(reflect.TypeOf(h))
}
*/
