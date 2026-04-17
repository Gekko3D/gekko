package volume

import "testing"

type task4OccupancyCase struct {
	name  string
	brick *Brick
}

func task4OccupancyCases() []task4OccupancyCase {
	return []task4OccupancyCase{
		{
			name: "sparse_corners",
			brick: task4MakeBrick(func(x, y, z int) bool {
				return (x == 0 || x == BrickSize-1) &&
					(y == 0 || y == BrickSize-1) &&
					(z == 0 || z == BrickSize-1)
			}),
		},
		{
			name: "clustered_core_4x4x4",
			brick: task4MakeBrick(func(x, y, z int) bool {
				return x >= 2 && x <= 5 && y >= 2 && y <= 5 && z >= 2 && z <= 5
			}),
		},
		{
			name: "surface_shell",
			brick: task4MakeBrick(func(x, y, z int) bool {
				return x == 0 || x == BrickSize-1 ||
					y == 0 || y == BrickSize-1 ||
					z == 0 || z == BrickSize-1
			}),
		},
		{
			name: "checkerboard",
			brick: task4MakeBrick(func(x, y, z int) bool {
				return (x+y+z)%2 == 0
			}),
		},
	}
}

func task4MakeBrick(occupied func(x, y, z int) bool) *Brick {
	brick := NewBrick()
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				if occupied(x, y, z) {
					brick.SetVoxel(x, y, z, 1)
				}
			}
		}
	}
	return brick
}

func task4VoxelToMicroIdx(voxelIdx uint32) uint32 {
	vx := voxelIdx & 7
	vy := (voxelIdx >> 3) & 7
	vz := (voxelIdx >> 6) & 7
	return (vx >> 1) + ((vy >> 1) << 2) + ((vz >> 1) << 4)
}

func task4DenseOnly(words [DenseOccupancyWordCount]uint32, voxelIdx uint32) bool {
	word := words[voxelIdx>>5]
	bit := uint32(1) << (voxelIdx & 31)
	return (word & bit) != 0
}

func task4MicroThenDense(mask uint64, words [DenseOccupancyWordCount]uint32, voxelIdx uint32) bool {
	microIdx := task4VoxelToMicroIdx(voxelIdx)
	if (mask & (uint64(1) << microIdx)) == 0 {
		return false
	}
	return task4DenseOnly(words, voxelIdx)
}

func TestTask4MicroMaskDenseOccupancyStats(t *testing.T) {
	for _, tc := range task4OccupancyCases() {
		words := tc.brick.DenseOccupancyWords()
		microPasses := 0
		hits := 0
		for voxelIdx := uint32(0); voxelIdx < BrickSize*BrickSize*BrickSize; voxelIdx++ {
			microIdx := task4VoxelToMicroIdx(voxelIdx)
			if (tc.brick.OccupancyMask64 & (uint64(1) << microIdx)) != 0 {
				microPasses++
			}
			if task4DenseOnly(words, voxelIdx) {
				hits++
			}
		}
		totalQueries := BrickSize * BrickSize * BrickSize
		t.Logf("%s: occupied=%d/%d micro_passes=%d micro_skips=%d skip_rate=%.1f%%",
			tc.name,
			hits,
			totalQueries,
			microPasses,
			totalQueries-microPasses,
			float64(totalQueries-microPasses)*100.0/float64(totalQueries),
		)
	}
}

var task4BenchmarkSink int

func BenchmarkTask4OccupancyMicroThenDense(b *testing.B) {
	for _, tc := range task4OccupancyCases() {
		words := tc.brick.DenseOccupancyWords()
		mask := tc.brick.OccupancyMask64
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			hits := 0
			for i := 0; i < b.N; i++ {
				if task4MicroThenDense(mask, words, uint32(i&511)) {
					hits++
				}
			}
			task4BenchmarkSink += hits
		})
	}
}

func BenchmarkTask4OccupancyDenseOnly(b *testing.B) {
	for _, tc := range task4OccupancyCases() {
		words := tc.brick.DenseOccupancyWords()
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			hits := 0
			for i := 0; i < b.N; i++ {
				if task4DenseOnly(words, uint32(i&511)) {
					hits++
				}
			}
			task4BenchmarkSink += hits
		})
	}
}
