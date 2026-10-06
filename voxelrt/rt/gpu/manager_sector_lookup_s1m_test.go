package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
	"strings"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Exercise production geometry service and lookup encoding/publication. The
// existing P2c fixture substitutes only physical native resources and fences.
func s1mFrame(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene) {
	t.Helper()
	s1mFrameWithFence(t, m, b, s, true)
}

func s1mFrameWithFence(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene, completed bool) {
	t.Helper()
	m.PrepareManagedGeometryFrame(s)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	p2cStep(t, m, b, s)
	if completed {
		m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(777))
		b.completed[777] = true
		m.AdvanceRetiredBuffers()
	}
	budget, stats := m.SectorLookupFrameBudget(), m.SectorLookupFrameStats()
	if budget.Enabled && (stats.AttemptedEntries > budget.MaxEntries || stats.UploadedBytes > budget.MaxUploadBytes) {
		t.Fatalf("global lookup allowance exceeded: stats %+v budget %+v", stats, budget)
	}
	var physical uint64
	for _, w := range b.writes {
		if s1mLookupLabel(w.label) {
			physical += uint64(len(w.data))
			if budget.Enabled && uint64(len(w.data)) > budget.MaxUploadBytes {
				t.Fatalf("indivisible lookup upload escaped byte cap: %s %d", w.label, len(w.data))
			}
		}
	}
	if budget.Enabled && physical != stats.UploadedBytes {
		t.Fatalf("lookup charged %d bytes, physically wrote %d", stats.UploadedBytes, physical)
	}
}

func s1mLookupLabel(label string) bool {
	return strings.Contains(label, "SectorGrid") || strings.Contains(label, "DirectSectorLookup")
}

func s1mFixture(t *testing.T, count int) (*GpuBufferManager, *p2cNative, *core.Scene, *s1l14Producer) {
	t.Helper()
	coords := make([][3]int, count)
	for i := range coords {
		coords[i] = [3]int{i - count/2, 0, 0}
	}
	m, b, s, p := s1l14Fixture(t, coords...)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 256})
	m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{Enabled: true, MaxEntries: 32, MaxUploadBytes: 256, MaxStageBytes: 128 << 20})
	return m, b, s, p
}

func s1mDrain(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene, ready func() bool) {
	t.Helper()
	for frame := 0; frame < 16384; frame++ {
		s1mFrame(t, m, b, s)
		if ready() {
			return
		}
	}
	t.Fatal("fitting finite lookup generation did not drain")
}

func s1mPublished(m *GpuBufferManager, p *s1l14Producer) bool {
	status, ok := m.ManagedGeometryGPUStatus(p.o)
	return ok && status.CurrentGeneration == p.g && !status.Pending && p.o.RenderVoxelMap() != nil && !m.SectorLookupFrameStats().Pending
}

type s1mImage struct {
	buffers [3]*wgpu.Buffer
	data    [3][]byte
	object  []byte
}

func s1mCapture(m *GpuBufferManager, b *p2cNative, o *core.VoxelObject) s1mImage {
	r := s1mImage{buffers: [3]*wgpu.Buffer{m.SectorGridBuf, m.SectorGridParamsBuf, m.DirectSectorLookupBuf}}
	for i, buffer := range r.buffers {
		r.data[i] = bytes.Clone(b.buffers[buffer])
	}
	if o != nil {
		r.object = buildObjectParamsData([]*core.VoxelObject{o}, m.Allocations, m.MaterialAllocations)
	}
	return r
}

func s1mUnchanged(t *testing.T, m *GpuBufferManager, b *p2cNative, o *core.VoxelObject, old s1mImage) {
	t.Helper()
	got := s1mCapture(m, b, o)
	if got.buffers != old.buffers || !bytes.Equal(got.object, old.object) {
		t.Fatal("incomplete/refused lookup replaced current buffers or visible object metadata")
	}
	for i := range old.data {
		if !bytes.Equal(got.data[i], old.data[i]) {
			t.Fatalf("incomplete/refused lookup overwrote current buffer %d", i)
		}
	}
}

