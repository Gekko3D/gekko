package gekko

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// A sealed worker snapshot. Pending envelopes retain this representation alone,
// not the cache/build coordinator used by compatibility getters to promote it.
type streamedGeometrySource struct {
	dense     *volume.XBrickMap
	compact   *streamedCompactGeometry
	qualified bool
	// Dense generic exposure permits public raw writes. The mark belongs to
	// the source, so eviction/readmission cannot make an old copy adoptable.
	exposed atomic.Bool
}

type streamedGeometryPromotion struct {
	once       sync.Once
	dense      *volume.XBrickMap
	panicValue any
}

type streamedCompactGeometry struct {
	sectors                   []streamedCompactSector
	revisions                 map[[3]int]uint64
	dirtySectors              map[[3]int]bool
	dirtyBricks               map[[6]int]bool
	revision                  uint64
	min, max                  mgl32.Vec3
	aabbDirty, structureDirty bool
	voxelCount                int
}

type streamedCompactSector struct {
	key, coords [3]int
	mask        uint64
	bricks      []streamedCompactBrick
	nilSector   bool
}

type streamedCompactBrick struct {
	occupancy    [8]uint64 // Exact authoritative cells, independent of renderer metadata.
	values       []uint8
	aux          []byte
	mask         uint64
	flags, atlas uint32
	uniform      uint8
	nilBrick     bool
}

func newStreamedGeometrySource(dense *volume.XBrickMap, qualify bool) *streamedGeometrySource {
	if dense == nil {
		return nil
	}
	source := &streamedGeometrySource{dense: dense, qualified: qualify}
	source.exposed.Store(!qualify)
	if !qualify {
		return source
	}
	compact := &streamedCompactGeometry{
		revisions:    cloneStreamedGeometryMap(dense.SectorRevisions),
		dirtySectors: cloneStreamedGeometryMap(dense.DirtySectors), dirtyBricks: cloneStreamedGeometryMap(dense.DirtyBricks),
		revision: dense.Revision, min: dense.CachedMin, max: dense.CachedMax,
		aabbDirty: dense.AABBDirty, structureDirty: dense.StructureDirty, voxelCount: dense.GetVoxelCount(),
		sectors: make([]streamedCompactSector, 0, len(dense.Sectors)),
	}
	for key, sector := range dense.Sectors {
		s := streamedCompactSector{key: key, nilSector: sector == nil}
		if sector != nil {
			s.coords, s.mask = sector.Coords, sector.BrickMask64
			s.bricks = make([]streamedCompactBrick, len(sector.PackedBricks))
			for i, brick := range sector.PackedBricks {
				b := &s.bricks[i]
				if brick == nil {
					b.nilBrick = true
					continue
				}
				b.mask, b.flags, b.atlas = brick.OccupancyMask64, brick.Flags, brick.AtlasOffset
				if brick.PrecomputedAux != nil {
					b.aux = append(make([]byte, 0, len(brick.PrecomputedAux)), brick.PrecomputedAux...)
				}
				var values [volume.BrickSize * volume.BrickSize * volume.BrickSize]uint8
				count, uniform, mixed := 0, uint8(0), false
				for z := 0; z < volume.BrickSize; z++ {
					for y := 0; y < volume.BrickSize; y++ {
						for x := 0; x < volume.BrickSize; x++ {
							value := brick.VoxelValue(x, y, z)
							if value == 0 {
								continue
							}
							linear := x + y*volume.BrickSize + z*volume.BrickSize*volume.BrickSize
							b.occupancy[linear/64] |= uint64(1) << uint(linear%64)
							values[count] = value
							count++
							if uniform == 0 {
								uniform = value
							} else if uniform != value {
								mixed = true
							}
						}
					}
				}
				if mixed {
					b.values = append([]uint8(nil), values[:count]...)
				} else {
					b.uniform = uniform
				}
			}
		}
		compact.sectors = append(compact.sectors, s)
	}
	source.compact, source.dense = compact, nil
	if source.charge() >= runtimeContentChargeSum(streamedPendingGeometryCharge(dense), int64(unsafe.Sizeof(*source))) {
		source.compact, source.dense = nil, dense
	}
	return source
}

func cloneStreamedGeometryMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (s *streamedGeometrySource) count() int {
	if s == nil {
		return 0
	}
	if s.dense != nil {
		return s.dense.GetVoxelCount()
	}
	return s.compact.voxelCount
}

func (s *streamedGeometrySource) charge() int64 {
	if s == nil {
		return 0
	}
	if s.dense != nil {
		return runtimeContentChargeSum(streamedPendingGeometryCharge(s.dense), int64(unsafe.Sizeof(*s)))
	}
	c := s.compact
	charge := runtimeContentChargeSum(int64(unsafe.Sizeof(*s)), int64(unsafe.Sizeof(*c)),
		runtimeContentChargeProduct(int64(cap(c.sectors)), int64(unsafe.Sizeof(streamedCompactSector{}))),
		runtimeContentChargeProduct(int64(len(c.revisions)), int64(unsafe.Sizeof([3]int{})+unsafe.Sizeof(uint64(0)))),
		runtimeContentChargeProduct(int64(len(c.dirtySectors)), int64(unsafe.Sizeof([3]int{})+unsafe.Sizeof(false))),
		runtimeContentChargeProduct(int64(len(c.dirtyBricks)), int64(unsafe.Sizeof([6]int{})+unsafe.Sizeof(false))))
	for _, sector := range c.sectors {
		charge = runtimeContentChargeSum(charge, runtimeContentChargeProduct(int64(cap(sector.bricks)), int64(unsafe.Sizeof(streamedCompactBrick{}))))
		for _, brick := range sector.bricks {
			charge = runtimeContentChargeSum(charge, int64(cap(brick.values)), int64(cap(brick.aux)))
		}
	}
	return charge
}

// Reconstruct source state exactly for generic exposure; registration then uses
// the established Copy/bounds/clean contract rather than changing that state.
func (s *streamedGeometrySource) materialize() *volume.XBrickMap {
	if s == nil {
		return nil
	}
	if s.dense != nil {
		return s.dense
	}
	c := s.compact
	dense := volume.NewXBrickMap()
	dense.Revision, dense.SectorRevisions = c.revision, cloneStreamedGeometryMap(c.revisions)
	dense.DirtySectors, dense.DirtyBricks = cloneStreamedGeometryMap(c.dirtySectors), cloneStreamedGeometryMap(c.dirtyBricks)
	dense.CachedMin, dense.CachedMax, dense.AABBDirty, dense.StructureDirty = c.min, c.max, c.aabbDirty, c.structureDirty
	for _, s := range c.sectors {
		if s.nilSector {
			dense.Sectors[s.key] = nil
			continue
		}
		sector := &volume.Sector{Coords: s.coords, BrickMask64: s.mask, PackedBricks: make([]*volume.Brick, len(s.bricks))}
		for i, b := range s.bricks {
			if b.nilBrick {
				continue
			}
			brick := &volume.Brick{OccupancyMask64: b.mask, Flags: b.flags, AtlasOffset: b.atlas}
			if b.aux != nil {
				brick.PrecomputedAux = append(make([]byte, 0, len(b.aux)), b.aux...)
			}
			next := 0
			for linear := 0; linear < volume.BrickSize*volume.BrickSize*volume.BrickSize; linear++ {
				if b.occupancy[linear/64]&(uint64(1)<<uint(linear%64)) == 0 {
					continue
				}
				value := b.uniform
				if value == 0 {
					value = b.values[next]
					next++
				}
				brick.Payload[linear%volume.BrickSize][linear/volume.BrickSize%volume.BrickSize][linear/(volume.BrickSize*volume.BrickSize)] = value
			}
			sector.PackedBricks[i] = brick
		}
		dense.Sectors[s.key] = sector
	}
	return dense
}

func (s *streamedGeometrySource) registrationCopy() *volume.XBrickMap {
	if s == nil {
		return nil
	}
	if s.dense != nil {
		return s.dense.Copy()
	}
	copy := s.materialize()
	// Copy starts with fresh upload work and does not inherit dirty marker maps.
	copy.DirtySectors, copy.DirtyBricks = make(map[[3]int]bool), make(map[[6]int]bool)
	copy.StructureDirty = true
	return copy
}
