package gekko

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	uiPanelPadding   = float32(16)
	uiPanelSpacing   = float32(10)
	uiPanelLabelGap  = float32(12)
	uiFieldPaddingX  = float32(16)
	uiFieldPaddingY  = float32(10)
	uiPanelMinHeight = float32(12)
)

type UiRuntime struct {
	widgets map[string]*uiWidgetState
	seen    map[string]bool
	focused string
}

type uiWidgetState struct {
	Hovered        bool
	Focused        bool
	Draft          string
	Dirty          bool
	LastControlled string
	ScrollY        float32
}

func newUiRuntime() *UiRuntime {
	return &UiRuntime{
		widgets: make(map[string]*uiWidgetState),
		seen:    make(map[string]bool),
	}
}

func (rt *UiRuntime) beginFrame() {
	clear(rt.seen)
}

func (rt *UiRuntime) endFrame() {
	for key, state := range rt.widgets {
		if !rt.seen[key] {
			if rt.focused == key {
				rt.focused = ""
			}
			delete(rt.widgets, key)
			continue
		}
		state.Hovered = false
	}
}

func (rt *UiRuntime) touch(key string) *uiWidgetState {
	rt.seen[key] = true
	state, ok := rt.widgets[key]
	if !ok {
		state = &uiWidgetState{}
		rt.widgets[key] = state
	}
	return state
}

func (rt *UiRuntime) focus(key string) {
	if rt.focused != "" && rt.focused != key {
		if prev, ok := rt.widgets[rt.focused]; ok {
			prev.Focused = false
			prev.Dirty = false
			prev.Draft = prev.LastControlled
		}
	}
	rt.focused = key
	state := rt.touch(key)
	state.Focused = true
}

func (rt *UiRuntime) blurFocused() {
	if rt.focused == "" {
		return
	}
	if state, ok := rt.widgets[rt.focused]; ok {
		state.Focused = false
		state.Dirty = false
		state.Draft = state.LastControlled
	}
	rt.focused = ""
}

type UiNode interface {
	isUiNode()
}

type UiPanel struct {
	Key       string
	Anchor    UiAnchor
	Position  [2]float32
	Width     float32
	MaxHeight float32
	Padding   float32
	Spacing   float32
	Scale     float32
	Title     string
	Visible   bool
	Children  []UiNode
}

func (UiPanel) isUiNode() {}

type UiColumn struct {
	Key      string
	Spacing  float32
	Children []UiNode
}

func (UiColumn) isUiNode() {}

type UiRow struct {
	Key        string
	Spacing    float32
	LabelWidth float32
	Children   []UiNode
}

func (UiRow) isUiNode() {}

type UiSpacer struct {
	Height float32
}

func (UiSpacer) isUiNode() {}

type UiLabel struct {
	Key   string
	Text  string
	Width float32
	Scale float32
	Dim   bool
}

func (UiLabel) isUiNode() {}

type UiTextAlign int

const (
	UiTextAlignDefault UiTextAlign = iota
	UiTextAlignLeft
	UiTextAlignCenter
	UiTextAlignRight
)

type UiButtonControl struct {
	Key     string
	Label   string
	Width   float32
	Scale   float32
	Align   UiTextAlign
	OnClick func()
}

func (UiButtonControl) isUiNode() {}

type UiTextField struct {
	Key         string
	Value       string
	Placeholder string
	Width       float32
	Scale       float32
	OnChange    func(string)
	OnCommit    func(string)
}

func (UiTextField) isUiNode() {}

type UiNumberField struct {
	Key         string
	Value       float32
	Precision   int
	Placeholder string
	Width       float32
	Scale       float32
	OnChange    func(float32)
	OnCommit    func(float32)
}

func (UiNumberField) isUiNode() {}

type UiSelectCycle struct {
	Key      string
	Options  []string
	Selected int
	Width    float32
	Scale    float32
	Align    UiTextAlign
	OnChange func(int)
}

func (UiSelectCycle) isUiNode() {}

type uiNodeKind int

const (
	uiNodePanel uiNodeKind = iota
	uiNodeColumn
	uiNodeRow
	uiNodeSpacer
	uiNodeLabel
	uiNodeButton
	uiNodeTextField
	uiNodeNumberField
	uiNodeSelectCycle
)

type uiLayoutNode struct {
	kind          uiNodeKind
	key           string
	x             float32
	y             float32
	w             float32
	h             float32
	node          UiNode
	children      []*uiLayoutNode
	contentTop    float32
	contentBottom float32
	contentHeight float32
	scrollY       float32
	scrollMax     float32
}

