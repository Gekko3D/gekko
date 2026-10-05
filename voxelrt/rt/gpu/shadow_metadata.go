package gpu

import (
	"math"
	"sort"

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

type groupedShadowCandidate struct {
	obj      *core.VoxelObject
	distance float32
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

	// Fit in camera-relative coordinates so translating the camera does not
	// perturb the extent. Retain its orientation and existing camera fallbacks.
	relativeCamera := *camera
	relativeCamera.Position = mgl32.Vec3{}
	relativeCamera.LookAt = camera.LookAt.Sub(camera.Position)
	corners := cameraSliceCorners(&relativeCamera, aspect, nearDist, farDist)
	view := mgl32.LookAtV(mgl32.Vec3{}, dir, shadowUpVector(dir))
	relativeCenter := mgl32.Vec3{}
	for _, corner := range corners {
		relativeCenter = relativeCenter.Add(corner)
	}
	relativeCenter = relativeCenter.Mul(1.0 / float32(len(corners)))
	relativeCenterLS := view.Mul4x1(relativeCenter.Vec4(1)).Vec3()
	halfExtent := float32(0)
	for _, corner := range corners {
		cornerLS := view.Mul4x1(corner.Vec4(1)).Vec3()
		halfExtent = maxf(halfExtent, float32(math.Abs(float64(cornerLS.X()-relativeCenterLS.X()))))
		halfExtent = maxf(halfExtent, float32(math.Abs(float64(cornerLS.Y()-relativeCenterLS.Y()))))
	}
	halfExtent = maxf(halfExtent, 1)
	if effectiveResolution >= 2 {
		// One base texel of margin covers the maximum half-final-texel snap.
		halfExtent += 2 * halfExtent / float32(effectiveResolution)
	}
	texelWorldSize := 2 * halfExtent / float32(effectiveResolution)
	worldCenter := camera.Position.Add(relativeCenter)
	centerLS := view.Mul4x1(worldCenter.Vec4(1)).Vec3()
	snappedX, snappedY := centerLS.X(), centerLS.Y()
	if effectiveResolution >= 2 {
		snappedX = float32(math.Round(float64(snappedX/texelWorldSize))) * texelWorldSize
		snappedY = float32(math.Round(float64(snappedY/texelWorldSize))) * texelWorldSize
		// Round preserves signed zero; exact dependency bits require one zero sign
		// for the entire origin cell, including negative sub-texel translations.
		if snappedX == 0 {
			snappedX = 0
		}
		if snappedY == 0 {
			snappedY = 0
		}
	}
	// Keep the light rotation fixed rather than rebuilding LookAt from translated
	// points. Lateral translation follows the world grid; depth stays unsnapped.
	view[12], view[13] = -snappedX, -snappedY
	view[14] = -(centerLS.Z() + directionalShadowDepth*.5)

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
	delta := volume.Position.Sub(center)
	clamped := mgl32.Vec3{
		maxf(-halfExtents.X(), minf(delta.X(), halfExtents.X())),
		maxf(-halfExtents.Y(), minf(delta.Y(), halfExtents.Y())),
		maxf(-halfExtents.Z(), minf(delta.Z(), halfExtents.Z())),
	}
	closest := center.Add(clamped)
	return closest.Sub(volume.Position).LenSqr() <= volume.Range*volume.Range
}

func collectShadowCasters(objects []*core.VoxelObject, directionalVolumes []directionalShadowCullVolume, spotVolumes []spotShadowCullVolume, pointVolumes []pointShadowCullVolume, cameraPosition mgl32.Vec3) []*core.VoxelObject {
	if len(directionalVolumes) == 0 && len(spotVolumes) == 0 && len(pointVolumes) == 0 {
		return nil
	}

	shadowObjects := make([]*core.VoxelObject, 0, len(objects))
	grouped := make(map[uint64][]groupedShadowCandidate)
	groupLimits := make(map[uint64]int)
	for _, obj := range objects {
		if obj == nil || obj.RenderWorldBounds() == nil || obj.RenderVoxelMap() == nil || !obj.CastsShadows {
			continue
		}

		include := false
		for _, volume := range directionalVolumes {
			if intersectsDirectionalShadowVolume(*obj.RenderWorldBounds(), volume) {
				include = true
				break
			}
		}
		if !include {
			for _, volume := range spotVolumes {
				if intersectsSpotShadowVolume(*obj.RenderWorldBounds(), volume) {
					include = true
					break
				}
			}
		}
		if !include {
			for _, volume := range pointVolumes {
				if intersectsPointShadowVolume(*obj.RenderWorldBounds(), volume) {
					include = true
					break
				}
			}
		}
		if include {
			distance := distancePointToAABB(cameraPosition, *obj.RenderWorldBounds())
			if obj.ShadowMaxDistance > 0 && distance > obj.ShadowMaxDistance {
				continue
			}
			if obj.ShadowCasterGroupID != 0 && obj.ShadowCasterGroupLimit > 0 {
				grouped[obj.ShadowCasterGroupID] = append(grouped[obj.ShadowCasterGroupID], groupedShadowCandidate{
					obj:      obj,
					distance: distance,
				})
				limit := groupLimits[obj.ShadowCasterGroupID]
				if limit == 0 || obj.ShadowCasterGroupLimit < limit {
					groupLimits[obj.ShadowCasterGroupID] = obj.ShadowCasterGroupLimit
				}
				continue
			}
			shadowObjects = append(shadowObjects, obj)
		}
	}
	if len(grouped) == 0 {
		return shadowObjects
	}

	groupIDs := make([]uint64, 0, len(grouped))
	for groupID := range grouped {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool {
		return groupIDs[i] < groupIDs[j]
	})
	for _, groupID := range groupIDs {
		candidates := grouped[groupID]
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].distance == candidates[j].distance {
				return candidates[i].obj.RenderWorldBounds()[0].Z() < candidates[j].obj.RenderWorldBounds()[0].Z()
			}
			return candidates[i].distance < candidates[j].distance
		})
		limit := groupLimits[groupID]
		if limit <= 0 || limit > len(candidates) {
			limit = len(candidates)
		}
		for i := 0; i < limit; i++ {
			shadowObjects = append(shadowObjects, candidates[i].obj)
		}
	}
	return shadowObjects
}

func rebuildShadowCasterScene(scene *core.Scene, shadowObjects []*core.VoxelObject) {
	scene.ShadowObjects = scene.ShadowObjects[:0]
	for _, obj := range shadowObjects {
		if obj != nil && obj.RenderVoxelMap() != nil && obj.RenderWorldBounds() != nil {
			scene.ShadowObjects = append(scene.ShadowObjects, obj)
		}
	}

	if len(scene.ShadowObjects) == 0 {
		scene.ShadowBVHNodesBytes = make([]byte, 64)
		return
	}

	aabbs := make([][2]mgl32.Vec3, len(scene.ShadowObjects))
	for i, obj := range scene.ShadowObjects {
		aabbs[i] = *obj.RenderWorldBounds()
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

func distancePointToAABB(point mgl32.Vec3, aabb [2]mgl32.Vec3) float32 {
	clamped := mgl32.Vec3{
		maxf(aabb[0].X(), minf(point.X(), aabb[1].X())),
		maxf(aabb[0].Y(), minf(point.Y(), aabb[1].Y())),
		maxf(aabb[0].Z(), minf(point.Z(), aabb[1].Z())),
	}
	return point.Sub(clamped).Len()
}
