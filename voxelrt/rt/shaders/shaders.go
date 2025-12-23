package shaders

import (
	_ "embed"
)

//go:embed raytrace.wgsl
var RaytraceWGSL string

//go:embed fullscreen.wgsl
var FullscreenWGSL string

//go:embed debug.wgsl
var DebugWGSL string
