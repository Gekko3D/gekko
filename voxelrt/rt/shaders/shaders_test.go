package shaders

import (
	"math"
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
		"light_phase",
		"surface_variation",
		"sphere_normal",
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

func TestFarPlanetRingShaderContract(t *testing.T) {
	for _, needle := range []string{
		"struct FarPlanetRingRecord",
		"@group(1) @binding(1) var<storage, read> far_planet_rings",
		"@group(2) @binding(1) var planet_depth",
		"struct FSOut",
		"sanitize_scene_depth",
		"NO_OCCLUDER_T",
		"textureLoad(scene_depth",
		"textureLoad(planet_depth",
		"occluder_t",
		"sample_t > scene_t",
		"safe_normalize",
		"sample_radial_profile",
		"ring_local_noise",
		"ring_texture_luma",
		"profile_7",
		"ring_ray_hits",
		"annulus_edge_hits",
		"shade_ring_hit",
		"angular_radians_per_pixel",
		"PARENT_DISC_MIN_PIXELS",
		"EDGE_ON_MIN_PIXELS",
		"NEAR_FIELD_FADE_INNER_METERS",
		"NEAR_FIELD_FADE_OUTER_METERS",
		"NEAR_FIELD_FADE_MIN_VISIBILITY",
		"DUST_HAZE_NEAR_CLEAR_METERS",
		"DUST_HAZE_PATH_FULL_METERS",
		"parent_render_radius",
		"parent_disc_occlusion",
		"parent_planet_shadow",
		"shadow_luma",
		"dust_haze_params",
		"dust_haze_lighting",
		"front_back_ring_classification",
		"sun_facing_brightness",
		"near_field_sample_fade",
		"ring_volume_interval",
		"shade_ring_volume_haze",
		"parent_radius",
		"parent_depth_light",
		"ring_light_direction",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing %q", needle)
		}
	}
	for _, forbidden := range []string{
		"Cassini",
		"Kirkwood",
		"268000000000",
		"planet_radius * 2.2",
		"proj < 0",
		"floor(angle",
	} {
		if strings.Contains(FarPlanetRingWGSL, forbidden) {
			t.Fatalf("far planet-ring shader contains deferred/forbidden logic %q", forbidden)
		}
	}
	if strings.Contains(FarPlanetRingWGSL, "camera.light_pos.xyz") {
		t.Fatal("far planet-ring shader must use record-local view-space light direction, not camera.light_pos")
	}
	if strings.Contains(FarPlanetRingWGSL, "out.weight = 1.0") {
		t.Fatal("far planet-ring shader must follow WBOIT weighted output, not a full fixed denominator")
	}
	for _, needle := range []string{
		"let weight = max(1e-3, alpha);",
		"out.accum = vec4<f32>(accum.rgb * alpha * weight, alpha);",
		"out.weight = alpha * weight;",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing WBOIT weighted output %q", needle)
		}
	}
}

func TestFarPlanetRingPlanetShadowDoesNotEraseOpacity(t *testing.T) {
	if strings.Contains(FarPlanetRingWGSL, "parent_visibility * shadow_visibility * near_field_fade") {
		t.Fatal("far planet-ring cast shadow must not multiply alpha into a missing ring segment")
	}
	for _, needle := range []string{
		"let alpha = ring.center_opacity.w * profile_opacity * edge_fade * hit.thickness_fade * parent_visibility * near_field_fade * edge_boost;",
		"let shadow_luma = mix(0.32, 1.0, shadow_visibility);",
		"let color = ring.tint_seed.rgb * texture_luma * lit * shadow_luma;",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing lighting-only planet shadow behavior %q", needle)
		}
	}
}

