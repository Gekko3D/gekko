package volume

import "github.com/go-gl/mathgl/mgl32"

func (x *XBrickMap) SetVoxel(gx, gy, gz int, val uint8) {
	// GPU-first mode: queue edit on GPU instead of CPU update
	if x.GPUEditMode && x.gpuManager != nil {
		type EditQueuer interface {
			QueueEdit(x, y, z int, val uint8)
		}
		if mgr, ok := x.gpuManager.(EditQueuer); ok {
			mgr.QueueEdit(gx, gy, gz, val)
			// Continue to CPU update
		}
	}

	// CPU update path (original logic)
	sx, sy, sz := gx/SectorSize, gy/SectorSize, gz/SectorSize
	slx, sly, slz := gx%SectorSize, gy%SectorSize, gz%SectorSize
	if slx < 0 {
		slx += SectorSize
		sx--
	}
	if sly < 0 {
		sly += SectorSize
		sy--
	}
	if slz < 0 {
		slz += SectorSize
		sz--
	}

	bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
	vx, vy, vz := slx%BrickSize, sly%BrickSize, slz%BrickSize

	sKey := [3]int{sx, sy, sz}
	bKey := [6]int{sx, sy, sz, bx, by, bz}

	if val == 0 {
		if sector, ok := x.Sectors[sKey]; ok {
			brick := sector.GetBrick(bx, by, bz)
			if brick != nil {
				if brick.Flags&BrickFlagSolid != 0 {
					brick.Expand(uint8(brick.AtlasOffset))
				}

				brick.SetVoxel(vx, vy, vz, 0)
				brick.RefreshMaterialFlags()
				if !x.GPUEditMode {
					x.DirtySectors[sKey] = true
					x.DirtyBricks[bKey] = true
				}

				// Incremental AABB: Only mark dirty if removing a boundary voxel
				if !x.AABBDirty {
					if float32(gx) == x.CachedMin.X() || float32(gx) == x.CachedMax.X()-1 ||
						float32(gy) == x.CachedMin.Y() || float32(gy) == x.CachedMax.Y()-1 ||
						float32(gz) == x.CachedMin.Z() || float32(gz) == x.CachedMax.Z()-1 {
						x.AABBDirty = true
					}
				}

				sector.RemoveBrickIfEmpty(bx, by, bz)
				if sector.IsEmpty() {
					delete(x.Sectors, sKey)
					x.StructureDirty = true
				} else if brick.IsEmpty() {
					x.StructureDirty = true
				} else {
					// Try to re-compress? Or leave as sparse until full rebuild?
					// For simple editing, leave sparse.
				}
			}
		}
	} else {
		sector, ok := x.Sectors[sKey]
		if !ok {
			sector = NewSector(sx, sy, sz)
			x.Sectors[sKey] = sector
			x.StructureDirty = true
		}

		brick, isNew := sector.GetOrCreateBrick(bx, by, bz)
		if isNew {
			x.StructureDirty = true
		} else {
			if brick.Flags&BrickFlagSolid != 0 {
				if brick.AtlasOffset == uint32(val) {
					return // Already solid with this value
				}
				brick.Expand(uint8(brick.AtlasOffset))
			}
		}

		brick.SetVoxel(vx, vy, vz, val)
		brick.RefreshMaterialFlags()
		if !x.GPUEditMode {
			x.DirtySectors[sKey] = true
			x.DirtyBricks[bKey] = true
		}

		// Incremental AABB: Expand existing bounds or mark dirty if already dirty
		if !x.AABBDirty {
			if len(x.Sectors) == 1 && isNew { // First voxel (roughly)
				x.CachedMin = mgl32.Vec3{float32(gx), float32(gy), float32(gz)}
				x.CachedMax = mgl32.Vec3{float32(gx + 1), float32(gy + 1), float32(gz + 1)}
			} else {
				x.CachedMin = mgl32.Vec3{
					min(x.CachedMin.X(), float32(gx)),
					min(x.CachedMin.Y(), float32(gy)),
					min(x.CachedMin.Z(), float32(gz)),
				}
				x.CachedMax = mgl32.Vec3{
					max(x.CachedMax.X(), float32(gx+1)),
					max(x.CachedMax.Y(), float32(gy+1)),
					max(x.CachedMax.Z(), float32(gz+1)),
				}
			}
		}

	}
}

// EnableGPUEditing enables GPU-accelerated voxel editing
func (x *XBrickMap) EnableGPUEditing(mgr interface{}) {
	x.GPUEditMode = true
	x.gpuManager = mgr
}

