package gekko

import (
	"fmt"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// One placement owns distinct single-use candidates even when its compiled CPU
// packet and global ordinary geometry are shared with another placement.
type streamedManagedPreparedAsset struct {
	path  string
	parts map[string]*streamedManagedPreparedPart
}

type streamedManagedPreparedPart struct {
	mu                    sync.Mutex
	shape                 *compiledAssetPacketShape
	owner                 *volume.ManagedXBrickMap
	authority, derivative *volume.XBrickMap
	bytes                 int64
	rendererBytes         int64
	bricks, voxels        int
	adoptedID             AssetId
}

func (p *streamedManagedPreparedPart) release() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.owner, p.authority, p.derivative, p.shape = nil, nil, nil, nil
	p.bytes, p.rendererBytes = 0, 0
	p.adoptedID = AssetId{}
}

func (p *streamedManagedPreparedPart) charge() int64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return runtimeContentChargeSum(int64(unsafe.Sizeof(streamedManagedPreparedPart{})), p.bytes)
}

func (p *streamedManagedPreparedAsset) release() {
	if p != nil {
		for _, part := range p.parts {
			part.release()
		}
	}
}

func streamedManagedPreparedCharge(values map[string]*streamedManagedPreparedAsset) int64 {
	if values == nil {
		return 0
	}
	metadata := make(map[string]*streamedManagedPreparedAsset, len(values))
	var bytes int64
	for key, asset := range values {
		if asset == nil {
			metadata[key] = nil
			continue
		}
		copy := &streamedManagedPreparedAsset{path: asset.path, parts: make(map[string]*streamedManagedPreparedPart, len(asset.parts))}
		for id, part := range asset.parts {
			copy.parts[id] = nil
			bytes = runtimeContentChargeSum(bytes, part.charge())
		}
		metadata[key] = copy
	}
	return runtimeContentChargeSum(bytes, runtimeContentGraphCharge(metadata))
}

// Constructor preflight includes owner base/current inventories and index
// storage as well as two independent dense snapshots and constructor scratch.
func streamedManagedPartPreflight(shape *compiledAssetPacketShape) (int64, error) {
	if shape == nil || shape.source == nil {
		return 0, fmt.Errorf("managed prepared shape is missing")
	}
	charge, ok := volume.PreflightManagedXBrickMap(shape.source)
	if !ok {
		return 0, fmt.Errorf("managed prepared shape is not sealable")
	}
	m := streamedGeometryBoundMath{}
	if charge.PeakBytes > uint64(^uint64(0)>>1) || charge.SnapshotBytes > uint64(^uint64(0)>>1) {
		return 0, fmt.Errorf("managed preparation charge overflow")
	}
	bytes := m.add(int64(charge.PeakBytes), m.mul(2, int64(charge.SnapshotBytes)))
	return bytes, m.err
}

func forEachStreamedManagedCandidate(payload streamedPreparedChunk, job streamedChunkLoadJob, visit func(string, *compiledAssetPacket, content.AssetPartDef, *compiledAssetPacketShape) error) error {
	if !job.managedPreparedAssets {
		return nil
	}
	for _, placement := range payload.PlacementItems {
		path, err := compiledAssetPacketKey(placement.AssetPath, job.LevelPath)
		if err != nil {
			return err
		}
		packet := payload.compiledAssets[path]
		if packet == nil || packet.def == nil || packet.def.Runtime != nil && packet.def.Runtime.CollapseVoxelParts {
			continue
		}
		for _, part := range packet.def.Parts {
			if _, lod := packet.partLODs[part.ID]; lod {
				continue
			}
			if payload.ObjectSnapshots[voxelObjectRuntimeKey(placement.PlacementID, part.ID)] != nil {
				continue
			}
			shape := packet.shapes[packet.parts[part.ID]]
			if shape == nil {
				continue
			}
			if err := visit(placement.PlacementID, packet, part, shape); err != nil {
				return err
			}
		}
	}
	return nil
}

func streamedManagedPreparedPreflight(payload streamedPreparedChunk, job streamedChunkLoadJob) (int64, error) {
	m := streamedGeometryBoundMath{}
	metadata := make(map[string]*streamedManagedPreparedAsset)
	var bytes int64
	err := forEachStreamedManagedCandidate(payload, job, func(placement string, packet *compiledAssetPacket, part content.AssetPartDef, shape *compiledAssetPacketShape) error {
		charge, err := streamedManagedPartPreflight(shape)
		if err != nil {
			return err
		}
		bytes = m.add(bytes, charge, int64(unsafe.Sizeof(streamedManagedPreparedPart{})))
		asset := metadata[placement]
		if asset == nil {
			asset = &streamedManagedPreparedAsset{path: packet.documentPath, parts: make(map[string]*streamedManagedPreparedPart)}
			metadata[placement] = asset
		}
		asset.parts[part.ID] = nil
		return m.err
	})
	if err != nil {
		return 0, err
	}
	if len(metadata) == 0 {
		return 0, nil
	}
	return m.add(bytes, runtimeContentGraphCharge(metadata)), m.err
}

