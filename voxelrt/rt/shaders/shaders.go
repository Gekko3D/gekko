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
var gBufferWGSL string

//go:embed deferred_lighting.wgsl
var DeferredLightingWGSL string

//go:embed tiled_light_cull.wgsl
var TiledLightCullWGSL string

//go:embed shadow_map.wgsl
var shadowMapWGSL string

//go:embed particles_billboard.wgsl
var ParticlesBillboardWGSL string

//go:embed particles_sim.wgsl
var ParticlesSimWGSL string

//go:embed analytic_medium.wgsl
var AnalyticMediumWGSL string

//go:embed water_surface.wgsl
var WaterSurfaceWGSL string

//go:embed planet_body.wgsl
var PlanetBodyWGSL string

//go:embed astronomical.wgsl
var AstronomicalWGSL string

//go:embed far_planet_ring.wgsl
var FarPlanetRingWGSL string

//go:embed debris_midfield.wgsl
var DebrisMidfieldWGSL string

/**
 */
//go:embed transparent_overlay.wgsl
var transparentOverlayWGSL string

//go:embed resolve_transparency.wgsl
var ResolveTransparencyWGSL string

//go:embed hiz.wgsl
var HiZWGSL string

//go:embed gizmo.wgsl
var GizmoWGSL string

//go:embed skybox.wgsl
var SkyboxWGSL string

//go:embed sprites.wgsl
var SpritesWGSL string

//go:embed beams.wgsl
var BeamsWGSL string

//go:embed decals.wgsl
var DecalsWGSL string

//go:embed sector_dda.wgsl
var sectorDDAWGSL string

// Public shader sources include the same sector traversal contract. Raw pass
// sources retain their independent bindings and inner voxel traversal paths.
var (
	GBufferWGSL            = sectorDDAWGSL + "\n" + gBufferWGSL
	ShadowMapWGSL          = sectorDDAWGSL + "\n" + shadowMapWGSL
	TransparentOverlayWGSL = sectorDDAWGSL + "\n" + transparentOverlayWGSL
)
