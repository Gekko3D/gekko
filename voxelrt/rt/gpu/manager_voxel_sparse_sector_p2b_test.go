package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/bits"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type p2bWrite struct {
	label  string
	buffer *wgpu.Buffer
	offset uint64
	data   []byte
}

// Record physical bytes and publication order at the same native boundary as
// the P2a fixtures. Production allocation, service and encoding remain intact.
type p2bNative struct {
	*p2aNative
	writes        []p2bWrite
	afterAuxWrite func()
}

func (b *p2bNative) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if err := b.p2aNative.WriteBuffer(buffer, offset, data); err != nil {
		return err
	}
	b.writes = append(b.writes, p2bWrite{b.labels[buffer], buffer, offset, bytes.Clone(data)})
	if b.labels[buffer] == "DenseOccupancyBuf" && b.afterAuxWrite != nil {
		callback := b.afterAuxWrite
		b.afterAuxWrite = nil
		callback()
	}
	return nil
}

func TestP2bMidWriteTopologyUsesCapturedRecordsAndDefersChangedTarget(t *testing.T) {
	for _, edit := range []string{"insert", "remove", "replace"} {
		t.Run(edit, func(t *testing.T) {
			m, b, scene := p2bFixture(t)
			o := p2aObject(scene, 1)
			first, later, changed := p2aBrick("uniform", 1), p2aBrick("uniform", 2), p2aBrick("uniform", 3)
			schedulePutBrick(o, [6]int{}, first)
			mask := uint64(1)
			if edit != "insert" {
				schedulePutBrick(o, [6]int{0, 0, 0, 3, 3, 1}, later)
				mask |= uint64(1) << 31
			}
			sector := o.XBrickMap.Sectors[[3]int{}]
			revision := o.XBrickMap.Revision
			b.afterAuxWrite = func() {
				switch edit {
				case "insert":
					_, _ = sector.GetOrCreateBrick(3, 3, 3)
					sector.PackedBricks[sector.GetPackedIndex(63)] = changed
				case "remove":
					packed := sector.GetPackedIndex(31)
					sector.PackedBricks = append(sector.PackedBricks[:packed], sector.PackedBricks[packed+1:]...)
					sector.BrickMask64 &^= uint64(1) << 31
				case "replace":
					sector.PackedBricks[sector.GetPackedIndex(31)] = changed
				}
			}
			p2bStep(t, m, b, scene)
			header := p2bHeader(t, m, b, sector)
			gotMask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
			if gotMask != mask {
				t.Fatalf("queued mask=%016x captured written topology=%016x", gotMask, mask)
			}
			p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, first)
			if edit != "insert" {
				p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, 3, 3, 1}, later)
			}
			if o.XBrickMap.Revision != revision || !o.XBrickMap.DirtySectors[[3]int{}] {
				t.Fatal("raw topology edit was acknowledged as the completed captured unit")
			}
			s1kReady(t, m, o, false)
			p2bStep(t, m, b, scene)
			if edit == "insert" {
				p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, 3, 3, 3}, changed)
			} else if edit == "replace" {
				p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, 3, 3, 1}, changed)
			} else if binary.LittleEndian.Uint32(p2bHeader(t, m, b, sector)[20:])&(uint32(1)<<31) != 0 {
				t.Fatal("resumed removal kept the removed captured record reachable")
			}
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2bSectorRemovedAfterMaterialQueueSkipsGeometryAndRetriesStructure(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	schedulePutBrick(o, [6]int{}, p2aBrick("uniform", 1))
	b.afterMaterialWrite = func() {
		delete(o.XBrickMap.Sectors, [3]int{})
		o.XBrickMap.StructureDirty = true
	}
	p2bStep(t, m, b, scene)
	if m.VoxelSectorsUploaded != 0 || p2bWrittenBytes(b, "SectorTableBuf")+p2bWrittenBytes(b, "BrickTableBuf")+p2bWrittenBytes(b, "DenseOccupancyBuf") != 0 || !o.XBrickMap.StructureDirty {
		t.Fatal("removed queued sector wrote geometry or lost structural retry")
	}
	p2bStep(t, m, b, scene)
	if len(m.Allocations[o.XBrickMap].Sectors) != 0 {
		t.Fatal("structural retry retained the removed sector")
	}
	s1kReady(t, m, o, true)
}