func TestS1mPolicyDefaultOffAndNilSafe(t *testing.T) {
	if got := DefaultSectorLookupFrameBudget(); got != (SectorLookupFrameBudget{Enabled: true, MaxEntries: 1024, MaxUploadBytes: 64 << 10, MaxStageBytes: 128 << 20}) {
		t.Fatalf("default policy %+v", got)
	}
	var nilManager *GpuBufferManager
	nilManager.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	if nilManager.SectorLookupFrameBudget() != (SectorLookupFrameBudget{}) || nilManager.SectorLookupFrameStats() != (SectorLookupFrameStats{}) {
		t.Fatal("nil manager policy/stats nonzero")
	}
	m, b, s := p2cFixture(t)
	if m.SectorLookupFrameBudget().Enabled {
		t.Fatal("zero-value manager enabled lookup service")
	}
	o := p2aObject(s, 1)
	p2cPut(o, 0, p2aBrick("uniform", 1))
	p2cStep(t, m, b, s)
	s1kReady(t, m, o, true)
	p2bAssertLookup(t, m, b.p2bNative, o, [3]int{}, true)
}

func TestS1mInitialCaptureInitAndUploadAreGlobalAndPrefixHidden(t *testing.T) {
	m, b, s, p := s1mFixture(t, 48)
	q := s1l14ProducerFor(s, [3]int{80, 0, 0}, [3]int{82, 0, 0})
	initial := s1mCapture(m, b, nil)
	s1mFrame(t, m, b, s)
	if !m.SectorLookupFrameStats().Pending || p.o.RenderVoxelMap() != nil || q.o.RenderVoxelMap() != nil {
		t.Fatal("first bounded lookup service exposed incomplete geometry or reported idle")
	}
	s1mUnchanged(t, m, b, nil, initial)
	var entries, uploaded uint64
	s1mDrain(t, m, b, s, func() bool {
		stats := m.SectorLookupFrameStats()
		entries += uint64(stats.AttemptedEntries)
		uploaded += stats.UploadedBytes
		return s1mPublished(m, p) && s1mPublished(m, q)
	})
	// Hash empty-cell initialization alone is at least 1024 entries. A private
	// capture/insertion/direct path cannot claim one entry for an entire table.
	if entries+32 < 1024 || uploaded+256 < 1024*32+16+50*4 {
		t.Fatalf("bulk lookup work was uncharged: entries=%d bytes=%d", entries, uploaded)
	}
	for key := range p.o.RenderVoxelMap().Sectors {
		p2bAssertLookup(t, m, b.p2bNative, p.o, key, true)
	}
	p2bAssertLookup(t, m, b.p2bNative, q.o, [3]int{80, 0, 0}, true)
	for i := 0; i < 4; i++ {
		s1mFrame(t, m, b, s)
		if stats := m.SectorLookupFrameStats(); stats.AttemptedEntries != 0 || stats.UploadedBytes != 0 || stats.Pending {
			t.Fatalf("idle lookup serviced work %+v", stats)
		}
	}
}

func TestS1mZeroEntryPauseAndZeroUploadPrepareWithoutPublication(t *testing.T) {
	for _, pause := range []string{"entries", "upload"} {
		t.Run(pause, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 3)
			budget := m.SectorLookupFrameBudget()
			if pause == "entries" {
				budget.MaxEntries = 0
			} else {
				budget.MaxUploadBytes = 0
			}
			m.SetSectorLookupFrameBudget(budget)
			old := s1mCapture(m, b, nil)
			var work uint64
			for i := 0; i < 96; i++ {
				s1mFrame(t, m, b, s)
				stats := m.SectorLookupFrameStats()
				work += uint64(stats.AttemptedEntries)
				if stats.UploadedBytes != 0 || (pause == "entries" && stats.AttemptedEntries != 0) || p.o.RenderVoxelMap() != nil {
					t.Fatal("zero allowance published or serviced paused resource")
				}
				s1mUnchanged(t, m, b, nil, old)
				if pause == "entries" {
					for buffer, label := range b.labels {
						if s1mLookupLabel(label) && buffer != old.buffers[0] && buffer != old.buffers[1] && buffer != old.buffers[2] {
							t.Fatal("zero entries created a lookup candidate resource")
						}
					}
				}
			}
			if pause == "upload" && work == 0 {
				t.Fatal("zero upload unnecessarily paused CPU preparation")
			}
			m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
		})
	}
}