type uiLayoutContext struct {
	state      *VoxelRtState
	input      *Input
	pixelRatio float32
}

func uiPanelInputSystem(state *VoxelRtState, input *Input, runtime *UiRuntime, cmd *Commands) {
	if state == nil || input == nil || runtime == nil || input.WindowWidth == 0 {
		return
	}

	runtime.beginFrame()

	ctx := makeUiLayoutContext(state, input)
	clickedField := ""
	clickConsumed := false
	hasFocusedField := false

	MakeQuery1[UiPanel](cmd).Map(func(eid EntityId, panel *UiPanel) bool {
		if panel == nil || !panelVisible(panel) {
			return true
		}

		panelState := runtime.touch(uiWidgetID(eid, uiPanelRuntimeKey(panel)))
		layout := uiBuildPanelLayout(ctx, panel, panelState.ScrollY)
		panelState.ScrollY = layout.scrollY
		uiHandleInput(layout, layout, eid, ctx, input, runtime, &clickedField, &clickConsumed, &hasFocusedField)
		return true
	})

	if input.JustPressed[MouseButtonLeft] && clickedField == "" && !clickConsumed {
		runtime.blurFocused()
	}

	if !hasFocusedField && runtime.focused != "" {
		runtime.blurFocused()
	}
}

func uiPanelRenderSystem(state *VoxelRtState, input *Input, runtime *UiRuntime, cmd *Commands) {
	if state == nil || input == nil || runtime == nil || input.WindowWidth == 0 {
		return
	}

	ctx := makeUiLayoutContext(state, input)
	MakeQuery1[UiPanel](cmd).Map(func(eid EntityId, panel *UiPanel) bool {
		if panel == nil || !panelVisible(panel) {
			return true
		}

		panelState := runtime.touch(uiWidgetID(eid, uiPanelRuntimeKey(panel)))
		layout := uiBuildPanelLayout(ctx, panel, panelState.ScrollY)
		panelState.ScrollY = layout.scrollY
		uiRenderLayout(layout, layout, eid, ctx, runtime)
		return true
	})

	runtime.endFrame()
}

func makeUiLayoutContext(state *VoxelRtState, input *Input) uiLayoutContext {
	pixelRatio := float32(state.RtApp.Config.Width) / float32(input.WindowWidth)
	if pixelRatio <= 0 {
		pixelRatio = 1.0
	}
	return uiLayoutContext{
		state:      state,
		input:      input,
		pixelRatio: pixelRatio,
	}
}

func panelVisible(panel *UiPanel) bool {
	return panel != nil
}

func uiBuildPanelLayout(ctx uiLayoutContext, panel *UiPanel, scrollY float32) *uiLayoutNode {
	scale := uiNodeScale(panel.Scale)
	padding := panel.Padding
	if padding <= 0 {
		padding = uiPanelPadding
	}
	spacing := panel.Spacing
	if spacing <= 0 {
		spacing = uiPanelSpacing
	}

	contentWidth := panel.Width
	if contentWidth <= 0 {
		contentWidth = 280
	}

	var titleH float32
	if panel.Title != "" {
		titleH = uiTextHeight(ctx, scale)
	}

	contentX := padding
	contentY := padding
	if titleH > 0 {
		contentY += titleH + spacing
	}

	var contentChildren []*uiLayoutNode
	currY := contentY
	maxChildW := float32(0)
	for idx, child := range panel.Children {
		childLayout := uiLayoutNodeFor(child, fmt.Sprintf("panel/%d", idx), contentX, currY, contentWidth-padding*2, ctx)
		if childLayout == nil {
			continue
		}
		contentChildren = append(contentChildren, childLayout)
		if childLayout.w > maxChildW {
			maxChildW = childLayout.w
		}
		currY += childLayout.h + spacing
	}
	if len(contentChildren) > 0 {
		currY -= spacing
	}

	panelW := contentWidth
	if maxChildW+padding*2 > panelW {
		panelW = maxChildW + padding*2
	}
	panelH := currY + padding
	contentHeight := currY - contentY
	if contentHeight < 0 {
		contentHeight = 0
	}
	maxHeight := panel.MaxHeight
	if maxHeight > 0 && panelH > maxHeight {
		panelH = maxHeight
	}
	if panelH < uiPanelMinHeight {
		panelH = uiPanelMinHeight
	}

	posX, posY := resolveUiPosition(panel.Anchor, panel.Position, panelW, panelH, ctx.input.WindowWidth, ctx.input.WindowHeight)
	root := &uiLayoutNode{
		kind: uiNodePanel,
		x:    posX,
		y:    posY,
		w:    panelW,
		h:    panelH,
		node: panel,
	}
	root.contentTop = posY + contentY
	root.contentBottom = posY + panelH - padding
	root.contentHeight = contentHeight
	viewportH := root.contentBottom - root.contentTop
	if viewportH < 0 {
		viewportH = 0
	}
	root.scrollMax = contentHeight - viewportH
	if root.scrollMax < 0 {
		root.scrollMax = 0
	}
	root.scrollY = clampUiScroll(scrollY, root.scrollMax)

	for _, child := range contentChildren {
		uiShiftLayout(child, posX, posY)
		uiShiftLayout(child, 0, -root.scrollY)
		root.children = append(root.children, child)
	}

	return root
}

