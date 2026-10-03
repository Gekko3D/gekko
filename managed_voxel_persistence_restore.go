package gekko

import (
	"path/filepath"
	"slices"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Restore only the current selected delta on the exact owned unmanaged snapshot.
// All proof happens before replacing its lifetime lease. Failed proof preserves
// ordinary editable geometry without binding a fresh construction to old history.
func restoreManagedVoxelPersistenceOwner(cmd *Commands, assets *AssetServer, eid EntityId, vmc VoxelModelComponent, source *volume.XBrickMap) (*volume.ManagedXBrickMap, authoredVoxelBase, *managedVoxelPersistenceBinding) {
	fail := func() (*volume.ManagedXBrickMap, authoredVoxelBase, *managedVoxelPersistenceBinding) {
		return nil, authoredVoxelBase{}, nil
	}
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	if state == nil || assets.managedVoxelEntry(vmc.GeometryAsset()) != nil {
		return fail()
	}
	lease := state.snapshotGeometryAssets[eid]
	if lease.ID != vmc.GeometryAsset() || lease.Server != assets {
		return fail()
	}
	ref, found := managedVoxelItemRef(cmd, eid)
	if !found || ref.LevelID != state.LevelID {
		return fail()
	}
	coord, owned := managedVoxelStreamMembership(state, eid, ref.PlacementID, ref.ItemID)
	if !owned {
		return fail()
	}
	path, selected := managedVoxelSelectedAssetPath(state, coord, ref.PlacementID)
	if !selected || path != managedVoxelResolvedPath(ref.AssetPath, state.LevelPath) {
		return fail()
	}
	if context := state.managedVoxelCommit; context != nil && context.coord == coord {
		if context.placement.PlacementID != ref.PlacementID || managedVoxelResolvedPath(context.placement.AssetPath, state.LevelPath) != path {
			return fail()
		}
	}
	key := voxelObjectRuntimeKey(ref.PlacementID, ref.ItemID)
	override, exists := state.voxelOverrideMap[key]
	if !exists || override.PlacementID != ref.PlacementID || override.ItemID != ref.ItemID {
		return fail()
	}
	payload, _, err := content.LoadVoxelObjectPayload(content.ResolveDocumentPath(override.SnapshotPath, state.WorldDeltaPath), nil)
	if err != nil || !(payload.SchemaVersion == content.CurrentVoxelObjectPayloadSchemaVersion && payload.Mode == content.VoxelObjectPayloadBaseDelta || payload.SchemaVersion == content.HybridVoxelObjectPayloadSchemaVersion && payload.Mode == content.VoxelObjectPayloadHybridDelta) {
		return fail()
	}
	def, err := state.Loader.LoadAsset(path)
	if err != nil || def.ID != ref.AssetID || def.Runtime != nil && def.Runtime.CollapseVoxelParts {
		return fail()
	}
	for _, part := range def.Parts {
		if part.ID != ref.ItemID {
			continue
		}
		if part.Source.Kind != content.AssetSourceKindVoxelShape || part.Source.VoxelShape == nil {
			return fail()
		}
		lattice := authoredVoxelShapeLattice(part.VoxelResolution)
		if lattice.VoxelResolution != VoxelResolutionOrDefault(&vmc) {
			return fail()
		}
		canonical := buildAuthoredVoxelShapeMap(part)
		base := VoxelObjectSnapshotFromXBrickMap(canonical)
		identity, decodedBytes, err := content.VoxelObjectBaseIdentity(base, lattice, nil)
		if err != nil {
			return fail()
		}
		resolved, err := content.ResolveVoxelObjectPayload(payload, base, lattice, ref.PlacementID, ref.ItemID, nil)
		if err != nil {
			return fail()
		}
		isolated := source.Copy()
		current := VoxelObjectSnapshotFromXBrickMap(isolated)
		slices.SortFunc(current.Voxels, func(a, b content.VoxelObjectVoxelDef) int {
			for _, pair := range [3][2]int{{a.Z, b.Z}, {a.Y, b.Y}, {a.X, b.X}} {
				if pair[0] < pair[1] {
					return -1
				}
				if pair[0] > pair[1] {
					return 1
				}
			}
			return 0
		})
		if !slices.Equal(current.Voxels, resolved.Voxels) {
			return fail()
		}
		binding := &managedVoxelPersistenceBinding{state: state, generation: state.Generation, levelID: state.LevelID, levelPath: filepath.Clean(state.LevelPath), ref: ref, assetPath: path, baseBricks: persistenceMapBrickCount(canonical), baseVoxels: len(base.Voxels), baseDecodedBytes: decodedBytes}
		return volume.NewManagedXBrickMapWithBase(canonical, isolated), authoredVoxelBase{identity: identity, lattice: lattice}, binding
	}
	return fail()
}
