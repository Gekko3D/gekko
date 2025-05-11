package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"math"
	"unsafe"
)

type ClientModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
}

type WgpuMesh struct {
	id           AssetId
	version      uint
	VertexBuffer *wgpu.Buffer
	IndexBuffer  *wgpu.Buffer
	VertexCount  uint32
}

type WgpuMaterial struct {
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
	Position  mgl32.Vec3
	Direction mgl32.Vec3
	Up        mgl32.Vec3
	Fov       float32
	Aspect    float32
	Near      float32
	Far       float32
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
	// https://github.com/go-gl/glfw
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
		System(windowEventsSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
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

func loadGpuResources(cmd *Commands, assets *AssetServer, state *gpuState) {
	MakeQuery4[Mesh, Material, WgpuMesh, WgpuMaterial](cmd).Map4(
		func(entityId EntityId, mesh *Mesh, material *Material,
			wgpuMesh *WgpuMesh, wgpuMaterial *WgpuMaterial) bool {

			materialAsset := assets.materials[material.assetId]
			var pipeline *wgpu.RenderPipeline
			if nil == wgpuMaterial {
				// WGPU material doesn't exist - needs creating
				pipeline = createRenderPipeline(materialAsset, state.device, state.surfaceConfig)
				mvpMatrix := generateMvpMatrix(float32(state.surfaceConfig.Width) / float32(state.surfaceConfig.Height))
				uniformBuf, err := state.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
					Label:    "Uniform Buffer",
					Contents: wgpu.ToBytes(mvpMatrix[:]),
					Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
				})
				if err != nil {
					panic(err)
				}
				bindGroupLayout := pipeline.GetBindGroupLayout(0)
				defer bindGroupLayout.Release()
				bindGroup, err := state.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
					Layout: bindGroupLayout,
					Entries: []wgpu.BindGroupEntry{
						{
							Binding: 0,
							Buffer:  uniformBuf,
							Size:    wgpu.WholeSize,
						},
					},
				})
				if err != nil {
					panic(err)
				}

				cmd.AddComponents(entityId, WgpuMaterial{
					id:            material.assetId,
					version:       0,
					Pipeline:      pipeline,
					BindGroup:     bindGroup,
					UniformBuffer: uniformBuf,
				})
			} else if materialAsset.version > wgpuMaterial.version {
				// WGPU material is out of date - needs updating
				// TODO implement
				pipeline = wgpuMaterial.Pipeline
			}

			meshAsset := assets.meshes[mesh.assetId]
			if nil == wgpuMesh {
				// WGPU mesh doesn't exist - needs creating
				vertexBuf, indexBuf := createBuffers(meshAsset, state.device, state.surfaceConfig)
				cmd.AddComponents(entityId, WgpuMesh{
					id:           mesh.assetId,
					version:      0,
					VertexBuffer: vertexBuf,
					IndexBuffer:  indexBuf,
					VertexCount:  uint32(len(meshAsset.indexes)),
				})
			} else if meshAsset.version > wgpuMesh.version {
				// WGPU mesh is out of date - needs updating
				// TODO implement
			}
			return true
		},
		WgpuMesh{}, WgpuMaterial{})
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

	//TODO let the user define this via MaterialAsset
	vertexBufferLayout := wgpu.VertexBufferLayout{
		ArrayStride: uint64(unsafe.Sizeof(mgl32.Vec3{})),
		StepMode:    wgpu.VertexStepModeVertex,
		Attributes: []wgpu.VertexAttribute{
			{
				Format:         wgpu.VertexFormatFloat32x4,
				Offset:         0,
				ShaderLocation: 0,
			},
		},
	}

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

func createBuffers(mesh MeshAsset, device *wgpu.Device, config *wgpu.SurfaceConfiguration) (vertexBuf *wgpu.Buffer, indexBuf *wgpu.Buffer) {
	vertexBuf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: wgpu.ToBytes(mesh.vertices[:]),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		panic(err)
	}
	indexBuf, err = device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(mesh.indexes[:]),
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		panic(err)
	}
	return vertexBuf, indexBuf
}

// TODO remove this func
func generateMvpMatrix(aspectRatio float32) mgl32.Mat4 {
	projection := mgl32.Perspective(math.Pi/4, aspectRatio, 1, 10)
	view := mgl32.LookAtV(
		mgl32.Vec3{1.5, -5, 3},
		mgl32.Vec3{0, 0, 0},
		mgl32.Vec3{0, 0, 1},
	)

	return projection.Mul4(view)
}

// TODO reuse generateMvpMatrix
func buildMvpMatrix(c *CameraComponent, t *TransformComponent, sc *wgpu.SurfaceConfiguration) mgl32.Mat4 {
	return generateMvpMatrix(float32(sc.Width) / float32(sc.Height))
	//model := mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z()).
	//	Mul4(mgl32.HomogRotate3DZ(t.Rotation)).
	//	Mul4(mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z()))
	//view := mgl32.LookAtV(
	//	c.Position,
	//	c.Position.Add(c.Direction),
	//	c.Up,
	//)
	//projection := mgl32.Perspective(
	//	mgl32.DegToRad(c.Fov),
	//	c.Aspect,
	//	c.Near,
	//	c.Far,
	//)
	//return projection.Mul4(view).Mul4(model)
}

