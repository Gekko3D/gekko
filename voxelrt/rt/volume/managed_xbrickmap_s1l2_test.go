package volume_test

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l2Capture(t *testing.T, owner *volume.ManagedXBrickMap) volume.ManagedGeometryView {
	t.Helper()
	view, ok := owner.CaptureGeometry()
	if !ok {
		t.Fatal("qualified sealed geometry capture unavailable")
	}
	return view
}

func s1l2Geometry(t *testing.T, view volume.ManagedGeometryView, want *volume.XBrickMap) {
	t.Helper()
	keys := make([][3]int, 0, len(want.Sectors))
	for key := range want.Sectors {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b [3]int) int {
		for axis := range a {
			if order := cmp.Compare(a[axis], b[axis]); order != 0 {
				return order
			}
		}
		return 0
	})
	if view.Len() != len(keys) {
		t.Fatalf("geometry length = %d, want %d", view.Len(), len(keys))
	}
	for index, key := range keys {
		if coord, ok := view.Coord(index); !ok || coord != key {
			t.Fatalf("geometry coordinate %d = %v, %v; want %v, true", index, coord, ok, key)
		}
		sector, ok := view.CopySector(index)
		if !ok || !reflect.DeepEqual(sector, want.Sectors[key]) {
			t.Fatalf("sector %v lost frozen header, packed order, payload, flags, occupancy, atlas or auxiliary data", key)
		}
	}
	for _, index := range []int{-1, len(keys), int(^uint(0) >> 1)} {
		if coord, ok := view.Coord(index); ok || coord != [3]int{} {
			t.Fatalf("invalid geometry coordinate %d = %v, %v", index, coord, ok)
		}
		if sector, ok := view.CopySector(index); ok || sector != nil {
			t.Fatalf("invalid sector copy %d = %v, %v", index, sector, ok)
		}
	}
}

func s1l2Fixture() *volume.XBrickMap {
	source := volume.NewXBrickMap()
	source.SetVoxel(-33, 0, 0, 1)
	source.SetVoxel(-32, 0, 0, 2)
	source.SetVoxel(0, 0, 0, 3)
	source.SetVoxel(8, 0, 0, 5)
	source.SetVoxel(24, 8, 16, 4)
	source.Sectors[[3]int{-2, 0, 0}].GetBrick(3, 0, 0).PrecomputedAux = make([]byte, 0)
	source.Sectors[[3]int{-1, 0, 0}].GetBrick(0, 0, 0).PrecomputedAux = []byte{1, 2, 3}
	source.Sectors[[3]int{0, 0, 0}].GetBrick(0, 0, 0).PrecomputedAux = []byte{9, 8, 7}
	brick := source.Sectors[[3]int{0, 0, 0}].GetBrick(3, 1, 2)
	brick.PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes)
	brick.PrecomputedAux[0], brick.PrecomputedAux[len(brick.PrecomputedAux)-1] = 5, 6
	source.Sectors[[3]int{2, -3, 1}] = volume.NewSector(2, -3, 1)
	return source
}

func TestS1l2EmptyNilAndExposedGeometryViews(t *testing.T) {
	empty := volume.NewXBrickMap()
	var zero volume.ManagedGeometryView
	s1l2Geometry(t, zero, empty)
	var nilOwner *volume.ManagedXBrickMap
	view, ok := nilOwner.CaptureGeometry()
	if ok {
		t.Fatal("nil owner accepted geometry capture")
	}
	s1l2Geometry(t, view, empty)
	for _, owner := range []*volume.ManagedXBrickMap{
		volume.NewManagedXBrickMap(nil), volume.NewManagedXBrickMapWithBase(nil, nil),
	} {
		s1l2Geometry(t, s1l2Capture(t, owner), empty)
		owner.SetVoxel(0, 0, 0, 1)
		populated := s1l2Capture(t, owner)
		populatedWant := volume.NewXBrickMap()
		populatedWant.SetVoxel(0, 0, 0, 1)
		owner.SetVoxel(0, 0, 0, 0)
		drained := s1l2Capture(t, owner)
		s1l2Geometry(t, drained, empty)
		owner.SetVoxel(0, 0, 0, 2)
		s1l2Geometry(t, populated, populatedWant.Copy())
		s1l2Geometry(t, drained, empty)
		populatedWant.SetVoxel(0, 0, 0, 2)
		s1l2Geometry(t, s1l2Capture(t, owner), populatedWant.Copy())
		owner.ExposeMutable()
		view, ok := owner.CaptureGeometry()
		if ok {
			t.Fatal("exposed owner accepted geometry capture")
		}
		s1l2Geometry(t, view, empty)
	}
}

