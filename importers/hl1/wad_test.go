package hl1

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

func TestParseWADDirectoryAndLookup(t *testing.T) {
	data := syntheticWAD([]string{"TESTWALL", "Other"})
	wad, err := ParseWAD(data, "test.wad")
	if err != nil {
		t.Fatalf("ParseWAD failed: %v", err)
	}
	if wad.Magic != "WAD3" {
		t.Fatalf("magic = %q", wad.Magic)
	}
	if !wad.HasEntry("testwall") || !wad.HasEntry("OTHER") {
		t.Fatalf("case-insensitive lookup failed: %+v", wad.Entries)
	}
}

func TestResolveWADPathsPrefersExistingValvePath(t *testing.T) {
	dir := t.TempDir()
	wadPath := filepath.Join(dir, "valve", "test.wad")
	if err := mkdirAll(filepath.Dir(wadPath)); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeFile(wadPath, syntheticWAD([]string{"TESTWALL"})); err != nil {
		t.Fatalf("write wad: %v", err)
	}
	paths := ResolveWADPaths(dir, `C:\Sierra\Half-Life\valve\test.wad`)
	if len(paths) != 1 {
		t.Fatalf("paths = %+v", paths)
	}
	if paths[0] != filepath.Clean(wadPath) {
		t.Fatalf("path = %q, want %q", paths[0], wadPath)
	}
}

func TestResolveWADPathsPrefersExistingValvePathFromOldAbsoluteQuiverPath(t *testing.T) {
	dir := t.TempDir()
	wadPath := filepath.Join(dir, "valve", "liquids.wad")
	if err := mkdirAll(filepath.Dir(wadPath)); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeFile(wadPath, syntheticWAD([]string{"WATER"})); err != nil {
		t.Fatalf("write wad: %v", err)
	}
	paths := ResolveWADPaths(dir, `/quiver/valve/liquids.wad`)
	if len(paths) != 1 {
		t.Fatalf("paths = %+v", paths)
	}
	if paths[0] != filepath.Clean(wadPath) {
		t.Fatalf("path = %q, want %q", paths[0], wadPath)
	}
}

func TestWADTextureColorAveragesMipTexPalette(t *testing.T) {
	wad, err := ParseWAD(syntheticMipTexWAD("TESTWALL", [][3]byte{{10, 20, 30}, {110, 120, 130}}, []byte{0, 1, 1, 0}), "color.wad")
	if err != nil {
		t.Fatalf("ParseWAD failed: %v", err)
	}
	color, ok := wad.TextureColor("testwall")
	if !ok {
		t.Fatal("TextureColor failed")
	}
	if color != ([4]uint8{60, 70, 80, 255}) {
		t.Fatalf("color = %+v", color)
	}
}

func TestWADTexturePixelsSamplesWrappedTexel(t *testing.T) {
	wad, err := ParseWAD(syntheticMipTexWAD("TESTWALL", [][3]byte{{10, 20, 30}, {110, 120, 130}}, []byte{0, 1, 1, 0}), "color.wad")
	if err != nil {
		t.Fatalf("ParseWAD failed: %v", err)
	}
	texture, ok := wad.TexturePixels("testwall")
	if !ok {
		t.Fatal("TexturePixels failed")
	}
	color, ok := texture.Sample(-1, 0)
	if !ok {
		t.Fatal("Sample failed")
	}
	if color != ([4]uint8{110, 120, 130, 255}) {
		t.Fatalf("wrapped sample = %+v", color)
	}
	sample, ok := texture.SampleTexel(-1, 0)
	if !ok {
		t.Fatal("SampleTexel failed")
	}
	if sample.PaletteIndex != 1 {
		t.Fatalf("palette index = %d", sample.PaletteIndex)
	}
}

func syntheticWAD(names []string) []byte {
	headerSize := 12
	entryPayloadSize := 1
	dirOffset := headerSize + len(names)*entryPayloadSize
	data := make([]byte, dirOffset+len(names)*32)
	copy(data[:4], []byte("WAD3"))
	binary.LittleEndian.PutUint32(data[4:], uint32(len(names)))
	binary.LittleEndian.PutUint32(data[8:], uint32(dirOffset))
	for i, name := range names {
		payloadOffset := headerSize + i*entryPayloadSize
		data[payloadOffset] = byte(i + 1)
		base := dirOffset + i*32
		binary.LittleEndian.PutUint32(data[base:], uint32(payloadOffset))
		binary.LittleEndian.PutUint32(data[base+4:], uint32(entryPayloadSize))
		binary.LittleEndian.PutUint32(data[base+8:], uint32(entryPayloadSize))
		data[base+12] = 0x43
		copy(data[base+16:base+32], []byte(name))
	}
	return data
}

func syntheticMipTexWAD(name string, palette [][3]byte, pixels []byte) []byte {
	headerSize := 12
	width := uint32(2)
	height := uint32(2)
	pixelCount := int(width * height)
	mip1 := pixelCount / 4
	mip2 := pixelCount / 16
	mip3 := pixelCount / 64
	payloadSize := 40 + pixelCount + mip1 + mip2 + mip3 + 2 + len(palette)*3
	dirOffset := headerSize + payloadSize
	data := make([]byte, dirOffset+32)
	copy(data[:4], []byte("WAD3"))
	binary.LittleEndian.PutUint32(data[4:], 1)
	binary.LittleEndian.PutUint32(data[8:], uint32(dirOffset))
	payloadOffset := headerSize
	copy(data[payloadOffset:payloadOffset+16], []byte(name))
	binary.LittleEndian.PutUint32(data[payloadOffset+16:], width)
	binary.LittleEndian.PutUint32(data[payloadOffset+20:], height)
	binary.LittleEndian.PutUint32(data[payloadOffset+24:], 40)
	copy(data[payloadOffset+40:payloadOffset+40+pixelCount], pixels)
	paletteSizeOffset := payloadOffset + 40 + pixelCount + mip1 + mip2 + mip3
	binary.LittleEndian.PutUint16(data[paletteSizeOffset:], uint16(len(palette)))
	paletteOffset := paletteSizeOffset + 2
	for i, color := range palette {
		copy(data[paletteOffset+i*3:paletteOffset+i*3+3], color[:])
	}
	base := dirOffset
	binary.LittleEndian.PutUint32(data[base:], uint32(payloadOffset))
	binary.LittleEndian.PutUint32(data[base+4:], uint32(payloadSize))
	binary.LittleEndian.PutUint32(data[base+8:], uint32(payloadSize))
	data[base+12] = 0x43
	copy(data[base+16:base+32], []byte(name))
	return data
}
