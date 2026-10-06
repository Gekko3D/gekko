package gpu

import (
	"encoding/binary"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Compatibility relaxes lookup CPU/upload policy only. Exercise the same
// physical backend, managed qualification and submission fences as bounded mode.
func sectorLookupCompatibilityFrame(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene) {
	t.Helper()
	m.PrepareManagedGeometryFrame(s)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	p2cStep(t, m, b, s)
	m.MarkRetiredBuffersSubmitted(new(wgpu.Queue), wgpu.SubmissionIndex(777))
	b.completed[777] = true
	m.AdvanceRetiredBuffers()
	stats, budget := m.SectorLookupFrameStats(), m.SectorLookupFrameBudget()
	if !stats.Compatibility && budget.Enabled && (stats.AttemptedEntries > budget.MaxEntries || stats.UploadedBytes > budget.MaxUploadBytes) {
		t.Fatalf("bounded work exceeded lookup policy: %+v", stats)
	}
	var written uint64
	for _, w := range b.writes {
		if s1mLookupLabel(w.label) {
			written += uint64(len(w.data))
		}
	}
	if written != stats.UploadedBytes {
		t.Fatalf("compatibility lost lookup write accounting: physical %d stats %+v", written, stats)
	}
}

func TestSectorLookupMixedRawTransitionPreservesUnreadyManagedCoverage(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	current := p.o.RenderVoxelMap()
	status := s1l14Status(t, m, p)
	before := m.SectorLookupFrameStats().CurrentGeneration
	budget := m.SectorLookupFrameBudget()
	budget.MaxEntries, budget.MaxUploadBytes = 0, 0
	m.SetSectorLookupFrameBudget(budget)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	for i := 0; i < 4; i++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
	}
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true})
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	raw := p2aObject(s, 1)
	p2cPut(raw, 0, p2aBrick("uniform", 4))
	compatibility := false
	for i := 0; i < 256; i++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
		compatibility = compatibility || m.SectorLookupFrameStats().Compatibility
		got := s1l14Status(t, m, p)
		if got.CurrentGeneration != status.CurrentGeneration || p.o.RenderVoxelMap() != current {
			t.Fatal("raw compatibility transition certified unready managed content")
		}
		s1l14Value(t, p.o, 16, 16, 16, 1)
		p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
		params := buildObjectParamsData([]*core.VoxelObject{raw}, m.Allocations, m.MaterialAllocations)
		if len(params) >= 28 && binary.LittleEndian.Uint32(params[24:]) == 1 && m.SectorLookupFrameStats().CurrentGeneration > before {
			break
		}
		if i == 255 {
			t.Fatal("raw transition did not complete through atomic lookup publication")
		}
	}
	if !compatibility {
		t.Fatal("raw transition did not explicitly report compatibility service")
	}
	for i := 0; i < 4; i++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
		if m.SectorLookupFrameStats().Compatibility {
			t.Fatal("stable trusted raw inventory remained in compatibility mode")
		}
	}
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 256})
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool {
		return s1mPublished(m, p) && m.SectorLookupFrameStats().StageBytes == 0 && m.SectorLookupFrameStats().RetiringEntries == 0
	})
	s1l14Value(t, p.o, 16, 16, 16, 3)
}

func TestSectorLookupUnknownHeaderTransitionRetiresBoundedPins(t *testing.T) {
	for _, header := range []string{"public", "shallow copy", "foreign manager"} {
		t.Run(header, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 1)
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
			current := p.o.RenderVoxelMap()
			before := m.SectorLookupFrameStats().CurrentGeneration
			known := m.Allocations[current]
			switch header {
			case "public":
				m.Allocations[current] = &ObjectGpuAllocation{Sectors: known.Sectors, Bricks: known.Bricks, DirectLookup: known.DirectLookup}
			case "shallow copy":
				copied := *known
				m.Allocations[current] = &copied
			case "foreign manager":
				other, otherBackend, otherScene := p2cFixture(t)
				raw := p2aObject(otherScene, 1)
				p2cPut(raw, 0, p2aBrick("uniform", 1))
				p2cStep(t, other, otherBackend, otherScene)
				copied := *other.Allocations[raw.XBrickMap]
				copied.Sectors, copied.Bricks, copied.DirectLookup = known.Sectors, known.Bricks, known.DirectLookup
				m.Allocations[current] = &copied
			}
			budget := m.SectorLookupFrameBudget()
			budget.MaxEntries, budget.MaxUploadBytes = 0, 0
			m.SetSectorLookupFrameBudget(budget)
			compatibility := false
			for i := 0; i < 256; i++ {
				sectorLookupCompatibilityFrame(t, m, b, s)
				compatibility = compatibility || m.SectorLookupFrameStats().Compatibility
				s1l14Value(t, p.o, 16, 16, 16, 1)
				p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
				stats := m.SectorLookupFrameStats()
				if stats.CurrentGeneration > before && stats.StageBytes == 0 && stats.RetiringEntries == 0 {
					break
				}
				if i == 255 {
					t.Fatal("unknown public header did not replace and retire prior bounded inventory")
				}
			}
			if !compatibility {
				t.Fatal("unknown header transition lacked explicit compatibility statistics")
			}
			params := buildObjectParamsData([]*core.VoxelObject{p.o}, m.Allocations, m.MaterialAllocations)
			if binary.LittleEndian.Uint32(params) != current.ID {
				t.Fatal("unknown header compatibility published stale object identity")
			}
			if m.SectorLookupFrameStats().RetiringEntries != 0 || m.SectorLookupFrameStats().StageBytes != 0 {
				t.Fatal("unknown header compatibility leaked bounded lookup staging or retiring pins")
			}
		})
	}
}
