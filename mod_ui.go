package gekko

import (
	"strings"
)

type UiModule struct{}

type UiButton struct {
	Label       string
	Position    [2]float32 // Screen pixels, top-left
	Width       float32    // Optional fixed width. If 0, auto-size.
	Scale       float32    // Optional scale multiplier (default 1.0)
	Clicked     bool
	Highlighted bool
	OnClick     func()
}

type UiTable struct {
	Headers  []string
	Rows     [][]string
	Position [2]float32
	Width    float32 // Optional fixed total width.
	Scale    float32 // Optional scale multiplier (default 1.0)
}

type UiListItem struct {
	Label    string
	Children []UiListItem
}

type UiList struct {
	Title    string
	Items    []UiListItem
	Position [2]float32
	Scale    float32 // Optional scale multiplier (default 1.0)
}

func (UiModule) Install(app *App, cmd *Commands) {
	app.UseSystem(System(uiInputSystem).InStage(PreUpdate).RunAlways())
	app.UseSystem(System(uiRenderSystem).InStage(PostUpdate).RunAlways())
}

const (
	uiButtonPaddingX = 20.0
)

func uiInputSystem(state *VoxelRtState, input *Input, cmd *Commands) {
	if state == nil || input.WindowWidth == 0 {
		return
	}

	mx, my := input.MouseX, input.MouseY
	isLMB := input.JustPressed[MouseButtonLeft]

	pixelRatio := float32(state.RtApp.Config.Width) / float32(input.WindowWidth)
	if pixelRatio <= 0 {
		pixelRatio = 1.0
	}

	MakeQuery1[UiButton](cmd).Map(func(eid EntityId, btn *UiButton) bool {
		btn.Clicked = false

		scale := btn.Scale
		if scale <= 0 {
			scale = 1.0
		}

		physTw, physTh := state.MeasureText(btn.Label, scale)
		tw := physTw / pixelRatio

		w := btn.Width
		if w == 0 {
			w = tw + uiButtonPaddingX*2.0*scale
		}

		// Button is 3 lines high
		h := 3.0 * (physTh / pixelRatio)

		if mx >= float64(btn.Position[0]) && mx <= float64(btn.Position[0]+w) &&
			my >= float64(btn.Position[1]) && my <= float64(btn.Position[1]+h) {
			btn.Highlighted = true
			if isLMB {
				btn.Clicked = true
				if btn.OnClick != nil {
					btn.OnClick()
				}
			}
		} else {
			btn.Highlighted = false
		}

		return true
	})
}

