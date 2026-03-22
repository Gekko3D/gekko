package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestSyncVoxelRtLightsUsesDaylightDirectionalLightAsSun(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	applyLevelEnvironment(cmd, &content.LevelEnvironmentDef{Preset: "daylight"})
	app.FlushCommands()

	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: app_rt.NewProfiler(),
		},
	}

	syncVoxelRtLights(state, cmd)

	if len(state.RtApp.Scene.Lights) != 1 {
		t.Fatalf("expected one non-ambient GPU light, got %d", len(state.RtApp.Scene.Lights))
	}
	if state.SunIntensity <= 0 {
		t.Fatalf("expected positive sun intensity, got %f", state.SunIntensity)
	}
	if state.SunDirection.Len() <= 0 {
		t.Fatalf("expected non-zero sun direction, got %v", state.SunDirection)
	}
	if state.RtApp.Scene.AmbientLight.Len() <= 0 {
		t.Fatalf("expected non-zero ambient light, got %v", state.RtApp.Scene.AmbientLight)
	}
}
