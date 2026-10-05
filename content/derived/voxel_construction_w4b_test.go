package derived

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestW4bDerivedImportedCleanMaterialParity(t *testing.T) {
	chunk := &content.ImportedWorldChunkDef{Voxels: []content.ImportedWorldVoxelDef{
		{X: 7, Value: 2, MaterialValue: 7}, {X: 7, Value: 0, MaterialValue: 9}, {X: 8, Value: 3},
		{X: -32, Y: -1, Z: -8, Value: 4, MaterialValue: 255}, {X: 7, Value: 5, MaterialValue: 10}, {X: 8, Value: 3},
	}, EmbeddedAux: &content.ImportedWorldChunkAuxDef{Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: bytes.Repeat([]byte{0x5a}, volume.VoxelAuxRecordBytes)}}}}
	original := slices.Clone(chunk.Voxels)
	want := volume.NewXBrickMap()
	for _, v := range chunk.Voxels {
		if v.Value == 0 {
			continue
		}
		value := v.Value
		if v.MaterialValue != 0 {
			value = v.MaterialValue
		}
		want.SetVoxel(v.X, v.Y, v.Z, value)
	}
	got := importedWorldChunkToXBrickMap(chunk)
	if !reflect.DeepEqual(got.Sectors, want.Sectors) || got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.GetVoxelCount() != want.GetVoxelCount() {
		t.Fatal("offline aux input changed filtered material/cell/revision semantics or adopted embedded aux")
	}
	gm, gx := got.ComputeAABB()
	wm, wx := want.ComputeAABB()
	if gm != wm || gx != wx {
		t.Fatal("offline reconstruction changed authoritative bounds")
	}
	if got.StructureDirty || len(got.DirtySectors) != 0 || len(got.DirtyBricks) != 0 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("offline reconstruction changed clean publication state or decoded source")
	}
	second := importedWorldChunkToXBrickMap(chunk)
	got.SetVoxel(7, 0, 0, 0)
	if _, value := second.GetVoxel(7, 0, 0); value != 10 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("offline inputs share editable geometry with another conversion/source")
	}
}
