package gpu

import (
	"encoding/binary"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	voxelNormalOctMax      = 127
	voxelNormalValidBit    = 1 << 14
	voxelNormalTwoSidedBit = 1 << 15

	voxelNormalSurfaceFitRadius       = 2
	voxelNormalSurfaceFitMinSamples   = 4
	voxelNormalSurfaceFitMaxError     = 0.08
	voxelNormalDensityComponentCutoff = 0.28
)

type voxelAdjacencyBakeKey struct {
	group uint32
	coord [3]int
}

type planetBakeKey struct {
	group uint32
	tile  [4]int
}

type voxelNormalBakeContext struct {
	adjacency map[voxelAdjacencyBakeKey]*core.VoxelObject
	planet    map[planetBakeKey]*core.VoxelObject
}

type objectDirtyBrickSnapshot struct {
	obj    *core.VoxelObject
	bricks [][6]int
}

func newVoxelNormalBakeContext(scene *core.Scene) voxelNormalBakeContext {
	ctx := voxelNormalBakeContext{
		adjacency: make(map[voxelAdjacencyBakeKey]*core.VoxelObject),
		planet:    make(map[planetBakeKey]*core.VoxelObject),
	}
	if scene == nil {
		return ctx
	}
	for _, obj := range scene.Objects {
		if obj == nil {
			continue
		}
		if group, coord, _, ok := voxelObjectAdjacencyMetadata(obj); ok {
			ctx.adjacency[voxelAdjacencyBakeKey{
				group: group,
				coord: coord,
			}] = obj
		}
		if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
			ctx.planet[planetBakeKey{
				group: obj.PlanetTileGroupID,
				tile:  [4]int{obj.PlanetTileFace, obj.PlanetTileLevel, obj.PlanetTileX, obj.PlanetTileY},
			}] = obj
		}
	}
	return ctx
}

func markCrossObjectNormalHaloDirty(scene *core.Scene, ctx voxelNormalBakeContext) {
	if scene == nil {
		return
	}
	snapshots := make([]objectDirtyBrickSnapshot, 0)
	for _, obj := range scene.Objects {
		if obj == nil || obj.XBrickMap == nil || len(obj.XBrickMap.DirtyBricks) == 0 {
			continue
		}
		snapshot := objectDirtyBrickSnapshot{obj: obj}
		for bKey, dirty := range obj.XBrickMap.DirtyBricks {
			if dirty {
				snapshot.bricks = append(snapshot.bricks, bKey)
			}
		}
		if len(snapshot.bricks) > 0 {
			snapshots = append(snapshots, snapshot)
		}
	}

	for _, snapshot := range snapshots {
		obj := snapshot.obj
		if _, _, _, ok := voxelObjectAdjacencyMetadata(obj); ok {
			markVoxelAdjacencyNormalHaloDirty(ctx, obj, snapshot.bricks)
		}
		if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
			markPlanetTileNormalHaloDirty(ctx, obj)
		}
	}
}

func markVoxelAdjacencyNormalHaloDirty(ctx voxelNormalBakeContext, obj *core.VoxelObject, dirtyBricks [][6]int) {
	group, chunkCoord, chunkSize, ok := voxelObjectAdjacencyMetadata(obj)
	if !ok {
		return
	}
	chunkBricks := chunkSize / volume.BrickSize
	if chunkBricks <= 0 {
		return
	}
	for _, bKey := range dirtyBricks {
		localBrick := [3]int{
			bKey[0]*volume.SectorBricks + bKey[3],
			bKey[1]*volume.SectorBricks + bKey[4],
			bKey[2]*volume.SectorBricks + bKey[5],
		}
		for axis := 0; axis < 3; axis++ {
			if localBrick[axis] == 0 {
				neighborCoord := chunkCoord
				neighborCoord[axis]--
				neighbor := ctx.adjacency[voxelAdjacencyBakeKey{group: group, coord: neighborCoord}]
				neighborBrick := localBrick
				neighborBrick[axis] = chunkBricks - 1
				markObjectBrickDirty(neighbor, neighborBrick)
			}
			if localBrick[axis] == chunkBricks-1 {
				neighborCoord := chunkCoord
				neighborCoord[axis]++
				neighbor := ctx.adjacency[voxelAdjacencyBakeKey{group: group, coord: neighborCoord}]
				neighborBrick := localBrick
				neighborBrick[axis] = 0
				markObjectBrickDirty(neighbor, neighborBrick)
			}
		}
	}
}

