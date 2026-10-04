package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"maps"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Only physical allocation and queue writes are replaced. Admission, structure,
// service completion, lookup construction and readiness use production paths.
func s1iResources(capacity uint64) voxelGPUResources {
	return voxelGPUResources{
		SectorTable: capacity, BrickTable: capacity, Auxiliary: capacity,
		Material: capacity, SectorGrid: capacity, DirectLookup: capacity,
		SectorGridParams: capacity, AtlasBytes: 4096,
		MaxBufferBytes: 1 << 40, MaxStorageBytes: 1 << 40, MaxUniformBytes: 1 << 40,
	}
}

func s1iBufferBytes(r voxelGPUResources) uint64 {
	return r.SectorTable + r.BrickTable + r.Auxiliary + r.Material + r.SectorGrid + r.DirectLookup + r.SectorGridParams
}

func s1iGrow(resources *voxelGPUResources) func(voxelGPUResources) error {
	return func(next voxelGPUResources) error {
		old := *resources
		retired := old.RetiredBytes
		for _, pair := range [][2]uint64{
			{old.SectorTable, next.SectorTable}, {old.BrickTable, next.BrickTable},
			{old.Auxiliary, next.Auxiliary}, {old.Material, next.Material},
			{old.SectorGrid, next.SectorGrid}, {old.DirectLookup, next.DirectLookup},
			{old.SectorGridParams, next.SectorGridParams},
		} {
			if pair[1] > pair[0] {
				retired += pair[0]
			}
		}
		next.RetiredBytes = retired
		*resources = next
		return nil
	}
}

func s1iObject(id uint32, sectors int, optional bool) *core.VoxelObject {
	obj := scheduleObject(id)
	obj.VoxelGPUAdmissionOptional = optional
	for i := 0; i < sectors; i++ {
		obj.XBrickMap.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
	}
	obj.XBrickMap.StructureDirty = true
	if sectors > 0 {
		obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	}
	return obj
}

func s1iRun(t *testing.T, m *GpuBufferManager, scene *core.Scene, r *voxelGPUResources, grow func(voxelGPUResources) error) []voxelUploadWork {
	t.Helper()
	m.prepareVoxelGPUAdmission(scene, r, grow)
	// Calling these again protects against a denied target leaking into either
	// of the existing paths used by other preparation callers.
	m.prepareVoxelStructureDirtyState(scene)
	var writes []voxelUploadWork
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		writes = append(writes, w)
		if w.kind == voxelUploadMaterial {
			return true
		}
		xbm := w.targetMap()
		key := w.sectorKey
		if w.kind == voxelUploadBrick {
			key = [3]int{w.brickKey[0], w.brickKey[1], w.brickKey[2]}
		}
		sector := xbm.Sectors[key]
		pointers := m.Allocations[xbm].Bricks[key]
		for i := 0; i < 64; i++ {
			if w.kind == voxelUploadBrick && i != w.brickKey[3]+w.brickKey[4]*4+w.brickKey[5]*16 {
				continue
			}
			brick := sector.GetBrick(i%4, (i/4)%4, i/16)
			pointers[i] = brick
			if brick != nil {
				if _, ok := m.BrickToAuxSlot[brick]; !ok {
					m.BrickToAuxSlot[brick] = m.VoxelAuxAlloc.Alloc()
				}
				if resolveBrickUploadMode(brick.Flags).usesPayload {
					if _, ok := m.BrickToSlot[brick]; !ok {
						slot, ok := m.allocPayloadSlot()
						if !ok {
							t.Fatal("admitted fixture exhausted atlas")
						}
						m.BrickToSlot[brick] = slot
					}
				}
			}
		}
		return true
	})
	buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	// Model successful lookup queue publication, as existing headless readiness
	// fixtures do; this does not certify native hash-buffer publication.
	m.lastSectorGridTopologyRevision = m.sectorTopologyRevision
	return writes
}

func s1iManager() *GpuBufferManager {
	m := s2dManager()
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	return m
}

