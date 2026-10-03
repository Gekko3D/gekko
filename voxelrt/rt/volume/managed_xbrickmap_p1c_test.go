package volume_test

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func p1cFixture() *volume.XBrickMap {
	x := volume.NewXBrickMap()
	for _, w := range []volume.VoxelWrite{{X: -33, Value: 2}, {X: -32, Value: 3}, {X: -1, Value: 4}, {X: 40, Value: 5}, {X: 96, Value: 6}} {
		x.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	x.SetVoxel(96, 0, 0, 0) // Preserve a removed-sector revision tombstone.
	x.ComputeAABB()
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{1, 2, 3}
		}
	}
	return x
}

func p1cSnapshotParity(t *testing.T, got, source *volume.XBrickMap) {
	t.Helper()
	want := source.Copy()
	if got.ID == source.ID || got.GPUEditMode || len(got.DirtySectors) != 0 || len(got.DirtyBricks) != 0 || !got.StructureDirty {
		t.Fatal("snapshot must have fresh ID and Copy upload/GPU state")
	}
	if !reflect.DeepEqual(got.Sectors, want.Sectors) || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.AABBDirty != want.AABBDirty || got.CachedMin != want.CachedMin || got.CachedMax != want.CachedMax {
		t.Fatal("snapshot must preserve payload, auxiliary metadata, bounds, revisions and tombstones exactly")
	}
}

func p1cValue(t *testing.T, x interface {
	GetVoxel(int, int, int) (bool, uint8)
}, gx, gy, gz int, value uint8) {
	t.Helper()
	found, got := x.GetVoxel(gx, gy, gz)
	if got != value || found != (value != 0) {
		t.Fatalf("voxel (%d,%d,%d): got (%v,%d), want (%v,%d)", gx, gy, gz, found, got, value != 0, value)
	}
}

func p1cChanges(t *testing.T, x *volume.ManagedXBrickMap, want []volume.VoxelWrite) {
	t.Helper()
	got, tracked := x.TrackedChanges()
	if !tracked || !slices.Equal(got, want) {
		t.Fatalf("tracked changes: got %v, %v; want %v, true", got, tracked, want)
	}
}

func TestP1cConstructorSnapshotAndDefensiveOwnership(t *testing.T) {
	empty := volume.NewManagedXBrickMap(nil)
	p1cValue(t, empty, 0, 0, 0, 0)
	p1cChanges(t, empty, nil)
	empty.SetVoxel(0, 0, 0, 7)
	p1cValue(t, empty, 0, 0, 0, 7)

	source := p1cFixture()
	source.EnableGPUEditing(struct{}{})
	owner := volume.NewManagedXBrickMap(source)
	first, second := owner.Snapshot(), owner.Snapshot()
	p1cSnapshotParity(t, first, source)
	if first.ID == second.ID {
		t.Fatal("snapshots must have distinct identities")
	}
	source.Sectors[[3]int{-2, 0, 0}].GetBrick(3, 0, 0).Payload[7][0][0] = 9
	source.Sectors[[3]int{-2, 0, 0}].GetBrick(3, 0, 0).PrecomputedAux[0] = 9
	source.SectorRevisions[[3]int{3, 0, 0}] = 99
	first.Sectors[[3]int{-1, 0, 0}].GetBrick(0, 0, 0).Payload[0][0][0] = 9
	first.Sectors[[3]int{-1, 0, 0}].GetBrick(0, 0, 0).PrecomputedAux[0] = 9
	first.SectorRevisions[[3]int{3, 0, 0}] = 99
	owner.SetVoxel(40, 0, 0, 8)
	p1cValue(t, owner, -33, 0, 0, 2)
	p1cValue(t, owner, -32, 0, 0, 3)
	p1cValue(t, second, 40, 0, 0, 5)
	want := p1cFixture()
	want.SetVoxel(40, 0, 0, 8)
	p1cSnapshotParity(t, owner.Snapshot(), want)
}

