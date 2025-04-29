package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
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
	instance := wgpu.CreateInstance(nil)   // CreateInstance(): the root wgpu object.
	surface := instance.CreateSurface(win) // CreateSurface(): wraps GLFW window into a wgpu surface.

	adapter := instance.RequestAdapter(&wgpu.RequestAdapterOptions{ // RequestAdapter(): finds a suitable GPU (discrete GPU preferred).
		CompatibleSurface: surface,
		PowerPreference:   wgpu.PowerPreferenceHighPerformance,
	})

	device, queue := adapter.RequestDevice(&wgpu.DeviceDescriptor{ // RequestDevice(): allocates the device and command queue.
		Label:            "Main Device",
		RequiredFeatures: nil,
		RequiredLimits:   nil,
	})

	surfaceConfig := wgpu.SurfaceConfiguration{ // Configure(surface): defines how the swapchain behaves (size, format, vsync).
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      surface.GetPreferredFormat(adapter),
		Width:       uint32(mod.WindowWidth),
		Height:      uint32(mod.WindowHeight),
		PresentMode: wgpu.PresentModeFifo, // vsync
	}
	surface.Configure(device, &surfaceConfig)

	for !win.ShouldClose() {
		win.PollEvents()

		// --- Drawing code will come later ---
	}

	win.Destroy()

	_ := queue

	cmd.AddResources(&clientState{
		windowGlfw:   win,
		windowWidth:  mod.WindowWidth,
		windowHeight: mod.WindowHeight,
		windowTitle:  mod.WindowTitle,
	})
}
