package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestC1aRuntimeLoaderReusesCompiledRawAndEffectiveMaterials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compiled.gkchunk")
	chunk := &content.ImportedWorldChunkDef{WorldID: "compiled", ChunkSize: 16, VoxelResolution: 0.25, Voxels: []content.ImportedWorldVoxelDef{{X: 0, Value: 3}, {X: 8, Y: 1, Z: 2, Value: 4, MaterialValue: 255}}}
	if err := content.SaveImportedWorldChunkWithOptions(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: "brick_zstd_binary_v1"}); err != nil {
		t.Fatal(err)
	}
	loader := NewRuntimeContentLoader()
	scope := loader.NewScope()
	t.Cleanup(scope.Close)
	first, err := scope.Loader().LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scope.Loader().LoadImportedWorldChunk(path)
	if err != nil || second != first || first.Voxels[1].Value != 4 || first.Voxels[1].MaterialValue != 255 {
		t.Fatal("compiled dispatch lost existing loader reuse or raw channels")
	}
	geometry := prepareImportedWorldChunkGeometry(first)
	if _, value := geometry.GetVoxel(0, 0, 0); value != 3 {
		t.Fatal("compiled palette fallback changed")
	}
	if _, value := geometry.GetVoxel(8, 1, 2); value != 255 {
		t.Fatal("compiled effective material mapping changed")
	}
	min, max := geometry.ComputeAABB()
	if min != ([3]float32{0, 0, 0}) || max != ([3]float32{9, 2, 3}) {
		t.Fatal("compiled preparation changed voxel lattice bounds")
	}
}
