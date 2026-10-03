package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS2iRetainedCandidatesBoundedAmidManyPins(t *testing.T) {
	const pins = 32
	m := s2dManager()
	active := make(map[*volume.XBrickMap]bool)
	pinned := make([]*core.VoxelObject, pins)
	for i := range pinned {
		kind := "uniform"
		if i%2 == 0 {
			kind = "mixed"
		}
		pinned[i] = s2dAllocated(t, m, kind, 1)
		pinned[i].RenderEnabled = false
		m.RetainVoxelMap(pinned[i].XBrickMap)
		active[pinned[i].XBrickMap] = true
	}
	m.evictRetainedVoxelMaps(active)
	pinnedBytes := s2dStats(t, m).Bytes
	var warm []*core.VoxelObject
	for i := 0; i < 4; i++ {
		obj := s2dAllocated(t, m, "uniform", 1)
		m.RetainVoxelMap(obj.XBrickMap)
		warm = append(warm, obj)
	}
	before := s2dStats(t, m)
	charge := (before.Bytes - pinnedBytes) / 4
	if charge == 0 || before.Bytes != pinnedBytes+4*charge || before.EvictionCandidateVisits != 0 {
		t.Fatalf("assigned fixture unexpectedly trimmed or changed charge: %+v", before)
	}
	if !m.ActivateRetainedVoxelMap(warm[0].XBrickMap) {
		t.Fatal("old warm map was not available for recency refresh")
	}
	m.RetainedVoxelMapBudgetBytes = int64(pinnedBytes + charge)
	m.RetainedVoxelMapBudgetSectors = 0
	m.evictRetainedVoxelMaps(active)
	after := s2dStats(t, m)
	if after.Entries != pins+1 || after.Bytes != pinnedBytes+charge || after.PinnedBytes != pinnedBytes ||
		after.Evictions-before.Evictions != 3 || after.EvictionCandidateVisits-before.EvictionCandidateVisits != 3 {
		t.Fatalf("multi-victim pressure must examine only three inactive victims: before=%+v after=%+v", before, after)
	}
	if !m.ActivateRetainedVoxelMap(warm[0].XBrickMap) {
		t.Fatal("recently activated warm map was evicted")
	}
	for _, obj := range warm[1:] {
		if m.ActivateRetainedVoxelMap(obj.XBrickMap) {
			t.Fatal("older inactive map survived LRU pressure")
		}
	}
	for _, obj := range pinned {
		if !m.ActivateRetainedVoxelMap(obj.XBrickMap) {
			t.Fatal("hidden active map lost its assigned geometry")
		}
	}
	m.evictRetainedVoxelMaps(active)
	if visits := s2dStats(t, m).EvictionCandidateVisits; visits != 3 {
		t.Fatalf("reads or no-pressure maintenance selected candidates: %d", visits)
	}
	m.RetainedVoxelMapBudgetBytes = 1
	m.evictRetainedVoxelMaps(active)
	pressure := s2dStats(t, m)
	if pressure.Entries != pins || pressure.Bytes != pinnedBytes || pressure.PinnedBytes != pinnedBytes || pressure.PressureBytes != pinnedBytes-1 || pressure.EvictionCandidateVisits != 4 {
		t.Fatalf("inactive cleanup did not preserve pinned excess: %+v", pressure)
	}
	m.evictRetainedVoxelMaps(active)
	if visits := s2dStats(t, m).EvictionCandidateVisits; visits != pressure.EvictionCandidateVisits {
		t.Fatalf("all-pinned pressure selected a candidate: %d", visits)
	}
}

func TestS2iRetainedRecencyPinTransitionsAndExplicitRelease(t *testing.T) {
	for _, touch := range []string{"retain", "activate"} {
		t.Run(touch, func(t *testing.T) {
			m := s2dManager()
			a, b, c := s2dAllocated(t, m, "uniform", 1), s2dAllocated(t, m, "uniform", 1), s2dAllocated(t, m, "uniform", 1)
			for _, obj := range []*core.VoxelObject{a, b, c} {
				m.RetainVoxelMap(obj.XBrickMap)
			}
			m.evictRetainedVoxelMaps(nil)
			before := s2dStats(t, m)
			charge := before.Bytes / 3
			if touch == "retain" {
				if !m.RetainVoxelMap(a.XBrickMap) {
					t.Fatal("retaining assigned map lost its allocation")
				}
			} else if !m.ActivateRetainedVoxelMap(a.XBrickMap) {
				t.Fatal("activation fixture missed assigned retained map")
			}
			m.RetainedVoxelMapBudgetBytes = int64(2 * charge)
			m.RetainedVoxelMapBudgetSectors = 0
			m.evictRetainedVoxelMaps(nil)
			if stats := s2dStats(t, m); stats.Bytes != 2*charge || stats.EvictionCandidateVisits != 1 || stats.Evictions != 1 {
				t.Fatalf("recency update did not produce one oldest victim: %+v", stats)
			}
			if m.ActivateRetainedVoxelMap(b.XBrickMap) || !m.ActivateRetainedVoxelMap(a.XBrickMap) {
				t.Fatal("public recency update failed to protect A over B")
			}
			active := map[*volume.XBrickMap]bool{c.XBrickMap: true}
			m.RetainedVoxelMapBudgetBytes = int64(charge)
			m.evictRetainedVoxelMaps(active)
			if stats := s2dStats(t, m); stats.Entries != 1 || stats.PinnedBytes != charge || stats.EvictionCandidateVisits != 2 || stats.Evictions != 2 {
				t.Fatalf("pin transition did not exclude C from pressure selection: %+v", stats)
			}
			d := s2dAllocated(t, m, "uniform", 1)
			m.RetainVoxelMap(d.XBrickMap)
			m.evictRetainedVoxelMaps(nil)
			if stats := s2dStats(t, m); stats.Bytes != charge || stats.PinnedBytes != 0 || stats.EvictionCandidateVisits != 3 || stats.Evictions != 3 {
				t.Fatalf("newly inactive C did not restore its saved older recency: %+v", stats)
			}
			if m.ActivateRetainedVoxelMap(c.XBrickMap) || !m.ActivateRetainedVoxelMap(d.XBrickMap) {
				t.Fatal("unpin made old C newer than D")
			}
			m.ReleaseRetainedVoxelMap(d.XBrickMap)
			m.ReleaseRetainedVoxelMap(d.XBrickMap)
			if stats := s2dStats(t, m); stats.Entries != 0 || stats.Bytes != 0 || stats.EvictionCandidateVisits != 3 {
				t.Fatalf("explicit release counted a pressure victim or retained its owner: %+v", stats)
			}
			e, f := s2dAllocated(t, m, "uniform", 1), s2dAllocated(t, m, "uniform", 1)
			m.RetainVoxelMap(e.XBrickMap)
			m.RetainVoxelMap(f.XBrickMap)
			m.evictRetainedVoxelMaps(nil)
			if stats := s2dStats(t, m); stats.Bytes != charge || stats.Entries != 1 || stats.EvictionCandidateVisits != 4 || stats.Evictions != 4 {
				t.Fatalf("released map remained a dead pressure candidate: %+v", stats)
			}
			if m.ActivateRetainedVoxelMap(d.XBrickMap) || m.ActivateRetainedVoxelMap(e.XBrickMap) || !m.ActivateRetainedVoxelMap(f.XBrickMap) {
				t.Fatal("explicit removal corrupted later retained-map selection")
			}
		})
	}
}
