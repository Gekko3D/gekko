package app

import "github.com/cogentcore/webgpu/wgpu"

// CAVolumeFeature owns CA volume simulation, bounds, and accumulation rendering.
type CAVolumeFeature struct{}

func (f *CAVolumeFeature) Name() string {
	return "ca-volumes"
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
	a.CAVolumePipeline = nil
	a.CAVolumeSimPipeline = nil
	a.CAVolumeBoundsPipeline = nil
}

func (f *CAVolumeFeature) DispatchCommandStage(a *App, stage FeatureCommandStage, encoder *wgpu.CommandEncoder) error {
	if a == nil || encoder == nil || a.BufferManager == nil {
		return nil
	}
	switch stage {
	case FeatureCommandStagePreGBufferVolumes:
		a.BufferManager.DispatchCAVolumeSim(encoder, a.CAVolumeSimPipeline)
		a.BufferManager.DispatchCAVolumeBounds(encoder, a.CAVolumeBoundsPipeline)
	case FeatureCommandStagePostLighting:
		if a.BufferManager.CAVolumeColorView == nil || a.BufferManager.CAVolumeDepthView == nil {
			a.HadCAVolumePass = false
			return nil
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
					ClearValue: wgpu.Color{R: volumetricClearDepth, G: 0, B: 0, A: 0},
				},
			},
		})
		defer pass.End()
		if a.CAVolumePipeline == nil || a.BufferManager.CAVolumeRenderBG0 == nil || a.BufferManager.CAVolumeRenderBG2 == nil || a.BufferManager.CurrentCAVolumeRenderBG1() == nil {
			a.HadCAVolumePass = false
			return nil
		}
		if a.BufferManager.CAVolumeVisibleCount == 0 {
			a.HadCAVolumePass = false
			return nil
		}
		volumes := a.BufferManager.CurrentCAVolumes()
		if len(volumes) == 0 {
			a.HadCAVolumePass = false
			return nil
		}
		candidates := buildCAVolumeRenderCandidates(a.Camera, a.BufferManager.VolumetricWidth, a.BufferManager.VolumetricHeight, volumes)
		if len(candidates) == 0 {
			a.HadCAVolumePass = false
			return nil
		}

		pass.SetPipeline(a.CAVolumePipeline)
		pass.SetBindGroup(0, a.BufferManager.CAVolumeRenderBG0, nil)
		pass.SetBindGroup(1, a.BufferManager.CurrentCAVolumeRenderBG1(), nil)
		pass.SetBindGroup(2, a.BufferManager.CAVolumeRenderBG2, nil)
		for _, candidate := range candidates {
			pass.SetScissorRect(candidate.Scissor.X, candidate.Scissor.Y, candidate.Scissor.W, candidate.Scissor.H)
			pass.Draw(3, 1, 0, uint32(candidate.VolumeIndex))
		}
		a.HadCAVolumePass = true
		return nil
	}
	return nil
}

func (f *CAVolumeFeature) HasCommandStage(a *App, stage FeatureCommandStage) bool {
	if a == nil || a.BufferManager == nil {
		return false
	}
	switch stage {
	case FeatureCommandStagePreGBufferVolumes:
		return a.CAVolumeSimPipeline != nil &&
			a.CAVolumeBoundsPipeline != nil &&
			a.BufferManager.CAVolumeCount > 0
	case FeatureCommandStagePostLighting:
		return a.BufferManager.CAVolumeColorView != nil &&
			a.BufferManager.CAVolumeDepthView != nil &&
			(a.BufferManager.CAVolumeVisibleCount > 0 || a.HadCAVolumePass)
	default:
		return false
	}
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
	if a.CAVolumeSimPipeline != nil {
		a.BufferManager.CreateCAVolumeSimBindGroups()
	}
	if a.CAVolumeBoundsPipeline != nil {
		a.BufferManager.CreateCAVolumeBoundsBindGroups()
	}
	if a.CAVolumePipeline != nil {
		a.BufferManager.CreateCAVolumeRenderBindGroups(a.CAVolumePipeline)
	}
}