func TestS1l2ConstructorsPreserveExactCurrentGeometryAndDefensiveCopies(t *testing.T) {
	for _, withBase := range []bool{false, true} {
		t.Run(fmt.Sprintf("withBase=%v", withBase), func(t *testing.T) {
			current := s1l2Fixture()
			// These copyable values must not be normalized during qualification.
			current.Sectors[[3]int{-2, 0, 0}].Coords = [3]int{91, -7, 12}
			brick := current.Sectors[[3]int{0, 0, 0}].GetBrick(0, 0, 0)
			brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = 0xffff, 401, 0
			base := volume.NewXBrickMap()
			base.SetVoxel(320, 0, 0, 1)
			// Eligibility follows current geometry, even with unsupported base Aux.
			base.Sectors[[3]int{10, 0, 0}].GetBrick(0, 0, 0).PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
			want := current.Copy()
			var owner *volume.ManagedXBrickMap
			if withBase {
				owner = volume.NewManagedXBrickMapWithBase(base, current)
			} else {
				owner = volume.NewManagedXBrickMap(current)
			}
			view := s1l2Capture(t, owner)
			s1l2Geometry(t, view, want)
			brick.Payload[0][0][0], brick.PrecomputedAux[0] = 99, 99
			clear(current.Sectors)
			clear(base.Sectors)
			s1l2Geometry(t, view, want)
			s1l2Geometry(t, s1l2Capture(t, owner), want)
			// Repeated copies share neither sector headers, pointer slices, inline
			// payload nor nonempty auxiliary bytes with each other or the view.
			for index := 0; index < view.Len(); index++ {
				first, _ := view.CopySector(index)
				second, _ := view.CopySector(index)
				if first == second {
					t.Fatal("sector results alias")
				}
				first.Coords, first.BrickMask64 = [3]int{8, 8, 8}, 0
				for i, b := range first.PackedBricks {
					if b == second.PackedBricks[i] {
						t.Fatal("copied brick results alias")
					}
					b.Payload[0][0][0], b.Flags, b.OccupancyMask64, b.AtlasOffset = 88, 0, 0, 0
					if len(b.PrecomputedAux) > 0 {
						b.PrecomputedAux[0] = 88
					}
					first.PackedBricks[i] = volume.NewBrick()
				}
				key, _ := view.Coord(index)
				if !reflect.DeepEqual(second, want.Sectors[key]) {
					t.Fatal("mutating one copy changed another copy")
				}
			}
			s1l2Geometry(t, view, want)
			s1l2Geometry(t, s1l2Capture(t, owner), want)
		})
	}
}

