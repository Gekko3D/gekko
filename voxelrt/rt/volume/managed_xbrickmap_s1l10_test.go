package volume_test

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l10Sector(t *testing.T, owner *volume.ManagedXBrickMap, key [3]int) volume.ManagedSectorView {
	t.Helper()
	view, ok := owner.CaptureSector(key)
	if !ok || view.Coord() != key {
		t.Fatalf("sector %v unavailable or wrong coordinate", key)
	}
	return view
}

func s1l10CheckSector(t *testing.T, view volume.ManagedSectorView, key [3]int, want *volume.Sector) {
	t.Helper()
	if view.Coord() != key || view.Present() != (want != nil) {
		t.Fatal("coordinate/presence mismatch")
	}
	got, ok := view.CopySector()
	if ok != (want != nil) || !reflect.DeepEqual(got, want) {
		t.Fatal("sector copy lost frozen public data")
	}
	if want == nil {
		if view.RetainedBytes() != 0 || view.CopyBytes() != 0 {
			t.Fatal("absent sector charged backing")
		}
		return
	}
	if view.RetainedBytes() == 0 || view.CopyBytes() != s1l5SectorBytes(want) || s1l5SectorBytes(got) != view.CopyBytes() {
		t.Fatal("sector charge disagrees with public copy domain")
	}
	if cap(got.PackedBricks) != len(got.PackedBricks) {
		t.Fatal("copy packed backing not exact length")
	}
	for i, brick := range got.PackedBricks {
		if cap(brick.PrecomputedAux) != cap(want.PackedBricks[i].PrecomputedAux) {
			t.Fatal("auxiliary capacity changed")
		}
	}
}

func TestS1l10VolumeZeroNilAbsentEmptyAndQualification(t *testing.T) {
	s1l10CheckSector(t, volume.ManagedSectorView{}, [3]int{}, nil)
	key := [3]int{-7, 2, 9}
	var nilOwner *volume.ManagedXBrickMap
	for _, owner := range []*volume.ManagedXBrickMap{nilOwner, volume.NewManagedXBrickMap(nil)} {
		view, ok := owner.CaptureSector(key)
		if owner == nil {
			if ok {
				t.Fatal("nil owner qualified")
			}
			s1l10CheckSector(t, view, [3]int{}, nil)
		} else {
			if !ok {
				t.Fatal("empty sealed owner refused absence")
			}
			s1l10CheckSector(t, view, key, nil)
		}
	}
	raw := volume.NewXBrickMap()
	raw.Sectors[key] = volume.NewSector(9, 8, 7)
	owner := volume.NewManagedXBrickMap(raw)
	s1l10CheckSector(t, s1l10Sector(t, owner, key), key, owner.Snapshot().Sectors[key])
	owner.ExposeMutable()
	if view, ok := owner.CaptureSector(key); ok {
		t.Fatal("exposed owner qualified")
	} else {
		s1l10CheckSector(t, view, [3]int{}, nil)
	}
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
			for _, coord := range [][3]int{{}, key} {
				view, ok := candidate.CaptureSector(coord)
				if ok {
					t.Fatal("unqualified owner accepted sector or absence")
				}
				s1l10CheckSector(t, view, [3]int{}, nil)
			}
		})
	}
}

