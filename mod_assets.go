package gekko

import (
	"image"
	"image/png"
	"os"

	"github.com/google/uuid"
)

type AssetId string

type AssetServer struct {
	meshes    map[AssetId]MeshAsset
	materials map[AssetId]MaterialAsset
	textures  map[AssetId]TextureAsset
}

type AssetServerModule struct{}

type Mesh struct {
	assetId AssetId
}

type Material struct {
	assetId AssetId
}

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
	version uint
	texels  []uint8
	width   uint32
	height  uint32
}

func (server AssetServer) LoadMesh(vertices AnySlice, indexes []uint16) Mesh {
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

func (server AssetServer) LoadMaterial(filename string, vertexType any) Material {
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

func (server AssetServer) CreateTexture(texels []uint8, texWidth uint32, texHeight uint32) AssetId {
	id := makeAssetId()

	server.textures[id] = TextureAsset{
		version: 0,
		texels:  texels,
		width:   texWidth,
		height:  texHeight,
	}

	return id
}

func (server AssetServer) LoadTexture(filename string) AssetId {
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
		version: 0,
		texels:  rgbaImg.Pix,
		width:   uint32(bounds.Max.X - bounds.Min.X),
		height:  uint32(bounds.Max.Y - bounds.Min.Y),
	}

	return id
}

func (AssetServerModule) Install(app *App, cmd *Commands) {
	app.addResources(&AssetServer{
		meshes:    make(map[AssetId]MeshAsset),
		materials: make(map[AssetId]MaterialAsset),
		textures:  make(map[AssetId]TextureAsset),
	})
}

func makeAssetId() AssetId {
	return AssetId(uuid.NewString())
}
