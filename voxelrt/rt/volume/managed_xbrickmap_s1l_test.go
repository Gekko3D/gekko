package volume_test

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1lCapture(t *testing.T, owner *volume.ManagedXBrickMap) volume.ManagedTopologyView {
	t.Helper()
	view, ok := owner.CaptureTopology()
	if !ok {
		t.Fatal("sealed topology capture unavailable")
	}
	return view
}

func s1lTopology(t *testing.T, view volume.ManagedTopologyView, want ...[3]int) {
	t.Helper()
	if got := view.Len(); got != len(want) {
		t.Fatalf("topology length = %d, want %d", got, len(want))
	}
	for i, key := range want {
		if got, ok := view.Coord(i); !ok || got != key {
			t.Fatalf("coordinate %d = %v, %v; want %v, true", i, got, ok, key)
		}
	}
	for _, i := range []int{-1, len(want), int(^uint(0) >> 1)} {
		if _, ok := view.Coord(i); ok {
			t.Fatalf("out-of-range coordinate %d available", i)
		}
	}
}

func TestS1lEmptyAndInvalidTopology(t *testing.T) {
	var zero volume.ManagedTopologyView
	s1lTopology(t, zero)
	var nilOwner *volume.ManagedXBrickMap
	if view, ok := nilOwner.CaptureTopology(); ok || view.Len() != 0 {
		t.Fatal("nil owner must refuse capture with an empty view")
	}
	for _, owner := range []*volume.ManagedXBrickMap{
		volume.NewManagedXBrickMap(nil),
		volume.NewManagedXBrickMapWithBase(nil, nil),
	} {
		s1lTopology(t, s1lCapture(t, owner))
	}
}

func TestS1lConstructorsOwnCurrentKeysInSignedXYZOrder(t *testing.T) {
	minInt, maxInt := -int(^uint(0)>>1)-1, int(^uint(0)>>1)
	want := [][3]int{
		{minInt, 0, 0}, {-2, 5, 1}, {-1, -2, 3}, {-1, 0, -4},
		{-1, 0, 2}, {0, -3, 0}, {0, 0, 0}, {maxInt, 0, 0},
	}
	for _, withBase := range []bool{false, true} {
		t.Run(fmt.Sprintf("withBase=%v", withBase), func(t *testing.T) {
			current := volume.NewXBrickMap()
			// Include allocated empty sectors and extreme signed keys. Map keys,
			// rather than payload occupancy or revision tombstones, define topology.
			for i := len(want) - 1; i >= 0; i-- {
				key := want[i]
				current.Sectors[key] = volume.NewSector(key[0], key[1], key[2])
			}
			current.SetVoxel(0, 0, 0, 7)
			current.SetVoxel(320, 0, 0, 9)
			current.SetVoxel(320, 0, 0, 0) // Tombstone is not a current sector.
			base := volume.NewXBrickMap()
			base.SetVoxel(64, 0, 0, 3) // Base-only sector must be absent.
			var owner *volume.ManagedXBrickMap
			if withBase {
				owner = volume.NewManagedXBrickMapWithBase(base, current)
			} else {
				owner = volume.NewManagedXBrickMap(current)
			}
			view := s1lCapture(t, owner)
			s1lTopology(t, view, want...)
			clear(current.Sectors)
			current.SetVoxel(96, 0, 0, 8)
			clear(base.Sectors)
			base.SetVoxel(128, 0, 0, 6)
			s1lTopology(t, view, want...)
			s1lTopology(t, s1lCapture(t, owner), want...)
		})
	}
	base := volume.NewXBrickMap()
	base.SetVoxel(0, 0, 0, 1)
	s1lTopology(t, s1lCapture(t, volume.NewManagedXBrickMapWithBase(base, nil)))
}

