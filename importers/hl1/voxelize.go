package hl1

import (
	"fmt"
	"math"
	"sort"
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

const DefaultMaxSolidSampleCells int64 = 20_000_000
const DefaultSolidBandDepth = 24

type VoxelizeOptions struct {
	VoxelResolution          float32
	FillClosed               bool
	MaxSolidSampleCells      int64
	StructuralFillMaterialID int
	SolidBandDepth           int
	TextureStore             *TextureStore
	MaterialColors           map[int][4]uint8
}

type VoxelizeResult struct {
	Voxels                []importcommon.Voxel
	Materials             []importcommon.Material
	BoundsMin             [3]int
	BoundsMax             [3]int
	SurfaceCount          int
	FilledCount           int
	SolidCount            int
	EmptyCount            int
	PlayableEmptyCount    int
	UnreachableEmptyCount int
	SampledCount          int64
	FloodSkipped          bool
}

func VoxelizeFacesCPU(faces []Face, opts VoxelizeOptions) VoxelizeResult {
	if opts.VoxelResolution <= 0 {
		opts.VoxelResolution = 0.1
	}
	voxels := make(map[[3]int]importcommon.Voxel)
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		voxelizeFaceSurface(face, opts, voxels)
	}
	surfaceCount := len(voxels)
	if opts.FillClosed {
		fillClosedInterior(voxels)
	}
	out := voxelsToSortedSlice(voxels)
	result := VoxelizeResult{
		Voxels:       out,
		Materials:    voxelizeResultMaterials(opts),
		SurfaceCount: surfaceCount,
		FilledCount:  len(voxels) - surfaceCount,
	}
	if len(out) > 0 {
		result.BoundsMin = [3]int{out[0].X, out[0].Y, out[0].Z}
		result.BoundsMax = result.BoundsMin
		for _, voxel := range out[1:] {
			result.BoundsMin[0] = min(result.BoundsMin[0], voxel.X)
			result.BoundsMin[1] = min(result.BoundsMin[1], voxel.Y)
			result.BoundsMin[2] = min(result.BoundsMin[2], voxel.Z)
			result.BoundsMax[0] = max(result.BoundsMax[0], voxel.X)
			result.BoundsMax[1] = max(result.BoundsMax[1], voxel.Y)
			result.BoundsMax[2] = max(result.BoundsMax[2], voxel.Z)
		}
	}
	return result
}

