package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s2dManager() *GpuBufferManager {
	return &GpuBufferManager{
		Allocations:  make(map[*volume.XBrickMap]*ObjectGpuAllocation),
		SectorToInfo: make(map[*volume.Sector]SectorGpuInfo),
		BrickToSlot:  make(map[*volume.Brick]PayloadSlot), BrickToAuxSlot: make(map[*volume.Brick]uint32),
		MaterialAllocations:   make(map[*core.VoxelObject]*MaterialGpuAllocation),
		VoxelPayloadPageCount: 1, VoxelPayloadBricks: 128,
	}
}

// Model completed assignments with the same slot allocators and pointer snapshots
// used by uploads. No GPU device or queue writes are needed for retention policy.
func s2dAllocated(t *testing.T, m *GpuBufferManager, kind string, bricks int) *core.VoxelObject {
	t.Helper()
	obj := scheduleObject(uint32(len(m.Allocations)+1), [3]int{})
	for i := 0; i < bricks; i++ {
		schedulePutBrick(obj, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, scheduleBrick(kind))
	}
	obj.XBrickMap.ClearDirty()
	alloc := testObjectGpuAllocationForMap(obj.XBrickMap)
	m.Allocations[obj.XBrickMap] = alloc
	for key, sector := range alloc.Sectors {
		m.SectorToInfo[sector] = SectorGpuInfo{SlotIndex: m.SectorAlloc.Alloc(), BrickTableIndex: m.BrickAlloc.Alloc() * 64}
		for _, brick := range alloc.Bricks[key] {
			if brick == nil {
				continue
			}
			m.BrickToAuxSlot[brick] = m.VoxelAuxAlloc.Alloc()
			if resolveBrickUploadMode(brick.Flags).usesPayload {
				slot, ok := m.allocPayloadSlot()
				if !ok {
					t.Fatal("fixture exhausted payload slots")
				}
				m.BrickToSlot[brick] = slot
			}
		}
	}
	return obj
}

func s2dStats(t *testing.T, m *GpuBufferManager) RetainedVoxelMapStats {
	t.Helper()
	s := m.RetainedVoxelMapStats()
	if m.RetainedVoxelMapStats() != s {
		t.Fatal("retention stats read changed observations or work counters")
	}
	return s
}

func TestS2dRetainedBytesFollowAssignedSparseDenseAndUniformSlots(t *testing.T) {
	if DefaultRetainedVoxelMapBudgetBytes != 128<<20 {
		t.Fatalf("default byte budget=%d, want 128 MiB", DefaultRetainedVoxelMapBudgetBytes)
	}
	var absent *GpuBufferManager
	if absent.RetainedVoxelMapStats() != (RetainedVoxelMapStats{}) {
		t.Fatal("nil manager stats must be zero")
	}
	for _, tc := range []struct {
		name, kind      string
		bricks, payload int
	}{{"sparse", "mixed", 1, 1}, {"dense", "mixed", 64, 64}, {"uniform", "uniform", 1, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			m := s2dManager()
			obj := s2dAllocated(t, m, tc.kind, tc.bricks)
			if !m.RetainVoxelMap(obj.XBrickMap) {
				t.Fatal("assigned allocation was not retained")
			}
			m.evictRetainedVoxelMaps(nil)
			want := uint64(256 + 32 + 64*BrickRecordSize + tc.bricks*VoxelAuxRecordBytes + tc.payload*512)
			s := s2dStats(t, m)
			if s.Entries != 1 || s.Bytes != want || s.PinnedBytes != 0 || s.MaxBytes != 0 || s.PressureBytes != 0 {
				t.Fatalf("assigned slot charge=%+v, want bytes %d", s, want)
			}
		})
	}
	// A CPU sector without an assigned sector/table block costs metadata only.
	m := s2dManager()
	obj := scheduleObject(10, [3]int{})
	m.Allocations[obj.XBrickMap] = testObjectGpuAllocationForMap(obj.XBrickMap)
	m.RetainVoxelMap(obj.XBrickMap)
	m.evictRetainedVoxelMaps(nil)
	if got := s2dStats(t, m).Bytes; got != 256 {
		t.Fatalf("unassigned sector charge=%d, want metadata only", got)
	}
}

