package volume

import (
	"math/bits"
	"slices"
)

// ManagedGeometryView retains frozen qualified sector geometry without its
// owner or mutable sector headers. Copies remain independent after owner edits,
// forks or exposure. The zero view is empty.
type ManagedGeometryView struct {
	root *managedIndexNode[*managedSectorRecord]
}

// CaptureGeometry captures qualified sealed geometry in constant time without
// allocation. Clearing the exclusive set makes later writes detach captured
// bricks, including normal halos. Nil, exposed or unqualified owners return an
// empty view and false. The owner requires exclusive access during capture;
// captured views can be read independently. Capture during a producer is unsupported.
func (m *ManagedXBrickMap) CaptureGeometry() (ManagedGeometryView, bool) {
	if m == nil || m.base == nil || !m.geometryQualified {
		return ManagedGeometryView{}, false
	}
	m.exclusive = nil
	return ManagedGeometryView{root: m.geometry}, true
}

// Len returns the captured sector count in constant time without allocation.
func (v ManagedGeometryView) Len() int { return managedIndexSize(v.root) }

// Coord reads signed X/Y/Z lexicographic order in logarithmic time without
// allocation. Invalid indices return a zero coordinate and false.
func (v ManagedGeometryView) Coord(index int) ([3]int, bool) {
	if node := managedIndexAt(v.root, index); node != nil {
		return node.coord, true
	}
	return [3]int{}, false
}

// CopySector defensively copies one captured sector, including at most 64
// dense bricks and at most VoxelAuxRecordBytes auxiliary bytes per brick.
// Invalid indices return nil and false. Headers, packed order and legacy brick
// metadata are preserved without normalization.
func (v ManagedGeometryView) CopySector(index int) (*Sector, bool) {
	if node := managedIndexAt(v.root, index); node != nil {
		return node.value.copySector(), true
	}
	return nil, false
}

// Records copy sector scalars and bounded packed references, never a mutable
// Sector or pointer slice. Brick backing becomes immutable to a public view at
// its capture barrier; owner-only records may reference exclusive current bricks.
type managedSectorRecord struct {
	coords [3]int
	mask   uint64
	count  int
	bricks [64]*Brick
}

func newManagedSectorRecord(sector *Sector) *managedSectorRecord {
	record := &managedSectorRecord{coords: sector.Coords, mask: sector.BrickMask64, count: len(sector.PackedBricks)}
	copy(record.bricks[:], sector.PackedBricks)
	return record
}

func (r *managedSectorRecord) copySector() *Sector {
	sector := &Sector{Coords: r.coords, BrickMask64: r.mask, PackedBricks: make([]*Brick, r.count)}
	for i := range sector.PackedBricks {
		sector.PackedBricks[i] = r.bricks[i].Copy()
	}
	return sector
}

// Qualify all copied current sectors before creating any record tree. Legacy
// copyable unsupported data retains the existing dense snapshot fallback.
func (m *ManagedXBrickMap) seedGeometryRecords() {
	for _, sector := range m.current.Sectors {
		if len(sector.PackedBricks) != bits.OnesCount64(sector.BrickMask64) {
			return
		}
		for _, brick := range sector.PackedBricks {
			aux := brick.PrecomputedAux
			if len(aux) > VoxelAuxRecordBytes || len(aux) == 0 && cap(aux) != 0 {
				return
			}
		}
	}
	keys := make([][3]int, 0, len(m.current.Sectors))
	for key := range m.current.Sectors {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareManagedTopologyCoords)
	m.geometry = buildManagedIndex(keys, func(key [3]int) *managedSectorRecord { return newManagedSectorRecord(m.current.Sectors[key]) })
	m.geometryQualified = true
}

func (m *ManagedXBrickMap) refreshGeometrySector(key [3]int) {
	if !m.geometryQualified {
		return
	}
	sector, present := m.current.Sectors[key]
	prior := findManagedIndex(m.geometry, key)
	if !present && prior == nil {
		return
	}
	if present && prior != nil && prior.value.matchesSector(sector) {
		return
	}
	var record *managedSectorRecord
	if present {
		record = newManagedSectorRecord(sector)
	}
	m.geometry = updateManagedIndex(m.geometry, key, record, present)
}

// Payload and flags can change in place only on exclusive backing. Public
// capture and Fork clear that exclusivity, so later mutations of captured
// backing detach and change these references. Compare before allocating a new
// record/path; already-exclusive content edits keep the existing index intact.
func (r *managedSectorRecord) matchesSector(sector *Sector) bool {
	if r.coords != sector.Coords || r.mask != sector.BrickMask64 || r.count != len(sector.PackedBricks) {
		return false
	}
	for i, brick := range sector.PackedBricks {
		if r.bricks[i] != brick {
			return false
		}
	}
	return true
}

// The fitted halo touches a fixed number of sector keys. Refresh their headers
// and packed references after detachment and the dense edit, including removals.
func (m *ManagedXBrickMap) refreshGeometryHalo(w VoxelWrite) {
	if !m.geometryQualified {
		return
	}
	minKey, _ := sectorBrickKeyForVoxel(w.X-VoxelNormalExtendedSurfaceFitRadius, w.Y-VoxelNormalExtendedSurfaceFitRadius, w.Z-VoxelNormalExtendedSurfaceFitRadius)
	maxKey, _ := sectorBrickKeyForVoxel(w.X+VoxelNormalExtendedSurfaceFitRadius, w.Y+VoxelNormalExtendedSurfaceFitRadius, w.Z+VoxelNormalExtendedSurfaceFitRadius)
	// The target remains mandatory even if integer-edge halo arithmetic wraps.
	target, _ := sectorBrickKeyForVoxel(w.X, w.Y, w.Z)
	m.refreshGeometrySector(target)
	for x := minKey[0]; x <= maxKey[0]; x++ {
		for y := minKey[1]; y <= maxKey[1]; y++ {
			for z := minKey[2]; z <= maxKey[2]; z++ {
				key := [3]int{x, y, z}
				if key != target {
					m.refreshGeometrySector(key)
				}
			}
		}
	}
}

// Material finalization can change flags and AtlasOffset on target bricks.
// Finalize the record references afterward, including an applied panic prefix.
func (m *ManagedXBrickMap) finishManagedBatch(batch *voxelEditBatch) {
	batch.finish(m.current)
	if m.geometryQualified {
		for key := range batch.bricks {
			m.refreshGeometrySector([3]int{key[0], key[1], key[2]})
		}
	}
}

// Private synchronous copies do not retain a view or clear exclusive backing.
func (m *ManagedXBrickMap) copyGeometrySnapshot() *XBrickMap {
	result := NewXBrickMap()
	walkManagedIndex(m.geometry, func(key [3]int, record *managedSectorRecord) {
		result.Sectors[key] = record.copySector()
	})
	m.copySnapshotMetadata(result)
	return result
}

func (m *ManagedXBrickMap) copySnapshotMetadata(result *XBrickMap) {
	result.CachedMin, result.CachedMax = m.current.CachedMin, m.current.CachedMax
	result.AABBDirty = m.current.AABBDirty
	result.Revision = m.current.Revision
	for key, revision := range m.current.SectorRevisions {
		result.SectorRevisions[key] = revision
	}
}
