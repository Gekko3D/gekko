package gpu

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/bvh"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	directionalShadowDepth       = float32(640.0)
	directionalShadowNear        = float32(0.1)
	directionalShadowMapSize     = float32(1024.0)
	directionalShadowCasterGuard = float32(4.0)
)

var directionalCascadeFarDistances = [core.DirectionalShadowCascadeCount]float32{48.0, 160.0}

type directionalShadowCullVolume struct {
	View       mgl32.Mat4
	HalfExtent float32
	NearPlane  float32
	FarPlane   float32
	GuardBand  float32
}

type spotShadowCullVolume struct {
	Position mgl32.Vec3
	Dir      mgl32.Vec3
	Range    float32
	CosCone  float32
}

type pointShadowCullVolume struct {
	Position mgl32.Vec3
	Range    float32
}

func shadowUpVector(dir mgl32.Vec3) mgl32.Vec3 {
	up := mgl32.Vec3{0, 1, 0}
	if math.Abs(float64(dir.Y())) > 0.99 {
		up = mgl32.Vec3{1, 0, 0}
	}
	return up
}

func cameraSliceCorners(camera *core.CameraState, aspect, nearDist, farDist float32) [8]mgl32.Vec3 {
	if aspect <= 0 {
		aspect = 1.0
	}
	forward := camera.GetForward()
	if forward.Len() < 1e-5 {
		forward = mgl32.Vec3{0, 0, -1}
	}
	forward = forward.Normalize()
	right := camera.GetRight()
	if right.Len() < 1e-5 {
		right = mgl32.Vec3{1, 0, 0}
	}
	right = right.Normalize()
	up := right.Cross(forward)
	if up.Len() < 1e-5 {
		up = mgl32.Vec3{0, 1, 0}
	} else {
		up = up.Normalize()
	}

	tanHalfFov := float32(math.Tan(float64(camera.FovRadians() * 0.5)))
	nearHalfH := tanHalfFov * nearDist
	nearHalfW := nearHalfH * aspect
	farHalfH := tanHalfFov * farDist
	farHalfW := farHalfH * aspect

	nearCenter := camera.Position.Add(forward.Mul(nearDist))
	farCenter := camera.Position.Add(forward.Mul(farDist))

	return [8]mgl32.Vec3{
		nearCenter.Sub(right.Mul(nearHalfW)).Sub(up.Mul(nearHalfH)),
		nearCenter.Add(right.Mul(nearHalfW)).Sub(up.Mul(nearHalfH)),
		nearCenter.Sub(right.Mul(nearHalfW)).Add(up.Mul(nearHalfH)),
		nearCenter.Add(right.Mul(nearHalfW)).Add(up.Mul(nearHalfH)),
		farCenter.Sub(right.Mul(farHalfW)).Sub(up.Mul(farHalfH)),
		farCenter.Add(right.Mul(farHalfW)).Sub(up.Mul(farHalfH)),
		farCenter.Sub(right.Mul(farHalfW)).Add(up.Mul(farHalfH)),
		farCenter.Add(right.Mul(farHalfW)).Add(up.Mul(farHalfH)),
	}
}

