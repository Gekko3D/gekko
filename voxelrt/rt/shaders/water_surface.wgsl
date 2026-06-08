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

struct DirectionalShadowCascade {
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
  params: vec4<f32>,
};

struct Light {
  position: vec4<f32>,
  direction: vec4<f32>,
  color: vec4<f32>,
  params: vec4<f32>,
  shadow_meta: vec4<u32>,
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
  directional_cascades: array<DirectionalShadowCascade, 2>,
};

struct TileLightListParams {
  tile_size: u32,
  tiles_x: u32,
  tiles_y: u32,
  max_lights_per_tile: u32,
  screen_width: u32,
  screen_height: u32,
  num_tiles: u32,
  pad0: u32,
};

struct TileLightHeader {
  offset: u32,
  count: u32,
  overflow: u32,
  pad0: u32,
};

struct WaterParams {
  header: vec4<u32>,
  params0: vec4<f32>,
};

struct WaterRecord {
  bounds: vec4<f32>,
  extents: vec4<f32>,
  color: vec4<f32>,
  absorption: vec4<f32>,
  flow: vec4<f32>,
  lighting: vec4<f32>,
  disturbance: vec4<u32>,
};

struct RippleRecord {
  position_age: vec4<f32>,
  params: vec4<f32>,
  motion: vec4<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
  @location(1) @interpolate(flat) water_index: u32,
};

struct FSOut {
  @location(0) accum: vec4<f32>,
  @location(1) weight: f32,
};

struct Ray {
  origin: vec3<f32>,
  dir: vec3<f32>,
  inv_dir: vec3<f32>,
};

struct Hit {
  valid: bool,
  t_enter: f32,
  t_exit: f32,
  pos: vec3<f32>,
  normal: vec3<f32>,
  water_index: u32,
};

const EPS: f32 = 1e-4;
const DEFAULT_WATER_CELL_SIZE: f32 = 0.2;
const PI: f32 = 3.14159265359;
const DISTURBANCE_IMPACT: u32 = 0u;
const DISTURBANCE_SKIM: u32 = 1u;
const DISTURBANCE_WAKE: u32 = 2u;
const MAX_WATER_LOCAL_LIGHT_REFLECTIONS: u32 = 8u;
const WATER_EDGE_MIN_X: u32 = 1u;
const WATER_EDGE_MAX_X: u32 = 2u;
const WATER_EDGE_MIN_Z: u32 = 4u;
const WATER_EDGE_MAX_Z: u32 = 8u;
const WATER_SHAPE_BOX: u32 = 0u;
const WATER_SHAPE_FOOTPRINT: u32 = 1u;

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(1) @binding(0) var<uniform> water_params: WaterParams;
@group(1) @binding(1) var<storage, read> waters: array<WaterRecord>;
@group(1) @binding(2) var<storage, read> ripples: array<RippleRecord>;
@group(2) @binding(0) var scene_depth: texture_2d<f32>;
@group(2) @binding(1) var opaque_lit: texture_2d<f32>;
@group(3) @binding(0) var<storage, read> lights: array<Light>;
@group(3) @binding(1) var<uniform> tile_light_params: TileLightListParams;
@group(3) @binding(2) var<storage, read> tile_light_headers: array<TileLightHeader>;
@group(3) @binding(3) var<storage, read> tile_light_indices: array<u32>;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn tile_index_for_water_uv(uv: vec2<f32>) -> u32 {
  if (tile_light_params.num_tiles == 0u || tile_light_params.tiles_x == 0u || tile_light_params.tiles_y == 0u) {
    return 0u;
  }
  let width = max(tile_light_params.screen_width, 1u);
  let height = max(tile_light_params.screen_height, 1u);
  let px = vec2<u32>(
    u32(clamp(uv.x * f32(width), 0.0, f32(width - 1u))),
    u32(clamp(uv.y * f32(height), 0.0, f32(height - 1u))),
  );
  let tile_size = max(tile_light_params.tile_size, 1u);
  let tile_coord = min(
    px / tile_size,
    vec2<u32>(tile_light_params.tiles_x - 1u, tile_light_params.tiles_y - 1u),
  );
  return min(tile_coord.y * tile_light_params.tiles_x + tile_coord.x, tile_light_params.num_tiles - 1u);
}