func TestS1mSameSizeStageRefusalPreservesCoverageAndRetries(t *testing.T) {
	m, b, s, p := s1mFixture(t, 2)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old, generation := s1mCapture(m, b, p.o), m.SectorLookupFrameStats().CurrentGeneration
	budget := m.SectorLookupFrameBudget()
	budget.MaxStageBytes = 1
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	for i := 0; i < 96; i++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
		for buffer, label := range b.labels {
			if s1mLookupLabel(label) && buffer != old.buffers[0] && buffer != old.buffers[1] && buffer != old.buffers[2] {
				t.Fatal("refused stage cap created native lookup backing")
			}
		}
		if stats := m.SectorLookupFrameStats(); stats.CurrentGeneration != generation || !stats.Pending || stats.StageBytes > 1 {
			t.Fatalf("refused lookup changed generation/charge %+v", stats)
		}
	}
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
	if m.SectorLookupFrameStats().CurrentGeneration <= generation {
		t.Fatal("retry did not publish successor")
	}
}

func TestS1mManagedReplacementLookupPausedKeepsCurrentAndReadinessPending(t *testing.T) {
	m, b, s, p := s1mFixture(t, 2)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	current := p.o.RenderVoxelMap()
	old := s1mCapture(m, b, p.o)
	budget := m.SectorLookupFrameBudget()
	budget.MaxUploadBytes = 0
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 2})
	for i := 0; i < 96; i++ {
		s1mFrame(t, m, b, s)
		status := s1l14Status(t, m, p)
		if p.o.RenderVoxelMap() != current || !status.Pending || status.StageReady {
			t.Fatal("content-upload completion certified replacement without matching lookup membership")
		}
		s1mUnchanged(t, m, b, p.o, old)
	}
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	s1l14Value(t, p.o, 80, 16, 16, 2)
}

func TestS1mCurrentCoordinateReplacementWaitsForLookupCommit(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	current := p.o.RenderVoxelMap()
	old := s1mCapture(m, b, p.o)
	before := s1l14Status(t, m, p).CurrentGeneration
	budget := m.SectorLookupFrameBudget()
	budget.MaxEntries = 0
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
	for i := 0; i < 12; i++ {
		s1mFrame(t, m, b, s)
		status := s1l14Status(t, m, p)
		if status.CurrentGeneration != before || !status.Pending {
			t.Fatal("current content ack outran committed lookup edge")
		}
		s1mUnchanged(t, m, b, p.o, old)
	}
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	if p.o.RenderVoxelMap() != current {
		t.Fatal("content-only lookup publication replaced display selection")
	}
	s1l14Value(t, p.o, 16, 16, 16, 2)
}

func TestS1mFiniteGenerationProgressUnderContinuousCoalescedEdits(t *testing.T) {
	m, b, s, p := s1mFixture(t, 3)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	before := m.SectorLookupFrameStats().CurrentGeneration
	s1l14Edit(m, p, volume.VoxelWrite{X: 112, Y: 16, Z: 16, Value: 2})
	// Keep modifying a different, already committed coordinate while the finite
	// lookup capture/init/upload services its accepted membership. A restart on
	// every notification cannot complete the 32KiB hash in this allowance.
	advanced := false
	for i := 0; i < 1024; i++ {
		s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: uint8(1 + i%2)}, volume.VoxelWrite{X: 320, Y: 16, Z: 16, Value: uint8(i % 2)})
		s1mFrame(t, m, b, s)
		if m.SectorLookupFrameStats().CurrentGeneration > before {
			advanced = true
			break
		}
	}
	if !advanced {
		t.Fatal("continuous content notifications starved accepted finite lookup generation")
	}
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{3, 0, 0}, true)
}

