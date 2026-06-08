package hl1

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	hl1TextureAnimationFPS = 10
	hl1ScrollAnimationFPS  = 12
	hl1ScrollPhaseCount    = 8
)

type hl1ScrollAxis string

const (
	hl1ScrollAxisU hl1ScrollAxis = "u"
	hl1ScrollAxisV hl1ScrollAxis = "v"
)

type animatedHL1TextureFrame struct {
	Name    string
	Index   int
	Texture TexturePixels
}

func animatedHL1SourceTextureName(textureName string) string {
	if _, ok := hl1AnimatedTextureGroup(textureName); !ok {
		return ""
	}
	return textureName
}

func hl1TextureAnimationID(textureName string) string {
	group, ok := hl1AnimatedTextureGroup(textureName)
	if !ok {
		return ""
	}
	return "hl1.texture." + group
}

func hl1TextureLooksScrollable(textureName string) bool {
	name := strings.ToLower(strings.TrimSpace(textureName))
	if name == "" {
		return false
	}
	return strings.HasPrefix(name, "scroll")
}

func hl1TextureScrollAnimationID(textureName string, axis hl1ScrollAxis) string {
	name := strings.ToLower(strings.TrimSpace(textureName))
	if name == "" {
		return ""
	}
	if axis != hl1ScrollAxisV {
		axis = hl1ScrollAxisU
	}
	return "hl1.scroll." + string(axis) + "." + name
}

func hl1ScrollAnimationSpec(animationID string) (string, hl1ScrollAxis, bool) {
	const prefix = "hl1.scroll."
	if !strings.HasPrefix(animationID, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(animationID, prefix)
	axis := hl1ScrollAxisU
	switch {
	case strings.HasPrefix(rest, "u."):
		rest = strings.TrimPrefix(rest, "u.")
	case strings.HasPrefix(rest, "v."):
		axis = hl1ScrollAxisV
		rest = strings.TrimPrefix(rest, "v.")
	}
	textureName := strings.TrimSpace(rest)
	if textureName == "" {
		return "", "", false
	}
	return textureName, axis, true
}

func hl1TextureScrollPhase(coord float32, textureSize int) int {
	if textureSize <= 0 {
		textureSize = 64
	}
	x := math.Mod(float64(coord), float64(textureSize))
	if x < 0 {
		x += float64(textureSize)
	}
	phase := int(math.Floor(x * float64(hl1ScrollPhaseCount) / float64(textureSize)))
	if phase < 0 {
		return 0
	}
	if phase >= hl1ScrollPhaseCount {
		return hl1ScrollPhaseCount - 1
	}
	return phase
}

func hl1AnimatedTextureGroup(textureName string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(textureName))
	if len(name) < 3 || name[0] != '+' || !unicode.IsDigit(rune(name[1])) {
		return "", false
	}
	group := strings.TrimSpace(name[2:])
	if group == "" {
		return "", false
	}
	return group, true
}

func (store *TextureStore) AnimatedTextureGroups() map[string][]animatedHL1TextureFrame {
	out := make(map[string][]animatedHL1TextureFrame)
	if store == nil {
		return out
	}
	for name, texture := range store.byName {
		group, ok := hl1AnimatedTextureGroup(name)
		if !ok {
			continue
		}
		frameIndex := int(name[1] - '0')
		out[group] = append(out[group], animatedHL1TextureFrame{
			Name:    texture.Name,
			Index:   frameIndex,
			Texture: texture,
		})
	}
	for group, frames := range out {
		sort.Slice(frames, func(i, j int) bool {
			if frames[i].Index != frames[j].Index {
				return frames[i].Index < frames[j].Index
			}
			return strings.ToLower(frames[i].Name) < strings.ToLower(frames[j].Name)
		})
		if len(frames) < 2 || frames[0].Index != 0 {
			delete(out, group)
			continue
		}
		out[group] = frames
	}
	return out
}

