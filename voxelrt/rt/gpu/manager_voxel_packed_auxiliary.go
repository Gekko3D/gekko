package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/bits"
	"sort"
	"time"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// SetPackedVoxelNormals selects the auxiliary wire layout before geometry owns
// any allocations. The dense default and same-value calls remain compatible.
func (m *GpuBufferManager) SetPackedVoxelNormals(enabled bool) error {
	if m == nil {
		return errors.New("nil voxel GPU manager")
	}
	if m.packedVoxelNormals == enabled {
		return nil
	}
	if len(m.Allocations) != 0 || len(m.SectorToInfo) != 0 || m.SectorAlloc.Tail != 0 || m.BrickAlloc.Tail != 0 || m.brickPackedLeased || m.VoxelAuxAlloc.Tail != 0 || len(m.BrickToAuxSlot) != 0 || m.auxiliaryPackedLeased || m.voxelGrowth != nil {
		return errors.New("packed voxel normals must be selected before geometry, auxiliary allocation or staged growth")
	}
	m.packedVoxelNormals = enabled
	return nil
}

type auxiliaryPacketLocation struct {
	sector *volume.Sector
	index  int
}
type auxiliaryPacketLease struct{ base, words uint32 }
type retiredAuxiliaryRange struct {
	lease      auxiliaryPacketLease
	queue      *wgpu.Queue
	submission uint64
}

func (m *GpuBufferManager) ensureAuxiliaryWordRanges() bool {
	prefix := uint64(m.VoxelAuxAlloc.Tail) * volume.VoxelAuxWordCount
	if !m.auxiliaryRangesInitialized {
		for _, slot := range m.BrickToAuxSlot {
			prefix = max(prefix, (uint64(slot)+1)*volume.VoxelAuxWordCount)
		}
		m.auxiliaryLegacyPrefix = prefix
		m.auxiliaryRanges.tail = prefix
		m.auxiliaryRangesInitialized = true
	}
	if prefix > m.auxiliaryLegacyPrefix {
		if m.auxiliaryPackedLeased {
			return false
		}
		m.auxiliaryLegacyPrefix = prefix
		m.auxiliaryRanges.tail = max(m.auxiliaryRanges.tail, prefix)
	}
	return prefix <= uint64(^uint32(0))
}
func (m *GpuBufferManager) retireAuxiliaryPacket(lease auxiliaryPacketLease) {
	if lease.words != 0 {
		m.retiredAuxiliaryRanges = append(m.retiredAuxiliaryRanges, retiredAuxiliaryRange{lease: lease})
	}
}
func (m *GpuBufferManager) advanceRetiredAuxiliaryRanges() {
	out := m.retiredAuxiliaryRanges[:0]
	for _, r := range m.retiredAuxiliaryRanges {
		done := false
		if r.queue != nil {
			if poll, ok := m.voxelNative.(interface {
				SubmissionComplete(*wgpu.Queue, uint64) bool
			}); ok {
				done = poll.SubmissionComplete(r.queue, r.submission)
			} else if m.Device != nil {
				done = m.Device.Poll(false, &wgpu.WrappedSubmissionIndex{Queue: r.queue, SubmissionIndex: wgpu.SubmissionIndex(r.submission)})
			}
		}
		if done {
			m.auxiliaryRanges.release(r.lease.base, r.lease.words)
		} else {
			out = append(out, r)
		}
	}
	clear(m.retiredAuxiliaryRanges[len(out):])
	m.retiredAuxiliaryRanges = out
}
func (m *GpuBufferManager) releaseSectorAuxiliaryPackets(sector *volume.Sector) {
	for index := 0; index < 64; index++ {
		location := auxiliaryPacketLocation{sector, index}
		if receipt, ok := m.auxiliaryPacketReceipts[location]; ok {
			m.retireAuxiliaryPacket(receipt)
			delete(m.auxiliaryPacketReceipts, location)
		}
		delete(m.plannedAuxiliaryRanges, location)
	}
}

