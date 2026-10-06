package gekko

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// These estimates count retained logical storage, not allocator/map buckets or
// payload-resolution temporaries. Arithmetic errors never become sole-oversize
// admissions. Coordinate grid products are capped before multiplication: sparse
// legacy records need not occupy or even fit their declared chunk lattice.
type streamedGeometryBoundMath struct{ err error }

func (m *streamedGeometryBoundMath) add(values ...int64) int64 {
	var total int64
	for _, v := range values {
		if v < 0 || v > math.MaxInt64-total {
			m.err = fmt.Errorf("streamed geometry charge overflow")
			return 0
		}
		total += v
	}
	return total
}
func (m *streamedGeometryBoundMath) mul(a, b int64) int64 {
	if a < 0 || b < 0 || (b != 0 && a > math.MaxInt64/b) {
		m.err = fmt.Errorf("streamed geometry charge overflow")
		return 0
	}
	return a * b
}
func streamedBoundCountAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
func streamedBoundFloor(v, unit int64) int64 {
	q := v / unit
	if v%unit < 0 {
		q--
	}
	return q
}

type streamedGeometryBoundRange struct {
	used   bool
	lo, hi [3]int64
}

func (r *streamedGeometryBoundRange) include(x, y, z int, unit int64) {
	point := [3]int64{streamedBoundFloor(int64(x), unit), streamedBoundFloor(int64(y), unit), streamedBoundFloor(int64(z), unit)}
	if !r.used {
		r.used = true
		r.lo = point
		r.hi = point
		return
	}
	for i, v := range point {
		r.lo[i] = min(r.lo[i], v)
		r.hi[i] = max(r.hi[i], v)
	}
}
func (r streamedGeometryBoundRange) cappedGrid(cap int64) int64 {
	if !r.used || cap == 0 {
		return 0
	}
	total := int64(1)
	for i := range r.lo {
		// Unit>=BrickSize makes the signed grid-coordinate span representable,
		// including min-int source coordinates. No min-int negation is used.
		span := r.hi[i] - r.lo[i] + 1
		if span > cap/total {
			return cap
		}
		total *= span
	}
	return min(cap, total)
}
func streamedRecordBounds(imported []content.ImportedWorldVoxelDef, snapshot []content.VoxelObjectVoxelDef) (int64, int64) {
	var bricks, sectors streamedGeometryBoundRange
	var count int64
	for _, v := range imported {
		if v.Value != 0 {
			count++
			bricks.include(v.X, v.Y, v.Z, volume.BrickSize)
			sectors.include(v.X, v.Y, v.Z, volume.SectorSize)
		}
	}
	for _, v := range snapshot {
		if v.Value != 0 {
			count++
			bricks.include(v.X, v.Y, v.Z, volume.BrickSize)
			sectors.include(v.X, v.Y, v.Z, volume.SectorSize)
		}
	}
	b := bricks.cappedGrid(count)
	return b, sectors.cappedGrid(b)
}
func streamedTerrainBounds(chunk *content.TerrainChunkDef) (int64, int64) {
	var bricks, sectors streamedGeometryBoundRange
	var b, s int64
	if chunk == nil || chunk.SolidValue == 0 {
		return 0, 0
	}
	for _, c := range chunk.Columns {
		if c.FilledVoxels <= 0 {
			continue
		}
		h := int64(c.FilledVoxels)
		b = streamedBoundCountAdd(b, h/volume.BrickSize+boolInt64(h%volume.BrickSize != 0))
		s = streamedBoundCountAdd(s, h/volume.SectorSize+boolInt64(h%volume.SectorSize != 0))
		bricks.include(c.X, 0, c.Z, volume.BrickSize)
		bricks.include(c.X, c.FilledVoxels-1, c.Z, volume.BrickSize)
		sectors.include(c.X, 0, c.Z, volume.SectorSize)
		sectors.include(c.X, c.FilledVoxels-1, c.Z, volume.SectorSize)
	}
	b = bricks.cappedGrid(b)
	return b, min(b, sectors.cappedGrid(s))
}
func (m *streamedGeometryBoundMath) auxRecords(aux *content.ImportedWorldChunkAuxDef) int64 {
	var bytes int64
	if aux != nil {
		for _, r := range aux.Records {
			if len(r.Bytes) == volume.VoxelAuxRecordBytes {
				bytes = m.add(bytes, streamedProxyAuxCopyCapacity)
			}
		}
	}
	return bytes
}
func (m *streamedGeometryBoundMath) mapBytes(b, s, revisions, aux int64) int64 {
	pointer := int64(unsafe.Sizeof((*volume.Brick)(nil)))
	return m.add(int64(unsafe.Sizeof(volume.XBrickMap{})), m.mul(s, int64(unsafe.Sizeof([3]int{}))+pointer),
		m.mul(revisions, int64(unsafe.Sizeof([3]int{}))+int64(unsafe.Sizeof(uint64(0)))),
		m.mul(s, int64(unsafe.Sizeof(volume.Sector{}))+int64(volume.SectorBricks*volume.SectorBricks*volume.SectorBricks)*pointer), m.mul(b, int64(unsafe.Sizeof(volume.Brick{}))), aux)
}
func (m *streamedGeometryBoundMath) haloBytes(b, s int64) int64 {
	axis := int64(1 + 2*((volume.VoxelNormalExtendedSurfaceFitRadius+volume.BrickSize-1)/volume.BrickSize))
	return m.add(m.mul(s, int64(unsafe.Sizeof([3]int{}))+int64(unsafe.Sizeof(false))), m.mul(m.mul(m.mul(axis, axis), axis), m.mul(b, int64(unsafe.Sizeof([6]int{}))+int64(unsafe.Sizeof(false)))))
}
func (m *streamedGeometryBoundMath) descriptor(b, s int64) int64 {
	pointer := int64(unsafe.Sizeof((*streamedGeometryStorageNode)(nil)))
	nodes := m.add(1, s, b)
	return m.add(int64(unsafe.Sizeof(streamedGeometryStorageDescriptor{})), m.mul(nodes, int64(unsafe.Sizeof(streamedGeometryStorageNode{}))+pointer), m.mul(m.mul(2, m.add(s, b)), pointer))
}
func (m *streamedGeometryBoundMath) coldImported(chunk *content.ImportedWorldChunkDef, aux *content.ImportedWorldChunkAuxDef, job streamedChunkLoadJob) int64 {
	if chunk == nil || chunk.NonEmptyVoxelCount == 0 {
		return 0
	}
	if chunk.EmbeddedAux != nil {
		aux = chunk.EmbeddedAux
	}
	b, s := streamedRecordBounds(chunk.Voxels, nil)
	clean := m.mapBytes(b, s, s, m.auxRecords(aux))
	total := m.add(clean, m.haloBytes(b, s), int64(unsafe.Sizeof(streamedGeometrySource{})))
	if !job.HasImportedWorldBacking {
		total = m.add(total, clean, m.descriptor(b, s), int64(unsafe.Sizeof(streamedGeometryRegistration{})))
	}
	return total
}
func (m *streamedGeometryBoundMath) otherGeometry(p streamedPreparedChunk, job streamedChunkLoadJob) int64 {
	var total int64
	if p.TerrainChunk != nil && p.TerrainChunk.NonEmptyVoxelCount > 0 {
		b, s := streamedTerrainBounds(p.TerrainChunk)
		clean := m.mapBytes(b, s, s, 0)
		total = m.add(total, clean, m.haloBytes(b, s), clean, int64(unsafe.Sizeof(streamedGeometryRegistration{})))
		if job.renderManaged {
			total = m.add(total, clean)
		}
	}
	for _, snapshot := range p.ObjectSnapshots {
		var records []content.VoxelObjectVoxelDef
		if snapshot != nil {
			records = snapshot.Voxels
		}
		b, s := streamedRecordBounds(nil, records)
		clean := m.mapBytes(b, s, s, 0)
		total = m.add(total, clean, m.haloBytes(b, s), clean, int64(unsafe.Sizeof(streamedGeometryRegistration{})))
	}
	return total
}
func streamedChunkPrebuildCharge(payload streamedPreparedChunk, job streamedChunkLoadJob) (int64, error) {
	return streamedChunkCapturedPrebuildCharge(payload, job, nil)
}
func streamedChunkCapturedPrebuildCharge(payload streamedPreparedChunk, job streamedChunkLoadJob, captured *streamedGeometrySource) (int64, error) {
	m := streamedGeometryBoundMath{}
	base := streamedPreparedChunkCharge(payload)
	if base == math.MaxInt64 {
		return 0, fmt.Errorf("streamed payload charge overflow")
	}
	imported := m.coldImported(payload.ImportedWorldChunk, payload.ImportedWorldAux, job)
	if captured != nil {
		imported = max(imported, m.capturedImported(captured, !job.HasImportedWorldBacking, job.compactPreparedGeometry && !job.HasImportedWorldBacking))
	}
	total := m.add(base, imported, m.otherGeometry(payload, job))
	return total, m.err
}