func ApplyHL1MaterialAnimations(manifest *content.ImportedWorldDef, textureStore *TextureStore) []importcommon.Diagnostic {
	if manifest == nil || textureStore == nil {
		return nil
	}
	groups := textureStore.AnimatedTextureGroups()
	targetsByAnimation := make(map[string][]content.ImportedWorldMaterialDef)
	for _, material := range manifest.Materials {
		if material.AnimationID == "" || material.PaletteIndex == 0 {
			continue
		}
		if strings.HasPrefix(material.AnimationID, "hl1.scroll.") {
			continue
		}
		targetsByAnimation[material.AnimationID] = append(targetsByAnimation[material.AnimationID], material)
	}
	if len(targetsByAnimation) == 0 {
		return nil
	}
	var diagnostics []importcommon.Diagnostic
	animations := make([]content.ImportedWorldMaterialAnimationDef, 0, len(targetsByAnimation))
	groupNames := make([]string, 0, len(groups))
	for group := range groups {
		groupNames = append(groupNames, group)
	}
	sort.Strings(groupNames)
	seenAnimations := make(map[string]struct{}, len(groupNames))
	for _, group := range groupNames {
		animationID := "hl1.texture." + group
		targets := targetsByAnimation[animationID]
		if len(targets) == 0 {
			continue
		}
		seenAnimations[animationID] = struct{}{}
		sort.Slice(targets, func(i, j int) bool {
			return targets[i].PaletteIndex < targets[j].PaletteIndex
		})
		frames := buildHL1MaterialAnimationFrames(targets, groups[group])
		if len(frames) < 2 {
			diagnostics = append(diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.material_animation_empty_frames",
				Subject:  animationID,
				Message:  fmt.Sprintf("animated material group matched %d runtime material value(s), but frame colors could not be built", len(targets)),
			})
			continue
		}
		paletteIndices := make([]uint8, 0, len(targets))
		for _, target := range targets {
			paletteIndices = append(paletteIndices, target.PaletteIndex)
		}
		animations = append(animations, content.ImportedWorldMaterialAnimationDef{
			ID:             animationID,
			Kind:           "palette_sequence",
			FPS:            hl1TextureAnimationFPS,
			Mode:           "loop",
			PaletteIndices: paletteIndices,
			Frames:         frames,
			Tags:           []string{"source:hl1", "material:animated_texture", fmt.Sprintf("source_texture_group:%s", group)},
		})
	}
	for animationID, targets := range targetsByAnimation {
		if _, ok := seenAnimations[animationID]; ok {
			continue
		}
		diagnostics = append(diagnostics, importcommon.Diagnostic{
			Severity: importcommon.SeverityInfo,
			Code:     "hl1.material_animation_unsupported_sequence",
			Subject:  animationID,
			Message:  fmt.Sprintf("animated-looking texture was used by %d runtime material value(s), but no supported +0/+1 digit sequence was found; GoldSrc alternate +a sequences are not animated yet", len(targets)),
		})
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Code != diagnostics[j].Code {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Subject < diagnostics[j].Subject
	})
	manifest.MaterialAnimations = append(manifest.MaterialAnimations, animations...)
	return diagnostics
}

func ApplyHL1ScrollMaterialAnimations(manifest *content.ImportedWorldDef, textureStore *TextureStore) []importcommon.Diagnostic {
	if manifest == nil || textureStore == nil {
		return nil
	}
	targetsByAnimation := map[string][]content.ImportedWorldMaterialDef{}
	for _, material := range manifest.Materials {
		if material.AnimationID == "" || material.PaletteIndex == 0 {
			continue
		}
		if _, _, ok := hl1ScrollAnimationSpec(material.AnimationID); !ok {
			continue
		}
		targetsByAnimation[material.AnimationID] = append(targetsByAnimation[material.AnimationID], material)
	}
	if len(targetsByAnimation) == 0 {
		return nil
	}
	animationIDs := make([]string, 0, len(targetsByAnimation))
	for animationID := range targetsByAnimation {
		animationIDs = append(animationIDs, animationID)
	}
	sort.Strings(animationIDs)
	var diagnostics []importcommon.Diagnostic
	for _, animationID := range animationIDs {
		targets := targetsByAnimation[animationID]
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].AnimationPhase != targets[j].AnimationPhase {
				return targets[i].AnimationPhase < targets[j].AnimationPhase
			}
			return targets[i].PaletteIndex < targets[j].PaletteIndex
		})
		textureName, axis, ok := hl1ScrollAnimationSpec(animationID)
		if !ok {
			continue
		}
		texture, ok := textureStore.Texture(textureName)
		if !ok || !texture.Valid() {
			diagnostics = append(diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.material_scroll_texture_missing",
				Subject:  animationID,
				Message:  fmt.Sprintf("scroll material animation matched %d runtime material value(s), but source texture pixels were unavailable", len(targets)),
			})
			continue
		}
		frames := buildHL1ScrollAnimationFrames(targets, texture, axis)
		if len(frames) < 2 {
			continue
		}
		paletteIndices := make([]uint8, 0, len(targets))
		for _, target := range targets {
			paletteIndices = append(paletteIndices, target.PaletteIndex)
		}
		manifest.MaterialAnimations = append(manifest.MaterialAnimations, content.ImportedWorldMaterialAnimationDef{
			ID:             animationID,
			Kind:           "palette_scroll",
			FPS:            hl1ScrollAnimationFPS,
			Mode:           "loop",
			PaletteIndices: paletteIndices,
			Frames:         frames,
			UVScroll:       hl1ImportedWorldUVScroll(axis),
			Tags:           []string{"source:hl1", "material:scroll_texture", fmt.Sprintf("source_texture:%s", texture.Name)},
		})
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Code != diagnostics[j].Code {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Subject < diagnostics[j].Subject
	})
	return diagnostics
}

