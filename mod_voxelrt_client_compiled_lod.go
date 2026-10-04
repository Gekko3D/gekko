package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Captures only an upload target, never a proof, asset lease or ECS pointer.
type compiledAssetLODReadyTarget struct {
	mapRef     *volume.XBrickMap
	id         uint32
	revision   uint64
	generation uint64
}

func compiledAssetLODTarget(target *volume.XBrickMap, generation uint64) compiledAssetLODReadyTarget {
	if target == nil {
		return compiledAssetLODReadyTarget{}
	}
	return compiledAssetLODReadyTarget{target, target.ID, target.Revision, generation}
}

func (target compiledAssetLODReadyTarget) matches(current *volume.XBrickMap, generation uint64) bool {
	return current != nil && target.mapRef == current && target.id == current.ID &&
		target.revision == current.Revision && target.generation == generation
}

type compiledAssetLODDisplay struct {
	object          *core.VoxelObject
	full            *volume.XBrickMap
	fullID          uint32
	fullInitialized bool // Only continuously active full display, not history.
	coarseReady     compiledAssetLODReadyTarget
	pendingReady    compiledAssetLODReadyTarget
}

func (s *VoxelRtState) clearCompiledAssetLOD(entity EntityId) {
	if record := s.compiledLODDisplays[entity]; record != nil {
		record.object.ClearRenderRepresentation()
		delete(s.compiledLODDisplays, entity)
	}
}

// Called before Update, after current material, transform and lattice tags.
// The result is local visibility wait, independent of the parent's hidden state.
func (s *VoxelRtState) syncCompiledAssetLOD(entity EntityId, object *core.VoxelObject, candidate qualifiedCompiledAssetLOD, qualified, wantCoarse bool) bool {
	if s == nil || object == nil || object.XBrickMap == nil {
		return false
	}
	record := s.compiledLODDisplays[entity]
	wasOwned := record != nil
	if record != nil && (record.object != object || record.full != object.XBrickMap || record.fullID != object.XBrickMap.ID) {
		s.clearCompiledAssetLOD(entity)
		record = nil
	}
	if record == nil {
		if !qualified && !wasOwned {
			return false
		}
		record = &compiledAssetLODDisplay{object: object, full: object.XBrickMap, fullID: object.XBrickMap.ID}
		if s.compiledLODDisplays == nil {
			s.compiledLODDisplays = make(map[EntityId]*compiledAssetLODDisplay)
		}
		s.compiledLODDisplays[entity] = record
	}
	if record.fullInitialized && object.RenderVoxelMap() != object.XBrickMap {
		record.fullInitialized = false
	}
	if !qualified {
		object.ClearRenderRepresentation()
		record.coarseReady, record.pendingReady = compiledAssetLODReadyTarget{}, compiledAssetLODReadyTarget{}
		return !record.fullInitialized
	}
	if record.fullInitialized && !wantCoarse {
		return false
	}
	if object.RenderVoxelMap() != candidate.coarse || !object.RenderRepresentationValid() {
		record.pendingReady = compiledAssetLODReadyTarget{}
		if !record.coarseReady.matches(candidate.coarse, 0) {
			record.coarseReady = compiledAssetLODReadyTarget{}
		}
		if !object.SetRenderLOD2(candidate.coarse) {
			record.coarseReady = compiledAssetLODReadyTarget{}
			return !record.fullInitialized // Setter restores full, preserving active readiness.
		}
		record.fullInitialized = false
	}
	coarseReady := record.coarseReady.matches(candidate.coarse, 0)
	if coarseReady {
		coarseReady, _, _ = s.RtApp.BufferManager.RenderVoxelObjectReady(object, candidate.coarse, candidate.coarse.Revision)
	}
	if wantCoarse {
		object.ClearPendingFullUpload()
		record.pendingReady = compiledAssetLODReadyTarget{}
	} else if coarseReady && object.SetPendingFullUpload() {
		generation := object.PendingFullUploadGeneration()
		if record.pendingReady.matches(object.XBrickMap, generation) {
			// Recheck the already-published target, including current material
			// allocation identity. A newly staged request has no prior stamp.
			ready, _, _ := s.RtApp.BufferManager.PendingFullVoxelObjectReady(object, object.XBrickMap, object.XBrickMap.Revision)
			if ready {
				object.ClearRenderRepresentation()
				record.fullInitialized = true
				record.pendingReady = compiledAssetLODReadyTarget{}
				return false
			}
		}
	}
	return !coarseReady
}

// Observe queued readiness after Update. Display changes only in the next sync.
func (s *VoxelRtState) refreshCompiledAssetLODStatuses() {
	if s == nil || s.RtApp == nil {
		return
	}
	for entity, record := range s.compiledLODDisplays {
		object := s.instanceMap[entity]
		if object != record.object || object == nil || object.XBrickMap != record.full || object.XBrickMap.ID != record.fullID {
			continue
		}
		selected := object.RenderVoxelMap()
		if selected == object.XBrickMap {
			if ready, _, _ := s.RtApp.BufferManager.VoxelObjectReady(object, selected, selected.Revision); ready {
				record.fullInitialized = true
			}
			continue
		}
		record.coarseReady, record.pendingReady = compiledAssetLODReadyTarget{}, compiledAssetLODReadyTarget{}
		if selected != nil {
			if ready, _, _ := s.RtApp.BufferManager.RenderVoxelObjectReady(object, selected, selected.Revision); ready {
				record.coarseReady = compiledAssetLODTarget(selected, 0)
			}
		}
		if pending := object.PendingFullUploadMap(); pending != nil {
			if ready, _, _ := s.RtApp.BufferManager.PendingFullVoxelObjectReady(object, pending, pending.Revision); ready {
				record.pendingReady = compiledAssetLODTarget(pending, object.PendingFullUploadGeneration())
			}
		}
	}
}
