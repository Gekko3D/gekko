package app

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// TextFeature owns text/debug overlay setup and frame lifecycle.
type TextFeature struct{}

type TextOverlayItem struct {
	Text     string
	Position [2]float32
	Scale    float32
	Color    [4]float32
}

type TextResources struct {
	Renderer     *core.TextRenderer
	Pipeline     *wgpu.RenderPipeline
	AtlasView    *wgpu.TextureView
	BindGroup    *wgpu.BindGroup
	VertexBuffer *wgpu.Buffer
	Items        []core.TextItem
	RectItems    []core.RectItem
	VertexCount  uint32
}

func (f *TextFeature) Name() string {
	return "text"
}

func (f *TextFeature) Enabled(*App) bool {
	return true
}

func (f *TextFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureTextOverlay}
}

func (f *TextFeature) GraphScreenStages() []FeatureScreenStage {
	return []FeatureScreenStage{FeatureScreenStagePostResolve}
}

func (f *TextFeature) Setup(a *App) error {
	if a == nil || a.Device == nil || a.Queue == nil || a.Config == nil {
		return nil
	}

	fontPath := a.FontPath
	if fontPath == "" {
		fontPath = f.resolveDefaultFontPath()
	}

	textRenderer, err := core.NewTextRenderer(fontPath, a.effectiveUIFontSize())
	if err != nil {
		// Preserve existing behavior: renderer continues even if text setup fails.
		fmt.Printf("WARNING: Failed to initialize text renderer: %v\n", err)
		return nil
	}
	a.ensureTextResources().Renderer = textRenderer
	a.setupTextResources()
	return nil
}

func (f *TextFeature) Resize(a *App, _, _ uint32) error {
	if a == nil || a.textRenderer() == nil {
		return nil
	}
	a.setupTextResources()
	return nil
}

func (f *TextFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *TextFeature) Update(a *App) error {
	if a == nil {
		return nil
	}

	if a.DebugMode {
		stats := fmt.Sprintf(
			"FPS: %.1f\nRender Mode: %s\n%s",
			a.FPS,
			renderModeLabel(a.RenderMode),
			a.PreviousProfilerStats,
		)
		if a.ShadowUpdateSummary != "" {
			stats += fmt.Sprintf("\nShadow Refresh:\n  %s\n", a.ShadowUpdateSummary)
		}
		x := float32(a.Config.Width) - 260
		a.DrawText(stats, x, 10, 0.6, [4]float32{1, 1, 0, 1})
	}

	textResources := a.textResources()
	if textResources == nil || (len(textResources.Items) == 0 && len(textResources.RectItems) == 0) || textResources.Renderer == nil || a.Device == nil || a.Queue == nil {
		return nil
	}

	vertices := textResources.Renderer.BuildVertices(textResources.Items, textResources.RectItems, int(a.Config.Width), int(a.Config.Height))
	if len(vertices) == 0 {
		textResources.VertexCount = 0
		return nil
	}
	vSize := uint64(len(vertices) * int(unsafe.Sizeof(core.TextVertex{})))
	if textResources.VertexBuffer == nil || textResources.VertexBuffer.GetSize() < vSize {
		if textResources.VertexBuffer != nil {
			textResources.VertexBuffer.Release()
		}
		var err error
		textResources.VertexBuffer, err = a.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Text VB",
			Size:  vSize,
			Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return fmt.Errorf("create text vertex buffer: %w", err)
		}
	}
	a.Queue.WriteBuffer(textResources.VertexBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), vSize))
	textResources.VertexCount = uint32(len(vertices))
	return nil
}

func (f *TextFeature) Render(a *App, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil || encoder == nil || target == nil {
		return nil
	}
	return a.renderTextOverlay(encoder, target)
}

func (a *App) textOverlayReady() bool {
	textResources := a.textResources()
	return a != nil &&
		textResources != nil &&
		textResources.VertexCount > 0 &&
		textResources.VertexBuffer != nil &&
		textResources.Pipeline != nil &&
		textResources.BindGroup != nil
}

