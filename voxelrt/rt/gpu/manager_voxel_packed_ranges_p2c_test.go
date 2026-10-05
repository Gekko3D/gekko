package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/bits"
	"strconv"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

var errP2cCapacity = errors.New("P2c brick record capacity exhausted")

// Fence completion is supplied at the same native boundary as allocation and
// writes. Advancing update frames alone must never make a submitted range safe.
type p2cNative struct {
	*p2bNative
	completed     map[uint64]bool
	maxBrickBytes uint64
}

func (b *p2cNative) SubmissionComplete(_ *wgpu.Queue, index uint64) bool { return b.completed[index] }
func (b *p2cNative) CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error) {
	if label == "BrickTableBuf" && b.maxBrickBytes != 0 && size > b.maxBrickBytes {
		return nil, errP2cCapacity
	}
	return b.p2bNative.CreateBuffer(label, size, uniform)
}

func p2cFixture(t *testing.T) (*GpuBufferManager, *p2cNative, *core.Scene) {
	t.Helper()
	m, b, s := p2bFixture(t)
	return m, &p2cNative{p2bNative: b, completed: make(map[uint64]bool)}, s
}
func p2cStep(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene) {
	t.Helper()
	b.reset()
	b.writes = nil
	m.updateVoxelData(s, b)
	m.updateSectorGrid(s)
	if m.VoxelUploadBytes != b.contentBytes || b.contentBytes > m.VoxelUploadBudget().MaxBytes {
		t.Fatalf("charged=%d written=%d budget=%d", m.VoxelUploadBytes, b.contentBytes, m.VoxelUploadBudget().MaxBytes)
	}
	s1kCharge(t, m, b.s1kNative)
}
func p2cHeader(t *testing.T, m *GpuBufferManager, b *p2cNative, sector *volume.Sector) (uint32, uint64) {
	t.Helper()
	h := p2bHeader(t, m, b.p2bNative, sector)
	if binary.LittleEndian.Uint32(h[28:]) != 1 {
		t.Fatalf("managed header layout=%d, want packed1", binary.LittleEndian.Uint32(h[28:]))
	}
	return binary.LittleEndian.Uint32(h[16:]), uint64(binary.LittleEndian.Uint32(h[20:])) | uint64(binary.LittleEndian.Uint32(h[24:]))<<32
}
func p2cAssertRow(t *testing.T, m *GpuBufferManager, b *p2cNative, sector *volume.Sector, index int, brick *volume.Brick) {
	t.Helper()
	base, mask := p2cHeader(t, m, b, sector)
	if mask&(uint64(1)<<index) == 0 {
		t.Fatalf("local%d absent from published mask", index)
	}
	rank := bits.OnesCount64(mask & ((uint64(1) << index) - 1))
	offset := uint64(base+uint32(rank)) * BrickRecordSize
	data := b.buffers[m.BrickTableBuf]
	if offset+BrickRecordSize > uint64(len(data)) {
		t.Fatal("ranked record outside physical buffer")
	}
	if binary.LittleEndian.Uint32(data[offset:]) != brick.AtlasOffset || binary.LittleEndian.Uint32(data[offset+20:]) != brick.Flags {
		t.Fatalf("local%d ranked material/flags differ from captured brick", index)
	}
	aux := uint64(binary.LittleEndian.Uint32(data[offset+24:])) * 4
	if aux+VoxelAuxRecordBytes > uint64(len(b.buffers[m.DenseOccupancyBuf])) || !bytes.Equal(b.buffers[m.DenseOccupancyBuf][aux:aux+VoxelAuxRecordBytes], brick.PrecomputedAux) {
		t.Fatalf("local%d ranked row contains wrong auxiliary bytes", index)
	}
}
func p2cPut(o *core.VoxelObject, index int, brick *volume.Brick) {
	schedulePutBrick(o, [6]int{0, 0, 0, index % 4, (index / 4) % 4, index / 16}, brick)
}

func TestP2cSparsePhysicalBrickCapacityAndBoundaryRanks(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	o := p2aObject(scene, 128)
	for key := range o.XBrickMap.Sectors {
		schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p2aBrick("uniform", byte(key[0]+1)))
	}
	p2cStep(t, m, b, scene)
	physical := b.BufferSize(m.BrickTableBuf)
	if physical < 128*BrickRecordSize || physical > 2*128*BrickRecordSize+256 {
		t.Fatalf("128 occupied records in128sectors reserved%dbytes", physical)
	}
	for key, sector := range o.XBrickMap.Sectors {
		p2cAssertRow(t, m, b, sector, 0, sector.GetBrick(0, 0, 0))
		p2bAssertLookup(t, m, b.p2bNative, o, key, true)
	}
	s1kReady(t, m, o, true)
	// Low/high word boundaries must map to four adjacent physical records.
	scene.Objects = nil
	p2cStep(t, m, b, scene)
	next := p2aObject(scene, 1)
	bricks := make(map[int]*volume.Brick)
	for _, index := range []int{0, 31, 32, 63} {
		bricks[index] = p2aBrick("uniform", byte(index+1))
		p2cPut(next, index, bricks[index])
	}
	p2cStep(t, m, b, scene)
	_, mask := p2cHeader(t, m, b, next.XBrickMap.Sectors[[3]int{}])
	if mask != 0x8000000180000001 {
		t.Fatalf("boundary mask=%x", mask)
	}
	for index, brick := range bricks {
		p2cAssertRow(t, m, b, next.XBrickMap.Sectors[[3]int{}], index, brick)
	}
}

