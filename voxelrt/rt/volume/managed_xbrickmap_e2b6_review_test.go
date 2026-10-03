package volume_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestE2b6CurrentCountsFollowExistingStaleSolidExpansion(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 3)
	brick := source.Sectors[[3]int{}].GetBrick(0, 0, 0)
	brick.Flags = volume.BrickFlagSolid
	brick.AtlasOffset = 9
	owner := volume.NewManagedXBrickMap(source)
	expected := source.Copy()
	e2b6Counts(t, owner, 1, 1)
	owner.SetVoxel(0, 0, 0, 7)
	expected.SetVoxel(0, 0, 0, 7)
	e2b6Counts(t, owner, 1, 512)
	p1cChanges(t, owner, e2b6ExpandedAssignments(false))
	p1cSnapshotParity(t, owner.Snapshot(), expected)
	p1cValue(t, owner, 1, 0, 0, 9)
	owner.SetVoxel(1, 0, 0, 0)
	expected.SetVoxel(1, 0, 0, 0)
	e2b6Counts(t, owner, 1, 511)
	p1cChanges(t, owner, e2b6ExpandedAssignments(true))
	p1cSnapshotParity(t, owner.Snapshot(), expected)
	child := owner.Fork()
	owner.ExposeMutable().SetVoxel(2, 0, 0, 0)
	if _, _, tracked := owner.CurrentGeometryCounts(); tracked {
		t.Fatal("exposure retained sealed counts")
	}
	e2b6Counts(t, child, 1, 511)
	p1cValue(t, child, 2, 0, 0, 9)
	p1cValue(t, source, 0, 0, 0, 3)
	p1cValue(t, source, 1, 0, 0, 0)
}

func e2b6ExpandedAssignments(removed bool) []volume.VoxelWrite {
	result := make([]volume.VoxelWrite, 0, 512)
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				if removed && x == 1 && y == 0 && z == 0 {
					continue
				}
				value := uint8(9)
				if x == 0 && y == 0 && z == 0 {
					value = 7
				}
				result = append(result, volume.VoxelWrite{X: x, Y: y, Z: z, Value: value})
			}
		}
	}
	return result
}

func TestE2b6TrackedHistoryFollowsStaleSolidAtlasExpansion(t *testing.T) {
	source := volume.NewXBrickMap()
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				source.SetVoxel(x, y, z, 3)
			}
		}
	}
	brick := source.Sectors[[3]int{}].GetBrick(0, 0, 0)
	brick.Flags = volume.BrickFlagSolid
	brick.AtlasOffset = 9
	owner := volume.NewManagedXBrickMap(source)
	expected := source.Copy()
	e2b6Counts(t, owner, 1, 512)
	p1cChanges(t, owner, nil)
	owner.SetVoxel(0, 0, 0, 7)
	expected.SetVoxel(0, 0, 0, 7)
	e2b6Counts(t, owner, 1, 512)
	p1cSnapshotParity(t, owner.Snapshot(), expected)
	p1cChanges(t, owner, e2b6ExpandedAssignments(false))
	if n, tracked := owner.TrackedChangeCount(); !tracked || n != 512 {
		t.Fatalf("expanded tracked count=%d,%v", n, tracked)
	}
	p1cValue(t, source, 1, 0, 0, 3)
}
