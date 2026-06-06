package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestDebrisMidfieldGPUHostsMapRendererInput(t *testing.T) {
	inputs := []DebrisMidfieldInput{
		{
			BandID:               "ring-a",
			CellID:               "ring-a-cell",
			AsteroidID:           "asteroid-a",
			RadialIndex:          1,
			AngularIndex:         2,
			VerticalIndex:        3,
			PositionViewSpace:    mgl32.Vec3{4, 5, 6},
			PlaneNormalViewSpace: mgl32.Vec3{0, 1, 0},
			InnerRadiusMeters:    10,
			OuterRadiusMeters:    20,
			Seed:                 21,
			Tint:                 [3]float32{0.1, 0.2, 0.3},
			Opacity:              0.4,
			DensityScale:         0.5,
			ApproachFade:         0.6,
			DistanceMeters:       30,
			GapInnerRadius:       11,
			GapOuterRadius:       12,
			LightDirViewSpace:    mgl32.Vec3{1, 0, 0},
			ActiveHandoff:        true,
			HandoffExact:         true,
			HandoffRadiusMeters:  13,
		},
	}

	hosts := debrisMidfieldGPUHosts(inputs)

	if len(hosts) != 1 {
		t.Fatalf("expected one debris-midfield host, got %d", len(hosts))
	}
	if hosts[0] != (gpu.DebrisMidfieldHost(inputs[0])) {
		t.Fatalf("unexpected debris-midfield host: %+v", hosts[0])
	}
	if len(debrisMidfieldGPUHosts(nil)) != 0 {
		t.Fatal("expected empty debris-midfield host output")
	}
}

func TestClearDebrisMidfieldInputClearsContributionCount(t *testing.T) {
	app := &App{
		BufferManager: &gpu.GpuBufferManager{DebrisMidfieldCount: 2},
	}

	app.ClearDebrisMidfieldInput()

	if got := app.BufferManager.DebrisMidfieldCount; got != 0 {
		t.Fatalf("expected debris-midfield count cleared, got %d", got)
	}
}
