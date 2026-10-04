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

// The verification session owns metadata and animations, while shape definitions
// are borrowed under its private child scope. Callers close it after building or
// publishing independently owned geometry; no borrowed shape escapes the session.
type compiledAssetVerification struct {
	scope      *RuntimeContentLoadScope
	def        *content.AssetDef
	animations *content.ResolvedAssetAnimations
	shapes     map[string]verifiedCompiledAssetShape
}

func (session *compiledAssetVerification) close() {
	if session != nil {
		session.scope.Close()
	}
}

func checkCompiledAssetWork(loader *RuntimeContentLoader, cancelled func() bool) error {
	if cancelled != nil && cancelled() {
		return fmt.Errorf("compiled asset preparation cancelled")
	}
	if !runtimeContentOriginOpen(loader) {
		return fmt.Errorf("content origin scope is closed")
	}
	return nil
}

func verifyCompiledAssetInput(path string, loader *RuntimeContentLoader, cancelled func() bool) (*compiledAssetVerification, error) {
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	scope := loader.NewScope()
	success := false
	defer func() {
		if !success {
			scope.Close()
		}
	}()
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
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
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
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
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
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	success = true
	return &compiledAssetVerification{scope: scope, def: &definition, animations: animations, shapes: verified}, nil
}

func prepareCompiledAuthoredAsset(path string, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	session, err := verifyCompiledAssetInput(path, loader, nil)
	if err != nil {
		return nil, err
	}
	defer session.close()
	definition, animations := session.def, session.animations
	prepared := &PreparedAuthoredAsset{def: definition, documentPath: path, animations: animations, parts: make(map[string]preparedAuthoredPart, len(definition.Parts))}
	for _, part := range definition.Parts {
		preparedPart := preparedAuthoredPart{}
		if assets != nil && part.Source.Kind == content.AssetSourceKindVoxelShape {
			shape := session.shapes[part.ID]
			// C1 identity includes canonical primary geometry and lattice. Physical
			// closure location and the material palette do not change geometry sharing.
			key := "compiled-asset-shape:" + shape.contentID
			id, warm := assets.SharedVoxelGeometryByCacheKey(key)
			if !warm {
				geometry, _ := compiledShapeGeometry(shape.definition)
				id = assets.RegisterSharedVoxelGeometryWithCacheKey(key, geometry, key)
			}
			assets.recordVerifiedAuthoredVoxelBase(id, shape.definition.Lattice, shape.baseIdentity)
			palette, err := authoredVoxelShapePalette(assets, definition, part)
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
