package gekko

import (
	"runtime"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
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
	//bufferUsages        []wgpu.BufferUsage
	group   uint32
	binding uint32
	data    []byte
}

type TransformComponent struct {
	Position mgl32.Vec3
	Rotation float32
	Scale    mgl32.Vec3
}

type CameraComponent struct {
	Position mgl32.Vec3
	LookAt   mgl32.Vec3
	Up       mgl32.Vec3
	Fov      float32
	Aspect   float32
	Near     float32
	Far      float32
}

type WindowState struct {
	// glfw
	windowGlfw   *glfw.Window
	WindowWidth  int
	WindowHeight int
	windowTitle  string
}

type gpuState struct {
	surface           *wgpu.Surface
	adapter           *wgpu.Adapter
	device            *wgpu.Device
	queue             *wgpu.Queue
	surfaceConfig     *wgpu.SurfaceConfiguration
	renderGroups      map[AssetId]renderGroup
	materialPipelines map[AssetId]*wgpu.RenderPipeline
}

func (mod ClientModule) Install(app *App, cmd *Commands) {
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		panic(err)
	}

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) // Important: tell GLFW we don't want OpenGL
	glfw.WindowHint(glfw.Resizable, glfw.True)

	win, err := glfw.CreateWindow(mod.WindowWidth, mod.WindowHeight, mod.WindowTitle, nil, nil)
	if err != nil {
		panic(err)
	}

	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	// wraps GLFW window into a wgpu surface.
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(win))
	// finds a suitable GPU (discrete GPU preferred)
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
		PowerPreference:   wgpu.PowerPreferenceHighPerformance,
	})
	if err != nil {
		panic(err)
	}
	defer adapter.Release()
	// allocates the device and command queue
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{
		Label:            "Main Device",
		RequiredFeatures: nil,
		RequiredLimits:   nil,
	})
	if err != nil {
		panic(err)
	}
	queue := device.GetQueue()

	caps := surface.GetCapabilities(adapter)
	// defines how the swapchain behaves (size, format, vsync)
	surfaceConfig := wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      caps.Formats[0],
		Width:       uint32(mod.WindowWidth),
		Height:      uint32(mod.WindowHeight),
		PresentMode: wgpu.PresentModeFifo, // vsync
		AlphaMode:   caps.AlphaModes[0],
	}

	surface.Configure(adapter, device, &surfaceConfig)

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
		&gpuState{
			surface:           surface,
			adapter:           adapter,
			device:            device,
			queue:             queue,
			surfaceConfig:     &surfaceConfig,
			renderGroups:      map[AssetId]renderGroup{},
			materialPipelines: map[AssetId]*wgpu.RenderPipeline{},
		},
		&WindowState{
			windowGlfw:   win,
			WindowWidth:  mod.WindowWidth,
			WindowHeight: mod.WindowHeight,
			windowTitle:  mod.WindowTitle,
		})
}

func loadMaterials(cmd *Commands, assets *AssetServer, s *gpuState) {
	MakeQuery2[Material, wgpuMaterial](cmd).Map(
		func(entityId EntityId, material *Material, gpuMaterial *wgpuMaterial) bool {
			materialAsset := assets.materials[material.assetId]
			pipeline, contains := s.materialPipelines[material.assetId]
			if !contains {
				pipeline = createRenderPipeline(materialAsset, s.device, s.surfaceConfig)
				s.materialPipelines[material.assetId] = pipeline
			}
			if nil == gpuMaterial {
				mt := wgpuMaterial{
					id:       material.assetId,
					version:  materialAsset.version,
					pipeline: pipeline,
				}
				cmd.AddComponents(entityId, mt)
			} else if materialAsset.version > gpuMaterial.version {
				// WGPU material is out of date - needs updating
				// TODO implement
			}
			return true
		}, wgpuMaterial{})
}

