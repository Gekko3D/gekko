package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

type Float2 = mgl32.Vec2
type Float3 = mgl32.Vec3
type Float4 = mgl32.Vec4
type Float4x4 = mgl32.Mat4

type ClientModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
}

type wgpuMesh struct {
	id           AssetId
	version      uint
	vertexBuffer *wgpu.Buffer
	indexBuffer  *wgpu.Buffer
	vertexCount  uint32
}

type wgpuMaterial struct {
	id       AssetId
	version  uint
	pipeline *wgpu.RenderPipeline
}

type wgpuTextureSet struct {
	textures map[AssetId]wgpuTexture
}

type wgpuTexture struct {
	id          AssetId
	version     uint
	group       uint32
	binding     uint32
	textureView *wgpu.TextureView
}

type wgpuSampler struct {
	version uint
	id      AssetId
	group   uint32
	binding uint32
	sampler *wgpu.Sampler
}

type wgpuSamplerSet struct {
	samplers map[AssetId]wgpuSampler
}

type wgpuBindGroupSet struct {
	bindGroups map[uint32]*wgpu.BindGroup
}

type wgpuBufferSet struct {
	buffers     map[bufferId]*wgpu.Buffer
	descriptors map[bufferId]bufferDescriptor
}

// TODO we use this as a map key, is it safe?
type bufferId struct {
	group   uint32
	binding uint32
}

// unites material and everything to render it: meshes, buffers, bindGroups
type renderGroup struct {
	material         *wgpuMaterial
	meshes           map[AssetId]*wgpuMesh
	bufferSets       []*wgpuBufferSet
	meshesBindGroups map[AssetId][]*wgpuBindGroupSet
	//TODO use this to update render group
	upToDate bool
}

type textureDescriptor struct {
	version      uint
	group        uint32
	binding      uint32
	textureAsset *TextureAsset
}
type bufferDescriptor struct {
	group   uint32
	binding uint32
	usage   wgpu.BufferUsage
	data    []byte
}

type TransformComponent struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3
}

type LocalTransformComponent struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3
}

func (t LocalTransformComponent) ToMat4() mgl32.Mat4 {
	translate := mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z())
	rotate := t.Rotation.Mat4()
	scale := mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z())
	return translate.Mul4(rotate).Mul4(scale)
}

type Parent struct {
	Entity EntityId
}

type CameraComponent struct {
	Position mgl32.Vec3
	LookAt   mgl32.Vec3
	Up       mgl32.Vec3
	Yaw      float32
	Pitch    float32
	Fov      float32
	Aspect   float32
	Near     float32
	Far      float32
}

type renderState struct {
	renderGroups      map[AssetId]renderGroup
	materialPipelines map[AssetId]*wgpu.RenderPipeline
}

func (mod ClientModule) Install(app *App, cmd *Commands) {
	windowState := createWindowState(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle)
	gpuState := createGpuState(windowState)
	rState := createRenderState()

	app.UseSystem(
		System(loadMaterials).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(loadMeshes).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(loadTextures).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(loadBuffers).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(loadBindGroups).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(buildRenderGroups).
			InStage(PreRender).
			RunAlways(),
	)
	app.UseSystem(
		System(rendering).
			InStage(Render).
			RunAlways(),
	)

	cmd.AddResources(
		windowState,
		gpuState,
		rState)
}

func createRenderState() *renderState {
	return &renderState{
		renderGroups:      map[AssetId]renderGroup{},
		materialPipelines: map[AssetId]*wgpu.RenderPipeline{},
	}
}

func loadMaterials(cmd *Commands, assets *AssetServer, rState *renderState, gpuState *GpuState) {
	MakeQuery2[Material, wgpuMaterial](cmd).Map(
		func(entityId EntityId, material *Material, gpuMaterial *wgpuMaterial) bool {
			asset := assets.materials[material.assetId]
			pipeline, contains := rState.materialPipelines[material.assetId]
			if !contains {
				pipeline = createRenderPipeline(asset.shaderName, asset.shaderListing, asset.vertexType, gpuState)
				rState.materialPipelines[material.assetId] = pipeline
			}
			if nil == gpuMaterial {
				mt := wgpuMaterial{
					id:       material.assetId,
					version:  asset.version,
					pipeline: pipeline,
				}
				cmd.AddComponents(entityId, mt)
			} else if asset.version > gpuMaterial.version {
				// WGPU material is out of date - needs updating
				// TODO implement
			}
			return true
		}, wgpuMaterial{})
}

