package gpu

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Keep pre-implementation RED executable: missing public behavior is a test
// failure, not an undefined-symbol compiler failure.
type p3aNormalPolicy interface{ SetPackedVoxelNormals(bool) error }

func p3aPolicy(t *testing.T, m *GpuBufferManager) p3aNormalPolicy {
	t.Helper()
	p, ok := any(m).(p3aNormalPolicy)
	if !ok {
		t.Fatal("P3a: GpuBufferManager must implement init-only SetPackedVoxelNormals(bool) error")
	}
	return p
}
func p3aFixture(t *testing.T) (*GpuBufferManager, *p2cNative, *core.Scene) {
	t.Helper()
	m, b, s := p2cFixture(t)
	if err := p3aPolicy(t, m).SetPackedVoxelNormals(true); err != nil {
		t.Fatal(err)
	}
	return m, b, s
}

// Dense authored sidecars deliberately need not agree with raw occupancy. Their
// occupancy is authoritative, and all occupied raw normal lanes are preserved.
func p3aBrick(indices []int, seed uint16) *volume.Brick {
	brick := p2aBrick("uniform", 1)
	clear(brick.PrecomputedAux)
	for _, i := range indices {
		brick.PrecomputedAux[i/8] |= byte(1 << (i % 8))
	}
	for i := 0; i < 512; i++ {
		binary.LittleEndian.PutUint16(brick.PrecomputedAux[64+2*i:], seed+uint16(i))
	}
	return brick
}
func p3aIndices(count int) []int {
	out := make([]int, count)
	for i := range out {
		out[i] = i
	}
	return out
}

// This independent wire oracle only compresses the existing dense bytes. It
// deliberately does not call manager encoding, normal or allocation helpers.
func p3aPacket(dense []byte) []byte {
	count := 0
	for _, v := range dense[:64] {
		count += bits.OnesCount8(v)
	}
	out := make([]byte, 64+4*((count+1)/2))
	copy(out, dense[:64])
	rank := 0
	for i := 0; i < 512; i++ {
		if dense[i/8]&byte(1<<(i%8)) == 0 {
			continue
		}
		copy(out[64+2*rank:66+2*rank], dense[64+2*i:66+2*i])
		rank++
	}
	return out
}
func p3aAssertPacket(t *testing.T, m *GpuBufferManager, b *p2cNative, sector *volume.Sector, index int, dense []byte) (uint32, []byte) {
	t.Helper()
	record := p2bRecord(t, m, b.p2bNative, sector, index)
	if got := binary.LittleEndian.Uint32(record[28:]); got != 1 {
		t.Fatalf("P3a brick auxiliary layout=%d, want packed1", got)
	}
	base := binary.LittleEndian.Uint32(record[24:])
	want := p3aPacket(dense)
	start := uint64(base) * 4
	data := b.buffers[m.DenseOccupancyBuf]
	if start+uint64(len(want)) > uint64(len(data)) {
		t.Fatal("published packet extends beyond actual bound auxiliary storage")
	}
	got := data[start : start+uint64(len(want))]
	if !bytes.Equal(got, want) {
		t.Fatalf("P3a packet at word%d differs: got%x want%x", base, got, want)
	}
	return base, bytes.Clone(got)
}
func p3aAssertUnchanged(t *testing.T, b *p2cNative, buffer *wgpu.Buffer, base uint32, packet []byte) {
	t.Helper()
	if !bytes.Equal(packet, b.buffers[buffer][uint64(base)*4:uint64(base)*4+uint64(len(packet))]) {
		t.Fatal("readable old packet overwritten before its submission completed")
	}
}
func p3aDirty(o *core.VoxelObject, index int) {
	o.XBrickMap.DirtyBricks[[6]int{0, 0, 0, index % 4, (index / 4) % 4, index / 16}] = true
}

func TestP3aDenseDefaultAndInitOnlyPolicy(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		m, b, s := p2cFixture(t)
		o := p2aObject(s, 1)
		brick := p3aBrick([]int{0, 31, 32, 255, 256, 511}, 0x8001)
		p2cPut(o, 0, brick)
		p2cStep(t, m, b, s)
		sector := o.XBrickMap.Sectors[[3]int{}]
		row := p2bRecord(t, m, b.p2bNative, sector, 0)
		if binary.LittleEndian.Uint32(row[28:]) != 0 {
			t.Fatal("default manager changed legacy dense auxiliary layout")
		}
		p2cAssertRow(t, m, b, sector, 0, brick)
		if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != VoxelAuxRecordBytes {
			t.Fatal("default auxiliary upload lost dense compatibility")
		}
		before := bytes.Clone(row)
		p := p3aPolicy(t, m)
		if err := p.SetPackedVoxelNormals(false); err != nil {
			t.Fatal("same-value dense policy is not idempotent", err)
		}
		if err := p.SetPackedVoxelNormals(true); err == nil {
			t.Fatal("late dense-to-packed policy change accepted")
		}
		p3aDirty(o, 0)
		p2cStep(t, m, b, s)
		if !bytes.Equal(before, p2bRecord(t, m, b.p2bNative, sector, 0)) {
			t.Fatal("rejected policy change mutated published dense format")
		}
	})
	t.Run("packed", func(t *testing.T) {
		m, b, s := p2cFixture(t)
		p := p3aPolicy(t, m)
		for _, enabled := range []bool{false, true, true, false, true} {
			if err := p.SetPackedVoxelNormals(enabled); err != nil {
				t.Fatal("pre-allocation policy change rejected", err)
			}
		}
		o := p2aObject(s, 1)
		brick := p3aBrick([]int{511}, 0xab00)
		p2cPut(o, 0, brick)
		p2cStep(t, m, b, s)
		sector := o.XBrickMap.Sectors[[3]int{}]
		base, packet := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
		if err := p.SetPackedVoxelNormals(true); err != nil {
			t.Fatal("same-value packed policy rejected", err)
		}
		if err := p.SetPackedVoxelNormals(false); err == nil {
			t.Fatal("late packed-to-dense policy change accepted")
		}
		p3aAssertUnchanged(t, b, m.DenseOccupancyBuf, base, packet)
		// Removing all maps does not make a previously initialized manager mutable.
		s.Objects = nil
		p2cStep(t, m, b, s)
		if err := p.SetPackedVoxelNormals(false); err == nil {
			t.Fatal("policy reopened after final-owner release")
		}
	})
}