func TestP1cForkAndNegativeSeamHaloIsolation(t *testing.T) {
	base := p1cFixture()
	owner := volume.NewManagedXBrickMap(base)
	owner.SetVoxel(40, 0, 0, 8)
	fork := owner.Fork()
	fork.SetVoxel(-33, 0, 0, 7)
	owner.SetVoxel(40, 0, 0, 6)
	p1cValue(t, fork, 40, 0, 0, 8)
	owner.SetVoxel(-1, 0, 0, 9)
	wantOwner, wantFork := base.Copy(), base.Copy()
	wantOwner.SetVoxel(40, 0, 0, 8)
	wantOwner.SetVoxel(40, 0, 0, 6)
	wantOwner.SetVoxel(-1, 0, 0, 9)
	wantFork.SetVoxel(40, 0, 0, 8)
	wantFork.SetVoxel(-33, 0, 0, 7)
	p1cSnapshotParity(t, owner.Snapshot(), wantOwner)
	p1cSnapshotParity(t, fork.Snapshot(), wantFork)
	p1cValue(t, base, -33, 0, 0, 2)
	p1cValue(t, base, -1, 0, 0, 4)
	if wantFork.Sectors[[3]int{-1, 0, 0}].GetBrick(0, 0, 0).PrecomputedAux != nil {
		t.Fatal("fixture must exercise halo invalidation across negative sector seam")
	}
	// Reverting an inherited edit uses the original construction base, not fork-time values.
	fork.SetVoxel(40, 0, 0, 5)
	p1cChanges(t, fork, []volume.VoxelWrite{{X: -33, Value: 7}})
	p1cChanges(t, owner, []volume.VoxelWrite{{X: -1, Value: 9}, {X: 40, Value: 6}})
}

func TestP1cChangesRemovalRevertOrderAndReturnedOwnership(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	owner.ApplyVoxelWrites(slices.Values([]volume.VoxelWrite{
		{X: 5, Y: 1, Z: 2, Value: 7}, {X: -32}, {X: 4, Y: -2, Z: -1, Value: 8},
		{X: 3, Y: -2, Z: -1, Value: 9}, {X: 9, Value: 2}, {X: 9},
		{X: 40, Value: 8}, {X: 40, Value: 5}, {X: 5, Y: 1, Z: 2, Value: 6},
	}))
	want := []volume.VoxelWrite{{X: 3, Y: -2, Z: -1, Value: 9}, {X: 4, Y: -2, Z: -1, Value: 8}, {X: -32}, {X: 5, Y: 1, Z: 2, Value: 6}}
	p1cChanges(t, owner, want)
	changes, _ := owner.TrackedChanges()
	changes[0] = volume.VoxelWrite{Value: 99}
	p1cChanges(t, owner, want)
	owner.SetVoxel(-32, 0, 0, 3)
	p1cChanges(t, owner, []volume.VoxelWrite{want[0], want[1], want[3]})
}

func TestP1cExposureAuthorityAndForkBoundary(t *testing.T) {
	owner := volume.NewManagedXBrickMap(p1cFixture())
	owner.SetVoxel(-33, 0, 0, 7)
	before := owner.Fork()
	raw := owner.ExposeMutable()
	if raw != owner.ExposeMutable() {
		t.Fatal("exposure must return stable authoritative pointer")
	}
	if changes, tracked := owner.TrackedChanges(); tracked || changes != nil {
		t.Fatal("exposure must permanently disable tracked changes")
	}
	brick := raw.Sectors[[3]int{-2, 0, 0}].GetBrick(3, 0, 0)
	brick.Payload[7][0][0] = 8 // No revision notification; public payload stays authoritative.
	raw.Sectors[[3]int{-1, 0, 0}].GetBrick(3, 0, 0).PrecomputedAux[0] = 9
	p1cValue(t, owner, -33, 0, 0, 8)
	p1cValue(t, before, -33, 0, 0, 7)
	if before.Snapshot().Sectors[[3]int{-1, 0, 0}].GetBrick(3, 0, 0).PrecomputedAux[0] != 1 {
		t.Fatal("exposure must detach auxiliary bytes even in untouched shared bricks")
	}
	p1cSnapshotParity(t, owner.Snapshot(), raw)
	after := owner.Fork()
	p1cChanges(t, after, nil)
	brick.Payload[7][0][0] = 9
	after.SetVoxel(-33, 0, 0, 6)
	owner.SetVoxel(40, 0, 0, 2)
	if owner.ExposeMutable() != raw {
		t.Fatal("managed writes must not replace exposed map")
	}
	p1cValue(t, raw, 40, 0, 0, 2)
	p1cValue(t, after, 40, 0, 0, 5)
	p1cValue(t, owner, -33, 0, 0, 9)
	p1cChanges(t, after, []volume.VoxelWrite{{X: -33, Value: 6}})
	after.SetVoxel(-33, 0, 0, 8)
	p1cChanges(t, after, nil)
	if changes, tracked := owner.TrackedChanges(); tracked || changes != nil {
		t.Fatal("managed writes must never reseal exposed owner")
	}
}

