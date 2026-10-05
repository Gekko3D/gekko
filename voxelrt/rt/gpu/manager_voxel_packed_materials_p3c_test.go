package gpu

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// A missing public API must produce executable RED, rather than a compile error.
type p3cMaterialPolicy interface{ SetPackedVoxelMaterials(bool) error }

func p3cPolicy(t *testing.T, m *GpuBufferManager) p3cMaterialPolicy {
	t.Helper()
	p, ok := any(m).(p3cMaterialPolicy)
	if !ok {
		t.Fatal("P3c: GpuBufferManager must implement init-only SetPackedVoxelMaterials(bool) error")
	}
	return p
}
func p3cFixture(t *testing.T, normals bool) (*GpuBufferManager, *p2cNative, *core.Scene) {
	t.Helper()
	m, b, s := p2cFixture(t)
	if err := p3cPolicy(t, m).SetPackedVoxelMaterials(true); err != nil {
		t.Fatal(err)
	}
	if err := p3aPolicy(t, m).SetPackedVoxelNormals(normals); err != nil {
		t.Fatal(err)
	}
	return m, b, s
}
func p3cBrick(indices []int, seed uint16) *volume.Brick {
	b := p3aBrick(indices, seed)
	b.Flags = 0
	// Includes every original palette byte, including zero. Authoritative authored
	// occupancy intentionally disagrees with both raw nonzero cells and micro mask.
	for i := 0; i < 512; i++ {
		b.Payload[i%8][(i/8)%8][i/64] = byte(i)
	}
	return b
}

// Independent wire oracle: compress only source bytes and never call a manager
// packer, allocator, normal baker or material encoder.
func p3cPacket(brick *volume.Brick, dense []byte, normals, materials bool) ([]byte, int) {
	packet := bytes.Clone(dense)
	if normals {
		packet = p3aPacket(dense)
	}
	materialStart := len(packet)
	if materials && brick.Flags&(volume.BrickFlagSolid|volume.BrickFlagUniformMaterial) == 0 {
		lanes := make([]byte, 0, 512)
		for i := 0; i < 512; i++ {
			if dense[i/8]&(1<<uint(i%8)) != 0 {
				lanes = append(lanes, brick.VoxelValue(i%8, (i/8)%8, i/64))
			}
		}
		tail := make([]byte, 4*((len(lanes)+3)/4))
		copy(tail, lanes)
		packet = append(packet, tail...)
	}
	return packet, materialStart
}
func p3cAssertPacket(t *testing.T, m *GpuBufferManager, b *p2cNative, sector *volume.Sector, index int, brick *volume.Brick, dense []byte, normals, materials bool) (uint32, []byte) {
	t.Helper()
	row := p2bRecord(t, m, b.p2bNative, sector, index)
	layout := uint32(0)
	if normals {
		layout |= 1
	}
	mixed := brick.Flags&(volume.BrickFlagSolid|volume.BrickFlagUniformMaterial) == 0
	if materials && mixed {
		layout |= 2
	}
	if got := binary.LittleEndian.Uint32(row[28:]); got != layout {
		t.Fatalf("aux layout=%d want%d", got, layout)
	}
	want, materialStart := p3cPacket(brick, dense, normals, materials)
	base := binary.LittleEndian.Uint32(row[24:])
	start := uint64(base) * 4
	data := b.buffers[m.DenseOccupancyBuf]
	if start+uint64(len(want)) > uint64(len(data)) {
		t.Fatal("packet extends beyond bound buffer")
	}
	got := data[start : start+uint64(len(want))]
	if !bytes.Equal(got, want) {
		t.Fatalf("packet at word%d differs: got%x want%x", base, got, want)
	}
	if materials && mixed {
		if got := binary.LittleEndian.Uint32(row[4:]); got != base+uint32(materialStart/4) {
			t.Fatalf("material wordbase=%d want%d", got, base+uint32(materialStart/4))
		}
		if binary.LittleEndian.Uint32(row[16:]) != 0 {
			t.Fatal("packed materials published a payload atlas page")
		}
	}
	return base, bytes.Clone(got)
}
func p3cNoAtlasWrites(t *testing.T, b *p2cNative) {
	t.Helper()
	for _, w := range b.writes {
		if w.label == "payload" {
			t.Fatal("packed materials wrote the atlas")
		}
	}
}

