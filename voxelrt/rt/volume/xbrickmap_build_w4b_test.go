package volume_test

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func w4bOrderedParity(t *testing.T, writes []volume.VoxelWrite) {
	t.Helper()
	got := volume.BuildXBrickMap(slices.Values(writes))
	want := p5fSequential(writes)
	w4aSameEditableMap(t, got, want)
}

func TestW4bEveryLocalHaloPosition(t *testing.T) {
	for _, offset := range []int{-32, -8, 0, 24} {
		t.Run(fmt.Sprintf("offset%d", offset), func(t *testing.T) {
			for z := 0; z < 8; z++ {
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						position := volume.VoxelWrite{X: offset + x, Y: offset + y, Z: offset + z}
						writes := []volume.VoxelWrite{position} // Absent zero must not mark a halo class.
						for _, value := range []uint8{7, 7, 255, 0, 9} {
							w := position
							w.Value = value
							writes = append(writes, w)
						}
						w4bOrderedParity(t, writes)
					}
				}
			}
		})
	}
}

func TestW4bSparseHalosAndCacheLifetimes(t *testing.T) {
	cases := [][]volume.VoxelWrite{
		// The bounding box of these two halo regions contains keys neither
		// actual changed voxel touches; extrema-based dirty marking is incorrect.
		{{X: 0, Y: 0, Z: 7, Value: 7}, {X: 7, Y: 7, Z: 0, Value: 9}},
		{{X: -8, Y: -8, Z: -1, Value: 7}, {X: -1, Y: -1, Z: -8, Value: 9}},
		// Repeated same-key removal/recreation invalidates a cached brick and sector.
		{{X: 7, Y: 7, Z: 7, Value: 7}, {X: 7, Y: 7, Z: 7}, {X: 7, Y: 7, Z: 7}, {X: 7, Y: 7, Z: 7, Value: 255}, {X: 7, Y: 7, Z: 7}, {X: 0, Y: 0, Z: 0, Value: 9}},
		// Insert earlier packed bricks, bounce sectors, then revisit old keys.
		{{X: 31, Y: 31, Z: 31, Value: 7}, {X: 0, Y: 0, Z: 0, Value: 8}, {X: 32, Y: 0, Z: 0, Value: 9}, {X: 31, Y: 31, Z: 31, Value: 10}, {X: 0, Y: 0, Z: 0}, {X: -1, Y: 0, Z: 0, Value: 11}, {X: 31, Y: 31, Z: 31}, {X: 31, Y: 31, Z: 31, Value: 12}, {X: 32, Y: 0, Z: 0, Value: 13}},
		// No-op in a new halo class must not suppress its later first change.
		{{X: 0, Y: 0, Z: 0, Value: 1}, {X: 7, Y: 7, Z: 7}, {X: 7, Y: 7, Z: 7, Value: 2}, {X: 4, Y: 0, Z: 4}, {X: 4, Y: 0, Z: 4, Value: 3}},
	}
	for i, writes := range cases {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			for prefix := 1; prefix <= len(writes); prefix++ {
				w4bOrderedParity(t, writes[:prefix])
			}
		})
	}
	var dense []volume.VoxelWrite
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				dense = append(dense, volume.VoxelWrite{X: x - 8, Y: y - 32, Z: z + 24, Value: uint8(1 + (x+y+z)%7)})
			}
		}
	}
	w4bOrderedParity(t, dense)
	for i := len(dense) - 1; i >= 0; i-- {
		w := dense[i]
		w.Value = 0
		dense = append(dense, w)
	}
	w4bOrderedParity(t, dense)
}

func TestW4bArbitraryOrderedWrites(t *testing.T) {
	rng := rand.New(rand.NewSource(4402))
	var writes []volume.VoxelWrite
	for i := 0; i < 768; i++ {
		w := volume.VoxelWrite{X: rng.Intn(81) - 40, Y: rng.Intn(81) - 40, Z: rng.Intn(81) - 40, Value: uint8(rng.Intn(5))}
		if i%5 == 0 && len(writes) > 0 {
			w = writes[len(writes)-1]
			w.Value = uint8(rng.Intn(5))
		}
		writes = append(writes, w)
	}
	for _, prefix := range []int{1, 7, 8, 31, 32, 127, 256, len(writes)} {
		w4bOrderedParity(t, writes[:prefix])
	}
}

var w4bBenchmarkMap *volume.XBrickMap

func BenchmarkW4bOrderedWriteTail(b *testing.B) {
	var writes []volume.VoxelWrite
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				writes = append(writes, volume.VoxelWrite{X: x, Y: y, Z: z, Value: 7})
			}
		}
	}
	initial := slices.Clone(writes)
	writes = append(writes, initial...)
	for _, value := range []uint8{255, 0} {
		for _, w := range initial {
			w.Value = value
			writes = append(writes, w)
		}
	}
	b.ReportAllocs()
	b.ReportMetric(float64(len(writes)), "writes/op")
	for i := 0; i < b.N; i++ {
		w4bBenchmarkMap = volume.BuildXBrickMap(slices.Values(writes))
	}
}