fn local_light_reflection(pos: vec3<f32>, normal: vec3<f32>, view_dir: vec3<f32>, roughness: f32, fresnel: f32, uv: vec2<f32>) -> vec3<f32> {
  if (camera.num_lights == 0u || tile_light_params.num_tiles == 0u) {
    return vec3<f32>(0.0);
  }

  let tile_header = tile_light_headers[tile_index_for_water_uv(uv)];
  let count = min(tile_header.count, MAX_WATER_LOCAL_LIGHT_REFLECTIONS);
  var reflected = vec3<f32>(0.0);
  let tight_shininess = mix(44.0, 12.0, roughness);
  let broad_shininess = mix(8.0, 3.0, roughness);
  let roughness_scale = mix(1.35, 0.42, roughness);
  let fresnel_scale = mix(0.72, 1.65, fresnel);

  for (var i = 0u; i < count; i++) {
    let light_idx = tile_light_indices[tile_header.offset + i];
    if (light_idx >= camera.num_lights) {
      continue;
    }
    let light = lights[light_idx];
    let light_type = u32(light.params.z);
    if (light_type == 1u) {
      continue;
    }

    let range = light.params.x;
    let l_vec = light.position.xyz - pos;
    let dist_to_light = length(l_vec);
    if (range <= 0.0 || dist_to_light >= range) {
      continue;
    }

    let l_dir = l_vec / max(dist_to_light, 1e-4);
    var attenuation = 0.0;
    let dist_sq = dist_to_light * dist_to_light;
    let factor = dist_to_light / range;
    let smooth_factor = max(0.0, 1.0 - factor * factor);
    attenuation = (1.0 / (dist_sq + 1.0)) * smooth_factor * smooth_factor * 24.0;

    if (light_type == 2u) {
      let spot_dir = normalize(light.direction.xyz);
      let cos_cur = dot(-l_dir, spot_dir);
      let cos_cone = light.params.y;
      if (cos_cur < cos_cone) {
        continue;
      }
      attenuation *= smoothstep(cos_cone, min(cos_cone + 0.1, 1.0), cos_cur);
    }

    let ndotl = saturate(dot(normal, l_dir));
    if (ndotl <= 0.0 || attenuation <= 0.0) {
      continue;
    }
    let half_dir = normalize(l_dir + view_dir);
    let half_angle = saturate(dot(normal, half_dir));
    let reflected_dir = reflect(-l_dir, normal);
    let reflected_angle = saturate(dot(reflected_dir, view_dir));
    let tight_spec = pow(max(half_angle, reflected_angle), tight_shininess);
    let broad_spec = pow(reflected_angle, broad_shininess) * 0.22;
    let spec = (tight_spec + broad_spec) * roughness_scale * fresnel_scale;
    reflected += light.color.xyz * light.color.w * attenuation * ndotl * spec * 0.85;
  }
  return reflected;
}

fn make_ray(uv: vec2<f32>) -> Ray {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = camera.inv_proj * clip;
  view = view / max(view.w, 1e-6);
  let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
  let dir_ws = normalize(world_target - camera.cam_pos.xyz);
  let safe_dir = vec3<f32>(
    select(dir_ws.x, 1e-5, abs(dir_ws.x) < 1e-5),
    select(dir_ws.y, 1e-5, abs(dir_ws.y) < 1e-5),
    select(dir_ws.z, 1e-5, abs(dir_ws.z) < 1e-5)
  );
  return Ray(camera.cam_pos.xyz, safe_dir, 1.0 / safe_dir);
}

fn camera_far_t() -> f32 {
  return max(camera.distance_limits.y, 1.0);
}

fn finite_depth_limit() -> f32 {
  return camera_far_t() - max(camera_far_t() * 1e-5, 1e-3);
}

