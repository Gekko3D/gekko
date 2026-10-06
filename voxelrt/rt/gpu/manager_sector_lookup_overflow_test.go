package gpu

import (
	"encoding/binary"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"math"
	"testing"
)

func TestSectorLookupProbeOverflowRetainsCurrentAndRecoversOnReplacement(t *testing.T) {
	m, b, s, p := s1mFixture(t, 1)
	m.SetSectorLookupFrameBudget(SectorLookupFrameBudget{Enabled: true, MaxEntries: 4096, MaxUploadBytes: 1 << 20, MaxStageBytes: 128 << 20})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) })
	budget := m.SectorLookupFrameBudget()
	paused := budget
	paused.MaxEntries, paused.MaxUploadBytes = 0, 0
	m.SetSectorLookupFrameBudget(paused)
	// 129 entries select 2048 hash cells. All keys land in one bucket, so
	// the final key exceeds the shader's 128-probe traversal contract.
	writes := make([]volume.VoxelWrite, 0, 128)
	for i := 1; i < 129; i++ {
		writes = append(writes, volume.VoxelWrite{X: i*2048*32 + 16, Y: 16, Z: 16, Value: 1})
	}
	s1l14Edit(m, p, writes...)
	ready := false
	for frame := 0; frame < 512; frame++ {
		s1mFrame(t, m, b, s)
		o := m.managedGPUOwners[p.o]
		for _, target := range []*managedGPUTarget{o.stage, o.candidate} {
			if target != nil && len(target.mapRef.Sectors) == 129 && m.managedReady(o, target) {
				ready = true
			}
		}
		if ready {
			break
		}
	}
	if !ready {
		t.Fatal("overflow fixture did not finish content uploads before lookup capture")
	}
	old := s1mCapture(m, b, p.o)
	before := m.SectorLookupFrameStats().CurrentGeneration
	m.SetSectorLookupFrameBudget(budget)
	failed := false
	for frame := 0; frame < 512; frame++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
		if stage := m.sectorLookupStage; stage != nil && stage.failed {
			if stage.gridSize != 2048 {
				t.Fatalf("overflow fixture hash cells=%d, want2048", stage.gridSize)
			}
			failed = true
			break
		}
	}
	if !failed {
		t.Fatal("129 clustered entries did not refuse an unreadable lookup generation")
	}
	if !m.SectorLookupFrameStats().Pending || m.SectorLookupFrameStats().CurrentGeneration != before {
		t.Fatal("overflow refusal lost pending retry or changed committed generation")
	}
	for i := 0; i < 4; i++ {
		s1mFrame(t, m, b, s)
		s1mUnchanged(t, m, b, p.o, old)
	}
	// Removing the final colliding key leaves 128 keys, whose final hit is
	// exactly the last legal probe. Replace the failed finite generation.
	s1l14Edit(m, p, volume.VoxelWrite{X: 128*2048*32 + 16, Y: 16, Z: 16})
	s1mDrain(t, m, b, s, func() bool { return s1mPublished(m, p) && m.sectorLookupRetired == nil })
	if m.SectorLookupFrameStats().CurrentGeneration <= before {
		t.Fatal("valid replacement did not publish after overflow refusal")
	}
	if m.Allocations[p.o.RenderVoxelMap()].DirectLookup.LookupMode != LookupModeHash {
		t.Fatal("clustered replacement unexpectedly selected direct lookup")
	}
	shaderFind := func(key [3]int) (uint32, bool) {
		params := b.buffers[m.SectorGridParamsBuf]
		mask := binary.LittleEndian.Uint32(params[4:])
		id := p.o.RenderVoxelMap().ID
		hash := uint32(key[0])*73856093 ^ uint32(key[1])*19349663 ^ uint32(key[2])*83492791 ^ id*99999989
		table := b.buffers[m.SectorGridBuf]
		for probe := uint32(0); probe < 128; probe++ {
			row := table[((hash+probe)&mask)*32:]
			slot := binary.LittleEndian.Uint32(row[20:])
			if slot == math.MaxUint32 {
				return 0, false
			}
			if binary.LittleEndian.Uint32(row[16:]) == id && int32(binary.LittleEndian.Uint32(row)) == int32(key[0]) && int32(binary.LittleEndian.Uint32(row[4:])) == int32(key[1]) && int32(binary.LittleEndian.Uint32(row[8:])) == int32(key[2]) {
				return slot, true
			}
		}
		return 0, false
	}
	for _, key := range [][3]int{{}, {127 * 2048, 0, 0}} {
		slot, found := shaderFind(key)
		if !found || slot != m.SectorToInfo[p.o.RenderVoxelMap().Sectors[key]].SlotIndex {
			t.Fatalf("128-probe shader lookup cannot read committed sector %v", key)
		}
	}
	if _, found := shaderFind([3]int{128 * 2048, 0, 0}); found {
		t.Fatal("removed overflowing key remains shader-visible")
	}
	if m.SectorLookupFrameStats().StageBytes != 0 {
		t.Fatal("failed generation retained stage ownership after valid replacement drained")
	}
}