// Wire occupancy comes only from the canonical dense auxiliary header. Valid
// authored auxiliary bytes remain authoritative even when raw payload differs.
func packedAuxiliaryWordCount(brick *volume.Brick) uint32 {
	if brick == nil {
		return 0
	}
	count := 0
	if len(brick.PrecomputedAux) == VoxelAuxRecordBytes {
		for i := 0; i < volume.DenseOccupancyWordCount; i++ {
			count += bits.OnesCount32(binary.LittleEndian.Uint32(brick.PrecomputedAux[4*i:]))
		}
	} else {
		for _, word := range brick.DenseOccupancyWords() {
			count += bits.OnesCount32(word)
		}
	}
	return volume.DenseOccupancyWordCount + uint32((count+1)/2)
}
func packVoxelAuxiliaryBytes(dense []byte) []byte {
	count := 0
	for i := 0; i < volume.DenseOccupancyWordCount; i++ {
		count += bits.OnesCount32(binary.LittleEndian.Uint32(dense[4*i:]))
	}
	packet := make([]byte, volume.DenseOccupancyWordCount*4+4*((count+1)/2))
	copy(packet, dense[:volume.DenseOccupancyWordCount*4])
	rank := 0
	for index := 0; index < 512; index++ {
		word := binary.LittleEndian.Uint32(dense[4*(index/32):])
		if word&(uint32(1)<<uint(index%32)) == 0 {
			continue
		}
		copy(packet[64+rank*2:66+rank*2], dense[64+index*2:66+index*2])
		rank++
	}
	return packet
}

func (p *voxelAdmissionPlan) reservePackedAuxiliaryMap(m *GpuBufferManager, xbm *volume.XBrickMap) {
	if !m.packedVoxelNormals {
		return
	}
	inventory := p.auxiliaryDemand
	if inventory == nil {
		return
	}
	units := inventory.units[xbm]
	// Legacy allocation snapshots still use dense brick records, but the selected
	// auxiliary policy owns exact physical packet locations for their uploads.
	if inventory.legacy {
		units = nil
		for key, sector := range xbm.Sectors {
			if sector != nil && (m.Allocations[xbm] == nil || xbm.StructureDirty || xbm.DirtySectors[key]) {
				units = append(units, auxiliaryUploadUnit{work: voxelUploadWork{kind: voxelUploadSector, target: xbm, sectorKey: key}})
			}
		}
		for key, dirty := range xbm.DirtyBricks {
			if dirty && key[3] >= 0 && key[3] < 4 && key[4] >= 0 && key[4] < 4 && key[5] >= 0 && key[5] < 4 {
				units = append(units, auxiliaryUploadUnit{work: voxelUploadWork{kind: voxelUploadBrick, target: xbm, brickKey: key}})
			}
		}
	}
	locations := make(map[auxiliaryPacketLocation]*volume.Brick)
	for _, unit := range units {
		sector := xbm.Sectors[unit.work.sectorCoordinate()]
		if sector == nil {
			continue
		}
		start, end := unit.work.brickRange()
		for index := start; index < end; index++ {
			if b := sector.GetBrick(index%4, (index/4)%4, index/16); b != nil {
				locations[auxiliaryPacketLocation{sector, index}] = b
			}
		}
	}
	ordered := make([]auxiliaryPacketLocation, 0, len(locations))
	for location := range locations {
		ordered = append(ordered, location)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		for axis := 0; axis < 3; axis++ {
			if a.sector.Coords[axis] != b.sector.Coords[axis] {
				return a.sector.Coords[axis] < b.sector.Coords[axis]
			}
		}
		return a.index < b.index
	})
	for _, location := range ordered {
		if _, exists := p.auxiliaryPacketReservations[location]; exists {
			continue
		}
		words := packedAuxiliaryWordCount(locations[location])
		base, ok := p.auxiliaryWordRanges.alloc(words, uint64(^uint32(0)))
		p.auxiliaryPacketReservations[location] = auxiliaryPacketLease{base, words}
		p.auxiliaryPacketReservationKeys = append(p.auxiliaryPacketReservationKeys, location)
		if !ok {
			p.auxiliaryWordRangeInvalid = true
		}
	}
}

// Captures belong only to one selected complete upload unit (at most 64 rows).
// Neighbor bake inputs retain the existing revision/halo ownership contract.
type capturedVoxelBrick struct {
	identity *volume.Brick
	source   volume.Brick
	packet   []byte
	lease    auxiliaryPacketLease
}
type packedAuxiliaryUploadSnapshot struct {
	sector   *volume.Sector
	info     SectorGpuInfo
	coords   [3]int
	mask     uint64
	rows     [64]*capturedVoxelBrick
	selected uint64
}

