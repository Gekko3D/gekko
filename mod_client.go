package gekko

import (
	"reflect"
	"runtime"
	"strconv"
	"unsafe"

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
	VertexBuffer *wgpu.Buffer
	IndexBuffer  *wgpu.Buffer
	VertexCount  uint32
}

type wgpuMaterial struct {
	id            AssetId
	version       uint
	Pipeline      *wgpu.RenderPipeline
	BindGroup     *wgpu.BindGroup
	UniformBuffer *wgpu.Buffer // MVP matrix
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
	surface       *wgpu.Surface
	adapter       *wgpu.Adapter
	device        *wgpu.Device
	queue         *wgpu.Queue
	surfaceConfig *wgpu.SurfaceConfiguration
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
		System(loadGpuResources).
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
			surface:       surface,
			adapter:       adapter,
			device:        device,
			queue:         queue,
			surfaceConfig: &surfaceConfig,
		},
		&WindowState{
			windowGlfw:   win,
			WindowWidth:  mod.WindowWidth,
			WindowHeight: mod.WindowHeight,
			windowTitle:  mod.WindowTitle,
		})
}

func loadGpuResources(cmd *Commands, assets *AssetServer, s *gpuState) {
	MakeQuery4[Mesh, Material, wgpuMesh, wgpuMaterial](cmd).Map4(
		func(entityId EntityId, mesh *Mesh, material *Material,
			gpuMesh *wgpuMesh, gpuMaterial *wgpuMaterial) bool {

			materialAsset := assets.materials[material.assetId]
			var pipeline *wgpu.RenderPipeline
			if nil == gpuMaterial {
				// WGPU material doesn't exist - needs creating
				pipeline = createRenderPipeline(materialAsset, s.device, s.surfaceConfig)
				mvpMatrix := mgl32.Mat4{}
				uniformBuf, err := s.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
					Label:    "Uniform Buffer",
					Contents: wgpu.ToBytes(mvpMatrix[:]),
					Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
				})
				if err != nil {
					panic(err)
				}

				textureView := loadTestTexture(s)
				defer textureView.Release()

				bindGroupLayout := pipeline.GetBindGroupLayout(0)
				defer bindGroupLayout.Release()

				bindGroup, err := s.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
					Layout: bindGroupLayout,
					Entries: []wgpu.BindGroupEntry{
						{
							Binding: 0,
							Buffer:  uniformBuf,
							Size:    wgpu.WholeSize,
						},
						{
							Binding:     1,
							TextureView: textureView,
							Size:        wgpu.WholeSize,
						},
					},
				})
				if err != nil {
					panic(err)
				}

				cmd.AddComponents(entityId, wgpuMaterial{
					id:            material.assetId,
					version:       0,
					Pipeline:      pipeline,
					BindGroup:     bindGroup,
					UniformBuffer: uniformBuf,
				})
			} else if materialAsset.version > gpuMaterial.version {
				// WGPU material is out of date - needs updating
				// TODO implement
				pipeline = gpuMaterial.Pipeline
			}

			meshAsset := assets.meshes[mesh.assetId]
			if nil == gpuMesh {
				// WGPU mesh doesn't exist - needs creating
				vertexBuf, indexBuf := createBuffers(meshAsset, s.device, s.surfaceConfig)
				cmd.AddComponents(entityId, wgpuMesh{
					id:           mesh.assetId,
					version:      0,
					VertexBuffer: vertexBuf,
					IndexBuffer:  indexBuf,
					VertexCount:  uint32(len(meshAsset.indexes)),
				})
			} else if meshAsset.version > gpuMesh.version {
				// WGPU mesh is out of date - needs updating
				// TODO implement
			}
			return true
		},
		wgpuMesh{}, wgpuMaterial{})
}

// TODO load this by user
const texelsSize = 256

func createTexels() (texels [texelsSize * texelsSize]uint8) {
	for id := 0; id < (texelsSize * texelsSize); id++ {
		cx := 3.0*float32(id%texelsSize)/float32(texelsSize-1) - 2.0
		cy := 2.0*float32(id/texelsSize)/float32(texelsSize-1) - 1.0
		x, y, count := float32(cx), float32(cy), uint8(0)
		for count < 0xFF && x*x+y*y < 4.0 {
			oldX := x
			x = x*x - y*y + cx
			y = 2.0*oldX*y + cy
			count += 1
		}
		texels[id] = count
	}

	return texels
}

