package volume

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type voxelCoord [3]int

// ComponentInfo holds a separated voxel part and its voxel count.
type ComponentInfo struct {
	Map        *XBrickMap
	VoxelCount int
	Min        mgl32.Vec3
	Max        mgl32.Vec3
}

// SplitDisconnectedComponents identifies disconnected voxel parts and returns them as separate XBrickMaps.
// The voxels in the returned maps are in the SAME local coordinate space as the original.
func (x *XBrickMap) SplitDisconnectedComponents() []ComponentInfo {
	if len(x.Sectors) == 0 {
		return nil
	}

	minB, maxB := x.ComputeAABB()
	minX, minY, minZ := int(math.Round(float64(minB[0]))), int(math.Round(float64(minB[1]))), int(math.Round(float64(minB[2])))
	maxX, maxY, maxZ := int(math.Round(float64(maxB[0]))), int(math.Round(float64(maxB[1]))), int(math.Round(float64(maxB[2])))

	sx := maxX - minX + 1
	sy := maxY - minY + 1
	sz := maxZ - minZ + 1
	volumeTotal := sx * sy * sz

	// Limit volume to avoid huge allocations (e.g. 2M voxels ~ 250KB bitset)
	if volumeTotal > 4000000 || volumeTotal <= 0 {
		return nil
	}

	// 1. Bitsets for connectivity
	// exists: 1 if voxel exists
	// visited: 1 if voxel has been processed
	exists := make([]uint64, (volumeTotal+63)/64)
	visited := make([]uint64, (volumeTotal+63)/64)
	values := make(map[int]uint8) // flatIndex -> value (sparse for colors)

	flatIdx := func(vx, vy, vz int) int {
		lx, ly, lz := vx-minX, vy-minY, vz-minZ
		return lz*sx*sy + ly*sx + lx
	}

	totalVoxels := 0
	for sKey, sector := range x.Sectors {
		ox, oy, oz := sKey[0]*SectorSize, sKey[1]*SectorSize, sKey[2]*SectorSize
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil || brick.IsEmpty() {
					continue
				}
				brickOx, brickOy, brickOz := ox+bx*BrickSize, oy+by*BrickSize, oz+bz*BrickSize
				if brick.Flags&BrickFlagSolid != 0 {
					val := uint8(brick.AtlasOffset)
					for vz := 0; vz < BrickSize; vz++ {
						for vy := 0; vy < BrickSize; vy++ {
							for vx := 0; vx < BrickSize; vx++ {
								idx := flatIdx(brickOx+vx, brickOy+vy, brickOz+vz)
								exists[idx/64] |= (1 << (idx % 64))
								values[idx] = val
								totalVoxels++
							}
						}
					}
					continue
				}
				for vz := 0; vz < BrickSize; vz++ {
					for vy := 0; vy < BrickSize; vy++ {
						for vx := 0; vx < BrickSize; vx++ {
							val := brick.Payload[vx][vy][vz]
							if val != 0 {
								idx := flatIdx(brickOx+vx, brickOy+vy, brickOz+vz)
								exists[idx/64] |= (1 << (idx % 64))
								values[idx] = val
								totalVoxels++
							}
						}
					}
				}
			}
		}
	}

	if totalVoxels == 0 {
		return nil
	}

	var components []ComponentInfo

	// 2. BFS using bitsets
	for idx := 0; idx < volumeTotal; idx++ {
		isSet := (exists[idx/64] & (1 << (idx % 64))) != 0
		isVis := (visited[idx/64] & (1 << (idx % 64))) != 0
		if !isSet || isVis {
			continue
		}

		// New component found
		newMap := NewXBrickMap()
		q := []int{idx}
		visited[idx/64] |= (1 << (idx % 64))

		vx := minX + (idx % sx)
		vy := minY + ((idx / sx) % sy)
		vz := minZ + (idx / (sx * sy))
		newMap.SetVoxel(vx, vy, vz, values[idx])
		compVoxelCount := 1

		cMin := mgl32.Vec3{float32(vx), float32(vy), float32(vz)}
		cMax := mgl32.Vec3{float32(vx + 1), float32(vy + 1), float32(vz + 1)}

		for len(q) > 0 {
			vIdx := q[0]
			q = q[1:]

			cx := minX + (vIdx % sx)
			cy := minY + ((vIdx / sx) % sy)
			cz := minZ + (vIdx / (sx * sy))

			// Update bounds
			cMin[0] = min(cMin[0], float32(cx))
			cMin[1] = min(cMin[1], float32(cy))
			cMin[2] = min(cMin[2], float32(cz))
			cMax[0] = max(cMax[0], float32(cx+1))
			cMax[1] = max(cMax[1], float32(cy+1))
			cMax[2] = max(cMax[2], float32(cz+1))

			// Neighbors (6-connectivity) in flat space
			for axis := 0; axis < 3; axis++ {
				for dir := -1; dir <= 1; dir += 2 {
					nx, ny, nz := cx, cy, cz
					if axis == 0 {
						nx += dir
					} else if axis == 1 {
						ny += dir
					} else {
						nz += dir
					}

					if nx < minX || nx > maxX || ny < minY || ny > maxY || nz < minZ || nz > maxZ {
						continue
					}

					ni := flatIdx(nx, ny, nz)
					nSet := (exists[ni/64] & (1 << (ni % 64))) != 0
					nVis := (visited[ni/64] & (1 << (ni % 64))) != 0
					if nSet && !nVis {
						visited[ni/64] |= (1 << (ni % 64))
						newMap.SetVoxel(nx, ny, nz, values[ni])
						q = append(q, ni)
						compVoxelCount++
					}
				}
			}
		}
		newMap.CachedMin = cMin
		newMap.CachedMax = cMax
		newMap.AABBDirty = false

		components = append(components, ComponentInfo{
			Map:        newMap,
			VoxelCount: compVoxelCount,
			Min:        cMin,
			Max:        cMax,
		})

		if compVoxelCount == totalVoxels {
			return nil // Optimization: only 1 component
		}
	}

	if len(components) <= 1 {
		return nil
	}

	return components
}

