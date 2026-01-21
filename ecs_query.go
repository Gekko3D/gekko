package gekko

import (
	"reflect"
	"slices"
)

// Query types with optional filters
type Query1[A any] struct {
	ecs     *Ecs
	with    []componentId
	without []componentId
	any     []componentId
}
type Query2[A, B any] struct {
	ecs     *Ecs
	with    []componentId
	without []componentId
	any     []componentId
}
type Query3[A, B, C any] struct {
	ecs     *Ecs
	with    []componentId
	without []componentId
	any     []componentId
}
type Query4[A, B, C, D any] struct {
	ecs     *Ecs
	with    []componentId
	without []componentId
	any     []componentId
}
type Query5[A, B, C, D, E any] struct {
	ecs     *Ecs
	with    []componentId
	without []componentId
	any     []componentId
}

// Constructors
func MakeQuery1[A any](cmd *Commands) Query1[A]             { return Query1[A]{ecs: cmd.app.ecs} }
func MakeQuery2[A, B any](cmd *Commands) Query2[A, B]       { return Query2[A, B]{ecs: cmd.app.ecs} }
func MakeQuery3[A, B, C any](cmd *Commands) Query3[A, B, C] { return Query3[A, B, C]{ecs: cmd.app.ecs} }
func MakeQuery4[A, B, C, D any](cmd *Commands) Query4[A, B, C, D] {
	return Query4[A, B, C, D]{ecs: cmd.app.ecs}
}
func MakeQuery5[A, B, C, D, E any](cmd *Commands) Query5[A, B, C, D, E] {
	return Query5[A, B, C, D, E]{ecs: cmd.app.ecs}
}

// Chainable filters (runtime type-based)
func (q Query1[A]) WithTypes(types ...any) Query1[A]  { q.with = append(q.with, idsOfValues(q.ecs, types...)...); return q }
func (q Query1[A]) WithoutTypes(types ...any) Query1[A] {
	q.without = append(q.without, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query1[A]) WithAnyTypes(types ...any) Query1[A] {
	q.any = append(q.any, idsOfValues(q.ecs, types...)...)
	return q
}

func (q Query2[A, B]) WithTypes(types ...any) Query2[A, B] {
	q.with = append(q.with, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query2[A, B]) WithoutTypes(types ...any) Query2[A, B] {
	q.without = append(q.without, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query2[A, B]) WithAnyTypes(types ...any) Query2[A, B] {
	q.any = append(q.any, idsOfValues(q.ecs, types...)...)
	return q
}

func (q Query3[A, B, C]) WithTypes(types ...any) Query3[A, B, C] {
	q.with = append(q.with, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query3[A, B, C]) WithoutTypes(types ...any) Query3[A, B, C] {
	q.without = append(q.without, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query3[A, B, C]) WithAnyTypes(types ...any) Query3[A, B, C] {
	q.any = append(q.any, idsOfValues(q.ecs, types...)...)
	return q
}

func (q Query4[A, B, C, D]) WithTypes(types ...any) Query4[A, B, C, D] {
	q.with = append(q.with, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query4[A, B, C, D]) WithoutTypes(types ...any) Query4[A, B, C, D] {
	q.without = append(q.without, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query4[A, B, C, D]) WithAnyTypes(types ...any) Query4[A, B, C, D] {
	q.any = append(q.any, idsOfValues(q.ecs, types...)...)
	return q
}

func (q Query5[A, B, C, D, E]) WithTypes(types ...any) Query5[A, B, C, D, E] {
	q.with = append(q.with, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query5[A, B, C, D, E]) WithoutTypes(types ...any) Query5[A, B, C, D, E] {
	q.without = append(q.without, idsOfValues(q.ecs, types...)...)
	return q
}
func (q Query5[A, B, C, D, E]) WithAnyTypes(types ...any) Query5[A, B, C, D, E] {
	q.any = append(q.any, idsOfValues(q.ecs, types...)...)
	return q
}

// Helper: type -> componentId
func idOf[T any](ecs *Ecs) componentId {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return ecs.getComponentId(t)
}

func idsOfValues(ecs *Ecs, vals ...any) []componentId {
	ids := make([]componentId, 0, len(vals))
	for _, v := range vals {
		t := reflect.TypeOf(v)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		ids = append(ids, ecs.getComponentId(t))
	}
	return ids
}

// Archetype key membership helpers (use sorted key for BinarySearch)
func archHas(arch *archetype, id componentId) bool {
	_, found := slices.BinarySearch(arch.key, id)
	return found
}
func hasAll(arch *archetype, ids []componentId) bool {
	for _, id := range ids {
		if !archHas(arch, id) {
			return false
		}
	}
	return true
}
func hasAny(arch *archetype, ids []componentId) bool {
	for _, id := range ids {
		if archHas(arch, id) {
			return true
		}
	}
	return false
}

// Existing identify helpers retained
func identifyOptionals(ecs *Ecs, components ...any) set[componentId] {
	res := make(set[componentId])
	for _, c := range components {
		res[ecs.getComponentId(reflect.TypeOf(c))] = struct{}{}
	}
	return res
}

func identifyComponents1[A any](ecs *Ecs) componentId {
	var a A
	return ecs.getComponentId(reflect.TypeOf(a))
}
func identifyComponents2[A, B any](ecs *Ecs) (componentId, componentId) {
	var a A
	var b B
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b))
}
func identifyComponents3[A, B, C any](ecs *Ecs) (componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c))
}
func identifyComponents4[A, B, C, D any](ecs *Ecs) (componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d))
}
func identifyComponents5[A, B, C, D, E any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d)), ecs.getComponentId(reflect.TypeOf(e))
}

