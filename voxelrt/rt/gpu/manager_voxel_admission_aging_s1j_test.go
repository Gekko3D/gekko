package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// Fixed, native-legal buffer capacities supply recurring admission opportunities.
// Releasing the previous winner returns its slots; capacities never shrink.
func s1jResources(sectorSlots, materialBlocks uint64) voxelGPUResources {
	r := s1iResources(256)
	if sectorSlots > 0 {
		r.BrickTable = sectorSlots * 64 * BrickRecordSize
		r.Auxiliary = sectorSlots * 64 * VoxelAuxRecordBytes
		r.SectorGrid = 32768
	}
	r.Material = materialBlocks * materialBlockCapacity * 64
	limit := max(r.Auxiliary, r.Material, r.SectorGrid)
	r.MaxBufferBytes, r.MaxStorageBytes, r.MaxUniformBytes = limit, limit, 256
	return r
}

func s1jReady(m *GpuBufferManager, obj *core.VoxelObject) bool {
	target := obj.RenderVoxelMap()
	if target == nil {
		return false
	}
	ready, _, _ := m.RenderVoxelObjectReady(obj, target, target.Revision)
	return ready
}

func s1jMaterialAlias(owner *core.VoxelObject, optional bool) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap, obj.MaterialTable = owner.XBrickMap, owner.MaterialTable
	obj.VoxelGPUAdmissionOptional = optional
	return obj
}

func TestS1jOptionalGeometryAgesPastFreshPriorityWithCurrentCPUContent(t *testing.T) {
	m, resources := s1iManager(), s1jResources(1, 1)
	seed := s1iObject(10, 1, false)
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{seed}}, &resources, s1iGrow(&resources))
	if !s1jReady(m, seed) {
		t.Fatal("one-slot admission fixture never reached ready")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: m.VoxelGPUAdmissionStats().TotalBytes})
	old := s1iObject(20, 1, true)
	old.VoxelUploadPriority, old.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 9999
	progressed := false
	for frame := 0; frame < 24; frame++ {
		// Tracked edits keep the same live map request and must not restart wait.
		value := uint8(2 - frame%2)
		old.XBrickMap.SetVoxel(0, 0, 0, value)
		fresh := s1iObject(uint32(100+frame), 1, true)
		fresh.VoxelUploadPriority, fresh.VoxelUploadOrder = core.VoxelUploadPriorityVisible, 1
		scene := &core.Scene{Objects: []*core.VoxelObject{fresh, old}}
		s1iRun(t, m, scene, &resources, s1iGrow(&resources))
		if frame == 0 && (s1jReady(m, old) || !s1jReady(m, fresh)) {
			t.Fatal("fresh raw priority did not establish initial admission precedence")
		}
		if s1jReady(m, old) {
			if found, got := old.XBrickMap.GetVoxel(0, 0, 0); !found || got != value {
				t.Fatal("aged admission did not preserve current authoritative content")
			}
			if s1jReady(m, fresh) {
				t.Fatal("one physical sector slot admitted both competing maps")
			}
			progressed = true
			break
		}
		if m.Allocations[old.XBrickMap] != nil || m.MaterialAllocations[old] != nil || !old.XBrickMap.StructureDirty || !s1jReady(m, fresh) {
			t.Fatal("deferred old demand acquired ownership, lost authority, or blocked fitting fresh work")
		}
	}
	if !progressed {
		t.Fatal("continuously live optional geometry starved behind recurring fresh higher-priority demand")
	}
}

func TestS1jOptionalSharedMaterialsAgeIndependentlyAndRequiredMaterialWins(t *testing.T) {
	// Empty shared geometry isolates two material blocks under a legal 32 KiB
	// hard storage limit; its required geometry owner never needs new capacity.
	m, resources := s1iManager(), s1jResources(0, 2)
	base := s1iObject(10, 0, false)
	base.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	seed := s1jMaterialAlias(base, false)
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, seed}}, &resources, s1iGrow(&resources))
	if !s1jReady(m, base) || !s1jReady(m, seed) {
		t.Fatal("two-block shared-material fixture never reached ready")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: m.VoxelGPUAdmissionStats().TotalBytes})
	old := s1jMaterialAlias(base, true)
	old.VoxelUploadPriority, old.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 9999
	progressed := false
	for frame := 0; frame < 24; frame++ {
		fresh := s1jMaterialAlias(base, true)
		fresh.VoxelUploadPriority, fresh.VoxelUploadOrder = core.VoxelUploadPriorityVisible, 1
		s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, fresh, old}}, &resources, s1iGrow(&resources))
		if !s1jReady(m, base) {
			t.Fatal("optional material pressure invalidated its required shared geometry owner")
		}
		if frame == 0 && (s1jReady(m, old) || !s1jReady(m, fresh)) {
			t.Fatal("fresh material raw priority did not establish initial precedence")
		}
		if s1jReady(m, old) {
			if s1jReady(m, fresh) {
				t.Fatal("two material blocks admitted base and both competing aliases")
			}
			progressed = true
			break
		}
		if m.MaterialAllocations[old] != nil || !s1jReady(m, fresh) {
			t.Fatal("waiting optional material bypassed admission or blocked a fitting alias")
		}
	}
	if !progressed {
		t.Fatal("optional material starved on permanently required shared geometry")
	}
	// Start another optional wait, then insert a fresh required material whose
	// raw priority is lower than the aged optional demand. Required service wins.
	waiting := s1jMaterialAlias(base, true)
	waiting.VoxelUploadPriority, waiting.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 9999
	for frame := 0; frame < 12; frame++ {
		fresh := s1jMaterialAlias(base, true)
		fresh.VoxelUploadPriority, fresh.VoxelUploadOrder = core.VoxelUploadPriorityVisible, 1
		s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, fresh, waiting}}, &resources, s1iGrow(&resources))
		if s1jReady(m, waiting) || !s1jReady(m, fresh) {
			t.Fatal("required-precedence fixture failed to retain the optional material wait")
		}
	}
	required := s1jMaterialAlias(base, false)
	required.VoxelUploadPriority, required.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 10000
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, waiting, required}}, &resources, s1iGrow(&resources))
	if !s1jReady(m, base) || !s1jReady(m, required) || s1jReady(m, waiting) || m.MaterialAllocations[waiting] != nil {
		t.Fatal("aged optional material outranked fresh required material admission")
	}
}

