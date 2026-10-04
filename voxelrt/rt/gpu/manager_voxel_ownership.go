package gpu

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

// A nonzero-sized token provides exact manager identity without allowing a
// borrowed allocation header to retain the entire manager and its resources.
type voxelAllocationOwnerToken struct{ identity byte }

// Counts describe allocated snapshot edges, including duplicate coordinates and
// brick indices. They never reserve desired CPU geometry or admission demand.
type voxelAllocationOwnership struct {
	token   *voxelAllocationOwnerToken
	headers map[*volume.XBrickMap]*ObjectGpuAllocation
	sectors map[*volume.Sector]int
	bricks  map[*volume.Brick]int
	depth   int
	legacy  bool
}

// Validate only the outer allocation inventory, once per operation. Derived
// snapshot maps are private manager producers; arbitrary GPU-plumbing edits are
// not tracked. Unknown/replaced headers permanently restore the legacy scans.
func (m *GpuBufferManager) beginVoxelOwnership() {
	o := &m.voxelOwnership
	if o.depth == 0 && !o.legacy {
		if o.token == nil {
			o.token = &voxelAllocationOwnerToken{}
			o.headers = make(map[*volume.XBrickMap]*ObjectGpuAllocation)
			o.sectors = make(map[*volume.Sector]int)
			o.bricks = make(map[*volume.Brick]int)
		}
		if len(o.headers) != len(m.Allocations) {
			m.invalidateVoxelOwnership()
		} else {
			for xbm, alloc := range m.Allocations {
				if !m.ownsVoxelAllocation(xbm, alloc) {
					m.invalidateVoxelOwnership()
					break
				}
			}
		}
	}
	o.depth++
}

func (m *GpuBufferManager) endVoxelOwnership() { m.voxelOwnership.depth-- }

func (m *GpuBufferManager) invalidateVoxelOwnership() {
	o := &m.voxelOwnership
	o.legacy = true
	o.token, o.headers, o.sectors, o.bricks = nil, nil, nil, nil
}

func (m *GpuBufferManager) ownsVoxelAllocation(xbm *volume.XBrickMap, alloc *ObjectGpuAllocation) bool {
	o := &m.voxelOwnership
	return !o.legacy && alloc != nil && o.token != nil && alloc.ownerToken == o.token &&
		alloc.ownerMap == xbm && o.headers[xbm] == alloc
}

func (m *GpuBufferManager) trackVoxelAllocation(xbm *volume.XBrickMap, alloc *ObjectGpuAllocation) {
	o := &m.voxelOwnership
	if !o.legacy {
		alloc.ownerToken, alloc.ownerMap = o.token, xbm
		o.headers[xbm] = alloc
	}
}

func decrementVoxelReference[T comparable](counts map[T]int, pointer T) {
	if counts[pointer] <= 1 {
		delete(counts, pointer)
	} else {
		counts[pointer]--
	}
}

func (m *GpuBufferManager) removeVoxelSnapshotEdges(sector *volume.Sector, pointers *[64]*volume.Brick) {
	o := &m.voxelOwnership
	if o.legacy {
		return
	}
	decrementVoxelReference(o.sectors, sector)
	m.removeVoxelBrickEdges(pointers)
}

func (m *GpuBufferManager) removeVoxelBrickEdges(pointers *[64]*volume.Brick) {
	if pointers != nil && !m.voxelOwnership.legacy {
		for _, brick := range pointers {
			if brick != nil {
				decrementVoxelReference(m.voxelOwnership.bricks, brick)
			}
		}
	}
}

// Capture the actual written header and array before execution. Selection or
// revision can change during an executor; successful queued writes still own
// their captured snapshot even when they cannot acknowledge current dirtiness.
type voxelUploadSnapshot struct {
	xbm      *volume.XBrickMap
	alloc    *ObjectGpuAllocation
	key      [3]int
	pointers *[64]*volume.Brick
	before   [64]*volume.Brick
}

func (m *GpuBufferManager) captureVoxelUploadSnapshot(w voxelUploadWork) voxelUploadSnapshot {
	if w.kind == voxelUploadMaterial || m.voxelOwnership.legacy {
		return voxelUploadSnapshot{}
	}
	s := voxelUploadSnapshot{xbm: w.targetMap(), key: w.sectorCoordinate()}
	s.alloc = m.Allocations[s.xbm]
	if !m.ownsVoxelAllocation(s.xbm, s.alloc) {
		m.invalidateVoxelOwnership()
		return voxelUploadSnapshot{}
	}
	s.pointers = s.alloc.Bricks[s.key]
	if s.pointers != nil {
		s.before = *s.pointers
	}
	return s
}

func (m *GpuBufferManager) commitVoxelUploadSnapshot(s voxelUploadSnapshot) {
	if s.alloc == nil || m.voxelOwnership.legacy {
		return
	}
	if m.Allocations[s.xbm] != s.alloc || s.alloc.Bricks[s.key] != s.pointers ||
		!m.ownsVoxelAllocation(s.xbm, s.alloc) || len(m.Allocations) != len(m.voxelOwnership.headers) {
		m.invalidateVoxelOwnership()
		return
	}
	if s.pointers == nil {
		return
	}
	// One captured sector is bounded at 64 entries, independent of total scene
	// size. Compare the whole array so injected successful executors are covered.
	for i, old := range s.before {
		next := s.pointers[i]
		if old == next {
			continue
		}
		if old != nil {
			decrementVoxelReference(m.voxelOwnership.bricks, old)
		}
		if next != nil {
			m.voxelOwnership.bricks[next]++
		}
	}
}