fn scene_depth_has_hit(depth: f32) -> bool {
  return depth > 0.0 && depth < finite_depth_limit();
}

fn water_cell_size(water: WaterRecord) -> f32 {
  return select(DEFAULT_WATER_CELL_SIZE, clamp(water.flow.w, 0.05, 1.0), water.flow.w > 0.0);
}

fn intersect_aabb(ray: Ray, min_b: vec3<f32>, max_b: vec3<f32>) -> vec2<f32> {
  let t0s = (min_b - ray.origin) * ray.inv_dir;
  let t1s = (max_b - ray.origin) * ray.inv_dir;
  let tsmaller = min(t0s, t1s);
  let tbigger = max(t0s, t1s);
  let tmin = max(tsmaller.x, max(tsmaller.y, tsmaller.z));
  let tmax = min(tbigger.x, min(tbigger.y, tbigger.z));
  return vec2<f32>(tmin, tmax);
}

fn box_normal(pos: vec3<f32>, min_b: vec3<f32>, max_b: vec3<f32>) -> vec3<f32> {
  if (abs(pos.x - min_b.x) < 0.02) { return vec3<f32>(-1.0, 0.0, 0.0); }
  if (abs(pos.x - max_b.x) < 0.02) { return vec3<f32>(1.0, 0.0, 0.0); }
  if (abs(pos.y - min_b.y) < 0.02) { return vec3<f32>(0.0, -1.0, 0.0); }
  if (abs(pos.y - max_b.y) < 0.02) { return vec3<f32>(0.0, 1.0, 0.0); }
  if (abs(pos.z - min_b.z) < 0.02) { return vec3<f32>(0.0, 0.0, -1.0); }
  return vec3<f32>(0.0, 0.0, 1.0);
}

fn stepped_wave_height(pos_xz: vec2<f32>, water: WaterRecord) -> f32 {
  let cell_size = water_cell_size(water);
  let flow_dir = normalize(select(vec2<f32>(1.0, 0.0), water.flow.xy, length(water.flow.xy) > 1e-5));
  let cell = floor(pos_xz / cell_size);
  let phase = dot(cell, flow_dir * 0.72) + water_params.params0.x * water.flow.z;
  let wave = sin(phase * 1.3) + cos(phase * 0.71 + dot(cell, vec2<f32>(0.25, -0.18)));
  let detail_phase = dot(cell, vec2<f32>(-0.47, 0.31)) + water_params.params0.x * water.flow.z * 0.63;
  let detail = sin(detail_phase * 2.1) * 0.38;
  let stepped = floor((wave + detail) * 1.75) / 1.75;
  return stepped * water.absorption.w;
}

fn ripple_height(pos_xz: vec2<f32>, water: WaterRecord) -> f32 {
  var total = 0.0;
  let start = water.disturbance.x;
  let end = min(start + water.disturbance.y, water_params.header.y);
  for (var i: u32 = start; i < end; i = i + 1u) {
    let ripple = ripples[i];
    let age = ripple.position_age.w;
    let lifetime = max(ripple.params.y, 0.01);
    let strength = ripple.params.x;
    let radius = max(ripple.motion.z, 0.05);
    let kind = u32(ripple.params.w);
    let life_t = clamp(age / lifetime, 0.0, 1.0);
    let fade = (1.0 - life_t) * (1.0 - life_t);
    if (fade <= 1e-3) {
      continue;
    }
    let delta = pos_xz - ripple.position_age.xz;
    let dist = length(delta);
    let wake_dir = normalize(select(vec2<f32>(1.0, 0.0), ripple.motion.xy, length(ripple.motion.xy) > 1e-4));
    let along = dot(delta, wake_dir);
    let across = length(delta - wake_dir * along);
    let speed_bias = clamp(length(ripple.motion.xy) / 9.0, 0.0, 1.0);
    let kind_speed = select(mix(2.2, 5.0, clamp(strength, 0.0, 1.0)), mix(3.2, 6.8, speed_bias), kind == DISTURBANCE_SKIM || kind == DISTURBANCE_WAKE);
    let ring_radius = radius * mix(0.6, 1.25, clamp(strength, 0.0, 1.0)) + age * kind_speed;
    let ring_width = radius * 0.18 + mix(0.16, 0.36, clamp(strength, 0.0, 1.0));
    var band = exp(-pow((dist - ring_radius) / max(ring_width, 1e-3), 2.0));
    if (kind == DISTURBANCE_SKIM) {
      let directional = saturate(along / max(ring_radius, 0.1));
      band *= exp(-pow(across / max(radius * 1.8 + age * 0.45, 1e-3), 2.0)) * directional;
    }
    if (kind == DISTURBANCE_WAKE) {
      let behind = saturate(-along / max(age * 2.2 + radius, 0.1));
      band *= exp(-pow(across / max(radius * 1.4 + age * 0.22, 1e-3), 2.0)) * behind;
    }
    let crest = sin((dist - ring_radius) * mix(11.0, 18.0, clamp(strength, 0.0, 1.0)) - age * 10.0);
    let depression = -exp(-pow(dist / max(radius * 1.3 + age * 0.35, 1e-3), 2.0)) * strength * fade * select(0.035, 0.018, kind == DISTURBANCE_WAKE);
    total += band * crest * fade * strength * 0.17 + depression;
  }
  return total;
}

