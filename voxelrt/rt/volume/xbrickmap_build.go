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
	// Fresh private bricks have no auxiliary data, so each halo key needs marking once.
	batch := voxelEditBatch{halos: x.DirtyBricks}
	var sector *Sector
	var brick *Brick
	var sectorKey [3]int
	var brickKey [6]int
	var cached bool
	var haloClasses uint8
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
		if !cached || sKey != sectorKey {
			sector = x.Sectors[sKey]
			sectorKey = sKey
		}
		if !cached || bKey != brickKey {
			brick = nil
			if sector != nil {
				brick = sector.GetBrick(bKey[3], bKey[4], bKey[5])
			}
			brickKey = bKey
			haloClasses = 0
			cached = true
		}
		current := uint8(0)
		if brick != nil {
			current = brick.VoxelValue(vx, vy, vz)
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
		if VoxelNormalExtendedSurfaceFitRadius == BrickSize/2 {
			// With a half-brick radius, each local half on an axis has the
			// same neighboring brick range. Deduplicate exact halo classes,
			// retaining transient changes without filling sparse gaps.
			class := vx/(BrickSize/2) + vy/(BrickSize/2)*2 + vz/(BrickSize/2)*4
			bit := uint8(1) << class
			if haloClasses&bit == 0 {
				x.markVoxelNormalHaloDirtyBatched(w.X, w.Y, w.Z, &batch)
				haloClasses |= bit
			}
		} else {
			x.markVoxelNormalHaloDirtyBatched(w.X, w.Y, w.Z, &batch)
		}
		if brick.IsEmpty() {
			sector.RemoveBrickIfEmpty(bKey[3], bKey[4], bKey[5])
			x.DirtySectors[sKey] = true
			if sector.IsEmpty() {
				delete(x.Sectors, sKey)
				sector = nil
			}
			brick = nil
		}
	}
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.RefreshMaterialFlags()
		}
	}
	return x
}
