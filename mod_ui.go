package gekko

type UiAnchor int

const (
	UiAnchorTopLeft UiAnchor = iota
	UiAnchorTopRight
	UiAnchorBottomLeft
	UiAnchorBottomRight
	UiAnchorTopCenter
	UiAnchorBottomCenter
	UiAnchorCenter
)

type UiModule struct{}

func (UiModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(newUiRuntime())
	app.UseSystem(System(uiPanelInputSystem).InStage(PreUpdate).RunAlways())
	app.UseSystem(System(uiPanelRenderSystem).InStage(PostUpdate).RunAlways())
}

func resolveUiPosition(anchor UiAnchor, offset [2]float32, width, height float32, winW, winH int) (float32, float32) {
	var x, y float32
	w, h := float32(winW), float32(winH)

	switch anchor {
	case UiAnchorTopLeft:
		x, y = offset[0], offset[1]
	case UiAnchorTopRight:
		x, y = w-width-offset[0], offset[1]
	case UiAnchorBottomLeft:
		x, y = offset[0], h-height-offset[1]
	case UiAnchorBottomRight:
		x, y = w-width-offset[0], h-height-offset[1]
	case UiAnchorTopCenter:
		x, y = (w-width)/2+offset[0], offset[1]
	case UiAnchorBottomCenter:
		x, y = (w-width)/2+offset[0], h-height-offset[1]
	case UiAnchorCenter:
		x, y = (w-width)/2+offset[0], (h-height)/2+offset[1]
	}
	return x, y
}
