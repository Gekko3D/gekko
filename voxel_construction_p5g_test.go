package gekko

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func p5gConstructionParity(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got.GetVoxelCount() != want.GetVoxelCount() || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) {
		t.Fatal("construction changed occupancy or content/sector revisions")
	}
	if !reflect.DeepEqual(VoxelObjectSnapshotFromXBrickMap(got), VoxelObjectSnapshotFromXBrickMap(want)) {
		t.Fatal("construction changed authoritative voxel positions/material bytes")
	}
	gm, gx := got.ComputeAABB()
	wm, wx := want.ComputeAABB()
	if gm != wm || gx != wx {
		t.Fatalf("bounds = %v..%v, want %v..%v", gm, gx, wm, wx)
	}
}

func TestP5gTerrainConstructionParity(t *testing.T) {
	chunk := &content.TerrainChunkDef{SolidValue: 7, Columns: []content.TerrainChunkColumnDef{
		{X: -32, Z: -1, FilledVoxels: 33}, {X: -32, Z: -1, FilledVoxels: 2},
		{X: 0, Z: 32, FilledVoxels: 1}, {X: 1, Z: 1, FilledVoxels: 0}, {X: 2, Z: 2, FilledVoxels: -3},
	}}
	original := slices.Clone(chunk.Columns)
	want := volume.NewXBrickMap()
	for _, column := range chunk.Columns {
		for y := 0; y < column.FilledVoxels; y++ {
			want.SetVoxel(column.X, y, column.Z, chunk.SolidValue)
		}
	}
	got := terrainChunkToXBrickMap(chunk)
	p5gConstructionParity(t, got, want)
	if got.GetVoxelCount() != 34 || !reflect.DeepEqual(chunk.Columns, original) {
		t.Fatal("duplicate/empty columns changed height coverage or source content")
	}
	second := terrainChunkToXBrickMap(chunk)
	got.SetVoxel(-32, 32, -1, 0)
	if _, value := second.GetVoxel(-32, 32, -1); value != 7 {
		t.Fatal("terrain conversions must own independent editable geometry")
	}
	zero := *chunk
	zero.SolidValue = 0
	p5gConstructionParity(t, terrainChunkToXBrickMap(&zero), volume.NewXBrickMap())
	p5gConstructionParity(t, terrainChunkToXBrickMap(nil), volume.NewXBrickMap())
}

func TestP5gSnapshotConstructionParity(t *testing.T) {
	def := &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{
		{X: -32, Y: -1, Z: -8, Value: 3}, {X: -32, Y: -1, Z: -8, Value: 0},
		{X: -32, Y: -1, Z: -8, Value: 3}, {X: -32, Y: -1, Z: -8, Value: 255},
		{X: 32, Y: 7, Z: 0, Value: 8}, {X: 99, Value: 0},
	}}
	original := slices.Clone(def.Voxels)
	want := volume.NewXBrickMap()
	for _, voxel := range def.Voxels {
		if voxel.Value != 0 {
			want.SetVoxel(voxel.X, voxel.Y, voxel.Z, voxel.Value)
		}
	}
	got := XBrickMapFromVoxelObjectSnapshot(def)
	p5gConstructionParity(t, got, want)
	if got.Revision != 3 || !reflect.DeepEqual(def.Voxels, original) {
		t.Fatal("snapshot zero filtering/duplicate writes changed or source was mutated")
	}
	for _, x := range []*volume.XBrickMap{got, XBrickMapFromVoxelObjectSnapshot(nil)} {
		if x.StructureDirty || len(x.DirtySectors) != 0 || len(x.DirtyBricks) != 0 {
			t.Fatal("snapshot reconstruction must clear initial renderer dirtiness, including nil input")
		}
	}
	canonical := VoxelObjectSnapshotFromXBrickMap(got)
	roundtrip := XBrickMapFromVoxelObjectSnapshot(canonical)
	if !reflect.DeepEqual(VoxelObjectSnapshotFromXBrickMap(roundtrip), canonical) {
		t.Fatal("snapshot roundtrip changed authoritative content")
	}
	second := XBrickMapFromVoxelObjectSnapshot(def)
	got.SetVoxel(-32, -1, -8, 0)
	if _, value := second.GetVoxel(-32, -1, -8); value != 255 || !reflect.DeepEqual(def.Voxels, original) {
		t.Fatal("snapshot reconstruction must isolate edits from other instances/source")
	}
}
