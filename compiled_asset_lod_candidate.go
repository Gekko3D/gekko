package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type compiledAssetLODGeometryPair struct {
	current, baseline *volume.XBrickMap
}

// One qualification belongs to one main-thread sync with stable primary maps.
// Never retain it across frames: raw edits need not advance a map revision.
type compiledAssetLODQualification struct {
	geometry map[compiledAssetLODGeometryPair]bool
	keys     map[*compiledAssetLODProof]compiledAssetLODKeys
}

type compiledAssetLODKeys struct {
	full, coarse string
}

func (q *compiledAssetLODQualification) proofKeys(proof *compiledAssetLODProof) compiledAssetLODKeys {
	if proof == nil {
		return compiledAssetLODKeys{}
	}
	if keys, exists := q.keys[proof]; exists {
		return keys
	}
	keys := compiledAssetLODKeys{full: "compiled-asset-shape:" + proof.sourceContentID, coarse: "compiled-asset-lod:" + proof.contentID}
	if q.keys == nil {
		q.keys = make(map[*compiledAssetLODProof]compiledAssetLODKeys)
	}
	q.keys[proof] = keys
	return keys
}

type qualifiedCompiledAssetLOD struct {
	binding *compiledAssetLODBinding
	coarse  *volume.XBrickMap
}

func (q *compiledAssetLODQualification) primaryMatches(current, baseline *volume.XBrickMap) bool {
	pair := compiledAssetLODGeometryPair{current: current, baseline: baseline}
	if matches, exists := q.geometry[pair]; exists {
		return matches
	}
	matches := compiledAssetPrimaryGeometryMatches(current, baseline)
	if q.geometry == nil {
		q.geometry = make(map[compiledAssetLODGeometryPair]bool)
	}
	q.geometry[pair] = matches
	return matches
}

// candidate reads only already-published assets. Availability is not opt-in,
// and a source asset's primary map cannot certify an object-owned fine copy.
func (q *compiledAssetLODQualification) candidate(server *AssetServer, intent compiledAssetLODComponent, vox *VoxelModelComponent, object *core.VoxelObject) (qualifiedCompiledAssetLOD, bool) {
	if q == nil || server == nil || vox == nil || object == nil || object.Transform == nil || object.XBrickMap == nil ||
		intent.fullID == (AssetId{}) || intent.coarseID == (AssetId{}) || intent.fullID == intent.coarseID ||
		vox.GeometryAsset() != intent.fullID || compiledAssetLODHasSpecialLattice(vox, object) {
		return qualifiedCompiledAssetLOD{}, false
	}

	// Membership, map presence and palette come from one coherent owner snapshot.
	// Do not use getVoxelGeometry: it may hydrate and mutate missing primary maps.
	server.mu.RLock()
	binding := server.compiledAssetLODs[intent.fullID]
	var keys compiledAssetLODKeys
	if binding != nil {
		keys = q.proofKeys(binding.proof)
	}
	valid := server.compiledAssetLODBindingValidWithKeysLocked(intent.fullID, binding, keys.full, keys.coarse)
	coarse := server.voxModels[intent.coarseID].XBrickMap
	palette, hasPalette := server.voxPalettes[vox.VoxelPalette]
	server.mu.RUnlock()
	if !valid || binding.coarseID != intent.coarseID || !hasPalette || object.XBrickMap == coarse ||
		VoxelResolutionOrDefault(vox) != binding.proof.lattice.VoxelResolution {
		return qualifiedCompiledAssetLOD{}, false
	}
	value := binding.proof.value
	for _, animation := range palette.Animations {
		for _, index := range animation.PaletteIndices {
			if index == value {
				return qualifiedCompiledAssetLOD{}, false
			}
		}
	}
	if !compiledAssetLODMaterialEligible(object.MaterialTable, value, false) ||
		!q.primaryMatches(object.XBrickMap, binding.proof.full) || !q.primaryMatches(coarse, binding.proof.coarse) {
		return qualifiedCompiledAssetLOD{}, false
	}
	return qualifiedCompiledAssetLOD{binding: binding, coarse: coarse}, true
}

func compiledAssetLODHasSpecialLattice(vox *VoxelModelComponent, object *core.VoxelObject) bool {
	return vox.ShareTerrainGeometry || vox.IsTerrainChunk || vox.IsPlanetTile ||
		vox.VoxelAdjacencyGroupID != 0 || vox.VoxelAdjacencyChunkCoord != [3]int{} || vox.VoxelAdjacencyChunkSize != 0 ||
		vox.TerrainGroupID != 0 || vox.TerrainChunkCoord != [3]int{} || vox.TerrainChunkSize != 0 ||
		vox.PlanetTileGroupID != 0 || vox.PlanetTileFace != 0 || vox.PlanetTileLevel != 0 || vox.PlanetTileX != 0 || vox.PlanetTileY != 0 ||
		object.IsTerrainChunk || object.IsPlanetTile ||
		object.VoxelAdjacencyGroupID != 0 || object.VoxelAdjacencyChunkCoord != [3]int{} || object.VoxelAdjacencyChunkSize != 0 ||
		object.TerrainGroupID != 0 || object.TerrainChunkCoord != [3]int{} || object.TerrainChunkSize != 0 ||
		object.PlanetTileGroupID != 0 || object.PlanetTileFace != 0 || object.PlanetTileLevel != 0 || object.PlanetTileX != 0 || object.PlanetTileY != 0
}