func TestP2cCapacityClassesKeepContiguousOccupiedRows(t *testing.T) {
	for _, count := range []int{0, 1, 2, 3, 5, 9, 17, 33, 64} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			m, b, scene := p2cFixture(t)
			o := p2aObject(scene, 1)
			bricks := make([]*volume.Brick, count)
			for i := range bricks {
				bricks[i] = p2aBrick("uniform", byte(i+1))
				p2cPut(o, i, bricks[i])
			}
			p2cStep(t, m, b, scene)
			_, mask := p2cHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}])
			if bits.OnesCount64(mask) != count {
				t.Fatal("packed mask occupancy changed")
			}
			for i, brick := range bricks {
				p2cAssertRow(t, m, b, o.XBrickMap.Sectors[[3]int{}], i, brick)
			}
			if p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != uint64(count*BrickRecordSize) {
				t.Fatal("class padding was uploaded as live records")
			}
			class := 0
			if count > 0 {
				class = 1
				for class < count {
					class *= 2
				}
			}
			if !m.RetainVoxelMap(o.XBrickMap) {
				t.Fatal("packed allocation was not retainable")
			}
			wantBytes := uint64(256 + 32 + class*BrickRecordSize + count*VoxelAuxRecordBytes)
			if got := m.RetainedVoxelMapStats().Bytes; got != wantBytes {
				t.Fatalf("occupied%d class%d assignedbytes=%d want%d", count, class, got, wantBytes)
			}
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2cMembershipRelocatesSameClassAndStableMaskUpdatesInPlace(t *testing.T) {
	m, b, scene := p2cFixture(t)
	o := p2aObject(scene, 1)
	first := p2aBrick("uniform", 1)
	p2cPut(o, 0, first)
	p2cStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldBase, _ := p2cHeader(t, m, b, sector)
	oldRow := bytes.Clone(b.buffers[m.BrickTableBuf][uint64(oldBase)*32 : uint64(oldBase+1)*32])
	s1l3ClearIndex(o, 0)
	next := p2aBrick("uniform", 2)
	p2cPut(o, 63, next)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, scene)
	base, mask := p2cHeader(t, m, b, sector)
	if base == oldBase || mask != uint64(1)<<63 || !bytes.Equal(oldRow, b.buffers[m.BrickTableBuf][uint64(oldBase)*32:uint64(oldBase+1)*32]) {
		t.Fatal("same-class membership change reused or overwrote reachable old range")
	}
	p2cAssertRow(t, m, b, sector, 63, next)
	replacement := p2aBrick("uniform", 3)
	p2cPut(o, 63, replacement)
	o.XBrickMap.DirtyBricks[[6]int{0, 0, 0, 3, 3, 3}] = true
	p2cStep(t, m, b, scene)
	stableBase, _ := p2cHeader(t, m, b, sector)
	if stableBase != base || m.VoxelSectorsUploaded != 0 || m.VoxelBricksUploaded != 1 {
		t.Fatal("stable-mask dirty brick relocated or rewrote a full sector")
	}
	p2cAssertRow(t, m, b, sector, 63, replacement)
}

func TestP2cDirtyBrickMembershipPromotionIsAtomicUnderSectorBudget(t *testing.T) {
	m, b, scene := p2cFixture(t)
	o := p2aObject(scene, 1)
	first := p2aBrick("uniform", 1)
	p2cPut(o, 31, first)
	p2cStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	oldBase, _ := p2cHeader(t, m, b, sector)
	second := p2aBrick("uniform", 2)
	p2cPut(o, 32, second)
	o.XBrickMap.DirtyBricks[[6]int{0, 0, 0, 0, 0, 2}] = true
	const bytesNeeded = 32 + 2*(BrickRecordSize+VoxelAuxRecordBytes)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: bytesNeeded, MaxBricks: 2})
	p2cStep(t, m, b, scene)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) {
		t.Fatal("membership-changing dirty brick bypassed full-sector budget")
	}
	s1kReady(t, m, o, false)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: bytesNeeded - 1, MaxSectors: 1, MaxBricks: 2})
	p2cStep(t, m, b, scene)
	if m.VoxelUploadBytes != 0 {
		t.Fatal("short promoted full-sector budget wrote partial packed rows")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: bytesNeeded, MaxSectors: 1, MaxBricks: 2})
	p2cStep(t, m, b, scene)
	base, _ := p2cHeader(t, m, b, sector)
	if base == oldBase || m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 2 || m.VoxelUploadBytes != bytesNeeded {
		t.Fatal("promoted membership transaction omitted whole occupied set/header")
	}
	p2cAssertRow(t, m, b, sector, 31, first)
	p2cAssertRow(t, m, b, sector, 32, second)
	s1kReady(t, m, o, true)
}

func TestP2cRelocationNeedsSimultaneousOldAndNewPhysicalCapacity(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	o := p2aObject(scene, 1)
	for i := 0; i < 8; i++ {
		p2cPut(o, i, p2aBrick("uniform", byte(i+1)))
	}
	p2cStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	base, _ := p2cHeader(t, m, b, sector)
	oldHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	oldRows := bytes.Clone(b.buffers[m.BrickTableBuf][base*32 : (base+8)*32])
	s1l3ClearIndex(o, 0)
	fresh := p2aBrick("uniform", 9)
	p2cPut(o, 63, fresh)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, scene)
	if !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) || !bytes.Equal(oldRows, b.buffers[m.BrickTableBuf][base*32:(base+8)*32]) || m.VoxelUploadBytes != 0 {
		t.Fatal("relocation spent its live old range as capacity credit")
	}
	s1kReady(t, m, o, false)
	b.maxBrickBytes = 512
	p2cStep(t, m, b, scene)
	next, _ := p2cHeader(t, m, b, sector)
	if next < base+8 && base < next+8 {
		t.Fatal("successful relocated range overlaps the retired old range")
	}
	p2cAssertRow(t, m, b, sector, 63, fresh)
	s1kReady(t, m, o, true)
}