func (s *packedAuxiliaryUploadSnapshot) current() bool {
	for _, row := range s.rows {
		if row != nil && (row.identity.Flags != row.source.Flags || row.identity.AtlasOffset != row.source.AtlasOffset || row.identity.OccupancyMask64 != row.source.OccupancyMask64 || row.identity.Payload != row.source.Payload || !sameAuthoritativeAuxiliarySource(row.identity.PrecomputedAux, row.source.PrecomputedAux)) {
			return false
		}
	}
	return true
}
func (m *GpuBufferManager) capturePackedAuxiliaryUpload(w voxelUploadWork, context func() voxelNormalBakeContext) voxelUploadWork {
	sector := w.targetMap().Sectors[w.sectorCoordinate()]
	if w.sectorSnapshot != nil {
		sector = w.sectorSnapshot.sector
	}
	s := &packedAuxiliaryUploadSnapshot{sector: sector, info: m.SectorToInfo[sector], coords: sector.Coords, mask: sector.BrickMask64}
	start, end := w.brickRange()
	// Capture every raw source before baking or issuing any native write.
	for index := start; index < end; index++ {
		if w.kind == voxelUploadSector && w.sparseRecordSet && w.recordMask&(uint64(1)<<index) == 0 {
			continue
		}
		s.selected |= uint64(1) << index
		if brick := w.desiredBrick(sector, index); brick != nil {
			source := *brick
			if len(brick.PrecomputedAux) == VoxelAuxRecordBytes {
				source.PrecomputedAux = bytes.Clone(brick.PrecomputedAux)
			} else {
				source.PrecomputedAux = nil
			}
			s.rows[index] = &capturedVoxelBrick{identity: brick, source: source}
		}
	}
	w.bytes = 0
	if w.kind == voxelUploadSector {
		w.bytes = 32
		if m.voxelBufferMirrored(0) {
			w.bytes *= 2
		}
	}
	for index, row := range s.rows {
		if s.selected&(uint64(1)<<index) == 0 {
			continue
		}
		w.bytes += BrickRecordSize
		if m.voxelBufferMirrored(1) {
			w.bytes += BrickRecordSize
		}
		if row == nil {
			continue
		}
		mode := resolveBrickUploadMode(row.source.Flags)
		if mode.usesPayload {
			w.bytes += payloadBytesPerBrick
		}
		if mode.usesAux {
			var dense []byte
			if len(row.source.PrecomputedAux) == VoxelAuxRecordBytes {
				dense = row.source.PrecomputedAux
			} else {
				begin := time.Now()
				ctx := voxelNormalBakeContext{}
				if context != nil && w.targetMap() == w.object.RenderVoxelMap() {
					ctx = context()
				}
				dense = buildVoxelAuxBytesForTarget(ctx, w.object, w.targetMap(), &row.source, brickOriginForSectorIndex(w.sectorCoordinate(), index))
				m.VoxelRuntimeNormalBakeDuration += time.Since(begin)
			}
			row.packet = packVoxelAuxiliaryBytes(dense)
			w.bytes += uint64(len(row.packet))
			if m.voxelBufferMirrored(2) {
				w.bytes += uint64(len(row.packet))
			}
		}
	}
	w.packedAuxiliarySnapshot = s
	return w
}

