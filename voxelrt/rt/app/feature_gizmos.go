package app

import (
	"fmt"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

// GizmoFeature owns gizmo pass setup and frame lifecycle.
type GizmoFeature struct{}

type GizmoOverlayItem struct {
	Type        core.GizmoType
	Color       [4]float32
	ModelMatrix mgl32.Mat4
}

type GizmoResources struct {
	Pass *gpu.GizmoRenderPass
}

func (f *GizmoFeature) Name() string {
	return "gizmos"
}

func (f *GizmoFeature) Enabled(*App) bool {
	return true
}

func (f *GizmoFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureGizmosOverlay}
}

func (f *GizmoFeature) GraphScreenStages() []FeatureScreenStage {
	return []FeatureScreenStage{FeatureScreenStagePostResolve}
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
	a.GizmoResources = &GizmoResources{Pass: gizmoPass}

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
	pass := a.gizmoPass()
	if pass == nil || a.Queue == nil || a.Scene == nil {
		return nil
	}
	pass.Update(a.Queue, a.Scene.Gizmos)
	return nil
}

func (f *GizmoFeature) Render(a *App, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	pass := a.gizmoPass()
	if pass == nil || encoder == nil || target == nil || pass.BindGroup == nil {
		return nil
	}
	return a.renderGizmosOverlay(encoder, target)
}

func (a *App) gizmoPass() *gpu.GizmoRenderPass {
	if a == nil || a.GizmoResources == nil {
		return nil
	}
	return a.GizmoResources.Pass
}

func (a *App) SetGizmoOverlayItems(items []GizmoOverlayItem) {
	if a == nil || a.Scene == nil {
		return
	}
	a.Scene.Gizmos = a.Scene.Gizmos[:0]
	for _, item := range items {
		a.Scene.Gizmos = append(a.Scene.Gizmos, core.Gizmo{
			Type:        item.Type,
			Color:       item.Color,
			ModelMatrix: item.ModelMatrix,
		})
	}
}

func (a *App) ClearGizmoOverlayItems() {
	if a == nil || a.Scene == nil {
		return
	}
	a.Scene.Gizmos = a.Scene.Gizmos[:0]
}

func (a *App) gizmosOverlayReady() bool {
	pass := a.gizmoPass()
	return a != nil &&
		pass != nil &&
		pass.BindGroup != nil &&
		pass.DepthBindGroup != nil &&
		a.Scene != nil &&
		len(a.Scene.Gizmos) > 0
}

func (a *App) gizmosOverlayGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureGizmosOverlay) &&
		a.gizmosOverlayReady()
}

func (a *App) recordGizmosOverlayPass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.gizmosOverlayGraphNodeEnabled() {
		return nil
	}
	a.Profiler.SetCount("GizmosGraphNode", 1)
	a.Profiler.SetCount("Gizmos", len(a.Scene.Gizmos))
	pass := a.gizmoPass()
	a.Profiler.SetCount("GizmosBGReady", boolToCount(pass != nil && pass.BindGroup != nil))
	a.Profiler.SetCount("GizmosDepthBGReady", boolToCount(pass != nil && pass.DepthBindGroup != nil))

	if encoder == nil {
		return fmt.Errorf("gizmos command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("gizmos frame context is nil")
	}
	if frame.SwapchainView == nil {
		return fmt.Errorf("gizmos swapchain view is nil")
	}
	return a.renderGizmosOverlay(encoder, frame.SwapchainView)
}

func (a *App) renderGizmosOverlay(encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	pass := a.gizmoPass()
	if pass == nil || encoder == nil || target == nil || pass.BindGroup == nil {
		return nil
	}

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    target,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	pass.Draw(renderPass, pass.BindGroup, pass.DepthBindGroup)
	if err := renderPass.End(); err != nil {
		return fmt.Errorf("gizmo render pass end failed: %w", err)
	}
	return nil
}

func (f *GizmoFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.GizmoResources = nil
}

func (f *GizmoFeature) HasScreenStage(a *App, stage FeatureScreenStage) bool {
	return stage == FeatureScreenStagePostResolve &&
		a != nil &&
		a.gizmosOverlayReady()
}

func (f *GizmoFeature) rebuildBindGroups(a *App, phase string) error {
	pass := a.gizmoPass()
	if pass == nil || a.BufferManager == nil || a.BufferManager.CameraBuf == nil || a.BufferManager.DepthView == nil {
		return nil
	}

	cameraBG, err := pass.CreateBindGroup(a.BufferManager.CameraBuf)
	if err != nil {
		fmt.Printf("ERROR: Failed to %s Gizmo BindGroup: %v\n", phase, err)
		return nil
	}
	depthBG, err := pass.CreateDepthBindGroup(a.BufferManager.DepthView)
	if err != nil {
		fmt.Printf("ERROR: Failed to %s Gizmo Depth BindGroup: %v\n", phase, err)
		return nil
	}
	pass.BindGroup = cameraBG
	pass.DepthBindGroup = depthBG
	return nil
}
