package gekko

import (
	"fmt"
	"math/bits"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

// validateCompiledAssetLODSource requires structurally valid, typed decoded C1
// definitions, borrowed immutably for this session. verifyCompiledAssetInput
// authenticates the source content/base identities and lattice before this call.
// It proves exact conservative coverage without rebuilding geometry or hashing
// frames. It establishes neither palette opacity nor validity for mutable live
// AssetServer geometry. All indexes are local and retain no source after return.
func validateCompiledAssetLODSource(lod *content.CompiledAssetLODDef, source verifiedCompiledAssetShape) error {
	if lod == nil || source.definition == nil || source.contentID == "" || source.baseIdentity == "" || lod.SourceContentID != source.contentID || lod.SourceLattice != source.definition.Lattice {
		return fmt.Errorf("compiled LOD source identity or lattice mismatch")
	}
	sourceBricks := compiledAssetLODSourceIndex(source.definition.Bricks)
	coarseBricks := compiledAssetLODSourceIndex(lod.Bricks)
	var minimum, maximum [3]int64
	var sourceCount, coarseCount int64
	for i := range source.definition.Bricks {
		brick := &source.definition.Bricks[i]
		for _, value := range brick.Values {
			if value != lod.Value {
				return fmt.Errorf("compiled LOD source is not the declared sole value")
			}
		}
		for wordIndex, word := range brick.Occupancy {
			for word != 0 {
				linear := wordIndex*64 + bits.TrailingZeros64(word)
				word &= word - 1
				p := compiledAssetLODSourceCell(brick.Coord, linear)
				var q [3]int64
				for axis := 0; axis < 3; axis++ {
					if sourceCount == 0 || p[axis] < minimum[axis] {
						minimum[axis] = p[axis]
					}
					if sourceCount == 0 || p[axis]+1 > maximum[axis] {
						maximum[axis] = p[axis] + 1
					}
					q[axis] = compiledAssetLODSourceFloor(p[axis], 2)
				}
				if !compiledAssetLODSourceContains(coarseBricks, q) {
					return fmt.Errorf("compiled LOD does not cover an occupied source cell")
				}
				sourceCount++
			}
		}
	}
	if sourceCount == 0 || sourceCount != lod.SourceVoxelCount || minimum != lod.SourceMin || maximum != lod.SourceMax {
		return fmt.Errorf("compiled LOD source count or occupied bounds mismatch")
	}
	for i := range lod.Bricks {
		brick := &lod.Bricks[i]
		for wordIndex, word := range brick.Occupancy {
			for word != 0 {
				linear := wordIndex*64 + bits.TrailingZeros64(word)
				word &= word - 1
				q := compiledAssetLODSourceCell(brick.Coord, linear)
				witness := false
				for local := 0; local < 8; local++ {
					p := [3]int64{2*q[0] + int64(local&1), 2*q[1] + int64(local>>1&1), 2*q[2] + int64(local>>2)}
					if compiledAssetLODSourceContains(sourceBricks, p) {
						witness = true
						break
					}
				}
				if !witness {
					return fmt.Errorf("compiled LOD cell has no occupied source witness")
				}
				coarseCount++
			}
		}
	}
	if coarseCount == 0 || sourceCount <= coarseCount {
		return fmt.Errorf("compiled LOD source does not reduce occupied cell count")
	}
	return nil
}

func compiledAssetLODSourceIndex(bricks []voxelcodec.Brick) map[[3]int32]*voxelcodec.Brick {
	index := make(map[[3]int32]*voxelcodec.Brick, len(bricks))
	for i := range bricks {
		index[bricks[i].Coord] = &bricks[i]
	}
	return index
}

func compiledAssetLODSourceCell(coord [3]int32, linear int) [3]int64 {
	return [3]int64{int64(coord[0])*8 + int64(linear%8), int64(coord[1])*8 + int64(linear/8%8), int64(coord[2])*8 + int64(linear/64)}
}

func compiledAssetLODSourceFloor(value, divisor int64) int64 {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}

func compiledAssetLODSourceContains(index map[[3]int32]*voxelcodec.Brick, p [3]int64) bool {
	var coord [3]int32
	var local [3]int64
	for axis := 0; axis < 3; axis++ {
		b := compiledAssetLODSourceFloor(p[axis], 8)
		coord[axis] = int32(b)
		local[axis] = p[axis] - 8*b
	}
	brick := index[coord]
	if brick == nil {
		return false
	}
	linear := local[0] + 8*local[1] + 64*local[2]
	return brick.Occupancy[linear/64]&(uint64(1)<<uint(linear%64)) != 0
}
