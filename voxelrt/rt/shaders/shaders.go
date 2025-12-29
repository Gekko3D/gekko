package shaders

import (
	_ "embed"
)

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

//go:embed particles_billboard.wgsl
var ParticlesBillboardWGSL string

/**
 */
//go:embed transparent_overlay.wgsl
var TransparentOverlayWGSL string

//go:embed resolve_transparency.wgsl
var ResolveTransparencyWGSL string

//go:embed hiz.wgsl
var HiZWGSL string
