package assets

import "github.com/google/uuid"

type AssetID struct {
	uuid.UUID
}

func NewID() AssetID {
	return AssetID{UUID: uuid.New()}
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
