package core

import (
	"fmt"
	"image"
	"image/draw"
	"os"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type TextVertex struct {
	Pos   [2]float32
	UV    [2]float32
	Color [4]float32
}

type TextItem struct {
	Text     string
	Position [2]float32 // Normalized screen coords [-1, 1], (0,0) is center
	Scale    float32
	Color    [4]float32
}

type GlyphInfo struct {
	UVMin [2]float32
	UVMax [2]float32
	Size  [2]float32
	Off   [2]float32
	Adv   float32
}

type TextRenderer struct {
	AtlasImage *image.Alpha
	Glyphs     map[rune]GlyphInfo
	Face       font.Face
}

func NewTextRenderer(fontPath string, fontSize float64) (*TextRenderer, error) {
	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read font file: %w", err)
	}

	f, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse font: %w", err)
	}

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create face: %w", err)
	}

	const atlasSize = 512
	atlas := image.NewAlpha(image.Rect(0, 0, atlasSize, atlasSize))
	glyphs := make(map[rune]GlyphInfo)

	x, y := 2, 2
	rowHeight := 0

	for r := rune(32); r < 127; r++ {
		bounds, mask, _, adv, ok := face.Glyph(fixed.Point26_6{}, r)
		if !ok {
			continue
		}

		w := mask.Bounds().Dx()
		h := mask.Bounds().Dy()

		if x+w >= atlasSize {
			x = 2
			y += rowHeight + 4
			rowHeight = 0
		}

		if y+h >= atlasSize {
			break
		}

		draw.Draw(atlas, image.Rect(x, y, x+w, y+h), mask, mask.Bounds().Min, draw.Src)

		glyphs[r] = GlyphInfo{
			UVMin: [2]float32{float32(x) / atlasSize, float32(y) / atlasSize},
			UVMax: [2]float32{float32(x+w) / atlasSize, float32(y+h) / atlasSize},
			Size:  [2]float32{float32(w), float32(h)},
			Off:   [2]float32{float32(bounds.Min.X), float32(bounds.Min.Y)},
			Adv:   float32(adv) / 64.0, // Convert fixed 26.6 to float
		}

		x += w + 4
		if h > rowHeight {
			rowHeight = h
		}
	}

	return &TextRenderer{
		AtlasImage: atlas,
		Glyphs:     glyphs,
		Face:       face,
	}, nil
}

func (tr *TextRenderer) BuildVertices(items []TextItem, screenW, screenH int) []TextVertex {
	vertices := make([]TextVertex, 0, len(items)*6)

	sw := float32(screenW)
	sh := float32(screenH)
	metrics := tr.Face.Metrics()
	ascent := float32(metrics.Ascent.Ceil())
	lineHeight := float32(metrics.Height.Ceil())

	for _, item := range items {
		startX := item.Position[0]
		posX := startX
		posY := item.Position[1] + ascent*item.Scale

		for _, r := range item.Text {
			if r == '\n' {
				posX = startX
				posY += lineHeight * item.Scale
				continue
			}

			g, ok := tr.Glyphs[r]
			if !ok {
				continue
			}

			x0 := (posX+g.Off[0]*item.Scale)/sw*2.0 - 1.0
			y0 := 1.0 - (posY+g.Off[1]*item.Scale)/sh*2.0
			x1 := (posX+(g.Off[0]+g.Size[0])*item.Scale)/sw*2.0 - 1.0
			y1 := 1.0 - (posY+(g.Off[1]+g.Size[1])*item.Scale)/sh*2.0

			// Triangle 1
			vertices = append(vertices, TextVertex{Pos: [2]float32{x0, y0}, UV: [2]float32{g.UVMin[0], g.UVMin[1]}, Color: item.Color})
			vertices = append(vertices, TextVertex{Pos: [2]float32{x1, y0}, UV: [2]float32{g.UVMax[0], g.UVMin[1]}, Color: item.Color})
			vertices = append(vertices, TextVertex{Pos: [2]float32{x0, y1}, UV: [2]float32{g.UVMin[0], g.UVMax[1]}, Color: item.Color})

			// Triangle 2
			vertices = append(vertices, TextVertex{Pos: [2]float32{x1, y0}, UV: [2]float32{g.UVMax[0], g.UVMin[1]}, Color: item.Color})
			vertices = append(vertices, TextVertex{Pos: [2]float32{x1, y1}, UV: [2]float32{g.UVMax[0], g.UVMax[1]}, Color: item.Color})
			vertices = append(vertices, TextVertex{Pos: [2]float32{x0, y1}, UV: [2]float32{g.UVMin[0], g.UVMax[1]}, Color: item.Color})

			posX += g.Adv * item.Scale
		}
	}

	return vertices
}

func (tr *TextRenderer) MeasureText(text string, scale float32) (float32, float32) {
	if tr == nil {
		return 0, 0
	}

	metrics := tr.Face.Metrics()
	lineHeight := float32(metrics.Height.Ceil())

	maxW := float32(0)
	currentW := float32(0)
	lines := 1

	for _, r := range text {
		if r == '\n' {
			if currentW > maxW {
				maxW = currentW
			}
			currentW = 0
			lines++
			continue
		}

		g, ok := tr.Glyphs[r]
		if !ok {
			continue
		}
		currentW += g.Adv * scale
	}

	if currentW > maxW {
		maxW = currentW
	}

	return maxW, lineHeight * scale * float32(lines)
}

func (tr *TextRenderer) GetLineHeight(scale float32) float32 {
	if tr == nil {
		return 0
	}
	metrics := tr.Face.Metrics()
	return float32(metrics.Height.Ceil()) * scale
}