func buildDirectionalShadowCascade(camera *core.CameraState, aspect float32, dir mgl32.Vec3, splitNear, splitFar float32, effectiveResolution uint32) (core.DirectionalShadowCascade, directionalShadowCullVolume) {
	cascade := core.DirectionalShadowCascade{}
	if camera == nil {
		return cascade, directionalShadowCullVolume{}
	}
	if effectiveResolution == 0 {
		effectiveResolution = shadowAtlasLayerResolution
	}

	nearDist := maxf(camera.NearPlane(), splitNear)
	farDist := minf(camera.FarPlane(), splitFar)
	if farDist <= nearDist {
		farDist = nearDist + 1.0
	}

	corners := cameraSliceCorners(camera, aspect, nearDist, farDist)
	up := shadowUpVector(dir)
	center := mgl32.Vec3{}
	for _, corner := range corners {
		center = center.Add(corner)
	}
	center = center.Mul(1.0 / float32(len(corners)))
	eye := center.Sub(dir.Mul(directionalShadowDepth * 0.5))
	view := mgl32.LookAtV(eye, center, up)

	centerLS := view.Mul4x1(center.Vec4(1.0)).Vec3()
	halfExtent := float32(0.0)
	for _, corner := range corners {
		cornerLS := view.Mul4x1(corner.Vec4(1.0)).Vec3()
		halfExtent = maxf(halfExtent, float32(math.Abs(float64(cornerLS.X()-centerLS.X()))))
		halfExtent = maxf(halfExtent, float32(math.Abs(float64(cornerLS.Y()-centerLS.Y()))))
	}
	if halfExtent < 1.0 {
		halfExtent = 1.0
	}
	texelWorldSize := (halfExtent * 2.0) / float32(effectiveResolution)
	snappedX := float32(math.Round(float64(centerLS.X()/texelWorldSize))) * texelWorldSize
	snappedY := float32(math.Round(float64(centerLS.Y()/texelWorldSize))) * texelWorldSize
	offsetLS := mgl32.Vec3{snappedX - centerLS.X(), snappedY - centerLS.Y(), -(directionalShadowDepth * 0.5) - centerLS.Z()}
	offsetWS := view.Inv().Mul4x1(offsetLS.Vec4(0.0)).Vec3()
	eye = eye.Add(offsetWS)
	center = center.Add(offsetWS)
	view = mgl32.LookAtV(eye, center, up)

	proj := mgl32.Ortho(-halfExtent, halfExtent, -halfExtent, halfExtent, directionalShadowNear, directionalShadowDepth)
	vp := proj.Mul4(view)

	cascade.ViewProj = [16]float32(vp)
	cascade.InvViewProj = [16]float32(vp.Inv())
	cascade.Params = [4]float32{
		splitFar,
		texelWorldSize,
		2.0 / (directionalShadowDepth - directionalShadowNear),
		0.0,
	}

	return cascade, directionalShadowCullVolume{
		View:       view,
		HalfExtent: halfExtent,
		NearPlane:  directionalShadowNear,
		FarPlane:   directionalShadowDepth,
		GuardBand:  maxf(directionalShadowCasterGuard, texelWorldSize*8.0),
	}
}

func transformAABBToLocal(mat mgl32.Mat4, aabb [2]mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	corners := [8]mgl32.Vec3{
		{aabb[0].X(), aabb[0].Y(), aabb[0].Z()},
		{aabb[1].X(), aabb[0].Y(), aabb[0].Z()},
		{aabb[0].X(), aabb[1].Y(), aabb[0].Z()},
		{aabb[1].X(), aabb[1].Y(), aabb[0].Z()},
		{aabb[0].X(), aabb[0].Y(), aabb[1].Z()},
		{aabb[1].X(), aabb[0].Y(), aabb[1].Z()},
		{aabb[0].X(), aabb[1].Y(), aabb[1].Z()},
		{aabb[1].X(), aabb[1].Y(), aabb[1].Z()},
	}

	minP := mgl32.Vec3{float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)}
	maxP := mgl32.Vec3{-float32(math.MaxFloat32), -float32(math.MaxFloat32), -float32(math.MaxFloat32)}
	for _, corner := range corners {
		p := mat.Mul4x1(corner.Vec4(1.0)).Vec3()
		minP = mgl32.Vec3{minf(minP.X(), p.X()), minf(minP.Y(), p.Y()), minf(minP.Z(), p.Z())}
		maxP = mgl32.Vec3{maxf(maxP.X(), p.X()), maxf(maxP.Y(), p.Y()), maxf(maxP.Z(), p.Z())}
	}
	return minP, maxP
}

