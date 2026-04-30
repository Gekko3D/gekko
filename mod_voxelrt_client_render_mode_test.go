package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
)

func TestRenderModeStringCoversModes(t *testing.T) {
	if got := RenderModeIndirect.String(); got != "Indirect" {
		t.Fatalf("expected Indirect label, got %q", got)
	}
	if got := RenderModeLightDensity.String(); got != "Light Density" {
		t.Fatalf("expected Light Density label, got %q", got)
	}
	if got := RenderModeCount; got != 7 {
		t.Fatalf("expected 7 render modes after removing probe GI modes, got %d", got)
	}
}

func TestCycleRenderModeWraps(t *testing.T) {
	state := &VoxelRtState{RtApp: &app_rt.App{RenderMode: uint32(RenderModeIndirect)}}

	state.CycleRenderMode()
	if got := RenderMode(state.RtApp.RenderMode); got != RenderModeLightDensity {
		t.Fatalf("expected indirect -> light density, got %v", got)
	}

	state.CycleRenderMode()
	if got := RenderMode(state.RtApp.RenderMode); got != RenderModeLit {
		t.Fatalf("expected light density -> lit wrap, got %v", got)
	}
}

func TestParseVoxelRtDepthModeAcceptsKnownModes(t *testing.T) {
	got, err := ParseVoxelRtDepthMode("reverse-z")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if got != VoxelRtDepthModeReverseZ {
		t.Fatalf("expected reverse-z depth mode, got %q", got)
	}
}

func TestParseVoxelRtDepthModeRejectsUnknownModes(t *testing.T) {
	if _, err := ParseVoxelRtDepthMode("log-z"); err == nil {
		t.Fatal("expected invalid depth mode to fail")
	}
}