func (b *p2bNative) WritePayload(page uint32, origin [3]uint32, data []byte) error {
	if err := b.p2aNative.WritePayload(page, origin, data); err != nil {
		return err
	}
	b.writes = append(b.writes, p2bWrite{label: "payload", data: bytes.Clone(data)})
	return nil
}

func p2bFixture(t *testing.T) (*GpuBufferManager, *p2bNative, *core.Scene) {
	t.Helper()
	m, b, scene := p2aFixture(t)
	return m, &p2bNative{p2aNative: b}, scene
}

func p2bStep(t *testing.T, m *GpuBufferManager, b *p2bNative, scene *core.Scene) bool {
	t.Helper()
	b.reset()
	b.writes = nil
	recreated := m.updateVoxelData(scene, b)
	m.updateSectorGrid(scene)
	if m.VoxelUploadBytes != b.contentBytes || b.contentBytes > m.VoxelUploadBudget().MaxBytes {
		t.Fatalf("content bytes charged=%d actual=%d budget=%d", m.VoxelUploadBytes, b.contentBytes, m.VoxelUploadBudget().MaxBytes)
	}
	s1kCharge(t, m, b.s1kNative)
	return recreated
}

func p2bWrittenBytes(b *p2bNative, label string) uint64 {
	var total uint64
	for _, write := range b.writes {
		if write.label == label {
			total += uint64(len(write.data))
		}
	}
	return total
}

func p2bHeader(t *testing.T, m *GpuBufferManager, b *p2bNative, sector *volume.Sector) []byte {
	t.Helper()
	info, ok := m.SectorToInfo[sector]
	if !ok {
		t.Fatal("sector was not allocated")
	}
	start := uint64(info.SlotIndex) * 32
	return b.buffers[m.SectorTableBuf][start : start+32]
}

func p2bRecord(t *testing.T, m *GpuBufferManager, b *p2bNative, sector *volume.Sector, index int) []byte {
	t.Helper()
	_, ok := m.SectorToInfo[sector]
	if !ok {
		t.Fatal("sector was not allocated")
	}
	header := p2bHeader(t, m, b, sector)
	base := binary.LittleEndian.Uint32(header[16:])
	if binary.LittleEndian.Uint32(header[28:]) == 1 {
		mask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
		if mask&(uint64(1)<<index) == 0 {
			t.Fatalf("absent packed local index%d has no physical record", index)
		}
		index = bits.OnesCount64(mask & ((uint64(1) << index) - 1))
	}
	start := uint64(base+uint32(index)) * BrickRecordSize
	return b.buffers[m.BrickTableBuf][start : start+BrickRecordSize]
}

func p2bAssertHeaderLast(t *testing.T, b *p2bNative) {
	t.Helper()
	firstHeader, lastData := -1, -1
	for i, write := range b.writes {
		if write.label == "SectorTableBuf" && firstHeader < 0 {
			firstHeader = i
		}
		if write.label == "BrickTableBuf" || write.label == "DenseOccupancyBuf" || write.label == "payload" {
			lastData = i
		}
	}
	if firstHeader < 0 || firstHeader <= lastData {
		t.Fatal("sector header was queued before its complete record/payload/auxiliary writes")
	}
}

