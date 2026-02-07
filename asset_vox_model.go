package gekko

import (
	"math"
)

type VoxelFileAsset struct {
	VoxFile *VoxFile
}

type VoxelModelAsset struct {
	VoxModel   VoxModel
	BrickSize  [3]uint32
	SourcePath string
}

type VoxelPaletteAsset struct {
	VoxPalette VoxPalette
	Materials  []VoxMaterial
	IsPBR      bool
	Roughness  float32
	Metalness  float32
	Emission   float32
	IOR        float32
	SourcePath string
}

func createVolumeTexels(voxModel *VoxModel, palette *VoxPalette) []uint8 {
	volume := make([]uint8, voxModel.SizeX*voxModel.SizeY*voxModel.SizeZ*4)
	for _, v := range voxModel.Voxels {
		idx := (int32(v.Z)*int32(voxModel.SizeY*voxModel.SizeX) + int32(v.Y)*int32(voxModel.SizeX) + int32(v.X)) * 4
		color := palette[v.ColorIndex]
		volume[idx+0] = color[0]
		volume[idx+1] = color[1]
		volume[idx+2] = color[2]
		volume[idx+3] = 255
	}
	return volume
}

func (server AssetServer) CreateVoxelBasedTexture(voxModel *VoxModel, palette *VoxPalette) AssetId {
	volumeTexels := createVolumeTexels(voxModel, palette)
	return server.CreateTextureFromTexels(volumeTexels[:], voxModel.SizeX, voxModel.SizeY, voxModel.SizeZ, TextureDimension3D, TextureFormatRGBA8Unorm)
}

func (server AssetServer) CreateVoxelModel(model VoxModel, resolution float32) AssetId {
	return server.CreateVoxelModelFromSource(model, resolution, "")
}

func (server AssetServer) CreateVoxelModelFromSource(model VoxModel, resolution float32, sourcePath string) AssetId {
	if resolution != 1.0 && resolution > 0 {
		model = ScaleVoxModel(model, resolution)
	}
	id := makeAssetId()
	server.voxModels[id] = VoxelModelAsset{
		VoxModel:   model,
		BrickSize:  [3]uint32{8, 8, 8},
		SourcePath: sourcePath,
	}
	return id
}

func (server AssetServer) CreateVoxelFile(voxFile *VoxFile) AssetId {
	id := makeAssetId()
	server.voxFiles[id] = voxFile
	// Automatically register all models in the file
	// (Note: some models might not be referenced by nodes, but we store them anyway)
	return id
}

func ScaleVoxModel(model VoxModel, scale float32) VoxModel {
	if scale <= 0 || scale == 1.0 {
		return model
	}
	newSizeX := uint32(math.Round(float64(float32(model.SizeX) * scale)))
	newSizeY := uint32(math.Round(float64(float32(model.SizeY) * scale)))
	newSizeZ := uint32(math.Round(float64(float32(model.SizeZ) * scale)))

	if newSizeX == 0 {
		newSizeX = 1
	}
	if newSizeY == 0 {
		newSizeY = 1
	}
	if newSizeZ == 0 {
		newSizeZ = 1
	}

	newVoxels := make([]Voxel, 0)

	if scale > 1.0 {
		// Upscaling
		for _, v := range model.Voxels {
			startX := uint32(float32(v.X) * scale)
			startY := uint32(float32(v.Y) * scale)
			startZ := uint32(float32(v.Z) * scale)
			endX := uint32(float32(v.X+1) * scale)
			endY := uint32(float32(v.Y+1) * scale)
			endZ := uint32(float32(v.Z+1) * scale)

			for x := startX; x < endX; x++ {
				for y := startY; y < endY; y++ {
					for z := startZ; z < endZ; z++ {
						if x < newSizeX && y < newSizeY && z < newSizeZ {
							newVoxels = append(newVoxels, Voxel{
								X: x, Y: y, Z: z,
								ColorIndex: v.ColorIndex,
							})
						}
					}
				}
			}
		}
	} else {
		// Downscaling with voting approximation
		type coord struct{ x, y, z uint32 }
		groups := make(map[coord]map[byte]int)
		for _, v := range model.Voxels {
			nx := uint32(float32(v.X) * scale)
			ny := uint32(float32(v.Y) * scale)
			nz := uint32(float32(v.Z) * scale)
			if nx >= newSizeX {
				nx = newSizeX - 1
			}
			if ny >= newSizeY {
				ny = newSizeY - 1
			}
			if nz >= newSizeZ {
				nz = newSizeZ - 1
			}
			c := coord{nx, ny, nz}
			if groups[c] == nil {
				groups[c] = make(map[byte]int)
			}
			groups[c][v.ColorIndex]++
		}

		for c, counts := range groups {
			maxCount := 0
			var bestColor byte
			for idx, count := range counts {
				if count > maxCount {
					maxCount = count
					bestColor = idx
				}
			}
			newVoxels = append(newVoxels, Voxel{
				X: c.x, Y: c.y, Z: c.z,
				ColorIndex: bestColor,
			})
		}
	}

	return VoxModel{
		SizeX: newSizeX, SizeY: newSizeY, SizeZ: newSizeZ,
		Voxels: newVoxels,
	}
}

func (server AssetServer) CreateVoxelPalette(palette VoxPalette, materials []VoxMaterial) AssetId {
	return server.CreateVoxelPaletteFromSource(palette, materials, "")
}

func (server AssetServer) CreateVoxelPaletteFromSource(palette VoxPalette, materials []VoxMaterial, sourcePath string) AssetId {
	id := makeAssetId()
	server.voxPalettes[id] = VoxelPaletteAsset{
		VoxPalette: palette,
		Materials:  materials,
		SourcePath: sourcePath,
	}
	return id
}

func (server AssetServer) CreateSimplePalette(rgba [4]uint8) AssetId {
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}
	return server.CreateVoxelPalette(p, nil)
}

func (server AssetServer) CreatePBRPalette(rgba [4]uint8, roughness, metalness, emission, ior float32) AssetId {
	id := makeAssetId()
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}

	// This is a bit tricky because the Palette asset doesn't store PBR properties.
	// The VoxelModelAsset holds the model, but the palette is just colors.
	// HOWEVER, the VoxelRtModule's conversion logic can be extended to look for
	// "pseudo-materials" or we can add a new asset type.
	// For now, I'll store the PBR properties in a new VoxelPaletteAsset field.

	server.voxPalettes[id] = VoxelPaletteAsset{
		VoxPalette: p,
		IsPBR:      true,
		Roughness:  roughness,
		Metalness:  metalness,
		Emission:   emission,
		IOR:        ior,
	}
	return id
}
