package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Qualification is live: pending commands may already invalidate a renderer
// attachment before the next synchronous bridge publication.
func managedVoxelInputQualified(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId, entry *managedVoxelGeometry) bool {
	if cmd == nil || cmd.app == nil || assets == nil || entry == nil || entry.exposed || entry.owner == nil || entry.producerActive {
		return false
	}
	vmc, err := validateManagedVoxelEntity(cmd, assets, eid)
	if err != nil || vmc.GeometryAsset() != id || assets.managedVoxelEntry(id) != entry {
		return false
	}
	if entry.app != nil && (entry.app != cmd.app || entry.entity != eid || vmc.OverrideGeometry != id) {
		return false
	}
	if vmc.VoxelAdjacencyGroupID != 0 || vmc.TerrainGroupID != 0 || vmc.PlanetTileGroupID != 0 {
		return false
	}
	for _, comp := range voxelEditComponents(cmd, eid) {
		switch transform := comp.(type) {
		case TransformComponent:
			return true
		case *TransformComponent:
			if transform != nil {
				return true
			}
		}
	}
	return false
}

// The provider belongs to the renderer attachment, never to a captured input.
// Unchanged attachments reuse it without capturing or replacing source identity.
func (state *VoxelRtState) attachManagedVoxelInput(eid EntityId, obj *core.VoxelObject, binding managedVoxelBinding) managedVoxelBinding {
	if binding.producerAttached {
		return binding
	}
	if !managedVoxelInputQualified(state.managedVoxelCommands, state.managedVoxelAssets, eid, binding.id, binding.entry) {
		obj.SetManagedGeometryProducer(nil, nil)
		binding.producerAttached = false
		return binding
	}
	id, entry, derivative := binding.id, binding.entry, binding.derivative
	qualified := func() bool {
		current, ok := state.managedVoxelBindings[eid]
		return ok && state.GetVoxelObject(eid) == obj && current.id == id && current.entry == entry && current.derivative == derivative && obj.XBrickMap == derivative && !current.exposed && current.generation == entry.generation && managedVoxelInputQualified(state.managedVoxelCommands, state.managedVoxelAssets, eid, id, entry)
	}
	obj.SetManagedGeometryProducerWithSectorReader(derivative, func() (volume.ManagedGeometryView, uint64, bool) {
		if !qualified() {
			return volume.ManagedGeometryView{}, 0, false
		}
		view, ok := entry.owner.CaptureGeometry()
		if !ok {
			return volume.ManagedGeometryView{}, 0, false
		}
		return view, entry.generation, true
	}, func(coord [3]int) (volume.ManagedSectorView, uint64, bool) {
		if !qualified() {
			return volume.ManagedSectorView{}, 0, false
		}
		view, ok := entry.owner.CaptureSector(coord)
		if !ok {
			return volume.ManagedSectorView{}, 0, false
		}
		return view, entry.generation, true
	})
	binding.producerAttached = true
	return binding
}