// Shift returns a new XBrickMap with all voxels shifted by (dx, dy, dz).
func (x *XBrickMap) Shift(dx, dy, dz int) *XBrickMap {
	newMap := NewXBrickMap()
	for sKey, sector := range x.Sectors {
		ox, oy, oz := sKey[0]*SectorSize, sKey[1]*SectorSize, sKey[2]*SectorSize
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil || brick.IsEmpty() {
					continue
				}
				brickOx, brickOy, brickOz := ox+bx*BrickSize, oy+by*BrickSize, oz+bz*BrickSize
				if brick.Flags&BrickFlagSolid != 0 {
					val := uint8(brick.AtlasOffset)
					for vz := 0; vz < BrickSize; vz++ {
						for vy := 0; vy < BrickSize; vy++ {
							for vx := 0; vx < BrickSize; vx++ {
								newMap.SetVoxel(brickOx+vx+dx, brickOy+vy+dy, brickOz+vz+dz, val)
							}
						}
					}
					continue
				}
				for vz := 0; vz < BrickSize; vz++ {
					for vy := 0; vy < BrickSize; vy++ {
						for vx := 0; vx < BrickSize; vx++ {
							val := brick.Payload[vx][vy][vz]
							if val != 0 {
								newMap.SetVoxel(brickOx+vx+dx, brickOy+vy+dy, brickOz+vz+dz, val)
							}
						}
					}
				}
			}
		}
	}
	return newMap
}

// Center calculates the AABB and returns a new XBrickMap shifted such that its center is at (0,0,0).
// Also returns the local center in the original coordinate space.
func (x *XBrickMap) Center() (*XBrickMap, mgl32.Vec3) {
	minB, maxB := x.ComputeAABB()
	localCenter := minB.Add(maxB).Mul(0.5)

	shiftX := int(-math.Round(float64(localCenter.X())))
	shiftY := int(-math.Round(float64(localCenter.Y())))
	shiftZ := int(-math.Round(float64(localCenter.Z())))

	shiftedMap := x.Shift(shiftX, shiftY, shiftZ)
	return shiftedMap, localCenter
}

func (x *XBrickMap) GetVoxelCount() int {
	count := 0
	for _, sector := range x.Sectors {
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil {
					continue
				}
				if brick.Flags&BrickFlagSolid != 0 {
					count += BrickSize * BrickSize * BrickSize
					continue
				}
				for vz := 0; vz < BrickSize; vz++ {
					for vy := 0; vy < BrickSize; vy++ {
						for vx := 0; vx < BrickSize; vx++ {
							if brick.Payload[vx][vy][vz] != 0 {
								count++
							}
						}
					}
				}
			}
		}
	}
	return count
}
