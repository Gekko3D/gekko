package gekko

import (
	"image"
	"image/png"
	"os"

	"github.com/google/uuid"
)

type AssetId string

type TextureFormat uint32

const (
	TextureFormatR8Uint     TextureFormat = 0x00000003
	TextureFormatR8Unorm    TextureFormat = 0x00000001
	TextureFormatRGBA8Unorm TextureFormat = 0x00000012
	TextureFormatRGBA8Uint  TextureFormat = 0x00000015
	TextureFormatR16Float   TextureFormat = 0x00000007
)

type TextureDimension uint32

const (
	TextureDimension1D TextureDimension = 0x00000000
	TextureDimension2D TextureDimension = 0x00000001
	TextureDimension3D TextureDimension = 0x00000002
)

type AssetServer struct {
	meshes      map[AssetId]MeshAsset
	materials   map[AssetId]MaterialAsset
	textures    map[AssetId]TextureAsset
	samplers    map[AssetId]SamplerAsset
	voxModels   map[AssetId]VoxelModelAsset
	voxPalettes map[AssetId]VoxelPaletteAsset
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

type VoxelModelAsset struct {
	VoxModel  VoxModel
	BrickSize [3]uint32
}

type VoxelPaletteAsset struct {
	VoxPalette VoxPalette
	Materials  []VoxMaterial
	IsPBR      bool
	Roughness  float32
	Metalness  float32
	Emission   float32
	IOR        float32
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

func createVolumeTexels(voxModel *VoxModel, palette *VoxPalette) []uint8 {
	volume := make([]uint8, voxModel.SizeX*voxModel.SizeY*voxModel.SizeZ*4)
	for _, v := range voxModel.Voxels {
		idx := (int32(v.Z)*int32(voxModel.SizeY*voxModel.SizeX) + int32(v.Y)*int32(voxModel.SizeX) + int32(v.X)) * 4
		color := palette[v.ColorIndex]
		volume[idx+0] = color[0]
		volume[idx+1] = color[1]
		volume[idx+2] = color[2]
		volume[idx+3] = 255
	}
	return volume
}

func (server AssetServer) CreateVoxelBasedTexture(voxModel *VoxModel, palette *VoxPalette) AssetId {
	volumeTexels := createVolumeTexels(voxModel, palette)
	return server.CreateTextureFromTexels(volumeTexels[:], voxModel.SizeX, voxModel.SizeY, voxModel.SizeZ, TextureDimension3D, TextureFormatRGBA8Unorm)
}

func (server AssetServer) CreateVoxelModel(model VoxModel) AssetId {
	id := makeAssetId()
	server.voxModels[id] = VoxelModelAsset{
		VoxModel: model,
		//TODO calculate brick size based on model
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateVoxelPalette(palette VoxPalette, materials []VoxMaterial) AssetId {
	id := makeAssetId()
	server.voxPalettes[id] = VoxelPaletteAsset{
		VoxPalette: palette,
		Materials:  materials,
	}
	return id
}

func (server AssetServer) CreateSimplePalette(rgba [4]uint8) AssetId {
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}
	return server.CreateVoxelPalette(p, nil)
}

func (server AssetServer) CreatePBRPalette(rgba [4]uint8, roughness, metalness, emission, ior float32) AssetId {
	id := makeAssetId()
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}

	// This is a bit tricky because the Palette asset doesn't store PBR properties.
	// The VoxelModelAsset holds the model, but the palette is just colors.
	// HOWEVER, the VoxelRtModule's conversion logic can be extended to look for
	// "pseudo-materials" or we can add a new asset type.
	// For now, I'll store the PBR properties in a new VoxelPaletteAsset field.

	server.voxPalettes[id] = VoxelPaletteAsset{
		VoxPalette: p,
		IsPBR:      true,
		Roughness:  roughness,
		Metalness:  metalness,
		Emission:   emission,
		IOR:        ior,
	}
	return id
}

func (server AssetServer) CreateSphereModel(radius float32) AssetId {
	id := makeAssetId()
	r := int(radius)
	size := uint32(r*2 + 1)
	voxels := []Voxel{}
	r2 := radius * radius

	for x := -r; x <= r; x++ {
		for y := -r; y <= r; y++ {
			for z := -r; z <= r; z++ {
				fx, fy, fz := float32(x), float32(y), float32(z)
				if fx*fx+fy*fy+fz*fz <= r2 {
					voxels = append(voxels, Voxel{
						X:          uint8(x + r),
						Y:          uint8(y + r),
						Z:          uint8(z + r),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: size, SizeY: size, SizeZ: size,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateCubeModel(size float32) AssetId {
	id := makeAssetId()
	s := int(size)
	dim := uint32(s)
	voxels := []Voxel{}

	for x := 0; x < s; x++ {
		for y := 0; y < s; y++ {
			for z := 0; z < s; z++ {
				voxels = append(voxels, Voxel{
					X: uint8(x), Y: uint8(y), Z: uint8(z),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: dim, SizeY: dim, SizeZ: dim,
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateConeModel(radius, height float32) AssetId {
	id := makeAssetId()
	r := int(radius)
	h := int(height)
	voxels := []Voxel{}

	for y := 0; y < h; y++ {
		currR := radius * (1.0 - float32(y)/height)
		currR2 := currR * currR
		for x := -r; x <= r; x++ {
			for z := -r; z <= r; z++ {
				fx, fz := float32(x), float32(z)
				if fx*fx+fz*fz <= currR2 {
					voxels = append(voxels, Voxel{
						X: uint8(x + r), Y: uint8(y), Z: uint8(z + r),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(r*2 + 1), SizeY: uint32(height), SizeZ: uint32(r*2 + 1),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreatePyramidModel(size, height float32) AssetId {
	id := makeAssetId()
	h := int(height)
	voxels := []Voxel{}
	halfS := size * 0.5

	for y := 0; y < h; y++ {
		scale := 1.0 - float32(y)/height
		limit := halfS * scale
		for x := int(-limit); x <= int(limit); x++ {
			for z := int(-limit); z <= int(limit); z++ {
				voxels = append(voxels, Voxel{
					X: uint8(float32(x) + halfS), Y: uint8(y), Z: uint8(float32(z) + halfS),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(size), SizeY: uint32(height), SizeZ: uint32(size),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
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

func (AssetServerModule) Install(app *App, cmd *Commands) {
	app.addResources(&AssetServer{
		meshes:      make(map[AssetId]MeshAsset),
		materials:   make(map[AssetId]MaterialAsset),
		textures:    make(map[AssetId]TextureAsset),
		samplers:    make(map[AssetId]SamplerAsset),
		voxModels:   make(map[AssetId]VoxelModelAsset),
		voxPalettes: make(map[AssetId]VoxelPaletteAsset),
	})
}

func makeAssetId() AssetId {
	return AssetId(uuid.NewString())
}
