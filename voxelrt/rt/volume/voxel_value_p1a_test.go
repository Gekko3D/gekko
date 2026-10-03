package volume_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestP1aVoxelValuePreservesRawPublicEdits(t *testing.T) {
	brick := volume.NewBrick()
	brick.SetVoxel(7, 0, 3, 255)
	brick.SetVoxel(0, 7, 5, 4)
	brick.Payload[2][3][7] = 9
	brick.Payload[0][7][5] = 0
	// Public raw edits may leave renderer metadata stale. The cell is authoritative.
	brick.Flags = volume.BrickFlagSolid | volume.BrickFlagUniformMaterial
	brick.AtlasOffset = 17
	brick.OccupancyMask64 = 0
	brick.PrecomputedAux = []byte{1, 2, 3}
	before := *brick
	before.PrecomputedAux = slices.Clone(brick.PrecomputedAux)
	for _, cell := range []struct {
		position [3]int
		value    uint8
	}{{[3]int{7, 0, 3}, 255}, {[3]int{0, 7, 5}, 0}, {[3]int{2, 3, 7}, 9}, {[3]int{0, 0, 0}, 0}} {
		if got := brick.VoxelValue(cell.position[0], cell.position[1], cell.position[2]); got != cell.value {
			t.Fatalf("cell %v = %d, want raw value %d", cell.position, got, cell.value)
		}
	}
	if !reflect.DeepEqual(*brick, before) {
		t.Fatal("point reads must not repair metadata, edit payload or invalidate auxiliary normals")
	}
}

func TestP1aVoxelValueRetainsIndependentCopyAndLiveReadVisibility(t *testing.T) {
	source := volume.NewBrick()
	source.SetVoxel(7, 2, 0, 6)
	source.SetVoxel(0, 3, 7, 2)
	source.PrecomputedAux = []byte{1, 2}
	copy := source.Copy()
	source.Payload[7][2][0] = 255
	copy.Payload[0][3][7] = 0
	if source.VoxelValue(7, 2, 0) != 255 || copy.VoxelValue(7, 2, 0) != 6 || source.VoxelValue(0, 3, 7) != 2 || copy.VoxelValue(0, 3, 7) != 0 {
		t.Fatal("reads must expose current raw cells while copied bricks remain independently editable")
	}
	if !reflect.DeepEqual(source.PrecomputedAux, []byte{1, 2}) || !reflect.DeepEqual(copy.PrecomputedAux, []byte{1, 2}) {
		t.Fatal("reading edited raw cells must retain auxiliary bytes on both independent bricks")
	}
}
