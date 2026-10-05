package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func c3h10PendingFixture(t *testing.T) (*GpuBufferManager, *core.Scene, *core.VoxelObject, *volume.XBrickMap) {
	t.Helper()
	m, scene, obj, coarse := c3h9GPUFixture(t)
	if !obj.SetPendingFullUpload() {
		t.Fatal("fixture pending full request rejected")
	}
	return m, scene, obj, coarse
}

func TestC3h10PendingCapacityStructureAndSelectedRecords(t *testing.T) {
	m, scene, obj, coarse := c3h10PendingFixture(t)
	full := obj.XBrickMap
	before := s3bClone(m.prepareSceneRecords(scene, mgl32.Vec3{}).visible)
	sectors, bricks := len(full.DirtySectors), len(full.DirtyBricks)
	structure := full.StructureDirty
	requiredSectors, requiredBricks := m.voxelAllocationRequirements(scene)
	if requiredSectors != m.SectorAlloc.Tail+uint32(len(full.Sectors)) || requiredBricks != m.BrickAlloc.Tail*64+uint32(len(full.Sectors))*64 {
		t.Fatal("pending authoritative full map absent from capacity plan", requiredSectors, requiredBricks)
	}
	if len(full.DirtySectors) != sectors || len(full.DirtyBricks) != bricks || full.StructureDirty != structure || m.Allocations[full] != nil {
		t.Fatal("capacity planning acknowledged or allocated pending content")
	}
	m.prepareVoxelStructureDirtyState(scene)
	if m.Allocations[coarse] == nil || m.Allocations[full] == nil || len(m.Allocations) != 2 || len(m.Allocations[full].Sectors) != len(full.Sectors) {
		t.Fatal("pending and selected map must coexist under the same allocation owner")
	}
	if len(full.DirtySectors) < sectors || len(full.DirtyBricks) < bricks {
		t.Fatal("structural preparation prematurely consumed pending content")
	}
	after := m.prepareSceneRecords(scene, mgl32.Vec3{}).visible
	if !bytes.Equal(before.instances, after.instances) || !bytes.Equal(before.params, after.params) || !bytes.Equal(before.bvh, after.bvh) || binary.LittleEndian.Uint32(after.params) != coarse.ID || len(scene.Objects) != 1 || scene.Objects[0] != obj {
		t.Fatal("fine staging changed selected GPU records or introduced helper objects")
	}
}

