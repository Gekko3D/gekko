package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

const EmptyBrickValue uint32 = 0xFFFFFFFF
const DirectColorFlag uint32 = 0x80000000

type VoxelModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
}

type sqVertex struct {
	pos      [4]float32 `gekko:"layout" location:"0" format:"float4"`
	texCoord [2]float32 `gekko:"layout" location:"1" format:"float2"`
}

type renderParametersUniform struct {
	WindowWidth     uint32
	WindowHeight    uint32
	EmptyBlockValue uint32
	Pad0            uint32
}

type voxelCameraUniform struct {
	ViewProjMx    mgl32.Mat4
	InvViewProjMx mgl32.Mat4
	Position      mgl32.Vec4
}

type transformsUniform struct {
	ModelMx    mgl32.Mat4
	InvModelMx mgl32.Mat4
}

type voxelInstanceUniform struct {
	Size      [3]uint32 // Size in voxels
	PaletteId uint32
	MacroGrid macroGridUniform
}

type macroGridUniform struct {
	Size       [3]uint32 // Dimensions in macro blocks
	Pad0       uint32    // Align to 16 bytes for next vec3u
	BrickSize  [3]uint32 // Voxels per brick (e.g., 8,8,8)
	DataOffset uint32    // Offset in macroIndex
}

type brickUniform struct {
	Position   [3]uint32 // Position in macro grid
	DataOffset uint32    // Offset in voxel pool
}

type voxelUniform struct {
	ColorIndex uint32
	Alpha      float32
}

type aabbUniform struct {
	Min mgl32.Vec4
	Max mgl32.Vec4
}

type visibleListParametersUniform struct {
	Count uint32
	Pad0  uint32
	Pad1  uint32
	Pad2  uint32
}

type voxelRenderState struct {
	screenQuadVertices           []sqVertex
	screenQuadIndices            []uint16
	vertexBuffer                 *wgpu.Buffer
	indexBuffer                  *wgpu.Buffer
	vertexCount                  uint32
	blitPipeline                 *wgpu.RenderPipeline
	computePipeline              *wgpu.ComputePipeline
	voxelComputeBindGroup        *wgpu.BindGroup
	renderBindGroup0             *wgpu.BindGroup
	blitBindGroup                *wgpu.BindGroup
	outputTexture                *wgpu.Texture
	outputTextureView            *wgpu.TextureView
	outputSampler                *wgpu.Sampler
	renderParametersBuffer       *wgpu.Buffer
	cameraBuffer                 *wgpu.Buffer
	transformsBuffer             *wgpu.Buffer
	voxelInstancesBuffer         *wgpu.Buffer
	macroIndexPoolBuffer         *wgpu.Buffer
	brickPoolBuffer              *wgpu.Buffer
	voxelPoolBuffer              *wgpu.Buffer
	palettesBuffer               *wgpu.Buffer
	instanceAABBsBuffer          *wgpu.Buffer
	visibleInstanceIndicesBuffer *wgpu.Buffer
	visibleListParametersBuffer  *wgpu.Buffer
	renderParametersUniform      renderParametersUniform
	cameraUniform                voxelCameraUniform
	visibleListParametersUniform visibleListParametersUniform
	transformsUniforms           []transformsUniform //per vox-instance transforms
	voxelInstancesUniform        []voxelInstanceUniform
	instanceAABBsUniform         []aabbUniform
	visibleInstanceIndices       []uint32
	macroIndexPoolUniform        []uint32 // 3D grid of brick indices
	brickPoolUniform             []brickUniform
	voxelPoolUniform             []voxelUniform
	palettesUniform              [][256]mgl32.Vec4
	entityVoxInstanceIds         map[EntityId]int   //entity -> vox-model id
	instanceIdToEntity           map[int]EntityId   //vox-model id -> entity
	paletteIds                   map[AssetId]uint32 //palette-asset -> palette id
	isVoxelPoolUpdated           bool
}

func (mod VoxelModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	gpuState := createGpuState(windowState)
	rState := createVoxelRenderState(windowState, gpuState)

	app.UseSystem(
		System(createVoxelUniforms).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(updateModelUniforms).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(updateCameraUniform).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(createBuffers).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(createBindGroup).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(voxelRendering).
			InStage(Render).
			RunAlways(),
	)
	cmd.AddResources(
		windowState,
		gpuState,
		rState,
	)
}
