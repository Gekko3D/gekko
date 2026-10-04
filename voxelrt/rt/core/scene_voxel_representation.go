package core

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Maps are borrowed semantically read-only. Revision guards detect tracked
// edits, not raw aliases, material changes, C1 provenance or GPU readiness.
// The renderer bridge must establish those qualifications before selection.
type voxelRenderRepresentation struct {
	source, coarse                 *volume.XBrickMap
	sourceRevision, coarseRevision uint64
	bounds                         [2]mgl32.Vec3
	boundsMatrix                   mgl32.Mat4
	boundsMin, boundsMax           mgl32.Vec3
	boundsValid                    bool
}

// SetRenderLOD2 selects a zero-anchored 2x display map without changing the
// authoritative map or transform. Rejected selection clears any previous one.
func (obj *VoxelObject) SetRenderLOD2(coarse *volume.XBrickMap) bool {
	if obj == nil {
		return false
	}
	obj.ClearRenderRepresentation()
	if obj.Transform == nil || obj.hasSpecialRenderLattice() || obj.XBrickMap == nil || coarse == nil || coarse == obj.XBrickMap || obj.XBrickMap.GetVoxelCount() == 0 || coarse.GetVoxelCount() == 0 {
		return false
	}
	obj.renderRepresentation = &voxelRenderRepresentation{
		source: obj.XBrickMap, coarse: coarse,
		sourceRevision: obj.XBrickMap.Revision, coarseRevision: coarse.Revision,
	}
	return true
}

// ClearRenderRepresentation explicitly restores the authoritative render path.
func (obj *VoxelObject) ClearRenderRepresentation() {
	if obj != nil {
		obj.renderRepresentation = nil
	}
}

// RenderRepresentationValid reports a selected representation with unchanged
// tracked geometry. An invalid selection stays selected until explicitly cleared.
func (obj *VoxelObject) RenderRepresentationValid() bool {
	if obj == nil || obj.Transform == nil || obj.hasSpecialRenderLattice() || obj.renderRepresentation == nil {
		return false
	}
	r := obj.renderRepresentation
	return obj.XBrickMap == r.source && r.source.Revision == r.sourceRevision && r.coarse.Revision == r.coarseRevision
}

func (obj *VoxelObject) hasSpecialRenderLattice() bool {
	return obj.IsTerrainChunk || obj.IsPlanetTile || obj.VoxelAdjacencyGroupID != 0 || obj.TerrainGroupID != 0 || obj.PlanetTileGroupID != 0
}

// Scene owns a value snapshot because derived bounds storage is updated in place.
type sceneRenderBoundsSnapshot struct {
	present bool
	bounds  [2][3]uint32
}

func (s *Scene) updateRenderBoundsSnapshot(obj *VoxelObject) bool {
	if s.lastRenderBounds == nil {
		s.lastRenderBounds = make(map[*VoxelObject]sceneRenderBoundsSnapshot)
	}
	var current sceneRenderBoundsSnapshot
	if bounds := obj.RenderWorldBounds(); obj.Transform != nil && obj.RenderVoxelMap() != nil && bounds != nil {
		current.present = true
		for end := range bounds {
			for axis, value := range bounds[end] {
				current.bounds[end][axis] = math.Float32bits(value)
			}
		}
	}
	previous, exists := s.lastRenderBounds[obj]
	s.lastRenderBounds[obj] = current
	return !exists || previous != current
}

// RenderVoxelMap fails closed for invalid active selections; it never implicitly
// falls back to full geometry that the upload owner may not have made ready.
func (obj *VoxelObject) RenderVoxelMap() *volume.XBrickMap {
	if obj == nil {
		return nil
	}
	if obj.renderRepresentation == nil {
		return obj.XBrickMap
	}
	if !obj.RenderRepresentationValid() {
		return nil
	}
	return obj.renderRepresentation.coarse
}

func (obj *VoxelObject) RenderObjectToWorld() mgl32.Mat4 {
	if obj == nil || obj.Transform == nil {
		return mgl32.Mat4{}
	}
	if obj.renderRepresentation == nil {
		return obj.Transform.ObjectToWorld()
	}
	if !obj.RenderRepresentationValid() {
		return mgl32.Mat4{}
	}
	return obj.Transform.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2))
}

func (obj *VoxelObject) RenderWorldToObject() mgl32.Mat4 {
	if obj == nil || obj.Transform == nil {
		return mgl32.Mat4{}
	}
	if obj.renderRepresentation == nil {
		return obj.Transform.WorldToObject()
	}
	if !obj.RenderRepresentationValid() {
		return mgl32.Mat4{}
	}
	return mgl32.Scale3D(.5, .5, .5).Mul4(obj.Transform.WorldToObject())
}

// RenderWorldBounds retains the exact legacy pointer with no selection. Derived
// bounds use occupied coarse extents and current matrices, independently of the
// authoritative AABB and its caller-managed dirty flags.
func (obj *VoxelObject) RenderWorldBounds() *[2]mgl32.Vec3 {
	if obj == nil {
		return nil
	}
	if obj.renderRepresentation == nil {
		return obj.WorldAABB
	}
	if !obj.RenderRepresentationValid() {
		return nil
	}
	r := obj.renderRepresentation
	matrix := obj.RenderObjectToWorld()
	if !r.boundsValid || r.coarse.AABBDirty || matrix != r.boundsMatrix {
		minimum, maximum := r.coarse.ComputeAABB()
		if !r.boundsValid || matrix != r.boundsMatrix || minimum != r.boundsMin || maximum != r.boundsMax {
			r.bounds = [2]mgl32.Vec3{{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}, {-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}}
			for corner := 0; corner < 8; corner++ {
				var p mgl32.Vec3
				for axis := 0; axis < 3; axis++ {
					p[axis] = minimum[axis]
					if corner&(1<<axis) != 0 {
						p[axis] = maximum[axis]
					}
				}
				world := matrix.Mul4x1(p.Vec4(1)).Vec3()
				for axis := 0; axis < 3; axis++ {
					r.bounds[0][axis] = min(r.bounds[0][axis], world[axis])
					r.bounds[1][axis] = max(r.bounds[1][axis], world[axis])
				}
			}
			r.boundsMatrix, r.boundsMin, r.boundsMax, r.boundsValid = matrix, minimum, maximum, true
		}
	}
	return &r.bounds
}