func TestP3aSparsePhysicalSavingsAndExactAssignedSpans(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	o := p2aObject(s, 128)
	for key := range o.XBrickMap.Sectors {
		schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p3aBrick([]int{511}, uint16(key[0])))
	}
	p2cStep(t, m, b, s)
	physical := b.BufferSize(m.DenseOccupancyBuf)
	if physical < 128*68 || physical > 2*128*68+256 {
		t.Fatalf("128 one-cell normal packets reserved %d physical bytes", physical)
	}
	for _, sector := range o.XBrickMap.Sectors {
		p3aAssertPacket(t, m, b, sector, 0, sector.GetBrick(0, 0, 0).PrecomputedAux)
	}
	if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 128*68 {
		t.Fatal("sparse upload includes fixed record padding")
	}
	if !m.RetainVoxelMap(o.XBrickMap) {
		t.Fatal("packed map not retainable")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+128*(32+32+68) {
		t.Fatalf("retained assigned spans=%d want%d", got, 256+128*(32+32+68))
	}
	s1kReady(t, m, o, true)
}

func TestP3aWireParityAcrossAllRawNormalValuesAndVoxelRanks(t *testing.T) {
	// Every 16-bit value, including nonzero invalid normals and both flag bits,
	// passes through actual uploads. Full occupancy makes each value reachable.
	m, b, s := p3aFixture(t)
	o := p2aObject(s, 128)
	for key := range o.XBrickMap.Sectors {
		schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p3aBrick(p3aIndices(512), uint16(key[0]*512)))
	}
	p2cStep(t, m, b, s)
	for _, sector := range o.XBrickMap.Sectors {
		p3aAssertPacket(t, m, b, sector, 0, sector.GetBrick(0, 0, 0).PrecomputedAux)
	}
	s1kReady(t, m, o, true)
	for _, indices := range [][]int{{}, {511}, {0, 31, 32}, {0, 31, 32, 255, 256, 511}, p3aIndices(511)} {
		t.Run(fmt.Sprintf("occupied%d", len(indices)), func(t *testing.T) {
			m, b, s := p3aFixture(t)
			o := p2aObject(s, 1)
			brick := p3aBrick(indices, 0x3fff)
			p2cPut(o, 63, brick)
			p2cStep(t, m, b, s)
			p3aAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 63, brick.PrecomputedAux)
			if got := p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf"); got != uint64(len(p3aPacket(brick.PrecomputedAux))) {
				t.Fatal("packet word span was padded beyond its odd final zero lane")
			}
		})
	}
}

func TestP3aAuthoritativePrecomputedAndGeneratedDenseParity(t *testing.T) {
	for _, precomputed := range []bool{true, false} {
		t.Run(fmt.Sprintf("precomputed%t", precomputed), func(t *testing.T) {
			// Compare the packet with the bytes the unchanged legacy normal path emits
			// for the same source; no private normal-builder implementation is asserted.
			denseManager, denseBackend, denseScene := p2cFixture(t)
			denseObj := p2aObject(denseScene, 1)
			brick := p3aBrick([]int{31, 32, 255, 256, 511}, 0xc001)
			if !precomputed {
				brick.PrecomputedAux = []byte{0x7f}
			}
			p2cPut(denseObj, 0, brick)
			p2cStep(t, denseManager, denseBackend, denseScene)
			denseRow := p2bRecord(t, denseManager, denseBackend.p2bNative, denseObj.XBrickMap.Sectors[[3]int{}], 0)
			base := uint64(binary.LittleEndian.Uint32(denseRow[24:])) * 4
			expected := bytes.Clone(denseBackend.buffers[denseManager.DenseOccupancyBuf][base : base+VoxelAuxRecordBytes])
			m, b, s := p3aFixture(t)
			o := p2aObject(s, 1)
			p2cPut(o, 0, brick)
			p2cStep(t, m, b, s)
			p3aAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 0, expected)
			if precomputed && !bytes.Equal(expected, brick.PrecomputedAux) {
				t.Fatal("legacy precomputed authority was not exercised")
			}
		})
	}
}

