package gekko

import (
	"reflect"
)

// RendererName identifies a concrete renderer module.
// Keep names aligned with ensureSingleRenderer tags.
type RendererName string

const (
	RendererWGPU    RendererName = "wgpu"
	RendererVoxelRT RendererName = "voxelrt"
)

// Renderer is an alias to Module for semantic clarity in APIs.
type Renderer interface {
	Module
}

// ensureWindowResource guarantees a single shared WindowState resource exists.
// If missing, it creates one with provided overrides or sensible defaults.
func ensureWindowResource(app *App, width, height int, title string) {
	t := reflect.TypeOf((*WindowState)(nil)).Elem()
	if _, ok := app.resources[t]; ok {
		return
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	if title == "" {
		title = "Gekko"
	}
	ws := createWindowState(width, height, title)
	app.addResources(ws)
	app.Logger().Infof("Created shared window (%dx%d) '%s'", width, height, title)
}

// UseRenderer installs exactly one renderer module, enforcing exclusivity via ensureSingleRenderer,
// and ensures a shared WindowState exists (created with defaults if missing).
// Usage:
//   app.UseRenderer(RendererWGPU, ClientModule{})
func (app *App) UseRenderer(name RendererName, mod Module) *App {
	ensureSingleRenderer(app, string(name))
	ensureWindowResource(app, 0, 0, "")
	app.Logger().Infof("Renderer selected: %s", name)
	app.UseModules(mod)
	return app
}

// UseRendererWithWindow installs the renderer and ensures a shared window with explicit size/title.
// Useful for examples/tests to control window params from a single place.
func (app *App) UseRendererWithWindow(name RendererName, mod Module, width, height int, title string) *App {
	ensureSingleRenderer(app, string(name))
	ensureWindowResource(app, width, height, title)
	app.Logger().Infof("Renderer selected: %s", name)
	app.UseModules(mod)
	return app
}
// Convenience wrappers for common renderer selections

// UseWGPU selects the WGPU renderer and ensures a shared window with the given parameters.
// Equivalent to calling UseRendererWithWindow(RendererWGPU, ClientModule{...}, ...).
func (app *App) UseWGPU(width, height int, title string) *App {
	return app.UseRendererWithWindow(RendererWGPU, ClientModule{
		WindowWidth:  width,
		WindowHeight: height,
		WindowTitle:  title,
	}, width, height, title)
}

// UseVoxelRT selects the Voxel RT renderer and ensures a shared window with the given parameters.
// Uses default AmbientLight (zero), DebugMode (false), and RenderModeLit.
// For advanced options, call UseRendererWithWindow with a configured VoxelRtModule directly.
func (app *App) UseVoxelRT(width, height int, title string) *App {
	return app.UseRendererWithWindow(RendererVoxelRT, VoxelRtModule{
		WindowWidth:  width,
		WindowHeight: height,
		WindowTitle:  title,
		DebugMode:    false,
		RenderMode:   RenderModeLit,
	}, width, height, title)
}