func TestS2dMutableCPUMapsKeepAssignedChargeAndReleaseSlots(t *testing.T) {
	for _, change := range []string{"delete", "replace"} {
		t.Run(change, func(t *testing.T) {
			m := s2dManager()
			obj := s2dAllocated(t, m, "mixed", 1)
			xbm := obj.XBrickMap
			oldSector := firstSectorForTest(xbm)
			oldBrick := firstBrickForTest(oldSector)
			sectorSlot := m.SectorToInfo[oldSector].SlotIndex
			tableSlot := m.SectorToInfo[oldSector].BrickTableIndex / 64
			payloadSlot, auxSlot := m.BrickToSlot[oldBrick], m.BrickToAuxSlot[oldBrick]
			material := &MaterialGpuAllocation{MaterialOffset: 256, MaterialCapacity: 256}
			m.MaterialAllocations[obj] = material
			m.MaterialAlloc.Tail = 2
			m.RetainVoxelMap(xbm)
			m.evictRetainedVoxelMaps(nil)
			charge := s2dStats(t, m).Bytes
			delete(xbm.Sectors, [3]int{})
			if change == "replace" {
				xbm.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
				schedulePutBrick(obj, [6]int{}, scheduleBrick("uniform"))
			}
			if got := s2dStats(t, m).Bytes; got != charge {
				t.Fatalf("CPU %s changed still-assigned charge: %d, want %d", change, got, charge)
			}
			currentSector := xbm.Sectors[[3]int{}]
			beforeFound, beforeColor := xbm.GetVoxel(0, 0, 0)
			m.RetainedVoxelMapBudgetBytes = 1
			m.evictRetainedVoxelMaps(nil)
			s := s2dStats(t, m)
			if s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 || s.Evictions != 1 {
				t.Fatalf("inactive byte eviction failed: %+v", s)
			}
			if m.Allocations[xbm] != nil || len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 || len(m.BrickToAuxSlot) != 0 {
				t.Fatal("eviction retained assigned sector/brick/aux/payload mappings")
			}
			if m.SectorAlloc.Alloc() != sectorSlot || m.BrickAlloc.Alloc() != tableSlot || m.PayloadAlloc[payloadSlot.Page].Alloc() != payloadSlot.Slot || m.VoxelAuxAlloc.Alloc() != auxSlot {
				t.Fatal("eviction failed to return assigned slots to allocators")
			}
			afterFound, afterColor := xbm.GetVoxel(0, 0, 0)
			if xbm.Sectors[[3]int{}] != currentSector || afterFound != beforeFound || afterColor != beforeColor || oldBrick.Payload[1][0][0] != 2 || m.MaterialAllocations[obj] != material || m.MaterialAlloc.Alloc() != 2 {
				t.Fatal("map eviction changed CPU geometry or object material allocation")
			}
		})
	}
}

func TestS2dSharedHiddenActiveMapsPinUnderPressure(t *testing.T) {
	m := s2dManager()
	first := s2dAllocated(t, m, "uniform", 1)
	first.RenderEnabled = false
	shared := core.NewVoxelObject()
	shared.XBrickMap = first.XBrickMap
	second := s2dAllocated(t, m, "mixed", 1)
	m.RetainVoxelMap(first.XBrickMap)
	m.RetainVoxelMap(shared.XBrickMap)
	m.RetainVoxelMap(second.XBrickMap)
	m.RetainedVoxelMapBudgetBytes, m.RetainedVoxelMapBudgetSectors = 1, 1
	active := map[*volume.XBrickMap]bool{first.XBrickMap: true, second.XBrickMap: true}
	m.evictRetainedVoxelMaps(active)
	s := s2dStats(t, m)
	want := uint64(2*(256+32+64*BrickRecordSize+VoxelAuxRecordBytes) + 512)
	if s.Entries != 2 || s.Bytes != want || s.PinnedBytes != want || s.MaxBytes != 1 || s.PressureBytes != want-1 || s.Evictions != 0 || s.Hits != 0 {
		t.Fatalf("shared active maps must charge once and survive pressure: %+v", s)
	}
	if m.Allocations[first.XBrickMap] == nil || m.Allocations[second.XBrickMap] == nil {
		t.Fatal("active pressure evicted live allocation")
	}
	delete(active, first.XBrickMap)
	m.evictRetainedVoxelMaps(active)
	s = s2dStats(t, m)
	if s.Entries != 1 || s.Bytes != s.PinnedBytes || s.PressureBytes != s.Bytes-1 || s.Evictions != 1 {
		t.Fatalf("inactive final release must trim around remaining live pressure: %+v", s)
	}
	if !m.ActivateRetainedVoxelMap(second.XBrickMap) || m.ActivateRetainedVoxelMap(first.XBrickMap) {
		t.Fatal("retained activation must hit live map and miss evicted map")
	}
	s = s2dStats(t, m)
	if s.Hits != 1 || s.Misses != 1 {
		t.Fatalf("activation diagnostics changed: %+v", s)
	}
}

