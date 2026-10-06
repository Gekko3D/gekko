package gekko

import (
	"image"
	"image/png"
	"os"
)

func (server *AssetServer) CreateMesh(vertices AnySlice, indexes []uint16) Mesh {
	id := makeAssetId()

	server.mu.Lock()
	server.meshes[id] = MeshAsset{
		Version:  0,
		Vertices: vertices,
		Indices:  indexes,
	}
	server.mu.Unlock()

	return Mesh{
		ID: id,
	}
}

func (server *AssetServer) CreateMaterial(filename string, vertexType any) Material {
	shaderData, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	id := makeAssetId()

	server.mu.Lock()
	server.materials[id] = MaterialAsset{
		Version:       0,
		ShaderName:    filename,
		ShaderListing: string(shaderData),
		VertexType:    vertexType,
	}
	server.mu.Unlock()

	return Material{
		ID: id,
	}
}

func (server *AssetServer) CreateTextureFromTexels(texels []uint8, texWidth uint32, texHeight uint32, texDepth uint32, dimension TextureDimension, format TextureFormat) AssetId {
	id := makeAssetId()

	server.mu.Lock()
	server.textures[id] = TextureAsset{
		Version:   0,
		Texels:    texels,
		Width:     texWidth,
		Height:    texHeight,
		Depth:     texDepth,
		Dimension: dimension,
		Format:    format,
	}
	server.mu.Unlock()

	return id
}

func (server *AssetServer) CreateTexture(filename string) AssetId {
	id := makeAssetId()

	texture, err := decodeTexturePNG(filename)
	if err != nil {
		panic(err)
	}
	server.mu.Lock()
	server.textures[id] = texture
	server.mu.Unlock()
	return id
}

// Pure CPU decoding preserves the public texture path's premultiplied RGBA conversion.
func decodeTexturePNG(filename string) (TextureAsset, error) {
	file, err := os.Open(filename)
	if err != nil {
		return TextureAsset{}, err
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		return TextureAsset{}, err
	}
	bounds := img.Bounds()
	rgbaImg, ok := img.(*image.RGBA)
	if !ok {
		rgbaImg = image.NewRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				rgbaImg.Set(x, y, img.At(x, y))
			}
		}
	}
	return TextureAsset{Version: 0, Texels: rgbaImg.Pix,
		Width: uint32(bounds.Dx()), Height: uint32(bounds.Dy()), Depth: 1,
		Dimension: TextureDimension2D, Format: TextureFormatRGBA8Unorm}, nil
}

func (server *AssetServer) CreateSampler() AssetId {
	id := makeAssetId()

	server.mu.Lock()
	server.samplers[id] = SamplerAsset{
		Version: 0,
		AssetID: id,
	}
	server.mu.Unlock()

	return id
}
