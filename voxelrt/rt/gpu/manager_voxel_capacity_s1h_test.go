package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1hCapacityObject(sectors int) *core.VoxelObject {
	obj := core.NewVoxelObject()
	for i := 0; i < sectors; i++ {
		obj.XBrickMap.SetVoxel(i*volume.SectorSize, 0, 0, 1)
	}
	return obj
}

func TestS1hVoxelCapacityPlanningSkipsCleanResidentSectors(t *testing.T) {
	const residentMaps, sectorsPerMap = 32, 32
	m := s2dManager()
	scene := &core.Scene{}
	for i := 0; i < residentMaps; i++ {
		scene.Objects = append(scene.Objects, s1hCapacityObject(sectorsPerMap))
	}
	// Use the actual CPU structural preparation path to establish assignments.
	// Capacity planning itself requires no GPU device, buffers or queue writes.
	m.prepareVoxelStructureDirtyState(scene)
	for _, obj := range scene.Objects {
		obj.XBrickMap.ClearDirty()
	}
	sectorTail, brickTail := m.SectorAlloc.Tail, m.BrickAlloc.Tail
	arrival := s1hCapacityObject(3)
	arrival.XBrickMap.ClearDirty()
	scene.Objects = append(scene.Objects, arrival)
	sectors, bricks := m.voxelAllocationRequirements(scene)
	if sectors != sectorTail+3 || bricks != brickTail*64+3*64 {
		t.Fatalf("arrival capacity=(%d,%d), want (%d,%d)", sectors, bricks, sectorTail+3, brickTail*64+3*64)
	}
	if visits := m.VoxelCapacityPlanningSectorVisitsLastUpdate; visits != 3 {
		t.Fatalf("small arrival visited %d sector entries; want 3 despite %d clean resident sectors", visits, residentMaps*sectorsPerMap)
	}
	// Planning is observational: repeated planning still needs the arrival.
	sectors, bricks = m.voxelAllocationRequirements(scene)
	if sectors != sectorTail+3 || bricks != brickTail*64+3*64 || m.VoxelCapacityPlanningSectorVisitsLastUpdate != 3 {
		t.Fatalf("repeated planning changed requirements or accumulated visits: sectors=%d bricks=%d visits=%d", sectors, bricks, m.VoxelCapacityPlanningSectorVisitsLastUpdate)
	}
	// An idle invocation resets its diagnostic even after an arrival frame.
	scene.Objects = scene.Objects[:residentMaps]
	sectors, bricks = m.voxelAllocationRequirements(scene)
	if sectors != sectorTail || bricks != brickTail*64 || m.VoxelCapacityPlanningSectorVisitsLastUpdate != 0 {
		t.Fatalf("idle planning did not preserve tails/reset work: sectors=%d bricks=%d visits=%d", sectors, bricks, m.VoxelCapacityPlanningSectorVisitsLastUpdate)
	}
}

func TestS1hVoxelCapacityPlanningPointerChangesAndSharedHiddenMaps(t *testing.T) {
	m := s2dManager()
	changed, clean := s1hCapacityObject(1), s1hCapacityObject(1)
	m.prepareVoxelStructureDirtyState(&core.Scene{Objects: []*core.VoxelObject{changed, clean}})
	changed.XBrickMap.ClearDirty()
	clean.XBrickMap.ClearDirty()
	sectorTail, brickTail := m.SectorAlloc.Tail, m.BrickAlloc.Tail
	replacement := s1hCapacityObject(1).XBrickMap.Sectors[[3]int{}]
	// Keep the sector count and coordinate unchanged but replace its pointer.
	changed.XBrickMap.Sectors[[3]int{}] = replacement
	changed.XBrickMap.StructureDirty = true
	assigned := clean.XBrickMap.Sectors[[3]int{}]
	newSector := s1hCapacityObject(1).XBrickMap.Sectors[[3]int{}]
	hidden := core.NewVoxelObject()
	hidden.RenderEnabled = false
	hidden.VoxelUploadPriority = core.VoxelUploadPriorityPrefetch
	hidden.XBrickMap.Sectors = map[[3]int]*volume.Sector{
		{}: assigned, {1, 0, 0}: replacement, {2, 0, 0}: replacement, {3, 0, 0}: newSector,
	}
	alias := core.NewVoxelObject()
	alias.XBrickMap = hidden.XBrickMap
	nilMap := core.NewVoxelObject()
	nilMap.XBrickMap = nil
	scene := &core.Scene{Objects: []*core.VoxelObject{nil, nilMap, clean, changed, changed, hidden, alias}}
	sectors, bricks := m.voxelAllocationRequirements(scene)
	// The assigned pointer needs no capacity. The replacement is shared by
	// both eligible maps and two coordinates, so only two new sectors remain.
	if sectors != sectorTail+2 || bricks != brickTail*64+2*64 {
		t.Fatalf("shared replacement capacity=(%d,%d), want (%d,%d)", sectors, bricks, sectorTail+2, brickTail*64+2*64)
	}
	if visits := m.VoxelCapacityPlanningSectorVisitsLastUpdate; visits != 5 {
		t.Fatalf("unique eligible maps must visit 1+4 entries, got %d", visits)
	}
	// A clean assigned map, nil object and nil geometry are all idle inputs.
	sectors, bricks = m.voxelAllocationRequirements(&core.Scene{Objects: []*core.VoxelObject{nil, nilMap, clean}})
	if sectors != sectorTail || bricks != brickTail*64 || m.VoxelCapacityPlanningSectorVisitsLastUpdate != 0 {
		t.Fatalf("idle/nil inputs changed capacity or retained visits: sectors=%d bricks=%d visits=%d", sectors, bricks, m.VoxelCapacityPlanningSectorVisitsLastUpdate)
	}
}