func TestS1jOptionalGeometryAndSharedMaterialCompeteInOneAgedQueue(t *testing.T) {
	m, resources := s1iManager(), s1jResources(1, 2)
	base := s1iObject(10, 0, false)
	base.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	seed := s1iObject(20, 1, false)
	s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, seed}}, &resources, s1iGrow(&resources))
	if !s1jReady(m, base) || !s1jReady(m, seed) {
		t.Fatal("merged-queue fixture failed to establish two material owners")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: m.VoxelGPUAdmissionStats().TotalBytes})
	old := s1jMaterialAlias(base, true)
	old.VoxelUploadPriority, old.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 9999
	progressed := false
	for frame := 0; frame < 24; frame++ {
		fresh := s1iObject(uint32(100+frame), 1, true)
		fresh.VoxelUploadPriority, fresh.VoxelUploadOrder = core.VoxelUploadPriorityVisible, 1
		s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{base, fresh, old}}, &resources, s1iGrow(&resources))
		if !s1jReady(m, base) {
			t.Fatal("merged optional queue invalidated the pinned empty base")
		}
		if frame == 0 && (s1jReady(m, old) || !s1jReady(m, fresh)) {
			t.Fatal("fresh optional geometry raw priority lost to a new lower-priority optional material")
		}
		if s1jReady(m, old) {
			if s1jReady(m, fresh) || m.Allocations[fresh.XBrickMap] != nil || m.MaterialAllocations[fresh] != nil {
				t.Fatal("fresh geometry bypassed its joint material preflight after aged material won")
			}
			progressed = true
			break
		}
		if m.MaterialAllocations[old] != nil || !s1jReady(m, fresh) {
			t.Fatal("merged-queue waiter bypassed admission or blocked fitting fresh geometry")
		}
	}
	if !progressed {
		t.Fatal("optional material starved behind a separate stream of fresh optional geometry")
	}
}

func TestS1jPendingGenerationAndDetachmentResetDeferredWait(t *testing.T) {
	for _, reset := range []string{"pending generation", "observed detachment"} {
		t.Run(reset, func(t *testing.T) {
			m, resources := s1iManager(), s1jResources(2, 2)
			obj := s1iObject(20, 1, false)
			coarse := s1iObject(10, 1, false).XBrickMap
			if !obj.SetRenderLOD2(coarse) {
				t.Fatal("coarse selection rejected")
			}
			seed := s1iObject(50, 1, false)
			s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{obj, seed}}, &resources, s1iGrow(&resources))
			obj.VoxelUploadPriority, obj.VoxelUploadOrder = core.VoxelUploadPriorityKeep, 9999
			if !s1jReady(m, obj) || !obj.SetPendingFullUpload() {
				t.Fatal("pending wait fixture lost its ready coarse selection")
			}
			full := obj.XBrickMap
			var lastFresh *core.VoxelObject
			// Keep prevents progress against fresh fallback requests for this wait
			// interval. The later Prefetch contender reveals any stale promotion.
			for frame := 0; frame < 24; frame++ {
				lastFresh = s1iObject(uint32(100+frame), 1, true)
				lastFresh.VoxelUploadPriority, lastFresh.VoxelUploadOrder = core.VoxelUploadPriorityFallback, 1
				s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{obj, lastFresh}}, &resources, s1iGrow(&resources))
				ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision)
				if ready || !s1jReady(m, obj) || !s1jReady(m, lastFresh) || m.Allocations[full] != nil {
					t.Fatal("pending demand failed to remain deferred behind fitting fallback work")
				}
			}
			switch reset {
			case "pending generation":
				previous := obj.PendingFullUploadGeneration()
				obj.ClearPendingFullUpload()
				if !obj.SetPendingFullUpload() || obj.PendingFullUploadGeneration() == previous {
					t.Fatal("fixture did not establish a new pending generation")
				}
			case "observed detachment":
				s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{lastFresh}}, &resources, s1iGrow(&resources))
			}
			fresh := s1iObject(999, 1, true)
			fresh.VoxelUploadPriority, fresh.VoxelUploadOrder = core.VoxelUploadPriorityPrefetch, 1
			s1iRun(t, m, &core.Scene{Objects: []*core.VoxelObject{obj, fresh}}, &resources, s1iGrow(&resources))
			ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision)
			if ready || !s1jReady(m, fresh) || !s1jReady(m, obj) || obj.RenderVoxelMap() != coarse || m.Allocations[full] != nil {
				t.Fatal("renewed/detached pending request inherited its cancelled wait or lost coarse coverage")
			}
		})
	}
}