func TestFarPlanetRingDistantHazeIsAnalyticNotBillboardFog(t *testing.T) {
	for _, needle := range []string{
		"struct RingInterval",
		"fn valid_ring_interval(segment_enter: f32, segment_exit: f32, min_t: f32) -> RingInterval",
		"fn dust_haze_half_thickness(record: FarPlanetRingRecord) -> f32",
		"fn dust_haze_max_sample_count(record: FarPlanetRingRecord) -> u32",
		"fn dust_haze_adaptive_sample_count(record: FarPlanetRingRecord, ray: vec3<f32>, path_len: f32, dust_opacity: f32, max_alpha: f32) -> u32",
		"let path_t = smoothstep(0.12, 0.95, saturate(path_len / DUST_HAZE_PATH_FULL_METERS));",
		"let edge_on_t = 1.0 - smoothstep(0.025, 0.20, abs(dot(ray, normal)));",
		"let density_t = smoothstep(0.015, 0.10, saturate(dust_opacity * max_alpha));",
		"let need = clamp(max(path_t, edge_on_t * 0.85) * density_t, 0.0, 1.0);",
		"fn dust_haze_phase_luma(record: FarPlanetRingRecord, ray: vec3<f32>) -> f32",
		"fn dust_haze_color(record: FarPlanetRingRecord, optical_density: f32, ray: vec3<f32>) -> vec3<f32>",
		"let desaturated_tint = mix(vec3<f32>(tint_luma), tint, 0.28);",
		"let thin_color = desaturated_tint * vec3<f32>(0.56, 0.64, 0.82);",
		"let dense_color = mix(tint, tint * vec3<f32>(1.18, 1.03, 0.82), 0.48);",
		"let density_luma = mix(0.82, 1.18, density_t);",
		"fn ring_volume_interval(ray: vec3<f32>, record: FarPlanetRingRecord, max_t: f32) -> RingInterval",
		"let half_thickness = dust_haze_half_thickness(record);",
		"let first = valid_ring_interval(enter, min(exit, inner_enter), min_t);",
		"return valid_ring_interval(max(enter, inner_exit), exit, min_t);",
		"fn ring_volume_haze_density_at_sample(ring: FarPlanetRingRecord, sample_pos: vec3<f32>, sample_t: f32) -> f32",
		"let radial_edge_fraction = clamp(ring.dust_haze_params.w, 0.001, 0.25);",
		"let radial_edge_fade = smoothstep(inner, inner + radial_edge_width, radius)",
		"let vertical_core = clamp(ring.dust_haze_lighting.x, 0.001, 0.95);",
		"let shadow_visibility = mix(1.0, parent_planet_shadow(ring, sample_pos), shadow_strength);",
		"fn shade_ring_volume_haze(ring: FarPlanetRingRecord, ray: vec3<f32>, scene_t: f32) -> vec4<f32>",
		"let dust_opacity = clamp(ring.parent_depth_light.x, 0.0, 1.0);",
		"let start_t = max(interval.enter, DUST_HAZE_NEAR_CLEAR_METERS);",
		"let max_alpha = clamp(ring.dust_haze_params.x, 0.0, 1.0);",
		"let sample_count = dust_haze_adaptive_sample_count(ring, ray, path_len, dust_opacity, max_alpha);",
		"let step_len = path_len / sample_count_f;",
		"for (var i = 0u; i < sample_count; i = i + 1u)",
		"integrated_density = integrated_density + ring_volume_haze_density_at_sample(ring, sample_pos, sample_t) * step_len;",
		"let optical_density = smoothstep(0.0, 1.0, saturate(integrated_density / DUST_HAZE_PATH_FULL_METERS));",
		"let color = dust_haze_color(ring, optical_density, ray);",
		"let haze = shade_ring_volume_haze(ring, ray, occluder_t);",
		"accum = accum + haze * (1.0 - accum.a);",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing analytic distant haze behavior %q", needle)
		}
	}
	if strings.Contains(FarPlanetRingWGSL, "ring_dust_fog") || strings.Contains(FarPlanetRingWGSL, "RingDustFog") {
		t.Fatal("distant haze must stay in the analytic far-ring shader, not the removed fog feature")
	}
	if strings.Contains(FarPlanetRingWGSL, "ring.center_opacity.w * distant_ring_haze") {
		t.Fatal("distant haze must not depend on suppressed far-ring surface opacity")
	}
	if strings.Contains(FarPlanetRingWGSL, "camera_local_ring_density") || strings.Contains(FarPlanetRingWGSL, "let camera_rel = -center;") {
		t.Fatal("distant haze density must come from the ship/player-side dust opacity, not camera-local ring density")
	}
	for _, forbidden := range []string{
		"let mid_t =",
		"let mid_rel =",
		"(enter + exit) * 0.5",
		"let sample_t = (start_t + end_t) * 0.5;",
		"let path_fade = smoothstep(0.0, DUST_HAZE_PATH_FULL_METERS, path_len);",
		"DUST_HAZE_MAX_ALPHA",
		"DUST_HAZE_SAMPLE_COUNT",
		"DUST_HAZE_RADIAL_EDGE_FADE_FRACTION",
		"DUST_HAZE_VERTICAL_CORE_FRACTION",
		"ring.tint_seed.rgb * vec3<f32>(0.48, 0.45, 0.39)",
	} {
		if strings.Contains(FarPlanetRingWGSL, forbidden) {
			t.Fatalf("far planet-ring distant haze must not use midpoint-only inner-hole clipping %q", forbidden)
		}
	}
}