func s1iHashEntries(t *testing.T, m *GpuBufferManager, scene *core.Scene) map[[4]int32]uint32 {
	t.Helper()
	data, size := m.buildSectorGridData(scene)
	if uint64(size)*32 > uint64(len(data)) {
		t.Fatal("hash-grid publication exceeds returned data")
	}
	entries := make(map[[4]int32]uint32)
	for i := uint32(0); i < size; i++ {
		record := data[i*32 : (i+1)*32]
		slot := binary.LittleEndian.Uint32(record[20:])
		if slot == math.MaxUint32 {
			continue
		}
		key := [4]int32{
			int32(binary.LittleEndian.Uint32(record)),
			int32(binary.LittleEndian.Uint32(record[4:])),
			int32(binary.LittleEndian.Uint32(record[8:])),
			int32(binary.LittleEndian.Uint32(record[16:])),
		}
		entries[key] = slot
	}
	return entries
}

func TestS1iBudgetAndPhysicalAccounting(t *testing.T) {
	var absent *GpuBufferManager
	absent.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	if got := absent.VoxelGPUAdmissionStats(); got != (VoxelGPUAdmissionStats{}) {
		t.Fatalf("nil manager diagnostics=%+v", got)
	}
	m := s1iManager()
	r := s1iResources(1 << 24)
	r.AtlasBytes, r.RetiredBytes = 4<<30, 768
	m.prepareVoxelGPUAdmission(&core.Scene{}, &r, s1iGrow(&r))
	s := m.VoxelGPUAdmissionStats()
	want := s1iBufferBytes(r) + r.AtlasBytes + r.RetiredBytes
	if s.CurrentBufferBytes != 7<<24 || s.RetiredBufferBytes != 768 || s.AtlasBytes != 4<<30 || s.TotalBytes != want || s.MaxBytes != 0 || s.PressureBytes != 0 {
		t.Fatalf("disabled cap physical accounting=%+v, total want %d", s, want)
	}
	if m.VoxelGPUAdmissionStats() != s {
		t.Fatal("stats read changed admission diagnostics")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: want - 1})
	m.prepareVoxelGPUAdmission(&core.Scene{}, &r, s1iGrow(&r))
	if s = m.VoxelGPUAdmissionStats(); s.MaxBytes != want-1 || s.PressureBytes != 1 || s.TotalBytes != want {
		t.Fatalf("fixed/retired pressure=%+v", s)
	}
	// The fake backend models actual retirement release, without shrinking any
	// current capacity. A subsequent admission boundary observes the release.
	r.RetiredBytes = 0
	m.prepareVoxelGPUAdmission(&core.Scene{}, &r, s1iGrow(&r))
	if s = m.VoxelGPUAdmissionStats(); s.RetiredBufferBytes != 0 || s.TotalBytes != want-768 || s.PressureBytes != 0 {
		t.Fatalf("released retired bytes still charged: %+v", s)
	}
}

func TestS1iPhysicalAccountingSaturatesWithoutWrappingCap(t *testing.T) {
	m, r := s1iManager(), s1iResources(1<<24)
	r.SectorTable, r.RetiredBytes = math.MaxUint64-1, math.MaxUint64-2
	r.MaxBufferBytes, r.MaxStorageBytes, r.MaxUniformBytes = math.MaxUint64, math.MaxUint64, math.MaxUint64
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: math.MaxUint64})
	m.prepareVoxelGPUAdmission(&core.Scene{}, &r, s1iGrow(&r))
	s := m.VoxelGPUAdmissionStats()
	if s.CurrentBufferBytes != math.MaxUint64 || s.RetiredBufferBytes != math.MaxUint64-2 || s.TotalBytes != math.MaxUint64 || s.MaxBytes != math.MaxUint64 || s.PressureBytes != 0 {
		t.Fatalf("physical accounting or maximal cap wrapped: %+v", s)
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: math.MaxUint64 - 1})
	m.prepareVoxelGPUAdmission(&core.Scene{}, &r, s1iGrow(&r))
	if s = m.VoxelGPUAdmissionStats(); s.TotalBytes != math.MaxUint64 || s.PressureBytes != 1 {
		t.Fatalf("saturated total lost byte pressure: %+v", s)
	}
}