func createRenderPipeline(material MaterialAsset, device *wgpu.Device, config *wgpu.SurfaceConfiguration) *wgpu.RenderPipeline {
	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          material.shaderName,
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: material.shaderListing},
	})
	if err != nil {
		panic(err)
	}
	defer shader.Release()

	vertexBufferLayout := createVertexBufferLayout(material.vertexType)

	pipeline, err := device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers:    []wgpu.VertexBufferLayout{vertexBufferLayout},
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    config.Format,
					Blend:     nil,
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyTriangleList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeBack,
		},
		DepthStencil: nil,
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})
	if err != nil {
		panic(err)
	}
	return pipeline
}

func loadMeshes(cmd *Commands, assets *AssetServer, s *gpuState) {
	MakeQuery2[Mesh, wgpuMesh](cmd).Map(
		func(entityId EntityId, mesh *Mesh, gpuMesh *wgpuMesh) bool {
			meshAsset := assets.meshes[mesh.assetId]
			if nil == gpuMesh {
				vertexBuf, indexBuf := createVertexIndexBuffers(meshAsset, s.device)
				cmd.AddComponents(entityId, wgpuMesh{
					id:           mesh.assetId,
					version:      meshAsset.version,
					vertexBuffer: vertexBuf,
					indexBuffer:  indexBuf,
					vertexCount:  uint32(len(meshAsset.indices)),
				})
			} else if meshAsset.version > gpuMesh.version {
				// WGPU mesh is out of date - needs updating
				// TODO implement
			}
			return true
		},
		wgpuMesh{})
}

func createVertexIndexBuffers(mesh MeshAsset, device *wgpu.Device) (vertexBuf *wgpu.Buffer, indexBuf *wgpu.Buffer) {
	vertexBuf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: untypedSliceToWgpuBytes(mesh.vertices),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		panic(err)
	}
	indexBuf, err = device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(mesh.indices),
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		panic(err)
	}
	return vertexBuf, indexBuf
}