func VoxelizeBSPSolidCPU(bsp *BSP, faces []Face, entities []importcommon.Entity, opts VoxelizeOptions) (VoxelizeResult, error) {
	if bsp == nil {
		return VoxelizeResult{}, fmt.Errorf("bsp is nil")
	}
	if opts.VoxelResolution <= 0 {
		opts.VoxelResolution = 0.1
	}
	if opts.MaxSolidSampleCells <= 0 {
		opts.MaxSolidSampleCells = DefaultMaxSolidSampleCells
	}
	if opts.SolidBandDepth <= 0 {
		opts.SolidBandDepth = DefaultSolidBandDepth
	}
	bounds, ok := voxelizedFaceBoundsGekko(faces)
	if !ok {
		bounds, ok = bsp.WorldBoundsGekko()
	}
	if !ok {
		return VoxelizeResult{}, fmt.Errorf("world bounds are missing")
	}
	minB, maxB := boundsToVoxelBounds(bounds, opts.VoxelResolution)
	minB = [3]int{minB[0] - opts.SolidBandDepth, minB[1] - opts.SolidBandDepth, minB[2] - opts.SolidBandDepth}
	maxB = [3]int{maxB[0] + opts.SolidBandDepth, maxB[1] + opts.SolidBandDepth, maxB[2] + opts.SolidBandDepth}
	fullSampled, err := boundedCellCount(minB, maxB)
	if err != nil {
		return VoxelizeResult{}, err
	}
	voxels := make(map[[3]int]importcommon.Voxel)
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		voxelizeFaceSurface(face, opts, voxels)
	}
	surfaceCount := len(voxels)
	playableEmpty := make(map[[3]int]struct{})
	floodSkipped := false
	var candidates map[[3]int]struct{}
	if fullSampled <= opts.MaxSolidSampleCells {
		playableEmpty, err = floodPlayableEmptyCells(bsp, entities, voxels, minB, maxB, opts)
		if err != nil {
			return VoxelizeResult{}, err
		}
		candidates, err = solidBandCandidatesFromPlayableEmpty(bsp, playableEmpty, minB, maxB, opts)
		if err != nil {
			return VoxelizeResult{}, err
		}
	} else {
		floodSkipped = true
		candidates, err = solidBandCandidatesFromSurfaceFaces(bsp, faces, minB, maxB, opts)
		if err != nil {
			return VoxelizeResult{}, err
		}
	}
	fillMaterialID := opts.StructuralFillMaterialID
	fillMaterials := map[[3]int]int{}
	if fillMaterialID <= 0 {
		fillMaterialID = dominantSurfaceMaterialID(voxels)
		fillMaterials = propagateStructuralFillMaterials(voxels, candidates, fillMaterialID)
	}
	solidCount := 0
	for key := range candidates {
		solidCount++
		if _, exists := voxels[key]; exists {
			continue
		}
		materialID := fillMaterialID
		if propagated := fillMaterials[key]; propagated > 0 {
			materialID = propagated
		}
		voxels[key] = importcommon.Voxel{
			X:          key[0],
			Y:          key[1],
			Z:          key[2],
			Palette:    uint8(min(max(materialID, 1), 255)),
			MaterialID: materialID,
			SolidKind:  "structural_fill",
		}
	}
	removedSurface, removedFill := carveLiquidContentVoxels(bsp, voxels, opts)
	surfaceCount = max(0, surfaceCount-removedSurface)
	solidCount = max(0, solidCount-removedFill)
	out := voxelsToSortedSlice(voxels)
	sampled := int64(len(playableEmpty) + len(candidates))
	result := VoxelizeResult{
		Voxels:                out,
		Materials:             voxelizeResultMaterials(opts),
		BoundsMin:             minB,
		BoundsMax:             maxB,
		SurfaceCount:          surfaceCount,
		FilledCount:           len(voxels) - surfaceCount,
		SolidCount:            solidCount,
		EmptyCount:            len(playableEmpty),
		PlayableEmptyCount:    len(playableEmpty),
		UnreachableEmptyCount: 0,
		SampledCount:          sampled,
		FloodSkipped:          floodSkipped || len(playableEmpty) == 0,
	}
	if len(out) > 0 {
		result.BoundsMin = [3]int{out[0].X, out[0].Y, out[0].Z}
		result.BoundsMax = result.BoundsMin
		for _, voxel := range out[1:] {
			result.BoundsMin[0] = min(result.BoundsMin[0], voxel.X)
			result.BoundsMin[1] = min(result.BoundsMin[1], voxel.Y)
			result.BoundsMin[2] = min(result.BoundsMin[2], voxel.Z)
			result.BoundsMax[0] = max(result.BoundsMax[0], voxel.X)
			result.BoundsMax[1] = max(result.BoundsMax[1], voxel.Y)
			result.BoundsMax[2] = max(result.BoundsMax[2], voxel.Z)
		}
	}
	return result, nil
}

func dominantSurfaceMaterialID(voxels map[[3]int]importcommon.Voxel) int {
	bestID := 1
	bestCount := 0
	counts := make(map[int]int)
	for _, voxel := range voxels {
		if voxel.MaterialID <= 0 || voxel.SolidKind == "structural_fill" || voxel.SolidKind == "interior_fill" {
			continue
		}
		counts[voxel.MaterialID]++
	}
	for id, count := range counts {
		if count > bestCount || (count == bestCount && id < bestID) {
			bestID = id
			bestCount = count
		}
	}
	return bestID
}

func propagateStructuralFillMaterials(surface map[[3]int]importcommon.Voxel, candidates map[[3]int]struct{}, fallbackMaterialID int) map[[3]int]int {
	if len(surface) == 0 || len(candidates) == 0 {
		return nil
	}
	result := make(map[[3]int]int, len(candidates))
	queue := make([][3]int, 0, len(surface)+len(candidates))
	for _, key := range sortedVoxelKeys(surface) {
		voxel := surface[key]
		if voxel.MaterialID <= 0 || voxel.SolidKind == "structural_fill" || voxel.SolidKind == "interior_fill" {
			continue
		}
		queue = append(queue, key)
	}
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for head := 0; head < len(queue); head++ {
		curr := queue[head]
		materialID := fallbackMaterialID
		if voxel, ok := surface[curr]; ok && voxel.MaterialID > 0 {
			materialID = voxel.MaterialID
		} else if assigned := result[curr]; assigned > 0 {
			materialID = assigned
		}
		for _, dir := range dirs {
			next := [3]int{curr[0] + dir[0], curr[1] + dir[1], curr[2] + dir[2]}
			if _, ok := candidates[next]; !ok {
				continue
			}
			if _, seen := result[next]; seen {
				continue
			}
			result[next] = materialID
			queue = append(queue, next)
		}
	}
	return result
}

