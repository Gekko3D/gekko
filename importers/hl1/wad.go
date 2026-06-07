package hl1

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

type WAD struct {
	Path        string
	Magic       string
	Entries     []WADEntry
	Diagnostics []importcommon.Diagnostic
	data        []byte
}

type WADEntry struct {
	Name        string
	Offset      int32
	DiskSize    int32
	Size        int32
	Type        byte
	Compression byte
}

type TexturePixels struct {
	Name   string
	Width  int
	Height int
	Pixels []byte
	Colors [][3]uint8
	Source string
}

type TextureSample struct {
	Color        [4]uint8
	PaletteIndex uint8
}

func LoadWAD(path string) (*WAD, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseWAD(data, path)
}

func ParseWAD(data []byte, path string) (*WAD, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("wad too small: %d bytes", len(data))
	}
	magic := string(data[:4])
	if magic != "WAD2" && magic != "WAD3" {
		return nil, fmt.Errorf("unsupported wad magic %q", magic)
	}
	count := int(readInt32(data, 4))
	dirOffset := int(readInt32(data, 8))
	if count < 0 || dirOffset < 0 || dirOffset > len(data) || count > (len(data)-dirOffset)/32 {
		return nil, fmt.Errorf("wad directory out of range: count=%d offset=%d file=%d", count, dirOffset, len(data))
	}
	out := &WAD{Path: path, Magic: magic, Entries: make([]WADEntry, 0, count), data: data}
	for i := 0; i < count; i++ {
		base := dirOffset + i*32
		entry := WADEntry{
			Offset:      readInt32(data, base+0),
			DiskSize:    readInt32(data, base+4),
			Size:        readInt32(data, base+8),
			Type:        data[base+12],
			Compression: data[base+13],
			Name:        cString(data[base+16 : base+32]),
		}
		if entry.Offset < 0 || entry.DiskSize < 0 || int(entry.Offset) > len(data) || int(entry.DiskSize) > len(data)-int(entry.Offset) {
			out.Diagnostics = append(out.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.wad_entry_out_of_range",
				Subject:  entry.Name,
				Message:  fmt.Sprintf("entry offset=%d disk_size=%d file=%d", entry.Offset, entry.DiskSize, len(data)),
			})
		}
		if entry.Compression != 0 {
			out.Diagnostics = append(out.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.wad_entry_compressed",
				Subject:  entry.Name,
				Message:  "compressed WAD entries are not supported",
			})
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

func (w *WAD) HasEntry(name string) bool {
	if w == nil {
		return false
	}
	for _, entry := range w.Entries {
		if strings.EqualFold(entry.Name, name) {
			return true
		}
	}
	return false
}

func (w *WAD) TextureColor(name string) ([4]uint8, bool) {
	if w == nil {
		return [4]uint8{}, false
	}
	for _, entry := range w.Entries {
		if !strings.EqualFold(entry.Name, name) || entry.Compression != 0 {
			continue
		}
		texture, ok := decodeMipTexture(w.data, int(entry.Offset), w.Path)
		if !ok {
			return [4]uint8{}, false
		}
		return texture.AverageColor()
	}
	return [4]uint8{}, false
}

func (w *WAD) TexturePixels(name string) (TexturePixels, bool) {
	if w == nil {
		return TexturePixels{}, false
	}
	for _, entry := range w.Entries {
		if !strings.EqualFold(entry.Name, name) || entry.Compression != 0 {
			continue
		}
		return decodeMipTexture(w.data, int(entry.Offset), w.Path)
	}
	return TexturePixels{}, false
}

func ResolveWADPaths(gameDir string, wadValue string) []string {
	parts := strings.Split(wadValue, ";")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, "\\", "/")
		candidates := wadPathCandidates(gameDir, part)
		chosen := candidates[0]
		for _, candidate := range candidates {
			if fileExists(candidate) {
				chosen = candidate
				break
			}
		}
		cleaned := filepath.Clean(chosen)
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func wadPathCandidates(gameDir string, source string) []string {
	cleaned := filepath.Clean(filepath.FromSlash(source))
	base := filepath.Base(cleaned)
	if len(cleaned) >= 3 && cleaned[1] == ':' {
		cleaned = cleaned[3:]
		base = filepath.Base(cleaned)
	}
	var out []string
	if filepath.IsAbs(cleaned) {
		out = append(out, cleaned)
		cleaned = strings.TrimLeft(cleaned, string(filepath.Separator))
	} else {
		out = append(out, filepath.Join(gameDir, cleaned))
	}
	if valveSuffix, ok := suffixFromPathSegment(cleaned, "valve"); ok {
		out = append(out, filepath.Join(gameDir, valveSuffix))
	}
	if strings.HasPrefix(strings.ToLower(cleaned), "valve"+string(filepath.Separator)) {
		out = append(out, filepath.Join(gameDir, cleaned))
	} else {
		out = append(out, filepath.Join(gameDir, "valve", base))
	}
	out = append(out, filepath.Join(gameDir, base))
	return out
}

