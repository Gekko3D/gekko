package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// This transient inventory contains only prospective complete upload units.
// Clean allocated maps contribute no sector walk; their snapshot edges already
// protect current auxiliary owners through voxelOwnership.
type auxiliaryUploadUnit struct {
	work    voxelUploadWork
	targets map[*volume.Brick]bool
}
type auxiliaryDemandInventory struct {
	units    map[*volume.XBrickMap][]auxiliaryUploadUnit
	future   map[*volume.Brick]bool
	unitUses map[*volume.Brick]int
	legacy   bool
}

func (m *GpuBufferManager) auxiliaryDemand(scene *core.Scene) *auxiliaryDemandInventory {
	inventory := &auxiliaryDemandInventory{units: make(map[*volume.XBrickMap][]auxiliaryUploadUnit), future: make(map[*volume.Brick]bool), unitUses: make(map[*volume.Brick]int), legacy: m.voxelOwnership.legacy}
	if inventory.legacy {
		return inventory
	}
	seen := make(map[*volume.XBrickMap]bool)
	for _, target := range voxelServiceTargets(scene) {
		xbm := target.mapRef
		if seen[xbm] {
			continue
		}
		seen[xbm] = true
		alloc := m.Allocations[xbm]
		structural := alloc == nil || xbm.StructureDirty
		appendUnit := func(key [3]int, start, end int) {
			sector := xbm.Sectors[key]
			if sector == nil {
				return
			}
			work := voxelUploadWork{kind: voxelUploadSector, object: target.object, target: xbm, sectorKey: key}
			if end-start == 1 {
				work.kind = voxelUploadBrick
				work.brickKey = [6]int{key[0], key[1], key[2], start % 4, (start / 4) % 4, start / 16}
			}
			unit := auxiliaryUploadUnit{work: work, targets: make(map[*volume.Brick]bool)}
			for index := start; index < end; index++ {
				if brick := sector.GetBrick(index%4, (index/4)%4, index/16); brick != nil {
					unit.targets[brick] = true
				}
			}
			for brick := range unit.targets {
				inventory.future[brick] = true
				inventory.unitUses[brick]++
			}
			inventory.units[xbm] = append(inventory.units[xbm], unit)
		}
		fullUnits := make(map[[3]int]bool)
		if structural {
			// Structural preparation may inspect all desired sector pointers, but an
			// unchanged sector retains its actual dirty-unit granularity. Prospective
			// membership protects release credits even outside an uploaded unit.
			for key, sector := range xbm.Sectors {
				if sector == nil {
					continue
				}
				for index := 0; index < 64; index++ {
					if brick := sector.GetBrick(index%4, (index/4)%4, index/16); brick != nil {
						inventory.future[brick] = true
					}
				}
				if alloc == nil || alloc.Sectors[key] != sector || xbm.DirtySectors[key] {
					appendUnit(key, 0, 64)
					fullUnits[key] = true
				}
			}
		} else {
			for key, dirty := range xbm.DirtySectors {
				if dirty {
					appendUnit(key, 0, 64)
					fullUnits[key] = true
				}
			}
		}
		for key, dirty := range xbm.DirtyBricks {
			sectorKey := [3]int{key[0], key[1], key[2]}
			if !dirty || fullUnits[sectorKey] || key[3] < 0 || key[3] >= 4 || key[4] < 0 || key[4] >= 4 || key[5] < 0 || key[5] >= 4 {
				continue
			}
			index := key[3] + key[4]*4 + key[5]*16
			appendUnit(sectorKey, index, index+1)
		}
	}
	return inventory
}

type auxiliaryReferenceChange struct {
	brick    *volume.Brick
	previous int
	existed  bool
}