func TestS1iOptionalDeferralPreservesCPUAndLookup(t *testing.T) {
	resident := s1iObject(10, 1, false)
	resident.XBrickMap.ClearDirty()
	m, scene := scheduleFixture(t, resident)
	r := s1iResources(1 << 20)
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	denied := s1iObject(20, 128, true)
	denied.RenderEnabled = false
	// The denied map borrows an already assigned sector pointer. Lookup must
	// still exclude the denied map rather than publish its shared pointer.
	denied.XBrickMap.Sectors[[3]int{}] = resident.XBrickMap.Sectors[[3]int{}]
	dirtySectors, dirtyBricks := maps.Clone(denied.XBrickMap.DirtySectors), maps.Clone(denied.XBrickMap.DirtyBricks)
	revision := denied.XBrickMap.Revision
	before := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	beforeHash := s1iHashEntries(t, m, scene)
	residentKey := [4]int32{0, 0, 0, int32(resident.XBrickMap.ID)}
	if slot, ok := beforeHash[residentKey]; len(beforeHash) != 1 || !ok || slot != 0 {
		t.Fatalf("initial resident hash entry=%v, want key %v at allocated slot 0", beforeHash, residentKey)
	}
	scene.Objects = append(scene.Objects, denied)
	writes := s1iRun(t, m, scene, &r, s1iGrow(&r))
	for _, w := range writes {
		if w.object == denied || w.targetMap() == denied.XBrickMap {
			t.Fatal("deferred geometry or material reached writer")
		}
	}
	if !denied.XBrickMap.StructureDirty || !reflect.DeepEqual(dirtySectors, denied.XBrickMap.DirtySectors) || !reflect.DeepEqual(dirtyBricks, denied.XBrickMap.DirtyBricks) || denied.XBrickMap.Revision != revision {
		t.Fatal("deferral acknowledged authoritative dirty work")
	}
	if found, value := denied.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatal("deferral changed CPU voxels")
	}
	if m.Allocations[denied.XBrickMap] != nil || m.MaterialAllocations[denied] != nil {
		t.Fatal("deferral acquired geometry/material ownership")
	}
	if after := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0); !bytes.Equal(before, after) {
		t.Fatal("denied aliased map changed published direct lookup")
	}
	t.Run("forced hash excludes denied alias", func(t *testing.T) {
		t.Setenv(forceHashLookupEnv, "1")
		if after := s1iHashEntries(t, m, scene); !reflect.DeepEqual(beforeHash, after) {
			t.Fatal("denied map published its shared sector pointer in hash lookup", after)
		}
	})
	if ready, _, _ := m.RenderVoxelObjectReady(denied, denied.XBrickMap, revision); ready {
		t.Fatal("deferred target reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(resident, resident.XBrickMap, resident.XBrickMap.Revision); !ready {
		t.Fatal("deferred arrival invalidated resident fallback")
	}
	if s := m.VoxelGPUAdmissionStats(); s.DeferredMaps != 1 || s.HardLimitDeferredMaps != 0 || s.PressureBytes != s.TotalBytes-1 {
		t.Fatalf("soft deferral diagnostics=%+v", s)
	}
}

