package app

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

// ParticlesFeature owns particle simulation/render pipeline lifecycle and accumulation rendering.
type ParticlesFeature struct{}

type ParticleResources struct {
	RenderPipeline   *wgpu.RenderPipeline
	InitPipeline     *wgpu.ComputePipeline
	SimPipeline      *wgpu.ComputePipeline
	SpawnPipeline    *wgpu.ComputePipeline
	FinalizePipeline *wgpu.ComputePipeline
	SpawnCount       uint32
	AtlasData        []byte
}

const DefaultParticleMaxCount uint32 = 1000000

// ParticleEmitterInput matches the WGSL emitter layout in particles_sim.wgsl.
type ParticleEmitterInput struct {
	Pos        [3]float32
	SpawnCount uint32

	Rot [4]float32

	LifeMin  float32
	LifeMax  float32
	SpeedMin float32
	SpeedMax float32

	SizeMin float32
	SizeMax float32
	Gravity float32
	Drag    float32

	ColorMin [4]float32
	ColorMax [4]float32

	ConeAngle   float32
	SpriteIndex uint32
	AtlasCols   uint32
	AtlasRows   uint32

	AlphaMode uint32
	Pad1      uint32
	Pad2      uint32
	Pad3      uint32
}

type ParticleFrameInput struct {
	DeltaTime     float32
	InvVoxelSize  float32
	MaxParticles  uint32
	SpawnRequests []uint32
	Emitters      []ParticleEmitterInput
}

func (f *ParticlesFeature) Name() string {
	return "particles"
}

func (f *ParticlesFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureParticlesSim, RenderNodeCoreAccumulation}
}

func (f *ParticlesFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePreGBuffer}
}

func (f *ParticlesFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (f *ParticlesFeature) Enabled(*App) bool {
	return true
}

func (f *ParticlesFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	if err := a.setupParticleSimulationPipelines(); err != nil {
		return err
	}
	a.setupParticlesPipeline()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupParticlesPipeline()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) OnSceneBuffersRecreated(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.CreateParticleSimBindGroups()
	f.rebuildRenderBindGroups(a)
	return nil
}

func (f *ParticlesFeature) Update(*App) error {
	return nil
}

func (f *ParticlesFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *ParticlesFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.ParticleResources = nil
}

func (f *ParticlesFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePreGBuffer {
		return nil
	}
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	resources := a.particleResources()
	if resources == nil {
		return nil
	}
	a.BufferManager.DispatchParticleSim(encoder, resources.InitPipeline, resources.SimPipeline)
	a.BufferManager.DispatchParticleSpawn(encoder, resources.SpawnPipeline, resources.FinalizePipeline, resources.SpawnCount)
	return nil
}

func (f *ParticlesFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	resources := a.particleResources()
	return stage == FeatureCommandStagePreGBuffer &&
		a != nil &&
		a.BufferManager != nil &&
		a.BufferManager.ParticleSystemActive &&
		resources != nil &&
		resources.InitPipeline != nil &&
		resources.SimPipeline != nil &&
		resources.SpawnPipeline != nil &&
		resources.FinalizePipeline != nil
}

func (f *ParticlesFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	resources := a.particleResources()
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		resources != nil &&
		resources.RenderPipeline != nil &&
		a.BufferManager.HasParticleContribution()
}

func (f *ParticlesFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	resources := a.particleResources()
	if resources == nil || resources.RenderPipeline == nil || !a.BufferManager.HasParticleContribution() {
		return nil
	}
	if a.BufferManager.ParticlesBindGroup0 == nil || a.BufferManager.ParticlesBindGroup1 == nil {
		return nil
	}

	pass.SetPipeline(resources.RenderPipeline)
	pass.SetBindGroup(0, a.BufferManager.ParticlesBindGroup0, nil)
	pass.SetBindGroup(1, a.BufferManager.ParticlesBindGroup1, nil)
	pass.DrawIndirect(a.BufferManager.ParticleIndirectBuf, 0)
	return nil
}

func (a *App) particlesSimulationReady() bool {
	resources := a.particleResources()
	return a != nil &&
		a.BufferManager != nil &&
		a.BufferManager.ParticleSystemActive &&
		resources != nil &&
		resources.InitPipeline != nil &&
		resources.SimPipeline != nil &&
		resources.SpawnPipeline != nil &&
		resources.FinalizePipeline != nil
}

func (a *App) particlesSimulationGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureParticlesSim) &&
		a.particlesSimulationReady()
}

