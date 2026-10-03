package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestE2b5ManagedConstructorRestoresOriginalBaseChanges(t *testing.T) {
	base := p1cFixture()
	current := base.Copy()
	current.SetVoxel(-33, 0, 0, 0)
	current.SetVoxel(40, 0, 0, 7)
	current.SetVoxel(8, 1, -8, 9)
	current.ComputeAABB()
	for _, sector := range current.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{7, 8, 9}
		}
	}
	current.SectorRevisions[[3]int{100, 0, 0}] = current.Revision
	owner := volume.NewManagedXBrickMapWithBase(base, current)
	p1cSnapshotParity(t, owner.Snapshot(), current)
	p1cChanges(t, owner, []volume.VoxelWrite{{X: 8, Y: 1, Z: -8, Value: 9}, {X: -33}, {X: 40, Value: 7}})
	if count, ok := owner.TrackedChangeCount(); !ok || count != 3 {
		t.Fatalf("restored count=%d,%v", count, ok)
	}
	inherited := owner.Fork()
	p1cChanges(t, inherited, []volume.VoxelWrite{{X: 8, Y: 1, Z: -8, Value: 9}, {X: -33}, {X: 40, Value: 7}})
	inherited.SetVoxel(40, 0, 0, 5)
	p1cChanges(t, inherited, []volume.VoxelWrite{{X: 8, Y: 1, Z: -8, Value: 9}, {X: -33}})
	for _, sector := range base.Sectors {
		for _, brick := range sector.PackedBricks {
			if len(brick.PrecomputedAux) > 0 {
				brick.PrecomputedAux[0] = 4
			}
		}
	}
	base.SetVoxel(40, 0, 0, 8)
	current.SetVoxel(40, 0, 0, 9)
	for _, sector := range current.Sectors {
		for _, brick := range sector.PackedBricks {
			if len(brick.PrecomputedAux) > 0 {
				brick.PrecomputedAux[0] = 3
			}
		}
	}
	p1cValue(t, owner, 40, 0, 0, 7)
	for _, sector := range owner.Snapshot().Sectors {
		for _, brick := range sector.PackedBricks {
			if brick.PrecomputedAux[0] != 7 {
				t.Fatal("current source aux aliased owner")
			}
		}
	}
	owner.SetVoxel(40, 0, 0, 5)
	owner.SetVoxel(-33, 0, 0, 2)
	owner.SetVoxel(8, 1, -8, 0)
	p1cChanges(t, owner, nil)
	child := owner.Fork()
	child.SetVoxel(-33, 0, 0, 0)
	p1cChanges(t, child, []volume.VoxelWrite{{X: -33}})
	p1cChanges(t, owner, nil)
	child.ExposeMutable().SetVoxel(40, 0, 0, 6)
	if _, ok := child.TrackedChangeCount(); ok {
		t.Fatal("exposure restored tracking")
	}
	p1cValue(t, owner, 40, 0, 0, 5)
	empty := volume.NewManagedXBrickMapWithBase(nil, nil)
	p1cChanges(t, empty, nil)
	one := volume.NewXBrickMap()
	one.SetVoxel(1, 0, 0, 3)
	additions := volume.NewManagedXBrickMapWithBase(nil, one)
	p1cChanges(t, additions, []volume.VoxelWrite{{X: 1, Value: 3}})
	removals := volume.NewManagedXBrickMapWithBase(one, nil)
	p1cChanges(t, removals, []volume.VoxelWrite{{X: 1}})
}