func loadTestTexture(s *gpuState) *wgpu.TextureView {
	texels := createTexels()
	textureExtent := wgpu.Extent3D{
		Width:              texelsSize,
		Height:             texelsSize,
		DepthOrArrayLayers: 1,
	}
	texture, err := s.device.CreateTexture(&wgpu.TextureDescriptor{
		Size:          textureExtent,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatR8Uint,
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

	s.queue.WriteTexture(
		texture.AsImageCopy(),
		wgpu.ToBytes(texels[:]),
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  texelsSize,
			RowsPerImage: wgpu.CopyStrideUndefined,
		},
		&textureExtent,
	)
	return textureView
}

func createRenderPipeline(material MaterialAsset, device *wgpu.Device, config *wgpu.SurfaceConfiguration) *wgpu.RenderPipeline {
	//TODO cache shader
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

func createVertexBufferLayout(vertexType any) wgpu.VertexBufferLayout {
	t := reflect.TypeOf(vertexType)
	if t.Kind() != reflect.Struct {
		panic("Vertex must be a struct")
	}

	var attributes []wgpu.VertexAttribute
	var offset uint64 = 0

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if "layout" == field.Tag.Get("gekko") {
			format := parseFormat(field.Tag.Get("format"))
			location, err := strconv.Atoi(field.Tag.Get("location"))
			if nil != err {
				panic(err)
			}

			attributes = append(attributes, wgpu.VertexAttribute{
				ShaderLocation: uint32(location),
				Offset:         offset,
				Format:         format,
			})
		}

		// Add size of field to offset
		offset += uint64(field.Type.Size())
	}

	return wgpu.VertexBufferLayout{
		ArrayStride: offset,
		StepMode:    wgpu.VertexStepModeVertex,
		Attributes:  attributes,
	}
}

func parseFormat(name string) wgpu.VertexFormat {
	switch name {
	case "float2":
		return wgpu.VertexFormatFloat32x2
	case "float3":
		return wgpu.VertexFormatFloat32x3
	case "float4":
		return wgpu.VertexFormatFloat32x4
	default:
		panic("unsupported vertex layout format: " + name)
	}
}

func untypedSliceToWgpuBytes(src AnySlice) []byte {
	l := src.Len()
	if l == 0 {
		return nil
	}

	return unsafe.Slice((*byte)(src.DataPointer()), l*src.ElementSize())
}

func createBuffers(mesh MeshAsset, device *wgpu.Device, config *wgpu.SurfaceConfiguration) (vertexBuf *wgpu.Buffer, indexBuf *wgpu.Buffer) {
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
		Contents: wgpu.ToBytes(mesh.indexes),
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		panic(err)
	}
	return vertexBuf, indexBuf
}

func buildMvpMatrix(c *CameraComponent, t *TransformComponent) mgl32.Mat4 {
	model := mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z()).
		Mul4(mgl32.HomogRotate3DZ(t.Rotation)).
		Mul4(mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z()))
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
	return projection.Mul4(view).Mul4(model)
}

func rendering(cmd *Commands, s *gpuState) {
	var camera *CameraComponent
	MakeQuery1[CameraComponent](cmd).Map1(
		func(entityId EntityId, c *CameraComponent) bool {
			camera = c
			//TODO multiple cameras?
			return true
		})

	materialsWithMashes := map[AssetId][]*wgpuMesh{}
	mashesWithMvps := map[AssetId]mgl32.Mat4{}
	materials := map[AssetId]*wgpuMaterial{}

	MakeQuery3[wgpuMesh, wgpuMaterial, TransformComponent](cmd).Map3(
		func(entityId EntityId, mesh *wgpuMesh, material *wgpuMaterial, transform *TransformComponent) bool {
			materials[material.id] = material
			matMeshes, contains := materialsWithMashes[material.id]
			if contains {
				matMeshes = append(matMeshes, mesh)
			} else {
				materialsWithMashes[material.id] = []*wgpuMesh{mesh}
			}
			mashesWithMvps[mesh.id] = buildMvpMatrix(camera, transform)
			return true
		})

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

	for materialId, mashes := range materialsWithMashes {
		material := materials[materialId]
		for _, mesh := range mashes {
			mvp := mashesWithMvps[mesh.id]
			err = s.queue.WriteBuffer(material.UniformBuffer, 0, wgpu.ToBytes(mvp[:]))
			if err != nil {
				panic(err)
			}
			renderPass.SetPipeline(material.Pipeline)
			renderPass.SetBindGroup(0, material.BindGroup, nil)
			renderPass.SetIndexBuffer(mesh.IndexBuffer, wgpu.IndexFormatUint16, 0, wgpu.WholeSize)
			renderPass.SetVertexBuffer(0, mesh.VertexBuffer, 0, wgpu.WholeSize)
			renderPass.DrawIndexed(mesh.VertexCount, 1, 0, 0, 0)
		}
	}
	err = renderPass.End()
	if err != nil {
		panic(err)
	}
	renderPass.Release()

	cmdBuffer, err := encoder.Finish(nil)
	if err != nil {
		panic(err)
	}
	defer cmdBuffer.Release()

	s.queue.Submit(cmdBuffer)
	s.surface.Present()
}
