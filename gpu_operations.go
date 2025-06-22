package gekko

import (
	"bytes"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
	"reflect"
	"runtime"
	"strconv"
)

type WindowState struct {
	// glfw
	windowGlfw   *glfw.Window
	WindowWidth  int
	WindowHeight int
	windowTitle  string
}

type GpuState struct {
	surface       *wgpu.Surface
	adapter       *wgpu.Adapter
	device        *wgpu.Device
	queue         *wgpu.Queue
	surfaceConfig *wgpu.SurfaceConfiguration
}

func createWindowState(windowWidth int, windowHeight int, windowTitle string) *WindowState {
	runtime.LockOSThread()
	if err := glfw.Init(); err != nil {
		panic(err)
	}

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) // Important: tell GLFW we don't want OpenGL
	glfw.WindowHint(glfw.Resizable, glfw.True)

	win, err := glfw.CreateWindow(windowWidth, windowHeight, windowTitle, nil, nil)
	if err != nil {
		panic(err)
	}

	return &WindowState{
		windowGlfw:   win,
		WindowWidth:  windowWidth,
		WindowHeight: windowHeight,
		windowTitle:  windowTitle,
	}
}

func createGpuState(s *WindowState) *GpuState {
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	// wraps GLFW window into a wgpu surface.
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(s.windowGlfw))
	// finds a suitable GPU (discrete GPU preferred)
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: surface,
		PowerPreference:   wgpu.PowerPreferenceHighPerformance,
	})
	if err != nil {
		panic(err)
	}
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
		Width:       uint32(s.WindowWidth),
		Height:      uint32(s.WindowHeight),
		PresentMode: wgpu.PresentModeFifo, // vsync
		AlphaMode:   caps.AlphaModes[0],
	}

	surface.Configure(adapter, device, &surfaceConfig)

	return &GpuState{
		surface:       surface,
		adapter:       adapter,
		device:        device,
		queue:         queue,
		surfaceConfig: &surfaceConfig,
	}
}

func createRenderPipeline(name string, shaderCode string, vertexType any, gpuState *GpuState) *wgpu.RenderPipeline {
	shader, err := gpuState.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          name,
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaderCode},
	})
	if err != nil {
		panic(err)
	}
	defer shader.Release()

	vertexBufferLayout := createVertexBufferLayout(vertexType)

	pipeline, err := gpuState.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
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
					Format:    gpuState.surfaceConfig.Format,
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

func createVertexIndexBuffers(vertices AnySlice, indices []uint16, device *wgpu.Device) (vertexBuf *wgpu.Buffer, indexBuf *wgpu.Buffer) {
	vertexBuf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: untypedSliceToWgpuBytes(vertices),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		panic(err)
	}
	indexBuf, err = device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(indices),
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		panic(err)
	}
	return vertexBuf, indexBuf
}

func createTextureFromAsset(txAsset *TextureAsset, gpuState *GpuState) *wgpu.TextureView {
	textureExtent := wgpu.Extent3D{
		Width:              txAsset.width,
		Height:             txAsset.height,
		DepthOrArrayLayers: txAsset.depth,
	}
	texture, err := gpuState.device.CreateTexture(&wgpu.TextureDescriptor{
		Size:          textureExtent,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension(txAsset.dimension),
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

	err = gpuState.queue.WriteTexture(
		texture.AsImageCopy(),
		wgpu.ToBytes(txAsset.texels),
		&wgpu.TextureDataLayout{
			Offset:       0,
			BytesPerRow:  txAsset.width * uint32(wgpuBytesPerPixel(wgpu.TextureFormat(txAsset.format))),
			RowsPerImage: txAsset.height,
		},
		&textureExtent,
	)
	if err != nil {
		panic(err)
	}
	return textureView
}

func createBufferFromDescriptor(descriptor bufferDescriptor, gpuState *GpuState) *wgpu.Buffer {
	buffer, err := gpuState.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Buffer",
		Contents: descriptor.data,
		Usage:    descriptor.usage,
	})
	if err != nil {
		panic(err)
	}
	return buffer
}

func createBuffer(name string, data any, gpuState *GpuState, usage wgpu.BufferUsage) *wgpu.Buffer {
	bufferBytes := toBufferBytes(data)
	buffer, err := gpuState.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    name,
		Contents: bufferBytes,
		Usage:    usage,
	})
	if err != nil {
		panic(err)
	}
	return buffer
}

func toBufferBytes(data any) []byte {
	val := reflect.ValueOf(data)
	buf := new(bytes.Buffer)
	readUniformsBytes(val, buf)
	return buf.Bytes()
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

// buffer bindings per group id
func createBufferGroupedBindings(groupedBindings map[uint32][]wgpu.BindGroupEntry, bufferSet *wgpuBufferSet) map[uint32][]wgpu.BindGroupEntry {
	for buffId, buffer := range bufferSet.buffers {
		buffDesc := bufferSet.descriptors[buffId]
		binding := wgpu.BindGroupEntry{
			Binding: buffDesc.binding,
			Buffer:  buffer,
			Size:    wgpu.WholeSize,
		}
		if bindings, ok := groupedBindings[buffDesc.group]; !ok {
			groupedBindings[buffDesc.group] = []wgpu.BindGroupEntry{binding}
		} else {
			groupedBindings[buffDesc.group] = append(bindings, binding)
		}
	}
	return groupedBindings
}

func createTextureGroupedBindings(groupedBindings map[uint32][]wgpu.BindGroupEntry, txSet *wgpuTextureSet) map[uint32][]wgpu.BindGroupEntry {
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
	return groupedBindings
}

func createSamplerGroupedBindings(groupedBindings map[uint32][]wgpu.BindGroupEntry, samplers *wgpuSamplerSet) map[uint32][]wgpu.BindGroupEntry {
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
	return groupedBindings
}

func createBindGroups(groupedBindings map[uint32][]wgpu.BindGroupEntry, pipeline *wgpu.RenderPipeline, device *wgpu.Device) map[uint32]*wgpu.BindGroup {
	bindGroups := map[uint32]*wgpu.BindGroup{}
	for groupId, bindings := range groupedBindings {
		bindGroupLayout := pipeline.GetBindGroupLayout(groupId)
		defer bindGroupLayout.Release()

		bindGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout:  bindGroupLayout,
			Entries: bindings,
		})
		if err != nil {
			panic(err)
		}
		bindGroups[groupId] = bindGroup
	}
	return bindGroups
}