func uiLayoutNodeFor(node UiNode, path string, x, y, width float32, ctx uiLayoutContext) *uiLayoutNode {
	switch typed := node.(type) {
	case UiColumn:
		return uiLayoutColumn(typed, path, x, y, width, ctx)
	case *UiColumn:
		return uiLayoutColumn(*typed, path, x, y, width, ctx)
	case UiRow:
		return uiLayoutRow(typed, path, x, y, width, ctx)
	case *UiRow:
		return uiLayoutRow(*typed, path, x, y, width, ctx)
	case UiSpacer:
		return &uiLayoutNode{kind: uiNodeSpacer, x: x, y: y, w: 0, h: typed.Height, node: typed}
	case *UiSpacer:
		return &uiLayoutNode{kind: uiNodeSpacer, x: x, y: y, w: 0, h: typed.Height, node: *typed}
	case UiLabel:
		return uiLayoutLabel(typed, path, x, y, ctx)
	case *UiLabel:
		return uiLayoutLabel(*typed, path, x, y, ctx)
	case UiButtonControl:
		return uiLayoutButton(typed, path, x, y, ctx)
	case *UiButtonControl:
		return uiLayoutButton(*typed, path, x, y, ctx)
	case UiTextField:
		return uiLayoutTextField(typed, path, x, y, ctx)
	case *UiTextField:
		return uiLayoutTextField(*typed, path, x, y, ctx)
	case UiNumberField:
		return uiLayoutNumberField(typed, path, x, y, ctx)
	case *UiNumberField:
		return uiLayoutNumberField(*typed, path, x, y, ctx)
	case UiSelectCycle:
		return uiLayoutSelectCycle(typed, path, x, y, ctx)
	case *UiSelectCycle:
		return uiLayoutSelectCycle(*typed, path, x, y, ctx)
	default:
		return nil
	}
}

func uiLayoutColumn(column UiColumn, path string, x, y, width float32, ctx uiLayoutContext) *uiLayoutNode {
	spacing := column.Spacing
	if spacing <= 0 {
		spacing = uiPanelSpacing
	}

	layout := &uiLayoutNode{
		kind: uiNodeColumn,
		key:  uiStableKey("column", column.Key, path),
		x:    x,
		y:    y,
		node: column,
	}

	currY := y
	maxW := float32(0)
	for idx, child := range column.Children {
		childLayout := uiLayoutNodeFor(child, fmt.Sprintf("%s/%d", path, idx), x, currY, width, ctx)
		if childLayout == nil {
			continue
		}
		layout.children = append(layout.children, childLayout)
		if childLayout.w > maxW {
			maxW = childLayout.w
		}
		currY += childLayout.h + spacing
	}
	if len(layout.children) > 0 {
		currY -= spacing
	}
	layout.w = maxW
	layout.h = currY - y
	if layout.h < 0 {
		layout.h = 0
	}
	return layout
}

func uiLayoutRow(row UiRow, path string, x, y, width float32, ctx uiLayoutContext) *uiLayoutNode {
	spacing := row.Spacing
	if spacing <= 0 {
		spacing = uiPanelLabelGap
	}

	layout := &uiLayoutNode{
		kind: uiNodeRow,
		key:  uiStableKey("row", row.Key, path),
		x:    x,
		y:    y,
		node: row,
	}

	var labelWidth float32
	if row.LabelWidth > 0 {
		labelWidth = row.LabelWidth
	}

	currX := x
	maxH := float32(0)
	totalW := float32(0)
	for idx, child := range row.Children {
		childLayout := uiLayoutNodeFor(child, fmt.Sprintf("%s/%d", path, idx), currX, y, width, ctx)
		if childLayout == nil {
			continue
		}
		if idx == 0 && labelWidth > 0 {
			childLayout.w = labelWidth
		}
		layout.children = append(layout.children, childLayout)
		currX += childLayout.w + spacing
		totalW += childLayout.w
		if childLayout.h > maxH {
			maxH = childLayout.h
		}
	}
	if len(layout.children) > 1 {
		totalW += spacing * float32(len(layout.children)-1)
	}
	layout.w = totalW
	layout.h = maxH
	for _, child := range layout.children {
		child.y = y + (maxH-child.h)/2
	}
	return layout
}

