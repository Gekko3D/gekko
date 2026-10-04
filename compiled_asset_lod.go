package gekko

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"unicode/utf8"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

const compiledAssetLOD2xReductionVersion = "occupancy-or-zero-anchored-2x-v1"

// compiledAssetLOD2xGeometry is a geometry-only compiler intermediate. Its
// lattice records the full-resolution source; coarse bounds use coarse cells.
// Material opacity, runtime binding and serialized derivative identity belong
// to later callers and are not established by this reduction.
type compiledAssetLOD2xGeometry struct {
	Bricks                                     []voxelcodec.Brick
	SourceLattice                              content.VoxelObjectLatticeDef
	Value                                      uint8
	SourceVoxelCount, CoarseVoxelCount         int64
	SourceMin, SourceMax, CoarseMin, CoarseMax [3]int64
	ReductionVersion                           string
}

// buildCompiledAssetLOD2x conservatively occupies every zero-anchored coarse
// cell touched by source geometry. A coarse cell q covers [2q,2q+2). Valid
// sources with mixed values or no strict voxel-count reduction are ineligible.
// All source validation completes before reporting ineligibility. Input is
// borrowed read-only and every returned mutable allocation belongs to the call.
func buildCompiledAssetLOD2x(source *content.CompiledAssetShapeDef) (*compiledAssetLOD2xGeometry, error) {
	if source == nil || source.SchemaVersion != content.CurrentCompiledAssetShapeSchemaVersion {
		return nil, fmt.Errorf("invalid compiled LOD source schema")
	}
	lattice := source.Lattice
	if lattice.VoxelResolution <= 0 || math.IsNaN(float64(lattice.VoxelResolution)) || math.IsInf(float64(lattice.VoxelResolution), 0) || len(lattice.RasterizationVersion) == 0 || len(lattice.RasterizationVersion) > 128 || !utf8.ValidString(lattice.RasterizationVersion) {
		return nil, fmt.Errorf("invalid compiled LOD source lattice")
	}
	seen := make(map[[3]int32]struct{}, len(source.Bricks))
	var sourceCount int64
	var value uint8
	mixed := false
	for _, brick := range source.Bricks {
		if brick.Materials != nil || brick.Aux != nil {
			return nil, fmt.Errorf("compiled LOD source contains secondary layers")
		}
		for _, coordinate := range brick.Coord {
			if coordinate < -268435456 || coordinate > 268435455 {
				return nil, fmt.Errorf("compiled LOD source exceeds portable coordinates")
			}
		}
		if _, duplicate := seen[brick.Coord]; duplicate {
			return nil, fmt.Errorf("duplicate compiled LOD source brick")
		}
		seen[brick.Coord] = struct{}{}
		count := 0
		for _, word := range brick.Occupancy {
			count += bits.OnesCount64(word)
		}
		if count == 0 || len(brick.Values) != count {
			return nil, fmt.Errorf("invalid compiled LOD source occupancy/value cardinality")
		}
		if sourceCount > math.MaxInt64-int64(count) {
			return nil, fmt.Errorf("compiled LOD source count overflow")
		}
		sourceCount += int64(count)
		for _, v := range brick.Values {
			if v == 0 {
				return nil, fmt.Errorf("compiled LOD source contains zero value")
			}
			if value == 0 {
				value = v
			} else if value != v {
				mixed = true
			}
		}
	}
	if sourceCount <= 1 || mixed {
		return nil, nil
	}
	result := &compiledAssetLOD2xGeometry{SourceLattice: lattice, Value: value, SourceVoxelCount: sourceCount, ReductionVersion: compiledAssetLOD2xReductionVersion}
	coarse := make(map[[3]int32]*voxelcodec.Brick)
	first := true
	for _, brick := range source.Bricks {
		for wordIndex, word := range brick.Occupancy {
			for word != 0 {
				linear := wordIndex*64 + bits.TrailingZeros64(word)
				word &= word - 1
				p := [3]int64{int64(brick.Coord[0])*8 + int64(linear%8), int64(brick.Coord[1])*8 + int64(linear/8%8), int64(brick.Coord[2])*8 + int64(linear/64)}
				var q [3]int64
				var coord [3]int32
				var local [3]int64
				for axis := 0; axis < 3; axis++ {
					q[axis] = p[axis] / 2
					if p[axis]%2 < 0 {
						q[axis]--
					}
					b, r := q[axis]/8, q[axis]%8
					if r < 0 {
						b--
						r += 8
					}
					coord[axis], local[axis] = int32(b), r
					if first || p[axis] < result.SourceMin[axis] {
						result.SourceMin[axis] = p[axis]
					}
					if first || p[axis]+1 > result.SourceMax[axis] {
						result.SourceMax[axis] = p[axis] + 1
					}
					if first || q[axis] < result.CoarseMin[axis] {
						result.CoarseMin[axis] = q[axis]
					}
					if first || q[axis]+1 > result.CoarseMax[axis] {
						result.CoarseMax[axis] = q[axis] + 1
					}
				}
				first = false
				out := coarse[coord]
				if out == nil {
					out = &voxelcodec.Brick{Coord: coord}
					coarse[coord] = out
				}
				bit := local[0] + 8*local[1] + 64*local[2]
				mask := uint64(1) << uint(bit%64)
				if out.Occupancy[bit/64]&mask == 0 {
					out.Occupancy[bit/64] |= mask
					result.CoarseVoxelCount++
				}
			}
		}
	}
	if result.CoarseVoxelCount >= sourceCount {
		return nil, nil
	}
	result.Bricks = make([]voxelcodec.Brick, 0, len(coarse))
	for _, brick := range coarse {
		count := 0
		for _, word := range brick.Occupancy {
			count += bits.OnesCount64(word)
		}
		brick.Values = make([]uint8, count)
		for i := range brick.Values {
			brick.Values[i] = value
		}
		result.Bricks = append(result.Bricks, *brick)
	}
	sort.Slice(result.Bricks, func(i, j int) bool {
		a, b := result.Bricks[i].Coord, result.Bricks[j].Coord
		for axis := 0; axis < 3; axis++ {
			if a[axis] != b[axis] {
				return a[axis] < b[axis]
			}
		}
		return false
	})
	return result, nil
}
