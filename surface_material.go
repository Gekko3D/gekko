package gekko

import (
	"strings"

	"github.com/gekko3d/gekko/content"
)

type SurfaceMaterialFacts struct {
	PaletteValue uint8
	Kind         string
	Tags         []string
}

func (facts SurfaceMaterialFacts) HasTag(tag string) bool {
	want := strings.ToLower(strings.TrimSpace(tag))
	for _, got := range facts.Tags {
		if got == want {
			return true
		}
	}
	return false
}

func SurfaceMaterialForRaycastHit(cmd *Commands, assets *AssetServer, hit RaycastHit) (SurfaceMaterialFacts, bool) {
	if cmd == nil || assets == nil || !hit.Hit || hit.Entity == 0 || hit.PaletteIndex == 0 {
		return SurfaceMaterialFacts{}, false
	}
	model, ok := voxelModelComponentForEntity(cmd, hit.Entity)
	if !ok || model.VoxelPalette == (AssetId{}) {
		return SurfaceMaterialFacts{}, false
	}
	palette, ok := assets.GetVoxelPalette(model.VoxelPalette)
	if !ok {
		return SurfaceMaterialFacts{}, false
	}
	material, ok := palette.SurfaceMaterials[hit.PaletteIndex]
	if !ok {
		return SurfaceMaterialFacts{}, false
	}
	return SurfaceMaterialFacts{
		PaletteValue: hit.PaletteIndex,
		Kind:         material.Kind,
		Tags:         append([]string(nil), material.Tags...),
	}, true
}

func normalizeVoxelSurfaceMaterial(kind string, tags []string) (VoxelSurfaceMaterial, bool) {
	material := VoxelSurfaceMaterial{Kind: strings.ToLower(strings.TrimSpace(kind))}
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		material.Tags = append(material.Tags, tag)
		if material.Kind == "" && strings.HasPrefix(tag, "kind:") {
			material.Kind = strings.TrimSpace(strings.TrimPrefix(tag, "kind:"))
		}
	}
	return material, material.Kind != "" || len(material.Tags) != 0
}

func addVoxelSurfaceMaterial(materials map[uint8]VoxelSurfaceMaterial, paletteValue uint8, kind string, tags []string) map[uint8]VoxelSurfaceMaterial {
	material, ok := normalizeVoxelSurfaceMaterial(kind, tags)
	if paletteValue == 0 || !ok {
		return materials
	}
	if materials == nil {
		materials = make(map[uint8]VoxelSurfaceMaterial)
	}
	materials[paletteValue] = material
	return materials
}

func authoredVoxelSurfaceMaterial(material content.AssetMaterialDef) (VoxelSurfaceMaterial, bool) {
	return normalizeVoxelSurfaceMaterial("", material.Tags)
}

func authoredVoxelSurfaceMaterialsForModel(model VoxModel, material content.AssetMaterialDef) map[uint8]VoxelSurfaceMaterial {
	facts, ok := authoredVoxelSurfaceMaterial(material)
	if !ok {
		return nil
	}
	result := make(map[uint8]VoxelSurfaceMaterial)
	for _, voxel := range model.Voxels {
		if voxel.ColorIndex != 0 {
			result[voxel.ColorIndex] = facts
		}
	}
	return result
}

func createAuthoredMaterialVoxelPalette(assets *AssetServer, material content.AssetMaterialDef) AssetId {
	return assets.CreateVoxelPaletteAsset(buildAuthoredMaterialVoxelPalette(material))
}

func buildAuthoredMaterialVoxelPalette(material content.AssetMaterialDef) VoxelPaletteAsset {
	var palette VoxPalette
	for i := range palette {
		palette[i] = material.BaseColor
	}
	surfaceMaterials := addVoxelSurfaceMaterial(nil, 1, "", material.Tags)
	return VoxelPaletteAsset{
		VoxPalette:       palette,
		SurfaceMaterials: surfaceMaterials,
		IsPBR:            true,
		Roughness:        material.Roughness,
		Metalness:        material.Metallic,
		Emission:         material.Emissive,
		IOR:              material.IOR,
		Transparency:     material.Transparency,
	}
}
