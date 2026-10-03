package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"reflect"
	"testing"
)

type e2c2BrickCounts struct{ Assignments, Current int }

func e2c2DenseBricks(source *volume.XBrickMap) map[[6]int]map[[3]int]uint8 {
	result := make(map[[6]int]map[[3]int]uint8)
	for sectorKey, sector := range source.Sectors {
		for i := 0; i < 64; i++ {
			brick := sector.GetBrick(i%4, i/4%4, i/16)
			if brick == nil {
				continue
			}
			key := [6]int{sectorKey[0], sectorKey[1], sectorKey[2], i % 4, i / 4 % 4, i / 16}
			for z := 0; z < 8; z++ {
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						if value := brick.Payload[x][y][z]; value != 0 {
							if result[key] == nil {
								result[key] = make(map[[3]int]uint8)
							}
							result[key][[3]int{x, y, z}] = value
						}
					}
				}
			}
		}
	}
	return result
}

func e2c2Verify(t *testing.T, owner *volume.ManagedXBrickMap, base, current *volume.XBrickMap) {
	t.Helper()
	original, live := e2c2DenseBricks(base), e2c2DenseBricks(current)
	expectedAssignments := make(map[[6]int]map[[3]int]uint8)
	all := make(map[[6]int]bool)
	for key := range original {
		all[key] = true
	}
	for key := range live {
		all[key] = true
	}
	for key := range all {
		for z := 0; z < 8; z++ {
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					local := [3]int{x, y, z}
					value := live[key][local]
					if value != original[key][local] {
						if expectedAssignments[key] == nil {
							expectedAssignments[key] = make(map[[3]int]uint8)
						}
						expectedAssignments[key][local] = value
					}
				}
			}
		}
	}
	want := make(map[[6]int]e2c2BrickCounts)
	for key, assignments := range expectedAssignments {
		want[key] = e2c2BrickCounts{len(assignments), len(live[key])}
	}
	got := make(map[[6]int]e2c2BrickCounts)
	if !owner.VisitChangedBricks(func(key [6]int, assignments, current int) bool {
		if _, duplicate := got[key]; duplicate {
			t.Fatal("changed inventory repeated key")
		}
		got[key] = e2c2BrickCounts{assignments, current}
		return true
	}) || !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory=%v want=%v", got, want)
	}
	for key := range all {
		assignments := make(map[[3]int]uint8)
		if !owner.VisitChangedBrickAssignments(key, func(local [3]int, value uint8) bool {
			if _, duplicate := assignments[local]; duplicate {
				t.Fatal("assignment visitor repeated cell")
			}
			assignments[local] = value
			return true
		}) {
			t.Fatal("sealed assignment visitor unavailable")
		}
		expected := expectedAssignments[key]
		if expected == nil {
			expected = map[[3]int]uint8{}
		}
		if !reflect.DeepEqual(assignments, expected) {
			t.Fatalf("brick%v assignments=%v want=%v", key, assignments, expected)
		}
		voxels := make(map[[3]int]uint8)
		if !owner.VisitCurrentBrickVoxels(key, func(local [3]int, value uint8) bool {
			if value == 0 {
				t.Fatal("current visitor emitted zero")
			}
			if _, duplicate := voxels[local]; duplicate {
				t.Fatal("current visitor repeated cell")
			}
			voxels[local] = value
			return true
		}) {
			t.Fatal("sealed current visitor unavailable")
		}
		expectedCurrent := live[key]
		if expectedCurrent == nil {
			expectedCurrent = map[[3]int]uint8{}
		}
		if !reflect.DeepEqual(voxels, expectedCurrent) {
			t.Fatalf("brick%v current=%v want=%v", key, voxels, expectedCurrent)
		}
	}
}