func TestP2cRetiredRangesWaitForSubmissionCompletionAndCoalesce(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	a, c, keep := p2aObject(scene, 1), p2aObject(scene, 1), p2aObject(scene, 1)
	a.XBrickMap.ID = 41
	c.XBrickMap.ID = 42
	keep.XBrickMap.ID = 43
	for i := 0; i < 2; i++ {
		p2cPut(a, i, p2aBrick("uniform", 1))
		p2cPut(c, i, p2aBrick("uniform", 2))
	}
	for i := 0; i < 4; i++ {
		p2cPut(keep, i, p2aBrick("uniform", 3))
	}
	p2cStep(t, m, b, scene)
	aBase, _ := p2cHeader(t, m, b, a.XBrickMap.Sectors[[3]int{}])
	cBase, _ := p2cHeader(t, m, b, c.XBrickMap.Sectors[[3]int{}])
	keepBase, _ := p2cHeader(t, m, b, keep.XBrickMap.Sectors[[3]int{}])
	if aBase+2 != cBase && cBase+2 != aBase {
		t.Fatal("fixture did not allocate adjacent two-row intervals")
	}
	scene.Objects = []*core.VoxelObject{keep}
	p2cStep(t, m, b, scene)
	if m.Allocations[a.XBrickMap] != nil || m.Allocations[c.XBrickMap] != nil {
		t.Fatal("removed owners remained allocated instead of final-owner range retirement")
	}
	arrival := p2aObject(scene, 1)
	arrival.XBrickMap.ID = 44
	arrival.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	for i := 0; i < 4; i++ {
		p2cPut(arrival, i, p2aBrick("uniform", 4))
	}
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		p2cStep(t, m, b, scene)
	}
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("unsubmitted retired intervals became reusable after frame aging")
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(42))
	p2cStep(t, m, b, scene)
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("submitted but incomplete intervals were reused")
	}
	b.completed[41] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, scene)
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("completion of unrelated fence released current retired intervals")
	}
	b.completed[42] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, scene)
	next, _ := p2cHeader(t, m, b, arrival.XBrickMap.Sectors[[3]int{}])
	want := min(aBase, cBase)
	if next != want || next == keepBase || b.BufferSize(m.BrickTableBuf) != 256 {
		t.Fatal("completed adjacent intervals did not coalesce for a four-row allocation")
	}
	for i := 0; i < 4; i++ {
		p2cAssertRow(t, m, b, keep.XBrickMap.Sectors[[3]int{}], i, keep.XBrickMap.Sectors[[3]int{}].GetBrick(i, 0, 0))
	}
	s1kReady(t, m, arrival, true)
	s1kReady(t, m, keep, true)
}

func TestP2cDeniedRangeReservationDoesNotLeakIntoLaterAdmission(t *testing.T) {
	m, b, scene := p2cFixture(t)
	// Preseed independent material/auxiliary demand before the first allocation;
	// only the packed record range is intentionally capacity constrained.
	b.buffers[m.MaterialBuf] = make([]byte, 3*materialBlockCapacity*64)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 11008)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	old, keep := p2aObject(scene, 1), p2aObject(scene, 1)
	old.XBrickMap.ID = 50
	keep.XBrickMap.ID = 51
	for i := 0; i < 2; i++ {
		p2cPut(old, i, p2aBrick("uniform", 1))
	}
	for i := 0; i < 4; i++ {
		p2cPut(keep, i, p2aBrick("uniform", 2))
	}
	p2cStep(t, m, b, scene)
	sector := old.XBrickMap.Sectors[[3]int{}]
	oldHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	oldBase, _ := p2cHeader(t, m, b, sector)
	old.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	p2cPut(old, 31, p2aBrick("uniform", 3))
	p2cPut(old, 63, p2aBrick("uniform", 4))
	old.XBrickMap.DirtySectors[[3]int{}] = true
	later := p2aObject(scene, 1)
	later.XBrickMap.ID = 52
	later.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	later.VoxelUploadOrder = 100
	fresh := p2aBrick("uniform", 5)
	p2cPut(later, 0, fresh)
	p2cStep(t, m, b, scene)
	if !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) {
		t.Fatal("denied relocation changed live old header")
	}
	laterBase, _ := p2cHeader(t, m, b, later.XBrickMap.Sectors[[3]int{}])
	if laterBase >= oldBase && laterBase < oldBase+2 {
		t.Fatal("denied relocation lent old live rows to later admission")
	}
	p2cAssertRow(t, m, b, later.XBrickMap.Sectors[[3]int{}], 0, fresh)
	s1kReady(t, m, later, true)
	s1kReady(t, m, old, false)
	s1kReady(t, m, keep, true)
}

