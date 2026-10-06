package gpu

import (
	"encoding/binary"
	"testing"
)

func TestSectorLookupSameGenerationPhysicalRootReplacementRequiresMatchingLookup(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	selected := p.o.RenderVoxelMap()
	oldSector := selected.Sectors[[3]int{}]
	oldSlot := m.SectorToInfo[oldSector].SlotIndex
	before := m.SectorLookupFrameStats().CurrentGeneration
	// A notification with unknown history requires a fresh physical copy even
	// when the qualified numeric generation has not changed.
	m.NotifyManagedGeometryGPUContent(p.o, p.g, p.g, nil)
	firstCommit := false
	for frame := 0; frame < 2048; frame++ {
		s1mFrame(t, m, b, s)
		if m.SectorLookupFrameStats().CurrentGeneration > before {
			firstCommit = true
			break
		}
	}
	if !firstCommit {
		t.Fatal("same-generation repair never committed its captured replacement lookup")
	}
	budget := m.SectorLookupFrameBudget()
	budget.MaxEntries = 0
	m.SetSectorLookupFrameBudget(budget)
	transferred := false
	for i := 0; i < 64; i++ {
		s1mFrame(t, m, b, s)
		if selected.Sectors[[3]int{}] != oldSector {
			transferred = true
			break
		}
	}
	if !transferred {
		t.Fatal("same-generation repair did not transfer a fresh physical coordinate")
	}
	status := s1l14Status(t, m, p)
	if status.CurrentGeneration != p.g {
		t.Fatal("fixture changed numeric generation instead of only the physical root")
	}
	if !status.Pending {
		t.Fatal("matching numeric generation certified an uncommitted physical root")
	}
	ready, _, _ := m.RenderVoxelObjectReady(p.o, selected, selected.Revision)
	if ready {
		t.Fatal("render readiness ignored a changed physical root while lookup service was paused")
	}
	s1l14Value(t, p.o, 16, 16, 16, 1)
	meta := m.Allocations[selected].DirectLookup
	if meta.LookupMode != LookupModeDirect || binary.LittleEndian.Uint32(b.buffers[m.DirectSectorLookupBuf][uint64(meta.TableBase)*4:]) != oldSlot {
		t.Fatal("paused same-generation repair lost its previously readable physical lookup edge")
	}
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	ready, _, _ = m.RenderVoxelObjectReady(p.o, selected, selected.Revision)
	if !ready {
		t.Fatal("matching committed physical lookup root did not restore readiness")
	}
}