fn water_surface_height(pos_xz: vec2<f32>, water: WaterRecord) -> f32 {
  let base = stepped_wave_height(pos_xz, water);
  let ripple = ripple_height(pos_xz, water);
  return base + ripple;
}

fn stepped_water_normal(pos: vec3<f32>, water: WaterRecord) -> vec3<f32> {
  let cell_size = water_cell_size(water);
  let dx = vec2<f32>(cell_size, 0.0);
  let dz = vec2<f32>(0.0, cell_size);
  let h_l = water_surface_height(pos.xz - dx, water);
  let h_r = water_surface_height(pos.xz + dx, water);
  let h_d = water_surface_height(pos.xz - dz, water);
  let h_u = water_surface_height(pos.xz + dz, water);
  return normalize(vec3<f32>(h_l - h_r, cell_size * 2.0, h_d - h_u));
}

fn water_edge_distance(pos: vec3<f32>, water: WaterRecord) -> f32 {
  let min_x_dist = pos.x - (water.bounds.x - water.extents.x);
  let max_x_dist = (water.bounds.x + water.extents.x) - pos.x;
  let min_z_dist = pos.z - (water.bounds.z - water.extents.y);
  let max_z_dist = (water.bounds.z + water.extents.y) - pos.z;
  let edge_mask = water.disturbance.z;
  let far_edge = 1e6;
  let x_min = select(min_x_dist, far_edge, (edge_mask & WATER_EDGE_MIN_X) != 0u);
  let x_max = select(max_x_dist, far_edge, (edge_mask & WATER_EDGE_MAX_X) != 0u);
  let z_min = select(min_z_dist, far_edge, (edge_mask & WATER_EDGE_MIN_Z) != 0u);
  let z_max = select(max_z_dist, far_edge, (edge_mask & WATER_EDGE_MAX_Z) != 0u);
  return min(min(x_min, x_max), min(z_min, z_max));
}

fn water_edge_factor(pos: vec3<f32>, water: WaterRecord) -> f32 {
  let cell_size = water_cell_size(water);
  let edge_dist = water_edge_distance(pos, water);
  let band = 1.0 - saturate(edge_dist / (cell_size * 3.5));
  return floor(band * 4.0) / 4.0;
}

fn water_foam_factor(pos: vec3<f32>, water: WaterRecord, thickness: f32, ndotv: f32, is_top: bool) -> f32 {
  if (!is_top) {
    return 0.0;
  }
  let cell_size = water_cell_size(water);
  let edge_dist = water_edge_distance(pos, water);
  let edge_band = 1.0 - saturate(edge_dist / (cell_size * 2.2));
  let shallow_band = 1.0 - saturate(thickness / (cell_size * 7.0));
  let grazing = pow(1.0 - saturate(ndotv), 2.0);
  let foam = max(edge_band, shallow_band * 0.45) * (0.72 + grazing * 0.28);
  return floor(saturate(foam) * 4.0) / 4.0;
}

