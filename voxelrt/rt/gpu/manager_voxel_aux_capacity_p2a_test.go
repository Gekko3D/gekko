package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/bits"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Preserve admission, unit scheduling, actual byte encoding, acknowledgement,
// migration and lookup publication. Only physical GPU operations are replaced.
type p2aNative struct {
	*s1kNative
	maxAuxBytes        uint64
	afterMaterialWrite func()
}

func (b *p2aNative) CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error) {
	if label == "DenseOccupancyBuf" && b.maxAuxBytes != 0 && size > b.maxAuxBytes {
		return nil, errors.New("P2a physical auxiliary capacity refused")
	}
	return b.s1kNative.CreateBuffer(label, size, uniform)
}

func (b *p2aNative) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if err := b.s1kNative.WriteBuffer(buffer, offset, data); err != nil {
		return err
	}
	if b.labels[buffer] == "MaterialBuf" && b.afterMaterialWrite != nil {
		callback := b.afterMaterialWrite
		b.afterMaterialWrite = nil
		callback()
	}
	return nil
}

func p2aFixture(t *testing.T) (*GpuBufferManager, *p2aNative, *core.Scene) {
	t.Helper()
	m := s1iManager()
	m.VoxelPayloadPageSize, m.VoxelPayloadBricks, m.VoxelPayloadPageCount = 16, 2, 1
	m.VoxelPayloadTex[0] = new(wgpu.Texture)
	b := &p2aNative{s1kNative: &s1kNative{t: t, buffers: make(map[*wgpu.Buffer][]byte), labels: make(map[*wgpu.Buffer]string), payload: make([]byte, 4096)}}
	fields := [7]**wgpu.Buffer{&m.SectorTableBuf, &m.BrickTableBuf, &m.DenseOccupancyBuf, &m.MaterialBuf, &m.SectorGridBuf, &m.DirectSectorLookupBuf, &m.SectorGridParamsBuf}
	// One auxiliary record plus normal buffer alignment. Brick table space
	// remains independently reserved in fixed 64-record sector ranges.
	sizes := [7]uint64{512, 128 * BrickRecordSize, 1280, 256 * 64, 32768, 256, 256}
	for i, field := range fields {
		buffer, err := b.CreateBuffer(voxelBufferLabels[i], sizes[i], i == 6)
		if err != nil {
			t.Fatal(err)
		}
		*field = buffer
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	return m, b, core.NewScene()
}

func p2aObject(scene *core.Scene, sectors int) *core.VoxelObject {
	o := s1iObject(uint32(10+len(scene.Objects)), sectors, false)
	// s1iObject seeds one voxel; replace with explicit test-controlled bricks.
	for _, sector := range o.XBrickMap.Sectors {
		sector.BrickMask64 = 0
		sector.PackedBricks = nil
	}
	scene.Objects = append(scene.Objects, o)
	return o
}

func p2aBrick(kind string, marker byte) *volume.Brick {
	b := scheduleBrick(kind)
	// Valid-sized authored sidecar lets tests inspect exact occupancy+normal
	// bytes without coupling these allocation contracts to runtime normal math.
	b.PrecomputedAux[0] = 1
	for i := 64; i < len(b.PrecomputedAux); i++ {
		b.PrecomputedAux[i] = marker
	}
	return b
}

func p2aStep(t *testing.T, m *GpuBufferManager, b *p2aNative, scene *core.Scene) bool {
	t.Helper()
	b.reset()
	recreated := m.updateVoxelData(scene, b)
	m.updateSectorGrid(scene)
	if m.VoxelUploadBytes != b.contentBytes {
		t.Fatalf("charged upload bytes=%d, physical bytes=%d", m.VoxelUploadBytes, b.contentBytes)
	}
	s1kCharge(t, m, b.s1kNative)
	return recreated
}

func p2aAuxBytes(t *testing.T, m *GpuBufferManager, b *p2aNative, xbm *volume.XBrickMap, key [6]int) (uint32, []byte) {
	t.Helper()
	sector := xbm.Sectors[[3]int{key[0], key[1], key[2]}]
	info, ok := m.SectorToInfo[sector]
	if !ok {
		t.Fatal("sector was not admitted")
	}
	index := uint32(key[3] + key[4]*4 + key[5]*16)
	header := b.buffers[m.SectorTableBuf][uint64(info.SlotIndex)*32 : uint64(info.SlotIndex+1)*32]
	recordBase := binary.LittleEndian.Uint32(header[16:])
	if binary.LittleEndian.Uint32(header[28:]) == 1 {
		mask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
		if mask&(uint64(1)<<index) == 0 {
			t.Fatalf("absent packed local index%d has no record", index)
		}
		index = uint32(bits.OnesCount64(mask & ((uint64(1) << index) - 1)))
	}
	start := uint64(recordBase+index) * BrickRecordSize
	records := b.buffers[m.BrickTableBuf]
	if start+BrickRecordSize > uint64(len(records)) {
		t.Fatal("fixed sector brick record exceeds buffer")
	}
	base := binary.LittleEndian.Uint32(records[start+24:])
	auxStart := uint64(base) * 4
	aux := b.buffers[m.DenseOccupancyBuf]
	if base%uint32(volume.VoxelAuxWordCount) != 0 || auxStart+VoxelAuxRecordBytes > uint64(len(aux)) {
		t.Fatal("published GPU auxiliary word base exceeds actual buffer capacity")
	}
	return base, aux[auxStart : auxStart+VoxelAuxRecordBytes]
}

func p2aAssertBrick(t *testing.T, m *GpuBufferManager, b *p2aNative, xbm *volume.XBrickMap, key [6]int, brick *volume.Brick) uint32 {
	t.Helper()
	base, data := p2aAuxBytes(t, m, b, xbm, key)
	if !bytes.Equal(data, brick.PrecomputedAux) {
		t.Fatal("GPU auxiliary offset points at another brick's occupancy/normal bytes")
	}
	return base
}

func p2aLockAuxiliaryRows(t *testing.T, m *GpuBufferManager, b *p2aNative, rows uint64) uint64 {
	t.Helper()
	capacity := (rows*VoxelAuxRecordBytes + 255) &^ uint64(255)
	for _, slot := range m.BrickToAuxSlot {
		if (uint64(slot)+1)*VoxelAuxRecordBytes > capacity {
			t.Fatal("fixture live auxiliary rows exceed requested physical capacity")
		}
	}
	// Model an adapter allocation with exactly this byte-backed capacity. This
	// test fixture normalization is independent of production growth policy;
	// occupied GPU bytes and published offsets remain untouched.
	b.buffers[m.DenseOccupancyBuf] = b.buffers[m.DenseOccupancyBuf][:capacity]
	b.maxAuxBytes = capacity
	return capacity
}

func TestP2aSparseAuxiliaryCapacityTracksOccupiedBricks(t *testing.T) {
	for _, kind := range []string{"solid", "uniform", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			m, b, scene := p2aFixture(t)
			count := 32
			if kind == "mixed" {
				count = 8
			} // Eight physical payload slots in this fixture.
			o := p2aObject(scene, count)
			bricks := make([]*volume.Brick, count)
			for i := range bricks {
				bricks[i] = p2aBrick(kind, byte(i+1))
				schedulePutBrick(o, [6]int{i, 0, 0, 0, 0, 0}, bricks[i])
			}
			p2aStep(t, m, b, scene)
			capacity := b.BufferSize(m.DenseOccupancyBuf)
			if b.BufferSize(m.BrickTableBuf) < uint64(count*BrickRecordSize) {
				t.Fatal("occupied packed brick rows do not fit physical table")
			}
			if capacity < uint64(count*VoxelAuxRecordBytes) || capacity > uint64(2*count*VoxelAuxRecordBytes+256) {
				t.Fatalf("%d occupied %s bricks reserved %d auxiliary bytes; want aligned geometric capacity near %d", count, kind, capacity, count*VoxelAuxRecordBytes)
			}
			seen := make(map[uint32]bool)
			for i, brick := range bricks {
				base := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{i, 0, 0, 0, 0, 0}, brick)
				if seen[base] {
					t.Fatal("distinct occupied bricks alias auxiliary rows")
				}
				seen[base] = true
			}
			s1kReady(t, m, o, true)
			t.Logf("%d sparse sectors: fixed brick-table bytes=%d, physical auxiliary bytes=%d, live auxiliary bytes=%d", count, b.BufferSize(m.BrickTableBuf), capacity, count*VoxelAuxRecordBytes)
		})
	}
}