func TestP2cSharedPhysicalSectorPublishesOneRangeAcrossOwners(t *testing.T) {
	m, b, scene := p2cFixture(t)
	a, other := p2aObject(scene, 1), p2aObject(scene, 1)
	a.XBrickMap.ID = 61
	other.XBrickMap.ID = 62
	first := p2aBrick("uniform", 1)
	p2cPut(a, 31, first)
	other.XBrickMap.Sectors[[3]int{}] = a.XBrickMap.Sectors[[3]int{}]
	p2cStep(t, m, b, scene)
	sector := a.XBrickMap.Sectors[[3]int{}]
	oldBase, _ := p2cHeader(t, m, b, sector)
	beforeA, beforeOther := m.Allocations[a.XBrickMap].shadowUploadEpoch, m.Allocations[other.XBrickMap].shadowUploadEpoch
	if !m.RetainVoxelMap(a.XBrickMap) || !m.RetainVoxelMap(other.XBrickMap) {
		t.Fatal("shared maps were not retainable")
	}
	s1l3ClearIndex(a, 31)
	fresh := p2aBrick("uniform", 2)
	p2cPut(a, 63, fresh)
	a.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, scene)
	base, mask := p2cHeader(t, m, b, sector)
	if base == oldBase || mask != uint64(1)<<63 {
		t.Fatal("shared physical membership did not relocate to one committed packed header")
	}
	p2cAssertRow(t, m, b, sector, 63, fresh)
	if m.Allocations[a.XBrickMap].shadowUploadEpoch <= beforeA || m.Allocations[other.XBrickMap].shadowUploadEpoch <= beforeOther {
		t.Fatal("physical shared-sector write did not invalidate each owner shadow epoch")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 2*(256+32+BrickRecordSize+VoxelAuxRecordBytes) {
		t.Fatalf("shared captured receipts charged%d bytes after oldpointer release", got)
	}
	// A stale other-map dirty unit must use the physical mask, not reinterpret
	// this new range using its old local receipt or emit a removed nil record.
	other.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, scene)
	same, _ := p2cHeader(t, m, b, sector)
	if same != base || m.VoxelBricksUploaded != 1 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 32 {
		t.Fatal("stale shared-owner receipt caused divergent range or nil packed row")
	}
	m.ReleaseRetainedVoxelMap(a.XBrickMap)
	scene.Objects = []*core.VoxelObject{other}
	p2cStep(t, m, b, scene)
	p2cAssertRow(t, m, b, sector, 63, fresh)
	p2bAssertLookup(t, m, b.p2bNative, other, [3]int{}, true)
	s1kReady(t, m, other, true)
}

func TestP2cLegacyDensePrefixAndPackedOwnershipFallbackStayDisjoint(t *testing.T) {
	m, b, scene := p2cFixture(t)
	// Imported public dense block1 owns records64..127; allocator prefix0..127
	// is reserved before managed packing starts, even when block0 is unoccupied.
	legacy := scheduleObject(71, [3]int{})
	p2cPut(legacy, 63, p2aBrick("uniform", 7))
	legacy.XBrickMap.ClearDirty()
	m.Allocations[legacy.XBrickMap] = testObjectGpuAllocationForMap(legacy.XBrickMap)
	sector := legacy.XBrickMap.Sectors[[3]int{}]
	slot := m.SectorAlloc.Alloc()
	m.BrickAlloc.Alloc()
	m.BrickAlloc.Alloc()
	m.SectorToInfo[sector] = SectorGpuInfo{SlotIndex: slot, BrickTableIndex: 64}
	b.buffers[m.BrickTableBuf] = make([]byte, 128*BrickRecordSize+256)
	for i := 0; i < 128*BrickRecordSize; i++ {
		b.buffers[m.BrickTableBuf][i] = 0x7d
	}
	header := b.buffers[m.SectorTableBuf][slot*32 : (slot+1)*32]
	binary.LittleEndian.PutUint32(header[16:], 64)
	binary.LittleEndian.PutUint32(header[24:], 0x80000000)
	oldHeader := bytes.Clone(header)
	oldRows := bytes.Clone(b.buffers[m.BrickTableBuf][:128*BrickRecordSize])
	scene.Objects = []*core.VoxelObject{legacy}
	managed := p2aObject(scene, 1)
	managed.XBrickMap.ID = 72
	brick := p2aBrick("uniform", 8)
	p2cPut(managed, 32, brick)
	p2cStep(t, m, b, scene)
	managedSector := managed.XBrickMap.Sectors[[3]int{}]
	base, _ := p2cHeader(t, m, b, managedSector)
	if base < 128 || !bytes.Equal(oldRows, b.buffers[m.BrickTableBuf][:128*BrickRecordSize]) || !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) {
		t.Fatal("packed allocation overlapped or reinterpreted imported dense prefix")
	}
	p2cAssertRow(t, m, b, managedSector, 32, brick)
	// A public allocation header replacement invalidates managed receipt proof,
	// but cannot change the physical sector's established packed interpretation.
	m.Allocations[managed.XBrickMap] = testObjectGpuAllocationForMap(managed.XBrickMap)
	managed.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, scene)
	same, _ := p2cHeader(t, m, b, managedSector)
	if same != base || m.VoxelBricksUploaded != 1 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 32 {
		t.Fatal("legacy ownership fallback reinterpreted a committed packed sector as dense")
	}
	p2cAssertRow(t, m, b, managedSector, 32, brick)
}

