package gekko

import "unsafe"

// Availability is ordinary AssetServer storage, not per-instance LOD opt-in.
// Proof maps remain private immutable storage independent of every packet/asset.
type compiledAssetLODBinding struct {
	fullID, coarseID AssetId
	proof            *compiledAssetLODProof
	bytes            int64
}

type compiledAssetLODStats struct {
	Entries int
	Bytes   int64
}

func (server *AssetServer) compiledAssetLODStorageStats() compiledAssetLODStats {
	if server == nil {
		return compiledAssetLODStats{}
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.compiledAssetLODStats
}

func (server *AssetServer) compiledAssetLODForGeometry(fullID AssetId) (*compiledAssetLODBinding, bool) {
	if server == nil {
		return nil, false
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	binding := server.compiledAssetLODs[fullID]
	if !server.compiledAssetLODBindingValidLocked(fullID, binding) {
		return nil, false
	}
	return binding, true
}

func validCompiledAssetLODProof(proof *compiledAssetLODProof) bool {
	return proof != nil && proof.full != nil && proof.coarse != nil &&
		proof.sourceContentID != "" && proof.contentID != "" && proof.value != 0 &&
		validAuthoredVoxelShapeLattice(proof.lattice) && proof.lattice.RasterizationVersion == authoredVoxelShapeRasterizationVersion
}

func (server *AssetServer) compiledAssetLODBindingValidLocked(fullID AssetId, binding *compiledAssetLODBinding) bool {
	if binding == nil || binding.proof == nil {
		return false
	}
	return server.compiledAssetLODBindingValidWithKeysLocked(fullID, binding,
		"compiled-asset-shape:"+binding.proof.sourceContentID, "compiled-asset-lod:"+binding.proof.contentID)
}

func (server *AssetServer) compiledAssetLODBindingValidWithKeysLocked(fullID AssetId, binding *compiledAssetLODBinding, fullKey, coarseKey string) bool {
	if binding == nil || binding.fullID != fullID || binding.coarseID == fullID || !validCompiledAssetLODProof(binding.proof) {
		return false
	}
	proof := binding.proof
	full, fullExists := server.voxModels[fullID]
	coarse, coarseExists := server.voxModels[binding.coarseID]
	return fullExists && full.XBrickMap != nil && coarseExists && coarse.XBrickMap != nil &&
		server.voxModelKeys[fullKey] == fullID &&
		server.voxModelKeys[coarseKey] == binding.coarseID &&
		server.authoredVoxelBases[fullID][proof.lattice] != ""
}

// Called after authenticated full adoption. Lock order remains server then
// registration. Only private immutable proofs are traversed while locked; warm
// public geometry is preserved and qualified later by the actual render owner.
func (server *AssetServer) adoptCompiledAssetPacketLOD(fullID AssetId, lod *compiledAssetPacketLOD) bool {
	if server == nil || lod == nil || fullID == (AssetId{}) || !validCompiledAssetLODProof(lod.proof) ||
		lod.contentID != lod.proof.contentID || lod.sourceContentID != lod.proof.sourceContentID || lod.value != lod.proof.value {
		return false
	}
	server.ensureVoxelStorage()
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.adoptCompiledAssetPacketLODLocked(fullID, lod)
}

// Caller holds server.mu through full packet publication and qualification.
func (server *AssetServer) adoptCompiledAssetPacketLODLocked(fullID AssetId, lod *compiledAssetPacketLOD) bool {
	if lod == nil || fullID == (AssetId{}) || !validCompiledAssetLODProof(lod.proof) || lod.contentID != lod.proof.contentID || lod.sourceContentID != lod.proof.sourceContentID || lod.value != lod.proof.value {
		return false
	}
	// LOD ownership directly borrows full/coarse maps. Their ordinary compiled
	// certificates must not survive that borrow, including failed binding attempts.
	server.revokeCompiledAssetWarmCertificateLocked(fullID)
	proof := lod.proof
	full, fullExists := server.voxModels[fullID]
	if !fullExists || full.XBrickMap == nil || server.voxModelKeys["compiled-asset-shape:"+proof.sourceContentID] != fullID || server.authoredVoxelBases[fullID][proof.lattice] == "" {
		return false
	}
	if binding, exists := server.compiledAssetLODs[fullID]; exists {
		server.revokeCompiledAssetWarmCertificateLocked(binding.coarseID)
		lod.registration.release()
		return server.compiledAssetLODBindingValidLocked(fullID, binding) &&
			binding.proof.sourceContentID == proof.sourceContentID && binding.proof.contentID == proof.contentID &&
			binding.proof.lattice == proof.lattice && binding.proof.value == proof.value
	}
	key := "compiled-asset-lod:" + proof.contentID
	coarseID, warm := server.voxModelKeys[key]
	server.revokeCompiledAssetWarmCertificateLocked(coarseID)
	if warm {
		lod.registration.release()
		asset, exists := server.voxModels[coarseID]
		if !exists || asset.XBrickMap == nil || coarseID == fullID {
			return false
		}
	} else {
		asset, _, _, _, taken := lod.registration.take(lod.source)
		if !taken {
			// A consumed handle can rebuild only from retained authenticated proof.
			source := proof.coarse.Copy()
			fresh := prepareStreamedGeometryRegistration(source)
			asset, _, _, _, taken = fresh.take(source)
			fresh.release()
		}
		if !taken || !compiledAssetPrimaryGeometryMatches(asset.XBrickMap, proof.coarse) {
			return false
		}
		coarseID = makeAssetId()
		asset.SourcePath = key
		server.voxModels[coarseID] = asset
		server.voxModelKeys[key] = coarseID
	}
	owned := *proof
	owned.full, owned.coarse = proof.full.Copy(), proof.coarse.Copy()
	binding := &compiledAssetLODBinding{fullID: fullID, coarseID: coarseID, proof: &owned}
	scalar := owned
	scalar.full, scalar.coarse = nil, nil
	binding.bytes = runtimeContentChargeSum(int64(unsafe.Sizeof(compiledAssetLODBinding{})), runtimeContentGraphCharge(&scalar),
		streamedPendingGeometryCharge(owned.full), streamedPendingGeometryCharge(owned.coarse))
	if server.compiledAssetLODs == nil {
		server.compiledAssetLODs = make(map[AssetId]*compiledAssetLODBinding)
	}
	server.compiledAssetLODs[fullID] = binding
	server.compiledAssetLODStats.Entries++
	server.compiledAssetLODStats.Bytes = runtimeContentChargeSum(server.compiledAssetLODStats.Bytes, binding.bytes)
	return true
}

// Delete only associations; coarse and full geometry retain independent ordinary
// asset lifetimes. Multiple associations can reference one coarse asset.
func (server *AssetServer) removeCompiledAssetLODBindingsLocked(id AssetId) {
	for fullID, binding := range server.compiledAssetLODs {
		if fullID == id || binding != nil && binding.coarseID == id {
			delete(server.compiledAssetLODs, fullID)
			if binding != nil {
				server.compiledAssetLODStats.Entries--
				server.compiledAssetLODStats.Bytes -= binding.bytes
			}
		}
	}
}
