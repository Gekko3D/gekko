package gekko

import (
	"sync"

	rootassets "github.com/gekko3d/gekko/assets"
)

type AssetId = rootassets.AssetID
type TextureFormat = rootassets.TextureFormat

const (
	TextureFormatRGBA8Unorm     = rootassets.TextureFormatRGBA8Unorm
	TextureFormatRGBA8UnormSrgb = rootassets.TextureFormatRGBA8UnormSrgb
)

type TextureDimension = rootassets.TextureDimension

const (
	TextureDimension1D = rootassets.TextureDimension1D
	TextureDimension2D = rootassets.TextureDimension2D
	TextureDimension3D = rootassets.TextureDimension3D
)

type Mesh = rootassets.Mesh
type Material = rootassets.Material
type Voxel = rootassets.Voxel
type VoxModel = rootassets.VoxModel
type VoxPalette = rootassets.VoxPalette
type VoxFile = rootassets.VoxFile
type VoxNodeType = rootassets.VoxNodeType
type VoxNode = rootassets.VoxNode
type VoxTransformFrame = rootassets.VoxTransformFrame
type VoxShapeModel = rootassets.VoxShapeModel
type VoxMaterial = rootassets.VoxMaterial
type VoxelFileAsset = rootassets.VoxelFileAsset
type VoxelModelAsset = rootassets.VoxelModelAsset
type VoxelPaletteAsset = rootassets.VoxelPaletteAsset
type MeshAsset = rootassets.MeshAsset
type MaterialAsset = rootassets.MaterialAsset
type TextureAsset = rootassets.TextureAsset
type SamplerAsset = rootassets.SamplerAsset

const (
	VoxNodeTransform = rootassets.VoxNodeTransform
	VoxNodeGroup     = rootassets.VoxNodeGroup
	VoxNodeShape     = rootassets.VoxNodeShape
)

type AssetServer struct {
	mu          sync.RWMutex
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
	return rootassets.NewID()
}