func TestP2cNonemptyToEmptyPublishesHeaderOnlyAndRetiresOldRange(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	o := p2aObject(scene, 1)
	for i := 0; i < 5; i++ {
		p2cPut(o, i, p2aBrick("uniform", byte(i+1)))
	}
	p2cStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldBase, _ := p2cHeader(t, m, b, sector)
	oldBuffer := m.BrickTableBuf
	oldRows := bytes.Clone(b.buffers[oldBuffer][oldBase*32 : (oldBase+8)*32])
	oldHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	if !m.RetainVoxelMap(o.XBrickMap) {
		t.Fatal("old occupied range was not retainable")
	}
	for i := 0; i < 5; i++ {
		s1l3ClearIndex(o, i)
	}
	o.XBrickMap.DirtySectors[[3]int{}] = true
	for _, budget := range []VoxelUploadBudget{{MaxBytes: 31, MaxSectors: 1}, {MaxBytes: 32}} {
		m.SetVoxelUploadBudget(budget)
		p2cStep(t, m, b, scene)
		if m.VoxelUploadBytes != 0 || !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) || !bytes.Equal(oldRows, b.buffers[oldBuffer][oldBase*32:(oldBase+8)*32]) {
			t.Fatal("deferred empty transition modified its committed occupied header/range")
		}
		s1kReady(t, m, o, false)
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32, MaxSectors: 1})
	p2cStep(t, m, b, scene)
	_, mask := p2cHeader(t, m, b, sector)
	if mask != 0 || m.VoxelUploadBytes != 32 || m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 0 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 0 || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 0 {
		t.Fatal("empty transition wrote live records or failed header-only publication")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
		t.Fatalf("empty committed sector retained%d assignedbytes, want288", got)
	}
	if !bytes.Equal(oldRows, b.buffers[oldBuffer][oldBase*32:(oldBase+8)*32]) {
		t.Fatal("empty publication overwrote the retired occupied range")
	}
	s1kReady(t, m, o, true)
	arrival := p2aObject(scene, 1)
	arrival.XBrickMap.ID = 81
	arrival.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	for i := 0; i < 8; i++ {
		p2cPut(arrival, i, p2aBrick("uniform", 9))
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		p2cStep(t, m, b, scene)
	}
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("empty transition retired range reused without submission")
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(82))
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, scene)
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("empty transition retired range reused before fence completion")
	}
	b.completed[82] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, scene)
	reused, _ := p2cHeader(t, m, b, arrival.XBrickMap.Sectors[[3]int{}])
	if reused != oldBase || b.BufferSize(m.BrickTableBuf) != 256 {
		t.Fatal("completed empty-transition range was not reusable at its physical capacity")
	}
	s1kReady(t, m, arrival, true)
	s1kReady(t, m, o, true)
}

func TestP2cShrinkAcrossClassesRelocatesOnlySurvivingRecords(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 512)
	b.maxBrickBytes = 512
	o := p2aObject(scene, 1)
	bricks := make([]*volume.Brick, 5)
	for i := range bricks {
		bricks[i] = p2aBrick("uniform", byte(i+1))
		p2cPut(o, i, bricks[i])
	}
	p2cStep(t, m, b, scene)
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldBase, _ := p2cHeader(t, m, b, sector)
	oldBuffer := m.BrickTableBuf
	oldRows := bytes.Clone(b.buffers[oldBuffer][oldBase*32 : (oldBase+8)*32])
	oldHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	if !m.RetainVoxelMap(o.XBrickMap) {
		t.Fatal("old class8 sector was not retainable")
	}
	for _, i := range []int{1, 2, 3} {
		s1l3ClearIndex(o, i)
	}
	o.XBrickMap.DirtySectors[[3]int{}] = true
	const exactBytes = 32 + 2*(BrickRecordSize+VoxelAuxRecordBytes)
	for _, budget := range []VoxelUploadBudget{{MaxBytes: exactBytes - 1, MaxSectors: 1, MaxBricks: 2}, {MaxBytes: exactBytes, MaxBricks: 2}} {
		m.SetVoxelUploadBudget(budget)
		p2cStep(t, m, b, scene)
		if m.VoxelUploadBytes != 0 || !bytes.Equal(oldHeader, p2bHeader(t, m, b.p2bNative, sector)) || !bytes.Equal(oldRows, b.buffers[oldBuffer][oldBase*32:(oldBase+8)*32]) {
			t.Fatal("short shrink transaction modified committed class8 data")
		}
		s1kReady(t, m, o, false)
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exactBytes, MaxSectors: 1, MaxBricks: 2})
	p2cStep(t, m, b, scene)
	next, mask := p2cHeader(t, m, b, sector)
	if next == oldBase || mask != 0x11 || m.VoxelUploadBytes != exactBytes || m.VoxelBricksUploaded != 2 || m.VoxelSectorsUploaded != 1 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 64 {
		t.Fatal("class8-to2 shrink omitted relocation or uploaded removed records")
	}
	if !bytes.Equal(oldRows, b.buffers[oldBuffer][oldBase*32:(oldBase+8)*32]) {
		t.Fatal("shrink overwrote its readable old range")
	}
	p2cAssertRow(t, m, b, sector, 0, bricks[0])
	p2cAssertRow(t, m, b, sector, 4, bricks[4])
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+32+2*BrickRecordSize+2*VoxelAuxRecordBytes {
		t.Fatalf("shrunk committed class retained%d assignedbytes", got)
	}
	s1kReady(t, m, o, true)
}

func TestP2cSharedDirtyBrickReceiptsUseWrittenPointerDuringRawCallbackEdit(t *testing.T) {
	m, b, scene := p2cFixture(t)
	source, other := p2aObject(scene, 1), p2aObject(scene, 1)
	source.XBrickMap.ID = 91
	other.XBrickMap.ID = 92
	initial := p2aBrick("uniform", 1)
	p2cPut(source, 32, initial)
	sector := source.XBrickMap.Sectors[[3]int{}]
	other.XBrickMap.Sectors[[3]int{}] = sector
	p2cStep(t, m, b, scene)
	beforeBase, _ := p2cHeader(t, m, b, sector)
	written, newer := p2aBrick("uniform", 2), p2aBrick("uniform", 3)
	p2cPut(source, 32, written)
	key := [6]int{0, 0, 0, 0, 0, 2}
	source.XBrickMap.DirtyBricks[key] = true
	revision := source.XBrickMap.Revision
	b.afterAuxWrite = func() { sector.PackedBricks[sector.GetPackedIndex(32)] = newer }
	p2cStep(t, m, b, scene)
	base, mask := p2cHeader(t, m, b, sector)
	if base != beforeBase || mask != uint64(1)<<32 || source.XBrickMap.Revision != revision {
		t.Fatal("stable-mask callback fixture unexpectedly changed header allocation or revision")
	}
	p2cAssertRow(t, m, b, sector, 32, written)
	t.Logf("callback completion: sourceDirty=%v sourceReceiptWritten=%v otherReceiptWritten=%v", source.XBrickMap.DirtyBricks[key], m.Allocations[source.XBrickMap].Bricks[[3]int{}][32] == written, m.Allocations[other.XBrickMap].Bricks[[3]int{}][32] == written)
	for _, owner := range []*core.VoxelObject{source, other} {
		receipt := m.Allocations[owner.XBrickMap].Bricks[[3]int{}]
		if receipt == nil || receipt[32] != written {
			t.Fatalf("map%d receipt certified a pointer that was not written", owner.XBrickMap.ID)
		}
	}
	if !source.XBrickMap.DirtyBricks[key] {
		t.Fatal("newer raw pointer was falsely acknowledged as uploaded")
	}
	s1kReady(t, m, source, false)
	p2cStep(t, m, b, scene)
	p2cAssertRow(t, m, b, sector, 32, newer)
	for _, owner := range []*core.VoxelObject{source, other} {
		if m.Allocations[owner.XBrickMap].Bricks[[3]int{}][32] != newer {
			t.Fatal("retry did not synchronize both shared receipts to the newly written pointer")
		}
	}
	s1kReady(t, m, source, true)
	s1kReady(t, m, other, true)
}

