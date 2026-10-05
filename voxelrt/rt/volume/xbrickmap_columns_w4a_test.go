package volume_test

import (
	"fmt"
	"math/bits"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func w4aSequentialColumns(columns []volume.VoxelColumn, value uint8) *volume.XBrickMap {
	x := volume.NewXBrickMap()
	for _, c := range columns {
		for y := 0; y < c.FilledVoxels; y++ {
			x.SetVoxel(c.X, y, c.Z, value)
		}
	}
	return x
}

func w4aSameContents(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got == nil || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.GetVoxelCount() != want.GetVoxelCount() {
		t.Fatal("column construction changed voxel count or ordered content/sector revisions")
	}
	// Public dense payloads, sector membership, masks and material flags feed
	// CPU collision and GPU publication; independent map IDs are excluded.
	if !reflect.DeepEqual(got.Sectors, want.Sectors) {
		t.Fatal("column construction changed authoritative cells, masks, material flags or auxiliary ownership")
	}
	gm, gx := got.ComputeAABB()
	wm, wx := want.ComputeAABB()
	if gm != wm || gx != wx {
		t.Fatalf("column bounds %v..%v != sequential %v..%v", gm, gx, wm, wx)
	}
}

func w4aSameEditableMap(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	w4aSameContents(t, got, want)
	if got.StructureDirty != want.StructureDirty || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) || !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) {
		t.Fatal("column construction changed exact sector/normal-halo upload dirtiness")
	}
}

func TestW4aColumnsSignedBoundariesAndHeights(t *testing.T) {
	positions := []int{-33, -32, -8, -1, 0, 7, 8, 31, 32}
	heights := []int{-3, 0, 1, 2, 7, 8, 9, 31, 32, 33, 65, 97}
	for _, value := range []uint8{0, 1, 255} {
		for _, height := range heights {
			t.Run(fmt.Sprintf("value%d/height%d", value, height), func(t *testing.T) {
				var columns []volume.VoxelColumn
				for i, x := range positions {
					columns = append(columns, volume.VoxelColumn{X: x, Z: positions[len(positions)-1-i], FilledVoxels: height})
				}
				original := slices.Clone(columns)
				got := volume.BuildXBrickMapColumns(slices.Values(columns), value)
				w4aSameEditableMap(t, got, w4aSequentialColumns(columns, value))
				if !reflect.DeepEqual(columns, original) {
					t.Fatal("builder mutated source columns")
				}
			})
		}
	}
}

func TestW4aColumnsConsumeOnceIncludingZeroValue(t *testing.T) {
	columns := []volume.VoxelColumn{{X: -1, Z: 8, FilledVoxels: 9}, {FilledVoxels: -1}, {FilledVoxels: 0}}
	for _, value := range []uint8{0, 255} {
		calls, yielded := 0, 0
		got := volume.BuildXBrickMapColumns(func(yield func(volume.VoxelColumn) bool) {
			calls++
			for _, c := range columns {
				yielded++
				if !yield(c) {
					return
				}
			}
		}, value)
		if calls != 1 || yielded != len(columns) {
			t.Fatalf("value %d: synchronous input consumption calls=%d columns=%d", value, calls, yielded)
		}
		w4aSameEditableMap(t, got, w4aSequentialColumns(columns, value))
		w4aSameEditableMap(t, volume.BuildXBrickMapColumns(nil, value), volume.NewXBrickMap())
	}
}