func TestP3aEveryUpdateIsCopyOnWriteAndBudgetUsesActualPacket(t *testing.T) {
	m, b, s := p3aFixture(t)
	o := p2aObject(s, 1)
	brick := p3aBrick([]int{0, 31, 511}, 0x4000)
	p2cPut(o, 0, brick)
	p2cStep(t, m, b, s)
	sector := o.XBrickMap.Sectors[[3]int{}]
	for _, edit := range []string{"reshuffle", "normal only", "identical"} {
		oldBuffer := m.DenseOccupancyBuf
		oldBase, oldPacket := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
		oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
		if edit == "reshuffle" {
			copy(brick.PrecomputedAux, p3aBrick([]int{32, 255, 256}, 0x4000).PrecomputedAux)
		}
		if edit == "normal only" {
			binary.LittleEndian.PutUint16(brick.PrecomputedAux[64+2*255:], 0x8123)
		}
		p3aDirty(o, 0)
		exact := uint64(BrickRecordSize + len(p3aPacket(brick.PrecomputedAux)))
		m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact - 1, MaxSectors: 1, MaxBricks: 1})
		p2cStep(t, m, b, s)
		if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
			t.Fatal("short packed budget partially published packet or record")
		}
		p3aAssertUnchanged(t, b, oldBuffer, oldBase, oldPacket)
		s1kReady(t, m, o, false)
		if !m.RetainVoxelMap(o.XBrickMap) {
			t.Fatal("deferred map not retainable")
		}
		if got := m.RetainedVoxelMapStats().Bytes; got != 256+32+32+uint64(len(oldPacket)) {
			t.Fatal("deferred virtual claim or quarantined span counted as retained assigned bytes", got)
		}
		m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact, MaxSectors: 1, MaxBricks: 1})
		p2cStep(t, m, b, s)
		next, _ := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
		if next == oldBase || m.VoxelUploadBytes != exact || m.VoxelSectorsUploaded != 0 {
			t.Fatal("stable-row update omitted COW or exact byte accounting")
		}
		p3aAssertUnchanged(t, b, oldBuffer, oldBase, oldPacket)
		s1kReady(t, m, o, true)
	}
}

func TestP3aRelocationNeedsOldAndNewCapacityAndPreservesPublication(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	o := p2aObject(s, 1)
	brick := p3aBrick(p3aIndices(256), 0x4000)
	p2cPut(o, 0, brick)
	// A 576-byte packet cannot fit: grow once, then close further growth.
	b.maxAuxBytes = 1280
	p2cStep(t, m, b, s)
	b.maxAuxBytes = 1280
	sector := o.XBrickMap.Sectors[[3]int{}]
	oldBuffer := m.DenseOccupancyBuf
	base, packet := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
	copy(brick.PrecomputedAux, p3aBrick(p3aIndices(512), 0x8000).PrecomputedAux)
	p3aDirty(o, 0)
	p2cStep(t, m, b, s)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
		t.Fatal("COW spent live old span as new capacity or partially published denied growth")
	}
	p3aAssertUnchanged(t, b, oldBuffer, base, packet)
	s1kReady(t, m, o, false)
	b.maxAuxBytes = 4096
	p2cStep(t, m, b, s)
	next, _ := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	if uint64(next)*4 < uint64(base)*4+uint64(len(packet)) && uint64(base)*4 < uint64(next)*4+1088 {
		t.Fatal("successful packet overlaps its still readable old interval")
	}
	p3aAssertUnchanged(t, b, oldBuffer, base, packet)
	s1kReady(t, m, o, true)
}

func TestP3aFinalOwnerPacketsWaitForStampedFenceAndCoalesce(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	a, c, keep := p2aObject(s, 1), p2aObject(s, 1), p2aObject(s, 1)
	for i, o := range []*core.VoxelObject{a, c, keep} {
		o.XBrickMap.ID = uint32(201 + i)
		p2cPut(o, 0, p3aBrick([]int{511}, uint16(i)))
	}
	p2cStep(t, m, b, s)
	aBase, aPacket := p3aAssertPacket(t, m, b, a.XBrickMap.Sectors[[3]int{}], 0, a.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux)
	cBase, cPacket := p3aAssertPacket(t, m, b, c.XBrickMap.Sectors[[3]int{}], 0, c.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux)
	oldBuffer := m.DenseOccupancyBuf
	if aBase+17 != cBase && cBase+17 != aBase {
		t.Fatal("fixture did not create adjacent exact 68-byte spans")
	}
	s.Objects = []*core.VoxelObject{keep}
	p2cStep(t, m, b, s)
	arrival := p2aObject(s, 1)
	arrival.XBrickMap.ID = 210
	arrival.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	fresh := p3aBrick(p3aIndices(36), 0x8000)
	p2cPut(arrival, 0, fresh)
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		p2cStep(t, m, b, s)
	}
	s1kReady(t, m, arrival, false)
	p3aAssertUnchanged(t, b, oldBuffer, aBase, aPacket)
	p3aAssertUnchanged(t, b, oldBuffer, cBase, cPacket)
	m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(211))
	p2cStep(t, m, b, s)
	s1kReady(t, m, arrival, false)
	b.completed[210] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, s)
	s1kReady(t, m, arrival, false)
	b.completed[211] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, s)
	next, _ := p3aAssertPacket(t, m, b, arrival.XBrickMap.Sectors[[3]int{}], 0, fresh.PrecomputedAux)
	if next != min(aBase, cBase) || b.BufferSize(m.DenseOccupancyBuf) != 256 {
		t.Fatal("completed adjacent spans did not coalesce without physical growth")
	}
	s1kReady(t, m, arrival, true)
	s1kReady(t, m, keep, true)
}

