package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// adoptCompiledAssetGeometry publishes an independently verified compiled shape
// through the existing global geometry owner. The caller proves the content and
// original-base identities before preparing the single-use registration.
// Lock order is server then registration; no registration lock escapes this call.
func (server *AssetServer) adoptCompiledAssetGeometry(contentID string, lattice content.VoxelObjectLatticeDef, baseIdentity string, source *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool) {
	if server == nil || source == nil || registration == nil || contentID == "" || baseIdentity == "" || !validAuthoredVoxelShapeLattice(lattice) || lattice.RasterizationVersion != authoredVoxelShapeRasterizationVersion {
		return AssetId{}, false
	}
	server.ensureVoxelStorage()
	key := "compiled-asset-shape:" + contentID
	server.mu.Lock()
	defer server.mu.Unlock()
	if id, warm := server.voxModelKeys[key]; warm {
		// A warm global asset retains its current mutable geometry. The unused
		// registration belongs to this attempt and must drain on either outcome.
		registration.release()
		if _, exists := server.voxModels[id]; !exists {
			return AssetId{}, false
		}
		if original := server.authoredVoxelBases[id][lattice]; original != "" && original != baseIdentity {
			return AssetId{}, false
		}
		server.recordVerifiedAuthoredVoxelBaseLocked(id, lattice, baseIdentity)
		return id, true
	}
	asset, rendererCopy, rendererBytes, _, taken := registration.take(source)
	if !taken {
		return AssetId{}, false
	}
	id := makeAssetId()
	asset.SourcePath = key
	server.voxModels[id] = asset
	server.voxModelKeys[key] = id
	server.recordVerifiedAuthoredVoxelBaseLocked(id, lattice, baseIdentity)
	if rendererCopy != nil {
		if server.preparedVoxelRendererCopies == nil {
			server.preparedVoxelRendererCopies = make(map[AssetId]preparedVoxelRendererCopy)
		}
		server.preparedVoxelRendererCopies[id] = preparedVoxelRendererCopy{source: asset.XBrickMap, geometry: rendererCopy, bytes: rendererBytes}
		server.preparedVoxelRendererCopyStats.Entries++
		server.preparedVoxelRendererCopyStats.Bytes += rendererBytes
	}
	return id, true
}
