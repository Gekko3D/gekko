package gekko

import (
	"strings"
)

const uiButtonPaddingX = 20.0

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

type UiButton struct {
	Label       string
	Position    [2]float32 // Screen pixels/offset
	Anchor      UiAnchor
	Width       float32 // Optional fixed width. If 0, auto-size.
	Scale       float32 // Optional scale multiplier (default 1.0)
	Clicked     bool
	Highlighted bool
	OnClick     func()
}

type UiTable struct {
	Headers     []string
	Rows        [][]string
	Position    [2]float32
	Anchor      UiAnchor
	Width       float32 // Optional fixed total width.
	Scale       float32 // Optional scale multiplier (default 1.0)
	OnCellClick func(row, col int)
}

type UiListItem struct {
	Label    string
	Children []UiListItem
	OnClick  func()
}

type UiList struct {
	Title    string
	Items    []UiListItem
	Position [2]float32
	Anchor   UiAnchor
	Scale    float32 // Optional scale multiplier (default 1.0)
}

type UiTextBox struct {
	Label    string
	Text     string
	Position [2]float32
	Anchor   UiAnchor
	Width    float32
	Scale    float32
	Focused  bool
	OnSubmit func(string)
}

func (UiModule) Install(app *App, cmd *Commands) {
	app.UseSystem(System(uiInputSystem).InStage(PreUpdate).RunAlways())
	app.UseSystem(System(uiRenderSystem).InStage(PostUpdate).RunAlways())
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

	// Handle Buttons
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
		h := 3.0 * (physTh / pixelRatio)
		posX, posY := resolveUiPosition(btn.Anchor, btn.Position, w, h, input.WindowWidth, input.WindowHeight)
		if mx >= float64(posX) && mx <= float64(posX+w) &&
			my >= float64(posY) && my <= float64(posY+h) {
			input.GuiCaptured = true
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

	// Handle TextBoxes
	MakeQuery1[UiTextBox](cmd).Map(func(eid EntityId, tb *UiTextBox) bool {
		scale := tb.Scale
		if scale <= 0 {
			scale = 1.0
		}
		displayLabel := tb.Label
		if tb.Text != "" {
			displayLabel = tb.Text
		}
		physTw, physTh := state.MeasureText(displayLabel, scale)
		tw := physTw / pixelRatio
		w := tb.Width
		if w == 0 {
			w = tw + uiButtonPaddingX*2.0*scale
		}
		h := 3.0 * (physTh / pixelRatio)
		posX, posY := resolveUiPosition(tb.Anchor, tb.Position, w, h, input.WindowWidth, input.WindowHeight)
		if mx >= float64(posX) && mx <= float64(posX+w) &&
			my >= float64(posY) && my <= float64(posY+h) {
			input.GuiCaptured = true
			if isLMB {
				tb.Focused = true
			}
		} else if isLMB {
			tb.Focused = false
		}
		if tb.Focused {
			for _, char := range input.CharBuffer {
				tb.Text += string(char)
			}
			if input.JustPressed[KeyBackspace] && len(tb.Text) > 0 {
				tb.Text = tb.Text[:len(tb.Text)-1]
			}
			if input.JustPressed[KeyEnter] {
				if tb.OnSubmit != nil {
					tb.OnSubmit(tb.Text)
				}
				tb.Focused = false
			}
		}
		return true
	})

	// Handle Lists
	MakeQuery1[UiList](cmd).Map(func(eid EntityId, list *UiList) bool {
		scale := list.Scale
		if scale <= 0 {
			scale = 1.0
		}
		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		var countItems func(items []UiListItem) int
		countItems = func(items []UiListItem) int {
			total := len(items)
			for _, item := range items {
				if len(item.Children) > 0 {
					total += countItems(item.Children)
				}
			}
			return total
		}
		totalItemCount := countItems(list.Items)
		estW := float32(200) * scale // Width is already in logical units
		estH := float32(totalItemCount) * lineH / pixelRatio
		if list.Title != "" {
			estH += lineH * 1.2 / pixelRatio
		}
		posX, posY := resolveUiPosition(list.Anchor, list.Position, estW, estH, input.WindowWidth, input.WindowHeight)
		if mx >= float64(posX) && mx <= float64(posX+estW) &&
			my >= float64(posY) && my <= float64(posY+estH) {
			input.GuiCaptured = true
		}
		currY := posY
		if list.Title != "" {
			currY += lineH * 1.2 / pixelRatio
		}
		var processItems func(items []UiListItem)
		processItems = func(items []UiListItem) {
			for i := range items {
				item := &items[i]
				itemH := lineH / pixelRatio
				if isLMB && mx >= float64(posX) && mx <= float64(posX+estW) &&
					my >= float64(currY) && my <= float64(currY+itemH) {
					if item.OnClick != nil {
						item.OnClick()
					}
				}
				currY += itemH
				if len(item.Children) > 0 {
					processItems(item.Children)
				}
			}
		}
		processItems(list.Items)
		return true
	})

	// Handle Tables
	MakeQuery1[UiTable](cmd).Map(func(eid EntityId, table *UiTable) bool {
		if len(table.Headers) == 0 {
			return true
		}
		scale := table.Scale
		if scale <= 0 {
			scale = 1.0
		}
		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		pipeWPhys, _ := state.MeasureText("|", scale)
		pipeW := pipeWPhys / pixelRatio
		colWidths := make([]float32, len(table.Headers))
		padding := 20.0 * scale
		for i, h := range table.Headers {
			twPhys, _ := state.MeasureText(h, scale)
			colWidths[i] = twPhys/pixelRatio + padding
		}
		for _, row := range table.Rows {
			for i, cell := range row {
				if i < len(colWidths) {
					twPhys, _ := state.MeasureText(cell, scale)
					if twPhys/pixelRatio+padding > colWidths[i] {
						colWidths[i] = twPhys/pixelRatio + padding
					}
				}
			}
		}
		if table.Width > 0 {
			sum := float32(0)
			for _, cw := range colWidths {
				sum += cw
			}
			extra := table.Width - sum - float32(len(table.Headers)+1)*pipeW
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
		h := float32(len(table.Rows)+3) * lineH / pixelRatio
		posX, posY := resolveUiPosition(table.Anchor, table.Position, totalW, h, input.WindowWidth, input.WindowHeight)
		if mx >= float64(posX) && mx <= float64(posX+totalW) &&
			my >= float64(posY) && my <= float64(posY+h) {
			input.GuiCaptured = true
			if isLMB {
				relativeY := (float32(my) - posY) / (lineH / pixelRatio)
				rowIdx := int(relativeY) - 3
				if rowIdx >= 0 && rowIdx < len(table.Rows) {
					currX := posX + pipeW
					for colIdx, cw := range colWidths {
						if mx >= float64(currX) && mx <= float64(currX+cw) {
							if table.OnCellClick != nil {
								table.OnCellClick(rowIdx, colIdx)
							}
							break
						}
						currX += cw + pipeW
					}
				}
			}
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
	dimColor := [4]float32{0.5, 0.5, 0.5, 1}

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

		tw, _ := state.MeasureText(btn.Label, scale)
		w := btn.Width * pixelRatio
		if w == 0 {
			w = tw + uiButtonPaddingX*2.0*pixelRatio*scale
		}

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		h := 3.0 * lineH
		pipeW, _ := state.MeasureText("|", scale)

		// Resolve position in window units, then convert to pixel ratio for rendering
		posX, posY := resolveUiPosition(btn.Anchor, btn.Position, w/pixelRatio, h/pixelRatio, input.WindowWidth, input.WindowHeight)
		drawX := posX * pixelRatio
		drawY := posY * pixelRatio

		y := drawY
		// 1. Top Border
		drawBoxHLine(drawX, y, w, scale, color)
		y += lineH

		// 2. Middle (Text and Vertical Bars)
		state.DrawText("|", drawX, y, scale, color)
		state.DrawText(btn.Label, drawX+(w-tw)/2.0, y, scale, textColor)
		state.DrawText("|", drawX+w-pipeW, y, scale, color)
		y += lineH

		// 3. Bottom Border
		drawBoxHLine(drawX, y, w, scale, color)

		return true
	})

	// Render TextBoxes
	MakeQuery1[UiTextBox](cmd).Map(func(eid EntityId, tb *UiTextBox) bool {
		color := normalColor
		if tb.Focused {
			color = highlightColor
		}

		scale := tb.Scale
		if scale <= 0 {
			scale = 1.0
		}

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}
		h := 3.0 * lineH
		pipeW, _ := state.MeasureText("|", scale)

		displayLabel := tb.Label
		isPlaceholder := false
		if tb.Text == "" {
			displayLabel = tb.Label
			isPlaceholder = true
		} else {
			displayLabel = tb.Text
		}

		if tb.Focused {
			displayLabel += "_"
		}

		tw, _ := state.MeasureText(displayLabel, scale)
		w := tb.Width * pixelRatio
		if w == 0 {
			w = tw + uiButtonPaddingX*2.0*pixelRatio*scale
		}

		posX, posY := resolveUiPosition(tb.Anchor, tb.Position, w/pixelRatio, h/pixelRatio, input.WindowWidth, input.WindowHeight)
		drawX := posX * pixelRatio
		drawY := posY * pixelRatio

		y := drawY
		// 1. Top Border
		drawBoxHLine(drawX, y, w, scale, color)
		y += lineH

		// 2. Middle (Text and Vertical Bars)
		state.DrawText("|", drawX, y, scale, color)

		drawColor := textColor
		if isPlaceholder && !tb.Focused {
			drawColor = dimColor
		}

		state.DrawText(displayLabel, drawX+uiButtonPaddingX*pixelRatio*scale, y, scale, drawColor)
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

		h := float32(len(table.Rows)+3) * lineH // Headers + separator + footer

		posX, posY := resolveUiPosition(table.Anchor, table.Position, totalW/pixelRatio, h/pixelRatio, input.WindowWidth, input.WindowHeight)
		drawX := posX * pixelRatio
		drawY := posY * pixelRatio

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

		lineH := state.GetLineHeight(scale)
		if lineH < 35*scale {
			lineH = 35 * scale
		}

		// Estimate list size for anchoring
		estW := float32(200) * scale * pixelRatio // Fallback width
		estH := float32(len(list.Items)) * lineH
		if list.Title != "" {
			estH += lineH * 1.2
		}

		posX, posY := resolveUiPosition(list.Anchor, list.Position, estW/pixelRatio, estH/pixelRatio, input.WindowWidth, input.WindowHeight)
		drawX := posX * pixelRatio
		drawY := posY * pixelRatio

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