func TestFarPlanetRingAdaptiveHazeSampleCountNumerically(t *testing.T) {
	const maxSamples = 6

	shortLowDensity := testDustHazeAdaptiveSampleCount(maxSamples, 40_000, 0.02, 0.11, 0.7)
	if shortLowDensity != 1 {
		t.Fatalf("expected short low-density haze path to use one sample, got %d", shortLowDensity)
	}

	longEdgeOn := testDustHazeAdaptiveSampleCount(maxSamples, 700_000, 1.0, 0.11, 0.01)
	if longEdgeOn != maxSamples {
		t.Fatalf("expected long edge-on haze path to use configured maximum %d samples, got %d", maxSamples, longEdgeOn)
	}

	longLowDensity := testDustHazeAdaptiveSampleCount(maxSamples, 700_000, 0.05, 0.11, 0.01)
	if longLowDensity >= longEdgeOn {
		t.Fatalf("expected low-density long path to reduce samples below dense path, got low=%d dense=%d", longLowDensity, longEdgeOn)
	}
}

func testDustHazeAdaptiveSampleCount(maxCount int, pathLen, dustOpacity, maxAlpha, absRayNormalDot float64) int {
	if maxCount <= 1 {
		return maxCount
	}
	pathT := testSmoothstep(0.12, 0.95, testSaturate(pathLen/650_000))
	edgeOnT := 1 - testSmoothstep(0.025, 0.20, math.Abs(absRayNormalDot))
	densityT := testSmoothstep(0.015, 0.10, testSaturate(dustOpacity*maxAlpha))
	need := testSaturate(math.Max(pathT, edgeOnT*0.85) * densityT)
	count := int(math.Floor(1 + float64(maxCount-1)*need + 0.5))
	if count < 1 {
		return 1
	}
	if count > maxCount {
		return maxCount
	}
	return count
}

func testSmoothstep(edge0, edge1, x float64) float64 {
	t := testSaturate((x - edge0) / (edge1 - edge0))
	return t * t * (3 - 2*t)
}