func loadMeshes(cmd *Commands, assets *AssetServer, gpuState *GpuState) {
	MakeQuery2[Mesh, wgpuMesh](cmd).Map(
		func(entityId EntityId, mesh *Mesh, gpuMesh *wgpuMesh) bool {
			asset := assets.meshes[mesh.assetId]
			if nil == gpuMesh {
				vertexBuf, indexBuf := createVertexIndexBuffers(asset.vertices, asset.indices, gpuState.device)
				cmd.AddComponents(entityId, wgpuMesh{
					id:           mesh.assetId,
					version:      asset.version,
					vertexBuffer: vertexBuf,
					indexBuffer:  indexBuf,
					vertexCount:  uint32(len(asset.indices)),
				})
			} else if asset.version > gpuMesh.version {
				// WGPU mesh is out of date - needs updating
				// TODO implement
			}
			return true
		},
		wgpuMesh{})
}

func loadTextures(cmd *Commands, assets *AssetServer, gpuState *GpuState) {
	MakeQuery2[wgpuTextureSet, wgpuSamplerSet](cmd).Map(
		func(entityId EntityId, gpuTextureSet *wgpuTextureSet, gpuSamplerSet *wgpuSamplerSet) bool {
			descriptors := findTextureDescriptors(entityId, cmd, assets)
			if nil == gpuTextureSet {
				txs := map[AssetId]wgpuTexture{}
				for id, d := range descriptors {
					txView := createTextureFromAsset(d.textureAsset, gpuState)
					txs[id] = wgpuTexture{
						id:          id,
						version:     d.version,
						group:       d.group,
						binding:     d.binding,
						textureView: txView,
					}
				}
				cmd.AddComponents(entityId, wgpuTextureSet{
					textures: txs,
				})
			} else {
				// compare each textureComponent with the same texture inside gpuTextureSet
				// update texture inside the set if it is outdated
				// TODO implement
			}

			if nil == gpuSamplerSet {
				samplers := map[AssetId]wgpuSampler{}
				for _, samplerDesc := range tryFindSamplers(cmd, entityId) {
					wrap := wgpuWrapMode(samplerDesc.wrapMode)
					filter := wgpuFilterMode(samplerDesc.filter)
					sampler, err := gpuState.device.CreateSampler(&wgpu.SamplerDescriptor{
						Label:         "",
						AddressModeU:  wrap,
						AddressModeV:  wrap,
						AddressModeW:  wrap,
						MagFilter:     filter,
						MinFilter:     filter,
						MipmapFilter:  wgpu.MipmapFilterModeLinear,
						LodMinClamp:   0.,
						LodMaxClamp:   32.,
						Compare:       wgpu.CompareFunctionUndefined,
						MaxAnisotropy: 1,
					})
					if err != nil {
						panic(err)
					}

					samplers[samplerDesc.assetId] = wgpuSampler{
						version: 0,
						id:      samplerDesc.assetId,
						group:   samplerDesc.group,
						binding: samplerDesc.binding,
						sampler: sampler,
					}
				}

				cmd.AddComponents(entityId, wgpuSamplerSet{
					samplers: samplers,
				})
			} else {
				// TODO implement sampler update
				// (not possible currently due to samplers being hardocded in the tags - will change with shader editor tool)
			}

			return true
		},
		wgpuTextureSet{},
		wgpuSamplerSet{},
	)
}

func loadBuffers(cmd *Commands, gpuState *GpuState) {
	MakeQuery1[wgpuBufferSet](cmd).Map(
		func(entityId EntityId, gpuBufferSet *wgpuBufferSet) bool {
			descriptors := findBufferDescriptors(entityId, cmd)
			if nil == gpuBufferSet {
				buffers := map[bufferId]*wgpu.Buffer{}
				for id, d := range descriptors {
					buffers[id] = createBufferFromDescriptor(d, gpuState)
				}
				cmd.AddComponents(entityId, wgpuBufferSet{buffers, descriptors})
			} else {
				//maybe new buffers were added? checking
				for id, d := range descriptors {
					_, contains := gpuBufferSet.buffers[id]
					if !contains {
						gpuBufferSet.buffers[id] = createBufferFromDescriptor(d, gpuState)
					}
				}
				//descriptor holds actual buffer's data, updating it each time
				gpuBufferSet.descriptors = descriptors
			}
			return true
		},
		wgpuBufferSet{})
}

