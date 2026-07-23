package app

import (
	"fmt"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

// BeamFeature owns depth-aware camera-facing beam rendering.
type BeamFeature struct{}

type BeamResources struct {
	Pipeline *wgpu.RenderPipeline
}

// BeamInstanceInput matches BeamInstance in beams.wgsl.
type BeamInstanceInput struct {
	StartWidth      [4]float32
	EndCoreFraction [4]float32
	CoreColor       [4]float32
	HaloColor       [4]float32
}

func (*BeamFeature) Name() string { return "beams" }

func (*BeamFeature) GraphNodeNames() []string {
	return []string{RenderNodeCoreAccumulation}
}

func (*BeamFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (*BeamFeature) Enabled(*App) bool { return true }

func (*BeamFeature) Setup(a *App) error {
	if a != nil {
		a.setupBeamsPipeline()
	}
	return nil
}

func (*BeamFeature) Resize(a *App, _, _ uint32) error {
	if a != nil {
		a.setupBeamsPipeline()
	}
	return nil
}

func (*BeamFeature) OnSceneBuffersRecreated(a *App) error {
	if a != nil && a.BufferManager != nil {
		a.BufferManager.RebuildBeamBindGroups(a.beamPipeline())
	}
	return nil
}

func (*BeamFeature) Update(*App) error { return nil }

func (*BeamFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error { return nil }

func (*BeamFeature) Shutdown(a *App) {
	if a != nil {
		a.BeamResources = nil
	}
}

func (*BeamFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		a.beamPipeline() != nil &&
		a.BufferManager.HasBeamContribution()
}

func (*BeamFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation || a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	pipeline := a.beamPipeline()
	if pipeline == nil || !a.BufferManager.HasBeamContribution() {
		return nil
	}
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, a.BufferManager.BeamsBindGroup0, nil)
	pass.SetBindGroup(1, a.BufferManager.BeamsBindGroup1, nil)
	pass.Draw(6, a.BufferManager.BeamCount, 0, 0)
	return nil
}

func (a *App) BeamPipeline() *wgpu.RenderPipeline { return a.beamPipeline() }

func (a *App) beamPipeline() *wgpu.RenderPipeline {
	if a == nil || a.BeamResources == nil {
		return nil
	}
	return a.BeamResources.Pipeline
}

func (a *App) ApplyBeamInput(instances []BeamInstanceInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	data, count := beamInstanceBytes(instances)
	a.BufferManager.UpdateBeams(data, count)
	a.BufferManager.SyncBeamBindGroups(a.beamPipeline())
}

func (a *App) ClearBeamInput() {
	if a != nil && a.BufferManager != nil {
		a.BufferManager.BeamCount = 0
	}
}

func beamInstanceBytes(instances []BeamInstanceInput) ([]byte, uint32) {
	count := uint32(len(instances))
	if count == 0 {
		return nil, 0
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), len(instances)*int(unsafe.Sizeof(BeamInstanceInput{}))), count
}

func (a *App) setupBeamsPipeline() {
	if a == nil || a.Device == nil {
		return
	}
	module, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Depth-aware Beams",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.BeamsWGSL},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create beam shader module: %v\n", err)
		return
	}
	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Beams BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type:           wgpu.BufferBindingTypeUniform,
					MinBindingSize: gpu_rt.CameraUniformSizeBytes,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type: wgpu.BufferBindingTypeReadOnlyStorage,
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create beams BGL0: %v\n", err)
		return
	}
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Beams BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageFragment,
			Texture: wgpu.TextureBindingLayout{
				SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
				ViewDimension: wgpu.TextureViewDimension2D,
			},
		}},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create beams BGL1: %v\n", err)
		return
	}
	layout, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create beams pipeline layout: %v\n", err)
		return
	}
	additive := &wgpu.BlendState{
		Color: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorOne, Operation: wgpu.BlendOperationAdd},
		Alpha: wgpu.BlendComponent{SrcFactor: wgpu.BlendFactorOne, DstFactor: wgpu.BlendFactorOne, Operation: wgpu.BlendOperationAdd},
	}
	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Beams Pipeline",
		Layout: layout,
		Vertex: wgpu.VertexState{Module: module, EntryPoint: "vs_main"},
		Fragment: &wgpu.FragmentState{
			Module:     module,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{Format: wgpu.TextureFormatRGBA16Float, Blend: additive, WriteMask: wgpu.ColorWriteMaskAll},
				{Format: wgpu.TextureFormatR16Float, Blend: additive, WriteMask: wgpu.ColorWriteMaskAll},
			},
		},
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		fmt.Printf("ERROR: Failed to create beam render pipeline: %v\n", err)
		return
	}
	a.BeamResources = &BeamResources{Pipeline: pipeline}
	if a.BufferManager != nil {
		a.BufferManager.RebuildBeamBindGroups(pipeline)
	}
}