func TestP3aSharedSectorAndSharedBrickHavePhysicalLocationReceipts(t *testing.T) {
	for _, sharedSector := range []bool{true, false} {
		t.Run(fmt.Sprintf("sharedSector%t", sharedSector), func(t *testing.T) {
			m, b, s := p3aFixture(t)
			a, other := p2aObject(s, 1), p2aObject(s, 1)
			a.XBrickMap.ID = 220
			other.XBrickMap.ID = 221
			brick := p3aBrick([]int{0, 511}, 0x4000)
			p2cPut(a, 0, brick)
			if sharedSector {
				other.XBrickMap.Sectors[[3]int{}] = a.XBrickMap.Sectors[[3]int{}]
			} else {
				p2cPut(other, 0, brick)
			}
			p2cStep(t, m, b, s)
			sa, so := a.XBrickMap.Sectors[[3]int{}], other.XBrickMap.Sectors[[3]int{}]
			aBase, aPacket := p3aAssertPacket(t, m, b, sa, 0, brick.PrecomputedAux)
			otherBase, otherPacket := p3aAssertPacket(t, m, b, so, 0, brick.PrecomputedAux)
			if sharedSector && aBase != otherBase {
				t.Fatal("shared physical sector duplicated packet ownership")
			}
			if !sharedSector && aBase == otherBase {
				t.Fatal("distinct physical sectors aliased mutable packet receipt by Brick pointer")
			}
			if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != uint64(68*(2-btoiP3a(sharedSector))) {
				t.Fatal("geometry uploads did not deduplicate by physical sector")
			}
			oldBuffer := m.DenseOccupancyBuf
			oldDense := bytes.Clone(brick.PrecomputedAux)
			binary.LittleEndian.PutUint16(brick.PrecomputedAux[64+2*511:], 0xabcd)
			p3aDirty(a, 0)
			p2cStep(t, m, b, s)
			p3aAssertPacket(t, m, b, sa, 0, brick.PrecomputedAux)
			if sharedSector {
				p3aAssertPacket(t, m, b, so, 0, brick.PrecomputedAux)
			} else {
				p3aAssertPacket(t, m, b, so, 0, oldDense)
			}
			p3aAssertUnchanged(t, b, oldBuffer, aBase, aPacket)
			p3aAssertUnchanged(t, b, oldBuffer, otherBase, otherPacket)
			s.Objects = []*core.VoxelObject{other}
			p2cStep(t, m, b, s)
			if sharedSector {
				p3aAssertPacket(t, m, b, so, 0, brick.PrecomputedAux)
			} else {
				p3aAssertPacket(t, m, b, so, 0, oldDense)
				p3aDirty(other, 0)
				p2cStep(t, m, b, s)
				p3aAssertPacket(t, m, b, so, 0, brick.PrecomputedAux)
			}
			s1kReady(t, m, other, true)
		})
	}
}
func btoiP3a(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestP3aWholeUnitCaptureSurvivesSamePointerRawCallbackEdit(t *testing.T) {
	m, b, s := p3aFixture(t)
	o := p2aObject(s, 1)
	first := p3aBrick([]int{0}, 0x4000)
	later := p3aBrick([]int{31, 511}, 0x8123)
	// Mixed payload forces the whole captured source, not just the sidecar, through
	// the native boundary. Change all public source bytes without changing identity.
	later.Flags = 0
	later.AtlasOffset = 17
	p2cPut(o, 0, first)
	p2cPut(o, 63, later)
	capturedDense := bytes.Clone(later.PrecomputedAux)
	capturedRaw := make([]byte, 512)
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				capturedRaw[x+y*8+z*64] = later.Payload[x][y][z]
			}
		}
	}
	capturedMask := later.OccupancyMask64
	b.afterAuxWrite = func() {
		later.Flags = volume.BrickFlagUniformMaterial
		later.AtlasOffset = 29
		later.OccupancyMask64 = 0x123456789abcdef0
		for z := 0; z < 8; z++ {
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					later.Payload[x][y][z] = byte((x+y*8+z*64)%7 + 1)
				}
			}
		}
		copy(later.PrecomputedAux, p3aBrick(p3aIndices(512), 0xffff).PrecomputedAux)
	}
	// The packet cost is captured before the first write. A later larger packet
	// cannot spend unbudgeted bytes or alter the current unit's captured records.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64 + 32 + 2*32 + 68 + 68 + 512, MaxSectors: 1, MaxBricks: 2})
	p2cStep(t, m, b, s)
	sector := o.XBrickMap.Sectors[[3]int{}]
	p3aAssertPacket(t, m, b, sector, 63, capturedDense)
	row := p2bRecord(t, m, b.p2bNative, sector, 63)
	mask := uint64(binary.LittleEndian.Uint32(row[8:])) | uint64(binary.LittleEndian.Uint32(row[12:]))<<32
	if binary.LittleEndian.Uint32(row[20:]) != 0 || mask != capturedMask {
		t.Fatal("whole-unit capture published post-callback flags or micro mask")
	}
	if m.VoxelUploadBytes != 64+32+2*32+68+68+512 {
		t.Fatal("whole unit cost was not captured before its first write", m.VoxelUploadBytes)
	}
	payloadFound := false
	for _, w := range b.writes {
		if w.label == "payload" {
			payloadFound = true
			if !bytes.Equal(w.data, capturedRaw) {
				t.Fatal("queued payload differs from captured source bytes")
			}
		}
	}
	if !payloadFound || !o.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("same-pointer raw edit lost captured payload or acknowledged newer source")
	}
	s1kReady(t, m, o, false)
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	p2cStep(t, m, b, s)
	p3aAssertPacket(t, m, b, sector, 63, later.PrecomputedAux)
	row = p2bRecord(t, m, b.p2bNative, sector, 63)
	if binary.LittleEndian.Uint32(row[0:]) != 29 || binary.LittleEndian.Uint32(row[20:]) != volume.BrickFlagUniformMaterial {
		t.Fatal("retry did not publish later source flags/material")
	}
	s1kReady(t, m, o, true)
}