// GetVoxel returns (found, value) for a voxel at global coordinates
func (x *XBrickMap) GetVoxel(gx, gy, gz int) (bool, uint8) {
	sx, sy, sz := gx/SectorSize, gy/SectorSize, gz/SectorSize
	slx, sly, slz := gx%SectorSize, gy%SectorSize, gz%SectorSize
	if slx < 0 {
		slx += SectorSize
		sx--
	}
	if sly < 0 {
		sly += SectorSize
		sy--
	}
	if slz < 0 {
		slz += SectorSize
		sz--
	}

	bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
	vx, vy, vz := slx%BrickSize, sly%BrickSize, slz%BrickSize

	sKey := [3]int{sx, sy, sz}

	sector, ok := x.Sectors[sKey]
	if !ok {
		return false, 0
	}

	brick := sector.GetBrick(bx, by, bz)
	if brick == nil {
		return false, 0
	}

	val := brick.Payload[vx][vy][vz]
	return val != 0, val
}

func (x *XBrickMap) Copy() *XBrickMap {
	newMap := NewXBrickMap()

	// Copy sectors
	for k, v := range x.Sectors {
		newMap.Sectors[k] = v.Copy()
	}

	newMap.CachedMin = x.CachedMin
	newMap.CachedMax = x.CachedMax
	newMap.AABBDirty = x.AABBDirty

	return newMap
}

func (x *XBrickMap) ComputeAABB() (mgl32.Vec3, mgl32.Vec3) {
	if !x.AABBDirty {
		return x.CachedMin, x.CachedMax
	}

	if len(x.Sectors) == 0 {
		x.CachedMin = mgl32.Vec3{}
		x.CachedMax = mgl32.Vec3{}
		x.AABBDirty = false
		return x.CachedMin, x.CachedMax
	}

	minB := mgl32.Vec3{float32(1e20), float32(1e20), float32(1e20)}
	maxB := mgl32.Vec3{float32(-1e20), float32(-1e20), float32(-1e20)}
	found := false

	for sKey, sector := range x.Sectors {
		if sector.IsEmpty() {
			continue
		}
		ox, oy, oz := float32(sKey[0]*SectorSize), float32(sKey[1]*SectorSize), float32(sKey[2]*SectorSize)

		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil || brick.IsEmpty() {
					continue
				}

				brickOx := ox + float32(bx*BrickSize)
				brickOy := oy + float32(by*BrickSize)
				brickOz := oz + float32(bz*BrickSize)

				if brick.Flags&BrickFlagSolid != 0 {
					vMin := mgl32.Vec3{brickOx, brickOy, brickOz}
					vMax := vMin.Add(mgl32.Vec3{float32(BrickSize), float32(BrickSize), float32(BrickSize)})
					minB = mgl32.Vec3{min(minB.X(), vMin.X()), min(minB.Y(), vMin.Y()), min(minB.Z(), vMin.Z())}
					maxB = mgl32.Vec3{max(maxB.X(), vMax.X()), max(maxB.Y(), vMax.Y()), max(maxB.Z(), vMax.Z())}
					found = true
					continue
				}

				// Iterate microblocks (2x2x2) using mask
				for m := 0; m < 64; m++ {
					if (brick.OccupancyMask64 & (1 << m)) != 0 {
						mx, my, mz := m%4, (m/4)%4, m/16
						ms := MicroSize
						startVx, startVy, startVz := mx*ms, my*ms, mz*ms
						for vx := 0; vx < ms; vx++ {
							for vy := 0; vy < ms; vy++ {
								for vz := 0; vz < ms; vz++ {
									if brick.Payload[startVx+vx][startVy+vy][startVz+vz] != 0 {
										vMin := mgl32.Vec3{
											brickOx + float32(startVx+vx),
											brickOy + float32(startVy+vy),
											brickOz + float32(startVz+vz),
										}
										vMax := vMin.Add(mgl32.Vec3{1, 1, 1})

										minB = mgl32.Vec3{min(minB.X(), vMin.X()), min(minB.Y(), vMin.Y()), min(minB.Z(), vMin.Z())}
										maxB = mgl32.Vec3{max(maxB.X(), vMax.X()), max(maxB.Y(), vMax.Y()), max(maxB.Z(), vMax.Z())}
										found = true
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if !found {
		x.CachedMin = mgl32.Vec3{}
		x.CachedMax = mgl32.Vec3{}
	} else {
		x.CachedMin = minB
		x.CachedMax = maxB
	}
	x.AABBDirty = false
	return x.CachedMin, x.CachedMax
}

func (x *XBrickMap) GetAABBMin() mgl32.Vec3 {
	x.ComputeAABB()
	return x.CachedMin
}

func (x *XBrickMap) GetAABBMax() mgl32.Vec3 {
	x.ComputeAABB()
	return x.CachedMax
}
