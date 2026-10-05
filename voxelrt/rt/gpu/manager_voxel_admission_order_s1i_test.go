package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestS1iRegressionNewAdopterPrecedesReplacementAdmission(t *testing.T) {
	m, resources := s1iManager(), s1iResources(0)
	const finalAuxiliaryBytes = 2 * 64 * VoxelAuxRecordBytes
	resources.SectorTable, resources.BrickTable, resources.Auxiliary = 256, 64*BrickRecordSize, 64*VoxelAuxRecordBytes
	resources.Material, resources.SectorGrid, resources.DirectLookup, resources.SectorGridParams = 2*materialBlockCapacity*64, 32768, 256, 256
	resources.MaxBufferBytes, resources.MaxStorageBytes, resources.MaxUniformBytes = finalAuxiliaryBytes, finalAuxiliaryBytes, 256
	a := s1iObject(10, 1, false)
	schedulePutBrick(a, [6]int{}, scheduleBrick("mixed"))
	scene := &core.Scene{Objects: []*core.VoxelObject{a}}
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if ready, _, _ := m.RenderVoxelObjectReady(a, a.XBrickMap, a.XBrickMap.Revision); !ready || resources.BrickTable != 64*BrickRecordSize {
		t.Fatal("fixture did not establish the ready old sector's exact brick-table capacity")
	}
	oldSector := a.XBrickMap.Sectors[[3]int{}]
	fresh := s1iObject(50, 1, false)
	schedulePutBrick(fresh, [6]int{}, scheduleBrick("mixed"))
	a.XBrickMap.Sectors[[3]int{}] = fresh.XBrickMap.Sectors[[3]int{}]
	a.XBrickMap.StructureDirty = true
	a.XBrickMap.Revision++
	a.VoxelUploadOrder = 2
	b := s1iObject(20, 0, false)
	b.XBrickMap.Sectors[[3]int{}] = oldSector
	b.VoxelUploadOrder = 1
	// Scene insertion and deterministic admission order differ: B first adopts
	// S, then A replaces its old S snapshot with T. Both final sector tables
	// must fit, including the one packed occupied record from each sector.
	scene.Objects = []*core.VoxelObject{a, b}
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if uint64(m.SectorAlloc.Tail)*32 > resources.SectorTable || m.brickRanges.tail*BrickRecordSize > resources.BrickTable || uint64(m.VoxelAuxAlloc.Tail)*VoxelAuxRecordBytes > resources.Auxiliary {
		t.Fatalf("reverse admission order escaped physical buffer ranges: sector=%d brick=%d auxiliary=%d resources=%+v", m.SectorAlloc.Tail, m.brickRanges.tail, m.VoxelAuxAlloc.Tail, resources)
	}
	if m.SectorAlloc.Tail != 2 || m.brickRanges.tail != 2 {
		t.Fatal("final replacement/adopter maps did not receive their two sector headers and packed rows")
	}
	for _, obj := range []*core.VoxelObject{a, b} {
		if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
			t.Fatal("fitting reverse-order replacement/adopter cohort did not reach ready")
		}
	}
}