func TestC3h10PendingSharedBudgetDedupeAndReadiness(t *testing.T) {
	m, scene, obj, coarse := c3h10PendingFixture(t)
	full := obj.XBrickMap
	m.prepareVoxelStructureDirtyState(scene)
	// Only queue writes are substituted by scheduleRun. This latch represents
	// the lookup publication that UpdateScene performs before readiness checks.
	m.lastSectorGridTopologyRevision = m.sectorTopologyRevision
	if ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision); ready {
		t.Fatal("unwritten pending geometry reported ready")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision); !ready {
		t.Fatal("pending full work blocked ready coarse display")
	}
	// Default order falls back to map ID: fine 11 precedes coarse 22 unless
	// display-role precedence is applied before that fallback.
	c3h9MarkDirty(coarse)
	m.SetVoxelUploadBudget(VoxelUploadBudget{math.MaxUint64, 1, 64})
	work := scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].targetMap() != coarse || m.VoxelSectorsUploaded != 1 || len(full.DirtySectors) != len(full.Sectors) {
		t.Fatal("equal-priority selected display lost shared budget tie to pending fine", work)
	}
	// Two pending owners and a selected-full owner share physical geometry;
	// the best priority/order wins, while materials are still per object.
	alias := core.NewVoxelObject()
	alias.XBrickMap = full
	alias.MaterialTable = obj.MaterialTable
	alias.VoxelUploadPriority = obj.VoxelUploadPriority
	alias.VoxelUploadOrder = 1
	if !alias.SetRenderLOD2(coarse) || !alias.SetPendingFullUpload() {
		t.Fatal("pending alias fixture")
	}
	m.MaterialAllocations[alias] = m.MaterialAllocations[obj]
	selectedFull := core.NewVoxelObject()
	selectedFull.XBrickMap = full
	selectedFull.MaterialTable = obj.MaterialTable
	selectedFull.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	selectedFull.VoxelUploadOrder = 999
	m.MaterialAllocations[selectedFull] = m.MaterialAllocations[obj]
	scene.Objects = append(scene.Objects, alias, selectedFull)
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	work = scheduleRun(t, m, scene)
	if len(work) != len(full.Sectors) {
		t.Fatal("shared pending map uploaded geometry more than once", work)
	}
	for _, w := range work {
		if w.kind != voxelUploadSector || w.targetMap() != full || w.object != alias {
			t.Fatal("pending geometry did not use actual full target and best owner", w)
		}
	}
	// Simulate UpdateScene publishing the newly successful full-sector headers.
	m.lastSectorGridTopologyRevision = m.sectorTopologyRevision
	if ready, s, b := m.PendingFullVoxelObjectReady(obj, full, full.Revision); !ready || s != 0 || b != 0 {
		t.Fatal("queued exact pending full target not ready", ready, s, b)
	}
	for _, target := range []*volume.XBrickMap{nil, coarse, c3h9Map(99, [3]int{})} {
		if ready, _, _ := m.PendingFullVoxelObjectReady(obj, target, full.Revision); ready {
			t.Fatal("pending readiness accepted unrelated target")
		}
	}
	if ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision+1); ready {
		t.Fatal("pending readiness accepted stale revision")
	}
	if ready, _, _ := m.VoxelObjectReady(obj, full, full.Revision); !ready {
		t.Fatal("pending full upload broke existing authoritative readiness")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, full, full.Revision); ready || obj.RenderVoxelMap() != coarse {
		t.Fatal("fine readiness changed or certified selected display")
	}
	delete(m.MaterialAllocations, obj)
	work = scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].kind != voxelUploadMaterial || m.VoxelMaterialsUploaded != 1 {
		t.Fatal("two map roles duplicated object material upload", work)
	}
	obj.ClearRenderRepresentation()
	if obj.RenderVoxelMap() != full || obj.PendingFullUploadMap() != nil {
		t.Fatal("explicit promotion failed")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, full, full.Revision); !ready {
		t.Fatal("ready fine map not available after explicit promotion")
	}
	if ready, _, _ := m.PendingFullVoxelObjectReady(obj, full, full.Revision); ready {
		t.Fatal("cleared pending request remained ready")
	}
	for _, manager := range []*GpuBufferManager{nil, {}} {
		if ready, _, _ := manager.PendingFullVoxelObjectReady(nil, nil, 0); ready {
			t.Fatal("nil pending target ready")
		}
	}
}

func TestC3h10PendingCancelAndSameRevisionRestageInvalidateQueuedWork(t *testing.T) {
	for _, restage := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "cancel and restage"}[restage], func(t *testing.T) {
			m, scene, obj, coarse := c3h10PendingFixture(t)
			full := obj.XBrickMap
			m.prepareVoxelStructureDirtyState(scene)
			// Normalize orphan normal-halo markers with no executor; existing
			// scheduler cleanup is distinct from successful content acknowledgement.
			m.serviceVoxelUploads(scene, nil)
			sectors, bricks := make(map[[3]int]bool), make(map[[6]int]bool)
			for k, v := range full.DirtySectors {
				sectors[k] = v
			}
			for k, v := range full.DirtyBricks {
				bricks[k] = v
			}
			generation, revision := obj.PendingFullUploadGeneration(), full.Revision
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
				calls++
				if calls != 1 || w.targetMap() != full {
					t.Fatal("obsolete pending generation reached executor", w)
				}
				obj.ClearPendingFullUpload()
				if restage && (!obj.SetPendingFullUpload() || obj.PendingFullUploadGeneration() == generation) {
					t.Fatal("restaging reused request generation")
				}
				return true
			})
			if calls != 1 || m.VoxelSectorsUploaded != 1 || m.VoxelUploadBytes == 0 || !reflect.DeepEqual(full.DirtySectors, sectors) || !reflect.DeepEqual(full.DirtyBricks, bricks) || full.Revision != revision || obj.RenderVoxelMap() != coarse {
				t.Fatal("obsolete completion acknowledged full dirty queues or lost written budget")
			}
			if restage {
				if work := scheduleRun(t, m, scene); len(work) != len(full.Sectors) {
					t.Fatal("new pending generation did not retain every stale unit", work)
				}
			} else if work := scheduleRun(t, m, scene); len(work) != 0 {
				t.Fatal("cancelled fine target still scheduled", work)
			}
		})
	}
}

