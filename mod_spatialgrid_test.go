package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestSpatialHashGrid_InsertionAndQuery(t *testing.T) {
	grid := NewSpatialHashGrid(2.0)

	eid1 := EntityId(1)
	aabb1 := AABBComponent{
		Min: mgl32.Vec3{0, 0, 0},
		Max: mgl32.Vec3{1, 1, 1},
	}

	eid2 := EntityId(2)
	aabb2 := AABBComponent{
		Min: mgl32.Vec3{3, 3, 3},
		Max: mgl32.Vec3{4, 4, 4},
	}

	grid.Insert(eid1, aabb1)
	grid.Insert(eid2, aabb2)

	// Query around eid1
	res1 := grid.QueryAABB(aabb1)
	if len(res1) != 1 || res1[0] != eid1 {
		t.Errorf("Expected eid1, got %v", res1)
	}

	// Query around eid2
	res2 := grid.QueryAABB(aabb2)
	if len(res2) != 1 || res2[0] != eid2 {
		t.Errorf("Expected eid2, got %v", res2)
	}

	// Query middle
	aabbMid := AABBComponent{
		Min: mgl32.Vec3{1, 1, 1},
		Max: mgl32.Vec3{3, 3, 3},
	}
	resMid := grid.QueryAABB(aabbMid)
	// Cell size 2.0.
	// aabb1 occupies cell (0,0,0) (and maybe part of neighbors?)
	// Cell index for 0 is 0. Cell index for 1 is 0. So aabb1 is only in cell (0,0,0).
	// aabb2 occupies cell index 1.5? ceil(1.5)=1? No, floor(3/2) = 1. floor(4/2) = 2.
	// So aabb2 is in cells (1,1,1) and maybe (2,2,2)?
	// Let's check getCellIndex(3) = floor(1.5) = 1. getCellIndex(4) = floor(2.0) = 2.
	// Yes, aabb2 is in cells where indices any of {1,2} are present.

	// aabbMid min=1 (idx 0), max=3 (idx 1).
	// So aabbMid covers cells (0,0,0) and (1,1,1).
	// It should return BOTH eid1 and eid2.
	if len(resMid) != 2 {
		t.Errorf("Expected 2 entities, got %d: %v", len(resMid), resMid)
	}
}

func TestSpatialGridModule_Systems(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	app.UseModules(SpatialGridModule{})
	app.build()

	// Create entity with Transform and Collider
	eid := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{10, 10, 10}, Scale: mgl32.Vec3{1, 1, 1}},
		&ColliderComponent{AABBHalfExtents: mgl32.Vec3{1, 1, 1}},
		&AABBComponent{},
	)

	app.FlushCommands()

	// Run app for one update
	// app.Update() doesn't exist? Run callSystems manually.
	app.callSystems(0, execute) // State 0, phase execute
	// PreUpdate runs before Execute in callSystems?
	// Let's check callSystems in app.go.
	// It loops through stages. Prelude, PreUpdate, Update...

	// Check if AABB was updated
	ecs := app.ecs
	archId := ecs.entityIndex[eid]
	arch := ecs.archetypes[archId]
	row := arch.entities[eid]

	compType := reflect.TypeOf(AABBComponent{})
	compId := ecs.getComponentId(compType)
	aabbSlice := arch.componentData[compId].([]AABBComponent)
	aabb := aabbSlice[row]

	if aabb.Min.X() != 9 || aabb.Max.X() != 11 {
		t.Errorf("AABB not updated correctly: %+v", aabb)
	}

	// Check if grid has it
	grid := app.resources[reflect.TypeOf(SpatialHashGrid{})].(*SpatialHashGrid)
	res := grid.QueryRadius(mgl32.Vec3{10, 10, 10}, 0.1)
	if len(res) != 1 || res[0] != eid {
		t.Errorf("Grid Query failed: %v", res)
	}
}
