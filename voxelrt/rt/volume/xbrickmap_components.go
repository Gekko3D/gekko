package volume

import (
	"math"
	"math/bits"

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

type subBrickComponent struct {
	parentBrick [3]int
	voxels      [8]uint64
	faceMasks   [6]uint64 // -X, +X, -Y, +Y, -Z, +Z (each 8x8 slice)
}

func (c *subBrickComponent) computeFaceMasks() {
	// -X (x=0) and +X (x=7)
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			if c.voxels[z]&(1<<(0+y*8)) != 0 {
				c.faceMasks[0] |= (uint64(1) << (y + z*8))
			}
			if c.voxels[z]&(1<<(7+y*8)) != 0 {
				c.faceMasks[1] |= (uint64(1) << (y + z*8))
			}
		}
	}
	// -Y (y=0) and +Y (y=7)
	for z := 0; z < 8; z++ {
		for x := 0; x < 8; x++ {
			if c.voxels[z]&(1<<(x+0*8)) != 0 {
				c.faceMasks[2] |= (uint64(1) << (x + z*8))
			}
			if c.voxels[z]&(1<<(x+7*8)) != 0 {
				c.faceMasks[3] |= (uint64(1) << (x + z*8))
			}
		}
	}
	// -Z (z=0) and +Z (z=7)
	c.faceMasks[4] = c.voxels[0]
	c.faceMasks[5] = c.voxels[7]
}

// SplitDisconnectedComponents identifies disconnected voxel parts and returns them as separate XBrickMaps.
// Optimized version using brick-level connectivity.
func (x *XBrickMap) SplitDisconnectedComponents() []ComponentInfo {
	if len(x.Sectors) == 0 {
		return nil
	}

	// 1. Gather all bricks and their internal components
	brickToComponents := make(map[[3]int][]*subBrickComponent)
	var allComps []*subBrickComponent
	totalVoxels := 0

	for sKey, sector := range x.Sectors {
		ox, oy, oz := sKey[0]*SectorBricks, sKey[1]*SectorBricks, sKey[2]*SectorBricks
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.PackedBricks[sector.GetPackedIndex(i)]
				if brick == nil || brick.IsEmpty() {
					continue
				}

				gx, gy, gz := ox+bx, oy+by, oz+bz
				comps := x.findInternalBrickComponents(brick, gx, gy, gz)
				if len(comps) > 0 {
					brickToComponents[[3]int{gx, gy, gz}] = comps
					allComps = append(allComps, comps...)
					for _, c := range comps {
						for z := 0; z < 8; z++ {
							totalVoxels += bits.OnesCount64(c.voxels[z])
						}
					}
				}
			}
		}
	}

	if len(allComps) == 0 {
		return nil
	}

	// Safety limit check (total voxels, not volume)
	// Increasing to 100M voxels as requested for large buildings.
	if totalVoxels > 100000000 {
		return nil
	}

	// 2. Build graph and find components
	adj := make(map[*subBrickComponent][]*subBrickComponent)
	for brickPos, comps := range brickToComponents {
		for _, c := range comps {
			// Check 6 neighbors
			for face := 0; face < 6; face++ {
				nx, ny, nz := brickPos[0], brickPos[1], brickPos[2]
				var oppFace int
				switch face {
				case 0:
					nx--
					oppFace = 1
				case 1:
					nx++
					oppFace = 0
				case 2:
					ny--
					oppFace = 3
				case 3:
					ny++
					oppFace = 2
				case 4:
					nz--
					oppFace = 5
				case 5:
					nz++
					oppFace = 4
				}

				if nComps, ok := brickToComponents[[3]int{nx, ny, nz}]; ok {
					for _, nc := range nComps {
						if (c.faceMasks[face] & nc.faceMasks[oppFace]) != 0 {
							adj[c] = append(adj[c], nc)
							adj[nc] = append(adj[nc], c)
						}
					}
				}
			}
		}
	}

	// 3. BFS on sub-components
	visited := make(map[*subBrickComponent]bool)
	var results []ComponentInfo

	for _, startNode := range allComps {
		if visited[startNode] {
			continue
		}

		// New graph component
		compVoxCount := 0
		newMap := NewXBrickMap()
		q := []*subBrickComponent{startNode}
		visited[startNode] = true

		cMin := mgl32.Vec3{float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)}
		cMax := mgl32.Vec3{float32(-math.MaxFloat32), float32(-math.MaxFloat32), float32(-math.MaxFloat32)}

		for len(q) > 0 {
			curr := q[0]
			q = q[1:]

			// Add all voxels from this sub-brick component to the new map
			bx, by, bz := curr.parentBrick[0], curr.parentBrick[1], curr.parentBrick[2]
			sx, sy, sz := bx/4, by/4, bz/4
			lbx, lby, lbz := bx%4, by%4, bz%4
			if lbx < 0 {
				lbx += 4
				sx--
			}
			if lby < 0 {
				lby += 4
				sy--
			}
			if lbz < 0 {
				lbz += 4
				sz--
			}

			// Find original brick to get values
			sector := x.Sectors[[3]int{sx, sy, sz}]
			brick := sector.GetBrick(lbx, lby, lbz)

			baseX, baseY, baseZ := curr.parentBrick[0]*8, curr.parentBrick[1]*8, curr.parentBrick[2]*8

			for vz := 0; vz < 8; vz++ {
				if curr.voxels[vz] == 0 {
					continue
				}
				for vy := 0; vy < 8; vy++ {
					row := (curr.voxels[vz] >> (vy * 8)) & 0xFF
					if row == 0 {
						continue
					}
					for vx := 0; vx < 8; vx++ {
						if (row & (1 << vx)) != 0 {
							val := uint8(0)
							if brick.Flags&BrickFlagSolid != 0 {
								val = uint8(brick.AtlasOffset)
							} else {
								val = brick.Payload[vx][vy][vz]
							}

							gx, gy, gz := baseX+vx, baseY+vy, baseZ+vz
							newMap.SetVoxel(gx, gy, gz, val)
							compVoxCount++

							cMin[0] = float32(math.Min(float64(cMin[0]), float64(gx)))
							cMin[1] = float32(math.Min(float64(cMin[1]), float64(gy)))
							cMin[2] = float32(math.Min(float64(cMin[2]), float64(gz)))
							cMax[0] = float32(math.Max(float64(cMax[0]), float64(gx+1)))
							cMax[1] = float32(math.Max(float64(cMax[1]), float64(gy+1)))
							cMax[2] = float32(math.Max(float64(cMax[2]), float64(gz+1)))
						}
					}
				}
			}

			// Neighbors in graph
			for _, neighbor := range adj[curr] {
				if !visited[neighbor] {
					visited[neighbor] = true
					q = append(q, neighbor)
				}
			}
		}

		newMap.CachedMin = cMin
		newMap.CachedMax = cMax
		newMap.AABBDirty = false

		results = append(results, ComponentInfo{
			Map:        newMap,
			VoxelCount: compVoxCount,
			Min:        cMin,
			Max:        cMax,
		})
	}

	if len(results) <= 1 {
		return nil
	}

	return results
}