func uiLayoutLabel(label UiLabel, path string, x, y float32, ctx uiLayoutContext) *uiLayoutNode {
	scale := uiNodeScale(label.Scale)
	tw, _ := uiMeasureText(ctx, label.Text, scale)
	w := tw
	if label.Width > 0 {
		w = label.Width
	}
	return &uiLayoutNode{
		kind: uiNodeLabel,
		key:  uiStableKey("label", label.Key, path),
		x:    x,
		y:    y,
		w:    w,
		h:    uiTextHeight(ctx, scale),
		node: label,
	}
}

func uiLayoutButton(button UiButtonControl, path string, x, y float32, ctx uiLayoutContext) *uiLayoutNode {
	scale := uiNodeScale(button.Scale)
	w, h := uiBoxSize(ctx, button.Width, button.Label, scale)
	return &uiLayoutNode{
		kind: uiNodeButton,
		key:  uiStableKey("button", button.Key, path),
		x:    x,
		y:    y,
		w:    w,
		h:    h,
		node: button,
	}
}

func uiLayoutTextField(field UiTextField, path string, x, y float32, ctx uiLayoutContext) *uiLayoutNode {
	scale := uiNodeScale(field.Scale)
	display := field.Value
	if display == "" {
		display = field.Placeholder
	}
	w, h := uiBoxSize(ctx, field.Width, display, scale)
	return &uiLayoutNode{
		kind: uiNodeTextField,
		key:  uiStableKey("textfield", field.Key, path),
		x:    x,
		y:    y,
		w:    w,
		h:    h,
		node: field,
	}
}

func uiLayoutNumberField(field UiNumberField, path string, x, y float32, ctx uiLayoutContext) *uiLayoutNode {
	scale := uiNodeScale(field.Scale)
	display := formatUiNumber(field.Value, field.Precision)
	if display == "" {
		display = field.Placeholder
	}
	w, h := uiBoxSize(ctx, field.Width, display, scale)
	return &uiLayoutNode{
		kind: uiNodeNumberField,
		key:  uiStableKey("numberfield", field.Key, path),
		x:    x,
		y:    y,
		w:    w,
		h:    h,
		node: field,
	}
}

func uiLayoutSelectCycle(field UiSelectCycle, path string, x, y float32, ctx uiLayoutContext) *uiLayoutNode {
	scale := uiNodeScale(field.Scale)
	label := currentUiSelectLabel(field)
	w, h := uiBoxSize(ctx, field.Width, label, scale)
	return &uiLayoutNode{
		kind: uiNodeSelectCycle,
		key:  uiStableKey("select", field.Key, path),
		x:    x,
		y:    y,
		w:    w,
		h:    h,
		node: field,
	}
}

func uiHandleInput(root *uiLayoutNode, layout *uiLayoutNode, eid EntityId, ctx uiLayoutContext, input *Input, runtime *UiRuntime, clickedField *string, clickConsumed *bool, hasFocusedField *bool) {
	if layout == nil {
		return
	}

	switch layout.kind {
	case uiNodePanel:
		if uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.y, layout.w, layout.h) {
			input.GuiCaptured = true
			panelState := runtime.touch(uiWidgetID(eid, uiPanelRuntimeKey(layout.node.(*UiPanel))))
			panelState.Hovered = true
			if layout.scrollMax > 0 && input.MouseScrollY != 0 &&
				uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.contentTop, layout.w, layout.contentBottom-layout.contentTop) {
				panelState.ScrollY = clampUiScroll(panelState.ScrollY-float32(input.MouseScrollY)*36, layout.scrollMax)
				layout.scrollY = panelState.ScrollY
			}
			if input.JustPressed[MouseButtonLeft] {
				*clickConsumed = true
			}
		}
	case uiNodeButton:
		if !uiLayoutVisible(layout, root) {
			return
		}
		button := layout.node.(UiButtonControl)
		state := runtime.touch(uiWidgetID(eid, layout.key))
		state.Hovered = uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.y, layout.w, layout.h)
		if state.Hovered {
			input.GuiCaptured = true
			if input.JustPressed[MouseButtonLeft] {
				*clickConsumed = true
				if button.OnClick != nil {
					button.OnClick()
				}
			}
		}
	case uiNodeSelectCycle:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiSelectCycle)
		state := runtime.touch(uiWidgetID(eid, layout.key))
		state.Hovered = uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.y, layout.w, layout.h)
		if state.Hovered {
			input.GuiCaptured = true
			if input.JustPressed[MouseButtonLeft] {
				*clickConsumed = true
				if len(field.Options) > 0 && field.OnChange != nil {
					next := field.Selected + 1
					if next >= len(field.Options) {
						next = 0
					}
					field.OnChange(next)
				}
			}
		}
	case uiNodeTextField:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiTextField)
		uiHandleTextFieldInput(layout, eid, field, input, runtime, clickedField, clickConsumed, hasFocusedField)
	case uiNodeNumberField:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiNumberField)
		uiHandleNumberFieldInput(layout, eid, field, input, runtime, clickedField, clickConsumed, hasFocusedField)
	}

	for _, child := range layout.children {
		if child.kind != uiNodePanel && !uiLayoutVisible(child, root) {
			continue
		}
		uiHandleInput(root, child, eid, ctx, input, runtime, clickedField, clickConsumed, hasFocusedField)
	}
}

