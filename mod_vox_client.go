package gekko

import (
	"fmt"
	"os"

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

type voxelRenderState struct {
	screenQuadVertices      []sqVertex
	screenQuadIndices       []uint16
	vertexBuffer            *wgpu.Buffer
	indexBuffer             *wgpu.Buffer
	vertexCount             uint32
	rayCastPipeline         *wgpu.RenderPipeline
	voxelBindGroup          *wgpu.BindGroup
	renderParametersBuffer  *wgpu.Buffer
	cameraBuffer            *wgpu.Buffer
	transformsBuffer        *wgpu.Buffer
	voxelInstancesBuffer    *wgpu.Buffer
	macroIndexPoolBuffer    *wgpu.Buffer
	brickPoolBuffer         *wgpu.Buffer
	voxelPoolBuffer         *wgpu.Buffer
	palettesBuffer          *wgpu.Buffer
	renderParametersUniform renderParametersUniform
	cameraUniform           voxelCameraUniform
	transformsUniforms      []transformsUniform //per vox-instance transforms
	voxelInstancesUniform   []voxelInstanceUniform
	macroIndexPoolUniform   []uint32 // 3D grid of brick indices
	brickPoolUniform        []brickUniform
	voxelPoolUniform        []voxelUniform
	palettesUniform         [][256]mgl32.Vec4
	entityVoxInstanceIds    map[EntityId]int   //entity -> vox-model id
	paletteIds              map[AssetId]uint32 //palette-asset -> palette id
	isVoxelPoolUpdated      bool
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

func createVoxelRenderState(windowState *WindowState, gpuState *GpuState) *voxelRenderState {
	aspect := float32(windowState.WindowHeight) / float32(windowState.WindowWidth)
	screenQuadVertices := []sqVertex{
		{pos: [4]float32{-1., 1., 0., 1.}, texCoord: [2]float32{0., 0.}},
		{pos: [4]float32{1., 1., 0., 1.}, texCoord: [2]float32{12., 0.}},
		{pos: [4]float32{-1., -1., 0., 1.}, texCoord: [2]float32{0., 12. * aspect}},
		{pos: [4]float32{1., -1., 0., 1.}, texCoord: [2]float32{12., 12. * aspect}},
	}
	screenQuadIndices := []uint16{0, 2, 1, 1, 2, 3}

	shaderData, err := os.ReadFile("D:\\IT\\Golang\\gekko3d\\gekko\\shaders\\raycasting.wgsl")
	if err != nil {
		panic(err)
	}
	rayCastPipeline := createRenderPipeline("voxel", string(shaderData), sqVertex{}, gpuState)

	vertexBuffer, indexBuffer := createVertexIndexBuffers(MakeAnySlice(screenQuadVertices), screenQuadIndices, gpuState.device)

	cameraUniform := voxelCameraUniform{
		ViewProjMx:    mgl32.Ident4(),
		InvViewProjMx: mgl32.Ident4().Inv(),
		Position:      mgl32.Vec4{0.0, 0.0, 0.0, 0.0},
	}
	//bindGroup and buffers are defined according to raycasting.wgsl shader code

	return &voxelRenderState{
		screenQuadVertices: screenQuadVertices,
		screenQuadIndices:  screenQuadIndices,
		vertexBuffer:       vertexBuffer,
		indexBuffer:        indexBuffer,
		vertexCount:        uint32(len(screenQuadIndices)),
		rayCastPipeline:    rayCastPipeline,
		renderParametersUniform: renderParametersUniform{
			WindowWidth:     uint32(windowState.WindowWidth),
			WindowHeight:    uint32(windowState.WindowHeight),
			EmptyBlockValue: EmptyBrickValue,
		},
		cameraUniform:        cameraUniform,
		entityVoxInstanceIds: map[EntityId]int{},
		paletteIds:           map[AssetId]uint32{},
	}
}

func createVoxelUniforms(cmd *Commands, server *AssetServer, rState *voxelRenderState) {
	//creates model and palette uniforms for new voxel entities
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			if _, ok := rState.entityVoxInstanceIds[entityId]; !ok {
				//vox instance is not loaded yet, loading
				voxAsset, paletteAssetId, paletteAsset := findVoxelModelAsset(entityId, cmd, server)
				if nil != voxAsset {
					//new vox instance goes to the last index
					voxInstanceId := len(rState.transformsUniforms)
					modelMx := buildModelMatrix(transform)
					rState.transformsUniforms = append(rState.transformsUniforms, transformsUniform{
						ModelMx:    modelMx,
						InvModelMx: modelMx.Inv(),
					})
					//creates or gets palette id
					paletteId := createOrGetPaletteIndex(paletteAssetId, paletteAsset, rState)
					//pushes model voxels into voxels pool
					macroGrid := createMacroGrid(voxAsset, rState)

					rState.voxelInstancesUniform = append(rState.voxelInstancesUniform, voxelInstanceUniform{
						Size: [3]uint32{
							voxAsset.VoxModel.SizeX,
							voxAsset.VoxModel.SizeY,
							voxAsset.VoxModel.SizeZ,
						},
						PaletteId: paletteId,
						MacroGrid: macroGrid,
					})

					rState.entityVoxInstanceIds[entityId] = voxInstanceId
					rState.paletteIds[paletteAssetId] = paletteId

					rState.isVoxelPoolUpdated = true
				}
			}
			return true
		})
}

