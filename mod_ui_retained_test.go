package gekko

import "testing"

func TestParseUiFloat(t *testing.T) {
	value, ok := parseUiFloat("12.5")
	if !ok || value != 12.5 {
		t.Fatalf("expected valid float parse, got %v %v", value, ok)
	}

	if _, ok := parseUiFloat("-"); ok {
		t.Fatalf("expected incomplete number to be rejected")
	}

	if _, ok := parseUiFloat("abc"); ok {
		t.Fatalf("expected invalid float to be rejected")
	}
}

func TestSyncUiFieldStatePreservesFocusedDraft(t *testing.T) {
	state := &uiWidgetState{
		Focused:        true,
		Draft:          "draft-value",
		LastControlled: "server-value",
	}

	syncUiFieldState(state, "new-server-value")

	if state.Draft != "draft-value" {
		t.Fatalf("expected focused draft to be preserved, got %q", state.Draft)
	}
	if state.LastControlled != "server-value" {
		t.Fatalf("expected focused state to keep last controlled value, got %q", state.LastControlled)
	}

	state.Focused = false
	syncUiFieldState(state, "new-server-value")
	if state.Draft != "new-server-value" {
		t.Fatalf("expected blurred field to resync from controlled value, got %q", state.Draft)
	}
}

func TestUiRuntimeFocusTransitionBlursPreviousField(t *testing.T) {
	runtime := newUiRuntime()
	first := runtime.touch("panel:first")
	second := runtime.touch("panel:second")

	first.Draft = "editing"
	first.LastControlled = "server"
	first.Focused = true
	runtime.focused = "panel:first"

	runtime.focus("panel:second")

	if first.Focused {
		t.Fatalf("expected previous field to lose focus")
	}
	if first.Draft != "server" {
		t.Fatalf("expected previous field draft to reset on blur, got %q", first.Draft)
	}
	if !second.Focused || runtime.focused != "panel:second" {
		t.Fatalf("expected second field to become focused")
	}
}

func TestResolveUiPositionAnchors(t *testing.T) {
	x, y := resolveUiPosition(UiAnchorTopRight, [2]float32{20, 30}, 100, 50, 1280, 720)
	if x != 1160 || y != 30 {
		t.Fatalf("unexpected top-right position: %v %v", x, y)
	}

	x, y = resolveUiPosition(UiAnchorBottomRight, [2]float32{20, 30}, 100, 50, 1280, 720)
	if x != 1160 || y != 640 {
		t.Fatalf("unexpected bottom-right position: %v %v", x, y)
	}
}

func TestClampUiScroll(t *testing.T) {
	if got := clampUiScroll(-10, 50); got != 0 {
		t.Fatalf("expected scroll to clamp at 0, got %v", got)
	}
	if got := clampUiScroll(80, 50); got != 50 {
		t.Fatalf("expected scroll to clamp at max, got %v", got)
	}
	if got := clampUiScroll(20, 50); got != 20 {
		t.Fatalf("expected in-range scroll to stay unchanged, got %v", got)
	}
}
