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

// Manager private targets are reachable for upload service without being selected
// by object records. Retiring maps remain owned but never receive new work.
func (m *GpuBufferManager) voxelServiceTargets(scene *core.Scene) func(func(int, voxelServiceTarget) bool) {
	return func(yield func(int, voxelServiceTarget) bool) {
		for i, t := range voxelServiceTargets(scene) {
			if !yield(i, t) {
				return
			}
		}
		if scene == nil {
			return
		}
		for i, obj := range scene.Objects {
			o := m.managedGPUOwners[obj]
			if o == nil {
				continue
			}
			if o.handoff {
				if obj.XBrickMap != nil && obj.RenderVoxelMap() != obj.XBrickMap {
					if !yield(i, voxelServiceTarget{object: obj, mapRef: obj.XBrickMap}) {
						return
					}
				}
				continue
			}
			if m.managedFrameBudget.Enabled && m.managedFrameBudget.MaxEntries == 0 {
				continue
			}
			for _, t := range []*managedGPUTarget{o.candidate, o.stage} {
				if t != nil && t.complete && !t.retiring && !t.syncPending {
					if !yield(i, voxelServiceTarget{object: obj, mapRef: t.mapRef}) {
						return
					}
				}
			}
		}
	}
}
func (m *GpuBufferManager) qualifyManagedWork(w voxelUploadWork) voxelUploadWork {
	w.managedManager = m
	if t := m.managedGPUMaps[w.targetMap()]; t != nil {
		w.managedManager = m
		w.managedTarget = t
	}
	return w
}