func (a *App) textOverlayGraphNodeEnabled() bool {
	return a != nil &&
		a.hasFeatureGraphNode(RenderNodeFeatureTextOverlay) &&
		a.textOverlayReady()
}

func (a *App) recordTextOverlayPass(encoder *wgpu.CommandEncoder, frame *FrameContext) error {
	if !a.textOverlayGraphNodeEnabled() {
		return nil
	}
	textResources := a.textResources()
	a.Profiler.SetCount("TextGraphNode", 1)
	a.Profiler.SetCount("TextVertices", int(textResources.VertexCount))
	a.Profiler.SetCount("TextPipelineReady", boolToCount(textResources.Pipeline != nil))
	a.Profiler.SetCount("TextBGReady", boolToCount(textResources.BindGroup != nil))

	if encoder == nil {
		return fmt.Errorf("text command encoder is nil")
	}
	if frame == nil {
		return fmt.Errorf("text frame context is nil")
	}
	if frame.SwapchainView == nil {
		return fmt.Errorf("text swapchain view is nil")
	}
	return a.renderTextOverlay(encoder, frame.SwapchainView)
}

func (a *App) renderTextOverlay(encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil || encoder == nil || target == nil {
		return nil
	}
	textResources := a.textResources()
	if textResources == nil || len(textResources.Items) == 0 || textResources.VertexBuffer == nil || textResources.Pipeline == nil || textResources.BindGroup == nil {
		return nil
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    target,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	pass.SetPipeline(textResources.Pipeline)
	pass.SetBindGroup(0, textResources.BindGroup, nil)
	pass.SetVertexBuffer(0, textResources.VertexBuffer, 0, textResources.VertexBuffer.GetSize())
	pass.Draw(textResources.VertexCount, 1, 0, 0)
	if err := pass.End(); err != nil {
		return fmt.Errorf("text render pass end failed: %w", err)
	}
	return nil
}

func (f *TextFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.TextResources = nil
}

func (f *TextFeature) HasScreenStage(a *App, stage FeatureScreenStage) bool {
	return stage == FeatureScreenStagePostResolve &&
		a != nil &&
		a.textOverlayReady()
}

func (a *App) textResources() *TextResources {
	if a == nil {
		return nil
	}
	return a.TextResources
}

func (a *App) ensureTextResources() *TextResources {
	if a == nil {
		return nil
	}
	if a.TextResources == nil {
		a.TextResources = &TextResources{}
	}
	return a.TextResources
}

func (a *App) SetTextOverlayItems(items []TextOverlayItem) {
	if a == nil {
		return
	}
	textResources := a.ensureTextResources()
	textResources.Items = textResources.Items[:0]
	a.AppendTextOverlayItems(items)
}

func (a *App) AppendTextOverlayItems(items []TextOverlayItem) {
	if a == nil {
		return
	}
	textResources := a.ensureTextResources()
	for _, item := range items {
		textResources.Items = append(textResources.Items, core.TextItem{
			Text:     item.Text,
			Position: item.Position,
			Scale:    item.Scale,
			Color:    item.Color,
		})
	}
	textResources.VertexCount = 0
}

func (a *App) textRenderer() *core.TextRenderer {
	resources := a.textResources()
	if resources == nil {
		return nil
	}
	return resources.Renderer
}

func (f *TextFeature) resolveDefaultFontPath() string {
	candidates := []string{
		"gekko/voxelrt/rt/fonts/Roboto-Medium.ttf",       // Root
		"../gekko/voxelrt/rt/fonts/Roboto-Medium.ttf",    // From subfolders like actiongame
		"../../gekko/voxelrt/rt/fonts/Roboto-Medium.ttf", // From examples/pbr_gallery etc.
		"assets/Roboto-Medium.ttf",                       // Local assets
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "Roboto-Medium.ttf"
}