func testSaturate(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func TestFarPlanetRingVolumeIntervalSkipsInnerHoleNumerically(t *testing.T) {
	const (
		inner         = 100000.0
		outer         = 200000.0
		halfThickness = 1000.0
		maxT          = 1000000.0
		minT          = 40000.0
	)

	got := testRingVolumeInterval(
		testVec3{},
		testVec3{y: 1},
		testVec3{x: 1},
		inner,
		outer,
		halfThickness,
		maxT,
		minT,
	)
	if !got.valid {
		t.Fatal("expected outward ray from inner hole to hit ring annulus")
	}
	if !almostEqual(got.enter, inner) || !almostEqual(got.exit, outer) {
		t.Fatalf("expected inner-hole ray to start haze at inner radius and exit at outer radius, got enter=%v exit=%v", got.enter, got.exit)
	}
	if got.enter <= minT {
		t.Fatalf("inner-hole path must not be counted as near-camera haze, got enter=%v min=%v", got.enter, minT)
	}
}

func TestFarPlanetRingVolumeIntervalChoosesNearestAnnulusLobeNumerically(t *testing.T) {
	const (
		inner         = 100000.0
		outer         = 200000.0
		halfThickness = 1000.0
		maxT          = 1000000.0
		minT          = 40000.0
	)

	got := testRingVolumeInterval(
		testVec3{x: 250000},
		testVec3{y: 1},
		testVec3{x: 1},
		inner,
		outer,
		halfThickness,
		maxT,
		minT,
	)
	if !got.valid {
		t.Fatal("expected ray through both sides of annulus to hit nearest lobe")
	}
	if !almostEqual(got.enter, 50000.0) || !almostEqual(got.exit, 150000.0) {
		t.Fatalf("expected nearest annulus lobe [50000, 150000], got enter=%v exit=%v", got.enter, got.exit)
	}
}

type testVec3 struct {
	x, y, z float64
}

type testRingInterval struct {
	valid bool
	enter float64
	exit  float64
}

func testRingVolumeInterval(center, normal, ray testVec3, inner, outer, halfThickness, maxT, minT float64) testRingInterval {
	normal = normal.normalize()
	rel0 := center.scale(-1)
	h0 := rel0.dot(normal)
	denom := ray.dot(normal)

	enter := 0.0
	exit := maxT
	if math.Abs(denom) < 1e-5 {
		if math.Abs(h0) > halfThickness {
			return testRingInterval{}
		}
	} else {
		ta := (-halfThickness - h0) / denom
		tb := (halfThickness - h0) / denom
		enter = math.Max(enter, math.Min(ta, tb))
		exit = math.Min(exit, math.Max(ta, tb))
	}
	if exit <= math.Max(enter, 0.0) {
		return testRingInterval{}
	}

	q0 := rel0.sub(normal.scale(h0))
	d := ray.sub(normal.scale(denom))
	a := d.dot(d)
	if a <= 1e-8 {
		radius := q0.length()
		if radius < inner || radius > outer {
			return testRingInterval{}
		}
		return testValidRingInterval(math.Max(enter, 0.0), exit, minT)
	}

	b := 2.0 * q0.dot(d)
	cOuter := q0.dot(q0) - outer*outer
	discOuter := b*b - 4.0*a*cOuter
	if discOuter < 0.0 {
		return testRingInterval{}
	}
	rootOuter := math.Sqrt(discOuter)
	enter = math.Max(enter, (-b-rootOuter)/(2.0*a))
	exit = math.Min(exit, (-b+rootOuter)/(2.0*a))
	if exit <= math.Max(enter, 0.0) {
		return testRingInterval{}
	}

	cInner := q0.dot(q0) - inner*inner
	discInner := b*b - 4.0*a*cInner
	if discInner < 0.0 {
		return testValidRingInterval(math.Max(enter, 0.0), exit, minT)
	}

	enter = math.Max(enter, 0.0)
	if exit <= math.Max(enter, minT) {
		return testRingInterval{}
	}

	rootInner := math.Sqrt(discInner)
	innerEnter := (-b - rootInner) / (2.0 * a)
	innerExit := (-b + rootInner) / (2.0 * a)
	if innerExit <= enter || innerEnter >= exit {
		return testValidRingInterval(enter, exit, minT)
	}

	first := testValidRingInterval(enter, math.Min(exit, innerEnter), minT)
	if first.valid {
		return first
	}
	return testValidRingInterval(math.Max(enter, innerExit), exit, minT)
}

func testValidRingInterval(segmentEnter, segmentExit, minT float64) testRingInterval {
	enter := math.Max(segmentEnter, minT)
	if segmentExit <= enter {
		return testRingInterval{}
	}
	return testRingInterval{valid: true, enter: enter, exit: segmentExit}
}

func (v testVec3) dot(other testVec3) float64 {
	return v.x*other.x + v.y*other.y + v.z*other.z
}

func (v testVec3) sub(other testVec3) testVec3 {
	return testVec3{x: v.x - other.x, y: v.y - other.y, z: v.z - other.z}
}

func (v testVec3) scale(s float64) testVec3 {
	return testVec3{x: v.x * s, y: v.y * s, z: v.z * s}
}

func (v testVec3) length() float64 {
	return math.Sqrt(v.dot(v))
}

func (v testVec3) normalize() testVec3 {
	l := v.length()
	if l <= 0 {
		return testVec3{}
	}
	return v.scale(1 / l)
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6
}

func TestFarPlanetRingNoiseAvoidsAngularHashSeam(t *testing.T) {
	for _, needle := range []string{
		"fn ring_local_noise(record: FarPlanetRingRecord, radial_t: f32, ring_dir: vec2<f32>) -> f32",
		"let radial_bucket = floor(radial_t * 128.0);",
		"ring_dir.x * 96.0",
		"ring_dir.y * 96.0",
		"angle * 7.0",
		"angle * 17.0",
		"let ring_dir = vec2<f32>(u, v) / max(radius, 1e-5);",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing seam-safe local noise %q", needle)
		}
	}
	if strings.Contains(FarPlanetRingWGSL, "floor(angle") {
		t.Fatal("far planet-ring local noise must not hash atan2 angle buckets")
	}
}

func TestFarPlanetRingTextureSurvivesWBOITNormalization(t *testing.T) {
	for _, needle := range []string{
		"fn ring_texture_luma(opacity_pattern: f32) -> f32",
		"let texture_luma = ring_texture_luma(profile_opacity);",
		"let color = ring.tint_seed.rgb * texture_luma * lit",
		"return vec4<f32>(color * alpha, alpha);",
		"out.accum = vec4<f32>(accum.rgb * alpha * weight, alpha);",
		"out.weight = alpha * weight;",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing WBOIT-visible texture contrast %q", needle)
		}
	}
}