func loadBindGroups(cmd *Commands, gpuState *GpuState) {
	MakeQuery5[wgpuMaterial, wgpuTextureSet, wgpuSamplerSet, wgpuBufferSet, wgpuBindGroupSet](cmd).Map(
		func(entityId EntityId, material *wgpuMaterial, txSet *wgpuTextureSet, samplers *wgpuSamplerSet, buffSet *wgpuBufferSet, bindGrSet *wgpuBindGroupSet) bool {
			if nil == bindGrSet {
				groupedBindings := map[uint32][]wgpu.BindGroupEntry{}
				//buffer bindings per group id
				groupedBindings = createBufferGroupedBindings(groupedBindings, buffSet)
				//texture bindings per group id
				groupedBindings = createTextureGroupedBindings(groupedBindings, txSet)
				// sampler bindings per group id
				groupedBindings = createSamplerGroupedBindings(groupedBindings, samplers)

				bindGroups := createBindGroups(groupedBindings, material.pipeline, gpuState.device)
				cmd.AddComponents(entityId, wgpuBindGroupSet{bindGroups})
			}
			return true
		}, wgpuBindGroupSet{})
}

// prepares render groups for each material
func buildRenderGroups(cmd *Commands, rs *renderState) {
	if len(rs.renderGroups) == 0 {
		materials := map[AssetId]*wgpuMaterial{}
		materialsMeshes := map[AssetId]map[AssetId]*wgpuMesh{}
		materialsBuffSets := map[AssetId][]*wgpuBufferSet{}
		materialsMeshesBindGroups := map[AssetId]map[AssetId][]*wgpuBindGroupSet{}
		MakeQuery4[wgpuMaterial, wgpuMesh, wgpuBufferSet, wgpuBindGroupSet](cmd).Map(
			func(entityId EntityId, material *wgpuMaterial, mesh *wgpuMesh, buffSet *wgpuBufferSet, groupSet *wgpuBindGroupSet) bool {
				materials[material.id] = material
				matMeshes, contains := materialsMeshes[material.id]
				if contains {
					matMeshes[mesh.id] = mesh
				} else {
					materialsMeshes[material.id] = map[AssetId]*wgpuMesh{mesh.id: mesh}
				}

				buffSets, contains := materialsBuffSets[material.id]
				if contains {
					materialsBuffSets[material.id] = append(buffSets, buffSet)
				} else {
					materialsBuffSets[material.id] = []*wgpuBufferSet{buffSet}
				}

				meshesBindGroups, contains := materialsMeshesBindGroups[material.id]
				if contains {
					meshBindGroups, contains := meshesBindGroups[mesh.id]
					if contains {
						materialsMeshesBindGroups[material.id][mesh.id] = append(meshBindGroups, groupSet)
					} else {
						materialsMeshesBindGroups[material.id][mesh.id] = []*wgpuBindGroupSet{groupSet}
					}
				} else {
					materialsMeshesBindGroups[material.id] = map[AssetId][]*wgpuBindGroupSet{
						mesh.id: {groupSet},
					}
				}
				return true
			})

		for matId, material := range materials {
			rs.renderGroups[matId] = renderGroup{
				material:         material,
				meshes:           materialsMeshes[matId],
				bufferSets:       materialsBuffSets[matId],
				meshesBindGroups: materialsMeshesBindGroups[matId],
				upToDate:         true,
			}
		}
	} else {
		//TODO update render groups
	}
}

// renders single frame
func rendering(cmd *Commands, rs *renderState, gpuState *GpuState) {
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

	for _, rg := range rs.renderGroups {
		//writing buffers
		for _, buffSet := range rg.bufferSets {
			for id, buff := range buffSet.buffers {
				buffData := buffSet.descriptors[id].data
				err = gpuState.queue.WriteBuffer(buff, 0, buffData)
				if err != nil {
					panic(err)
				}
			}
		}

		renderPass.SetPipeline(rg.material.pipeline)
		for _, mesh := range rg.meshes {
			renderPass.SetIndexBuffer(mesh.indexBuffer, wgpu.IndexFormatUint16, 0, wgpu.WholeSize)
			renderPass.SetVertexBuffer(0, mesh.vertexBuffer, 0, wgpu.WholeSize)
			for _, bindGroupSet := range rg.meshesBindGroups[mesh.id] {
				for groupId, bindGroup := range bindGroupSet.bindGroups {
					renderPass.SetBindGroup(groupId, bindGroup, nil)
					renderPass.DrawIndexed(mesh.vertexCount, 1, 0, 0, 0)
				}
			}
		}
	}
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
