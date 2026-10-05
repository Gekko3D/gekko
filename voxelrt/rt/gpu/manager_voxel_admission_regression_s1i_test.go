package gpu

import (
	"bytes"
	"maps"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS1iRegressionRetainedReactivationRespectsHardLookupCapacity(t *testing.T) {
	m, resources := s1iManager(), s1iResources(0)
	const storageLimit = 64 * VoxelAuxRecordBytes
	resources.MaxBufferBytes, resources.MaxStorageBytes, resources.MaxUniformBytes = storageLimit, storageLimit, 256
	shared := volume.NewSector(0, 0, 0)
	first, second := volume.NewXBrickMap(), volume.NewXBrickMap()
	first.ID, second.ID = 10, 20
	for i := 0; i < 129; i++ {
		first.Sectors[[3]int{i, 0, 0}], second.Sectors[[3]int{i, 0, 0}] = shared, shared
	}
	first.StructureDirty, second.StructureDirty = true, true
	owner := scheduleObject(30)
	owner.XBrickMap = first
	schedulePutBrick(owner, [6]int{}, scheduleBrick("mixed"))
	scene := &core.Scene{Objects: []*core.VoxelObject{owner}}
	// Rotate one actual object/material owner between separately admitted maps.
	// Retention pins only geometry, so this establishes one legal material block.
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if ready, _, _ := m.RenderVoxelObjectReady(owner, first, first.Revision); !ready {
		t.Fatal("first retained map never reached ready")
	}
	m.RetainVoxelMap(first)
	owner.XBrickMap = second
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if ready, _, _ := m.RenderVoxelObjectReady(owner, second, second.Revision); !ready {
		t.Fatal("second retained map never reached ready")
	}
	m.RetainVoxelMap(second)
	if uint64(m.VoxelAuxAlloc.Tail)*VoxelAuxRecordBytes > resources.Auxiliary || resources.Auxiliary > storageLimit || resources.SectorGrid != 65536 || m.SectorAlloc.Tail != 1 || m.BrickAlloc.Tail != 1 || resources.Material > storageLimit {
		t.Fatalf("fixture did not establish legal shared geometry/hash capacities: %+v", resources)
	}
	beforeHash := s1iHashEntries(t, m, scene)
	if len(beforeHash) != 129 {
		t.Fatal("ready fallback hash fixture lost its live entries")
	}
	for i := 0; i < 129; i++ {
		if slot, ok := beforeHash[[4]int32{int32(i), 0, 0, int32(second.ID)}]; !ok || slot != 0 {
			t.Fatal("ready fallback hash key/slot fixture incorrect")
		}
	}
	beforeDirect := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	retainedAllocation := m.Allocations[first]
	owner.VoxelUploadOrder = 1
	reactivated := scheduleObject(40)
	reactivated.XBrickMap, reactivated.MaterialTable = first, owner.MaterialTable
	reactivated.VoxelUploadOrder = 2
	scene.Objects = append(scene.Objects, reactivated)
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if m.Allocations[first] != retainedAllocation || m.RetainedVoxelMapStats().Entries != 2 {
		t.Fatal("hard lookup denial discarded the retained geometry pin")
	}
	data, size := m.buildSectorGridData(scene)
	if uint64(size)*32 > resources.SectorGrid || uint64(len(data)) > resources.MaxStorageBytes {
		t.Fatal("reactivated cohort published a hash buffer larger than admitted capacity")
	}
	if s := m.VoxelGPUAdmissionStats(); s.HardLimitDeferredMaps != 1 {
		t.Fatalf("reactivated lookup did not defer at hard capacity: %+v", s)
	}
	if after := s1iHashEntries(t, m, scene); !reflect.DeepEqual(beforeHash, after) {
		t.Fatal("hard-denied retained map published new hash keys")
	}
	if after := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0); !bytes.Equal(beforeDirect, after) {
		t.Fatal("hard-denied retained map published a new direct table")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(owner, second, second.Revision); !ready {
		t.Fatal("reactivated lookup pressure invalidated the ready fallback")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(reactivated, first, first.Revision); ready {
		t.Fatal("hard-denied retained map reported ready without admitted lookup")
	}
}

func TestS1iRegressionReplacementAndNewAdopterShareFinalCapacity(t *testing.T) {
	for _, reverseScene := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement first", true: "new adopter first"}[reverseScene], func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			const finalAuxiliaryBytes = 2 * 64 * VoxelAuxRecordBytes
			resources.SectorTable, resources.BrickTable, resources.Auxiliary = 256, 64*BrickRecordSize, 64*VoxelAuxRecordBytes
			resources.Material, resources.SectorGrid, resources.DirectLookup, resources.SectorGridParams = 2*materialBlockCapacity*64, 32768, 256, 256
			resources.MaxBufferBytes, resources.MaxStorageBytes, resources.MaxUniformBytes = finalAuxiliaryBytes, finalAuxiliaryBytes, 256
			a := s1iObject(10, 1, false)
			for i := 0; i < 64; i++ {
				schedulePutBrick(a, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, scheduleBrick("mixed"))
			}
			scene := &core.Scene{Objects: []*core.VoxelObject{a}}
			s1iRun(t, m, scene, &resources, s1iGrow(&resources))
			if m.VoxelAuxAlloc.Tail != 64 || resources.Auxiliary != 64*VoxelAuxRecordBytes {
				t.Fatal("fixture did not fill the old sector's exact auxiliary capacity")
			}
			oldSector := a.XBrickMap.Sectors[[3]int{}]
			fresh := s1iObject(50, 1, false)
			for i := 0; i < 64; i++ {
				brick := scheduleBrick("mixed")
				brick.SetVoxel(0, 0, 0, 3)
				brick.RefreshMaterialFlags()
				schedulePutBrick(fresh, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, brick)
			}
			a.XBrickMap.Sectors[[3]int{}] = fresh.XBrickMap.Sectors[[3]int{}]
			a.XBrickMap.StructureDirty = true
			a.XBrickMap.Revision++
			a.VoxelUploadOrder = 1
			b := s1iObject(20, 0, false)
			b.XBrickMap.Sectors[[3]int{}] = oldSector
			b.VoxelUploadOrder = 2
			scene.Objects = []*core.VoxelObject{a, b}
			if reverseScene {
				scene.Objects = []*core.VoxelObject{b, a}
			}
			s1iRun(t, m, scene, &resources, s1iGrow(&resources))
			// Both final sectors fit the exact legal auxiliary capacity. Preparation
			// must not consume an extra slot absent from the resource transaction.
			if uint64(m.SectorAlloc.Tail)*32 > resources.SectorTable || uint64(m.BrickAlloc.Tail)*64*BrickRecordSize > resources.BrickTable || uint64(m.VoxelAuxAlloc.Tail)*VoxelAuxRecordBytes > resources.Auxiliary {
				t.Fatalf("actual allocation tails escaped admitted buffers: sector=%d brick=%d auxiliary=%d resources=%+v", m.SectorAlloc.Tail, m.BrickAlloc.Tail, m.VoxelAuxAlloc.Tail, resources)
			}
			if resources.Auxiliary != finalAuxiliaryBytes || m.SectorAlloc.Tail != 2 || m.BrickAlloc.Tail != 2 || m.VoxelAuxAlloc.Tail != 128 {
				t.Fatalf("final two-sector capacity not established: sector=%d brick=%d auxiliary=%d resources=%+v", m.SectorAlloc.Tail, m.BrickAlloc.Tail, m.VoxelAuxAlloc.Tail, resources)
			}
			for _, obj := range []*core.VoxelObject{a, b} {
				if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
					t.Fatal("fitting replacement/new-adopter cohort did not reach ready")
				}
			}
			if found, value := a.XBrickMap.GetVoxel(0, 0, 0); !found || value != 3 {
				t.Fatal("replacement lost its independent CPU material")
			}
			if found, value := b.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
				t.Fatal("new adopter lost the old shared CPU material")
			}
			m.RetainVoxelMap(b.XBrickMap)
			m.evictRetainedVoxelMaps(map[*volume.XBrickMap]bool{a.XBrickMap: true, b.XBrickMap: true})
			want := uint64(256 + 32 + 64*BrickRecordSize + 64*(VoxelAuxRecordBytes+512))
			if got := m.RetainedVoxelMapStats().Bytes; got != want {
				t.Fatalf("new adopter lost assigned shared payload/auxiliary ownership: %d, want %d", got, want)
			}
		})
	}
}

