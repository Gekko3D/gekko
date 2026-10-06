package gekko

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

// Notification is synchronous engine-thread work after completed publication.
// It uses the manager's accepted identity without capturing or advancing inputs.
func notifyManagedVoxelGeometryContent(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId, entry *managedVoxelGeometry, writes []volume.VoxelWrite) {
	if len(writes) == 0 || cmd == nil || cmd.app == nil {
		return
	}
	state := voxelRtStateFromApp(cmd.app)
	if state == nil || state.RtApp == nil || state.RtApp.BufferManager == nil {
		return
	}
	manager := state.RtApp.BufferManager
	if !manager.ManagedGeometryAdmissionBudget().Enabled {
		return
	}
	obj := state.GetVoxelObject(eid)
	status, admitted := manager.ManagedGeometryContentStatus(obj)
	if !admitted {
		return
	}
	binding, bound := state.managedVoxelBindings[eid]
	if !bound || binding.id != id || binding.entry != entry || entry == nil || binding.exposed || !binding.producerAttached || binding.derivative == nil || obj == nil || obj.XBrickMap != binding.derivative || binding.generation != entry.generation || entry.generation < status.Input.Generation() {
		return
	}
	if state.managedVoxelCommands == nil || state.managedVoxelCommands.app != cmd.app || state.managedVoxelAssets != assets || !managedVoxelInputQualified(state.managedVoxelCommands, state.managedVoxelAssets, eid, id, entry) || !obj.MatchesManagedGeometrySource(status.Input) {
		return
	}
	for _, write := range writes {
		volume.VisitVoxelNormalHaloSectors(write.X, write.Y, write.Z, func(coord [3]int) bool {
			manager.QueueManagedGeometryContent(obj, status.Input, coord)
			return true
		})
	}
}
