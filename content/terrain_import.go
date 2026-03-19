package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

func LoadHeightmapPNG(path string) (int, int, []uint16, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, nil, "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, 0, nil, "", err
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, "", err
	}
	hash := sha256.Sum256(contentBytes)
	if info.Size() == 0 {
		return 0, 0, nil, "", fmt.Errorf("heightmap png is empty")
	}

	img, err := png.Decode(bytes.NewReader(contentBytes))
	if err != nil {
		return 0, 0, nil, "", err
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	samples := make([]uint16, 0, width*height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray := color.Gray16Model.Convert(img.At(x, y)).(color.Gray16)
			samples = append(samples, gray.Y)
		}
	}
	return width, height, samples, hex.EncodeToString(hash[:]), nil
}

func ImportHeightmapPNG(path string, name string, worldSize Vec2, heightScale float32, voxelResolution float32, chunkSize int) (*TerrainSourceDef, error) {
	width, height, samples, hash, err := LoadHeightmapPNG(path)
	if err != nil {
		return nil, err
	}

	def := NewTerrainSourceDef(name)
	if strings.TrimSpace(def.Name) == "" {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if base == "" {
			base = "terrain"
		}
		def.Name = base
	}
	def.SampleWidth = width
	def.SampleHeight = height
	def.HeightSamples = samples
	def.WorldSize = worldSize
	def.HeightScale = heightScale
	def.VoxelResolution = voxelResolution
	def.ChunkSize = chunkSize
	def.ImportSource = &TerrainImportDef{
		PNGPath:    path,
		SourceHash: hash,
	}
	EnsureTerrainSourceDefaults(def)
	return def, nil
}
