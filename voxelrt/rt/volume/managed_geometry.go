package volume

import (
	"math/bits"
	"slices"
	"unsafe"
)

// ManagedGeometryView retains frozen qualified sector geometry without its
// owner or mutable sector headers. Copies remain independent after owner edits,
// forks or exposure. The zero view is empty.
type ManagedGeometryView struct {
	root          *managedIndexNode[*managedSectorRecord]
	retainedBytes uint64
	copyBytes     uint64
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
	return ManagedGeometryView{root: m.geometry, retainedBytes: m.geometryRetainedBytes, copyBytes: m.geometryCopyBytes}, true
}

// Len returns the captured sector count in constant time without allocation.
func (v ManagedGeometryView) Len() int { return managedIndexSize(v.root) }

// RetainedBytes returns a frozen conservative charge in constant time without
// allocation. It includes reachable geometry index nodes and records, a complete
// dense Brick per packed reference and each auxiliary slice's backing capacity.
// Aliases may be charged repeatedly. The view itself, owner/map metadata,
// coordinate-only topology, allocator overhead and GPU storage are excluded.
func (v ManagedGeometryView) RetainedBytes() uint64 { return v.retainedBytes }

// CopyBytes returns the frozen aggregate charge of all CopySector outputs in
// constant time without allocation. Each output charges its Sector header,
// exact-length packed pointer backing, a complete Brick per packed reference
// and the captured auxiliary backing capacity. Retained input records, map
// metadata, allocator overhead and GPU storage are excluded.
func (v ManagedGeometryView) CopyBytes() uint64 { return v.copyBytes }

// CopySectorBytes preflights one CopySector output in logarithmic time without
// allocation. Invalid indices return zero and false.
func (v ManagedGeometryView) CopySectorBytes(index int) (uint64, bool) {
	if node := managedIndexAt(v.root, index); node != nil {
		return node.value.copyBytes, true
	}
	return 0, false
}

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
	coords        [3]int
	mask          uint64
	count         int
	bricks        [64]*Brick
	retainedBytes uint64
	copyBytes     uint64
}

func newManagedSectorRecord(sector *Sector, retainedBytes, copyBytes uint64) *managedSectorRecord {
	record := &managedSectorRecord{coords: sector.Coords, mask: sector.BrickMask64, count: len(sector.PackedBricks), retainedBytes: retainedBytes, copyBytes: copyBytes}
	copy(record.bricks[:], sector.PackedBricks)
	return record
}

func (r *managedSectorRecord) copySector() *Sector {
	sector := &Sector{Coords: r.coords, BrickMask64: r.mask, PackedBricks: make([]*Brick, r.count)}
	for i := range sector.PackedBricks {
		brick := *r.bricks[i]
		if aux := brick.PrecomputedAux; aux != nil {
			brick.PrecomputedAux = make([]byte, len(aux), cap(aux))
			copy(brick.PrecomputedAux, aux)
		}
		sector.PackedBricks[i] = &brick
	}
	return sector
}

func addManagedGeometryBytes(total, charge uint64) (uint64, bool) {
	result, carry := bits.Add64(total, charge, 0)
	return result, carry == 0
}

// Every geometry root reaches exactly one node and record per sector; no AVL
// subtree accounting is needed. Count backing capacity rather than auxiliary
// length, which only bounds copy work. Cached charges on old records never change.
func managedSectorGeometryBytes(sector *Sector) (retainedBytes, copyBytes uint64, ok bool) {
	if len(sector.PackedBricks) != bits.OnesCount64(sector.BrickMask64) {
		return 0, 0, false
	}
	var backingBytes uint64
	for _, brick := range sector.PackedBricks {
		aux := brick.PrecomputedAux
		if len(aux) > VoxelAuxRecordBytes || len(aux) == 0 && cap(aux) != 0 {
			return 0, 0, false
		}
		backingBytes, ok = addManagedGeometryBytes(backingBytes, uint64(unsafe.Sizeof(Brick{})))
		if !ok {
			return 0, 0, false
		}
		backingBytes, ok = addManagedGeometryBytes(backingBytes, uint64(cap(aux)))
		if !ok {
			return 0, 0, false
		}
	}
	retainedHeader := uint64(unsafe.Sizeof(managedIndexNode[*managedSectorRecord]{})) + uint64(unsafe.Sizeof(managedSectorRecord{}))
	retainedBytes, ok = addManagedGeometryBytes(backingBytes, retainedHeader)
	if !ok {
		return 0, 0, false
	}
	// Qualified cardinality is bounded by the 64-bit mask population.
	copyHeader := uint64(unsafe.Sizeof(Sector{})) + uint64(len(sector.PackedBricks))*uint64(unsafe.Sizeof((*Brick)(nil)))
	copyBytes, ok = addManagedGeometryBytes(backingBytes, copyHeader)
	return retainedBytes, copyBytes, ok
}