func sortedVoxelKeys(voxels map[[3]int]importcommon.Voxel) [][3]int {
	keys := make([][3]int, 0, len(voxels))
	for key := range voxels {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][2] < keys[j][2]
	})
	return keys
}

func carveLiquidContentVoxels(bsp *BSP, voxels map[[3]int]importcommon.Voxel, opts VoxelizeOptions) (int, int) {
	if bsp == nil || len(voxels) == 0 {
		return 0, 0
	}
	removedSurface := 0
	removedFill := 0
	for key, voxel := range voxels {
		contents, err := bsp.PointContentsGekko(0, voxelCenter(key, opts.VoxelResolution))
		if err != nil || !IsLiquidContent(contents) {
			continue
		}
		if voxel.SolidKind == "structural_fill" {
			removedFill++
		} else {
			removedSurface++
		}
		delete(voxels, key)
	}
	return removedSurface, removedFill
}

func solidCandidateKeysFromSurfaceFlood(bsp *BSP, surface map[[3]int]importcommon.Voxel, minB, maxB [3]int, opts VoxelizeOptions) (map[[3]int]struct{}, bool, error) {
	if len(surface) == 0 {
		return nil, false, nil
	}
	solid := make(map[[3]int]struct{})
	checked := make(map[[3]int]struct{}, len(surface))
	queue := make([][3]int, 0, len(surface))
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	addIfSolid := func(key [3]int) error {
		if !insideBounds(key, minB, maxB) {
			return nil
		}
		if _, ok := checked[key]; ok {
			return nil
		}
		checked[key] = struct{}{}
		contents, err := bsp.PointContentsGekko(0, voxelCenter(key, opts.VoxelResolution))
		if err != nil {
			return err
		}
		if !IsSolidContent(contents) {
			return nil
		}
		solid[key] = struct{}{}
		queue = append(queue, key)
		if int64(len(solid)) > opts.MaxSolidSampleCells {
			return fmt.Errorf("solid voxel candidate flood too large: %d cells exceeds cap %d", len(solid), opts.MaxSolidSampleCells)
		}
		return nil
	}
	for key := range surface {
		if err := addIfSolid(key); err != nil {
			return nil, true, err
		}
		for _, dir := range dirs {
			next := [3]int{key[0] + dir[0], key[1] + dir[1], key[2] + dir[2]}
			if err := addIfSolid(next); err != nil {
				return nil, true, err
			}
		}
	}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr[0] + dir[0], curr[1] + dir[1], curr[2] + dir[2]}
			if err := addIfSolid(next); err != nil {
				return nil, true, err
			}
		}
	}
	return solid, true, nil
}

func solidCandidateKeys(bsp *BSP, clampMin, clampMax [3]int, opts VoxelizeOptions) (map[[3]int]struct{}, bool, error) {
	candidates := make(map[[3]int]struct{})
	usedLeafBounds := false
	for _, leaf := range bsp.Leafs {
		if !IsSolidContent(leaf.Contents) || !leafHasUsableBounds(leaf) {
			continue
		}
		leafBounds := HammerBoundsToGekko(
			importcommon.Vec3{X: float32(leaf.Min[0]), Y: float32(leaf.Min[1]), Z: float32(leaf.Min[2])},
			importcommon.Vec3{X: float32(leaf.Max[0]), Y: float32(leaf.Max[1]), Z: float32(leaf.Max[2])},
		)
		minB, maxB := boundsToVoxelBounds(leafBounds, opts.VoxelResolution)
		minB, maxB, ok := clampVoxelBounds(minB, maxB, clampMin, clampMax)
		if !ok {
			continue
		}
		usedLeafBounds = true
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					candidates[[3]int{x, y, z}] = struct{}{}
					if int64(len(candidates)) > opts.MaxSolidSampleCells {
						return nil, true, fmt.Errorf("solid voxel candidate grid too large: %d cells exceeds cap %d", len(candidates), opts.MaxSolidSampleCells)
					}
				}
			}
		}
	}
	return candidates, usedLeafBounds, nil
}

func leafHasUsableBounds(leaf Leaf) bool {
	for axis := 0; axis < 3; axis++ {
		if leaf.Min[axis] != 0 || leaf.Max[axis] != 0 {
			return true
		}
	}
	return false
}