func TestS1iExactBoundaryStableOrderAndSmallerFit(t *testing.T) {
	// Obtain the backend's complete physical allocation for one small target.
	// Use that measured result as the exact boundary, including all lookup bytes.
	calibration, r := s1iManager(), s1iResources(0)
	seed := s1iObject(30, 1, true)
	s1iRun(t, calibration, &core.Scene{Objects: []*core.VoxelObject{seed}}, &r, s1iGrow(&r))
	boundary := calibration.VoxelGPUAdmissionStats().TotalBytes
	if boundary <= r.AtlasBytes {
		t.Fatal("calibration failed to allocate voxel buffers")
	}
	for _, deficit := range []uint64{0, 1} {
		m, resources := s1iManager(), s1iResources(0)
		large, small := s1iObject(10, 128, true), s1iObject(30, 1, true)
		large.VoxelUploadOrder, small.VoxelUploadOrder = 1, 2
		m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: boundary - deficit})
		writes := s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{small, large}}, &resources, s1iGrow(&resources))
		for _, w := range writes {
			if w.object == large {
				t.Fatal("oversized first-priority target reached writer")
			}
		}
		ready, _, _ := m.RenderVoxelObjectReady(small, small.XBrickMap, small.XBrickMap.Revision)
		if ready != (deficit == 0) {
			t.Fatalf("exact boundary minus %d: small ready=%v", deficit, ready)
		}
	}
	// Two otherwise equal fresh maps compete for one material block. Stable
	// explicit order selects the winner independently of scene insertion order.
	for _, reverse := range []bool{false, true} {
		m, resources := s1iManager(), s1iResources(0)
		a, b := s1iObject(40, 1, true), s1iObject(20, 1, true)
		a.VoxelUploadOrder, b.VoxelUploadOrder = 1, 2
		objects := []*core.VoxelObject{a, b}
		if reverse {
			objects = []*core.VoxelObject{b, a}
		}
		m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: boundary})
		s1iRun(t, m, &core.Scene{Objects: objects}, &resources, s1iGrow(&resources))
		if ready, _, _ := m.RenderVoxelObjectReady(a, a.XBrickMap, a.XBrickMap.Revision); !ready {
			t.Fatal("stable earlier-order target did not win admission")
		}
		if ready, _, _ := m.RenderVoxelObjectReady(b, b.XBrickMap, b.XBrickMap.Revision); ready {
			t.Fatal("second new material owner exceeded exact physical boundary")
		}
	}
}

func TestS1iRequiredSharedPinsAndCapacityReuse(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m, r := s1iManager(), s1iResources(0)
		required := s1iObject(10, 1, false)
		required.RenderEnabled = !hidden
		optional := core.NewVoxelObject()
		optional.XBrickMap, optional.MaterialTable = required.XBrickMap, required.MaterialTable
		optional.VoxelGPUAdmissionOptional, optional.RenderEnabled = true, false
		m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
		s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{optional, required}}, &r, s1iGrow(&r))
		if ready, _, _ := m.RenderVoxelObjectReady(required, required.XBrickMap, required.XBrickMap.Revision); !ready {
			t.Fatal("required shared hidden/visible map blocked by optional alias")
		}
		if m.MaterialAllocations[optional] != nil {
			t.Fatal("required shared geometry bypassed separate new optional material admission")
		}
		if ready, _, _ := m.RenderVoxelObjectReady(optional, optional.XBrickMap, optional.XBrickMap.Revision); ready {
			t.Fatal("new optional material owner reported ready without material admission")
		}
		if s := m.VoxelGPUAdmissionStats(); s.PressureBytes != s.TotalBytes-1 {
			t.Fatalf("required soft excess not exposed: %+v", s)
		}
	}
	m, r := s1iManager(), s1iResources(1<<24)
	optional := s1iObject(20, 1, true)
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	scene := &core.Scene{Objects: []*core.VoxelObject{optional}}
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	if ready, _, _ := m.RenderVoxelObjectReady(optional, optional.XBrickMap, optional.XBrickMap.Revision); !ready {
		t.Fatal("zero-growth optional work blocked despite reusable capacity")
	}
	// A previously admitted map remains pinned when its edit needs growth.
	m, r = s1iManager(), s1iResources(0)
	optional = s1iObject(30, 1, true)
	scene = &core.Scene{Objects: []*core.VoxelObject{optional}}
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	for i := 1; i < 600; i++ {
		optional.XBrickMap.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
	}
	optional.XBrickMap.StructureDirty = true
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	if optional.XBrickMap.StructureDirty || m.VoxelGPUAdmissionStats().DeferredMaps != 0 {
		t.Fatal("admitted optional owner lost its pin during structural growth")
	}
	if s := m.VoxelGPUAdmissionStats(); s.RetiredBufferBytes == 0 || s.TotalBytes != s1iBufferBytes(r)+r.RetiredBytes+r.AtlasBytes || s.PressureBytes != s.TotalBytes-1 {
		t.Fatalf("pinned replacement failed to charge unreleased overlap: %+v", s)
	}
}

