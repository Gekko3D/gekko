package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Model adoption shares the ordinary global owner, with its own namespace and
// rasterization provenance. Full geometry remains collision/edit authority;
// declared dimensions retain source bounds without reconstructing raw samples.
// Lock order is server then registration, matching ordinary shape adoption.
func (server *AssetServer) adoptCompiledAssetModelGeometry(contentID string, lattice content.VoxelObjectLatticeDef, baseIdentity string, dimensions [3]uint32, source *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool) {
	if server == nil || source == nil || registration == nil || contentID == "" || baseIdentity == "" || !compiledModelPositiveFinite(lattice.VoxelResolution) || lattice.RasterizationVersion != compiledAssetModelRasterizationVersion {
		return AssetId{}, false
	}
	server.ensureVoxelStorage()
	key := "compiled-asset-model:" + contentID
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
	asset.VoxModel.SizeX, asset.VoxModel.SizeY, asset.VoxModel.SizeZ = dimensions[0], dimensions[1], dimensions[2]
	if dimensions != ([3]uint32{}) {
		asset.LocalMin = mgl32.Vec3{}
		asset.LocalMax = mgl32.Vec3{float32(dimensions[0]), float32(dimensions[1]), float32(dimensions[2])}
	}
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
