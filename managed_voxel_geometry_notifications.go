package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Notification is synchronous engine-thread work after completed publication.
// CPU accepted and GPU current/staging identities qualify independently without
// capturing or advancing inputs.
func notifyManagedVoxelGeometryContent(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId, entry *managedVoxelGeometry, writes []volume.VoxelWrite, previous uint64) {
	if len(writes) == 0 || cmd == nil || cmd.app == nil {
		return
	}
	state := voxelRtStateFromApp(cmd.app)
	if state == nil || state.RtApp == nil || state.RtApp.BufferManager == nil {
		return
	}
	manager := state.RtApp.BufferManager
	obj := state.GetVoxelObject(eid)
	binding, bound := state.managedVoxelBindings[eid]
	if !bound || binding.id != id || binding.entry != entry || entry == nil || binding.exposed || !binding.producerAttached || binding.derivative == nil || obj == nil || obj.XBrickMap != binding.derivative || binding.generation != entry.generation {
		return
	}
	if state.managedVoxelCommands == nil || state.managedVoxelCommands.app != cmd.app || state.managedVoxelAssets != assets || !managedVoxelInputQualified(state.managedVoxelCommands, state.managedVoxelAssets, eid, id, entry) {
		return
	}
	qualified := func(input core.ManagedGeometryInput) bool {
		return entry.generation >= input.Generation() && obj.MatchesManagedGeometrySource(input)
	}
	if status, admitted := manager.ManagedGeometryContentStatus(obj); manager.ManagedGeometryAdmissionBudget().Enabled && admitted && qualified(status.Input) {
		for _, write := range writes {
			volume.VisitVoxelNormalHaloSectors(write.X, write.Y, write.Z, func(coord [3]int) bool {
				manager.QueueManagedGeometryContent(obj, status.Input, coord)
				return true
			})
		}
		manager.RecordManagedGeometryContentPublication(obj, status.Input, previous, entry.generation)
	}
	if status, owned := manager.ManagedGeometryGPUStatus(obj); owned && (qualified(status.CurrentInput) || qualified(status.StageInput)) {
		manager.NotifyManagedGeometryGPUContent(obj, previous, entry.generation, writes)
	}
}
