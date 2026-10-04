package volume_test

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// The admission domain is the independently mutable public CopySector output,
// including frozen auxiliary capacity, but excluding retained input records,
// allocator overhead, map metadata and GPU storage.
func s1l5SectorBytes(sector *volume.Sector) uint64 {
	charge := uint64(unsafe.Sizeof(volume.Sector{})) + uint64(len(sector.PackedBricks))*uint64(unsafe.Sizeof((*volume.Brick)(nil)))
	for _, brick := range sector.PackedBricks {
		charge += uint64(unsafe.Sizeof(volume.Brick{})) + uint64(cap(brick.PrecomputedAux))
	}
	return charge
}

func s1l5Check(t *testing.T, view volume.ManagedGeometryView, want *volume.XBrickMap) uint64 {
	t.Helper()
	s1l2Geometry(t, view, want)
	var total uint64
	for index := 0; index < view.Len(); index++ {
		key, _ := view.Coord(index)
		// Preflight precedes allocation and uses expected geometry as its oracle.
		charge, ok := view.CopySectorBytes(index)
		expected := s1l5SectorBytes(want.Sectors[key])
		if !ok || charge != expected {
			t.Fatalf("sector %v preflight = %d, %v; want %d, true", key, charge, ok, expected)
		}
		sector, _ := view.CopySector(index)
		if cap(sector.PackedBricks) != len(sector.PackedBricks) {
			t.Fatalf("sector %v packed capacity = %d, want %d", key, cap(sector.PackedBricks), len(sector.PackedBricks))
		}
		for i, brick := range sector.PackedBricks {
			wantCapacity := cap(want.Sectors[key].PackedBricks[i].PrecomputedAux)
			if cap(brick.PrecomputedAux) != wantCapacity {
				t.Fatalf("sector %v brick %d aux capacity = %d, want frozen %d", key, i, cap(brick.PrecomputedAux), wantCapacity)
			}
		}
		if s1l5SectorBytes(sector) != charge {
			t.Fatalf("sector %v copy disagrees with preflight", key)
		}
		total += charge
	}
	if got := view.CopyBytes(); got != total {
		t.Fatalf("total copy preflight = %d, want %d", got, total)
	}
	for _, index := range []int{-1, view.Len(), int(^uint(0) >> 1)} {
		if charge, ok := view.CopySectorBytes(index); ok || charge != 0 {
			t.Fatalf("invalid indexed preflight %d = %d, %v", index, charge, ok)
		}
	}
	return total
}

func TestS1l5EmptyAndUnavailableCopyPreflight(t *testing.T) {
	empty := volume.NewXBrickMap()
	var zero volume.ManagedGeometryView
	s1l5Check(t, zero, empty)
	var nilOwner *volume.ManagedXBrickMap
	view, ok := nilOwner.CaptureGeometry()
	if ok {
		t.Fatal("nil owner accepted capture")
	}
	s1l5Check(t, view, empty)
	for _, owner := range []*volume.ManagedXBrickMap{
		volume.NewManagedXBrickMap(nil), volume.NewManagedXBrickMapWithBase(s1l2Fixture(), nil),
	} {
		s1l5Check(t, s1l2Capture(t, owner), empty)
	}
	allocated := volume.NewXBrickMap()
	allocated.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
	owner := volume.NewManagedXBrickMap(allocated)
	if got := s1l5Check(t, s1l2Capture(t, owner), allocated.Copy()); got != uint64(unsafe.Sizeof(volume.Sector{})) {
		t.Fatalf("allocated empty sector charge = %d, want header only", got)
	}
	owner.ExposeMutable()
	view, ok = owner.CaptureGeometry()
	if ok {
		t.Fatal("exposed owner accepted capture")
	}
	s1l5Check(t, view, empty)
	for _, kind := range []string{"oversized-aux", "borrowed-empty-aux", "extra-packed"} {
		t.Run(kind, func(t *testing.T) {
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			sector := source.Sectors[[3]int{}]
			switch kind {
			case "oversized-aux":
				sector.PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
			case "borrowed-empty-aux":
				sector.PackedBricks[0].PrecomputedAux = make([]byte, 0, 11)
			case "extra-packed":
				sector.PackedBricks = append(sector.PackedBricks, volume.NewBrick())
			}
			candidate := volume.NewManagedXBrickMap(source)
			view, ok := candidate.CaptureGeometry()
			if ok {
				t.Fatal("unqualified owner accepted capture")
			}
			s1l5Check(t, view, empty)
			p1cSnapshotParity(t, candidate.Snapshot(), source)
		})
	}
}

