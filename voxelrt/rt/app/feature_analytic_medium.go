package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// AnalyticMediumFeature owns the bounded analytic medium accumulation pass.
type AnalyticMediumFeature struct{}

type AnalyticMediumResources struct {
	Pipeline *wgpu.RenderPipeline
}

type AnalyticMediumInput struct {
	EntityID                  uint32
	Shape                     uint32
	Position                  mgl32.Vec3
	Rotation                  mgl32.Quat
	OuterRadius               float32
	InnerRadius               float32
	BoxExtents                [3]float32
	Density                   float32
	Falloff                   float32
	EdgeSoftness              float32
	PhaseG                    float32
	LightStrength             float32
	AmbientStrength           float32
	LimbStrength              float32
	LimbExponent              float32
	DiskHazeStrength          float32
	DiskHazeTintMix           float32
	OpaqueExtinctionScale     float32
	BackgroundExtinctionScale float32
	BoundaryFadeStart         float32
	BoundaryFadeEnd           float32
	OpaqueAlphaScale          float32
	BackgroundAlphaScale      float32
	OpaqueRevealScale         float32
	BackgroundRevealScale     float32
	Color                     [3]float32
	AbsorptionColor           [3]float32
	EmissionColor             [3]float32
	NoiseScale                float32
	NoiseStrength             float32
	SampleCount               uint32
	CloudBlockSize            float32
	CloudThreshold            float32
	CloudTime                 float32
	CloudAltitudeSteps        float32
}

func analyticDepthClearValue(a *App) float64 {
	if a != nil && a.Camera != nil {
		far := float64(a.Camera.FarPlane())
		if far > 0 {
			return far
		}
	}
	return 1e20
}

func (f *AnalyticMediumFeature) Name() string {
	return "analytic-media"
}

func (f *AnalyticMediumFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureAnalyticMedia}
}

func (f *AnalyticMediumFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePostLighting}
}

func (f *AnalyticMediumFeature) Enabled(*App) bool {
	return true
}

func (f *AnalyticMediumFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupAnalyticMediumPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupAnalyticMediumPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.AnalyticMediumBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *AnalyticMediumFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *AnalyticMediumFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.AnalyticMediumResources = nil
}

func (f *AnalyticMediumFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.analyticMediumPipeline() != nil &&
		a.BufferManager.CurrentVolumetricView() != nil &&
		a.BufferManager.CurrentVolumetricDepthView() != nil
}

func (f *AnalyticMediumFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	return a.recordAnalyticMediumPass(encoder)
}

func (a *App) analyticMediumReady() bool {
	return a != nil &&
		a.BufferManager != nil &&
		a.analyticMediumPipeline() != nil &&
		a.BufferManager.CurrentVolumetricView() != nil &&
		a.BufferManager.CurrentVolumetricDepthView() != nil
}

func (a *App) analyticMediumGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureAnalyticMedia) &&
		a.analyticMediumReady()
}

func (a *App) recordAnalyticMediumPass(encoder *wgpu.CommandEncoder) error {
	if !a.analyticMediumGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("AnalyticMediaGraphNode", 1)
	a.Profiler.SetCount("AnalyticMediaCount", int(a.BufferManager.AnalyticMediumCount))
	a.Profiler.SetCount("AnalyticMediaBG0Ready", boolToCount(a.BufferManager.AnalyticMediumBG0 != nil))
	a.Profiler.SetCount("AnalyticMediaBG1Ready", boolToCount(a.BufferManager.AnalyticMediumBG1 != nil))
	a.Profiler.SetCount("AnalyticMediaBG2Ready", boolToCount(a.BufferManager.AnalyticMediumBG2 != nil))

	if encoder == nil {
		return fmt.Errorf("analytic media command encoder is nil")
	}

	if a.BufferManager.AnalyticMediumBG0 == nil || a.BufferManager.AnalyticMediumBG1 == nil || a.BufferManager.AnalyticMediumBG2 == nil {
		if a.BufferManager.AnalyticMediumCount == 0 {
			// Still clear the current volumetric targets so stale history is not reused.
			pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
				ColorAttachments: []wgpu.RenderPassColorAttachment{
					{
						View:       a.BufferManager.CurrentVolumetricView(),
						LoadOp:     wgpu.LoadOpClear,
						StoreOp:    wgpu.StoreOpStore,
						ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
					},
					{
						View:       a.BufferManager.CurrentVolumetricDepthView(),
						LoadOp:     wgpu.LoadOpClear,
						StoreOp:    wgpu.StoreOpStore,
						ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
					},
				},
			})
			return pass.End()
		}
		return nil
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       a.BufferManager.CurrentVolumetricView(),
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
			},
			{
				View:       a.BufferManager.CurrentVolumetricDepthView(),
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
			},
		},
	})
	pass.SetPipeline(a.analyticMediumPipeline())
	pass.SetBindGroup(0, a.BufferManager.AnalyticMediumBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.AnalyticMediumBG1, nil)
	pass.SetBindGroup(2, a.BufferManager.AnalyticMediumBG2, nil)
	if a.BufferManager.AnalyticMediumCount > 0 {
		pass.Draw(3, 1, 0, 0)
	}
	return pass.End()
}

