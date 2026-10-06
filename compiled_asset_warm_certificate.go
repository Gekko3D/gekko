package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Only verified cold registration creates this certificate. Returning a mutable
// ordinary geometry alias permanently removes it for that asset ID, even when
// the caller only reads. Warm publication never recreates a certificate.
type compiledAssetWarmCertificate struct {
	key          string
	lattice      content.VoxelObjectLatticeDef
	baseIdentity string
	model        bool
	dimensions   [3]uint32
	source       *volume.XBrickMap
	localMin     mgl32.Vec3
	localMax     mgl32.Vec3
	brickSize    [3]uint32
	sourcePath   string
	runtimeOwned bool
	voxelCount   int
}

// All certificate operations require the AssetServer's exclusive lock.
func (server *AssetServer) recordCompiledAssetWarmCertificateLocked(id AssetId, key string, lattice content.VoxelObjectLatticeDef, baseIdentity string, model bool, asset VoxelGeometryAsset) {
	if server.compiledAssetWarmCertificates == nil {
		server.compiledAssetWarmCertificates = make(map[AssetId]compiledAssetWarmCertificate)
	}
	server.compiledAssetWarmCertificates[id] = compiledAssetWarmCertificate{
		key: key, lattice: lattice, baseIdentity: baseIdentity, model: model,
		dimensions: [3]uint32{asset.VoxModel.SizeX, asset.VoxModel.SizeY, asset.VoxModel.SizeZ},
		source:     asset.XBrickMap, localMin: asset.LocalMin, localMax: asset.LocalMax,
		brickSize: asset.BrickSize, sourcePath: asset.SourcePath,
		runtimeOwned: asset.RuntimeOwned, voxelCount: len(asset.VoxModel.Voxels),
	}
}

func (server *AssetServer) revokeCompiledAssetWarmCertificateLocked(id AssetId) {
	delete(server.compiledAssetWarmCertificates, id)
}

// This checks scalar identity and the unborrowed registered source, never scans
// mutable maps or treats pointer/revision equality as a content certificate.
func (server *AssetServer) compiledAssetWarmCertificateMatchesLocked(id AssetId, shape *compiledAssetPacketShape, asset VoxelGeometryAsset) bool {
	certificate, exists := server.compiledAssetWarmCertificates[id]
	if !exists || shape == nil || certificate.source == nil {
		return false
	}
	key := compiledAssetPacketGeometryKey(shape)
	return certificate.key == key && server.voxModelKeys[key] == id &&
		certificate.lattice == shape.lattice && certificate.baseIdentity == shape.baseIdentity &&
		certificate.model == shape.model && certificate.dimensions == shape.dimensions &&
		certificate.source == asset.XBrickMap && certificate.localMin == asset.LocalMin && certificate.localMax == asset.LocalMax &&
		certificate.dimensions == [3]uint32{asset.VoxModel.SizeX, asset.VoxModel.SizeY, asset.VoxModel.SizeZ} &&
		certificate.brickSize == asset.BrickSize && certificate.sourcePath == asset.SourcePath &&
		certificate.runtimeOwned == asset.RuntimeOwned && certificate.voxelCount == len(asset.VoxModel.Voxels)
}
