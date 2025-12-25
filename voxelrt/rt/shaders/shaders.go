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

//go:embed text.wgsl
var TextWGSL string

//go:embed gbuffer.wgsl
var GBufferWGSL string

//go:embed deferred_lighting.wgsl
var DeferredLightingWGSL string

//go:embed shadow_map.wgsl
var ShadowMapWGSL string