func HL1ScrollAssetMaterialAnimations(materials []importcommon.Material, textureStore *TextureStore) []content.AssetMaterialAnimationDef {
	if textureStore == nil {
		return nil
	}
	targetsByAnimation := map[string][]content.ImportedWorldMaterialDef{}
	for _, material := range materials {
		if material.AnimationID == "" || material.PaletteIndex == 0 {
			continue
		}
		if _, _, ok := hl1ScrollAnimationSpec(material.AnimationID); !ok {
			continue
		}
		targetsByAnimation[material.AnimationID] = append(targetsByAnimation[material.AnimationID], content.ImportedWorldMaterialDef{
			ID:                material.ID,
			PaletteIndex:      material.PaletteIndex,
			SourceTextureName: material.SourceTextureName,
			AnimationID:       material.AnimationID,
			AnimationPhase:    material.AnimationPhase,
			BaseColor:         content.ImportedWorldPaletteColor{material.BaseColor[0], material.BaseColor[1], material.BaseColor[2], material.BaseColor[3]},
			Kind:              material.Kind,
			Tags:              append([]string(nil), material.Tags...),
		})
	}
	if len(targetsByAnimation) == 0 {
		return nil
	}
	animationIDs := make([]string, 0, len(targetsByAnimation))
	for animationID := range targetsByAnimation {
		animationIDs = append(animationIDs, animationID)
	}
	sort.Strings(animationIDs)
	out := make([]content.AssetMaterialAnimationDef, 0, len(animationIDs))
	for _, animationID := range animationIDs {
		targets := targetsByAnimation[animationID]
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].AnimationPhase != targets[j].AnimationPhase {
				return targets[i].AnimationPhase < targets[j].AnimationPhase
			}
			return targets[i].PaletteIndex < targets[j].PaletteIndex
		})
		textureName, axis, ok := hl1ScrollAnimationSpec(animationID)
		if !ok {
			continue
		}
		texture, ok := textureStore.Texture(textureName)
		if !ok || !texture.Valid() {
			continue
		}
		importedFrames := buildHL1ScrollAnimationFrames(targets, texture, axis)
		if len(importedFrames) < 2 {
			continue
		}
		paletteIndices := make([]uint8, 0, len(targets))
		for _, target := range targets {
			paletteIndices = append(paletteIndices, target.PaletteIndex)
		}
		assetFrames := make([]content.AssetMaterialAnimationFrameDef, 0, len(importedFrames))
		for _, frame := range importedFrames {
			colors := make([][4]uint8, 0, len(frame.Colors))
			for _, color := range frame.Colors {
				colors = append(colors, [4]uint8{color[0], color[1], color[2], color[3]})
			}
			assetFrames = append(assetFrames, content.AssetMaterialAnimationFrameDef{
				Duration: frame.Duration,
				Colors:   colors,
			})
		}
		out = append(out, content.AssetMaterialAnimationDef{
			ID:             animationID,
			Kind:           "palette_scroll",
			FPS:            hl1ScrollAnimationFPS,
			Mode:           "loop",
			PaletteIndices: paletteIndices,
			Frames:         assetFrames,
			UVScroll:       hl1AssetUVScroll(axis),
			Tags:           []string{"source:hl1", "material:scroll_texture", fmt.Sprintf("source_texture:%s", texture.Name)},
		})
	}
	return out
}

func hl1ImportedWorldUVScroll(axis hl1ScrollAxis) *content.ImportedWorldMaterialUVScrollDef {
	return &content.ImportedWorldMaterialUVScrollDef{Velocity: hl1UVScrollVelocity(axis)}
}

func hl1AssetUVScroll(axis hl1ScrollAxis) *content.AssetMaterialUVScrollDef {
	return &content.AssetMaterialUVScrollDef{Velocity: hl1UVScrollVelocity(axis)}
}

