package gekko

import (
	"sort"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func VoxelObjectSnapshotFromXBrickMap(xbm *volume.XBrickMap) *content.VoxelObjectSnapshotDef {
	snapshot := &content.VoxelObjectSnapshotDef{
		SchemaVersion: content.CurrentVoxelObjectSnapshotSchemaVersion,
	}
	if xbm == nil {
		return snapshot
	}
	for sKey, sector := range xbm.Sectors {
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) == 0 {
				continue
			}
			bx, by, bz := i%4, (i/4)%4, i/16
			brick := sector.GetBrick(bx, by, bz)
			if brick == nil {
				continue
			}
			originX := sKey[0]*volume.SectorSize + bx*volume.BrickSize
			originY := sKey[1]*volume.SectorSize + by*volume.BrickSize
			originZ := sKey[2]*volume.SectorSize + bz*volume.BrickSize
			for vz := 0; vz < volume.BrickSize; vz++ {
				for vy := 0; vy < volume.BrickSize; vy++ {
					for vx := 0; vx < volume.BrickSize; vx++ {
						val := brick.Payload[vx][vy][vz]
						if val == 0 {
							continue
						}
						snapshot.Voxels = append(snapshot.Voxels, content.VoxelObjectVoxelDef{
							X:     originX + vx,
							Y:     originY + vy,
							Z:     originZ + vz,
							Value: val,
						})
					}
				}
			}
		}
	}
	sort.Slice(snapshot.Voxels, func(i, j int) bool {
		if snapshot.Voxels[i].X != snapshot.Voxels[j].X {
			return snapshot.Voxels[i].X < snapshot.Voxels[j].X
		}
		if snapshot.Voxels[i].Y != snapshot.Voxels[j].Y {
			return snapshot.Voxels[i].Y < snapshot.Voxels[j].Y
		}
		return snapshot.Voxels[i].Z < snapshot.Voxels[j].Z
	})
	return snapshot
}

func XBrickMapFromVoxelObjectSnapshot(def *content.VoxelObjectSnapshotDef) *volume.XBrickMap {
	xbm := volume.NewXBrickMap()
	if def == nil {
		xbm.ClearDirty()
		return xbm
	}
	for _, voxel := range def.Voxels {
		if voxel.Value == 0 {
			continue
		}
		xbm.SetVoxel(voxel.X, voxel.Y, voxel.Z, voxel.Value)
	}
	xbm.ClearDirty()
	return xbm
}

func terrainChunkDefFromXBrickMap(terrainID string, coord content.TerrainChunkCoordDef, chunkSize int, voxelResolution float32, xbm *volume.XBrickMap) *content.TerrainChunkDef {
	chunk := &content.TerrainChunkDef{
		SchemaVersion:   content.CurrentTerrainChunkSchemaVersion,
		TerrainID:       terrainID,
		Coord:           coord,
		ChunkSize:       chunkSize,
		VoxelResolution: voxelResolution,
		SolidValue:      content.DefaultTerrainChunkSolidValue,
	}
	if xbm == nil {
		return chunk
	}

	min, max := xbm.ComputeAABB()
	maxY := int(max.Y())
	foundSolid := false
	for x := 0; x < chunkSize; x++ {
		for z := 0; z < chunkSize; z++ {
			filled := 0
			for y := 0; y < maxY; y++ {
				ok, val := xbm.GetVoxel(x, y, z)
				if !ok || val == 0 {
					continue
				}
				if !foundSolid {
					chunk.SolidValue = val
					foundSolid = true
				}
				if y+1 > filled {
					filled = y + 1
				}
			}
			if filled == 0 {
				continue
			}
			chunk.Columns = append(chunk.Columns, content.TerrainChunkColumnDef{
				X:            x,
				Z:            z,
				FilledVoxels: filled,
			})
			chunk.NonEmptyVoxelCount += filled
		}
	}
	if min == max {
		chunk.NonEmptyVoxelCount = 0
	}
	return chunk
}
