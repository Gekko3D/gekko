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

type voxelRenderState struct {
	screenQuadVertices []sqVertex
	screenQuadIndices  []uint16
	vertexBuffer       *wgpu.Buffer
	indexBuffer        *wgpu.Buffer
	vertexCount        uint32
	rayCastPipeline    *wgpu.RenderPipeline
	voxelBindGroup     *wgpu.BindGroup
	cameraBuffer       *wgpu.Buffer
	modelsBuffer       *wgpu.Buffer
	voxelPoolBuffer    *wgpu.Buffer
	palettesBuffer     *wgpu.Buffer
	cameraUniform      voxelCameraUniform
	modelsUniform      map[AssetId]voxelModelUniform
	palettesUniform    [][256]mgl32.Vec4
	voxelPoolUniform   []voxelUniform
	entityModels       map[EntityId]AssetId
	paletteIndexes     map[AssetId]uint32
	isVoxelPoolUpdated bool
}

type voxelCameraUniform struct {
	ViewProjMx    mgl32.Mat4
	InvViewProjMx mgl32.Mat4
	Position      mgl32.Vec3
	Width         float32
	Height        float32
	Padding       mgl32.Vec3
}

type voxelModelUniform struct {
	ModelMx         mgl32.Mat4
	InvModelMx      mgl32.Mat4
	Size            mgl32.Vec3 // Size of the volume in world units
	Padding1        float32
	Resolution      mgl32.Vec3 // Voxel grid resolution (e.g., 256x256x256)
	Padding2        float32
	PaletteIndex    float32
	Padding3        float32
	VoxelPoolOffset float32
	Padding4        float32
}

type voxelUniform struct {
	ColorIndex float32
	Alpha      float32
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
		Position:      mgl32.Vec3{0, 0, 0},
		Width:         float32(windowState.WindowWidth),
		Height:        float32(windowState.WindowHeight),
	}
	//bindGroup and buffers are defined according to raycasting.wgsl shader code

	return &voxelRenderState{
		screenQuadVertices: screenQuadVertices,
		screenQuadIndices:  screenQuadIndices,
		vertexBuffer:       vertexBuffer,
		indexBuffer:        indexBuffer,
		vertexCount:        uint32(len(screenQuadIndices)),
		rayCastPipeline:    rayCastPipeline,
		cameraUniform:      cameraUniform,
		modelsUniform:      map[AssetId]voxelModelUniform{},
		entityModels:       map[EntityId]AssetId{},
		paletteIndexes:     map[AssetId]uint32{},
	}
}

func createVoxelUniforms(cmd *Commands, server *AssetServer, rState *voxelRenderState) {
	//creates model and palette uniforms for new voxel entities
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			if _, ok := rState.entityModels[entityId]; !ok {
				voxModelId, model, paletteId, palette := findVoxelModelAsset(entityId, cmd, server)
				if nil != model {
					//pushes model voxels into voxels pool
					voxelPoolOffset := pushVoxelsIntoPool(model, rState)
					pIndex := getPaletteIndex(paletteId, palette, rState)
					modelMx := buildModelMatrix(transform)
					rState.modelsUniform[voxModelId] = voxelModelUniform{
						ModelMx:    modelMx,
						InvModelMx: modelMx.Inv(),
						//TODO user-defined size
						Size: [3]float32{
							float32(model.VoxModel.SizeX),
							float32(model.VoxModel.SizeY),
							float32(model.VoxModel.SizeZ),
						},
						Resolution: [3]float32{
							float32(model.VoxModel.SizeX),
							float32(model.VoxModel.SizeY),
							float32(model.VoxModel.SizeZ),
						},
						PaletteIndex:    float32(pIndex),
						VoxelPoolOffset: float32(voxelPoolOffset),
					}
					rState.entityModels[entityId] = voxModelId
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
			ColorIndex: float32(v.ColorIndex),
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

func getPaletteIndex(paletteId AssetId, asset *VoxelPaletteAsset, rState *voxelRenderState) uint32 {
	if idx, ok := rState.paletteIndexes[paletteId]; ok {
		return idx
	} else {
		palette := makeVoxelPalette(asset)
		rState.palettesUniform = append(rState.palettesUniform, palette)
		rState.paletteIndexes[paletteId] = uint32(len(rState.palettesUniform) - 1)
		return rState.paletteIndexes[paletteId]
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
		rState.modelsBuffer = createBuffer("models", getMapValues(rState.modelsUniform), gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
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
					Buffer:  rState.cameraBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 1,
					Buffer:  rState.modelsBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 2,
					Buffer:  rState.voxelPoolBuffer,
					Size:    wgpu.WholeSize,
				},
				{
					Binding: 3,
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

	err = gpuState.queue.WriteBuffer(rs.cameraBuffer, 0, toBufferBytes(rs.cameraUniform))
	err = gpuState.queue.WriteBuffer(rs.modelsBuffer, 0, toBufferBytes(getMapValues(rs.modelsUniform)))
	if rs.isVoxelPoolUpdated {
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
			if voxModelId, ok := rState.entityModels[entityId]; ok {
				modelUniform := rState.modelsUniform[voxModelId]
				modelMx := buildModelMatrix(transform)
				modelUniform.ModelMx = modelMx
				modelUniform.InvModelMx = modelMx.Inv()
				rState.modelsUniform[voxModelId] = modelUniform
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
			rState.cameraUniform.Position = camera.Position
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
