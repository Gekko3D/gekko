package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

func TestUpdateTiledLightMetricsClearsDirectionalOnlyState(t *testing.T) {
	app := &App{
		Scene: &core.Scene{
			Lights: []core.Light{
				{Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 0}},
			},
		},
		BufferManager: &gpu.GpuBufferManager{
			TileLightTilesX: 1,
			TileLightTilesY: 1,
		},
	}

	app.updateTiledLightMetrics(mgl32.Ident4(), mgl32.Ident4(), mgl32.Vec3{})

	if app.BufferManager.TileLightAvgCount != 0 {
		t.Fatalf("avg count = %d, want 0 for directional-only scene", app.BufferManager.TileLightAvgCount)
	}
	if app.BufferManager.TileLightMaxCount != 0 {
		t.Fatalf("max count = %d, want 0 for directional-only scene", app.BufferManager.TileLightMaxCount)
	}
}

func TestUpdateTiledLightMetricsKeepsLocalLightMetrics(t *testing.T) {
	app := &App{
		Scene: &core.Scene{
			Lights: []core.Light{
				{
					Position: [4]float32{0, 0, 0, 1},
					Params:   [4]float32{5, 0, float32(core.LightTypePoint), 0},
				},
			},
		},
		BufferManager: &gpu.GpuBufferManager{
			TileLightTilesX: 1,
			TileLightTilesY: 1,
		},
	}

	app.updateTiledLightMetrics(mgl32.Ident4(), mgl32.Ident4(), mgl32.Vec3{})

	if app.BufferManager.TileLightAvgCount == 0 {
		t.Fatal("expected local light metrics to remain populated")
	}
	if app.BufferManager.TileLightMaxCount == 0 {
		t.Fatal("expected local light max count to remain populated")
	}
}
