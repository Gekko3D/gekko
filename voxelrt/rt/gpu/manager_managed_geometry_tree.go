package gpu

import (
	"math/bits"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Immutable ordinal tree; replacement retains only the current path and leaves.
type managedGeometryStageNode struct {
	left, right       *managedGeometryStageNode
	sector            *volume.Sector
	bytes, generation uint64
}

// ManagedGeometryStageStorageBytes charges a full tree and one maximum cloned path.
func ManagedGeometryStageStorageBytes(n int) (uint64, bool) {
	if n < 0 {
		return 0, false
	}
	if n == 0 {
		return 0, true
	}
	full, ok := managedGeometryMultiply(uint64(n), 2)
	if !ok {
		return 0, false
	}
	nodes, ok := managedGeometryAdd(full-1, uint64(bits.Len(uint(n-1))+1))
	if !ok {
		return 0, false
	}
	return managedGeometryMultiply(nodes, uint64(unsafe.Sizeof(managedGeometryStageNode{})))
}
func managedGeometryLeaf(root *managedGeometryStageNode, lo, hi, index int) *managedGeometryStageNode {
	if root == nil || index < lo || index >= hi {
		return nil
	}
	if hi-lo == 1 {
		return root
	}
	mid := lo + (hi-lo)/2
	if index < mid {
		return managedGeometryLeaf(root.left, lo, mid, index)
	}
	return managedGeometryLeaf(root.right, mid, hi, index)
}
func managedGeometryInstall(root *managedGeometryStageNode, lo, hi, index int, sector *volume.Sector, bytes, generation uint64) *managedGeometryStageNode {
	node := new(managedGeometryStageNode)
	if root != nil {
		*node = *root
	}
	if hi-lo == 1 {
		node.sector, node.bytes, node.generation = sector, bytes, generation
		return node
	}
	mid := lo + (hi-lo)/2
	if index < mid {
		node.left = managedGeometryInstall(node.left, lo, mid, index, sector, bytes, generation)
	} else {
		node.right = managedGeometryInstall(node.right, mid, hi, index, sector, bytes, generation)
	}
	return node
}
func managedGeometryOrdinal(input core.ManagedGeometryInput, coord [3]int) (int, bool) {
	geometry := input.Geometry()
	lo, hi := 0, geometry.Len()
	for lo < hi {
		mid := lo + (hi-lo)/2
		c, ok := geometry.Coord(mid)
		if !ok {
			return 0, false
		}
		switch managedGeometryContentCompare(c, coord) {
		case -1:
			lo = mid + 1
		case 1:
			hi = mid
		default:
			return mid, true
		}
	}
	return 0, false
}
func managedGeometryInspectSector(source *volume.Sector) *volume.Sector {
	if source == nil {
		return nil
	}
	out := *source
	if source.PackedBricks != nil {
		out.PackedBricks = make([]*volume.Brick, len(source.PackedBricks), cap(source.PackedBricks))
	}
	for i, sourceBrick := range source.PackedBricks {
		if sourceBrick == nil {
			continue
		}
		brick := *sourceBrick
		if sourceBrick.PrecomputedAux != nil {
			brick.PrecomputedAux = make([]byte, len(sourceBrick.PrecomputedAux), cap(sourceBrick.PrecomputedAux))
			copy(brick.PrecomputedAux, sourceBrick.PrecomputedAux)
		}
		out.PackedBricks[i] = &brick
	}
	return &out
}
