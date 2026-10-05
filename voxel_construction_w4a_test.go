package gekko

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func w4aOldTerrainStream(chunk *content.TerrainChunkDef) *volume.XBrickMap {
	return volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		if chunk == nil {
			return
		}
		for _, c := range chunk.Columns {
			for y := 0; y < c.FilledVoxels; y++ {
				if !yield(volume.VoxelWrite{X: c.X, Y: y, Z: c.Z, Value: chunk.SolidValue}) {
					return
				}
			}
		}
	})
}

func TestW4aTerrainColumnRouteParity(t *testing.T) {
	for _, value := range []uint8{0, 7, 255} {
		chunk := &content.TerrainChunkDef{SolidValue: value, Columns: []content.TerrainChunkColumnDef{
			{X: -33, Z: -32, FilledVoxels: 33}, {X: 32, Z: 7, FilledVoxels: 96},
			{X: -33, Z: -32, FilledVoxels: 2}, {X: 0, Z: -1, FilledVoxels: 9},
			{X: -33, Z: -32, FilledVoxels: 65}, {X: 32, Z: 7, FilledVoxels: 128},
			{X: 8, Z: 8, FilledVoxels: -1}, {X: 7, Z: 31, FilledVoxels: 0},
		}}
		original := slices.Clone(chunk.Columns)
		want := volume.NewXBrickMap()
		for _, c := range chunk.Columns {
			for y := 0; y < c.FilledVoxels; y++ {
				want.SetVoxel(c.X, y, c.Z, value)
			}
		}
		got := terrainChunkToXBrickMap(chunk)
		p5gConstructionParity(t, got, want)
		if !reflect.DeepEqual(got.Sectors, want.Sectors) || got.StructureDirty != want.StructureDirty || !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) {
			t.Fatal("terrain route changed material/occupancy encoding or exact initial upload/halo coverage")
		}
		if !reflect.DeepEqual(chunk.Columns, original) {
			t.Fatal("terrain route mutated authored columns")
		}
		independent := terrainChunkToXBrickMap(chunk)
		got.SetVoxel(-33, 64, -32, 0)
		if _, v := independent.GetVoxel(-33, 64, -32); v != value {
			t.Fatal("terrain conversions share editable geometry")
		}
	}
	p5gConstructionParity(t, terrainChunkToXBrickMap(nil), volume.NewXBrickMap())
}

var w4aBenchmarkMap *volume.XBrickMap

func BenchmarkW4aTerrainColumnConstruction(b *testing.B) {
	for _, width := range []int{16, 32} {
		for _, height := range []int{8, 64, 256} {
			chunk := &content.TerrainChunkDef{SolidValue: 7, ChunkSize: width}
			voxels := 0
			for z := 0; z < width; z++ {
				for x := 0; x < width; x++ {
					filled := height + (x+z)%7
					chunk.Columns = append(chunk.Columns, content.TerrainChunkColumnDef{X: x, Z: z, FilledVoxels: filled})
					voxels += filled
				}
			}
			b.Run(fmt.Sprintf("%dx%d/height%d", width, width, height), func(b *testing.B) {
				for _, method := range []struct {
					name  string
					build func(*content.TerrainChunkDef) *volume.XBrickMap
				}{{"terrain-route", terrainChunkToXBrickMap}, {"legacy-voxel-stream", w4aOldTerrainStream}} {
					b.Run(method.name, func(b *testing.B) {
						b.ReportAllocs()
						b.ReportMetric(float64(voxels), "voxels/op")
						for i := 0; i < b.N; i++ {
							w4aBenchmarkMap = method.build(chunk)
						}
					})
				}
			})
		}
	}
}
