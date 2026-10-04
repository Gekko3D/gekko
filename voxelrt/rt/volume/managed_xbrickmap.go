package volume

import (
	"iter"
	"maps"
	"slices"
)

// ManagedXBrickMap owns opt-in sealed geometry and final voxel assignments
// relative to its construction base. Each owner requires exclusive access;
// independent forks may be edited concurrently. Dense brick payloads remain
// unchanged, but sealed owners share them until a write requires detachment.
type ManagedXBrickMap struct {
	current                      *XBrickMap
	base                         *XBrickMap
	changes                      map[[3]int]uint8
	changedBrickCounts           map[[6]int]int
	brickVoxelCounts             map[[6]int]int
	staleSolid                   map[[6]int]bool
	currentBricks, currentVoxels int
	// Publication revisions also include auxiliary-only fitted-normal halos.
	// Public SectorRevisions continue to describe dense voxel writes only.
	publicationRevisions map[[3]int]uint64
	// Immutable coordinate-only membership, shared by captures and sealed forks.
	topology          *managedTopologyNode
	geometry          *managedIndexNode[*managedSectorRecord]
	geometryQualified bool
	// Only these brick pointers are exclusively owned. Fork clears this set
	// because every current brick becomes shared, including previous edits.
	exclusive map[*Brick]struct{}
}

// NewManagedXBrickMap defensively copies source, including auxiliary data and
// revision tombstones. A nil source constructs empty sealed geometry. Source
// must remain exclusively owned by the caller during construction.
func NewManagedXBrickMap(source *XBrickMap) *ManagedXBrickMap {
	var base *XBrickMap
	if source == nil {
		base = NewXBrickMap()
	} else {
		base = source.Copy()
	}
	owner := &ManagedXBrickMap{current: shareManagedMap(base), base: base}
	owner.seedGeometryCounts()
	owner.seedTopology()
	owner.seedGeometryRecords()
	return owner
}

// NewManagedXBrickMapWithBase defensively copies independent original and
// current geometry, preserving the current Copy metadata and auxiliary data.
// Final primary assignments are computed once without invoking edit mutators.
// Both sources require exclusive access during construction; nil means empty.
func NewManagedXBrickMapWithBase(base, current *XBrickMap) *ManagedXBrickMap {
	if base == nil {
		base = NewXBrickMap()
	} else {
		base = base.Copy()
	}
	if current == nil {
		current = NewXBrickMap()
	} else {
		current = current.Copy()
	}
	owner := &ManagedXBrickMap{base: base, current: current}
	visit := func(source, other *XBrickMap, removals bool) {
		for key, sector := range source.Sectors {
			for i := 0; i < 64; i++ {
				brick := sector.GetBrick(i%4, i/4%4, i/16)
				if brick == nil {
					continue
				}
				for x := 0; x < BrickSize; x++ {
					for y := 0; y < BrickSize; y++ {
						for z := 0; z < BrickSize; z++ {
							value := brick.VoxelValue(x, y, z)
							if value == 0 {
								continue
							}
							coord := [3]int{key[0]*SectorSize + i%4*BrickSize + x, key[1]*SectorSize + i/4%4*BrickSize + y, key[2]*SectorSize + i/16*BrickSize + z}
							_, counterpart := other.GetVoxel(coord[0], coord[1], coord[2])
							if value == counterpart || removals && counterpart != 0 {
								continue
							}
							if owner.changes == nil {
								owner.changes = make(map[[3]int]uint8)
							}
							if removals {
								owner.changes[coord] = 0
							} else {
								owner.changes[coord] = value
							}
						}
					}
				}
			}
		}
	}
	visit(current, base, false)
	visit(base, current, true)
	for coord := range owner.changes {
		_, key := sectorBrickKeyForVoxel(coord[0], coord[1], coord[2])
		owner.addChangedBrickAssignment(key)
	}
	owner.seedGeometryCounts()
	owner.seedTopology()
	owner.seedGeometryRecords()
	return owner
}