func TestS1mEqualCountReplacementAndMapIDKeepCommittedObjectRows(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old := s1mCapture(m, b, p.o)
	oldID := binary.LittleEndian.Uint32(old.object)
	raw := p.o.XBrickMap.Copy()
	p.owner = volume.NewManagedXBrickMap(raw)
	p.o.XBrickMap = raw.Copy()
	p.g++
	p.install()
	s1mFrame(t, m, b, s)
	s1mUnchanged(t, m, b, p.o, old)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	row := buildObjectParamsData([]*core.VoxelObject{p.o}, m.Allocations, m.MaterialAllocations)
	if binary.LittleEndian.Uint32(row) == oldID {
		t.Fatal("committed map identity did not advance with lookup")
	}
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16}, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, false)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
}

func TestS1mCancelRemoveAndDisableDrainLookupOwnership(t *testing.T) {
	for _, operation := range []string{"cancel", "remove", "disable"} {
		t.Run(operation, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 3)
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
			s1l14Edit(m, p, volume.VoxelWrite{X: 144, Y: 16, Z: 16, Value: 2})
			s1mFrame(t, m, b, s)
			switch operation {
			case "cancel":
				m.CancelManagedGeometryInputs(p.o)
			case "remove":
				s.Objects = nil
			case "disable":
				budget := m.SectorLookupFrameBudget()
				budget.MaxEntries = 0
				m.SetSectorLookupFrameBudget(budget)
				m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{})
			}
			// Stamp each frame's last-use submission and complete that exact fence.
			s1mDrain(t, m, b, s, func() bool {
				m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(901))
				b.completed[901] = true
				m.AdvanceRetiredBuffers()
				stats := m.SectorLookupFrameStats()
				return !stats.Pending && stats.StageBytes == 0 && stats.RetiringEntries == 0 && (operation != "remove" || len(m.Allocations) == 0)
			})
		})
	}
}

// Lookup errors use the same native boundary as content, and deliberately occur
// after some candidate bytes have already been queued successfully.
type s1mFailNative struct {
	*p2cNative
	writesBeforeFailure int
	fail                bool
}

func (b *s1mFailNative) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if b.fail && s1mLookupLabel(b.labels[buffer]) {
		if b.writesBeforeFailure == 0 {
			return errors.New("injected lookup candidate write failure")
		}
		b.writesBeforeFailure--
	}
	return b.p2cNative.WriteBuffer(buffer, offset, data)
}

func TestS1mCreateAndPartialUploadFailuresRetainCurrentAndCharges(t *testing.T) {
	for _, kind := range []string{"create", "write"} {
		t.Run(kind, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 1)
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
			old := s1mCapture(m, b, p.o)
			f := &s1mFailNative{p2cNative: b, fail: kind == "write", writesBeforeFailure: 2}
			b.failCreate = kind == "create"
			s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 2})
			for i := 0; i < 160; i++ {
				m.PrepareManagedGeometryFrame(s)
				b.reset()
				b.writes = nil
				m.updateVoxelData(s, f)
				m.updateSectorGrid(s)
				s1mUnchanged(t, m, b, p.o, old)
				stats := m.SectorLookupFrameStats()
				if !stats.Pending || stats.UploadedBytes > m.SectorLookupFrameBudget().MaxUploadBytes {
					t.Fatal("native failure certified candidate or exceeded cap")
				}
				if kind == "write" && i > 130 && stats.StageBytes == 0 {
					t.Fatal("partial write failure erased owned candidate charge")
				}
			}
			b.failCreate, f.fail = false, false
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
			p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
		})
	}
}

