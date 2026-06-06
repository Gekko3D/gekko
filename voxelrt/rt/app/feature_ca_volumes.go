package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// CAVolumeFeature owns CA volume simulation, bounds, and accumulation rendering.
type CAVolumeFeature struct{}

type CAVolumeResources struct {
	RenderPipeline *wgpu.RenderPipeline
	SimPipeline    *wgpu.ComputePipeline
	BoundsPipeline *wgpu.ComputePipeline
	HadPass        bool
}

type CAVolumeInput struct {
	EntityID        uint32
	Type            uint32
	Preset          uint32
	Resolution      [3]uint32
	Position        mgl32.Vec3
	Rotation        mgl32.Quat
	VoxelScale      mgl32.Vec3
	Intensity       float32
	Diffusion       float32
	Buoyancy        float32
	Cooling         float32
	Dissipation     float32
	Extinction      float32
	Emission        float32
	StepsPending    float32
	StepDt          float32
	ScatterColor    [3]float32
	ShadowTint      [3]float32
	AbsorptionColor [3]float32
}

type CAVolumeFrameInput struct {
	Volumes                []CAVolumeInput
	DeltaTime              float32
	UpdatePresets          bool
	RequestedVolumeCount   uint32
	ResolutionClampedCount uint32
	DeferredStepCount      uint32
	SuspendedVolumeCount   uint32
	DroppedVolumeCount     uint32
	TotalScheduledSteps    uint32
}

func (f *CAVolumeFeature) Name() string {
	return "ca-volumes"
}

func (f *CAVolumeFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureCAVolumesSim, RenderNodeFeatureCAVolumesRender}
}

func (f *CAVolumeFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePreGBufferVolumes, FeatureCommandStagePostLighting}
}

func (f *CAVolumeFeature) Enabled(*App) bool {
	return true
}

func (f *CAVolumeFeature) Setup(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	if err := a.createCAVolumeSimPipeline(); err != nil {
		return err
	}
	if err := a.createCAVolumeBoundsPipeline(); err != nil {
		return err
	}
	a.BufferManager.UpdateCAVolumes(nil)
	a.BufferManager.UpdateCAParams(0)
	a.setupCAVolumePipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupCAVolumePipeline()
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) OnSceneBuffersRecreated(a *App) error {
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Update(a *App) error {
	if a == nil || a.BufferManager == nil || !a.BufferManager.CAVolumeBindingsDirty {
		return nil
	}
	f.rebuildBindGroups(a)
	return nil
}

func (f *CAVolumeFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *CAVolumeFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.CAVolumeResources = nil
}

func (f *CAVolumeFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	switch stage {
	case FeatureCommandStagePreGBufferVolumes:
		return a.recordCAVolumeSimulationPass(encoder)
	case FeatureCommandStagePostLighting:
		return a.recordCAVolumeRenderPass(encoder)
	}
	return nil
}

func (f *CAVolumeFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	if a == nil || a.BufferManager == nil {
		return false
	}
	switch stage {
	case FeatureCommandStagePreGBufferVolumes:
		return a.caVolumeSimPipeline() != nil &&
			a.caVolumeBoundsPipeline() != nil &&
			a.BufferManager.CAVolumeCount > 0
	case FeatureCommandStagePostLighting:
		return a.BufferManager.CAVolumeColorView != nil &&
			a.BufferManager.CAVolumeDepthView != nil &&
			(a.BufferManager.CAVolumeVisibleCount > 0 || a.HadCAVolumePass())
	default:
		return false
	}
}

func (a *App) caVolumesSimulationReady() bool {
	return a != nil &&
		a.BufferManager != nil &&
		a.caVolumeSimPipeline() != nil &&
		a.caVolumeBoundsPipeline() != nil &&
		a.BufferManager.CAVolumeCount > 0
}

func (a *App) caVolumesSimulationGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureCAVolumesSim) &&
		a.caVolumesSimulationReady()
}

func (a *App) recordCAVolumeSimulationPass(encoder *wgpu.CommandEncoder) error {
	if !a.caVolumesSimulationGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("CAVolumeSimGraphNode", 1)
	a.Profiler.SetCount("CAVolumeSimCount", int(a.BufferManager.CAVolumeCount))

	if encoder == nil {
		return fmt.Errorf("CA volume simulation command encoder is nil")
	}

	a.Profiler.BeginScope("CA Volume Simulation")
	defer a.Profiler.EndScope("CA Volume Simulation")
	a.BufferManager.DispatchCAVolumeSim(encoder, a.caVolumeSimPipeline())
	a.BufferManager.DispatchCAVolumeBounds(encoder, a.caVolumeBoundsPipeline())
	return nil
}

