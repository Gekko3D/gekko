package app

import "github.com/cogentcore/webgpu/wgpu"

// FrameContext carries per-frame targets and dimensions shared by graph nodes.
type FrameContext struct {
	Width         uint32
	Height        uint32
	SwapchainView *wgpu.TextureView
	WorkgroupsX   uint32
	WorkgroupsY   uint32
}