func TestP3aAdmissionClaimsSurviveOppositeServiceOrderAndDeferral(t *testing.T) {
	for _, deferLarge := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferLarge%t", deferLarge), func(t *testing.T) {
			m, b, s := p3aFixture(t)
			b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
			b.maxAuxBytes = 256
			large := p2aObject(s, 1)
			large.XBrickMap.ID = 240
			large.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
			largeBrick := p3aBrick(p3aIndices(36), 0x8000)
			p2cPut(large, 0, largeBrick)
			small := p2aObject(s, 1)
			small.XBrickMap.ID = 241
			small.VoxelGPUAdmissionOptional = true
			small.VoxelUploadPriority = core.VoxelUploadPriorityFallback
			smallBrick := p3aBrick([]int{511}, 0x1234)
			p2cPut(small, 63, smallBrick)
			if deferLarge {
				m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128 + 32 + 32 + 68, MaxSectors: 1, MaxBricks: 1})
			}
			p2cStep(t, m, b, s)
			s1kReady(t, m, small, true)
			smallSector := small.XBrickMap.Sectors[[3]int{}]
			smallBase, smallPacket := p3aAssertPacket(t, m, b, smallSector, 63, smallBrick.PrecomputedAux)
			oldBuffer := m.DenseOccupancyBuf
			if deferLarge {
				s1kReady(t, m, large, false)
				p2bAssertLookup(t, m, b.p2bNative, large, [3]int{}, false)
				if !m.RetainVoxelMap(large.XBrickMap) {
					t.Fatal("prepared deferred target not retainable")
				}
				if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
					t.Fatal("deferred virtual auxiliary claim became a real assigned lease", got)
				}
				m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 32 + 32 + 136, MaxSectors: 1, MaxBricks: 1})
				p2cStep(t, m, b, s)
			}
			largeBase, _ := p3aAssertPacket(t, m, b, large.XBrickMap.Sectors[[3]int{}], 0, largeBrick.PrecomputedAux)
			if largeBase < smallBase+17 && smallBase < largeBase+34 {
				t.Fatal("opposite service order overlapped virtual packet claims")
			}
			if b.BufferSize(m.DenseOccupancyBuf) != 256 {
				t.Fatal("order fragmented fitting exact spans and forced growth")
			}
			p3aAssertUnchanged(t, b, oldBuffer, smallBase, smallPacket)
			s1kReady(t, m, large, true)
			s1kReady(t, m, small, true)
		})
	}
}

func TestP3aDeniedCandidateRollsBackAuxiliaryClaim(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	denied := p2aObject(s, 1)
	denied.XBrickMap.ID = 250
	p2cPut(denied, 0, p3aBrick(p3aIndices(128), 0x4000)) // 320 bytes, impossible.
	later := p2aObject(s, 1)
	later.XBrickMap.ID = 251
	later.VoxelGPUAdmissionOptional = true
	fresh := p3aBrick(p3aIndices(36), 0xabcd)
	p2cPut(later, 0, fresh)
	p2cStep(t, m, b, s)
	s1kReady(t, m, denied, false)
	s1kReady(t, m, later, true)
	p3aAssertPacket(t, m, b, later.XBrickMap.Sectors[[3]int{}], 0, fresh.PrecomputedAux)
	if b.BufferSize(m.DenseOccupancyBuf) != 256 || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 136 {
		t.Fatal("denied candidate leaked a span or prevented fitting later admission")
	}
	p2bAssertLookup(t, m, b.p2bNative, denied, [3]int{}, false)
}

