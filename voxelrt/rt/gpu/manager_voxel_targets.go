package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// These descriptors live only for preparation. Geometry ownership remains in
// Allocations; display records and adjacency continue to use selected maps.
type voxelServiceTarget struct {
	object            *core.VoxelObject
	mapRef            *volume.XBrickMap
	pendingGeneration uint64
}

// The iterator yields directly from resident objects without a scene-sized
// temporary allocation or a second residency cache. Index is the scene index.
func voxelServiceTargets(scene *core.Scene) func(func(int, voxelServiceTarget) bool) {
	return func(yield func(int, voxelServiceTarget) bool) {
		if scene == nil {
			return
		}
		for index, obj := range scene.Objects {
			if selected := obj.RenderVoxelMap(); selected != nil {
				if !yield(index, voxelServiceTarget{object: obj, mapRef: selected}) {
					return
				}
			}
			if pending := obj.PendingFullUploadMap(); pending != nil {
				if !yield(index, voxelServiceTarget{object: obj, mapRef: pending, pendingGeneration: obj.PendingFullUploadGeneration()}) {
					return
				}
			}
		}
	}
}
