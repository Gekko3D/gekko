package gekko

import (
	"github.com/google/uuid"
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

type Vec3 struct{ X, Y, Z float32 }
type MeshAsset struct {
	version  uint
	vertices []Vec3
}

type MaterialAsset struct {
	version       uint
	color         Vec3
	shaderListing string
}

func (server AssetServer) LoadMesh(filename string) Mesh {
	id := makeAssetId()

	server.meshes[id] = MeshAsset{
		/*...*/
		version:  0,
		vertices: make([]Vec3, 0),
	}

	return Mesh{
		assetId: id,
	}
}

func (server AssetServer) LoadMaterial(shaderFilename string, color Vec3) Material {
	id := makeAssetId()

	server.materials[id] = MaterialAsset{
		version: 0,
		color:   color,
		shaderListing: `
			struct Uniforms {
				mvp: mat4x4<f32>,
			};
			@group(0) @binding(0)
			var<uniform> uniforms: Uniforms;

			@vertex
			fn vs_main(@location(0) position: vec2<f32>, @location(1) color: vec3<f32>)
				-> @builtin(position) vec4<f32> {
				let pos = vec4<f32>(position, 0.0, 1.0);
				return uniforms.mvp * pos;
			}

			@fragment
			fn fs_main(@location(0) color: vec3<f32>) -> @location(0) vec4<f32> {
				return vec4<f32>(color, 1.0);
			}`,
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