func TestFarPlanetRingAlphaAffectsWBOITColorContribution(t *testing.T) {
	if strings.Contains(FarPlanetRingWGSL, "out.accum = vec4<f32>(accum.rgb * weight, alpha);") {
		t.Fatal("far planet-ring alpha must not be normalized out of the resolved color contribution")
	}
	for _, needle := range []string{
		"return vec4<f32>(color * alpha, alpha);",
		"let alpha = saturate(accum.a);",
		"out.accum = vec4<f32>(accum.rgb * alpha * weight, alpha);",
		"out.weight = alpha * weight;",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing alpha-sensitive WBOIT contribution %q", needle)
		}
	}
}

func TestFarPlanetRingNoDepthHitDoesNotClampToCameraFarPlane(t *testing.T) {
	for _, needle := range []string{
		"const NO_OCCLUDER_T: f32",
		"return NO_OCCLUDER_T;",
		"let occluder_t = min(scene_t, planet_t);",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing no-hit depth behavior %q", needle)
		}
	}
	if strings.Contains(FarPlanetRingWGSL, "return far_t;") {
		t.Fatal("far planet-ring no-hit depth must not clamp ring visibility to the camera far plane")
	}
}

func TestFarPlanetRingNearFieldFadeIsSampleLocal(t *testing.T) {
	for _, needle := range []string{
		"fn near_field_sample_fade(sample_pos: vec3<f32>) -> f32",
		"let camera_distance = length(sample_pos);",
		"let local_fade = smoothstep(NEAR_FIELD_FADE_INNER_METERS, NEAR_FIELD_FADE_OUTER_METERS, camera_distance);",
		"return mix(NEAR_FIELD_FADE_MIN_VISIBILITY, 1.0, local_fade);",
	} {
		if !strings.Contains(FarPlanetRingWGSL, needle) {
			t.Fatalf("far planet-ring shader missing sample-local near-field fade %q", needle)
		}
	}
	if strings.Contains(FarPlanetRingWGSL, "fn near_field_sample_fade(_sample_pos: vec3<f32>)") {
		t.Fatal("far planet-ring near-field fade must not ignore the hit sample position")
	}
}

func TestDebrisMidfieldShaderContract(t *testing.T) {
	for _, needle := range []string{
		"struct DebrisMidfieldRecord",
		"@group(1) @binding(1) var<storage, read> cells",
		"@group(2) @binding(0) var scene_depth",
		"@group(2) @binding(1) var planet_depth",
		"seed_bits",
		"record_density",
		"record_fade",
		"active_handoff",
		"exact_handoff",
		"handoff_pad",
		"clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;",
		"sanitize_scene_depth",
		"textureLoad(scene_depth",
		"textureLoad(planet_depth",
		"let t_occluder = min(t_scene, t_planet);",
		"t_pixel > t_occluder + bias",
		"let depth_diff = (t_occluder + bias) - t_pixel;",
	} {
		if !strings.Contains(DebrisMidfieldWGSL, needle) {
			t.Fatalf("debris-midfield shader missing %q", needle)
		}
	}
}

func TestDebrisMidfieldWBOITRangeMatchesFarFade(t *testing.T) {
	for _, needle := range []string{
		"if (distance > 200000.0)",
		"lod_alpha = 1.0 - smoothstep(200000.0, 300000.0, distance);",
		"let depth_norm = clamp(t_pixel / 300000.0, 0.0, 1.0);",
		"out.accum = vec4<f32>(final_rgb * alpha * weight, alpha);",
		"out.weight = alpha * weight;",
	} {
		if !strings.Contains(DebrisMidfieldWGSL, needle) {
			t.Fatalf("debris-midfield shader missing matched WBOIT/far-fade behavior %q", needle)
		}
	}
	if strings.Contains(DebrisMidfieldWGSL, "t_pixel / 120000.0") {
		t.Fatal("debris-midfield WBOIT range must not end before its 200-300km LOD fade")
	}
}

func TestDebrisMidfieldNonExactCardsStayCloseFleckMidVisible(t *testing.T) {
	for _, needle := range []string{
		"let background_size_near = mix(1.5, 12.0",
		"let background_size_mid = mix(12.0, 72.0",
		"let active_halo_size_near = mix(2.0, 16.0",
		"let active_halo_size_mid = mix(8.0, 46.0",
		"smoothstep(3500.0, 18000.0, distance)",
		"smoothstep(1800.0, 9000.0, distance)",
		"let random_size = select(background_size, active_halo_size, active_handoff);",
		"Exact handoff markers are the only",
	} {
		if !strings.Contains(DebrisMidfieldWGSL, needle) {
			t.Fatalf("debris-midfield shader missing close/mid-field size contract %q", needle)
		}
	}
	if strings.Contains(DebrisMidfieldWGSL, "mix(5.0, 82.0") {
		t.Fatal("debris-midfield atmospheric cards must not use ship-sized random billboards")
	}
}