func (x *XBrickMap) findInternalBrickComponents(brick *Brick, gx, gy, gz int) []*subBrickComponent {
	if brick.Flags&BrickFlagSolid != 0 {
		c := &subBrickComponent{
			parentBrick: [3]int{gx, gy, gz},
			voxels:      [8]uint64{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		}
		c.computeFaceMasks()
		return []*subBrickComponent{c}
	}

	var components []*subBrickComponent
	var visited [8]uint64

	for vz := 0; vz < 8; vz++ {
		for vy := 0; vy < 8; vy++ {
			for vx := 0; vx < 8; vx++ {
				if brick.Payload[vx][vy][vz] == 0 {
					continue
				}
				if (visited[vz] & (uint64(1) << (vx + vy*8))) != 0 {
					continue
				}

				// New sub-component found
				comp := &subBrickComponent{parentBrick: [3]int{gx, gy, gz}}
				q := [][3]int{{vx, vy, vz}}
				visited[vz] |= (uint64(1) << (vx + vy*8))
				comp.voxels[vz] |= (uint64(1) << (vx + vy*8))

				for len(q) > 0 {
					curr := q[0]
					q = q[1:]

					// Neighbors (6-connectivity)
					for axis := 0; axis < 3; axis++ {
						for dir := -1; dir <= 1; dir += 2 {
							nx, ny, nz := curr[0], curr[1], curr[2]
							if axis == 0 {
								nx += dir
							} else if axis == 1 {
								ny += dir
							} else {
								nz += dir
							}

							if nx < 0 || nx >= 8 || ny < 0 || ny >= 8 || nz < 0 || nz >= 8 {
								continue
							}

							if brick.Payload[nx][ny][nz] != 0 && (visited[nz]&(uint64(1)<<(nx+ny*8))) == 0 {
								visited[nz] |= (uint64(1) << (nx + ny*8))
								comp.voxels[nz] |= (uint64(1) << (nx + ny*8))
								q = append(q, [3]int{nx, ny, nz})
							}
						}
					}
				}
				comp.computeFaceMasks()
				components = append(components, comp)
			}
		}
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