func p2bAssertLookup(t *testing.T, m *GpuBufferManager, b *p2bNative, o *core.VoxelObject, key [3]int, want bool) {
	t.Helper()
	params := b.buffers[m.SectorGridParamsBuf]
	gridSize := binary.LittleEndian.Uint32(params)
	hash := b.buffers[m.SectorGridBuf]
	hashFound := false
	if uint64(gridSize)*32 > uint64(len(hash)) {
		t.Fatal("published hash exceeds physical buffer")
	}
	for i := uint32(0); i < gridSize; i++ {
		row := hash[i*32 : (i+1)*32]
		if binary.LittleEndian.Uint32(row[20:]) == math.MaxUint32 {
			continue
		}
		if binary.LittleEndian.Uint32(row[16:]) == o.RenderVoxelMap().ID && int32(binary.LittleEndian.Uint32(row)) == int32(key[0]) && int32(binary.LittleEndian.Uint32(row[4:])) == int32(key[1]) && int32(binary.LittleEndian.Uint32(row[8:])) == int32(key[2]) {
			hashFound = true
		}
	}
	directFound := false
	if alloc := m.Allocations[o.RenderVoxelMap()]; alloc != nil {
		meta := alloc.DirectLookup
		if meta.LookupMode == LookupModeDirect && meta.TableBase != DirectSectorLookupInvalid {
			local := [3]int64{int64(key[0]) - int64(meta.Origin[0]), int64(key[1]) - int64(meta.Origin[1]), int64(key[2]) - int64(meta.Origin[2])}
			if local[0] >= 0 && local[1] >= 0 && local[2] >= 0 && uint64(local[0]) < uint64(meta.Extent[0]) && uint64(local[1]) < uint64(meta.Extent[1]) && uint64(local[2]) < uint64(meta.Extent[2]) {
				index := uint64(meta.TableBase) + uint64(local[0]) + uint64(meta.Extent[0])*(uint64(local[1])+uint64(meta.Extent[1])*uint64(local[2]))
				data := b.buffers[m.DirectSectorLookupBuf]
				if index*4+4 > uint64(len(data)) {
					t.Fatal("direct lookup metadata exceeds physical buffer")
				}
				word := binary.LittleEndian.Uint32(data[index*4:])
				directFound = word != DirectSectorLookupInvalid
				if want && word != m.SectorToInfo[o.RenderVoxelMap().Sectors[key]].SlotIndex {
					t.Fatalf("direct lookup published slot %d, want current sector slot", word)
				}
			}
		}
	}
	if hashFound != want || directFound != want {
		t.Fatalf("sector lookup hash=%v direct=%v, want published=%v", hashFound, directFound, want)
	}
}

