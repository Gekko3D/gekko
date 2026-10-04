package gpu

import (
	"fmt"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Substitute only physical writes. Preparation and successful completion run
// through the manager paths, including executors that update allocated pointers.
func s1l3WriteSlots(t testing.TB, m *GpuBufferManager, w voxelUploadWork) {
	t.Helper()
	if w.kind == voxelUploadMaterial {
		return
	}
	payload, auxiliary := m.voxelUploadReleases(w)
	for brick := range payload {
		m.releaseBrickSlot(brick)
	}
	for brick := range auxiliary {
		m.releaseVoxelAuxSlot(brick)
	}
	key := w.sectorCoordinate()
	sector := w.targetMap().Sectors[key]
	pointers := m.Allocations[w.targetMap()].Bricks[key]
	start, end := w.brickRange()
	for i := start; i < end; i++ {
		brick := sector.GetBrick(i%4, (i/4)%4, i/16)
		pointers[i] = brick
		if brick == nil {
			continue
		}
		if _, exists := m.BrickToAuxSlot[brick]; !exists {
			m.BrickToAuxSlot[brick] = m.VoxelAuxAlloc.Alloc()
		}
		if resolveBrickUploadMode(brick.Flags).usesPayload {
			if _, exists := m.BrickToSlot[brick]; !exists {
				slot, ok := m.allocPayloadSlot()
				if !ok {
					t.Fatal("admitted work exhausted payload capacity")
				}
				m.BrickToSlot[brick] = slot
			}
		}
	}
}

func s1l3PrepareAndService(t testing.TB, m *GpuBufferManager, scene *core.Scene, resources *voxelGPUResources) {
	t.Helper()
	m.prepareVoxelGPUAdmission(scene, resources, s1iGrow(resources))
	m.prepareVoxelStructureDirtyState(scene)
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		s1l3WriteSlots(t, m, w)
		return true
	})
}

// Raw index edits preserve the shared brick's authoritative contents.
func s1l3ClearIndex(obj *core.VoxelObject, index int) {
	sector := obj.XBrickMap.Sectors[[3]int{}]
	packed := sector.GetPackedIndex(index)
	sector.PackedBricks = append(sector.PackedBricks[:packed], sector.PackedBricks[packed+1:]...)
	sector.BrickMask64 &^= uint64(1) << index
	obj.XBrickMap.Revision++
	obj.XBrickMap.DirtyBricks[[6]int{0, 0, 0, index % 4, (index / 4) % 4, index / 16}] = true
}

func TestS1l3DuplicateIndicesAndMovedBrickKeepSlots(t *testing.T) {
	for _, wholeSector := range []bool{false, true} {
		t.Run(fmt.Sprintf("whole-sector=%v", wholeSector), func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			obj := s1iObject(10, 1, false)
			brick := scheduleBrick("mixed")
			schedulePutBrick(obj, [6]int{}, brick)
			schedulePutBrick(obj, [6]int{0, 0, 0, 1, 0, 0}, brick)
			scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
			s1l3PrepareAndService(t, m, scene, &resources)
			payload, auxiliary := m.BrickToSlot[brick], m.BrickToAuxSlot[brick]
			m.VoxelPayloadBricks = 1 // No spare payload slot can hide premature release.
			s1l3ClearIndex(obj, 0)
			s1l3PrepareAndService(t, m, scene, &resources)
			if got, ok := m.BrickToSlot[brick]; !ok || got != payload {
				t.Fatal("clearing one duplicate index released the surviving payload")
			}
			// The destination is not yet in the allocation snapshot. Admission
			// must protect the whole current sector when clearing the old index.
			schedulePutBrick(obj, [6]int{0, 0, 0, 2, 0, 0}, brick)
			s1l3ClearIndex(obj, 1)
			if wholeSector {
				obj.XBrickMap.DirtySectors[[3]int{}] = true
			}
			s1l3PrepareAndService(t, m, scene, &resources)
			if got, ok := m.BrickToSlot[brick]; !ok || got != payload {
				t.Fatal("moving a brick released its payload before the destination write")
			}
			if got, ok := m.BrickToAuxSlot[brick]; !ok || got != auxiliary {
				t.Fatal("duplicate removal or move released surviving auxiliary storage")
			}
			obj.XBrickMap.DirtyBricks[[6]int{0, 0, 0, 2, 0, 0}] = true
			s1l3PrepareAndService(t, m, scene, &resources)
			delete(obj.XBrickMap.Sectors, [3]int{})
			obj.XBrickMap.StructureDirty = true
			m.prepareVoxelStructureDirtyState(scene)
			if _, ok := m.BrickToSlot[brick]; ok {
				t.Fatal("final moved reference retained payload storage")
			}
			if _, ok := m.BrickToAuxSlot[brick]; ok {
				t.Fatal("final moved reference retained auxiliary storage")
			}
			if m.PayloadAlloc[payload.Page].Alloc() != payload.Slot || m.VoxelAuxAlloc.Alloc() != auxiliary {
				t.Fatal("final moved reference did not return its assigned slots")
			}
		})
	}
}