func TestP2aDenseSectorKeepsAll64AuxiliaryRows(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 1)
	bricks := make([]*volume.Brick, 64)
	for i := range bricks {
		bricks[i] = p2aBrick("uniform", byte(i+1))
		schedulePutBrick(o, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, bricks[i])
	}
	p2aStep(t, m, b, scene)
	seen := make(map[uint32]bool)
	for i, brick := range bricks {
		base := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, brick)
		if seen[base] {
			t.Fatal("dense sector reused a live auxiliary slot")
		}
		seen[base] = true
	}
	if b.BufferSize(m.DenseOccupancyBuf) < 64*VoxelAuxRecordBytes {
		t.Fatal("dense sector was under-reserved")
	}
	s1kReady(t, m, o, true)
}

func TestP2aSharedMapsBricksAndPendingFullReserveDistinctPointersOnce(t *testing.T) {
	m, b, scene := p2aFixture(t)
	shared := p2aBrick("uniform", 9)
	a := p2aObject(scene, 1)
	schedulePutBrick(a, [6]int{}, shared)
	alias := p2aObject(scene, 0)
	alias.XBrickMap = a.XBrickMap
	independent := p2aObject(scene, 1)
	schedulePutBrick(independent, [6]int{}, shared)
	pending := p2aObject(scene, 1)
	schedulePutBrick(pending, [6]int{}, shared)
	full := pending.XBrickMap
	if !pending.SetRenderLOD2(a.XBrickMap) || !pending.SetPendingFullUpload() {
		t.Fatal("pending full fixture was rejected")
	}
	p2aStep(t, m, b, scene)
	if b.BufferSize(m.DenseOccupancyBuf) > 2*VoxelAuxRecordBytes+256 {
		t.Fatal("shared current/future brick pointer was reserved once per owner or sector")
	}
	base := p2aAssertBrick(t, m, b, a.XBrickMap, [6]int{}, shared)
	for _, xbm := range []*volume.XBrickMap{independent.XBrickMap, full} {
		if p2aAssertBrick(t, m, b, xbm, [6]int{}, shared) != base {
			t.Fatal("shared brick received multiple GPU auxiliary rows")
		}
	}
	for _, o := range scene.Objects {
		s1kReady(t, m, o, true)
	}
	if ready, _, _ := m.PendingFullVoxelObjectReady(pending, full, full.Revision); !ready {
		t.Fatal("shared pending full target did not become ready")
	}
}

