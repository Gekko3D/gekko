package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// The verified static DTO contains scalar properties only. Preserve nil/empty
// containers while owning all mutable tables independently of the session.
func compiledModelRuntimePalette(definition *content.CompiledAssetModelPaletteDef) VoxelPaletteAsset {
	if definition == nil {
		return VoxelPaletteAsset{}
	}
	palette := VoxelPaletteAsset{VoxPalette: definition.Colors, IsPBR: definition.IsPBR, Roughness: definition.Roughness, Metalness: definition.Metalness, Emission: definition.Emission, IOR: definition.IOR, Transparency: definition.Transparency}
	if definition.Materials != nil {
		palette.Materials = make([]VoxMaterial, len(definition.Materials))
		for i, material := range definition.Materials {
			palette.Materials[i] = VoxMaterial{ID: material.ID, Type: material.Type, Weight: material.Weight}
			if material.Property != nil {
				palette.Materials[i].Property = make(map[string]interface{}, len(material.Property))
				for key, value := range material.Property {
					palette.Materials[i].Property[key] = value
				}
			}
		}
	}
	if definition.SurfaceMaterials != nil {
		palette.SurfaceMaterials = make(map[uint8]VoxelSurfaceMaterial, len(definition.SurfaceMaterials))
		for index, surface := range definition.SurfaceMaterials {
			palette.SurfaceMaterials[index] = VoxelSurfaceMaterial{Kind: surface.Kind, Tags: cloneCompiledPaletteSlice(surface.Tags)}
		}
	}
	return palette
}

func prepareCompiledAssetPacketModel(packet *compiledAssetPacket, partID string, verified verifiedCompiledAssetModel, paletteBindings map[string]string, loader *RuntimeContentLoader, cancelled func() bool) error {
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return err
	}
	packet.parts[partID] = verified.contentID
	if _, exists := packet.shapes[verified.contentID]; !exists {
		// This borrowed view only drives the primary conversion; model dimensions
		// travel separately and are not substituted for occupied map bounds.
		source, _ := compiledShapeGeometry(&content.CompiledAssetShapeDef{Bricks: verified.definition.Bricks})
		registration := prepareStreamedGeometryRegistration(source)
		packet.shapes[verified.contentID] = &compiledAssetPacketShape{contentID: verified.contentID, lattice: verified.definition.Lattice, baseIdentity: verified.baseIdentity, model: true, dimensions: verified.definition.Dimensions, source: source, registration: registration}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return err
	}
	binding := "model:" + verified.palette.ID
	if key, exists := paletteBindings[binding]; exists {
		packet.partPalettes[partID] = key
		return nil
	}
	palette := compiledModelRuntimePalette(verified.palette)
	registration, err := prepareCompiledPaletteRegistration(&palette)
	if err != nil {
		return err
	}
	key := registration.key
	if _, exists := packet.palettes[key]; exists {
		registration.release()
	} else {
		packet.palettes[key] = &compiledAssetPacketPalette{source: &palette, registration: registration}
	}
	paletteBindings[binding] = key
	packet.partPalettes[partID] = key
	return checkCompiledAssetWork(loader, cancelled)
}

func compiledAssetPacketGeometryKey(shape *compiledAssetPacketShape) string {
	if shape.model {
		return "compiled-asset-model:" + shape.contentID
	}
	return "compiled-asset-shape:" + shape.contentID
}

func adoptCompiledAssetPacketGeometry(assets *AssetServer, shape *compiledAssetPacketShape, source *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool) {
	if shape.model {
		return assets.adoptCompiledAssetModelGeometry(shape.contentID, shape.lattice, shape.baseIdentity, shape.dimensions, source, registration)
	}
	return assets.adoptCompiledAssetGeometry(shape.contentID, shape.lattice, shape.baseIdentity, source, registration)
}

func adoptCompiledAssetPacketGeometryOutcome(assets *AssetServer, shape *compiledAssetPacketShape, source *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool, bool) {
	if shape.model {
		return assets.adoptCompiledAssetModelGeometryOutcome(shape.contentID, shape.lattice, shape.baseIdentity, shape.dimensions, source, registration)
	}
	return assets.adoptCompiledAssetGeometryOutcome(shape.contentID, shape.lattice, shape.baseIdentity, source, registration)
}

// Caller holds assets.mu for the whole packet's publication.
func adoptCompiledAssetPacketGeometryOutcomeLocked(assets *AssetServer, shape *compiledAssetPacketShape, source *volume.XBrickMap, registration *streamedGeometryRegistration) (AssetId, bool, bool) {
	if shape.model {
		return assets.adoptCompiledAssetModelGeometryOutcomeLocked(shape.contentID, shape.lattice, shape.baseIdentity, shape.dimensions, source, registration)
	}
	return assets.adoptCompiledAssetGeometryOutcomeLocked(shape.contentID, shape.lattice, shape.baseIdentity, source, registration)
}
