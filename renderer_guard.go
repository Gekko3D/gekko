package gekko

import (
	"fmt"
	"reflect"
)

// RendererTag marks that a renderer has been installed into the App.
// Only one renderer should be installed at a time.
type RendererTag struct {
	Name string
}

// ensureSingleRenderer enforces a single renderer invariant.
// If a different renderer is already installed, it panics with a clear message.
func ensureSingleRenderer(app *App, name string) {
	if app == nil {
		panic("ensureSingleRenderer: app is nil")
	}
	t := reflect.TypeOf((*RendererTag)(nil)).Elem()
	if res, ok := app.resources[t]; ok {
		if tag, ok2 := res.(*RendererTag); ok2 {
			if tag.Name != name {
				// Also log via injected logger if present, then fail fast
				app.Logger().Errorf("Multiple renderers installed: %s and %s", tag.Name, name)
				panic(fmt.Sprintf("Multiple renderers installed: %s and %s", tag.Name, name))
			}
			return
		}
		// Unexpected type collision
		panic("RendererTag resource present with unexpected type")
	}
	app.addResources(&RendererTag{Name: name})
}