func TestP2aStableDirtyBrickAddsAuxiliaryDemandWithoutStructureDirty(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 1)
	first := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, first)
	p2aStep(t, m, b, scene)
	oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first)
	added := p2aBrick("uniform", 2)
	key := [6]int{0, 0, 0, 1, 0, 0}
	schedulePutBrick(o, key, added)
	o.XBrickMap.DirtyBricks[key] = true
	if o.XBrickMap.StructureDirty {
		t.Fatal("fixture unexpectedly changed sector structure")
	}
	p2aStep(t, m, b, scene)
	if p2aAssertBrick(t, m, b, o.XBrickMap, key, added) == oldBase {
		t.Fatal("dirty brick insertion aliased existing auxiliary data")
	}
	if p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first) != oldBase {
		t.Fatal("insertion moved unchanged auxiliary offset")
	}
	s1kReady(t, m, o, true)
}

func TestP2aBalancedSameUnitReplacementFitsLockedAuxiliaryCapacity(t *testing.T) {
	for _, unit := range []string{"brick", "sector", "sector and empty sector"} {
		t.Run(unit, func(t *testing.T) {
			m, b, scene := p2aFixture(t)
			o := p2aObject(scene, 1)
			old := p2aBrick("uniform", 1)
			schedulePutBrick(o, [6]int{}, old)
			p2aStep(t, m, b, scene)
			buffer := m.DenseOccupancyBuf
			oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, old)
			p2aLockAuxiliaryRows(t, m, b, 1)
			next := p2aBrick("uniform", 2)
			schedulePutBrick(o, [6]int{}, next)
			if unit != "brick" {
				o.XBrickMap.DirtySectors[[3]int{}] = true
			} else {
				o.XBrickMap.DirtyBricks[[6]int{}] = true
			}
			if unit == "sector and empty sector" {
				// Structural growth elsewhere does not replace this unit's sector
				// or consume auxiliary rows: it is still an exclusive one-for-one
				// content replacement within the original allocated sector.
				o.XBrickMap.Sectors[[3]int{1, 0, 0}] = volume.NewSector(1, 0, 0)
				o.XBrickMap.StructureDirty = true
				o.XBrickMap.Revision++
			}
			p2aStep(t, m, b, scene)
			if m.DenseOccupancyBuf != buffer || p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, next) != oldBase {
				t.Fatal("balanced same-unit replacement required extra auxiliary storage")
			}
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2aMovedOldPointerCannotSpendDeferredUnitRelease(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 2)
	old := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, old)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, old)
	fresh := p2aBrick("uniform", 2)
	schedulePutBrick(o, [6]int{}, fresh)
	schedulePutBrick(o, [6]int{1, 0, 0, 0, 0, 0}, old)
	o.XBrickMap.DirtySectors[[3]int{}] = true
	o.XBrickMap.DirtySectors[[3]int{1, 0, 0}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 1, MaxBricks: 64})
	p2aStep(t, m, b, scene)
	if !o.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("replacement consumed a slot whose old pointer is needed by another deferred unit")
	}
	if base, exists := m.BrickToAuxSlot[old]; !exists || base*uint32(volume.VoxelAuxWordCount) != oldBase {
		t.Fatal("moving old pointer lost its existing auxiliary ownership")
	}
	if _, exists := m.BrickToAuxSlot[fresh]; exists {
		t.Fatal("deferred replacement allocated out-of-capacity auxiliary storage")
	}
	if !o.XBrickMap.DirtySectors[[3]int{1, 0, 0}] {
		p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{1, 0, 0, 0, 0, 0}, old)
	}
	s1kReady(t, m, o, false)
	b.maxAuxBytes = 0
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	p2aStep(t, m, b, scene)
	p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, fresh)
	p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{1, 0, 0, 0, 0, 0}, old)
	s1kReady(t, m, o, true)
}

