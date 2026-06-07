package app

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

type waterScissorRect struct {
	X uint32
	Y uint32
	W uint32
	H uint32
}

type waterRenderCandidate struct {
	WaterIndex int
	Scissor    waterScissorRect
}

func buildWaterRenderCandidates(camera *core.CameraState, width, height uint32, waters []gpu_rt.WaterSurfaceHost) []waterRenderCandidate {
	if camera == nil || width == 0 || height == 0 || len(waters) == 0 {
		return nil
	}
	candidates := make([]waterRenderCandidate, 0, len(waters))
	for idx, water := range waters {
		scissor, ok := projectedWaterScissor(camera, width, height, water)
		if !ok || scissor.W == 0 || scissor.H == 0 {
			continue
		}
		candidates = append(candidates, waterRenderCandidate{WaterIndex: idx, Scissor: scissor})
	}
	return candidates
}

func projectedWaterScissor(camera *core.CameraState, width, height uint32, water gpu_rt.WaterSurfaceHost) (waterScissorRect, bool) {
	full := waterScissorRect{X: 0, Y: 0, W: width, H: height}
	if camera == nil || width == 0 || height == 0 || water.HalfExtents[0] <= 0 || water.HalfExtents[1] <= 0 || water.Depth <= 0 {
		return full, false
	}
	if cameraInsideWaterVolume(camera.Position, water) {
		return full, true
	}

	aspect := float32(width) / float32(height)
	if aspect <= 0 {
		aspect = 1
	}
	view := camera.GetViewMatrix()
	proj := camera.ProjectionMatrix(aspect)
	near := camera.NearPlane()
	if near <= 0 {
		near = 0.1
	}

	minNDCX := float32(math.Inf(1))
	minNDCY := float32(math.Inf(1))
	maxNDCX := float32(math.Inf(-1))
	maxNDCY := float32(math.Inf(-1))
	visible := false

	for _, corner := range waterWorldCorners(water) {
		viewPos := view.Mul4x1(corner.Vec4(1.0))
		if viewPos.Z() >= 0 {
			return full, true
		}
		if -viewPos.Z() < near {
			viewPos[2] = -near
		}
		clip := proj.Mul4x1(viewPos)
		if absf(clip.W()) < 1e-6 {
			continue
		}
		ndc := clip.Mul(1.0 / clip.W())
		minNDCX = minf(minNDCX, ndc.X())
		minNDCY = minf(minNDCY, ndc.Y())
		maxNDCX = maxf(maxNDCX, ndc.X())
		maxNDCY = maxf(maxNDCY, ndc.Y())
		visible = true
	}

	if !visible {
		return full, false
	}
	if maxNDCX < -1 || minNDCX > 1 || maxNDCY < -1 || minNDCY > 1 {
		return full, false
	}

	minNDCX = clampf(minNDCX, -1, 1)
	minNDCY = clampf(minNDCY, -1, 1)
	maxNDCX = clampf(maxNDCX, -1, 1)
	maxNDCY = clampf(maxNDCY, -1, 1)

	minX := uint32(math.Floor(float64((minNDCX + 1) * 0.5 * float32(width))))
	maxX := uint32(math.Ceil(float64((maxNDCX + 1) * 0.5 * float32(width))))
	minY := uint32(math.Floor(float64((1 - (maxNDCY+1)*0.5) * float32(height))))
	maxY := uint32(math.Ceil(float64((1 - (minNDCY+1)*0.5) * float32(height))))

	minX = min(width, minX)
	maxX = min(width, maxX)
	minY = min(height, minY)
	maxY = min(height, maxY)
	if maxX <= minX || maxY <= minY {
		return full, false
	}

	const pixelPad uint32 = 8
	if minX > pixelPad {
		minX -= pixelPad
	} else {
		minX = 0
	}
	if minY > pixelPad {
		minY -= pixelPad
	} else {
		minY = 0
	}
	maxX = min(width, maxX+pixelPad)
	maxY = min(height, maxY+pixelPad)

	return waterScissorRect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
}

func cameraInsideWaterVolume(cameraPos mgl32.Vec3, water gpu_rt.WaterSurfaceHost) bool {
	minB, maxB := waterBounds(water)
	return cameraPos.X() >= minB.X() && cameraPos.X() <= maxB.X() &&
		cameraPos.Y() >= minB.Y() && cameraPos.Y() <= maxB.Y() &&
		cameraPos.Z() >= minB.Z() && cameraPos.Z() <= maxB.Z()
}

func waterWorldCorners(water gpu_rt.WaterSurfaceHost) [8]mgl32.Vec3 {
	minB, maxB := waterBounds(water)
	return [8]mgl32.Vec3{
		{minB.X(), minB.Y(), minB.Z()},
		{maxB.X(), minB.Y(), minB.Z()},
		{minB.X(), maxB.Y(), minB.Z()},
		{maxB.X(), maxB.Y(), minB.Z()},
		{minB.X(), minB.Y(), maxB.Z()},
		{maxB.X(), minB.Y(), maxB.Z()},
		{minB.X(), maxB.Y(), maxB.Z()},
		{maxB.X(), maxB.Y(), maxB.Z()},
	}
}

func waterBounds(water gpu_rt.WaterSurfaceHost) (mgl32.Vec3, mgl32.Vec3) {
	cell := water.VisualCellSize
	if cell <= 0 {
		cell = 0.2
	}
	wavePad := maxf(cell*3, water.WaveAmplitude*8)
	xzPad := wavePad + 0.25
	yPad := wavePad + 0.15
	center := water.Position
	minB := mgl32.Vec3{
		center.X() - water.HalfExtents[0] - xzPad,
		center.Y() - water.Depth - yPad,
		center.Z() - water.HalfExtents[1] - xzPad,
	}
	maxB := mgl32.Vec3{
		center.X() + water.HalfExtents[0] + xzPad,
		center.Y() + yPad,
		center.Z() + water.HalfExtents[1] + xzPad,
	}
	return minB, maxB
}