func (f *AnalyticMediumFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.analyticMediumPipeline() == nil {
		return
	}
	a.BufferManager.CreateAnalyticMediumBindGroups(a.analyticMediumPipeline())
}

func (a *App) analyticMediumResources() *AnalyticMediumResources {
	if a == nil {
		return nil
	}
	return a.AnalyticMediumResources
}

func (a *App) ensureAnalyticMediumResources() *AnalyticMediumResources {
	if a == nil {
		return nil
	}
	if a.AnalyticMediumResources == nil {
		a.AnalyticMediumResources = &AnalyticMediumResources{}
	}
	return a.AnalyticMediumResources
}

func (a *App) analyticMediumPipeline() *wgpu.RenderPipeline {
	resources := a.analyticMediumResources()
	if resources == nil {
		return nil
	}
	return resources.Pipeline
}

func (a *App) ApplyAnalyticMediumInput(media []AnalyticMediumInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdateAnalyticMedia(analyticMediumGPUHosts(media))
}

func (a *App) ClearAnalyticMediumInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.AnalyticMediumCount = 0
}

func analyticMediumGPUHosts(media []AnalyticMediumInput) []gpu.AnalyticMediumHost {
	hosts := make([]gpu.AnalyticMediumHost, 0, len(media))
	for _, medium := range media {
		hosts = append(hosts, gpu.AnalyticMediumHost{
			EntityID:                  medium.EntityID,
			Shape:                     medium.Shape,
			Position:                  medium.Position,
			Rotation:                  medium.Rotation,
			OuterRadius:               medium.OuterRadius,
			InnerRadius:               medium.InnerRadius,
			BoxExtents:                medium.BoxExtents,
			Density:                   medium.Density,
			Falloff:                   medium.Falloff,
			EdgeSoftness:              medium.EdgeSoftness,
			PhaseG:                    medium.PhaseG,
			LightStrength:             medium.LightStrength,
			AmbientStrength:           medium.AmbientStrength,
			LimbStrength:              medium.LimbStrength,
			LimbExponent:              medium.LimbExponent,
			DiskHazeStrength:          medium.DiskHazeStrength,
			DiskHazeTintMix:           medium.DiskHazeTintMix,
			OpaqueExtinctionScale:     medium.OpaqueExtinctionScale,
			BackgroundExtinctionScale: medium.BackgroundExtinctionScale,
			BoundaryFadeStart:         medium.BoundaryFadeStart,
			BoundaryFadeEnd:           medium.BoundaryFadeEnd,
			OpaqueAlphaScale:          medium.OpaqueAlphaScale,
			BackgroundAlphaScale:      medium.BackgroundAlphaScale,
			OpaqueRevealScale:         medium.OpaqueRevealScale,
			BackgroundRevealScale:     medium.BackgroundRevealScale,
			Color:                     medium.Color,
			AbsorptionColor:           medium.AbsorptionColor,
			EmissionColor:             medium.EmissionColor,
			NoiseScale:                medium.NoiseScale,
			NoiseStrength:             medium.NoiseStrength,
			SampleCount:               medium.SampleCount,
			CloudBlockSize:            medium.CloudBlockSize,
			CloudThreshold:            medium.CloudThreshold,
			CloudTime:                 medium.CloudTime,
			CloudAltitudeSteps:        medium.CloudAltitudeSteps,
		})
	}
	return hosts
}