func (m *GpuBufferManager) planCapturedAuxiliaryUpload(w voxelUploadWork) (voxelIndexRanges, bool) {
	if !m.ensureAuxiliaryWordRanges() {
		return voxelIndexRanges{}, false
	}
	limit := uint64(^uint32(0))
	if m.voxelNative != nil {
		limit = m.voxelNative.BufferSize(m.DenseOccupancyBuf) / 4
	}
	s := w.packedAuxiliarySnapshot
	// Exact promises are disjoint by admission construction. The ordinary path
	// claims only this complete unit, without walking other deferred packets.
	exact := m.auxiliaryRanges.clone()
	promised := true
	for index, row := range s.rows {
		if row == nil || len(row.packet) == 0 {
			continue
		}
		words := uint32(len(row.packet) / 4)
		own, ok := m.plannedAuxiliaryRanges[auxiliaryPacketLocation{s.sector, index}]
		if !ok || own.words < words || !exact.claim(own.base, words, limit) {
			promised = false
			break
		}
		row.lease = auxiliaryPacketLease{own.base, words}
	}
	if promised {
		return exact, true
	}
	trial := m.auxiliaryRanges.clone()
	// Protect every other admitted virtual span before using spare space for late
	// halo demand or a packet which grew since capacity admission.
	for other, reserved := range m.plannedAuxiliaryRanges {
		if other.sector == s.sector && s.selected&(uint64(1)<<other.index) != 0 {
			continue
		}
		if !trial.claim(reserved.base, reserved.words, limit) {
			return voxelIndexRanges{}, false
		}
	}
	for index, row := range s.rows {
		if row == nil || len(row.packet) == 0 {
			continue
		}
		words := uint32(len(row.packet) / 4)
		location := auxiliaryPacketLocation{s.sector, index}
		if own, ok := m.plannedAuxiliaryRanges[location]; ok && own.words >= words && trial.claim(own.base, words, limit) {
			row.lease = auxiliaryPacketLease{own.base, words}
		} else {
			base, ok := trial.alloc(words, limit)
			if !ok {
				return voxelIndexRanges{}, false
			}
			row.lease = auxiliaryPacketLease{base, words}
		}
	}
	// Protection is virtual only: claim this unit against the real allocator,
	// leaving other deferred plans unassigned.
	claimed := m.auxiliaryRanges.clone()
	for _, row := range s.rows {
		if row != nil && row.lease.words != 0 && !claimed.claim(row.lease.base, row.lease.words, limit) {
			return voxelIndexRanges{}, false
		}
	}
	return claimed, true
}
func (m *GpuBufferManager) publishAuxiliaryUpload(w voxelUploadWork) {
	s := w.packedAuxiliarySnapshot
	if s == nil {
		return
	}
	if info, ok := m.SectorToInfo[s.sector]; !ok || info.SlotIndex != s.info.SlotIndex || info.packed != s.info.packed {
		// Native callbacks may detach the final physical owner after writes begin.
		// Those queued packets still need a submission fence, but own no live row.
		for _, row := range s.rows {
			if row != nil {
				m.retireAuxiliaryPacket(row.lease)
			}
		}
		return
	}
	if m.auxiliaryPacketReceipts == nil {
		m.auxiliaryPacketReceipts = make(map[auxiliaryPacketLocation]auxiliaryPacketLease)
	}
	selected := s.selected
	if w.kind == voxelUploadSector {
		selected = ^uint64(0)
	}
	for index := 0; index < 64; index++ {
		if selected&(uint64(1)<<index) == 0 {
			continue
		}
		location := auxiliaryPacketLocation{s.sector, index}
		if previous, exists := m.auxiliaryPacketReceipts[location]; exists {
			m.retireAuxiliaryPacket(previous)
			delete(m.auxiliaryPacketReceipts, location)
		}
		if row := s.rows[index]; row != nil && row.lease.words != 0 {
			m.auxiliaryPacketReceipts[location] = row.lease
		}
		delete(m.plannedAuxiliaryRanges, location)
	}
	// The packet is physical sector state; every indexed owner charges its receipt.
	info := m.SectorToInfo[s.sector]
	if info.packed != nil {
		for owner := range info.packed.owners {
			m.markRetainedVoxelMapAccountingDirty(owner.xbm)
		}
	} else {
		// Imported dense allocation headers do not supply the managed owner index.
		for xbm, alloc := range m.Allocations {
			for _, sector := range alloc.Sectors {
				if sector == s.sector {
					m.markRetainedVoxelMapAccountingDirty(xbm)
					break
				}
			}
		}
	}
}

func samePackedPhysicalUpload(a, b voxelUploadWork) bool {
	sa, sb := a.packedAuxiliarySnapshot, b.packedAuxiliarySnapshot
	if sa == nil || sb == nil || sa.sector != sb.sector || sa.selected != sb.selected || a.kind != b.kind {
		return false
	}
	if sa.coords != sb.coords || sa.mask != sb.mask || sa.info.SlotIndex != sb.info.SlotIndex || sa.info.packed != sb.info.packed {
		return false
	}
	for i, ra := range sa.rows {
		if !sameCapturedVoxelBrick(ra, sb.rows[i]) {
			return false
		}
	}
	return true
}
func sameCapturedVoxelBrick(a, b *capturedVoxelBrick) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return a.identity == b.identity && a.source.Flags == b.source.Flags && a.source.AtlasOffset == b.source.AtlasOffset && a.source.OccupancyMask64 == b.source.OccupancyMask64 && a.source.Payload == b.source.Payload && bytes.Equal(a.source.PrecomputedAux, b.source.PrecomputedAux) && bytes.Equal(a.packet, b.packet)
}

// Invalid auxiliary slices are ignored by the canonical baker. Captures retain
// only the fixed authored format, and ignored malformed data cannot grow scratch.
func sameAuthoritativeAuxiliarySource(a, b []byte) bool {
	if len(a) != VoxelAuxRecordBytes && len(b) != VoxelAuxRecordBytes {
		return true
	}
	return bytes.Equal(a, b)
}