func TestDebrisMidfieldAtmosphericCardsAreDarkerLumpierAndNonTargetLike(t *testing.T) {
	for _, needle := range []string{
		"@location(8) exact_handoff: f32",
		"out.exact_handoff = select(0.0, 1.0, exact_handoff);",
		"let atmospheric_card = 1.0 - in.exact_handoff;",
		"let chip_noise =",
		"let chipped_edge = atmospheric_card * 0.035",
		"let pit_mask = clamp(",
		"let dust_alpha = mix(1.0, clamp(0.44 + 0.56 * pit_mask, 0.36, 1.0), atmospheric_card);",
		"let pit_shadow = mix(1.0, mix(0.54, 1.0, pit_mask), atmospheric_card);",
		"let atmospheric_darkening = mix(1.0, 0.62, atmospheric_card);",
		"let final_rgb = in.color.rgb * shaded * edge_shadow * grain * pit_shadow * atmospheric_darkening;",
	} {
		if !strings.Contains(DebrisMidfieldWGSL, needle) {
			t.Fatalf("debris-midfield shader missing atmospheric non-target material behavior %q", needle)
		}
	}
	if strings.Contains(DebrisMidfieldWGSL, "let atmospheric_darkening = 1.0") {
		t.Fatal("debris-midfield atmospheric cards must stay darker than exact handoff markers")
	}
}

func TestDebrisMidfieldDoesNotUseLargeFogBillboards(t *testing.T) {
	for _, forbidden := range []string{
		"atmosphere_gate",
		"atmosphere_size",
		"atmosphere_extent",
		"distant_atmosphere",
		"atmosphere_alpha",
		"mix(base_random_size",
		"mix(900.0, 4200.0",
	} {
		if strings.Contains(DebrisMidfieldWGSL, forbidden) {
			t.Fatalf("debris-midfield shader must not use blocky billboard haze path %q", forbidden)
		}
	}
	if strings.Contains(DebrisMidfieldWGSL, "ring_dust_fog") || strings.Contains(DebrisMidfieldWGSL, "RingDustFog") {
		t.Fatal("midfield debris must not depend on the removed ring-fog path")
	}
}

func TestAstronomicalShaderBypassesLegacyRingOrBeltRendering(t *testing.T) {
	if !strings.Contains(AstronomicalWGSL, "Dedicated far planet-ring rendering owns this path now") {
		t.Fatal("expected astronomical shader to document inert legacy ring path")
	}
	for _, forbidden := range []string{
		"Cassini",
		"Kirkwood",
		"268000000000",
		"planet_radius * 2.2",
	} {
		if strings.Contains(AstronomicalWGSL, forbidden) {
			t.Fatalf("astronomical shader still contains active legacy ring logic %q", forbidden)
		}
	}
}

func TestDeferredSkyRayReconstructionGuardsFarPlaneW(t *testing.T) {
	if !strings.Contains(DeferredLightingWGSL, "view = view / max(view.w, 1e-6);") {
		t.Fatal("deferred lighting sky ray reconstruction must guard near-zero inverse-projection w")
	}
}

func TestDeferredDirectionalShadowRejectsOutOfCascadeDepth(t *testing.T) {
	if !strings.Contains(DeferredLightingWGSL, "proj_pos.z >= -1.0 && proj_pos.z <= 1.0") {
		t.Fatal("directional shadow sampling must reject receivers outside cascade depth")
	}
}

func TestVoxelShadowSamplingStaysHardNoPCF(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "deferred lighting", code: DeferredLightingWGSL},
		{name: "transparent overlay", code: TransparentOverlayWGSL},
	} {
		for _, forbidden := range []string{
			"pcf_visibility",
			"sample_weight_sum",
			"mix(hard_visibility",
			"ao_quality.z",
			"ao_quality.w",
		} {
			if strings.Contains(tc.code, forbidden) {
				t.Fatalf("%s shader must keep voxel shadows hard; found %q", tc.name, forbidden)
			}
		}
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
		{name: "far planet ring", code: FarPlanetRingWGSL},
		{name: "planet body", code: PlanetBodyWGSL},
	} {
		if strings.Contains(tc.code, "view = view / view.w") || strings.Contains(tc.code, "view / view.w") {
			t.Fatalf("%s shader has unguarded inverse-projection w division", tc.name)
		}
	}
}