func TestS1l5CopyPreflightFormulaExactCapacityAndIndependence(t *testing.T) {
	source := s1l2Fixture() // Signed ordering, sparse bricks, nil/empty/full Aux.
	sector := volume.NewSector(5, -2, 1)
	source.Sectors[[3]int{5, -2, 1}] = sector
	lengths := []int{0, 1, 7, 17, volume.VoxelAuxRecordBytes}
	for i := 0; i < 64; i++ {
		brick, _ := sector.GetOrCreateBrick(i%4, i/4%4, i/16)
		brick.Payload[0][0][0] = uint8(i + 1)
		brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = uint32(100+i), uint32(300+i), uint64(i)
		length := lengths[i%len(lengths)]
		if i%10 != 0 {
			brick.PrecomputedAux = make([]byte, length)
			for j := range brick.PrecomputedAux {
				brick.PrecomputedAux[j] = byte(i + j)
			}
		}
	}
	// Two packed references must each own and charge their output brick.
	sector.PackedBricks[63] = sector.PackedBricks[1]
	// Source spare capacity contracts through the existing constructor Copy;
	// output charge follows that frozen backing, rather than source capacity.
	sector.PackedBricks[2].PrecomputedAux = make([]byte, 7, 4096)
	sector.Coords = [3]int{99, -8, 77} // Preserve the header independently of key.
	want := source.Copy()
	owner := volume.NewManagedXBrickMap(source)
	view := s1l2Capture(t, owner)
	retained := view.RetainedBytes()
	total := s1l5Check(t, view, want)
	for index := 0; index < view.Len(); index++ {
		first, _ := view.CopySector(index)
		second, _ := view.CopySector(index)
		if first == second {
			t.Fatal("sector copies alias")
		}
		if len(first.PackedBricks) == 64 && first.PackedBricks[63] == first.PackedBricks[1] {
			t.Fatal("packed aliases did not receive independent output bricks")
		}
		first.Coords, first.BrickMask64 = [3]int{}, 0
		for i, brick := range first.PackedBricks {
			if brick == second.PackedBricks[i] {
				t.Fatal("brick copies alias")
			}
			brick.Payload[0][0][0], brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = 255, 0, 0, 0
			if len(brick.PrecomputedAux) > 0 {
				brick.PrecomputedAux[0] = 255
			}
			first.PackedBricks[i] = volume.NewBrick()
		}
		key, _ := view.Coord(index)
		if !reflect.DeepEqual(second, want.Sectors[key]) {
			t.Fatal("mutating a copy changed a second output")
		}
	}
	if got := s1l5Check(t, view, want); got != total || view.RetainedBytes() != retained {
		t.Fatal("output mutation changed a frozen input or copy charge")
	}
}

