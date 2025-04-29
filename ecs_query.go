package gekko

import (
	"reflect"
)

// TO get more queries:
//  1. Uncomment QueryN
//  2. Uncomment identifyComponentsN
//  3. Copy MapN-1() and implement according to other Map() functions
//  4. Implement Command's MakeQuery to make it available in the user code
type Query1[A any] struct{ ecs *Ecs }
type Query2[A, B any] struct{ ecs *Ecs }
type Query3[A, B, C any] struct{ ecs *Ecs }
type Query4[A, B, C, D any] struct{ ecs *Ecs }

/*
type Query5[A, B, C, D, E any] struct{ ecs *Ecs }
type Query6[A, B, C, D, E, F any] struct{ ecs *Ecs }
type Query7[A, B, C, D, E, F, G any] struct{ ecs *Ecs }
type Query8[A, B, C, D, E, F, G, H any] struct{ ecs *Ecs }
*/

func MakeQuery1[A any](cmd *Commands) Query1[A]             { return Query1[A]{ecs: cmd.app.ecs} }
func MakeQuery2[A, B any](cmd *Commands) Query2[A, B]       { return Query2[A, B]{ecs: cmd.app.ecs} }
func MakeQuery3[A, B, C any](cmd *Commands) Query3[A, B, C] { return Query3[A, B, C]{ecs: cmd.app.ecs} }
func MakeQuery4[A, B, C, D any](cmd *Commands) Query4[A, B, C, D] {
	return Query4[A, B, C, D]{ecs: cmd.app.ecs}
}

func (q Query1[A]) Map1(m func(EntityId, *A) bool) {
	id1 := identifyComponents1[A](q.ecs)

	for _, arch := range q.ecs.archetypes {
		// Check required components
		var comps1 []A
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else {
			continue
		}

		// Return entities
		for entityId, row := range arch.entities {
			if !m(entityId, &comps1[row]) {
				return
			}
		}
	}
}

func (q Query2[A, B]) Map2(m func(EntityId, *A, *B) bool) {
	id1, id2 := identifyComponents2[A, B](q.ecs)

	for _, arch := range q.ecs.archetypes {
		// Check required components
		var comps1 []A
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else {
			continue
		}

		var comps2 []B
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else {
			continue
		}

		// Return entities
		for entityId, row := range arch.entities {
			if !m(entityId, &comps1[row], &comps2[row]) {
				return
			}
		}
	}
}

func (q Query3[A, B, C]) Map3(m func(EntityId, *A, *B, *C) bool) {
	id1, id2, id3 := identifyComponents3[A, B, C](q.ecs)

	for _, arch := range q.ecs.archetypes {
		// Check required components
		var comps1 []A
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else {
			continue
		}

		var comps2 []B
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else {
			continue
		}

		var comps3 []C
		if arg3CompData, ok := arch.componentData[id3]; ok {
			comps3 = arg3CompData.([]C)
		} else {
			continue
		}

		// Return entities
		for entityId, row := range arch.entities {
			if !m(entityId, &comps1[row], &comps2[row], &comps3[row]) {
				return
			}
		}
	}
}

func (q Query4[A, B, C, D]) Map4(m func(EntityId, *A, *B, *C, *D) bool) {
	id1, id2, id3, id4 := identifyComponents4[A, B, C, D](q.ecs)

	for _, arch := range q.ecs.archetypes {
		// Check required components
		var comps1 []A
		if arg1CompData, ok := arch.componentData[id1]; ok {
			comps1 = arg1CompData.([]A)
		} else {
			continue
		}

		var comps2 []B
		if arg2CompData, ok := arch.componentData[id2]; ok {
			comps2 = arg2CompData.([]B)
		} else {
			continue
		}

		var comps3 []C
		if arg3CompData, ok := arch.componentData[id3]; ok {
			comps3 = arg3CompData.([]C)
		} else {
			continue
		}

		var comps4 []D
		if arg4CompData, ok := arch.componentData[id4]; ok {
			comps4 = arg4CompData.([]D)
		} else {
			continue
		}

		// Return entities
		for entityId, row := range arch.entities {
			if !m(entityId, &comps1[row], &comps2[row], &comps3[row], &comps4[row]) {
				return
			}
		}
	}
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

/*
func identifyComponents5[A, B, C, D, E any](ecs *Ecs) (componentId, componentId, componentId, componentId, componentId) {
	var a A
	var b B
	var c C
	var d D
	var e E
	return ecs.getComponentId(reflect.TypeOf(a)), ecs.getComponentId(reflect.TypeOf(b)), ecs.getComponentId(reflect.TypeOf(c)), ecs.getComponentId(reflect.TypeOf(d)), ecs.getComponentId(reflect.TypeOf(e))
}

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