func TestP2aSharedFreshPointerCannotBorrowAnotherDeferredUnitsRelease(t *testing.T) {
	m, b, scene := p2aFixture(t)
	owner := p2aObject(scene, 1)
	old := p2aBrick("uniform", 1)
	schedulePutBrick(owner, [6]int{}, old)
	uncredited := p2aObject(scene, 1)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	oldBase, oldBytes := p2aAuxBytes(t, m, b, owner.XBrickMap, [6]int{})
	saved := bytes.Clone(oldBytes)
	fresh := p2aBrick("uniform", 2)
	schedulePutBrick(owner, [6]int{}, fresh)
	schedulePutBrick(uncredited, [6]int{}, fresh)
	owner.XBrickMap.DirtySectors[[3]int{}] = true
	uncredited.XBrickMap.DirtySectors[[3]int{}] = true
	owner.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	uncredited.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 1, MaxBricks: 64})
	p2aStep(t, m, b, scene)
	if !uncredited.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatal("highest-priority unit without releases borrowed another deferred unit's old auxiliary slot")
	}
	if owner.XBrickMap.DirtySectors[[3]int{}] {
		// The older allocation is still the queued GPU authority until its
		// owning replacement unit actually writes. No hypothetical clear credit
		// may retire these bytes to serve the higher-priority shared arrival.
		start := uint64(oldBase) * 4
		if !bytes.Equal(saved, b.buffers[m.DenseOccupancyBuf][start:start+VoxelAuxRecordBytes]) {
			t.Fatal("deferred owner's old auxiliary bytes were overwritten")
		}
		if _, exists := m.BrickToAuxSlot[old]; !exists {
			t.Fatal("deferred old ownership was released before its upload unit")
		}
	} else {
		p2aAssertBrick(t, m, b, owner.XBrickMap, [6]int{}, fresh)
	}
	b.maxAuxBytes = 0
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	p2aStep(t, m, b, scene)
	if p2aAssertBrick(t, m, b, owner.XBrickMap, [6]int{}, fresh) != p2aAssertBrick(t, m, b, uncredited.XBrickMap, [6]int{}, fresh) {
		t.Fatal("shared fresh pointer was allocated twice after safe service")
	}
	s1kReady(t, m, owner, true)
	s1kReady(t, m, uncredited, true)
}

