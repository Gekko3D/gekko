package gekko

import (
	"fmt"

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
	part, resolution, err := loadAuthoredLevelVoxelPart(assets, loader, path)
	return part.model, part.palette, resolution, err
}

// Carry only the selected part's declared intent. Shared geometry availability
// cannot opt another input in, and the stable tuple wrapper remains full-only.
func loadAuthoredLevelVoxelPart(assets *AssetServer, loader *RuntimeContentLoader, path string) (preparedAuthoredPart, float32, error) {
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	if !isCompiledAssetPath(path) {
		asset, err := loader.LoadAsset(path)
		if err != nil {
			return preparedAuthoredPart{}, 0, &authoredAssetInputLoadError{err}
		}
		model, palette, resolution, err := VoxelModelFromAsset(assets, asset, path)
		return preparedAuthoredPart{model: model, palette: palette}, resolution, err
	}
	session, err := verifyCompiledAssetInput(path, loader, nil)
	if err != nil {
		return preparedAuthoredPart{}, 0, err
	}
	defer session.close()
	if assets == nil {
		return preparedAuthoredPart{}, 0, nil
	}
	if len(session.def.Parts) == 0 {
		return preparedAuthoredPart{}, 0, fmt.Errorf("asset %s has no parts", path)
	}
	part := session.def.Parts[0]
	voxelResolution := part.VoxelResolution
	if voxelResolution <= 0 {
		voxelResolution = content.DefaultAssetVoxelSize
	}
	if part.Source.Kind == content.AssetSourceKindGroup {
		return preparedAuthoredPart{}, voxelResolution, nil
	}
	_, declaredLOD := session.lods[part.ID]
	_, declaredModel := session.models[part.ID]
	if declaredLOD || declaredModel {
		packet, err := prepareCompiledAssetPacketFromVerification(path, session, loader, nil, []content.AssetPartDef{part})
		if err != nil {
			return preparedAuthoredPart{}, 0, err
		}
		defer packet.release()
		prepared, err := publishCompiledAssetPacket(packet, assets, loader)
		if err != nil {
			return preparedAuthoredPart{}, 0, err
		}
		selected := prepared.parts[part.ID]
		return selected, voxelResolution, nil
	}
	shape := session.shapes[part.ID]
	key := "compiled-asset-shape:" + shape.contentID
	id, warm := assets.SharedVoxelGeometryByCacheKey(key)
	if warm {
		if original := assets.authoredVoxelBaseIdentity(id, shape.definition.Lattice); original != "" && original != shape.baseIdentity {
			return preparedAuthoredPart{}, 0, fmt.Errorf("compiled shape original base identity conflicts with shared geometry")
		}
	} else {
		geometry, _ := compiledShapeGeometry(shape.definition)
		id = assets.RegisterSharedVoxelGeometryWithCacheKey(key, geometry, key)
	}
	assets.recordVerifiedAuthoredVoxelBase(id, shape.definition.Lattice, shape.baseIdentity)
	palette, err := authoredVoxelShapePalette(assets, session.def, part)
	if err != nil {
		return preparedAuthoredPart{}, 0, err
	}
	if err := checkCompiledAssetWork(loader, nil); err != nil {
		return preparedAuthoredPart{}, 0, err
	}
	return preparedAuthoredPart{model: id, palette: palette}, voxelResolution, nil
}
