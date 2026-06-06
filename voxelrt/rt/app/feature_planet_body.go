package app

import (
	"fmt"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// PlanetBodyFeature owns analytic far-body planet rendering.
type PlanetBodyFeature struct{}

type PlanetBodyResources struct {
	Pipeline *wgpu.RenderPipeline
}

type PlanetBodyInput struct {
	EntityID               uint32
	Seed                   uint32
	Position               mgl32.Vec3
	Rotation               mgl32.Quat
	Radius                 float32
	OceanRadius            float32
	AtmosphereRadius       float32
	AtmosphereRimWidth     float32
	HeightAmplitude        float32
	NoiseScale             float32
	BlockSize              float32
	HeightSteps            uint32
	HandoffNearAlt         float32
	HandoffFarAlt          float32
	BiomeMix               float32
	BakedSurfaceResolution uint32
	BakedSurfaceSamples    []PlanetBakedSurfaceSampleInput
	BakedSurfaceID         uintptr
	BandColors             [6][3]float32
	AmbientStrength        float32
	DiffuseStrength        float32
	SpecularStrength       float32
	RimStrength            float32
	EmissionStrength       float32
	TerrainLowColor        [3]float32
	TerrainHighColor       [3]float32
	RockColor              [3]float32
	OceanDeepColor         [3]float32
	OceanShallowColor      [3]float32
	AtmosphereColor        [3]float32
}

type PlanetBodySurfaceInput struct {
	BakedSurfaceResolution uint32
	BakedSurfaceSamples    []PlanetBakedSurfaceSampleInput
	BakedSurfaceID         uintptr
}

type PlanetBakedSurfaceSampleInput struct {
	Height       float32
	NormalOctX   float32
	NormalOctY   float32
	MaterialBand float32
}

func (f *PlanetBodyFeature) Name() string {
	return "planet-bodies"
}

func (f *PlanetBodyFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeaturePlanetBodies}
}

func (f *PlanetBodyFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePostLighting}
}

func (f *PlanetBodyFeature) Enabled(*App) bool {
	return true
}

func (f *PlanetBodyFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupPlanetBodyPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupPlanetBodyPipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.PlanetBodyBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *PlanetBodyFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *PlanetBodyFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.PlanetBodyResources = nil
}

func (f *PlanetBodyFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePostLighting &&
		a != nil &&
		a.BufferManager != nil &&
		a.planetBodyPipeline() != nil &&
		a.StorageView != nil &&
		a.BufferManager.PlanetDepthView != nil
}

func (f *PlanetBodyFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePostLighting {
		return nil
	}
	return a.recordPlanetBodiesPass(encoder)
}

func (a *App) planetBodiesReady() bool {
	return a != nil &&
		a.BufferManager != nil &&
		a.planetBodyPipeline() != nil &&
		a.StorageView != nil &&
		a.BufferManager.PlanetDepthView != nil
}

func (a *App) planetBodiesGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeaturePlanetBodies) &&
		a.planetBodiesReady()
}

func (a *App) recordPlanetBodiesPass(encoder *wgpu.CommandEncoder) error {
	if !a.planetBodiesGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("PlanetBodiesGraphNode", 1)
	a.Profiler.SetCount("PlanetBodies", int(a.BufferManager.PlanetBodyCount))
	a.Profiler.SetCount("PlanetBodiesBG0Ready", boolToCount(a.BufferManager.PlanetBodyBG0 != nil))
	a.Profiler.SetCount("PlanetBodiesBG1Ready", boolToCount(a.BufferManager.PlanetBodyBG1 != nil))
	a.Profiler.SetCount("PlanetBodiesBG2Ready", boolToCount(a.BufferManager.PlanetBodyBG2 != nil))

	if encoder == nil {
		return fmt.Errorf("planet bodies command encoder is nil")
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:    a.StorageView,
				LoadOp:  wgpu.LoadOpLoad,
				StoreOp: wgpu.StoreOpStore,
			},
			{
				View:       a.BufferManager.PlanetDepthView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
			},
		},
	})
	if a.BufferManager.PlanetBodyCount > 0 && a.BufferManager.PlanetBodyBG0 != nil && a.BufferManager.PlanetBodyBG1 != nil && a.BufferManager.PlanetBodyBG2 != nil {
		pass.SetPipeline(a.planetBodyPipeline())
		pass.SetBindGroup(0, a.BufferManager.PlanetBodyBG0, nil)
		pass.SetBindGroup(1, a.BufferManager.PlanetBodyBG1, nil)
		pass.SetBindGroup(2, a.BufferManager.PlanetBodyBG2, nil)
		pass.Draw(3, 1, 0, 0)
	}
	return pass.End()
}