func loadTextures(cmd *Commands, assets *AssetServer, s *gpuState) {
	MakeQuery2[wgpuTextureSet, wgpuSamplerSet](cmd).Map(
		func(entityId EntityId, gpuTextureSet *wgpuTextureSet, gpuSamplerSet *wgpuSamplerSet) bool {
			descriptors := findTextureDescriptors(entityId, cmd, assets)
			if nil == gpuTextureSet {
				txs := map[AssetId]wgpuTexture{}
				for id, d := range descriptors {
					txView := loadTexture(d.textureAsset, s)
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
					sampler, err := s.device.CreateSampler(&wgpu.SamplerDescriptor{
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

func loadTexture(txAsset *TextureAsset, s *gpuState) *wgpu.TextureView {
	textureExtent := wgpu.Extent3D{
		Width:              txAsset.width,
		Height:             txAsset.height,
		DepthOrArrayLayers: 1,
	}
	texture, err := s.device.CreateTexture(&wgpu.TextureDescriptor{
		Size:          textureExtent,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormat(txAsset.format),
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	defer texture.Release()

	textureView, err := texture.CreateView(nil)
	if err != nil {
		panic(err)
	}

	err = s.queue.WriteTexture(
		texture.AsImageCopy(),
		wgpu.ToBytes(txAsset.texels),
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  txAsset.width * uint32(wgpuBytesPerPixel(wgpu.TextureFormat(txAsset.format))),
			RowsPerImage: wgpu.CopyStrideUndefined,
		},
		&textureExtent,
	)
	if err != nil {
		panic(err)
	}
	return textureView
}

func loadBuffers(cmd *Commands, s *gpuState) {
	MakeQuery1[wgpuBufferSet](cmd).Map(
		func(entityId EntityId, gpuBufferSet *wgpuBufferSet) bool {
			descriptors := findBufferDescriptors(entityId, cmd)
			if nil == gpuBufferSet {
				buffers := map[bufferId]*wgpu.Buffer{}
				for id, d := range descriptors {
					buffers[id] = loadBuffer(d, s)
				}
				cmd.AddComponents(entityId, wgpuBufferSet{buffers, descriptors})
			} else {
				//maybe new buffers were added? checking
				for id, d := range descriptors {
					_, contains := gpuBufferSet.buffers[id]
					if !contains {
						gpuBufferSet.buffers[id] = loadBuffer(d, s)
					}
				}
				//descriptor holds actual buffer's data, updating it each time
				gpuBufferSet.descriptors = descriptors
			}
			return true
		},
		wgpuBufferSet{})
}

func loadBuffer(descriptor bufferDescriptor, s *gpuState) *wgpu.Buffer {
	buffer, err := s.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Buffer",
		Contents: descriptor.data,
		//TODO use from descriptor
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		panic(err)
	}
	return buffer
}

func loadBindGroups(cmd *Commands, s *gpuState) {
	MakeQuery5[wgpuMaterial, wgpuTextureSet, wgpuSamplerSet, wgpuBufferSet, wgpuBindGroupSet](cmd).Map(
		func(entityId EntityId, material *wgpuMaterial, txSet *wgpuTextureSet, samplers *wgpuSamplerSet, buffSet *wgpuBufferSet, bindGrSet *wgpuBindGroupSet) bool {
			if nil == bindGrSet {
				groupedBindings := map[uint32][]wgpu.BindGroupEntry{}
				//buffer bindings
				for buffId, buffer := range buffSet.buffers {
					buffDesc := buffSet.descriptors[buffId]
					binding := wgpu.BindGroupEntry{
						Binding: buffDesc.binding,
						Buffer:  buffer,
						Size:    wgpu.WholeSize,
					}
					bindings, contains := groupedBindings[buffDesc.group]
					if !contains {
						groupedBindings[buffDesc.group] = []wgpu.BindGroupEntry{binding}
					} else {
						groupedBindings[buffDesc.group] = append(bindings, binding)
					}
				}

				//texture bindings
				for _, tx := range txSet.textures {
					binding := wgpu.BindGroupEntry{
						Binding:     tx.binding,
						TextureView: tx.textureView,
						Size:        wgpu.WholeSize,
					}

					if bindings, ok := groupedBindings[tx.group]; !ok {
						groupedBindings[tx.group] = []wgpu.BindGroupEntry{binding}
					} else {
						groupedBindings[tx.group] = append(bindings, binding)
					}
				}

				// sampler bindings
				for _, sampler := range samplers.samplers {
					binding := wgpu.BindGroupEntry{
						Binding: sampler.binding,
						Sampler: sampler.sampler,
						Size:    wgpu.WholeSize,
					}

					if bindings, ok := groupedBindings[sampler.group]; !ok {
						groupedBindings[sampler.group] = []wgpu.BindGroupEntry{binding}
					} else {
						groupedBindings[sampler.group] = append(bindings, binding)
					}
				}

				bindGroups := map[uint32]*wgpu.BindGroup{}
				for groupId, bindings := range groupedBindings {
					bindGroupLayout := material.pipeline.GetBindGroupLayout(groupId)
					defer bindGroupLayout.Release()

					bindGroup, err := s.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
						Layout:  bindGroupLayout,
						Entries: bindings,
					})
					if err != nil {
						panic(err)
					}
					bindGroups[groupId] = bindGroup
				}

				cmd.AddComponents(entityId, wgpuBindGroupSet{bindGroups})
			}
			return true
		}, wgpuBindGroupSet{})
}

// prepares render groups for each material
func buildRenderGroups(cmd *Commands, s *gpuState) {
	if len(s.renderGroups) == 0 {
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
			s.renderGroups[matId] = renderGroup{
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
func rendering(cmd *Commands, s *gpuState) {
	nextTexture, err := s.surface.GetCurrentTexture()
	if err != nil {
		panic(err)
	}
	view, err := nextTexture.CreateView(nil)
	if err != nil {
		panic(err)
	}
	defer view.Release()
	encoder, err := s.device.CreateCommandEncoder(nil)
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

	for _, rg := range s.renderGroups {
		//writing buffers
		for _, buffSet := range rg.bufferSets {
			for id, buff := range buffSet.buffers {
				buffData := buffSet.descriptors[id].data
				err = s.queue.WriteBuffer(buff, 0, buffData)
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

	s.queue.Submit(cmdBuffer)
	s.surface.Present()
}