func TestS1iRegressionRequiredMaterialHardRefusalPreservesFreshAuthority(t *testing.T) {
	m, resources := s1iManager(), s1iResources(0)
	const materialLimit = 64 * materialBlockCapacity * 64
	resources.MaxBufferBytes, resources.MaxStorageBytes, resources.MaxUniformBytes = materialLimit, materialLimit, 256
	fallback := s1iObject(10, 1, false)
	scene := &core.Scene{Objects: []*core.VoxelObject{fallback}}
	for i := 1; i < 64; i++ {
		obj := scheduleObject(uint32(30 + i))
		obj.XBrickMap, obj.MaterialTable = fallback.XBrickMap, fallback.MaterialTable
		scene.Objects = append(scene.Objects, obj)
	}
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if resources.Material != materialLimit || len(m.MaterialAllocations) != 64 {
		t.Fatal("fixture did not fill the exact legal material buffer")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(fallback, fallback.XBrickMap, fallback.XBrickMap.Revision); !ready {
		t.Fatal("existing shared fallback never reached ready")
	}
	fresh := s1iObject(999, 1, false)
	scene.Objects = append(scene.Objects, fresh)
	dirtySectors, dirtyBricks := maps.Clone(fresh.XBrickMap.DirtySectors), maps.Clone(fresh.XBrickMap.DirtyBricks)
	revision := fresh.XBrickMap.Revision
	for _, work := range s1iRun(t, m, scene, &resources, s1iGrow(&resources)) {
		if work.object == fresh || work.targetMap() == fresh.XBrickMap {
			t.Fatal("hard-refused required material owner reached a geometry/content writer")
		}
	}
	if m.Allocations[fresh.XBrickMap] != nil || m.MaterialAllocations[fresh] != nil || !fresh.XBrickMap.StructureDirty || !reflect.DeepEqual(dirtySectors, fresh.XBrickMap.DirtySectors) || !reflect.DeepEqual(dirtyBricks, fresh.XBrickMap.DirtyBricks) || fresh.XBrickMap.Revision != revision {
		t.Fatal("hard material refusal assigned fresh ownership or acknowledged CPU dirty authority")
	}
	if found, value := fresh.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatal("hard material refusal changed CPU geometry")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(fresh, fresh.XBrickMap, revision); ready {
		t.Fatal("hard-refused required material owner reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(fallback, fallback.XBrickMap, fallback.XBrickMap.Revision); !ready {
		t.Fatal("hard material refusal invalidated the already ready fallback")
	}
	if s := m.VoxelGPUAdmissionStats(); s.HardLimitDeferredMaps != 1 {
		t.Fatalf("hard material refusal lost map-level diagnostic: %+v", s)
	}
}
