package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"
)

type ClientModule struct {
	WindowWidth  int
	WindowHeight int
	WindowTitle  string
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

	cmd.AddResources(&clientState{
		windowGlfw:   win,
		windowWidth:  mod.WindowWidth,
		windowHeight: mod.WindowHeight,
		windowTitle:  mod.WindowTitle,
	})
}

func windowEventsSystem(state *clientState) {
	if !state.windowGlfw.ShouldClose() {
		glfw.PollEvents()
	}
}
