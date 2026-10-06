package gpu

import (
	"encoding/binary"
	"fmt"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"os"
	"unsafe"
)

// SectorLookupFrameBudget bounds private lookup preparation independently of
// geometry and native creation budgets. Zero value preserves legacy updates.
// Enabled zero entries pauses capture, upload and retirement; zero upload bytes
// permits CPU preparation. Raw transitions and untracked public headers use the
// explicit synchronous compatibility exception reported in frame stats.
type SectorLookupFrameBudget struct {
	Enabled                       bool
	MaxEntries                    uint32
	MaxUploadBytes, MaxStageBytes uint64
}

// SectorLookupFrameStats describes the most recent lookup service. StageBytes
// retains admitted preparation and retirement charges after policy reductions;
// permanent live inventory and settled current metadata are outside this cap.
type SectorLookupFrameStats struct {
	Compatibility                                  bool
	AttemptedEntries                               uint32
	UploadedBytes                                  uint64
	Pending                                        bool
	StageBytes, CurrentGeneration, RetiringEntries uint64
}

func DefaultSectorLookupFrameBudget() SectorLookupFrameBudget {
	return SectorLookupFrameBudget{true, 1024, 64 << 10, 128 << 20}
}
func (m *GpuBufferManager) SetSectorLookupFrameBudget(b SectorLookupFrameBudget) {
	if m != nil {
		if m.sectorLookupBudget.Enabled && !b.Enabled && m.sectorLookupStage != nil {
			m.sectorLookupStage.cancelled = true
		}
		m.sectorLookupBudget = b
		if b.Enabled && b.MaxEntries > 0 {
			m.sectorLookupDrain = b.MaxEntries
		}
	}
}
func (m *GpuBufferManager) SectorLookupFrameBudget() SectorLookupFrameBudget {
	if m == nil {
		return SectorLookupFrameBudget{}
	}
	return m.sectorLookupBudget
}
func (m *GpuBufferManager) SectorLookupFrameStats() SectorLookupFrameStats {
	if m == nil {
		return SectorLookupFrameStats{}
	}
	return m.sectorLookupStats
}

type sectorLookupMapSnapshot struct {
	sectorPins, brickPins                 *sectorLookupPinNode
	selectedSectorPins, selectedBrickPins *sectorLookupPinNode
	object                                *core.VoxelObject
	mapRef                                *volume.XBrickMap
	allocation                            *ObjectGpuAllocation
	root                                  *sectorLookupInventoryNode
	untracked, managed                    bool
	mapID                                 uint32
	generation                            uint64
	count                                 int
	entries                               []sectorLookupInventoryEntry
	direct                                directSectorLookupMetadata
	cells                                 uint32
}
type sectorLookupGeneration struct {
	cleanupMap, cleanupEntry                         int
	maps                                             []sectorLookupMapSnapshot
	data                                             [3][]byte
	buffers                                          [3]*wgpu.Buffer
	backend                                          voxelNativeBackend
	charge                                           uint64
	serial                                           uint64
	phase, mapCursor, entryCursor, cellCursor, probe int
	gridSize                                         uint32
	uploadBuffer                                     int
	uploadOffset                                     uint64
	used                                             [3]bool
	queue                                            *wgpu.Queue
	submission                                       uint64
	frames                                           int
	retireMap, retireEntry                           int
	rootsReleased                                    bool
	failed, cancelled, abandoned                     bool
}