func TestS1l5HistoricalAndCurrentCopyPreflightAcrossTrackedChanges(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(31, 0, 0, 1)
	source.SetVoxel(32, 0, 0, 2)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, 7)
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = make([]byte, 17)
	owner := volume.NewManagedXBrickMap(source)
	want := source.Copy()
	type frozen struct {
		view           volume.ManagedGeometryView
		want           *volume.XBrickMap
		copy, retained uint64
	}
	var history []frozen
	capture := func(candidate *volume.ManagedXBrickMap, expected *volume.XBrickMap) uint64 {
		view := s1l2Capture(t, candidate)
		charge := s1l5Check(t, view, expected)
		history = append(history, frozen{view, expected.Copy(), charge, view.RetainedBytes()})
		return charge
	}
	initial := capture(owner, want)
	auxCharge := uint64(cap(want.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux) + cap(want.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux))
	owner.SetVoxel(31, 0, 0, 3)
	want.SetVoxel(31, 0, 0, 3)
	if got := capture(owner, want); got != initial-auxCharge {
		t.Fatalf("target and cross-sector normal-halo Aux release = %d, want %d", got, initial-auxCharge)
	}
	for _, write := range []volume.VoxelWrite{{X: 64, Value: 4}, {X: 8, Value: 5}, {X: 8}, {X: 64}} {
		owner.SetVoxel(write.X, write.Y, write.Z, write.Value)
		want.SetVoxel(write.X, write.Y, write.Z, write.Value)
		capture(owner, want)
	}
	child := owner.Fork()
	childWant := want.Copy()
	capture(child, childWant)
	child.SetVoxel(-33, 0, 0, 6)
	childWant.SetVoxel(-33, 0, 0, 6)
	capture(child, childWant)
	capture(owner, want)
	marker := &struct{}{}
	func() {
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("producer panic = %v, want marker", got)
			}
		}()
		owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{X: 96, Value: 7})
			panic(marker)
		})
	}()
	want.SetVoxel(96, 0, 0, 7)
	capture(owner, want)
	owner.SetVoxel(96, 0, 0, 0)
	want.SetVoxel(96, 0, 0, 0)
	capture(owner, want)
	owner.SetVoxel(31, 0, 0, 0)
	want.SetVoxel(31, 0, 0, 0)
	capture(owner, want)
	owner.SetVoxel(32, 0, 0, 0)
	want.SetVoxel(32, 0, 0, 0)
	if got := capture(owner, want); got != 0 {
		t.Fatal("removing final sector left a copy charge")
	}
	raw := owner.ExposeMutable()
	raw.SetVoxel(96, 0, 0, 9)
	clear(raw.Sectors)
	child.ExposeMutable().SetVoxel(-33, 0, 0, 0)
	for i, old := range history {
		if got := s1l5Check(t, old.view, old.want); got != old.copy || old.view.RetainedBytes() != old.retained {
			t.Fatalf("historical capture %d charge changed", i)
		}
	}
}

func TestS1l5CopyPreflightAllocatesNothingAndPreservesPublication(t *testing.T) {
	for _, size := range []int{1, 1024} {
		t.Run(fmt.Sprintf("sectors=%d", size), func(t *testing.T) {
			source := volume.NewXBrickMap()
			for x := 0; x < size; x++ {
				source.SetVoxel(x*volume.SectorSize, 0, 0, 1)
				source.Sectors[[3]int{x, 0, 0}].PackedBricks[0].PrecomputedAux = make([]byte, 17)
			}
			owner := volume.NewManagedXBrickMap(source)
			view := s1l2Capture(t, owner)
			before := owner.Snapshot()
			one := s1l5SectorBytes(before.Sectors[[3]int{}])
			valid := true
			allocs := testing.AllocsPerRun(100, func() {
				valid = valid && view.CopyBytes() == uint64(size)*one
				for _, index := range [3]int{0, size / 2, size - 1} {
					charge, ok := view.CopySectorBytes(index)
					valid = valid && ok && charge == one
				}
				charge, ok := view.CopySectorBytes(-1)
				valid = valid && !ok && charge == 0
			})
			if !valid || allocs != 0 {
				t.Fatalf("preflight valid=%v allocations=%.1f, want true and zero", valid, allocs)
			}
			after := owner.Snapshot()
			if after.Revision != before.Revision || !reflect.DeepEqual(after.SectorRevisions, before.SectorRevisions) ||
				!reflect.DeepEqual(after.DirtySectors, before.DirtySectors) || !reflect.DeepEqual(after.DirtyBricks, before.DirtyBricks) {
				t.Fatal("read-only preflight changed publication or dirty state")
			}
			s1l5Check(t, s1l2Capture(t, owner), source.Copy())
		})
	}
}