func TestP3aStagedGenerationMirrorsExactPacketBytesAndKeepsParity(t *testing.T) {
	m, b, s := p3aFixture(t)
	resident := p2aObject(s, 1)
	resident.XBrickMap.ID = 260
	initial := p3aBrick([]int{0, 511}, 0x4000)
	p2cPut(resident, 0, initial)
	p2cStep(t, m, b, s)
	// Establish a real pending seven-buffer migration. Many sparse packets exceed
	// the current auxiliary storage as well as the sector/record tables.
	arrival := p2aObject(s, 129)
	arrival.XBrickMap.ID = 261
	for key := range arrival.XBrickMap.Sectors {
		schedulePutBrick(arrival, [6]int{key[0], key[1], key[2]}, p3aBrick([]int{511}, 0x8123))
	}
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
	p2cStep(t, m, b, s)
	if !m.VoxelGPUWorkStats().Pending {
		t.Fatal("fixture failed to establish unpublished staging generation")
	}
	// Remove arrival demand so its contents cannot repair the resident after
	// publication. Current resident writes must mirror into the staged buffers.
	s.Objects = []*core.VoxelObject{resident}
	next := p3aBrick([]int{31, 32, 511}, 0x8001)
	p2cPut(resident, 0, next)
	p3aDirty(resident, 0)
	const exactMirror = 2 * (32 + 72)
	sector := resident.XBrickMap.Sectors[[3]int{}]
	oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exactMirror - 1, MaxSectors: 1, MaxBricks: 1})
	p2cStep(t, m, b, s)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
		t.Fatal("mirrored packet+record bypassed exact global byte cap")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exactMirror, MaxSectors: 1, MaxBricks: 1})
	p2cStep(t, m, b, s)
	p3aAssertPacket(t, m, b, sector, 0, next.PrecomputedAux)
	if m.VoxelUploadBytes != exactMirror || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 144 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 64 {
		t.Fatal("staging charged padded packets or omitted mirror record")
	}
	s1kReady(t, m, resident, true)
	// Finish physical copies with all content paused. The acknowledged edit must
	// already exist in the newly published generation.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 0, MaxSectors: 4096, MaxBricks: 1 << 20})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 1 << 30})
	p2cStep(t, m, b, s)
	if m.VoxelGPUWorkStats().Pending || m.VoxelUploadBytes != 0 {
		t.Fatal("migration did not publish with content uploads paused")
	}
	p3aAssertPacket(t, m, b, sector, 0, next.PrecomputedAux)
}

func TestP3aLegacyFixedAuxPrefixRemainsDisjointAndGrowthFailsClosed(t *testing.T) {
	m, b, s := p3aFixture(t)
	// Public fixed slots are imported compatibility ownership. Packed tail
	// allocation must reserve the complete prefix, including unoccupied slot0.
	legacy := p3aBrick([]int{0}, 0x1234)
	m.VoxelAuxAlloc.Alloc()
	slot := m.VoxelAuxAlloc.Alloc()
	m.BrickToAuxSlot[legacy] = slot
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 4096)
	for i := 0; i < 2*VoxelAuxRecordBytes; i++ {
		b.buffers[m.DenseOccupancyBuf][i] = 0x7d
	}
	prefix := bytes.Clone(b.buffers[m.DenseOccupancyBuf][:2*VoxelAuxRecordBytes])
	o := p2aObject(s, 1)
	fresh := p3aBrick([]int{511}, 0xabcd)
	p2cPut(o, 0, fresh)
	p2cStep(t, m, b, s)
	sector := o.XBrickMap.Sectors[[3]int{}]
	base, packet := p3aAssertPacket(t, m, b, sector, 0, fresh.PrecomputedAux)
	if uint64(base)*4 < 2*VoxelAuxRecordBytes || !bytes.Equal(prefix, b.buffers[m.DenseOccupancyBuf][:2*VoxelAuxRecordBytes]) {
		t.Fatal("packed packet overlapped legacy dense fixed prefix")
	}
	oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
	oldBuffer := m.DenseOccupancyBuf
	// Later public prefix growth intersects an assigned packed interval. Reject
	// publication instead of reinterpreting either ownership domain.
	m.VoxelAuxAlloc.Alloc()
	p3aDirty(o, 0)
	p2cStep(t, m, b, s)
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
		t.Fatal("conflicting public fixed-prefix growth failed open")
	}
	p3aAssertUnchanged(t, b, oldBuffer, base, packet)
	if !bytes.Equal(prefix, b.buffers[oldBuffer][:2*VoxelAuxRecordBytes]) {
		t.Fatal("failed prefix growth corrupted imported dense bytes")
	}
	s1kReady(t, m, o, false)
}

func TestP3aInitPolicyRejectsPreexistingGeometryAuxiliaryAndStagingOwnership(t *testing.T) {
	for _, ownership := range []string{"solid geometry", "public auxiliary", "staged growth"} {
		t.Run(ownership, func(t *testing.T) {
			m, b, s := p2cFixture(t)
			policy := p3aPolicy(t, m)
			switch ownership {
			case "public auxiliary":
				slot := m.VoxelAuxAlloc.Alloc()
				m.BrickToAuxSlot[p3aBrick([]int{0}, 0)] = slot
			case "solid geometry":
				o := p2aObject(s, 1)
				p2cPut(o, 0, p2aBrick("solid", 1))
				p2cStep(t, m, b, s)
			case "staged growth":
				o := p2aObject(s, 129)
				for key := range o.XBrickMap.Sectors {
					schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p2aBrick("uniform", 1))
				}
				m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
				p2cStep(t, m, b, s)
				if !m.VoxelGPUWorkStats().Pending {
					t.Fatal("fixture failed to establish staged ownership")
				}
			}
			if err := policy.SetPackedVoxelNormals(true); err == nil {
				t.Fatal("init-only policy accepted existing", ownership)
			}
			if err := policy.SetPackedVoxelNormals(false); err != nil {
				t.Fatal("same-value policy rejected with existing ownership", err)
			}
		})
	}
}