func TestS1lAssignmentsChangeOnlyTargetMembershipAndRetainHistory(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(-1, 0, 0, 1)
	source.SetVoxel(0, 0, 0, 2)
	source.SetVoxel(8, 0, 0, 3)
	source.Sectors[[3]int{9, 9, 9}] = volume.NewSector(9, 9, 9)
	source.Sectors[[3]int{-1, 0, 0}].GetBrick(3, 0, 0).PrecomputedAux = []byte{1, 2}
	owner := volume.NewManagedXBrickMap(source)
	original := s1lCapture(t, owner)
	want := [][3]int{{-1, 0, 0}, {0, 0, 0}, {9, 9, 9}}
	owner.ApplyVoxelWrites(nil)
	owner.SetVoxel(0, 0, 0, 2)   // Existing-value no-op.
	owner.SetVoxel(640, 0, 0, 0) // Absent-value no-op.
	owner.SetVoxel(0, 0, 0, 4)   // Content and negative-seam auxiliary halo only.
	owner.SetVoxel(1, 0, 0, 5)   // A second voxel in the same sector.
	s1lTopology(t, s1lCapture(t, owner), want...)
	owner.SetVoxel(0, 0, 0, 0)
	owner.SetVoxel(1, 0, 0, 0) // Another brick still keeps this sector alive.
	s1lTopology(t, s1lCapture(t, owner), want...)
	owner.SetVoxel(8, 0, 0, 0) // Last occupied brick removes its sector.
	removed := s1lCapture(t, owner)
	s1lTopology(t, removed, [3]int{-1, 0, 0}, [3]int{9, 9, 9})
	owner.SetVoxel(-33, -1, -1, 6)
	inserted := s1lCapture(t, owner)
	s1lTopology(t, inserted, [3]int{-2, -1, -1}, [3]int{-1, 0, 0}, [3]int{9, 9, 9})
	owner.SetVoxel(-33, -1, -1, 0)
	owner.SetVoxel(0, 0, 0, 2) // Reinsert the original key after removal.
	s1lTopology(t, s1lCapture(t, owner), want...)
	s1lTopology(t, original, want...)
	s1lTopology(t, removed, [3]int{-1, 0, 0}, [3]int{9, 9, 9})
	s1lTopology(t, inserted, [3]int{-2, -1, -1}, [3]int{-1, 0, 0}, [3]int{9, 9, 9})
}

func TestS1lForkExposureAndFreshRawForkHaveIndependentHistory(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	owner.SetVoxel(0, 0, 0, 1)
	owner.SetVoxel(32, 0, 0, 2)
	inherited := s1lCapture(t, owner)
	child := owner.Fork()
	owner.SetVoxel(0, 0, 0, 0)
	child.SetVoxel(32, 0, 0, 0)
	beforeExposure := s1lCapture(t, owner)
	s1lTopology(t, beforeExposure, [3]int{1, 0, 0})
	s1lTopology(t, s1lCapture(t, child), [3]int{0, 0, 0})
	raw := owner.ExposeMutable()
	raw.SetVoxel(32, 0, 0, 0)
	raw.SetVoxel(64, 0, 0, 3)
	raw.Sectors[[3]int{3, -1, 2}] = volume.NewSector(3, -1, 2)
	if view, ok := owner.CaptureTopology(); ok || view.Len() != 0 {
		t.Fatal("exposed owner must refuse topology capture")
	}
	fresh := owner.Fork()
	freshView := s1lCapture(t, fresh)
	s1lTopology(t, freshView, [3]int{2, 0, 0}, [3]int{3, -1, 2})
	clear(raw.Sectors)
	owner.SetVoxel(128, 0, 0, 4)
	if _, ok := owner.CaptureTopology(); ok {
		t.Fatal("managed writes or forks restored exposed topology availability")
	}
	s1lTopology(t, s1lCapture(t, fresh), [3]int{2, 0, 0}, [3]int{3, -1, 2})
	fresh.SetVoxel(64, 0, 0, 0)
	fresh.SetVoxel(160, 0, 0, 5)
	s1lTopology(t, s1lCapture(t, fresh), [3]int{3, -1, 2}, [3]int{5, 0, 0})
	s1lTopology(t, inherited, [3]int{0, 0, 0}, [3]int{1, 0, 0})
	s1lTopology(t, beforeExposure, [3]int{1, 0, 0})
	s1lTopology(t, freshView, [3]int{2, 0, 0}, [3]int{3, -1, 2})
	s1lTopology(t, s1lCapture(t, child), [3]int{0, 0, 0})
}