// Logical counts follow dense primary bytes, independently of compression flags
// and occupancy caches. Stale Solid flags require rare target-brick repair after
// existing dense mutations, which can expand AtlasOffset into implicit cells.
func managedBrickGeometry(brick *Brick) (int, bool) {
	if brick == nil {
		return 0, false
	}
	count, uniform := 0, brick.AtlasOffset > 0 && brick.AtlasOffset <= 255
	for x := 0; x < BrickSize; x++ {
		for y := 0; y < BrickSize; y++ {
			for z := 0; z < BrickSize; z++ {
				value := brick.VoxelValue(x, y, z)
				if value != 0 {
					count++
				}
				if uint32(value) != brick.AtlasOffset {
					uniform = false
				}
			}
		}
	}
	return count, brick.Flags&BrickFlagSolid != 0 && (count != BrickSize*BrickSize*BrickSize || !uniform)
}

func (m *ManagedXBrickMap) setBrickGeometryCount(key [6]int, count int) {
	previous := m.brickVoxelCounts[key]
	m.currentVoxels += count - previous
	if previous == 0 && count > 0 {
		m.currentBricks++
	}
	if previous > 0 && count == 0 {
		m.currentBricks--
	}
	if count == 0 {
		delete(m.brickVoxelCounts, key)
		return
	}
	if m.brickVoxelCounts == nil {
		m.brickVoxelCounts = make(map[[6]int]int)
	}
	m.brickVoxelCounts[key] = count
}

func (m *ManagedXBrickMap) setStaleSolid(key [6]int, stale bool) {
	if !stale {
		delete(m.staleSolid, key)
		return
	}
	if m.staleSolid == nil {
		m.staleSolid = make(map[[6]int]bool)
	}
	m.staleSolid[key] = true
}

func (m *ManagedXBrickMap) seedGeometryCounts() {
	for sectorKey, sector := range m.current.Sectors {
		for i := 0; i < 64; i++ {
			key := [6]int{sectorKey[0], sectorKey[1], sectorKey[2], i % 4, i / 4 % 4, i / 16}
			count, stale := managedBrickGeometry(sector.GetBrick(key[3], key[4], key[5]))
			m.setBrickGeometryCount(key, count)
			m.setStaleSolid(key, stale)
		}
	}
}

func (m *ManagedXBrickMap) reconcileManagedBrick(key [6]int, brick *Brick) {
	count := 0
	uniform := brick != nil && brick.AtlasOffset > 0 && brick.AtlasOffset <= 255
	for x := 0; x < BrickSize; x++ {
		for y := 0; y < BrickSize; y++ {
			for z := 0; z < BrickSize; z++ {
				coord := [3]int{key[0]*SectorSize + key[3]*BrickSize + x, key[1]*SectorSize + key[4]*BrickSize + y, key[2]*SectorSize + key[5]*BrickSize + z}
				var value uint8
				if brick != nil {
					value = brick.VoxelValue(x, y, z)
					if uint32(value) != brick.AtlasOffset {
						uniform = false
					}
				}
				if value != 0 {
					count++
				}
				m.trackManagedAssignment(coord, value)
			}
		}
	}
	m.setBrickGeometryCount(key, count)
	m.setStaleSolid(key, brick != nil && brick.Flags&BrickFlagSolid != 0 && (count != BrickSize*BrickSize*BrickSize || !uniform))
}

func (m *ManagedXBrickMap) trackManagedAssignment(coord [3]int, value uint8) {
	_, original := m.base.GetVoxel(coord[0], coord[1], coord[2])
	_, tracked := m.changes[coord]
	_, brick := sectorBrickKeyForVoxel(coord[0], coord[1], coord[2])
	if value == original {
		if tracked {
			delete(m.changes, coord)
			if m.changedBrickCounts[brick] == 1 {
				delete(m.changedBrickCounts, brick)
			} else {
				m.changedBrickCounts[brick]--
			}
		}
		return
	}
	if m.changes == nil {
		m.changes = make(map[[3]int]uint8)
	}
	if !tracked {
		m.addChangedBrickAssignment(brick)
	}
	m.changes[coord] = value
}

// CurrentGeometryCounts reports current nonempty bricks and primary voxels
// without allocation. Exposed owners permanently disable this sealed metadata.
func (m *ManagedXBrickMap) CurrentGeometryCounts() (int, int, bool) {
	if m.base == nil {
		return 0, 0, false
	}
	return m.currentBricks, m.currentVoxels, true
}

