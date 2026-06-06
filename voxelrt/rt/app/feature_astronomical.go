package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// AstronomicalFeature owns far-field angular celestial rendering.
type AstronomicalFeature struct{}

type AstronomicalResources struct {
	Pipeline *wgpu.RenderPipeline
}

type AstronomicalBodyInput struct {
	Kind                      uint32
	DirectionViewSpace        mgl32.Vec3
	LightDirectionViewSpace   mgl32.Vec3
	AngularRadiusRad          float32
	GlowAngularRadiusRad      float32
	RingInnerAngularRadiusRad float32
	RingOuterAngularRadiusRad float32
	PhaseLight01              float32
	BodyTint                  [3]float32
	EmissionStrength          float32
	Seed                      uint32
	OcclusionPriority         int32
	RingNormalViewSpace       mgl32.Vec3
	RingInnerRadiusMeters     float32
	RingOuterRadiusMeters     float32
	RingDistanceMeters        float32
	RingParentPlanetRadius    float32
}

func (f *AstronomicalFeature) Name() string {
	return "astronomical"
}

func (f *AstronomicalFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureAstronomical}
}

func (f *AstronomicalFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePostLighting}
}

func (f *AstronomicalFeature) Enabled(*App) bool {
	return true
}

func (f *AstronomicalFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupAstronomicalPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupAstronomicalPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.AstronomicalBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *AstronomicalFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *AstronomicalFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.AstronomicalResources = nil
}

func (f *AstronomicalFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.astronomicalPipeline() != nil &&
		a.StorageView != nil &&
		a.BufferManager.DepthView != nil
}

func (f *AstronomicalFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	return a.recordAstronomicalPass(encoder)
}

func (a *App) astronomicalReady() bool {
	return a != nil &&
		a.BufferManager != nil &&
		a.astronomicalPipeline() != nil &&
		a.StorageView != nil &&
		a.BufferManager.DepthView != nil
}

func (a *App) astronomicalGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureAstronomical) &&
		a.astronomicalReady()
}

func (a *App) recordAstronomicalPass(encoder *wgpu.CommandEncoder) error {
	if !a.astronomicalGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("AstronomicalGraphNode", 1)
	a.Profiler.SetCount("AstronomicalBodies", int(a.BufferManager.AstronomicalBodyCount))
	a.Profiler.SetCount("AstronomicalBG0Ready", boolToCount(a.BufferManager.AstronomicalBG0 != nil))
	a.Profiler.SetCount("AstronomicalBG1Ready", boolToCount(a.BufferManager.AstronomicalBG1 != nil))
	a.Profiler.SetCount("AstronomicalBG2Ready", boolToCount(a.BufferManager.AstronomicalBG2 != nil))

	if encoder == nil {
		return fmt.Errorf("astronomical command encoder is nil")
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:    a.StorageView,
				LoadOp:  wgpu.LoadOpLoad,
				StoreOp: wgpu.StoreOpStore,
			},
		},
	})
	if a.BufferManager.AstronomicalBodyCount > 0 && a.BufferManager.AstronomicalBG0 != nil && a.BufferManager.AstronomicalBG1 != nil && a.BufferManager.AstronomicalBG2 != nil {
		pass.SetPipeline(a.astronomicalPipeline())
		pass.SetBindGroup(0, a.BufferManager.AstronomicalBG0, nil)
		pass.SetBindGroup(1, a.BufferManager.AstronomicalBG1, nil)
		pass.SetBindGroup(2, a.BufferManager.AstronomicalBG2, nil)
		pass.Draw(3, 1, 0, 0)
	}
	return pass.End()
}

func (f *AstronomicalFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.astronomicalPipeline() == nil {
		return
	}
	a.BufferManager.CreateAstronomicalBindGroups(a.astronomicalPipeline())
}

func (a *App) astronomicalResources() *AstronomicalResources {
	if a == nil {
		return nil
	}
	return a.AstronomicalResources
}

func (a *App) ensureAstronomicalResources() *AstronomicalResources {
	if a == nil {
		return nil
	}
	if a.AstronomicalResources == nil {
		a.AstronomicalResources = &AstronomicalResources{}
	}
	return a.AstronomicalResources
}

func (a *App) astronomicalPipeline() *wgpu.RenderPipeline {
	resources := a.astronomicalResources()
	if resources == nil {
		return nil
	}
	return resources.Pipeline
}

func (a *App) ApplyAstronomicalInput(bodies []AstronomicalBodyInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdateAstronomicalBodies(astronomicalGPUHosts(bodies))
}

func (a *App) ClearAstronomicalInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.AstronomicalBodyCount = 0
}

func astronomicalGPUHosts(bodies []AstronomicalBodyInput) []gpu.AstronomicalBodyHost {
	hosts := make([]gpu.AstronomicalBodyHost, 0, len(bodies))
	for _, body := range bodies {
		hosts = append(hosts, gpu.AstronomicalBodyHost{
			Kind:                      body.Kind,
			DirectionViewSpace:        body.DirectionViewSpace,
			LightDirectionViewSpace:   body.LightDirectionViewSpace,
			AngularRadiusRad:          body.AngularRadiusRad,
			GlowAngularRadiusRad:      body.GlowAngularRadiusRad,
			RingInnerAngularRadiusRad: body.RingInnerAngularRadiusRad,
			RingOuterAngularRadiusRad: body.RingOuterAngularRadiusRad,
			PhaseLight01:              body.PhaseLight01,
			BodyTint:                  body.BodyTint,
			EmissionStrength:          body.EmissionStrength,
			Seed:                      body.Seed,
			OcclusionPriority:         body.OcclusionPriority,
			RingNormalViewSpace:       body.RingNormalViewSpace,
			RingInnerRadiusMeters:     body.RingInnerRadiusMeters,
			RingOuterRadiusMeters:     body.RingOuterRadiusMeters,
			RingDistanceMeters:        body.RingDistanceMeters,
			RingParentPlanetRadius:    body.RingParentPlanetRadius,
		})
	}
	return hosts
}