func TestS1iOptionalReplacementRequiresPeakOverlapBudget(t *testing.T) {
	setup := func() (*GpuBufferManager, *core.Scene, *voxelGPUResources, *core.VoxelObject) {
		m, r := s1iManager(), s1iResources(0)
		resident := s1iObject(10, 1, false)
		scene := &core.Scene{Objects: []*core.VoxelObject{resident}}
		s1iRun(t, m, scene, &r, s1iGrow(&r))
		arrival := s1iObject(20, 64, true)
		scene.Objects = append(scene.Objects, arrival)
		return m, scene, &r, arrival
	}
	// Measure the same replacement with the soft cap disabled. The cap below
	// fits its resulting current capacities but excludes the old buffers that
	// the backend must retain during replacement.
	calibration, scene, resources, _ := setup()
	initialBytes := s1iBufferBytes(*resources) + resources.AtlasBytes + resources.RetiredBytes
	s1iRun(t, calibration, scene, resources, s1iGrow(resources))
	measured := calibration.VoxelGPUAdmissionStats()
	currentOnly := measured.CurrentBufferBytes + measured.AtlasBytes
	if measured.RetiredBufferBytes == 0 || currentOnly <= initialBytes || measured.TotalBytes <= currentOnly {
		t.Fatalf("replacement fixture did not establish nonzero overlap: %+v", measured)
	}
	m, scene, resources, arrival := setup()
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: currentOnly})
	for _, w := range s1iRun(t, m, scene, resources, s1iGrow(resources)) {
		if w.object == arrival || w.targetMap() == arrival.XBrickMap {
			t.Fatal("optional replacement admitted using final current bytes instead of peak overlap")
		}
	}
	if !arrival.XBrickMap.StructureDirty || m.Allocations[arrival.XBrickMap] != nil || m.MaterialAllocations[arrival] != nil {
		t.Fatal("peak-budget deferral acquired or acknowledged arriving ownership")
	}
	if s := m.VoxelGPUAdmissionStats(); s.DeferredMaps != 1 || s.HardLimitDeferredMaps != 0 {
		t.Fatalf("peak-budget deferral diagnostic=%+v", s)
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: measured.TotalBytes})
	s1iRun(t, m, scene, resources, s1iGrow(resources))
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); !ready {
		t.Fatal("optional replacement failed to recover after peak-sized cap raise")
	}
	if s := m.VoxelGPUAdmissionStats(); s.TotalBytes != measured.TotalBytes || s.PressureBytes != 0 {
		t.Fatalf("replacement peak boundary accounting=%+v, want %d", s, measured.TotalBytes)
	}
}

func TestS1iHardLimitsAndBackendRefusalKeepRetryableAuthority(t *testing.T) {
	for _, refusal := range []string{"hard buffer", "hard storage", "hard uniform", "backend"} {
		t.Run(refusal, func(t *testing.T) {
			m, r := s1iManager(), s1iResources(0)
			obj := s1iObject(10, 1, false)
			scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
			beforeSectors, beforeBricks := maps.Clone(obj.XBrickMap.DirtySectors), maps.Clone(obj.XBrickMap.DirtyBricks)
			grow := s1iGrow(&r)
			switch refusal {
			case "hard buffer":
				r.MaxBufferBytes = 1024
			case "hard storage":
				r.MaxStorageBytes = 1024
			case "hard uniform":
				r.MaxUniformBytes = 8
			case "backend":
				grow = func(voxelGPUResources) error { return errors.New("test allocation refused") }
			}
			if writes := s1iRun(t, m, scene, &r, grow); len(writes) != 0 {
				t.Fatal("failed allocation reached a content writer")
			}
			if m.Allocations[obj.XBrickMap] != nil || m.MaterialAllocations[obj] != nil || !obj.XBrickMap.StructureDirty || !reflect.DeepEqual(beforeSectors, obj.XBrickMap.DirtySectors) || !reflect.DeepEqual(beforeBricks, obj.XBrickMap.DirtyBricks) {
				t.Fatal("allocation refusal changed ownership or acknowledged dirty authority")
			}
			if found, value := obj.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
				t.Fatal("allocation refusal changed CPU geometry")
			}
			s := m.VoxelGPUAdmissionStats()
			if refusal == "backend" {
				if s.AllocationFailures == 0 || s.LastError == "" {
					t.Fatalf("backend refusal lost diagnostic: %+v", s)
				}
			} else if s.HardLimitDeferredMaps != 1 {
				t.Fatalf("required hard-limit deferral=%+v", s)
			}
			r.MaxBufferBytes, r.MaxStorageBytes, r.MaxUniformBytes = 1<<40, 1<<40, 1<<40
			s1iRun(t, m, scene, &r, s1iGrow(&r))
			if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatal("refused target did not recover on fitting retry")
			}
		})
	}
}

