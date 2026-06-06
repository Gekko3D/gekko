package gpu

import (
	"encoding/binary"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	voxelNormalAxisZero     = 0
	voxelNormalAxisPositive = 1
	voxelNormalAxisNegative = 2
	voxelNormalValidBit     = 1 << 6
	voxelNormalTwoSidedBit  = 1 << 7
)

type terrainBakeKey struct {
	group uint32
	coord [3]int
}

type planetBakeKey struct {
	group uint32
	tile  [4]int
}

type voxelNormalBakeContext struct {
	terrain map[terrainBakeKey]*core.VoxelObject
	planet  map[planetBakeKey]*core.VoxelObject
}

type objectDirtyBrickSnapshot struct {
	obj    *core.VoxelObject
	bricks [][6]int
}

func newVoxelNormalBakeContext(scene *core.Scene) voxelNormalBakeContext {
	ctx := voxelNormalBakeContext{
		terrain: make(map[terrainBakeKey]*core.VoxelObject),
		planet:  make(map[planetBakeKey]*core.VoxelObject),
	}
	if scene == nil {
		return ctx
	}
	for _, obj := range scene.Objects {
		if obj == nil {
			continue
		}
		if obj.IsTerrainChunk && obj.TerrainGroupID != 0 && obj.TerrainChunkSize > 0 {
			ctx.terrain[terrainBakeKey{
				group: obj.TerrainGroupID,
				coord: obj.TerrainChunkCoord,
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
		if obj.IsTerrainChunk && obj.TerrainGroupID != 0 && obj.TerrainChunkSize > 0 {
			markTerrainNormalHaloDirty(ctx, obj, snapshot.bricks)
		}
		if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
			markPlanetTileNormalHaloDirty(ctx, obj)
		}
	}
}

func markTerrainNormalHaloDirty(ctx voxelNormalBakeContext, obj *core.VoxelObject, dirtyBricks [][6]int) {
	chunkBricks := obj.TerrainChunkSize / volume.BrickSize
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
				neighborCoord := obj.TerrainChunkCoord
				neighborCoord[axis]--
				neighbor := ctx.terrain[terrainBakeKey{group: obj.TerrainGroupID, coord: neighborCoord}]
				neighborBrick := localBrick
				neighborBrick[axis] = chunkBricks - 1
				markObjectBrickDirty(neighbor, neighborBrick)
			}
			if localBrick[axis] == chunkBricks-1 {
				neighborCoord := obj.TerrainChunkCoord
				neighborCoord[axis]++
				neighbor := ctx.terrain[terrainBakeKey{group: obj.TerrainGroupID, coord: neighborCoord}]
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
				nx, ny, nz, valid, twoSided := bakedVoxelNormal(ctx, obj, global)
				if !valid {
					continue
				}
				buf[normalBase+voxelIdx] = encodeBakedVoxelNormal(nx, ny, nz, twoSided)
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

func encodeBakedVoxelNormal(nx, ny, nz int, twoSided bool) byte {
	axisBits := func(v int) byte {
		if v > 0 {
			return voxelNormalAxisPositive
		}
		if v < 0 {
			return voxelNormalAxisNegative
		}
		return voxelNormalAxisZero
	}

	b := byte(voxelNormalValidBit)
	b |= axisBits(nx)
	b |= axisBits(ny) << 2
	b |= axisBits(nz) << 4
	if twoSided {
		b |= voxelNormalTwoSidedBit
	}
	return b
}

func bakedVoxelNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int) (int, int, int, bool, bool) {
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
	if nx != 0 || ny != 0 || nz != 0 {
		return nx, ny, nz, true, twoSided
	}

	tx, ty, tz := 0, 0, 0
	if occPX == 0 || occNX == 0 {
		tx = axisTieBreakSign(obj, voxel, 0)
	}
	if occPY == 0 || occNY == 0 {
		ty = axisTieBreakSign(obj, voxel, 1)
	}
	if occPZ == 0 || occNZ == 0 {
		tz = axisTieBreakSign(obj, voxel, 2)
	}
	if tx != 0 || ty != 0 || tz != 0 {
		return tx, ty, tz, true, twoSided
	}
	return 0, 0, 0, false, twoSided
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
	if obj.IsTerrainChunk && obj.TerrainGroupID != 0 && obj.TerrainChunkSize > 0 {
		return sampleTerrainOccupancyForBakedNormal(ctx, obj, voxel, localOcc)
	}
	if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
		return samplePlanetTileOccupancyForBakedNormal(ctx, obj, voxel, localOcc)
	}
	return localOcc
}

func sampleTerrainOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int, localOcc bool) bool {
	chunkSize := obj.TerrainChunkSize
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
	neighbor := ctx.terrain[terrainBakeKey{
		group: obj.TerrainGroupID,
		coord: [3]int{
			obj.TerrainChunkCoord[0] + offset[0],
			obj.TerrainChunkCoord[1] + offset[1],
			obj.TerrainChunkCoord[2] + offset[2],
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
