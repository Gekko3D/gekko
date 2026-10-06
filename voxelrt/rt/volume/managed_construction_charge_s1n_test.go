package volume_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS1nConstructionPreflightIsAllocationFreeAndBoundsIndependentSnapshots(t *testing.T) {
	empty, ok := volume.PreflightManagedXBrickMap(nil)
	if !ok || empty.OwnerBytes == 0 || empty.SnapshotBytes == 0 || empty.PeakBytes < empty.OwnerBytes {
		t.Fatal("nil source missing empty logical construction bound")
	}
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, 2, 64)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux[0] = 3
	charge, ok := volume.PreflightManagedXBrickMap(source)
	if !ok || charge.OwnerBytes <= empty.OwnerBytes || charge.SnapshotBytes <= empty.SnapshotBytes || charge.PeakBytes <= charge.OwnerBytes {
		t.Fatal("occupied payload/scratch omitted from constructor bound")
	}
	if allocations := testing.AllocsPerRun(10, func() {
		_, valid := volume.PreflightManagedXBrickMap(source)
		if !valid {
			panic("valid source rejected")
		}
	}); allocations != 0 {
		t.Fatalf("preflight allocated before admission: %v", allocations)
	}
	owner := volume.NewManagedXBrickMap(source)
	first, second := owner.Snapshot(), owner.Snapshot()
	frozen, ok := owner.CaptureGeometry()
	if !ok {
		t.Fatal("preflight accepted an unqualified constructor source")
	}
	if charge.OwnerBytes < frozen.RetainedBytes() || charge.SnapshotBytes < frozen.CopyBytes() {
		t.Fatal("logical constructor bound is smaller than sealed retained/copy payload")
	}
	source.SetVoxel(0, 0, 0, 8)
	first.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux[0] = 7
	first.SetVoxel(0, 0, 0, 9)
	if occupied, value := second.GetVoxel(0, 0, 0); !occupied || value != 1 {
		t.Fatal("independent snapshot inherited source/authority mutation")
	}
	if got := second.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux[0]; got != 3 {
		t.Fatal("independent snapshot auxiliary storage aliases")
	}
	if occupied, value := owner.Snapshot().GetVoxel(0, 0, 0); !occupied || value != 1 {
		t.Fatal("constructor source or snapshot escaped into sealed owner")
	}
}

func TestS1nConstructionPreflightCountsPackedOccurrencesAndAuxCapacity(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	base, ok := volume.PreflightManagedXBrickMap(source)
	if !ok {
		t.Fatal("baseline rejected")
	}
	brick := source.Sectors[[3]int{}].PackedBricks[0]
	brick.PrecomputedAux = make([]byte, 1, 128)
	aux, ok := volume.PreflightManagedXBrickMap(source)
	if !ok || aux.OwnerBytes-base.OwnerBytes < 128 || aux.SnapshotBytes-base.SnapshotBytes < 128 {
		t.Fatal("retained auxiliary capacity omitted from bound")
	}
	source.SetVoxel(64, 0, 0, 1)
	source.Sectors[[3]int{2, 0, 0}].PackedBricks[0] = brick
	repeated, ok := volume.PreflightManagedXBrickMap(source)
	if !ok || repeated.OwnerBytes <= aux.OwnerBytes || repeated.SnapshotBytes-aux.SnapshotBytes < 128 {
		t.Fatal("aliased packed occurrence incorrectly charged as shared constructor storage")
	}
}

func TestS1nConstructionPreflightRefusesUnsupportedStorageWithoutMutation(t *testing.T) {
	for _, kind := range []string{"gpu-first", "nil-sector", "nil-brick", "mask-mismatch", "aux-overflow", "empty-retained-aux"} {
		t.Run(kind, func(t *testing.T) {
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			sector := source.Sectors[[3]int{}]
			switch kind {
			case "gpu-first":
				source.GPUEditMode = true
			case "nil-sector":
				source.Sectors[[3]int{}] = nil
			case "nil-brick":
				sector.PackedBricks[0] = nil
			case "mask-mismatch":
				sector.BrickMask64 = 0
			case "aux-overflow":
				sector.PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
			case "empty-retained-aux":
				sector.PackedBricks[0].PrecomputedAux = make([]byte, 0, 16)
			}
			revision, gpuMode, mask := source.Revision, source.GPUEditMode, sector.BrickMask64
			packed := append([]*volume.Brick(nil), sector.PackedBricks...)
			var auxiliary []byte
			var length, capacity int
			if packed[0] != nil {
				auxiliary = append([]byte(nil), packed[0].PrecomputedAux...)
				length, capacity = len(packed[0].PrecomputedAux), cap(packed[0].PrecomputedAux)
			}
			if _, accepted := volume.PreflightManagedXBrickMap(source); accepted {
				t.Fatal("unsupported storage accepted")
			}
			if source.Revision != revision || source.GPUEditMode != gpuMode || sector.BrickMask64 != mask || !reflect.DeepEqual(sector.PackedBricks, packed) {
				t.Fatal("rejected preflight changed source")
			}
			if packed[0] != nil && (len(packed[0].PrecomputedAux) != length || cap(packed[0].PrecomputedAux) != capacity || !reflect.DeepEqual(packed[0].PrecomputedAux, auxiliary) && length != 0) {
				t.Fatal("rejected preflight changed auxiliary storage")
			}
		})
	}
}