func prepareStreamedManagedAssets(payload *streamedPreparedChunk, job streamedChunkLoadJob) error {
	return forEachStreamedManagedCandidate(*payload, job, func(placement string, packet *compiledAssetPacket, part content.AssetPartDef, shape *compiledAssetPacketShape) error {
		if streamedPreparationCancelled(job.prepareCancel) {
			return fmt.Errorf("managed preparation cancelled")
		}
		bytes, err := streamedManagedPartPreflight(shape)
		if err != nil {
			return err
		}
		owner := volume.NewManagedXBrickMap(shape.source)
		authority := owner.Snapshot()
		derivative := owner.Snapshot()
		bricks, voxels, qualified := owner.CurrentGeometryCounts()
		if !qualified {
			return fmt.Errorf("managed preparation is unqualified")
		}
		if payload.managedPreparedAssets == nil {
			payload.managedPreparedAssets = make(map[string]*streamedManagedPreparedAsset)
		}
		asset := payload.managedPreparedAssets[placement]
		if asset == nil {
			asset = &streamedManagedPreparedAsset{path: packet.documentPath, parts: make(map[string]*streamedManagedPreparedPart)}
			payload.managedPreparedAssets[placement] = asset
		}
		asset.parts[part.ID] = &streamedManagedPreparedPart{shape: shape, owner: owner, authority: authority, derivative: derivative, bytes: bytes, rendererBytes: streamedPendingGeometryCharge(derivative), bricks: bricks, voxels: voxels}
		return nil
	})
}

// Recheck the unborrowed compiled registration at actual transfer, before root
// creation or callbacks can expose mutable global geometry. No dense copy or
// comparison is performed on the main thread; borrowed globals retain fallback.
func adoptStreamedManagedParts(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, placement string, prepared *PreparedAuthoredAsset, candidates *streamedManagedPreparedAsset) map[string]AssetId {
	adopted := make(map[string]AssetId)
	if candidates == nil || state == nil || !state.Config.EnableManagedPreparedAssets || prepared == nil || filepath.Clean(candidates.path) != filepath.Clean(prepared.documentPath) {
		return adopted
	}
	for partID, candidate := range candidates.parts {
		part := prepared.parts[partID]
		if part.compiledLOD != (AssetId{}) || state.voxelOverrideMap[voxelObjectRuntimeKey(placement, partID)].SnapshotPath != "" {
			continue
		}
		candidate.mu.Lock()
		if candidate.owner == nil || candidate.shape == nil {
			candidate.mu.Unlock()
			continue
		}
		assets.mu.Lock()
		base, exists := assets.voxModels[part.model]
		if !exists || !assets.compiledAssetWarmCertificateMatchesLocked(part.model, candidate.shape, base) {
			assets.mu.Unlock()
			candidate.mu.Unlock()
			continue
		}
		id := makeAssetId()
		base.XBrickMap = candidate.authority
		base.RuntimeOwned = true
		assets.voxModels[id] = base
		if assets.managedVoxelGeometry == nil {
			assets.managedVoxelGeometry = make(map[AssetId]*managedVoxelGeometry)
		}
		entry := &managedVoxelGeometry{owner: candidate.owner, app: cmd.app, preparedBaseBricks: candidate.bricks, preparedBaseVoxels: candidate.voxels, preparedBaseBytes: candidate.shape.baseDecodedBytes}
		// Inline construction can certify E2 persistence; model rasterization
		// remains independent and uses the existing full fallback.
		if !candidate.shape.model {
			entry.authoredBase = authoredVoxelBase{identity: candidate.shape.baseIdentity, lattice: candidate.shape.lattice}
		}
		assets.managedVoxelGeometry[id] = entry
		if assets.preparedVoxelRendererCopies == nil {
			assets.preparedVoxelRendererCopies = make(map[AssetId]preparedVoxelRendererCopy)
		}
		derivativeBytes := candidate.rendererBytes
		assets.preparedVoxelRendererCopies[id] = preparedVoxelRendererCopy{source: candidate.authority, geometry: candidate.derivative, bytes: derivativeBytes}
		assets.preparedVoxelRendererCopyStats.Entries++
		assets.preparedVoxelRendererCopyStats.Bytes += derivativeBytes
		assets.mu.Unlock()
		candidate.owner, candidate.authority, candidate.derivative = nil, nil, nil
		candidate.bytes, candidate.rendererBytes = 0, 0
		candidate.adoptedID = id
		candidate.mu.Unlock()
		adopted[partID] = id
	}
	return adopted
}

