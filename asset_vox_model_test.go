package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestDeleteVoxelGeometryRemovesSharedCacheEntry(t *testing.T) {
	server := &AssetServer{
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	cacheKey := "test:shared-geom"

	id := server.RegisterSharedVoxelGeometryWithCacheKey(cacheKey, xbm, cacheKey)
	if _, ok := server.GetVoxelGeometry(id); !ok {
		t.Fatal("expected registered shared geometry")
	}

	if !server.DeleteVoxelGeometry(id) {
		t.Fatal("expected shared geometry delete to succeed")
	}
	if _, ok := server.GetVoxelGeometry(id); ok {
		t.Fatal("expected geometry to be removed from asset storage")
	}
	if cachedID, ok := server.voxModelKeys[cacheKey]; ok && cachedID == id {
		t.Fatal("expected geometry cache key to be removed")
	}
}

func TestCreateFrameModelBuildsHollowInterior(t *testing.T) {
	server := &AssetServer{
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	id := server.CreateFrameModel(10, 4, 8, 2, 1)
	geometry, ok := server.GetVoxelGeometry(id)
	if !ok || geometry.XBrickMap == nil {
		t.Fatal("expected frame geometry asset")
	}

	if found, value := geometry.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatalf("expected outer frame voxel at origin, got found=%v value=%d", found, value)
	}
	if found, value := geometry.XBrickMap.GetVoxel(1, 2, 6); !found || value != 1 {
		t.Fatalf("expected near-edge frame voxel, got found=%v value=%d", found, value)
	}
	if found, _ := geometry.XBrickMap.GetVoxel(3, 1, 3); found {
		t.Fatal("expected frame interior to remain hollow")
	}
}

func TestEntityLODSimplifiedGeometryCachesAndShrinksVoxelCount(t *testing.T) {
	server := &AssetServer{
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	geometryID := server.CreateCubeModel(8, 8, 8, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{180, 220, 255, 255})
	source, ok := server.GetVoxelGeometry(geometryID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	simplifiedID, simplified, ok := server.entityLODSimplifiedGeometry(geometryID, paletteID, &source)
	if !ok || simplified == nil || simplified.XBrickMap == nil {
		t.Fatal("expected simplified geometry")
	}
	if simplifiedID == geometryID {
		t.Fatal("expected simplified geometry to use a separate asset id")
	}
	if simplified.XBrickMap.GetVoxelCount() >= source.XBrickMap.GetVoxelCount() {
		t.Fatalf("expected simplified geometry to shrink voxel count, got source=%d simplified=%d", source.XBrickMap.GetVoxelCount(), simplified.XBrickMap.GetVoxelCount())
	}

	simplifiedID2, _, ok := server.entityLODSimplifiedGeometry(geometryID, paletteID, &source)
	if !ok {
		t.Fatal("expected simplified geometry cache lookup")
	}
	if simplifiedID2 != simplifiedID {
		t.Fatalf("expected simplified cache reuse, got %s then %s", simplifiedID, simplifiedID2)
	}
}

func TestEntityLODImpostorTextureCachesAndProducesOpaquePixels(t *testing.T) {
	server := &AssetServer{
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	geometryID := server.CreateFrameModel(12, 12, 12, 2, 1.0)
	paletteID := server.CreateSimplePalette([4]uint8{255, 180, 96, 255})
	source, ok := server.GetVoxelGeometry(geometryID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	textureID, ok := server.entityLODImpostorTexture(geometryID, paletteID, &source)
	if !ok || textureID == (AssetId{}) {
		t.Fatal("expected impostor texture")
	}
	tex, ok := server.textures[textureID]
	if !ok {
		t.Fatal("expected impostor texture asset")
	}
	if tex.Width != entityLODImpostorTextureSize || tex.Height != entityLODImpostorTextureSize {
		t.Fatalf("expected %dx%d impostor texture, got %dx%d", entityLODImpostorTextureSize, entityLODImpostorTextureSize, tex.Width, tex.Height)
	}
	opaquePixels := 0
	for i := 3; i < len(tex.Texels); i += 4 {
		if tex.Texels[i] > 0 {
			opaquePixels++
		}
	}
	if opaquePixels == 0 {
		t.Fatal("expected impostor texture to contain visible pixels")
	}

	textureID2, ok := server.entityLODImpostorTexture(geometryID, paletteID, &source)
	if !ok {
		t.Fatal("expected impostor cache lookup")
	}
	if textureID2 != textureID {
		t.Fatalf("expected impostor cache reuse, got %s then %s", textureID, textureID2)
	}
}

func TestEntityLODImpostorTextureSkipsFullyTransparentFrontVoxels(t *testing.T) {
	server := &AssetServer{
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 2,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 2},
			{X: 0, Y: 0, Z: 1, ColorIndex: 1},
		},
	}, 1.0)
	var palette VoxPalette
	palette[1] = [4]uint8{255, 64, 64, 0}
	palette[2] = [4]uint8{64, 200, 255, 255}
	paletteID := server.CreateVoxelPalette(palette, nil)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	textureID, ok := server.entityLODImpostorTexture(modelID, paletteID, &source)
	if !ok || textureID == (AssetId{}) {
		t.Fatal("expected impostor texture to be generated from opaque voxel behind transparent front voxel")
	}
	tex, ok := server.textures[textureID]
	if !ok {
		t.Fatal("expected impostor texture asset")
	}

	opaquePixels := 0
	for i := 0; i < len(tex.Texels); i += 4 {
		if tex.Texels[i+3] == 0 {
			continue
		}
		opaquePixels++
		if tex.Texels[i+0] != 64 || tex.Texels[i+1] != 200 || tex.Texels[i+2] != 255 || tex.Texels[i+3] != 255 {
			t.Fatalf("expected visible impostor texel to come from the opaque voxel behind the transparent front voxel, got rgba=%v", tex.Texels[i:i+4])
		}
	}
	if opaquePixels == 0 {
		t.Fatal("expected at least one visible impostor texel")
	}
}

func TestEntityLODImpostorTextureUsesOpaqueReadableCoverage(t *testing.T) {
	server := &AssetServer{
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	modelID := server.CreateVoxelModel(VoxModel{
		SizeX: 1,
		SizeY: 1,
		SizeZ: 1,
		Voxels: []Voxel{
			{X: 0, Y: 0, Z: 0, ColorIndex: 1},
		},
	}, 1.0)
	var palette VoxPalette
	palette[1] = [4]uint8{180, 220, 255, 96}
	paletteID := server.CreateVoxelPalette(palette, nil)
	source, ok := server.GetVoxelGeometry(modelID)
	if !ok || source.XBrickMap == nil {
		t.Fatal("expected source geometry")
	}

	textureID, ok := server.entityLODImpostorTexture(modelID, paletteID, &source)
	if !ok || textureID == (AssetId{}) {
		t.Fatal("expected impostor texture")
	}
	tex, ok := server.textures[textureID]
	if !ok {
		t.Fatal("expected impostor texture asset")
	}

	filled := 0
	for i := 0; i < len(tex.Texels); i += 4 {
		if tex.Texels[i+3] == 0 {
			continue
		}
		filled++
		if tex.Texels[i+0] != 180 || tex.Texels[i+1] != 220 || tex.Texels[i+2] != 255 || tex.Texels[i+3] != 255 {
			t.Fatalf("expected opaque readable impostor texel, got rgba=%v", tex.Texels[i:i+4])
		}
	}
	if filled < 16 {
		t.Fatalf("expected impostor projection to stamp a visible texel footprint, got %d filled texels", filled)
	}
}

func TestEntityLODDilateTransparentRGBCopiesNeighborColor(t *testing.T) {
	texels := make([]uint8, 3*3*4)
	center := (1*3 + 1) * 4
	texels[center+0] = 255
	texels[center+1] = 96
	texels[center+2] = 220
	texels[center+3] = 255

	entityLODDilateTransparentRGB(texels, 3, 3)

	coloredTransparent := 0
	for i := 0; i < len(texels); i += 4 {
		if texels[i+3] != 0 {
			continue
		}
		if texels[i+0] != 0 || texels[i+1] != 0 || texels[i+2] != 0 {
			coloredTransparent++
		}
	}
	if coloredTransparent == 0 {
		t.Fatal("expected transparent texels to carry neighbor RGB for stable filtering")
	}
}

func TestEntityLODDotTextureReturnsStableSharedAsset(t *testing.T) {
	server := &AssetServer{
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	id1 := server.entityLODDotTexture()
	id2 := server.entityLODDotTexture()
	if id1 == (AssetId{}) || id2 == (AssetId{}) {
		t.Fatal("expected dot texture ids")
	}
	if id1 != id2 {
		t.Fatalf("expected shared dot texture id, got %s and %s", id1, id2)
	}
	tex, ok := server.textures[id1]
	if !ok {
		t.Fatal("expected dot texture asset")
	}
	if tex.Width != entityLODDotTextureSize || tex.Height != entityLODDotTextureSize {
		t.Fatalf("expected %dx%d dot texture, got %dx%d", entityLODDotTextureSize, entityLODDotTextureSize, tex.Width, tex.Height)
	}
}
