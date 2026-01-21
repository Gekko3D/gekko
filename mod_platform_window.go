package gekko

import (
	"reflect"
)

// PlatformWindowModule ensures a single shared GLFW window (WindowState) is created
// and made available as a resource for any renderer or input module.
// Install is idempotent: if a WindowState resource already exists, it is reused.
type PlatformWindowModule struct {
	Width  int
	Height int
	Title  string
}

// NewPlatformWindow creates a module that provides a shared WindowState resource.
// If Width/Height are zero, sensible defaults are used.
func NewPlatformWindow(width, height int, title string) *PlatformWindowModule {
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	if title == "" {
		title = "Gekko"
	}
	return &PlatformWindowModule{
		Width:  width,
		Height: height,
		Title:  title,
	}
}

// Install provides the WindowState resource if missing.
func (m PlatformWindowModule) Install(app *App, cmd *Commands) {
	// Resource type key for WindowState
	t := reflect.TypeOf((*WindowState)(nil)).Elem()
	if _, ok := app.resources[t]; ok {
		// Already created by another module (or user code); no-op to preserve single-window invariant.
		return
	}

	ws := createWindowState(m.Width, m.Height, m.Title)
	app.addResources(ws)
}