func TestP2bSparseAndDenseFullSectorWriteOnlySelectedRecords(t *testing.T) {
	for _, count := range []int{1, 64} {
		t.Run(map[int]string{1: "sparse", 64: "dense"}[count], func(t *testing.T) {
			m, b, scene := p2bFixture(t)
			o := p2aObject(scene, 1)
			bricks := make([]*volume.Brick, count)
			for i := range bricks {
				bricks[i] = p2aBrick("uniform", byte(i+1))
				schedulePutBrick(o, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, bricks[i])
			}
			p2bStep(t, m, b, scene)
			if m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != count || p2bWrittenBytes(b, "BrickTableBuf") != uint64(count*BrickRecordSize) || p2bWrittenBytes(b, "DenseOccupancyBuf") != uint64(count*VoxelAuxRecordBytes) {
				t.Fatalf("%d occupied records: sectors=%d bricks=%d physical record bytes=%d aux bytes=%d", count, m.VoxelSectorsUploaded, m.VoxelBricksUploaded, p2bWrittenBytes(b, "BrickTableBuf"), p2bWrittenBytes(b, "DenseOccupancyBuf"))
			}
			if m.VoxelUploadBytes != uint64(64+32+count*(BrickRecordSize+VoxelAuxRecordBytes)) {
				t.Fatal("sparse full-sector bytes include unreachable holes or omit actual records")
			}
			for i, brick := range bricks {
				p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, brick)
			}
			p2bAssertHeaderLast(t, b)
			p2bAssertLookup(t, m, b, o, [3]int{}, true)
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2bSparseSectorMasksPreserveLogicalIndicesAcrossLowHighWords(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	indices := []int{0, 31, 32, 63}
	for _, i := range indices {
		schedulePutBrick(o, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, p2aBrick("uniform", byte(i+1)))
	}
	p2bStep(t, m, b, scene)
	if m.VoxelBricksUploaded != 4 || p2bWrittenBytes(b, "BrickTableBuf") != 4*BrickRecordSize {
		t.Fatal("sparse bit-boundary sector wrote records outside its four occupied indices")
	}
	sector := o.XBrickMap.Sectors[[3]int{}]
	header := p2bHeader(t, m, b, sector)
	if binary.LittleEndian.Uint32(header[20:]) != 0x80000001 || binary.LittleEndian.Uint32(header[24:]) != 0x80000001 {
		t.Fatal("sparse full-sector publication changed low/high mask bit addressing")
	}
	for _, i := range indices {
		p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, sector.GetBrick(i%4, (i/4)%4, i/16))
	}
	p2bAssertHeaderLast(t, b)
	s1kReady(t, m, o, true)
}

func TestP2bFirstHeaderPublicationUsesExactSparseBudgetAndLookupGate(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	brick := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, brick)
	// First admit only the private one-row material upload. The freshly
	// allocated sector must remain unreachable until its full header succeeds.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64})
	p2bStep(t, m, b, scene)
	if m.VoxelMaterialsUploaded != 1 || m.VoxelSectorsUploaded != 0 {
		t.Fatal("material-only publication fixture did not isolate the fresh sector")
	}
	p2bAssertLookup(t, m, b, o, [3]int{}, false)
	s1kReady(t, m, o, false)
	before := bytes.Clone(p2bHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}]))
	const sectorBytes = 32 + BrickRecordSize + VoxelAuxRecordBytes
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: sectorBytes - 1, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(before, p2bHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}])) || !o.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("one-byte-short sector publication changed committed header/content or acknowledged dirtiness")
	}
	p2bAssertLookup(t, m, b, o, [3]int{}, false)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: sectorBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != sectorBytes || m.VoxelBricksUploaded != 1 || m.VoxelSectorsUploaded != 1 {
		t.Fatal("exact sparse sector budget failed to publish its one current record")
	}
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, brick)
	p2bAssertHeaderLast(t, b)
	p2bAssertLookup(t, m, b, o, [3]int{}, true)
	s1kReady(t, m, o, true)
}

func TestP2bMaterialAndSparseSectorShareOneExactFrameBudget(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	schedulePutBrick(o, [6]int{}, p2aBrick("uniform", 1))
	const bytes = 64 + 32 + BrickRecordSize + VoxelAuxRecordBytes
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: bytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != bytes || m.VoxelMaterialsUploaded != 1 || m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 1 {
		t.Fatal("material upload prevented a fitting one-record full-sector publication")
	}
	s1kReady(t, m, o, true)
}

func TestP2bFullSectorRemovalMakesRemovedRecordUnreachable(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	first, removed := p2aBrick("uniform", 1), p2aBrick("uniform", 2)
	schedulePutBrick(o, [6]int{}, first)
	schedulePutBrick(o, [6]int{0, 0, 0, 3, 3, 3}, removed)
	p2bStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldHeader := bytes.Clone(p2bHeader(t, m, b, sector))
	oldRecord := bytes.Clone(p2bRecord(t, m, b, sector, 63))
	s1l3ClearIndex(o, 63)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	oldBuffer := m.BrickTableBuf
	oldStart := uint64(binary.LittleEndian.Uint32(oldHeader[16:])) * BrickRecordSize
	oldLength := uint64(2 * BrickRecordSize)
	if binary.LittleEndian.Uint32(oldHeader[28:]) == 0 {
		oldLength = 64 * BrickRecordSize
	}
	oldRows := bytes.Clone(b.buffers[oldBuffer][oldStart : oldStart+oldLength])
	const sectorBytes = 32 + BrickRecordSize + VoxelAuxRecordBytes
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: sectorBytes - 1, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if !bytes.Equal(oldHeader, p2bHeader(t, m, b, sector)) || !bytes.Equal(oldRecord, p2bRecord(t, m, b, sector, 63)) || m.VoxelUploadBytes != 0 {
		t.Fatal("deferred removal altered committed header or old occupied record")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: sectorBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelBricksUploaded != 1 || m.VoxelUploadBytes != sectorBytes || !bytes.Equal(oldRows, b.buffers[oldBuffer][oldStart:oldStart+oldLength]) {
		t.Fatal("packed removal overwrote old range or charged removed records")
	}
	if binary.LittleEndian.Uint32(p2bHeader(t, m, b, sector)[24:]) != 0 {
		t.Fatal("removed high-bit brick remained reachable through sector mask")
	}
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, first)
	p2bAssertHeaderLast(t, b)
	s1kReady(t, m, o, true)
}