func TestC3h10PendingSelectedRoleWinsExplicitOrderBudgetTie(t *testing.T) {
	m, scene, obj, coarse := c3h10PendingFixture(t)
	m.prepareVoxelStructureDirtyState(scene)
	obj.VoxelUploadOrder = 50
	c3h9MarkDirty(coarse)
	m.SetVoxelUploadBudget(VoxelUploadBudget{math.MaxUint64, 1, 64})
	work := scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].targetMap() != coarse || len(obj.XBrickMap.DirtySectors) != len(obj.XBrickMap.Sectors) {
		t.Fatal("equal explicit owner order selected/pending tie displaced coarse work", work)
	}
}

func TestC3h10PendingSourceEditDuringExecutionPreservesNewDirtyQueues(t *testing.T) {
	m, scene, obj, _ := c3h10PendingFixture(t)
	full := obj.XBrickMap
	m.prepareVoxelStructureDirtyState(scene)
	m.serviceVoxelUploads(scene, nil)
	var sectors map[[3]int]bool
	var bricks map[[6]int]bool
	var writtenBytes uint64
	calls := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		calls++
		if calls != 1 || w.targetMap() != full {
			t.Fatal("obsolete pending source revision reached executor", w)
		}
		writtenBytes = w.bytes
		full.SetVoxel(97, 0, 0, 1)
		sectors, bricks = make(map[[3]int]bool), make(map[[6]int]bool)
		for k, v := range full.DirtySectors {
			sectors[k] = v
		}
		for k, v := range full.DirtyBricks {
			bricks[k] = v
		}
		return true
	})
	occupied, material := full.GetVoxel(97, 0, 0)
	if calls != 1 || m.VoxelUploadBytes != writtenBytes || m.VoxelSectorsUploaded != 1 || !reflect.DeepEqual(full.DirtySectors, sectors) || !reflect.DeepEqual(full.DirtyBricks, bricks) || !full.StructureDirty || !occupied || material != 1 || obj.XBrickMap != full {
		t.Fatal("old successful upload acknowledged new source edit or lost its written budget")
	}
	if obj.PendingFullUploadMap() != nil || obj.PendingFullUploadGeneration() != 0 || obj.RenderVoxelMap() != nil {
		t.Fatal("source edit left pending/display targets valid")
	}
}

func TestC3h10PendingOlderWorkWinsBeforeSelectedRoleTie(t *testing.T) {
	m, scene, obj, coarse := c3h10PendingFixture(t)
	m.prepareVoxelStructureDirtyState(scene)
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { return false })
	c3h9MarkDirty(coarse)
	m.SetVoxelUploadBudget(VoxelUploadBudget{math.MaxUint64, 1, 64})
	work := scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].targetMap() != obj.XBrickMap || len(coarse.DirtySectors) != 1 {
		t.Fatal("fresh selected work displaced older equal-priority pending work", work)
	}
}

func TestC3h10PendingLookupPublicationIncludesFullWhileRecordsStayCoarse(t *testing.T) {
	m, scene, obj, coarse := c3h9GPUFixture(t)
	if !m.sectorGridSelectionChanged(scene) || m.sectorGridSelectionChanged(scene) {
		t.Fatal("fixture stable selected snapshot")
	}
	if !obj.SetPendingFullUpload() || !m.sectorGridSelectionChanged(scene) || m.sectorGridSelectionChanged(scene) {
		t.Fatal("pending adoption absent or unstable in lookup ownership snapshot")
	}
	full := obj.XBrickMap
	full.ID++
	if !m.sectorGridSelectionChanged(scene) || m.sectorGridSelectionChanged(scene) {
		t.Fatal("pending mutable map ID absent from lookup snapshot")
	}
	m.prepareVoxelStructureDirtyState(scene)
	data := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 9)
	meta := m.Allocations[full].DirectLookup
	for coordinate := range full.Sectors {
		local := [3]uint32{uint32(int32(coordinate[0]) - meta.Origin[0]), uint32(int32(coordinate[1]) - meta.Origin[1]), uint32(int32(coordinate[2]) - meta.Origin[2])}
		index := meta.TableBase - 9 + flattenDirectSectorLookupIndex(local, meta.Extent)
		if binary.LittleEndian.Uint32(data[index*4:]) != DirectSectorLookupInvalid {
			t.Fatal("unpublished pending header entered direct lookup")
		}
	}
	// The fake executor models successful header writes before lookup publication.
	scheduleRun(t, m, scene)
	data = buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 9)
	for _, target := range []*volume.XBrickMap{coarse, full} {
		meta := m.Allocations[target].DirectLookup
		if meta.LookupMode == 0 || meta.TableBase < 9 {
			t.Fatal("missing target direct lookup", target.ID, meta)
		}
		for coordinate, sector := range target.Sectors {
			local := [3]uint32{uint32(int32(coordinate[0]) - meta.Origin[0]), uint32(int32(coordinate[1]) - meta.Origin[1]), uint32(int32(coordinate[2]) - meta.Origin[2])}
			index := meta.TableBase - 9 + flattenDirectSectorLookupIndex(local, meta.Extent)
			if int(index)*4+4 > len(data) || binary.LittleEndian.Uint32(data[index*4:]) != m.SectorToInfo[sector].SlotIndex {
				t.Fatal("pending/selected direct lookup encoded wrong physical sector", target.ID, coordinate)
			}
		}
	}
	if binary.LittleEndian.Uint32(m.prepareSceneRecords(scene, mgl32.Vec3{}).visible.params) != coarse.ID {
		t.Fatal("pending direct lookup changed selected parameter map")
	}
	obj.ClearPendingFullUpload()
	if !m.sectorGridSelectionChanged(scene) || m.sectorGridSelectionChanged(scene) {
		t.Fatal("cancelled pending target retained lookup publication ownership")
	}
}

