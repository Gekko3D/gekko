package hl1

import (
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

type TextureStore struct {
	byName map[string]TexturePixels
}

func NewTextureStore(textures []Texture, wads []*WAD) *TextureStore {
	store := &TextureStore{byName: make(map[string]TexturePixels)}
	for _, texture := range textures {
		if texture.Name == "" {
			continue
		}
		if texture.Pixels.Width > 0 {
			store.byName[strings.ToLower(texture.Name)] = texture.Pixels
		}
	}
	for _, wad := range wads {
		if wad == nil {
			continue
		}
		for _, entry := range wad.Entries {
			if entry.Name == "" || entry.Compression != 0 {
				continue
			}
			key := strings.ToLower(entry.Name)
			if _, exists := store.byName[key]; exists {
				continue
			}
			if texture, ok := wad.TexturePixels(entry.Name); ok {
				store.byName[key] = texture
			}
		}
	}
	return store
}

func (store *TextureStore) Sample(name string, u, v float32) ([4]uint8, bool) {
	sample, ok := store.SampleTexel(name, u, v)
	return sample.Color, ok
}

func (store *TextureStore) SampleTexel(name string, u, v float32) (TextureSample, bool) {
	if store == nil || name == "" {
		return TextureSample{}, false
	}
	texture, ok := store.byName[strings.ToLower(name)]
	if !ok {
		return TextureSample{}, false
	}
	return texture.SampleTexel(u, v)
}

func FixedBakedPaletteMaterials() []importcommon.Material {
	materials := make([]importcommon.Material, 0, 255)
	for rBin := 0; rBin < bakedPaletteRBinCount; rBin++ {
		for gBin := 0; gBin < bakedPaletteGBinCount; gBin++ {
			for bBin := 0; bBin < bakedPaletteBBinCount; bBin++ {
				index := bakedPaletteIndexFromBins(rBin, gBin, bBin)
				materials = append(materials, importcommon.Material{
					ID:           index,
					PaletteIndex: uint8(index),
					BaseColor: [4]uint8{
						uint8((rBin*255 + (bakedPaletteRBinCount-1)/2) / (bakedPaletteRBinCount - 1)),
						uint8((gBin*255 + (bakedPaletteGBinCount-1)/2) / (bakedPaletteGBinCount - 1)),
						uint8((bBin*255 + (bakedPaletteBBinCount-1)/2) / (bakedPaletteBBinCount - 1)),
						255,
					},
					Kind:      "baked_texture",
					Roughness: 0.9,
					Tags:      []string{"source:hl1", "material:baked_texture"},
				})
			}
		}
	}
	for _, material := range fixedEmissivePaletteMaterials() {
		materials = append(materials, material)
	}
	return materials
}

func fixedEmissivePaletteMaterials() []importcommon.Material {
	tones := []struct {
		lowColor    [3]uint8
		highColor   [3]uint8
		maxEmissive float32
	}{
		{lowColor: [3]uint8{128, 92, 48}, highColor: [3]uint8{255, 224, 132}, maxEmissive: 3.0},
		{lowColor: [3]uint8{92, 124, 168}, highColor: [3]uint8{188, 224, 255}, maxEmissive: 2.6},
		{lowColor: [3]uint8{120, 120, 112}, highColor: [3]uint8{248, 248, 228}, maxEmissive: 2.8},
	}
	materials := make([]importcommon.Material, 0, emissiveToneCount*emissiveRampLevels)
	for tone, def := range tones {
		for level := 0; level < emissiveRampLevels; level++ {
			index := emissivePaletteIndexForToneLevel(tone, level)
			f := float32(level) / float32(emissiveRampLevels-1)
			materials = append(materials, importcommon.Material{
				ID:           int(index),
				PaletteIndex: index,
				BaseColor: [4]uint8{
					lerpByte(def.lowColor[0], def.highColor[0], level, emissiveRampLevels-1),
					lerpByte(def.lowColor[1], def.highColor[1], level, emissiveRampLevels-1),
					lerpByte(def.lowColor[2], def.highColor[2], level, emissiveRampLevels-1),
					255,
				},
				Kind:       "baked_texture_emissive",
				EmitsLight: true,
				Emissive:   0.6 + f*(def.maxEmissive-0.6),
				Roughness:  0.45,
				Tags:       []string{"source:hl1", "material:baked_texture", "material:emissive"},
			})
		}
	}
	return materials
}

const (
	bakedPaletteRBinCount = 6
	bakedPaletteGBinCount = 6
	bakedPaletteBBinCount = 6
	bakedPaletteBinCount  = bakedPaletteRBinCount * bakedPaletteGBinCount * bakedPaletteBBinCount
	emissivePaletteStart  = bakedPaletteBinCount + 1
	emissiveRampLevels    = 13
	emissiveToneCount     = 3

	emissiveWarmTone    = 0
	emissiveCoolTone    = 1
	emissiveNeutralTone = 2

	emissiveWarmPaletteIndex    uint8 = emissivePaletteStart + emissiveRampLevels - 1
	emissiveCoolPaletteIndex    uint8 = emissivePaletteStart + emissiveRampLevels*2 - 1
	emissiveNeutralPaletteIndex uint8 = emissivePaletteStart + emissiveRampLevels*3 - 1
)

func bakedPaletteIndex(color [4]uint8) int {
	rBin := min(int(color[0])*bakedPaletteRBinCount/256, bakedPaletteRBinCount-1)
	gBin := min(int(color[1])*bakedPaletteGBinCount/256, bakedPaletteGBinCount-1)
	bBin := min(int(color[2])*bakedPaletteBBinCount/256, bakedPaletteBBinCount-1)
	return bakedPaletteIndexFromBins(rBin, gBin, bBin)
}

func bakedPaletteIndexFromBins(rBin, gBin, bBin int) int {
	return 1 + (rBin*bakedPaletteGBinCount+gBin)*bakedPaletteBBinCount + bBin
}

func emissivePaletteIndexForToneLevel(tone, level int) uint8 {
	tone = max(0, min(tone, emissiveToneCount-1))
	level = max(0, min(level, emissiveRampLevels-1))
	return uint8(emissivePaletteStart + tone*emissiveRampLevels + level)
}

func lerpByte(low, high uint8, step, maxStep int) uint8 {
	if maxStep <= 0 {
		return high
	}
	return uint8((int(low)*(maxStep-step) + int(high)*step + maxStep/2) / maxStep)
}
