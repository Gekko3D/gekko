package gekko

import (
	"image"
	"image/png"
	"os"
)

func (server AssetServer) CreateMesh(vertices AnySlice, indexes []uint16) Mesh {
	id := makeAssetId()

	server.meshes[id] = MeshAsset{
		Version:  0,
		Vertices: vertices,
		Indices:  indexes,
	}

	return Mesh{
		ID: id,
	}
}

func (server AssetServer) CreateMaterial(filename string, vertexType any) Material {
	shaderData, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	id := makeAssetId()

	server.materials[id] = MaterialAsset{
		Version:       0,
		ShaderName:    filename,
		ShaderListing: string(shaderData),
		VertexType:    vertexType,
	}

	return Material{
		ID: id,
	}
}

func (server AssetServer) CreateTextureFromTexels(texels []uint8, texWidth uint32, texHeight uint32, texDepth uint32, dimension TextureDimension, format TextureFormat) AssetId {
	id := makeAssetId()

	server.textures[id] = TextureAsset{
		Version:   0,
		Texels:    texels,
		Width:     texWidth,
		Height:    texHeight,
		Depth:     texDepth,
		Dimension: dimension,
		Format:    format,
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
		Version:   0,
		Texels:    rgbaImg.Pix,
		Width:     uint32(bounds.Max.X - bounds.Min.X),
		Height:    uint32(bounds.Max.Y - bounds.Min.Y),
		Depth:     1,
		Dimension: TextureDimension2D,
		Format:    TextureFormatRGBA8Unorm,
	}

	return id
}

func (server AssetServer) CreateSampler() AssetId {
	id := makeAssetId()

	server.samplers[id] = SamplerAsset{
		Version: 0,
		AssetID: id,
	}

	return id
}
