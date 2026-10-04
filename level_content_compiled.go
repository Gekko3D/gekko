package gekko

import (
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
)

// Only a failure to load the selected input is eligible for the pickup's
// missing-visual tolerance. Missing dependencies of a present input are errors.
type authoredAssetInputLoadError struct{ err error }

func (err *authoredAssetInputLoadError) Error() string { return err.err.Error() }
func (err *authoredAssetInputLoadError) Unwrap() error { return err.err }

// Single-model level consumers select Parts[0], including a group at that index.
// Compiled inputs prove their whole closure before publishing only that part.
func loadAuthoredLevelVoxelModel(assets *AssetServer, loader *RuntimeContentLoader, path string) (AssetId, AssetId, float32, error) {
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	if filepath.Ext(path) != ".gkassetc" {
		asset, err := loader.LoadAsset(path)
		if err != nil {
			return AssetId{}, AssetId{}, 0, &authoredAssetInputLoadError{err}
		}
		return VoxelModelFromAsset(assets, asset, path)
	}
	session, err := verifyCompiledAssetInput(path, loader, nil)
	if err != nil {
		return AssetId{}, AssetId{}, 0, err
	}
	defer session.close()
	if assets == nil {
		return AssetId{}, AssetId{}, 0, nil
	}
	if len(session.def.Parts) == 0 {
		return AssetId{}, AssetId{}, 0, fmt.Errorf("asset %s has no parts", path)
	}
	part := session.def.Parts[0]
	voxelResolution := part.VoxelResolution
	if voxelResolution <= 0 {
		voxelResolution = content.DefaultAssetVoxelSize
	}
	if part.Source.Kind == content.AssetSourceKindGroup {
		return AssetId{}, AssetId{}, voxelResolution, nil
	}
	shape := session.shapes[part.ID]
	key := "compiled-asset-shape:" + shape.contentID
	id, warm := assets.SharedVoxelGeometryByCacheKey(key)
	if warm {
		if original := assets.authoredVoxelBaseIdentity(id, shape.definition.Lattice); original != "" && original != shape.baseIdentity {
			return AssetId{}, AssetId{}, 0, fmt.Errorf("compiled shape original base identity conflicts with shared geometry")
		}
	} else {
		geometry, _ := compiledShapeGeometry(shape.definition)
		id = assets.RegisterSharedVoxelGeometryWithCacheKey(key, geometry, key)
	}
	assets.recordVerifiedAuthoredVoxelBase(id, shape.definition.Lattice, shape.baseIdentity)
	palette, err := authoredVoxelShapePalette(assets, session.def, part)
	if err != nil {
		return AssetId{}, AssetId{}, 0, err
	}
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return AssetId{}, AssetId{}, 0, err
	}
	return id, palette, voxelResolution, nil
}