func TestP3aReplacedPacketsWaitForTheirStampedSubmission(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	o := p2aObject(s, 1)
	brick := p3aBrick([]int{511}, 0x8000)
	p2cPut(o, 0, brick)
	p2cStep(t, m, b, s)
	sector := o.XBrickMap.Sectors[[3]int{}]
	firstBase, firstPacket := p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	oldBuffer := m.DenseOccupancyBuf
	// Two successful replacements leave 204 assigned/quarantined bytes. The
	// next exact 68-byte COW packet cannot use either unfenced retired interval.
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint16(brick.PrecomputedAux[64+2*511:], uint16(0x4001+i))
		p3aDirty(o, 0)
		p2cStep(t, m, b, s)
		p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	}
	oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
	binary.LittleEndian.PutUint16(brick.PrecomputedAux[64+2*511:], 0xabcd)
	p3aDirty(o, 0)
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		p2cStep(t, m, b, s)
	}
	if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
		t.Fatal("replacement packet reused an unstamped span as capacity credit")
	}
	p3aAssertUnchanged(t, b, oldBuffer, firstBase, firstPacket)
	s1kReady(t, m, o, false)
	m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(281))
	p2cStep(t, m, b, s)
	s1kReady(t, m, o, false)
	b.completed[280] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, s)
	s1kReady(t, m, o, false)
	b.completed[281] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, s)
	p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	s1kReady(t, m, o, true)
	if b.BufferSize(m.DenseOccupancyBuf) != 256 {
		t.Fatal("completed replacement spans were not reusable at constrained capacity")
	}
}

func TestP3aPartialMembershipRemovalRetiresOldPacketsAndCopiesSurvivor(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	o := p2aObject(s, 1)
	o.XBrickMap.ID = 290
	removed, survivor := p3aBrick([]int{0}, 0x4000), p3aBrick([]int{511}, 0x8000)
	p2cPut(o, 0, removed)
	p2cPut(o, 63, survivor)
	p2cStep(t, m, b, s)
	sector := o.XBrickMap.Sectors[[3]int{}]
	removedBase, removedPacket := p3aAssertPacket(t, m, b, sector, 0, removed.PrecomputedAux)
	survivorBase, survivorPacket := p3aAssertPacket(t, m, b, sector, 63, survivor.PrecomputedAux)
	oldBuffer := m.DenseOccupancyBuf
	if removedBase+17 != survivorBase && survivorBase+17 != removedBase {
		t.Fatal("fixture did not create adjacent original packet intervals")
	}
	s1l3ClearIndex(o, 0)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	p2cStep(t, m, b, s)
	nextSurvivorBase, _ := p3aAssertPacket(t, m, b, sector, 63, survivor.PrecomputedAux)
	if nextSurvivorBase == survivorBase {
		t.Fatal("full-sector membership publication reused survivor packet instead of COW")
	}
	_, mask := p2cHeader(t, m, b, sector)
	if mask != uint64(1)<<63 {
		t.Fatal("removed record remains reachable")
	}
	p3aAssertUnchanged(t, b, oldBuffer, removedBase, removedPacket)
	p3aAssertUnchanged(t, b, oldBuffer, survivorBase, survivorPacket)
	if !m.RetainVoxelMap(o.XBrickMap) {
		t.Fatal("surviving map not retainable")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+32+32+68 {
		t.Fatal("retention charged removed or quarantined old packets", got)
	}
	arrival := p2aObject(s, 1)
	arrival.XBrickMap.ID = 291
	arrival.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	fresh := p3aBrick(p3aIndices(36), 0xabcd)
	p2cPut(arrival, 0, fresh)
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		p2cStep(t, m, b, s)
	}
	s1kReady(t, m, arrival, false)
	s1kReady(t, m, o, true)
	m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(292))
	p2cStep(t, m, b, s)
	s1kReady(t, m, arrival, false)
	b.completed[292] = true
	m.AdvanceRetiredBuffers()
	p2cStep(t, m, b, s)
	next, _ := p3aAssertPacket(t, m, b, arrival.XBrickMap.Sectors[[3]int{}], 0, fresh.PrecomputedAux)
	if next != min(removedBase, survivorBase) || b.BufferSize(m.DenseOccupancyBuf) != 256 {
		t.Fatal("completed removed/replaced packet spans did not coalesce for arrival")
	}
	stable, _ := p3aAssertPacket(t, m, b, sector, 63, survivor.PrecomputedAux)
	if stable != nextSurvivorBase {
		t.Fatal("removed receipt retirement freed the survivor's newer packet")
	}
	s1kReady(t, m, arrival, true)
	s1kReady(t, m, o, true)
}

