package hl1

import (
	"sort"
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

func AdaptiveBakedPaletteMaterials(colors [][4]uint8) ([]importcommon.Material, map[[4]uint8]uint8) {
	palette := adaptiveBakedPalette(colors, bakedPaletteBinCount)
	materials := make([]importcommon.Material, 0, len(palette)+emissiveToneCount*emissiveRampLevels)
	indexByColor := make(map[[4]uint8]uint8)
	for i, color := range palette {
		index := uint8(i + 1)
		materials = append(materials, importcommon.Material{
			ID:           int(index),
			PaletteIndex: index,
			BaseColor:    color,
			Kind:         "baked_texture",
			Roughness:    0.9,
			Tags:         []string{"source:hl1", "material:baked_texture", "palette:adaptive"},
		})
	}
	for _, color := range uniqueSortedColors(colors) {
		if len(palette) == 0 {
			break
		}
		indexByColor[color] = nearestAdaptivePaletteIndex(color, palette)
	}
	for _, material := range fixedEmissivePaletteMaterials() {
		materials = append(materials, material)
	}
	return materials, indexByColor
}

type adaptiveColorCount struct {
	Color [4]uint8
	Count int
}

func adaptiveBakedPalette(colors [][4]uint8, limit int) [][4]uint8 {
	if limit <= 0 {
		return nil
	}
	unique := countedSortedColors(colors)
	if len(unique) == 0 {
		return nil
	}
	if len(unique) <= limit {
		out := make([][4]uint8, 0, len(unique))
		for _, entry := range unique {
			out = append(out, entry.Color)
		}
		return out
	}
	boxes := []adaptiveColorBox{{Colors: unique}}
	for len(boxes) < limit {
		boxIndex := adaptiveBoxToSplit(boxes)
		if boxIndex < 0 {
			break
		}
		left, right, ok := splitAdaptiveColorBox(boxes[boxIndex])
		if !ok {
			break
		}
		boxes[boxIndex] = left
		boxes = append(boxes, right)
	}
	out := make([][4]uint8, 0, len(boxes))
	for _, box := range boxes {
		out = append(out, adaptiveBoxAverageColor(box))
	}
	sort.Slice(out, func(i, j int) bool {
		return compareRGBA(out[i], out[j]) < 0
	})
	return out
}

type adaptiveColorBox struct {
	Colors []adaptiveColorCount
}

func adaptiveBoxToSplit(boxes []adaptiveColorBox) int {
	best := -1
	bestScore := -1
	for i, box := range boxes {
		if len(box.Colors) < 2 {
			continue
		}
		rRange, gRange, bRange := adaptiveBoxRanges(box)
		score := max(max(rRange, gRange), bRange) * adaptiveBoxWeight(box)
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	return best
}

func splitAdaptiveColorBox(box adaptiveColorBox) (adaptiveColorBox, adaptiveColorBox, bool) {
	if len(box.Colors) < 2 {
		return adaptiveColorBox{}, adaptiveColorBox{}, false
	}
	axis := adaptiveBoxSplitAxis(box)
	colors := append([]adaptiveColorCount(nil), box.Colors...)
	sort.Slice(colors, func(i, j int) bool {
		a := adaptiveColorChannel(colors[i].Color, axis)
		b := adaptiveColorChannel(colors[j].Color, axis)
		if a != b {
			return a < b
		}
		return compareRGBA(colors[i].Color, colors[j].Color) < 0
	})
	total := 0
	for _, entry := range colors {
		total += entry.Count
	}
	accum := 0
	split := 1
	for i, entry := range colors[:len(colors)-1] {
		accum += entry.Count
		if accum*2 >= total {
			split = i + 1
			break
		}
	}
	return adaptiveColorBox{Colors: colors[:split]}, adaptiveColorBox{Colors: colors[split:]}, true
}

func adaptiveBoxSplitAxis(box adaptiveColorBox) int {
	rRange, gRange, bRange := adaptiveBoxRanges(box)
	if rRange >= gRange && rRange >= bRange {
		return 0
	}
	if gRange >= bRange {
		return 1
	}
	return 2
}

func adaptiveBoxRanges(box adaptiveColorBox) (int, int, int) {
	mins := [3]int{255, 255, 255}
	maxs := [3]int{0, 0, 0}
	for _, entry := range box.Colors {
		color := entry.Color
		for axis := 0; axis < 3; axis++ {
			value := int(adaptiveColorChannel(color, axis))
			if value < mins[axis] {
				mins[axis] = value
			}
			if value > maxs[axis] {
				maxs[axis] = value
			}
		}
	}
	return maxs[0] - mins[0], maxs[1] - mins[1], maxs[2] - mins[2]
}

func adaptiveBoxWeight(box adaptiveColorBox) int {
	total := 0
	for _, entry := range box.Colors {
		total += entry.Count
	}
	return total
}

func adaptiveBoxAverageColor(box adaptiveColorBox) [4]uint8 {
	var r, g, b, a, total int
	for _, entry := range box.Colors {
		r += int(entry.Color[0]) * entry.Count
		g += int(entry.Color[1]) * entry.Count
		b += int(entry.Color[2]) * entry.Count
		a += int(entry.Color[3]) * entry.Count
		total += entry.Count
	}
	if total <= 0 {
		return [4]uint8{180, 180, 180, 255}
	}
	return [4]uint8{uint8(r / total), uint8(g / total), uint8(b / total), uint8(a / total)}
}

func nearestAdaptivePaletteIndex(color [4]uint8, palette [][4]uint8) uint8 {
	bestIndex := 0
	bestDist := int(^uint(0) >> 1)
	for i, candidate := range palette {
		dr := int(color[0]) - int(candidate[0])
		dg := int(color[1]) - int(candidate[1])
		db := int(color[2]) - int(candidate[2])
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			bestIndex = i
			bestDist = dist
		}
	}
	return uint8(bestIndex + 1)
}

func countedSortedColors(colors [][4]uint8) []adaptiveColorCount {
	counts := make(map[[4]uint8]int)
	for _, color := range colors {
		if color[3] == 0 {
			continue
		}
		color[3] = 255
		counts[color]++
	}
	out := make([]adaptiveColorCount, 0, len(counts))
	for color, count := range counts {
		out = append(out, adaptiveColorCount{Color: color, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return compareRGBA(out[i].Color, out[j].Color) < 0
	})
	return out
}

func uniqueSortedColors(colors [][4]uint8) [][4]uint8 {
	counted := countedSortedColors(colors)
	out := make([][4]uint8, 0, len(counted))
	for _, entry := range counted {
		out = append(out, entry.Color)
	}
	return out
}

func adaptiveColorChannel(color [4]uint8, axis int) uint8 {
	switch axis {
	case 0:
		return color[0]
	case 1:
		return color[1]
	default:
		return color[2]
	}
}

func compareRGBA(a, b [4]uint8) int {
	for i := 0; i < 4; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
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
