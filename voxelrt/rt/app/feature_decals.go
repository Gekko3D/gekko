package app

import (
	"fmt"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

// DecalInstanceInput is the 80-byte GPU record consumed by the decal pass.
type DecalInstanceInput struct {
	Position, Rotation, HalfExtents, Color [4]float32
	Atlas                                  [4]uint32
}

var _ [80]byte = [unsafe.Sizeof(DecalInstanceInput{})]byte{}

type DecalBatchInput struct {
	AtlasKey                     string
	FirstInstance, InstanceCount uint32
}

// DecalFeature owns the opt-in target and volume pass.
type DecalFeature struct{}
type DecalResources struct{ Pipeline *wgpu.RenderPipeline }

func (*DecalFeature) Name() string      { return "decals" }
func (*DecalFeature) Enabled(*App) bool { return true }
func (*DecalFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureDecals}
}
func (*DecalFeature) GraphCommandStages() []FeatureCommandStage {
	return []FeatureCommandStage{FeatureCommandStagePreLighting}
}
func (*DecalFeature) Setup(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.setupDecalsPipeline()
	a.BufferManager.CreateDecalFallback()
	if a.Config != nil {
		a.BufferManager.CreateDecalTargets(a.Config.Width, a.Config.Height)
	}
	a.rebuildDecalLightingBinding()
	return nil
}
func (*DecalFeature) Resize(a *App, w, h uint32) error {
	if a != nil && a.BufferManager != nil {
		a.BufferManager.CreateDecalTargets(w, h)
		a.rebuildDecalLightingBinding()
	}
	return nil
}
func (*DecalFeature) OnSceneBuffersRecreated(a *App) error {
	if a != nil && a.BufferManager != nil {
		a.BufferManager.RebuildDecalBindGroups(a.decalPipeline())
	}
	return nil
}
func (*DecalFeature) Update(*App) error                                          { return nil }
func (*DecalFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error { return nil }
func (*DecalFeature) Shutdown(a *App) {
	if a != nil {
		if a.BufferManager != nil {
			a.BufferManager.ReleaseDecals()
			a.rebuildDecalLightingBinding()
		}
		a.DecalResources = nil
	}
}
func (*DecalFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	return stage == FeatureCommandStagePreLighting && a != nil && a.BufferManager != nil && a.decalPipeline() != nil && a.BufferManager.DecalView != nil
}
func (*DecalFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if stage != FeatureCommandStagePreLighting || a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	m, p := a.BufferManager, a.decalPipeline()
	if p == nil || m.DecalView == nil {
		return nil
	}
	a.Profiler.SetCount("DecalCount", int(m.DecalCount))
	a.Profiler.SetCount("DecalAtlasBatches", len(m.DecalBatches))
	a.Profiler.SetCount("DecalDrawCalls", 0)
	a.Profiler.SetCount("DecalTargetReady", 1)
	a.Profiler.SetCount("DecalBindingsReady", boolToCount(m.HasDecalContribution()))
	a.Profiler.SetCount("DecalPassRecorded", 1)
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{ColorAttachments: []wgpu.RenderPassColorAttachment{{View: m.DecalView, LoadOp: wgpu.LoadOpClear, StoreOp: wgpu.StoreOpStore}}})
	if m.HasDecalContribution() && m.DecalGBufferBindGroup(p) != nil {
		pass.SetPipeline(p)
		pass.SetBindGroup(1, m.DecalGBufferBindGroup(p), nil)
		draws := 0
		for _, b := range m.DecalBatches {
			if b.BindGroup0 != nil && b.InstanceCount > 0 {
				pass.SetBindGroup(0, b.BindGroup0, nil)
				pass.Draw(36, b.InstanceCount, 0, b.FirstInstance)
				draws++
			}
		}
		a.Profiler.SetCount("DecalDrawCalls", draws)
	}
	if err := pass.End(); err != nil {
		return fmt.Errorf("decal pass End failed: %w", err)
	}
	return nil
}