// macroGrid - grid of voxel bricks.
// each non-empty macroGrid element points to bricks array element
// each brick points to voxel pool offset
func createMacroGrid(voxModelAsset *VoxelModelAsset, rState *voxelRenderState) macroGridUniform {
	// Calculate macro grid dimensions
	brickSizeX := voxModelAsset.BrickSize[0]
	brickSizeY := voxModelAsset.BrickSize[1]
	brickSizeZ := voxModelAsset.BrickSize[2]
	// ceil-div without float to avoid integer truncation bugs
	macroSizeX := (voxModelAsset.VoxModel.SizeX + brickSizeX - 1) / brickSizeX
	macroSizeY := (voxModelAsset.VoxModel.SizeY + brickSizeY - 1) / brickSizeY
	macroSizeZ := (voxModelAsset.VoxModel.SizeZ + brickSizeZ - 1) / brickSizeZ
	fmt.Printf("MacroGrid Size: %dx%dx%d\n", macroSizeX, macroSizeY, macroSizeZ)
	// Group voxels by their position in brick, group bricks by their position in macroGrid
	// [brick pos] -> [voxle pos] -> voxel
	marcoGridBricks := map[[3]uint32]map[[3]uint32]Voxel{}
	for _, voxel := range voxModelAsset.VoxModel.Voxels {
		// Brick position in mackroGrid
		brickX := uint32(voxel.X) / brickSizeX
		brickY := uint32(voxel.Y) / brickSizeY
		brickZ := uint32(voxel.Z) / brickSizeZ
		brickPos := [3]uint32{brickX, brickY, brickZ}
		if _, ok := marcoGridBricks[brickPos]; !ok {
			marcoGridBricks[brickPos] = map[[3]uint32]Voxel{}
		}
		// Voxel position in brick
		voxelX := uint32(voxel.X) % brickSizeX
		voxelY := uint32(voxel.Y) % brickSizeY
		voxelZ := uint32(voxel.Z) % brickSizeZ
		voxelPos := [3]uint32{voxelX, voxelY, voxelZ}
		marcoGridBricks[brickPos][voxelPos] = voxel
	}
	// Current macroGrid offset
	currentMacroIndexOffset := uint32(len(rState.macroIndexPoolUniform))
	// Add new macroGrid to the pool, init it with empty bricks
	for mcId := uint32(0); mcId < macroSizeX*macroSizeY*macroSizeZ; mcId++ {
		rState.macroIndexPoolUniform = append(rState.macroIndexPoolUniform, EmptyBrickValue)
	}
	// Process each potential brick in the macroGrid
	for x := uint32(0); x < macroSizeX; x++ {
		for y := uint32(0); y < macroSizeY; y++ {
			for z := uint32(0); z < macroSizeZ; z++ {
				brickPos := [3]uint32{x, y, z}
				// For each non-empty brick create brick uniforms
				if brickVoxels, ok := marcoGridBricks[brickPos]; ok {
					// Calculate macroGrid index for this cell
					macroGridId := currentMacroIndexOffset + getFlatArrayIndex(x, y, z, macroSizeX, macroSizeY)

					// Detect solid-color brick: fully filled and all voxels have same color
					totalVox := brickSizeX * brickSizeY * brickSizeZ
					isSolid := false
					var solidColor uint32 = 0
					if uint32(len(brickVoxels)) == totalVox {
						first := true
						for _, v := range brickVoxels {
							if first {
								solidColor = uint32(v.ColorIndex)
								first = false
							} else if uint32(v.ColorIndex) != solidColor {
								solidColor = 0
								isSolid = false
								break
							}
							isSolid = true
						}
					}

					if isSolid {
						// Encode direct color reference: high bit set + palette color index
						rState.macroIndexPoolUniform[macroGridId] = DirectColorFlag | (solidColor & 0x7FFFFFFF)
						continue
					}

					// Non-solid brick: allocate brick and voxels
					currentBrickVoxPoolOffset := uint32(len(rState.voxelPoolUniform))
					brick := brickUniform{
						Position:   [3]uint32{x, y, z},
						DataOffset: currentBrickVoxPoolOffset,
					}
					brickId := uint32(len(rState.brickPoolUniform))
					rState.brickPoolUniform = append(rState.brickPoolUniform, brick)
					// Init brick voxels in voxel pool
					for voxPoolId := uint32(0); voxPoolId < totalVox; voxPoolId++ {
						rState.voxelPoolUniform = append(rState.voxelPoolUniform, voxelUniform{
							ColorIndex: 0, // Empty voxel
							Alpha:      0.0,
						})
					}
					// Set non-empty voxels in voxel pool
					for vx := uint32(0); vx < brickSizeX; vx++ {
						for vy := uint32(0); vy < brickSizeY; vy++ {
							for vz := uint32(0); vz < brickSizeZ; vz++ {
								voxelPos := [3]uint32{vx, vy, vz}
								if voxel, ok := brickVoxels[voxelPos]; ok {
									voxelId := currentBrickVoxPoolOffset + getFlatArrayIndex(vx, vy, vz, brickSizeX, brickSizeY)
									//TODO pass alpha from voxel
									rState.voxelPoolUniform[voxelId] = voxelUniform{
										ColorIndex: uint32(voxel.ColorIndex),
										Alpha:      1.0,
									}
								}
							}
						}
					}
					// Put new brick pointer to macroGrid
					rState.macroIndexPoolUniform[macroGridId] = brickId
				}
			}
		}
	}

	return macroGridUniform{
		Size:       [3]uint32{macroSizeX, macroSizeY, macroSizeZ},
		BrickSize:  voxModelAsset.BrickSize,
		DataOffset: currentMacroIndexOffset, // Offset in macroIndex where this model's data starts
	}
}

