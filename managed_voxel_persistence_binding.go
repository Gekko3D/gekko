package gekko

import (
	"path/filepath"

	"github.com/gekko3d/gekko/content"
)

// A value-only binding captures the authored owner at enablement. It neither
// retains base geometry nor turns mutable runtime references into provenance.
type managedVoxelPersistenceBinding struct {
	state                  *StreamedLevelRuntimeState
	generation             uint64
	levelID, levelPath     string
	ref                    AuthoredLevelItemRefComponent
	assetPath              string
	baseBricks, baseVoxels int
	baseDecodedBytes       int64
}

// This context only bridges unpublished synchronous placement hooks. Each
// advance invocation restores the previous context, including panic unwinding.
type managedVoxelCommitContext struct {
	chunk      *streamedLoadedChunk
	coord      ChunkCoord
	generation uint64
	placement  streamedPlacementInstance
}

func managedVoxelItemRef(cmd *Commands, eid EntityId) (AuthoredLevelItemRefComponent, bool) {
	var ref AuthoredLevelItemRefComponent
	found := false
	for _, component := range voxelEditComponents(cmd, eid) {
		switch value := component.(type) {
		case AuthoredLevelItemRefComponent:
			ref, found = value, true
		case *AuthoredLevelItemRefComponent:
			if value != nil {
				ref, found = *value, true
			}
		}
	}
	// Tags are descriptive and have no role in the persisted owner tuple.
	ref.Tags = nil
	return ref, found
}

func managedVoxelResolvedPath(path, levelPath string) string {
	return filepath.Clean(content.ResolveDocumentPath(path, levelPath))
}

func managedVoxelStreamMembership(state *StreamedLevelRuntimeState, eid EntityId, placementID, itemID string) (ChunkCoord, bool) {
	if state == nil || !state.Initialized {
		return ChunkCoord{}, false
	}
	key := voxelObjectRuntimeKey(placementID, itemID)
	coord, exists := state.ObjectChunk[key]
	if !exists {
		return ChunkCoord{}, false
	}
	chunk := streamedLoadedOrActiveChunk(state, coord)
	if context := state.managedVoxelCommit; context != nil && context.coord == coord && context.generation == state.Generation {
		chunk = context.chunk
	}
	if chunk == nil || chunk.ObjectEntities[key] != eid {
		return coord, false
	}
	_, owned := chunk.OwnedEntities[eid]
	return coord, owned
}

func managedVoxelSelectedAssetPath(state *StreamedLevelRuntimeState, coord ChunkCoord, placementID string) (string, bool) {
	for _, placement := range state.PlacementsByChunk[coord] {
		if placement.PlacementID == placementID {
			return managedVoxelResolvedPath(placement.AssetPath, state.LevelPath), true
		}
	}
	return "", false
}

// Lifetime ownership comes from existing chunk maps, independently of mutable
// authored references. This metadata search runs only at enablement.
func leaseManagedVoxelOverride(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId) bool {
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	if state == nil || !state.Initialized {
		return false
	}
	ownedBy := func(coord ChunkCoord, chunk *streamedLoadedChunk) bool {
		if chunk == nil {
			return false
		}
		if _, owned := chunk.OwnedEntities[eid]; !owned {
			return false
		}
		for key, entity := range chunk.ObjectEntities {
			if mapped, exists := state.ObjectChunk[key]; entity == eid && exists && mapped == coord {
				return true
			}
		}
		return false
	}
	owned := false
	if context := state.managedVoxelCommit; context != nil && context.generation == state.Generation {
		owned = ownedBy(context.coord, context.chunk)
	}
	if !owned {
		for coord, chunk := range state.LoadedChunks {
			if ownedBy(coord, chunk) {
				owned = true
				break
			}
		}
	}
	if !owned {
		for coord := range state.readyCommits.activeChunks {
			if active := activeStreamedChunkCommit(state, coord); active != nil && active.entryGeneration == state.Generation && ownedBy(coord, active.chunk) {
				owned = true
				break
			}
		}
	}
	if !owned {
		return false
	}
	old := state.snapshotGeometryAssets[eid]
	if state.snapshotGeometryAssets == nil {
		state.snapshotGeometryAssets = make(map[EntityId]streamedGeometryAssetLease)
	}
	state.snapshotGeometryAssets[eid] = streamedGeometryAssetLease{ID: id, Server: assets}
	if old.Server != nil && (old.ID != id || old.Server != assets) {
		old.Server.DeleteVoxelGeometry(old.ID)
	}
	return true
}