func TestS1l3UntrackedSnapshotsPreserveFinalOwnerLifetime(t *testing.T) {
	for _, source := range []string{"seeded", "foreign", "mixed seeded", "mixed foreign"} {
		t.Run(source, func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			obj := s1iObject(10, 1, false)
			brick := scheduleBrick("mixed")
			schedulePutBrick(obj, [6]int{}, brick)
			scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
			s1l3PrepareAndService(t, m, scene, &resources)
			sector := obj.XBrickMap.Sectors[[3]int{}]
			info, payload, auxiliary := m.SectorToInfo[sector], m.BrickToSlot[brick], m.BrickToAuxSlot[brick]
			alias := s1iObject(20, 0, false)
			alias.XBrickMap.Sectors[[3]int{}] = sector
			alias.XBrickMap.StructureDirty = true
			var snapshot *ObjectGpuAllocation
			if source == "foreign" || source == "mixed foreign" {
				foreign, storage := s1iManager(), s1iResources(0)
				s1l3PrepareAndService(t, foreign, &core.Scene{Objects: []*core.VoxelObject{alias}}, &storage)
				snapshot = foreign.Allocations[alias.XBrickMap]
			} else {
				snapshot = testObjectGpuAllocationForMap(alias.XBrickMap)
			}
			if source == "seeded" || source == "foreign" {
				// Replace the manager-created header with an untracked header.
				m.Allocations[obj.XBrickMap] = snapshot
			} else {
				m.Allocations[alias.XBrickMap] = snapshot
				m.releaseVoxelMapAllocation(obj.XBrickMap, m.Allocations[obj.XBrickMap])
				if got, ok := m.SectorToInfo[sector]; !ok || got != info {
					t.Fatal("tracked owner release lost the untracked surviving sector")
				}
				if got, ok := m.BrickToSlot[brick]; !ok || got != payload {
					t.Fatal("tracked owner release lost the untracked surviving payload")
				}
				obj = alias
			}
			// Release must follow the allocated snapshot after raw CPU mutation.
			obj.XBrickMap.Sectors = make(map[[3]int]*volume.Sector)
			m.releaseVoxelMapAllocation(obj.XBrickMap, m.Allocations[obj.XBrickMap])
			if len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 || len(m.BrickToAuxSlot) != 0 {
				t.Fatal("final untracked snapshot release left assigned resources")
			}
			if m.SectorAlloc.Alloc() != info.SlotIndex || m.BrickAlloc.Alloc() != info.BrickTableIndex/64 || m.PayloadAlloc[payload.Page].Alloc() != payload.Slot || m.VoxelAuxAlloc.Alloc() != auxiliary {
				t.Fatal("final untracked snapshot release did not return assigned slots")
			}
		})
	}
}

func TestS1l3ExecutorRetargetAndRefusalPreserveWrittenLifetime(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("successful-write=%v", success), func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			obj, alias := s1iObject(10, 1, false), s1iObject(20, 1, false)
			old, next := scheduleBrick("mixed"), scheduleBrick("mixed")
			schedulePutBrick(obj, [6]int{}, old)
			schedulePutBrick(alias, [6]int{}, old)
			scene := &core.Scene{Objects: []*core.VoxelObject{obj, alias}}
			s1l3PrepareAndService(t, m, scene, &resources)
			oldMap := obj.XBrickMap
			schedulePutBrick(obj, [6]int{}, next)
			oldMap.DirtyBricks[[6]int{}] = true
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
				calls++
				if success {
					s1l3WriteSlots(t, m, w)
					obj.XBrickMap = s1iObject(30, 0, false).XBrickMap
				}
				return success
			})
			if calls != 1 || !oldMap.DirtyBricks[[6]int{}] {
				t.Fatal("retargeted or refused work acknowledged current dirty authority")
			}
			m.releaseVoxelMapAllocation(alias.XBrickMap, m.Allocations[alias.XBrickMap])
			if _, exists := m.BrickToSlot[old]; exists == success {
				t.Fatal("release did not follow the successful/refused allocation snapshot")
			}
			if _, exists := m.BrickToSlot[next]; exists != success {
				t.Fatal("successful retarget lost written storage, or refusal assigned unwritten storage")
			}
			m.releaseVoxelMapAllocation(oldMap, m.Allocations[oldMap])
			if len(m.BrickToSlot) != 0 || len(m.BrickToAuxSlot) != 0 {
				t.Fatal("final written snapshot release leaked brick resources")
			}
		})
	}
}

