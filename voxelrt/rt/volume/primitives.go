package volume

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// Sphere fills a sphere in the XBrickMap
func Sphere(xbm *XBrickMap, center mgl32.Vec3, radius float32, paletteIdx uint8) {
	r2 := radius * radius
	minBound := [3]int{
		int(math.Floor(float64(center.X() - radius))),
		int(math.Floor(float64(center.Y() - radius))),
		int(math.Floor(float64(center.Z() - radius))),
	}
	maxBound := [3]int{
		int(math.Ceil(float64(center.X() + radius))),
		int(math.Ceil(float64(center.Y() + radius))),
		int(math.Ceil(float64(center.Z() + radius))),
	}

	for x := minBound[0]; x <= maxBound[0]; x++ {
		for y := minBound[1]; y <= maxBound[1]; y++ {
			for z := minBound[2]; z <= maxBound[2]; z++ {
				dx := float32(x) - center.X() + 0.5
				dy := float32(y) - center.Y() + 0.5
				dz := float32(z) - center.Z() + 0.5
				if dx*dx+dy*dy+dz*dz <= r2 {
					xbm.SetVoxel(x, y, z, paletteIdx)
				}
			}
		}
	}
}

// Cube fills a cube in the XBrickMap
func Cube(xbm *XBrickMap, minB, maxB mgl32.Vec3, paletteIdx uint8) {
	minI := [3]int{
		int(math.Floor(float64(minB.X()))),
		int(math.Floor(float64(minB.Y()))),
		int(math.Floor(float64(minB.Z()))),
	}
	maxI := [3]int{
		int(math.Floor(float64(maxB.X()))),
		int(math.Floor(float64(maxB.Y()))),
		int(math.Floor(float64(maxB.Z()))),
	}

	for x := minI[0]; x <= maxI[0]; x++ {
		for y := minI[1]; y <= maxI[1]; y++ {
			for z := minI[2]; z <= maxI[2]; z++ {
				xbm.SetVoxel(x, y, z, paletteIdx)
			}
		}
	}
}

// Cone fills a cone in the XBrickMap
// base is the center of the base circle, tip is the apex
func Cone(xbm *XBrickMap, base, tip mgl32.Vec3, radius float32, paletteIdx uint8) {
	heightVec := tip.Sub(base)
	height := heightVec.Len()
	if height < 1e-5 {
		return
	}
	axis := heightVec.Normalize()

	// Broad bounding box
	maxDim := float32(math.Max(float64(radius), float64(height)))
	center := base.Add(tip).Mul(0.5)
	minB := [3]int{
		int(math.Floor(float64(center.X() - maxDim))),
		int(math.Floor(float64(center.Y() - maxDim))),
		int(math.Floor(float64(center.Z() - maxDim))),
	}
	maxB := [3]int{
		int(math.Ceil(float64(center.X() + maxDim))),
		int(math.Ceil(float64(center.Y() + maxDim))),
		int(math.Ceil(float64(center.Z() + maxDim))),
	}

	for x := minB[0]; x <= maxB[0]; x++ {
		for y := minB[1]; y <= maxB[1]; y++ {
			for z := minB[2]; z <= maxB[2]; z++ {
				p := mgl32.Vec3{float32(x) + 0.5, float32(y) + 0.5, float32(z) + 0.5}
				v := p.Sub(base)
				distOnAxis := v.Dot(axis)
				if distOnAxis < 0 || distOnAxis > height {
					continue
				}

				radiusAtDist := radius * (1.0 - distOnAxis/height)
				distToAxis2 := v.LenSqr() - distOnAxis*distOnAxis
				if distToAxis2 <= radiusAtDist*radiusAtDist {
					xbm.SetVoxel(x, y, z, paletteIdx)
				}
			}
		}
	}
}

// Pyramid fills a square pyramid in the XBrickMap
func Pyramid(xbm *XBrickMap, base, tip mgl32.Vec3, size float32, paletteIdx uint8) {
	heightVec := tip.Sub(base)
	height := heightVec.Len()
	if height < 1e-5 {
		return
	}
	axis := heightVec.Normalize()

	// Standard orientation helper
	up := mgl32.Vec3{0, 1, 0}
	if math.Abs(float64(axis.Dot(up))) > 0.99 {
		up = mgl32.Vec3{1, 0, 0}
	}
	right := axis.Cross(up).Normalize()
	forward := right.Cross(axis).Normalize()

	maxDim := float32(math.Max(float64(size), float64(height)))
	center := base.Add(tip).Mul(0.5)
	minB := [3]int{
		int(math.Floor(float64(center.X() - maxDim))),
		int(math.Floor(float64(center.Y() - maxDim))),
		int(math.Floor(float64(center.Z() - maxDim))),
	}
	maxB := [3]int{
		int(math.Ceil(float64(center.X() + maxDim))),
		int(math.Ceil(float64(center.Y() + maxDim))),
		int(math.Ceil(float64(center.Z() + maxDim))),
	}

	halfSize := size * 0.5

	for x := minB[0]; x <= maxB[0]; x++ {
		for y := minB[1]; y <= maxB[1]; y++ {
			for z := minB[2]; z <= maxB[2]; z++ {
				p := mgl32.Vec3{float32(x) + 0.5, float32(y) + 0.5, float32(z) + 0.5}
				v := p.Sub(base)
				distOnAxis := v.Dot(axis)
				if distOnAxis < 0 || distOnAxis > height {
					continue
				}

				scale := 1.0 - distOnAxis/height
				s := halfSize * scale

				dx := v.Dot(right)
				dz := v.Dot(forward)

				if math.Abs(float64(dx)) <= float64(s) && math.Abs(float64(dz)) <= float64(s) {
					xbm.SetVoxel(x, y, z, paletteIdx)
				}
			}
		}
	}
}

// Point fills a single voxel
func Point(xbm *XBrickMap, x, y, z int, paletteIdx uint8) {
	xbm.SetVoxel(x, y, z, paletteIdx)
}
