package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func (s *VoxelRtState) DrawDebugRay(origin, dir mgl32.Vec3, color [4]float32, duration float32) {
	if s == nil {
		return
	}
	s.debugRays = append(s.debugRays, DebugRay{
		Origin:   origin,
		Dir:      dir,
		Color:    color,
		Duration: duration,
	})
}

func (s *VoxelRtState) Project(pos mgl32.Vec3) (float32, float32, bool) {
	if s == nil || s.RtApp == nil {
		return 0, 0, false
	}
	// Use current camera to build projection
	view := s.RtApp.Camera.GetViewMatrix()
	aspect := float32(s.RtApp.Config.Width) / float32(s.RtApp.Config.Height)
	if aspect == 0 {
		aspect = 1.0
	}

	// GET ACTUAL FOV FROM CAMERA COMPONENT
	fov := float32(45.0) // Default
	// We need a way to get the true FOV. For now matching playing.go
	proj := mgl32.Perspective(mgl32.DegToRad(fov), aspect, 0.1, 1000.0)
	vp := proj.Mul4(view)

	clip := vp.Mul4x1(pos.Vec4(1.0))

	// Clip points behind far or too close to near plane
	if clip.W() < 0.1 {
		return 0, 0, false
	}

	ndc := clip.Vec3().Mul(1.0 / clip.W())

	// NDC to Screen (USE PIXEL DIMENSIONS)
	w, h := float32(s.RtApp.Config.Width), float32(s.RtApp.Config.Height)
	x := (ndc.X()*0.5 + 0.5) * w
	y := (1.0 - (ndc.Y()*0.5 + 0.5)) * h

	// Final bounds check
	if x < 0 || x > w || y < 0 || y > h {
		return x, y, false
	}

	return x, y, true
}

func (s *VoxelRtState) Raycast(origin, dir mgl32.Vec3, tMax float32) RaycastHit {
	if s == nil {
		return RaycastHit{}
	}

	bestHit := RaycastHit{T: tMax + 1.0}

	// 1. Check all instances (models, CA, etc.)
	checkMap := func(m map[EntityId]*core.VoxelObject) {
		for eid, obj := range m {
			if obj.XBrickMap == nil {
				continue
			}

			// Transform ray to object space
			w2o := obj.Transform.WorldToObject()
			localOrigin := w2o.Mul4x1(origin.Vec4(1.0)).Vec3()

			// Direction transformation
			localDirUnnorm := w2o.Mul4x1(dir.Vec4(0.0)).Vec3()
			scaleFactor := localDirUnnorm.Len()
			// Avoid division by zero
			if scaleFactor < 1e-6 {
				continue
			}
			localDir := localDirUnnorm.Mul(1.0 / scaleFactor)

			localTMax := tMax * scaleFactor

			hit, t, pos, normal := obj.XBrickMap.RayMarch(localOrigin, localDir, 0, localTMax)
			if hit {
				// We need to convert t back to world space distance.
				// World distance = t * |ObjDir| where ObjDir is the untransformed local direction.
				// Since we normalized localDir, we need the original scale factor.

				// Actually, a better way: hitPointWorld = o2w * hitPointLocal.
				// tWorld = |hitPointWorld - origin|

				o2w := obj.Transform.ObjectToWorld()
				localHitPos := localOrigin.Add(localDir.Mul(t))
				worldHitPos := o2w.Mul4x1(localHitPos.Vec4(1.0)).Vec3()
				worldT := worldHitPos.Sub(origin).Len()

				if worldT < bestHit.T {
					bestHit.Hit = true
					bestHit.T = worldT
					bestHit.Pos = pos

					// Transform normal to world space
					// Normal transform: transpose(inverse(M))
					worldNormal := o2w.Mul4x1(normal.Vec4(0.0)).Vec3().Normalize()
					bestHit.Normal = worldNormal
					bestHit.Entity = eid
				}
			}
		}
	}

	checkMap(s.instanceMap)
	checkMap(s.caVolumeMap)
	checkMap(s.worldMap)

	if bestHit.Hit {
		return bestHit
	}
	return RaycastHit{}
}

func (s *VoxelRtState) RaycastSubstepped(origin, dir mgl32.Vec3, distance float32, substeps int) RaycastHit {
	if substeps <= 1 {
		return s.Raycast(origin, dir, distance)
	}

	subDt := distance / float32(substeps)
	for i := 0; i < substeps; i++ {
		subOrigin := origin.Add(dir.Mul(float32(i) * subDt))
		hit := s.Raycast(subOrigin, dir, subDt)
		if hit.Hit {
			// Offset T by the distance already traveled
			hit.T += float32(i) * subDt
			return hit
		}
	}
	return RaycastHit{}
}