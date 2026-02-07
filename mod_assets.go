package gekko

import (
	"github.com/google/uuid"
)

type AssetId struct {
	uuid.UUID
}

type TextureFormat uint32

const (
	TextureFormatRGBA8Unorm TextureFormat = iota
	TextureFormatRGBA8UnormSrgb
)

type TextureDimension uint32

const (
	TextureDimension1D TextureDimension = iota
	TextureDimension2D
	TextureDimension3D
)

type Mesh struct {
	assetId AssetId
}

type Material struct {
	assetId AssetId
}

type AssetServer struct {
	meshes      map[AssetId]MeshAsset
	materials   map[AssetId]MaterialAsset
	textures    map[AssetId]TextureAsset
	samplers    map[AssetId]SamplerAsset
	voxModels   map[AssetId]VoxelModelAsset
	voxPalettes map[AssetId]VoxelPaletteAsset
	voxFiles    map[AssetId]*VoxFile
}

type AssetServerModule struct{}

func (AssetServerModule) Install(app *App, cmd *Commands) {
	server := &AssetServer{
		meshes:      make(map[AssetId]MeshAsset),
		materials:   make(map[AssetId]MaterialAsset),
		textures:    make(map[AssetId]TextureAsset),
		samplers:    make(map[AssetId]SamplerAsset),
		voxModels:   make(map[AssetId]VoxelModelAsset),
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
		voxFiles:    make(map[AssetId]*VoxFile),
	}
	cmd.AddResources(server)
}

func makeAssetId() AssetId {
	return AssetId{uuid.New()}
}
