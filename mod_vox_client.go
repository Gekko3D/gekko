package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
	"os"
)

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
	WindowWidth  uint32
	WindowHeight uint32
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

type voxelModelUniform struct {
	Size            mgl32.Vec4
	PaletteId       uint32
	VoxelPoolOffset uint32
	Padding         [8]byte
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
	voxelModelsBuffer       *wgpu.Buffer
	voxelPoolBuffer         *wgpu.Buffer
	palettesBuffer          *wgpu.Buffer
	renderParametersUniform renderParametersUniform
	cameraUniform           voxelCameraUniform
	transformsUniforms      []transformsUniform //per vox-model transforms
	voxelModelsUniform      []voxelModelUniform
	voxelPoolUniform        []voxelUniform
	palettesUniform         [][256]mgl32.Vec4
	entityVoxModelIds       map[EntityId]int   //entity -> vox-model id
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

	shaderData, err := os.ReadFile("/Users/ddevidch/code/golang/gekko3d/gekko/shaders/raycasting.wgsl")
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
			WindowWidth:  uint32(windowState.WindowWidth),
			WindowHeight: uint32(windowState.WindowHeight),
		},
		cameraUniform:     cameraUniform,
		entityVoxModelIds: map[EntityId]int{},
		paletteIds:        map[AssetId]uint32{},
	}
}

func createVoxelUniforms(cmd *Commands, server *AssetServer, rState *voxelRenderState) {
	//creates model and palette uniforms for new voxel entities
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			if _, ok := rState.entityVoxModelIds[entityId]; !ok {
				//model is not loaded yet, loading
				voxAsset, paletteId, paletteAsset := findVoxelModelAsset(entityId, cmd, server)
				if nil != voxAsset {
					voxModelId := len(rState.transformsUniforms)
					modelMx := buildModelMatrix(transform)
					rState.transformsUniforms = append(rState.transformsUniforms, transformsUniform{
						ModelMx:    modelMx,
						InvModelMx: modelMx.Inv(),
					})
					//creates or gets palette id
					pId := createOrGetPaletteIndex(paletteId, paletteAsset, rState)
					//pushes model voxels into voxels pool
					voxelPoolOffset := pushVoxelsIntoPool(voxAsset, rState)
					rState.voxelModelsUniform = append(rState.voxelModelsUniform, voxelModelUniform{
						Size: mgl32.Vec4{
							float32(voxAsset.VoxModel.SizeX),
							float32(voxAsset.VoxModel.SizeY),
							float32(voxAsset.VoxModel.SizeZ),
							0.0,
						},
						PaletteId:       pId,
						VoxelPoolOffset: voxelPoolOffset,
					})

					rState.entityVoxModelIds[entityId] = voxModelId
					rState.paletteIds[paletteId] = pId

					rState.isVoxelPoolUpdated = true
				}
			}
			return true
		})
}

func pushVoxelsIntoPool(model *VoxelModelAsset, rState *voxelRenderState) uint32 {
	offset := uint32(len(rState.voxelPoolUniform))
	initModelSpace(&model.VoxModel, rState)
	for _, v := range model.VoxModel.Voxels {
		idx := offset + getFlatVoxelArrayIndex(&model.VoxModel, uint32(v.X), uint32(v.Y), uint32(v.Z))
		rState.voxelPoolUniform[idx] = voxelUniform{
			ColorIndex: uint32(v.ColorIndex),
			Alpha:      1.0, //solid by default
		}
	}
	return offset
}

// adding empty voxels for the new model into the pool
func initModelSpace(model *VoxModel, rState *voxelRenderState) {
	for range model.SizeX * model.SizeY * model.SizeZ {
		rState.voxelPoolUniform = append(rState.voxelPoolUniform, voxelUniform{})
	}
}

func getFlatVoxelArrayIndex(model *VoxModel, x, y, z uint32) uint32 {
	return z*model.SizeX*model.SizeY + y*model.SizeX + x
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
	if nil == rState.cameraBuffer && len(rState.voxelPoolUniform) > 0 {
		rState.cameraBuffer = createBuffer("camera", rState.cameraUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.renderParametersBuffer = createBuffer("renderParameters", rState.renderParametersUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.transformsBuffer = createBuffer("transforms", rState.transformsUniforms, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.voxelModelsBuffer = createBuffer("voxModels", rState.voxelModelsUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
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
					Buffer:  rState.voxelModelsBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 4,
					Buffer:  rState.voxelPoolBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 5,
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
	if len(rs.voxelPoolUniform) == 0 {
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
		err = gpuState.queue.WriteBuffer(rs.voxelModelsBuffer, 0, toBufferBytes(rs.voxelModelsUniform))
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
			if voxModelId, ok := rState.entityVoxModelIds[entityId]; ok {
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
