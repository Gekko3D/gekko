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
	LightingData             []byte
	BakeStaticLightmaps      bool
	MaterialColors           map[int][4]uint8
	ForceScrollAnimation     bool
	ScrollDirectionHammer    importcommon.Vec3
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
	sampledColors := make(map[[3]int][4]uint8)
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		voxelizeFaceSurface(face, opts, voxels, sampledColors)
	}
	surfaceCount := len(voxels)
	materials := applyAdaptiveVoxelPalette(voxels, sampledColors, opts)
	if opts.FillClosed {
		fillClosedInterior(voxels)
	}
	out := voxelsToSortedSlice(voxels)
	result := VoxelizeResult{
		Voxels:       out,
		Materials:    materials,
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
	sampledColors := make(map[[3]int][4]uint8)
	for _, face := range faces {
		if !shouldVoxelizeFaceKind(materialKind(face.TextureName)) {
			continue
		}
		voxelizeFaceSurface(face, opts, voxels, sampledColors)
	}
	surfaceCount := len(voxels)
	materials := applyAdaptiveVoxelPalette(voxels, sampledColors, opts)
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
		Materials:             materials,
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

func sortedColorSampleKeys(colors map[[3]int][4]uint8) [][3]int {
	keys := make([][3]int, 0, len(colors))
	for key := range colors {
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

func voxelizeFaceSurface(face Face, opts VoxelizeOptions, voxels map[[3]int]importcommon.Voxel, sampledColors map[[3]int][4]uint8) {
	for _, key := range rasterizeFaceSurfaceKeys(face, opts) {
		if _, exists := voxels[key]; exists {
			continue
		}
		if faceCutoutTexelIsEmpty(face, key, opts) {
			continue
		}
		materialID := face.TextureID + 1
		palette := uint8(min(max(materialID, 1), 255))
		if sample, ok := bakedFaceSample(face, key, opts); ok {
			if sample.Emissive {
				materialID = int(sample.Palette)
				palette = sample.Palette
			} else {
				materialID = 1
				palette = 1
				sampledColors[key] = sample.Color
			}
		}
		sourceTexture, animationID, animationPhase := hl1VoxelMaterialAnimation(face, key, opts)
		voxels[key] = importcommon.Voxel{
			X:                 key[0],
			Y:                 key[1],
			Z:                 key[2],
			Palette:           palette,
			MaterialID:        materialID,
			SolidKind:         materialKind(face.TextureName),
			SourceTextureName: sourceTexture,
			AnimationID:       animationID,
			AnimationPhase:    animationPhase,
		}
	}
}

func hl1VoxelMaterialAnimation(face Face, key [3]int, opts VoxelizeOptions) (string, string, int) {
	if animationID := hl1TextureAnimationID(face.TextureName); animationID != "" {
		return face.TextureName, animationID, 0
	}
	if !opts.ForceScrollAnimation && !hl1TextureLooksScrollable(face.TextureName) {
		return "", "", 0
	}
	u, v, ok := faceTextureUV(face, key, opts)
	if !ok {
		return face.TextureName, hl1TextureScrollAnimationID(face.TextureName, hl1ScrollAxisU), 0
	}
	axis := hl1ScrollAxisU
	size := 0
	if opts.TextureStore != nil {
		if texture, ok := opts.TextureStore.Texture(face.TextureName); ok {
			axis = hl1FaceScrollAxis(face, texture, opts.ScrollDirectionHammer)
			size = hl1ScrollTextureSize(texture, axis)
		}
	}
	coord := u
	if axis == hl1ScrollAxisV {
		coord = v
	}
	return face.TextureName, hl1TextureScrollAnimationID(face.TextureName, axis), hl1TextureScrollPhase(coord, size)
}

func hl1FaceScrollAxis(face Face, texture TexturePixels, direction importcommon.Vec3) hl1ScrollAxis {
	if !texture.Valid() || len(face.Vertices) == 0 {
		return hl1ScrollAxisU
	}
	if axis, ok := hl1TextureScrollContentAxis(texture); ok {
		return axis
	}
	if vec3Length(direction) > 1e-4 {
		sScore := math.Abs(float64(dotVec3(normalizeVec3(face.TexInfo.S.Axis), normalizeVec3(direction))))
		tScore := math.Abs(float64(dotVec3(normalizeVec3(face.TexInfo.T.Axis), normalizeVec3(direction))))
		if tScore > sScore*1.05 {
			return hl1ScrollAxisV
		}
		if sScore > tScore*1.05 {
			return hl1ScrollAxisU
		}
	}
	minU, maxU := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	minV, maxV := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for _, vertex := range face.Vertices {
		u := dotVec3(vertex, face.TexInfo.S.Axis) + face.TexInfo.S.Shift
		v := dotVec3(vertex, face.TexInfo.T.Axis) + face.TexInfo.T.Shift
		minU = minFloat32(minU, u)
		maxU = maxFloat32(maxU, u)
		minV = minFloat32(minV, v)
		maxV = maxFloat32(maxV, v)
	}
	uSpan := (maxU - minU) / float32(max(1, texture.Width))
	vSpan := (maxV - minV) / float32(max(1, texture.Height))
	if vSpan > uSpan*1.1 {
		return hl1ScrollAxisV
	}
	return hl1ScrollAxisU
}

func hl1TextureScrollContentAxis(texture TexturePixels) (hl1ScrollAxis, bool) {
	if !texture.Valid() || texture.Width <= 1 || texture.Height <= 1 {
		return "", false
	}
	xVariance := hl1TextureMeanLumaVarianceByColumn(texture)
	yVariance := hl1TextureMeanLumaVarianceByRow(texture)
	const minVariance = 4.0
	const dominance = 1.2
	switch {
	case xVariance > minVariance && xVariance > yVariance*dominance:
		return hl1ScrollAxisU, true
	case yVariance > minVariance && yVariance > xVariance*dominance:
		return hl1ScrollAxisV, true
	default:
		return "", false
	}
}

func hl1TextureMeanLumaVarianceByColumn(texture TexturePixels) float64 {
	values := make([]float64, 0, texture.Width)
	for x := 0; x < texture.Width; x++ {
		sum := 0.0
		count := 0.0
		for y := 0; y < texture.Height; y++ {
			color, ok := texture.ColorAt(x, y)
			if !ok {
				continue
			}
			sum += float64(hl1Luma(color))
			count++
		}
		if count > 0 {
			values = append(values, sum/count)
		}
	}
	return hl1Variance(values)
}

func hl1TextureMeanLumaVarianceByRow(texture TexturePixels) float64 {
	values := make([]float64, 0, texture.Height)
	for y := 0; y < texture.Height; y++ {
		sum := 0.0
		count := 0.0
		for x := 0; x < texture.Width; x++ {
			color, ok := texture.ColorAt(x, y)
			if !ok {
				continue
			}
			sum += float64(hl1Luma(color))
			count++
		}
		if count > 0 {
			values = append(values, sum/count)
		}
	}
	return hl1Variance(values)
}

func hl1Luma(color [4]uint8) int {
	return (int(color[0])*299 + int(color[1])*587 + int(color[2])*114) / 1000
}

func hl1Variance(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	return variance / float64(len(values))
}

func applyAdaptiveVoxelPalette(voxels map[[3]int]importcommon.Voxel, sampledColors map[[3]int][4]uint8, opts VoxelizeOptions) []importcommon.Material {
	if opts.TextureStore == nil && len(opts.MaterialColors) == 0 {
		return nil
	}
	colors := make([][4]uint8, 0, len(sampledColors))
	for _, key := range sortedColorSampleKeys(sampledColors) {
		colors = append(colors, sampledColors[key])
	}
	materials, indexByColor := AdaptiveBakedPaletteMaterials(colors)
	for _, key := range sortedColorSampleKeys(sampledColors) {
		voxel, ok := voxels[key]
		if !ok {
			continue
		}
		color := sampledColors[key]
		color[3] = 255
		index := indexByColor[color]
		if index == 0 {
			continue
		}
		voxel.Palette = index
		voxel.MaterialID = int(index)
		voxels[key] = voxel
	}
	return materials
}

type bakedFaceSampleResult struct {
	Color    [4]uint8
	Palette  uint8
	Emissive bool
}

func bakedFaceSample(face Face, key [3]int, opts VoxelizeOptions) (bakedFaceSampleResult, bool) {
	if opts.TextureStore == nil && len(opts.MaterialColors) == 0 {
		return bakedFaceSampleResult{}, false
	}
	if isCutoutTexture(face.TextureName) {
		if sample, sampled, opaque := sampleFaceTextureCutoutOpaqueSample(face, key, opts); sampled && opaque {
			color := lightmapModulatedFaceColor(face, key, opts, sample.Color)
			if index, emissive := emissivePaletteIndexForTexel(face.TextureName, color); emissive {
				return bakedFaceSampleResult{Palette: index, Emissive: true}, true
			}
			return bakedFaceSampleResult{Color: color}, true
		}
	}
	if opts.TextureStore != nil {
		if sample, ok := sampleFaceTextureTexel(face, key, opts); ok {
			if index, emissive := emissivePaletteIndexForTexel(face.TextureName, sample.Color); emissive {
				return bakedFaceSampleResult{Palette: index, Emissive: true}, true
			}
			color := lightmapModulatedFaceColor(face, key, opts, sample.Color)
			return bakedFaceSampleResult{Color: color}, true
		}
	}
	if color, ok := opts.MaterialColors[face.TextureID+1]; ok && color != ([4]uint8{}) {
		color = lightmapModulatedFaceColor(face, key, opts, color)
		return bakedFaceSampleResult{Color: color}, true
	}
	return bakedFaceSampleResult{}, false
}

func lightmapModulatedFaceColor(face Face, key [3]int, opts VoxelizeOptions, color [4]uint8) [4]uint8 {
	if !opts.BakeStaticLightmaps {
		return color
	}
	sample, ok := sampleFaceLightmap(face, key, opts)
	if !ok {
		return color
	}
	return [4]uint8{
		lightmapModulatedChannel(color[0], sample[0]),
		lightmapModulatedChannel(color[1], sample[1]),
		lightmapModulatedChannel(color[2], sample[2]),
		color[3],
	}
}

func lightmapModulatedChannel(albedo, light uint8) uint8 {
	value := int(albedo) * int(light) / 128
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func sampleFaceLightmap(face Face, key [3]int, opts VoxelizeOptions) ([3]uint8, bool) {
	if face.LightOfs < 0 || len(opts.LightingData) == 0 || face.Styles[0] == 255 {
		return [3]uint8{}, false
	}
	mins, size, ok := faceLightmapExtents(face)
	if !ok || size[0] <= 0 || size[1] <= 0 {
		return [3]uint8{}, false
	}
	u, v, ok := faceTextureUV(face, key, opts)
	if !ok {
		return [3]uint8{}, false
	}
	x := clampInt(int(math.Round(float64(u/16.0-float32(mins[0])))), 0, size[0]-1)
	y := clampInt(int(math.Round(float64(v/16.0-float32(mins[1])))), 0, size[1]-1)
	offset := int(face.LightOfs) + (y*size[0]+x)*3
	if offset < 0 || offset+2 >= len(opts.LightingData) {
		return [3]uint8{}, false
	}
	return [3]uint8{opts.LightingData[offset], opts.LightingData[offset+1], opts.LightingData[offset+2]}, true
}

func faceLightmapExtents(face Face) ([2]int, [2]int, bool) {
	if len(face.Vertices) == 0 {
		return [2]int{}, [2]int{}, false
	}
	mins := [2]float32{float32(math.MaxFloat32), float32(math.MaxFloat32)}
	maxs := [2]float32{-float32(math.MaxFloat32), -float32(math.MaxFloat32)}
	for _, vertex := range face.Vertices {
		s := dotVec3(vertex, face.TexInfo.S.Axis) + face.TexInfo.S.Shift
		t := dotVec3(vertex, face.TexInfo.T.Axis) + face.TexInfo.T.Shift
		mins[0] = minFloat32(mins[0], s)
		mins[1] = minFloat32(mins[1], t)
		maxs[0] = maxFloat32(maxs[0], s)
		maxs[1] = maxFloat32(maxs[1], t)
	}
	lightMin := [2]int{
		int(math.Floor(float64(mins[0] / 16.0))),
		int(math.Floor(float64(mins[1] / 16.0))),
	}
	lightMax := [2]int{
		int(math.Ceil(float64(maxs[0] / 16.0))),
		int(math.Ceil(float64(maxs[1] / 16.0))),
	}
	return lightMin, [2]int{lightMax[0] - lightMin[0] + 1, lightMax[1] - lightMin[1] + 1}, true
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

func emissivePaletteIndexForTexel(textureName string, color [4]uint8) (uint8, bool) {
	if !isCandidateEmissiveTextureName(textureName) || color[3] == 0 {
		return 0, false
	}
	r := int(color[0])
	g := int(color[1])
	b := int(color[2])
	luma := (r*299 + g*587 + b*114) / 1000
	maxChannel := max(r, max(g, b))
	if luma < 105 || maxChannel < 140 {
		return 0, false
	}
	level := min(emissiveRampLevels-1, max(0, (luma-105)*(emissiveRampLevels-1)/(255-105)))
	if b > r+24 && b > g-8 {
		return emissivePaletteIndexForToneLevel(emissiveCoolTone, level), true
	}
	if r > b+18 || g > b+18 {
		return emissivePaletteIndexForToneLevel(emissiveWarmTone, level), true
	}
	return emissivePaletteIndexForToneLevel(emissiveNeutralTone, level), true
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

func normalizeVec3(v importcommon.Vec3) importcommon.Vec3 {
	length := vec3Length(v)
	if length <= 1e-6 {
		return importcommon.Vec3{}
	}
	return importcommon.Vec3{X: v.X / length, Y: v.Y / length, Z: v.Z / length}
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

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
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
