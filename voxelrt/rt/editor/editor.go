package editor

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"

	"github.com/go-gl/mathgl/mgl32"
)

type Ray struct {
	Origin    mgl32.Vec3
	Direction mgl32.Vec3
}

type Editor struct {
	BrushRadius    float32
	BrushValue     uint8
	SelectedObject *core.VoxelObject

	// Debounced Scaling
	PendingScaleFactor  float32
	LastScaleInputTime  float64
	LastScaleUpdateTime float64
}

func NewEditor() *Editor {
	return &Editor{
		BrushRadius:        2.0,
		BrushValue:         1,
		PendingScaleFactor: 1.0,
	}
}

func (e *Editor) Select(scene *core.Scene, ray Ray) {
	hit := e.Pick(scene, ray)
	if hit != nil {
		e.SelectedObject = hit.Object
	} else {
		e.SelectedObject = nil
	}
}

func (e *Editor) ScaleSelected(scene *core.Scene, factor float32, now float64) {
	if e.SelectedObject == nil {
		return
	}
	e.PendingScaleFactor *= factor
	e.LastScaleInputTime = now
}

func (e *Editor) Update(scene *core.Scene, now float64) {
	if e.SelectedObject == nil || e.PendingScaleFactor == 1.0 {
		return
	}

	// Apply if 200ms of silence OR if we haven't updated in 100ms
	idle := (now - e.LastScaleInputTime) > 0.2
	periodic := (now - e.LastScaleUpdateTime) > 0.1

	if idle || periodic {
		scene.RescaleObject(e.SelectedObject, e.PendingScaleFactor)
		e.PendingScaleFactor = 1.0
		e.LastScaleUpdateTime = now
	}
}

func (e *Editor) GetPickRay(mouseX, mouseY float64, width, height int, camera *core.CameraState) Ray {
	// Normalized Device Coordinates
	nx := (2.0*float32(mouseX))/float32(width) - 1.0
	ny := 1.0 - (2.0*float32(mouseY))/float32(height) // Flip Y for NDC

	forward := camera.GetForward()
	right := camera.GetRight()
	up := right.Cross(forward)

	// Aspect ratio and FOV
	aspect := float32(width) / float32(height)
	fovRad := mgl32.DegToRad(60.0) // Matches app.go
	tanHalfFov := float32(math.Tan(float64(fovRad / 2.0)))

	// Ray direction in world space
	dir := forward.Add(right.Mul(nx * aspect * tanHalfFov)).Add(up.Mul(ny * tanHalfFov))
	dir = dir.Normalize()

	return Ray{camera.Position, dir}
}

type HitResult struct {
	Object *core.VoxelObject
	Coord  [3]int
	T      float32
	Normal mgl32.Vec3
}

func (e *Editor) Pick(scene *core.Scene, ray Ray) *HitResult {
	closestT := float32(1e20)
	var bestHit *HitResult

	for _, obj := range scene.Objects {
		// 1. Broad phase: World AABB
		if obj.WorldAABB == nil {
			continue
		}
		tMin, tMax := intersectAABB(ray, obj.WorldAABB[0], obj.WorldAABB[1])
		if tMin > tMax || tMax < 0 || tMin > closestT {
			continue
		}

		// 2. Narrow phase: Object Space Ray March
		o2w := obj.Transform.ObjectToWorld()
		w2o := o2w.Inv()

		// Transform ray to object space
		ro4 := w2o.Mul4x1(ray.Origin.Vec4(1.0))
		rd4 := w2o.Mul4x1(ray.Direction.Vec4(0.0))
		ro := ro4.Vec3()
		rd := rd4.Vec3()

		hit, tObj, coord, normal := obj.XBrickMap.RayMarch(ro, rd, 0, 1000.0)

		if hit {
			// Compute world hit point and distance
			pHitOs := ro.Add(rd.Mul(tObj))
			pHitWs4 := o2w.Mul4x1(pHitOs.Vec4(1.0))
			pHitWs := pHitWs4.Vec3()
			tWorld := pHitWs.Sub(ray.Origin).Len()

			if tWorld < closestT {
				closestT = tWorld
				// Transform normal to world space
				nWs4 := o2w.Mul4x1(normal.Vec4(0.0))
				nWs := nWs4.Vec3().Normalize()

				bestHit = &HitResult{
					Object: obj,
					Coord:  coord,
					T:      tWorld,
					Normal: nWs,
				}
			}
		}
	}

	return bestHit
}

func (e *Editor) ApplyBrush(obj *core.VoxelObject, centerCoord [3]int, normal mgl32.Vec3) {
	cx, cy, cz := centerCoord[0], centerCoord[1], centerCoord[2]

	// If we are building (not erasing), offset by normal to place voxels ON the surface
	if e.BrushValue != 0 {
		cx += int(math.Round(float64(normal.X())))
		cy += int(math.Round(float64(normal.Y())))
		cz += int(math.Round(float64(normal.Z())))
	}

	r := int(math.Ceil(float64(e.BrushRadius)))

	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r; dy++ {
			for dz := -r; dz <= r; dz++ {
				vx, vy, vz := cx+dx, cy+dy, cz+dz

				distSq := float32(dx*dx + dy*dy + dz*dz)
				if distSq > e.BrushRadius*e.BrushRadius {
					continue
				}

				obj.XBrickMap.SetVoxel(vx, vy, vz, e.BrushValue)
			}
		}
	}
}

func intersectAABB(ray Ray, minB, maxB mgl32.Vec3) (float32, float32) {
	invDir := mgl32.Vec3{1.0 / (ray.Direction.X() + 1e-8), 1.0 / (ray.Direction.Y() + 1e-8), 1.0 / (ray.Direction.Z() + 1e-8)}
	t1 := minB.Sub(ray.Origin)
	t1 = mgl32.Vec3{t1.X() * invDir.X(), t1.Y() * invDir.Y(), t1.Z() * invDir.Z()}
	t2 := maxB.Sub(ray.Origin)
	t2 = mgl32.Vec3{t2.X() * invDir.X(), t2.Y() * invDir.Y(), t2.Z() * invDir.Z()}

	tMinV := mgl32.Vec3{float32(math.Min(float64(t1.X()), float64(t2.X()))), float32(math.Min(float64(t1.Y()), float64(t2.Y()))), float32(math.Min(float64(t1.Z()), float64(t2.Z())))}
	tMaxV := mgl32.Vec3{float32(math.Max(float64(t1.X()), float64(t2.X()))), float32(math.Max(float64(t1.Y()), float64(t2.Y()))), float32(math.Max(float64(t1.Z()), float64(t2.Z())))}

	realMin := float32(math.Max(0, math.Max(float64(tMinV.X()), math.Max(float64(tMinV.Y()), float64(tMinV.Z())))))
	realMax := float32(math.Min(math.MaxFloat32, math.Min(float64(tMaxV.X()), math.Min(float64(tMaxV.Y()), float64(tMaxV.Z())))))

	return realMin, realMax
}