func (a *App) caVolumesRenderReady() bool {
	return a != nil &&
		a.BufferManager != nil &&
		a.BufferManager.CAVolumeColorView != nil &&
		a.BufferManager.CAVolumeDepthView != nil &&
		(a.BufferManager.CAVolumeVisibleCount > 0 || a.HadCAVolumePass())
}

func (a *App) caVolumesRenderGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureCAVolumesRender) &&
		a.caVolumesRenderReady()
}

func (a *App) recordCAVolumeRenderPass(encoder *wgpu.CommandEncoder) error {
	if !a.caVolumesRenderGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("CAVolumeRenderGraphNode", 1)
	a.Profiler.SetCount("CAVolumeRenderVisible", int(a.BufferManager.CAVolumeVisibleCount))
	a.Profiler.SetCount("CAVolumeRenderHadPrevious", boolToCount(a.HadCAVolumePass()))
	a.Profiler.SetCount("CAVolumeRenderPipelineReady", boolToCount(a.caVolumeRenderPipeline() != nil))
	a.Profiler.SetCount("CAVolumeRenderBG0Ready", boolToCount(a.BufferManager.CAVolumeRenderBG0 != nil))
	a.Profiler.SetCount("CAVolumeRenderBG1Ready", boolToCount(a.BufferManager.CurrentCAVolumeRenderBG1() != nil))
	a.Profiler.SetCount("CAVolumeRenderBG2Ready", boolToCount(a.BufferManager.CAVolumeRenderBG2 != nil))

	if encoder == nil {
		return fmt.Errorf("CA volume render command encoder is nil")
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       a.BufferManager.CAVolumeColorView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 1},
			},
			{
				View:       a.BufferManager.CAVolumeDepthView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: analyticDepthClearValue(a), G: 0, B: 0, A: 0},
			},
		},
	})
	defer pass.End()
	renderPipeline := a.caVolumeRenderPipeline()
	if renderPipeline == nil || a.BufferManager.CAVolumeRenderBG0 == nil || a.BufferManager.CAVolumeRenderBG2 == nil || a.BufferManager.CurrentCAVolumeRenderBG1() == nil {
		a.SetHadCAVolumePass(false)
		return nil
	}
	if a.BufferManager.CAVolumeVisibleCount == 0 {
		a.SetHadCAVolumePass(false)
		return nil
	}
	volumes := a.BufferManager.CurrentCAVolumes()
	if len(volumes) == 0 {
		a.SetHadCAVolumePass(false)
		return nil
	}
	candidates := buildCAVolumeRenderCandidates(a.Camera, a.BufferManager.VolumetricWidth, a.BufferManager.VolumetricHeight, volumes)
	if len(candidates) == 0 {
		a.SetHadCAVolumePass(false)
		return nil
	}

	pass.SetPipeline(renderPipeline)
	pass.SetBindGroup(0, a.BufferManager.CAVolumeRenderBG0, nil)
	pass.SetBindGroup(1, a.BufferManager.CurrentCAVolumeRenderBG1(), nil)
	pass.SetBindGroup(2, a.BufferManager.CAVolumeRenderBG2, nil)
	for _, candidate := range candidates {
		pass.SetScissorRect(candidate.Scissor.X, candidate.Scissor.Y, candidate.Scissor.W, candidate.Scissor.H)
		pass.Draw(3, 1, 0, uint32(candidate.VolumeIndex))
	}
	a.SetHadCAVolumePass(true)
	return nil
}

func (f *CAVolumeFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return false
}

func (f *CAVolumeFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	_ = a
	_ = stage
	_ = pass
	return nil
}

func (f *CAVolumeFeature) rebuildBindGroups(a *App) {
	if a == nil || a.BufferManager == nil {
		return
	}
	if a.caVolumeSimPipeline() != nil {
		a.BufferManager.CreateCAVolumeSimBindGroups()
	}
	if a.caVolumeBoundsPipeline() != nil {
		a.BufferManager.CreateCAVolumeBoundsBindGroups()
	}
	if renderPipeline := a.caVolumeRenderPipeline(); renderPipeline != nil {
		a.BufferManager.CreateCAVolumeRenderBindGroups(renderPipeline)
	}
}

