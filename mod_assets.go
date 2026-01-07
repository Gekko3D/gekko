package gekko

import (
	"image"
	"image/png"
	"math"
	"os"

	"github.com/go-gl/mathgl/mgl32"
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
	voxFiles    map[AssetId]*VoxFile
}

type VoxelFileAsset struct {
	VoxFile *VoxFile
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

func (server AssetServer) CreateVoxelModel(model VoxModel, resolution float32) AssetId {
	if resolution != 1.0 && resolution > 0 {
		model = ScaleVoxModel(model, resolution)
	}
	id := makeAssetId()
	server.voxModels[id] = VoxelModelAsset{
		VoxModel: model,
		//TODO calculate brick size based on model
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateVoxelFile(voxFile *VoxFile) AssetId {
	id := makeAssetId()
	server.voxFiles[id] = voxFile
	// Automatically register all models in the file
	// (Note: some models might not be referenced by nodes, but we store them anyway)
	return id
}

func ScaleVoxModel(model VoxModel, scale float32) VoxModel {
	if scale <= 0 || scale == 1.0 {
		return model
	}
	newSizeX := uint32(math.Round(float64(float32(model.SizeX) * scale)))
	newSizeY := uint32(math.Round(float64(float32(model.SizeY) * scale)))
	newSizeZ := uint32(math.Round(float64(float32(model.SizeZ) * scale)))

	if newSizeX == 0 {
		newSizeX = 1
	}
	if newSizeY == 0 {
		newSizeY = 1
	}
	if newSizeZ == 0 {
		newSizeZ = 1
	}

	newVoxels := make([]Voxel, 0)

	if scale > 1.0 {
		// Upscaling
		for _, v := range model.Voxels {
			startX := uint32(float32(v.X) * scale)
			startY := uint32(float32(v.Y) * scale)
			startZ := uint32(float32(v.Z) * scale)
			endX := uint32(float32(v.X+1) * scale)
			endY := uint32(float32(v.Y+1) * scale)
			endZ := uint32(float32(v.Z+1) * scale)

			for x := startX; x < endX; x++ {
				for y := startY; y < endY; y++ {
					for z := startZ; z < endZ; z++ {
						if x < newSizeX && y < newSizeY && z < newSizeZ {
							newVoxels = append(newVoxels, Voxel{
								X: x, Y: y, Z: z,
								ColorIndex: v.ColorIndex,
							})
						}
					}
				}
			}
		}
	} else {
		// Downscaling with voting approximation
		type coord struct{ x, y, z uint32 }
		groups := make(map[coord]map[byte]int)
		for _, v := range model.Voxels {
			nx := uint32(float32(v.X) * scale)
			ny := uint32(float32(v.Y) * scale)
			nz := uint32(float32(v.Z) * scale)
			if nx >= newSizeX {
				nx = newSizeX - 1
			}
			if ny >= newSizeY {
				ny = newSizeY - 1
			}
			if nz >= newSizeZ {
				nz = newSizeZ - 1
			}
			c := coord{nx, ny, nz}
			if groups[c] == nil {
				groups[c] = make(map[byte]int)
			}
			groups[c][v.ColorIndex]++
		}

		for c, counts := range groups {
			maxCount := 0
			var bestColor byte
			for idx, count := range counts {
				if count > maxCount {
					maxCount = count
					bestColor = idx
				}
			}
			newVoxels = append(newVoxels, Voxel{
				X: c.x, Y: c.y, Z: c.z,
				ColorIndex: bestColor,
			})
		}
	}

	return VoxModel{
		SizeX: newSizeX, SizeY: newSizeY, SizeZ: newSizeZ,
		Voxels: newVoxels,
	}
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

func (server AssetServer) CreateSphereModel(radius float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledRadius := radius * resolution
	r := int(scaledRadius)
	size := uint32(r*2 + 1)
	voxels := []Voxel{}
	r2 := scaledRadius * scaledRadius

	for x := -r; x <= r; x++ {
		for y := -r; y <= r; y++ {
			for z := -r; z <= r; z++ {
				fx, fy, fz := float32(x), float32(y), float32(z)
				if fx*fx+fy*fy+fz*fz <= r2 {
					voxels = append(voxels, Voxel{
						X:          uint32(x + r),
						Y:          uint32(y + r),
						Z:          uint32(z + r),
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

func (server AssetServer) CreateCubeModel(sizeX, sizeY, sizeZ float32, resolution float32) AssetId {
	id := makeAssetId()
	sx, sy, sz := int(sizeX*resolution), int(sizeY*resolution), int(sizeZ*resolution)
	voxels := []Voxel{}

	for x := 0; x < sx; x++ {
		for y := 0; y < sy; y++ {
			for z := 0; z < sz; z++ {
				voxels = append(voxels, Voxel{
					X: uint32(x), Y: uint32(y), Z: uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(sx), SizeY: uint32(sy), SizeZ: uint32(sz),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreateConeModel(radius, height float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledRadius := radius * resolution
	scaledHeight := height * resolution
	r := int(scaledRadius)
	h := int(scaledHeight)
	voxels := []Voxel{}

	for z := 0; z < h; z++ {
		currR := scaledRadius * (1.0 - float32(z)/scaledHeight)
		currR2 := currR * currR
		for x := -r; x <= r; x++ {
			for y := -r; y <= r; y++ {
				fx, fy := float32(x), float32(y)
				if fx*fx+fy*fy <= currR2 {
					voxels = append(voxels, Voxel{
						X: uint32(x + r), Y: uint32(y + r), Z: uint32(z),
						ColorIndex: 1,
					})
				}
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(r*2 + 1), SizeY: uint32(r*2 + 1), SizeZ: uint32(h),
			Voxels: voxels,
		},
		BrickSize: [3]uint32{8, 8, 8},
	}
	return id
}

func (server AssetServer) CreatePyramidModel(size, height float32, resolution float32) AssetId {
	id := makeAssetId()
	scaledSize := size * resolution
	scaledHeight := height * resolution
	h := int(scaledHeight)
	voxels := []Voxel{}
	halfS := scaledSize * 0.5

	for z := 0; z < h; z++ {
		scale := 1.0 - float32(z)/scaledHeight
		limit := halfS * scale
		for x := int(-limit); x <= int(limit); x++ {
			for y := int(-limit); y <= int(limit); y++ {
				voxels = append(voxels, Voxel{
					X: uint32(float32(x) + halfS), Y: uint32(float32(y) + halfS), Z: uint32(z),
					ColorIndex: 1,
				})
			}
		}
	}

	server.voxModels[id] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: uint32(scaledSize), SizeY: uint32(scaledSize), SizeZ: uint32(scaledHeight),
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
		voxFiles:    make(map[AssetId]*VoxFile),
	})
}

func makeAssetId() AssetId {
	return AssetId(uuid.NewString())
}

func (server AssetServer) SpawnHierarchicalVoxelModel(cmd *Commands, voxId AssetId, rootTransform TransformComponent, voxelScale float32) EntityId {
	voxFile, ok := server.voxFiles[voxId]
	if !ok {
		panic("Voxel file asset not found")
	}

	paletteId := server.CreateVoxelPalette(voxFile.Palette, voxFile.VoxMaterials)

	// Create a root entity to hold the global transform
	rootEntity := cmd.AddEntity(
		&rootTransform,
		&LocalTransform{Position: rootTransform.Position, Rotation: rootTransform.Rotation, Scale: rootTransform.Scale},
		&WorldTransform{},
		&TransformComponent{Position: rootTransform.Position, Rotation: rootTransform.Rotation, Scale: rootTransform.Scale},
	)

	// We need a map to keep track of spawned entities by node ID to link children to parents
	nodeEntities := make(map[int]EntityId)

	// Node 0 is always the root transform in MagicaVoxel
	server.spawnVoxNode(cmd, voxFile, 0, rootEntity, nodeEntities, paletteId, voxelScale)

	return rootEntity
}

// Decode MagicaVoxel rotation byte to Quaternion and Scale
// Ported from dot_vox (Rust) to ensure correct handling of all 48 cases including reflections.
func decodeVoxRotation(r byte) (mgl32.Quat, mgl32.Vec3) {
	index_nz1 := int(r & 3)
	index_nz2 := int((r >> 2) & 3)
	flip := int((r >> 4) & 7)

	si := mgl32.Vec3{1.0, 1.0, 1.0}
	sf := mgl32.Vec3{-1.0, -1.0, -1.0}

	const SQRT_2_2 = float32(0.70710678) // sqrt(2)/2

	// Helper to create Quat from [x, y, z, w]
	q := func(x, y, z, w float32) mgl32.Quat {
		return mgl32.Quat{W: w, V: mgl32.Vec3{x, y, z}}
	}

	var quats [4]mgl32.Quat
	var mapping [8]int
	var scales [8]mgl32.Vec3

	// Default scales mapping (alternating si/sf) common to many cases in dot_vox
	// But dot_vox defines them explicitly per case.
	// si, sf, sf, si, sf, si, si, sf
	scales_standard := [8]mgl32.Vec3{si, sf, sf, si, sf, si, si, sf}
	// sf, si, si, sf, si, sf, sf, si
	scales_inverted := [8]mgl32.Vec3{sf, si, si, sf, si, sf, sf, si}

	switch {
	case index_nz1 == 0 && index_nz2 == 1:
		quats = [4]mgl32.Quat{
			q(0.0, 0.0, 0.0, 1.0),
			q(0.0, 0.0, 1.0, 0.0),
			q(0.0, 1.0, 0.0, 0.0),
			q(1.0, 0.0, 0.0, 0.0),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 0 && index_nz2 == 2:
		quats = [4]mgl32.Quat{
			q(0.0, SQRT_2_2, SQRT_2_2, 0.0),
			q(SQRT_2_2, 0.0, 0.0, SQRT_2_2),
			q(SQRT_2_2, 0.0, 0.0, -SQRT_2_2),
			q(0.0, SQRT_2_2, -SQRT_2_2, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	case index_nz1 == 1 && index_nz2 == 2:
		quats = [4]mgl32.Quat{
			q(0.5, 0.5, 0.5, -0.5),
			q(0.5, -0.5, 0.5, 0.5),
			q(0.5, -0.5, -0.5, -0.5),
			q(0.5, 0.5, -0.5, 0.5),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 1 && index_nz2 == 0:
		quats = [4]mgl32.Quat{
			q(0.0, 0.0, SQRT_2_2, SQRT_2_2),
			q(0.0, 0.0, SQRT_2_2, -SQRT_2_2),
			q(SQRT_2_2, SQRT_2_2, 0.0, 0.0),
			q(SQRT_2_2, -SQRT_2_2, 0.0, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	case index_nz1 == 2 && index_nz2 == 0:
		quats = [4]mgl32.Quat{
			q(0.5, 0.5, 0.5, 0.5),
			q(0.5, -0.5, -0.5, 0.5),
			q(0.5, 0.5, -0.5, -0.5),
			q(0.5, -0.5, 0.5, -0.5),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 2 && index_nz2 == 1:
		quats = [4]mgl32.Quat{
			q(0.0, SQRT_2_2, 0.0, -SQRT_2_2),
			q(SQRT_2_2, 0.0, SQRT_2_2, 0.0),
			q(0.0, SQRT_2_2, 0.0, SQRT_2_2),
			q(SQRT_2_2, 0.0, -SQRT_2_2, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	default:
		// Fallback for invalid rotation
		return mgl32.QuatIdent(), si
	}

	return quats[mapping[flip]], scales[flip]
}

func (server AssetServer) spawnVoxNode(cmd *Commands, voxFile *VoxFile, nodeId int, parentEntity EntityId, nodeEntities map[int]EntityId, paletteId AssetId, voxelScale float32) {
	node, ok := voxFile.Nodes[nodeId]
	if !ok {
		return
	}

	var currentEntity EntityId

	switch node.Type {
	case VoxNodeTransform:
		// Create a transform entity
		var pos mgl32.Vec3
		var rot mgl32.Quat
		var scale mgl32.Vec3

		// Decode Rotation and Scale using dot_vox logic
		if len(node.Frames) > 0 {
			f := node.Frames[0]
			const VoxelUnitSize = 0.1
			pos = mgl32.Vec3{f.LocalTrans[0], f.LocalTrans[1], f.LocalTrans[2]}.Mul(VoxelUnitSize)
			rot, scale = decodeVoxRotation(f.Rotation)
		} else {
			rot = mgl32.QuatIdent()
			scale = mgl32.Vec3{1, 1, 1}
		}

		// Fix 2: Pivot Offset Logic Moved to VoxNodeShape
		// We no longer apply the centering offset here. The Transform node represents the Pivot Point.
		// The Shape node will spawn a child entity offset by -Size/2 to center the mesh on this pivot.

		currentEntity = cmd.AddEntity(
			&LocalTransform{Position: pos, Rotation: rot, Scale: scale},
			&TransformComponent{Position: pos, Rotation: rot, Scale: scale}, // Added for compatibility with query
			&Parent{Entity: parentEntity},
			&WorldTransform{},
		)
		nodeEntities[node.ID] = currentEntity

		// Transform nodes have one child
		server.spawnVoxNode(cmd, voxFile, node.ChildID, currentEntity, nodeEntities, paletteId, voxelScale)

	case VoxNodeGroup:
		// Group nodes just collect children
		currentEntity = cmd.AddEntity(
			&LocalTransform{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
			&Parent{Entity: parentEntity},
			&WorldTransform{},
		)
		nodeEntities[node.ID] = currentEntity

		for _, childID := range node.ChildrenIDs {
			server.spawnVoxNode(cmd, voxFile, childID, currentEntity, nodeEntities, paletteId, voxelScale)
		}

	case VoxNodeShape:
		// Shape nodes hold model references
		// In MagicaVoxel, models pivot around their center.
		// Since the parent TransformNode is positioned at the Pivot Point (Joint),
		// we must spawn the Mesh as a Child Entity offset by -Size/2.
		for _, m := range node.Models {
			modelAssetId := server.CreateVoxelModel(voxFile.Models[m.ModelID], voxelScale)

			// Calculate centering offset
			model := voxFile.Models[m.ModelID]
			centerOffset := mgl32.Vec3{
				float32(model.SizeX) * -0.5,
				float32(model.SizeY) * -0.5,
				float32(model.SizeZ) * -0.5,
			}

			// Scale the offset to world units (using the same VoxelUnitSize as translation)
			const VoxelUnitSize = 0.1
			centerOffset = centerOffset.Mul(VoxelUnitSize)

			// Create a child entity for the mesh.
			// Rotation is Identity because the rotation is handled by the Parent TransformNode.
			// Scale is 1.0 because Scale is also handled by Parent TransformNode (usually).
			// Wait, the Parent Scale might need to apply to the Offset?
			// LocalTransform translates relative to parent. Parent scale scales the child's position?
			// In most engines, ChildPosition IS scaled by ParentScale.
			// So if ParentScale is (1,1,1), good. If ParentScale is (-1,-1,-1) [Reflection],
			// The Child Position (Offset) will be flipped too.
			// If Offset is (-5, -5, -5) and ParentScale is (-1), EffPos is (5,5,5).
			// This might be what we want to keep the mesh inside the reflection?

			cmd.AddEntity(
				&LocalTransform{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&TransformComponent{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&Parent{Entity: parentEntity}, // Attached to the TransformNode (Pivot)
				&WorldTransform{},
				&VoxelModelComponent{
					VoxelModel:   modelAssetId,
					VoxelPalette: paletteId,
				},
			)
		}
		// Shape nodes are leaves in the scene graph for purposes of hierarchy (they attach to their parent nTRN)
	}
}
