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

func TestImportedWorldPaletteAssetPreservesMaterialAnimations(t *testing.T) {
	assets := newSpawnTestAssetServer()
	def := &content.ImportedWorldDef{
		WorldID: "world",
		MaterialPalette: []content.ImportedWorldPaletteColor{
			{0, 0, 0, 0},
			{10, 20, 30, 255},
		},
		MaterialAnimations: []content.ImportedWorldMaterialAnimationDef{{
			ID:             "hl1.texture.light",
			Kind:           "palette_sequence",
			FPS:            10,
			Mode:           "loop",
			PaletteIndices: []uint8{1},
			Frames: []content.ImportedWorldMaterialAnimationFrameDef{
				{Colors: []content.ImportedWorldPaletteColor{{10, 20, 30, 255}}},
				{
					Colors:         []content.ImportedWorldPaletteColor{{40, 50, 60, 255}},
					EmissiveColors: []content.ImportedWorldPaletteColor{{120, 90, 60, 255}},
					Emission:       []float32{1.5},
					Roughness:      []float32{0.3},
					Transparency:   []float32{0.2},
				},
			},
			UVScroll: &content.ImportedWorldMaterialUVScrollDef{Velocity: [2]float32{1, 0}},
		}},
	}

	paletteID := ImportedWorldPaletteAsset(assets, def)
	palette, ok := assets.GetVoxelPalette(paletteID)
	if !ok {
		t.Fatal("expected imported-world palette asset")
	}
	if len(palette.Animations) != 1 || palette.Animations[0].ID != "hl1.texture.light" {
		t.Fatalf("animations = %+v", palette.Animations)
	}
	if len(palette.Animations[0].Frames) != 2 || palette.Animations[0].Frames[1].Colors[0] != ([4]uint8{40, 50, 60, 255}) {
		t.Fatalf("animation frames = %+v", palette.Animations[0].Frames)
	}
	if palette.Animations[0].UVScroll == nil || palette.Animations[0].UVScroll.Velocity != ([2]float32{1, 0}) {
		t.Fatalf("animation uv scroll = %+v", palette.Animations[0].UVScroll)
	}
	frame := palette.Animations[0].Frames[1]
	if frame.EmissiveColors[0] != ([4]uint8{120, 90, 60, 255}) || frame.Emission[0] != 1.5 || frame.Roughness[0] != 0.3 || frame.Transparency[0] != 0.2 {
		t.Fatalf("animation material frame = %+v", frame)
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

func TestImportedWorldVoxMaterialsKeepCutoutBarsOpaque(t *testing.T) {
	materials := importedWorldVoxMaterials([]content.ImportedWorldMaterialDef{
		{
			PaletteIndex: 7,
			Kind:         "grate",
			Roughness:    0.55,
			Metallic:     0.65,
		},
		{
			PaletteIndex: 8,
			Kind:         "cutout",
			Roughness:    0.9,
		},
	})
	if len(materials) != 2 {
		t.Fatalf("materials = %+v", materials)
	}
	if got := materials[0].Property["_type"]; got != "_metal" {
		t.Fatalf("expected grate bars to render as opaque metal, got %+v", materials[0].Property)
	}
	if got := materials[1].Property["_type"]; got != "_diffuse" {
		t.Fatalf("expected generic cutout texels to render as opaque diffuse, got %+v", materials[1].Property)
	}
	if materials[0].Property["_trans"] != float32(0) || materials[1].Property["_trans"] != float32(0) {
		t.Fatalf("expected no bar transparency, got %+v", materials)
	}
}