// shareManagedMap shares only bricks. Sector headers, pointer slices and all
// mutable map metadata belong to the returned map, with a fresh identity.
func shareManagedMap(source *XBrickMap) *XBrickMap {
	result := NewXBrickMap()
	id := result.ID
	*result = *source
	result.ID = id
	result.GPUEditMode = false
	result.gpuManager = nil
	result.Sectors = make(map[[3]int]*Sector, len(source.Sectors))
	for key, sector := range source.Sectors {
		header := *sector
		header.PackedBricks = slices.Clone(sector.PackedBricks)
		result.Sectors[key] = &header
	}
	result.DirtySectors = maps.Clone(source.DirtySectors)
	result.DirtyBricks = maps.Clone(source.DirtyBricks)
	result.SectorRevisions = maps.Clone(source.SectorRevisions)
	return result
}

// GetVoxel reads the current authoritative dense payload.
func (m *ManagedXBrickMap) GetVoxel(x, y, z int) (bool, uint8) {
	return m.current.GetVoxel(x, y, z)
}

// SetVoxel synchronously applies one assignment using ordinary dense semantics.
func (m *ManagedXBrickMap) SetVoxel(x, y, z int, value uint8) {
	m.setVoxel(VoxelWrite{X: x, Y: y, Z: z, Value: value}, nil)
}

// ApplyVoxelWrites consumes the sequence once in order; nil is a no-op. The
// producer may read applied payloads through GetVoxel, but must not mutate,
// reenter, fork, expose or publish the owner during the call. Material flags
// finalize on return, including the applied prefix when a producer panics.
func (m *ManagedXBrickMap) ApplyVoxelWrites(writes iter.Seq[VoxelWrite]) {
	if m.base == nil {
		m.current.ApplyVoxelWrites(writes)
		return
	}
	if writes == nil {
		return
	}
	var batch voxelEditBatch
	defer m.finishManagedBatch(&batch)
	for w := range writes {
		m.setVoxel(w, &batch)
	}
}

func (m *ManagedXBrickMap) setVoxel(w VoxelWrite, batch *voxelEditBatch) {
	if m.base == nil {
		m.current.SetVoxel(w.X, w.Y, w.Z, w.Value)
		return
	}
	_, previous := m.current.GetVoxel(w.X, w.Y, w.Z)
	if previous == w.Value {
		return
	}
	// Dense edits invalidate normal auxiliary data throughout the fitted halo,
	// so neighboring bricks must detach before the existing mutator runs too.
	m.detachVoxelHalo(w.X, w.Y, w.Z)
	sKey, bKey := sectorBrickKeyForVoxel(w.X, w.Y, w.Z)
	stale := m.staleSolid[bKey]
	_, sectorWasPresent := m.current.Sectors[sKey]
	var old *Brick
	if sector := m.current.Sectors[sKey]; sector != nil {
		old = sector.GetBrick(bKey[3], bKey[4], bKey[5])
	}
	m.current.setVoxel(w.X, w.Y, w.Z, w.Value, batch)
	m.reconcileTopology(sKey, sectorWasPresent)
	m.markPublicationHalo(w)
	var brick *Brick
	if sector := m.current.Sectors[sKey]; sector != nil {
		brick = sector.GetBrick(bKey[3], bKey[4], bKey[5])
	}
	if old != brick {
		delete(m.exclusive, old)
	}
	if brick != nil {
		m.ownBrick(brick)
	}
	// Stale occupancy metadata can make the dense mutator remove a whole
	// non-Solid brick. Reconcile its implicit removals only on that rare path.
	if stale || brick == nil && m.brickVoxelCounts[bKey] > 1 {
		m.reconcileManagedBrick(bKey, brick)
	} else {
		_, actual := m.current.GetVoxel(w.X, w.Y, w.Z)
		count := m.brickVoxelCounts[bKey]
		if previous == 0 && actual != 0 {
			count++
		}
		if previous != 0 && actual == 0 {
			count--
		}
		m.setBrickGeometryCount(bKey, count)
		m.trackManagedAssignment([3]int{w.X, w.Y, w.Z}, actual)
	}
	m.refreshGeometryHalo(w)
}