fn disturbance_foam_factor(pos_xz: vec2<f32>, water: WaterRecord) -> f32 {
  var total = 0.0;
  let start = water.disturbance.x;
  let end = min(start + water.disturbance.y, water_params.header.y);
  for (var i: u32 = start; i < end; i = i + 1u) {
    let ripple = ripples[i];
    let age = ripple.position_age.w;
    let lifetime = max(ripple.params.y, 0.01);
    let life_t = clamp(age / lifetime, 0.0, 1.0);
    let fade = (1.0 - life_t) * (1.0 - life_t);
    if (fade <= 1e-3) {
      continue;
    }
    let strength = ripple.params.x;
    let radius = max(ripple.motion.z, 0.05);
    let foam = max(ripple.motion.w, 0.0);
    let kind = u32(ripple.params.w);
    let delta = pos_xz - ripple.position_age.xz;
    let dist = length(delta);
    let wake_dir = normalize(select(vec2<f32>(1.0, 0.0), ripple.motion.xy, length(ripple.motion.xy) > 1e-4));
    let along = dot(delta, wake_dir);
    let across = length(delta - wake_dir * along);
    let speed_bias = clamp(length(ripple.motion.xy) / 9.0, 0.0, 1.0);
    let ring_radius = radius + age * mix(2.6, 6.2, max(strength * 0.55, speed_bias));
    let ring_width = radius * 0.28 + mix(0.18, 0.42, strength);
    var band = exp(-pow((dist - ring_radius) / max(ring_width, 1e-3), 2.0));
    if (kind == DISTURBANCE_SKIM) {
      band *= exp(-pow(across / max(radius * 2.0 + age * 0.5, 1e-3), 2.0)) * saturate(along / max(ring_radius, 0.1));
    }
    if (kind == DISTURBANCE_WAKE) {
      band *= exp(-pow(across / max(radius * 1.55 + age * 0.28, 1e-3), 2.0)) * saturate(-along / max(age * 2.4 + radius, 0.1));
    }
    total += band * fade * foam;
  }
  return floor(saturate(total) * 4.0) / 4.0;
}

fn water_side_masked(normal: vec3<f32>, water: WaterRecord) -> bool {
  let edge_mask = water.disturbance.z;
  if (normal.x < -0.9 && (edge_mask & WATER_EDGE_MIN_X) != 0u) {
    return true;
  }
  if (normal.x > 0.9 && (edge_mask & WATER_EDGE_MAX_X) != 0u) {
    return true;
  }
  if (normal.z < -0.9 && (edge_mask & WATER_EDGE_MIN_Z) != 0u) {
    return true;
  }
  if (normal.z > 0.9 && (edge_mask & WATER_EDGE_MAX_Z) != 0u) {
    return true;
  }
  return false;
}

fn water_shape_kind(water: WaterRecord) -> u32 {
  return water.disturbance.w;
}

fn point_inside_water_footprint(pos: vec3<f32>, water: WaterRecord) -> bool {
  let local = abs(pos.xz - water.bounds.xz);
  return local.x <= water.extents.x + EPS && local.y <= water.extents.y + EPS;
}

fn sample_opaque(uv: vec2<f32>) -> vec3<f32> {
  let dims = textureDimensions(opaque_lit);
  let coord = vec2<i32>(
    clamp(i32(uv.x * f32(dims.x)), 0, i32(dims.x) - 1),
    clamp(i32(uv.y * f32(dims.y)), 0, i32(dims.y) - 1),
  );
  return textureLoad(opaque_lit, coord, 0).rgb;
}

fn sample_scene_depth(uv: vec2<f32>) -> f32 {
  let dims = textureDimensions(scene_depth);
  let coord = vec2<i32>(
    clamp(i32(uv.x * f32(dims.x)), 0, i32(dims.x) - 1),
    clamp(i32(uv.y * f32(dims.y)), 0, i32(dims.y) - 1),
  );
  return textureLoad(scene_depth, coord, 0).r;
}