func boundsToVoxelBounds(bounds importcommon.Bounds, resolution float32) ([3]int, [3]int) {
	return [3]int{
			int(math.Floor(float64(bounds.Min.X / resolution))),
			int(math.Floor(float64(bounds.Min.Y / resolution))),
			int(math.Floor(float64(bounds.Min.Z / resolution))),
		}, [3]int{
			int(math.Floor(float64(bounds.Max.X / resolution))),
			int(math.Floor(float64(bounds.Max.Y / resolution))),
			int(math.Floor(float64(bounds.Max.Z / resolution))),
		}
}

func clampVoxelBounds(minB, maxB, clampMin, clampMax [3]int) ([3]int, [3]int, bool) {
	for axis := 0; axis < 3; axis++ {
		minB[axis] = max(minB[axis], clampMin[axis])
		maxB[axis] = min(maxB[axis], clampMax[axis])
		if maxB[axis] < minB[axis] {
			return minB, maxB, false
		}
	}
	return minB, maxB, true
}

func voxelizeFaceSurface(face Face, opts VoxelizeOptions, voxels map[[3]int]importcommon.Voxel) {
	for _, key := range rasterizeFaceSurfaceKeys(face, opts) {
		if _, exists := voxels[key]; exists {
			continue
		}
		if faceCutoutTexelIsEmpty(face, key, opts) {
			continue
		}
		materialID := face.TextureID + 1
		palette := uint8(min(max(materialID, 1), 255))
		if baked, ok := bakedFacePalette(face, key, opts); ok {
			materialID = baked
			palette = uint8(baked)
		}
		voxels[key] = importcommon.Voxel{
			X:          key[0],
			Y:          key[1],
			Z:          key[2],
			Palette:    palette,
			MaterialID: materialID,
			SolidKind:  materialKind(face.TextureName),
		}
	}
}

func voxelizeResultMaterials(opts VoxelizeOptions) []importcommon.Material {
	if opts.TextureStore == nil && len(opts.MaterialColors) == 0 {
		return nil
	}
	return FixedBakedPaletteMaterials()
}

func bakedFacePalette(face Face, key [3]int, opts VoxelizeOptions) (int, bool) {
	if opts.TextureStore == nil && len(opts.MaterialColors) == 0 {
		return 0, false
	}
	if isCutoutTexture(face.TextureName) {
		if sample, sampled, opaque := sampleFaceTextureCutoutOpaqueSample(face, key, opts); sampled && opaque {
			return bakedPaletteIndex(sample.Color), true
		}
	}
	if opts.TextureStore != nil {
		if color, ok := sampleFaceTextureColor(face, key, opts); ok {
			return bakedPaletteIndex(color), true
		}
	}
	if color, ok := opts.MaterialColors[face.TextureID+1]; ok && color != ([4]uint8{}) {
		return bakedPaletteIndex(color), true
	}
	return 0, false
}

func sampleFaceTextureColor(face Face, key [3]int, opts VoxelizeOptions) ([4]uint8, bool) {
	sample, ok := sampleFaceTextureTexel(face, key, opts)
	return sample.Color, ok
}

func faceCutoutTexelIsEmpty(face Face, key [3]int, opts VoxelizeOptions) bool {
	if !isCutoutTexture(face.TextureName) {
		return false
	}
	_, sampled, opaque := sampleFaceTextureCutoutOpaqueSample(face, key, opts)
	return sampled && !opaque
}

func sampleFaceTextureTexel(face Face, key [3]int, opts VoxelizeOptions) (TextureSample, bool) {
	if opts.TextureStore == nil {
		return TextureSample{}, false
	}
	u, v, ok := faceTextureUV(face, key, opts)
	if !ok {
		return TextureSample{}, false
	}
	return opts.TextureStore.SampleTexel(face.TextureName, u, v)
}

func sampleFaceTextureCutoutOpaqueSample(face Face, key [3]int, opts VoxelizeOptions) (TextureSample, bool, bool) {
	if opts.TextureStore == nil {
		return TextureSample{}, false, false
	}
	u, v, ok := faceTextureUV(face, key, opts)
	if !ok {
		return TextureSample{}, false, false
	}
	sampleHammerRadius := opts.VoxelResolution / HammerUnitMeters * 0.5
	du := maxFloat32(1, vec3Length(face.TexInfo.S.Axis)*sampleHammerRadius)
	dv := maxFloat32(1, vec3Length(face.TexInfo.T.Axis)*sampleHammerRadius)
	offsets := []float32{0, -0.5, 0.5, -1, 1}
	sampled := false
	for _, sv := range offsets {
		for _, su := range offsets {
			sample, ok := opts.TextureStore.SampleTexel(face.TextureName, u+su*du, v+sv*dv)
			if !ok {
				continue
			}
			sampled = true
			if sample.PaletteIndex != 255 {
				return sample, true, true
			}
		}
	}
	return TextureSample{}, sampled, false
}

