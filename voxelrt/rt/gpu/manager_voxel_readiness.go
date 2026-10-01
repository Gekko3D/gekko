package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// VoxelObjectReady checks the exact adopted target using only constant-time
// allocation and dirty-queue metadata. Ready means writes are queued before
// rendering, not that the GPU has finished executing them.
func (m *GpuBufferManager) VoxelObjectReady(obj *core.VoxelObject, xbm *volume.XBrickMap, targetRevision uint64) (ready bool, pendingSectors int, pendingBricks int) {
	if xbm == nil {
		return false, 0, 0
	}
	pendingSectors, pendingBricks = len(xbm.DirtySectors), len(xbm.DirtyBricks)
	if m == nil || obj == nil || obj.XBrickMap != xbm || xbm.Revision != targetRevision {
		return false, pendingSectors, pendingBricks
	}
	alloc := m.Allocations[xbm]
	if alloc == nil || xbm.StructureDirty ||
		len(alloc.Sectors) != len(xbm.Sectors) || len(alloc.Bricks) != len(xbm.Sectors) ||
		pendingSectors != 0 || pendingBricks != 0 ||
		m.lastSectorGridTopologyRevision != m.sectorTopologyRevision {
		return false, pendingSectors, pendingBricks
	}
	matAlloc := m.MaterialAllocations[obj]
	if matAlloc == nil {
		return false, pendingSectors, pendingBricks
	}
	ptr, length := materialTableIdentity(obj.MaterialTable)
	capacity := materialUploadRows(length)
	ready = matAlloc.MaterialTablePtr == ptr && matAlloc.MaterialTableLen == length &&
		matAlloc.BufferGeneration == m.MaterialBufferGeneration && uint64(matAlloc.MaterialCapacity) >= uint64(capacity)
	return ready, pendingSectors, pendingBricks
}