func TestP2cAbsentDirtyBrickHaloDoesNotClearNextRankedRecord(t *testing.T) {
	for _, hole := range []int{1, 30, 32, 62} {
		t.Run(strconv.Itoa(hole), func(t *testing.T) {
			m, b, scene := p2cFixture(t)
			o := p2aObject(scene, 1)
			occupied := []int{0, 31, 63}
			if hole == 32 {
				occupied = []int{0, 1, 31, 63}
			}
			bricks := make(map[int]*volume.Brick)
			for _, index := range occupied {
				bricks[index] = p2aBrick("uniform", byte(index+1))
				p2cPut(o, index, bricks[index])
			}
			p2cStep(t, m, b, scene)
			sector := o.XBrickMap.Sectors[[3]int{}]
			base, mask := p2cHeader(t, m, b, sector)
			header := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
			rows := bytes.Clone(b.buffers[m.BrickTableBuf][base*32 : (base+uint32(len(occupied)))*32])
			key := [6]int{0, 0, 0, hole % 4, (hole / 4) % 4, hole / 16}
			// Normal halo queues can include an absent neighbor without a membership
			// change. Its insertion rank is the NEXT occupied row, never a nil slot.
			o.XBrickMap.DirtyBricks[key] = true
			p2cStep(t, m, b, scene)
			next, nextMask := p2cHeader(t, m, b, sector)
			if next != base || nextMask != mask || !bytes.Equal(header, p2bHeader(t, m, b.p2bNative, sector)) || !bytes.Equal(rows, b.buffers[m.BrickTableBuf][base*32:(base+uint32(len(occupied)))*32]) {
				t.Fatal("absent halo dirty index cleared or relocated a live ranked record")
			}
			if p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 0 || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 0 || m.VoxelUploadBytes != 0 || m.VoxelBricksUploaded != 0 {
				t.Fatal("absent halo metadata queued physical writes or consumed upload budget")
			}
			if o.XBrickMap.DirtyBricks[key] {
				t.Fatal("absent unchanged halo dirty metadata was not acknowledged")
			}
			for index, brick := range bricks {
				p2cAssertRow(t, m, b, sector, index, brick)
			}
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2cRetainedEmptySectorsCannotSuppressOccupiedBaseZeroCharge(t *testing.T) {
	m, b, scene := p2cFixture(t)
	o := p2aObject(scene, 9)
	brick := p2aBrick("uniform", 7)
	schedulePutBrick(o, [6]int{8, 0, 0}, brick)
	p2cStep(t, m, b, scene)
	base, _ := p2cHeader(t, m, b, o.XBrickMap.Sectors[[3]int{8, 0, 0}])
	if base != 0 {
		t.Fatal("fixture did not place the sole occupied packed range at base0")
	}
	const wantBytes = 256 + 9*32 + BrickRecordSize + VoxelAuxRecordBytes
	// Empty capacity0 headers may share the numeric base0 sentinel but own no
	// interval. Accounting must deduplicate occupied intervals independently of
	// map iteration order; every recalculation must charge the sole live row.
	for i := 0; i < 100; i++ {
		if !m.RetainVoxelMap(o.XBrickMap) {
			t.Fatal("mixed empty/occupied map was not retainable")
		}
		if got := m.RetainedVoxelMapStats().Bytes; got != wantBytes {
			t.Fatalf("retention recalculation%d charged%d bytes, want%d; empty base0 suppressed the occupied row", i, got, wantBytes)
		}
	}
	p2cAssertRow(t, m, b, o.XBrickMap.Sectors[[3]int{8, 0, 0}], 0, brick)
}

func TestP2cAdmissionReservationsSurviveOppositeContentPriorityOrder(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	// Independent resources fit both cohorts. Only the eight physical record
	// slots constrain range placement; the surviving intervals split free4/free1.
	b.buffers[m.MaterialBuf] = make([]byte, 6*materialBlockCapacity*64)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 17408)
	initial := make([]*core.VoxelObject, 4)
	counts := []int{4, 2, 1, 1}
	for i, count := range counts {
		initial[i] = p2aObject(scene, 1)
		initial[i].XBrickMap.ID = uint32(101 + i)
		for index := 0; index < count; index++ {
			p2cPut(initial[i], index, p2aBrick("uniform", byte(10+i)))
		}
	}
	p2cStep(t, m, b, scene)
	wantBases := []uint32{0, 4, 6, 7}
	for i, o := range initial {
		base, _ := p2cHeader(t, m, b, o.XBrickMap.Sectors[[3]int{}])
		if base != wantBases[i] {
			t.Fatalf("initial interval%d base%d want%d", i, base, wantBases[i])
		}
	}
	scene.Objects = []*core.VoxelObject{initial[1], initial[3]}
	p2cStep(t, m, b, scene)
	if m.Allocations[initial[0].XBrickMap] != nil || m.Allocations[initial[2].XBrickMap] != nil {
		t.Fatal("released range owners remained assigned")
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(104))
	b.completed[104] = true
	m.AdvanceRetiredBuffers()
	large := p2aObject(scene, 1)
	large.XBrickMap.ID = 110
	large.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
	for index := 0; index < 4; index++ {
		p2cPut(large, index, p2aBrick("uniform", 20))
	}
	small := p2aObject(scene, 1)
	small.XBrickMap.ID = 111
	small.VoxelGPUAdmissionOptional = true
	small.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	p2cPut(small, 63, p2aBrick("uniform", 21))
	// Admission must reserve required large before optional small, but service
	// selects small first. Execution must honor those reserved physical intervals.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128 + 2*32 + 5*(BrickRecordSize+VoxelAuxRecordBytes), MaxSectors: 2, MaxBricks: 5})
	p2cStep(t, m, b, scene)
	if m.Allocations[large.XBrickMap] == nil || m.Allocations[small.XBrickMap] == nil {
		t.Fatal("fitting required/optional maps were not both admitted")
	}
	t.Logf("opposite content order: small published base=%d large sector pending=%v uploaded sectors=%d records=%d", binary.LittleEndian.Uint32(p2bHeader(t, m, b.p2bNative, small.XBrickMap.Sectors[[3]int{}])[16:]), large.XBrickMap.DirtySectors[[3]int{}], m.VoxelSectorsUploaded, m.VoxelBricksUploaded)
	s1kReady(t, m, large, true)
	s1kReady(t, m, small, true)
	largeBase, _ := p2cHeader(t, m, b, large.XBrickMap.Sectors[[3]int{}])
	smallBase, _ := p2cHeader(t, m, b, small.XBrickMap.Sectors[[3]int{}])
	if largeBase != 0 || smallBase != 6 || b.BufferSize(m.BrickTableBuf) != 256 || b.creates != 0 {
		t.Fatal("service order fragmented the reserved free4/free1 intervals or grew the constrained table")
	}
	if m.VoxelSectorsUploaded != 2 || m.VoxelBricksUploaded != 5 {
		t.Fatal("fitting reservations did not complete their exact logical work")
	}
	for _, o := range scene.Objects {
		sector := o.XBrickMap.Sectors[[3]int{}]
		for index := 0; index < 64; index++ {
			if brick := sector.GetBrick(index%4, (index/4)%4, index/16); brick != nil {
				p2cAssertRow(t, m, b, sector, index, brick)
			}
		}
	}
}

func p2cFreshOppositePriority(t *testing.T) (*GpuBufferManager, *p2cNative, *core.Scene, *core.VoxelObject, *core.VoxelObject) {
	t.Helper()
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	b.buffers[m.MaterialBuf] = make([]byte, 2*materialBlockCapacity*64)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 8704)
	large := p2aObject(scene, 1)
	large.XBrickMap.ID = 120
	large.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
	for i := 0; i < 4; i++ {
		p2cPut(large, i, p2aBrick("uniform", byte(i+1)))
	}
	small := p2aObject(scene, 1)
	small.XBrickMap.ID = 121
	small.VoxelGPUAdmissionOptional = true
	small.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	p2cPut(small, 31, p2aBrick("uniform", 9))
	return m, b, scene, large, small
}

func TestP2cFreshTailReservationsHonorOppositeServiceOrder(t *testing.T) {
	m, b, scene, large, small := p2cFreshOppositePriority(t)
	p2cStep(t, m, b, scene)
	s1kReady(t, m, large, true)
	s1kReady(t, m, small, true)
	largeBase, _ := p2cHeader(t, m, b, large.XBrickMap.Sectors[[3]int{}])
	smallBase, _ := p2cHeader(t, m, b, small.XBrickMap.Sectors[[3]int{}])
	if largeBase != 0 || smallBase != 4 || b.BufferSize(m.BrickTableBuf) != 256 || b.creates != 0 {
		t.Fatalf("fresh planned claims large=%d small=%d; want0/4 without growth", largeBase, smallBase)
	}
	for i := 0; i < 4; i++ {
		p2cAssertRow(t, m, b, large.XBrickMap.Sectors[[3]int{}], i, large.XBrickMap.Sectors[[3]int{}].GetBrick(i, 0, 0))
	}
	p2cAssertRow(t, m, b, small.XBrickMap.Sectors[[3]int{}], 31, small.XBrickMap.Sectors[[3]int{}].GetBrick(3, 3, 1))
}

func TestP2cBudgetDeferredVirtualTailGapRemainsReclaimable(t *testing.T) {
	m, b, scene, large, small := p2cFreshOppositePriority(t)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128 + 32 + BrickRecordSize + VoxelAuxRecordBytes, MaxSectors: 1, MaxBricks: 1})
	p2cStep(t, m, b, scene)
	s1kReady(t, m, small, true)
	s1kReady(t, m, large, false)
	p2bAssertLookup(t, m, b.p2bNative, large, [3]int{}, false)
	smallSector := small.XBrickMap.Sectors[[3]int{}]
	smallBase, _ := p2cHeader(t, m, b, smallSector)
	if smallBase != 4 || m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 1 {
		t.Fatal("small first-frame upload did not preserve required virtual tail gap")
	}
	smallHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, smallSector))
	oldBuffer := m.BrickTableBuf
	smallRow := bytes.Clone(b.buffers[oldBuffer][smallBase*32 : (smallBase+1)*32])
	if !m.RetainVoxelMap(large.XBrickMap) {
		t.Fatal("prepared deferred owner was not retainable")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
		t.Fatalf("deferred virtual reservation acquired%d assignedbytes, want metadata/header288 only", got)
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32 + 4*(BrickRecordSize+VoxelAuxRecordBytes), MaxSectors: 1, MaxBricks: 4})
	p2cStep(t, m, b, scene)
	largeBase, _ := p2cHeader(t, m, b, large.XBrickMap.Sectors[[3]int{}])
	if largeBase != 0 || b.BufferSize(m.BrickTableBuf) != 256 || b.creates != 0 || !bytes.Equal(smallRow, b.buffers[oldBuffer][smallBase*32:(smallBase+1)*32]) || !bytes.Equal(smallHeader, p2bHeader(t, m, b.p2bNative, smallSector)) {
		t.Fatal("replanning deferred virtual gap grew table or overwrote published small range")
	}
	s1kReady(t, m, large, true)
	s1kReady(t, m, small, true)
}