func TestC3h10PendingDedupeUsesActualMapFallbackOrderAndBackpressure(t *testing.T) {
	m, scene, obj, _ := c3h10PendingFixture(t)
	full := obj.XBrickMap
	m.prepareVoxelStructureDirtyState(scene)
	alias := core.NewVoxelObject()
	alias.XBrickMap = full
	alias.MaterialTable = obj.MaterialTable
	// Its unrelated selected map ID must not improve ownership of the full map.
	if !alias.SetRenderLOD2(c3h9Map(1, [3]int{})) || !alias.SetPendingFullUpload() {
		t.Fatal("alias fixture")
	}
	m.MaterialAllocations[alias] = m.MaterialAllocations[obj]
	scene.Objects = append(scene.Objects, alias)
	m.prepareVoxelStructureDirtyState(scene)
	// Keep alias display clean, so only the shared fine target competes.
	alias.RenderVoxelMap().ClearDirty()
	m.serviceVoxelUploads(scene, nil)
	sectors, bricks := len(full.DirtySectors), len(full.DirtyBricks)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	if work := scheduleRun(t, m, scene); len(work) != 0 || len(full.DirtySectors) != sectors || len(full.DirtyBricks) != bricks || m.VoxelDirtySectorsPending != sectors || m.VoxelDirtyBricksPending != bricks {
		t.Fatal("paused global budget consumed or double-counted shared pending queues", work)
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { return false })
	if len(full.DirtySectors) != sectors || len(full.DirtyBricks) != bricks || m.VoxelUploadBytes != 0 {
		t.Fatal("refused pending execution consumed queues")
	}
	work := scheduleRun(t, m, scene)
	if len(work) != len(full.Sectors) {
		t.Fatal("pending shared map did not drain exactly once", work)
	}
	for _, w := range work {
		if w.object != obj || w.targetMap() != full {
			t.Fatal("fallback ownership used unrelated selected map ID", w)
		}
	}
}

func TestC3h10PendingNormalBakeSamplesFullTarget(t *testing.T) {
	_, scene, obj, coarse := c3h10PendingFixture(t)
	full := obj.XBrickMap
	brick := full.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if brick == nil || len(brick.PrecomputedAux) != 0 {
		t.Fatal("fixture must require runtime normal baking")
	}
	min, max := full.ComputeAABB()
	oracle := func(target *volume.XBrickMap) []byte {
		return volume.BuildVoxelAuxBytes(brick, [3]int{}, volume.VoxelNormalBakeOptions{BoundsMin: min, BoundsMax: max, HasBounds: true, SampleOccupancy: func(p [3]int) bool { occupied, _ := target.GetVoxel(p[0], p[1], p[2]); return occupied }})
	}
	want := oracle(full)
	if bytes.Equal(want, oracle(coarse)) {
		t.Fatal("fixture does not distinguish full and coarse occupancy normals")
	}
	got := buildVoxelAuxBytesForTarget(newVoxelNormalBakeContext(scene), obj, full, brick, [3]int{})
	if !bytes.Equal(got, want) || obj.RenderVoxelMap() != coarse || len(scene.Objects) != 1 || scene.Objects[0] != obj {
		t.Fatal("pending normals sampled coarse geometry or mutated the scene owner")
	}
}
