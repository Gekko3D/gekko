package volume

import (
	"encoding/binary"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

const (
	VoxelNormalOctMax      = 127
	VoxelNormalValidBit    = 1 << 14
	VoxelNormalTwoSidedBit = 1 << 15

	VoxelNormalSurfaceFitRadius         = 2
	VoxelNormalExtendedSurfaceFitRadius = 4 // Includes a full 3:1 staircase period at offset (3, 1).
	VoxelNormalSurfaceFitMinSamples     = 4
	VoxelNormalSurfaceFitMaxError       = 0.08
	VoxelNormalDensityComponentCutoff   = 0.28

	VoxelAuxRecordBytes = VoxelAuxWordCount * 4
)

type VoxelNormalBakeOptions struct {
	SampleOccupancy func([3]int) bool
	BoundsMin       mgl32.Vec3
	BoundsMax       mgl32.Vec3
	HasBounds       bool
}

func BuildVoxelAuxBytes(brick *Brick, brickOrigin [3]int, opts VoxelNormalBakeOptions) []byte {
	buf := make([]byte, VoxelAuxRecordBytes)
	if brick == nil {
		return buf
	}
	words := brick.DenseOccupancyWords()
	for i, word := range words {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], word)
	}
	if opts.SampleOccupancy == nil {
		return buf
	}

	normalBase := DenseOccupancyWordCount * 4
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				if !BrickVoxelOccupied(brick, x, y, z) {
					continue
				}
				voxelIdx := DenseOccupancyLinearIndexLocal(x, y, z)
				global := [3]int{brickOrigin[0] + x, brickOrigin[1] + y, brickOrigin[2] + z}
				normal, valid, twoSided := BakedVoxelNormal(opts, global)
				if !valid {
					continue
				}
				binary.LittleEndian.PutUint16(buf[normalBase+voxelIdx*2:normalBase+voxelIdx*2+2], EncodeBakedVoxelNormal(normal, twoSided))
			}
		}
	}
	return buf
}

func BrickVoxelOccupied(brick *Brick, x, y, z int) bool {
	if brick == nil {
		return false
	}
	if brick.Flags&BrickFlagSolid != 0 {
		return true
	}
	return brick.VoxelValue(x, y, z) != 0
}

func DenseOccupancyLinearIndexLocal(x, y, z int) int {
	return x + y*BrickSize + z*BrickSize*BrickSize
}

func EncodeBakedVoxelNormal(normal mgl32.Vec3, twoSided bool) uint16 {
	n := NormalizedVec3OrZero(normal)
	if n.LenSqr() <= 1e-8 {
		return 0
	}
	denom := float32(math.Abs(float64(n.X())) + math.Abs(float64(n.Y())) + math.Abs(float64(n.Z())))
	if denom <= 1e-8 {
		return 0
	}
	ox := n.X() / denom
	oy := n.Y() / denom
	if n.Z() < 0 {
		oldX, oldY := ox, oy
		ox = (1 - float32(math.Abs(float64(oldY)))) * signNotZero(oldX)
		oy = (1 - float32(math.Abs(float64(oldX)))) * signNotZero(oldY)
	}
	pack := func(v float32) uint16 {
		v = clampFloat32(v*0.5+0.5, 0, 1)
		return uint16(math.Round(float64(v * VoxelNormalOctMax)))
	}

	b := uint16(VoxelNormalValidBit)
	b |= pack(ox)
	b |= pack(oy) << 7
	if twoSided {
		b |= VoxelNormalTwoSidedBit
	}
	return b
}