func TestGBufferHitExposureAcceptsRayFacingBoundaryHits(t *testing.T) {
	required := []string{
		"the hit-position fallback can pick an internal side face",
		"let ray_facing = -sign(ray_dir_os);",
		"vi_hit + vec3<i32>(i32(ray_facing.x), 0, 0)",
		"vi_hit + vec3<i32>(0, i32(ray_facing.y), 0)",
		"vi_hit + vec3<i32>(0, 0, i32(ray_facing.z))",
	}
	for _, needle := range required {
		if !strings.Contains(GBufferWGSL, needle) {
			t.Fatalf("gbuffer shader missing ray-facing voxel-boundary hit exposure guard %q", needle)
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
		{name: "far planet ring", code: FarPlanetRingWGSL},
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

func TestPlanetBodyShaderHasGasGiantPixelReliefPath(t *testing.T) {
	required := []string{
		"fn is_gas_giant_planet",
		"let gas_mid_alt_end = max(planet.surface.z * 220.0, planet.bounds.w * 1.65);",
		"let gas_near_alt_end = max(planet.surface.z * 120.0, planet.bounds.w * 0.85);",
		"fn gas_giant_lane_signal",
		"fn gas_giant_cloud_light_step",
		"fn gas_giant_virtual_cell_dir",
		"fn gas_giant_virtual_cell_detail",
		"let virtual_resolution = min(base_resolution * mix(1.0, 4.0, saturate(detail_weight)), 2048.0);",
		"fn quantize_dithered_step",
		"fn gas_giant_limb_haze",
		"fn gas_giant_terminator_step",
		"let gas_height_step = floor(gas_height01 * 5.0) / 5.0;",
		"let near_weight = 1.0 - smoothstep(near_alt_start, near_alt_end, altitude);",
		"terrain_mix = clamp(terrain_mix + 0.18 + detail_settings.near_weight * 0.16, 0.0, 1.0);",
		"let gas_detail_weight = select(0.0, max(detail_settings.near_weight, detail_settings.mid_weight * 0.72), gas_giant);",
		"let gas_limb_haze = gas_giant_limb_haze(world_normal, view_dir, gas_detail_weight, ipos);",
		"let virtual_detail = gas_giant_virtual_cell_detail(planet, local_normal, gas_detail_weight);",
		"base_color = mix(base_color, virtual_detail.xyz, gas_detail_weight * 0.62);",
		"let cloud_light = gas_giant_cloud_light_step(block_local_normal, u32(planet.noise.z), gas_detail_weight) * virtual_detail.w;",
		"base_color = mix(base_color * gas_lane_light * cloud_light, planet.atmosphere.xyz, max(gas_limb * 0.10, gas_limb_haze * 0.18));",
		"ndotl = mix(ndotl, quantize_dithered_step(ndotl, mix(5.0, 7.0, gas_detail_weight), ipos), 0.72);",
		"let terminator = gas_giant_terminator_step(block_normal, light_dir, gas_detail_weight, ipos);",
		"ambient_shadow = mix(0.12, 1.06, terminator);",
		"rim = gas_limb_haze * planet.style.w;",
	}
	for _, needle := range required {
		if !strings.Contains(PlanetBodyWGSL, needle) {
			t.Fatalf("planet body shader missing gas giant pixel relief path %q", needle)
		}
	}
	forbidden := []string{
		"fn gas_giant_cloud_normal_world",
		"gas_relief_strength",
		"relief_delta *",
		"cloud_normal",
	}
	for _, needle := range forbidden {
		if strings.Contains(PlanetBodyWGSL, needle) {
			t.Fatalf("planet body shader still uses double gas giant normal perturbation %q", needle)
		}
	}
}

func TestPlanetBodyShaderHasRockyPixelDetailPath(t *testing.T) {
	required := []string{
		"fn is_rocky_detail_planet",
		"fn rocky_virtual_cell_dir",
		"fn rocky_virtual_cell_detail",
		"fn rocky_virtual_relief_signal",
		"fn rocky_virtual_relief_normal_world",
		"fn rocky_surface_cloud_mask",
		"fn gas_giant_surface_cloud_mask",
		"let default_mid_alt_end = max(planet.surface.z * 44.0, planet.bounds.w * 0.65);",
		"let default_near_alt_end = max(planet.surface.z * 14.0, planet.bounds.w * 0.16);",
		"let virtual_resolution = min(base_resolution * mix(1.0, 5.0, saturate(detail_weight)), 2048.0);",
		"let rocky_detail = is_rocky_detail_planet(planet);",
		"let rocky_detail_weight = select(0.0, max(detail_settings.near_weight, detail_settings.mid_weight * 0.82), rocky_detail);",
		"let virtual_relief = rocky_virtual_relief_normal_world(planet, local_normal, rocky_detail_weight, surface_hit.signed_height, is_ocean);",
		"relief_normal = normalize(mix(relief_normal, virtual_relief, relief_blend));",
		"terrain_mix = clamp(terrain_mix + rocky_detail_weight * select(0.08, 0.18, !is_ocean), 0.0, 1.0);",
		"let virtual_detail = rocky_virtual_cell_detail(planet, local_normal, surface_hit.signed_height, is_ocean, rocky_detail_weight);",
		"base_color = mix(base_color, virtual_detail.xyz, rocky_detail_weight * 0.72);",
		"base_color = mix(base_color, base_color * virtual_detail.w, rocky_detail_weight * 0.58);",
		"let cloud_mask = rocky_surface_cloud_mask(planet, local_normal, rocky_detail_weight);",
		"let cloud_mask = gas_giant_surface_cloud_mask(planet, local_normal, gas_detail_weight);",
		"ndotl = mix(ndotl, quantize_dithered_step(ndotl, mix(5.0, 8.0, rocky_detail_weight), ipos), 0.38);",
	}
	for _, needle := range required {
		if !strings.Contains(PlanetBodyWGSL, needle) {
			t.Fatalf("planet body shader missing rocky pixel detail path %q", needle)
		}
	}
	forbidden := []string{
		"fn rocky_cloud_normal_world",
		"rocky_relief_strength",
		"rocky_normal",
		"sample_planet_surface(planet, rocky_virtual",
		"let patch =",
	}
	for _, needle := range forbidden {
		if strings.Contains(PlanetBodyWGSL, needle) {
			t.Fatalf("planet body shader must keep rocky relief out of ray geometry and smooth cloud normals, found %q", needle)
		}
	}
}

func TestAstronomicalShaderHasAtmosphereCloudLOD(t *testing.T) {
	required := []string{
		"fn far_cloud_mask",
		"fn far_rocky_cloud_mask",
		"fn far_gas_cloud_mask",
		"fn far_atmosphere_color",
		"fn far_atmosphere_halo",
		"fn star_corona_sample",
		"let band_steps = floor(bands * 7.0) / 7.0;",
		"color = mix(color * mix(0.90, 1.16, cloud.y), far_atmosphere_color(body, kind), cloud.x * 0.26);",
		"color = mix(color, min(cloud_tint * (1.05 + cloud.y * 0.22), vec3<f32>(1.0)), cloud.x * 0.42 * day);",
		"sample = far_atmosphere_halo(body, ray_dir, body_dir, angle, radius);",
		"sample = star_corona_sample(body, angle, radius, glow_radius);",
	}
	for _, needle := range required {
		if !strings.Contains(AstronomicalWGSL, needle) {
			t.Fatalf("astronomical shader missing atmosphere/cloud LOD path %q", needle)
		}
	}
}

func TestAnalyticMediumShaderHasPixelSteppedAtmosphereHaze(t *testing.T) {
	required := []string{
		"fn pixel_dither",
		"fn quantize_dithered_medium_step",
		"let sun_side = smoothstep(-0.12, 0.32, dot(radial_dir, light_dir));",
		"let atmosphere_light_gate = max(sun_side, self_emissive * 0.85);",
		"let night_air_gate = mix(0.08, 1.0, atmosphere_light_gate);",
		"let haze_cell = quantize_dithered_medium_step(",
		"let cloud_core = floor(saturate((n - threshold) / max(1.0 - threshold, 1e-4)) * 5.0) / 5.0;",
		"let cloud_step = quantize_dithered_medium_step(cloud_luma, select(4.0, 6.0, procedural_noise_enabled), ipos, i + 41u);",
		"select(5.0, 7.0, procedural_noise_enabled)",
		"* mix(0.88, 1.16, haze_cell)",
		"* night_air_gate",
		"* mix(0.20, 1.0, atmosphere_light_gate)",
		"* mix(0.92, 1.16, cloud_step)",
		"* mix(0.92, 1.10, haze_cell)",
		"* mix(0.86, 1.18, haze_cell)",
	}
	for _, needle := range required {
		if !strings.Contains(AnalyticMediumWGSL, needle) {
			t.Fatalf("analytic medium shader missing pixel-stepped atmosphere haze path %q", needle)
		}
	}
}