func TestS1mCommittedHashDirectParamsAndObjectMetadataAgree(t *testing.T) {
	m, b, s, p := s1mFixture(t, 64)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	image := s1mCapture(m, b, p.o)
	gridSize := binary.LittleEndian.Uint32(image.data[1])
	if gridSize < 1024 || gridSize&(gridSize-1) != 0 || binary.LittleEndian.Uint32(image.data[1][4:]) != gridSize-1 || uint64(gridSize)*32 > uint64(len(image.data[0])) {
		t.Fatal("hash parameters do not describe committed physical table")
	}
	var rows uint32
	for i := uint32(0); i < gridSize; i++ {
		row := image.data[0][i*32 : (i+1)*32]
		if binary.LittleEndian.Uint32(row[20:]) != math.MaxUint32 {
			rows++
			if binary.LittleEndian.Uint32(row[16:]) != binary.LittleEndian.Uint32(image.object) {
				t.Fatal("object ID and hash membership differ")
			}
		}
	}
	if rows != 64 || binary.LittleEndian.Uint32(image.object[24:]) != rows || binary.LittleEndian.Uint32(image.object[108:]) != LookupModeDirect {
		t.Fatal("object count/direct descriptor mismatches committed lookup")
	}
	for key := range p.o.RenderVoxelMap().Sectors {
		p2bAssertLookup(t, m, b.p2bNative, p.o, key, true)
	}
}

func TestS1mLoweredStageCapKeepsPartiallyWrittenOwnership(t *testing.T) {
	m, b, s, p := s1mFixture(t, 2)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old := s1mCapture(m, b, p.o)
	s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 2})
	s1mDrain(t, m, b, s, func() bool {
		stats := m.SectorLookupFrameStats()
		return stats.Pending && stats.StageBytes != 0 && stats.UploadedBytes != 0
	})
	charged := m.SectorLookupFrameStats().StageBytes
	budget := m.SectorLookupFrameBudget()
	budget.MaxEntries, budget.MaxUploadBytes, budget.MaxStageBytes = 0, 0, 1
	m.SetSectorLookupFrameBudget(budget)
	for i := 0; i < 8; i++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
		if got := m.SectorLookupFrameStats(); got.StageBytes != charged || got.AttemptedEntries != 0 || !got.Pending {
			t.Fatalf("lowered policy erased/serviced existing candidate charge: %+v / %d", got, charged)
		}
	}
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
}

func TestS1mSubWordUploadAllowanceStallsAndNativeCreatesShareWorkPolicy(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	budget := m.SectorLookupFrameBudget()
	budget.MaxUploadBytes = 3
	m.SetSectorLookupFrameBudget(budget)
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 64, MaxCopyBytes: 1 << 20})
	for i := 0; i < 96; i++ {
		s1mFrame(t, m, b, s)
		stats, native := m.SectorLookupFrameStats(), m.VoxelGPUWorkStats()
		if stats.UploadedBytes != 0 || p.o.RenderVoxelMap() != nil {
			t.Fatal("subword byte allowance queued unaligned writes or publication")
		}
		if native.Creates > 1 || native.OversizedCreates > 1 || (native.CreatedBytes > 64 && native.OversizedCreates != 1) {
			t.Fatalf("lookup bypassed shared indivisible-create policy %+v", native)
		}
	}
	budget.MaxUploadBytes = 256
	m.SetSectorLookupFrameBudget(budget)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
}