func faceTextureUV(face Face, key [3]int, opts VoxelizeOptions) (float32, float32, bool) {
	point := GekkoToHammer(voxelCenter(key, opts.VoxelResolution))
	projected := projectPointToFacePlane(point, face)
	u := dotVec3(projected, face.TexInfo.S.Axis) + face.TexInfo.S.Shift
	v := dotVec3(projected, face.TexInfo.T.Axis) + face.TexInfo.T.Shift
	return u, v, true
}

func projectPointToFacePlane(point importcommon.Vec3, face Face) importcommon.Vec3 {
	if len(face.Vertices) == 0 {
		return point
	}
	normal := face.Normal
	len2 := dotVec3(normal, normal)
	if len2 < 1e-8 {
		return point
	}
	dist := dotVec3(face.Vertices[0], normal)
	offset := (dotVec3(point, normal) - dist) / len2
	return importcommon.Vec3{
		X: point.X - normal.X*offset,
		Y: point.Y - normal.Y*offset,
		Z: point.Z - normal.Z*offset,
	}
}

func rasterizeFaceSurfaceKeys(face Face, opts VoxelizeOptions) [][3]int {
	if len(face.Vertices) < 3 {
		return nil
	}
	world := make([]importcommon.Vec3, 0, len(face.Vertices))
	for _, vertex := range face.Vertices {
		world = append(world, HammerToGekko(vertex))
	}
	keys := make(map[[3]int]struct{})
	half := importcommon.Vec3{
		X: opts.VoxelResolution * 0.5,
		Y: opts.VoxelResolution * 0.5,
		Z: opts.VoxelResolution * 0.5,
	}
	for i := 1; i < len(world)-1; i++ {
		tri := [3]importcommon.Vec3{world[0], world[i], world[i+1]}
		minB, maxB := triangleVoxelBounds(tri, opts.VoxelResolution)
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					key := [3]int{x, y, z}
					if triangleIntersectsVoxel(tri, key, half, opts.VoxelResolution) {
						keys[key] = struct{}{}
					}
				}
			}
		}
	}
	out := make([][3]int, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		if out[i][1] != out[j][1] {
			return out[i][1] < out[j][1]
		}
		return out[i][2] < out[j][2]
	})
	return out
}

func triangleVoxelBounds(tri [3]importcommon.Vec3, resolution float32) ([3]int, [3]int) {
	minV := minVec3(minVec3(tri[0], tri[1]), tri[2])
	maxV := maxVec3(maxVec3(tri[0], tri[1]), tri[2])
	return [3]int{
			int(math.Floor(float64(minV.X / resolution))),
			int(math.Floor(float64(minV.Y / resolution))),
			int(math.Floor(float64(minV.Z / resolution))),
		}, [3]int{
			int(math.Floor(float64(maxV.X / resolution))),
			int(math.Floor(float64(maxV.Y / resolution))),
			int(math.Floor(float64(maxV.Z / resolution))),
		}
}

func triangleIntersectsVoxel(tri [3]importcommon.Vec3, key [3]int, half importcommon.Vec3, resolution float32) bool {
	center := voxelCenter(key, resolution)
	return triangleIntersectsAABB(tri, center, half)
}

func triangleIntersectsAABB(tri [3]importcommon.Vec3, center importcommon.Vec3, half importcommon.Vec3) bool {
	v0 := subVec3(tri[0], center)
	v1 := subVec3(tri[1], center)
	v2 := subVec3(tri[2], center)
	if !overlapsAxis(importcommon.Vec3{X: 1}, half, v0, v1, v2) ||
		!overlapsAxis(importcommon.Vec3{Y: 1}, half, v0, v1, v2) ||
		!overlapsAxis(importcommon.Vec3{Z: 1}, half, v0, v1, v2) {
		return false
	}
	e0 := subVec3(v1, v0)
	e1 := subVec3(v2, v1)
	e2 := subVec3(v0, v2)
	normal := crossVec3(e0, e1)
	if !overlapsAxis(normal, half, v0, v1, v2) {
		return false
	}
	boxAxes := [3]importcommon.Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	for _, edge := range [3]importcommon.Vec3{e0, e1, e2} {
		for _, boxAxis := range boxAxes {
			if !overlapsAxis(crossVec3(edge, boxAxis), half, v0, v1, v2) {
				return false
			}
		}
	}
	return true
}