func suffixFromPathSegment(path string, segment string) (string, bool) {
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i, part := range parts {
		if strings.EqualFold(part, segment) {
			return filepath.Join(parts[i:]...), true
		}
	}
	return "", false
}

func LoadResolvedWADs(paths []string) ([]*WAD, []importcommon.Diagnostic) {
	wads := make([]*WAD, 0, len(paths))
	var diagnostics []importcommon.Diagnostic
	for _, path := range paths {
		wad, err := LoadWAD(path)
		if err != nil {
			diagnostics = append(diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.wad_load_failed",
				Subject:  path,
				Message:  err.Error(),
			})
			continue
		}
		diagnostics = append(diagnostics, wad.Diagnostics...)
		wads = append(wads, wad)
	}
	return wads, diagnostics
}

func averageMipTexColor(data []byte, offset int) ([4]uint8, bool) {
	texture, ok := decodeMipTexture(data, offset, "")
	if !ok {
		return [4]uint8{}, false
	}
	return texture.AverageColor()
}

func decodeMipTexture(data []byte, offset int, source string) (TexturePixels, bool) {
	if offset < 0 || offset+40 > len(data) {
		return TexturePixels{}, false
	}
	name := cString(data[offset : offset+16])
	width := int(readUint32(data, offset+16))
	height := int(readUint32(data, offset+20))
	pixelOffset := int(readUint32(data, offset+24))
	if width <= 0 || height <= 0 || pixelOffset <= 0 || width > 8192 || height > 8192 {
		return TexturePixels{}, false
	}
	pixelCount := width * height
	pixelStart := offset + pixelOffset
	if pixelStart < 0 || pixelStart > len(data) || pixelCount > len(data)-pixelStart {
		return TexturePixels{}, false
	}
	mipBytes := pixelCount + pixelCount/4 + pixelCount/16 + pixelCount/64
	paletteSizeOffset := pixelStart + mipBytes
	if paletteSizeOffset+2 > len(data) {
		return TexturePixels{}, false
	}
	paletteCount := int(readUint16(data, paletteSizeOffset))
	paletteStart := paletteSizeOffset + 2
	if paletteCount <= 0 || paletteStart > len(data) || paletteCount > (len(data)-paletteStart)/3 {
		return TexturePixels{}, false
	}
	colors := make([][3]uint8, paletteCount)
	for i := 0; i < paletteCount; i++ {
		base := paletteStart + i*3
		colors[i] = [3]uint8{data[base], data[base+1], data[base+2]}
	}
	pixels := append([]byte(nil), data[pixelStart:pixelStart+pixelCount]...)
	return TexturePixels{Name: name, Width: width, Height: height, Pixels: pixels, Colors: colors, Source: source}, true
}

func (texture TexturePixels) AverageColor() ([4]uint8, bool) {
	if texture.Width <= 0 || texture.Height <= 0 || len(texture.Pixels) == 0 || len(texture.Colors) == 0 {
		return [4]uint8{}, false
	}
	var r, g, b, n int
	for _, index := range texture.Pixels {
		paletteIndex := int(index)
		if paletteIndex >= len(texture.Colors) {
			continue
		}
		color := texture.Colors[paletteIndex]
		r += int(color[0])
		g += int(color[1])
		b += int(color[2])
		n++
	}
	if n == 0 {
		return [4]uint8{}, false
	}
	return [4]uint8{uint8(r / n), uint8(g / n), uint8(b / n), 255}, true
}

func (texture TexturePixels) Sample(u, v float32) ([4]uint8, bool) {
	sample, ok := texture.SampleTexel(u, v)
	return sample.Color, ok
}

func (texture TexturePixels) SampleTexel(u, v float32) (TextureSample, bool) {
	if texture.Width <= 0 || texture.Height <= 0 || len(texture.Pixels) < texture.Width*texture.Height || len(texture.Colors) == 0 {
		return TextureSample{}, false
	}
	x := wrapTextureCoord(int(math.Floor(float64(u))), texture.Width)
	y := wrapTextureCoord(int(math.Floor(float64(v))), texture.Height)
	paletteIndex := int(texture.Pixels[y*texture.Width+x])
	if paletteIndex < 0 || paletteIndex >= len(texture.Colors) {
		return TextureSample{}, false
	}
	color := texture.Colors[paletteIndex]
	return TextureSample{Color: [4]uint8{color[0], color[1], color[2], 255}, PaletteIndex: uint8(paletteIndex)}, true
}

func wrapTextureCoord(value int, size int) int {
	if size <= 0 {
		return 0
	}
	value %= size
	if value < 0 {
		value += size
	}
	return value
}
