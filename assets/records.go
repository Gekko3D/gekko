package assets

import rooteecs "github.com/gekko3d/gekko/ecs"

type MeshAsset struct {
	Version  uint
	Vertices rooteecs.AnySlice
	Indices  []uint16
}

type MaterialAsset struct {
	Version       uint
	ShaderName    string
	ShaderListing string
	VertexType    any
}

type TextureAsset struct {
	Version   uint
	Texels    []uint8
	Width     uint32
	Height    uint32
	Depth     uint32
	Dimension TextureDimension
	Format    TextureFormat
}

type SamplerAsset struct {
	Version uint
	AssetID AssetID
}
