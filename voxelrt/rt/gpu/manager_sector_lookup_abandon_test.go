package gpu

import (
	"errors"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

type sectorLookupAbandonNative struct {
	*p2cNative
	writesBeforeFailure int
	created             map[*wgpu.Buffer]bool
	attempted           map[*wgpu.Buffer]bool
	released            map[*wgpu.Buffer]bool
}

func (b *sectorLookupAbandonNative) CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error) {
	buffer, err := b.p2cNative.CreateBuffer(label, size, uniform)
	if err == nil && s1mLookupLabel(label) {
		b.created[buffer] = true
	}
	return buffer, err
}
func (b *sectorLookupAbandonNative) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if s1mLookupLabel(b.labels[buffer]) {
		b.attempted[buffer] = true
		if b.writesBeforeFailure == 0 {
			return errors.New("permanent lookup candidate write failure")
		}
		b.writesBeforeFailure--
	}
	return b.p2cNative.WriteBuffer(buffer, offset, data)
}
func (b *sectorLookupAbandonNative) ReleaseBuffer(buffer *wgpu.Buffer) {
	b.released[buffer] = true
	b.p2cNative.ReleaseBuffer(buffer)
}
func sectorLookupAbandonFrame(t *testing.T, m *GpuBufferManager, b *p2cNative, native voxelNativeBackend, s *core.Scene) {
	t.Helper()
	m.PrepareManagedGeometryFrame(s)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	b.reset()
	b.writes = nil
	m.updateVoxelData(s, native)
	m.updateSectorGrid(s)
}

func TestSectorLookupRemovalAbandonsPermanentlyFailingPrivateCapture(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old := s1mCapture(m, b, p.o)
	before := m.SectorLookupFrameStats().CurrentGeneration
	q := s1l14ProducerFor(s, [3]int{4, 0, 0}, [3]int{5, 0, 0}, [3]int{6, 0, 0})
	failing := &sectorLookupAbandonNative{p2cNative: b, writesBeforeFailure: 2, created: map[*wgpu.Buffer]bool{}, attempted: map[*wgpu.Buffer]bool{}, released: map[*wgpu.Buffer]bool{}}
	partial := false
	for frame := 0; frame < 2048; frame++ {
		sectorLookupAbandonFrame(t, m, b, failing, s)
		s1mUnchanged(t, m, b, p.o, old)
		if failing.writesBeforeFailure == 0 && m.SectorLookupFrameStats().StageBytes > 0 {
			partial = true
			break
		}
	}
	if !partial {
		t.Fatal("fixture did not own a partially uploaded private lookup capture")
	}
	// Exercise the permanent error before explicit removal invalidates this
	// private capture. Its hidden target must never acquire render selection.
	for i := 0; i < 4; i++ {
		sectorLookupAbandonFrame(t, m, b, failing, s)
		s1mUnchanged(t, m, b, p.o, old)
	}
	if q.o.RenderVoxelMap() != nil {
		t.Fatal("failed lookup capture exposed its private target")
	}
	s.Objects = []*core.VoxelObject{p.o}
	for frame := 0; frame < 256; frame++ {
		sectorLookupAbandonFrame(t, m, b, b, s)
		s1mUnchanged(t, m, b, p.o, old)
		stats := m.SectorLookupFrameStats()
		if stats.StageBytes == 0 && stats.RetiringEntries == 0 && !stats.Pending {
			break
		}
		if frame == 255 {
			t.Fatal("removed target retained obsolete private lookup CPU ownership after permanent write failure")
		}
	}
	if m.SectorLookupFrameStats().CurrentGeneration != before {
		t.Fatal("abandonment published a failed or obsolete lookup capture")
	}
	// Used native buffers belong to the captured backend and require an exact
	// submission completion. Never-written candidate buffers can release now.
	for buffer := range failing.created {
		if failing.attempted[buffer] {
			if failing.released[buffer] {
				t.Fatal("abandoned queued lookup buffer released before its submission fence")
			}
		} else if !failing.released[buffer] {
			t.Fatal("abandoned unused native lookup buffer retained ownership")
		}
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(1001))
	for i := 0; i < RetiredBufferFrameDelay+4; i++ {
		m.AdvanceRetiredBuffers()
	}
	for buffer := range failing.attempted {
		if failing.released[buffer] {
			t.Fatal("frame aging released queued abandoned lookup backing")
		}
	}
	b.completed[1000] = true
	m.AdvanceRetiredBuffers()
	for buffer := range failing.attempted {
		if failing.released[buffer] {
			t.Fatal("unrelated submission released queued abandoned lookup backing")
		}
	}
	b.completed[1001] = true
	for i := 0; i < 8; i++ {
		m.AdvanceRetiredBuffers()
	}
	for buffer := range failing.created {
		if !failing.released[buffer] {
			t.Fatal("abandoned native backing did not retire through its captured backend after exact completion")
		}
	}
	s1mUnchanged(t, m, b, p.o, old)
	s1l14Value(t, p.o, 16, 16, 16, 1)
}