func TestP2aStructuralDirtyBrickUnitsCannotBorrowDeferredClearCredit(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 2)
	clearKey := [6]int{1, 0, 0, 1, 0, 0}
	old := p2aBrick("uniform", 1)
	schedulePutBrick(o, clearKey, old)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	oldBase, oldBytes := p2aAuxBytes(t, m, b, o.XBrickMap, clearKey)
	saved := bytes.Clone(oldBytes)
	fresh := p2aBrick("uniform", 2)
	freshKey := [6]int{}
	clearSector := o.XBrickMap.Sectors[[3]int{1, 0, 0}]
	packed := clearSector.GetPackedIndex(1)
	clearSector.PackedBricks = append(clearSector.PackedBricks[:packed], clearSector.PackedBricks[packed+1:]...)
	clearSector.BrickMask64 &^= uint64(1) << 1
	o.XBrickMap.DirtyBricks[clearKey] = true
	o.XBrickMap.Revision++
	schedulePutBrick(o, freshKey, fresh)
	o.XBrickMap.DirtyBricks[freshKey] = true
	o.XBrickMap.Sectors[[3]int{2, 0, 0}] = volume.NewSector(2, 0, 0)
	o.XBrickMap.StructureDirty = true
	o.XBrickMap.Revision++
	// Sector ordering tries fresh sector0 before the independent clear in1.
	// Membership changes require one full-sector transaction; the unrelated
	// empty sector2 cannot lend a release to the earlier fresh sector.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 1, MaxBricks: 1})
	p2aStep(t, m, b, scene)
	if !o.XBrickMap.DirtyBricks[freshKey] {
		t.Fatal("fresh dirty-brick unit borrowed another deferred dirty-brick unit's release under StructureDirty")
	}
	if _, exists := m.BrickToAuxSlot[fresh]; exists {
		t.Fatal("fresh unit allocated beyond one-row physical capacity before its own release")
	}
	if o.XBrickMap.DirtyBricks[clearKey] {
		start := uint64(oldBase) * 4
		if !bytes.Equal(saved, b.buffers[m.DenseOccupancyBuf][start:start+VoxelAuxRecordBytes]) {
			t.Fatal("deferred clear's queued auxiliary bytes were overwritten")
		}
		if _, exists := m.BrickToAuxSlot[old]; !exists {
			t.Fatal("deferred dirty-brick clear released ownership before execution")
		}
	}
	// Once physical growth is possible, an earlier fresh unit can write while
	// the later clear is still deferred; it must preserve those old GPU bytes.
	b.maxAuxBytes = 0
	p2aStep(t, m, b, scene)
	p2aAssertBrick(t, m, b, o.XBrickMap, freshKey, fresh)
	if o.XBrickMap.DirtyBricks[clearKey] {
		start := uint64(oldBase) * 4
		if !bytes.Equal(saved, b.buffers[m.DenseOccupancyBuf][start:start+VoxelAuxRecordBytes]) {
			t.Fatal("admitting fresh unit overwrote another deferred clear's old row")
		}
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	p2aStep(t, m, b, scene)
	p2aAssertBrick(t, m, b, o.XBrickMap, freshKey, fresh)
	s1kReady(t, m, o, true)
}