func BakedVoxelNormal(opts VoxelNormalBakeOptions, voxel [3]int) (mgl32.Vec3, bool, bool) {
	sample := opts.SampleOccupancy
	if sample == nil {
		return mgl32.Vec3{}, false, false
	}
	occPX := boolToInt(sample([3]int{voxel[0] + 1, voxel[1], voxel[2]}))
	occNX := boolToInt(sample([3]int{voxel[0] - 1, voxel[1], voxel[2]}))
	occPY := boolToInt(sample([3]int{voxel[0], voxel[1] + 1, voxel[2]}))
	occNY := boolToInt(sample([3]int{voxel[0], voxel[1] - 1, voxel[2]}))
	occPZ := boolToInt(sample([3]int{voxel[0], voxel[1], voxel[2] + 1}))
	occNZ := boolToInt(sample([3]int{voxel[0], voxel[1], voxel[2] - 1}))

	nx := occNX - occPX
	ny := occNY - occPY
	nz := occNZ - occPZ
	twoSided := (occPX == 0 && occNX == 0) || (occPY == 0 && occNY == 0) || (occPZ == 0 && occNZ == 0)

	tx, ty, tz := 0, 0, 0
	exposedAxisPairs := 0
	exposedAxis := -1
	if occPX == 0 || occNX == 0 {
		tx = axisTieBreakSign(opts, voxel, 0)
		exposedAxisPairs++
		exposedAxis = 0
	}
	if occPY == 0 || occNY == 0 {
		ty = axisTieBreakSign(opts, voxel, 1)
		exposedAxisPairs++
		exposedAxis = 1
	}
	if occPZ == 0 || occNZ == 0 {
		tz = axisTieBreakSign(opts, voxel, 2)
		exposedAxisPairs++
		exposedAxis = 2
	}
	if normal, ok := DensityGradientVoxelNormal(opts, voxel); ok {
		if twoSided && VoxelNormalHasMultipleComponents(normal) {
			normal = CanonicalVoxelNormalHemisphere(normal)
		}
		return normal, true, twoSided
	}
	if nx != 0 || ny != 0 || nz != 0 {
		return NormalizedVec3OrZero(mgl32.Vec3{float32(nx), float32(ny), float32(nz)}), true, twoSided
	}
	if twoSided && exposedAxisPairs == 1 && hasExtendedSurfaceFitSamples(sample, voxel, exposedAxis) {
		if normal, ok := fittedSurfaceVoxelNormal(opts, voxel, mgl32.Vec3{float32(tx), float32(ty), float32(tz)}, VoxelNormalExtendedSurfaceFitRadius); ok {
			return CanonicalVoxelNormalHemisphere(normal), true, true
		}
	}
	if exposedAxisPairs <= 1 {
		if tx != 0 || ty != 0 || tz != 0 {
			return NormalizedVec3OrZero(mgl32.Vec3{float32(tx), float32(ty), float32(tz)}), true, twoSided
		}
		return mgl32.Vec3{}, false, twoSided
	}

	if normal, ok := FittedSurfaceVoxelNormal(opts, voxel, mgl32.Vec3{float32(tx), float32(ty), float32(tz)}); ok {
		if twoSided {
			normal = CanonicalVoxelNormalHemisphere(normal)
		}
		return normal, true, twoSided
	}
	if tx != 0 || ty != 0 || tz != 0 {
		return NormalizedVec3OrZero(mgl32.Vec3{float32(tx), float32(ty), float32(tz)}), true, twoSided
	}
	return mgl32.Vec3{}, false, twoSided
}

func FittedSurfaceVoxelNormal(opts VoxelNormalBakeOptions, voxel [3]int, orient mgl32.Vec3) (mgl32.Vec3, bool) {
	return fittedSurfaceVoxelNormal(opts, voxel, orient, VoxelNormalSurfaceFitRadius)
}

func hasExtendedSurfaceFitSamples(sample func([3]int) bool, voxel [3]int, normalAxis int) bool {
	if sample == nil || normalAxis < 0 || normalAxis > 2 {
		return false
	}
	for tangentAxis := 0; tangentAxis < 3; tangentAxis++ {
		if tangentAxis == normalAxis {
			continue
		}
		forward, backward := false, false
		for normalOffset := -1; normalOffset <= 1; normalOffset += 2 {
			forwardVoxel := voxel
			forwardVoxel[tangentAxis] += 3
			forwardVoxel[normalAxis] += normalOffset
			forward = forward || sample(forwardVoxel)

			backwardVoxel := voxel
			backwardVoxel[tangentAxis] -= 3
			backwardVoxel[normalAxis] += normalOffset
			backward = backward || sample(backwardVoxel)
		}
		if forward && backward {
			return true
		}
	}
	return false
}

func fittedSurfaceVoxelNormal(opts VoxelNormalBakeOptions, voxel [3]int, orient mgl32.Vec3, radius int) (mgl32.Vec3, bool) {
	if radius <= 0 {
		return mgl32.Vec3{}, false
	}
	type weightedOffset struct {
		x, y, z int
		weight  float64
	}
	offsets := make([]weightedOffset, 0, 32)
	for dz := -radius; dz <= radius; dz++ {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				dist2 := dx*dx + dy*dy + dz*dz
				if dist2 > radius*radius {
					continue
				}
				if !opts.SampleOccupancy([3]int{voxel[0] + dx, voxel[1] + dy, voxel[2] + dz}) {
					continue
				}
				offsets = append(offsets, weightedOffset{x: dx, y: dy, z: dz, weight: 1 / float64(dist2)})
			}
		}
	}
	if len(offsets) < VoxelNormalSurfaceFitMinSamples {
		return mgl32.Vec3{}, false
	}

	best := mgl32.Vec3{}
	bestScore := math.MaxFloat64
	for i := 0; i < len(offsets); i++ {
		a := mgl32.Vec3{float32(offsets[i].x), float32(offsets[i].y), float32(offsets[i].z)}
		for j := i + 1; j < len(offsets); j++ {
			b := mgl32.Vec3{float32(offsets[j].x), float32(offsets[j].y), float32(offsets[j].z)}
			candidate := a.Cross(b)
			if candidate.LenSqr() <= 1e-8 {
				continue
			}
			candidate = candidate.Normalize()
			score := 0.0
			totalWeight := 0.0
			for _, offset := range offsets {
				ov := mgl32.Vec3{float32(offset.x), float32(offset.y), float32(offset.z)}
				score += offset.weight * math.Pow(float64(candidate.Dot(ov.Normalize())), 2)
				totalWeight += offset.weight
			}
			score /= math.Max(totalWeight, 1e-6)
			if score < bestScore {
				bestScore = score
				best = candidate
			}
		}
	}
	if bestScore > VoxelNormalSurfaceFitMaxError || best.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, false
	}
	if orient.LenSqr() > 1e-8 && best.Dot(orient) < 0 {
		best = best.Mul(-1)
	}
	return NormalizedVec3OrZero(best), true
}