func overlapsAxis(axis importcommon.Vec3, half importcommon.Vec3, v0, v1, v2 importcommon.Vec3) bool {
	axisLen2 := dotVec3(axis, axis)
	if axisLen2 < 1e-10 {
		return true
	}
	p0 := dotVec3(v0, axis)
	p1 := dotVec3(v1, axis)
	p2 := dotVec3(v2, axis)
	minP := minFloat32(p0, minFloat32(p1, p2))
	maxP := maxFloat32(p0, maxFloat32(p1, p2))
	r := half.X*float32(math.Abs(float64(axis.X))) +
		half.Y*float32(math.Abs(float64(axis.Y))) +
		half.Z*float32(math.Abs(float64(axis.Z)))
	return minP <= r && maxP >= -r
}

func floodPlayableEmptyCells(bsp *BSP, entities []importcommon.Entity, surface map[[3]int]importcommon.Voxel, minB, maxB [3]int, opts VoxelizeOptions) (map[[3]int]struct{}, error) {
	seeds := playableSeedKeys(entities, opts.VoxelResolution)
	seen := make(map[[3]int]struct{})
	if len(seeds) == 0 {
		return seen, nil
	}
	queue := make([][3]int, 0, len(seeds))
	for _, seed := range seeds {
		emptySeed, ok := nearestBSPEmptySeed(bsp, seed, surface, minB, maxB, opts.VoxelResolution)
		if !ok {
			continue
		}
		if _, exists := seen[emptySeed]; exists {
			continue
		}
		seen[emptySeed] = struct{}{}
		queue = append(queue, emptySeed)
	}
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr[0] + dir[0], curr[1] + dir[1], curr[2] + dir[2]}
			if !insideBounds(next, minB, maxB) {
				continue
			}
			if _, exists := seen[next]; exists {
				continue
			}
			if _, isSurface := surface[next]; isSurface {
				continue
			}
			contents, err := bsp.PointContentsGekko(0, voxelCenter(next, opts.VoxelResolution))
			if err != nil {
				return nil, err
			}
			if !IsPlayableEmptyContent(contents) {
				continue
			}
			seen[next] = struct{}{}
			if int64(len(seen)) > opts.MaxSolidSampleCells {
				return nil, fmt.Errorf("playable empty flood too large: %d cells exceeds cap %d", len(seen), opts.MaxSolidSampleCells)
			}
			queue = append(queue, next)
		}
	}
	return seen, nil
}

type solidBandNode struct {
	Key   [3]int
	Depth int
}

func solidBandCandidatesFromPlayableEmpty(bsp *BSP, playableEmpty map[[3]int]struct{}, minB, maxB [3]int, opts VoxelizeOptions) (map[[3]int]struct{}, error) {
	candidates := make(map[[3]int]struct{})
	if len(playableEmpty) == 0 {
		return candidates, nil
	}
	queue := make([]solidBandNode, 0)
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	addIfSolid := func(key [3]int, depth int) error {
		if !insideBounds(key, minB, maxB) {
			return nil
		}
		if _, isEmpty := playableEmpty[key]; isEmpty {
			return nil
		}
		if _, exists := candidates[key]; exists {
			return nil
		}
		contents, err := bsp.PointContentsGekko(0, voxelCenter(key, opts.VoxelResolution))
		if err != nil {
			return err
		}
		if !IsSolidContent(contents) {
			return nil
		}
		candidates[key] = struct{}{}
		if int64(len(candidates)) > opts.MaxSolidSampleCells {
			return fmt.Errorf("solid band too large: %d cells exceeds cap %d", len(candidates), opts.MaxSolidSampleCells)
		}
		if depth < opts.SolidBandDepth {
			queue = append(queue, solidBandNode{Key: key, Depth: depth})
		}
		return nil
	}
	for key := range playableEmpty {
		for _, dir := range dirs {
			next := [3]int{key[0] + dir[0], key[1] + dir[1], key[2] + dir[2]}
			if err := addIfSolid(next, 1); err != nil {
				return nil, err
			}
		}
	}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr.Key[0] + dir[0], curr.Key[1] + dir[1], curr.Key[2] + dir[2]}
			if err := addIfSolid(next, curr.Depth+1); err != nil {
				return nil, err
			}
		}
	}
	return candidates, nil
}