func (a *App) HadCAVolumePass() bool {
	resources := a.caVolumeResources()
	return resources != nil && resources.HadPass
}

func (a *App) SetHadCAVolumePass(hadPass bool) {
	if a == nil {
		return
	}
	a.ensureCAVolumeResources().HadPass = hadPass
}

func (a *App) caVolumeResources() *CAVolumeResources {
	if a == nil {
		return nil
	}
	return a.CAVolumeResources
}

func (a *App) ensureCAVolumeResources() *CAVolumeResources {
	if a == nil {
		return nil
	}
	if a.CAVolumeResources == nil {
		a.CAVolumeResources = &CAVolumeResources{}
	}
	return a.CAVolumeResources
}

func (a *App) ApplyCAVolumeInput(input CAVolumeFrameInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	if input.UpdatePresets {
		a.BufferManager.UpdateCAPresets()
	}
	a.BufferManager.CARequestedVolumeCount = input.RequestedVolumeCount
	a.BufferManager.CAResolutionClampedCount = input.ResolutionClampedCount
	a.BufferManager.CADeferredStepVolumeCount = input.DeferredStepCount
	a.BufferManager.CASuspendedVolumeCount = input.SuspendedVolumeCount
	a.BufferManager.CADroppedVolumeCount = input.DroppedVolumeCount
	a.BufferManager.CATotalScheduledSteps = input.TotalScheduledSteps
	a.BufferManager.UpdateCAVolumes(caVolumeGPUHosts(input.Volumes))
	a.BufferManager.UpdateCAParams(input.DeltaTime)
}

func (a *App) ClearCAVolumeInput() {
	if a == nil {
		return
	}
	a.SetHadCAVolumePass(false)
	if a.BufferManager == nil {
		return
	}
	a.BufferManager.CAVolumeCount = 0
	a.BufferManager.CAVolumeVisibleCount = 0
	a.BufferManager.CARequestedVolumeCount = 0
	a.BufferManager.CAResolutionClampedCount = 0
	a.BufferManager.CADeferredStepVolumeCount = 0
	a.BufferManager.CASuspendedVolumeCount = 0
	a.BufferManager.CADroppedVolumeCount = 0
	a.BufferManager.CATotalScheduledSteps = 0
	a.BufferManager.CAAtlasCellCount = 0
	a.BufferManager.CAAtlasByteCount = 0
	a.BufferManager.CAVolumeBindingsDirty = true
}

func caVolumeGPUHosts(volumes []CAVolumeInput) []gpu.CAVolumeHost {
	if len(volumes) == 0 {
		return nil
	}
	hosts := make([]gpu.CAVolumeHost, 0, len(volumes))
	for _, volume := range volumes {
		hosts = append(hosts, gpu.CAVolumeHost{
			EntityID:        volume.EntityID,
			Type:            volume.Type,
			Preset:          volume.Preset,
			Resolution:      volume.Resolution,
			Position:        volume.Position,
			Rotation:        volume.Rotation,
			VoxelScale:      volume.VoxelScale,
			Intensity:       volume.Intensity,
			Diffusion:       volume.Diffusion,
			Buoyancy:        volume.Buoyancy,
			Cooling:         volume.Cooling,
			Dissipation:     volume.Dissipation,
			Extinction:      volume.Extinction,
			Emission:        volume.Emission,
			StepsPending:    volume.StepsPending,
			StepDt:          volume.StepDt,
			ScatterColor:    volume.ScatterColor,
			ShadowTint:      volume.ShadowTint,
			AbsorptionColor: volume.AbsorptionColor,
		})
	}
	return hosts
}

func (a *App) caVolumeRenderPipeline() *wgpu.RenderPipeline {
	resources := a.caVolumeResources()
	if resources == nil {
		return nil
	}
	return resources.RenderPipeline
}

func (a *App) caVolumeSimPipeline() *wgpu.ComputePipeline {
	resources := a.caVolumeResources()
	if resources == nil {
		return nil
	}
	return resources.SimPipeline
}

func (a *App) caVolumeBoundsPipeline() *wgpu.ComputePipeline {
	resources := a.caVolumeResources()
	if resources == nil {
		return nil
	}
	return resources.BoundsPipeline
}
