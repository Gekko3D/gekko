package hl1

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestApplyHL1MaterialAnimationsBuildsPaletteSequence(t *testing.T) {
	store := &TextureStore{byName: map[string]TexturePixels{
		"+0light": {
			Name:   "+0LIGHT",
			Width:  2,
			Height: 1,
			Pixels: []byte{0, 1},
			Colors: [][3]uint8{{200, 20, 20}, {20, 20, 200}},
		},
		"+1light": {
			Name:   "+1LIGHT",
			Width:  2,
			Height: 1,
			Pixels: []byte{0, 1},
			Colors: [][3]uint8{{20, 200, 20}, {220, 220, 40}},
		},
	}}
	manifest := &content.ImportedWorldDef{
		Materials: []content.ImportedWorldMaterialDef{
			{PaletteIndex: 7, AnimationID: "hl1.texture.light", SourceTextureName: "+0LIGHT", BaseColor: content.ImportedWorldPaletteColor{200, 20, 20, 255}},
			{PaletteIndex: 8, AnimationID: "hl1.texture.light", SourceTextureName: "+0LIGHT", BaseColor: content.ImportedWorldPaletteColor{20, 20, 200, 255}},
		},
	}

	diagnostics := ApplyHL1MaterialAnimations(manifest, store)

	if len(manifest.MaterialAnimations) != 1 {
		t.Fatalf("animations = %+v", manifest.MaterialAnimations)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	animation := manifest.MaterialAnimations[0]
	if animation.ID != "hl1.texture.light" || animation.Kind != "palette_sequence" || animation.FPS != hl1TextureAnimationFPS {
		t.Fatalf("animation metadata = %+v", animation)
	}
	if len(animation.PaletteIndices) != 2 || animation.PaletteIndices[0] != 7 || animation.PaletteIndices[1] != 8 {
		t.Fatalf("palette indices = %+v", animation.PaletteIndices)
	}
	if len(animation.Frames) != 2 {
		t.Fatalf("frames = %+v", animation.Frames)
	}
	if animation.Frames[1].Colors[0] != (content.ImportedWorldPaletteColor{20, 200, 20, 255}) {
		t.Fatalf("frame 1 target 0 color = %+v", animation.Frames[1].Colors[0])
	}
	if animation.Frames[1].Colors[1] != (content.ImportedWorldPaletteColor{220, 220, 40, 255}) {
		t.Fatalf("frame 1 target 1 color = %+v", animation.Frames[1].Colors[1])
	}
}

func TestHL1AnimatedTextureGroupIgnoresAlternateLetterSequencesForNow(t *testing.T) {
	if _, ok := hl1AnimatedTextureGroup("+aLIGHT"); ok {
		t.Fatalf("alternate +a texture sequence should not be imported as a normal loop yet")
	}
	if id := hl1TextureAnimationID("+0LIGHT"); id != "hl1.texture.light" {
		t.Fatalf("animation id = %q", id)
	}
}

func TestHL1TextureLooksScrollableOnlyAcceptsExplicitScrollPrefix(t *testing.T) {
	if !hl1TextureLooksScrollable("scroll_conv3") {
		t.Fatalf("scroll_conv3 should be treated as scrollable")
	}
	for _, name := range []string{"c2a4_conv1", "c2a4_conv2", "conveyor_frame", "belt_trim"} {
		if hl1TextureLooksScrollable(name) {
			t.Fatalf("%s should not be treated as scrollable from name alone", name)
		}
	}
}

func TestApplyHL1MaterialAnimationsReportsUnsupportedAlternateOnlySequence(t *testing.T) {
	store := &TextureStore{byName: map[string]TexturePixels{
		"+0light": {
			Name:   "+0LIGHT",
			Width:  1,
			Height: 1,
			Pixels: []byte{0},
			Colors: [][3]uint8{{200, 20, 20}},
		},
		"+alight": {
			Name:   "+aLIGHT",
			Width:  1,
			Height: 1,
			Pixels: []byte{0},
			Colors: [][3]uint8{{20, 20, 20}},
		},
	}}
	manifest := &content.ImportedWorldDef{
		Materials: []content.ImportedWorldMaterialDef{
			{PaletteIndex: 7, AnimationID: "hl1.texture.light", SourceTextureName: "+0LIGHT", BaseColor: content.ImportedWorldPaletteColor{200, 20, 20, 255}},
		},
	}

	diagnostics := ApplyHL1MaterialAnimations(manifest, store)

	if len(manifest.MaterialAnimations) != 0 {
		t.Fatalf("animations = %+v", manifest.MaterialAnimations)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "hl1.material_animation_unsupported_sequence" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestApplyHL1ScrollMaterialAnimationsBuildsPaletteScroll(t *testing.T) {
	store := &TextureStore{byName: map[string]TexturePixels{
		"scroll_conv3": scrollTestTexture(),
	}}
	manifest := &content.ImportedWorldDef{
		Materials: []content.ImportedWorldMaterialDef{
			{PaletteIndex: 21, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.scroll_conv3", AnimationPhase: 0, BaseColor: content.ImportedWorldPaletteColor{10, 0, 0, 255}},
			{PaletteIndex: 22, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.scroll_conv3", AnimationPhase: 1, BaseColor: content.ImportedWorldPaletteColor{20, 0, 0, 255}},
		},
	}

	diagnostics := ApplyHL1ScrollMaterialAnimations(manifest, store)

	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(manifest.MaterialAnimations) != 1 {
		t.Fatalf("animations = %+v", manifest.MaterialAnimations)
	}
	animation := manifest.MaterialAnimations[0]
	if animation.ID != "hl1.scroll.scroll_conv3" || animation.Kind != "palette_scroll" || animation.FPS != hl1ScrollAnimationFPS {
		t.Fatalf("animation metadata = %+v", animation)
	}
	if animation.UVScroll == nil || animation.UVScroll.Velocity != ([2]float32{1, 0}) {
		t.Fatalf("animation uv scroll = %+v", animation.UVScroll)
	}
	if len(animation.Frames) != hl1ScrollPhaseCount {
		t.Fatalf("frames = %d", len(animation.Frames))
	}
	if animation.Frames[0].Colors[0] != (content.ImportedWorldPaletteColor{10, 0, 0, 255}) {
		t.Fatalf("frame 0 phase 0 color = %+v", animation.Frames[0].Colors[0])
	}
	if animation.Frames[1].Colors[0] != (content.ImportedWorldPaletteColor{20, 0, 0, 255}) {
		t.Fatalf("frame 1 phase 0 color = %+v", animation.Frames[1].Colors[0])
	}
}

func TestApplyHL1ScrollMaterialAnimationsCanScrollVAxis(t *testing.T) {
	store := &TextureStore{byName: map[string]TexturePixels{
		"scroll_conv3": verticalScrollTestTexture(),
	}}
	manifest := &content.ImportedWorldDef{
		Materials: []content.ImportedWorldMaterialDef{
			{PaletteIndex: 31, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.v.scroll_conv3", AnimationPhase: 0, BaseColor: content.ImportedWorldPaletteColor{10, 0, 0, 255}},
			{PaletteIndex: 32, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.v.scroll_conv3", AnimationPhase: 1, BaseColor: content.ImportedWorldPaletteColor{20, 0, 0, 255}},
		},
	}

	diagnostics := ApplyHL1ScrollMaterialAnimations(manifest, store)

	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(manifest.MaterialAnimations) != 1 {
		t.Fatalf("animations = %+v", manifest.MaterialAnimations)
	}
	animation := manifest.MaterialAnimations[0]
	if animation.ID != "hl1.scroll.v.scroll_conv3" || animation.Kind != "palette_scroll" {
		t.Fatalf("animation metadata = %+v", animation)
	}
	if animation.UVScroll == nil || animation.UVScroll.Velocity != ([2]float32{0, 1}) {
		t.Fatalf("animation uv scroll = %+v", animation.UVScroll)
	}
	if animation.Frames[0].Colors[0] != (content.ImportedWorldPaletteColor{10, 0, 0, 255}) {
		t.Fatalf("frame 0 phase 0 color = %+v", animation.Frames[0].Colors[0])
	}
	if animation.Frames[1].Colors[0] != (content.ImportedWorldPaletteColor{20, 0, 0, 255}) {
		t.Fatalf("frame 1 phase 0 color = %+v", animation.Frames[1].Colors[0])
	}
}

func TestHL1ScrollAssetMaterialAnimationsUseMaterializedPhasePalette(t *testing.T) {
	store := &TextureStore{byName: map[string]TexturePixels{
		"scroll_conv3": scrollTestTexture(),
	}}
	result := VoxelizeResult{
		Materials: []importcommon.Material{{
			ID:           1,
			PaletteIndex: 1,
			BaseColor:    [4]uint8{10, 0, 0, 255},
			Kind:         "baked_texture",
			Roughness:    0.9,
		}},
		Voxels: []importcommon.Voxel{
			{X: 0, Palette: 1, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.scroll_conv3", AnimationPhase: 0},
			{X: 1, Palette: 1, SourceTextureName: "scroll_conv3", AnimationID: "hl1.scroll.scroll_conv3", AnimationPhase: 1},
		},
	}

	materializeHL1ScrollAssetVoxels(&result)
	animations := HL1ScrollAssetMaterialAnimations(result.Materials, store)

	if result.Voxels[0].Palette == 1 || result.Voxels[1].Palette == 1 || result.Voxels[0].Palette == result.Voxels[1].Palette {
		t.Fatalf("expected separate animated palette values, got voxels %+v", result.Voxels)
	}
	if len(animations) != 1 {
		t.Fatalf("animations = %+v", animations)
	}
	animation := animations[0]
	if animation.Kind != "palette_scroll" || len(animation.PaletteIndices) != 2 || len(animation.Frames) != hl1ScrollPhaseCount {
		t.Fatalf("animation = %+v", animation)
	}
	if animation.UVScroll == nil || animation.UVScroll.Velocity != ([2]float32{1, 0}) {
		t.Fatalf("animation uv scroll = %+v", animation.UVScroll)
	}
	if animation.PaletteIndices[0] != result.Voxels[0].Palette || animation.PaletteIndices[1] != result.Voxels[1].Palette {
		t.Fatalf("animation palette indices = %+v, voxels = %+v", animation.PaletteIndices, result.Voxels)
	}
}

func verticalScrollTestTexture() TexturePixels {
	return TexturePixels{
		Name:   "scroll_conv3",
		Width:  1,
		Height: 8,
		Pixels: []byte{0, 1, 2, 3, 4, 5, 6, 7},
		Colors: [][3]uint8{
			{10, 0, 0},
			{20, 0, 0},
			{30, 0, 0},
			{40, 0, 0},
			{50, 0, 0},
			{60, 0, 0},
			{70, 0, 0},
			{80, 0, 0},
		},
	}
}

func scrollTestTexture() TexturePixels {
	return TexturePixels{
		Name:   "scroll_conv3",
		Width:  8,
		Height: 1,
		Pixels: []byte{0, 1, 2, 3, 4, 5, 6, 7},
		Colors: [][3]uint8{
			{10, 0, 0},
			{20, 0, 0},
			{30, 0, 0},
			{40, 0, 0},
			{50, 0, 0},
			{60, 0, 0},
			{70, 0, 0},
			{80, 0, 0},
		},
	}
}
