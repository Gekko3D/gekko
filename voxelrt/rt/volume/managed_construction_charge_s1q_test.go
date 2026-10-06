package volume_test

import (
	"maps"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1qPreflightSourceState(source *volume.XBrickMap) *volume.XBrickMap {
	if source == nil {
		return nil
	}
	copy := *source
	copy.Sectors = maps.Clone(source.Sectors)
	copy.DirtySectors = maps.Clone(source.DirtySectors)
	copy.DirtyBricks = maps.Clone(source.DirtyBricks)
	copy.SectorRevisions = maps.Clone(source.SectorRevisions)
	for key, sector := range source.Sectors {
		if sector == nil {
			continue
		}
		cloned := *sector
		cloned.PackedBricks = append([]*volume.Brick(nil), sector.PackedBricks...)
		for i, brick := range sector.PackedBricks {
			if brick != nil {
				cloned.PackedBricks[i] = brick.Copy()
			}
		}
		copy.Sectors[key] = &cloned
	}
	return &copy
}

func TestS1qWithBasePreflightBoundsEmptyAndIndependentCurrentSnapshots(t *testing.T) {
	occupied := volume.NewXBrickMap()
	occupied.SetVoxel(-9, 0, 0, 7)
	occupied.Sectors[[3]int{-1, 0, 0}].PackedBricks[0].PrecomputedAux = []byte{1, 2, 3}
	for _, inputs := range []struct {
		name          string
		base, current *volume.XBrickMap
	}{
		{"nil-both", nil, nil},
		{"empty-both", volume.NewXBrickMap(), volume.NewXBrickMap()},
		{"empty-base", volume.NewXBrickMap(), occupied},
		{"empty-current", occupied, volume.NewXBrickMap()},
		{"nil-base", nil, occupied},
		{"nil-current", occupied, nil},
	} {
		t.Run(inputs.name, func(t *testing.T) {
			beforeBase, beforeCurrent := s1qPreflightSourceState(inputs.base), s1qPreflightSourceState(inputs.current)
			charge, accepted := volume.PreflightManagedXBrickMapWithBase(inputs.base, inputs.current)
			current, valid := volume.PreflightManagedXBrickMap(inputs.current)
			if !accepted || !valid || charge.OwnerBytes == 0 || charge.SnapshotBytes == 0 || charge.PeakBytes < charge.OwnerBytes || charge.SnapshotBytes != current.SnapshotBytes {
				t.Fatal("dual-source preflight lost empty-owner or current-snapshot bound")
			}
			if !reflect.DeepEqual(inputs.base, beforeBase) || !reflect.DeepEqual(inputs.current, beforeCurrent) {
				t.Fatal("preflight mutated source state")
			}
			owner := volume.NewManagedXBrickMapWithBase(inputs.base, inputs.current)
			view, valid := owner.CaptureGeometry()
			if !valid || charge.OwnerBytes < view.RetainedBytes() || charge.SnapshotBytes < view.CopyBytes() {
				t.Fatal("accepted dual-source bound is smaller than sealed storage")
			}
		})
	}
}

func TestS1qWithBasePreflightIncludesDisjointAssignmentAndChangedBrickStorage(t *testing.T) {
	base, current := volume.NewXBrickMap(), volume.NewXBrickMap()
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			for z := 0; z < 8; z++ {
				base.SetVoxel(x, y, z, 1)
				current.SetVoxel(x+64, y, z, 7)
			}
		}
	}
	beforeBase, beforeCurrent := s1qPreflightSourceState(base), s1qPreflightSourceState(current)
	charge, accepted := volume.PreflightManagedXBrickMapWithBase(base, current)
	baseCharge, baseValid := volume.PreflightManagedXBrickMap(base)
	currentCharge, currentValid := volume.PreflightManagedXBrickMap(current)
	if !accepted || !baseValid || !currentValid {
		t.Fatal("supported disjoint inputs refused")
	}
	owner := volume.NewManagedXBrickMapWithBase(base, current)
	changes, tracked := owner.TrackedChanges()
	if !tracked || len(changes) != 1024 {
		t.Fatal("disjoint fixture did not create removal and addition assignments")
	}
	// The public logical minimum covers two independent geometry copies and
	// coordinates/values of every assignment plus both changed brick counters.
	assignmentBytes := uint64(len(changes)) * (uint64(unsafe.Sizeof([3]int{})) + uint64(unsafe.Sizeof(uint8(0))))
	changedBrickBytes := 2 * (uint64(unsafe.Sizeof([6]int{})) + uint64(unsafe.Sizeof(int(0))))
	minimum := baseCharge.SnapshotBytes + currentCharge.SnapshotBytes + assignmentBytes + changedBrickBytes
	if charge.OwnerBytes < minimum || charge.PeakBytes < charge.OwnerBytes || charge.SnapshotBytes != currentCharge.SnapshotBytes {
		t.Fatal("dual-source bound omits original/current geometry or assignment/changed-brick tracking")
	}
	if !reflect.DeepEqual(base, beforeBase) || !reflect.DeepEqual(current, beforeCurrent) {
		t.Fatal("preflight or construction mutated disjoint inputs")
	}
}

func TestS1qWithBasePreflightRefusesUnsupportedEitherOperandWithoutMutation(t *testing.T) {
	for _, operand := range []string{"base", "current"} {
		for _, invalid := range []string{"gpu-edit", "nil-sector", "nil-brick", "mask-mismatch"} {
			t.Run(operand+"/"+invalid, func(t *testing.T) {
				base, current := volume.NewXBrickMap(), volume.NewXBrickMap()
				base.SetVoxel(0, 0, 0, 1)
				current.SetVoxel(64, 0, 0, 7)
				target, key := base, [3]int{}
				if operand == "current" {
					target, key = current, [3]int{2, 0, 0}
				}
				switch invalid {
				case "gpu-edit":
					target.GPUEditMode = true
				case "nil-sector":
					target.Sectors[key] = nil
				case "nil-brick":
					target.Sectors[key].PackedBricks[0] = nil
				case "mask-mismatch":
					target.Sectors[key].BrickMask64 = 0
				}
				beforeBase, beforeCurrent := s1qPreflightSourceState(base), s1qPreflightSourceState(current)
				if _, accepted := volume.PreflightManagedXBrickMapWithBase(base, current); accepted {
					t.Fatal("dual-source preflight accepted unsupported operand")
				}
				if !reflect.DeepEqual(base, beforeBase) || !reflect.DeepEqual(current, beforeCurrent) {
					t.Fatal("refused dual-source preflight mutated an operand")
				}
			})
		}
	}
}
