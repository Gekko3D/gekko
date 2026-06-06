package app

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// WaterFeature owns dedicated stylized water surface accumulation rendering.
type WaterFeature struct{}

type WaterResources struct {
	Pipeline *wgpu.RenderPipeline
}

type WaterSurfaceInput struct {
	EntityID        uint32
	Position        mgl32.Vec3
	HalfExtents     [2]float32
	Depth           float32
	Color           [3]float32
	AbsorptionColor [3]float32
	Opacity         float32
	Roughness       float32
	Refraction      float32
	FlowDirection   [2]float32
	FlowSpeed       float32
	WaveAmplitude   float32
}

type WaterRippleInput struct {
	WaterIndex uint32
	Position   mgl32.Vec3
	Strength   float32
	Age        float32
	Lifetime   float32
}

func (f *WaterFeature) Name() string {
	return "water"
}

func (f *WaterFeature) GraphNodeNames() []string {
	return []string{RenderNodeCoreAccumulation}
}

func (f *WaterFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (f *WaterFeature) Enabled(*App) bool {
	return true
}

func (f *WaterFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupWaterPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupWaterPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.WaterBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *WaterFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *WaterFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.WaterResources = nil
}

func (f *WaterFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	pipeline := a.waterPipeline()
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		pipeline != nil &&
		a.BufferManager.HasWaterContribution()
}

func (f *WaterFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	pipeline := a.waterPipeline()
	if pipeline == nil || !a.BufferManager.HasWaterContribution() {
		return nil
	}
	if a.BufferManager.WaterBG0 == nil || a.BufferManager.WaterBG1 == nil || a.BufferManager.WaterBG2 == nil {
		return nil
	}

	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, a.BufferManager.WaterBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.WaterBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.WaterBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *WaterFeature) rebuildBindGroups(a *App) {
	pipeline := a.waterPipeline()
	if pipeline == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.StorageView = a.StorageView
	a.BufferManager.CreateWaterBindGroups(pipeline)
}

func (a *App) waterPipeline() *wgpu.RenderPipeline {
	if a == nil || a.WaterResources == nil {
		return nil
	}
	return a.WaterResources.Pipeline
}

func (a *App) ApplyWaterInput(waters []WaterSurfaceInput, ripples []WaterRippleInput, dt float32) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdateWaterSurfaces(waterSurfaceGPUHosts(waters), waterRippleGPUHosts(ripples), dt)
}

func (a *App) ClearWaterInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.WaterCount = 0
	a.BufferManager.WaterRippleCount = 0
}

func waterSurfaceGPUHosts(waters []WaterSurfaceInput) []gpu.WaterSurfaceHost {
	hosts := make([]gpu.WaterSurfaceHost, 0, len(waters))
	for _, water := range waters {
		hosts = append(hosts, gpu.WaterSurfaceHost{
			EntityID:        water.EntityID,
			Position:        water.Position,
			HalfExtents:     water.HalfExtents,
			Depth:           water.Depth,
			Color:           water.Color,
			AbsorptionColor: water.AbsorptionColor,
			Opacity:         water.Opacity,
			Roughness:       water.Roughness,
			Refraction:      water.Refraction,
			FlowDirection:   water.FlowDirection,
			FlowSpeed:       water.FlowSpeed,
			WaveAmplitude:   water.WaveAmplitude,
		})
	}
	return hosts
}

func waterRippleGPUHosts(ripples []WaterRippleInput) []gpu.WaterRippleHost {
	hosts := make([]gpu.WaterRippleHost, 0, len(ripples))
	for _, ripple := range ripples {
		hosts = append(hosts, gpu.WaterRippleHost{
			WaterIndex: ripple.WaterIndex,
			Position:   ripple.Position,
			Strength:   ripple.Strength,
			Age:        ripple.Age,
			Lifetime:   ripple.Lifetime,
		})
	}
	return hosts
}
