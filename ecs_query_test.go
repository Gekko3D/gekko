package gekko

import (
	"testing"
)

func TestQuery_Map(t *testing.T) {
	type Comp1 struct{ a int }
	type Comp2 struct{ b float32 }
	type Comp3 struct{}

	ecs := MakeEcs()
	ecs.addEntity(Comp1{a: 1})                                 // comp1 only                       -- shouldn't match
	id2 := ecs.addEntity(Comp1{a: 2}, Comp2{b: 1.37})          // comp1 & comp2                    -- should match
	id3 := ecs.addEntity(Comp1{a: 3}, Comp2{b: 4.20}, Comp3{}) // comp1 & comp2 + something extra  -- should match
	ecs.addEntity(Comp1{a: 4}, Comp3{})                        // comp1 + something extra          -- shouldn't match
	ecs.addEntity(Comp2{b: 3.14})                              // comp2 only                       -- shouldn't match

	query := Query2[Comp1, Comp2]{ecs: &ecs}

	expectedA := map[EntityId]Comp1{id2: {a: 2}, id3: {a: 3}}
	expectedB := map[EntityId]Comp2{id2: {b: 1.37}, id3: {b: 4.20}}
	numResults := 0

	query.Map(func(entityId EntityId, comp1 *Comp1, comp2 *Comp2) bool {
		expA, okA := expectedA[entityId]
		if !okA {
			t.Errorf("Unexpected EntityId: %v", entityId)
		} else if *comp1 != expA {
			t.Errorf("Unexpected A for %v, expected %v got %v", entityId, expA, *comp1)
		}

		expB, okB := expectedB[entityId]
		if !okB {
			t.Errorf("Unexpected EntityId: %v", entityId)
		} else if *comp2 != expB {
			t.Errorf("Unexpected B for %v, expected %v got %v", entityId, expB, *comp2)
		}

		numResults += 1
		return true
	})

	if 2 != numResults {
		t.Errorf("Unexpected number of results, got %v", numResults)
	}
}