func TestP2aStructuralReplacementReusesOnlyFinalReferenceAuxiliarySlot(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "final owner", true: "retained owner"}[retained], func(t *testing.T) {
			m, b, scene := p2aFixture(t)
			o := p2aObject(scene, 1)
			old := p2aBrick("uniform", 1)
			schedulePutBrick(o, [6]int{}, old)
			var retainedMap *volume.XBrickMap
			if retained {
				alias := p2aObject(scene, 1)
				alias.XBrickMap.Sectors[[3]int{}] = o.XBrickMap.Sectors[[3]int{}]
				retainedMap = alias.XBrickMap
			}
			p2aStep(t, m, b, scene)
			oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, old)
			if retained {
				if !m.RetainVoxelMap(retainedMap) {
					t.Fatal("allocated map retention failed")
				}
				scene.Objects = []*core.VoxelObject{o}
			}
			p2aLockAuxiliaryRows(t, m, b, 1)
			buffer := m.DenseOccupancyBuf
			next := p2aBrick("uniform", 2)
			o.XBrickMap.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
			schedulePutBrick(o, [6]int{}, next)
			o.XBrickMap.StructureDirty = true
			p2aStep(t, m, b, scene)
			if retained {
				s1kReady(t, m, o, false)
				if p2aAssertBrick(t, m, b, retainedMap, [6]int{}, old) != oldBase {
					t.Fatal("retained reference lost its assigned auxiliary offset")
				}
				if _, exists := m.BrickToAuxSlot[next]; exists {
					t.Fatal("structural replacement spent a retained owner's auxiliary slot")
				}
				m.RetainedVoxelMapBudgetBytes = 1 // Evict the inactive old map through normal retention.
				p2aStep(t, m, b, scene)
			}
			if m.DenseOccupancyBuf != buffer || p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, next) != oldBase {
				t.Fatal("final-reference structural replacement failed exact-capacity slot reuse")
			}
			s1kReady(t, m, o, true)
		})
	}
}

func TestP2aPostAdmissionFreshDemandDefersBeforeAnyOutOfBoundsWrite(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 1)
	first := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, first)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	// Queue a material refresh to inject new geometry after capacity planning,
	// before the existing dirty sector's executor is reached. Raw map changes
	// do not supply a revision signal, so the execution fit check must qualify it.
	o.MaterialTable = []core.Material{{Emission: 2}}
	o.XBrickMap.DirtySectors[[3]int{}] = true
	second := p2aBrick("uniform", 2)
	b.afterMaterialWrite = func() {
		sector := o.XBrickMap.Sectors[[3]int{}]
		_, _ = sector.GetOrCreateBrick(1, 0, 0)
		sector.PackedBricks[sector.GetPackedIndex(1)] = second
	}
	p2aStep(t, m, b, scene)
	if b.afterMaterialWrite != nil {
		t.Fatal("post-admission fixture never reached material write")
	}
	if !o.XBrickMap.DirtySectors[[3]int{}] || m.VoxelSectorsUploaded != 0 {
		t.Fatal("unplanned fresh auxiliary demand was acknowledged instead of deferred")
	}
	if _, exists := m.BrickToAuxSlot[second]; exists {
		t.Fatal("unplanned fresh brick allocated a slot outside physical capacity")
	}
	p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first)
	s1kReady(t, m, o, false)
	b.maxAuxBytes = 0
	p2aStep(t, m, b, scene)
	p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{0, 0, 0, 1, 0, 0}, second)
	s1kReady(t, m, o, true)
}

func TestP2aDeniedCandidateRollsBackFreshAuxiliaryReservations(t *testing.T) {
	m, b, scene := p2aFixture(t)
	resident := p2aObject(scene, 1)
	shared := p2aBrick("uniform", 1)
	schedulePutBrick(resident, [6]int{}, shared)
	// Keep material admission independent of the refused auxiliary growth:
	// the later sharing owner needs its own already-reserved private block.
	b.buffers[m.MaterialBuf] = make([]byte, 2*256*64)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	oldBase := p2aAssertBrick(t, m, b, resident.XBrickMap, [6]int{}, shared)
	denied := p2aObject(scene, 1)
	denied.VoxelGPUAdmissionOptional = true
	fresh := p2aBrick("uniform", 2)
	schedulePutBrick(denied, [6]int{}, fresh)
	// A later candidate can reuse current auxiliary storage without inheriting
	// the refused candidate's reservation or leaking its logical assignments.
	arrival := p2aObject(scene, 1)
	arrival.VoxelGPUAdmissionOptional = true
	schedulePutBrick(arrival, [6]int{}, shared)
	for i := 0; i < 3; i++ {
		p2aStep(t, m, b, scene)
		if m.Allocations[denied.XBrickMap] != nil || m.MaterialAllocations[denied] != nil {
			t.Fatal("refused auxiliary candidate published geometry/material ownership")
		}
		if _, exists := m.BrickToAuxSlot[fresh]; exists {
			t.Fatal("refused candidate leaked a fresh auxiliary slot")
		}
		s1kReady(t, m, resident, true)
		s1kReady(t, m, arrival, true)
		if p2aAssertBrick(t, m, b, arrival.XBrickMap, [6]int{}, shared) != oldBase {
			t.Fatal("later sharing candidate inherited refused reservation")
		}
	}
	b.maxAuxBytes = 0
	p2aStep(t, m, b, scene)
	if p2aAssertBrick(t, m, b, denied.XBrickMap, [6]int{}, fresh) == oldBase {
		t.Fatal("admission retry aliased resident auxiliary data")
	}
	s1kReady(t, m, denied, true)
}