func (m *ManagedXBrickMap) markPublicationHalo(w VoxelWrite) {
	if m.publicationRevisions == nil {
		m.publicationRevisions = make(map[[3]int]uint64)
	}
	minKey, _ := sectorBrickKeyForVoxel(w.X-VoxelNormalExtendedSurfaceFitRadius, w.Y-VoxelNormalExtendedSurfaceFitRadius, w.Z-VoxelNormalExtendedSurfaceFitRadius)
	maxKey, _ := sectorBrickKeyForVoxel(w.X+VoxelNormalExtendedSurfaceFitRadius, w.Y+VoxelNormalExtendedSurfaceFitRadius, w.Z+VoxelNormalExtendedSurfaceFitRadius)
	for x := minKey[0]; x <= maxKey[0]; x++ {
		for y := minKey[1]; y <= maxKey[1]; y++ {
			for z := minKey[2]; z <= maxKey[2]; z++ {
				m.publicationRevisions[[3]int{x, y, z}] = m.current.Revision
			}
		}
	}
}

func (m *ManagedXBrickMap) ownBrick(brick *Brick) {
	if m.exclusive == nil {
		m.exclusive = make(map[*Brick]struct{})
	}
	m.exclusive[brick] = struct{}{}
}

func (m *ManagedXBrickMap) detachBrick(sector *Sector, index int) {
	brick := sector.PackedBricks[index]
	if _, owned := m.exclusive[brick]; owned {
		return
	}
	copy := brick.Copy()
	sector.PackedBricks[index] = copy
	m.ownBrick(copy)
}

func (m *ManagedXBrickMap) detachVoxelHalo(x, y, z int) {
	// Always protect the target, including integer-edge coordinates where the
	// existing fitted-halo arithmetic can wrap before enumerating neighbors.
	sKey, key := sectorBrickKeyForVoxel(x, y, z)
	if sector := m.current.Sectors[sKey]; sector != nil {
		flat := key[3] + key[4]*SectorBricks + key[5]*SectorBricks*SectorBricks
		if sector.BrickMask64&(uint64(1)<<flat) != 0 {
			m.detachBrick(sector, sector.GetPackedIndex(flat))
		}
	}
	_, minKey := sectorBrickKeyForVoxel(x-VoxelNormalExtendedSurfaceFitRadius, y-VoxelNormalExtendedSurfaceFitRadius, z-VoxelNormalExtendedSurfaceFitRadius)
	_, maxKey := sectorBrickKeyForVoxel(x+VoxelNormalExtendedSurfaceFitRadius, y+VoxelNormalExtendedSurfaceFitRadius, z+VoxelNormalExtendedSurfaceFitRadius)
	for bx := minKey[0]*SectorBricks + minKey[3]; bx <= maxKey[0]*SectorBricks+maxKey[3]; bx++ {
		for by := minKey[1]*SectorBricks + minKey[4]; by <= maxKey[1]*SectorBricks+maxKey[4]; by++ {
			for bz := minKey[2]*SectorBricks + minKey[5]; bz <= maxKey[2]*SectorBricks+maxKey[5]; bz++ {
				sKey, key := sectorBrickKeyForVoxel(bx*BrickSize, by*BrickSize, bz*BrickSize)
				if sector := m.current.Sectors[sKey]; sector != nil {
					flat := key[3] + key[4]*SectorBricks + key[5]*SectorBricks*SectorBricks
					if sector.BrickMask64&(uint64(1)<<flat) != 0 {
						m.detachBrick(sector, sector.GetPackedIndex(flat))
					}
				}
			}
		}
	}
}

// Fork shares sealed bricks and inherits the original base and current changes.
// Forking an exposed owner instead defensively seals its current raw geometry
// as a fresh base; the raw map requires exclusive access during that copy.
func (m *ManagedXBrickMap) Fork() *ManagedXBrickMap {
	if m.base == nil {
		return NewManagedXBrickMap(m.current)
	}
	child := &ManagedXBrickMap{
		current:              shareManagedMap(m.current),
		base:                 m.base,
		changes:              maps.Clone(m.changes),
		changedBrickCounts:   maps.Clone(m.changedBrickCounts),
		publicationRevisions: maps.Clone(m.publicationRevisions),
		topology:             m.topology,
		geometry:             m.geometry,
		geometryQualified:    m.geometryQualified,
		brickVoxelCounts:     maps.Clone(m.brickVoxelCounts),
		staleSolid:           maps.Clone(m.staleSolid),
		currentBricks:        m.currentBricks, currentVoxels: m.currentVoxels,
	}
	m.exclusive = nil
	return child
}

