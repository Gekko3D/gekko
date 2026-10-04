package gekko

import (
	"reflect"
	"sync"

	rootassets "github.com/gekko3d/gekko/assets"
	"github.com/gekko3d/gekko/content"
)

type AssetId = rootassets.AssetID
type TextureFormat = rootassets.TextureFormat

const (
	TextureFormatRGBA8Unorm     = rootassets.TextureFormatRGBA8Unorm
	TextureFormatRGBA8UnormSrgb = rootassets.TextureFormatRGBA8UnormSrgb
)

type TextureDimension = rootassets.TextureDimension

const (
	TextureDimension1D = rootassets.TextureDimension1D
	TextureDimension2D = rootassets.TextureDimension2D
	TextureDimension3D = rootassets.TextureDimension3D
)

type Mesh = rootassets.Mesh
type Material = rootassets.Material
type Voxel = rootassets.Voxel
type VoxModel = rootassets.VoxModel
type VoxPalette = rootassets.VoxPalette
type VoxFile = rootassets.VoxFile
type VoxNodeType = rootassets.VoxNodeType
type VoxNode = rootassets.VoxNode
type VoxTransformFrame = rootassets.VoxTransformFrame
type VoxShapeModel = rootassets.VoxShapeModel
type VoxMaterial = rootassets.VoxMaterial
type VoxelFileAsset = rootassets.VoxelFileAsset
type VoxelGeometryAsset = rootassets.VoxelGeometryAsset
type VoxelModelAsset = rootassets.VoxelModelAsset
type VoxelPaletteAsset = rootassets.VoxelPaletteAsset
type VoxelSurfaceMaterial = rootassets.VoxelSurfaceMaterial
type VoxelPaletteAnimation = rootassets.VoxelPaletteAnimation
type VoxelPaletteAnimationFrame = rootassets.VoxelPaletteAnimationFrame
type VoxelPaletteUVScroll = rootassets.VoxelPaletteUVScroll
type VoxelPaletteMaterialFrameOverride = rootassets.VoxelPaletteMaterialFrameOverride
type MeshAsset = rootassets.MeshAsset
type MaterialAsset = rootassets.MaterialAsset
type TextureAsset = rootassets.TextureAsset
type SamplerAsset = rootassets.SamplerAsset

const (
	VoxNodeTransform = rootassets.VoxNodeTransform
	VoxNodeGroup     = rootassets.VoxNodeGroup
	VoxNodeShape     = rootassets.VoxNodeShape
)

type AssetServer struct {
	mu                    sync.RWMutex
	managedVoxelGeometry  map[AssetId]*managedVoxelGeometry
	compiledAssetLODs     map[AssetId]*compiledAssetLODBinding
	compiledAssetLODStats compiledAssetLODStats
	authoredVoxelBases    map[AssetId]map[content.VoxelObjectLatticeDef]string
	meshes                map[AssetId]MeshAsset
	materials             map[AssetId]MaterialAsset
	textures              map[AssetId]TextureAsset
	textureKeys           map[string]AssetId
	samplers              map[AssetId]SamplerAsset
	voxModels             map[AssetId]VoxelGeometryAsset
	voxModelKeys          map[string]AssetId
	voxPalettes           map[AssetId]VoxelPaletteAsset
	voxPaletteKeys        map[string]AssetId
	voxFiles              map[AssetId]*VoxFile

	preparedVoxelRendererCopies    map[AssetId]preparedVoxelRendererCopy
	preparedVoxelRendererCopyStats PreparedVoxelRendererCopyStats
	authoredVoxelCollapseStats     AuthoredVoxelCollapseStats
}

// AuthoredVoxelCollapseStats reports cold rasterization attempts and validated
// warm composite reuses. Reading these counters performs no geometry work.
type AuthoredVoxelCollapseStats struct {
	Builds uint64
	Hits   uint64
}

func (server *AssetServer) AuthoredVoxelCollapseStats() AuthoredVoxelCollapseStats {
	if server == nil {
		return AuthoredVoxelCollapseStats{}
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.authoredVoxelCollapseStats
}

type AssetServerModule struct{}

func (AssetServerModule) Install(app *App, cmd *Commands) {
	server := &AssetServer{
		meshes:         make(map[AssetId]MeshAsset),
		materials:      make(map[AssetId]MaterialAsset),
		textures:       make(map[AssetId]TextureAsset),
		textureKeys:    make(map[string]AssetId),
		samplers:       make(map[AssetId]SamplerAsset),
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
	cmd.AddResources(server)
}

func (server *AssetServer) GetVoxelGeometry(id AssetId) (VoxelGeometryAsset, bool) {
	server.mu.Lock()
	if entry := server.managedVoxelGeometry[id]; entry != nil && !entry.exposed {
		asset := server.voxModels[id]
		asset.XBrickMap = entry.owner.ExposeMutable()
		asset.XBrickMap.ComputeAABB()
		server.voxModels[id] = asset
		entry.exposed = true
		entry.authoredBase = authoredVoxelBase{}
		entry.generation++
	}
	server.mu.Unlock()
	return server.getVoxelGeometry(id)
}

// Internal reads preserve managed sealing and never expose mutable authority.
func (server *AssetServer) getVoxelGeometry(id AssetId) (VoxelGeometryAsset, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	m, ok := server.voxModels[id]
	if ok && m.XBrickMap == nil && len(m.VoxModel.Voxels) > 0 {
		hydrated := buildVoxelGeometryAsset(m.VoxModel, m.SourcePath)
		hydrated.RuntimeOwned = m.RuntimeOwned
		m.XBrickMap = hydrated.XBrickMap
		m.LocalMin = hydrated.LocalMin
		m.LocalMax = hydrated.LocalMax
		if m.BrickSize == [3]uint32{} {
			m.BrickSize = hydrated.BrickSize
		}
		server.voxModels[id] = m
	}
	return m, ok
}

func (server *AssetServer) GetVoxelModel(id AssetId) (VoxelModelAsset, bool) {
	return server.GetVoxelGeometry(id)
}

func (server *AssetServer) GetVoxelPalette(id AssetId) (VoxelPaletteAsset, bool) {
	server.mu.RLock()
	defer server.mu.RUnlock()
	palette, ok := server.voxPalettes[id]
	return palette, ok
}

func makeAssetId() AssetId {
	return rootassets.NewID()
}

func assetServerFromApp(app *App) *AssetServer {
	if app == nil {
		return nil
	}
	if resource, ok := app.resources[reflect.TypeOf(AssetServer{})]; ok {
		return resource.(*AssetServer)
	}
	return nil
}