func TestP2bReusedSectorHidesOldHeaderAndLeavesUnreachableHoles(t *testing.T) {
	m, b, scene := p2bFixture(t)
	old := p2aObject(scene, 1)
	schedulePutBrick(old, [6]int{0, 0, 0, 3, 3, 3}, p2aBrick("uniform", 1))
	p2bStep(t, m, b, scene)
	oldInfo := m.SectorToInfo[old.XBrickMap.Sectors[[3]int{}]]
	oldBuffer := m.BrickTableBuf
	oldHeader := p2bHeader(t, m, b, old.XBrickMap.Sectors[[3]int{}])
	oldOffset := uint64(binary.LittleEndian.Uint32(oldHeader[16:])) * BrickRecordSize
	if binary.LittleEndian.Uint32(oldHeader[28:]) == 0 {
		oldOffset += 63 * BrickRecordSize
	}
	stale := bytes.Clone(b.buffers[oldBuffer][oldOffset : oldOffset+BrickRecordSize])
	scene.Objects = nil
	p2bStep(t, m, b, scene)
	next := p2aObject(scene, 1)
	next.XBrickMap.ID = 99
	brick := p2aBrick("uniform", 2)
	schedulePutBrick(next, [6]int{}, brick)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64})
	p2bStep(t, m, b, scene)
	info := m.SectorToInfo[next.XBrickMap.Sectors[[3]int{}]]
	if info.SlotIndex != oldInfo.SlotIndex {
		t.Fatal("fixture did not reuse the released sector slot")
	}
	p2bAssertLookup(t, m, b, next, [3]int{}, false)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32 + BrickRecordSize + VoxelAuxRecordBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	sector := next.XBrickMap.Sectors[[3]int{}]
	if m.VoxelBricksUploaded != 1 || !bytes.Equal(stale, b.buffers[oldBuffer][oldOffset:oldOffset+BrickRecordSize]) || binary.LittleEndian.Uint32(p2bHeader(t, m, b, sector)[24:]) != 0 {
		t.Fatal("reused fresh sector wrote unreachable stale holes or exposed old high-bit header")
	}
	p2aAssertBrick(t, m, b.p2aNative, next.XBrickMap, [6]int{}, brick)
	p2bAssertLookup(t, m, b, next, [3]int{}, true)
	s1kReady(t, m, next, true)
}

func TestP2bEmptySectorHeaderCanPublishWithZeroBrickBudget(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64})
	p2bStep(t, m, b, scene)
	p2bAssertLookup(t, m, b, o, [3]int{}, false)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 31, MaxSectors: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != 0 || m.VoxelSectorsUploaded != 0 {
		t.Fatal("short empty-header budget published a sector")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32, MaxSectors: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != 32 || m.VoxelBricksUploaded != 0 || m.VoxelSectorsUploaded != 1 || p2bWrittenBytes(b, "BrickTableBuf") != 0 {
		t.Fatal("empty full-sector header consumed brick budget or wrote 64 holes")
	}
	p2bAssertLookup(t, m, b, o, [3]int{}, true)
	s1kReady(t, m, o, true)
}

