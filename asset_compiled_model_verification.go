package gekko

import (
	"encoding/json"
	"fmt"

	"github.com/gekko3d/gekko/content"
)

// Model frames remain borrowed under the verification scope. Palette tables are
// owned by this session, independently of both cache metadata and other sessions.
func verifyCompiledAssetModels(header *content.CompiledAssetModelHeaderDef, parts map[string]content.AssetPartDef, verification, origin *RuntimeContentLoader, path string, cancelled func() bool) (map[string]verifiedCompiledAssetModel, error) {
	if header == nil {
		return nil, nil
	}
	palettes := make(map[string]*content.CompiledAssetModelPaletteDef, len(header.Palettes))
	for i := range header.Palettes {
		if err := checkCompiledAssetWork(origin, cancelled); err != nil {
			return nil, err
		}
		data, err := json.Marshal(&header.Palettes[i])
		if err != nil {
			return nil, err
		}
		var palette content.CompiledAssetModelPaletteDef
		if err := json.Unmarshal(data, &palette); err != nil {
			return nil, err
		}
		palettes[palette.ID] = &palette
		if err := checkCompiledAssetWork(origin, cancelled); err != nil {
			return nil, err
		}
	}
	models := make(map[string]verifiedCompiledAssetModel, len(header.Models))
	identities := make(map[*content.CompiledAssetModelDef]string)
	for _, ref := range header.Models {
		if err := checkCompiledAssetWork(origin, cancelled); err != nil {
			return nil, err
		}
		part, exists := parts[ref.PartID]
		if !exists || (part.Source.Kind != content.AssetSourceKindProceduralPrimitive && part.Source.Kind != content.AssetSourceKindVoxModel && part.Source.Kind != content.AssetSourceKindVoxSceneNode) {
			return nil, fmt.Errorf("compiled model does not identify a model part")
		}
		modelPath, err := content.ResolveCompiledAssetReference(ref.Path, path)
		if err != nil {
			return nil, err
		}
		model, info, err := verification.LoadCompiledAssetModel(modelPath)
		if err != nil {
			return nil, err
		}
		if err := checkCompiledAssetWork(origin, cancelled); err != nil {
			return nil, err
		}
		lattice := content.VoxelObjectLatticeDef{VoxelResolution: part.VoxelResolution, RasterizationVersion: compiledAssetModelRasterizationVersion}
		if info.ContentID != ref.ContentID || info.EncodedBytes != ref.EncodedBytes || info.DecodedBytes != ref.DecodedBytes || model.Lattice != lattice {
			return nil, fmt.Errorf("compiled model reference identity, size or lattice mismatch")
		}
		identity, exists := identities[model]
		if !exists {
			identity, _, err = content.CompiledAssetModelBaseIdentity(model, nil)
			if err != nil {
				return nil, err
			}
			identities[model] = identity
		}
		if identity != ref.BaseIdentity {
			return nil, fmt.Errorf("compiled model original base identity mismatch")
		}
		palette := palettes[ref.PaletteID]
		if palette == nil {
			return nil, fmt.Errorf("compiled model palette is missing")
		}
		models[ref.PartID] = verifiedCompiledAssetModel{definition: model, contentID: info.ContentID, baseIdentity: identity, palette: palette}
		if err := checkCompiledAssetWork(origin, cancelled); err != nil {
			return nil, err
		}
	}
	return models, nil
}