func TestP1cOrderedWritesPanicPrefixAndNoOpAux(t *testing.T) {
	base := p1cFixture()
	owner, want := volume.NewManagedXBrickMap(base), base.Copy()
	owner.ApplyVoxelWrites(nil)
	owner.ApplyVoxelWrites(slices.Values([]volume.VoxelWrite{{X: -33, Value: 2}, {X: 200}}))
	owner.SetVoxel(-33, 0, 0, 2)
	owner.SetVoxel(200, 0, 0, 0)
	p1cSnapshotParity(t, owner.Snapshot(), want)
	p1cChanges(t, owner, nil)
	calls := 0
	writes := []volume.VoxelWrite{{X: -33}, {X: -32, Value: 8}, {X: -32, Value: 9}, {X: 200, Value: 3}}
	func() {
		defer func() {
			if recover() != "producer panic" {
				t.Fatal("producer panic must propagate unchanged")
			}
		}()
		owner.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
			calls++
			for _, w := range writes {
				want.SetVoxel(w.X, w.Y, w.Z, w.Value)
				if !yield(w) {
					t.Fatal("ordered writes must consume producer")
				}
				p1cValue(t, owner, w.X, w.Y, w.Z, w.Value)
			}
			panic("producer panic")
		})
	}()
	if calls != 1 {
		t.Fatal("producer must execute synchronously once")
	}
	p1cSnapshotParity(t, owner.Snapshot(), want)
	p1cChanges(t, owner, []volume.VoxelWrite{{X: -33}, {X: -32, Value: 9}, {X: 200, Value: 3}})
}

func TestP1cIndependentForkConcurrentEdits(t *testing.T) {
	base := p1cFixture()
	owner := volume.NewManagedXBrickMap(base)
	fork := owner.Fork()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			owner.SetVoxel(-33, 0, 0, uint8(i%2+7))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			fork.SetVoxel(-32, 0, 0, uint8(i%2+9))
		}
	}()
	wg.Wait()
	p1cValue(t, owner, -33, 0, 0, 8)
	p1cValue(t, owner, -32, 0, 0, 3)
	p1cValue(t, fork, -33, 0, 0, 2)
	p1cValue(t, fork, -32, 0, 0, 10)
	p1cSnapshotParity(t, base.Copy(), base)
}

func TestP1cRemoveReinsertForkIsolation(t *testing.T) {
	base := p1cFixture()
	owner := volume.NewManagedXBrickMap(base)
	owner.SetVoxel(-33, 0, 0, 0)
	owner.SetVoxel(-33, 0, 0, 7)
	fork := owner.Fork()
	owner.SetVoxel(-33, 0, 0, 8)
	fork.SetVoxel(-33, 0, 0, 9)
	p1cValue(t, owner, -33, 0, 0, 8)
	p1cValue(t, fork, -33, 0, 0, 9)
	p1cValue(t, base, -33, 0, 0, 2)
	p1cChanges(t, owner, []volume.VoxelWrite{{X: -33, Value: 8}})
	p1cChanges(t, fork, []volume.VoxelWrite{{X: -33, Value: 9}})
	wantOwner, wantFork := base.Copy(), base.Copy()
	for _, x := range []*volume.XBrickMap{wantOwner, wantFork} {
		x.SetVoxel(-33, 0, 0, 0)
		x.SetVoxel(-33, 0, 0, 7)
	}
	wantOwner.SetVoxel(-33, 0, 0, 8)
	wantFork.SetVoxel(-33, 0, 0, 9)
	p1cSnapshotParity(t, owner.Snapshot(), wantOwner)
	p1cSnapshotParity(t, fork.Snapshot(), wantFork)
}

func TestP1cSolidBrickForkPaintAndRemoval(t *testing.T) {
	base := volume.NewXBrickMap()
	for z := 0; z < volume.BrickSize; z++ {
		for y := 0; y < volume.BrickSize; y++ {
			for x := 0; x < volume.BrickSize; x++ {
				base.SetVoxel(x, y, z, 3)
			}
		}
	}
	base.ComputeAABB()
	brick := base.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if brick.Flags&volume.BrickFlagSolid == 0 {
		t.Fatal("fixture must exercise solid brick expansion")
	}
	brick.PrecomputedAux = []byte{1, 2, 3}
	owner := volume.NewManagedXBrickMap(base)
	fork := owner.Fork()
	owner.SetVoxel(1, 1, 1, 7)
	fork.SetVoxel(2, 2, 2, 0)
	wantOwner, wantFork := base.Copy(), base.Copy()
	wantOwner.SetVoxel(1, 1, 1, 7)
	wantFork.SetVoxel(2, 2, 2, 0)
	p1cSnapshotParity(t, owner.Snapshot(), wantOwner)
	p1cSnapshotParity(t, fork.Snapshot(), wantFork)
	p1cValue(t, base, 1, 1, 1, 3)
	p1cValue(t, base, 2, 2, 2, 3)
	if brick.PrecomputedAux[0] != 1 || brick.Flags&volume.BrickFlagSolid == 0 {
		t.Fatal("solid expansion must not alter original brick metadata")
	}
	p1cChanges(t, owner, []volume.VoxelWrite{{X: 1, Y: 1, Z: 1, Value: 7}})
	p1cChanges(t, fork, []volume.VoxelWrite{{X: 2, Y: 2, Z: 2}})
}