func TestP3cDefaultAtlasAndIndependentPolicyCombinations(t *testing.T) {
	for _, normal := range []bool{false, true} {
		for _, material := range []bool{false, true} {
			t.Run(fmt.Sprintf("normals%t_materials%t", normal, material), func(t *testing.T) {
				m, b, s := p2cFixture(t)
				if normal {
					if err := p3aPolicy(t, m).SetPackedVoxelNormals(true); err != nil {
						t.Fatal(err)
					}
				}
				if material {
					if err := p3cPolicy(t, m).SetPackedVoxelMaterials(true); err != nil {
						t.Fatal(err)
					}
				}
				o := p2aObject(s, 1)
				brick := p3cBrick([]int{0, 31, 32, 255, 256, 511}, 0x7fff)
				p2cPut(o, 63, brick)
				p2cStep(t, m, b, s)
				p3cAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 63, brick, brick.PrecomputedAux, normal, material)
				if material {
					p3cNoAtlasWrites(t, b)
				} else {
					raw := make([]byte, 512)
					for i := range raw {
						raw[i] = byte(i)
					}
					found := false
					for _, w := range b.writes {
						if w.label == "payload" {
							found = true
							if !bytes.Equal(w.data, raw) {
								t.Fatal("legacy atlas bytes changed")
							}
						}
					}
					if !found {
						t.Fatal("default/normal-only policy omitted atlas upload")
					}
				}
				s1kReady(t, m, o, true)
			})
		}
	}
}
func TestP3cPaletteAndNormalWireRankBoundaries(t *testing.T) {
	for _, normals := range []bool{false, true} {
		for _, count := range []int{0, 1, 3, 4, 5, 31, 32, 33, 511, 512} {
			t.Run(fmt.Sprintf("normals%t_n%d", normals, count), func(t *testing.T) {
				m, b, s := p3cFixture(t, normals)
				o := p2aObject(s, 1)
				indices := p3aIndices(count)
				if count <= 5 {
					boundaries := []int{0, 31, 32, 255, 511}
					indices = boundaries[:count]
				}
				brick := p3cBrick(indices, 0x3fff)
				p2cPut(o, 63, brick)
				p2cStep(t, m, b, s)
				_, packet := p3cAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 63, brick, brick.PrecomputedAux, normals, true)
				if got := p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf"); got != uint64(len(packet)) {
					t.Fatalf("packet uploaded%d bytes want%d", got, len(packet))
				}
				p3cNoAtlasWrites(t, b)
			})
		}
	}
	// Full 16-bit normal domain preserves invalid encodings and both flag bits.
	m, b, s := p3cFixture(t, true)
	o := p2aObject(s, 128)
	for key := range o.XBrickMap.Sectors {
		schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p3cBrick(p3aIndices(512), uint16(key[0]*512)))
	}
	p2cStep(t, m, b, s)
	for _, sector := range o.XBrickMap.Sectors {
		brick := sector.GetBrick(0, 0, 0)
		p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, true, true)
	}
	p3cNoAtlasWrites(t, b)
}
func TestP3cInvalidAuthoredAuxFallsBackToDenseObservableSource(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			brick := scheduleBrick("mixed")
			brick.PrecomputedAux = []byte{0xff}
			dm, db, ds := p2cFixture(t)
			dobj := p2aObject(ds, 1)
			p2cPut(dobj, 0, brick)
			p2cStep(t, dm, db, ds)
			row := p2bRecord(t, dm, db.p2bNative, dobj.XBrickMap.Sectors[[3]int{}], 0)
			base := uint64(binary.LittleEndian.Uint32(row[24:])) * 4
			dense := bytes.Clone(db.buffers[dm.DenseOccupancyBuf][base : base+VoxelAuxRecordBytes])
			m, b, s := p3cFixture(t, normals)
			o := p2aObject(s, 1)
			p2cPut(o, 0, brick)
			p2cStep(t, m, b, s)
			p3cAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 0, brick, dense, normals, true)
			p3cNoAtlasWrites(t, b)
		})
	}
}
func TestP3cInitOnlyAndIdempotentAfterRelease(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var m *GpuBufferManager
		p := p3cPolicy(t, m)
		if err := p.SetPackedVoxelMaterials(true); err == nil {
			t.Fatal("nil manager policy accepted")
		}
	})

	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			m, b, s := p2cFixture(t)
			p := p3cPolicy(t, m)
			for _, v := range []bool{false, true, false, initial, initial} {
				if err := p.SetPackedVoxelMaterials(v); err != nil {
					t.Fatal(err)
				}
			}
			o := p2aObject(s, 1)
			p2cPut(o, 0, p2aBrick("solid", 1))
			p2cStep(t, m, b, s)
			for _, release := range []bool{false, true} {
				if release {
					s.Objects = nil
					p2cStep(t, m, b, s)
				}
				if err := p.SetPackedVoxelMaterials(initial); err != nil {
					t.Fatal("same-value policy rejected", err)
				}
				if err := p.SetPackedVoxelMaterials(!initial); err == nil {
					t.Fatal("late policy change accepted")
				}
			}
		})
	}
	for _, ownership := range []string{"sector allocation", "brick allocation", "auxiliary", "staged"} {
		t.Run(ownership, func(t *testing.T) {
			m, b, s := p2cFixture(t)
			p := p3cPolicy(t, m)
			if ownership == "sector allocation" {
				m.SectorAlloc.Alloc()
			} else if ownership == "brick allocation" {
				m.BrickAlloc.Alloc()
			} else if ownership == "auxiliary" {
				slot := m.VoxelAuxAlloc.Alloc()
				m.BrickToAuxSlot[p2aBrick("uniform", 1)] = slot
			} else {
				o := p2aObject(s, 129)
				for key := range o.XBrickMap.Sectors {
					schedulePutBrick(o, [6]int{key[0], key[1], key[2]}, p2aBrick("uniform", 1))
				}
				m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
				p2cStep(t, m, b, s)
				if !m.VoxelGPUWorkStats().Pending {
					t.Fatal("fixture did not establish staging")
				}
			}
			if p.SetPackedVoxelMaterials(true) == nil {
				t.Fatal("policy accepted existing ownership")
			}
			if err := p.SetPackedVoxelMaterials(false); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestP3cUniformAndSolidSkipMaterialLanes(t *testing.T) {
	for _, normals := range []bool{false, true} {
		for _, kind := range []string{"uniform", "solid"} {
			t.Run(fmt.Sprintf("%t_%s", normals, kind), func(t *testing.T) {
				m, b, s := p3cFixture(t, normals)
				o := p2aObject(s, 1)
				brick := p2aBrick(kind, 17)
				p2cPut(o, 0, brick)
				p2cStep(t, m, b, s)
				row := p2bRecord(t, m, b.p2bNative, o.XBrickMap.Sectors[[3]int{}], 0)
				if binary.LittleEndian.Uint32(row[28:])&2 != 0 {
					t.Fatal("nonmixed brick set packed-material bit")
				}
				p3cNoAtlasWrites(t, b)
				_, packet := p3cAssertPacket(t, m, b, o.XBrickMap.Sectors[[3]int{}], 0, brick, brick.PrecomputedAux, normals, true)
				if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != uint64(len(packet)) {
					t.Fatal("nonmixed brick changed normal sidecar or gained material tail")
				}
				if binary.LittleEndian.Uint32(row[0:]) != brick.AtlasOffset || binary.LittleEndian.Uint32(row[4:]) != 0 || binary.LittleEndian.Uint32(row[16:]) != 0 {
					t.Fatal("nonmixed scalar material or unused payload fields changed")
				}
			})
		}
	}
}
func TestP3cExactBudgetCOWAndAtlasExhaustionIrrelevant(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			m.VoxelPayloadBricks = 0
			m.VoxelPayloadPageCount = 0
			o := p2aObject(s, 1)
			brick := p3cBrick([]int{0, 31, 511}, 0x4000)
			p2cPut(o, 0, brick)
			p2cStep(t, m, b, s)
			sector := o.XBrickMap.Sectors[[3]int{}]
			oldBuf := m.DenseOccupancyBuf
			oldBase, oldPacket := p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
			brick.Payload[7][7][7] = 231
			p3aDirty(o, 0)
			packet, _ := p3cPacket(brick, brick.PrecomputedAux, normals, true)
			exact := uint64(32 + len(packet))
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact - 1, MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			if m.VoxelUploadBytes != 0 || len(b.writes) != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
				t.Fatal("insufficient exact unit budget wrote bytes")
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact, MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			next, _ := p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			if next == oldBase || m.VoxelUploadBytes != exact {
				t.Fatal("replacement reused live packet or charged wrong bytes")
			}
			p3aAssertUnchanged(t, b, oldBuf, oldBase, oldPacket)
			p3cNoAtlasWrites(t, b)
			s1kReady(t, m, o, true)
			if !m.RetainVoxelMap(o.XBrickMap) {
				t.Fatal("map not retainable")
			}
			if got := m.RetainedVoxelMapStats().Bytes; got != uint64(256+32+32+len(packet)) {
				t.Fatalf("retention=%d omits or duplicates material tail", got)
			}
		})
	}
}