func TestP3aLatePacketGrowthDoesNotInvadeAnotherPendingClaim(t *testing.T) {
	m, b, s := p3aFixture(t)
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 256)
	b.maxAuxBytes = 256
	fitting := p2aObject(s, 1)
	fitting.XBrickMap.ID = 300
	fitting.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
	fittingBrick := p3aBrick(p3aIndices(36), 0x4000)
	p2cPut(fitting, 0, fittingBrick)
	grown := p2aObject(s, 1)
	grown.XBrickMap.ID = 301
	grown.VoxelGPUAdmissionOptional = true
	grown.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	grownBrick := p3aBrick([]int{511}, 0x8000)
	p2cPut(grown, 63, grownBrick)
	b.afterMaterialWrite = func() { copy(grownBrick.PrecomputedAux, p3aBrick(p3aIndices(512), 0xabcd).PrecomputedAux) }
	p2cStep(t, m, b, s)
	s1kReady(t, m, fitting, true)
	s1kReady(t, m, grown, false)
	fittingSector := fitting.XBrickMap.Sectors[[3]int{}]
	fittingBase, fittingPacket := p3aAssertPacket(t, m, b, fittingSector, 0, fittingBrick.PrecomputedAux)
	oldBuffer := m.DenseOccupancyBuf
	oldFittingRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, fittingSector, 0))
	if m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 1 || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 136 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 32 {
		t.Fatal("late grown unit wrote partial content or blocked fitting pending claim")
	}
	p2bAssertLookup(t, m, b.p2bNative, grown, [3]int{}, false)
	if !m.RetainVoxelMap(grown.XBrickMap) {
		t.Fatal("deferred late-grown target not retainable")
	}
	if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
		t.Fatal("denied late growth acquired a real auxiliary lease", got)
	}
	b.maxAuxBytes = 4096
	p2cStep(t, m, b, s)
	p3aAssertPacket(t, m, b, grown.XBrickMap.Sectors[[3]int{}], 63, grownBrick.PrecomputedAux)
	p3aAssertUnchanged(t, b, oldBuffer, fittingBase, fittingPacket)
	if !bytes.Equal(oldFittingRow, p2bRecord(t, m, b.p2bNative, fittingSector, 0)) {
		t.Fatal("successful late growth republished or corrupted fitting owner's record")
	}
	p3aAssertPacket(t, m, b, fittingSector, 0, fittingBrick.PrecomputedAux)
	s1kReady(t, m, grown, true)
	s1kReady(t, m, fitting, true)
}

func TestP3aSharedPacketDedupTracksLatestPhysicalPublication(t *testing.T) {
	m, b, s := p3aFixture(t)
	owners := []*core.VoxelObject{p2aObject(s, 1), p2aObject(s, 1), p2aObject(s, 1)}
	brick := p3aBrick([]int{511}, 0x4000)
	p2cPut(owners[0], 0, brick)
	sector := owners[0].XBrickMap.Sectors[[3]int{}]
	for i, o := range owners {
		o.XBrickMap.ID = uint32(310 + i)
		o.VoxelUploadPriority = core.VoxelUploadPriorityKeep
		o.VoxelUploadOrder = uint64(i + 1)
		o.XBrickMap.Sectors[[3]int{}] = sector
	}
	packetA := bytes.Clone(brick.PrecomputedAux)
	packetB := p3aBrick([]int{511}, 0x8000).PrecomputedAux
	revisions := []uint64{owners[0].XBrickMap.Revision, owners[1].XBrickMap.Revision, owners[2].XBrickMap.Revision}
	callbackCount := 0
	b.afterAuxWrite = func() {
		callbackCount++
		copy(brick.PrecomputedAux, packetB)
		b.afterAuxWrite = func() {
			callbackCount++
			copy(brick.PrecomputedAux, packetA)
		}
	}
	p2cStep(t, m, b, s)
	if callbackCount != 2 {
		t.Fatalf("A-to-B-to-A callback sequence ran %d callbacks, want2", callbackCount)
	}
	for i, o := range owners {
		if o.XBrickMap.Revision != revisions[i] || o.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0) != brick {
			t.Fatal("raw callback fixture changed revision or source identity")
		}
	}
	p3aAssertPacket(t, m, b, sector, 0, packetA)
	if got := p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf"); got != 3*68 {
		t.Fatalf("A-to-B-to-A physical publication wrote %d auxiliary bytes, want204: historical A cannot deduplicate latest B", got)
	}
	ready, pendingSectors, pendingBricks := m.RenderVoxelObjectReady(owners[2], owners[2].XBrickMap, owners[2].XBrickMap.Revision)
	t.Logf("A-to-B-to-A callbacks=%d third owner ready=%t pending sectors=%d bricks=%d", callbackCount, ready, pendingSectors, pendingBricks)
	s1kReady(t, m, owners[2], true)
}

func TestP3aSharedIdenticalPacketAcknowledgesWithinOnePhysicalUnitBudget(t *testing.T) {
	m, b, s := p3aFixture(t)
	first, second := p2aObject(s, 1), p2aObject(s, 1)
	first.XBrickMap.ID = 320
	second.XBrickMap.ID = 321
	first.VoxelUploadOrder = 1
	second.VoxelUploadOrder = 2
	first.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	second.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	brick := p3aBrick([]int{511}, 0x4000)
	p2cPut(first, 0, brick)
	sector := first.XBrickMap.Sectors[[3]int{}]
	second.XBrickMap.Sectors[[3]int{}] = sector
	// Two material rows plus one physical sector publication fit exactly. A
	// second owner acknowledging the same already-written packet costs nothing.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128 + 32 + 32 + 68, MaxSectors: 1, MaxBricks: 1})
	p2cStep(t, m, b, s)
	p3aAssertPacket(t, m, b, sector, 0, brick.PrecomputedAux)
	if m.VoxelUploadBytes != 260 || m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 1 {
		t.Fatalf("shared physical unit charge bytes=%d sectors=%d bricks=%d", m.VoxelUploadBytes, m.VoxelSectorsUploaded, m.VoxelBricksUploaded)
	}
	if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != 68 || p2bWrittenBytes(b.p2bNative, "SectorTableBuf") != 32 || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 32 {
		t.Fatal("identical shared packet wrote more than one physical unit")
	}
	for _, owner := range []*core.VoxelObject{first, second} {
		s1kReady(t, m, owner, true)
		if len(owner.XBrickMap.DirtySectors) != 0 {
			t.Fatal("zero-cost shared receipt acknowledgment left dirty sector pending")
		}
	}
}