fn refraction_weight(base_depth: f32, refracted_depth: f32, hit_t: f32, cell_size: f32) -> f32 {
  if (!scene_depth_has_hit(refracted_depth)) {
    return 0.35;
  }
  if (refracted_depth < hit_t - cell_size * 0.5) {
    return 0.0;
  }
  if (!scene_depth_has_hit(base_depth)) {
    return 0.5;
  }
  let discontinuity = abs(refracted_depth - base_depth);
  return 1.0 - saturate((discontinuity - cell_size * 2.0) / (cell_size * 12.0));
}

@vertex
fn vs_main(@builtin(vertex_index) vi: u32, @builtin(instance_index) ii: u32) -> VSOut {
  var out: VSOut;
  let x = f32((vi << 1u) & 2u);
  let y = f32(vi & 2u);
  out.position = vec4<f32>(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);
  out.uv = vec2<f32>(x, y);
  out.water_index = ii;
  return out;
}

@fragment
fn fs_main(in: VSOut) -> FSOut {
  let dims = textureDimensions(scene_depth);
  let ipos = vec2<i32>(
    clamp(i32(in.position.x), 0, i32(dims.x) - 1),
    clamp(i32(in.position.y), 0, i32(dims.y) - 1),
  );
  let uv_screen = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let t_scene = textureLoad(scene_depth, ipos, 0).r;
  let ray = make_ray(uv_screen);

  let far_t = camera_far_t();
  var hit = Hit(false, far_t, far_t, vec3<f32>(0.0), vec3<f32>(0.0), 0u);
  if (in.water_index >= water_params.header.x) {
    discard;
  }
  let water_for_intersect = waters[in.water_index];
  let center = water_for_intersect.bounds.xyz;
  let depth = max(water_for_intersect.bounds.w, 0.01);
  let min_b = vec3<f32>(center.x - water_for_intersect.extents.x, center.y - depth, center.z - water_for_intersect.extents.y);
  let max_b = vec3<f32>(center.x + water_for_intersect.extents.x, center.y, center.z + water_for_intersect.extents.y);
  if (water_shape_kind(water_for_intersect) == WATER_SHAPE_FOOTPRINT) {
    if (abs(ray.dir.y) > 1e-5) {
      let t_plane = (center.y - ray.origin.y) / ray.dir.y;
      if (t_plane > 0.0 && !(scene_depth_has_hit(t_scene) && t_plane > t_scene + 0.03)) {
        let pos = ray.origin + ray.dir * t_plane;
        if (point_inside_water_footprint(pos, water_for_intersect)) {
          let scene_limited_exit = select(t_plane + depth, min(t_scene, t_plane + depth), scene_depth_has_hit(t_scene) && t_scene > t_plane);
          hit = Hit(true, t_plane, scene_limited_exit, pos, vec3<f32>(0.0, 1.0, 0.0), in.water_index);
        }
      }
    }
  } else {
    let span = intersect_aabb(ray, min_b, max_b);
    if (span.x <= span.y && span.y > 0.0) {
      let t_enter = max(span.x, 0.0);
      if (!(scene_depth_has_hit(t_scene) && t_enter > t_scene + 0.03)) {
        let pos = ray.origin + ray.dir * t_enter;
        hit = Hit(true, t_enter, span.y, pos, box_normal(pos, min_b, max_b), in.water_index);
      }
    }
  }

  if (!hit.valid) {
    discard;
  }

  let water = waters[hit.water_index];
  let cell_size = water_cell_size(water);
  let thickness = max(hit.t_exit - hit.t_enter, cell_size * 0.6);
  let is_top = hit.normal.y > 0.9;
  if (!is_top && water_side_masked(hit.normal, water)) {
    discard;
  }
  var normal = hit.normal;
  if (is_top) {
    normal = stepped_water_normal(hit.pos, water);
  }
  let ripple = ripple_height(hit.pos.xz, water);
  let edge_factor = water_edge_factor(hit.pos, water);

  let view_dir = normalize(camera.cam_pos.xyz - hit.pos);
  let light_dir = normalize(camera.light_pos.xyz - hit.pos);
  let half_dir = normalize(light_dir + view_dir);
  let ndotl = saturate(dot(normal, light_dir));
  let ndotv = saturate(dot(normal, view_dir));
  let fresnel = pow(1.0 - ndotv, 5.0);
  let roughness = water.extents.w;
  let shininess = mix(120.0, 16.0, roughness);
  let direct_light_exposure = clamp(water.lighting.x, 0.0, 1.0);
  let specular = pow(saturate(dot(normal, half_dir)), shininess) * mix(0.12, 0.7, 1.0 - roughness) * direct_light_exposure;
  let absorption = exp(-water.absorption.rgb * thickness * 0.18);
  let tint_strength = mix(0.24, 0.42, saturate(thickness / (cell_size * 10.0)));
  let tint = mix(vec3<f32>(1.0), water.color.rgb * absorption, tint_strength);

  var refract_uv = uv_screen;
  if (is_top) {
    let distort = normal.xz * water.color.w * 0.04 * min(thickness, 1.2);
    refract_uv += distort;
  }
  let clamped_refract_uv = clamp(refract_uv, vec2<f32>(0.0), vec2<f32>(1.0));
  let refracted_t = sample_scene_depth(clamped_refract_uv);
  let refract_mix = 0.82 * refraction_weight(t_scene, refracted_t, hit.t_enter, cell_size);
  let opaque_bg = sample_opaque(uv_screen);
  let refracted_bg = sample_opaque(clamped_refract_uv);

  let direct_lit = ndotl * 0.18 * direct_light_exposure + specular;
  let base_light = camera.ambient_color.rgb * (0.38 + 0.16 * normal.y) + vec3<f32>(direct_lit + fresnel * 0.06);
  var surface_rgb = water.color.rgb * base_light;
  if (!is_top) {
    surface_rgb *= 0.45;
  }
  if (is_top) {
    surface_rgb += vec3<f32>(0.12, 0.16, 0.2) * clamp(abs(ripple) * 3.0, 0.0, 1.0);
  }
  let foam_factor = max(water_foam_factor(hit.pos, water, thickness, ndotv, is_top), disturbance_foam_factor(hit.pos.xz, water));
  let edge_highlight = vec3<f32>(0.14, 0.24, 0.34) * edge_factor * select(1.0, 0.35, !is_top);
  let local_reflection = select(vec3<f32>(0.0), local_light_reflection(hit.pos, normal, view_dir, roughness, fresnel, uv_screen), is_top);
  let foam_rgb = mix(vec3<f32>(0.58, 0.82, 0.94), vec3<f32>(0.92, 0.98, 1.0), fresnel);
  let transmitted_bg = mix(opaque_bg, refracted_bg, refract_mix) * tint;
  let surface_mix = select(0.18, 0.08, !is_top);
  let shaded_surface = surface_rgb + vec3<f32>(specular) + local_reflection + water.color.rgb * fresnel * 0.12 + edge_highlight;
  let water_rgb = mix(shaded_surface, foam_rgb, foam_factor * 0.42);
  let final_rgb = mix(transmitted_bg, water_rgb, surface_mix + fresnel * 0.18 + edge_factor * 0.08 + foam_factor * 0.12) + local_reflection * mix(0.06, 0.14, fresnel);

  var alpha = water.extents.z * mix(0.42, 0.62, saturate(thickness / (cell_size * 8.0)));
  if (!is_top) {
    alpha *= 0.86;
  }
  alpha = min(alpha + edge_factor * 0.06 + foam_factor * 0.04, 0.74);
  let depth_norm = saturate(hit.t_enter / 90.0);
  let weight = max(1e-3, alpha) * pow(1.0 - depth_norm, 5.0);

  var out: FSOut;
  out.accum = vec4<f32>(final_rgb * alpha * weight, alpha);
  out.weight = alpha * weight;
  return out;
}
