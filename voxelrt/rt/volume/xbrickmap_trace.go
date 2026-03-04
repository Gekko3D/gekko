package volume

import (
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

func (x *XBrickMap) RayMarch(rayOrigin, rayDir mgl32.Vec3, tMin, tMax float32) (bool, float32, [3]int, mgl32.Vec3) {
	t := tMin
	// Protect against very small or zero direction components
	safeX := rayDir.X()
	if math.Abs(float64(safeX)) < 1e-7 {
		if safeX >= 0 {
			safeX = 1e-7
		} else {
			safeX = -1e-7
		}
	}
	safeY := rayDir.Y()
	if math.Abs(float64(safeY)) < 1e-7 {
		if safeY >= 0 {
			safeY = 1e-7
		} else {
			safeY = -1e-7
		}
	}
	safeZ := rayDir.Z()
	if math.Abs(float64(safeZ)) < 1e-7 {
		if safeZ >= 0 {
			safeZ = 1e-7
		} else {
			safeZ = -1e-7
		}
	}

	invDir := mgl32.Vec3{1.0 / safeX, 1.0 / safeY, 1.0 / safeZ}

	iterations := 0
	const maxIterations = 10000 // Increased from 2000

	for t < tMax && iterations < maxIterations {
		iterations++

		// Use a dynamic offset for higher stability at large t
		tBias := 0.001
		if t > 100 {
			tBias = 0.005
		}

		p := rayOrigin.Add(rayDir.Mul(t + float32(tBias)))

		sx, sy, sz := int(math.Floor(float64(p.X()/SectorSize))), int(math.Floor(float64(p.Y()/SectorSize))), int(math.Floor(float64(p.Z()/SectorSize)))

		sKey := [3]int{sx, sy, sz}
		sector, ok := x.Sectors[sKey]
		if !ok {
			// Step to next sector
			res := x.stepToNext(p, rayDir, invDir, float32(SectorSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a sector, check bricks
		flX, flY, flZ := math.Floor(float64(p.X())), math.Floor(float64(p.Y())), math.Floor(float64(p.Z()))
		slx := int(flX) % SectorSize
		if slx < 0 {
			slx += SectorSize
		}
		sly := int(flY) % SectorSize
		if sly < 0 {
			sly += SectorSize
		}
		slz := int(flZ) % SectorSize
		if slz < 0 {
			slz += SectorSize
		}

		bx, by, bz := slx/BrickSize, sly/BrickSize, slz/BrickSize
		brick := sector.GetBrick(bx, by, bz)
		if brick == nil {
			res := x.stepToNext(p, rayDir, invDir, float32(BrickSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a brick, check microcells
		blx := slx % BrickSize
		bly := sly % BrickSize
		blz := slz % BrickSize

		mx, my, mz := blx/MicroSize, bly/MicroSize, blz/MicroSize
		microIdx := mx + my*4 + mz*16
		if (brick.OccupancyMask64 & (1 << microIdx)) == 0 {
			res := x.stepToNext(p, rayDir, invDir, float32(MicroSize))
			t += float32(math.Max(float64(res), 0.001))
			continue
		}

		// Inside a microcell, check voxels
		vx, vy, vz := blx, bly, blz
		paletteIdx := brick.Payload[vx][vy][vz]
		if paletteIdx != 0 {
			// Hit!
			vMin := mgl32.Vec3{
				float32(sx*SectorSize + bx*BrickSize + vx),
				float32(sy*SectorSize + by*BrickSize + vy),
				float32(sz*SectorSize + bz*BrickSize + vz),
			}
			vCenter := vMin.Add(mgl32.Vec3{0.5, 0.5, 0.5})
			pHit := rayOrigin.Add(rayDir.Mul(t))
			localP := pHit.Sub(vCenter)
			absP := mgl32.Vec3{float32(math.Abs(float64(localP.X()))), float32(math.Abs(float64(localP.Y()))), float32(math.Abs(float64(localP.Z())))}
			maxC := float32(math.Max(float64(absP.X()), math.Max(float64(absP.Y()), float64(absP.Z()))))

			normal := mgl32.Vec3{0, 0, 0}
			if absP.X() >= maxC-0.01 {
				if localP.X() > 0 {
					normal[0] = 1
				} else {
					normal[0] = -1
				}
			} else if absP.Y() >= maxC-0.01 {
				if localP.Y() > 0 {
					normal[1] = 1
				} else {
					normal[1] = -1
				}
			} else if absP.Z() >= maxC-0.01 {
				if localP.Z() > 0 {
					normal[2] = 1
				} else {
					normal[2] = -1
				}
			}

			return true, t, [3]int{int(flX), int(flY), int(flZ)}, normal
		}

		res := x.stepToNext(p, rayDir, invDir, 1.0)
		t += float32(math.Max(float64(res), 0.001))
	}

	if iterations >= maxIterations {
		fmt.Printf("WARNING: Max iterations reached in RayMarch (t=%f, tMax=%f)\n", t, tMax)
	}

	return false, t, [3]int{}, mgl32.Vec3{}
}

func (x *XBrickMap) stepToNext(p, dir, invDir mgl32.Vec3, size float32) float32 {
	res := float32(1e10)
	for i := 0; i < 3; i++ {
		if dir[i] == 0 {
			continue
		}

		var dist float32
		if dir[i] > 0 {
			// Distance to next whole size boundary
			dist = (float32(math.Floor(float64(p[i]/size+1e-6)))+1)*size - p[i]
		} else {
			// Distance to previous whole size boundary
			dist = (float32(math.Floor(float64(p[i]/size-1e-6))))*size - p[i]
		}

		tVal := dist * invDir[i]
		if tVal > 1e-6 && tVal < res {
			res = tVal
		}
	}
	// Add a tiny extra bit to ensure we actually cross the boundary in the next iteration
	if res < 1e10 {
		return res + 1e-4
	}
	return res
}

func (x *XBrickMap) Resample(scale float32) *XBrickMap {
	newMap := NewXBrickMap()
	if len(x.Sectors) == 0 {
		return newMap
	}

	minB, maxB := x.ComputeAABB()
	fmt.Printf("Resampling Map: Min=%v Max=%v Scale=%f\n", minB, maxB, scale)

	// Calculate new bounds relative to (0,0,0) for now, but we want to stay stable.
	// Actually, the most predictable behavior is scaling relative to minB.

	// Shifted old coordinates: p' = (p - minB) * scale + minB?
	// No, let's keep it simple: newGridPos = oldGridPos * scale.
	// This mirrors world space: WorldPos = GridPos * 0.1.
	// If we want WorldPos to be same, we keep same mapping.

	newMin := minB.Mul(scale)
	newMax := maxB.Mul(scale)

	// Iterate over the new bounding box
	minX, minY, minZ := int(math.Floor(float64(newMin.X()))), int(math.Floor(float64(newMin.Y()))), int(math.Floor(float64(newMin.Z())))
	maxX, maxY, maxZ := int(math.Ceil(float64(newMax.X()))), int(math.Ceil(float64(newMax.Y()))), int(math.Ceil(float64(newMax.Z())))

	// Safety: Check total iterations
	iterationsX := maxX - minX + 1
	iterationsY := maxY - minY + 1
	iterationsZ := maxZ - minZ + 1
	totalIters := int64(iterationsX) * int64(iterationsY) * int64(iterationsZ)

	if totalIters > 100*100*100*100 { // 100M iterations limit
		fmt.Printf("REJECTED: Rescale too large (%d voxels grid volume)\n", totalIters)
		return x // Return original
	}

	fmt.Printf("Iterating range: [%d %d %d] to [%d %d %d]\n", minX, minY, minZ, maxX, maxY, maxZ)
	voxelCount := 0

	invScale := 1.0 / scale

	for gx := minX; gx <= maxX; gx++ {
		for gy := minY; gy <= maxY; gy++ {
			for gz := minZ; gz <= maxZ; gz++ {
				// Nearest neighbor sampling with center alignment
				// We project the center of the new voxel (gx+0.5) back to old space
				oldX := int(math.Floor((float64(gx) + 0.5) * float64(invScale)))
				oldY := int(math.Floor((float64(gy) + 0.5) * float64(invScale)))
				oldZ := int(math.Floor((float64(gz) + 0.5) * float64(invScale)))

				found, val := x.GetVoxel(oldX, oldY, oldZ)
				if found {
					newMap.SetVoxel(gx, gy, gz, val)
					voxelCount++
				}
			}
		}
	}

	newMap.ComputeAABB()
	fmt.Printf("Resample Done: Generated %d voxels. New AABB: %v - %v\n", voxelCount, newMap.CachedMin, newMap.CachedMax)
	return newMap
}
