package volume_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestP5kBuildExactDirtyHaloHistory(t *testing.T) {
	writes := []volume.VoxelWrite{
		{X: -33, Y: -33, Z: -33, Value: 2},
		{X: -32, Y: -32, Z: -32, Value: 3},
		{X: -31, Y: -31, Z: -31, Value: 4},
		{X: -32, Y: -32, Z: -32, Value: 5},
		{X: -32, Y: -32, Z: -32, Value: 5}, // No-op within an existing halo.
		{X: -32, Y: -32, Z: -32}, {X: -31, Y: -31, Z: -31}, // Empty the sector.
		{X: -32, Y: -32, Z: -32, Value: 6}, // Recreate it after its tombstone.
		{X: -8, Y: -1, Z: -8, Value: 7},
		{X: -7, Y: 0, Z: -7, Value: 8},
		{X: 80, Y: 80, Z: 80, Value: 9}, {X: 80, Y: 80, Z: 80},
		{X: 80, Y: 80, Z: 80}, {X: 160, Y: 160, Z: 160}, // Empty no-ops.
	}
	got := volume.BuildXBrickMap(slices.Values(writes))
	want := p5fSequential(writes)
	if !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) {
		t.Fatal("constructor changed exact normal-halo or sector dirty history")
	}
	if got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) {
		t.Fatal("constructor changed revisions across no-ops, sector removal or recreation")
	}
	for _, w := range writes {
		_, gv := got.GetVoxel(w.X, w.Y, w.Z)
		_, wv := want.GetVoxel(w.X, w.Y, w.Z)
		if gv != wv {
			t.Fatalf("voxel (%d,%d,%d) = %d, want %d", w.X, w.Y, w.Z, gv, wv)
		}
	}
}