func (a *App) recordDecalPass(encoder *wgpu.CommandEncoder) error {
	if a == nil || !a.hasFeatureGraphNode(RenderNodeFeatureDecals) {
		return nil
	}
	return (&DecalFeature{}).DispatchCommandStage(a, FeatureCommandStagePreLighting, encoder)
}

func (a *App) rebuildDecalLightingBinding() {
	if a == nil || a.BufferManager == nil || a.LightingPipeline == nil || a.StorageView == nil {
		return
	}
	a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, a.StorageView)
}

func (a *App) DecalPipeline() *wgpu.RenderPipeline { return a.decalPipeline() }
func (a *App) decalPipeline() *wgpu.RenderPipeline {
	if a == nil || a.DecalResources == nil {
		return nil
	}
	return a.DecalResources.Pipeline
}
func (a *App) ApplyDecalInput(instances []DecalInstanceInput, batches []DecalBatchInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	data, count := decalInstanceBytes(instances)
	a.BufferManager.UpdateDecals(data, count)
	a.BufferManager.SyncDecalBatches(a.decalPipeline(), decalBatchDescs(batches))
}
func (a *App) ClearDecalInput() {
	if a != nil && a.BufferManager != nil {
		a.BufferManager.UpdateDecals(nil, 0)
		a.BufferManager.SyncDecalBatches(a.decalPipeline(), nil)
	}
}
func decalInstanceBytes(instances []DecalInstanceInput) ([]byte, uint32) {
	if len(instances) == 0 {
		return nil, 0
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), len(instances)*int(unsafe.Sizeof(DecalInstanceInput{}))), uint32(len(instances))
}
func decalBatchDescs(batches []DecalBatchInput) []gpu.DecalBatchDesc {
	descs := make([]gpu.DecalBatchDesc, 0, len(batches))
	for _, b := range batches {
		descs = append(descs, gpu.DecalBatchDesc{AtlasKey: b.AtlasKey, FirstInstance: b.FirstInstance, InstanceCount: b.InstanceCount})
	}
	return descs
}

func (a *App) setupDecalsPipeline() {
	if a == nil || a.Device == nil {
		return
	}
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "Deferred Decals", WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.DecalsWGSL}})
	if err != nil {
		return
	}
	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "Decals BGL0", Entries: []wgpu.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: gpu.CameraUniformSizeBytes}}, {Binding: 1, Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage}}, {Binding: 2, Visibility: wgpu.ShaderStageFragment, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeFloat, ViewDimension: wgpu.TextureViewDimension2D}}, {Binding: 3, Visibility: wgpu.ShaderStageFragment, Sampler: wgpu.SamplerBindingLayout{Type: wgpu.SamplerBindingTypeFiltering}}}})
	if err != nil {
		return
	}
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "Decals BGL1", Entries: []wgpu.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}}, {Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}}}})
	if err != nil {
		return
	}
	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1}})
	if err != nil {
		return
	}
	blend := &wgpu.BlendState{Color: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorOneMinusSrcAlpha, Operation: wgpu.BlendOperationAdd}, Alpha: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorOneMinusSrcAlpha, Operation: wgpu.BlendOperationAdd}}
	p, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{Label: "Deferred Decals Pipeline", Layout: layout, Vertex: wgpu.VertexState{Module: module, EntryPoint: "vs_main"}, Fragment: &wgpu.FragmentState{Module: module, EntryPoint: "fs_main", Targets: []wgpu.ColorTargetState{{Format: wgpu.TextureFormatRGBA8Unorm, Blend: blend, WriteMask: wgpu.ColorWriteMaskAll}}}, Primitive: wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList, CullMode: wgpu.CullModeNone}, Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF}})
	if err != nil {
		return
	}
	a.DecalResources = &DecalResources{Pipeline: p}
	if a.BufferManager != nil {
		a.BufferManager.RebuildDecalBindGroups(p)
	}
}