func DensityGradientVoxelNormal(opts VoxelNormalBakeOptions, voxel [3]int) (mgl32.Vec3, bool) {
	if opts.SampleOccupancy == nil {
		return mgl32.Vec3{}, false
	}
	gx, gy, gz := 0.0, 0.0, 0.0
	for dz := -VoxelNormalSurfaceFitRadius; dz <= VoxelNormalSurfaceFitRadius; dz++ {
		for dy := -VoxelNormalSurfaceFitRadius; dy <= VoxelNormalSurfaceFitRadius; dy++ {
			for dx := -VoxelNormalSurfaceFitRadius; dx <= VoxelNormalSurfaceFitRadius; dx++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				dist2 := dx*dx + dy*dy + dz*dz
				if dist2 > VoxelNormalSurfaceFitRadius*VoxelNormalSurfaceFitRadius {
					continue
				}
				if !opts.SampleOccupancy([3]int{voxel[0] + dx, voxel[1] + dy, voxel[2] + dz}) {
					continue
				}
				weight := 1 / float64(dist2)
				gx -= float64(dx) * weight
				gy -= float64(dy) * weight
				gz -= float64(dz) * weight
			}
		}
	}
	return NormalizedGradientVoxelNormal(gx, gy, gz)
}

func NormalizedGradientVoxelNormal(gx, gy, gz float64) (mgl32.Vec3, bool) {
	maxAbs := math.Max(math.Abs(gx), math.Max(math.Abs(gy), math.Abs(gz)))
	if maxAbs <= 1e-6 {
		return mgl32.Vec3{}, false
	}
	filter := func(v float64) float32 {
		if math.Abs(v) < maxAbs*VoxelNormalDensityComponentCutoff {
			return 0
		}
		return float32(v)
	}
	n := mgl32.Vec3{filter(gx), filter(gy), filter(gz)}
	if n.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, false
	}
	return NormalizedVec3OrZero(n), true
}

func NormalizedVec3OrZero(v mgl32.Vec3) mgl32.Vec3 {
	if v.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}
	}
	return v.Normalize()
}

func CanonicalVoxelNormalHemisphere(v mgl32.Vec3) mgl32.Vec3 {
	n := NormalizedVec3OrZero(v)
	if n.LenSqr() <= 1e-8 {
		return n
	}
	ax := float32(math.Abs(float64(n.X())))
	ay := float32(math.Abs(float64(n.Y())))
	az := float32(math.Abs(float64(n.Z())))
	switch {
	case ax >= ay && ax >= az:
		if n.X() < 0 {
			return n.Mul(-1)
		}
	case ay >= ax && ay >= az:
		if n.Y() < 0 {
			return n.Mul(-1)
		}
	default:
		if n.Z() < 0 {
			return n.Mul(-1)
		}
	}
	return n
}

func VoxelNormalHasMultipleComponents(v mgl32.Vec3) bool {
	const threshold float32 = 0.2
	count := 0
	if float32(math.Abs(float64(v.X()))) >= threshold {
		count++
	}
	if float32(math.Abs(float64(v.Y()))) >= threshold {
		count++
	}
	if float32(math.Abs(float64(v.Z()))) >= threshold {
		count++
	}
	return count > 1
}

func axisTieBreakSign(opts VoxelNormalBakeOptions, voxel [3]int, axis int) int {
	if !opts.HasBounds {
		return 1
	}
	center := float32(voxel[axis]) + 0.5
	if center-opts.BoundsMin[axis]+1e-4 < opts.BoundsMax[axis]-center {
		return -1
	}
	return 1
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func signNotZero(v float32) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