// Qualify all copied current sectors before creating any record tree. Legacy
// copyable unsupported data retains the existing dense snapshot fallback.
func (m *ManagedXBrickMap) seedGeometryRecords() {
	var total, copyTotal uint64
	for _, sector := range m.current.Sectors {
		charge, copyCharge, ok := managedSectorGeometryBytes(sector)
		if !ok {
			return
		}
		copyTotal, ok = addManagedGeometryBytes(copyTotal, copyCharge)
		if !ok {
			return
		}
		total, ok = addManagedGeometryBytes(total, charge)
		if !ok {
			return
		}
	}
	keys := make([][3]int, 0, len(m.current.Sectors))
	for key := range m.current.Sectors {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareManagedTopologyCoords)
	m.geometry = buildManagedIndex(keys, func(key [3]int) *managedSectorRecord {
		sector := m.current.Sectors[key]
		// The exclusive constructor already qualified and charged every sector.
		charge, copyCharge, _ := managedSectorGeometryBytes(sector)
		return newManagedSectorRecord(sector, charge, copyCharge)
	})
	m.geometryRetainedBytes = total
	m.geometryCopyBytes = copyTotal
	m.geometryQualified = true
}

func (m *ManagedXBrickMap) disableGeometryRecords() {
	m.geometry = nil
	m.geometryRetainedBytes = 0
	m.geometryCopyBytes = 0
	m.geometryQualified = false
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
	var charge, copyCharge uint64
	if present {
		var ok bool
		charge, copyCharge, ok = managedSectorGeometryBytes(sector)
		if !ok {
			m.disableGeometryRecords()
			return
		}
		if prior != nil && prior.value.matchesSector(sector, charge, copyCharge) {
			return
		}
	}
	var oldCharge, oldCopyCharge uint64
	if prior != nil {
		oldCharge = prior.value.retainedBytes
		oldCopyCharge = prior.value.copyBytes
	}
	if m.geometryRetainedBytes < oldCharge || m.geometryCopyBytes < oldCopyCharge {
		m.disableGeometryRecords()
		return
	}
	total, ok := addManagedGeometryBytes(m.geometryRetainedBytes-oldCharge, charge)
	if !ok {
		m.disableGeometryRecords()
		return
	}
	copyTotal, ok := addManagedGeometryBytes(m.geometryCopyBytes-oldCopyCharge, copyCharge)
	if !ok {
		m.disableGeometryRecords()
		return
	}
	var record *managedSectorRecord
	if present {
		record = newManagedSectorRecord(sector, charge, copyCharge)
	}
	m.geometry = updateManagedIndex(m.geometry, key, record, present)
	m.geometryRetainedBytes = total
	m.geometryCopyBytes = copyTotal
}

// Payload and flags can change in place only on exclusive backing. Public
// capture and Fork clear that exclusivity, so later mutations of captured
// backing detach and change these references. Compare before allocating a new
// record/path; already-exclusive content edits keep the existing index intact.
func (r *managedSectorRecord) matchesSector(sector *Sector, retainedBytes, copyBytes uint64) bool {
	if r.coords != sector.Coords || r.mask != sector.BrickMask64 || r.count != len(sector.PackedBricks) || r.retainedBytes != retainedBytes || r.copyBytes != copyBytes {
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