func intersectsDirectionalShadowVolume(aabb [2]mgl32.Vec3, volume directionalShadowCullVolume) bool {
	minLS, maxLS := transformAABBToLocal(volume.View, aabb)
	guard := volume.GuardBand
	if maxLS.X() < -volume.HalfExtent-guard || minLS.X() > volume.HalfExtent+guard {
		return false
	}
	if maxLS.Y() < -volume.HalfExtent-guard || minLS.Y() > volume.HalfExtent+guard {
		return false
	}
	nearZ := -volume.NearPlane + guard
	farZ := -volume.FarPlane - guard
	return maxLS.Z() >= farZ && minLS.Z() <= nearZ
}

func intersectsSpotShadowVolume(aabb [2]mgl32.Vec3, volume spotShadowCullVolume) bool {
	center := aabb[0].Add(aabb[1]).Mul(0.5)
	radius := aabb[1].Sub(center).Len()
	toCenter := center.Sub(volume.Position)
	dist := toCenter.Len()
	if dist-radius > volume.Range {
		return false
	}
	if dist <= radius || dist <= 1e-5 {
		return true
	}

	dir := volume.Dir
	if dir.Len() < 1e-5 {
		return false
	}
	dir = dir.Normalize()
	dotCenter := dir.Dot(toCenter.Mul(1.0 / dist))
	angularSlack := float32(math.Asin(math.Min(1.0, float64(radius/dist))))
	minDot := float32(math.Cos(math.Acos(float64(volume.CosCone)) + float64(angularSlack)))
	return dotCenter >= minDot
}

func intersectsPointShadowVolume(aabb [2]mgl32.Vec3, volume pointShadowCullVolume) bool {
	center := aabb[0].Add(aabb[1]).Mul(0.5)
	halfExtents := aabb[1].Sub(center)
	delta := center.Sub(volume.Position)
	clamped := mgl32.Vec3{
		maxf(-halfExtents.X(), minf(delta.X(), halfExtents.X())),
		maxf(-halfExtents.Y(), minf(delta.Y(), halfExtents.Y())),
		maxf(-halfExtents.Z(), minf(delta.Z(), halfExtents.Z())),
	}
	closest := center.Add(clamped)
	return closest.Sub(volume.Position).LenSqr() <= volume.Range*volume.Range
}

func collectShadowCasters(objects []*core.VoxelObject, directionalVolumes []directionalShadowCullVolume, spotVolumes []spotShadowCullVolume, pointVolumes []pointShadowCullVolume) []*core.VoxelObject {
	if len(directionalVolumes) == 0 && len(spotVolumes) == 0 && len(pointVolumes) == 0 {
		return nil
	}

	shadowObjects := make([]*core.VoxelObject, 0, len(objects))
	for _, obj := range objects {
		if obj == nil || obj.WorldAABB == nil || obj.XBrickMap == nil {
			continue
		}

		include := false
		for _, volume := range directionalVolumes {
			if intersectsDirectionalShadowVolume(*obj.WorldAABB, volume) {
				include = true
				break
			}
		}
		if !include {
			for _, volume := range spotVolumes {
				if intersectsSpotShadowVolume(*obj.WorldAABB, volume) {
					include = true
					break
				}
			}
		}
		if !include {
			for _, volume := range pointVolumes {
				if intersectsPointShadowVolume(*obj.WorldAABB, volume) {
					include = true
					break
				}
			}
		}
		if include {
			shadowObjects = append(shadowObjects, obj)
		}
	}
	return shadowObjects
}

func rebuildShadowCasterScene(scene *core.Scene, shadowObjects []*core.VoxelObject) {
	scene.ShadowObjects = scene.ShadowObjects[:0]
	scene.ShadowObjects = append(scene.ShadowObjects, shadowObjects...)

	if len(scene.ShadowObjects) == 0 {
		scene.ShadowBVHNodesBytes = make([]byte, 64)
		return
	}

	aabbs := make([][2]mgl32.Vec3, len(scene.ShadowObjects))
	for i, obj := range scene.ShadowObjects {
		aabbs[i] = *obj.WorldAABB
	}
	builder := &bvh.TLASBuilder{}
	scene.ShadowBVHNodesBytes = builder.Build(aabbs)
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
