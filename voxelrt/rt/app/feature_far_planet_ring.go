package app

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

type FarPlanetRingFeature struct{}

type FarPlanetRingResources struct {
	Pipeline *wgpu.RenderPipeline
}

type FarPlanetRingInput struct {
	BandID                           string
	ParentBodyID                     string
	CenterCameraRelativeMeters       mgl32.Vec3
	NormalCameraRelative             mgl32.Vec3
	TangentUCameraRelative           mgl32.Vec3
	TangentVCameraRelative           mgl32.Vec3
	InnerRadiusMeters                float32
	OuterRadiusMeters                float32
	HalfThicknessMeters              float32
	Tint                             [3]float32
	Opacity                          float32
	DustHazeOpacity                  float32
	DustHazeMaxAlpha                 float32
	DustHazeThicknessScale           float32
	DustHazeMinHalfThicknessMeters   float32
	DustHazeRadialEdgeFadeFraction   float32
	DustHazeVerticalCoreFraction     float32
	DustHazeSampleCount              float32
	DustHazeForwardScatterStrength   float32
	DustHazeShadowStrength           float32
	Seed                             uint32
	RadialOpacityProfile             [32]float32
	ParentCenterCameraRelativeMeters mgl32.Vec3
	ParentRadiusMeters               float32
	ParentDepthMeters                float32
	LightDirectionViewSpace          mgl32.Vec3
}

func (f *FarPlanetRingFeature) Name() string {
	return "far_planet_ring"
}

func (f *FarPlanetRingFeature) GraphNodeNames() []string {
	return []string{RenderNodeCoreAccumulation}
}

func (f *FarPlanetRingFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (f *FarPlanetRingFeature) Enabled(*App) bool {
	return true
}

func (f *FarPlanetRingFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupFarPlanetRingPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupFarPlanetRingPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.FarPlanetRingBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *FarPlanetRingFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *FarPlanetRingFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.FarPlanetRingResources = nil
}

func (f *FarPlanetRingFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return false
}

func (f *FarPlanetRingFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	return nil
}

func (f *FarPlanetRingFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	pipeline := a.farPlanetRingPipeline()
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		pipeline != nil &&
		a.BufferManager.DepthView != nil &&
		a.BufferManager.PlanetDepthView != nil &&
		a.BufferManager.TransparentAccumView != nil &&
		a.BufferManager.TransparentWeightView != nil &&
		a.BufferManager.FarPlanetRingCount > 0 &&
		a.BufferManager.FarPlanetRingBG0 != nil &&
		a.BufferManager.FarPlanetRingBG1 != nil &&
		a.BufferManager.FarPlanetRingBG2 != nil
}

func (f *FarPlanetRingFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	pipeline := a.farPlanetRingPipeline()
	if pipeline == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	if a.BufferManager.FarPlanetRingCount == 0 {
		return nil
	}
	if a.BufferManager.FarPlanetRingBG0 == nil || a.BufferManager.FarPlanetRingBG1 == nil || a.BufferManager.FarPlanetRingBG2 == nil {
		return nil
	}
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, a.BufferManager.FarPlanetRingBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.FarPlanetRingBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.FarPlanetRingBG2, nil)
	pass.Draw(3, 1, 0, 0)
	return nil
}

func (f *FarPlanetRingFeature) rebuildBindGroups(a *App) {
	pipeline := a.farPlanetRingPipeline()
	if pipeline == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.CreateFarPlanetRingBindGroups(pipeline)
}

func (a *App) farPlanetRingPipeline() *wgpu.RenderPipeline {
	if a == nil || a.FarPlanetRingResources == nil {
		return nil
	}
	return a.FarPlanetRingResources.Pipeline
}

func (a *App) ApplyFarPlanetRingInput(rings []FarPlanetRingInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdateFarPlanetRings(farPlanetRingGPUHosts(rings))
}

func (a *App) ClearFarPlanetRingInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.FarPlanetRingCount = 0
}

func farPlanetRingGPUHosts(rings []FarPlanetRingInput) []gpu.FarPlanetRingHost {
	hosts := make([]gpu.FarPlanetRingHost, 0, len(rings))
	for _, ring := range rings {
		hosts = append(hosts, gpu.FarPlanetRingHost{
			BandID:                           ring.BandID,
			ParentBodyID:                     ring.ParentBodyID,
			CenterCameraRelativeMeters:       ring.CenterCameraRelativeMeters,
			NormalCameraRelative:             ring.NormalCameraRelative,
			TangentUCameraRelative:           ring.TangentUCameraRelative,
			TangentVCameraRelative:           ring.TangentVCameraRelative,
			InnerRadiusMeters:                ring.InnerRadiusMeters,
			OuterRadiusMeters:                ring.OuterRadiusMeters,
			HalfThicknessMeters:              ring.HalfThicknessMeters,
			Tint:                             ring.Tint,
			Opacity:                          ring.Opacity,
			DustHazeOpacity:                  ring.DustHazeOpacity,
			DustHazeMaxAlpha:                 ring.DustHazeMaxAlpha,
			DustHazeThicknessScale:           ring.DustHazeThicknessScale,
			DustHazeMinHalfThicknessMeters:   ring.DustHazeMinHalfThicknessMeters,
			DustHazeRadialEdgeFadeFraction:   ring.DustHazeRadialEdgeFadeFraction,
			DustHazeVerticalCoreFraction:     ring.DustHazeVerticalCoreFraction,
			DustHazeSampleCount:              ring.DustHazeSampleCount,
			DustHazeForwardScatterStrength:   ring.DustHazeForwardScatterStrength,
			DustHazeShadowStrength:           ring.DustHazeShadowStrength,
			Seed:                             ring.Seed,
			RadialOpacityProfile:             ring.RadialOpacityProfile,
			ParentCenterCameraRelativeMeters: ring.ParentCenterCameraRelativeMeters,
			ParentRadiusMeters:               ring.ParentRadiusMeters,
			ParentDepthMeters:                ring.ParentDepthMeters,
			LightDirectionViewSpace:          ring.LightDirectionViewSpace,
		})
	}
	return hosts
}