func TestS1l3SharedSamePointerModeChangeKeepsOutsideAliasSlots(t *testing.T) {
	for _, mode := range []string{"uniform", "solid"} {
		t.Run(mode, func(t *testing.T) {
			m, resources := s1iManager(), s1iResources(0)
			obj, alias := s1iObject(10, 1, false), s1iObject(20, 1, false)
			brick := scheduleBrick("mixed")
			schedulePutBrick(obj, [6]int{}, brick)
			schedulePutBrick(alias, [6]int{}, brick)
			scene := &core.Scene{Objects: []*core.VoxelObject{obj, alias}}
			s1l3PrepareAndService(t, m, scene, &resources)
			payload, auxiliary := m.BrickToSlot[brick], m.BrickToAuxSlot[brick]
			if mode == "solid" {
				brick.Expand(1)
			} else {
				brick.SetVoxel(1, 0, 0, 1)
			}
			brick.RefreshMaterialFlags()
			obj.XBrickMap.Revision++
			obj.XBrickMap.DirtyBricks[[6]int{}] = true
			s1l3PrepareAndService(t, m, scene, &resources)
			if got, exists := m.BrickToSlot[brick]; !exists || got != payload {
				t.Fatal("same-pointer mode change reclaimed the outside alias's payload")
			}
			if got, exists := m.BrickToAuxSlot[brick]; !exists || got != auxiliary {
				t.Fatal("same-pointer mode change reclaimed the outside alias's auxiliary storage")
			}
			m.releaseVoxelMapAllocation(alias.XBrickMap, m.Allocations[alias.XBrickMap])
			scene.Objects = []*core.VoxelObject{obj}
			obj.XBrickMap.DirtyBricks[[6]int{}] = true
			s1l3PrepareAndService(t, m, scene, &resources)
			if _, exists := m.BrickToSlot[brick]; exists {
				t.Fatal("nonpayload rewrite retained payload after the outside alias disappeared")
			}
			if got, exists := m.BrickToAuxSlot[brick]; !exists || got != auxiliary {
				t.Fatal("nonpayload rewrite released the surviving brick's auxiliary storage")
			}
			m.releaseVoxelMapAllocation(obj.XBrickMap, m.Allocations[obj.XBrickMap])
			if _, exists := m.BrickToAuxSlot[brick]; exists {
				t.Fatal("final same-pointer owner retained auxiliary storage")
			}
			if m.PayloadAlloc[payload.Page].Alloc() != payload.Slot || m.VoxelAuxAlloc.Alloc() != auxiliary {
				t.Fatal("same-pointer mode change did not return its final assigned slots")
			}
		})
	}
}

func BenchmarkS1l3HotSectorRemoval(b *testing.B) {
	for _, sectors := range []int{1, 256, 4096} {
		b.Run(fmt.Sprintf("clean-sectors=%d", sectors), func(b *testing.B) {
			m, resources := s1iManager(), s1iResources(0)
			m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 40, MaxSectors: 1 << 20, MaxBricks: 1 << 24})
			hot, cold := s1iObject(10, 1, false), s1iObject(20, sectors, false)
			schedulePutBrick(hot, [6]int{}, scheduleBrick("mixed"))
			for i := 0; i < sectors; i++ {
				schedulePutBrick(cold, [6]int{i, 0, 0, 0, 0, 0}, scheduleBrick("uniform"))
			}
			scene := &core.Scene{Objects: []*core.VoxelObject{hot, cold}}
			s1l3PrepareAndService(b, m, scene, &resources)
			if cold.XBrickMap.StructureDirty || len(cold.XBrickMap.DirtySectors) != 0 || len(cold.XBrickMap.DirtyBricks) != 0 {
				b.Fatal("benchmark cold geometry did not complete service")
			}
			sector := hot.XBrickMap.Sectors[[3]int{}]
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				delete(hot.XBrickMap.Sectors, [3]int{})
				hot.XBrickMap.StructureDirty = true
				m.prepareVoxelStructureDirtyState(scene)
				b.StopTimer()
				hot.XBrickMap.Sectors[[3]int{}] = sector
				hot.XBrickMap.StructureDirty = true
				m.prepareVoxelStructureDirtyState(scene)
				m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
					s1l3WriteSlots(b, m, w)
					return true
				})
				b.StartTimer()
			}
		})
	}
}