func TestS1l10VolumeCopiesSparseAndFullPackedPublicData(t *testing.T) {
	source := s1l2Fixture()
	key := [3]int{5, -2, 1}
	sector := volume.NewSector(99, -8, 77)
	source.Sectors[key] = sector
	for i := 0; i < 64; i++ {
		brick, _ := sector.GetOrCreateBrick(i%4, i/4%4, i/16)
		brick.Payload[0][0][0] = uint8(i + 1)
		brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = uint32(100+i), uint32(300+i), uint64(i)
		switch i % 4 {
		case 1:
			brick.PrecomputedAux = make([]byte, 0)
		case 2:
			brick.PrecomputedAux = make([]byte, 7, 4096)
		case 3:
			brick.PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes)
		}
		for j := range brick.PrecomputedAux {
			brick.PrecomputedAux[j] = byte(i + j)
		}
	}
	sector.PackedBricks[63] = sector.PackedBricks[1]
	owner := volume.NewManagedXBrickMap(source)
	want := source.Copy()
	for coord, expected := range want.Sectors {
		view := s1l10Sector(t, owner, coord)
		s1l10CheckSector(t, view, coord, expected)
		retained, copied := view.RetainedBytes(), view.CopyBytes()
		first, _ := view.CopySector()
		second, _ := view.CopySector()
		if first == second {
			t.Fatal("sector copies alias")
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
		}
		if len(second.PackedBricks) == 64 && second.PackedBricks[63] == second.PackedBricks[1] {
			t.Fatal("packed output references alias")
		}
		s1l10CheckSector(t, view, coord, expected)
		if retained != view.RetainedBytes() || copied != view.CopyBytes() {
			t.Fatal("copy mutation changed frozen charge")
		}
	}
}

func TestS1l10VolumeRetainedChargeIsOneRecord(t *testing.T) {
	key := [3]int{-2, 3, 4}
	empty := volume.NewXBrickMap()
	empty.Sectors[key] = volume.NewSector(-2, 3, 4)
	baseline := s1l10Sector(t, volume.NewManagedXBrickMap(empty), key).RetainedBytes()
	if baseline == 0 {
		t.Fatal("allocated empty record has no retained charge")
	}
	source := empty.Copy()
	brick, _ := source.Sectors[key].GetOrCreateBrick(0, 0, 0)
	brick.Payload[0][0][0] = 1
	brick.PrecomputedAux = make([]byte, 17)
	owner := volume.NewManagedXBrickMap(source)
	one := s1l10Sector(t, owner, key)
	auxCapacity := cap(owner.Snapshot().Sectors[key].PackedBricks[0].PrecomputedAux)
	want := baseline + uint64(unsafe.Sizeof(volume.Brick{})) + uint64(auxCapacity)
	if one.RetainedBytes() != want {
		t.Fatalf("single record charge=%d want=%d", one.RetainedBytes(), want)
	}
	for x := 0; x < 1024; x++ {
		owner.SetVoxel(x*volume.SectorSize, 0, 0, 2)
	}
	again := s1l10Sector(t, owner, key)
	if again.RetainedBytes() != want {
		t.Fatal("unrelated topology contributed to sector charge")
	}
	whole := s1l2Capture(t, owner)
	var sectors uint64
	for i := 0; i < whole.Len(); i++ {
		coord, _ := whole.Coord(i)
		sectors += s1l10Sector(t, owner, coord).RetainedBytes()
	}
	if whole.RetainedBytes() <= sectors {
		t.Fatal("whole-view index accounting not separate from sector records")
	}
}