func TestS1l2QualificationKeepsExactLegacyFallback(t *testing.T) {
	for _, kind := range []string{"extra-packed", "65-packed", "oversized-aux", "empty-borrowed-aux"} {
		for _, withBase := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/withBase=%v", kind, withBase), func(t *testing.T) {
				source := volume.NewXBrickMap()
				source.SetVoxel(0, 0, 0, 1)
				sector := source.Sectors[[3]int{}]
				switch kind {
				case "extra-packed":
					sector.PackedBricks = append(sector.PackedBricks, volume.NewBrick())
				case "65-packed":
					sector.BrickMask64 = ^uint64(0)
					for len(sector.PackedBricks) < 65 {
						sector.PackedBricks = append(sector.PackedBricks, volume.NewBrick())
					}
				case "oversized-aux":
					sector.PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
				case "empty-borrowed-aux":
					sector.PackedBricks[0].PrecomputedAux = make([]byte, 0, 11)
				}
				var owner *volume.ManagedXBrickMap
				if withBase {
					owner = volume.NewManagedXBrickMapWithBase(nil, source)
				} else {
					owner = volume.NewManagedXBrickMap(source)
				}
				for _, candidate := range []*volume.ManagedXBrickMap{owner, owner.Fork()} {
					view, ok := candidate.CaptureGeometry()
					if ok {
						t.Fatal("unsupported geometry accepted capture")
					}
					s1l2Geometry(t, view, volume.NewXBrickMap())
					s1lTopology(t, s1lCapture(t, candidate), [3]int{})
					p1cSnapshotParity(t, candidate.Snapshot(), source)
					p1eParity(t, candidate.CopyChangedSectors(nil, 0), source.Copy())
					if kind == "empty-borrowed-aux" {
						for _, snapshot := range []*volume.XBrickMap{candidate.Snapshot(), candidate.CopyChangedSectors(nil, 0)} {
							aux := snapshot.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux
							if aux == nil || len(aux) != 0 || cap(aux) != 11 {
								t.Fatal("fallback changed borrowed empty auxiliary slice semantics")
							}
						}
					}
				}
				if kind == "oversized-aux" || kind == "empty-borrowed-aux" {
					owner.SetVoxel(0, 0, 0, 2) // Dense writes clear unsupported Aux.
					if _, ok := owner.CaptureGeometry(); ok {
						t.Fatal("ordinary edit restored permanently unqualified capture")
					}
					if _, ok := owner.Fork().CaptureGeometry(); ok {
						t.Fatal("sealed fork lost inherited disqualification")
					}
				}
				// Repairing copyable unsupported data through exposure can qualify
				// a fresh fork, while the original remains unavailable forever.
				raw := owner.ExposeMutable()
				clear(raw.Sectors)
				raw.SetVoxel(32, 0, 0, 2)
				if _, ok := owner.CaptureGeometry(); ok {
					t.Fatal("exposure restored capture availability")
				}
				s1l2Geometry(t, s1l2Capture(t, owner.Fork()), raw.Copy())
			})
		}
	}
	// The exact upper boundary remains eligible, including a full sector.
	source := volume.NewXBrickMap()
	sector := volume.NewSector(0, 0, 0)
	source.Sectors[[3]int{}] = sector
	for i := 0; i < 64; i++ {
		brick, _ := sector.GetOrCreateBrick(i%4, i/4%4, i/16)
		brick.SetVoxel(0, 0, 0, uint8(i+1))
		brick.PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes)
		brick.PrecomputedAux[volume.VoxelAuxRecordBytes-1] = uint8(i)
	}
	s1l2Geometry(t, s1l2Capture(t, volume.NewManagedXBrickMap(source)), source.Copy())
}

func TestS1l2CaptureFreezesExclusiveEditsHaloDeletionAndExposure(t *testing.T) {
	source := s1l2Fixture()
	owner := volume.NewManagedXBrickMap(source)
	want := source.Copy()
	initialWant := source.Copy()
	initial := s1l2Capture(t, owner)
	owner.SetVoxel(-33, 0, 0, 6)
	want.SetVoxel(-33, 0, 0, 6)
	owner.SetVoxel(0, 0, 0, 7) // Target and halo bricks are now already exclusive.
	want.SetVoxel(0, 0, 0, 7)
	previousWant := want.Copy()
	previous := s1l2Capture(t, owner)
	// Mutate immediately after capture, before Fork can establish its own barrier.
	owner.SetVoxel(0, 0, 0, 8)
	want.SetVoxel(0, 0, 0, 8)
	s1l2Geometry(t, previous, previousWant)
	child := owner.Fork()
	childWant := want.Copy()
	owner.SetVoxel(-33, 0, 0, 0)
	want.SetVoxel(-33, 0, 0, 0)
	owner.SetVoxel(-33, 0, 0, 9)
	want.SetVoxel(-33, 0, 0, 9)
	child.SetVoxel(-32, 0, 0, 10)
	childWant.SetVoxel(-32, 0, 0, 10)
	s1l2Geometry(t, s1l2Capture(t, owner), want)
	s1l2Geometry(t, s1l2Capture(t, child), childWant)
	s1l2Geometry(t, previous, previousWant)
	s1l2Geometry(t, initial, initialWant)
	beforeRaw := s1l2Capture(t, owner)
	raw := owner.ExposeMutable()
	raw.Sectors[[3]int{0, 0, 0}].GetBrick(3, 1, 2).Payload[0][0][0] = 99
	raw.Sectors[[3]int{0, 0, 0}].GetBrick(3, 1, 2).PrecomputedAux = []byte{99}
	clear(raw.Sectors)
	owner.SetVoxel(96, 0, 0, 11)
	s1l2Geometry(t, beforeRaw, want)
	s1l2Geometry(t, previous, previousWant)
	s1l2Geometry(t, s1l2Capture(t, child), childWant)
	fresh := owner.Fork()
	freshWant := raw.Copy()
	freshView := s1l2Capture(t, fresh)
	raw.SetVoxel(96, 0, 0, 12)
	fresh.SetVoxel(96, 0, 0, 13)
	s1l2Geometry(t, freshView, freshWant)
}