// Copy allocates per map edge / packed occurrence, even when the immutable
// source aliases identities. The source charge itself may deduplicate them.
// Fixed aux copies use the existing measured append capacity; other lengths use
// a capacity bound covering the allocator's minimum class and nil-append rounding.
func (m *streamedGeometryBoundMath) copiedAux(length, capacity int) int64 {
	if length == 0 {
		return int64(capacity)
	}
	if length == volume.VoxelAuxRecordBytes {
		return max(int64(capacity), streamedProxyAuxCopyCapacity)
	}
	return max(int64(capacity), max(8, m.mul(2, int64(length))))
}
func (m *streamedGeometryBoundMath) capturedImported(source *streamedGeometrySource, register, compactRequest bool) int64 {
	if source == nil {
		return 0
	}
	pointer := int64(unsafe.Sizeof((*volume.Brick)(nil)))
	var clean, dirty, b, s int64
	if source.dense != nil {
		mapData := source.dense
		clean = m.add(int64(unsafe.Sizeof(volume.XBrickMap{})), m.mul(int64(len(mapData.Sectors)), int64(unsafe.Sizeof([3]int{}))+pointer), m.mul(int64(len(mapData.SectorRevisions)), int64(unsafe.Sizeof([3]int{}))+int64(unsafe.Sizeof(uint64(0)))))
		for _, sector := range mapData.Sectors {
			if sector == nil {
				continue
			}
			s++
			clean = m.add(clean, int64(unsafe.Sizeof(*sector)), m.mul(int64(len(sector.PackedBricks)), pointer))
			for _, brick := range sector.PackedBricks {
				if brick != nil {
					b++
					clean = m.add(clean, int64(unsafe.Sizeof(*brick)), m.copiedAux(len(brick.PrecomputedAux), cap(brick.PrecomputedAux)))
				}
			}
		}
	} else if source.compact != nil {
		c := source.compact
		clean = m.add(int64(unsafe.Sizeof(volume.XBrickMap{})), m.mul(int64(len(c.sectors)), int64(unsafe.Sizeof([3]int{}))+pointer), m.mul(int64(len(c.revisions)), int64(unsafe.Sizeof([3]int{}))+int64(unsafe.Sizeof(uint64(0)))))
		dirty = m.add(m.mul(int64(len(c.dirtySectors)), int64(unsafe.Sizeof([3]int{}))+int64(unsafe.Sizeof(false))), m.mul(int64(len(c.dirtyBricks)), int64(unsafe.Sizeof([6]int{}))+int64(unsafe.Sizeof(false))))
		for _, sector := range c.sectors {
			if sector.nilSector {
				continue
			}
			s++
			clean = m.add(clean, int64(unsafe.Sizeof(volume.Sector{})), m.mul(int64(len(sector.bricks)), pointer))
			for _, brick := range sector.bricks {
				if !brick.nilBrick {
					b++
					clean = m.add(clean, int64(unsafe.Sizeof(volume.Brick{})), m.copiedAux(len(brick.aux), cap(brick.aux)))
				}
			}
		}
	} else {
		m.err = fmt.Errorf("invalid captured streamed geometry source")
		return 0
	}
	retained := source.charge()
	if retained == math.MaxInt64 {
		m.err = fmt.Errorf("captured geometry charge overflow")
		return 0
	}
	total := retained
	if source.compact != nil && !compactRequest {
		total = m.add(total, clean, dirty)
	}
	if register {
		total = m.add(total, clean, m.descriptor(b, s), int64(unsafe.Sizeof(streamedGeometryRegistration{})))
	}
	return total
}
func streamedProxyCapturedPrebuildCharge(payload streamedPreparedSectorProxy, inputBound int64, captured *streamedGeometrySource, compactRequest bool) (int64, error) {
	m := streamedGeometryBoundMath{}
	base := streamedPreparedProxyCharge(payload)
	if base == math.MaxInt64 {
		return 0, fmt.Errorf("streamed proxy payload charge overflow")
	}
	actual := m.add(base, m.capturedImported(captured, true, compactRequest))
	return max(inputBound, actual), m.err
}
