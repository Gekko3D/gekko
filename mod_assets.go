package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"os"
)

type AssetId string

type AssetServer struct {
	meshes    map[AssetId]MeshAsset
	materials map[AssetId]MaterialAsset
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
	vertices []mgl32.Vec3
	indexes  []uint16
}

type MaterialAsset struct {
	version       uint
	shaderName    string
	shaderListing string
}

func (server AssetServer) LoadMesh(vertices []mgl32.Vec3, indexes []uint16) Mesh {
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

func (server AssetServer) LoadMaterial(filename string) Material {
	shaderData, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	id := makeAssetId()

	server.materials[id] = MaterialAsset{
		version:       0,
		shaderName:    filename,
		shaderListing: string(shaderData),
	}

	return Material{
		assetId: id,
	}
}

func (AssetServerModule) Install(app *App, cmd *Commands) {
	app.addResources(&AssetServer{
		meshes:    make(map[AssetId]MeshAsset),
		materials: make(map[AssetId]MaterialAsset),
	})
}

func makeAssetId() AssetId {
	return AssetId(uuid.NewString())
}