func uiHandleTextFieldInput(layout *uiLayoutNode, eid EntityId, field UiTextField, input *Input, runtime *UiRuntime, clickedField *string, clickConsumed *bool, hasFocusedField *bool) {
	id := uiWidgetID(eid, layout.key)
	state := runtime.touch(id)
	syncUiFieldState(state, field.Value)

	hovered := uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.y, layout.w, layout.h)
	state.Hovered = hovered
	if hovered {
		input.GuiCaptured = true
		if input.JustPressed[MouseButtonLeft] {
			*clickedField = id
			*clickConsumed = true
			runtime.focus(id)
		}
	}

	if !state.Focused {
		return
	}
	*hasFocusedField = true

	for _, char := range input.CharBuffer {
		state.Draft += string(char)
		state.Dirty = true
		if field.OnChange != nil {
			field.OnChange(state.Draft)
		}
	}
	if input.JustPressed[KeyBackspace] && len(state.Draft) > 0 {
		runes := []rune(state.Draft)
		state.Draft = string(runes[:len(runes)-1])
		state.Dirty = true
		if field.OnChange != nil {
			field.OnChange(state.Draft)
		}
	}
	if input.JustPressed[KeyEnter] {
		if field.OnCommit != nil {
			field.OnCommit(state.Draft)
		}
		state.LastControlled = state.Draft
		state.Dirty = false
		state.Focused = false
		runtime.focused = ""
	}
}

func uiHandleNumberFieldInput(layout *uiLayoutNode, eid EntityId, field UiNumberField, input *Input, runtime *UiRuntime, clickedField *string, clickConsumed *bool, hasFocusedField *bool) {
	id := uiWidgetID(eid, layout.key)
	state := runtime.touch(id)
	controlled := formatUiNumber(field.Value, field.Precision)
	syncUiFieldState(state, controlled)

	hovered := uiPointInRect(float32(input.MouseX), float32(input.MouseY), layout.x, layout.y, layout.w, layout.h)
	state.Hovered = hovered
	if hovered {
		input.GuiCaptured = true
		if input.JustPressed[MouseButtonLeft] {
			*clickedField = id
			*clickConsumed = true
			runtime.focus(id)
		}
	}

	if !state.Focused {
		return
	}
	*hasFocusedField = true

	for _, char := range input.CharBuffer {
		if strings.ContainsRune("0123456789.-", char) {
			state.Draft += string(char)
			state.Dirty = true
			if field.OnChange != nil {
				if parsed, ok := parseUiFloat(state.Draft); ok {
					field.OnChange(parsed)
				}
			}
		}
	}
	if input.JustPressed[KeyBackspace] && len(state.Draft) > 0 {
		runes := []rune(state.Draft)
		state.Draft = string(runes[:len(runes)-1])
		state.Dirty = true
		if field.OnChange != nil {
			if parsed, ok := parseUiFloat(state.Draft); ok {
				field.OnChange(parsed)
			}
		}
	}
	if input.JustPressed[KeyEnter] {
		if parsed, ok := parseUiFloat(state.Draft); ok {
			if field.OnCommit != nil {
				field.OnCommit(parsed)
			}
			state.LastControlled = formatUiNumber(parsed, field.Precision)
		} else {
			state.Draft = controlled
			state.LastControlled = controlled
		}
		state.Dirty = false
		state.Focused = false
		runtime.focused = ""
	}
}

func syncUiFieldState(state *uiWidgetState, controlled string) {
	if state.LastControlled == "" && state.Draft == "" {
		state.Draft = controlled
		state.LastControlled = controlled
		return
	}
	if !state.Focused && state.LastControlled != controlled {
		state.Draft = controlled
		state.LastControlled = controlled
		state.Dirty = false
	}
}

