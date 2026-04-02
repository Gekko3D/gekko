package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
)

func TestVoxelRtStateShadowSoftnessAccessors(t *testing.T) {
	state := &VoxelRtState{
		RtApp: app_rt.NewApp(nil),
	}

	if got := state.DirectionalShadowSoftness(); got != 0.65 {
		t.Fatalf("expected default directional shadow softness 0.65, got %v", got)
	}
	if got := state.SpotShadowSoftness(); got != 0.40 {
		t.Fatalf("expected default spot shadow softness 0.40, got %v", got)
	}

	state.SetDirectionalShadowSoftness(0.2)
	state.SetSpotShadowSoftness(0.9)

	if got := state.DirectionalShadowSoftness(); got != 0.2 {
		t.Fatalf("expected directional shadow softness 0.2, got %v", got)
	}
	if got := state.SpotShadowSoftness(); got != 0.9 {
		t.Fatalf("expected spot shadow softness 0.9, got %v", got)
	}
}
