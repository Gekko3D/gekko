package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

// GizmoFeature owns gizmo pass setup and frame lifecycle.
type GizmoFeature struct{}

func (f *GizmoFeature) Name() string {
	return "gizmos"
}

func (f *GizmoFeature) Enabled(*App) bool {
	return true
}

func (f *GizmoFeature) Setup(a *App) error {
	if a == nil || a.Device == nil || a.Config == nil {
		return nil
	}

	gizmoPass, err := gpu.NewGizmoRenderPass(a.Device, a.Config.Format)
	if err != nil {
		// Preserve prior behavior: log gizmo setup failures but keep renderer alive.
		fmt.Printf("ERROR: Failed to create Gizmo pass: %v\n", err)
		return nil
	}
	a.GizmoPass = gizmoPass

	if err := f.rebuildBindGroups(a, "create"); err != nil {
		return err
	}
	return nil
}

func (f *GizmoFeature) Resize(a *App, _, _ uint32) error {
	return f.rebuildBindGroups(a, "resize")
}

func (f *GizmoFeature) OnSceneBuffersRecreated(a *App) error {
	return f.rebuildBindGroups(a, "recreate")
}

func (f *GizmoFeature) Update(a *App) error {
	if a == nil || a.GizmoPass == nil || a.Queue == nil || a.Scene == nil {
		return nil
	}
	a.GizmoPass.Update(a.Queue, a.Scene.Gizmos)
	return nil
}

func (f *GizmoFeature) Render(a *App, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil || encoder == nil || target == nil || a.GizmoPass == nil || a.GizmoPass.BindGroup == nil {
		return nil
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    target,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	a.GizmoPass.Draw(pass, a.GizmoPass.BindGroup, a.GizmoPass.DepthBindGroup)
	if err := pass.End(); err != nil {
		return fmt.Errorf("gizmo render pass end failed: %w", err)
	}
	return nil
}

func (f *GizmoFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.GizmoPass = nil
}

func (f *GizmoFeature) rebuildBindGroups(a *App, phase string) error {
	if a == nil || a.GizmoPass == nil || a.BufferManager == nil || a.BufferManager.CameraBuf == nil || a.BufferManager.DepthView == nil {
		return nil
	}

	cameraBG, err := a.GizmoPass.CreateBindGroup(a.BufferManager.CameraBuf)
	if err != nil {
		fmt.Printf("ERROR: Failed to %s Gizmo BindGroup: %v\n", phase, err)
		return nil
	}
	depthBG, err := a.GizmoPass.CreateDepthBindGroup(a.BufferManager.DepthView)
	if err != nil {
		fmt.Printf("ERROR: Failed to %s Gizmo Depth BindGroup: %v\n", phase, err)
		return nil
	}
	a.GizmoPass.BindGroup = cameraBG
	a.GizmoPass.DepthBindGroup = depthBG
	return nil
}