func uiRenderLayout(root *uiLayoutNode, layout *uiLayoutNode, eid EntityId, ctx uiLayoutContext, runtime *UiRuntime) {
	if layout == nil {
		return
	}

	switch layout.kind {
	case uiNodePanel:
		panel := layout.node.(*UiPanel)
		uiRenderPanel(layout, ctx, panel)
	case uiNodeLabel:
		if !uiLayoutVisible(layout, root) {
			return
		}
		uiRenderLabel(layout, ctx)
	case uiNodeButton:
		if !uiLayoutVisible(layout, root) {
			return
		}
		button := layout.node.(UiButtonControl)
		uiRenderButton(layout, ctx, button, runtime.touch(uiWidgetID(eid, layout.key)))
	case uiNodeTextField:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiTextField)
		uiRenderTextField(layout, ctx, field, runtime.touch(uiWidgetID(eid, layout.key)))
	case uiNodeNumberField:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiNumberField)
		uiRenderNumberField(layout, ctx, field, runtime.touch(uiWidgetID(eid, layout.key)))
	case uiNodeSelectCycle:
		if !uiLayoutVisible(layout, root) {
			return
		}
		field := layout.node.(UiSelectCycle)
		uiRenderSelectCycle(layout, ctx, field, runtime.touch(uiWidgetID(eid, layout.key)))
	}

	for _, child := range layout.children {
		if child.kind != uiNodePanel && !uiLayoutVisible(child, root) {
			continue
		}
		uiRenderLayout(root, child, eid, ctx, runtime)
	}
}

func uiRenderPanel(layout *uiLayoutNode, ctx uiLayoutContext, panel *UiPanel) {
	scale := uiNodeScale(panel.Scale)
	color := [4]float32{0.9, 0.9, 0.9, 1}
	uiDrawBox(ctx, layout.x, layout.y, layout.w, layout.h, color, scale)
	if panel.Title != "" {
		uiDrawText(ctx, panel.Title, layout.x+uiPanelPadding, layout.y+uiPanelPadding, scale, [4]float32{1, 1, 0, 1})
		uiDrawText(ctx, strings.Repeat("-", intMax(8, len(panel.Title)+2)), layout.x+uiPanelPadding, layout.y+uiPanelPadding+uiTextHeight(ctx, scale)*0.9, 0.45, [4]float32{0.65, 0.65, 0.65, 1})
	}
	if layout.scrollMax > 0 {
		uiDrawScrollbar(ctx, layout)
	}
}

func uiRenderLabel(layout *uiLayoutNode, ctx uiLayoutContext) {
	label := layout.node.(UiLabel)
	scale := uiNodeScale(label.Scale)
	color := [4]float32{1, 1, 1, 1}
	if label.Dim {
		color = [4]float32{0.65, 0.65, 0.65, 1}
	}
	uiDrawText(ctx, label.Text, layout.x, layout.y, scale, color)
}

func uiRenderButton(layout *uiLayoutNode, ctx uiLayoutContext, button UiButtonControl, state *uiWidgetState) {
	scale := uiNodeScale(button.Scale)
	color := [4]float32{1, 1, 1, 1}
	if state.Hovered {
		color = [4]float32{1, 1, 0, 1}
	}
	uiDrawButtonBox(ctx, layout.x, layout.y, layout.w, layout.h, scale, color, button.Label, false, false, resolveUiTextAlign(button.Align, UiTextAlignCenter))
}

func uiRenderTextField(layout *uiLayoutNode, ctx uiLayoutContext, field UiTextField, state *uiWidgetState) {
	scale := uiNodeScale(field.Scale)
	display := state.Draft
	isPlaceholder := false
	if display == "" {
		display = field.Placeholder
		isPlaceholder = true
	}
	if state.Focused {
		display += "_"
	}
	uiDrawFieldBox(ctx, layout.x, layout.y, layout.w, layout.h, scale, uiFieldColor(state), display, state.Focused, isPlaceholder)
}

func uiRenderNumberField(layout *uiLayoutNode, ctx uiLayoutContext, field UiNumberField, state *uiWidgetState) {
	scale := uiNodeScale(field.Scale)
	display := state.Draft
	isPlaceholder := false
	if display == "" {
		display = field.Placeholder
		isPlaceholder = true
	}
	if state.Focused {
		display += "_"
	}
	uiDrawFieldBox(ctx, layout.x, layout.y, layout.w, layout.h, scale, uiFieldColor(state), display, state.Focused, isPlaceholder)
}

