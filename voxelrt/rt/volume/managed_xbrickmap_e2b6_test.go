package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"iter"
	"testing"
)

func e2b6Counts(t *testing.T, owner *volume.ManagedXBrickMap, bricks, voxels int) {
	t.Helper()
	b, v, tracked := owner.CurrentGeometryCounts()
	if !tracked || b != bricks || v != voxels {
		t.Fatalf("current counts=(%d,%d,%v), want (%d,%d,true)", b, v, tracked, bricks, voxels)
	}
}

func TestE2b6CurrentCountsFollowPayloadAndEdits(t *testing.T) {
	current := volume.NewXBrickMap()
	for _, w := range []volume.VoxelWrite{{X: -9, Value: 2}, {X: -8, Value: 3}, {Value: 4}, {X: 1, Value: 5}} {
		current.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	// Constructor counts read primary payload when acceleration flags disagree.
	stale := current.Copy()
	for _, sector := range stale.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.OccupancyMask64 = 0
			brick.Flags = volume.BrickFlagSolid | volume.BrickFlagUniformMaterial
		}
	}
	empty, _ := stale.Sectors[[3]int{}].GetOrCreateBrick(2, 0, 0)
	empty.OccupancyMask64 = ^uint64(0)
	empty.Flags = volume.BrickFlagSolid
	for _, restored := range []bool{false, true} {
		staleOwner := volume.NewManagedXBrickMap(stale)
		if restored {
			staleOwner = volume.NewManagedXBrickMapWithBase(p1cFixture(), stale)
		}
		e2b6Counts(t, staleOwner, 3, 4)
		owner := volume.NewManagedXBrickMap(current)
		if restored {
			owner = volume.NewManagedXBrickMapWithBase(p1cFixture(), current)
		}
		e2b6Counts(t, owner, 3, 4)
		owner.SetVoxel(0, 0, 0, 7)
		owner.SetVoxel(0, 0, 0, 7)
		e2b6Counts(t, owner, 3, 4)
		owner.SetVoxel(2, 0, 0, 8)
		e2b6Counts(t, owner, 3, 5)
		owner.SetVoxel(-9, 0, 0, 0)
		e2b6Counts(t, owner, 2, 4)
		owner.SetVoxel(-8, 0, 0, 0)
		e2b6Counts(t, owner, 1, 3)
		owner.SetVoxel(-9, 0, 0, 6)
		e2b6Counts(t, owner, 2, 4)
		owner.SetVoxel(-9, 0, 0, 2)
		e2b6Counts(t, owner, 2, 4)
		child := owner.Fork()
		child.SetVoxel(0, 0, 0, 0)
		child.SetVoxel(1, 0, 0, 0)
		child.SetVoxel(2, 0, 0, 0)
		e2b6Counts(t, child, 1, 1)
		e2b6Counts(t, owner, 2, 4)
		child.SetVoxel(-8, 0, 0, 3)
		e2b6Counts(t, child, 2, 2)
		e2b6Counts(t, owner, 2, 4)
		if allocations := testing.AllocsPerRun(100, func() { owner.CurrentGeometryCounts() }); allocations != 0 {
			t.Fatalf("count query allocates: %g", allocations)
		}
		raw := owner.ExposeMutable()
		if _, _, tracked := owner.CurrentGeometryCounts(); tracked {
			t.Fatal("exposed owner reports sealed counts")
		}
		raw.SetVoxel(40, 0, 0, 9)
		resealed := volume.NewManagedXBrickMap(raw)
		e2b6Counts(t, resealed, 3, 5)
		e2b6Counts(t, child, 2, 2)
	}
	e2b6Counts(t, volume.NewManagedXBrickMap(nil), 0, 0)
	e2b6Counts(t, volume.NewManagedXBrickMapWithBase(p1cFixture(), nil), 0, 0)
}

func TestE2b6CurrentCountsPublishOrderedPanicPrefix(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	marker := &struct{}{}
	writes := iter.Seq[volume.VoxelWrite](func(yield func(volume.VoxelWrite) bool) {
		yield(volume.VoxelWrite{X: -9, Value: 2})
		e2b6Counts(t, owner, 1, 1)
		yield(volume.VoxelWrite{X: -8, Value: 3})
		e2b6Counts(t, owner, 2, 2)
		yield(volume.VoxelWrite{X: -9})
		e2b6Counts(t, owner, 1, 1)
		panic(marker)
	})
	func() {
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("panic=%v", got)
			}
		}()
		owner.ApplyVoxelWrites(writes)
	}()
	e2b6Counts(t, owner, 1, 1)
	p1cValue(t, owner, -9, 0, 0, 0)
	p1cValue(t, owner, -8, 0, 0, 3)
}

func TestE2b6CurrentCountsSurviveSolidBrickExpansion(t *testing.T) {
	source := volume.NewXBrickMap()
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				source.SetVoxel(x, y, z, 2)
			}
		}
	}
	brick := source.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if !brick.TryCompress() || brick.Flags&volume.BrickFlagSolid == 0 {
		t.Fatal("fixture is not an actual solid brick")
	}
	owner := volume.NewManagedXBrickMap(source)
	e2b6Counts(t, owner, 1, 512)
	owner.SetVoxel(0, 0, 0, 9)
	e2b6Counts(t, owner, 1, 512)
	owner.SetVoxel(1, 0, 0, 0)
	owner.SetVoxel(2, 0, 0, 0)
	e2b6Counts(t, owner, 1, 510)
	owner.SetVoxel(0, 0, 0, 2)
	e2b6Counts(t, owner, 1, 510)
	owner.SetVoxel(1, 0, 0, 2)
	e2b6Counts(t, owner, 1, 511)
	owner.SetVoxel(2, 0, 0, 2)
	e2b6Counts(t, owner, 1, 512)
	p1cValue(t, owner, 7, 7, 7, 2)
	p1cValue(t, source, 0, 0, 0, 2)
}
