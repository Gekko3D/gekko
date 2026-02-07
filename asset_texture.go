package gekko

import (
	"image"
	"image/png"
	"os"
)

type MeshAsset struct {
	version  uint
	vertices AnySlice
	indices  []uint16
}

type MaterialAsset struct {
	version       uint
	shaderName    string
	shaderListing string
	vertexType    any
}

type TextureAsset struct {
	version   uint
	texels    []uint8
	width     uint32
	height    uint32
	depth     uint32
	dimension TextureDimension
	format    TextureFormat
}

type SamplerAsset struct {
	version uint
	assetId AssetId
}

func (server AssetServer) CreateMesh(vertices AnySlice, indexes []uint16) Mesh {
	id := makeAssetId()

	server.meshes[id] = MeshAsset{
		0,
		vertices,
		indexes,
	}

	return Mesh{
		assetId: id,
	}
}

func (server AssetServer) CreateMaterial(filename string, vertexType any) Material {
	shaderData, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	id := makeAssetId()

	server.materials[id] = MaterialAsset{
		version:       0,
		shaderName:    filename,
		shaderListing: string(shaderData),
		vertexType:    vertexType,
	}

	return Material{
		assetId: id,
	}
}

func (server AssetServer) CreateTextureFromTexels(texels []uint8, texWidth uint32, texHeight uint32, texDepth uint32, dimension TextureDimension, format TextureFormat) AssetId {
	id := makeAssetId()

	server.textures[id] = TextureAsset{
		version:   0,
		texels:    texels,
		width:     texWidth,
		height:    texHeight,
		depth:     texDepth,
		dimension: dimension,
		format:    format,
	}

	return id
}

func (server AssetServer) CreateTexture(filename string) AssetId {
	id := makeAssetId()

	file, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// Decode the image
	img, err := png.Decode(file)
	if err != nil {
		panic(err)
	}

	bounds := img.Bounds()

	// Convert to RGBA if needed
	rgbaImg, ok := img.(*image.RGBA)
	if !ok {
		// Convert to RGBA format
		rgbaImg = image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgbaImg.Set(x, y, img.At(x, y))
			}
		}
	}

	server.textures[id] = TextureAsset{
		version:   0,
		texels:    rgbaImg.Pix,
		width:     uint32(bounds.Max.X - bounds.Min.X),
		height:    uint32(bounds.Max.Y - bounds.Min.Y),
		depth:     1,
		dimension: TextureDimension2D,
		format:    TextureFormatRGBA8Unorm,
	}

	return id
}

func (server AssetServer) CreateSampler() AssetId {
	id := makeAssetId()

	server.samplers[id] = SamplerAsset{
		version: 0,
		assetId: id,
	}

	return id
}