func TestS1l2OrderedFinalizationAndProducerPanicPrefix(t *testing.T) {
	source := s1l2Fixture()
	owner := volume.NewManagedXBrickMap(source)
	before := s1l2Capture(t, owner)
	beforeWant := source.Copy()
	want := source.Copy()
	writes := []volume.VoxelWrite{{X: -33}, {X: 64, Value: 7}, {X: 65, Value: 8}, {X: 65, Value: 7}}
	for _, panicProducer := range []bool{false, true} {
		marker := &struct{}{}
		func() {
			defer func() {
				if got := recover(); panicProducer && got != marker || !panicProducer && got != nil {
					t.Fatalf("producer panic = %v, panic requested = %v", got, panicProducer)
				}
			}()
			owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
				for _, write := range writes {
					yield(write)
				}
				if panicProducer {
					panic(marker)
				}
			})
		}()
		for _, write := range writes {
			want.SetVoxel(write.X, write.Y, write.Z, write.Value)
		}
		// Geometry publication inside the producer is unsupported.
		s1l2Geometry(t, s1l2Capture(t, owner), want)
		s1l2Geometry(t, before, beforeWant)
		// Make the second producer exercise insertion/removal, not only no-ops.
		writes = []volume.VoxelWrite{{X: 64}, {X: 65}, {X: -33, Value: 4}, {X: -34, Value: 4}}
	}
}

func TestS1l2FrozenReadsDuringIndependentOwnerAndForkEdits(t *testing.T) {
	owner := volume.NewManagedXBrickMap(s1l2Fixture())
	child := owner.Fork()
	owner.SetVoxel(0, 0, 0, 5)
	child.SetVoxel(0, 0, 0, 6)
	ownerWant, childWant := owner.Snapshot(), child.Snapshot()
	ownerView, childView := s1l2Capture(t, owner), s1l2Capture(t, child)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	for _, candidate := range []*volume.ManagedXBrickMap{owner, child} {
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 40; i++ {
				candidate.SetVoxel(0, 0, 0, uint8(i%2+7))
				candidate.SetVoxel(-33, 0, 0, uint8(i%2))
			}
		}()
	}
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 40; i++ {
			s1l2Geometry(t, ownerView, ownerWant)
			s1l2Geometry(t, childView, childWant)
		}
	}()
	close(start)
	wg.Wait()
	s1l2Geometry(t, ownerView, ownerWant)
	s1l2Geometry(t, childView, childWant)
}

func TestS1l2FirstCaptureAndCoordinateReadsAllocateNothing(t *testing.T) {
	for _, size := range []int{1, 1024} {
		for _, withBase := range []bool{false, true} {
			t.Run(fmt.Sprintf("sectors=%d/withBase=%v", size, withBase), func(t *testing.T) {
				const runs = 8
				owners := make([]*volume.ManagedXBrickMap, runs+1)
				views := make([]volume.ManagedGeometryView, runs+1)
				for i := range owners {
					source := volume.NewXBrickMap()
					for x := size - 1; x >= 0; x-- {
						source.Sectors[[3]int{x, 0, 0}] = volume.NewSector(x, 0, 0)
					}
					if withBase {
						owners[i] = volume.NewManagedXBrickMapWithBase(nil, source)
					} else {
						owners[i] = volume.NewManagedXBrickMap(source)
					}
				}
				next, valid := 0, true
				// Fresh owners include the warmup: first-capture copying cannot hide.
				allocs := testing.AllocsPerRun(runs, func() {
					view, ok := owners[next].CaptureGeometry()
					views[next] = view
					next++
					valid = valid && ok && view.Len() == size
					for _, index := range [3]int{0, size / 2, size - 1} {
						coord, present := view.Coord(index)
						valid = valid && present && coord == [3]int{index, 0, 0}
					}
				})
				if !valid || next != runs+1 {
					t.Fatal("first geometry capture or coordinate read incorrect")
				}
				if allocs != 0 {
					t.Fatalf("first capture/Len/Coord allocated %.1f times at %d sectors", allocs, size)
				}
				for _, view := range views {
					if view.Len() != size {
						t.Fatal("retained geometry view changed during other captures")
					}
				}
			})
		}
	}
}