func uiRenderSystem(state *VoxelRtState, input *Input, cmd *Commands) {
	if state == nil || input.WindowWidth == 0 {
		return
	}

	pixelRatio := float32(state.RtApp.Config.Width) / float32(input.WindowWidth)
	if pixelRatio <= 0 {
		pixelRatio = 1.0
	}

	// Constants for ASCII style
	normalColor := [4]float32{1, 1, 1, 1}
	highlightColor := [4]float32{1, 1, 0, 1}
	textColor := [4]float32{1, 1, 1, 1}

	// Precise horizontal line by drawing corners and filling with dashes
	drawBoxHLine := func(x, y, w, scale float32, color [4]float32) {
		plusW, _ := state.MeasureText("+", scale)
		dashW, _ := state.MeasureText("-", scale)
		if dashW <= 0 {
			dashW = 10 * scale
		}

		state.DrawText("+", x, y, scale, color)

		interiorW := w - 2.0*plusW
		if interiorW > 0 {
			count := int(interiorW/dashW) + 1
			state.DrawText(strings.Repeat("-", count), x+plusW, y, scale, color)
		}

		state.DrawText("+", x+w-plusW, y, scale, color)
	}

	// Render Buttons
	MakeQuery1[UiButton](cmd).Map(func(eid EntityId, btn *UiButton) bool {
		color := normalColor
		if btn.Highlighted {
			color = highlightColor
		}

		scale := btn.Scale
		if scale <= 0 {
			scale = 1.0
		}

		drawX := btn.Position[0] * pixelRatio
		drawY := btn.Position[1] * pixelRatio

		tw, _ := state.MeasureText(btn.Label, scale)
		w := btn.Width * pixelRatio
		if w == 0 {
			w = tw + uiButtonPaddingX*2.0*pixelRatio*scale
		}

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		pipeW, _ := state.MeasureText("|", scale)

		y := drawY
		// 1. Top Border
		drawBoxHLine(drawX, y, w, scale, color)
		y += lineH

		// 2. Middle (Text and Vertical Bars)
		// Left Bar
		state.DrawText("|", drawX, y, scale, color)
		// Centered Text
		state.DrawText(btn.Label, drawX+(w-tw)/2.0, y, scale, textColor)
		// Right Bar
		state.DrawText("|", drawX+w-pipeW, y, scale, color)
		y += lineH

		// 3. Bottom Border
		drawBoxHLine(drawX, y, w, scale, color)

		return true
	})

	// Render Tables
	MakeQuery1[UiTable](cmd).Map(func(eid EntityId, table *UiTable) bool {
		if len(table.Headers) == 0 {
			return true
		}

		scale := table.Scale
		if scale <= 0 {
			scale = 1.0
		}

		drawX := table.Position[0] * pixelRatio
		drawY := table.Position[1] * pixelRatio

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		pipeW, _ := state.MeasureText("|", scale)
		dashW, _ := state.MeasureText("-", scale)
		if dashW <= 0 {
			dashW = 10 * scale
		}

		// 1. Determine Column Widths
		colWidths := make([]float32, len(table.Headers))
		padding := 20.0 * pixelRatio * scale
		for i, h := range table.Headers {
			tw, _ := state.MeasureText(h, scale)
			colWidths[i] = tw + padding // Min padding
		}
		for _, row := range table.Rows {
			for i, cell := range row {
				if i < len(colWidths) {
					tw, _ := state.MeasureText(cell, scale)
					if tw+padding > colWidths[i] {
						colWidths[i] = tw + padding
					}
				}
			}
		}

		// 2. Adjust for total width if specified
		tablePhysW := table.Width * pixelRatio
		if tablePhysW > 0 {
			sum := float32(0)
			for _, cw := range colWidths {
				sum += cw
			}
			extra := tablePhysW - sum - float32(len(table.Headers)+1)*pipeW
			if extra > 0 {
				added := extra / float32(len(table.Headers))
				for i := range colWidths {
					colWidths[i] += added
				}
			}
		}

		totalW := pipeW
		for _, cw := range colWidths {
			totalW += cw + pipeW
		}

		y := drawY

		drawTableHLine := func(x, y, scale float32, color [4]float32) {
			currX := x
			state.DrawText("+", currX, y, scale, color)
			currX += pipeW
			for _, cw := range colWidths {
				count := int(cw/dashW) + 1
				state.DrawText(strings.Repeat("-", count), currX, y, scale, color)
				currX += cw
				state.DrawText("+", currX, y, scale, color)
				currX += pipeW
			}
		}

		renderRow := func(items []string, yPos float32, color [4]float32) {
			currX := drawX
			state.DrawText("|", currX, yPos, scale, normalColor)
			currX += pipeW
			for i, item := range items {
				itemW, _ := state.MeasureText(item, scale)
				// Center alignment in column
				offsetX := (colWidths[i] - itemW) / 2.0
				state.DrawText(item, currX+offsetX, yPos, scale, color)
				currX += colWidths[i]
				state.DrawText("|", currX, yPos, scale, normalColor)
				currX += pipeW
			}
		}

		drawTableHLine(drawX, y, scale, normalColor)
		y += lineH
		renderRow(table.Headers, y, highlightColor)
		y += lineH
		drawTableHLine(drawX, y, scale, normalColor)
		y += lineH

		for _, row := range table.Rows {
			renderRow(row, y, textColor)
			y += lineH
		}
		drawTableHLine(drawX, y, scale, normalColor)

		return true
	})

	// Render Lists
	MakeQuery1[UiList](cmd).Map(func(eid EntityId, list *UiList) bool {
		scale := list.Scale
		if scale <= 0 {
			scale = 1.0
		}

		drawX := list.Position[0] * pixelRatio
		drawY := list.Position[1] * pixelRatio

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}

		y := drawY
		if list.Title != "" {
			state.DrawText("== "+list.Title+" ==", drawX, y, scale, highlightColor)
			y += lineH * 1.2
		}

		var renderItems func(items []UiListItem, x float32, depth int)
		renderItems = func(items []UiListItem, x float32, depth int) {
			for _, item := range items {
				prefix := ""
				for i := 0; i < depth; i++ {
					prefix += "  "
				}
				state.DrawText(prefix+"- "+item.Label, x, y, scale, textColor)
				y += lineH
				if len(item.Children) > 0 {
					renderItems(item.Children, x, depth+1)
				}
			}
		}

		renderItems(list.Items, drawX, 0)
		return true
	})
}
