package derived

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestP5gAuxSourceMaterialFiltering(t *testing.T) {
	chunk := &content.ImportedWorldChunkDef{Voxels: []content.ImportedWorldVoxelDef{
		{X: 0, Value: 2, MaterialValue: 7}, {X: 0, Value: 0, MaterialValue: 9},
		{X: 1, Value: 3}, {X: 1, Value: 5, MaterialValue: 10},
	}}
	original := slices.Clone(chunk.Voxels)
	x := importedWorldChunkToXBrickMap(chunk)
	for position, value := range map[int]uint8{0: 7, 1: 10} {
		if _, got := x.GetVoxel(position, 0, 0); got != value {
			t.Fatalf("aux source material at %d = %d, want %d", position, got, value)
		}
	}
	if x.GetVoxelCount() != 2 || x.Revision != 3 || x.StructureDirty || len(x.DirtySectors) != 0 || len(x.DirtyBricks) != 0 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("aux source conversion changed zero filtering, revisions, clean state or decoded input")
	}
}
