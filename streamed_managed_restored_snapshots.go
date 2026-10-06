package gekko

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Scalar, per-item ownership and validated payload identity. No mutable global
// geometry, decoded frame, or placement-wide v2 marker authorizes restoration.
type streamedSnapshotProof struct {
	snapshotPath, assetPath, assetID, placement, item string
	contentID, mode, baseIdentity                     string
	schema                                            int
	lattice                                           content.VoxelObjectLatticeDef
	bound                                             bool
}

func bindStreamedSnapshotCompiledProof(proof *streamedSnapshotProof, placements []streamedPlacementInstance, packets map[string]*compiledAssetPacket, levelPath string) *compiledAssetPacketShape {
	for _, placement := range placements {
		if placement.PlacementID != proof.placement {
			continue
		}
		path, err := compiledAssetPacketKey(placement.AssetPath, levelPath)
		if err != nil {
			return nil
		}
		packet := packets[path]
		if packet == nil || packet.def == nil || packet.def.Runtime != nil && packet.def.Runtime.CollapseVoxelParts {
			return nil
		}
		if _, lod := packet.partLODs[proof.item]; lod {
			return nil
		}
		shape := packet.shapes[packet.parts[proof.item]]
		if shape == nil {
			return nil
		}
		if shape.model && proof.schema != 1 {
			return nil
		}
		proof.assetPath, proof.assetID = path, packet.def.ID
		if proof.schema == 1 {
			proof.lattice = shape.lattice
		}
		return shape
	}
	return nil
}

// Unchanged fully validated content identity can reuse the worker's resolution;
// changed input goes through the existing canonical resolver and compatibility
// registration. Legacy input retains ordered record comparison and no base proof.
func currentStreamedPreparedSnapshot(p *streamedPreparedChunk, state *StreamedLevelRuntimeState, placement streamedPlacementInstance, key string, override content.VoxelObjectOverrideDef) (*content.VoxelObjectSnapshotDef, bool, error) {
	packet := p.objectSnapshotGeometry[key]
	proof := p.objectSnapshotProofs[key]
	path, pathErr := compiledAssetPacketKey(placement.AssetPath, state.LevelPath)
	if pathErr != nil || packet == nil || packet.managed == nil || proof == nil || key != voxelObjectRuntimeKey(override.PlacementID, override.ItemID) || proof.placement != override.PlacementID || proof.item != override.ItemID || placement.PlacementID != proof.placement || proof.assetPath != path || proof.snapshotPath != managedVoxelResolvedPath(override.SnapshotPath, state.WorldDeltaPath) {
		return nil, false, nil
	}
	payload, info, err := content.LoadVoxelObjectPayload(proof.snapshotPath, nil)
	if err != nil {
		return nil, false, err
	}
	if payload.SchemaVersion != proof.schema || payload.Mode != proof.mode {
		return nil, false, nil
	}
	if proof.contentID != "" {
		if info.ContentID == "" || info.ContentID != proof.contentID || payload.PlacementID != proof.placement || payload.ItemID != proof.item || payload.Lattice != proof.lattice || payload.BaseIdentity != proof.baseIdentity {
			return nil, false, nil
		}
	} else {
		if proof.schema != 1 {
			return nil, false, nil
		}
		current, err := content.ResolveVoxelObjectPayload(payload, nil, content.VoxelObjectLatticeDef{}, "", "", nil)
		if err != nil {
			return nil, false, err
		}
		if !streamedObjectSnapshotMatches(packet.snapshot, current) {
			return nil, false, nil
		}
	}
	return packet.snapshot, proof.schema != 1, nil
}

func streamedRestoredPartPreflight(base, current *volume.XBrickMap, bound bool) (int64, error) {
	charge, ok := volume.PreflightManagedXBrickMap(current)
	if bound {
		charge, ok = volume.PreflightManagedXBrickMapWithBase(base, current)
	}
	if !ok {
		return 0, fmt.Errorf("restored managed geometry is not sealable")
	}
	m := streamedGeometryBoundMath{}
	if charge.PeakBytes > uint64(^uint64(0)>>1) || charge.SnapshotBytes > uint64(^uint64(0)>>1) {
		return 0, fmt.Errorf("restored managed charge overflow")
	}
	return m.add(int64(charge.PeakBytes), m.mul(2, int64(charge.SnapshotBytes)), int64(unsafe.Sizeof(streamedManagedPreparedPart{}))), m.err
}