// Snapshot returns an independent ordinary map with the existing Copy contract,
// including fresh identity, preserved CPU data and reset GPU editing state.
func (m *ManagedXBrickMap) Snapshot() *XBrickMap {
	if m.geometryQualified {
		return m.copyGeometrySnapshot()
	}
	return m.current.Copy()
}

// CopyChangedSectors publishes a clean immutable snapshot with fresh identity.
// Previous must be an unchanged snapshot of this owner at sinceRevision, or a
// matching inherited snapshot from before fork divergence. Divergent sibling
// snapshots are invalid inputs. Only previous immutable sectors are shared;
// none are shared with current/base storage. Snapshot stays independently mutable.
// Publishing during an ordered producer is unsupported. Exposed owners always
// copy fully because supported raw writes can bypass revision notifications.
func (m *ManagedXBrickMap) CopyChangedSectors(previous *XBrickMap, sinceRevision uint64) *XBrickMap {
	if previous == nil || m.base == nil {
		result := m.Snapshot()
		result.ClearDirty()
		return result
	}
	result := NewXBrickMap()
	if m.geometryQualified {
		walkManagedIndex(m.geometry, func(key [3]int, record *managedSectorRecord) {
			prior := previous.Sectors[key]
			if prior != nil && m.publicationRevisions[key] <= sinceRevision {
				result.Sectors[key] = prior
			} else {
				result.Sectors[key] = record.copySector()
			}
		})
	} else {
		for key, sector := range m.current.Sectors {
			prior := previous.Sectors[key]
			if prior != nil && m.publicationRevisions[key] <= sinceRevision {
				result.Sectors[key] = prior
			} else {
				result.Sectors[key] = sector.Copy()
			}
		}
	}
	result.CachedMin, result.CachedMax = m.current.CachedMin, m.current.CachedMax
	result.AABBDirty = m.current.AABBDirty
	result.Revision = m.current.Revision
	result.SectorRevisions = maps.Clone(m.current.SectorRevisions)
	result.ClearDirty()
	return result
}

// ExposeMutable irreversibly promotes this owner to raw dense authority. All
// shared bricks and auxiliary bytes detach first. Subsequent exposures return
// the same pointer, and managed writes continue to mutate that authoritative map.
func (m *ManagedXBrickMap) ExposeMutable() *XBrickMap {
	if m.base != nil {
		for _, sector := range m.current.Sectors {
			for index := range sector.PackedBricks {
				m.detachBrick(sector, index)
			}
		}
		m.base = nil
		m.changes = nil
		m.changedBrickCounts = nil
		m.brickVoxelCounts, m.staleSolid = nil, nil
		m.currentBricks, m.currentVoxels = 0, 0
		m.publicationRevisions = nil
		m.topology = nil
		m.geometry = nil
		m.geometryQualified = false
		m.exclusive = nil
	}
	return m.current
}

// TrackedChangeCount reports final assignment count without allocation.
func (m *ManagedXBrickMap) TrackedChangeCount() (int, bool) {
	if m.base == nil {
		return 0, false
	}
	return len(m.changes), true
}

// VisitTrackedChanges visits unordered final assignments without allocation.
// A false callback stops traversal; the result still reports availability.
// The single owner must not mutate, expose, or reenter during this callback.
func (m *ManagedXBrickMap) VisitTrackedChanges(visit func(VoxelWrite) bool) bool {
	if m.base == nil {
		return false
	}
	for key, value := range m.changes {
		if !visit(VoxelWrite{X: key[0], Y: key[1], Z: key[2], Value: value}) {
			break
		}
	}
	return true
}

// TrackedChanges returns independently owned final assignments relative to the
// original base, ordered by z, y, then x. Zero is an explicit removal. Reverts
// are omitted. An exposed owner returns (nil, false) permanently.
func (m *ManagedXBrickMap) TrackedChanges() ([]VoxelWrite, bool) {
	if m.base == nil {
		return nil, false
	}
	var result []VoxelWrite
	for key, value := range m.changes {
		result = append(result, VoxelWrite{X: key[0], Y: key[1], Z: key[2], Value: value})
	}
	slices.SortFunc(result, func(a, b VoxelWrite) int {
		for _, pair := range [3][2]int{{a.Z, b.Z}, {a.Y, b.Y}, {a.X, b.X}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
	return result, true
}
