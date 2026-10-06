package gekko

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
)

type verifiedCompiledAssetShape struct {
	definition       *content.CompiledAssetShapeDef
	baseIdentity     string
	baseDecodedBytes int64
	contentID        string
}

type verifiedCompiledAssetModel struct {
	definition   *content.CompiledAssetModelDef
	contentID    string
	baseIdentity string
	palette      *content.CompiledAssetModelPaletteDef
}

type verifiedCompiledAssetLOD struct {
	definition *content.CompiledAssetLODDef
	contentID  string
}

// The verification session owns metadata and animations, while shape and LOD definitions
// are borrowed under its private child scope. Callers close it after building or
// publishing independently owned geometry; no borrowed frame escapes the session.
type compiledAssetVerification struct {
	scope      *RuntimeContentLoadScope
	def        *content.AssetDef
	animations *content.ResolvedAssetAnimations
	shapes     map[string]verifiedCompiledAssetShape
	lods       map[string]verifiedCompiledAssetLOD
	models     map[string]verifiedCompiledAssetModel
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
	var header *content.CompiledAssetHeaderDef
	var modelHeader *content.CompiledAssetModelHeaderDef
	var err error
	if filepath.Ext(path) == ".gkmodelassetc" {
		modelHeader, _, err = verification.LoadCompiledAssetModelHeader(path)
		if err == nil {
			// A borrowed structural view reuses inline/LOD verification only. The model
			// header's typed decoder already validated original metadata without source IO.
			header = &content.CompiledAssetHeaderDef{Asset: modelHeader.Asset, Shapes: modelHeader.Shapes, LODs: modelHeader.LODs}
		}
	} else {
		header, _, err = verification.LoadCompiledAssetHeader(path)
	}
	if err != nil {
		return nil, &authoredAssetInputLoadError{err}
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
	if modelHeader == nil {
		if validation := content.ValidateAsset(&definition, content.AssetValidationOptions{}); validation.HasErrors() {
			return nil, fmt.Errorf("compiled asset validation failed: %s", validation.Error())
		}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	animations, err := content.ResolveCompiledAssetAnimations(&definition, path)
	if err != nil {
		return nil, fmt.Errorf("compiled asset animation resolution failed: %w", err)
	}
	content.NormalizeAssetDef(&definition)
	// The typed model header already proved hierarchy without authoring IO.
	// Public hierarchy validation also checks source files, so retain it only
	// for the legacy route whose source kinds do not carry VOX references.
	if modelHeader == nil {
		if err := ValidateAssetHierarchy(&definition); err != nil {
			return nil, err
		}
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
	identityBytes := make(map[*content.CompiledAssetShapeDef]int64)
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
			identity, identityBytes[shape], err = content.CompiledAssetShapeBaseIdentity(shape, nil)
			if err != nil {
				return nil, err
			}
			identities[shape] = identity
		}
		if identity != ref.BaseIdentity {
			return nil, fmt.Errorf("compiled shape original base identity mismatch")
		}
		verified[part.ID] = verifiedCompiledAssetShape{definition: shape, baseIdentity: identity, baseDecodedBytes: identityBytes[shape], contentID: ref.ContentID}
	}
	models, err := verifyCompiledAssetModels(modelHeader, parts, verification, loader, path, cancelled)
	if err != nil {
		return nil, err
	}
	var lods map[string]verifiedCompiledAssetLOD
	if len(header.LODs) > 0 {
		lods = make(map[string]verifiedCompiledAssetLOD, len(header.LODs))
		// Authenticated logical identities permit reusing the exact OR proof only
		// within this immutable session. Every physical reference still loads and
		// verifies its own identity, sizes and declared source/reduction binding.
		type proofPair struct {
			sourceContentID string
			lodContentID    string
		}
		proofs := make(map[proofPair]struct{}, len(header.LODs))
		for _, ref := range header.LODs {
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			lodPath, err := content.ResolveCompiledAssetReference(ref.Path, path)
			if err != nil {
				return nil, err
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			lod, info, err := verification.LoadCompiledAssetLOD(lodPath)
			if err != nil {
				return nil, err
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			if info.ContentID != ref.ContentID || info.EncodedBytes != ref.EncodedBytes || info.DecodedBytes != ref.DecodedBytes || lod.SourceContentID != ref.SourceContentID || lod.Factor != ref.Factor || lod.ReductionVersion != ref.ReductionVersion {
				return nil, fmt.Errorf("compiled LOD reference identity, size or source/reduction mismatch")
			}
			source, exists := verified[ref.PartID]
			if !exists || source.contentID != lod.SourceContentID {
				return nil, fmt.Errorf("compiled LOD reference does not identify its verified source")
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			pair := proofPair{source.contentID, info.ContentID}
			if _, exists := proofs[pair]; !exists {
				if err := validateCompiledAssetLODSource(lod, source); err != nil {
					return nil, err
				}
				proofs[pair] = struct{}{}
			}
			if err := checkCompiledAssetWork(loader, cancelled); err != nil {
				return nil, err
			}
			lods[ref.PartID] = verifiedCompiledAssetLOD{definition: lod, contentID: info.ContentID}
		}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	success = true
	return &compiledAssetVerification{scope: scope, def: &definition, animations: animations, shapes: verified, lods: lods, models: models}, nil
}

func prepareCompiledAuthoredAsset(path string, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	session, err := verifyCompiledAssetInput(path, loader, nil)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if assets != nil && (len(session.lods) != 0 || len(session.models) != 0) {
		packet, err := prepareCompiledAssetPacketFromVerification(path, session, loader, nil, session.def.Parts)
		if err != nil {
			return nil, err
		}
		defer packet.release()
		return publishCompiledAssetPacket(packet, assets, loader)
	}
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
				registration := prepareStreamedGeometryRegistration(geometry)
				var adopted bool
				id, adopted = assets.adoptCompiledAssetGeometry(shape.contentID, shape.definition.Lattice, shape.baseIdentity, geometry, registration)
				registration.release()
				if !adopted {
					return nil, fmt.Errorf("compiled asset geometry adoption rejected")
				}
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