func TestS1iBackendRefusalLeavesExistingResourcesAndFallbackReady(t *testing.T) {
	m, r := s1iManager(), s1iResources(0)
	fallback := s1iObject(10, 1, false)
	scene := &core.Scene{Objects: []*core.VoxelObject{fallback}}
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	arrival := s1iObject(20, 64, false)
	scene.Objects = append(scene.Objects, arrival)
	before := r
	dirtySectors, dirtyBricks := maps.Clone(arrival.XBrickMap.DirtySectors), maps.Clone(arrival.XBrickMap.DirtyBricks)
	attemptedGrowth := false
	writes := s1iRun(t, m, scene, &r, func(next voxelGPUResources) error {
		attemptedGrowth = attemptedGrowth || next.SectorTable > r.SectorTable || next.BrickTable > r.BrickTable || next.Auxiliary > r.Auxiliary || next.Material > r.Material || next.SectorGrid > r.SectorGrid || next.DirectLookup > r.DirectLookup
		return errors.New("test replacement refused")
	})
	for _, work := range writes {
		if work.object == arrival || work.targetMap() == arrival.XBrickMap {
			t.Fatal("refused replacement reached target content writer")
		}
	}
	if !attemptedGrowth || r != before {
		t.Fatal("fixture did not refuse a real replacement atomically")
	}
	if m.Allocations[arrival.XBrickMap] != nil || m.MaterialAllocations[arrival] != nil || !arrival.XBrickMap.StructureDirty || !reflect.DeepEqual(dirtySectors, arrival.XBrickMap.DirtySectors) || !reflect.DeepEqual(dirtyBricks, arrival.XBrickMap.DirtyBricks) {
		t.Fatal("replacement refusal changed arriving ownership or dirty authority")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(arrival, arrival.XBrickMap, arrival.XBrickMap.Revision); ready {
		t.Fatal("refused replacement target reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(fallback, fallback.XBrickMap, fallback.XBrickMap.Revision); !ready {
		t.Fatal("refused replacement invalidated already ready fallback")
	}
}

func TestS1iHardLimitClampsHeadroomAndPreservesBlockedLookupSnapshot(t *testing.T) {
	m, r := s1iManager(), s1iResources(0)
	obj := s1iObject(10, 1, false)
	fallback := s1iObject(20, 0, false)
	scene := &core.Scene{Objects: []*core.VoxelObject{obj, fallback}}
	// Content fits these hard limits; the legacy 2,048-record auxiliary
	// headroom does not. Admission must trim slack rather than reject content.
	r.MaxBufferBytes, r.MaxStorageBytes, r.MaxUniformBytes = 64*VoxelAuxRecordBytes, 64*VoxelAuxRecordBytes, 256
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
		t.Fatal("fitting content rejected because allocation headroom exceeded hard limit")
	}
	before := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	beforeHash := s1iHashEntries(t, m, scene)
	residentKey := [4]int32{0, 0, 0, int32(obj.XBrickMap.ID)}
	if slot, ok := beforeHash[residentKey]; len(beforeHash) != 1 || !ok || slot != 0 {
		t.Fatalf("initial allocated hash entry=%v, want key %v at allocated slot 0", beforeHash, residentKey)
	}
	obj.XBrickMap.Sectors[[3]int{}] = volume.NewSector(0, 0, 0)
	obj.XBrickMap.SetVoxel(0, 0, 0, 2)
	for i := 1; i < 100; i++ {
		obj.XBrickMap.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
	}
	obj.XBrickMap.StructureDirty = true
	dirtySectors, dirtyBricks := maps.Clone(obj.XBrickMap.DirtySectors), maps.Clone(obj.XBrickMap.DirtyBricks)
	if writes := s1iRun(t, m, scene, &r, s1iGrow(&r)); len(writes) != 0 {
		t.Fatal("hard-blocked structural edit leaked content writes")
	}
	if !obj.XBrickMap.StructureDirty || !reflect.DeepEqual(dirtySectors, obj.XBrickMap.DirtySectors) || !reflect.DeepEqual(dirtyBricks, obj.XBrickMap.DirtyBricks) {
		t.Fatal("hard-blocked edit acknowledged dirty authority")
	}
	if after := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0); !bytes.Equal(before, after) {
		t.Fatal("hard-blocked edit published CPU topology instead of its allocated snapshot")
	}
	t.Run("forced hash preserves allocated snapshot", func(t *testing.T) {
		t.Setenv(forceHashLookupEnv, "1")
		if after := s1iHashEntries(t, m, scene); !reflect.DeepEqual(beforeHash, after) {
			t.Fatal("hard-blocked edit changed its old hash keys/slots or published new CPU entries", after)
		}
	})
	if s := m.VoxelGPUAdmissionStats(); s.HardLimitDeferredMaps != 1 {
		t.Fatalf("hard-blocked pinned edit diagnostic=%+v", s)
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
		t.Fatal("hard-blocked structural replacement reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(fallback, fallback.XBrickMap, fallback.XBrickMap.Revision); !ready {
		t.Fatal("hard-blocked structural replacement invalidated unrelated fallback")
	}
}

func TestS1iPendingFineDeferralPreservesCoarseReadiness(t *testing.T) {
	m, scene, obj, coarse := c3h10PendingFixture(t)
	r := s1iResources(0)
	// Model existing physical storage using a coarse-only preparation boundary.
	obj.ClearPendingFullUpload()
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	full := obj.XBrickMap
	for i := 3; i < 128; i++ {
		full.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
	}
	full.StructureDirty = true
	if !obj.SetPendingFullUpload() {
		t.Fatal("pending request rejected")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	s1iRun(t, m, scene, &r, s1iGrow(&r))
	if obj.RenderVoxelMap() != coarse || !full.StructureDirty {
		t.Fatal("fine deferral changed display selection or acknowledged structure")
	}
	if ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision); ready {
		t.Fatal("deferred fine map reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision); !ready {
		t.Fatal("deferred fine allocation lost coarse fallback readiness")
	}
}

func TestS1iSharedSectorAndBrickReleaseWaitsForFinalOwner(t *testing.T) {
	first := s1iObject(10, 1, false)
	schedulePutBrick(first, [6]int{}, scheduleBrick("mixed"))
	second := s1iObject(20, 0, false)
	second.XBrickMap.Sectors[[3]int{}] = first.XBrickMap.Sectors[[3]int{}]
	m, resources := s1iManager(), s1iResources(0)
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{first, second}}, &resources, s1iGrow(&resources))
	sectorTail, brickTail, auxTail := m.SectorAlloc.Tail, m.BrickAlloc.Tail, m.VoxelAuxAlloc.Tail
	m.RetainVoxelMap(first.XBrickMap)
	m.RetainVoxelMap(second.XBrickMap)
	scene := &core.Scene{Objects: []*core.VoxelObject{second}}
	before := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	m.releaseVoxelMapAllocation(first.XBrickMap, m.Allocations[first.XBrickMap])
	if after := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0); !bytes.Equal(before, after) {
		t.Fatal("removing one map released its surviving alias's sector lookup")
	}
	m.evictRetainedVoxelMaps(map[*volume.XBrickMap]bool{second.XBrickMap: true})
	want := uint64(256 + 32 + 64*BrickRecordSize + VoxelAuxRecordBytes + 512)
	if got := m.RetainedVoxelMapStats().Bytes; got != want {
		t.Fatalf("surviving alias lost assigned sector/brick/aux/payload storage: %d, want %d", got, want)
	}
	second.XBrickMap.DirtyBricks[[6]int{}] = true
	scheduleRun(t, m, scene)
	if ready, _, _ := m.RenderVoxelObjectReady(second, second.XBrickMap, second.XBrickMap.Revision); !ready {
		t.Fatal("surviving aliased geometry cannot be serviced")
	}
	m.releaseVoxelMapAllocation(second.XBrickMap, m.Allocations[second.XBrickMap])
	if got := m.RetainedVoxelMapStats(); got.Entries != 0 || got.Bytes != 0 {
		t.Fatalf("final alias release retained assigned ownership: %+v", got)
	}
	// Observe final object retirement through the real admission cleanup so
	// both geometry and the independent material block become reusable.
	s1iRun(t, m, &core.Scene{}, &resources, s1iGrow(&resources))
	third := s1iObject(30, 1, true)
	schedulePutBrick(third, [6]int{}, scheduleBrick("mixed"))
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{third}}, &resources, s1iGrow(&resources))
	if ready, _, _ := m.RenderVoxelObjectReady(third, third.XBrickMap, third.XBrickMap.Revision); !ready || m.SectorAlloc.Tail != sectorTail || m.BrickAlloc.Tail != brickTail || m.VoxelAuxAlloc.Tail != auxTail {
		t.Fatal("final alias release did not make its assigned sector/brick/aux slots reusable")
	}
}

