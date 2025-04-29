package gekko

import (
	"reflect"
	"testing"
)

func TestEcs_MakeEcs(t *testing.T) {
	ecs := MakeEcs()

	// Check if the fields are initialized properly
	if len(ecs.archetypes) != 0 {
		t.Errorf("Expected archetypes to be empty, got %v", ecs.archetypes)
	}

	if len(ecs.entityIndex) != 0 {
		t.Errorf("Expected entityIndex to be empty, got %v", ecs.entityIndex)
	}

	if ecs.entityIdCounter != 0 {
		t.Errorf("Expected entityIdCounter to be 0, got %v", ecs.entityIdCounter)
	}

	if ecs.componentIdCounter != 0 {
		t.Errorf("Expected componentIdCounter to be 0, got %v", ecs.componentIdCounter)
	}
}

func TestEcs_AddEntity(t *testing.T) {
	ecs := MakeEcs()

	// Add an entity with no components (can also test with components added)
	entityId := ecs.addEntity()

	// Check if the entity is added to the entityIndex
	if _, ok := ecs.entityIndex[entityId]; !ok {
		t.Errorf("Expected entityId %v to be in entityIndex", entityId)
	}

	type TestComponent struct {
		x string
	}
	testComp := TestComponent{
		x: "test",
	}

	entityId2 := ecs.addEntity(testComp)
	// Check if the entity is added to the entityIndex
	if _, ok := ecs.entityIndex[entityId2]; !ok {
		t.Errorf("Expected entityId %v to be in entityIndex", entityId2)
	}

	archId1 := ecs.entityIndex[entityId]
	archId2 := ecs.entityIndex[entityId2]
	if archId1 == archId2 {
		t.Errorf("Entities with different components ended up in the same Archetype")
	}
}

func TestEcs_AddComponents(t *testing.T) {
	type TestComponent0 struct{ a int }
	type TestComponent1 struct{ x string }
	type TestComponent2 struct{ y string }
	type TestComponent3 struct{ z string }

	ecs := MakeEcs()

	// Create a new entity
	entityId := ecs.addEntity(TestComponent0{a: 1337})

	// Add components to the entity
	ecs.addComponents(entityId, TestComponent1{x: "test"}, TestComponent2{y: "hello"})

	// Test using pointers too
	ecs.addComponents(entityId, &TestComponent3{z: "test-2"})

	// Verify if the entity's archetype has changed or the components are properly added
	// The specific checks depend on your component's expected behavior.
	// For example, check if the new archetype has the added components.
	archId := ecs.entityIndex[entityId]
	arch := ecs.archetypes[archId]
	if 4 != len(arch.componentData) {
		t.Errorf("Should have ended up in an Archetype with 2 components")
	}
}
func TestEcs_AddInvalidComponentShouldPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on invalid component type")
		}
	}()

	ecs := MakeEcs()
	ecs.addEntity(123) // invalid component
}

func TestEcs_ComponentRegistration(t *testing.T) {
	type Position struct{ x, y float64 }

	ecs := MakeEcs()
	id1 := ecs.getComponentId(reflect.TypeOf(Position{}))
	id2 := ecs.getComponentId(reflect.TypeOf(Position{}))

	if id1 != id2 {
		t.Errorf("expected component IDs to be equal")
	}

	tp := ecs.getComponentType(id1)
	if tp != reflect.TypeOf(Position{}) {
		t.Errorf("expected Position type, got %s", tp.Name())
	}
}

func TestEcs_ArchetypeKeyExtension(t *testing.T) {
	key := dedupAndSortArchetypeKey([]componentId{3, 1, 2, 1, 3})
	expected := archetypeKey{1, 2, 3}

	for i, v := range key {
		if v != expected[i] {
			t.Errorf("dedup: expected %v, got %v", expected, key)
		}
	}

	key = combineArchetypeKeys([]componentId{1, 2, 3}, []componentId{4, 3, 2, 1})
	expected = archetypeKey{1, 2, 3, 4}

	for i, v := range key {
		if v != expected[i] {
			t.Errorf("combine: expected %v, got %v", expected, key)
		}
	}
}

func TestEcs_RemoveEntity(t *testing.T) {
	type Position struct{ X, Y float64 }

	ecs := MakeEcs()
	id := ecs.addEntity(Position{1, 2})
	ecs.removeEntity(id)

	if _, ok := ecs.entityIndex[id]; ok {
		t.Errorf("entity not removed")
	}
}

func TestEcs_RecycleEntity(t *testing.T) {
	ecs := MakeEcs()

	// Add an entity
	id := ecs.addEntity()

	// Recycle the entity
	ecs.recycleEntity(id)

	// Ensure that the entity is removed from the entityIndex and archetype
	if _, ok := ecs.entityIndex[id]; ok {
		t.Errorf("Expected entityId %v to be removed from entityIndex", id)
	}
}
