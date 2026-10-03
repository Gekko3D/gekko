package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"reflect"
	"testing"
)

func TestE2c3InventoryAllowsBoundedReadOnlyTargetVisitors(t *testing.T) {
	base := volume.NewXBrickMap()
	base.SetVoxel(0, 0, 0, 1)
	base.SetVoxel(1, 0, 0, 2)
	base.SetVoxel(8, 0, 0, 3)
	owner := volume.NewManagedXBrickMap(base)
	owner.SetVoxel(0, 0, 0, 7)
	owner.SetVoxel(1, 0, 0, 0)
	owner.SetVoxel(8, 0, 0, 0)
	assignments := make(map[[6]int]map[[3]int]uint8)
	current := make(map[[6]int]map[[3]int]uint8)
	visits := 0
	if !owner.VisitChangedBricks(func(key [6]int, n, live int) bool {
		visits++
		assignments[key] = make(map[[3]int]uint8)
		current[key] = make(map[[3]int]uint8)
		if !owner.VisitChangedBrickAssignments(key, func(local [3]int, value uint8) bool { assignments[key][local] = value; return true }) || len(assignments[key]) != n {
			t.Fatal("nested assignment read disagrees with inventory")
		}
		if !owner.VisitCurrentBrickVoxels(key, func(local [3]int, value uint8) bool { current[key][local] = value; return true }) || len(current[key]) != live {
			t.Fatal("nested current read disagrees with inventory")
		}
		inner := 0
		if !owner.VisitChangedBrickAssignments(key, func([3]int, uint8) bool { inner++; return false }) || inner != 1 {
			t.Fatal("inner short circuit lost availability")
		}
		inner = 0
		if !owner.VisitCurrentBrickVoxels(key, func([3]int, uint8) bool { inner++; return false }) || inner != min(live, 1) {
			t.Fatal("inner current short circuit failed")
		}
		return true
	}) || visits != 2 {
		t.Fatal("inner short circuit interrupted outer inventory")
	}
	wantAssignments := map[[6]int]map[[3]int]uint8{{}: {[3]int{}: 7, {1, 0, 0}: 0}, {0, 0, 0, 1, 0, 0}: {[3]int{}: 0}}
	wantCurrent := map[[6]int]map[[3]int]uint8{{}: {[3]int{}: 7}, {0, 0, 0, 1, 0, 0}: {}}
	if !reflect.DeepEqual(assignments, wantAssignments) || !reflect.DeepEqual(current, wantCurrent) {
		t.Fatal("nested visitors changed paint/clear records")
	}
	cell := func([3]int, uint8) bool { return true }
	inventory := func(key [6]int, _, _ int) bool {
		owner.VisitChangedBrickAssignments(key, cell)
		owner.VisitCurrentBrickVoxels(key, cell)
		return true
	}
	if allocations := testing.AllocsPerRun(100, func() { owner.VisitChangedBricks(inventory) }); allocations != 0 {
		t.Fatalf("nested read-only visits allocate: %g", allocations)
	}
}
