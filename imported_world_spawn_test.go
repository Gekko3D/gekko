package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestImportedWorldPaletteAssetPreservesEmissiveMaterials(t *testing.T) {
	assets := newSpawnTestAssetServer()
	def := &content.ImportedWorldDef{
		WorldID: "world",
		Palette: []content.ImportedWorldPaletteColor{
			{0, 0, 0, 0},
			{240, 220, 120, 255},
		},
		Materials: []content.ImportedWorldMaterialDef{{
			ID:                10,
			PaletteIndex:      1,
			SourceTextureName: "LIGHT01",
			BaseColor:         content.ImportedWorldPaletteColor{240, 220, 120, 255},
			Kind:              "emissive",
			EmitsLight:        true,
			Emissive:          2.75,
		}},
	}

	paletteID := ImportedWorldPaletteAsset(assets, def)
	palette, ok := assets.GetVoxelPalette(paletteID)
	if !ok {
		t.Fatal("expected imported-world palette asset")
	}
	if palette.VoxPalette[1] != [4]uint8{240, 220, 120, 255} {
		t.Fatalf("palette color = %+v", palette.VoxPalette[1])
	}
	if len(palette.Materials) != 1 || palette.Materials[0].ID != 1 {
		t.Fatalf("materials = %+v", palette.Materials)
	}
	if got, ok := palette.Materials[0].Property["_type"]; !ok || got != "_emit" {
		t.Fatalf("expected emit material, got %+v", palette.Materials[0].Property)
	}
	if got, ok := palette.Materials[0].Property["_emit"]; !ok || got != float32(2.75) {
		t.Fatalf("expected emissive strength, got %+v", palette.Materials[0].Property)
	}
}

func TestImportedWorldPaletteAssetPrefersRuntimeMaterialPalette(t *testing.T) {
	assets := newSpawnTestAssetServer()
	def := &content.ImportedWorldDef{
		WorldID: "world",
		Palette: []content.ImportedWorldPaletteColor{
			{0, 0, 0, 0},
			{255, 0, 0, 255},
		},
		MaterialPalette: []content.ImportedWorldPaletteColor{
			{0, 0, 0, 0},
			{120, 180, 220, 255},
		},
		Materials: []content.ImportedWorldMaterialDef{{
			PaletteIndex: 1,
			Kind:         "glass",
			Transparent:  true,
			Transparency: 0.55,
		}},
	}

	paletteID := ImportedWorldPaletteAsset(assets, def)
	palette, ok := assets.GetVoxelPalette(paletteID)
	if !ok {
		t.Fatal("expected imported-world palette asset")
	}
	if palette.VoxPalette[1] != [4]uint8{120, 180, 220, 255} {
		t.Fatalf("expected material palette color, got %+v", palette.VoxPalette[1])
	}
	if len(palette.Materials) != 1 || palette.Materials[0].Property["_type"] != "_glass" {
		t.Fatalf("expected glass material, got %+v", palette.Materials)
	}
}

func TestImportedWorldVoxMaterialsPreservePBRHints(t *testing.T) {
	materials := importedWorldVoxMaterials([]content.ImportedWorldMaterialDef{
		{
			PaletteIndex: 2,
			Kind:         "metal",
			Roughness:    0.42,
			Metallic:     0.8,
		},
		{
			PaletteIndex: 3,
			Kind:         "water",
			Transparent:  true,
			Roughness:    0.12,
			Transparency: 0.62,
		},
	})
	if len(materials) != 2 {
		t.Fatalf("materials = %+v", materials)
	}
	metal := materials[0].Property
	if got := metal["_type"]; got != "_metal" {
		t.Fatalf("expected metal type, got %+v", metal)
	}
	if got := metal["_rough"]; got != float32(0.42) {
		t.Fatalf("expected roughness, got %+v", metal)
	}
	if got := metal["_metal"]; got != float32(0.8) {
		t.Fatalf("expected metallic, got %+v", metal)
	}
	glass := materials[1].Property
	if got := glass["_type"]; got != "_glass" {
		t.Fatalf("expected glass type, got %+v", glass)
	}
	if got := glass["_trans"]; got != float32(0.62) {
		t.Fatalf("expected transparency, got %+v", glass)
	}
}