func TestP2aRefusedStructuralReleaseCreditCannotFinanceLaterCandidate(t *testing.T) {
	m, b, scene := p2aFixture(t)
	owner := p2aObject(scene, 1)
	old := p2aBrick("uniform", 1)
	schedulePutBrick(owner, [6]int{}, old)
	// Reserve the later object's material capacity ahead of time so its
	// rejection can only depend on auxiliary ownership/capacity accounting.
	b.buffers[m.MaterialBuf] = make([]byte, 2*256*64)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	oldSector := owner.XBrickMap.Sectors[[3]int{}]
	oldAllocation := m.Allocations[owner.XBrickMap]
	oldBase, oldBytes := p2aAuxBytes(t, m, b, owner.XBrickMap, [6]int{})
	saved := bytes.Clone(oldBytes)
	first, second := p2aBrick("uniform", 2), p2aBrick("uniform", 3)
	owner.XBrickMap.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
	schedulePutBrick(owner, [6]int{}, first)
	schedulePutBrick(owner, [6]int{0, 0, 0, 1, 0, 0}, second)
	owner.XBrickMap.StructureDirty = true
	later := p2aObject(scene, 1)
	later.VoxelGPUAdmissionOptional = true
	third := p2aBrick("uniform", 4)
	schedulePutBrick(later, [6]int{}, third)
	p2aStep(t, m, b, scene)
	if m.Allocations[owner.XBrickMap] != oldAllocation || oldAllocation.Sectors[[3]int{}] != oldSector || m.Allocations[later.XBrickMap] != nil || m.MaterialAllocations[later] != nil {
		t.Fatal("refused structural transaction published ownership or lent its release credit to later candidate")
	}
	if !bytes.Equal(saved, b.buffers[m.DenseOccupancyBuf][uint64(oldBase)*4:uint64(oldBase)*4+VoxelAuxRecordBytes]) {
		t.Fatal("refused structural release overwrote old queued auxiliary content")
	}
	for _, brick := range []*volume.Brick{first, second, third} {
		if _, exists := m.BrickToAuxSlot[brick]; exists {
			t.Fatal("refused credit transaction leaked a new auxiliary assignment")
		}
	}
	b.maxAuxBytes = 0
	p2aStep(t, m, b, scene)
	bases := []uint32{p2aAssertBrick(t, m, b, owner.XBrickMap, [6]int{}, first), p2aAssertBrick(t, m, b, owner.XBrickMap, [6]int{0, 0, 0, 1, 0, 0}, second), p2aAssertBrick(t, m, b, later.XBrickMap, [6]int{}, third)}
	if bases[0] == bases[1] || bases[0] == bases[2] || bases[1] == bases[2] {
		t.Fatal("rollback retry aliased distinct auxiliary rows")
	}
	s1kReady(t, m, owner, true)
	s1kReady(t, m, later, true)
}

