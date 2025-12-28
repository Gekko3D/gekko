package core

import (
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
	Objects        []*VoxelObject
	VisibleObjects []*VoxelObject
	BVHNodesBytes  []byte // Linearized BVH nodes
	Lights         []Light
	// Builder TODO
}

func NewScene() *Scene {
	return &Scene{
		Objects: []*VoxelObject{},
	}
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

func (s *Scene) Commit(planes [6]mgl32.Vec4) {
	// Recompute AABBs
	anyChanged := false
	for _, obj := range s.Objects {
		if obj.UpdateWorldAABB() {
			anyChanged = true
		}
	}

	// Culling: Populate VisibleObjects
	s.VisibleObjects = s.VisibleObjects[:0] // Clear but keep capacity
	for _, obj := range s.Objects {
		if obj.WorldAABB != nil && AABBInFrustum(*obj.WorldAABB, planes) {
			s.VisibleObjects = append(s.VisibleObjects, obj)
		}
	}

	if !anyChanged && len(s.BVHNodesBytes) > 0 {
		return
	}

	// Build BVH from Visible Objects AABBs
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

// AABBInFrustum checks if an AABB is visible within the frustum defined by 6 planes.
// Planes are expected to be in Ax+By+Cz+D=0 form, with the normal pointing INSIDE.
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
