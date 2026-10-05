package gekko

import (
	"bytes"
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestW4bImportedRouteMaterialAuxAndOwnership(t *testing.T) {
	aux0, aux8 := bytes.Repeat([]byte{0x5a}, volume.VoxelAuxRecordBytes), bytes.Repeat([]byte{0x37}, volume.VoxelAuxRecordBytes)
	chunk := &content.ImportedWorldChunkDef{Voxels: []content.ImportedWorldVoxelDef{
		{X: 7, Value: 2, MaterialValue: 7}, {X: 7, Value: 0, MaterialValue: 9},
		{X: 8, Value: 3}, {X: -32, Y: -1, Z: -8, Value: 4, MaterialValue: 255},
		{X: 7, Value: 5, MaterialValue: 10}, {X: 8, Value: 3}, {X: 31, Y: 31, Z: 31, Value: 6},
	}, EmbeddedAux: &content.ImportedWorldChunkAuxDef{Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: aux0}, {Origin: [3]int{8, 0, 0}, Bytes: aux8}}}}
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
	want.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = bytes.Clone(aux0)
	want.Sectors[[3]int{}].GetBrick(1, 0, 0).PrecomputedAux = bytes.Clone(aux8)
	got := ImportedWorldChunkToXBrickMap(chunk)
	p5gConstructionParity(t, got, want)
	if !reflect.DeepEqual(got.Sectors, want.Sectors) || !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) || got.StructureDirty != want.StructureDirty || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("import route changed decoded cells/materials, initial upload/halo history, embedded aux or source")
	}
	second := ImportedWorldChunkToXBrickMap(chunk)
	aux0[0] = 0
	aux8[0] = 0
	for bx, expected := range map[int]byte{0: 0x5a, 1: 0x37} {
		if got.Sectors[[3]int{}].GetBrick(bx, 0, 0).PrecomputedAux[0] != expected || second.Sectors[[3]int{}].GetBrick(bx, 0, 0).PrecomputedAux[0] != expected {
			t.Fatal("imported auxiliary packets alias decoded source")
		}
	}
	got.SetVoxel(7, 0, 0, 0)
	if got.Sectors[[3]int{}].GetBrick(1, 0, 0).PrecomputedAux != nil {
		t.Fatal("boundary edit retained adjacent stale authored normal packet")
	}
	if _, value := second.GetVoxel(7, 0, 0); value != 10 || second.Sectors[[3]int{}].GetBrick(1, 0, 0).PrecomputedAux[0] != 0x37 || !reflect.DeepEqual(chunk.Voxels, original) {
		t.Fatal("editing imported map changed another conversion/source/auxiliary ownership")
	}
}

func w4bImportFixture(kind string) *content.ImportedWorldChunkDef {
	chunk := &content.ImportedWorldChunkDef{}
	add := func(x, y, z int) {
		chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1, MaterialValue: uint8(1 + (x+y+z)%13)})
	}
	switch kind {
	case "dense-x-fast":
		for z := 0; z < 32; z++ {
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					add(x, y, z)
				}
			}
		}
	case "dense-brick-major":
		for bz := 0; bz < 4; bz++ {
			for by := 0; by < 4; by++ {
				for bx := 0; bx < 4; bx++ {
					for z := 0; z < 8; z++ {
						for y := 0; y < 8; y++ {
							for x := 0; x < 8; x++ {
								add(bx*8+x, by*8+y, bz*8+z)
							}
						}
					}
				}
			}
		}
	case "shell":
		for z := 0; z < 32; z++ {
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					if x == 0 || y == 0 || z == 0 || x == 31 || y == 31 || z == 31 {
						add(x, y, z)
					}
				}
			}
		}
	case "sparse-scattered":
		rng := rand.New(rand.NewSource(4402))
		for i := 0; i < 2048; i++ {
			chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: rng.Intn(256) - 128, Y: rng.Intn(256) - 128, Z: rng.Intn(256) - 128, Value: 1, MaterialValue: uint8(1 + i%13)})
		}
	}
	return chunk
}

var w4bImportedBenchmarkMap *volume.XBrickMap

func BenchmarkW4bImportedConstruction(b *testing.B) {
	for _, kind := range []string{"dense-x-fast", "dense-brick-major", "shell", "sparse-scattered"} {
		chunk := w4bImportFixture(kind)
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(len(chunk.Voxels)), "records/op")
			for i := 0; i < b.N; i++ {
				w4bImportedBenchmarkMap = ImportedWorldChunkToXBrickMap(chunk)
			}
		})
	}
}
