package app

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

type caScissorRect struct {
	X uint32
	Y uint32
	W uint32
	H uint32
}

func projectedCAVolumeScissor(camera *core.CameraState, width, height uint32, volume gpu_rt.CAVolumeHost) (caScissorRect, bool) {
	full := caScissorRect{X: 0, Y: 0, W: width, H: height}
	if camera == nil || width == 0 || height == 0 {
		return full, false
	}
	if cameraInsideCAVolume(camera.Position, volume, true) {
		return full, true
	}

	aspect := float32(width) / float32(height)
	if aspect <= 0 {
		aspect = 1.0
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

	for _, corner := range caVolumeWorldCorners(volume, true) {
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

	if minX > width {
		minX = width
	}
	if maxX > width {
		maxX = width
	}
	if minY > height {
		minY = height
	}
	if maxY > height {
		maxY = height
	}
	if maxX <= minX || maxY <= minY {
		return full, false
	}

	// Keep a small guard band because the projected bounds come from a clipped 3D box
	// while the shader still adds noisy shell detail near the edges.
	const pixelPad uint32 = 2
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

	return caScissorRect{
		X: minX,
		Y: minY,
		W: maxX - minX,
		H: maxY - minY,
	}, true
}

func caVolumeWorldCorners(volume gpu_rt.CAVolumeHost, expanded bool) [8]mgl32.Vec3 {
	localToWorld := caVolumeLocalToWorld(volume)
	minCorner, maxCorner := caVolumeLocalBounds(volume, expanded)
	return [8]mgl32.Vec3{
		transformPoint(localToWorld, mgl32.Vec3{minCorner.X(), minCorner.Y(), minCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{maxCorner.X(), minCorner.Y(), minCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{minCorner.X(), maxCorner.Y(), minCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{maxCorner.X(), maxCorner.Y(), minCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{minCorner.X(), minCorner.Y(), maxCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{maxCorner.X(), minCorner.Y(), maxCorner.Z()}),
		transformPoint(localToWorld, mgl32.Vec3{minCorner.X(), maxCorner.Y(), maxCorner.Z()}),
		transformPoint(localToWorld, maxCorner),
	}
}

func cameraInsideCAVolume(cameraPos mgl32.Vec3, volume gpu_rt.CAVolumeHost, expanded bool) bool {
	worldToLocal := caVolumeLocalToWorld(volume).Inv()
	local := worldToLocal.Mul4x1(cameraPos.Vec4(1.0)).Vec3()
	minCorner, maxCorner := caVolumeLocalBounds(volume, expanded)
	return local.X() >= minCorner.X() && local.X() <= maxCorner.X() &&
		local.Y() >= minCorner.Y() && local.Y() <= maxCorner.Y() &&
		local.Z() >= minCorner.Z() && local.Z() <= maxCorner.Z()
}

func caVolumeWorldCenter(volume gpu_rt.CAVolumeHost) mgl32.Vec3 {
	return transformPoint(caVolumeLocalToWorld(volume), mgl32.Vec3{
		float32(volume.Resolution[0]) * 0.5,
		float32(volume.Resolution[1]) * 0.5,
		float32(volume.Resolution[2]) * 0.5,
	})
}

func caVolumeLocalToWorld(volume gpu_rt.CAVolumeHost) mgl32.Mat4 {
	return mgl32.Translate3D(volume.Position.X(), volume.Position.Y(), volume.Position.Z()).
		Mul4(volume.Rotation.Mat4()).
		Mul4(mgl32.Scale3D(volume.VoxelScale.X(), volume.VoxelScale.Y(), volume.VoxelScale.Z()))
}

func caVolumeLocalBounds(volume gpu_rt.CAVolumeHost, expanded bool) (mgl32.Vec3, mgl32.Vec3) {
	minCorner := mgl32.Vec3{0, 0, 0}
	maxCorner := mgl32.Vec3{
		float32(volume.Resolution[0]),
		float32(volume.Resolution[1]),
		float32(volume.Resolution[2]),
	}
	if !expanded {
		return minCorner, maxCorner
	}

	expandLo := float32(3.0)
	expandHi := float32(4.0)
	if volume.Preset == 3 && volume.Type == 1 {
		expandLo = 8.0
		expandHi = 10.0
	}

	shell := float32(0.8)
	if volume.Type == 1 {
		shell = 0.55
	}
	if volume.Preset == 2 && volume.Type == 0 {
		shell = 1.0
	} else if volume.Preset == 3 {
		shell = 0.0
	} else if volume.Preset == 4 {
		shell = 0.75
	}

	paddingLo := expandLo + shell
	paddingHi := expandHi + shell
	return mgl32.Vec3{-paddingLo, -paddingLo, -paddingLo}, mgl32.Vec3{
		maxCorner.X() + paddingHi,
		maxCorner.Y() + paddingHi,
		maxCorner.Z() + paddingHi,
	}
}

func transformPoint(m mgl32.Mat4, p mgl32.Vec3) mgl32.Vec3 {
	return m.Mul4x1(p.Vec4(1.0)).Vec3()
}

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
