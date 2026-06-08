package gekko

import (
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type ImportedWorldMaterialLookup struct {
	WorldID         string
	materials       [256]content.ImportedWorldMaterialDef
	hasMaterial     [256]bool
	sourceMaterials map[string]content.ImportedWorldMaterialDef
}

func NewImportedWorldMaterialLookup(def *content.ImportedWorldDef) ImportedWorldMaterialLookup {
	lookup := ImportedWorldMaterialLookup{}
	if def == nil {
		return lookup
	}
	lookup.WorldID = def.WorldID
	for _, material := range def.SourceMaterials {
		lookup.addMaterial(material)
	}
	for _, material := range def.Materials {
		lookup.addMaterial(material)
	}
	return lookup
}

func (lookup *ImportedWorldMaterialLookup) addMaterial(material content.ImportedWorldMaterialDef) {
	if material.PaletteIndex != 0 {
		lookup.materials[material.PaletteIndex] = material
		lookup.hasMaterial[material.PaletteIndex] = true
	}
	name := normalizeImportedWorldMaterialLookupTextureName(material.SourceTextureName)
	if name == "" {
		return
	}
	if lookup.sourceMaterials == nil {
		lookup.sourceMaterials = make(map[string]content.ImportedWorldMaterialDef)
	}
	lookup.sourceMaterials[name] = material
}

func (lookup ImportedWorldMaterialLookup) MaterialForPalette(paletteIndex uint8) (content.ImportedWorldMaterialDef, bool) {
	if paletteIndex == 0 || !lookup.hasMaterial[paletteIndex] {
		return content.ImportedWorldMaterialDef{}, false
	}
	return lookup.materials[paletteIndex], true
}

func (lookup ImportedWorldMaterialLookup) MaterialForSourceTexture(textureName string) (content.ImportedWorldMaterialDef, bool) {
	name := normalizeImportedWorldMaterialLookupTextureName(textureName)
	if name == "" || lookup.sourceMaterials == nil {
		return content.ImportedWorldMaterialDef{}, false
	}
	material, ok := lookup.sourceMaterials[name]
	return material, ok
}

func (lookup ImportedWorldMaterialLookup) MaterialForChunkVoxel(chunk *content.ImportedWorldChunkDef, local [3]int) (content.ImportedWorldMaterialDef, uint8, bool) {
	paletteIndex, ok := content.ImportedWorldChunkPaletteAt(chunk, local[0], local[1], local[2])
	if !ok {
		return content.ImportedWorldMaterialDef{}, 0, false
	}
	material, ok := lookup.MaterialForPalette(paletteIndex)
	return material, paletteIndex, ok
}

func (lookup ImportedWorldMaterialLookup) MaterialForXBrickMapVoxel(xbm *volume.XBrickMap, local [3]int) (content.ImportedWorldMaterialDef, uint8, bool) {
	if xbm == nil {
		return content.ImportedWorldMaterialDef{}, 0, false
	}
	found, paletteIndex := xbm.GetVoxel(local[0], local[1], local[2])
	if !found {
		return content.ImportedWorldMaterialDef{}, 0, false
	}
	material, ok := lookup.MaterialForPalette(paletteIndex)
	return material, paletteIndex, ok
}

func normalizeImportedWorldMaterialLookupTextureName(textureName string) string {
	return strings.ToUpper(strings.TrimSpace(textureName))
}
