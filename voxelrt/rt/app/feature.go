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
	// FeatureCommandStagePostGBuffer runs after the core G-buffer and Hi-Z work.
	FeatureCommandStagePostGBuffer
	// FeatureCommandStagePreLighting runs after shadows but before tiled cull/lighting.
	FeatureCommandStagePreLighting
	// FeatureCommandStagePostLighting runs after the core lighting pass completes.
	FeatureCommandStagePostLighting
	// FeatureCommandStagePreResolve runs after optional accumulation and before resolve.
	FeatureCommandStagePreResolve
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

// FeatureCommandStageContributor is an optional interface used to indicate that
// a command stage should be scheduled for the current frame.
type FeatureCommandStageContributor interface {
	HasCommandStage(*App, FeatureCommandStage) bool
}

// FeaturePassStageContributor is an optional interface used to indicate that an
// in-pass stage should be scheduled for the current frame.
type FeaturePassStageContributor interface {
	HasPassStage(*App, FeaturePassStage) bool
}

// FeatureScreenStageContributor is an optional interface used to indicate that
// a screen stage should be scheduled for the current frame.
type FeatureScreenStageContributor interface {
	HasScreenStage(*App, FeatureScreenStage) bool
}

// FeatureGraphNodeOwner marks features whose render work is handled by
// explicit graph nodes instead of the compatibility feature-stage dispatchers.
type FeatureGraphNodeOwner interface {
	GraphNodeNames() []string
}

// FeatureGraphCommandStageOwner marks command stages that a graph-owned feature
// has migrated out of the compatibility command-stage dispatcher.
type FeatureGraphCommandStageOwner interface {
	GraphCommandStages() []FeatureCommandStage
}

// FeatureGraphPassStageOwner marks graph-owned features that still draw inside
// a renderer-owned pass stage, such as WBOIT accumulation.
type FeatureGraphPassStageOwner interface {
	FeatureGraphNodeOwner
	FeaturePassStageContributor
	FeaturePassStageHandler
	GraphPassStages() []FeaturePassStage
}

// FeatureGraphScreenStageOwner marks screen stages that a graph-owned feature
// has migrated out of the compatibility screen-stage dispatcher.
type FeatureGraphScreenStageOwner interface {
	GraphScreenStages() []FeatureScreenStage
}
