package hl1

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	defaultHL1WaterDepth = float32(1.2)
	waterTopNormalYMin   = float32(0.7)
	waterMergeEpsilon    = float32(0.02)
	waterRectFillRatio   = float32(0.86)
)

type hl1WaterRect struct {
	Kind     string
	SurfaceY float32
	Depth    float32
	MinX     float32
	MaxX     float32
	MinZ     float32
	MaxZ     float32
}

type hl1LiquidFaceBounds struct {
	Kind string
	Min  importcommon.Vec3
	Max  importcommon.Vec3
}

type hl1WaterBox struct {
	Kind string
	Min  importcommon.Vec3
	Max  importcommon.Vec3
}

func buildHL1WaterBodies(bsp *BSP, faces []Face, voxelResolution float32) []content.LevelWaterBodyDef {
	if voxelResolution <= 0 {
		voxelResolution = 0.1
	}
	if rects := buildHL1LiquidRectsFromLeafs(bsp, voxelResolution); len(rects) > 0 {
		return buildHL1WaterBodyDefs(mergeHL1WaterRects(rects))
	}
	liquidBounds := make([]hl1LiquidFaceBounds, 0)
	for _, face := range faces {
		kind := materialKind(face.TextureName)
		if !isLiquidMaterialKind(kind) {
			continue
		}
		bounds, ok := faceBoundsGekko(face)
		if !ok {
			continue
		}
		liquidBounds = append(liquidBounds, hl1LiquidFaceBounds{Kind: kind, Min: bounds.Min, Max: bounds.Max})
	}
	rects := make([]hl1WaterRect, 0)
	for _, face := range faces {
		kind := materialKind(face.TextureName)
		if !isLiquidMaterialKind(kind) {
			continue
		}
		normal := hammerVectorToGekko(face.Normal)
		if normal.Y < waterTopNormalYMin {
			continue
		}
		bounds, ok := faceBoundsGekko(face)
		if !ok {
			continue
		}
		if bounds.Max.X-bounds.Min.X < voxelResolution || bounds.Max.Z-bounds.Min.Z < voxelResolution {
			continue
		}
		surfaceY := averageFaceY(face)
		depth := liquidDepthForTopFace(kind, surfaceY, bounds, liquidBounds, voxelResolution)
		rects = append(rects, hl1WaterRect{
			Kind:     kind,
			SurfaceY: surfaceY,
			Depth:    depth,
			MinX:     bounds.Min.X,
			MaxX:     bounds.Max.X,
			MinZ:     bounds.Min.Z,
			MaxZ:     bounds.Max.Z,
		})
	}
	return buildHL1WaterBodyDefs(mergeHL1WaterRects(rects))
}

func buildHL1LiquidRectsFromLeafs(bsp *BSP, voxelResolution float32) []hl1WaterRect {
	if bsp == nil {
		return nil
	}
	boxes := make([]hl1WaterBox, 0)
	minExtent := maxFloat32(voxelResolution, 0.001)
	for _, leaf := range bsp.Leafs {
		kind := liquidContentKind(leaf.Contents)
		if kind == "" || !leafHasUsableBounds(leaf) {
			continue
		}
		bounds := HammerBoundsToGekko(
			importcommon.Vec3{X: float32(leaf.Min[0]), Y: float32(leaf.Min[1]), Z: float32(leaf.Min[2])},
			importcommon.Vec3{X: float32(leaf.Max[0]), Y: float32(leaf.Max[1]), Z: float32(leaf.Max[2])},
		)
		width := bounds.Max.X - bounds.Min.X
		length := bounds.Max.Z - bounds.Min.Z
		depth := bounds.Max.Y - bounds.Min.Y
		if width < minExtent || length < minExtent || depth < minExtent {
			continue
		}
		boxes = append(boxes, hl1WaterBox{Kind: kind, Min: bounds.Min, Max: bounds.Max})
	}
	components := connectedHL1WaterBoxComponents(boxes, maxFloat32(voxelResolution, waterMergeEpsilon))
	rects := make([]hl1WaterRect, 0, len(components))
	for _, component := range components {
		if len(component) == 0 {
			continue
		}
		box := component[0]
		for _, other := range component[1:] {
			box.Min = minVec3(box.Min, other.Min)
			box.Max = maxVec3(box.Max, other.Max)
		}
		rects = append(rects, hl1WaterRect{
			Kind:     box.Kind,
			SurfaceY: box.Max.Y,
			Depth:    box.Max.Y - box.Min.Y,
			MinX:     box.Min.X,
			MaxX:     box.Max.X,
			MinZ:     box.Min.Z,
			MaxZ:     box.Max.Z,
		})
	}
	return rects
}