func TestS1mLargeDirectInvalidInitializationAndHashCollisionProbesAreCharged(t *testing.T) {
	for _, kind := range []string{"direct", "collisions"} {
		t.Run(kind, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 0)
			raw := volume.NewXBrickMap()
			if kind == "direct" {
				for i := 0; i < 257; i++ {
					raw.SetVoxel(i*32+16, 16, 16, 1)
				}
				raw.SetVoxel(4095*32+16, 16, 16, 1)
			} else {
				// All sixteen keys hash to one bucket in the 1024-cell table.
				for i := 0; i < 16; i++ {
					raw.SetVoxel(i*1024*32+16, 16, 16, 1)
				}
			}
			p.owner, p.o.XBrickMap = volume.NewManagedXBrickMap(raw), raw.Copy()
			p.install()
			var entries uint64
			s1mDrain(t, m, b, s, func() bool {
				entries += uint64(m.SectorLookupFrameStats().AttemptedEntries)
				return s1mPublished(m, p)
			})
			alloc := m.Allocations[p.o.RenderVoxelMap()]
			if kind == "direct" {
				if alloc.DirectLookup.LookupMode != LookupModeDirect || alloc.DirectLookup.Extent[0] != 4096 || entries < 4096+4096+258 {
					t.Fatalf("large hash/direct initialization or capture escaped entry cap: metadata %+v entries %d", alloc.DirectLookup, entries)
				}
				p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{4095, 0, 0}, true)
				p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{3000, 0, 0}, false)
			} else if alloc.DirectLookup.LookupMode != LookupModeHash || entries < 1024+16+136 {
				t.Fatalf("collision probes uncharged: entries %d lookup %+v", entries, alloc.DirectLookup)
			}
		})
	}
}

func TestS1mLookupPinsOldSectorAndBrickEdgesUntilExactFence(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1l14Edit(m, p, volume.VoxelWrite{X: 17, Y: 16, Z: 16, Value: 2})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	sector := p.o.RenderVoxelMap().Sectors[[3]int{}]
	sectorBuffer, brickBuffer := m.SectorTableBuf, m.BrickTableBuf
	header := bytes.Clone(p2bHeader(t, m, b.p2bNative, sector))
	base, _ := p2cHeader(t, m, b, sector)
	slot := m.SectorToInfo[sector].SlotIndex
	brick := bytes.Clone(b.buffers[brickBuffer][base*32 : (base+1)*32])
	oldBrick := sector.GetBrick(2, 2, 2)
	payloadSlot, payloadOwned := m.BrickToSlot[oldBrick]
	auxSlot, auxOwned := m.BrickToAuxSlot[oldBrick]
	if !payloadOwned || !auxOwned {
		t.Fatal("mixed fixture missing independently owned payload/aux slots")
	}
	auxBuffer := m.DenseOccupancyBuf
	auxBase := uint64(binary.LittleEndian.Uint32(brick[24:])) * 4
	auxBytes := bytes.Clone(b.buffers[auxBuffer][auxBase : auxBase+VoxelAuxRecordBytes])
	payloadBytes := s1mPayloadSlot(b, payloadSlot)
	budget := m.SectorLookupFrameBudget()
	budget.MaxUploadBytes = 0
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
	assertOld := func() {
		t.Helper()
		if data := b.buffers[sectorBuffer]; uint64(len(data)) < uint64(slot+1)*32 || !bytes.Equal(data[slot*32:(slot+1)*32], header) {
			t.Fatal("lookup pin lost old readable sector slot")
		}
		if data := b.buffers[brickBuffer]; uint64(len(data)) < uint64(base+1)*32 || !bytes.Equal(data[base*32:(base+1)*32], brick) {
			t.Fatal("lookup sector pin omitted its readable brick edge")
		}
		if got, ok := m.BrickToSlot[oldBrick]; !ok || got != payloadSlot {
			t.Fatal("lookup edge released old mixed payload slot before fence")
		}
		if got, ok := m.BrickToAuxSlot[oldBrick]; !ok || got != auxSlot {
			t.Fatal("lookup edge released old occupancy/normal slot before fence")
		}
		if !bytes.Equal(s1mPayloadSlot(b, payloadSlot), payloadBytes) {
			t.Fatal("old mixed material payload overwritten through lookup pin")
		}
		if data := b.buffers[auxBuffer]; uint64(len(data)) < auxBase+VoxelAuxRecordBytes || !bytes.Equal(data[auxBase:auxBase+VoxelAuxRecordBytes], auxBytes) {
			t.Fatal("old auxiliary bytes overwritten through lookup pin")
		}

	}
	for i := 0; i < 64; i++ {
		s1mFrameWithFence(t, m, b, s, false)
		assertOld()
	}
	budget.MaxUploadBytes = 256
	m.SetSectorLookupFrameBudget(budget)
	before := m.SectorLookupFrameStats().CurrentGeneration
	for i := 0; i < 4096; i++ {
		s1mFrameWithFence(t, m, b, s, false)
		assertOld()
		if m.SectorLookupFrameStats().CurrentGeneration > before {
			break
		}
		if i == 4095 {
			t.Fatal("lookup candidate did not commit without retiring old generation")
		}
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(1001))
	for i := 0; i < RetiredBufferFrameDelay+8; i++ {
		m.AdvanceRetiredBuffers()
		s1mFrameWithFence(t, m, b, s, false)
		assertOld()
	}
	if m.SectorLookupFrameStats().RetiringEntries == 0 {
		t.Fatal("frame aging released lookup sector/brick pins without completed last-use submission")
	}
	// Completing a different submission cannot release the exact old last use.
	b.completed[1000] = true
	m.AdvanceRetiredBuffers()
	s1mFrameWithFence(t, m, b, s, false)
	assertOld()
	if m.SectorLookupFrameStats().RetiringEntries == 0 {
		t.Fatal("unrelated completed submission released old lookup")
	}
	b.completed[1001] = true
	s1mDrain(t, m, b, s, func() bool {
		stats := m.SectorLookupFrameStats()
		return s1mPublished(m, p) && stats.RetiringEntries == 0 && stats.StageBytes == 0
	})
}