func (m *GpuBufferManager) sectorLookupNativeStageBytes() uint64 {
	var n uint64
	if g := m.sectorLookupStage; g != nil {
		for _, b := range g.buffers {
			if b != nil {
				n += g.backend.BufferSize(b)
			}
		}
	}
	return n
}
func (m *GpuBufferManager) sectorLookupOwnsNativeStage() bool {
	return m.sectorLookupNativeStageBytes() != 0
}
func (m *GpuBufferManager) markSectorLookupSubmitted(q *wgpu.Queue, i wgpu.SubmissionIndex) {
	if g := m.sectorLookupCurrent; g != nil {
		g.queue = q
		g.submission = uint64(i)
	}
	if g := m.sectorLookupRetired; g != nil && g.queue == nil {
		g.queue = q
		g.submission = uint64(i)
	}
}
func (m *GpuBufferManager) sectorLookupPinnedSector(s *volume.Sector) bool {
	return m.sectorLookupPinned(s, nil)
}
func (m *GpuBufferManager) sectorLookupPinnedBrick(b *volume.Brick) bool {
	return m.sectorLookupPinned(nil, b)
}
func (m *GpuBufferManager) sectorLookupPinned(s *volume.Sector, b *volume.Brick) bool {
	sectorKey, brickKey := uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(b))
	for _, g := range []*sectorLookupGeneration{m.sectorLookupCurrent, m.sectorLookupStage, m.sectorLookupRetired} {
		if g == nil {
			continue
		}
		for _, a := range g.maps {
			if !g.rootsReleased && (sectorKey != 0 && sectorLookupPinReferences(a.sectorPins, sectorKey) > 0 || brickKey != 0 && sectorLookupPinReferences(a.brickPins, brickKey) > 0) {
				return true
			}
			if sectorKey != 0 && sectorLookupPinReferences(a.selectedSectorPins, sectorKey) > 0 || brickKey != 0 && sectorLookupPinReferences(a.selectedBrickPins, brickKey) > 0 {
				return true
			}
		}
	}
	return false
}