func TestS2dRetainedLRURefreshesActiveUseWithoutHits(t *testing.T) {
	m := s2dManager()
	a := s2dAllocated(t, m, "uniform", 1)
	b := s2dAllocated(t, m, "uniform", 1)
	c := s2dAllocated(t, m, "uniform", 1)
	m.RetainVoxelMap(a.XBrickMap)
	m.RetainVoxelMap(b.XBrickMap)
	m.evictRetainedVoxelMaps(map[*volume.XBrickMap]bool{a.XBrickMap: true})
	charge := s2dStats(t, m).Bytes / 2
	m.RetainVoxelMap(c.XBrickMap)
	m.RetainedVoxelMapBudgetBytes = int64(2 * charge)
	m.evictRetainedVoxelMaps(map[*volume.XBrickMap]bool{c.XBrickMap: true})
	s := s2dStats(t, m)
	if s.Entries != 2 || s.Bytes != 2*charge || s.PinnedBytes != charge || s.Evictions != 1 || s.Hits != 0 || s.Activations != 0 {
		t.Fatalf("active age refresh must trim older inactive allocation without hits: %+v", s)
	}
	if !m.ActivateRetainedVoxelMap(a.XBrickMap) || m.ActivateRetainedVoxelMap(b.XBrickMap) {
		t.Fatal("recently active A must survive older inactive B")
	}
}

func TestS2dEmptyEntriesBoundedAndByteCapIndependentOfSectorCap(t *testing.T) {
	m := s2dManager()
	m.RetainedVoxelMapBudgetBytes = 512
	for i := 0; i < 3; i++ {
		if m.RetainVoxelMap(volume.NewXBrickMap()) {
			t.Fatal("empty map incorrectly reported an allocation")
		}
	}
	before := s2dStats(t, m)
	if before.Entries != 3 || before.Bytes != 768 || before.Evictions != 0 {
		t.Fatalf("stats must observe untrimmed metadata without evicting: %+v", before)
	}
	m.evictRetainedVoxelMaps(nil)
	s := s2dStats(t, m)
	if s.Entries != 2 || s.Bytes != 512 || s.PinnedBytes != 0 || s.Evictions != 1 {
		t.Fatalf("empty entries escaped byte cap with sector cap disabled: %+v", s)
	}
	m.RetainedVoxelMapBudgetBytes = 1
	m.evictRetainedVoxelMaps(nil)
	if s = s2dStats(t, m); s.Entries != 0 || s.Bytes != 0 || s.Evictions != 3 {
		t.Fatalf("empty retained owner was not released: %+v", s)
	}
	for _, disabled := range []int64{0, -1} {
		m := s2dManager()
		m.RetainedVoxelMapBudgetBytes, m.RetainedVoxelMapBudgetSectors = disabled, 2
		a, b := s2dAllocated(t, m, "uniform", 1), s2dAllocated(t, m, "mixed", 1)
		m.RetainVoxelMap(a.XBrickMap)
		m.RetainVoxelMap(b.XBrickMap)
		m.evictRetainedVoxelMaps(nil)
		s := s2dStats(t, m)
		if s.Entries != 2 || s.Bytes == 0 || s.MaxBytes != 0 || s.PressureBytes != 0 || s.Evictions != 0 {
			t.Fatalf("nonpositive byte cap %d must preserve legacy sector policy: %+v", disabled, s)
		}
		m.RetainedVoxelMapBudgetSectors = 1
		m.evictRetainedVoxelMaps(nil)
		if s = s2dStats(t, m); s.Entries != 1 || s.Sectors != 1 || s.Evictions != 1 || s.MaxBytes != 0 || s.PressureBytes != 0 {
			t.Fatalf("legacy sector cap stopped working with byte cap %d: %+v", disabled, s)
		}
	}
}