func markPlanetTileNormalHaloDirty(ctx voxelNormalBakeContext, obj *core.VoxelObject) {
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			neighbor := ctx.planet[planetBakeKey{
				group: obj.PlanetTileGroupID,
				tile: [4]int{
					obj.PlanetTileFace,
					obj.PlanetTileLevel,
					obj.PlanetTileX + dx,
					obj.PlanetTileY + dy,
				},
			}]
			markAllObjectBricksDirty(neighbor)
		}
	}
}

func markObjectBrickDirty(obj *core.VoxelObject, localBrick [3]int) {
	if obj == nil || obj.XBrickMap == nil {
		return
	}
	if localBrick[0] < 0 || localBrick[1] < 0 || localBrick[2] < 0 {
		return
	}
	sKey := [3]int{
		localBrick[0] / volume.SectorBricks,
		localBrick[1] / volume.SectorBricks,
		localBrick[2] / volume.SectorBricks,
	}
	bx := localBrick[0] % volume.SectorBricks
	by := localBrick[1] % volume.SectorBricks
	bz := localBrick[2] % volume.SectorBricks
	if sector := obj.XBrickMap.Sectors[sKey]; sector == nil || sector.GetBrick(bx, by, bz) == nil {
		return
	}
	obj.XBrickMap.DirtyBricks[[6]int{sKey[0], sKey[1], sKey[2], bx, by, bz}] = true
}

func markAllObjectBricksDirty(obj *core.VoxelObject) {
	if obj == nil || obj.XBrickMap == nil {
		return
	}
	for sKey, sector := range obj.XBrickMap.Sectors {
		if sector == nil {
			continue
		}
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (uint64(1) << i)) == 0 {
				continue
			}
			bx, by, bz := i%4, (i/4)%4, i/16
			obj.XBrickMap.DirtyBricks[[6]int{sKey[0], sKey[1], sKey[2], bx, by, bz}] = true
		}
	}
}

func voxelObjectAdjacencyMetadata(obj *core.VoxelObject) (uint32, [3]int, int, bool) {
	if obj == nil {
		return 0, [3]int{}, 0, false
	}
	if obj.VoxelAdjacencyGroupID != 0 && obj.VoxelAdjacencyChunkSize > 0 {
		return obj.VoxelAdjacencyGroupID, obj.VoxelAdjacencyChunkCoord, obj.VoxelAdjacencyChunkSize, true
	}
	if obj.IsTerrainChunk && obj.TerrainGroupID != 0 && obj.TerrainChunkSize > 0 {
		return obj.TerrainGroupID, obj.TerrainChunkCoord, obj.TerrainChunkSize, true
	}
	return 0, [3]int{}, 0, false
}

func buildVoxelAuxBytes(ctx voxelNormalBakeContext, obj *core.VoxelObject, brick *volume.Brick, brickOrigin [3]int) []byte {
	buf := make([]byte, VoxelAuxRecordBytes)
	words := brick.DenseOccupancyWords()
	for i, word := range words {
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], word)
	}

	normalBase := volume.DenseOccupancyWordCount * 4
	for z := 0; z < volume.BrickSize; z++ {
		for y := 0; y < volume.BrickSize; y++ {
			for x := 0; x < volume.BrickSize; x++ {
				if !brickVoxelOccupied(brick, x, y, z) {
					continue
				}
				voxelIdx := denseOccupancyLinearIndexLocal(x, y, z)
				global := [3]int{brickOrigin[0] + x, brickOrigin[1] + y, brickOrigin[2] + z}
				normal, valid, twoSided := bakedVoxelNormal(ctx, obj, global)
				if !valid {
					continue
				}
				binary.LittleEndian.PutUint16(buf[normalBase+voxelIdx*2:normalBase+voxelIdx*2+2], encodeBakedVoxelNormal(normal, twoSided))
			}
		}
	}
	return buf
}

