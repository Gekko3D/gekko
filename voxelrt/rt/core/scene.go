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
	Objects       []*VoxelObject
	BVHNodesBytes []byte // Linearized BVH nodes
	Lights        []Light
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

func (s *Scene) Commit() {
	// Recompute AABBs
	anyChanged := false
	for _, obj := range s.Objects {
		if obj.UpdateWorldAABB() {
			anyChanged = true
		}
	}

	if !anyChanged && len(s.BVHNodesBytes) > 0 {
		return
	}

	// Build BVH from object AABBs
	if len(s.Objects) > 0 {
		aabbs := make([][2]mgl32.Vec3, len(s.Objects))
		for i, obj := range s.Objects {
			if obj.WorldAABB != nil {
				aabbs[i] = *obj.WorldAABB
			} else {
				// Empty AABB if not set
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