func solidBandCandidatesFromSurfaceFaces(bsp *BSP, faces []Face, minB, maxB [3]int, opts VoxelizeOptions) (map[[3]int]struct{}, error) {
	candidates := make(map[[3]int]struct{})
	queue := make([]solidBandNode, 0)
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	addIfSolid := func(key [3]int, depth int) error {
		if !insideBounds(key, minB, maxB) {
			return nil
		}
		if _, exists := candidates[key]; exists {
			return nil
		}
		contents, err := bsp.PointContentsGekko(0, voxelCenter(key, opts.VoxelResolution))
		if err != nil {
			return err
		}
		if !IsSolidContent(contents) {
			return nil
		}
		candidates[key] = struct{}{}
		if int64(len(candidates)) > opts.MaxSolidSampleCells {
			return fmt.Errorf("solid surface band too large: %d cells exceeds cap %d", len(candidates), opts.MaxSolidSampleCells)
		}
		if depth < opts.SolidBandDepth {
			queue = append(queue, solidBandNode{Key: key, Depth: depth})
		}
		return nil
	}
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		for _, key := range rasterizeFaceSurfaceKeys(face, opts) {
			if err := addIfSolid(key, 0); err != nil {
				return nil, err
			}
			for _, dir := range dirs {
				next := [3]int{key[0] + dir[0], key[1] + dir[1], key[2] + dir[2]}
				if err := addIfSolid(next, 1); err != nil {
					return nil, err
				}
			}
		}
	}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr.Key[0] + dir[0], curr.Key[1] + dir[1], curr.Key[2] + dir[2]}
			if err := addIfSolid(next, curr.Depth+1); err != nil {
				return nil, err
			}
		}
	}
	return candidates, nil
}

func nearestBSPEmptySeed(bsp *BSP, seed [3]int, surface map[[3]int]importcommon.Voxel, minB, maxB [3]int, resolution float32) ([3]int, bool) {
	for radius := 0; radius <= 3; radius++ {
		for x := seed[0] - radius; x <= seed[0]+radius; x++ {
			for y := seed[1] - radius; y <= seed[1]+radius; y++ {
				for z := seed[2] - radius; z <= seed[2]+radius; z++ {
					key := [3]int{x, y, z}
					if !insideBounds(key, minB, maxB) {
						continue
					}
					if _, isSurface := surface[key]; isSurface {
						continue
					}
					contents, err := bsp.PointContentsGekko(0, voxelCenter(key, resolution))
					if err == nil && IsPlayableEmptyContent(contents) {
						return key, true
					}
				}
			}
		}
	}
	return [3]int{}, false
}

func floodPlayableEmpty(bsp *BSP, entities []importcommon.Entity, solid map[[3]int]importcommon.Voxel, minB, maxB [3]int, resolution float32) int {
	seeds := playableSeedKeys(entities, resolution)
	if len(seeds) == 0 {
		return 0
	}
	seen := make(map[[3]int]struct{})
	queue := make([][3]int, 0, len(seeds))
	for _, seed := range seeds {
		emptySeed, ok := nearestEmptySeed(seed, solid, minB, maxB)
		if !ok {
			continue
		}
		if _, exists := seen[emptySeed]; exists {
			continue
		}
		seen[emptySeed] = struct{}{}
		queue = append(queue, emptySeed)
	}
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr[0] + dir[0], curr[1] + dir[1], curr[2] + dir[2]}
			if !insideBounds(next, minB, maxB) {
				continue
			}
			if _, exists := seen[next]; exists {
				continue
			}
			if _, isSolid := solid[next]; isSolid {
				continue
			}
			contents, err := bsp.PointContentsGekko(0, voxelCenter(next, resolution))
			if err != nil || IsSolidContent(contents) {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return len(seen)
}

func voxelizedFaceBoundsGekko(faces []Face) (importcommon.Bounds, bool) {
	var out importcommon.Bounds
	found := false
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		for _, vertex := range face.Vertices {
			converted := HammerToGekko(vertex)
			if !found {
				out.Min = converted
				out.Max = converted
				found = true
				continue
			}
			out.Min = minVec3(out.Min, converted)
			out.Max = maxVec3(out.Max, converted)
		}
	}
	return out, found
}

func playableSeedKeys(entities []importcommon.Entity, resolution float32) [][3]int {
	var out [][3]int
	for _, entity := range entities {
		if !isPlayerStartClass(entity.ClassName) {
			continue
		}
		out = append(out, keyForPosition(entity.WorldPosition, resolution))
	}
	return out
}

func isPlayerStartClass(className string) bool {
	switch strings.ToLower(className) {
	case "info_player_start", "info_player_deathmatch", "info_player_coop":
		return true
	default:
		return false
	}
}