func TestS1lProducerPanicRetainsAppliedTopologyPrefix(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	owner.SetVoxel(0, 0, 0, 1)
	before := s1lCapture(t, owner)
	marker := &struct{}{}
	func() {
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("producer panic = %v, want marker", got)
			}
		}()
		owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{X: -1, Y: -33, Z: 32, Value: 2})
			yield(volume.VoxelWrite{}) // Remove the original last voxel.
			panic(marker)
		})
	}()
	// Capture only after unwinding; publication inside an edit producer is unsupported.
	s1lTopology(t, s1lCapture(t, owner), [3]int{-1, -2, 1})
	s1lTopology(t, before, [3]int{0, 0, 0})
}

func TestS1lChainedStructuralEditsKeepRanksAndHistoricalViews(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	var keys [64][3]int
	for i := range keys {
		keys[i] = [3]int{i%4 - 2, i/4%4 - 2, i/16 - 2}
	}
	expected := make(map[[3]int]bool)
	type retained struct {
		view volume.ManagedTopologyView
		want [][3]int
	}
	var history []retained
	check := func(retain bool) {
		t.Helper()
		want := make([][3]int, 0, len(expected))
		for key := range expected {
			want = append(want, key)
		}
		slices.SortFunc(want, func(a, b [3]int) int {
			for axis := range a {
				if order := cmp.Compare(a[axis], b[axis]); order != 0 {
					return order
				}
			}
			return 0
		})
		view := s1lCapture(t, owner)
		s1lTopology(t, view, want...)
		for _, old := range history {
			s1lTopology(t, old.view, old.want...)
		}
		if retain {
			history = append(history, retained{view: view, want: want})
		}
	}
	assign := func(index int, value uint8) {
		t.Helper()
		key := keys[index]
		owner.SetVoxel(key[0]*volume.SectorSize+3, key[1]*volume.SectorSize+3, key[2]*volume.SectorSize+3, value)
		if value == 0 {
			delete(expected, key)
		} else {
			expected[key] = true
		}
		check(false)
	}
	check(true)
	for i := range keys {
		assign(i*37%len(keys), 1) // A permutation rather than sorted insertion.
	}
	check(true)
	for i := 0; i < len(keys); i += 3 {
		assign(i, 0)
	}
	check(true)
	for i := 60; i >= 0; i -= 6 {
		assign(i, 2) // Reinsert only part of the first deleted subset.
	}
	check(true)
	for i := 1; i < len(keys); i += 2 {
		assign(i, 0)
	}
	check(true)
	for i := len(keys) - 1; i >= 1; i -= 2 {
		assign(i, 3)
	}
	check(true)
	for i := range keys {
		assign(i, 0)
	}
	check(true) // A drained sealed owner still captures a valid empty topology.
	assign(17, 4)
	check(true) // Reinsertion must preserve the retained empty and populated views.
}

func TestS1lFirstCaptureAndIndexedReadsAllocateNothing(t *testing.T) {
	for _, size := range []int{1, 2048} {
		t.Run(fmt.Sprintf("sectors=%d", size), func(t *testing.T) {
			// AllocsPerRun warms up once. Use a fresh eagerly constructed owner
			// every time so lazy first-capture or first-read copying cannot hide.
			const runs = 8
			owners := make([]*volume.ManagedXBrickMap, runs+1)
			views := make([]volume.ManagedTopologyView, runs+1)
			for i := range owners {
				source := volume.NewXBrickMap()
				for x := size - 1; x >= 0; x-- {
					source.Sectors[[3]int{x, 0, 0}] = volume.NewSector(x, 0, 0)
				}
				owners[i] = volume.NewManagedXBrickMap(source)
			}
			next, valid := 0, true
			allocs := testing.AllocsPerRun(runs, func() {
				view, ok := owners[next].CaptureTopology()
				views[next] = view
				next++
				valid = valid && ok && view.Len() == size
				for _, index := range [3]int{0, size / 2, size - 1} {
					coord, present := view.Coord(index)
					valid = valid && present && coord == [3]int{index, 0, 0}
				}
			})
			if !valid || next != runs+1 {
				t.Fatal("first capture or indexed reads returned incorrect topology")
			}
			if allocs != 0 {
				t.Fatalf("first capture/Len/Coord allocated %.1f times at %d sectors", allocs, size)
			}
			// Retain all views across the measured calls, as a resumable consumer would.
			for _, view := range views {
				if view.Len() != size {
					t.Fatal("retained view changed during other captures")
				}
			}
		})
	}
}