func (m *GpuBufferManager) sectorLookupMaps(scene *core.Scene) ([]sectorLookupMapSnapshot, bool) {
	maps := make([]sectorLookupMapSnapshot, 0)
	seen := map[*volume.XBrickMap]bool{}
	for _, target := range m.voxelServiceTargets(scene) {
		x := target.mapRef
		if x == nil || seen[x] {
			continue
		}
		seen[x] = true
		a := m.Allocations[x]
		if a == nil {
			continue
		}
		untracked := !m.ownsVoxelAllocation(x, a)
		a.lookupPublicationKnown = true
		if a.lookupAdmissionKnown && !a.lookupAdmitted {
			continue
		}
		t := m.managedGPUMaps[x]
		var gen uint64
		if t != nil {
			o := m.managedGPUOwners[target.object]
			if o == nil || !m.managedReady(o, t) {
				// A changing current keeps the exact displayed membership until
				// uploaded successor content is qualified for a fresh capture.
				if o != nil && t == o.current && m.sectorLookupCurrent != nil {
					for _, committed := range m.sectorLookupCurrent.maps {
						if committed.mapRef == x {
							committed.sectorPins, committed.brickPins = committed.selectedSectorPins, committed.selectedBrickPins
							committed.selectedSectorPins, committed.selectedBrickPins = nil, nil
							committed.entries = nil
							maps = append(maps, committed)
							break
						}
					}
				}
				continue
			}
			gen = t.generation
		}
		root := a.lookupRoot
		sectorPins, brickPins := a.lookupSectorPins, a.lookupBrickPins
		if untracked {
			// Public headers have no mutation proof. Compatibility validates
			// their dictionary synchronously before accepting a private image.
			root, sectorPins, brickPins = m.sectorLookupPublicSnapshot(x, a)
			a.lookupRoot, a.lookupSectorPins, a.lookupBrickPins = root, sectorPins, brickPins
		}
		count := sectorLookupInventoryCount(root)
		maps = append(maps, sectorLookupMapSnapshot{object: target.object, mapRef: x, allocation: a, root: root, sectorPins: sectorPins, brickPins: brickPins, mapID: x.ID, generation: gen, count: count, direct: defaultDirectSectorLookupMetadata(), untracked: untracked, managed: t != nil || m.managedGPUOwners[target.object] != nil})
	}
	return maps, true
}
func sectorLookupSame(a, b []sectorLookupMapSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].mapRef != b[i].mapRef || a[i].allocation != b[i].allocation || a[i].root != b[i].root || a[i].mapID != b[i].mapID || a[i].generation != b[i].generation {
			return false
		}
	}
	return true
}
func (m *GpuBufferManager) newSectorLookup(maps []sectorLookupMapSnapshot) *sectorLookupGeneration {
	g := &sectorLookupGeneration{maps: maps, frames: RetiredBufferFrameDelay}
	total := 0
	direct := uint64(0)
	charge := uint64(unsafe.Sizeof(*g)) + uint64(cap(maps))*uint64(unsafe.Sizeof(sectorLookupMapSnapshot{}))
	for i := range g.maps {
		a := &g.maps[i]
		total += a.count
		charge = addRetainedVoxelBytes(charge, sectorLookupInventoryBytes(a.root))
		charge = addRetainedVoxelBytes(charge, uint64(a.count)*uint64(unsafe.Sizeof(sectorLookupInventoryEntry{})))
		charge = addRetainedVoxelBytes(charge, uint64(a.count)*65*uint64(unsafe.Sizeof(sectorLookupPinNode{}))*3)
		if a.count > 0 && os.Getenv(forceHashLookupEnv) != "1" {
			lo, hi := sectorLookupInventoryBounds(a.root)
			ext := [3]uint32{}
			cells := int64(1)
			for j := range 3 {
				n := int64(hi[j]) - int64(lo[j]) + 1
				if n <= 0 || n > DirectSectorLookupMaxCells {
					cells = 0
					break
				}
				ext[j] = uint32(n)
				cells *= n
				if cells > DirectSectorLookupMaxCells {
					cells = 0
					break
				}
			}
			if directSectorLookupQualified(cells, a.count) {
				a.direct = directSectorLookupMetadata{LookupMode: LookupModeDirect, Origin: [3]int32{int32(lo[0]), int32(lo[1]), int32(lo[2])}, Extent: ext, TableBase: uint32(direct)}
				a.cells = uint32(cells)
				direct += uint64(cells)
			}
		}
	}
	size := uint64(1024)
	for size < uint64(total)*8 {
		size *= 2
	}
	if total == 0 {
		size = 2
	}
	g.gridSize = uint32(size)
	direct = max(direct, 1)
	charge = addRetainedVoxelBytes(charge, size*32+direct*4+16)
	g.charge = charge
	backend := m.voxelNative
	if backend != nil {
		buffer, storage, uniform := backend.Limits()
		if size > uint64(^uint32(0)) || size*32 > min(buffer, storage) || direct*4 > min(buffer, storage) || 16 > min(buffer, uniform) {
			return nil
		}
	}
	if charge > m.sectorLookupBudget.MaxStageBytes {
		return nil
	}
	for i := range g.maps {
		g.maps[i].entries = make([]sectorLookupInventoryEntry, 0, g.maps[i].count)
	}
	g.data = [3][]byte{make([]byte, size*32), make([]byte, direct*4), make([]byte, 16)}
	binary.LittleEndian.PutUint32(g.data[2], g.gridSize)
	binary.LittleEndian.PutUint32(g.data[2][4:], g.gridSize-1)
	return g
}
func (m *GpuBufferManager) lookupEntry(left *uint32) bool {
	if *left == 0 {
		return false
	}
	*left--
	m.sectorLookupStats.AttemptedEntries++
	return true
}
func (m *GpuBufferManager) serviceSectorLookupCPU(g *sectorLookupGeneration, left *uint32) {
	for *left > 0 && g.phase != 5 && !g.failed {
		switch g.phase {
		case 0: // Stable coordinate enumeration also constructs selected exact pins.
			if g.mapCursor == len(g.maps) {
				g.phase = 6
				g.rootsReleased = true
				g.mapCursor = 0
				g.cellCursor = 0
				continue
			}
			a := &g.maps[g.mapCursor]
			if g.entryCursor == a.count {
				g.mapCursor++
				g.entryCursor = 0
				continue
			}
			if !m.lookupEntry(left) {
				return
			}
			e, ok := sectorLookupInventoryAt(a.root, g.entryCursor)
			g.entryCursor++
			if ok && e.eligible {
				a.entries = append(a.entries, e)
				sectorLookupPinEntry(&a.selectedSectorPins, &a.selectedBrickPins, e, 1)
			}
		case 6: // Drop capture pins only after every eligible edge has a selected pin.
			if g.cleanupMap == len(g.maps) {
				g.phase = 1
				continue
			}
			a := &g.maps[g.cleanupMap]
			if g.cleanupEntry == a.count {
				a.sectorPins, a.brickPins = nil, nil
				g.cleanupMap++
				g.cleanupEntry = 0
				continue
			}
			m.lookupEntry(left)
			e, ok := sectorLookupInventoryAt(a.root, g.cleanupEntry)
			g.cleanupEntry++
			if ok {
				m.releaseSectorLookupEdge(e)
			}
		case 1:
			if g.cellCursor == int(g.gridSize) {
				g.phase++
				g.cellCursor = 0
				continue
			}
			m.lookupEntry(left)
			binary.LittleEndian.PutUint32(g.data[0][g.cellCursor*32+20:], ^uint32(0))
			g.cellCursor++
		case 2:
			if g.cellCursor == len(g.data[1])/4 {
				g.phase++
				g.cellCursor = 0
				continue
			}
			m.lookupEntry(left)
			binary.LittleEndian.PutUint32(g.data[1][g.cellCursor*4:], DirectSectorLookupInvalid)
			g.cellCursor++
		case 3:
			if g.mapCursor == len(g.maps) {
				g.phase++
				g.mapCursor = 0
				g.entryCursor = 0
				continue
			}
			a := &g.maps[g.mapCursor]
			if g.entryCursor == len(a.entries) {
				g.mapCursor++
				g.entryCursor = 0
				continue
			}
			e := a.entries[g.entryCursor]
			h := uint32(e.coordinate[0])*73856093 ^ uint32(e.coordinate[1])*19349663 ^ uint32(e.coordinate[2])*83492791 ^ a.mapID*99999989
			idx := (h + uint32(g.probe)) & (g.gridSize - 1)
			m.lookupEntry(left)
			row := g.data[0][idx*32:]
			if binary.LittleEndian.Uint32(row[20:]) == ^uint32(0) {
				for j := range 3 {
					binary.LittleEndian.PutUint32(row[j*4:], uint32(e.coordinate[j]))
				}
				binary.LittleEndian.PutUint32(row[16:], a.mapID)
				binary.LittleEndian.PutUint32(row[20:], e.slot)
				g.entryCursor++
				g.probe = 0
			} else {
				g.probe++
				if g.probe == 128 {
					g.failed = true
					fmt.Println("WARNING: bounded sector lookup hash overflow; retaining committed generation")
				}
			}
		case 4:
			if g.mapCursor == len(g.maps) {
				g.phase++
				g.mapCursor = 0
				g.entryCursor = 0
				continue
			}
			a := &g.maps[g.mapCursor]
			if g.entryCursor == len(a.entries) {
				g.mapCursor++
				g.entryCursor = 0
				continue
			}
			e := a.entries[g.entryCursor]
			m.lookupEntry(left)
			g.entryCursor++
			if a.cells != 0 {
				local := [3]uint32{}
				for j := range 3 {
					local[j] = uint32(int32(e.coordinate[j]) - a.direct.Origin[j])
				}
				idx := a.direct.TableBase + flattenDirectSectorLookupIndex(local, a.direct.Extent)
				binary.LittleEndian.PutUint32(g.data[1][idx*4:], e.slot)
			}
		}
	}
}
func (m *GpuBufferManager) createSectorLookup(g *sectorLookupGeneration) bool {
	if m.voxelGrowth != nil {
		return false
	}
	if g.backend == nil {
		g.backend = m.voxelNative
		if g.backend == nil {
			g.backend = nativeVoxelBackend{m}
		}
	}
	r := m.currentVoxelGPUResources()
	additional := uint64(0)
	for i, b := range g.buffers {
		if b == nil {
			additional += uint64(len(g.data[i]))
		}
	}
	if cap := m.voxelGPUAdmissionBudget.MaxBytes; cap != 0 && voxelResourceTotalBytes(r)+additional > cap {
		return false
	}
	bl, sl, ul := g.backend.Limits()
	for i, b := range g.buffers {
		if b != nil {
			continue
		}
		size := uint64(len(g.data[i]))
		limit := min(bl, sl)
		if i == 2 {
			limit = min(bl, ul)
		}
		if size > limit {
			return false
		}
		ok, over := m.canCreateVoxelBuffer(size)
		if !ok {
			return false
		}
		buf, err := g.backend.CreateBuffer([]string{"SectorGridBuf", "DirectSectorLookupBuf", "SectorGridParamsBuf"}[i], size, i == 2)
		if err != nil {
			for j, b := range g.buffers {
				if b != nil && !g.used[j] {
					g.backend.ReleaseBuffer(b)
					g.buffers[j] = nil
				}
			}
			return false
		}
		g.buffers[i] = buf
		m.voxelGPUWorkStats.Creates++
		m.voxelGPUWorkStats.CreatedBytes += size
		if over {
			m.voxelGPUWorkStats.OversizedCreates++
		}
	}
	return true
}
func (m *GpuBufferManager) uploadSectorLookup(g *sectorLookupGeneration, left *uint32) bool {
	bytes := (m.sectorLookupBudget.MaxUploadBytes - min(m.sectorLookupBudget.MaxUploadBytes, m.sectorLookupStats.UploadedBytes)) &^ uint64(3)
	for g.uploadBuffer < 3 && bytes > 0 && *left > 0 {
		i := g.uploadBuffer
		n := min(uint64(len(g.data[i]))-g.uploadOffset, bytes)
		m.lookupEntry(left)
		g.used[i] = true
		if err := g.backend.WriteBuffer(g.buffers[i], g.uploadOffset, g.data[i][g.uploadOffset:g.uploadOffset+n]); err != nil {
			return false
		}
		g.uploadOffset += n
		bytes -= n
		m.sectorLookupStats.UploadedBytes += n
		if g.uploadOffset == uint64(len(g.data[i])) {
			g.uploadBuffer++
			g.uploadOffset = 0
		}
	}
	return g.uploadBuffer == 3
}
func (m *GpuBufferManager) releaseSectorLookupEdge(e sectorLookupInventoryEntry) {
	m.releaseUnreferencedSector(e.sector)
	for _, b := range e.bricks {
		if b != nil && !m.voxelBrickReferenced(b) {
			m.releaseBrickSlot(b)
			m.releaseVoxelAuxSlot(b)
		}
	}
}
func (m *GpuBufferManager) cleanupSectorLookup(g *sectorLookupGeneration, left *uint32) bool {
	for g.retireMap < len(g.maps) && *left > 0 {
		a := &g.maps[g.retireMap]
		if g.retireEntry == len(a.entries) {
			a.root = nil
			a.sectorPins, a.brickPins = nil, nil
			g.retireMap++
			g.retireEntry = 0
			continue
		}
		m.lookupEntry(left)
		e := a.entries[g.retireEntry]
		a.entries[g.retireEntry] = sectorLookupInventoryEntry{}
		sectorLookupPinEntry(&a.selectedSectorPins, &a.selectedBrickPins, e, -1)
		g.retireEntry++
		m.releaseSectorLookupEdge(e)
	}
	return g.retireMap == len(g.maps)
}
func (m *GpuBufferManager) lookupFenceDone(g *sectorLookupGeneration) bool {
	if g.abandoned {
		return true
	}
	if g.queue != nil {
		if p, ok := g.backend.(interface {
			SubmissionComplete(*wgpu.Queue, uint64) bool
		}); ok {
			return p.SubmissionComplete(g.queue, g.submission)
		}
		if m.Device != nil {
			return m.Device.Poll(false, &wgpu.WrappedSubmissionIndex{Queue: g.queue, SubmissionIndex: wgpu.SubmissionIndex(g.submission)})
		}
		return false
	}
	g.frames--
	return g.frames <= 0
}
func (m *GpuBufferManager) updateBoundedSectorLookup(scene *core.Scene) bool {
	m.sectorLookupStats = SectorLookupFrameStats{}
	budget := m.sectorLookupBudget
	left := budget.MaxEntries
	maps, _ := m.sectorLookupMaps(scene)
	compatibility := m.sectorLookupCompatibility(maps)
	m.sectorLookupStats.Compatibility = compatibility
	if compatibility {
		left = ^uint32(0)
		m.sectorLookupBudget.MaxUploadBytes = ^uint64(0)
		m.sectorLookupBudget.MaxStageBytes = ^uint64(0)
	}
	if !budget.Enabled {
		left = m.sectorLookupDrain
		if left == 0 {
			left = DefaultSectorLookupFrameBudget().MaxEntries
		}
		m.sectorLookupBudget.MaxUploadBytes = ^uint64(0)
		m.sectorLookupBudget.MaxStageBytes = ^uint64(0)
	}
	changed := m.sectorLookupCurrent == nil || !sectorLookupSame(m.sectorLookupCurrent.maps, maps)
	recreated := false
	if left > 0 {
		if g := m.sectorLookupStage; g != nil && (g.cancelled || m.sectorLookupCaptureRemoved(g, scene)) {
			m.abandonSectorLookupStage(g)
		}
		if r := m.sectorLookupRetired; r != nil && m.lookupFenceDone(r) {
			if (!r.abandoned || m.cleanupAbandonedSectorLookupRoots(r, &left)) && m.cleanupSectorLookup(r, &left) {
				m.sectorLookupRetired = nil
			}
		}
		if g := m.sectorLookupStage; g != nil && g.failed && (!budget.Enabled || !sectorLookupSame(g.maps, maps)) {
			if m.cleanupSectorLookup(g, &left) {
				m.sectorLookupStage = nil
			}
		}
		if m.sectorLookupStage == nil && m.sectorLookupRetired == nil && changed {
			m.sectorLookupStage = m.newSectorLookup(maps)
		}
		if g := m.sectorLookupStage; g != nil {
			m.serviceSectorLookupCPU(g, &left)
			if g.phase == 5 && !g.failed && left > 0 && m.createSectorLookup(g) && m.uploadSectorLookup(g, &left) {
				old := m.sectorLookupCurrent
				m.sectorLookupSerial++
				g.serial = m.sectorLookupSerial
				g.rootsReleased = true
				published := make(map[*ObjectGpuAllocation]bool, len(g.maps))
				for _, a := range g.maps {
					published[a.allocation] = true
					if !a.allocation.lookupCommitted || a.allocation.lookupCommittedRoot != a.root {
						a.allocation.shadowUploadEpoch++
					}
				}
				if old != nil {
					for _, a := range old.maps {
						if !published[a.allocation] {
							a.allocation.shadowUploadEpoch++
						}
						a.allocation.lookupCommitted = false
						a.allocation.lookupCommittedRoot = nil
					}
				}
				for i := range g.maps {
					a := &g.maps[i]
					a.allocation.lookupCommitted = true
					a.allocation.lookupCommittedRoot = a.root
					a.allocation.lookupMapID = a.mapID
					a.allocation.lookupSectorCount = uint32(len(a.entries))
					a.allocation.lookupGeneration = a.generation
					a.allocation.DirectLookup = a.direct
				}
				previous := [3]*wgpu.Buffer{m.SectorGridBuf, m.DirectSectorLookupBuf, m.SectorGridParamsBuf}
				m.SectorGridBuf, m.DirectSectorLookupBuf, m.SectorGridParamsBuf = g.buffers[0], g.buffers[1], g.buffers[2]
				for _, b := range previous {
					if b != nil {
						m.retireVoxelBuffer(b, g.backend.BufferSize(b), g.backend)
					}
				}
				g.data = [3][]byte{}
				m.sectorLookupCurrent = g
				m.sectorLookupStage = nil
				if old != nil {
					old.queue = nil
					old.submission = 0
					old.frames = RetiredBufferFrameDelay
					old.data = [3][]byte{}
					old.buffers = [3]*wgpu.Buffer{}
					m.sectorLookupRetired = old
				}
				recreated = true
			}
		}
	}
	if !budget.Enabled || compatibility {
		m.sectorLookupBudget = budget
	}
	st := &m.sectorLookupStats
	if g := m.sectorLookupCurrent; g != nil {
		st.CurrentGeneration = g.serial
	}
	if g := m.sectorLookupStage; g != nil {
		st.StageBytes += g.charge
	}
	if g := m.sectorLookupRetired; g != nil {
		st.StageBytes += g.charge
		for _, a := range g.maps[g.retireMap:] {
			st.RetiringEntries += uint64(len(a.entries))
		}
		st.RetiringEntries -= uint64(g.retireEntry)
		if g.abandoned && g.cleanupMap < len(g.maps) {
			for _, a := range g.maps[g.cleanupMap:] {
				st.RetiringEntries += uint64(a.count)
			}
			st.RetiringEntries -= uint64(g.cleanupEntry)
		}
	}
	st.Pending = m.sectorLookupStage != nil || m.sectorLookupRetired != nil || m.sectorLookupCurrent == nil || !sectorLookupSame(m.sectorLookupCurrent.maps, maps)
	if m.voxelNative != nil {
		m.refreshVoxelGPUAdmissionStats(m.currentVoxelGPUResources())
	}
	return recreated
}

