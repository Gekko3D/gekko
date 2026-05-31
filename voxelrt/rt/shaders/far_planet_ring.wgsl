struct CameraData {
  view_proj: mat4x4<f32>,
  inv_view: mat4x4<f32>,
  inv_proj: mat4x4<f32>,
  cam_pos: vec4<f32>,
  light_pos: vec4<f32>,
  ambient_color: vec4<f32>,
  debug_mode: u32,
  render_mode: u32,
  num_lights: u32,
  pad1: u32,
  screen_size: vec2<f32>,
  pad2: vec2<f32>,
  ao_quality: vec4<f32>,
  distance_limits: vec4<f32>,
};

struct FarPlanetRingParams {
  ring_count: u32,
  pad0: u32,
  pad1: u32,
  pad2: u32,
};

struct FarPlanetRingRecord {
  center_opacity: vec4<f32>,
  normal_thickness: vec4<f32>,
  tangent_u_inner: vec4<f32>,
  tangent_v_outer: vec4<f32>,
  tint_seed: vec4<f32>,
  parent_radius: vec4<f32>,
  parent_depth_light: vec4<f32>,
  dust_haze_params: vec4<f32>,
  dust_haze_lighting: vec4<f32>,
  profile_0: vec4<f32>,
  profile_1: vec4<f32>,
  profile_2: vec4<f32>,
  profile_3: vec4<f32>,
  profile_4: vec4<f32>,
  profile_5: vec4<f32>,
  profile_6: vec4<f32>,
  profile_7: vec4<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

struct FSOut {
  @location(0) accum: vec4<f32>,
  @location(1) weight: f32,
};

const PARENT_DISC_MIN_PIXELS: f32 = 2.75;
const EDGE_ON_MIN_PIXELS: f32 = 2.0;
const NO_OCCLUDER_T: f32 = 1.0e30;
const NEAR_FIELD_FADE_INNER_METERS: f32 = 180000.0;
const NEAR_FIELD_FADE_OUTER_METERS: f32 = 360000.0;
const NEAR_FIELD_FADE_MIN_VISIBILITY: f32 = 0.10;
const DUST_HAZE_NEAR_CLEAR_METERS: f32 = 40000.0;
const DUST_HAZE_PATH_FULL_METERS: f32 = 650000.0;

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(1) @binding(0) var<uniform> far_planet_ring_params: FarPlanetRingParams;
@group(1) @binding(1) var<storage, read> far_planet_rings: array<FarPlanetRingRecord>;
@group(2) @binding(0) var scene_depth: texture_2d<f32>;
@group(2) @binding(1) var planet_depth: texture_2d<f32>;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn sanitize_scene_depth(depth: f32) -> f32 {
  let far_t = max(camera.distance_limits.y, 1.0);
  if (depth > 0.0 && depth < far_t) {
    return depth;
  }
  return NO_OCCLUDER_T;
}

fn safe_normalize(v: vec3<f32>, fallback: vec3<f32>) -> vec3<f32> {
  let len_sq = dot(v, v);
  if (len_sq <= 1e-8) {
    return fallback;
  }
  return v * inverseSqrt(len_sq);
}

fn view_ray_from_uv(uv: vec2<f32>) -> vec3<f32> {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = camera.inv_proj * clip;
  view = view / max(view.w, 1e-6);
  return normalize(view.xyz);
}

fn angular_radians_per_pixel() -> f32 {
  let tan_half_fov_y = max(abs(camera.inv_proj[1].y), 1e-4);
  let vertical_fov = max(2.0 * atan(tan_half_fov_y), 1e-4);
  return vertical_fov / max(camera.screen_size.y, 1.0);
}

fn profile_value(record: FarPlanetRingRecord, index: u32) -> f32 {
  let lane = index % 4u;
  let group = index / 4u;
  if (group == 0u) {
    return record.profile_0[lane];
  } else if (group == 1u) {
    return record.profile_1[lane];
  } else if (group == 2u) {
    return record.profile_2[lane];
  } else if (group == 3u) {
    return record.profile_3[lane];
  } else if (group == 4u) {
    return record.profile_4[lane];
  } else if (group == 5u) {
    return record.profile_5[lane];
  } else if (group == 6u) {
    return record.profile_6[lane];
  }
  return record.profile_7[lane];
}

fn sample_radial_profile(record: FarPlanetRingRecord, t: f32) -> f32 {
  let x = saturate(t) * 31.0;
  let i = min(u32(floor(x)), 30u);
  let f = fract(x);
  let a = profile_value(record, i);
  let b = profile_value(record, i + 1u);
  return mix(a, b, f);
}

fn hash_f32(v: vec3<f32>) -> f32 {
  return fract(sin(dot(v, vec3<f32>(127.1, 311.7, 74.7))) * 43758.5453123);
}

fn ring_local_noise(record: FarPlanetRingRecord, radial_t: f32, ring_dir: vec2<f32>) -> f32 {
  let seed = f32(bitcast<u32>(record.tint_seed.w) & 65535u) * 0.00037;
  let radial_bucket = floor(radial_t * 128.0);
  let fine = hash_f32(vec3<f32>(radial_bucket + seed, ring_dir.x * 96.0, ring_dir.y * 96.0));
  let angle = atan2(ring_dir.y, ring_dir.x);
  let bands =
    0.50 +
    0.25 * sin(radial_t * 96.0 + seed * 11.0) +
    0.12 * sin(radial_t * 227.0 + angle * 7.0 + seed * 29.0) +
    0.10 * sin(radial_t * 391.0 + angle * 17.0 + seed * 43.0) +
    0.10 * fine;
  return clamp(bands, 0.35, 1.15);
}

fn ring_texture_luma(opacity_pattern: f32) -> f32 {
  return clamp(0.42 + opacity_pattern * 0.72, 0.28, 1.18);
}

fn parent_render_radius(record: FarPlanetRingRecord) -> f32 {
  let parent_center = record.parent_radius.xyz;
  let parent_radius = record.parent_radius.w;
  let min_angular_radius = angular_radians_per_pixel() * PARENT_DISC_MIN_PIXELS;
  let min_render_radius = tan(min_angular_radius) * max(length(parent_center), 1.0);
  return max(parent_radius, min_render_radius);
}

fn parent_disc_occlusion(record: FarPlanetRingRecord, ray: vec3<f32>, sample_t: f32) -> f32 {
  let parent_center = record.parent_radius.xyz;
  if (-parent_center.z <= 0.0) {
    return 1.0;
  }
  let parent_radius = parent_render_radius(record);
  let center_t = dot(parent_center, ray);
  if (center_t <= 0.0) {
    return 1.0;
  }
  let closest = parent_center - ray * center_t;
  let closest_sq = dot(closest, closest);
  let radius_sq = parent_radius * parent_radius;
  if (closest_sq > radius_sq) {
    return 1.0;
  }
  let half_chord = sqrt(max(radius_sq - closest_sq, 0.0));
  let planet_front_t = center_t - half_chord;
  if (planet_front_t > 0.0 && sample_t > planet_front_t) {
    return 0.0;
  }
  return 1.0;
}

fn front_back_ring_classification(record: FarPlanetRingRecord, sample_pos: vec3<f32>) -> f32 {
  let normal = normalize(record.normal_thickness.xyz);
  let parent_center = record.parent_radius.xyz;
  return sign(dot(sample_pos - parent_center, normal));
}

fn ring_light_direction(record: FarPlanetRingRecord) -> vec3<f32> {
  return safe_normalize(record.parent_depth_light.yzw, vec3<f32>(0.0, 1.0, 0.0));
}

fn sun_facing_brightness(record: FarPlanetRingRecord) -> f32 {
  let normal = normalize(record.normal_thickness.xyz);
  let sun_dir = ring_light_direction(record);
  return 0.35 + 0.65 * saturate(abs(dot(normal, sun_dir)));
}

fn parent_planet_shadow(record: FarPlanetRingRecord, sample_pos: vec3<f32>) -> f32 {
  let parent_center = record.parent_radius.xyz;
  let parent_radius = record.parent_radius.w;
  let sun_dir = ring_light_direction(record);
  let shadow_axis = -sun_dir;
  let from_parent = sample_pos - parent_center;
  let axial = dot(from_parent, shadow_axis);
  if (axial <= 0.0) {
    return 1.0;
  }
  let lateral = length(from_parent - shadow_axis * axial);
  return smoothstep(parent_radius * 0.72, parent_radius * 1.08, lateral);
}

fn near_field_sample_fade(sample_pos: vec3<f32>) -> f32 {
  let camera_distance = length(sample_pos);
  let local_fade = smoothstep(NEAR_FIELD_FADE_INNER_METERS, NEAR_FIELD_FADE_OUTER_METERS, camera_distance);
  return mix(NEAR_FIELD_FADE_MIN_VISIBILITY, 1.0, local_fade);
}

struct RingHit {
  valid: bool,
  position: vec3<f32>,
  edge_on: f32,
  thickness_fade: f32,
};

struct RingHits {
  first: RingHit,
  second: RingHit,
};

struct RingInterval {
  valid: bool,
  enter: f32,
  exit: f32,
};

fn invalid_ring_hit() -> RingHit {
  return RingHit(false, vec3<f32>(0.0), 0.0, 0.0);
}

fn invalid_ring_interval() -> RingInterval {
  return RingInterval(false, 0.0, 0.0);
}

fn valid_ring_interval(segment_enter: f32, segment_exit: f32, min_t: f32) -> RingInterval {
  let enter = max(segment_enter, min_t);
  if (segment_exit <= enter) {
    return invalid_ring_interval();
  }
  return RingInterval(true, enter, segment_exit);
}

fn dust_haze_half_thickness(record: FarPlanetRingRecord) -> f32 {
  let thickness_scale = max(record.dust_haze_params.y, 1.0);
  let min_half_thickness = max(record.dust_haze_params.z, 1.0);
  return max(record.normal_thickness.w * thickness_scale, min_half_thickness);
}

fn dust_haze_max_sample_count(record: FarPlanetRingRecord) -> u32 {
  return u32(clamp(floor(record.dust_haze_lighting.y + 0.5), 1.0, 8.0));
}

fn dust_haze_adaptive_sample_count(record: FarPlanetRingRecord, ray: vec3<f32>, path_len: f32, dust_opacity: f32, max_alpha: f32) -> u32 {
  let max_count = dust_haze_max_sample_count(record);
  if (max_count <= 1u) {
    return max_count;
  }

  let path_t = smoothstep(0.12, 0.95, saturate(path_len / DUST_HAZE_PATH_FULL_METERS));
  let normal = normalize(record.normal_thickness.xyz);
  let edge_on_t = 1.0 - smoothstep(0.025, 0.20, abs(dot(ray, normal)));
  let density_t = smoothstep(0.015, 0.10, saturate(dust_opacity * max_alpha));
  let need = clamp(max(path_t, edge_on_t * 0.85) * density_t, 0.0, 1.0);
  let count_f = mix(1.0, f32(max_count), need);
  return u32(clamp(floor(count_f + 0.5), 1.0, f32(max_count)));
}

fn dust_haze_phase_luma(record: FarPlanetRingRecord, ray: vec3<f32>) -> f32 {
  let strength = clamp(record.dust_haze_lighting.z, 0.0, 1.0);
  let forward = pow(saturate(dot(ray, ring_light_direction(record)) * 0.5 + 0.5), 3.0);
  return mix(1.0, 0.78 + forward * 0.55, strength);
}

fn dust_haze_color(record: FarPlanetRingRecord, optical_density: f32, ray: vec3<f32>) -> vec3<f32> {
  let density_t = smoothstep(0.10, 0.85, optical_density);
  let tint = max(record.tint_seed.rgb, vec3<f32>(0.0));
  let tint_luma = dot(tint, vec3<f32>(0.299, 0.587, 0.114));
  let desaturated_tint = mix(vec3<f32>(tint_luma), tint, 0.28);
  let thin_color = desaturated_tint * vec3<f32>(0.56, 0.64, 0.82);
  let dense_color = mix(tint, tint * vec3<f32>(1.18, 1.03, 0.82), 0.48);
  let density_luma = mix(0.82, 1.18, density_t);
  return mix(thin_color, dense_color, density_t) * density_luma * sun_facing_brightness(record) * dust_haze_phase_luma(record, ray);
}

fn edge_ring_hit_at_t(ray: vec3<f32>, center: vec3<f32>, normal: vec3<f32>, half_thickness: f32, t_hit: f32) -> RingHit {
  if (t_hit <= 0.0) {
    return invalid_ring_hit();
  }
  let rel = -center + ray * t_hit;
  let sample_t = length(ray * t_hit);
  let apparent_half_thickness = angular_radians_per_pixel() * EDGE_ON_MIN_PIXELS * max(sample_t, 1.0);
  let effective_half_thickness = max(half_thickness, apparent_half_thickness);
  let height = abs(dot(rel, normal));
  if (height > effective_half_thickness) {
    return invalid_ring_hit();
  }
  let thickness_fade = 1.0 - smoothstep(max(effective_half_thickness * 0.55, 0.001), max(effective_half_thickness, 0.002), height);
  return RingHit(true, ray * t_hit, 1.0, thickness_fade);
}

fn annulus_edge_hits(ray: vec3<f32>, center: vec3<f32>, normal: vec3<f32>, inner: f32, outer: f32, half_thickness: f32) -> RingHits {
  let rel0 = -center;
  let denom = dot(ray, normal);
  let q0 = rel0 - normal * dot(rel0, normal);
  let d = ray - normal * denom;
  let a = dot(d, d);
  if (a <= 1e-8) {
    return RingHits(invalid_ring_hit(), invalid_ring_hit());
  }

  let b = 2.0 * dot(q0, d);
  let c_outer = dot(q0, q0) - outer * outer;
  let disc_outer = b * b - 4.0 * a * c_outer;
  if (disc_outer < 0.0) {
    return RingHits(invalid_ring_hit(), invalid_ring_hit());
  }

  let root_outer = sqrt(disc_outer);
  var t_outer_entry = (-b - root_outer) / (2.0 * a);
  let t_outer_exit = (-b + root_outer) / (2.0 * a);
  if (t_outer_exit <= 0.0) {
    return RingHits(invalid_ring_hit(), invalid_ring_hit());
  }
  if (t_outer_entry <= 0.0) {
    t_outer_entry = max(0.001, t_outer_entry);
  }

  var first_t = t_outer_entry;
  var first_rel = rel0 + ray * first_t;
  var first_radius = length(first_rel - normal * dot(first_rel, normal));
  if (first_radius < inner) {
    let c_inner = dot(q0, q0) - inner * inner;
    let disc_inner = b * b - 4.0 * a * c_inner;
    if (disc_inner < 0.0) {
      return RingHits(invalid_ring_hit(), invalid_ring_hit());
    }
    let root_inner = sqrt(disc_inner);
    first_t = (-b + root_inner) / (2.0 * a);
    if (first_t <= 0.0 || first_t > t_outer_exit) {
      return RingHits(invalid_ring_hit(), invalid_ring_hit());
    }
  }

  var second_t = t_outer_exit;
  var second_rel = rel0 + ray * second_t;
  var second_radius = length(second_rel - normal * dot(second_rel, normal));
  if (second_radius < inner) {
    let c_inner = dot(q0, q0) - inner * inner;
    let disc_inner = b * b - 4.0 * a * c_inner;
    if (disc_inner >= 0.0) {
      let root_inner = sqrt(disc_inner);
      second_t = (-b - root_inner) / (2.0 * a);
    }
  }

  let first = edge_ring_hit_at_t(ray, center, normal, half_thickness, first_t);
  var second = edge_ring_hit_at_t(ray, center, normal, half_thickness, second_t);
  if (abs(second_t - first_t) < 1.0) {
    second = invalid_ring_hit();
  }
  return RingHits(first, second);
}

fn ring_ray_hits(ray: vec3<f32>, record: FarPlanetRingRecord) -> RingHits {
  let center = record.center_opacity.xyz;
  let normal = normalize(record.normal_thickness.xyz);
  let inner = record.tangent_u_inner.w;
  let outer = record.tangent_v_outer.w;
  let half_thickness = max(record.normal_thickness.w, 1.0);
  let rel0 = -center;
  let denom = dot(ray, normal);
  let h0 = dot(rel0, normal);

  if (abs(denom) < 1e-4) {
    return annulus_edge_hits(ray, center, normal, inner, outer, half_thickness);
  }

  let t_hit = -h0 / denom;
  if (t_hit <= 0.0) {
    return RingHits(invalid_ring_hit(), invalid_ring_hit());
  }
  return RingHits(RingHit(true, ray * t_hit, saturate(1.0 - abs(denom) * 8.0), 1.0), invalid_ring_hit());
}

fn ring_volume_interval(ray: vec3<f32>, record: FarPlanetRingRecord, max_t: f32) -> RingInterval {
  let center = record.center_opacity.xyz;
  let normal = normalize(record.normal_thickness.xyz);
  let inner = record.tangent_u_inner.w;
  let outer = record.tangent_v_outer.w;
  let half_thickness = dust_haze_half_thickness(record);
  let rel0 = -center;
  let h0 = dot(rel0, normal);
  let denom = dot(ray, normal);

  var enter = 0.0;
  var exit = max_t;
  if (abs(denom) < 1e-5) {
    if (abs(h0) > half_thickness) {
      return invalid_ring_interval();
    }
  } else {
    let ta = (-half_thickness - h0) / denom;
    let tb = (half_thickness - h0) / denom;
    enter = max(enter, min(ta, tb));
    exit = min(exit, max(ta, tb));
  }
  if (exit <= max(enter, 0.0)) {
    return invalid_ring_interval();
  }

  let q0 = rel0 - normal * h0;
  let d = ray - normal * denom;
  let a = dot(d, d);
  let min_t = DUST_HAZE_NEAR_CLEAR_METERS;
  if (a <= 1e-8) {
    let radius = length(q0);
    if (radius < inner || radius > outer) {
      return invalid_ring_interval();
    }
    return valid_ring_interval(max(enter, 0.0), exit, min_t);
  } else {
    let b = 2.0 * dot(q0, d);
    let c_outer = dot(q0, q0) - outer * outer;
    let disc_outer = b * b - 4.0 * a * c_outer;
    if (disc_outer < 0.0) {
      return invalid_ring_interval();
    }
    let root_outer = sqrt(disc_outer);
    enter = max(enter, (-b - root_outer) / (2.0 * a));
    exit = min(exit, (-b + root_outer) / (2.0 * a));
    if (exit <= max(enter, 0.0)) {
      return invalid_ring_interval();
    }

    let c_inner = dot(q0, q0) - inner * inner;
    let disc_inner = b * b - 4.0 * a * c_inner;
    if (disc_inner < 0.0) {
      return valid_ring_interval(max(enter, 0.0), exit, min_t);
    }

    enter = max(enter, 0.0);
    if (exit <= max(enter, min_t)) {
      return invalid_ring_interval();
    }

    let root_inner = sqrt(disc_inner);
    let inner_enter = (-b - root_inner) / (2.0 * a);
    let inner_exit = (-b + root_inner) / (2.0 * a);
    if (inner_exit <= enter || inner_enter >= exit) {
      return valid_ring_interval(enter, exit, min_t);
    }

    let first = valid_ring_interval(enter, min(exit, inner_enter), min_t);
    if (first.valid) {
      return first;
    }
    return valid_ring_interval(max(enter, inner_exit), exit, min_t);
  }
}

fn ring_volume_haze_density_at_sample(ring: FarPlanetRingRecord, sample_pos: vec3<f32>, sample_t: f32) -> f32 {
  let center = ring.center_opacity.xyz;
  let normal = normalize(ring.normal_thickness.xyz);
  let rel = sample_pos - center;
  let u = dot(rel, normalize(ring.tangent_u_inner.xyz));
  let v = dot(rel, normalize(ring.tangent_v_outer.xyz));
  let radius = length(vec2<f32>(u, v));
  let inner = ring.tangent_u_inner.w;
  let outer = ring.tangent_v_outer.w;
  let width = max(outer - inner, 1e-5);
  if (radius <= inner || radius >= outer) {
    return 0.0;
  }

  let radial_t = (radius - inner) / width;
  let radial_edge_fraction = clamp(ring.dust_haze_params.w, 0.001, 0.25);
  let radial_edge_width = max(width * radial_edge_fraction, 1.0);
  let radial_edge_fade = smoothstep(inner, inner + radial_edge_width, radius) *
    (1.0 - smoothstep(outer - radial_edge_width, outer, radius));

  let half_thickness = dust_haze_half_thickness(ring);
  let height = abs(dot(rel, normal));
  let vertical_core = clamp(ring.dust_haze_lighting.x, 0.001, 0.95);
  let vertical_density = 1.0 - smoothstep(half_thickness * vertical_core, half_thickness, height);

  let ring_dir = vec2<f32>(u, v) / max(radius, 1e-5);
  let profile_opacity = sample_radial_profile(ring, radial_t) * ring_local_noise(ring, radial_t, ring_dir);
  let range_fade = smoothstep(DUST_HAZE_NEAR_CLEAR_METERS, DUST_HAZE_PATH_FULL_METERS, sample_t);
  let shadow_strength = clamp(ring.dust_haze_lighting.w, 0.0, 1.0);
  let shadow_visibility = mix(1.0, parent_planet_shadow(ring, sample_pos), shadow_strength);
  return clamp(profile_opacity, 0.0, 1.0) * radial_edge_fade * vertical_density * range_fade * shadow_visibility;
}

fn shade_ring_volume_haze(ring: FarPlanetRingRecord, ray: vec3<f32>, scene_t: f32) -> vec4<f32> {
  let dust_opacity = clamp(ring.parent_depth_light.x, 0.0, 1.0);
  if (dust_opacity <= 0.001) {
    return vec4<f32>(0.0);
  }
  let interval = ring_volume_interval(ray, ring, scene_t);
  if (!interval.valid) {
    return vec4<f32>(0.0);
  }
  let start_t = max(interval.enter, DUST_HAZE_NEAR_CLEAR_METERS);
  let end_t = interval.exit;
  let path_len = max(end_t - start_t, 0.0);
  if (path_len <= 0.0) {
    return vec4<f32>(0.0);
  }

  let max_alpha = clamp(ring.dust_haze_params.x, 0.0, 1.0);
  let sample_count = dust_haze_adaptive_sample_count(ring, ray, path_len, dust_opacity, max_alpha);
  let sample_count_f = f32(sample_count);
  let step_len = path_len / sample_count_f;
  var integrated_density = 0.0;
  for (var i = 0u; i < sample_count; i = i + 1u) {
    let sample_t = start_t + (f32(i) + 0.5) * step_len;
    let sample_pos = ray * sample_t;
    integrated_density = integrated_density + ring_volume_haze_density_at_sample(ring, sample_pos, sample_t) * step_len;
  }

  let optical_density = smoothstep(0.0, 1.0, saturate(integrated_density / DUST_HAZE_PATH_FULL_METERS));
  let alpha = saturate(dust_opacity * max_alpha * optical_density);
  if (alpha <= 0.001) {
    return vec4<f32>(0.0);
  }
  let color = dust_haze_color(ring, optical_density, ray);
  return vec4<f32>(color * alpha, alpha);
}

fn shade_ring_hit(ring: FarPlanetRingRecord, ray: vec3<f32>, hit: RingHit, scene_t: f32) -> vec4<f32> {
  if (!hit.valid) {
    return vec4<f32>(0.0);
  }
  let center = ring.center_opacity.xyz;
  let sample_pos = hit.position;
  let sample_t = length(sample_pos);
  if (sample_t > scene_t + 2.0) {
    return vec4<f32>(0.0);
  }
  let rel = sample_pos - center;
  let u = dot(rel, normalize(ring.tangent_u_inner.xyz));
  let v = dot(rel, normalize(ring.tangent_v_outer.xyz));
  let radius = length(vec2<f32>(u, v));
  let inner = ring.tangent_u_inner.w;
  let outer = ring.tangent_v_outer.w;
  if (radius < inner || radius > outer) {
    return vec4<f32>(0.0);
  }
  let radial_t = (radius - inner) / max(outer - inner, 1e-5);
  let edge_fade = smoothstep(inner, inner + max((outer - inner) * 0.02, 1.0), radius) *
    (1.0 - smoothstep(outer - max((outer - inner) * 0.02, 1.0), outer, radius));
  let ring_dir = vec2<f32>(u, v) / max(radius, 1e-5);
  let profile_opacity = sample_radial_profile(ring, radial_t) * ring_local_noise(ring, radial_t, ring_dir);
  let texture_luma = ring_texture_luma(profile_opacity);
  let parent_visibility = parent_disc_occlusion(ring, ray, sample_t);
  let shadow_visibility = parent_planet_shadow(ring, sample_pos);
  let near_field_fade = near_field_sample_fade(sample_pos);
  let _front_back = front_back_ring_classification(ring, sample_pos);
  let edge_boost = mix(1.0, 1.8, hit.edge_on);
  let alpha = ring.center_opacity.w * profile_opacity * edge_fade * hit.thickness_fade * parent_visibility * near_field_fade * edge_boost;
  let lit = sun_facing_brightness(ring);
  let shadow_luma = mix(0.32, 1.0, shadow_visibility);
  let color = ring.tint_seed.rgb * texture_luma * lit * shadow_luma;
  return vec4<f32>(color * alpha, alpha);
}

@vertex
fn vs_main(@builtin(vertex_index) vertex_index: u32) -> VSOut {
  var out: VSOut;
  let x = f32((vertex_index << 1u) & 2u);
  let y = f32(vertex_index & 2u);
  out.uv = vec2<f32>(x, y);
  out.position = vec4<f32>(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);
  return out;
}

@fragment
fn fs_main(in: VSOut) -> FSOut {
  let dim = textureDimensions(scene_depth);
  let pix = vec2<i32>(
    clamp(i32(in.position.x), 0, i32(dim.x) - 1),
    clamp(i32(in.position.y), 0, i32(dim.y) - 1),
  );
  let scene_t = sanitize_scene_depth(textureLoad(scene_depth, pix, 0).r);
  let planet_t = sanitize_scene_depth(textureLoad(planet_depth, pix, 0).r);
  let occluder_t = min(scene_t, planet_t);

  let ray = view_ray_from_uv(in.uv);
  var accum = vec4<f32>(0.0);
  let count = min(far_planet_ring_params.ring_count, 128u);
  for (var i = 0u; i < count; i = i + 1u) {
    let ring = far_planet_rings[i];
    let hits = ring_ray_hits(ray, ring);
    let first = shade_ring_hit(ring, ray, hits.first, occluder_t);
    accum = accum + first * (1.0 - accum.a);
    let second = shade_ring_hit(ring, ray, hits.second, occluder_t);
    accum = accum + second * (1.0 - accum.a);
    let haze = shade_ring_volume_haze(ring, ray, occluder_t);
    accum = accum + haze * (1.0 - accum.a);
  }
  if (accum.a <= 0.001) {
    discard;
  }
  let alpha = saturate(accum.a);
  let weight = max(1e-3, alpha);
  var out: FSOut;
  out.accum = vec4<f32>(accum.rgb * alpha * weight, alpha);
  out.weight = alpha * weight;
  return out;
}