func hl1UVScrollVelocity(axis hl1ScrollAxis) [2]float32 {
	if axis == hl1ScrollAxisV {
		return [2]float32{0, 1}
	}
	return [2]float32{1, 0}
}

func PopulateMaterialAnimationReport(report *importcommon.ImportReport, manifest *content.ImportedWorldDef) {
	if report == nil || manifest == nil {
		return
	}
	animatedMaterials := map[uint8]struct{}{}
	frameCount := 0
	for _, animation := range manifest.MaterialAnimations {
		frameCount += len(animation.Frames)
		for _, index := range animation.PaletteIndices {
			if index != 0 {
				animatedMaterials[index] = struct{}{}
			}
		}
	}
	report.MaterialAnimationCount = len(manifest.MaterialAnimations)
	report.AnimatedMaterialCount = len(animatedMaterials)
	report.MaterialAnimationFrameCount = frameCount
}

func buildHL1MaterialAnimationFrames(targets []content.ImportedWorldMaterialDef, frames []animatedHL1TextureFrame) []content.ImportedWorldMaterialAnimationFrameDef {
	if len(targets) == 0 || len(frames) < 2 {
		return nil
	}
	base := frames[0].Texture
	if !base.Valid() {
		return nil
	}
	targetColors := make([][4]uint8, 0, len(targets))
	for _, target := range targets {
		targetColors = append(targetColors, [4]uint8{target.BaseColor[0], target.BaseColor[1], target.BaseColor[2], target.BaseColor[3]})
	}
	assignments := assignHL1BaseTexturePixelsToTargets(base, targetColors)
	out := make([]content.ImportedWorldMaterialAnimationFrameDef, 0, len(frames))
	for _, frame := range frames {
		colors := averageHL1FrameColorsByTarget(frame.Texture, base.Width, base.Height, assignments, targetColors)
		out = append(out, content.ImportedWorldMaterialAnimationFrameDef{
			Colors: colors,
		})
	}
	return out
}

func assignHL1BaseTexturePixelsToTargets(base TexturePixels, targetColors [][4]uint8) []int {
	assignments := make([]int, base.Width*base.Height)
	for i := range assignments {
		assignments[i] = -1
	}
	if len(targetColors) == 0 || !base.Valid() {
		return assignments
	}
	for y := 0; y < base.Height; y++ {
		for x := 0; x < base.Width; x++ {
			color, ok := base.ColorAt(x, y)
			if !ok {
				continue
			}
			assignments[y*base.Width+x] = nearestHL1AnimationTarget(color, targetColors)
		}
	}
	return assignments
}

func averageHL1FrameColorsByTarget(frame TexturePixels, baseWidth, baseHeight int, assignments []int, fallback [][4]uint8) []content.ImportedWorldPaletteColor {
	type accum struct {
		R, G, B, A int
		N          int
	}
	accums := make([]accum, len(fallback))
	if frame.Valid() {
		for i, target := range assignments {
			if target < 0 || target >= len(accums) {
				continue
			}
			baseX := i % max(1, baseWidth)
			baseY := (i / max(1, baseWidth)) % max(1, baseHeight)
			x := int(float32(baseX) * float32(frame.Width) / float32(max(1, baseWidth)))
			y := int(float32(baseY) * float32(frame.Height) / float32(max(1, baseHeight)))
			color, ok := frame.ColorAt(x, y)
			if !ok {
				continue
			}
			accums[target].R += int(color[0])
			accums[target].G += int(color[1])
			accums[target].B += int(color[2])
			accums[target].A += int(color[3])
			accums[target].N++
		}
	}
	out := make([]content.ImportedWorldPaletteColor, 0, len(fallback))
	for i, entry := range accums {
		if entry.N == 0 {
			color := fallback[i]
			out = append(out, content.ImportedWorldPaletteColor{color[0], color[1], color[2], color[3]})
			continue
		}
		out = append(out, content.ImportedWorldPaletteColor{
			uint8(entry.R / entry.N),
			uint8(entry.G / entry.N),
			uint8(entry.B / entry.N),
			uint8(entry.A / entry.N),
		})
	}
	return out
}

