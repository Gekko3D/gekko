package shaders

import (
	"strings"
	"testing"
)

func TestBillboardShadersApplyWebGPUClipZConversion(t *testing.T) {
	required := []string{
		"clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;",
		"out.position = raster_clip_pos(world_pos);",
	}

	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "particles", code: ParticlesBillboardWGSL},
		{name: "sprites", code: SpritesWGSL},
	} {
		for _, needle := range required {
			if !strings.Contains(tc.code, needle) {
				t.Fatalf("%s shader missing reverse-z raster clip conversion: %q", tc.name, needle)
			}
		}
	}
}

func TestAstronomicalShaderIsEmbedded(t *testing.T) {
	for _, needle := range []string{
		"struct AstronomicalRecord",
		"@group(1) @binding(1) var<storage, read> astronomical_bodies",
		"@group(2) @binding(0) var scene_depth",
		"scene_depth_has_hit",
		"angular_radians_per_pixel",
		"STAR_CORE_MIN_PIXELS",
		"OCCLUSION_PRIORITY_SELECTED",
		"PLANET_DISC_MIN_PIXELS",
		"GAS_GIANT_DISC_MIN_PIXELS",
	} {
		if !strings.Contains(AstronomicalWGSL, needle) {
			t.Fatalf("astronomical shader missing %q", needle)
		}
	}
}

func TestDeferredSkyRayReconstructionGuardsFarPlaneW(t *testing.T) {
	if !strings.Contains(DeferredLightingWGSL, "view = view / max(view.w, 1e-6);") {
		t.Fatal("deferred lighting sky ray reconstruction must guard near-zero inverse-projection w")
	}
}

func TestRayReconstructionGuardsFarPlaneW(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "gbuffer", code: GBufferWGSL},
		{name: "deferred lighting", code: DeferredLightingWGSL},
		{name: "transparent overlay", code: TransparentOverlayWGSL},
		{name: "ca volume render", code: CAVolumeRenderWGSL},
		{name: "debug", code: DebugWGSL},
		{name: "water surface", code: WaterSurfaceWGSL},
		{name: "astronomical", code: AstronomicalWGSL},
		{name: "planet body", code: PlanetBodyWGSL},
	} {
		if strings.Contains(tc.code, "view = view / view.w") || strings.Contains(tc.code, "view / view.w") {
			t.Fatalf("%s shader has unguarded inverse-projection w division", tc.name)
		}
	}
}

func TestFarRangeDepthValidityUsesCameraFarPlane(t *testing.T) {
	forbidden := []string{
		"camera_far_t() * 0.5",
		"far_t * 0.5",
		"distance_limits.y, 1.0) * 0.5",
		"camera_far_half",
		"far_half",
	}
	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "analytic medium", code: AnalyticMediumWGSL},
		{name: "resolve transparency", code: ResolveTransparencyWGSL},
		{name: "planet body", code: PlanetBodyWGSL},
		{name: "astronomical", code: AstronomicalWGSL},
		{name: "water surface", code: WaterSurfaceWGSL},
	} {
		for _, needle := range forbidden {
			if strings.Contains(tc.code, needle) {
				t.Fatalf("%s shader still uses a half-far-plane depth validity cutoff: %q", tc.name, needle)
			}
		}
		if !strings.Contains(tc.code, "1e-5") {
			t.Fatalf("%s shader missing finite far-plane guard", tc.name)
		}
	}
}
