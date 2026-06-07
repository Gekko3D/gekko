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