func TestP2aAuxiliaryGrowthPausesAndMirrorsLatestOccupiedBytes(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 1)
	first := p2aBrick("uniform", 1)
	schedulePutBrick(o, [6]int{}, first)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 1)
	b.maxAuxBytes = 0
	buffer := m.DenseOccupancyBuf
	oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first)
	second := p2aBrick("uniform", 2)
	key := [6]int{0, 0, 0, 1, 0, 0}
	schedulePutBrick(o, key, second)
	o.XBrickMap.DirtyBricks[key] = true
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true})
	p2aStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending || m.DenseOccupancyBuf != buffer || b.creates != 0 {
		t.Fatal("paused creation published auxiliary growth")
	}
	p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first)
	s1kReady(t, m, o, false)
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30})
	p2aStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending || m.DenseOccupancyBuf != buffer || m.VoxelGPUAdmissionStats().StagingBytes == 0 {
		t.Fatal("paused copying published auxiliary generation")
	}
	for i := 64; i < len(first.PrecomputedAux); i++ {
		first.PrecomputedAux[i] = 3
	}
	o.XBrickMap.DirtyBricks[[6]int{}] = true
	o.XBrickMap.Revision++
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 4})
	for i := 0; i < 1024; i++ {
		p2aStep(t, m, b, scene)
		if !m.VoxelGPUWorkStats().Pending {
			break
		}
		if i == 1023 {
			t.Fatal("bounded auxiliary growth did not finish")
		}
	}
	if m.DenseOccupancyBuf == buffer || p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, first) != oldBase {
		t.Fatal("growth changed existing auxiliary offset or lost mirrored edit")
	}
	p2aAssertBrick(t, m, b, o.XBrickMap, key, second)
	s1kReady(t, m, o, true)
}

func TestP2aEmptyShrinkReusePreservesHighWaterWithoutAliasing(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 1)
	first, second := p2aBrick("uniform", 1), p2aBrick("uniform", 2)
	schedulePutBrick(o, [6]int{}, first)
	schedulePutBrick(o, [6]int{0, 0, 0, 1, 0, 0}, second)
	p2aStep(t, m, b, scene)
	p2aLockAuxiliaryRows(t, m, b, 2)
	buffer, capacity := m.DenseOccupancyBuf, b.BufferSize(m.DenseOccupancyBuf)
	oldBase := p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{0, 0, 0, 1, 0, 0}, second)
	s1l3ClearIndex(o, 0)
	p2aStep(t, m, b, scene)
	if p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{0, 0, 0, 1, 0, 0}, second) != oldBase {
		t.Fatal("shrink reclaimed surviving auxiliary slot")
	}
	s1l3ClearIndex(o, 1)
	p2aStep(t, m, b, scene)
	if m.DenseOccupancyBuf != buffer || b.BufferSize(buffer) != capacity {
		t.Fatal("empty geometry changed physical auxiliary high water")
	}
	freshA, freshB := p2aBrick("uniform", 3), p2aBrick("uniform", 4)
	schedulePutBrick(o, [6]int{}, freshA)
	schedulePutBrick(o, [6]int{0, 0, 0, 1, 0, 0}, freshB)
	o.XBrickMap.DirtyBricks[[6]int{}] = true
	o.XBrickMap.DirtyBricks[[6]int{0, 0, 0, 1, 0, 0}] = true
	p2aStep(t, m, b, scene)
	if p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{}, freshA) == p2aAssertBrick(t, m, b, o.XBrickMap, [6]int{0, 0, 0, 1, 0, 0}, freshB) || m.DenseOccupancyBuf != buffer {
		t.Fatal("empty-row reuse aliased new bricks or grew high-water capacity")
	}
	s1kReady(t, m, o, true)
}

func TestP2aIdleAuxiliaryPlanningDoesNotWalkCleanResidentSectors(t *testing.T) {
	m, b, scene := p2aFixture(t)
	o := p2aObject(scene, 32)
	for i := 0; i < 32; i++ {
		schedulePutBrick(o, [6]int{i, 0, 0, 0, 0, 0}, p2aBrick("uniform", byte(i+1)))
	}
	p2aStep(t, m, b, scene)
	s1kReady(t, m, o, true)
	capacity := b.BufferSize(m.DenseOccupancyBuf)
	p2aStep(t, m, b, scene)
	if m.VoxelCapacityPlanningSectorVisitsLastUpdate != 0 || b.contentBytes != 0 || b.creates != 0 || b.BufferSize(m.DenseOccupancyBuf) != capacity {
		t.Fatal("idle auxiliary planning walked clean sectors, uploaded content or changed capacity")
	}
}
