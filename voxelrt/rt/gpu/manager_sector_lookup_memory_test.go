package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestSectorLookupStageCapChargesRetainedPayloadBacking(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	// Pack GPU materials so this test isolates retained CPU inputs without
	// exhausting the small fake native fixture's primary atlas.
	if err := m.SetPackedVoxelMaterials(true); err != nil {
		t.Fatal(err)
	}
	// One small inventory plus the minimum hash fits this cap; the same
	// coordinate with substantial retained auxiliary backing must not fit it.
	budget := m.SectorLookupFrameBudget()
	budget.MaxStageBytes = 96 << 10
	m.SetSectorLookupFrameBudget(budget)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	old := s1mCapture(m, b, p.o)
	before := m.SectorLookupFrameStats().CurrentGeneration
	raw := volume.NewXBrickMap()
	sector := volume.NewSector(0, 0, 0)
	for i := 0; i < 64; i++ {
		sector.GetOrCreateBrick(i%4, i/4%4, i/16)
		sector.PackedBricks[sector.GetPackedIndex(i)] = p2aBrick("mixed", 3)
	}
	raw.Sectors[[3]int{}] = sector
	p.owner = volume.NewManagedXBrickMap(raw)
	if view, qualified := p.owner.CaptureGeometry(); !qualified || view.Len() != 1 {
		t.Fatal("large mixed-brick input must remain sealed and qualified")
	}
	p.o.XBrickMap = raw.Copy()
	p.g++
	p.install()
	for frame := 0; frame < 128; frame++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
		stats := m.SectorLookupFrameStats()
		if stats.CurrentGeneration != before || !stats.Pending || stats.StageBytes > budget.MaxStageBytes {
			t.Fatalf("retained-input refusal changed publication or exceeded stage charge: %+v", stats)
		}
	}
	// Raising only lookup retention policy permits the exact same finite input
	// to commit. Neither producer identity nor payload content changes on retry.
	budget.MaxStageBytes = 128 << 20
	m.SetSectorLookupFrameBudget(budget)
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) && m.SectorLookupFrameStats().StageBytes == 0 })
	if m.SectorLookupFrameStats().CurrentGeneration <= before {
		t.Fatal("fitting retained-input cap failed to retry and publish")
	}
	s1l14Value(t, p.o, 16, 16, 16, 1)
	s1l14Value(t, p.o, 17, 16, 16, 2)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
}
