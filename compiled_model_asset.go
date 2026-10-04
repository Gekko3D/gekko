package gekko

import (
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

// CompiledAssetModelCompileResult adds unique model frame counts without changing
// the existing inline compiler's exported result structs.
type CompiledAssetModelCompileResult struct {
	CompiledAssetCompileDetailedResult
	ModelsWritten, ModelsReused int
}

// CompileAuthoredModelAsset emits a complete explicit model-header closure with
// inline shapes, groups, procedural primitives and selected VOX model/scene parts.
// Authoring inputs must remain stable. Explicit codecs remain caller-owned.
func CompileAuthoredModelAsset(inputPath, outputPath string, codec *voxelcodec.Codec) (CompiledAssetModelCompileResult, error) {
	return CompileAuthoredModelAssetWithOptions(inputPath, outputPath, codec, CompiledAssetCompileOptions{})
}

// CompileAuthoredModelAssetWithOptions accepts the existing inline LOD opt-in.
// Model derivatives and static collapse remain unsupported by this format.
func CompileAuthoredModelAssetWithOptions(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions) (CompiledAssetModelCompileResult, error) {
	if filepath.Ext(outputPath) != ".gkmodelassetc" {
		return CompiledAssetModelCompileResult{}, fmt.Errorf("compiled model output must end in lowercase .gkmodelassetc")
	}
	result, err := compileAuthoredAssetClosure(inputPath, outputPath, codec, options, true)
	if err != nil {
		return CompiledAssetModelCompileResult{}, err
	}
	return result, nil
}

func compiledModelPaletteDefinition(palette *VoxelPaletteAsset) (content.CompiledAssetModelPaletteDef, error) {
	if palette == nil || palette.Animations != nil || palette.MaterialFrameOverrides != nil {
		return content.CompiledAssetModelPaletteDef{}, fmt.Errorf("compiled model palette must be static")
	}
	definition := content.CompiledAssetModelPaletteDef{Colors: palette.VoxPalette, IsPBR: palette.IsPBR, Roughness: palette.Roughness, Metalness: palette.Metalness, Emission: palette.Emission, IOR: palette.IOR, Transparency: palette.Transparency}
	if palette.Materials != nil {
		definition.Materials = make([]content.CompiledAssetModelMaterialDef, len(palette.Materials))
		for i, material := range palette.Materials {
			definition.Materials[i] = content.CompiledAssetModelMaterialDef{ID: material.ID, Type: material.Type, Weight: material.Weight}
			if material.Property != nil {
				definition.Materials[i].Property = make(map[string]any, len(material.Property))
				for key, value := range material.Property {
					definition.Materials[i].Property[key] = value
				}
			}
		}
	}
	if palette.SurfaceMaterials != nil {
		definition.SurfaceMaterials = make(map[uint8]content.CompiledAssetModelSurfaceMaterialDef, len(palette.SurfaceMaterials))
		for index, material := range palette.SurfaceMaterials {
			definition.SurfaceMaterials[index] = content.CompiledAssetModelSurfaceMaterialDef{Kind: material.Kind, Tags: cloneCompiledPaletteSlice(material.Tags)}
		}
	}
	id, err := content.CompiledAssetModelPaletteIdentity(&definition)
	if err != nil {
		return content.CompiledAssetModelPaletteDef{}, err
	}
	definition.ID = id
	return definition, nil
}

func compileAssetModelPartIntoHeader(asset *content.AssetDef, part content.AssetPartDef, path string, codec *voxelcodec.Codec, header *content.CompiledAssetModelHeaderDef, palettes map[string]struct{}, add func([]byte, string, compiledAssetFileKind) string) ([]string, error) {
	source, err := compileAssetPartModel(asset, part, path)
	if err != nil {
		return nil, err
	}
	data, info, err := content.EncodeCompiledAssetModel(source.definition, codec)
	if err != nil {
		return nil, err
	}
	base, _, err := content.CompiledAssetModelBaseIdentity(source.definition, codec)
	if err != nil {
		return nil, err
	}
	palette, err := compiledModelPaletteDefinition(&source.palette)
	if err != nil {
		return nil, err
	}
	if _, exists := palettes[palette.ID]; !exists {
		header.Palettes = append(header.Palettes, palette)
		palettes[palette.ID] = struct{}{}
	}
	relative := add(data, ".gkmodel", compiledAssetModelFile)
	header.Models = append(header.Models, content.CompiledAssetModelRefDef{PartID: part.ID, Path: relative, ContentID: info.ContentID, BaseIdentity: base, EncodedBytes: info.EncodedBytes, DecodedBytes: info.DecodedBytes, PaletteID: palette.ID})
	return source.dependencies, nil
}