func (p *voxelAdmissionPlan) addAuxiliary(m *GpuBufferManager, xbm *volume.XBrickMap) {
	inventory := p.auxiliaryDemand
	if inventory == nil || inventory.legacy {
		return
	}
	alloc := m.Allocations[xbm]
	// All admitted structural removals happen before any additions. Credit only
	// mapped slots whose final snapshot reference is guaranteed to disappear and
	// whose pointer is absent from every prospective unit (including deferred ones).
	if alloc != nil && xbm.StructureDirty {
		for key, sector := range alloc.Sectors {
			if xbm.Sectors[key] == sector {
				continue
			}
			pointers := alloc.Bricks[key]
			if pointers == nil {
				continue
			}
			for _, brick := range pointers {
				if brick == nil || inventory.future[brick] {
					continue
				}
				if _, mapped := m.BrickToAuxSlot[brick]; !mapped {
					continue
				}
				refs, exists := p.auxiliaryRemovedReferences[brick]
				p.auxiliaryReferenceJournal = append(p.auxiliaryReferenceJournal, auxiliaryReferenceChange{brick, refs, exists})
				refs++
				p.auxiliaryRemovedReferences[brick] = refs
				if refs == m.voxelOwnership.bricks[brick] && !p.auxiliaryReleased[brick] {
					p.auxiliaryReleased[brick] = true
					p.auxiliaryReleaseJournal = append(p.auxiliaryReleaseJournal, brick)
					p.auxiliaryFree++
				}
			}
		}
	}
	for _, unit := range inventory.units[xbm] {
		var needed, exclusive uint64
		for brick := range unit.targets {
			if !resolveBrickUploadMode(brick.Flags).usesAux {
				continue
			}
			if _, mapped := m.BrickToAuxSlot[brick]; mapped {
				continue
			}
			if p.auxiliaryReserved[brick] {
				continue
			}
			p.auxiliaryReserved[brick] = true
			p.auxiliaryReservationJournal = append(p.auxiliaryReservationJournal, brick)
			needed++
			if inventory.unitUses[brick] == 1 {
				exclusive++
			}
		}
		// Unit-local releases cannot finance another unit or a shared fresh pointer.
		// Their surplus stays unavailable until execution actually performs release.
		var releases uint64
		if alloc != nil && alloc.Sectors[unit.work.sectorCoordinate()] == xbm.Sectors[unit.work.sectorCoordinate()] && alloc.Bricks[unit.work.sectorCoordinate()] != nil && exclusive != 0 {
			_, old := m.voxelUploadReleases(unit.work)
			for brick := range old {
				if !inventory.future[brick] {
					releases++
				}
			}
		}
		consumeVoxelSlots(&p.auxiliaryTail, &p.auxiliaryFree, needed-min(exclusive, releases))
	}
}

// Admission cannot foresee every normal-halo or externally injected dirty
// demand. Qualify the actual complete unit against current physical capacity
// immediately before any records, releases or auxiliary writes are queued.
func (m *GpuBufferManager) voxelUploadAuxiliaryFits(work voxelUploadWork) bool {
	if m.voxelNative == nil {
		return true
	} // Legacy headless executors own capacity.
	capacity := m.voxelNative.BufferSize(m.DenseOccupancyBuf) / VoxelAuxRecordBytes
	available := uint64(0)
	for _, slot := range m.VoxelAuxAlloc.Free {
		if uint64(slot) < capacity {
			available++
		}
	}
	if uint64(m.VoxelAuxAlloc.Tail) < capacity {
		available += capacity - uint64(m.VoxelAuxAlloc.Tail)
	}
	_, releases := m.voxelUploadReleases(work)
	for brick := range releases {
		if slot, exists := m.BrickToAuxSlot[brick]; exists && uint64(slot) < capacity {
			available++
		}
	}
	needed := make(map[*volume.Brick]bool)
	sector := work.targetMap().Sectors[work.sectorCoordinate()]
	if sector == nil {
		return false
	}
	start, end := work.brickRange()
	for index := start; index < end; index++ {
		brick := sector.GetBrick(index%4, (index/4)%4, index/16)
		if brick == nil || !resolveBrickUploadMode(brick.Flags).usesAux {
			continue
		}
		if _, exists := m.BrickToAuxSlot[brick]; !exists {
			needed[brick] = true
		}
	}
	return uint64(len(needed)) <= available
}
