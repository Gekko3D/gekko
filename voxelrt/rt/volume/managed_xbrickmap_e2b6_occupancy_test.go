package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestE2b6CountsAndHistoryFollowStaleOccupancyBrickDeletion(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 3)
	source.SetVoxel(7, 0, 0, 4)
	brick := source.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if brick.Flags&volume.BrickFlagSolid != 0 {
		t.Fatal("fixture must not exercise solid expansion")
	}
	brick.OccupancyMask64 = 0
	parent := volume.NewManagedXBrickMap(source)
	e2b6Counts(t, parent, 1, 2)
	owner := parent.Fork()
	expected := source.Copy()
	owner.SetVoxel(0, 0, 0, 0)
	expected.SetVoxel(0, 0, 0, 0)
	p1cSnapshotParity(t, owner.Snapshot(), expected)
	p1cValue(t, owner, 0, 0, 0, 0)
	p1cValue(t, owner, 7, 0, 0, 0)
	e2b6Counts(t, owner, 0, 0)
	p1cChanges(t, owner, []volume.VoxelWrite{{}, {X: 7}})
	e2b6Counts(t, parent, 1, 2)
	p1cChanges(t, parent, nil)
	p1cValue(t, parent, 7, 0, 0, 4)
	p1cValue(t, source, 0, 0, 0, 3)
	p1cValue(t, source, 7, 0, 0, 4)
}