func TestP2cLateGrowthFallbackExcludesOtherPendingReservedSpan(t *testing.T) {
	m, b, scene, large, small := p2cFreshOppositePriority(t)
	sector := small.XBrickMap.Sectors[[3]int{}]
	late := p2aBrick("uniform", 10)
	b.afterMaterialWrite = func() { _, _ = sector.GetOrCreateBrick(3, 3, 3); sector.PackedBricks[sector.GetPackedIndex(63)] = late }
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128 + 2*32 + 6*(BrickRecordSize+VoxelAuxRecordBytes), MaxSectors: 2, MaxBricks: 6})
	p2cStep(t, m, b, scene)
	s1kReady(t, m, large, true)
	s1kReady(t, m, small, true)
	largeBase, _ := p2cHeader(t, m, b, large.XBrickMap.Sectors[[3]int{}])
	smallBase, mask := p2cHeader(t, m, b, sector)
	if largeBase != 0 || smallBase < 4 || smallBase+2 > 8 || mask != (uint64(1)<<31|uint64(1)<<63) || b.BufferSize(m.BrickTableBuf) != 256 || b.creates != 0 {
		t.Fatalf("late-growth fallback invaded pending reservation: large=%d small=%d mask=%x", largeBase, smallBase, mask)
	}
	if m.VoxelBricksUploaded != 6 || m.VoxelSectorsUploaded != 2 {
		t.Fatal("fitting late growth skipped occupied rows or fitting reserved owner")
	}
	p2cAssertRow(t, m, b, sector, 63, late)
	for i := 0; i < 4; i++ {
		p2cAssertRow(t, m, b, large.XBrickMap.Sectors[[3]int{}], i, large.XBrickMap.Sectors[[3]int{}].GetBrick(i, 0, 0))
	}
}