func brickVoxelOccupied(brick *volume.Brick, x, y, z int) bool {
	if brick == nil {
		return false
	}
	if brick.Flags&volume.BrickFlagSolid != 0 {
		return true
	}
	return brick.Payload[x][y][z] != 0
}

func denseOccupancyLinearIndexLocal(x, y, z int) int {
	return x + y*volume.BrickSize + z*volume.BrickSize*volume.BrickSize
}

func encodeBakedVoxelNormal(normal mgl32.Vec3, twoSided bool) uint16 {
	n := normalizedVec3OrZero(normal)
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
		return uint16(math.Round(float64(v * voxelNormalOctMax)))
	}

	b := uint16(voxelNormalValidBit)
	b |= pack(ox)
	b |= pack(oy) << 7
	if twoSided {
		b |= voxelNormalTwoSidedBit
	}
	return b
}

func bakedVoxelNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int) (mgl32.Vec3, bool, bool) {
	occPX := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0] + 1, voxel[1], voxel[2]}))
	occNX := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0] - 1, voxel[1], voxel[2]}))
	occPY := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0], voxel[1] + 1, voxel[2]}))
	occNY := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0], voxel[1] - 1, voxel[2]}))
	occPZ := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0], voxel[1], voxel[2] + 1}))
	occNZ := boolToInt(sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0], voxel[1], voxel[2] - 1}))

	nx := occNX - occPX
	ny := occNY - occPY
	nz := occNZ - occPZ
	twoSided := (occPX == 0 && occNX == 0) || (occPY == 0 && occNY == 0) || (occPZ == 0 && occNZ == 0)

	tx, ty, tz := 0, 0, 0
	exposedAxisPairs := 0
	if occPX == 0 || occNX == 0 {
		tx = axisTieBreakSign(obj, voxel, 0)
		exposedAxisPairs++
	}
	if occPY == 0 || occNY == 0 {
		ty = axisTieBreakSign(obj, voxel, 1)
		exposedAxisPairs++
	}
	if occPZ == 0 || occNZ == 0 {
		tz = axisTieBreakSign(obj, voxel, 2)
		exposedAxisPairs++
	}
	if normal, ok := densityGradientVoxelNormal(ctx, obj, voxel); ok {
		if twoSided && voxelNormalHasMultipleComponents(normal) {
			normal = canonicalVoxelNormalHemisphere(normal)
		}
		return normal, true, twoSided
	}
	if nx != 0 || ny != 0 || nz != 0 {
		return normalizedVec3OrZero(mgl32.Vec3{float32(nx), float32(ny), float32(nz)}), true, twoSided
	}
	if exposedAxisPairs <= 1 {
		if tx != 0 || ty != 0 || tz != 0 {
			return normalizedVec3OrZero(mgl32.Vec3{float32(tx), float32(ty), float32(tz)}), true, twoSided
		}
		return mgl32.Vec3{}, false, twoSided
	}

	if normal, ok := fittedSurfaceVoxelNormal(ctx, obj, voxel, mgl32.Vec3{float32(tx), float32(ty), float32(tz)}); ok {
		if twoSided {
			normal = canonicalVoxelNormalHemisphere(normal)
		}
		return normal, true, twoSided
	}
	if tx != 0 || ty != 0 || tz != 0 {
		return normalizedVec3OrZero(mgl32.Vec3{float32(tx), float32(ty), float32(tz)}), true, twoSided
	}
	return mgl32.Vec3{}, false, twoSided
}

func fittedSurfaceVoxelNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int, orient mgl32.Vec3) (mgl32.Vec3, bool) {
	type weightedOffset struct {
		x, y, z int
		weight  float64
	}
	offsets := make([]weightedOffset, 0, 32)
	for dz := -voxelNormalSurfaceFitRadius; dz <= voxelNormalSurfaceFitRadius; dz++ {
		for dy := -voxelNormalSurfaceFitRadius; dy <= voxelNormalSurfaceFitRadius; dy++ {
			for dx := -voxelNormalSurfaceFitRadius; dx <= voxelNormalSurfaceFitRadius; dx++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				dist2 := dx*dx + dy*dy + dz*dz
				if dist2 > voxelNormalSurfaceFitRadius*voxelNormalSurfaceFitRadius {
					continue
				}
				if !sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0] + dx, voxel[1] + dy, voxel[2] + dz}) {
					continue
				}
				offsets = append(offsets, weightedOffset{x: dx, y: dy, z: dz, weight: 1 / float64(dist2)})
			}
		}
	}
	if len(offsets) < voxelNormalSurfaceFitMinSamples {
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
	if bestScore > voxelNormalSurfaceFitMaxError || best.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, false
	}
	if orient.LenSqr() > 1e-8 && best.Dot(orient) < 0 {
		best = best.Mul(-1)
	}
	return normalizedVec3OrZero(best), true
}

func densityGradientVoxelNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int) (mgl32.Vec3, bool) {
	gx, gy, gz := 0.0, 0.0, 0.0
	for dz := -voxelNormalSurfaceFitRadius; dz <= voxelNormalSurfaceFitRadius; dz++ {
		for dy := -voxelNormalSurfaceFitRadius; dy <= voxelNormalSurfaceFitRadius; dy++ {
			for dx := -voxelNormalSurfaceFitRadius; dx <= voxelNormalSurfaceFitRadius; dx++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				dist2 := dx*dx + dy*dy + dz*dz
				if dist2 > voxelNormalSurfaceFitRadius*voxelNormalSurfaceFitRadius {
					continue
				}
				if !sampleOccupancyForBakedNormal(ctx, obj, [3]int{voxel[0] + dx, voxel[1] + dy, voxel[2] + dz}) {
					continue
				}
				weight := 1 / float64(dist2)
				gx -= float64(dx) * weight
				gy -= float64(dy) * weight
				gz -= float64(dz) * weight
			}
		}
	}
	return normalizedGradientVoxelNormal(gx, gy, gz)
}

func normalizedGradientVoxelNormal(gx, gy, gz float64) (mgl32.Vec3, bool) {
	maxAbs := math.Max(math.Abs(gx), math.Max(math.Abs(gy), math.Abs(gz)))
	if maxAbs <= 1e-6 {
		return mgl32.Vec3{}, false
	}
	filter := func(v float64) float32 {
		if math.Abs(v) < maxAbs*voxelNormalDensityComponentCutoff {
			return 0
		}
		return float32(v)
	}
	n := mgl32.Vec3{filter(gx), filter(gy), filter(gz)}
	if n.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, false
	}
	return normalizedVec3OrZero(n), true
}

func normalizedVec3OrZero(v mgl32.Vec3) mgl32.Vec3 {
	if v.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}
	}
	return v.Normalize()
}

func canonicalVoxelNormalHemisphere(v mgl32.Vec3) mgl32.Vec3 {
	n := normalizedVec3OrZero(v)
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

func voxelNormalHasMultipleComponents(v mgl32.Vec3) bool {
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

func axisTieBreakSign(obj *core.VoxelObject, voxel [3]int, axis int) int {
	if obj == nil || obj.XBrickMap == nil {
		return 1
	}
	minB, maxB := obj.XBrickMap.ComputeAABB()
	center := float32(voxel[axis]) + 0.5
	if center-minB[axis]+1e-4 < maxB[axis]-center {
		return -1
	}
	return 1
}

func sampleOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int) bool {
	if obj == nil || obj.XBrickMap == nil {
		return false
	}

	localOcc, _ := obj.XBrickMap.GetVoxel(voxel[0], voxel[1], voxel[2])
	if _, _, _, ok := voxelObjectAdjacencyMetadata(obj); ok {
		return sampleVoxelAdjacencyOccupancyForBakedNormal(ctx, obj, voxel, localOcc)
	}
	if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
		return samplePlanetTileOccupancyForBakedNormal(ctx, obj, voxel, localOcc)
	}
	return localOcc
}

func sampleVoxelAdjacencyOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int, localOcc bool) bool {
	group, chunkCoord, chunkSize, ok := voxelObjectAdjacencyMetadata(obj)
	if !ok {
		return localOcc
	}
	if voxel[0] >= 0 && voxel[0] < chunkSize &&
		voxel[1] >= 0 && voxel[1] < chunkSize &&
		voxel[2] >= 0 && voxel[2] < chunkSize {
		return localOcc
	}

	offset := [3]int{
		floorDivInt(voxel[0], chunkSize),
		floorDivInt(voxel[1], chunkSize),
		floorDivInt(voxel[2], chunkSize),
	}
	neighbor := ctx.adjacency[voxelAdjacencyBakeKey{
		group: group,
		coord: [3]int{
			chunkCoord[0] + offset[0],
			chunkCoord[1] + offset[1],
			chunkCoord[2] + offset[2],
		},
	}]
	if neighbor == nil || neighbor.XBrickMap == nil {
		return false
	}
	nv := [3]int{
		positiveModInt(voxel[0], chunkSize),
		positiveModInt(voxel[1], chunkSize),
		positiveModInt(voxel[2], chunkSize),
	}
	occ, _ := neighbor.XBrickMap.GetVoxel(nv[0], nv[1], nv[2])
	return occ
}

func samplePlanetTileOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int, localOcc bool) bool {
	localPos := mgl32.Vec3{float32(voxel[0]) + 0.5, float32(voxel[1]) + 0.5, float32(voxel[2]) + 0.5}
	if localOcc || pointInsideLocalBounds(localPos, obj, 0.25) {
		return localOcc
	}

	worldPos := obj.Transform.ObjectToWorld().Mul4x1(localPos.Vec4(1)).Vec3()
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			neighbor := ctx.planet[planetBakeKey{
				group: obj.PlanetTileGroupID,
				tile: [4]int{
					obj.PlanetTileFace,
					obj.PlanetTileLevel,
					obj.PlanetTileX + dx,
					obj.PlanetTileY + dy,
				},
			}]
			if samplePlanetTileNeighborOccupancy(worldPos, neighbor) {
				return true
			}
		}
	}
	return localOcc
}

func samplePlanetTileNeighborOccupancy(worldPos mgl32.Vec3, neighbor *core.VoxelObject) bool {
	if neighbor == nil || neighbor.XBrickMap == nil {
		return false
	}
	neighborPos := neighbor.Transform.WorldToObject().Mul4x1(worldPos.Vec4(1)).Vec3()
	if !pointInsideLocalBounds(neighborPos, neighbor, 0.75) {
		return false
	}
	occ, _ := neighbor.XBrickMap.GetVoxel(
		int(float32Floor(neighborPos.X())),
		int(float32Floor(neighborPos.Y())),
		int(float32Floor(neighborPos.Z())),
	)
	return occ
}

func pointInsideLocalBounds(p mgl32.Vec3, obj *core.VoxelObject, padding float32) bool {
	if obj == nil || obj.XBrickMap == nil {
		return false
	}
	minB, maxB := obj.XBrickMap.ComputeAABB()
	return p.X() >= minB.X()-padding && p.Y() >= minB.Y()-padding && p.Z() >= minB.Z()-padding &&
		p.X() <= maxB.X()+padding && p.Y() <= maxB.Y()+padding && p.Z() <= maxB.Z()+padding
}

func floorDivInt(a, b int) int {
	q := a / b
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		q--
	}
	return q
}

func positiveModInt(a, b int) int {
	r := a % b
	if r < 0 {
		r += absInt(b)
	}
	return r
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func float32Floor(v float32) float32 {
	return float32(math.Floor(float64(v)))
}