func TestP3cPacketReuseRequiresStampedCompletion(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			brick := p3cBrick([]int{511}, 0x8000)
			packet, _ := p3cPacket(brick, brick.PrecomputedAux, normals, true)
			capacity := uint64(3 * len(packet))
			b.buffers[m.DenseOccupancyBuf] = make([]byte, capacity)
			b.maxAuxBytes = capacity
			o := p2aObject(s, 1)
			p2cPut(o, 0, brick)
			p2cStep(t, m, b, s)
			sector := o.XBrickMap.Sectors[[3]int{}]
			firstBase, firstPacket := p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			oldBuffer := m.DenseOccupancyBuf
			for i := 0; i < 2; i++ {
				brick.Payload[7][7][7] = byte(100 + i)
				p3aDirty(o, 0)
				p2cStep(t, m, b, s)
				p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			}
			row := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
			brick.Payload[7][7][7] = 209
			p3aDirty(o, 0)
			for i := 0; i < RetiredBufferFrameDelay+2; i++ {
				m.AdvanceRetiredBuffers()
				p2cStep(t, m, b, s)
			}
			if m.VoxelUploadBytes != 0 || !bytes.Equal(row, p2bRecord(t, m, b.p2bNative, sector, 0)) {
				t.Fatal("unstamped material packet reused")
			}
			p3aAssertUnchanged(t, b, oldBuffer, firstBase, firstPacket)
			s1kReady(t, m, o, false)
			m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(381))
			p2cStep(t, m, b, s)
			s1kReady(t, m, o, false)
			b.completed[380] = true
			m.AdvanceRetiredBuffers()
			p2cStep(t, m, b, s)
			s1kReady(t, m, o, false)
			p3aAssertUnchanged(t, b, oldBuffer, firstBase, firstPacket)
			b.completed[381] = true
			m.AdvanceRetiredBuffers()
			p2cStep(t, m, b, s)
			p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			s1kReady(t, m, o, true)
			if b.BufferSize(m.DenseOccupancyBuf) != capacity {
				t.Fatal("completed packets were not reused")
			}
		})
	}
}
func TestP3cSharedSectorVersusSharedBrickOwnership(t *testing.T) {
	for _, normals := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("%t_%t", normals, shared), func(t *testing.T) {
				m, b, s := p3cFixture(t, normals)
				a, other := p2aObject(s, 1), p2aObject(s, 1)
				a.XBrickMap.ID = 420
				other.XBrickMap.ID = 421
				brick := p3cBrick([]int{0, 511}, 0x4000)
				p2cPut(a, 0, brick)
				if shared {
					other.XBrickMap.Sectors[[3]int{}] = a.XBrickMap.Sectors[[3]int{}]
				} else {
					p2cPut(other, 0, brick)
				}
				p2cStep(t, m, b, s)
				sa, so := a.XBrickMap.Sectors[[3]int{}], other.XBrickMap.Sectors[[3]int{}]
				ab, ap := p3cAssertPacket(t, m, b, sa, 0, brick, brick.PrecomputedAux, normals, true)
				ob, op := p3cAssertPacket(t, m, b, so, 0, brick, brick.PrecomputedAux, normals, true)
				if shared != (ab == ob) {
					t.Fatal("packet ownership does not follow physical sector location")
				}
				units := 2
				if shared {
					units = 1
				}
				if p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != uint64(units*len(ap)) {
					t.Fatal("physical packet deduplication incorrect")
				}
				old := brick.Copy()
				oldBuffer := m.DenseOccupancyBuf
				brick.Payload[7][7][7] = 71
				p3aDirty(a, 0)
				p2cStep(t, m, b, s)
				p3cAssertPacket(t, m, b, sa, 0, brick, brick.PrecomputedAux, normals, true)
				if shared {
					p3cAssertPacket(t, m, b, so, 0, brick, brick.PrecomputedAux, normals, true)
				} else {
					p3cAssertPacket(t, m, b, so, 0, old, old.PrecomputedAux, normals, true)
				}
				p3aAssertUnchanged(t, b, oldBuffer, ab, ap)
				p3aAssertUnchanged(t, b, oldBuffer, ob, op)
			})
		}
	}
}
func TestP3cWholePacketCaptureAndFlagTransition(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			o := p2aObject(s, 1)
			first := p3cBrick([]int{0}, 0x4000)
			later := p3cBrick([]int{31, 511}, 0x8123)
			p2cPut(o, 0, first)
			p2cPut(o, 63, later)
			captured := later.Copy()
			revision := o.XBrickMap.Revision
			b.afterAuxWrite = func() {
				later.Flags = volume.BrickFlagUniformMaterial
				later.AtlasOffset = 29
				later.OccupancyMask64 = 0x123456789abcdef0
				later.Payload[7][7][7] = 255
				copy(later.PrecomputedAux, p3aBrick(p3aIndices(512), 0xffff).PrecomputedAux)
			}
			pa, _ := p3cPacket(first, first.PrecomputedAux, normals, true)
			pb, _ := p3cPacket(captured, captured.PrecomputedAux, normals, true)
			exact := uint64(64 + 32 + 2*32 + len(pa) + len(pb))
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact, MaxSectors: 1, MaxBricks: 2})
			p2cStep(t, m, b, s)
			sector := o.XBrickMap.Sectors[[3]int{}]
			p3cAssertPacket(t, m, b, sector, 63, captured, captured.PrecomputedAux, normals, true)
			row := p2bRecord(t, m, b.p2bNative, sector, 63)
			mask := uint64(binary.LittleEndian.Uint32(row[8:])) | uint64(binary.LittleEndian.Uint32(row[12:]))<<32
			if binary.LittleEndian.Uint32(row[20:]) != captured.Flags || mask != captured.OccupancyMask64 || m.VoxelUploadBytes != exact {
				t.Fatal("same-pointer callback changed captured source or byte charge")
			}
			if o.XBrickMap.Revision != revision || !o.XBrickMap.DirtySectors[[3]int{}] {
				t.Fatal("raw callback fixture lost pending source change")
			}
			s1kReady(t, m, o, false)
			p3cNoAtlasWrites(t, b)
			m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
			p2cStep(t, m, b, s)
			p3cAssertPacket(t, m, b, sector, 63, later, later.PrecomputedAux, normals, true)
			row = p2bRecord(t, m, b.p2bNative, sector, 63)
			if binary.LittleEndian.Uint32(row[0:]) != 29 || binary.LittleEndian.Uint32(row[20:]) != volume.BrickFlagUniformMaterial {
				t.Fatal("retry did not remove material tail and publish uniform scalar")
			}
			s1kReady(t, m, o, true)
			// Restore mixed membership, then remove the first local brick. Old packets
			// stay readable while the survivor receives its own fresh packet.
			later.Flags = 0
			p3aDirty(o, 63)
			p2cStep(t, m, b, s)
			oldBuffer := m.DenseOccupancyBuf
			oldBase, oldPacket := p3cAssertPacket(t, m, b, sector, 63, later, later.PrecomputedAux, normals, true)
			s1l3ClearIndex(o, 0)
			o.XBrickMap.DirtySectors[[3]int{}] = true
			p2cStep(t, m, b, s)
			next, packet := p3cAssertPacket(t, m, b, sector, 63, later, later.PrecomputedAux, normals, true)
			_, mask = p2cHeader(t, m, b, sector)
			if mask != uint64(1)<<63 || next == oldBase {
				t.Fatal("membership publication failed to COW survivor")
			}
			p3aAssertUnchanged(t, b, oldBuffer, oldBase, oldPacket)
			if !m.RetainVoxelMap(o.XBrickMap) {
				t.Fatal("survivor not retainable")
			}
			if got := m.RetainedVoxelMapStats().Bytes; got != uint64(256+32+32+len(packet)) {
				t.Fatal("retention charged removed or retired material packets", got)
			}
		})
	}
}
func TestP3cDeferredAndDeniedClaimsDoNotOverlap(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			// Isolate auxiliary refusal from material-table growth for three owners.
			b.buffers[m.MaterialBuf] = make([]byte, 3*materialBlockCapacity*64)
			largeBrick := p3cBrick(p3aIndices(36), 0x8000)
			smallBrick := p3cBrick([]int{511}, 0x1234)
			lp, _ := p3cPacket(largeBrick, largeBrick.PrecomputedAux, normals, true)
			sp, _ := p3cPacket(smallBrick, smallBrick.PrecomputedAux, normals, true)
			capacity := uint64(len(lp) + len(sp))
			b.buffers[m.DenseOccupancyBuf] = make([]byte, capacity)
			b.maxAuxBytes = capacity
			denied := p2aObject(s, 1)
			denied.XBrickMap.ID = 430
			p2cPut(denied, 0, p3cBrick(p3aIndices(512), 0))
			p2cPut(denied, 63, p3cBrick(p3aIndices(512), 0))
			large := p2aObject(s, 1)
			large.XBrickMap.ID = 431
			large.VoxelGPUAdmissionOptional = true
			large.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
			p2cPut(large, 0, largeBrick)
			small := p2aObject(s, 1)
			small.XBrickMap.ID = 432
			small.VoxelGPUAdmissionOptional = true
			small.VoxelUploadPriority = core.VoxelUploadPriorityFallback
			p2cPut(small, 63, smallBrick)
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: uint64(3*64 + 32 + 32 + len(sp)), MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			s1kReady(t, m, denied, false)
			s1kReady(t, m, large, false)
			s1kReady(t, m, small, true)
			sb, sPacket := p3cAssertPacket(t, m, b, small.XBrickMap.Sectors[[3]int{}], 63, smallBrick, smallBrick.PrecomputedAux, normals, true)
			oldBuffer := m.DenseOccupancyBuf
			if !m.RetainVoxelMap(large.XBrickMap) {
				t.Fatal("deferred map not retainable")
			}
			if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
				t.Fatal("deferred claim charged a real packet", got)
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: uint64(32 + 32 + len(lp)), MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			lb, _ := p3cAssertPacket(t, m, b, large.XBrickMap.Sectors[[3]int{}], 0, largeBrick, largeBrick.PrecomputedAux, normals, true)
			if uint64(lb)*4 < uint64(sb)*4+uint64(len(sp)) && uint64(sb)*4 < uint64(lb)*4+uint64(len(lp)) {
				t.Fatal("reordered deferred claim overlaps assigned material packet")
			}
			p3aAssertUnchanged(t, b, oldBuffer, sb, sPacket)
			if b.BufferSize(m.DenseOccupancyBuf) != capacity {
				t.Fatal("denied candidate leaked auxiliary claim")
			}
			s1kReady(t, m, large, true)
		})
	}
}
func TestP3cGrowthMirrorsCompleteMaterialPacketAndChargesBytes(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			if !normals {
				// Preserve room for resident COW while the growth copy is paused.
				b.buffers[m.DenseOccupancyBuf] = make([]byte, 4096)
			}
			resident := p2aObject(s, 1)
			resident.XBrickMap.ID = 440
			initial := p3cBrick([]int{0, 511}, 0x4000)
			p2cPut(resident, 0, initial)
			p2cStep(t, m, b, s)
			arrival := p2aObject(s, 129)
			arrival.XBrickMap.ID = 441
			for key := range arrival.XBrickMap.Sectors {
				schedulePutBrick(arrival, [6]int{key[0], key[1], key[2]}, p3cBrick([]int{511}, 0x8123))
			}
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
			p2cStep(t, m, b, s)
			if !m.VoxelGPUWorkStats().Pending {
				t.Fatal("fixture did not establish pending migration")
			}
			s.Objects = []*core.VoxelObject{resident}
			next := p3cBrick([]int{31, 32, 511}, 0x8001)
			next.Payload[7][7][7] = 219
			p2cPut(resident, 0, next)
			p3aDirty(resident, 0)
			packet, _ := p3cPacket(next, next.PrecomputedAux, normals, true)
			exact := uint64(2 * (32 + len(packet)))
			sector := resident.XBrickMap.Sectors[[3]int{}]
			oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact - 1, MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			if m.VoxelUploadBytes != 0 || !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
				t.Fatal("mirror bypassed exact byte cap")
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: exact, MaxSectors: 1, MaxBricks: 1})
			p2cStep(t, m, b, s)
			p3cAssertPacket(t, m, b, sector, 0, next, next.PrecomputedAux, normals, true)
			if m.VoxelUploadBytes != exact || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != uint64(2*len(packet)) || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 64 {
				t.Fatal("migration omitted or mischarged packet material tail")
			}
			p3cNoAtlasWrites(t, b)
			s1kReady(t, m, resident, true)
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 0, MaxSectors: 4096, MaxBricks: 1 << 20})
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 1 << 30})
			p2cStep(t, m, b, s)
			if m.VoxelGPUWorkStats().Pending || m.VoxelUploadBytes != 0 {
				t.Fatal("migration failed with content paused")
			}
			p3cAssertPacket(t, m, b, sector, 0, next, next.PrecomputedAux, normals, true)
		})
	}
}

