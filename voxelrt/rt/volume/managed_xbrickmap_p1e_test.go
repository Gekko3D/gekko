package volume_test

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func p1eParity(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got == nil || got.ID == want.ID || got.GPUEditMode || got.StructureDirty || len(got.DirtySectors) != 0 || len(got.DirtyBricks) != 0 {
		t.Fatal("incremental snapshot must be clean with a fresh identity")
	}
	if !reflect.DeepEqual(got.Sectors, want.Sectors) || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.CachedMin != want.CachedMin || got.CachedMax != want.CachedMax || got.AABBDirty != want.AABBDirty {
		t.Fatal("incremental snapshot lost dense payload/auxiliary/bounds/revision parity")
	}
}

func TestP1eNilPreviousNoOpAndImmutableReuse(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	previous := owner.CopyChangedSectors(nil, 0)
	p1eParity(t, previous, owner.Snapshot())
	owner.ApplyVoxelWrites(nil)
	owner.SetVoxel(40, 0, 0, 5)
	next := owner.CopyChangedSectors(previous, previous.Revision)
	p1eParity(t, next, owner.Snapshot())
	for key, sector := range previous.Sectors {
		if next.Sectors[key] != sector {
			t.Fatal("no-op recopied untouched immutable sector")
		}
	}
	owner.SetVoxel(40, 0, 0, 8)
	edited := owner.CopyChangedSectors(next, next.Revision)
	p1eParity(t, edited, owner.Snapshot())
	if edited.Sectors[[3]int{1, 0, 0}] == next.Sectors[[3]int{1, 0, 0}] || edited.Sectors[[3]int{-2, 0, 0}] != next.Sectors[[3]int{-2, 0, 0}] {
		t.Fatal("incremental publication did not copy edited sector and reuse distant sector")
	}
	owner.SetVoxel(40, 0, 0, 9)
	p1cValue(t, previous, 40, 0, 0, 5)
	p1cValue(t, next, 40, 0, 0, 5)
	p1cValue(t, edited, 40, 0, 0, 8)
	p1cChanges(t, owner, []volume.VoxelWrite{{X: 40, Value: 9}})
}

func TestP1eNegativeSeamAuxiliaryHaloAndForkHistory(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	previous := owner.CopyChangedSectors(nil, 0)
	// Editing -33 invalidates auxiliary data at -32 across the negative sector
	// seam without changing that neighboring sector's dense revision.
	neighbor := [3]int{-1, 0, 0}
	owner.SetVoxel(-33, 0, 0, 7)
	fork := owner.Fork()
	next := fork.CopyChangedSectors(previous, previous.Revision)
	p1eParity(t, next, fork.Snapshot())
	if next.SectorRevisions[neighbor] != previous.SectorRevisions[neighbor] {
		t.Fatal("fixture no longer exercises auxiliary-only halo invalidation")
	}
	if next.Sectors[neighbor] == previous.Sectors[neighbor] || next.Sectors[neighbor].GetBrick(0, 0, 0).PrecomputedAux != nil {
		t.Fatal("fork failed to inherit negative-seam halo history")
	}
	if previous.Sectors[neighbor].GetBrick(0, 0, 0).PrecomputedAux[0] != 1 {
		t.Fatal("halo edit mutated previous immutable snapshot")
	}
	if next.Sectors[[3]int{1, 0, 0}] != previous.Sectors[[3]int{1, 0, 0}] {
		t.Fatal("normal halo copied distant untouched sector")
	}
	owner.SetVoxel(40, 0, 0, 6)
	childNoOp := fork.CopyChangedSectors(next, next.Revision)
	p1eParity(t, childNoOp, fork.Snapshot())
	if childNoOp.Sectors[[3]int{1, 0, 0}] != next.Sectors[[3]int{1, 0, 0}] {
		t.Fatal("parent edit changed child's independent publication history")
	}
	p1cValue(t, childNoOp, 40, 0, 0, 5)
	fork.SetVoxel(40, 0, 0, 8)
	forkNext := fork.CopyChangedSectors(childNoOp, childNoOp.Revision)
	p1eParity(t, forkNext, fork.Snapshot())
	p1cValue(t, next, 40, 0, 0, 5)
	p1cValue(t, owner, 40, 0, 0, 6)
	p1cChanges(t, fork, []volume.VoxelWrite{{X: -33, Value: 7}, {X: 40, Value: 8}})
	// Snapshot retains its independent mutable contract even though incremental
	// snapshots deliberately share immutable storage with each other.
	independent := fork.Snapshot()
	independent.SetVoxel(40, 0, 0, 9)
	p1cValue(t, forkNext, 40, 0, 0, 8)
	p1cValue(t, fork, 40, 0, 0, 8)
}

func TestP1eDeletionReinsertionTombstonesAndRevert(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	previous := owner.CopyChangedSectors(nil, 0)
	owner.SetVoxel(40, 0, 0, 0)
	removed := owner.CopyChangedSectors(previous, previous.Revision)
	p1eParity(t, removed, owner.Snapshot())
	if removed.Sectors[[3]int{1, 0, 0}] != nil || removed.SectorRevisions[[3]int{1, 0, 0}] <= previous.Revision {
		t.Fatal("removed sector lost revision tombstone")
	}
	owner.SetVoxel(40, 0, 0, 5)
	reinserted := owner.CopyChangedSectors(removed, removed.Revision)
	p1eParity(t, reinserted, owner.Snapshot())
	p1cValue(t, removed, 40, 0, 0, 0)
	p1cValue(t, previous, 40, 0, 0, 5)
	p1cValue(t, reinserted, 40, 0, 0, 5)
	p1cChanges(t, owner, nil)
}

func TestP1ePanicPrefixSnapshotAndTracking(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	previous := owner.CopyChangedSectors(nil, 0)
	marker := &struct{}{}
	func() {
		defer func() {
			if recover() != marker {
				t.Fatal("producer panic did not propagate unchanged")
			}
		}()
		owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{X: -33, Value: 7})
			yield(volume.VoxelWrite{X: 40, Value: 0})
			panic(marker)
		})
	}()
	next := owner.CopyChangedSectors(previous, previous.Revision)
	p1eParity(t, next, owner.Snapshot())
	p1cChanges(t, owner, []volume.VoxelWrite{{X: -33, Value: 7}, {X: 40}})
	p1cValue(t, previous, -33, 0, 0, 2)
	p1cValue(t, next, -33, 0, 0, 7)
}

func TestP1eExposedSnapshotsAlwaysCopyUnnotifiedRawData(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	previous := owner.CopyChangedSectors(nil, 0)
	raw := owner.ExposeMutable()
	raw.Sectors[[3]int{1, 0, 0}].GetBrick(1, 0, 0).Payload[0][0][0] = 9
	next := owner.CopyChangedSectors(previous, previous.Revision)
	p1eParity(t, next, owner.Snapshot())
	for key, sector := range next.Sectors {
		if sector == previous.Sectors[key] || sector == raw.Sectors[key] {
			t.Fatal("exposed owner reused potentially stale or mutable sector")
		}
	}
	p1cValue(t, previous, 40, 0, 0, 5)
	p1cValue(t, next, 40, 0, 0, 9)
	raw.SetVoxel(40, 0, 0, 8)
	p1cValue(t, next, 40, 0, 0, 9)
	if _, tracked := owner.TrackedChanges(); tracked {
		t.Fatal("snapshot restored exposed owner's tracking")
	}
}