// Hooks may select an owner that ordinary managed inputs cannot certify. Only
// this placement's exact automatically adopted ID may be demoted. Its already
// independent CPU authority contains finalized managed edits, so compatibility
// keeps both that content and the existing chunk lease instead of restoring the
// potentially different shared base. Explicitly enabled/replaced owners stay
// under their existing contracts.
func reconcileStreamedManagedParts(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, chunk *streamedLoadedChunk, placement string, entities map[string]EntityId, candidates *streamedManagedPreparedAsset) {
	if candidates == nil || state == nil {
		return
	}
	for partID, candidate := range candidates.parts {
		candidate.mu.Lock()
		id := candidate.adoptedID
		candidate.mu.Unlock()
		if id == (AssetId{}) {
			continue
		}
		eid := entities[partID]
		key := voxelObjectRuntimeKey(placement, partID)
		if chunk.ObjectEntities[key] != eid {
			continue
		}
		if _, owned := chunk.OwnedEntities[eid]; !owned {
			continue
		}
		lease := state.snapshotGeometryAssets[eid]
		vmc, found := voxelModelComponentForEdit(cmd, eid)
		entry := assets.managedVoxelEntry(id)
		if !found || vmc.OverrideGeometry != id || lease.ID != id || lease.Server != assets || entry == nil || entry.entity != eid || entry.app != cmd.app {
			continue
		}
		_, err := validateManagedVoxelEntity(cmd, assets, eid)
		if err == nil && vmc.VoxelAdjacencyGroupID == 0 && vmc.TerrainGroupID == 0 && vmc.PlanetTileGroupID == 0 {
			continue
		}
		assets.mu.Lock()
		if assets.managedVoxelGeometry[id] == entry {
			// Authority snapshots are independent and are synchronously updated
			// after every managed edit, including an applied panic prefix. Exposed
			// entries already retain their exact dense authority pointer.
			delete(assets.managedVoxelGeometry, id)
			assets.removePreparedVoxelRendererCopyLocked(id)
		}
		assets.mu.Unlock()
	}
}

func bindStreamedManagedPart(cmd *Commands, assets *AssetServer, eid EntityId, id AssetId) {
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	if state == nil || state.managedVoxelCommit == nil || state.managedVoxelCommit.generation != state.Generation {
		return
	}
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	entry := assets.managedVoxelEntry(id)
	if !ok || entry == nil || entry.app != cmd.app || entry.entity != 0 {
		return
	}
	vmc.OverrideGeometry = id
	if _, err := validateManagedVoxelEntityWithModel(cmd, assets, eid, vmc); err != nil {
		return
	}
	entry.entity = eid
	cmd.AddComponents(eid, &vmc)
}

func claimStreamedManagedPart(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, eid EntityId, id AssetId) {
	if state == nil || state.managedVoxelCommit == nil || state.managedVoxelCommit.generation != state.Generation {
		return
	}
	vmc, entry, enabled := managedVoxelEntity(cmd, assets, eid)
	if !enabled || vmc.OverrideGeometry != id || !leaseManagedVoxelOverride(cmd, assets, eid, id) {
		return
	}
	if entry.authoredBase.identity == "" {
		return
	}
	ref, found := managedVoxelItemRef(cmd, eid)
	if !found {
		return
	}
	context := state.managedVoxelCommit
	path, selected := managedVoxelSelectedAssetPath(state, context.coord, ref.PlacementID)
	if !selected || ref.LevelID != state.LevelID || ref.PlacementID != context.placement.PlacementID || path != managedVoxelResolvedPath(ref.AssetPath, state.LevelPath) || path != managedVoxelResolvedPath(context.placement.AssetPath, state.LevelPath) {
		return
	}
	entry.persistenceBinding = &managedVoxelPersistenceBinding{state: state, generation: state.Generation, levelID: state.LevelID, levelPath: filepath.Clean(state.LevelPath), ref: ref, assetPath: path, baseBricks: entry.preparedBaseBricks, baseVoxels: entry.preparedBaseVoxels, baseDecodedBytes: entry.preparedBaseBytes}
}

// Managed first derivatives need only their private sealing/generation proof;
// unlike ordinary public geometry, their content cannot bypass that proof.
func (assets *AssetServer) takeManagedPreparedRendererCopy(id AssetId, entry *managedVoxelGeometry, source *volume.XBrickMap) *volume.XBrickMap {
	assets.mu.Lock()
	defer assets.mu.Unlock()
	candidate, exists := assets.removePreparedVoxelRendererCopyLocked(id)
	asset, registered := assets.voxModels[id]
	if !exists || !registered || entry == nil || assets.managedVoxelGeometry[id] != entry || entry.exposed || entry.producerActive || entry.generation != 0 || source == nil || asset.XBrickMap != source || candidate.source != source {
		return nil
	}
	assets.preparedVoxelRendererCopyStats.Adoptions++
	return candidate.geometry
}
