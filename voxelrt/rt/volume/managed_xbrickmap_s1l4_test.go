package volume_test

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS1l4EmptyAndUnavailableGeometryCharge(t *testing.T) {
	var zero volume.ManagedGeometryView
	if got := zero.RetainedBytes(); got != 0 {
		t.Fatalf("zero view charge = %d, want 0", got)
	}
	var nilOwner *volume.ManagedXBrickMap
	view, ok := nilOwner.CaptureGeometry()
	if ok || view.RetainedBytes() != 0 {
		t.Fatal("nil owner returned an available or charged view")
	}
	base := s1l2Fixture()
	for _, owner := range []*volume.ManagedXBrickMap{
		volume.NewManagedXBrickMap(nil), volume.NewManagedXBrickMapWithBase(base, nil),
	} {
		if got := s1l2Capture(t, owner).RetainedBytes(); got != 0 {
			t.Fatalf("empty current geometry charged owner/base storage: %d", got)
		}
	}
	emptySector := volume.NewXBrickMap()
	emptySector.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
	owner := volume.NewManagedXBrickMap(emptySector)
	if got := s1l2Capture(t, owner).RetainedBytes(); got == 0 {
		t.Fatal("allocated empty sector has no geometry storage charge")
	}
	owner.ExposeMutable()
	view, ok = owner.CaptureGeometry()
	if ok || view.RetainedBytes() != 0 {
		t.Fatal("exposed owner returned an available or charged view")
	}
	unsupported := volume.NewXBrickMap()
	unsupported.SetVoxel(0, 0, 0, 1)
	unsupported.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
	view, ok = volume.NewManagedXBrickMap(unsupported).CaptureGeometry()
	if ok || view.RetainedBytes() != 0 {
		t.Fatal("unqualified owner returned an available or charged view")
	}
}

func TestS1l4ChargeIncludesWholeDenseBricksAndAuxiliaryCapacity(t *testing.T) {
	source := volume.NewXBrickMap()
	source.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
	emptyCharge := s1l2Capture(t, volume.NewManagedXBrickMap(source)).RetainedBytes()
	source.SetVoxel(0, 0, 0, 1)
	oneCharge := s1l2Capture(t, volume.NewManagedXBrickMap(source)).RetainedBytes()
	source.SetVoxel(volume.BrickSize, 0, 0, 2)
	twoCharge := s1l2Capture(t, volume.NewManagedXBrickMap(source)).RetainedBytes()
	// The retained allocation includes the complete public Brick, including its
	// metadata and slice header, even for a one-voxel or compressed payload.
	brickBytes := uint64(unsafe.Sizeof(volume.Brick{}))
	if oneCharge < emptyCharge || oneCharge-emptyCharge < brickBytes ||
		twoCharge < oneCharge || twoCharge-oneCharge < brickBytes {
		t.Fatalf("dense brick storage undercharged: empty=%d, one=%d, two=%d, whole brick=%d", emptyCharge, oneCharge, twoCharge, brickBytes)
	}

	brick := source.Sectors[[3]int{}].PackedBricks[0]
	brick.PrecomputedAux = []byte{7}
	// Copy's append rounding provides a public, valid backing capacity larger
	// than its length; the charge must follow the owned copy, not source capacity.
	wantAuxCapacity := cap(brick.Copy().PrecomputedAux)
	if wantAuxCapacity <= len(brick.PrecomputedAux) {
		t.Fatal("fixture needs copied auxiliary capacity greater than length")
	}
	view := s1l2Capture(t, volume.NewManagedXBrickMap(source))
	if got := view.RetainedBytes(); got != twoCharge+uint64(wantAuxCapacity) {
		t.Fatalf("auxiliary charge = %d, want base %d + backing capacity %d (length %d)", got, twoCharge, wantAuxCapacity, len(brick.PrecomputedAux))
	}
	sector, _ := view.CopySector(0)
	sector.PackedBricks[0].PrecomputedAux[0] = 99
	if got := view.RetainedBytes(); got != twoCharge+uint64(wantAuxCapacity) {
		t.Fatal("mutating a defensive sector copy changed captured charge")
	}
}

