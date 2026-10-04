package gpu

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	voxelNormalOctMax      = volume.VoxelNormalOctMax
	voxelNormalValidBit    = volume.VoxelNormalValidBit
	voxelNormalTwoSidedBit = volume.VoxelNormalTwoSidedBit

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
		if obj == nil || obj.RenderVoxelMap() == nil {
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
	markCrossObjectNormalHaloDirtyWithContext(scene, func() voxelNormalBakeContext { return ctx })
}

// The getter and its live object references belong only to this update. Dirty
// halo work and runtime baking share one full-scene context, built on demand.
func (m *GpuBufferManager) prepareVoxelNormalBakeContext(scene *core.Scene) func() voxelNormalBakeContext {
	m.VoxelNormalContextObjectVisitsLastUpdate = 0
	var ctx voxelNormalBakeContext
	built := false
	context := func() voxelNormalBakeContext {
		if !built {
			ctx = newVoxelNormalBakeContext(scene)
			built = true
			m.VoxelNormalContextBuildCount++
			if scene != nil {
				m.VoxelNormalContextObjectVisitsLastUpdate = len(scene.Objects)
			}
		}
		return ctx
	}
	markCrossObjectNormalHaloDirtyWithContext(scene, context)
	return context
}

func markCrossObjectNormalHaloDirtyWithContext(scene *core.Scene, context func() voxelNormalBakeContext) {
	if scene == nil {
		return
	}
	snapshots := make([]objectDirtyBrickSnapshot, 0)
	for _, obj := range scene.Objects {
		if obj == nil || obj.RenderVoxelMap() == nil || len(obj.RenderVoxelMap().DirtyBricks) == 0 {
			continue
		}
		snapshot := objectDirtyBrickSnapshot{obj: obj}
		for bKey, dirty := range obj.RenderVoxelMap().DirtyBricks {
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
			markVoxelAdjacencyNormalHaloDirty(context(), obj, snapshot.bricks)
		}
		if obj.IsPlanetTile && obj.PlanetTileGroupID != 0 {
			markPlanetTileNormalHaloDirty(context(), obj)
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
		var offsets [3][3]int
		counts := [3]int{1, 1, 1}
		for axis := 0; axis < 3; axis++ {
			if localBrick[axis] < 0 || localBrick[axis] >= chunkBricks {
				offsets[axis][0] = floorDivInt(localBrick[axis], chunkBricks)
				continue
			}
			if localBrick[axis] == 0 {
				offsets[axis][counts[axis]] = -1
				counts[axis]++
			}
			if localBrick[axis] == chunkBricks-1 {
				offsets[axis][counts[axis]] = 1
				counts[axis]++
			}
		}
		for xi := 0; xi < counts[0]; xi++ {
			for yi := 0; yi < counts[1]; yi++ {
				for zi := 0; zi < counts[2]; zi++ {
					offset := [3]int{offsets[0][xi], offsets[1][yi], offsets[2][zi]}
					if offset == ([3]int{}) {
						continue
					}
					neighborCoord := [3]int{
						chunkCoord[0] + offset[0],
						chunkCoord[1] + offset[1],
						chunkCoord[2] + offset[2],
					}
					neighborBrick := localBrick
					for axis := 0; axis < 3; axis++ {
						switch {
						case localBrick[axis] < 0 || localBrick[axis] >= chunkBricks:
							neighborBrick[axis] = positiveModInt(localBrick[axis], chunkBricks)
						case offset[axis] < 0:
							neighborBrick[axis] = chunkBricks - 1
						case offset[axis] > 0:
							neighborBrick[axis] = 0
						}
					}
					neighbor := ctx.adjacency[voxelAdjacencyBakeKey{group: group, coord: neighborCoord}]
					markObjectBrickDirty(neighbor, neighborBrick)
				}
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
	if obj == nil || obj.RenderVoxelMap() == nil {
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
	if sector := obj.RenderVoxelMap().Sectors[sKey]; sector == nil || sector.GetBrick(bx, by, bz) == nil {
		return
	}
	obj.RenderVoxelMap().MarkBrickNormalDirty([6]int{sKey[0], sKey[1], sKey[2], bx, by, bz})
}

func markAllObjectBricksDirty(obj *core.VoxelObject) {
	if obj == nil || obj.RenderVoxelMap() == nil {
		return
	}
	for sKey, sector := range obj.RenderVoxelMap().Sectors {
		if sector == nil {
			continue
		}
		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (uint64(1) << i)) == 0 {
				continue
			}
			bx, by, bz := i%4, (i/4)%4, i/16
			obj.RenderVoxelMap().MarkBrickNormalDirty([6]int{sKey[0], sKey[1], sKey[2], bx, by, bz})
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

func buildVoxelAuxBytesWithContext(context func() voxelNormalBakeContext, obj *core.VoxelObject, brick *volume.Brick, brickOrigin [3]int) []byte {
	if brick != nil && len(brick.PrecomputedAux) == VoxelAuxRecordBytes {
		return brick.PrecomputedAux
	}
	return buildVoxelAuxBytes(context(), obj, brick, brickOrigin)
}

func buildVoxelAuxBytes(ctx voxelNormalBakeContext, obj *core.VoxelObject, brick *volume.Brick, brickOrigin [3]int) []byte {
	if brick != nil && len(brick.PrecomputedAux) == VoxelAuxRecordBytes {
		return brick.PrecomputedAux
	}
	opts := volume.VoxelNormalBakeOptions{}
	if obj != nil && obj.RenderVoxelMap() != nil {
		minB, maxB := obj.RenderVoxelMap().ComputeAABB()
		opts.BoundsMin = minB
		opts.BoundsMax = maxB
		opts.HasBounds = true
		opts.SampleOccupancy = func(voxel [3]int) bool {
			return sampleOccupancyForBakedNormal(ctx, obj, voxel)
		}
	}
	return volume.BuildVoxelAuxBytes(brick, brickOrigin, opts)
}

func brickVoxelOccupied(brick *volume.Brick, x, y, z int) bool {
	return volume.BrickVoxelOccupied(brick, x, y, z)
}

func denseOccupancyLinearIndexLocal(x, y, z int) int {
	return volume.DenseOccupancyLinearIndexLocal(x, y, z)
}

func encodeBakedVoxelNormal(normal mgl32.Vec3, twoSided bool) uint16 {
	return volume.EncodeBakedVoxelNormal(normal, twoSided)
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
	if obj == nil || obj.RenderVoxelMap() == nil {
		return 1
	}
	minB, maxB := obj.RenderVoxelMap().ComputeAABB()
	center := float32(voxel[axis]) + 0.5
	if center-minB[axis]+1e-4 < maxB[axis]-center {
		return -1
	}
	return 1
}

func sampleOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int) bool {
	if obj == nil || obj.RenderVoxelMap() == nil {
		return false
	}

	localOcc, _ := obj.RenderVoxelMap().GetVoxel(voxel[0], voxel[1], voxel[2])
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
	if neighbor == nil || neighbor.RenderVoxelMap() == nil {
		return false
	}
	nv := [3]int{
		positiveModInt(voxel[0], chunkSize),
		positiveModInt(voxel[1], chunkSize),
		positiveModInt(voxel[2], chunkSize),
	}
	occ, _ := neighbor.RenderVoxelMap().GetVoxel(nv[0], nv[1], nv[2])
	return occ
}

func samplePlanetTileOccupancyForBakedNormal(ctx voxelNormalBakeContext, obj *core.VoxelObject, voxel [3]int, localOcc bool) bool {
	localPos := mgl32.Vec3{float32(voxel[0]) + 0.5, float32(voxel[1]) + 0.5, float32(voxel[2]) + 0.5}
	if localOcc || pointInsideLocalBounds(localPos, obj, 0.25) {
		return localOcc
	}

	worldPos := obj.RenderObjectToWorld().Mul4x1(localPos.Vec4(1)).Vec3()
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
	if neighbor == nil || neighbor.RenderVoxelMap() == nil {
		return false
	}
	neighborPos := neighbor.RenderWorldToObject().Mul4x1(worldPos.Vec4(1)).Vec3()
	if !pointInsideLocalBounds(neighborPos, neighbor, 0.75) {
		return false
	}
	occ, _ := neighbor.RenderVoxelMap().GetVoxel(
		int(float32Floor(neighborPos.X())),
		int(float32Floor(neighborPos.Y())),
		int(float32Floor(neighborPos.Z())),
	)
	return occ
}

func pointInsideLocalBounds(p mgl32.Vec3, obj *core.VoxelObject, padding float32) bool {
	if obj == nil || obj.RenderVoxelMap() == nil {
		return false
	}
	minB, maxB := obj.RenderVoxelMap().ComputeAABB()
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