func (f *PlanetBodyFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil || a.planetBodyPipeline() == nil {
		return
	}
	a.BufferManager.CreatePlanetBodyBindGroups(a.planetBodyPipeline())
}

func (a *App) planetBodyResources() *PlanetBodyResources {
	if a == nil {
		return nil
	}
	return a.PlanetBodyResources
}

func (a *App) ensurePlanetBodyResources() *PlanetBodyResources {
	if a == nil {
		return nil
	}
	if a.PlanetBodyResources == nil {
		a.PlanetBodyResources = &PlanetBodyResources{}
	}
	return a.PlanetBodyResources
}

func (a *App) planetBodyPipeline() *wgpu.RenderPipeline {
	resources := a.planetBodyResources()
	if resources == nil {
		return nil
	}
	return resources.Pipeline
}

func (a *App) ApplyPlanetBodyInput(planets []PlanetBodyInput, preloads []PlanetBodySurfaceInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.UpdatePlanetBodiesWithSurfacePreloads(planetBodyGPUHosts(planets), planetBodySurfaceGPUHosts(preloads))
}

func (a *App) ClearPlanetBodyInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.PlanetBodyCount = 0
}

func planetBodyGPUHosts(planets []PlanetBodyInput) []gpu.PlanetBodyHost {
	hosts := make([]gpu.PlanetBodyHost, 0, len(planets))
	for _, planet := range planets {
		hosts = append(hosts, gpu.PlanetBodyHost{
			EntityID:               planet.EntityID,
			Seed:                   planet.Seed,
			Position:               planet.Position,
			Rotation:               planet.Rotation,
			Radius:                 planet.Radius,
			OceanRadius:            planet.OceanRadius,
			AtmosphereRadius:       planet.AtmosphereRadius,
			AtmosphereRimWidth:     planet.AtmosphereRimWidth,
			HeightAmplitude:        planet.HeightAmplitude,
			NoiseScale:             planet.NoiseScale,
			BlockSize:              planet.BlockSize,
			HeightSteps:            planet.HeightSteps,
			HandoffNearAlt:         planet.HandoffNearAlt,
			HandoffFarAlt:          planet.HandoffFarAlt,
			BiomeMix:               planet.BiomeMix,
			BakedSurfaceResolution: planet.BakedSurfaceResolution,
			BakedSurfaceSamples:    planetBakedSurfaceGPUHostSlice(planet.BakedSurfaceSamples),
			BakedSurfaceID:         planet.BakedSurfaceID,
			BandColors:             planet.BandColors,
			AmbientStrength:        planet.AmbientStrength,
			DiffuseStrength:        planet.DiffuseStrength,
			SpecularStrength:       planet.SpecularStrength,
			RimStrength:            planet.RimStrength,
			EmissionStrength:       planet.EmissionStrength,
			TerrainLowColor:        planet.TerrainLowColor,
			TerrainHighColor:       planet.TerrainHighColor,
			RockColor:              planet.RockColor,
			OceanDeepColor:         planet.OceanDeepColor,
			OceanShallowColor:      planet.OceanShallowColor,
			AtmosphereColor:        planet.AtmosphereColor,
		})
	}
	return hosts
}

func planetBodySurfaceGPUHosts(preloads []PlanetBodySurfaceInput) []gpu.PlanetBodySurfaceHost {
	hosts := make([]gpu.PlanetBodySurfaceHost, 0, len(preloads))
	for _, preload := range preloads {
		hosts = append(hosts, gpu.PlanetBodySurfaceHost{
			BakedSurfaceResolution: preload.BakedSurfaceResolution,
			BakedSurfaceSamples:    planetBakedSurfaceGPUHostSlice(preload.BakedSurfaceSamples),
			BakedSurfaceID:         preload.BakedSurfaceID,
		})
	}
	return hosts
}

func planetBakedSurfaceGPUHostSlice(samples []PlanetBakedSurfaceSampleInput) []gpu.PlanetBakedSurfaceSampleHost {
	if len(samples) == 0 {
		return nil
	}
	return unsafe.Slice((*gpu.PlanetBakedSurfaceSampleHost)(unsafe.Pointer(unsafe.SliceData(samples))), len(samples))
}