func TestS1l4HistoricalChargeAndCurrentTrackedRefresh(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(7, 0, 0, 1)
	source.SetVoxel(8, 0, 0, 2)
	for _, brick := range source.Sectors[[3]int{}].PackedBricks {
		brick.PrecomputedAux = []byte{1, 2, 3}
	}
	owner := volume.NewManagedXBrickMap(source)
	type chargedView struct {
		view  volume.ManagedGeometryView
		bytes uint64
	}
	var history []chargedView
	capture := func(candidate *volume.ManagedXBrickMap) uint64 {
		view := s1l2Capture(t, candidate)
		charge := view.RetainedBytes()
		history = append(history, chargedView{view, charge})
		return charge
	}
	initial := capture(owner)
	ownedCopy, _ := history[0].view.CopySector(0)
	var auxBytes uint64
	for _, brick := range ownedCopy.PackedBricks {
		auxBytes += uint64(cap(brick.PrecomputedAux))
	}
	owner.SetVoxel(7, 0, 0, 3) // Clears target and adjacent brick halo Aux.
	withoutAux := capture(owner)
	if initial < auxBytes || withoutAux != initial-auxBytes {
		t.Fatalf("target/halo Aux release charge = %d, want %d - %d", withoutAux, initial, auxBytes)
	}
	owner.SetVoxel(24, 0, 0, 4)
	added := capture(owner)
	if added < withoutAux || added-withoutAux < uint64(unsafe.Sizeof(volume.Brick{})) {
		t.Fatal("tracked brick insertion did not charge its dense backing")
	}
	owner.SetVoxel(24, 0, 0, 0)
	if got := capture(owner); got != withoutAux {
		t.Fatalf("tracked brick removal charge = %d, want %d", got, withoutAux)
	}
	child := owner.Fork()
	if got := capture(child); got != withoutAux {
		t.Fatal("sealed fork changed current geometry charge")
	}
	child.SetVoxel(64, 0, 0, 5)
	if got := capture(child); got <= withoutAux {
		t.Fatal("fork insertion did not increase its charge")
	}
	if got := capture(owner); got != withoutAux {
		t.Fatal("fork edit changed original current charge")
	}
	marker := &struct{}{}
	func() {
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("producer panic = %v, want marker", got)
			}
		}()
		owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{X: 96, Value: 6})
			panic(marker)
		})
	}()
	if got := capture(owner); got <= withoutAux {
		t.Fatal("applied panic prefix did not update current geometry charge")
	}
	owner.SetVoxel(7, 0, 0, 0)
	owner.SetVoxel(8, 0, 0, 0)
	owner.SetVoxel(96, 0, 0, 0)
	if got := capture(owner); got != 0 {
		t.Fatalf("drained current geometry charge = %d, want 0", got)
	}
	raw := owner.ExposeMutable()
	raw.SetVoxel(128, 0, 0, 7)
	clear(raw.Sectors)
	child.ExposeMutable().SetVoxel(64, 0, 0, 0)
	for i, frozen := range history {
		if got := frozen.view.RetainedBytes(); got != frozen.bytes {
			t.Fatalf("historical view %d charge changed: got %d, want %d", i, got, frozen.bytes)
		}
	}
}

func TestS1l4FreshAndRepeatedCaptureChargeAllocateNothing(t *testing.T) {
	for _, size := range []int{1, 1024} {
		t.Run(fmt.Sprintf("sectors=%d", size), func(t *testing.T) {
			const runs = 8
			owners := make([]*volume.ManagedXBrickMap, runs+1)
			views := make([]volume.ManagedGeometryView, runs+1)
			for i := range owners {
				source := volume.NewXBrickMap()
				for x := 0; x < size; x++ {
					source.Sectors[[3]int{x, 0, 0}] = volume.NewSector(x, 0, 0)
				}
				owners[i] = volume.NewManagedXBrickMap(source)
			}
			next, valid := 0, true
			allocs := testing.AllocsPerRun(runs, func() {
				view, ok := owners[next].CaptureGeometry()
				charge := view.RetainedBytes()
				views[next] = view
				valid = valid && ok && charge > 0
				for range 3 {
					repeated, available := owners[next].CaptureGeometry()
					valid = valid && available && repeated.RetainedBytes() == charge && view.RetainedBytes() == charge
				}
				next++
			})
			if !valid || next != runs+1 {
				t.Fatal("fresh or repeated capture returned an incorrect charge")
			}
			if allocs != 0 {
				t.Fatalf("capture/RetainedBytes allocated %.1f times at %d sectors", allocs, size)
			}
			for _, view := range views {
				if view.RetainedBytes() == 0 {
					t.Fatal("retained charge disappeared after other captures")
				}
			}
		})
	}
}