func uiRenderSelectCycle(layout *uiLayoutNode, ctx uiLayoutContext, field UiSelectCycle, state *uiWidgetState) {
	scale := uiNodeScale(field.Scale)
	color := [4]float32{1, 1, 1, 1}
	if state.Hovered {
		color = [4]float32{1, 1, 0, 1}
	}
	uiDrawButtonBox(ctx, layout.x, layout.y, layout.w, layout.h, scale, color, currentUiSelectLabel(field), false, false, resolveUiTextAlign(field.Align, UiTextAlignCenter))
}

func uiFieldColor(state *uiWidgetState) [4]float32 {
	if state.Focused {
		return [4]float32{1, 1, 0, 1}
	}
	if state.Hovered {
		return [4]float32{1, 1, 0.5, 1}
	}
	return [4]float32{1, 1, 1, 1}
}

func uiDrawButtonBox(ctx uiLayoutContext, x, y, w, h, scale float32, color [4]float32, label string, focused bool, placeholder bool, align UiTextAlign) {
	uiDrawBox(ctx, x, y, w, h, color, scale)

	lineH := ctx.state.GetLineHeight(scale)
	if lineH < 35*scale {
		lineH = 35 * scale
	}

	textColor := [4]float32{1, 1, 1, 1}
	if placeholder && !focused {
		textColor = [4]float32{0.55, 0.55, 0.55, 1}
	}

	textX := x + uiFieldPaddingX
	if tw, _ := uiMeasureText(ctx, label, scale); tw > 0 {
		switch align {
		case UiTextAlignCenter:
			textX = x + (w-tw)/2
		case UiTextAlignRight:
			textX = x + w - uiFieldPaddingX - tw
		}
	}
	if textX < x+uiFieldPaddingX {
		textX = x + uiFieldPaddingX
	}
	textY := y + (h-uiTextHeight(ctx, scale))/2
	uiDrawText(ctx, label, textX, textY, scale, textColor)
}

func uiDrawFieldBox(ctx uiLayoutContext, x, y, w, h, scale float32, color [4]float32, label string, focused bool, placeholder bool) {
	uiDrawBox(ctx, x, y, w, h, color, scale)

	textColor := [4]float32{1, 1, 1, 1}
	if placeholder && !focused {
		textColor = [4]float32{0.55, 0.55, 0.55, 1}
	}

	textY := y + h/2 - uiTextHeight(ctx, scale)/2 + uiTextHeight(ctx, scale)*0.08
	uiDrawText(ctx, label, x+uiFieldPaddingX, textY, scale, textColor)
}

func uiDrawBox(ctx uiLayoutContext, x, y, w, h float32, color [4]float32, scale float32) {
	lineH := ctx.state.GetLineHeight(scale)
	if lineH < 35*scale {
		lineH = 35 * scale
	}
	drawX := x * ctx.pixelRatio
	drawY := y * ctx.pixelRatio
	drawW := w * ctx.pixelRatio
	rows := int((h*ctx.pixelRatio)/lineH + 0.5)
	if rows < 3 {
		rows = 3
	}
	pipeW, _ := ctx.state.MeasureText("|", scale)

	uiDrawBoxHLine(ctx, drawX, drawY, drawW, scale, color)
	for row := 1; row < rows-1; row++ {
		rowY := drawY + float32(row)*lineH
		ctx.state.DrawText("|", drawX, rowY, scale, color)
		ctx.state.DrawText("|", drawX+drawW-pipeW, rowY, scale, color)
	}
	uiDrawBoxHLine(ctx, drawX, drawY+float32(rows-1)*lineH, drawW, scale, color)
}

func uiDrawBoxHLine(ctx uiLayoutContext, x, y, w, scale float32, color [4]float32) {
	plusW, _ := ctx.state.MeasureText("+", scale)
	dashW, _ := ctx.state.MeasureText("-", scale)
	if dashW <= 0 {
		dashW = 10 * scale
	}

	ctx.state.DrawText("+", x, y, scale, color)
	interiorW := w - 2.0*plusW
	if interiorW > 0 {
		count := int(interiorW/dashW) + 1
		ctx.state.DrawText(strings.Repeat("-", count), x+plusW, y, scale, color)
	}
	ctx.state.DrawText("+", x+w-plusW, y, scale, color)
}

func uiDrawText(ctx uiLayoutContext, text string, x, y, scale float32, color [4]float32) {
	ctx.state.DrawText(text, x*ctx.pixelRatio, y*ctx.pixelRatio, scale, color)
}