func TestP2bSharedMapsAndSectorPointersUseSparseRecords(t *testing.T) {
	for _, sharedMap := range []bool{true, false} {
		t.Run(map[bool]string{true: "map", false: "sector"}[sharedMap], func(t *testing.T) {
			m, b, scene := p2bFixture(t)
			a := p2aObject(scene, 1)
			brick := p2aBrick("uniform", 7)
			schedulePutBrick(a, [6]int{}, brick)
			other := p2aObject(scene, 1)
			other.XBrickMap.ID = 22
			if sharedMap {
				other.XBrickMap = a.XBrickMap
			} else {
				other.XBrickMap.Sectors[[3]int{}] = a.XBrickMap.Sectors[[3]int{}]
			}
			p2bStep(t, m, b, scene)
			units := m.VoxelSectorsUploaded
			if units == 0 || units > 2 || (sharedMap && units != 1) || m.VoxelBricksUploaded != units || p2bWrittenBytes(b, "BrickTableBuf") != uint64(units)*BrickRecordSize {
				t.Fatalf("shared geometry sectors=%d records=%d physical record bytes=%d", units, m.VoxelBricksUploaded, p2bWrittenBytes(b, "BrickTableBuf"))
			}
			if m.VoxelUploadBytes != 128+uint64(units)*(32+BrickRecordSize+VoxelAuxRecordBytes) {
				t.Fatal("shared sparse geometry charged holes or duplicated shared-map work")
			}
			p2aAssertBrick(t, m, b.p2aNative, a.XBrickMap, [6]int{}, brick)
			for _, owner := range []*core.VoxelObject{a, other} {
				p2bAssertLookup(t, m, b, owner, [3]int{}, true)
				s1kReady(t, m, owner, true)
			}
			scene.Objects = []*core.VoxelObject{other}
			p2bStep(t, m, b, scene)
			p2aAssertBrick(t, m, b.p2aNative, other.XBrickMap, [6]int{}, brick)
			s1kReady(t, m, other, true)
		})
	}
}

func TestP2bUnknownCommittedHeadersKeepDenseCompatibility(t *testing.T) {
	o := scheduleObject(7, [3]int{})
	schedulePutBrick(o, [6]int{}, scheduleBrick("uniform"))
	m, scene := scheduleFixture(t, o)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	scheduleRun(t, m, scene)
	if m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 64 || m.VoxelUploadBytes != 32+64*BrickRecordSize+VoxelAuxRecordBytes {
		t.Fatal("foreign/manual header lost its conservative dense full-sector fallback")
	}
}

func TestP2bLateFreshRecordDemandRechecksBudgetBeforePublication(t *testing.T) {
	m, b, scene := p2bFixture(t)
	// Two physical auxiliary rows isolate selection/budget validation from growth.
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 2304)
	o := p2aObject(scene, 1)
	first, late := p2aBrick("uniform", 1), p2aBrick("uniform", 2)
	schedulePutBrick(o, [6]int{}, first)
	revision := o.XBrickMap.Revision
	b.afterMaterialWrite = func() {
		sector := o.XBrickMap.Sectors[[3]int{}]
		_, _ = sector.GetOrCreateBrick(3, 3, 3)
		sector.PackedBricks[sector.GetPackedIndex(63)] = late
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64 + 32 + BrickRecordSize + VoxelAuxRecordBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if o.XBrickMap.Revision != revision || m.VoxelMaterialsUploaded != 1 || m.VoxelSectorsUploaded != 0 || m.VoxelBricksUploaded != 0 || m.VoxelUploadBytes != 64 || !o.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("late no-revision fresh record bypassed budget revalidation or lost dirtiness")
	}
	if p2bWrittenBytes(b, "SectorTableBuf")+p2bWrittenBytes(b, "BrickTableBuf")+p2bWrittenBytes(b, "DenseOccupancyBuf") != 0 {
		t.Fatal("over-budget late demand partially wrote geometry")
	}
	p2bAssertLookup(t, m, b, o, [3]int{}, false)
	s1kReady(t, m, o, false)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32 + 2*(BrickRecordSize+VoxelAuxRecordBytes), MaxSectors: 1, MaxBricks: 2})
	p2bStep(t, m, b, scene)
	if m.VoxelBricksUploaded != 2 {
		t.Fatal("late fresh record was skipped on resumed full-sector publication")
	}
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, first)
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{0, 0, 0, 3, 3, 3}, late)
	p2bAssertHeaderLast(t, b)
	s1kReady(t, m, o, true)
}

