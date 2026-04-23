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
};

struct RippleRecord {
  position_age: vec4<f32>,
  params: vec4<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
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
const VOXEL_SIZE: f32 = 0.1;
const PI: f32 = 3.14159265359;

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(1) @binding(0) var<uniform> water_params: WaterParams;
@group(1) @binding(1) var<storage, read> waters: array<WaterRecord>;
@group(1) @binding(2) var<storage, read> ripples: array<RippleRecord>;
@group(2) @binding(0) var scene_depth: texture_2d<f32>;
@group(2) @binding(1) var opaque_lit: texture_2d<f32>;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
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

fn camera_far_half() -> f32 {
  return camera_far_t() * 0.5;
}

fn scene_depth_has_hit(depth: f32) -> bool {
  return depth > 0.0 && depth < camera_far_half();
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
  let flow_dir = normalize(select(vec2<f32>(1.0, 0.0), water.flow.xy, length(water.flow.xy) > 1e-5));
  let cell = floor(pos_xz / VOXEL_SIZE);
  let phase = dot(cell, flow_dir * 0.72) + water_params.params0.x * water.flow.z;
  let wave = sin(phase * 1.3) + cos(phase * 0.71 + dot(cell, vec2<f32>(0.25, -0.18)));
  let stepped = floor(wave * 1.5) / 1.5;
  return stepped * water.absorption.w;
}

fn ripple_height(pos_xz: vec2<f32>, water_index: u32) -> f32 {
  var total = 0.0;
  for (var i: u32 = 0u; i < water_params.header.y; i = i + 1u) {
    let ripple = ripples[i];
    if (u32(ripple.params.z) != water_index) {
      continue;
    }
    let age = ripple.position_age.w;
    let lifetime = max(ripple.params.y, 0.01);
    let strength = ripple.params.x;
    let life_t = clamp(age / lifetime, 0.0, 1.0);
    let fade = (1.0 - life_t) * (1.0 - life_t);
    if (fade <= 1e-3) {
      continue;
    }
    let delta = pos_xz - ripple.position_age.xz;
    let dist = length(delta);
    let ring_radius = age * mix(2.2, 5.0, clamp(strength, 0.0, 1.0));
    let ring_width = mix(0.16, 0.34, clamp(strength, 0.0, 1.0));
    let band = exp(-pow((dist - ring_radius) / max(ring_width, 1e-3), 2.0));
    let crest = sin((dist - ring_radius) * 15.0 - age * 10.0);
    total += band * crest * fade * strength * 0.16;
  }
  return total;
}

fn water_surface_height(pos_xz: vec2<f32>, water: WaterRecord, water_index: u32) -> f32 {
  let base = stepped_wave_height(pos_xz, water);
  let ripple = ripple_height(pos_xz, water_index);
  return base + ripple;
}

fn stepped_water_normal(pos: vec3<f32>, water: WaterRecord, water_index: u32) -> vec3<f32> {
  let dx = vec2<f32>(VOXEL_SIZE, 0.0);
  let dz = vec2<f32>(0.0, VOXEL_SIZE);
  let h_l = water_surface_height(pos.xz - dx, water, water_index);
  let h_r = water_surface_height(pos.xz + dx, water, water_index);
  let h_d = water_surface_height(pos.xz - dz, water, water_index);
  let h_u = water_surface_height(pos.xz + dz, water, water_index);
  return normalize(vec3<f32>(h_l - h_r, VOXEL_SIZE * 2.0, h_d - h_u));
}

fn water_edge_factor(pos: vec3<f32>, water: WaterRecord) -> f32 {
  let local = abs(pos.xz - water.bounds.xz);
  let edge_dist = min(water.extents.x - local.x, water.extents.y - local.y);
  let band = 1.0 - saturate(edge_dist / (VOXEL_SIZE * 3.0));
  return floor(band * 3.0) / 3.0;
}

fn sample_opaque(uv: vec2<f32>) -> vec3<f32> {
  let dims = textureDimensions(opaque_lit);
  let coord = vec2<i32>(
    clamp(i32(uv.x * f32(dims.x)), 0, i32(dims.x) - 1),
    clamp(i32(uv.y * f32(dims.y)), 0, i32(dims.y) - 1),
  );
  return textureLoad(opaque_lit, coord, 0).rgb;
}

@vertex
fn vs_main(@builtin(vertex_index) vi: u32) -> VSOut {
  var out: VSOut;
  let x = f32((vi << 1u) & 2u);
  let y = f32(vi & 2u);
  out.position = vec4<f32>(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);
  out.uv = vec2<f32>(x, y);
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
  for (var i: u32 = 0u; i < water_params.header.x; i = i + 1u) {
    let water = waters[i];
    let center = water.bounds.xyz;
    let depth = max(water.bounds.w, 0.01);
    let min_b = vec3<f32>(center.x - water.extents.x, center.y - depth, center.z - water.extents.y);
    let max_b = vec3<f32>(center.x + water.extents.x, center.y, center.z + water.extents.y);
    let span = intersect_aabb(ray, min_b, max_b);
    if (span.x > span.y || span.y <= 0.0) {
      continue;
    }
    let t_enter = max(span.x, 0.0);
    if (scene_depth_has_hit(t_scene) && t_enter > t_scene + 0.03) {
      continue;
    }
    if (t_enter >= hit.t_enter) {
      continue;
    }
    let pos = ray.origin + ray.dir * t_enter;
    hit = Hit(true, t_enter, span.y, pos, box_normal(pos, min_b, max_b), i);
  }

  if (!hit.valid) {
    discard;
  }

  let water = waters[hit.water_index];
  let thickness = max(hit.t_exit - hit.t_enter, VOXEL_SIZE * 0.6);
  let is_top = hit.normal.y > 0.9;
  var normal = hit.normal;
  if (is_top) {
    normal = stepped_water_normal(hit.pos, water, hit.water_index);
  }
  let ripple = ripple_height(hit.pos.xz, hit.water_index);
  let edge_factor = water_edge_factor(hit.pos, water);

  let view_dir = normalize(camera.cam_pos.xyz - hit.pos);
  let light_dir = normalize(camera.light_pos.xyz - hit.pos);
  let half_dir = normalize(light_dir + view_dir);
  let ndotl = saturate(dot(normal, light_dir));
  let ndotv = saturate(dot(normal, view_dir));
  let fresnel = pow(1.0 - ndotv, 5.0);
  let roughness = water.extents.w;
  let shininess = mix(120.0, 16.0, roughness);
  let specular = pow(saturate(dot(normal, half_dir)), shininess) * mix(0.12, 0.7, 1.0 - roughness);
  let absorption = exp(-water.absorption.rgb * thickness * 0.18);
  let tint_strength = mix(0.24, 0.42, saturate(thickness / (VOXEL_SIZE * 10.0)));
  let tint = mix(vec3<f32>(1.0), water.color.rgb * absorption, tint_strength);

  var refract_uv = uv_screen;
  if (is_top) {
    let distort = normal.xz * water.color.w * 0.04 * min(thickness, 1.2);
    refract_uv += distort;
  }
  let opaque_bg = sample_opaque(uv_screen);
  let refracted_bg = sample_opaque(clamp(refract_uv, vec2<f32>(0.0), vec2<f32>(1.0)));

  let base_light = camera.ambient_color.rgb * (0.38 + 0.16 * normal.y) + vec3<f32>(ndotl * 0.18 + specular + fresnel * 0.06);
  var surface_rgb = water.color.rgb * base_light;
  if (!is_top) {
    surface_rgb *= 0.45;
  }
  if (is_top) {
    surface_rgb += vec3<f32>(0.12, 0.16, 0.2) * clamp(abs(ripple) * 3.0, 0.0, 1.0);
  }
  let edge_highlight = vec3<f32>(0.14, 0.24, 0.34) * edge_factor * select(1.0, 0.35, !is_top);
  let transmitted_bg = mix(opaque_bg, refracted_bg, 0.82) * tint;
  let surface_mix = select(0.18, 0.08, !is_top);
  let final_rgb = mix(transmitted_bg, surface_rgb + vec3<f32>(specular) + water.color.rgb * fresnel * 0.12 + edge_highlight, surface_mix + fresnel * 0.18 + edge_factor * 0.08);

  var alpha = water.extents.z * mix(0.42, 0.62, saturate(thickness / (VOXEL_SIZE * 8.0)));
  if (!is_top) {
    alpha *= 0.86;
  }
  alpha = min(alpha + edge_factor * 0.06, 0.72);
  let depth_norm = saturate(hit.t_enter / 90.0);
  let weight = max(1e-3, alpha) * pow(1.0 - depth_norm, 5.0);

  var out: FSOut;
  out.accum = vec4<f32>(final_rgb * alpha * weight, alpha);
  out.weight = alpha * weight;
  return out;
}