func TestS1iStructuralRemovalPreservesInteriorAliases(t *testing.T) {
	for _, sharing := range []string{"duplicate sector coordinate", "distinct sectors shared brick"} {
		t.Run(sharing, func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			obj := s1iObject(10, 1, false)
			shared := scheduleBrick("mixed")
			schedulePutBrick(obj, [6]int{}, shared)
			switch sharing {
			case "duplicate sector coordinate":
				obj.XBrickMap.Sectors[[3]int{1, 0, 0}] = obj.XBrickMap.Sectors[[3]int{}]
			case "distinct sectors shared brick":
				obj.XBrickMap.Sectors[[3]int{1, 0, 0}] = volume.NewSector(1, 0, 0)
				schedulePutBrick(obj, [6]int{1, 0, 0, 0, 0, 0}, shared)
			}
			scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
			s1iRun(t, m, scene, &resources, s1iGrow(&resources))
			m.RetainVoxelMap(obj.XBrickMap)
			sectorTail, brickTail, auxTail := m.SectorAlloc.Tail, m.BrickAlloc.Tail, m.VoxelAuxAlloc.Tail
			delete(obj.XBrickMap.Sectors, [3]int{})
			obj.XBrickMap.StructureDirty = true
			s1iRun(t, m, scene, &resources, s1iGrow(&resources))
			m.evictRetainedVoxelMaps(map[*volume.XBrickMap]bool{obj.XBrickMap: true})
			want := uint64(256 + 32 + 64*BrickRecordSize + VoxelAuxRecordBytes + 512)
			if got := m.RetainedVoxelMapStats().Bytes; got != want {
				t.Fatalf("structural removal released surviving alias storage: %d, want %d", got, want)
			}
			if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatal("surviving interior alias not ready after structural removal")
			}
			if found, value := obj.XBrickMap.GetVoxel(volume.SectorSize, 0, 0); !found || value != 1 {
				t.Fatal("structural removal changed surviving authoritative brick")
			}
			m.releaseVoxelMapAllocation(obj.XBrickMap, m.Allocations[obj.XBrickMap])
			// Keep the already allocated object material owner while replacing its
			// released geometry; only new optional geometry needs admission.
			fresh := s1iObject(30, 1, true)
			schedulePutBrick(fresh, [6]int{}, scheduleBrick("mixed"))
			obj.XBrickMap, obj.VoxelGPUAdmissionOptional = fresh.XBrickMap, true
			m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
			s1iRun(t, m, scene, &resources, s1iGrow(&resources))
			if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready || m.SectorAlloc.Tail != sectorTail || m.BrickAlloc.Tail != brickTail || m.VoxelAuxAlloc.Tail != auxTail {
				t.Fatal("final interior alias release did not permit optional free-slot reuse under pressure")
			}
		})
	}
}
