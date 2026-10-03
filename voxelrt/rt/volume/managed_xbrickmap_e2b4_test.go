package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"reflect"
	"testing"
)

func TestE2b4TrackedCountAndVisitor(t *testing.T) {
	m := volume.NewManagedXBrickMap(p1cFixture())
	check := func(owner *volume.ManagedXBrickMap, n int, tracked bool) {
		t.Helper()
		if got, ok := owner.TrackedChangeCount(); got != n || ok != tracked {
			t.Fatalf("count=(%d,%v), want (%d,%v)", got, ok, n, tracked)
		}
		seen := make(map[[3]int]uint8)
		ok := owner.VisitTrackedChanges(func(w volume.VoxelWrite) bool { seen[[3]int{w.X, w.Y, w.Z}] = w.Value; return true })
		if ok != tracked || len(seen) != n {
			t.Fatalf("visitor=(%v,%v), want %d/%v", seen, ok, n, tracked)
		}
	}
	check(m, 0, true)
	m.SetVoxel(-33, 0, 0, 0)
	m.SetVoxel(8, 0, 0, 9)
	m.SetVoxel(40, 0, 0, 7)
	check(m, 3, true)
	assignments := make(map[[3]int]uint8)
	m.VisitTrackedChanges(func(w volume.VoxelWrite) bool { assignments[[3]int{w.X, w.Y, w.Z}] = w.Value; return true })
	if !reflect.DeepEqual(assignments, map[[3]int]uint8{{-33, 0, 0}: 0, {8, 0, 0}: 9, {40, 0, 0}: 7}) {
		t.Fatalf("visitor assignments=%v", assignments)
	}
	m.SetVoxel(40, 0, 0, 7)
	check(m, 3, true)
	m.SetVoxel(40, 0, 0, 5)
	m.SetVoxel(8, 0, 0, 0)
	check(m, 1, true)
	assignments = make(map[[3]int]uint8)
	m.VisitTrackedChanges(func(w volume.VoxelWrite) bool { assignments[[3]int{w.X, w.Y, w.Z}] = w.Value; return true })
	if !reflect.DeepEqual(assignments, map[[3]int]uint8{{-33, 0, 0}: 0}) {
		t.Fatalf("reverted visitor assignments=%v", assignments)
	}
	fork := m.Fork()
	fork.SetVoxel(-33, 0, 0, 2)
	fork.SetVoxel(10, 0, 0, 6)
	check(fork, 1, true)
	check(m, 1, true)
	calls := 0
	if !m.VisitTrackedChanges(func(w volume.VoxelWrite) bool { calls++; return false }) || calls != 1 {
		t.Fatal("sealed visitor must stop at first callback without losing tracking availability")
	}
	m.SetVoxel(10, 0, 0, 6)
	calls = 0
	m.VisitTrackedChanges(func(w volume.VoxelWrite) bool { calls++; return false })
	if calls != 1 {
		t.Fatal("visitor did not short circuit")
	}
	if allocations := testing.AllocsPerRun(100, func() { m.TrackedChangeCount(); m.VisitTrackedChanges(func(volume.VoxelWrite) bool { return true }) }); allocations != 0 {
		t.Fatalf("count/visitor allocate: %g", allocations)
	}
	m.ExposeMutable()
	check(m, 0, false)
	calls = 0
	if m.VisitTrackedChanges(func(volume.VoxelWrite) bool { calls++; return true }) || calls != 0 {
		t.Fatal("exposed visitor published tracked assignments")
	}
	check(fork, 1, true)
}