func getFlatArrayIndex(x, y, z, sizeX, sizeY uint32) uint32 {
	return z*sizeX*sizeY + y*sizeX + x
}

func createOrGetPaletteIndex(assetId AssetId, asset *VoxelPaletteAsset, rState *voxelRenderState) uint32 {
	if idx, ok := rState.paletteIds[assetId]; ok {
		return idx
	} else {
		palette := makeVoxelPalette(asset)
		rState.palettesUniform = append(rState.palettesUniform, palette)
		rState.paletteIds[assetId] = uint32(len(rState.palettesUniform) - 1)
		return rState.paletteIds[assetId]
	}
}

func makeVoxelPalette(asset *VoxelPaletteAsset) [256]mgl32.Vec4 {
	palette := [256]mgl32.Vec4{{}}
	for i, v := range asset.VoxPalette {
		palette[i] = mgl32.Vec4{float32(v[0]) / 255.0, float32(v[1]) / 255.0, float32(v[2]) / 255.0, float32(v[3]) / 255.0}
	}
	return palette
}

// TODO run only once?
func createBuffers(gpuState *GpuState, rState *voxelRenderState) {
	//we need to create buffers only once
	if nil == rState.cameraBuffer && len(rState.voxelInstancesUniform) > 0 {
		// Ensure voxelPool has at least one element so buffer creation/bindings are valid
		if len(rState.voxelPoolUniform) == 0 {
			rState.voxelPoolUniform = append(rState.voxelPoolUniform, voxelUniform{ColorIndex: 0, Alpha: 0.0})
		}
		rState.renderParametersBuffer = createBuffer("renderParameters", rState.renderParametersUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.cameraBuffer = createBuffer("camera", rState.cameraUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.transformsBuffer = createBuffer("transforms", rState.transformsUniforms, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.voxelInstancesBuffer = createBuffer("voxelInstances", rState.voxelInstancesUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.macroIndexPoolBuffer = createBuffer("macroIndexPool", rState.macroIndexPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.brickPoolBuffer = createBuffer("brickPool", rState.brickPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.voxelPoolBuffer = createBuffer("voxelPool", rState.voxelPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.palettesBuffer = createBuffer("palettes", rState.palettesUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
	}
}

// TODO run only once?
func createBindGroup(gpuState *GpuState, rState *voxelRenderState) {
	//we need to create bind group only once
	if nil == rState.voxelBindGroup && nil != rState.voxelPoolBuffer {
		bindGroupLayout := rState.rayCastPipeline.GetBindGroupLayout(0)
		defer bindGroupLayout.Release()
		bindGroup, err := gpuState.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: bindGroupLayout,
			Entries: []wgpu.BindGroupEntry{
				{
					Binding: 0,
					Buffer:  rState.renderParametersBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 1,
					Buffer:  rState.cameraBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 2,
					Buffer:  rState.transformsBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 3,
					Buffer:  rState.voxelInstancesBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 4,
					Buffer:  rState.macroIndexPoolBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 5,
					Buffer:  rState.brickPoolBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 6,
					Buffer:  rState.voxelPoolBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 7,
					Buffer:  rState.palettesBuffer,
					Size:    wgpu.WholeSize,
				},
			},
		})
		if err != nil {
			panic(err)
		}
		rState.voxelBindGroup = bindGroup
	}
}

// renders single frame
func voxelRendering(rs *voxelRenderState, gpuState *GpuState) {
	if len(rs.voxelInstancesUniform) == 0 {
		//nothing to render
		return
	}
	nextTexture, err := gpuState.surface.GetCurrentTexture()
	if err != nil {
		panic(err)
	}
	view, err := nextTexture.CreateView(nil)
	if err != nil {
		panic(err)
	}
	defer view.Release()
	encoder, err := gpuState.device.CreateCommandEncoder(nil)
	if err != nil {
		panic(err)
	}
	defer encoder.Release()

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       view,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0.1, G: 0.2, B: 0.3, A: 1.0},
			},
		},
	})
	defer renderPass.Release()

	err = gpuState.queue.WriteBuffer(rs.renderParametersBuffer, 0, toBufferBytes(rs.renderParametersUniform))
	err = gpuState.queue.WriteBuffer(rs.cameraBuffer, 0, toBufferBytes(rs.cameraUniform))
	err = gpuState.queue.WriteBuffer(rs.transformsBuffer, 0, toBufferBytes(rs.transformsUniforms))

	if rs.isVoxelPoolUpdated {
		err = gpuState.queue.WriteBuffer(rs.voxelInstancesBuffer, 0, toBufferBytes(rs.voxelInstancesUniform))
		err = gpuState.queue.WriteBuffer(rs.macroIndexPoolBuffer, 0, toBufferBytes(rs.macroIndexPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.brickPoolBuffer, 0, toBufferBytes(rs.brickPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.voxelPoolBuffer, 0, toBufferBytes(rs.voxelPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.palettesBuffer, 0, toBufferBytes(rs.palettesUniform))
		rs.isVoxelPoolUpdated = false
	}
	if err != nil {
		panic(err)
	}

	renderPass.SetPipeline(rs.rayCastPipeline)
	renderPass.SetIndexBuffer(rs.indexBuffer, wgpu.IndexFormatUint16, 0, wgpu.WholeSize)
	renderPass.SetVertexBuffer(0, rs.vertexBuffer, 0, wgpu.WholeSize)
	renderPass.SetBindGroup(0, rs.voxelBindGroup, nil)
	renderPass.DrawIndexed(rs.vertexCount, 1, 0, 0, 0)

	err = renderPass.End()
	if err != nil {
		panic(err)
	}

	cmdBuffer, err := encoder.Finish(nil)
	if err != nil {
		panic(err)
	}
	defer cmdBuffer.Release()

	gpuState.queue.Submit(cmdBuffer)
	gpuState.surface.Present()
}

func updateModelUniforms(cmd *Commands, rState *voxelRenderState) {
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			if voxModelId, ok := rState.entityVoxInstanceIds[entityId]; ok {
				uniform := rState.transformsUniforms[voxModelId]
				modelMx := buildModelMatrix(transform)
				uniform.ModelMx = modelMx
				uniform.InvModelMx = modelMx.Inv()
				rState.transformsUniforms[voxModelId] = uniform
			}
			return true
		})
}

func buildModelMatrix(t *TransformComponent) mgl32.Mat4 {
	return mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z()).
		Mul4(mgl32.HomogRotate3DZ(t.Rotation)).
		Mul4(mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z()))
}

func updateCameraUniform(cmd *Commands, rState *voxelRenderState) {
	MakeQuery1[CameraComponent](cmd).Map(
		func(entityId EntityId, camera *CameraComponent) bool {
			camMx := buildCameraMatrix(camera)
			rState.cameraUniform.ViewProjMx = camMx
			rState.cameraUniform.InvViewProjMx = camMx.Inv()
			rState.cameraUniform.Position = mgl32.Vec4{camera.Position[0], camera.Position[1], camera.Position[2], 0.0}
			//TODO add support of multiple cameras
			return false
		})
}

func buildCameraMatrix(c *CameraComponent) mgl32.Mat4 {
	view := mgl32.LookAtV(
		c.Position,
		c.LookAt,
		c.Up,
	)
	projection := mgl32.Perspective(
		c.Fov,
		c.Aspect,
		c.Near,
		c.Far,
	)
	return projection.Mul4(view)
}
