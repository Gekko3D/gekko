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

func (f *TextFeature) Name() string {
	return "text"
}

func (f *TextFeature) Enabled(*App) bool {
	return true
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
	a.TextRenderer = textRenderer
	a.setupTextResources()
	return nil
}

func (f *TextFeature) Resize(a *App, _, _ uint32) error {
	if a == nil || a.TextRenderer == nil {
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

	if len(a.TextItems) == 0 || a.TextRenderer == nil || a.Device == nil || a.Queue == nil {
		return nil
	}

	vertices := a.TextRenderer.BuildVertices(a.TextItems, int(a.Config.Width), int(a.Config.Height))
	if len(vertices) == 0 {
		return nil
	}
	vSize := uint64(len(vertices) * int(unsafe.Sizeof(core.TextVertex{})))
	if a.TextVertexBuffer == nil || a.TextVertexBuffer.GetSize() < vSize {
		if a.TextVertexBuffer != nil {
			a.TextVertexBuffer.Release()
		}
		var err error
		a.TextVertexBuffer, err = a.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "Text VB",
			Size:  vSize,
			Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
		if err != nil {
			return fmt.Errorf("create text vertex buffer: %w", err)
		}
	}
	a.Queue.WriteBuffer(a.TextVertexBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), vSize))
	a.TextVertexCount = uint32(len(vertices))
	return nil
}

func (f *TextFeature) Render(a *App, encoder *wgpu.CommandEncoder, target *wgpu.TextureView) error {
	if a == nil || encoder == nil || target == nil {
		return nil
	}
	if len(a.TextItems) == 0 || a.TextVertexBuffer == nil || a.TextPipeline == nil || a.TextBindGroup == nil {
		return nil
	}

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:    target,
			LoadOp:  wgpu.LoadOpLoad,
			StoreOp: wgpu.StoreOpStore,
		}},
	})
	pass.SetPipeline(a.TextPipeline)
	pass.SetBindGroup(0, a.TextBindGroup, nil)
	pass.SetVertexBuffer(0, a.TextVertexBuffer, 0, a.TextVertexBuffer.GetSize())
	pass.Draw(a.TextVertexCount, 1, 0, 0)
	if err := pass.End(); err != nil {
		return fmt.Errorf("text render pass end failed: %w", err)
	}
	return nil
}

func (f *TextFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.TextRenderer = nil
	a.TextPipeline = nil
	a.TextBindGroup = nil
	a.TextAtlasView = nil
	a.TextVertexBuffer = nil
	a.TextItems = nil
	a.TextVertexCount = 0
}

func (f *TextFeature) resolveDefaultFontPath() string {
	candidates := []string{
		"gekko/voxelrt/rt/fonts/Roboto-Medium.ttf",    // Root
		"../gekko/voxelrt/rt/fonts/Roboto-Medium.ttf", // From subfolders like actiongame
		"assets/Roboto-Medium.ttf",                    // Local assets
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "Roboto-Medium.ttf"
}
