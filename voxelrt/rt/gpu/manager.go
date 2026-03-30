package gpu

import (
	"sync"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

const (
	HeadroomPayload = 4 * 1024 * 1024
	HeadroomTables  = 64 * 1024

	MaxUpdatesPerFrame  = 1024               // Cap voxel/sector updates per frame
	SafeBufferSizeLimit = 1024 * 1024 * 1024 // 1GB Warning/Compaction Limit

	// Texture Atlas Constants
	MaxVoxelAtlasPages = 4
	AtlasBricksPerSide = 128                                   // 128^3 = 2,097,152 bricks (1GB at 512 bytes per brick)
	AtlasSize          = AtlasBricksPerSide * volume.BrickSize // 1024 voxels per side if BrickSize is 8
	BrickRecordSize    = 20

	TiledLightingTileSize         = 16
	TiledLightingMaxLightsPerTile = 128
)

type GpuSkyboxLayer struct {
	ColorA      [4]float32 // xyz: color, w: threshold
	ColorB      [4]float32 // xyz: color, w: opacity
	Offset      [4]float32 // xyz: offset, w: scale
	Persistence float32
	Lacunarity  float32
	Seed        int32
	Octaves     int32
	BlendMode   uint32
	Invert      uint32
	LayerType   uint32
	Pad2        uint32
}

type GpuSkyboxUniforms struct {
	LayerCount uint32
	Pad1       uint32
	Pad2       uint32
	Pad3       uint32
	SunDir     [4]float32 // xyz: dir, w: intensity
	SunColor   [4]float32 // xyz: halo color, w: core glow strength
	SunParams  [4]float32 // x: core glow exponent, y: atmosphere exponent, z: atmosphere glow strength
	DiskColor  [4]float32 // xyz: disk color, w: disk strength
	DiskParams [4]float32 // x: disk start, y: disk end
}

type SpriteAtlasResource struct {
	Texture *wgpu.Texture
	View    *wgpu.TextureView
	Version uint
}

type SpriteRenderBatch struct {
	FirstInstance uint32
	InstanceCount uint32
	BindGroup0    *wgpu.BindGroup
}

type CAVolumeHost struct {
	EntityID        uint32
	Type            uint32
	Preset          uint32
	Resolution      [3]uint32
	Position        mgl32.Vec3
	Rotation        mgl32.Quat
	VoxelScale      mgl32.Vec3
	Intensity       float32
	Diffusion       float32
	Buoyancy        float32
	Cooling         float32
	Dissipation     float32
	Extinction      float32
	Emission        float32
	StepsPending    float32
	StepDt          float32
	ScatterColor    [3]float32
	ShadowTint      [3]float32
	AbsorptionColor [3]float32
}

type CAPresetData struct {
	SmokeSeed        float32
	FireSeed         float32
	SmokeInject      float32
	FireInject       float32
	Diffusion        float32
	Buoyancy         float32
	Cooling          float32
	Dissipation      float32
	SmokeDensityCut  float32
	FireHeatCut      float32
	SigmaTSmoke      float32
	SigmaTFire       float32
	AlphaScaleSmoke  float32
	AlphaScaleFire   float32
	AbsorptionScale  float32
	ScatterScale     float32
	EmberTint        [4]float32
	FireCoreTint     [4]float32
	Flags            uint32
	Pad1, Pad2, Pad3 uint32
}

type GpuBufferManager struct {
	Device *wgpu.Device

	LightingQuality core.LightingQualityConfig

	CameraBuf            *wgpu.Buffer
	InstancesBuf         *wgpu.Buffer
	BVHNodesBuf          *wgpu.Buffer
	ShadowInstancesBuf   *wgpu.Buffer
	ShadowBVHNodesBuf    *wgpu.Buffer
	LightsBuf            *wgpu.Buffer
	ShadowUpdatesBuf     *wgpu.Buffer
	ShadowLayerParamsBuf *wgpu.Buffer
	TileLightParamsBuf   *wgpu.Buffer
	TileLightHeadersBuf  *wgpu.Buffer
	TileLightIndicesBuf  *wgpu.Buffer

	MaterialBuf           *wgpu.Buffer
	SectorTableBuf        *wgpu.Buffer
	BrickTableBuf         *wgpu.Buffer
	VoxelPayloadTex       [MaxVoxelAtlasPages]*wgpu.Texture
	VoxelPayloadView      [MaxVoxelAtlasPages]*wgpu.TextureView
	VoxelPayloadPageSize  uint32
	VoxelPayloadPageCount uint32
	VoxelPayloadBricks    uint32
	ObjectParamsBuf       *wgpu.Buffer
	ShadowObjectParamsBuf *wgpu.Buffer
	Tree64Buf             *wgpu.Buffer
	SectorGridBuf         *wgpu.Buffer
	SectorGridParamsBuf   *wgpu.Buffer
	TerrainChunkLookupBuf *wgpu.Buffer

	// G-Buffer Textures
	GBufferDepth    *wgpu.Texture
	GBufferNormal   *wgpu.Texture
	GBufferMaterial *wgpu.Texture
	GBufferPosition *wgpu.Texture

	// Transparent Accumulation Targets (WBOIT)
	TransparentAccumTex  *wgpu.Texture // RGBA16Float, accum premultiplied color
	TransparentWeightTex *wgpu.Texture // R16Float, accum weight

	// G-Buffer Views
	DepthView    *wgpu.TextureView
	NormalView   *wgpu.TextureView
	MaterialView *wgpu.TextureView
	PositionView *wgpu.TextureView

	// Transparent Accumulation Views
	TransparentAccumView  *wgpu.TextureView
	TransparentWeightView *wgpu.TextureView

	// Shadow Map Resources
	ShadowMapArray           *wgpu.Texture
	ShadowMapView            *wgpu.TextureView
	ShadowMapLayers          uint32
	DirectionalShadowArrays  [core.DirectionalShadowCascadeCount]*wgpu.Texture
	DirectionalShadowViews   [core.DirectionalShadowCascadeCount]*wgpu.TextureView
	DirectionalShadowLayers  uint32
	ShadowLayerParams        []ShadowLayerParams
	shadowCacheStates        []shadowCacheState
	shadowCachedCascades     []core.DirectionalShadowCascade
	shadowTierOffsets        [shadowTierCount]int
	VoxelUploadRevision      uint64
	shadowDirectionalVolumes []directionalShadowCullVolume
	shadowSpotVolumes        []spotShadowCullVolume

	// Skybox Resources
	SkyboxTex            *wgpu.Texture
	SkyboxView           *wgpu.TextureView
	SkyboxSampler        *wgpu.Sampler
	SkyboxSamplerNearest *wgpu.Sampler
	SkyboxSmooth         bool
	SkyboxLayersBuf      *wgpu.Buffer
	SkyboxParamsBuf      *wgpu.Buffer
	SkyboxGenPipeline    *wgpu.ComputePipeline
	SkyboxGenBindGroup   *wgpu.BindGroup
	SkyboxRevision       uint64

	// Hi-Z Occlusion
	HiZTexture     *wgpu.Texture
	HiZViews       []*wgpu.TextureView // Mip views
	ReadbackBuffer *wgpu.Buffer
	HiZPipeline    *wgpu.ComputePipeline
	HiZBindGroups  []*wgpu.BindGroup // One per mip transition

	HiZReadbackLevel   uint32
	HiZReadbackWidth   uint32
	HiZReadbackHeight  uint32
	HiZState           int // 0: Idle, 1: Copy (GPU), 2: Mapping (Wait GPU), 3: Mapped (Read CPU)
	StateMu            sync.Mutex
	LastHiZData        []float32
	LastHiZW, LastHiZH uint32

	// Bind Groups for new passes
	GBufferBindGroup          *wgpu.BindGroup
	GBufferBindGroup0         *wgpu.BindGroup
	GBufferBindGroup2         *wgpu.BindGroup
	LightingBindGroup         *wgpu.BindGroup
	LightingBindGroup2        *wgpu.BindGroup // For G-Buffer inputs and output
	LightingBindGroupMaterial *wgpu.BindGroup // For Group 2 voxel data
	LightingTileBindGroup     *wgpu.BindGroup // For Group 3 tiled light lists
	TiledLightCullBindGroup0  *wgpu.BindGroup
	TiledLightCullBindGroup1  *wgpu.BindGroup

	// Shadow Map Bind Groups
	ShadowPipeline   *wgpu.ComputePipeline
	ShadowBindGroup0 *wgpu.BindGroup
	ShadowBindGroup1 *wgpu.BindGroup
	ShadowBindGroup2 *wgpu.BindGroup

	DebugBindGroup0 *wgpu.BindGroup

	// Particles (rendered after lighting)
	ParticlePoolBuf      *wgpu.Buffer
	ParticleDeadPoolBuf  *wgpu.Buffer
	ParticleAliveListBuf *wgpu.Buffer
	ParticleCountersBuf  *wgpu.Buffer
	ParticleIndirectBuf  *wgpu.Buffer
	ParticleEmittersBuf  *wgpu.Buffer
	ParticleSpawnBuf     *wgpu.Buffer // SpawnRequests
	ParticleParamsBuf    *wgpu.Buffer
	ParticleAtlasTex     *wgpu.Texture
	ParticleAtlasView    *wgpu.TextureView
	ParticleAtlasSampler *wgpu.Sampler
	ParticleSimPipeline  *wgpu.ComputePipeline
	ParticleSimBG0       *wgpu.BindGroup
	ParticleSimBG1       *wgpu.BindGroup
	ParticleSimBG2       *wgpu.BindGroup
	ParticlesBindGroup0  *wgpu.BindGroup // camera + pool + alive_list
	ParticlesBindGroup1  *wgpu.BindGroup // gbuffer depth
	ParticleCount        uint32
	MaxParticleCount     uint32

	// Sprites (billboards, UI or world)
	SpriteBuf          *wgpu.Buffer
	SpriteCount        uint32
	SpriteAtlasSampler *wgpu.Sampler
	SpriteAtlases      map[string]*SpriteAtlasResource
	SpriteBatches      []SpriteRenderBatch
	SpritesBindGroup1  *wgpu.BindGroup // gbuffer depth

	// Transparent overlay (single-layer transparency over lit image)
	TransparentBG0 *wgpu.BindGroup // camera + instances + BVH
	TransparentBG1 *wgpu.BindGroup // voxel data buffers
	TransparentBG2 *wgpu.BindGroup // gbuffer depth/material/shadows
	TransparentBG3 *wgpu.BindGroup // tiled light lists
	StorageView    *wgpu.TextureView

	// GPU cellular automata + volumetric rendering
	CAVolumeBuf            *wgpu.Buffer
	CABoundsBuf            *wgpu.Buffer
	CAParamsBuf            *wgpu.Buffer
	CAPresetBuf            *wgpu.Buffer
	CAFieldTexA            *wgpu.Texture
	CAFieldTexB            *wgpu.Texture
	CAFieldViewA           *wgpu.TextureView
	CAFieldViewB           *wgpu.TextureView
	CAVolumeSimPipeline    *wgpu.ComputePipeline
	CAVolumeBoundsPipeline *wgpu.ComputePipeline
	CAVolumeSimBG0         *wgpu.BindGroup
	CAVolumeSimBG1A        *wgpu.BindGroup
	CAVolumeSimBG1B        *wgpu.BindGroup
	CAVolumeBoundsBG0      *wgpu.BindGroup
	CAVolumeBoundsBG1A     *wgpu.BindGroup
	CAVolumeBoundsBG1B     *wgpu.BindGroup
	CAVolumeRenderBG0      *wgpu.BindGroup
	CAVolumeRenderBG1A     *wgpu.BindGroup
	CAVolumeRenderBG1B     *wgpu.BindGroup
	CAVolumeRenderBG2      *wgpu.BindGroup
	CAVolumeCount          uint32
	CAAtlasWidth           uint32
	CAAtlasHeight          uint32
	CAAtlasDepth           uint32
	CAFieldIndex           int
	CAElapsedTime          float32
	CAVolumeBindingsDirty  bool
	caLayout               []caVolumeLayout

	// Batch update tracking
	BatchMode      bool                       // Enable batching of updates within a frame
	PendingUpdates map[*volume.XBrickMap]bool // Maps with pending updates in current batch

	// Allocators for global pools
	SectorAlloc  SlotAllocator
	BrickAlloc   SlotAllocator                     // Allocates blocks of 64 bricks
	PayloadAlloc [MaxVoxelAtlasPages]SlotAllocator // Allocates bricks (512 bytes each) per atlas page

	// Mapping from volume objects to GPU slots
	SectorToInfo map[*volume.Sector]SectorGpuInfo
	BrickToSlot  map[*volume.Brick]PayloadSlot

	MaterialAlloc       SlotAllocator // Allocates blocks of 256 materials (16KB each)
	Allocations         map[*volume.XBrickMap]*ObjectGpuAllocation
	MaterialAllocations map[*core.VoxelObject]*MaterialGpuAllocation

	// Smooth streaming state
	SectorsPerFrame          uint32
	lastTotalSectors         int
	lastSceneRevision        uint64
	gridDataPool             []byte
	TileLightTilesX          uint32
	TileLightTilesY          uint32
	TileLightAvgCount        int
	TileLightMaxCount        int
	VoxelSectorsUploaded     int
	VoxelBricksUploaded      int
	VoxelDirtySectorsPending int
	VoxelDirtyBricksPending  int
}

type caVolumeLayout struct {
	EntityID   uint32
	Type       uint32
	Resolution [3]uint32
}

// ObjectGpuAllocation tracks the GPU memory regions assigned to a specific object.
type ObjectGpuAllocation struct {
	Sectors map[[3]int]*volume.Sector     // Track which sector is at which coordinate
	Bricks  map[[3]int]*[64]*volume.Brick // Track pointers per sector to detect brick removal
}

type MaterialGpuAllocation struct {
	MaterialOffset   uint32 // In elements (64 bytes each)
	MaterialCapacity uint32 // In elements
}

type SectorGpuInfo struct {
	SlotIndex       uint32
	BrickTableIndex uint32 // Index into global BrickTableBuf (64 slots per sector)
}

type PayloadSlot struct {
	Page uint32
	Slot uint32
}

type SlotAllocator struct {
	Tail uint32
	Free []uint32
}

func (a *SlotAllocator) Alloc() uint32 {
	if len(a.Free) > 0 {
		idx := a.Free[len(a.Free)-1]
		a.Free = a.Free[:len(a.Free)-1]
		return idx
	}
	idx := a.Tail
	a.Tail++
	return idx
}

func (a *SlotAllocator) FreeSlot(idx uint32) {
	a.Free = append(a.Free, idx)
}

func NewGpuBufferManager(device *wgpu.Device) *GpuBufferManager {
	pageSize := computeVoxelPayloadPageSize(device.GetLimits().Limits.MaxTextureDimension3D)
	m := &GpuBufferManager{
		Device:                device,
		LightingQuality:       core.DefaultLightingQualityConfig(),
		BatchMode:             false,
		SectorsPerFrame:       MaxUpdatesPerFrame,
		VoxelPayloadPageSize:  pageSize,
		VoxelPayloadPageCount: MaxVoxelAtlasPages,
		VoxelPayloadBricks:    pageSize / volume.BrickSize,
	}
	m.Allocations = make(map[*volume.XBrickMap]*ObjectGpuAllocation)
	m.MaterialAllocations = make(map[*core.VoxelObject]*MaterialGpuAllocation)
	m.PendingUpdates = make(map[*volume.XBrickMap]bool)
	m.SectorToInfo = make(map[*volume.Sector]SectorGpuInfo)
	m.BrickToSlot = make(map[*volume.Brick]PayloadSlot)
	m.SpriteAtlases = make(map[string]*SpriteAtlasResource)

	// Pre-allocate minimal buffers to avoid bind group validation errors at startup
	m.ensureBuffer("SectorTableBuf", &m.SectorTableBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("BrickTableBuf", &m.BrickTableBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("MaterialBuf", &m.MaterialBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ObjectParamsBuf", &m.ObjectParamsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ShadowObjectParamsBuf", &m.ShadowObjectParamsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("Tree64Buf", &m.Tree64Buf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("SectorGridBuf", &m.SectorGridBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("SectorGridParamsBuf", &m.SectorGridParamsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("TerrainChunkLookupBuf", &m.TerrainChunkLookupBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("CameraBuf", &m.CameraBuf, nil, wgpu.BufferUsageUniform, 1024)
	m.ensureBuffer("InstancesBuf", &m.InstancesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("BVHNodesBuf", &m.BVHNodesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ShadowInstancesBuf", &m.ShadowInstancesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ShadowBVHNodesBuf", &m.ShadowBVHNodesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("LightsBuf", &m.LightsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ShadowUpdatesBuf", &m.ShadowUpdatesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("ShadowLayerParamsBuf", &m.ShadowLayerParamsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("TileLightParamsBuf", &m.TileLightParamsBuf, nil, wgpu.BufferUsageUniform, 256)
	m.ensureBuffer("TileLightHeadersBuf", &m.TileLightHeadersBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("TileLightIndicesBuf", &m.TileLightIndicesBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("CABoundsBuf", &m.CABoundsBuf, nil, wgpu.BufferUsageStorage, 1024)
	m.ensureBuffer("CAPresetBuf", &m.CAPresetBuf, nil, wgpu.BufferUsageStorage, 4096)
	m.ensureBuffer("SpriteBuf", &m.SpriteBuf, nil, wgpu.BufferUsageStorage, 1024)

	return m
}

func computeVoxelPayloadPageSize(maxTextureDimension3D uint32) uint32 {
	size := uint32(AtlasSize)
	if maxTextureDimension3D != 0 && maxTextureDimension3D < size {
		size = maxTextureDimension3D
	}
	size -= size % volume.BrickSize
	if size < volume.BrickSize {
		panic("maxTextureDimension3D is too small for voxel payload pages")
	}
	return size
}

func (m *GpuBufferManager) appendVoxelPayloadEntries(entries []wgpu.BindGroupEntry, startBinding uint32) []wgpu.BindGroupEntry {
	for i := uint32(0); i < MaxVoxelAtlasPages; i++ {
		entries = append(entries, wgpu.BindGroupEntry{
			Binding:     startBinding + i,
			TextureView: m.VoxelPayloadView[i],
		})
	}
	return entries
}

// CreateTransparentOverlayBindGroups wires the overlay pass bind groups:
// Group 0: camera (uniform) + instances (storage) + BVH nodes (storage)
// Group 1: voxel data buffers (sector, brick, payload, object params, tree64, sector grid, sector grid params)
// Group 2: gbuffer depth/material + shadow maps + lit opaque color
// Group 3: tiled light list buffers
func (m *GpuBufferManager) CreateTransparentOverlayBindGroups(pipeline *wgpu.RenderPipeline) {
	if pipeline == nil {
		return
	}
	var err error

	// Group 0
	m.TransparentBG0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.InstancesBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.BVHNodesBuf, Size: wgpu.WholeSize},
			{Binding: 3, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
			{Binding: 4, Buffer: m.ShadowLayerParamsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	// Group 1
	m.TransparentBG1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: m.appendVoxelPayloadEntries([]wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.SectorTableBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.BrickTableBuf, Size: wgpu.WholeSize},
			{Binding: 6, Buffer: m.MaterialBuf, Size: wgpu.WholeSize},
			{Binding: 7, Buffer: m.ObjectParamsBuf, Size: wgpu.WholeSize},
			{Binding: 8, Buffer: m.Tree64Buf, Size: wgpu.WholeSize},
			{Binding: 9, Buffer: m.SectorGridBuf, Size: wgpu.WholeSize},
			{Binding: 10, Buffer: m.SectorGridParamsBuf, Size: wgpu.WholeSize},
		}, 2),
	})
	if err != nil {
		panic(err)
	}

	// Group 2
	m.TransparentBG2, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(2),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: m.DepthView},
			{Binding: 1, TextureView: m.MaterialView},
			{Binding: 2, TextureView: m.ShadowMapView},
			{Binding: 3, TextureView: m.StorageView},
		},
	})
	if err != nil {
		panic(err)
	}

	m.TransparentBG3, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(3),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.TileLightParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.TileLightHeadersBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.TileLightIndicesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}