// Query1.Map with archetype prefiltering and With/Without/WithAny
func (q Query1[A]) Map(m func(EntityId, *A) bool, optionals ...any) {
	id1 := identifyComponents1[A](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)

	// Build required list (exclude optionals)
	var req []componentId
	if _, ok := opt[id1]; !ok {
		req = append(req, id1)
	}
	req = append(req, q.with...)

	for _, arch := range q.ecs.archetypes {
		if len(q.without) > 0 && hasAny(arch, q.without) {
			continue
		}
		if len(q.any) > 0 && !hasAny(arch, q.any) {
			continue
		}
		if !hasAll(arch, req) {
			continue
		}

		// Fetch slices (optionals allowed)
		var comps1 []A
		no_a := false
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else if _, ok := opt[id1]; ok {
			no_a = true
		} else {
			continue
		}

		for entityId, row := range arch.entities {
			var a *A
			if no_a {
				a = nil
			} else {
				a = &comps1[row]
			}
			if !m(entityId, a) {
				return
			}
		}
	}
}

func (q Query2[A, B]) Map(m func(EntityId, *A, *B) bool, optionals ...any) {
	id1, id2 := identifyComponents2[A, B](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)

	var req []componentId
	if _, ok := opt[id1]; !ok {
		req = append(req, id1)
	}
	if _, ok := opt[id2]; !ok {
		req = append(req, id2)
	}
	req = append(req, q.with...)

	for _, arch := range q.ecs.archetypes {
		if len(q.without) > 0 && hasAny(arch, q.without) {
			continue
		}
		if len(q.any) > 0 && !hasAny(arch, q.any) {
			continue
		}
		if !hasAll(arch, req) {
			continue
		}

		var comps1 []A
		no_a := false
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else if _, ok := opt[id1]; ok {
			no_a = true
		} else {
			continue
		}

		var comps2 []B
		no_b := false
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else if _, ok := opt[id2]; ok {
			no_b = true
		} else {
			continue
		}

		for entityId, row := range arch.entities {
			var a *A
			if no_a {
				a = nil
			} else {
				a = &comps1[row]
			}
			var b *B
			if no_b {
				b = nil
			} else {
				b = &comps2[row]
			}
			if !m(entityId, a, b) {
				return
			}
		}
	}
}

func (q Query3[A, B, C]) Map(m func(EntityId, *A, *B, *C) bool, optionals ...any) {
	id1, id2, id3 := identifyComponents3[A, B, C](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)

	var req []componentId
	if _, ok := opt[id1]; !ok {
		req = append(req, id1)
	}
	if _, ok := opt[id2]; !ok {
		req = append(req, id2)
	}
	if _, ok := opt[id3]; !ok {
		req = append(req, id3)
	}
	req = append(req, q.with...)

	for _, arch := range q.ecs.archetypes {
		if len(q.without) > 0 && hasAny(arch, q.without) {
			continue
		}
		if len(q.any) > 0 && !hasAny(arch, q.any) {
			continue
		}
		if !hasAll(arch, req) {
			continue
		}

		var comps1 []A
		no_a := false
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else if _, ok := opt[id1]; ok {
			no_a = true
		} else {
			continue
		}

		var comps2 []B
		no_b := false
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else if _, ok := opt[id2]; ok {
			no_b = true
		} else {
			continue
		}

		var comps3 []C
		no_c := false
		if arg3CompData, ok := arch.componentData[id3]; ok {
			comps3 = arg3CompData.([]C)
		} else if _, ok := opt[id3]; ok {
			no_c = true
		} else {
			continue
		}

		for entityId, row := range arch.entities {
			var a *A
			if no_a {
				a = nil
			} else {
				a = &comps1[row]
			}
			var b *B
			if no_b {
				b = nil
			} else {
				b = &comps2[row]
			}
			var c *C
			if no_c {
				c = nil
			} else {
				c = &comps3[row]
			}
			if !m(entityId, a, b, c) {
				return
			}
		}
	}
}

