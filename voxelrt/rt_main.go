package main

import (
	"flag"
	"runtime"
	"github.com/gekko3d/gekko/voxelrt/rt/app"

	"github.com/go-gl/glfw/v3.3/glfw"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	debug := flag.Bool("debug", false, "Enable debug mode (AABB visualization)")
	flag.Parse()

	if err := glfw.Init(); err != nil {
		panic(err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(1280, 720, "VoxelRT Go", nil, nil)
	if err != nil {
		panic(err)
	}
	defer window.Destroy()

	application := app.NewApp(window)
	application.DebugMode = *debug
	if err := application.Init(); err != nil {
		panic(err)
	}

	window.SetFramebufferSizeCallback(func(w *glfw.Window, width, height int) {
		application.Resize(width, height)
	})

	// Input callbacks
	window.SetCursorPosCallback(func(w *glfw.Window, xpos, ypos float64) {
		if application.MouseCaptured {
			dx := float32(xpos - 640) // Center
			dy := float32(ypos - 360)

			application.Camera.Yaw += dx * application.Camera.Sensitivity
			application.Camera.Pitch -= dy * application.Camera.Sensitivity

			// Clamp pitch
			// ...

			w.SetCursorPos(640, 360) // Reset
		}
	})

	window.SetKeyCallback(func(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		if key == glfw.KeyTab && action == glfw.Press {
			application.MouseCaptured = !application.MouseCaptured
			if application.MouseCaptured {
				w.SetInputMode(glfw.CursorMode, glfw.CursorDisabled) // Use Disabled for relative movement
			} else {
				w.SetInputMode(glfw.CursorMode, glfw.CursorNormal)
			}
		}
		if key == glfw.KeyEscape && action == glfw.Press {
			w.SetShouldClose(true)
		}
	})

	window.SetMouseButtonCallback(func(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		application.HandleClick(int(button), int(action))
	})

	for !window.ShouldClose() {
		glfw.PollEvents()
		application.Update()
		application.Render()
	}
}
