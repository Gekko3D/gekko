package app

import "github.com/cogentcore/webgpu/wgpu"

// RenderNode is a graph-scheduled renderer pass.
//
// The interface mirrors the existing feature lifecycle so feature-stage code can
// migrate into graph nodes incrementally without changing renderer behavior.
type RenderNode interface {
	Name() string
	Enabled(*App) bool
	Setup(*App) error
	Resize(*App, uint32, uint32) error
	OnSceneBuffersRecreated(*App) error
	Update(*App) error
	Record(*App, *wgpu.CommandEncoder, *FrameContext) error
	Shutdown(*App)
}

// RenderNodeSpec declares a node and the nodes that must run before it.
type RenderNodeSpec struct {
	Name  string
	After []string
	Node  RenderNode
}