func (q Query4[A, B, C, D]) Map(m func(EntityId, *A, *B, *C, *D) bool, optionals ...any) {
	id1, id2, id3, id4 := identifyComponents4[A, B, C, D](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)

	var req []componentId
	if _, ok := opt[id1]; !ok {
		req = append(req, id1)
	}
	if _, ok := opt[id2]; !ok {
		req = append(req, id2)
	}
	if _, ok := opt[id3]; !ok {
		req = append(req, id3)
	}
	if _, ok := opt[id4]; !ok {
		req = append(req, id4)
	}
	req = append(req, q.with...)

	for _, arch := range q.ecs.archetypes {
		if len(q.without) > 0 && hasAny(arch, q.without) {
			continue
		}
		if len(q.any) > 0 && !hasAny(arch, q.any) {
			continue
		}
		if !hasAll(arch, req) {
			continue
		}

		var comps1 []A
		no_a := false
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else if _, ok := opt[id1]; ok {
			no_a = true
		} else {
			continue
		}

		var comps2 []B
		no_b := false
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else if _, ok := opt[id2]; ok {
			no_b = true
		} else {
			continue
		}

		var comps3 []C
		no_c := false
		if arg3CompData, ok := arch.componentData[id3]; ok {
			comps3 = arg3CompData.([]C)
		} else if _, ok := opt[id3]; ok {
			no_c = true
		} else {
			continue
		}

		var comps4 []D
		no_d := false
		if arg4CompData, ok := arch.componentData[id4]; ok {
			comps4 = arg4CompData.([]D)
		} else if _, ok := opt[id4]; ok {
			no_d = true
		} else {
			continue
		}

		for entityId, row := range arch.entities {
			var a *A
			if no_a {
				a = nil
			} else {
				a = &comps1[row]
			}
			var b *B
			if no_b {
				b = nil
			} else {
				b = &comps2[row]
			}
			var c *C
			if no_c {
				c = nil
			} else {
				c = &comps3[row]
			}
			var d *D
			if no_d {
				d = nil
			} else {
				d = &comps4[row]
			}
			if !m(entityId, a, b, c, d) {
				return
			}
		}
	}
}

func (q Query5[A, B, C, D, E]) Map(m func(EntityId, *A, *B, *C, *D, *E) bool, optionals ...any) {
	id1, id2, id3, id4, id5 := identifyComponents5[A, B, C, D, E](q.ecs)
	opt := identifyOptionals(q.ecs, optionals...)

	var req []componentId
	if _, ok := opt[id1]; !ok {
		req = append(req, id1)
	}
	if _, ok := opt[id2]; !ok {
		req = append(req, id2)
	}
	if _, ok := opt[id3]; !ok {
		req = append(req, id3)
	}
	if _, ok := opt[id4]; !ok {
		req = append(req, id4)
	}
	if _, ok := opt[id5]; !ok {
		req = append(req, id5)
	}
	req = append(req, q.with...)

	for _, arch := range q.ecs.archetypes {
		if len(q.without) > 0 && hasAny(arch, q.without) {
			continue
		}
		if len(q.any) > 0 && !hasAny(arch, q.any) {
			continue
		}
		if !hasAll(arch, req) {
			continue
		}

		var comps1 []A
		no_a := false
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else if _, ok := opt[id1]; ok {
			no_a = true
		} else {
			continue
		}

		var comps2 []B
		no_b := false
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else if _, ok := opt[id2]; ok {
			no_b = true
		} else {
			continue
		}

		var comps3 []C
		no_c := false
		if arg3CompData, ok := arch.componentData[id3]; ok {
			comps3 = arg3CompData.([]C)
		} else if _, ok := opt[id3]; ok {
			no_c = true
		} else {
			continue
		}

		var comps4 []D
		no_d := false
		if arg4CompData, ok := arch.componentData[id4]; ok {
			comps4 = arg4CompData.([]D)
		} else if _, ok := opt[id4]; ok {
			no_d = true
		} else {
			continue
		}

		var comps5 []E
		no_e := false
		if arg5CompData, ok := arch.componentData[id5]; ok {
			comps5 = arg5CompData.([]E)
		} else if _, ok := opt[id5]; ok {
			no_e = true
		} else {
			continue
		}

		for entityId, row := range arch.entities {
			var a *A
			if no_a {
				a = nil
			} else {
				a = &comps1[row]
			}
			var b *B
			if no_b {
				b = nil
			} else {
				b = &comps2[row]
			}
			var c *C
			if no_c {
				c = nil
			} else {
				c = &comps3[row]
			}
			var d *D
			if no_d {
				d = nil
			} else {
				d = &comps4[row]
			}
			var e *E
			if no_e {
				e = nil
			} else {
				e = &comps5[row]
			}
			if !m(entityId, a, b, c, d, e) {
				return
			}
		}
	}
}

/*
 // Optional: extend to Query6..Query8 similarly if needed.
*/