// Bind only a canonical construction that agrees with the actual streamed
// placement and immutable loader definition. Failure leaves ordinary tracking
// available and selects full persistence fallback.
func captureManagedVoxelPersistenceBinding(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId) {
	if !leaseManagedVoxelOverride(cmd, assets, eid, id) {
		return
	}
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	ref, found := managedVoxelItemRef(cmd, eid)
	if !found {
		return
	}
	coord, owned := managedVoxelStreamMembership(state, eid, ref.PlacementID, ref.ItemID)
	if !owned {
		return
	}
	entry := assets.managedVoxelEntry(id)
	if entry == nil || entry.exposed || entry.authoredBase.identity == "" || ref.LevelID != state.LevelID {
		return
	}
	actualPath, selected := managedVoxelSelectedAssetPath(state, coord, ref.PlacementID)
	refPath := managedVoxelResolvedPath(ref.AssetPath, state.LevelPath)
	if !selected || actualPath != refPath {
		return
	}
	if context := state.managedVoxelCommit; context != nil && context.coord == coord {
		if context.placement.PlacementID != ref.PlacementID || managedVoxelResolvedPath(context.placement.AssetPath, state.LevelPath) != actualPath {
			return
		}
	}
	canonical, err := loadRuntimeAssetCanonicalPart(state.Loader, actualPath, ref.ItemID, true, runtimeAssetCanonicalOptions{
		proveAuthoredBase: true,
		acceptMetadata: func(assetID string, lattice content.VoxelObjectLatticeDef) bool {
			return assetID == ref.AssetID && lattice == entry.authoredBase.lattice
		},
	})
	if err != nil || canonical.assetID != ref.AssetID || canonical.lattice != entry.authoredBase.lattice || canonical.identity != entry.authoredBase.identity {
		return
	}
	entry.persistenceBinding = &managedVoxelPersistenceBinding{state: state, generation: state.Generation, levelID: state.LevelID, levelPath: filepath.Clean(state.LevelPath), ref: ref, assetPath: actualPath, baseBricks: canonical.bricks, baseVoxels: canonical.voxels, baseDecodedBytes: canonical.decodedBytes}
}

// Query captured provenance using only current owner metadata and membership.
// Changed ownership, references, lattice, or exposure selects full fallback.
func managedVoxelPersistenceBase(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, eid EntityId, placementID, itemID string) (string, content.VoxelObjectLatticeDef, bool) {
	identity, lattice, qualified := managedVoxelGeometryBase(cmd, assets, eid)
	_, entry, enabled := managedVoxelEntity(cmd, assets, eid)
	if !qualified || !enabled || entry.persistenceBinding == nil || state == nil {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	binding := entry.persistenceBinding
	ref, found := managedVoxelItemRef(cmd, eid)
	if !found || binding.state != state || binding.generation != state.Generation || binding.levelID != state.LevelID || binding.levelPath != filepath.Clean(state.LevelPath) || ref.LevelID != binding.ref.LevelID || ref.PlacementID != binding.ref.PlacementID || ref.ItemID != binding.ref.ItemID || ref.AssetID != binding.ref.AssetID || ref.VolumeID != binding.ref.VolumeID || ref.PlacementID != placementID || ref.ItemID != itemID || managedVoxelResolvedPath(ref.AssetPath, state.LevelPath) != binding.assetPath {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	coord, owned := managedVoxelStreamMembership(state, eid, placementID, itemID)
	actualPath, selected := managedVoxelSelectedAssetPath(state, coord, placementID)
	if !owned || !selected || actualPath != binding.assetPath {
		return "", content.VoxelObjectLatticeDef{}, false
	}
	return identity, lattice, true
}
