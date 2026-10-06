package core

import (
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Display storage is borrowed from the managed GPU owner. Its publication bounds
// are explicit, so neither mutable authority nor target edits change coverage.
type managedRenderGeometry struct {
	source           *managedGeometrySource
	generation       uint64
	target           *volume.XBrickMap
	minimum, maximum mgl32.Vec3
	bounds           [2]mgl32.Vec3
	boundsMatrix     mgl32.Mat4
	boundsValid      bool
}

// SetManagedRenderGeometry publishes an ordinary managed display selection.
// A nil target installs initial unready geometry. Calls require engine-thread
// access; target and local conservative bounds belong to the publication owner.
// Failed qualification preserves the existing selection.
func (obj *VoxelObject) SetManagedRenderGeometry(expected ManagedGeometryInput, target *volume.XBrickMap, minimum, maximum mgl32.Vec3) bool {
	if obj == nil || obj.Transform == nil || !obj.MatchesManagedGeometrySource(expected) {
		return false
	}
	for axis := 0; axis < 3; axis++ {
		if math.IsNaN(float64(minimum[axis])) || math.IsInf(float64(minimum[axis]), 0) || math.IsNaN(float64(maximum[axis])) || math.IsInf(float64(maximum[axis]), 0) || minimum[axis] > maximum[axis] {
			return false
		}
	}
	obj.managedRenderGeometry = &managedRenderGeometry{
		source: expected.source, generation: expected.generation, target: target,
		minimum: minimum, maximum: maximum,
	}
	return true
}

// ClearManagedRenderGeometry clears only the stored publication identity. It
// also works after its producer detaches; an older publication cannot clear a
// newer generation from the same source.
func (obj *VoxelObject) ClearManagedRenderGeometry(expected ManagedGeometryInput) bool {
	if obj == nil || obj.managedRenderGeometry == nil || expected.source == nil {
		return false
	}
	r := obj.managedRenderGeometry
	if r.source != expected.source || r.generation != expected.generation {
		return false
	}
	obj.managedRenderGeometry = nil
	return true
}

func (obj *VoxelObject) managedRenderGeometryValid() bool {
	return obj.Transform != nil && obj.renderRepresentation == nil && !obj.hasSpecialRenderLattice()
}

// RenderLocalBounds returns shader coverage in the selected render grid.
// Managed publications retain explicit conservative bounds independently of
// the occupied bounds cached on their normal-baking storage.
func (obj *VoxelObject) RenderLocalBounds() (mgl32.Vec3, mgl32.Vec3) {
	selected := obj.RenderVoxelMap()
	if selected == nil {
		return mgl32.Vec3{}, mgl32.Vec3{}
	}
	if obj.managedRenderGeometry != nil {
		return obj.managedRenderGeometry.minimum, obj.managedRenderGeometry.maximum
	}
	return selected.ComputeAABB()
}

func (obj *VoxelObject) managedRenderWorldBounds() *[2]mgl32.Vec3 {
	r := obj.managedRenderGeometry
	if r.target == nil || !obj.managedRenderGeometryValid() {
		return nil
	}
	matrix := obj.Transform.ObjectToWorld()
	if !r.boundsValid || matrix != r.boundsMatrix {
		r.bounds = [2]mgl32.Vec3{{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}, {-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}}
		for corner := 0; corner < 8; corner++ {
			p := r.minimum
			for axis := 0; axis < 3; axis++ {
				if corner&(1<<axis) != 0 {
					p[axis] = r.maximum[axis]
				}
			}
			world := matrix.Mul4x1(p.Vec4(1)).Vec3()
			for axis := 0; axis < 3; axis++ {
				r.bounds[0][axis] = min(r.bounds[0][axis], world[axis])
				r.bounds[1][axis] = max(r.bounds[1][axis], world[axis])
			}
		}
		r.boundsMatrix, r.boundsValid = matrix, true
	}
	return &r.bounds
}