func (a *App) recordParticlesSimulationPass(encoder *wgpu.CommandEncoder) error {
	if !a.particlesSimulationGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("ParticlesSimGraphNode", 1)
	a.Profiler.SetCount("ParticlesSystemActive", boolToCount(a.BufferManager.ParticleSystemActive))
	a.Profiler.SetCount("ParticlesSpawnCount", int(a.particleSpawnCount()))

	if encoder == nil {
		return fmt.Errorf("particles simulation command encoder is nil")
	}

	a.Profiler.BeginScope("Particles Simulation")
	defer a.Profiler.EndScope("Particles Simulation")
	resources := a.particleResources()
	if resources == nil {
		return nil
	}
	a.BufferManager.DispatchParticleSim(encoder, resources.InitPipeline, resources.SimPipeline)
	a.BufferManager.DispatchParticleSpawn(encoder, resources.SpawnPipeline, resources.FinalizePipeline, resources.SpawnCount)
	return nil
}

func (f *ParticlesFeature) rebuildRenderBindGroups(a *App) {
	renderPipeline := a.ParticleRenderPipeline()
	if renderPipeline == nil || a.BufferManager == nil {
		return
	}
	m := a.BufferManager
	if m.CameraBuf == nil || m.ParticlePoolBuf == nil || m.ParticleAliveListBuf == nil {
		return
	}
	if m.ParticleAtlasView == nil || m.ParticleAtlasSampler == nil || m.DepthView == nil {
		return
	}
	m.CreateParticlesBindGroups(renderPipeline)
}

func (a *App) ParticleRenderPipeline() *wgpu.RenderPipeline {
	resources := a.particleResources()
	if resources == nil {
		return nil
	}
	return resources.RenderPipeline
}

func (a *App) particleResources() *ParticleResources {
	if a == nil {
		return nil
	}
	return a.ParticleResources
}

func (a *App) ensureParticleResources() *ParticleResources {
	if a == nil {
		return nil
	}
	if a.ParticleResources == nil {
		a.ParticleResources = &ParticleResources{}
	}
	return a.ParticleResources
}

func (a *App) SetParticleSpawnCount(spawnCount uint32) {
	resources := a.ensureParticleResources()
	if resources == nil {
		return
	}
	resources.SpawnCount = spawnCount
}

func (a *App) particleSpawnCount() uint32 {
	resources := a.particleResources()
	if resources == nil {
		return 0
	}
	return resources.SpawnCount
}

func (a *App) ApplyParticleInput(input ParticleFrameInput) {
	resources := a.ensureParticleResources()
	if resources == nil {
		return
	}
	resources.SpawnCount = uint32(len(input.SpawnRequests))
	if a.BufferManager == nil {
		return
	}

	maxParticles := input.MaxParticles
	if maxParticles == 0 {
		maxParticles = DefaultParticleMaxCount
	}
	emitterBytes, emitterCount := particleEmitterBytes(input.Emitters)
	a.BufferManager.UpdateParticleParams(input.DeltaTime, input.InvVoxelSize, uint32(time.Now().UnixNano()), emitterCount, a.Camera.Position)
	recreated := a.BufferManager.UpdateParticles(maxParticles, emitterBytes)
	a.BufferManager.UpdateSpawnRequests(input.SpawnRequests)
	if recreated || a.BufferManager.ParticlesBindGroup0 == nil || a.BufferManager.ParticleSimBG0 == nil {
		a.BufferManager.CreateParticleSimBindGroups()
		a.BufferManager.CreateParticlesBindGroups(a.ParticleRenderPipeline())
	}
}

func (a *App) ClearParticleInput() {
	a.SetParticleSpawnCount(0)
}

func particleEmitterBytes(emitters []ParticleEmitterInput) ([]byte, uint32) {
	if len(emitters) == 0 {
		return nil, 0
	}
	count := uint32(len(emitters))
	return unsafe.Slice((*byte)(unsafe.Pointer(&emitters[0])), len(emitters)*int(unsafe.Sizeof(ParticleEmitterInput{}))), count
}