func TestP3cFinalOwnerRemovalFencesMaterialPacket(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			brick := p3cBrick([]int{511}, 0x4000)
			packet, _ := p3cPacket(brick, brick.PrecomputedAux, normals, true)
			capacity := uint64(len(packet))
			b.buffers[m.DenseOccupancyBuf] = make([]byte, capacity)
			b.maxAuxBytes = capacity
			first, other := p2aObject(s, 1), p2aObject(s, 1)
			first.XBrickMap.ID = 510
			other.XBrickMap.ID = 511
			p2cPut(first, 0, brick)
			sector := first.XBrickMap.Sectors[[3]int{}]
			other.XBrickMap.Sectors[[3]int{}] = sector
			p2cStep(t, m, b, s)
			base, oldPacket := p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			oldBuffer := m.DenseOccupancyBuf
			s.Objects = []*core.VoxelObject{other}
			p2cStep(t, m, b, s)
			s1kReady(t, m, other, true)
			stable, _ := p3cAssertPacket(t, m, b, sector, 0, brick, brick.PrecomputedAux, normals, true)
			if stable != base {
				t.Fatal("nonfinal shared owner removal moved surviving packet")
			}
			// A submission after nonfinal removal must not release live shared bytes.
			m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(512))
			b.completed[512] = true
			m.AdvanceRetiredBuffers()
			s.Objects = nil
			p2cStep(t, m, b, s)
			arrival := p2aObject(s, 1)
			arrival.XBrickMap.ID = 513
			fresh := p3cBrick([]int{511}, 0x8000)
			fresh.Payload[7][7][7] = 179
			p2cPut(arrival, 0, fresh)
			for i := 0; i < RetiredBufferFrameDelay+2; i++ {
				m.AdvanceRetiredBuffers()
				p2cStep(t, m, b, s)
			}
			s1kReady(t, m, arrival, false)
			p3aAssertUnchanged(t, b, oldBuffer, base, oldPacket)
			p2bAssertLookup(t, m, b.p2bNative, arrival, [3]int{}, false)
			m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(514))
			p2cStep(t, m, b, s)
			s1kReady(t, m, arrival, false)
			b.completed[513] = true
			m.AdvanceRetiredBuffers()
			p2cStep(t, m, b, s)
			s1kReady(t, m, arrival, false)
			p3aAssertUnchanged(t, b, oldBuffer, base, oldPacket)
			b.completed[514] = true
			m.AdvanceRetiredBuffers()
			p2cStep(t, m, b, s)
			next, _ := p3cAssertPacket(t, m, b, arrival.XBrickMap.Sectors[[3]int{}], 0, fresh, fresh.PrecomputedAux, normals, true)
			if next != base || b.BufferSize(m.DenseOccupancyBuf) != capacity {
				t.Fatal("completed final-owner packet was not reused at fixed capacity")
			}
			s1kReady(t, m, arrival, true)
		})
	}
}
func TestP3cLateRawGrowthProtectsOtherMaterialClaim(t *testing.T) {
	for _, normals := range []bool{false, true} {
		t.Run(fmt.Sprint(normals), func(t *testing.T) {
			m, b, s := p3cFixture(t, normals)
			fittingBrick := p3cBrick(p3aIndices(36), 0x4000)
			grownBrick := p3cBrick([]int{511}, 0x8000)
			fitPacket, _ := p3cPacket(fittingBrick, fittingBrick.PrecomputedAux, normals, true)
			smallPacket, _ := p3cPacket(grownBrick, grownBrick.PrecomputedAux, normals, true)
			capacity := uint64(len(fitPacket) + len(smallPacket))
			b.buffers[m.DenseOccupancyBuf] = make([]byte, capacity)
			b.maxAuxBytes = capacity
			fitting := p2aObject(s, 1)
			fitting.XBrickMap.ID = 520
			fitting.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
			p2cPut(fitting, 0, fittingBrick)
			grown := p2aObject(s, 1)
			grown.XBrickMap.ID = 521
			grown.VoxelGPUAdmissionOptional = true
			grown.VoxelUploadPriority = core.VoxelUploadPriorityFallback
			p2cPut(grown, 63, grownBrick)
			revision := grown.XBrickMap.Revision
			b.afterMaterialWrite = func() {
				copy(grownBrick.PrecomputedAux, p3aBrick(p3aIndices(512), 0xabcd).PrecomputedAux)
				grownBrick.Payload[7][7][7] = 197
			}
			p2cStep(t, m, b, s)
			if grown.XBrickMap.Revision != revision {
				t.Fatal("raw growth fixture changed source revision")
			}
			s1kReady(t, m, fitting, true)
			s1kReady(t, m, grown, false)
			sector := fitting.XBrickMap.Sectors[[3]int{}]
			base, packet := p3cAssertPacket(t, m, b, sector, 0, fittingBrick, fittingBrick.PrecomputedAux, normals, true)
			oldBuffer := m.DenseOccupancyBuf
			oldRow := bytes.Clone(p2bRecord(t, m, b.p2bNative, sector, 0))
			if m.VoxelSectorsUploaded != 1 || m.VoxelBricksUploaded != 1 || p2bWrittenBytes(b.p2bNative, "DenseOccupancyBuf") != uint64(len(fitPacket)) || p2bWrittenBytes(b.p2bNative, "BrickTableBuf") != 32 {
				t.Fatal("late growth invaded pending claim or wrote partial material packet")
			}
			p2bAssertLookup(t, m, b.p2bNative, grown, [3]int{}, false)
			p3cNoAtlasWrites(t, b)
			if !m.RetainVoxelMap(grown.XBrickMap) {
				t.Fatal("late deferred map not retainable")
			}
			if got := m.RetainedVoxelMapStats().Bytes; got != 256+32 {
				t.Fatal("late denied growth assigned real material packet", got)
			}
			b.maxAuxBytes = 8192
			p2cStep(t, m, b, s)
			p3cAssertPacket(t, m, b, grown.XBrickMap.Sectors[[3]int{}], 63, grownBrick, grownBrick.PrecomputedAux, normals, true)
			p3aAssertUnchanged(t, b, oldBuffer, base, packet)
			if !bytes.Equal(oldRow, p2bRecord(t, m, b.p2bNative, sector, 0)) {
				t.Fatal("late retry republished unaffected owner")
			}
			p3cAssertPacket(t, m, b, sector, 0, fittingBrick, fittingBrick.PrecomputedAux, normals, true)
			s1kReady(t, m, grown, true)
		})
	}
}