func connectedHL1WaterBoxComponents(boxes []hl1WaterBox, eps float32) [][]hl1WaterBox {
	visited := make([]bool, len(boxes))
	components := make([][]hl1WaterBox, 0)
	for i := range boxes {
		if visited[i] {
			continue
		}
		visited[i] = true
		queue := []int{i}
		component := make([]hl1WaterBox, 0, 1)
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			component = append(component, boxes[next])
			for j := range boxes {
				if visited[j] {
					continue
				}
				if boxes[next].Kind == boxes[j].Kind && waterBoxesOverlapOrTouch(boxes[next], boxes[j], eps) {
					visited[j] = true
					queue = append(queue, j)
				}
			}
		}
		components = append(components, component)
	}
	return components
}

func waterBoxesOverlapOrTouch(a, b hl1WaterBox, eps float32) bool {
	return rangesOverlapOrTouch(a.Min.X, a.Max.X, b.Min.X, b.Max.X, eps) &&
		rangesOverlapOrTouch(a.Min.Y, a.Max.Y, b.Min.Y, b.Max.Y, eps) &&
		rangesOverlapOrTouch(a.Min.Z, a.Max.Z, b.Min.Z, b.Max.Z, eps)
}

func buildHL1WaterBodyDefs(rects []hl1WaterRect) []content.LevelWaterBodyDef {
	bodies := make([]content.LevelWaterBodyDef, 0, len(rects))
	continuityGroups := hl1WaterContinuityGroups(rects)
	for i, rect := range rects {
		centerX := (rect.MinX + rect.MaxX) * 0.5
		centerZ := (rect.MinZ + rect.MaxZ) * 0.5
		directLightOcclusion := float32(1)
		body := content.LevelWaterBodyDef{
			ID:                   fmt.Sprintf("hl1_%s_%d", rect.Kind, i),
			Name:                 rect.Kind,
			Mode:                 content.LevelWaterBodyModeExplicitRect,
			SurfaceY:             rect.SurfaceY,
			Depth:                rect.Depth,
			RectHalfExtents:      content.Vec2{(rect.MaxX - rect.MinX) * 0.5, (rect.MaxZ - rect.MinZ) * 0.5},
			SourceTag:            "hl1:" + rect.Kind,
			ContinuityGroup:      continuityGroups[i],
			DebugName:            rect.Kind,
			Color:                liquidColor(rect.Kind),
			AbsorptionColor:      liquidAbsorptionColor(rect.Kind),
			Opacity:              liquidOpacity(rect.Kind),
			Roughness:            0.18,
			Refraction:           0.45,
			DirectLightOcclusion: &directLightOcclusion,
			FlowDirection:        content.Vec2{1, 0},
			FlowSpeed:            0.35,
			WaveAmplitude:        0.04,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{centerX, rect.SurfaceY, centerZ},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "liquid:" + rect.Kind},
		}
		bodies = append(bodies, body)
	}
	return bodies
}

func hl1WaterContinuityGroups(rects []hl1WaterRect) []string {
	groups := make([]string, len(rects))
	visited := make([]bool, len(rects))
	groupIndex := 0
	for i := range rects {
		if visited[i] {
			continue
		}
		visited[i] = true
		queue := []int{i}
		component := make([]int, 0, 1)
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			component = append(component, next)
			for j := range rects {
				if visited[j] {
					continue
				}
				if hl1WaterRectsContinuous(rects[next], rects[j]) {
					visited[j] = true
					queue = append(queue, j)
				}
			}
		}
		if len(component) <= 1 {
			continue
		}
		groupID := fmt.Sprintf("hl1:%s:water_continuity:%d", rects[i].Kind, groupIndex)
		groupIndex++
		for _, idx := range component {
			groups[idx] = groupID
		}
	}
	return groups
}

func hl1WaterRectsContinuous(a, b hl1WaterRect) bool {
	if a.Kind != b.Kind || absFloat32(a.SurfaceY-b.SurfaceY) > waterMergeEpsilon {
		return false
	}
	if !rectsOverlapOrTouch(a.MinX, a.MaxX, a.MinZ, a.MaxZ, b.MinX, b.MaxX, b.MinZ, b.MaxZ, waterMergeEpsilon) {
		return false
	}
	overlapX := minFloat32(a.MaxX, b.MaxX) - maxFloat32(a.MinX, b.MinX)
	overlapZ := minFloat32(a.MaxZ, b.MaxZ) - maxFloat32(a.MinZ, b.MinZ)
	return overlapX > waterMergeEpsilon || overlapZ > waterMergeEpsilon
}