func TestS1l10VolumeHistoricalBarrierHaloRemovalForkAndExposure(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(31, 0, 0, 1)
	source.SetVoxel(32, 0, 0, 2)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = []byte{4, 5, 6}
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = []byte{7, 8, 9}
	owner := volume.NewManagedXBrickMap(source)
	type frozen struct {
		view             volume.ManagedSectorView
		key              [3]int
		sector           *volume.Sector
		retained, copied uint64
	}
	var history []frozen
	capture := func(candidate *volume.ManagedXBrickMap, key [3]int) volume.ManagedSectorView {
		view := s1l10Sector(t, candidate, key)
		expected := candidate.Snapshot().Sectors[key]
		s1l10CheckSector(t, view, key, expected)
		history = append(history, frozen{view, key, expected, view.RetainedBytes(), view.CopyBytes()})
		return view
	}
	// CaptureSector alone must establish the target and halo detach barrier.
	capture(owner, [3]int{})
	capture(owner, [3]int{1, 0, 0})
	missing := capture(owner, [3]int{2, 0, 0})
	owner.SetVoxel(31, 0, 0, 3)
	neighbor := capture(owner, [3]int{1, 0, 0})
	sector, _ := neighbor.CopySector()
	if sector.PackedBricks[0].PrecomputedAux != nil {
		t.Fatal("halo auxiliary bytes remained current")
	}
	owner.SetVoxel(31, 0, 0, 1) // Return to construction base.
	capture(owner, [3]int{})
	owner.SetVoxel(31, 0, 0, 0)
	s1l10CheckSector(t, capture(owner, [3]int{}), [3]int{}, nil)
	owner.SetVoxel(64, 0, 0, 4)
	capture(owner, [3]int{2, 0, 0})
	s1l10CheckSector(t, missing, [3]int{2, 0, 0}, nil)
	fork := owner.Fork()
	capture(fork, [3]int{1, 0, 0})
	fork.SetVoxel(32, 0, 0, 5)
	capture(fork, [3]int{1, 0, 0})
	capture(owner, [3]int{1, 0, 0})
	clear(owner.ExposeMutable().Sectors)
	clear(fork.ExposeMutable().Sectors)
	for _, old := range history {
		s1l10CheckSector(t, old.view, old.key, old.sector)
		if old.view.RetainedBytes() != old.retained || old.view.CopyBytes() != old.copied {
			t.Fatal("historical charges changed")
		}
	}
}

func TestS1l10VolumeCaptureAndPreflightAllocateNothing(t *testing.T) {
	for _, size := range []int{2, 1025} {
		t.Run(fmt.Sprintf("sectors=%d", size), func(t *testing.T) {
			source := volume.NewXBrickMap()
			for x := 0; x < size; x++ {
				source.SetVoxel(x*volume.SectorSize, 0, 0, 1)
			}
			owner := volume.NewManagedXBrickMap(source)
			before := owner.Snapshot()
			valid := true
			allocs := testing.AllocsPerRun(100, func() {
				for _, x := range []int{0, size / 2, size - 1, size} {
					coord := [3]int{x, 0, 0}
					view, ok := owner.CaptureSector(coord)
					valid = valid && ok && view.Coord() == coord && view.Present() == (x < size)
					if x < size {
						valid = valid && view.CopyBytes() == s1l5SectorBytes(before.Sectors[coord]) && view.RetainedBytes() > 0
					} else {
						valid = valid && view.CopyBytes() == 0 && view.RetainedBytes() == 0
					}
				}
			})
			if !valid || allocs != 0 {
				t.Fatalf("valid=%v allocations=%g; want true, zero", valid, allocs)
			}
			after := owner.Snapshot()
			if before.Revision != after.Revision || !reflect.DeepEqual(before.SectorRevisions, after.SectorRevisions) || !reflect.DeepEqual(before.DirtySectors, after.DirtySectors) || !reflect.DeepEqual(before.DirtyBricks, after.DirtyBricks) {
				t.Fatal("capture changed publication metadata")
			}
		})
	}
}

func TestS1l10VolumeCaptureSectorIsExclusiveBrickBarrier(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	key := [3]int{}
	owner.SetVoxel(0, 0, 0, 2)
	old := s1l10Sector(t, owner, key)
	want, ok := old.CopySector()
	if !ok {
		t.Fatal("warmed target sector unavailable")
	}
	retained, copied := old.RetainedBytes(), old.CopyBytes()
	// No full capture, snapshot or fork intervenes: this sector capture alone
	// must detach the exclusively owned brick on the next write.
	owner.SetVoxel(0, 0, 0, 3)
	s1l10CheckSector(t, old, key, want)
	if old.RetainedBytes() != retained || old.CopyBytes() != copied {
		t.Fatal("exclusive write changed historical charges")
	}
	current := s1l10Sector(t, owner, key)
	sector, ok := current.CopySector()
	if !ok || sector.GetBrick(0, 0, 0).Payload[0][0][0] != 3 {
		t.Fatal("current sector missed same-brick write")
	}
	if want.GetBrick(0, 0, 0).Payload[0][0][0] != 2 {
		t.Fatal("historical sector missed warmed write")
	}
}