func sectorLookupInventoryBytes(n *sectorLookupInventoryNode) uint64 {
	if n == nil {
		return 0
	}
	return n.bytes
}
func sectorLookupInventoryBounds(n *sectorLookupInventoryNode) ([3]int, [3]int) {
	if n == nil {
		return [3]int{}, [3]int{}
	}
	return n.min, n.max
}

func (m *GpuBufferManager) sectorLookupPublicSnapshot(x *volume.XBrickMap, a *ObjectGpuAllocation) (*sectorLookupInventoryNode, *sectorLookupPinNode, *sectorLookupPinNode) {
	same := sectorLookupInventoryCount(a.lookupRoot) == len(a.Sectors)
	var root *sectorLookupInventoryNode
	var sp, bp *sectorLookupPinNode
	for c, s := range a.Sectors {
		info, ok := m.SectorToInfo[s]
		if !ok {
			same = false
			continue
		}
		e := sectorLookupInventoryEntry{coordinate: c, sector: s, slot: info.SlotIndex, eligible: !info.pending}
		if p := a.Bricks[c]; p != nil {
			e.bricks = *p
		}
		e.inputBytes = sectorLookupRetainedInputBytes(e)
		if same {
			old, ok := sectorLookupInventoryFind(a.lookupRoot, c)
			same = ok && old == e
		}
		root = sectorLookupInventorySet(root, e)
		sectorLookupPinEntry(&sp, &bp, e, 1)
	}
	if same {
		return a.lookupRoot, a.lookupSectorPins, a.lookupBrickPins
	}
	return root, sp, bp
}