func TestE2c2ChangedBrickInventoryTracksFinalAssignmentsAndForks(t *testing.T) {
	base := volume.NewXBrickMap()
	for _, w := range []volume.VoxelWrite{{X: -33, Value: 2}, {X: -32, Value: 3}, {X: -9, Value: 4}, {X: -8, Value: 5}, {Value: 6}, {X: 1, Value: 7}, {X: 64, Value: 8}} {
		base.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	current := base.Copy()
	owner := volume.NewManagedXBrickMap(base)
	e2c2Verify(t, owner, base, current)
	for _, w := range []volume.VoxelWrite{{Value: 9}, {Value: 9}, {X: -33}, {X: -32}, {X: -9, Value: 6}, {X: -8}, {X: -9, Value: 7}, {X: -9, Value: 4}, {X: 2, Value: 10}, {X: 2}, {X: -33, Value: 2}} {
		owner.SetVoxel(w.X, w.Y, w.Z, w.Value)
		current.SetVoxel(w.X, w.Y, w.Z, w.Value)
		e2c2Verify(t, owner, base, current)
	}
	// An entirely cleared brick retains its zero assignment; the final revert
	// above removes the -33 brick from inventory while -32 remains cleared.
	child := owner.Fork()
	childCurrent := current.Copy()
	child.SetVoxel(-32, 0, 0, 3)
	childCurrent.SetVoxel(-32, 0, 0, 3)
	child.SetVoxel(1, 0, 0, 8)
	childCurrent.SetVoxel(1, 0, 0, 8)
	e2c2Verify(t, child, base, childCurrent)
	e2c2Verify(t, owner, base, current)
	restored := volume.NewManagedXBrickMapWithBase(base, current)
	e2c2Verify(t, restored, base, current)
	restored.SetVoxel(0, 0, 0, 6)
	reverted := current.Copy()
	reverted.SetVoxel(0, 0, 0, 6)
	e2c2Verify(t, restored, base, reverted)
	e2c2Verify(t, owner, base, current)
}

func TestE2c2BrickVisitorsStopWithoutAllocationAndExposureIsPermanent(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	owner.SetVoxel(-8, 0, 0, 2)
	owner.SetVoxel(0, 0, 0, 3)
	owner.SetVoxel(1, 0, 0, 4)
	key := [6]int{}
	calls := 0
	if !owner.VisitChangedBricks(func([6]int, int, int) bool { calls++; return false }) || calls != 1 {
		t.Fatal("inventory short circuit lost sealed availability")
	}
	calls = 0
	if !owner.VisitChangedBrickAssignments(key, func([3]int, uint8) bool { calls++; return false }) || calls != 1 {
		t.Fatal("assignment short circuit failed")
	}
	calls = 0
	if !owner.VisitCurrentBrickVoxels(key, func([3]int, uint8) bool { calls++; return false }) || calls != 1 {
		t.Fatal("current short circuit failed")
	}
	inventory := func([6]int, int, int) bool { return true }
	cell := func([3]int, uint8) bool { return true }
	if allocations := testing.AllocsPerRun(100, func() {
		owner.VisitChangedBricks(inventory)
		owner.VisitChangedBrickAssignments(key, cell)
		owner.VisitCurrentBrickVoxels(key, cell)
	}); allocations != 0 {
		t.Fatalf("brick visitors allocate: %g", allocations)
	}
	fork := owner.Fork()
	if allocations := testing.AllocsPerRun(100, func() {
		fork.VisitChangedBricks(inventory)
		fork.VisitChangedBrickAssignments(key, cell)
		fork.VisitCurrentBrickVoxels(key, cell)
	}); allocations != 0 {
		t.Fatalf("fork visitors allocate: %g", allocations)
	}
	restored := volume.NewManagedXBrickMapWithBase(nil, fork.Snapshot())
	if allocations := testing.AllocsPerRun(100, func() {
		restored.VisitChangedBricks(inventory)
		restored.VisitChangedBrickAssignments(key, cell)
		restored.VisitCurrentBrickVoxels(key, cell)
	}); allocations != 0 {
		t.Fatalf("restored visitors allocate: %g", allocations)
	}
	owner.ExposeMutable().SetVoxel(1, 0, 0, 0)
	calls = 0
	if owner.VisitChangedBricks(func([6]int, int, int) bool { calls++; return true }) || owner.VisitChangedBrickAssignments(key, func([3]int, uint8) bool { calls++; return true }) || owner.VisitCurrentBrickVoxels(key, func([3]int, uint8) bool { calls++; return true }) || calls != 0 {
		t.Fatal("exposed visitor published sealed metadata")
	}
	owner.SetVoxel(1, 0, 0, 4)
	if owner.VisitChangedBricks(inventory) {
		t.Fatal("raw revert restored sealed availability")
	}
	if !fork.VisitChangedBrickAssignments(key, cell) {
		t.Fatal("exposure disabled independent fork")
	}
}

func TestE2c2BrickVisitorsFollowImplicitDenseChanges(t *testing.T) {
	for _, kind := range []string{"stale_solid", "stale_occupancy"} {
		t.Run(kind, func(t *testing.T) {
			base := volume.NewXBrickMap()
			base.SetVoxel(0, 0, 0, 3)
			brick := base.Sectors[[3]int{}].GetBrick(0, 0, 0)
			value := uint8(7)
			if kind == "stale_solid" {
				brick.Flags = volume.BrickFlagSolid
				brick.AtlasOffset = 9
			} else {
				base.SetVoxel(7, 0, 0, 4)
				brick.OccupancyMask64 = 0
				value = 0
			}
			current := base.Copy()
			owner := volume.NewManagedXBrickMap(base).Fork()
			owner.SetVoxel(0, 0, 0, value)
			current.SetVoxel(0, 0, 0, value)
			e2c2Verify(t, owner, base, current)
		})
	}
}

func TestE2c2BrickKeysValidateNativeIntegerBoundaries(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	owner := volume.NewManagedXBrickMap(nil)
	owner.SetVoxel(-8, 0, 0, 2)
	owner.SetVoxel(0, 0, 0, 3)
	invalid := [][6]int{{0, 0, 0, -1, 0, 0}, {0, 0, 0, 4, 0, 0}, {0, 0, 0, 0, 4, 0}, {0, 0, 0, 0, 0, 4}, {maxInt, 0, 0, 3, 0, 0}, {minInt, 0, 0, 0, 0, 0}, {99, 0, 0, 0, 0, 0}}
	for _, key := range invalid {
		calls := 0
		cell := func([3]int, uint8) bool { calls++; return true }
		if !owner.VisitChangedBrickAssignments(key, cell) || !owner.VisitCurrentBrickVoxels(key, cell) || calls != 0 {
			t.Fatalf("invalid/absent key %v overflowed into a real brick", key)
		}
	}
	current := volume.NewXBrickMap()
	minKey, maxKey := [3]int{minInt / 32, 0, 0}, [3]int{maxInt / 32, 0, 0}
	current.Sectors[minKey] = volume.NewSector(minKey[0], 0, 0)
	low, _ := current.Sectors[minKey].GetOrCreateBrick(0, 0, 0)
	low.SetVoxel(0, 0, 0, 9)
	current.Sectors[maxKey] = volume.NewSector(maxKey[0], 0, 0)
	high, _ := current.Sectors[maxKey].GetOrCreateBrick(3, 0, 0)
	high.SetVoxel(7, 0, 0, 7)
	restored := volume.NewManagedXBrickMapWithBase(nil, current)
	e2c2Verify(t, restored, volume.NewXBrickMap(), current)
}
