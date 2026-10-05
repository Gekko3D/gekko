package volume

import "iter"

// VoxelColumn fills a uniform vertical prefix [0, FilledVoxels) at X, Z.
type VoxelColumn struct {
	X, Z         int
	FilledVoxels int
}

// BuildXBrickMapColumns consumes columns synchronously once into a fresh
// editable map. A nil sequence is empty. Contents, revisions and dirty coverage
// follow ordered SetVoxel calls with value, including overlapping prefixes.
// Material flags are finalized once per brick before return.
func BuildXBrickMapColumns(columns iter.Seq[VoxelColumn], value uint8) *XBrickMap {
	x := NewXBrickMap()
	if columns == nil {
		return x
	}
	batch := voxelEditBatch{halos: x.DirtyBricks}
	for column := range columns {
		if value == 0 || column.FilledVoxels <= 0 {
			continue
		}
		vx, vz := column.X%BrickSize, column.Z%BrickSize
		if vx < 0 {
			vx += BrickSize
		}
		if vz < 0 {
			vz += BrickSize
		}
		for y := 0; y < column.FilledVoxels; {
			vy := y % BrickSize
			count := min(BrickSize-vy, column.FilledVoxels-y)
			sKey, bKey := sectorBrickKeyForVoxel(column.X, y, column.Z)
			sector := x.Sectors[sKey]
			if sector == nil {
				sector = NewSector(sKey[0], sKey[1], sKey[2])
				x.Sectors[sKey] = sector
			}
			brick, isNew := sector.GetOrCreateBrick(bKey[3], bKey[4], bKey[5])
			if isNew {
				x.DirtySectors[sKey] = true
			}
			// This private map contains only prefixes of the same material.
			// Previously filled cells precede the newly added suffix in a run.
			first := vy
			for first < vy+count && brick.Payload[vx][first][vz] == value {
				first++
			}
			if first == vy+count {
				y += count
				continue
			}
			for localY := first; localY < vy+count; localY++ {
				brick.Payload[vx][localY][vz] = value
			}
			for microY := first / MicroSize; microY <= (vy+count-1)/MicroSize; microY++ {
				bit := vx/MicroSize + microY*4 + vz/MicroSize*16
				brick.OccupancyMask64 |= uint64(1) << bit
			}
			x.Revision += uint64(vy + count - first)
			x.SectorRevisions[sKey] = x.Revision
			// At fixed X/Z the endpoint halo ranges both include this owning
			// brick. Their union covers exactly the intervening voxel halos.
			x.markVoxelNormalHaloDirtyBatched(column.X, y+first-vy, column.Z, &batch)
			if first < vy+count-1 {
				x.markVoxelNormalHaloDirtyBatched(column.X, y+count-1, column.Z, &batch)
			}
			y += count
		}
	}
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.RefreshMaterialFlags()
		}
	}
	return x
}