func (m *GpuBufferManager) sectorLookupCompatibility(maps []sectorLookupMapSnapshot) bool {
	for _, a := range maps {
		if a.untracked {
			return true
		}
		if a.managed {
			continue
		}
		same := false
		if g := m.sectorLookupCurrent; g != nil {
			for _, c := range g.maps {
				if c.mapRef == a.mapRef && c.allocation == a.allocation && c.root == a.root && c.mapID == a.mapID {
					same = true
					break
				}
			}
		}
		if !same {
			return true
		}
	}
	if g := m.sectorLookupCurrent; g != nil {
		for _, c := range g.maps {
			if c.managed {
				continue
			}
			found := false
			for _, a := range maps {
				if a.mapRef == c.mapRef {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
	}
	return false
}

func (m *GpuBufferManager) sectorLookupCoverageCurrent(x *volume.XBrickMap) bool {
	a := m.Allocations[x]
	return a != nil && a.lookupCommitted && a.lookupCommittedRoot == a.lookupRoot && a.lookupMapID == x.ID && a.lookupSectorCount == uint32(len(a.Sectors))
}

func (m *GpuBufferManager) sectorLookupOwnershipActive() bool {
	return m != nil && (m.sectorLookupBudget.Enabled || m.sectorLookupCurrent != nil || m.sectorLookupStage != nil || m.sectorLookupRetired != nil)
}

func (m *GpuBufferManager) cancelSectorLookupObject(object *core.VoxelObject) {
	if g := m.sectorLookupStage; g != nil {
		for _, a := range g.maps {
			if a.object == object {
				g.cancelled = true
				return
			}
		}
	}
}
func (m *GpuBufferManager) sectorLookupCaptureRemoved(g *sectorLookupGeneration, scene *core.Scene) bool {
	resident := make(map[*core.VoxelObject]bool)
	if scene != nil {
		for _, o := range scene.Objects {
			resident[o] = true
		}
	}
	for _, a := range g.maps {
		if a.object != nil && !resident[a.object] {
			return true
		}
	}
	return false
}
func (m *GpuBufferManager) abandonSectorLookupStage(g *sectorLookupGeneration) {
	// A private image was never shader-visible. Its coordinate pins can drain
	// immediately; native buffers with queued writes keep their own exact fence.
	for i, b := range g.buffers {
		if b == nil {
			continue
		}
		if g.used[i] {
			m.retireVoxelBuffer(b, g.backend.BufferSize(b), g.backend)
		} else {
			g.backend.ReleaseBuffer(b)
		}
	}
	g.buffers = [3]*wgpu.Buffer{}
	g.data = [3][]byte{}
	g.abandoned = true
	g.rootsReleased = true
	g.cleanupMap, g.cleanupEntry = 0, 0
	g.retireMap, g.retireEntry = 0, 0
	m.sectorLookupStage = nil
	m.sectorLookupRetired = g
}
func (m *GpuBufferManager) cleanupAbandonedSectorLookupRoots(g *sectorLookupGeneration, left *uint32) bool {
	for g.cleanupMap < len(g.maps) && *left > 0 {
		a := &g.maps[g.cleanupMap]
		if g.cleanupEntry == a.count {
			a.sectorPins, a.brickPins = nil, nil
			g.cleanupMap++
			g.cleanupEntry = 0
			continue
		}
		m.lookupEntry(left)
		e, ok := sectorLookupInventoryAt(a.root, g.cleanupEntry)
		g.cleanupEntry++
		if ok {
			m.releaseSectorLookupEdge(e)
		}
	}
	return g.cleanupMap == len(g.maps)
}