func TestP2bSparseSectorMirrorsChargeBytesOncePerGeneration(t *testing.T) {
	m, b, scene := p2bFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	o := p2aObject(scene, 1)
	brick := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, brick)
	p2bStep(t, m, b, scene)
	oldHeader := bytes.Clone(p2bHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}]))
	before := s1kPublished(m)
	arrival := p2aObject(scene, 32)
	arrival.XBrickMap.ID = 33
	for key := range arrival.XBrickMap.Sectors {
		schedulePutBrick(arrival, [6]int{key[0], key[1], key[2]}, p2aBrick("uniform", 8))
	}
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30})
	p2bStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending || s1kPublished(m) != before || m.VoxelGPUAdmissionStats().StagingBytes == 0 {
		t.Fatal("fixture did not establish unpublished geometry staging buffers")
	}
	// In-place data edit preserves its committed pointer while the full-sector
	// upload must refresh record/auxiliary/header bytes in both generations.
	for i := 64; i < len(brick.PrecomputedAux); i++ {
		brick.PrecomputedAux[i] = 3
	}
	o.XBrickMap.Revision++
	o.XBrickMap.DirtySectors[[3]int{}] = true
	const mirroredBytes = 2 * (32 + BrickRecordSize + VoxelAuxRecordBytes)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: mirroredBytes - 1, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldHeader, p2bHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}])) || !o.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("one-byte-short mirrored transaction altered committed geometry")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: mirroredBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelUploadBytes != mirroredBytes || m.VoxelBricksUploaded != 1 || m.VoxelSectorsUploaded != 1 || p2bWrittenBytes(b, "SectorTableBuf") != 64 || p2bWrittenBytes(b, "BrickTableBuf") != 2*BrickRecordSize || p2bWrittenBytes(b, "DenseOccupancyBuf") != 2*VoxelAuxRecordBytes {
		t.Fatal("mirrored sparse unit did not charge physical bytes and one logical record")
	}
	p2bAssertHeaderLast(t, b)
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, brick)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 1 << 30})
	for i := 0; i < 8 && m.VoxelGPUWorkStats().Pending; i++ {
		p2bStep(t, m, b, scene)
		if m.VoxelUploadBytes != 0 {
			t.Fatal("paused content budget wrote during generation publication")
		}
	}
	if m.VoxelGPUWorkStats().Pending || s1kPublished(m) == before {
		t.Fatal("staged generation failed to publish")
	}
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, brick)
}

func TestP2bMixedPayloadCompletesBeforeSparseHeader(t *testing.T) {
	m, b, scene := p2bFixture(t)
	o := p2aObject(scene, 1)
	brick := p2aBrick("mixed", 5)
	schedulePutBrick(o, [6]int{}, brick)
	const exactBytes = 64 + 32 + BrickRecordSize + VoxelAuxRecordBytes + 512
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exactBytes, MaxSectors: 1, MaxBricks: 1})
	p2bStep(t, m, b, scene)
	if m.VoxelBricksUploaded != 1 || m.VoxelUploadBytes != exactBytes || p2bWrittenBytes(b, "payload") != 512 {
		t.Fatal("mixed sparse publication did not charge its actual payload and record")
	}
	p2bAssertHeaderLast(t, b)
	p2aAssertBrick(t, m, b.p2aNative, o.XBrickMap, [6]int{}, brick)
	s1kReady(t, m, o, true)
}