func prepareStreamedRestoredManagedPart(p *streamedPreparedChunk, job streamedChunkLoadJob, key string, packet *streamedObjectSnapshotGeometry) (int64, error) {
	proof := p.objectSnapshotProofs[key]
	if !job.managedPreparedAssets || proof == nil || proof.assetPath == "" {
		return 0, nil
	}
	compiled := p.compiledAssets[proof.assetPath]
	shape := compiled.shapes[compiled.parts[proof.item]]
	var base *volume.XBrickMap
	if proof.bound {
		base = shape.source
	}
	bytes, err := streamedRestoredPartPreflight(base, packet.source, proof.bound)
	if err != nil {
		return 0, err
	}
	// Initial composite prebuild credit already owns every ordinary source and
	// registration, including still-unbuilt siblings and terrain. Add this exact
	// owner/authority/derivative peak without consuming that reservation.
	if p.pendingCredit != nil {
		cost, accepted, err := p.pendingCredit.reserveConstruction(bytes)
		if err != nil {
			return 0, err
		}
		if !accepted {
			return cost, nil
		}
	}
	if streamedPreparationCancelled(job.prepareCancel) {
		return 0, nil
	}
	var owner *volume.ManagedXBrickMap
	if proof.bound {
		owner = volume.NewManagedXBrickMapWithBase(base, packet.source)
	} else {
		owner = volume.NewManagedXBrickMap(packet.source)
	}
	authority, derivative := owner.Snapshot(), owner.Snapshot()
	bricks, voxels := persistenceMapBrickCount(packet.source), packet.source.GetVoxelCount()
	if proof.bound {
		bricks, voxels = persistenceMapBrickCount(base), base.GetVoxelCount()
	}
	packet.managed = &streamedManagedPreparedPart{shape: shape, owner: owner, authority: authority, derivative: derivative, bytes: bytes - int64(unsafe.Sizeof(streamedManagedPreparedPart{})), rendererBytes: streamedPendingGeometryCharge(derivative), bricks: bricks, voxels: voxels}
	return 0, nil
}

