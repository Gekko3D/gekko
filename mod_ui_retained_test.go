package gekko

import (
	"math"
	"testing"
)

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

func TestUiProgressFractionClampsToRange(t *testing.T) {
	if got := uiProgressFraction(-5, 0, 10); got != 0 {
		t.Fatalf("expected below-min progress to clamp to 0, got %v", got)
	}
	if got := uiProgressFraction(15, 0, 10); got != 1 {
		t.Fatalf("expected above-max progress to clamp to 1, got %v", got)
	}
	if got := uiProgressFraction(5, 0, 10); got != 0.5 {
		t.Fatalf("expected half progress, got %v", got)
	}
}

func TestUiProgressFractionRejectsInvalidRange(t *testing.T) {
	if got := uiProgressFraction(5, 10, 10); got != 0 {
		t.Fatalf("expected zero-width range to render empty, got %v", got)
	}
	if got := uiProgressFraction(5, 10, 0); got != 0 {
		t.Fatalf("expected inverted range to render empty, got %v", got)
	}
	if got := uiProgressFraction(float32(math.NaN()), 0, 10); got != 0 {
		t.Fatalf("expected NaN value to render empty, got %v", got)
	}
	if got := uiProgressFraction(5, 0, float32(math.Inf(1))); got != 0 {
		t.Fatalf("expected infinite range to render empty, got %v", got)
	}
}

func TestUiProgressBarTextDefaults(t *testing.T) {
	got := uiProgressBarText(UiProgressBar{
		Label: "Loading",
		Value: 5,
		Min:   0,
		Max:   10,
	})
	want := "Loading [#########---------] 50%"
	if got != want {
		t.Fatalf("unexpected progress bar text:\nwant %q\n got %q", want, got)
	}
}

func TestUiProgressBarTextCustomValueLabel(t *testing.T) {
	got := uiProgressBarText(UiProgressBar{
		Label:      "Speed",
		Value:      20,
		Min:        0,
		Max:        40,
		ValueLabel: "20 / 40 u/s",
	})
	want := "Speed [#########---------] 20 / 40 u/s"
	if got != want {
		t.Fatalf("unexpected progress bar text:\nwant %q\n got %q", want, got)
	}
}

func TestUiProgressBarKeepsConfiguredValueWidth(t *testing.T) {
	bar := UiProgressBar{ValueLabel: "1 / 40 u/s", ValueWidth: 78}
	if bar.ValueWidth != 78 {
		t.Fatalf("expected progress bar value width to be configurable, got %v", bar.ValueWidth)
	}
}

func TestUiProgressBarTextCustomGlyphsUseFirstRune(t *testing.T) {
	got := uiProgressBarText(UiProgressBar{
		Value: 5,
		Min:   0,
		Max:   10,
		Fill:  "=>",
		Empty: ". ",
	})
	want := "[=========.........] 50%"
	if got != want {
		t.Fatalf("unexpected custom glyph progress bar text:\nwant %q\n got %q", want, got)
	}
}

func TestUiProgressBarTextFullAndEmpty(t *testing.T) {
	full := uiProgressBarText(UiProgressBar{Value: 10, Min: 0, Max: 10})
	if want := "[##################] 100%"; full != want {
		t.Fatalf("unexpected full progress bar:\nwant %q\n got %q", want, full)
	}

	empty := uiProgressBarText(UiProgressBar{Value: 0, Min: 0, Max: 10})
	if want := "[------------------] 0%"; empty != want {
		t.Fatalf("unexpected empty progress bar:\nwant %q\n got %q", want, empty)
	}
}
