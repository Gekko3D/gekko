package core

import (
	"fmt"

	"github.com/gekko3d/gekko/voxelrt/rt/bvh"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/go-gl/mathgl/mgl32"
)

type VoxelObject struct {
	Transform     *Transform
	XBrickMap     *volume.XBrickMap
	MaterialTable []Material
	WorldAABB     *[2]mgl32.Vec3 // Min, Max
	Tree64LOD     []byte
	LODThreshold  float32
}

func NewVoxelObject() *VoxelObject {
	return &VoxelObject{
		Transform:    NewTransform(),
		XBrickMap:    volume.NewXBrickMap(),
		LODThreshold: 50.0,
	}
}

func (obj *VoxelObject) UpdateWorldAABB() bool {
	if !obj.XBrickMap.AABBDirty && !obj.Transform.Dirty && obj.WorldAABB != nil {
		return false
	}

	minB, maxB := obj.XBrickMap.ComputeAABB()
	// Transform to world
	// Corner transformation...
	// Naive conservative AABB transform

	if minB.X() > maxB.X() {
		obj.WorldAABB = nil
	} else {
		corners := [8]mgl32.Vec3{
			{minB.X(), minB.Y(), minB.Z()},
			{maxB.X(), minB.Y(), minB.Z()},
			{minB.X(), maxB.Y(), minB.Z()},
			{maxB.X(), maxB.Y(), minB.Z()},
			{minB.X(), minB.Y(), maxB.Z()},
			{maxB.X(), minB.Y(), maxB.Z()},
			{minB.X(), maxB.Y(), maxB.Z()},
			{maxB.X(), maxB.Y(), maxB.Z()},
		}

		o2w := obj.Transform.ObjectToWorld()

		inf := float32(1e20)
		wMin := mgl32.Vec3{inf, inf, inf}
		wMax := mgl32.Vec3{-inf, -inf, -inf}

		for _, c := range corners {
			wc := o2w.Mul4x1(c.Vec4(1.0)).Vec3()
			wMin = mgl32.Vec3{min(wMin.X(), wc.X()), min(wMin.Y(), wc.Y()), min(wMin.Z(), wc.Z())}
			wMax = mgl32.Vec3{max(wMax.X(), wc.X()), max(wMax.Y(), wc.Y()), max(wMax.Z(), wc.Z())}
		}

		obj.WorldAABB = &[2]mgl32.Vec3{wMin, wMax}
	}

	obj.Transform.Dirty = false
	return true
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

type Scene struct {
	Objects          []*VoxelObject
	VisibleObjects   []*VoxelObject
	BVHNodesBytes    []byte // Linearized BVH nodes
	Lights           []Light
	TargetVoxelSize  float32
	lastVisibleCount int
}

func NewScene() *Scene {
	return &Scene{
		Objects:         []*VoxelObject{},
		TargetVoxelSize: 0.1, // 10cm default
	}
}

func (s *Scene) RescaleObject(obj *VoxelObject, factor float32) {
	// Enforce voxel size standard
	if obj.XBrickMap == nil {
		return
	}

	minB, _ := obj.XBrickMap.ComputeAABB()

	// Calculate shift to keep object stable in world space
	// Position_new = Position_old + minB * Scale * (1 - factor)
	vSize := s.TargetVoxelSize
	shift := minB.Mul(vSize * (1.0 - factor))
	obj.Transform.Position = obj.Transform.Position.Add(shift)
	obj.Transform.Dirty = true

	fmt.Printf("Rescaling Object. Factor=%f Shift=%v\n", factor, shift)

	newMap := obj.XBrickMap.Resample(factor)
	obj.XBrickMap = newMap

	// Enforce the standard scale
	obj.Transform.Scale = mgl32.Vec3{vSize, vSize, vSize}

	obj.XBrickMap.AABBDirty = true
	obj.UpdateWorldAABB()
}

func (s *Scene) AddObject(obj *VoxelObject) {
	s.Objects = append(s.Objects, obj)
}

func (s *Scene) RemoveObject(obj *VoxelObject) {
	for i, o := range s.Objects {
		if o == obj {
			s.Objects = append(s.Objects[:i], s.Objects[i+1:]...)
			return
		}
	}
}

func (s *Scene) Commit(planes [6]mgl32.Vec4, hizData []float32, hizW, hizH uint32, lastViewProj mgl32.Mat4) {
	// Recompute AABBs
	for _, obj := range s.Objects {
		obj.UpdateWorldAABB()
	}

	// Culling: Populate VisibleObjects
	s.VisibleObjects = s.VisibleObjects[:0] // Clear but keep capacity

	useHiZ := len(hizData) > 0 && hizW > 0 && hizH > 0

	for _, obj := range s.Objects {
		if obj.WorldAABB == nil {
			continue
		}

		// 1. Frustum Culling
		if !AABBInFrustum(*obj.WorldAABB, planes) {
			continue
		}

		// 2. Occlusion Culling (Hi-Z)
		if useHiZ {
			if IsOccluded(*obj.WorldAABB, hizData, hizW, hizH, lastViewProj) {
				continue
			}
		}

		s.VisibleObjects = append(s.VisibleObjects, obj)
	}

	// Build BVH from Visible Objects AABBs
	// We always rebuild if any objects are visible, as identity/order can change even if count stays the same.
	if len(s.VisibleObjects) > 0 {
		aabbs := make([][2]mgl32.Vec3, len(s.VisibleObjects))
		for i, obj := range s.VisibleObjects {
			if obj.WorldAABB != nil {
				aabbs[i] = *obj.WorldAABB
			} else {
				aabbs[i] = [2]mgl32.Vec3{{0, 0, 0}, {0, 0, 0}}
			}
		}

		// Use BVH builder
		builder := &bvh.TLASBuilder{}
		s.BVHNodesBytes = builder.Build(aabbs)
	} else {
		s.BVHNodesBytes = make([]byte, 64) // Empty BVH
	}
}

// IsOccluded checks if the AABB is fully occluded by the Hi-Z buffer.
func IsOccluded(aabb [2]mgl32.Vec3, hizData []float32, w, h uint32, viewProj mgl32.Mat4) bool {
	// 1. Project AABB to Screen Space of PREVIOUS frame
	// We need to find the screen-space bounding box of the AABB.
	minP := mgl32.Vec3{1, 1, 1}
	maxP := mgl32.Vec3{-1, -1, 0}

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

	minZ := float32(1e20) // Nearest depth

	for _, c := range corners {
		// Transform to Clip Space
		clip := viewProj.Mul4x1(c.Vec4(1.0))

		// If behind camera (w <= 0), we can't project correctly.
		// If *any* point is behind camera, the object intersects the near plane.
		// Conservatively, we should assume it's visible.
		if clip.W() <= 0 {
			return false // Intersects near plane -> Visible
		}

		ndc := clip.Vec3().Mul(1.0 / clip.W()) // Perspective divide

		// NDC is -1..1 for X,Y. Z is usually 0..1 (WebGPU) or -1..1 (GL).
		// mgl32 usually produces GL -1..1 Z.
		// We need to match what HiZ stores.
		// HiZ stores G-Buffer Depth (Ray Distance) or Projected Depth?
		// My hiz.wgsl reads 'sourceTexture'.
		// If sourceTexture is G-Buffer Depth (R32F), it stores Linear Depth (t).
		// Wait. hiz.wgsl reads whatever is in binding 0.
		// In App.go, we pass `a.BufferManager.DepthView`.
		// GBuffer Depth Texture contains `hit_res.t` (linear world distance).
		// So HiZ contains Linear Distance.
		// So we must compare against Linear Distance from Camera Center.

		// Calculate Linear Distance for this corner.
		// Camera Position? We don't have it passed here.
		// BUT `clip.W()` IS roughly the linear distance along view axis (Z view).
		// For perspective projection, W_clip = -Z_view.
		// So clip.W() is distance in front of camera.
		// So we can use clip.W() as depth metric?
		// Does GBuffer store clip.W() or Ray distance?
		// gbuffer.wgsl: `hit_res.t` is ray length.
		// Ray length >= Z_view.
		// If we use Z_view (clip.W()) as approx?
		// If object min Z_view > HiZ max t.
		// Since t >= Z_view always (hypotenuse vs leg).
		// If min Z_view > max t. Then min Z_view > max t >= max Z_view_surface.
		// So object is behind.
		// Wait. If object is at Z=100.
		// Occluder is at Z=50. t might be 52.
		// 100 > 52. Correct.
		// So checking clip.W() against HiZ(t) is conservative (safe).
		// Yes.

		// Project to Texture Space 0..1
		u := ndc.X()*0.5 + 0.5
		v := -ndc.Y()*0.5 + 0.5 // Flip Y for Texture coords (usually)

		minP[0] = min(minP[0], u)
		minP[1] = min(minP[1], v)
		maxP[0] = max(maxP[0], u)
		maxP[1] = max(maxP[1], v)

		if clip.W() < minZ {
			minZ = clip.W()
		}
		// Actually init minZ with huge?
		if clip.W() < minZ || minZ == 1.0 { // fix logic
		}
	}
	// Re-init minZ properly
	minZ = 1e20
	// Bounds check
	for _, c := range corners {
		clip := viewProj.Mul4x1(c.Vec4(1.0))
		if clip.W() <= 0 {
			return false
		}
		if clip.W() < minZ {
			minZ = clip.W()
		}
	}

	// Clamp UV to 0..1
	minP[0] = max(minP[0], 0)
	minP[1] = max(minP[1], 0)
	maxP[0] = min(maxP[0], 1)
	maxP[1] = min(maxP[1], 1)

	if minP[0] >= maxP[0] || minP[1] >= maxP[1] {
		// Off screen? If entirely off screen in U,V
		// Frustum culling handles this (for current frame).
		// But this is LAST frame. Object might have moved on screen.
		// If it was off-screen last frame, we have no depth info.
		// Conservative: Visible.
		return false
	}

	// Convert to HiZ pixel coords
	startX := uint32(minP[0] * float32(w))
	startY := uint32(minP[1] * float32(h))
	endX := uint32(maxP[0] * float32(w))
	endY := uint32(maxP[1] * float32(h))

	// Clamp to texture bounds
	if endX >= w {
		endX = w - 1
	}
	if endY >= h {
		endY = h - 1
	}
	if startX >= w {
		startX = w - 1
	} // Should not happen if clamped U
	if startY >= h {
		startY = h - 1
	}
	if startX > endX || startY > endY {
		return false
	}

	// Sample HiZ
	// We want the MAX depth in this region.
	// Since we are traversing a specific mip level manually on CPU...
	// We iterate the pixels.
	// Optimize: if region is large, we should have selected a coarser mip.
	// But we only read back ONE mip (e.g. 64x36).
	// So we just iterate. 64x36 is small enough.

	maxOccluderDepth := float32(0.0)

	for y := startY; y <= endY; y++ {
		rowOffset := y * w
		for x := startX; x <= endX; x++ {
			d := hizData[rowOffset+x]
			if d > maxOccluderDepth {
				maxOccluderDepth = d
			}
		}
	}

	// If the object's NEAREST point (minZ) is FURTHER than the occluder's FURTHEST point (maxOccluderDepth),
	// then the object is completely hidden.
	// Check:
	// Occluder (Wall) at distance 10. HiZ max = 10.
	// Object (Enemy) at distance 20. minZ = 20.
	// 20 > 10. Occluded? YES.

	// Check 2:
	// Occluder (Wall) with hole. Wall is at 10. Hole is empty (depth 60000).
	// HiZ max = 60000 (because MAX reduction).
	// Object at 20.
	// 20 > 60000? NO. Visible.
	// Correct.

	// Tolerance?
	if minZ > maxOccluderDepth {
		return true
	}

	return false
}

// AABBInFrustum checks if an AABB is visible within the frustum defined by 6 planes.
func AABBInFrustum(aabb [2]mgl32.Vec3, planes [6]mgl32.Vec4) bool {
	for i := 0; i < 6; i++ {
		plane := planes[i]
		// Find negative vertex (furthest in direction of normal)
		// If negative vertex is outside (dist < 0), then box is fully outside.

		// Actually, convention:
		// Normal points INSIDE.
		// We want to check if the box is fully BEHIND the plane (outside).
		// That corresponds to finding the point with HIGHEST signed distance (most inside).
		// If that point is still < 0, then ALL points are < 0.

		var p mgl32.Vec3
		if plane[0] > 0 {
			p[0] = aabb[1][0] // Max
		} else {
			p[0] = aabb[0][0] // Min
		}
		if plane[1] > 0 {
			p[1] = aabb[1][1] // Max
		} else {
			p[1] = aabb[0][1] // Min
		}
		if plane[2] > 0 {
			p[2] = aabb[1][2] // Max
		} else {
			p[2] = aabb[0][2] // Min
		}

		dist := plane[0]*p[0] + plane[1]*p[1] + plane[2]*p[2] + plane[3]
		if dist < 0 {
			return false
		}
	}
	return true
}