func nearestEmptySeed(seed [3]int, solid map[[3]int]importcommon.Voxel, minB, maxB [3]int) ([3]int, bool) {
	if insideBounds(seed, minB, maxB) {
		if _, isSolid := solid[seed]; !isSolid {
			return seed, true
		}
	}
	for radius := 1; radius <= 3; radius++ {
		for x := seed[0] - radius; x <= seed[0]+radius; x++ {
			for y := seed[1] - radius; y <= seed[1]+radius; y++ {
				for z := seed[2] - radius; z <= seed[2]+radius; z++ {
					key := [3]int{x, y, z}
					if !insideBounds(key, minB, maxB) {
						continue
					}
					if _, isSolid := solid[key]; !isSolid {
						return key, true
					}
				}
			}
		}
	}
	return [3]int{}, false
}

func insideBounds(key, minB, maxB [3]int) bool {
	return key[0] >= minB[0] && key[1] >= minB[1] && key[2] >= minB[2] &&
		key[0] <= maxB[0] && key[1] <= maxB[1] && key[2] <= maxB[2]
}

func boundedCellCount(minB, maxB [3]int) (int64, error) {
	var total int64 = 1
	for axis := 0; axis < 3; axis++ {
		if maxB[axis] < minB[axis] {
			return 0, fmt.Errorf("empty voxel sample bounds: %v..%v", minB, maxB)
		}
		total *= int64(maxB[axis] - minB[axis] + 1)
	}
	return total, nil
}

func keyForPosition(position importcommon.Vec3, resolution float32) [3]int {
	return [3]int{
		int(math.Floor(float64(position.X / resolution))),
		int(math.Floor(float64(position.Y / resolution))),
		int(math.Floor(float64(position.Z / resolution))),
	}
}

func voxelCenter(key [3]int, resolution float32) importcommon.Vec3 {
	return importcommon.Vec3{
		X: (float32(key[0]) + 0.5) * resolution,
		Y: (float32(key[1]) + 0.5) * resolution,
		Z: (float32(key[2]) + 0.5) * resolution,
	}
}

func fillClosedInterior(voxels map[[3]int]importcommon.Voxel) {
	if len(voxels) == 0 {
		return
	}
	minB, maxB := voxelMapBounds(voxels)
	minB = [3]int{minB[0] - 1, minB[1] - 1, minB[2] - 1}
	maxB = [3]int{maxB[0] + 1, maxB[1] + 1, maxB[2] + 1}
	start := minB
	seen := map[[3]int]struct{}{start: {}}
	queue := [][3]int{start}
	dirs := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, dir := range dirs {
			next := [3]int{curr[0] + dir[0], curr[1] + dir[1], curr[2] + dir[2]}
			if next[0] < minB[0] || next[1] < minB[1] || next[2] < minB[2] ||
				next[0] > maxB[0] || next[1] > maxB[1] || next[2] > maxB[2] {
				continue
			}
			if _, ok := seen[next]; ok {
				continue
			}
			if _, solid := voxels[next]; solid {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	for x := minB[0] + 1; x <= maxB[0]-1; x++ {
		for y := minB[1] + 1; y <= maxB[1]-1; y++ {
			for z := minB[2] + 1; z <= maxB[2]-1; z++ {
				key := [3]int{x, y, z}
				if _, solid := voxels[key]; solid {
					continue
				}
				if _, exterior := seen[key]; exterior {
					continue
				}
				voxels[key] = importcommon.Voxel{X: x, Y: y, Z: z, Palette: 1, MaterialID: 1, SolidKind: "interior_fill"}
			}
		}
	}
}

func subVec3(a, b importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z}
}

func dotVec3(a, b importcommon.Vec3) float32 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

func vec3Length(v importcommon.Vec3) float32 {
	return float32(math.Sqrt(float64(dotVec3(v, v))))
}

func crossVec3(a, b importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func voxelsToSortedSlice(voxels map[[3]int]importcommon.Voxel) []importcommon.Voxel {
	out := make([]importcommon.Voxel, 0, len(voxels))
	for _, voxel := range voxels {
		out = append(out, voxel)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].Z < out[j].Z
	})
	return out
}

func voxelMapBounds(voxels map[[3]int]importcommon.Voxel) ([3]int, [3]int) {
	first := true
	var minB, maxB [3]int
	for key := range voxels {
		if first {
			minB, maxB = key, key
			first = false
			continue
		}
		minB[0] = min(minB[0], key[0])
		minB[1] = min(minB[1], key[1])
		minB[2] = min(minB[2], key[2])
		maxB[0] = max(maxB[0], key[0])
		maxB[1] = max(maxB[1], key[1])
		maxB[2] = max(maxB[2], key[2])
	}
	return minB, maxB
}
