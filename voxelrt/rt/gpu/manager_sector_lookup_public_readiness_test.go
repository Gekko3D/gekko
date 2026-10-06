package gpu

import (
	"bytes"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestSectorLookupUnknownPublicMembershipRequiresCommittedCompatibilityRoot(t *testing.T) {
	m, b, s := p2cFixture(t)
	m.SetSectorLookupFrameBudget(DefaultSectorLookupFrameBudget())
	first := p2aObject(s, 1)
	p2cPut(first, 0, p2aBrick("uniform", 1))
	other := p2aObject(s, 1)
	p2cPut(other, 0, p2aBrick("uniform", 1))
	other.XBrickMap.SetVoxel(48, 16, 16, 1)
	for frame := 0; frame < 256; frame++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
		stats := m.SectorLookupFrameStats()
		ready, _, _ := m.RenderVoxelObjectReady(first, first.XBrickMap, first.XBrickMap.Revision)
		otherReady, _, _ := m.RenderVoxelObjectReady(other, other.XBrickMap, other.XBrickMap.Revision)
		if ready && otherReady && !stats.Pending && stats.StageBytes == 0 {
			break
		}
		if frame == 255 {
			t.Fatal("raw fixture did not publish both initial allocation inventories")
		}
	}
	old := s1mCapture(m, b, first)
	known := m.Allocations[first.XBrickMap]
	replacement := &ObjectGpuAllocation{Sectors: map[[3]int]*volume.Sector{}, Bricks: map[[3]int]*[64]*volume.Brick{}, DirectLookup: known.DirectLookup}
	for c, sector := range known.Sectors {
		replacement.Sectors[c] = sector
		replacement.Bricks[c] = known.Bricks[c]
	}
	incoming := other.XBrickMap.Sectors[[3]int{1, 0, 0}]
	replacement.Sectors[[3]int{1, 0, 0}] = incoming
	replacement.Bricks[[3]int{1, 0, 0}] = m.Allocations[other.XBrickMap].Bricks[[3]int{1, 0, 0}]
	first.XBrickMap.Sectors[[3]int{1, 0, 0}] = incoming
	first.XBrickMap.Revision++
	first.XBrickMap.ClearDirty()
	m.Allocations[first.XBrickMap] = replacement
	before := m.SectorLookupFrameStats().CurrentGeneration
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true})
	for frame := 0; frame < 8; frame++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
		stats := m.SectorLookupFrameStats()
		if !stats.Compatibility || stats.CurrentGeneration != before {
			t.Fatal("paused native creation unexpectedly committed unknown public membership")
		}
		ready, _, _ := m.RenderVoxelObjectReady(first, first.XBrickMap, first.XBrickMap.Revision)
		if ready {
			t.Fatal("unknown public allocation certified changed membership before matching compatibility lookup commit")
		}
		image := s1mCapture(m, b, first)
		for i := range old.buffers {
			if image.buffers[i] != old.buffers[i] || !bytes.Equal(image.data[i], old.data[i]) {
				t.Fatal("refused compatibility native creation changed previously committed lookup bytes")
			}
		}
		p2bAssertLookup(t, m, b.p2bNative, first, [3]int{}, true)
	}
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{})
	for frame := 0; frame < 256; frame++ {
		sectorLookupCompatibilityFrame(t, m, b, s)
		ready, _, _ := m.RenderVoxelObjectReady(first, first.XBrickMap, first.XBrickMap.Revision)
		stats := m.SectorLookupFrameStats()
		if ready && stats.CurrentGeneration > before && stats.StageBytes == 0 && stats.RetiringEntries == 0 {
			p2bAssertLookup(t, m, b.p2bNative, first, [3]int{}, true)
			p2bAssertLookup(t, m, b.p2bNative, first, [3]int{1, 0, 0}, true)
			return
		}
	}
	t.Fatal("unknown public membership did not become ready after matching compatibility commit")
}
