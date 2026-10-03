package volume

import "iter"

// VoxelWrite is an ordered assignment of a material value to one voxel.
// Value zero removes the voxel.
type VoxelWrite struct {
	X, Y, Z int
	Value   uint8
}

// BuildXBrickMap consumes writes synchronously once into a fresh editable map.
// A nil sequence is empty. Contents, revisions and dirty coverage follow ordered
// SetVoxel calls, while material flags are finalized once per surviving brick.
func BuildXBrickMap(writes iter.Seq[VoxelWrite]) *XBrickMap {
	x := NewXBrickMap()
	if writes == nil {
		return x
	}
	for w := range writes {
		sKey, bKey := sectorBrickKeyForVoxel(w.X, w.Y, w.Z)
		vx, vy, vz := w.X%BrickSize, w.Y%BrickSize, w.Z%BrickSize
		if vx < 0 {
			vx += BrickSize
		}
		if vy < 0 {
			vy += BrickSize
		}
		if vz < 0 {
			vz += BrickSize
		}
		sector := x.Sectors[sKey]
		var brick *Brick
		if sector != nil {
			brick = sector.GetBrick(bKey[3], bKey[4], bKey[5])
		}
		current := uint8(0)
		if brick != nil {
			current = brick.Payload[vx][vy][vz]
		}
		if current == w.Value {
			continue
		}

		x.Revision++
		x.SectorRevisions[sKey] = x.Revision
		if brick == nil {
			if sector == nil {
				sector = NewSector(sKey[0], sKey[1], sKey[2])
				x.Sectors[sKey] = sector
			}
			brick, _ = sector.GetOrCreateBrick(bKey[3], bKey[4], bKey[5])
			x.DirtySectors[sKey] = true
		}
		brick.SetVoxel(vx, vy, vz, w.Value)
		// Record each changed write, including transient/deleted geometry, so
		// adjacent normal uploads retain the same coverage as live edits.
		x.markVoxelNormalHaloDirty(w.X, w.Y, w.Z)
		if brick.IsEmpty() {
			sector.RemoveBrickIfEmpty(bKey[3], bKey[4], bKey[5])
			x.DirtySectors[sKey] = true
			if sector.IsEmpty() {
				delete(x.Sectors, sKey)
			}
		}
	}
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.RefreshMaterialFlags()
		}
	}
	return x
}
