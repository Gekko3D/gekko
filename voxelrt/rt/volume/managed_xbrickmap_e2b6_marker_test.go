package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestE2b6StaleSolidMarkerSurvivesEmptyBrickAndDenseEarlyReturn(t *testing.T) {
	for _, kind := range []string{"allocated_zero", "solid_early_return"} {
		t.Run(kind, func(t *testing.T) {
			source := volume.NewXBrickMap()
			source.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
			brick, _ := source.Sectors[[3]int{}].GetOrCreateBrick(0, 0, 0)
			bricks, voxels := 0, 0
			if kind == "solid_early_return" {
				brick.SetVoxel(0, 0, 0, 3)
				bricks, voxels = 1, 1
			}
			brick.Flags = volume.BrickFlagSolid
			brick.AtlasOffset = 9
			owner := volume.NewManagedXBrickMap(source)
			expected := source.Copy()
			e2b6Counts(t, owner, bricks, voxels)
			if kind == "solid_early_return" {
				owner.SetVoxel(0, 0, 0, 9)
				expected.SetVoxel(0, 0, 0, 9)
				e2b6Counts(t, owner, 1, 1)
				p1cValue(t, owner, 0, 0, 0, 3)
				p1cChanges(t, owner, nil)
				p1cSnapshotParity(t, owner.Snapshot(), expected)
			}
			parent := owner
			owner = owner.Fork()
			owner.SetVoxel(0, 0, 0, 7)
			expected.SetVoxel(0, 0, 0, 7)
			e2b6Counts(t, owner, 1, 512)
			p1cSnapshotParity(t, owner.Snapshot(), expected)
			p1cChanges(t, owner, e2b6ExpandedAssignments(false))
			e2b6Counts(t, parent, bricks, voxels)
			p1cChanges(t, parent, nil)
		})
	}
}