func uiDrawScrollbar(ctx uiLayoutContext, layout *uiLayoutNode) {
	if layout == nil || layout.scrollMax <= 0 {
		return
	}

	trackX := layout.x + layout.w - 14
	trackTop := layout.contentTop
	trackHeight := layout.contentBottom - layout.contentTop
	if trackHeight <= 0 {
		return
	}

	uiDrawText(ctx, "^", trackX, trackTop-6, 0.45, [4]float32{0.6, 0.6, 0.6, 1})
	uiDrawText(ctx, "v", trackX, layout.contentBottom-10, 0.45, [4]float32{0.6, 0.6, 0.6, 1})

	handleRatio := trackHeight / layout.contentHeight
	if handleRatio > 1 {
		handleRatio = 1
	}
	handleHeight := trackHeight * handleRatio
	minHandleHeight := uiTextHeight(ctx, 0.5) * 1.5
	if handleHeight < minHandleHeight {
		handleHeight = minHandleHeight
	}
	if handleHeight > trackHeight {
		handleHeight = trackHeight
	}

	scrollRatio := float32(0)
	if layout.scrollMax > 0 {
		scrollRatio = layout.scrollY / layout.scrollMax
	}
	handleTravel := trackHeight - handleHeight
	handleY := trackTop + handleTravel*scrollRatio
	rows := int(handleHeight / uiTextHeight(ctx, 0.45))
	if rows < 1 {
		rows = 1
	}
	for i := 0; i < rows; i++ {
		uiDrawText(ctx, "#", trackX, handleY+float32(i)*uiTextHeight(ctx, 0.45), 0.45, [4]float32{0.85, 0.85, 0.85, 1})
	}
}

func uiTextHeight(ctx uiLayoutContext, scale float32) float32 {
	lineH := ctx.state.GetLineHeight(scale)
	if lineH < 35*scale {
		lineH = 35 * scale
	}
	return lineH / ctx.pixelRatio
}

func uiMeasureText(ctx uiLayoutContext, text string, scale float32) (float32, float32) {
	w, h := ctx.state.MeasureText(text, scale)
	return w / ctx.pixelRatio, h / ctx.pixelRatio
}

func uiBoxSize(ctx uiLayoutContext, width float32, text string, scale float32) (float32, float32) {
	tw, _ := uiMeasureText(ctx, text, scale)
	if width <= 0 {
		width = tw + uiFieldPaddingX*2.0
	}
	return width, uiTextHeight(ctx, scale) * 3.0
}

func uiNodeScale(scale float32) float32 {
	if scale <= 0 {
		return 1.0
	}
	return scale
}

func uiShiftLayout(layout *uiLayoutNode, dx, dy float32) {
	layout.x += dx
	layout.y += dy
	for _, child := range layout.children {
		uiShiftLayout(child, dx, dy)
	}
}

func uiPointInRect(px, py, x, y, w, h float32) bool {
	return px >= x && px <= x+w && py >= y && py <= y+h
}

func uiWidgetID(eid EntityId, key string) string {
	return fmt.Sprintf("%d:%s", eid, key)
}

func uiPanelRuntimeKey(panel *UiPanel) string {
	if panel == nil {
		return "panel"
	}
	return uiStableKey("panel", panel.Key, panel.Title)
}

func uiStableKey(prefix, explicit, path string) string {
	if explicit != "" {
		return prefix + "/" + explicit
	}
	return prefix + "/" + path
}

func currentUiSelectLabel(field UiSelectCycle) string {
	if len(field.Options) == 0 {
		return ""
	}
	if field.Selected < 0 || field.Selected >= len(field.Options) {
		return field.Options[0]
	}
	return field.Options[field.Selected]
}

func parseUiFloat(text string) (float32, bool) {
	text = strings.TrimSpace(text)
	if text == "" || text == "-" || text == "." || text == "-." {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 32)
	if err != nil {
		return 0, false
	}
	return float32(value), true
}

func formatUiNumber(value float32, precision int) string {
	if precision < 0 {
		precision = 0
	}
	return strconv.FormatFloat(float64(value), 'f', precision, 32)
}

func clampUiScroll(value, max float32) float32 {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}

func resolveUiTextAlign(value UiTextAlign, fallback UiTextAlign) UiTextAlign {
	if value == UiTextAlignDefault {
		return fallback
	}
	return value
}

func uiLayoutVisible(layout *uiLayoutNode, root *uiLayoutNode) bool {
	if layout == nil || root == nil {
		return false
	}
	if root.kind != uiNodePanel {
		return true
	}
	if layout.kind == uiNodePanel {
		return true
	}
	return layout.y >= root.contentTop && layout.y+layout.h <= root.contentBottom
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