func TestW4aColumnsOrderedDuplicateExtensions(t *testing.T) {
	columns := []volume.VoxelColumn{
		{X: -1, Z: -32, FilledVoxels: 33},
		{X: 32, Z: 0, FilledVoxels: 96},
		{X: -1, Z: -32, FilledVoxels: 2}, // Shorter prefix cannot remove cells.
		{X: -1, Z: -32, FilledVoxels: 65},
		{X: 0, Z: 0, FilledVoxels: 97},
		{X: -1, Z: -32, FilledVoxels: 65}, // Equal prefix is a revision no-op.
		{X: 32, Z: 0, FilledVoxels: 128},
		{X: -1, Z: -32, FilledVoxels: 8},
		{X: -1, Z: -32, FilledVoxels: 100},
		{X: 0, Z: 0, FilledVoxels: -4},
	}
	for _, prefix := range []int{3, 6, len(columns)} {
		got := volume.BuildXBrickMapColumns(slices.Values(columns[:prefix]), 7)
		w4aSameEditableMap(t, got, w4aSequentialColumns(columns[:prefix], 7))
	}
	previous := w4aSequentialColumns(columns[:3], 7).Copy()
	got := volume.BuildXBrickMapColumns(slices.Values(columns), 7)
	changed := got.CopyChangedSectors(previous, previous.Revision)
	w4aSameContents(t, changed, w4aSequentialColumns(columns, 7))
	got.SetVoxel(-1, 99, -32, 0)
	if _, value := changed.GetVoxel(-1, 99, -32); value != 7 {
		t.Fatal("editing the built map mutated its changed-sector snapshot")
	}
	if _, value := previous.GetVoxel(-1, 64, -32); value != 0 {
		t.Fatal("changed-sector construction mutated its previous snapshot")
	}
}

func TestW4aColumnsMaterialOccupancyEditsAndOwnership(t *testing.T) {
	var columns []volume.VoxelColumn
	for x := -8; x < 0; x++ {
		for z := -8; z < 0; z++ {
			columns = append(columns, volume.VoxelColumn{X: x, Z: z, FilledVoxels: 9})
		}
	}
	columns = append(columns, volume.VoxelColumn{X: 0, Z: -1, FilledVoxels: 33})
	got, want := volume.BuildXBrickMapColumns(slices.Values(columns), 255), w4aSequentialColumns(columns, 255)
	w4aSameEditableMap(t, got, want)
	sector := got.Sectors[[3]int{-1, 0, -1}]
	for by, expected := range []struct {
		flags  uint32
		voxels int
	}{{volume.BrickFlagSolid, 512}, {volume.BrickFlagUniformMaterial, 64}} {
		brick := sector.GetBrick(3, by, 3)
		if brick == nil || brick.Flags != expected.flags || brick.AtlasOffset != 255 {
			t.Fatalf("brick Y%d did not retain solid/partial uniform material255", by)
		}
		occupied := 0
		for _, word := range brick.DenseOccupancyWords() {
			occupied += bits.OnesCount32(word)
		}
		if occupied != expected.voxels {
			t.Fatalf("brick Y%d dense occupancy %d != %d", by, occupied, expected.voxels)
		}
	}
	// Pretend these fresh maps have been uploaded with authored normal data.
	// A boundary edit must invalidate the same neighboring auxiliary packets.
	for _, x := range []*volume.XBrickMap{got, want} {
		x.ClearDirty()
		for _, s := range x.Sectors {
			for _, b := range s.PackedBricks {
				b.PrecomputedAux = make([]byte, volume.VoxelAuxWordCount*4)
				b.PrecomputedAux[0] = 17
			}
		}
	}
	copy := got.Copy()
	for _, edit := range []volume.VoxelWrite{{X: -1, Y: 7, Z: -1}, {X: -1, Y: 7, Z: -1, Value: 3}, {X: 0, Y: 32, Z: -1}, {X: 0, Y: 33, Z: -1, Value: 255}} {
		got.SetVoxel(edit.X, edit.Y, edit.Z, edit.Value)
		want.SetVoxel(edit.X, edit.Y, edit.Z, edit.Value)
		w4aSameEditableMap(t, got, want)
	}
	if _, value := copy.GetVoxel(-1, 7, -1); value != 255 {
		t.Fatal("built-map edits changed its independent copy")
	}
	copy.Sectors[[3]int{-1, 0, -1}].GetBrick(3, 0, 3).PrecomputedAux[0] = 23
	if got.Sectors[[3]int{-1, 0, -1}].GetBrick(3, 0, 3).PrecomputedAux != nil {
		t.Fatal("normal-halo edit failed to invalidate the built brick auxiliary data")
	}
	copy.SetVoxel(-8, 0, -8, 4)
	if _, value := got.GetVoxel(-8, 0, -8); value != 255 {
		t.Fatal("copy edit changed built geometry")
	}
}
