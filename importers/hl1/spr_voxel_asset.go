package hl1

import (
	"fmt"
	"strings"

	"github.com/gekko3d/gekko/content"
)

type SPRVoxelAssetOptions struct {
	Name            string
	SourceRef       string
	VoxelResolution float32
	MaxPixels       int
}

func BuildSPRVoxelAsset(geometry SPRGeometry, opts SPRVoxelAssetOptions) (*content.AssetDef, int, error) {
	if len(geometry.Frames) == 0 {
		return nil, 0, fmt.Errorf("spr contains no decoded frames")
	}
	resolution := opts.VoxelResolution
	if resolution <= 0 {
		resolution = DefaultImportedVoxelResolution
	}
	frame := geometry.Frames[0]
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = 4096
	}
	skip := spritePixelStep(frame.Width, frame.Height, maxPixels)
	voxels, colors := spriteFrameVoxels(geometry, frame, skip)
	if len(voxels) == 0 {
		return nil, 0, fmt.Errorf("spr produced no visible voxels")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "hl1_sprite"
	}
	asset := content.NewAssetDef(name)
	castsShadows := false
	asset.Runtime = &content.AssetRuntimeDef{
		CollapseVoxelParts: true,
		CastsShadows:       &castsShadows,
	}
	asset.Tags = []string{"source:hl1", "source_asset:spr", "generated:sprite_voxel_card", "source_ref:" + opts.SourceRef}
	var shapePalette []content.AssetVoxelPaletteEntryDef
	asset.Materials, shapePalette = spriteMaterialsAndPalette(colors)
	asset.Parts = []content.AssetPartDef{{
		ID:              "sprite_card",
		Name:            name,
		VoxelResolution: resolution * float32(skip),
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: shapePalette,
				Voxels:  voxels,
			},
		},
		Transform: content.AssetTransformDef{
			Position: content.Vec3{
				float32(frame.OriginX) * HammerUnitMeters,
				float32(frame.OriginY-frame.Height+1) * HammerUnitMeters,
				0,
			},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Tags: []string{"source:hl1", "kind:sprite_card"},
	}}
	content.EnsureAssetIDs(asset)
	return asset, len(voxels), nil
}

func spritePixelStep(width, height, maxPixels int) int {
	if width <= 0 || height <= 0 || width*height <= maxPixels {
		return 1
	}
	step := 1
	for (width/step)*(height/step) > maxPixels {
		step++
	}
	return step
}

func spriteFrameVoxels(geometry SPRGeometry, frame SPRFrame, step int) ([]content.VoxelObjectVoxelDef, map[uint8][4]uint8) {
	if step <= 0 {
		step = 1
	}
	materialByColor := map[[4]uint8]uint8{}
	paletteByValue := map[uint8][4]uint8{}
	nextValue := uint8(1)
	voxels := make([]content.VoxelObjectVoxelDef, 0, frame.Width*frame.Height/(step*step))
	for y := 0; y < frame.Height; y += step {
		for x := 0; x < frame.Width; x += step {
			index := int(frame.Pixels[y*frame.Width+x])
			if !spritePixelVisible(geometry, index) {
				continue
			}
			color := spritePaletteColor(geometry, index)
			value, ok := materialByColor[color]
			if !ok {
				if nextValue == 0 {
					continue
				}
				value = nextValue
				nextValue++
				materialByColor[color] = value
				paletteByValue[value] = color
			}
			voxels = append(voxels, content.VoxelObjectVoxelDef{X: x / step, Y: (frame.Height - 1 - y) / step, Z: 0, Value: value})
		}
	}
	return voxels, paletteByValue
}

func spritePixelVisible(geometry SPRGeometry, paletteIndex int) bool {
	if paletteIndex < 0 || paletteIndex >= len(geometry.Palette) {
		return false
	}
	color := geometry.Palette[paletteIndex]
	if geometry.Info.TextureFormat == 1 || geometry.Info.TextureFormat == 2 {
		return color[0] > 2 || color[1] > 2 || color[2] > 2
	}
	if geometry.Info.TextureFormat == 3 && paletteIndex == 255 {
		return false
	}
	return true
}

func spritePaletteColor(geometry SPRGeometry, paletteIndex int) [4]uint8 {
	color := geometry.Palette[paletteIndex]
	alpha := uint8(255)
	if geometry.Info.TextureFormat == 1 || geometry.Info.TextureFormat == 2 {
		maxChannel := max(max(int(color[0]), int(color[1])), int(color[2]))
		alpha = uint8(max(32, min(255, maxChannel)))
	}
	return [4]uint8{color[0], color[1], color[2], alpha}
}

func spriteMaterialsAndPalette(colors map[uint8][4]uint8) ([]content.AssetMaterialDef, []content.AssetVoxelPaletteEntryDef) {
	materials := make([]content.AssetMaterialDef, 0, len(colors))
	palette := make([]content.AssetVoxelPaletteEntryDef, 0, len(colors))
	for value := uint8(1); value != 0; value++ {
		color, ok := colors[value]
		if !ok {
			continue
		}
		materialID := fmt.Sprintf("spr_%d", value)
		transparency := float32(0)
		if color[3] < 255 {
			transparency = 1 - float32(color[3])/255
		}
		materials = append(materials, content.AssetMaterialDef{
			ID:           materialID,
			Name:         materialID,
			BaseColor:    color,
			Roughness:    0.25,
			Emissive:     1.5,
			Transparency: transparency,
			Tags:         []string{"source:hl1", "source_asset:spr", "kind:sprite", "material:sprite", "material:emissive"},
		})
		palette = append(palette, content.AssetVoxelPaletteEntryDef{Value: value, MaterialID: materialID})
	}
	return materials, palette
}
