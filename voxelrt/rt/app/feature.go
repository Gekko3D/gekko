package app

import "github.com/cogentcore/webgpu/wgpu"

// Feature is an optional renderer extension with lifecycle hooks.
// Core renderer behavior must remain valid even when no features are registered.
type Feature interface {
	Name() string
	Enabled(*App) bool
	Setup(*App) error
	Resize(*App, uint32, uint32) error
	OnSceneBuffersRecreated(*App) error
	Update(*App) error
	Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error
	Shutdown(*App)
}

// FeatureCommandStage defines command-encoder dispatch points in the frame.
type FeatureCommandStage uint8

const (
	// FeatureCommandStagePreGBuffer runs before the G-buffer compute pass.
	FeatureCommandStagePreGBuffer FeatureCommandStage = iota
	// FeatureCommandStagePreGBufferVolumes is reserved for volume prepasses that
	// historically ran as a separate block before G-buffer.
	FeatureCommandStagePreGBufferVolumes
)

// FeaturePassStage defines in-pass draw points inside renderer-owned render passes.
type FeaturePassStage uint8

const (
	// FeaturePassStageAccumulation runs inside the WBOIT accumulation pass.
	FeaturePassStageAccumulation FeaturePassStage = iota
)

// FeatureScreenStage defines screen-space rendering points outside core resolve.
type FeatureScreenStage uint8

const (
	// FeatureScreenStagePostResolve runs after core resolve on the swapchain view.
	FeatureScreenStagePostResolve FeatureScreenStage = iota
)

// FeatureCommandStageHandler is an optional interface for command-encoder stages.
type FeatureCommandStageHandler interface {
	DispatchCommandStage(*App, FeatureCommandStage, *wgpu.CommandEncoder) error
}

// FeaturePassStageHandler is an optional interface for render-pass stages.
type FeaturePassStageHandler interface {
	RenderPassStage(*App, FeaturePassStage, *wgpu.RenderPassEncoder) error
}