func rendering(cmd *Commands, s *gpuState) {
	var camera *CameraComponent
	MakeQuery1[CameraComponent](cmd).Map1(
		func(entityId EntityId, c *CameraComponent) bool {
			camera = c
			return true
		})

	materialsWithMashes := map[AssetId][]*WgpuMesh{}
	mashesWithMvps := map[AssetId]mgl32.Mat4{}
	materials := map[AssetId]*WgpuMaterial{}

	MakeQuery3[WgpuMesh, WgpuMaterial, TransformComponent](cmd).Map3(
		func(entityId EntityId, mesh *WgpuMesh, material *WgpuMaterial, transform *TransformComponent) bool {
			materials[material.id] = material
			matMeshes, contains := materialsWithMashes[material.id]
			if contains {
				matMeshes = append(matMeshes, mesh)
			} else {
				materialsWithMashes[material.id] = []*WgpuMesh{mesh}
			}
			mashesWithMvps[mesh.id] = buildMvpMatrix(camera, transform, s.surfaceConfig)
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

func windowEventsSystem(state *WindowState) {
	if !state.windowGlfw.ShouldClose() {
		glfw.PollEvents()
	}

	// Keyboard state
	// if (glfwGetKey(window, GLFW_KEY_SPACE) == GLFW_PRESS) {
	// 	// Spacebar is being held down
	// }

	// Mouse state
	// if (glfwGetMouseButton(window, GLFW_MOUSE_BUTTON_LEFT) == GLFW_PRESS) {
	// 	// Left mouse button is held
	// }

	// Cursor position
	// void cursor_position_callback(GLFWwindow* window, double xpos, double ypos) {
	// 	// Mouse moved to (xpos, ypos)
	// }
	// glfwSetCursorPosCallback(window, cursor_position_callback);

	// Mouse scroll
	// void scroll_callback(GLFWwindow* window, double xoffset, double yoffset) {
	// 	// Mouse wheel scroll
	// }
	// glfwSetScrollCallback(window, scroll_callback);
}

/*
package main

import (
	"log"
	"math"
	"runtime"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/rajveermalviya/go-webgpu/wgpu"
)

// ECS Types

type Entity uint32
var nextEntityID Entity = 1

func NewEntity() Entity {
	id := nextEntityID
	nextEntityID++
	return id
}

type MeshComponent struct {
	VertexBuffer wgpu.Buffer
	VertexCount  uint32
}

type MaterialComponent struct {
	Pipeline      wgpu.RenderPipeline
	BindGroup     wgpu.BindGroup
	UniformBuffer wgpu.Buffer // MVP matrix
}

type TransformComponent struct {
	Position mgl32.Vec3
	Rotation float32
	Scale    mgl32.Vec3
}

type CameraComponent struct {
	Position  mgl32.Vec3
	Direction mgl32.Vec3
	Up        mgl32.Vec3
	Fov       float32
	Aspect    float32
	Near      float32
	Far       float32
}

// ECS Storage
var (
	MeshComponents      = map[Entity]MeshComponent{}
	MaterialComponents  = map[Entity]MaterialComponent{}
	TransformComponents = map[Entity]TransformComponent{}
)

func init() {
	runtime.LockOSThread()
}

func main() {
	if err := glfw.Init(); err != nil {
		log.Fatalln("Failed to initialize GLFW:", err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(800, 600, "Rotating Triangle", nil, nil)
	if err != nil {
		log.Fatalln("Failed to create window:", err)
	}

	instance := wgpu.CreateInstance(nil)
	surface := instance.CreateSurfaceFromWindow(window)
	adapter := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
	}, nil)
	device, queue := adapter.RequestDevice(nil)

	swapChainFormat := wgpu.TextureFormatBgra8Unorm
	config := wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      swapChainFormat,
		Width:       800,
		Height:      600,
		PresentMode: wgpu.PresentModeFifo,
	}
	surface.Configure(device, &config)

	shaderCode := `
        struct Uniforms {
            mvp: mat4x4<f32>,
        };
        @group(0) @binding(0)
        var<uniform> uniforms: Uniforms;

        @vertex
        fn vs_main(@location(0) position: vec2<f32>, @location(1) color: vec3<f32>)
            -> @builtin(position) vec4<f32> {
            let pos = vec4<f32>(position, 0.0, 1.0);
            return uniforms.mvp * pos;
        }

        @fragment
        fn fs_main(@location(0) color: vec3<f32>) -> @location(0) vec4<f32> {
            return vec4<f32>(color, 1.0);
        }`

	shaderModule := device.CreateShaderModule(wgpu.ShaderModuleDescriptor{
		Code: shaderCode,
	})

	bindGroupLayout := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Entries: []wgpu.BindGroupLayoutEntry{ {
			Binding:    0,
			Visibility: wgpu.ShaderStageVertex,
			Buffer:     &wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform},
		} },
	})

	pipelineLayout := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []wgpu.BindGroupLayout{bindGroupLayout},
	})

	pipeline := device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shaderModule,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{ {
				ArrayStride: 5 * 4,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x2, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x3, Offset: 2 * 4, ShaderLocation: 1},
				},
			} },
		},
		Fragment: &wgpu.FragmentState{
			Module:     shaderModule,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{ {
				Format: swapChainFormat,
			} },
		},
		Primitive: wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
	})

	camera := CameraComponent{
		Position:  mgl32.Vec3{0, 0, 2},
		Direction: mgl32.Vec3{0, 0, -1},
		Up:        mgl32.Vec3{0, 1, 0},
		Fov:       mgl32.DegToRad(45),
		Aspect:    800.0 / 600.0,
		Near:      0.1,
		Far:       100,
	}

	triangle := AddTriangleEntity(device, pipeline, bindGroupLayout)
	entities := []Entity{triangle}
	last := time.Now()

	for !window.ShouldClose() {
		glfw.PollEvents()
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now

		for _, e := range entities {
			t := TransformComponents[e]
			t.Rotation += dt
			TransformComponents[e] = t
		}

		UpdateTransforms(queue, entities, camera)
		RenderSystem(device, surface, entities)
	}
}

func AddTriangleEntity(device wgpu.Device, pipeline wgpu.RenderPipeline, layout wgpu.BindGroupLayout) Entity {
	entity := NewEntity()

	vertices := []float32{
		0.0, 0.5, 1.0, 0.0, 0.0,
		-0.5, -0.5, 0.0, 1.0, 0.0,
		0.5, -0.5, 0.0, 0.0, 1.0,
	}
	vertexBuffer := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Contents: wgpu.ToBytes(vertices),
		Usage:    wgpu.BufferUsageVertex,
	})

	uniformBuffer := device.CreateBuffer(&wgpu.BufferDescriptor{
		Size:  64,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})

	bindGroup := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{ {
			Binding: 0,
			Buffer:  uniformBuffer,
		} },
	})

	MeshComponents[entity] = MeshComponent{
		VertexBuffer: vertexBuffer,
		VertexCount:  3,
	}
	MaterialComponents[entity] = MaterialComponent{
		Pipeline:      pipeline,
		BindGroup:     bindGroup,
		UniformBuffer: uniformBuffer,
	}
	TransformComponents[entity] = TransformComponent{
		Position: mgl32.Vec3{0, 0, 0},
		Rotation: 0,
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	return entity
}

func UpdateTransforms(queue wgpu.Queue, entities []Entity, camera CameraComponent) {
	view := mgl32.LookAtRH(camera.Position, camera.Position.Add(camera.Direction), camera.Up)
	proj := mgl32.Perspective(camera.Fov, camera.Aspect, camera.Near, camera.Far)

	for _, e := range entities {
		transform, ok := TransformComponents[e]
		mat, hasMat := MaterialComponents[e]
		if !ok || !hasMat {
			continue
		}

		rot := mgl32.HomogRotate3DZ(transform.Rotation)
		translation := mgl32.Translate3D(transform.Position.X(), transform.Position.Y(), transform.Position.Z())
		scale := mgl32.Scale3D(transform.Scale.X(), transform.Scale.Y(), transform.Scale.Z())

		model := scale.Mul4(rot).Mul4(translation) // row-major
		mvp := model.Mul4(view).Mul4(proj)         // row-major MVP
		mvpT := mvp.Transpose()                    // upload as column-major for WGSL

		queue.WriteBuffer(mat.UniformBuffer, 0, mvpT[:])
	}
}

func RenderSystem(device wgpu.Device, surface wgpu.Surface, entities []Entity) {
	view := surface.GetCurrentTextureView()
	encoder := device.CreateCommandEncoder()

	rpass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{ {
			View:       view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: 0.1, G: 0.1, B: 0.1, A: 1.0},
		} },
	})

	for _, e := range entities {
		mesh, hasMesh := MeshComponents[e]
		mat, hasMat := MaterialComponents[e]
		if !hasMesh || !hasMat {
			continue
		}
		rpass.SetPipeline(mat.Pipeline)
		rpass.SetBindGroup(0, mat.BindGroup, nil)
		rpass.SetVertexBuffer(0, mesh.VertexBuffer, 0, 0)
		rpass.Draw(mesh.VertexCount, 1, 0, 0)
	}

	rpass.End()
	device.QueueSubmit([]wgpu.CommandBuffer{encoder.Finish()})
	surface.Present()
}

*/
