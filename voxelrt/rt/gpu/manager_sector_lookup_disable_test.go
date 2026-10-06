package gpu

import (
	"bytes"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestSectorLookupDisableDrainsAtomicOwnershipBeforeStructuralPublication(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	selected := p.o.RenderVoxelMap()
	old := s1mCapture(m, b, p.o)
	before := m.SectorLookupFrameStats().CurrentGeneration
	oldGeneration := s1l14Status(t, m, p).CurrentGeneration
	budget := m.SectorLookupFrameBudget()
	budget.MaxUploadBytes = 0
	m.SetSectorLookupFrameBudget(budget)
	s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	for i := 0; i < 8; i++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
	}
	m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{})
	advanced := false
	for frame := 0; frame < 2048; frame++ {
		s1mFrame(t, m, b, s)
		status := s1l14Status(t, m, p)
		stats := m.SectorLookupFrameStats()
		if stats.CurrentGeneration == before {
			if p.o.RenderVoxelMap() != selected || status.CurrentGeneration != oldGeneration {
				t.Fatal("disabling lookup policy published structural content before its lookup generation")
			}
			params := buildObjectParamsData([]*core.VoxelObject{p.o}, m.Allocations, m.MaterialAllocations)
			if !bytes.Equal(params, old.object) {
				t.Fatal("lookup disable exposed object metadata before matching lookup publication")
			}
			s1l14Value(t, p.o, 80, 16, 16, 0)
		} else {
			advanced = true
		}
		// Existing readable coverage must survive every drain prefix, even after
		// policy is disabled while allocation and submission ownership remains.
		s1l14Value(t, p.o, 16, 16, 16, 1)
		p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
		if status.CurrentGeneration == p.g && !status.Pending && stats.StageBytes == 0 && stats.RetiringEntries == 0 {
			if !advanced {
				t.Fatal("managed structural publication never committed a matching lookup generation")
			}
			s1l14Value(t, p.o, 80, 16, 16, 3)
			p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
			return
		}
	}
	t.Fatal("disabled lookup policy did not finish atomic ownership drain")
}

func TestSectorLookupDisabledPolicyHandoffRetainsCoverageUntilCommittedOrdinaryLookup(t *testing.T) {
	for _, reason := range []string{"managed disable", "source handoff"} {
		t.Run(reason, func(t *testing.T) {
			m, b, s, p := s1mFixture(t, 1)
			s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
			selected := p.o.RenderVoxelMap()
			m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{})
			m.SetVoxelUploadBudget(VoxelUploadBudget{})
			p.o.XBrickMap.SetVoxel(80, 16, 16, 3)
			switch reason {
			case "managed disable":
				m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{})
			case "source handoff":
				p.o.SetManagedGeometryProducer(nil, nil)
			}
			for i := 0; i < 4; i++ {
				sectorLookupCompatibilityFrame(t, m, b, s)
				if p.o.RenderVoxelMap() != selected {
					t.Fatal("handoff cleared managed coverage while ordinary content was unuploaded")
				}
				s1l14Value(t, p.o, 16, 16, 16, 1)
				p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
				if m.ManagedGeometryAdmissionStats().TotalStageBytes == 0 {
					t.Fatal("handoff erased ownership before ordinary lookup readiness")
				}
			}
			m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
			for frame := 0; frame < 2048; frame++ {
				sectorLookupCompatibilityFrame(t, m, b, s)
				s1l14Value(t, p.o, 16, 16, 16, 1)
				p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
				if p.o.RenderVoxelMap() == p.o.XBrickMap && m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
					ready, _, _ := m.RenderVoxelObjectReady(p.o, p.o.XBrickMap, p.o.XBrickMap.Revision)
					stats := m.SectorLookupFrameStats()
					if !ready {
						t.Fatal("ordinary handoff selected content before matching lookup readiness")
					}
					if stats.StageBytes != 0 || stats.RetiringEntries != 0 {
						continue
					}
					s1l14Value(t, p.o, 80, 16, 16, 3)
					p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
					return
				}
			}
			t.Fatal("managed opt-out did not drain lookup and geometry ownership")
		})
	}
}