func adoptStreamedRestoredManagedPart(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, eid EntityId, snapshot *content.VoxelObjectSnapshotDef, packet *streamedObjectSnapshotGeometry, proof *streamedSnapshotProof) bool {
	if !state.Config.EnableManagedPreparedAssets || packet == nil || packet.managed == nil || proof == nil || snapshot != packet.snapshot {
		return false
	}
	ref, found := managedVoxelItemRef(cmd, eid)
	vmc, model := voxelModelComponentForEdit(cmd, eid)
	coord, owned := managedVoxelStreamMembership(state, eid, proof.placement, proof.item)
	path, selected := managedVoxelSelectedAssetPath(state, coord, proof.placement)
	// The packet key is absolute; existing persistence ownership deliberately
	// retains the selected resolved spelling and exact reference equality.
	absolutePath, pathErr := filepath.Abs(path)
	if !found || !model || !owned || !selected || pathErr != nil || ref.LevelID != state.LevelID || ref.AssetID != proof.assetID || ref.PlacementID != proof.placement || ref.ItemID != proof.item || filepath.Clean(absolutePath) != proof.assetPath || managedVoxelResolvedPath(ref.AssetPath, state.LevelPath) != path || VoxelResolutionOrDefault(&vmc) != proof.lattice.VoxelResolution || vmc.VoxelAdjacencyGroupID != 0 || vmc.TerrainGroupID != 0 || vmc.PlanetTileGroupID != 0 {
		return false
	}
	candidate := packet.managed
	candidate.mu.Lock()
	defer candidate.mu.Unlock()
	if candidate.owner == nil || candidate.authority == nil {
		return false
	}
	id := makeAssetId()
	// Source bounds were finalized on the worker; adoption does no geometry scan.
	min, max := packet.source.CachedMin, packet.source.CachedMax
	entry := &managedVoxelGeometry{owner: candidate.owner, app: cmd.app, entity: eid}
	if proof.bound {
		entry.authoredBase = authoredVoxelBase{identity: proof.baseIdentity, lattice: proof.lattice}
		entry.persistenceBinding = &managedVoxelPersistenceBinding{state: state, generation: state.Generation, levelID: state.LevelID, levelPath: filepath.Clean(state.LevelPath), ref: ref, assetPath: path, baseBricks: candidate.bricks, baseVoxels: candidate.voxels, baseDecodedBytes: candidate.shape.baseDecodedBytes}
	}
	assets.mu.Lock()
	assets.voxModels[id] = VoxelGeometryAsset{XBrickMap: candidate.authority, LocalMin: min, LocalMax: max, BrickSize: [3]uint32{8, 8, 8}, RuntimeOwned: true}
	if assets.managedVoxelGeometry == nil {
		assets.managedVoxelGeometry = make(map[AssetId]*managedVoxelGeometry)
	}
	assets.managedVoxelGeometry[id] = entry
	assets.mu.Unlock()
	proposed := vmc
	proposed.OverrideGeometry = id
	if _, err := validateManagedVoxelEntityWithModel(cmd, assets, eid, proposed); err != nil || !leaseManagedVoxelOverride(cmd, assets, eid, id) {
		assets.DeleteVoxelGeometry(id)
		return false
	}
	assets.mu.Lock()
	if assets.preparedVoxelRendererCopies == nil {
		assets.preparedVoxelRendererCopies = make(map[AssetId]preparedVoxelRendererCopy)
	}
	assets.preparedVoxelRendererCopies[id] = preparedVoxelRendererCopy{source: candidate.authority, geometry: candidate.derivative, bytes: candidate.rendererBytes}
	assets.preparedVoxelRendererCopyStats.Entries++
	assets.preparedVoxelRendererCopyStats.Bytes += candidate.rendererBytes
	assets.mu.Unlock()
	cmd.AddComponents(eid, &proposed)
	state.Metrics.PreparedGeometryAssetAdoptions++
	candidate.owner, candidate.authority, candidate.derivative = nil, nil, nil
	candidate.bytes, candidate.rendererBytes = 0, 0
	candidate.adoptedID = id
	return true
}

func streamedHasRestoredManagedCandidate(p *streamedPreparedChunk, placement string) bool {
	for key, proof := range p.objectSnapshotProofs {
		if proof != nil && proof.placement == placement && p.objectSnapshotGeometry[key] != nil && p.objectSnapshotGeometry[key].managed != nil {
			return true
		}
	}
	return false
}

func reconcileStreamedRestoredManagedParts(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, chunk *streamedLoadedChunk, placement string, entities map[string]EntityId, p *streamedPreparedChunk) {
	parts := make(map[string]*streamedManagedPreparedPart)
	for item, eid := range entities {
		key := voxelObjectRuntimeKey(placement, item)
		packet := p.objectSnapshotGeometry[key]
		if packet == nil || packet.managed == nil {
			continue
		}
		candidate := packet.managed
		candidate.mu.Lock()
		id := candidate.adoptedID
		candidate.mu.Unlock()
		if id == (AssetId{}) {
			continue
		}
		vmc, found := voxelModelComponentForEdit(cmd, eid)
		lease := state.snapshotGeometryAssets[eid]
		if (!found || vmc.OverrideGeometry != id) && lease.ID == id && lease.Server == assets {
			state.releaseStreamedSnapshotGeometryAsset(eid)
			continue
		}
		parts[item] = candidate
	}
	// Both candidate kinds obey the same exact-ID demotion rules after hooks.
	reconcileStreamedManagedParts(cmd, assets, state, chunk, placement, entities, &streamedManagedPreparedAsset{parts: parts})
}
