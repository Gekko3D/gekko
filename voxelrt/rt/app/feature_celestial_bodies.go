package app

import (
	"fmt"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

type CelestialBodyRenderData struct {
	CenterRadius    [4]float32
	SurfaceColor    [4]float32
	AtmosphereColor [4]float32
	CloudColor      [4]float32
	Params          [4]float32
	Noise           [4]float32
	ArtPrimary      [4]float32
	ArtSecondary    [4]float32
	ArtTertiary     [4]float32
	Flags           [4]float32
}

type celestialBodyParams struct {
	BodyCount   uint32
	TimeSeconds float32
	_           [2]uint32
}

type CelestialBodiesFeature struct {
	paramsBuf *wgpu.Buffer
	bodyBuf   *wgpu.Buffer
	bg0       *wgpu.BindGroup
	bg1       *wgpu.BindGroup
}

func (f *CelestialBodiesFeature) Name() string {
	return "celestial-bodies"
}

func (f *CelestialBodiesFeature) Enabled(*App) bool {
	return true
}

func (f *CelestialBodiesFeature) Setup(a *App) error {
	if a == nil || a.Device == nil || a.BufferManager == nil || a.Config == nil {
		return nil
	}
	if err := f.ensureBuffers(a, 1); err != nil {
		return err
	}
	if err := f.setupPipeline(a); err != nil {
		return err
	}
	return f.rebuildBindGroups(a)
}

func (f *CelestialBodiesFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	return f.rebuildBindGroups(a)
}

func (f *CelestialBodiesFeature) OnSceneBuffersRecreated(a *App) error {
	if a == nil {
		return nil
	}
	return f.rebuildBindGroups(a)
}

func (f *CelestialBodiesFeature) Update(a *App) error {
	if a == nil || a.Device == nil || a.Queue == nil {
		return nil
	}
	count := len(a.CelestialBodies)
	if err := f.ensureBuffers(a, count); err != nil {
		return err
	}
	params := celestialBodyParams{
		BodyCount:   uint32(count),
		TimeSeconds: float32(a.RenderFrameIndex) * (1.0 / 60.0),
	}
	a.Queue.WriteBuffer(f.paramsBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&params)), unsafe.Sizeof(params)))
	if count > 0 {
		size := uintptr(count) * unsafe.Sizeof(CelestialBodyRenderData{})
		a.Queue.WriteBuffer(f.bodyBuf, 0, unsafe.Slice((*byte)(unsafe.Pointer(&a.CelestialBodies[0])), size))
	}
	return nil
}

func (f *CelestialBodiesFeature) Render(a *App, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil || encoder == nil || target == nil || len(a.CelestialBodies) == 0 {
		return nil
	}
	if a.CelestialPipeline == nil || f.bg0 == nil || f.bg1 == nil {
		return nil
	}
	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    target,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	pass.SetPipeline(a.CelestialPipeline)
	pass.SetBindGroup(0, f.bg0, nil)
	pass.SetBindGroup(1, f.bg1, nil)
	pass.Draw(3, 1, 0, 0)
	if err := pass.End(); err != nil {
		return fmt.Errorf("celestial bodies render pass end failed: %w", err)
	}
	return nil
}

func (f *CelestialBodiesFeature) Shutdown(a *App) {
	if a != nil {
		a.CelestialPipeline = nil
	}
	f.paramsBuf = nil
	f.bodyBuf = nil
	f.bg0 = nil
	f.bg1 = nil
}

func (f *CelestialBodiesFeature) HasScreenStage(a *App, stage FeatureScreenStage) bool {
	return stage == FeatureScreenStagePostResolve &&
		a != nil &&
		len(a.CelestialBodies) > 0 &&
		a.CelestialPipeline != nil &&
		f.bg0 != nil &&
		f.bg1 != nil
}

func (f *CelestialBodiesFeature) ensureBuffers(a *App, count int) error {
	if count < 1 {
		count = 1
	}
	if f.paramsBuf == nil {
		buf, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Celestial Params",
			Size:  uint64(unsafe.Sizeof(celestialBodyParams{})),
			Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return fmt.Errorf("create celestial params buffer: %w", err)
		}
		f.paramsBuf = buf
	}
	required := uint64(uintptr(count) * unsafe.Sizeof(CelestialBodyRenderData{}))
	if required == 0 {
		required = uint64(unsafe.Sizeof(CelestialBodyRenderData{}))
	}
	if f.bodyBuf == nil || f.bodyBuf.GetSize() < required {
		buf, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Celestial Bodies",
			Size:  required,
			Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return fmt.Errorf("create celestial body buffer: %w", err)
		}
		f.bodyBuf = buf
		f.bg0 = nil
	}
	return nil
}

func (f *CelestialBodiesFeature) setupPipeline(a *App) error {
	mod, err := a.Device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "Celestial Bodies",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.CelestialBodiesWGSL},
	})
	if err != nil {
		return fmt.Errorf("create celestial shader module: %w", err)
	}

	bgl0, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Celestial BGL0",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: 288},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeUniform, MinBindingSize: uint64(unsafe.Sizeof(celestialBodyParams{}))},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageFragment,
				Buffer:     wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeReadOnlyStorage},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create celestial bgl0: %w", err)
	}
	bgl1, err := a.Device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "Celestial BGL1",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create celestial bgl1: %w", err)
	}
	pl, err := a.Device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bgl0, bgl1},
	})
	if err != nil {
		return fmt.Errorf("create celestial pipeline layout: %w", err)
	}
	pipeline, err := a.Device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Celestial Bodies Pipeline",
		Layout: pl,
		Vertex: wgpu.VertexState{
			Module:     mod,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     mod,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format: a.Config.Format,
				Blend: &wgpu.BlendState{
					Color: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorSrcAlpha,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						Operation: wgpu.BlendOperationAdd,
					},
					Alpha: wgpu.BlendComponent{
						SrcFactor: wgpu.BlendFactorOne,
						DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						Operation: wgpu.BlendOperationAdd,
					},
				},
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
	})
	if err != nil {
		return fmt.Errorf("create celestial pipeline: %w", err)
	}
	a.CelestialPipeline = pipeline
	return nil
}

func (f *CelestialBodiesFeature) rebuildBindGroups(a *App) error {
	if a == nil || a.BufferManager == nil || a.BufferManager.CameraBuf == nil || a.BufferManager.DepthView == nil || a.CelestialPipeline == nil || f.paramsBuf == nil || f.bodyBuf == nil {
		return nil
	}
	bg0, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.CelestialPipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: a.BufferManager.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: f.paramsBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: f.bodyBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		return fmt.Errorf("create celestial bg0: %w", err)
	}
	bg1, err := a.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: a.CelestialPipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: a.BufferManager.DepthView},
		},
	})
	if err != nil {
		return fmt.Errorf("create celestial bg1: %w", err)
	}
	f.bg0 = bg0
	f.bg1 = bg1
	return nil
}
