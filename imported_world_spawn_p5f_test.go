package gekko

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestP5fImportedConstructionPreservesMaterialAndAux(t *testing.T) {
	chunk := &content.ImportedWorldChunkDef{Voxels: []content.ImportedWorldVoxelDef{
		{X: 0, Value: 2, MaterialValue: 7},
		{X: 0, Value: 0, MaterialValue: 9}, // Source zero is skipped, never a deletion.
		{X: 1, Value: 3},
		{X: -32, Value: 4, MaterialValue: 8},
		{X: 1, Value: 5, MaterialValue: 10},
	}}
	original := append([]content.ImportedWorldVoxelDef(nil), chunk.Voxels...)
	x := ImportedWorldChunkToXBrickMap(chunk)
	for position, value := range map[int]uint8{0: 7, 1: 10, -32: 8} {
		if _, got := x.GetVoxel(position, 0, 0); got != value {
			t.Fatalf("material at %d = %d, want %d", position, got, value)
		}
	}
	if x.GetVoxelCount() != 3 || x.Revision != 4 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("import changed ordered source/material semantics or mutated decoded content")
	}
	auxBytes := bytes.Repeat([]byte{0x5a}, volume.VoxelAuxRecordBytes)
	aux := &content.ImportedWorldChunkAuxDef{Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: auxBytes}}}
	ApplyImportedWorldChunkAuxToXBrickMap(x, aux)
	brick := x.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if !bytes.Equal(brick.PrecomputedAux, auxBytes) {
		t.Fatal("newly built brick must accept existing normal auxiliary records")
	}
	auxBytes[0] = 0
	if brick.PrecomputedAux[0] != 0x5a {
		t.Fatal("normal auxiliary payload aliases decoded source")
	}
	second := ImportedWorldChunkToXBrickMap(chunk)
	x.SetVoxel(0, 0, 0, 0)
	if _, value := second.GetVoxel(0, 0, 0); value != 7 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("fresh imported maps must isolate edits and decoded content")
	}
	if ImportedWorldChunkToXBrickMap(nil).GetVoxelCount() != 0 {
		t.Fatal("nil imported chunk must produce an editable empty map")
	}
}
