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
	materials := make([]importcommon.Material, 0, 252)
	for rBin := 0; rBin < 6; rBin++ {
		for gBin := 0; gBin < 7; gBin++ {
			for bBin := 0; bBin < 6; bBin++ {
				index := bakedPaletteIndexFromBins(rBin, gBin, bBin)
				materials = append(materials, importcommon.Material{
					ID:           index,
					PaletteIndex: uint8(index),
					BaseColor: [4]uint8{
						uint8((rBin*255 + 2) / 5),
						uint8((gBin*255 + 3) / 6),
						uint8((bBin*255 + 2) / 5),
						255,
					},
					Kind: "baked_texture",
				})
			}
		}
	}
	return materials
}

func bakedPaletteIndex(color [4]uint8) int {
	rBin := int(color[0]) * 6 / 256
	gBin := int(color[1]) * 7 / 256
	bBin := int(color[2]) * 6 / 256
	if rBin > 5 {
		rBin = 5
	}
	if gBin > 6 {
		gBin = 6
	}
	if bBin > 5 {
		bBin = 5
	}
	return bakedPaletteIndexFromBins(rBin, gBin, bBin)
}

func bakedPaletteIndexFromBins(rBin, gBin, bBin int) int {
	return 1 + (rBin*7+gBin)*6 + bBin
}
