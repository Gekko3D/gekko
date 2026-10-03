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
	current *XBrickMap
	base    *XBrickMap
	changes map[[3]int]uint8
	// Publication revisions also include auxiliary-only fitted-normal halos.
	// Public SectorRevisions continue to describe dense voxel writes only.
	publicationRevisions map[[3]int]uint64
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
	return &ManagedXBrickMap{current: shareManagedMap(base), base: base}
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
	defer batch.finish(m.current)
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
	var old *Brick
	if sector := m.current.Sectors[sKey]; sector != nil {
		old = sector.GetBrick(bKey[3], bKey[4], bKey[5])
	}
	m.current.setVoxel(w.X, w.Y, w.Z, w.Value, batch)
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
	_, original := m.base.GetVoxel(w.X, w.Y, w.Z)
	key := [3]int{w.X, w.Y, w.Z}
	if w.Value == original {
		delete(m.changes, key)
	} else {
		if m.changes == nil {
			m.changes = make(map[[3]int]uint8)
		}
		m.changes[key] = w.Value
	}
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
		publicationRevisions: maps.Clone(m.publicationRevisions),
	}
	m.exclusive = nil
	return child
}

// Snapshot returns an independent ordinary map with the existing Copy contract,
// including fresh identity, preserved CPU data and reset GPU editing state.
func (m *ManagedXBrickMap) Snapshot() *XBrickMap {
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
		result := m.current.Copy()
		result.ClearDirty()
		return result
	}
	result := NewXBrickMap()
	for key, sector := range m.current.Sectors {
		prior := previous.Sectors[key]
		if prior != nil && m.publicationRevisions[key] <= sinceRevision {
			result.Sectors[key] = prior
		} else {
			result.Sectors[key] = sector.Copy()
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
		m.publicationRevisions = nil
		m.exclusive = nil
	}
	return m.current
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