func buildHL1ScrollAnimationFrames(targets []content.ImportedWorldMaterialDef, texture TexturePixels, axis hl1ScrollAxis) []content.ImportedWorldMaterialAnimationFrameDef {
	if len(targets) == 0 || !texture.Valid() {
		return nil
	}
	targetColors := make([][4]uint8, 0, len(targets))
	targetPhases := make([]int, 0, len(targets))
	for _, target := range targets {
		targetColors = append(targetColors, [4]uint8{target.BaseColor[0], target.BaseColor[1], target.BaseColor[2], target.BaseColor[3]})
		targetPhases = append(targetPhases, target.AnimationPhase)
	}
	assignments := assignHL1ScrollTexturePixelsToTargets(texture, targetColors, targetPhases, axis)
	frames := make([]content.ImportedWorldMaterialAnimationFrameDef, 0, hl1ScrollPhaseCount)
	for frame := 0; frame < hl1ScrollPhaseCount; frame++ {
		shift := frame * max(1, hl1ScrollTextureSize(texture, axis)/hl1ScrollPhaseCount)
		colors := averageHL1ScrolledColorsByTarget(texture, assignments, targetColors, shift, axis)
		frames = append(frames, content.ImportedWorldMaterialAnimationFrameDef{Colors: colors})
	}
	return frames
}

func assignHL1ScrollTexturePixelsToTargets(texture TexturePixels, targetColors [][4]uint8, targetPhases []int, axis hl1ScrollAxis) []int {
	assignments := make([]int, texture.Width*texture.Height)
	for i := range assignments {
		assignments[i] = -1
	}
	if len(targetColors) == 0 || !texture.Valid() {
		return assignments
	}
	for y := 0; y < texture.Height; y++ {
		for x := 0; x < texture.Width; x++ {
			color, ok := texture.ColorAt(x, y)
			if !ok {
				continue
			}
			phase := hl1TextureScrollPhase(float32(hl1ScrollPixelCoord(x, y, axis)), hl1ScrollTextureSize(texture, axis))
			assignments[y*texture.Width+x] = nearestHL1ScrollAnimationTarget(color, phase, targetColors, targetPhases)
		}
	}
	return assignments
}

func averageHL1ScrolledColorsByTarget(texture TexturePixels, assignments []int, fallback [][4]uint8, shift int, axis hl1ScrollAxis) []content.ImportedWorldPaletteColor {
	type accum struct {
		R, G, B, A int
		N          int
	}
	accums := make([]accum, len(fallback))
	if texture.Valid() {
		for i, target := range assignments {
			if target < 0 || target >= len(accums) {
				continue
			}
			x := i % texture.Width
			y := i / texture.Width
			if axis == hl1ScrollAxisV {
				y += shift
			} else {
				x += shift
			}
			color, ok := texture.ColorAt(x, y)
			if !ok {
				continue
			}
			accums[target].R += int(color[0])
			accums[target].G += int(color[1])
			accums[target].B += int(color[2])
			accums[target].A += int(color[3])
			accums[target].N++
		}
	}
	out := make([]content.ImportedWorldPaletteColor, 0, len(fallback))
	for i, entry := range accums {
		if entry.N == 0 {
			color := fallback[i]
			out = append(out, content.ImportedWorldPaletteColor{color[0], color[1], color[2], color[3]})
			continue
		}
		out = append(out, content.ImportedWorldPaletteColor{
			uint8(entry.R / entry.N),
			uint8(entry.G / entry.N),
			uint8(entry.B / entry.N),
			uint8(entry.A / entry.N),
		})
	}
	return out
}

func hl1ScrollTextureSize(texture TexturePixels, axis hl1ScrollAxis) int {
	if axis == hl1ScrollAxisV {
		return texture.Height
	}
	return texture.Width
}

func hl1ScrollPixelCoord(x, y int, axis hl1ScrollAxis) int {
	if axis == hl1ScrollAxisV {
		return y
	}
	return x
}

func nearestHL1AnimationTarget(color [4]uint8, targets [][4]uint8) int {
	best := 0
	bestDist := math.MaxInt
	for i, target := range targets {
		dr := int(color[0]) - int(target[0])
		dg := int(color[1]) - int(target[1])
		db := int(color[2]) - int(target[2])
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	return best
}

func nearestHL1ScrollAnimationTarget(color [4]uint8, phase int, targetColors [][4]uint8, targetPhases []int) int {
	best := -1
	bestDist := math.MaxInt
	for i, target := range targetColors {
		if i < len(targetPhases) && targetPhases[i] != phase {
			continue
		}
		dr := int(color[0]) - int(target[0])
		dg := int(color[1]) - int(target[1])
		db := int(color[2]) - int(target[2])
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	if best >= 0 {
		return best
	}
	return nearestHL1AnimationTarget(color, targetColors)
}
