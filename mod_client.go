package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

type ClientModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
}

type WgpuMesh struct {
	version      uint
	VertexBuffer wgpu.Buffer
	VertexCount  uint32
}

type WgpuMaterial struct {
	version       uint
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

type clientState struct {
	// glfw
	windowGlfw   *glfw.Window
	windowWidth  int
	windowHeight int
	windowTitle  string

	// wgpu
	Instance      wgpu.Instance
	Surface       wgpu.Surface
	Adapter       wgpu.Adapter
	Device        wgpu.Device
	Queue         wgpu.Queue
	SurfaceConfig wgpu.SurfaceConfiguration
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

	// https://github.com/cogentcore/webgpu
	instance := wgpu.CreateInstance(nil)                                  // CreateInstance(): the root wgpu object.
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(win)) // CreateSurface(): wraps GLFW window into a wgpu surface.

	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{ // RequestAdapter(): finds a suitable GPU (discrete GPU preferred).
		CompatibleSurface: surface,
		PowerPreference:   wgpu.PowerPreferenceHighPerformance,
	})
	if err != nil {
		panic(err)
	}

	device, queue := adapter.RequestDevice(&wgpu.DeviceDescriptor{ // RequestDevice(): allocates the device and command queue.
		Label:            "Main Device",
		RequiredFeatures: nil,
		RequiredLimits:   nil,
	})
	_ = queue

	caps := surface.GetCapabilities(adapter)

	surfaceConfig := wgpu.SurfaceConfiguration{ // Configure(surface): defines how the swapchain behaves (size, format, vsync).
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      caps.Formats[0],
		Width:       uint32(mod.WindowWidth),
		Height:      uint32(mod.WindowHeight),
		PresentMode: wgpu.PresentModeFifo, // vsync
		AlphaMode:   caps.AlphaModes[0],
	}
	surface.Configure(adapter, device, &surfaceConfig)

	// for !win.ShouldClose() {
	// 	glfw.PollEvents()

	// 	// --- Drawing code will come later ---
	// }

	//win.Destroy()

	app.UseSystem(
		System(windowEventsSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(renderSyncAssets).
			InStage(PreRender).
			RunAlways(),
	)

	cmd.AddResources(&clientState{
		windowGlfw:   win,
		windowWidth:  mod.WindowWidth,
		windowHeight: mod.WindowHeight,
		windowTitle:  mod.WindowTitle,
	})
}

func renderSyncAssets(cmd *Commands, assets *AssetServer) {
	MakeQuery2[Mesh, WgpuMesh](cmd).Map2(func(entityId EntityId, mesh *Mesh, wgpuMesh *WgpuMesh) bool {
		asset := assets.meshes[mesh.assetId]

		if nil == wgpuMesh {
			// WGPU mesh doesn't exist - needs creating
		} else if asset.version > wgpuMesh.version {
			// WGPU mesh is out of date - needs updating
		}

		return true
	}, WgpuMesh{})
}

func windowEventsSystem(state *clientState) {
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