func TestS1mCollisionProbeDifferentialCannotHideBehindCaptureCharges(t *testing.T) {
	service := func(collide bool) uint64 {
		m, b, s, p := s1mFixture(t, 0)
		raw := volume.NewXBrickMap()
		for i := 0; i < 16; i++ {
			x := i * 1024
			if !collide {
				x += i
			}
			raw.SetVoxel(x*32+16, 16, 16, 1)
		}
		p.owner, p.o.XBrickMap = volume.NewManagedXBrickMap(raw), raw.Copy()
		p.install()
		var entries uint64
		s1mDrain(t, m, b, s, func() bool {
			entries += uint64(m.SectorLookupFrameStats().AttemptedEntries)
			return s1mPublished(m, p)
		})
		if m.Allocations[p.o.RenderVoxelMap()].DirectLookup.LookupMode != LookupModeHash {
			t.Fatal("differential fixture entered direct mode")
		}
		return entries
	}
	clustered, dispersed := service(true), service(false)
	// Identical map count, 1024-cell hash and hash-only objects. Sixteen
	// clustered insertions need 1+...+16 probes; dispersed insertions need 16.
	if clustered < dispersed+120 {
		t.Fatalf("probe accounting hidden by common snapshot charges: clustered=%d dispersed=%d", clustered, dispersed)
	}
}

func TestS1mUnknownPublicAllocationHeadersKeepSynchronousCompatibility(t *testing.T) {
	m, b, s := p2cFixture(t)
	o := p2aObject(s, 1)
	p2cPut(o, 0, p2aBrick("uniform", 1))
	p2cStep(t, m, b, s)
	known := m.Allocations[o.XBrickMap]
	// Public callers may construct allocation headers. They have no immutable
	// manager-owned inventory proof, even if the payload pointers match.
	m.Allocations[o.XBrickMap] = &ObjectGpuAllocation{Sectors: known.Sectors, Bricks: known.Bricks, DirectLookup: known.DirectLookup}
	o.XBrickMap.ID += 100
	m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{Enabled: true})
	m.updateSectorGrid(s)
	row := buildObjectParamsData([]*core.VoxelObject{o}, m.Allocations, m.MaterialAllocations)
	if binary.LittleEndian.Uint32(row) != o.XBrickMap.ID {
		t.Fatal("unknown header compatibility left stale object identity")
	}
	p2bAssertLookup(t, m, b.p2bNative, o, [3]int{}, true)
}