func faceBoundsGekko(face Face) (importcommon.Bounds, bool) {
	if len(face.Vertices) == 0 {
		return importcommon.Bounds{}, false
	}
	first := HammerToGekko(face.Vertices[0])
	bounds := importcommon.Bounds{Min: first, Max: first}
	for _, vertex := range face.Vertices[1:] {
		converted := HammerToGekko(vertex)
		bounds.Min = minVec3(bounds.Min, converted)
		bounds.Max = maxVec3(bounds.Max, converted)
	}
	return bounds, true
}

func averageFaceY(face Face) float32 {
	if len(face.Vertices) == 0 {
		return 0
	}
	var sum float32
	for _, vertex := range face.Vertices {
		sum += HammerToGekko(vertex).Y
	}
	return sum / float32(len(face.Vertices))
}

func liquidDepthForTopFace(kind string, surfaceY float32, top importcommon.Bounds, all []hl1LiquidFaceBounds, voxelResolution float32) float32 {
	depth := float32(0)
	for _, candidate := range all {
		if candidate.Kind != kind {
			continue
		}
		if !rectsOverlapOrTouch(top.Min.X, top.Max.X, top.Min.Z, top.Max.Z, candidate.Min.X, candidate.Max.X, candidate.Min.Z, candidate.Max.Z, voxelResolution) {
			continue
		}
		if candidate.Min.Y < surfaceY {
			depth = maxFloat32(depth, surfaceY-candidate.Min.Y)
		}
	}
	if depth < voxelResolution {
		depth = maxFloat32(defaultHL1WaterDepth, voxelResolution*4)
	}
	return depth
}