func TestP2cCompletedFreeSuffixJoinsUnassignedTailCapacity(t *testing.T) {
	m, b, scene := p2cFixture(t)
	b.buffers[m.BrickTableBuf] = make([]byte, 256)
	b.maxBrickBytes = 256
	b.buffers[m.MaterialBuf] = make([]byte, 3*materialBlockCapacity*64)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 8704)
	keep, release := p2aObject(scene, 1), p2aObject(scene, 1)
	keep.XBrickMap.ID = 130
	release.XBrickMap.ID = 131
	for i := 0; i < 4; i++ {
		p2cPut(keep, i, p2aBrick("uniform", byte(i+1)))
	}
	for i := 0; i < 2; i++ {
		p2cPut(release, i, p2aBrick("uniform", 8))
	}
	p2cStep(t, m, b, scene)
	keepSector := keep.XBrickMap.Sectors[[3]int{}]
	releaseSector := release.XBrickMap.Sectors[[3]int{}]
	keepBase, _ := p2cHeader(t, m, b, keepSector)
	releaseBase, _ := p2cHeader(t, m, b, releaseSector)
	if keepBase != 0 || releaseBase != 4 {
		t.Fatalf("initial published intervals keep=%d release=%d, want0/4", keepBase, releaseBase)
	}
	oldBuffer := m.BrickTableBuf
	keepRows := bytes.Clone(b.buffers[oldBuffer][:4*BrickRecordSize])
	keepHeader := bytes.Clone(p2bHeader(t, m, b.p2bNative, keepSector))
	scene.Objects = []*core.VoxelObject{keep}
	p2cStep(t, m, b, scene)
	if m.Allocations[release.XBrickMap] != nil {
		t.Fatal("suffix owner remained assigned instead of retiring")
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(132))
	b.completed[132] = true
	m.AdvanceRetiredBuffers()
	arrival := p2aObject(scene, 1)
	arrival.XBrickMap.ID = 133
	for i := 0; i < 4; i++ {
		p2cPut(arrival, i, p2aBrick("uniform", byte(i+10)))
	}
	p2cStep(t, m, b, scene)
	s1kReady(t, m, arrival, true)
	s1kReady(t, m, keep, true)
	base, _ := p2cHeader(t, m, b, arrival.XBrickMap.Sectors[[3]int{}])
	if base != 4 || b.BufferSize(m.BrickTableBuf) != 256 || b.creates != 0 {
		t.Fatal("completed free suffix failed to join unused physical tail capacity without growth")
	}
	if !bytes.Equal(keepRows, b.buffers[oldBuffer][:4*BrickRecordSize]) || !bytes.Equal(keepHeader, p2bHeader(t, m, b.p2bNative, keepSector)) {
		t.Fatal("suffix extension overwrote surviving published range/header")
	}
	for _, o := range scene.Objects {
		sector := o.XBrickMap.Sectors[[3]int{}]
		for i := 0; i < 4; i++ {
			p2cAssertRow(t, m, b, sector, i, sector.GetBrick(i, 0, 0))
		}
	}
}