func s1mPayloadSlot(b *p2cNative, payloadSlot PayloadSlot) []byte {
	slot := payloadSlot.Slot
	origin := [3]uint32{slot % 2 * 8, slot / 2 % 2 * 8, slot / 4 * 8}
	var data []byte
	for z := uint32(0); z < 8; z++ {
		for y := uint32(0); y < 8; y++ {
			start := origin[0] + (origin[1]+y)*16 + (origin[2]+z)*256
			data = append(data, b.payload[start:start+8]...)
		}
	}
	return data
}

func TestS1mSameSizeLookupCandidatePreflightsOldPlusNewPhysicalBytes(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	// Current and successor content fit existing independent tables. The only
	// necessary extra physical resource is the same-sized lookup generation.
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 8*VoxelAuxRecordBytes)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old := s1mCapture(m, b, p.o)
	cap := b.liveBytes() + uint64(len(b.payload))
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: cap})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
	contentQueued := false
	for i := 0; i < 96; i++ {
		s1mFrame(t, m, b, s)
		for _, w := range b.writes {
			if w.label == "SectorTableBuf" || w.label == "BrickTableBuf" {
				contentQueued = true
			}
		}
		s1mUnchanged(t, m, b, p.o, old)
		if b.liveBytes()+uint64(len(b.payload)) > cap {
			t.Fatal("same-size lookup staging bypassed old+new physical admission")
		}
	}
	if !contentQueued {
		t.Fatal("physical cap refused all content before testing lookup candidate")
	}
	if !m.SectorLookupFrameStats().Pending {
		t.Fatal("same-size physical refusal lost retry")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
}

func TestS1mGrowthOverlapMirrorsCurrentContentAndPreservesLookup(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	current := p.o.RenderVoxelMap()
	s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 2})
	s1mDrain(t, m, b, s, func() bool {
		stats := m.SectorLookupFrameStats()
		return stats.Pending && stats.StageBytes != 0 && stats.UploadedBytes != 0
	})
	// Keep a physical growth generation alive across service frames, and make a
	// newer committed-coordinate write while lookup publication is outstanding.
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 20, MaxCopyBytes: 256})
	q := s1l14ProducerFor(s)
	raw := volume.NewXBrickMap()
	for i := 0; i < 48; i++ {
		raw.SetVoxel((i+100)*32+16, 16, 16, 1)
	}
	q.owner, q.o.XBrickMap = volume.NewManagedXBrickMap(raw), raw.Copy()
	q.install()
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) && s1mPublished(m, q) && !m.VoxelGPUWorkStats().Pending })
	if current == nil {
		t.Fatal("fixture never published current coverage")
	}
	s1l14Value(t, p.o, 16, 16, 16, 3)
	sector := p.o.RenderVoxelMap().Sectors[[3]int{}]
	brick := sector.GetBrick(2, 2, 2)
	base, mask := p2cHeader(t, m, b, sector)
	if mask&(uint64(1)<<42) == 0 {
		t.Fatal("newest committed brick missing from ranked mask")
	}
	rank := bits.OnesCount64(mask & ((uint64(1) << 42) - 1))
	offset := uint64(base+uint32(rank)) * BrickRecordSize
	row := b.buffers[m.BrickTableBuf][offset : offset+BrickRecordSize]
	if binary.LittleEndian.Uint32(row) != brick.AtlasOffset || binary.LittleEndian.Uint32(row[20:]) != brick.Flags {
		t.Fatal("growth lost newest committed material/flags")
	}
	aux := uint64(binary.LittleEndian.Uint32(row[24:])) * 4
	if aux+VoxelAuxRecordBytes > uint64(len(b.buffers[m.DenseOccupancyBuf])) {
		t.Fatal("newest ranked auxiliary span exceeds physical capacity")
	}
	if slot, ok := m.BrickToSlot[brick]; ok {
		if got := s1mPayloadSlot(b, slot)[0]; got != 3 {
			t.Fatalf("growth lost newest material payload: %d", got)
		}
	}
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
	for key := range q.o.RenderVoxelMap().Sectors {
		p2bAssertLookup(t, m, b.p2bNative, q.o, key, true)
	}
}