func mergeHL1WaterRects(rects []hl1WaterRect) []hl1WaterRect {
	sort.Slice(rects, func(i, j int) bool {
		if rects[i].SurfaceY != rects[j].SurfaceY {
			return rects[i].SurfaceY < rects[j].SurfaceY
		}
		if rects[i].MinX != rects[j].MinX {
			return rects[i].MinX < rects[j].MinX
		}
		return rects[i].MinZ < rects[j].MinZ
	})
	merged := true
	for merged {
		merged = false
		for i := 0; i < len(rects) && !merged; i++ {
			for j := i + 1; j < len(rects); j++ {
				if combined, ok := mergeHL1WaterRectPair(rects[i], rects[j]); ok {
					rects[i] = combined
					rects = append(rects[:j], rects[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	for areaMerged := true; areaMerged; {
		areaMerged = false
		for i := 0; i < len(rects) && !areaMerged; i++ {
			for j := i + 1; j < len(rects); j++ {
				if combined, ok := mergeHL1WaterRectPairByFill(rects[i], rects[j]); ok {
					rects[i] = combined
					rects = append(rects[:j], rects[j+1:]...)
					areaMerged = true
					break
				}
			}
		}
	}
	return rects
}

func mergeHL1WaterRectPair(a, b hl1WaterRect) (hl1WaterRect, bool) {
	if a.Kind != b.Kind || absFloat32(a.SurfaceY-b.SurfaceY) > waterMergeEpsilon || absFloat32(a.Depth-b.Depth) > waterMergeEpsilon {
		return hl1WaterRect{}, false
	}
	sameZ := absFloat32(a.MinZ-b.MinZ) <= waterMergeEpsilon && absFloat32(a.MaxZ-b.MaxZ) <= waterMergeEpsilon
	if sameZ && rangesOverlapOrTouch(a.MinX, a.MaxX, b.MinX, b.MaxX, waterMergeEpsilon) {
		a.MinX = minFloat32(a.MinX, b.MinX)
		a.MaxX = maxFloat32(a.MaxX, b.MaxX)
		return a, true
	}
	sameX := absFloat32(a.MinX-b.MinX) <= waterMergeEpsilon && absFloat32(a.MaxX-b.MaxX) <= waterMergeEpsilon
	if sameX && rangesOverlapOrTouch(a.MinZ, a.MaxZ, b.MinZ, b.MaxZ, waterMergeEpsilon) {
		a.MinZ = minFloat32(a.MinZ, b.MinZ)
		a.MaxZ = maxFloat32(a.MaxZ, b.MaxZ)
		return a, true
	}
	return hl1WaterRect{}, false
}

func mergeHL1WaterRectPairByFill(a, b hl1WaterRect) (hl1WaterRect, bool) {
	if a.Kind != b.Kind || absFloat32(a.SurfaceY-b.SurfaceY) > waterMergeEpsilon || absFloat32(a.Depth-b.Depth) > waterMergeEpsilon {
		return hl1WaterRect{}, false
	}
	if !rectsOverlapOrTouch(a.MinX, a.MaxX, a.MinZ, a.MaxZ, b.MinX, b.MaxX, b.MinZ, b.MaxZ, waterMergeEpsilon) {
		return hl1WaterRect{}, false
	}
	combined := hl1WaterRect{
		Kind:     a.Kind,
		SurfaceY: a.SurfaceY,
		Depth:    a.Depth,
		MinX:     minFloat32(a.MinX, b.MinX),
		MaxX:     maxFloat32(a.MaxX, b.MaxX),
		MinZ:     minFloat32(a.MinZ, b.MinZ),
		MaxZ:     maxFloat32(a.MaxZ, b.MaxZ),
	}
	combinedArea := waterRectArea(combined)
	if combinedArea <= 0 {
		return hl1WaterRect{}, false
	}
	fillRatio := (waterRectArea(a) + waterRectArea(b) - waterRectIntersectionArea(a, b)) / combinedArea
	if fillRatio < waterRectFillRatio {
		return hl1WaterRect{}, false
	}
	return combined, true
}

func waterRectArea(rect hl1WaterRect) float32 {
	return maxFloat32(0, rect.MaxX-rect.MinX) * maxFloat32(0, rect.MaxZ-rect.MinZ)
}

func waterRectIntersectionArea(a, b hl1WaterRect) float32 {
	width := minFloat32(a.MaxX, b.MaxX) - maxFloat32(a.MinX, b.MinX)
	height := minFloat32(a.MaxZ, b.MaxZ) - maxFloat32(a.MinZ, b.MinZ)
	return maxFloat32(0, width) * maxFloat32(0, height)
}

func isLiquidMaterialKind(kind string) bool {
	switch kind {
	case "water", "slime", "lava":
		return true
	default:
		return false
	}
}

func liquidContentKind(contents int32) string {
	switch contents {
	case ContentsWater, ContentsTranslucent:
		return "water"
	case ContentsSlime:
		return "slime"
	case ContentsLava:
		return "lava"
	default:
		return ""
	}
}

func shouldVoxelizeFaceKind(kind string) bool {
	switch kind {
	case "sky", "trigger", "clip", "origin", "tool":
		return false
	default:
		return !isLiquidMaterialKind(kind)
	}
}

func hammerVectorToGekko(v importcommon.Vec3) importcommon.Vec3 {
	return importcommon.Vec3{X: v.X, Y: v.Z, Z: -v.Y}
}

func rectsOverlapOrTouch(aMinX, aMaxX, aMinZ, aMaxZ, bMinX, bMaxX, bMinZ, bMaxZ, eps float32) bool {
	return rangesOverlapOrTouch(aMinX, aMaxX, bMinX, bMaxX, eps) &&
		rangesOverlapOrTouch(aMinZ, aMaxZ, bMinZ, bMaxZ, eps)
}

func rangesOverlapOrTouch(aMin, aMax, bMin, bMax, eps float32) bool {
	return aMin <= bMax+eps && bMin <= aMax+eps
}

func absFloat32(v float32) float32 {
	return float32(math.Abs(float64(v)))
}

func liquidColor(kind string) content.Vec3 {
	switch kind {
	case "slime":
		return content.Vec3{0.34, 0.76, 0.18}
	case "lava":
		return content.Vec3{1.0, 0.28, 0.05}
	default:
		return content.Vec3{0.12, 0.42, 0.78}
	}
}

func liquidAbsorptionColor(kind string) content.Vec3 {
	switch kind {
	case "slime":
		return content.Vec3{0.18, 0.55, 0.08}
	case "lava":
		return content.Vec3{1.0, 0.18, 0.03}
	default:
		return content.Vec3{0.18, 0.42, 0.8}
	}
}

func liquidOpacity(kind string) float32 {
	switch kind {
	case "lava":
		return 0.82
	default:
		return 0.62
	}
}
