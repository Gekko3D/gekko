package gekko

import (
	"encoding/json"
	"fmt"

	"github.com/gekko3d/gekko/content"
)

type verifiedCompiledAssetShape struct {
	definition   *content.CompiledAssetShapeDef
	baseIdentity string
	contentID    string
}

func prepareCompiledAuthoredAsset(path string, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	if !runtimeContentOriginOpen(loader) {
		return nil, fmt.Errorf("content origin scope is closed")
	}
	scope := loader.NewScope()
	defer scope.Close()
	verification := scope.Loader()
	header, _, err := verification.LoadCompiledAssetHeader(path)
	if err != nil {
		return nil, err
	}
	// Prepared metadata owns every nested table. Cache definitions remain read-only.
	metadata, err := json.Marshal(header.Asset)
	if err != nil {
		return nil, err
	}
	var definition content.AssetDef
	if err := json.Unmarshal(metadata, &definition); err != nil {
		return nil, err
	}
	if validation := content.ValidateAsset(&definition, content.AssetValidationOptions{}); validation.HasErrors() {
		return nil, fmt.Errorf("compiled asset validation failed: %s", validation.Error())
	}
	animations, err := content.ResolveCompiledAssetAnimations(&definition, path)
	if err != nil {
		return nil, fmt.Errorf("compiled asset animation resolution failed: %w", err)
	}
	content.NormalizeAssetDef(&definition)
	if err := ValidateAssetHierarchy(&definition); err != nil {
		return nil, err
	}
	for i := range definition.Emitters {
		emitter := &definition.Emitters[i].Emitter
		if emitter.TexturePath != "" {
			emitter.TexturePath, err = content.ResolveCompiledAssetReference(emitter.TexturePath, path)
			if err != nil {
				return nil, err
			}
		}
	}
	parts := make(map[string]content.AssetPartDef, len(definition.Parts))
	for _, part := range definition.Parts {
		parts[part.ID] = part
	}
	verified := make(map[string]verifiedCompiledAssetShape, len(header.Shapes))
	identities := make(map[*content.CompiledAssetShapeDef]string)
	for _, ref := range header.Shapes {
		part, exists := parts[ref.PartID]
		if !exists || part.Source.Kind != content.AssetSourceKindVoxelShape || part.Source.VoxelShape == nil {
			return nil, fmt.Errorf("compiled shape does not identify a voxel part")
		}
		shapePath, err := content.ResolveCompiledAssetReference(ref.Path, path)
		if err != nil {
			return nil, err
		}
		shape, info, err := verification.LoadCompiledAssetShape(shapePath)
		if err != nil {
			return nil, err
		}
		if info.ContentID != ref.ContentID || info.EncodedBytes != ref.EncodedBytes || info.DecodedBytes != ref.DecodedBytes || shape.Lattice != authoredVoxelShapeLattice(part.VoxelResolution) {
			return nil, fmt.Errorf("compiled shape reference identity, size or lattice mismatch")
		}
		identity, exists := identities[shape]
		if !exists {
			identity, _, err = content.CompiledAssetShapeBaseIdentity(shape, nil)
			if err != nil {
				return nil, err
			}
			identities[shape] = identity
		}
		if identity != ref.BaseIdentity {
			return nil, fmt.Errorf("compiled shape original base identity mismatch")
		}
		verified[part.ID] = verifiedCompiledAssetShape{definition: shape, baseIdentity: identity, contentID: ref.ContentID}
	}
	// This is the publication boundary: every geometry and animation reference
	// proof has completed, and the origin must still permit the operation.
	if !runtimeContentOriginOpen(loader) {
		return nil, fmt.Errorf("content origin scope is closed")
	}
	prepared := &PreparedAuthoredAsset{def: &definition, documentPath: path, animations: animations, parts: make(map[string]preparedAuthoredPart, len(definition.Parts))}
	for _, part := range definition.Parts {
		preparedPart := preparedAuthoredPart{}
		if assets != nil && part.Source.Kind == content.AssetSourceKindVoxelShape {
			shape := verified[part.ID]
			// C1 identity includes canonical primary geometry and lattice. Physical
			// closure location and the material palette do not change geometry sharing.
			key := "compiled-asset-shape:" + shape.contentID
			id, warm := assets.SharedVoxelGeometryByCacheKey(key)
			if !warm {
				geometry, _ := compiledShapeGeometry(shape.definition)
				id = assets.RegisterSharedVoxelGeometryWithCacheKey(key, geometry, key)
			}
			assets.recordVerifiedAuthoredVoxelBase(id, shape.definition.Lattice, shape.baseIdentity)
			palette, err := authoredVoxelShapePalette(assets, &definition, part)
			if err != nil {
				return nil, err
			}
			preparedPart = preparedAuthoredPart{model: id, palette: palette}
		}
		prepared.parts[part.ID] = preparedPart
	}
	if !runtimeContentOriginOpen(loader) {
		return nil, fmt.Errorf("content origin scope is closed")
	}
	return prepared, nil
}
