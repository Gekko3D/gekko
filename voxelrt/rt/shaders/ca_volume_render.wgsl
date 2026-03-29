const FAR_T: f32 = 60000.0;

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
  pad2: vec2<u32>,
  ao_quality: vec4<f32>,
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

struct CAParams {
  dt: f32,
  elapsed: f32,
  volume_count: u32,
  atlas_width: u32,
  atlas_height: u32,
  atlas_depth: u32,
  pad1: u32,
  pad2: u32,
  pad3: u32,
  pad4: u32,
  pad5: u32,
  pad6: u32,
};

struct VolumeRecord {
  local_to_world: mat4x4<f32>,
  world_to_local: mat4x4<f32>,
  sim_params: vec4<f32>,
  render_params: vec4<f32>,
  scatter_color: vec4<f32>,
  shadow_tint: vec4<f32>,
  absorption_color: vec4<f32>,
  grid: vec4<f32>,
};

struct PresetRecord {
  smoke_seed: f32,
  fire_seed: f32,
  smoke_inject: f32,
  fire_inject: f32,
  diffusion: f32,
  buoyancy: f32,
  cooling: f32,
  dissipation: f32,
  smoke_density_cut: f32,
  fire_heat_cut: f32,
  sigma_t_smoke: f32,
  sigma_t_fire: f32,
  alpha_scale_smoke: f32,
  alpha_scale_fire: f32,
  absorption_scale: f32,
  scatter_scale: f32,
  ember_tint: vec4<f32>,
  fire_core_tint: vec4<f32>,
  flags: u32,
  pad1: u32,
  pad2: u32,
  pad3: u32,
};

struct VolumeBounds {
  min_coord: vec4<u32>,
  max_coord: vec4<u32>,
};

struct Ray {
  origin: vec3<f32>,
  dir: vec3<f32>,
  inv_dir: vec3<f32>,
};

struct CADDState {
  cell: vec3<i32>,
  step: vec3<i32>,
  t_max: vec3<f32>,
  t_delta: vec3<f32>,
  t_end: f32,
  min_cell: vec3<i32>,
  max_cell: vec3<i32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

fn hash13(p: vec3<f32>) -> f32 {
  let h = dot(p, vec3<f32>(127.1, 311.7, 74.7));
  return fract(sin(h) * 43758.5453);
}

struct FSOut {
  @location(0) accum: vec4<f32>,
  @location(1) weight: f32,
};

@group(0) @binding(0) var<uniform> uCamera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;
@group(1) @binding(0) var<uniform> ca_params: CAParams;
@group(1) @binding(1) var<storage, read> volumes: array<VolumeRecord>;
@group(1) @binding(2) var ca_field: texture_3d<f32>;
@group(1) @binding(3) var<storage, read> volume_bounds: array<VolumeBounds>;
@group(1) @binding(4) var<storage, read> ca_presets: array<PresetRecord>;
@group(2) @binding(0) var in_depth: texture_2d<f32>;

fn make_safe_dir(d: vec3<f32>) -> vec3<f32> {
  let eps = 1e-6;
  let sx = select(d.x, (select(1.0, -1.0, d.x < 0.0)) * eps, abs(d.x) < eps);
  let sy = select(d.y, (select(1.0, -1.0, d.y < 0.0)) * eps, abs(d.y) < eps);
  let sz = select(d.z, (select(1.0, -1.0, d.z < 0.0)) * eps, abs(d.z) < eps);
  return vec3<f32>(sx, sy, sz);
}

fn get_ray_from_uv(uv: vec2<f32>) -> Ray {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = uCamera.inv_proj * clip;
  view = view / view.w;
  let world_target = (uCamera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
  let origin = uCamera.cam_pos.xyz;
  let dir = normalize(world_target - origin);
  let safe_dir = make_safe_dir(dir);
  return Ray(origin, dir, 1.0 / safe_dir);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
  let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
  let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
  let safe_dir = make_safe_dir(new_dir);
  return Ray(new_origin, new_dir, 1.0 / safe_dir);
}

fn world_distance_along_ray(ray: Ray, pos_ws: vec3<f32>) -> f32 {
  return dot(pos_ws - ray.origin, ray.dir);
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

fn ca_dda_init(origin: vec3<f32>, dir: vec3<f32>, min_cell: vec3<i32>, max_cell: vec3<i32>, t_entry: f32, t_exit: f32) -> CADDState {
  let pos = origin + dir * t_entry;
  let max_start = max_cell - vec3<i32>(1);
  var cell = vec3<i32>(
    clamp(i32(floor(pos.x)), min_cell.x, max_start.x),
    clamp(i32(floor(pos.y)), min_cell.y, max_start.y),
    clamp(i32(floor(pos.z)), min_cell.z, max_start.z),
  );

  var step = vec3<i32>(0, 0, 0);
  if (dir.x > 0.0) { step.x = 1; } else { step.x = -1; }
  if (dir.y > 0.0) { step.y = 1; } else { step.y = -1; }
  if (dir.z > 0.0) { step.z = 1; } else { step.z = -1; }

  var boundary = vec3<f32>(0.0);
  if (dir.x > 0.0) { boundary.x = f32(cell.x + 1); } else { boundary.x = f32(cell.x); }
  if (dir.y > 0.0) { boundary.y = f32(cell.y + 1); } else { boundary.y = f32(cell.y); }
  if (dir.z > 0.0) { boundary.z = f32(cell.z + 1); } else { boundary.z = f32(cell.z); }

  var t_max = vec3<f32>(FAR_T);
  if (abs(dir.x) > 1e-8) { t_max.x = t_entry + (boundary.x - pos.x) / dir.x; }
  if (abs(dir.y) > 1e-8) { t_max.y = t_entry + (boundary.y - pos.y) / dir.y; }
  if (abs(dir.z) > 1e-8) { t_max.z = t_entry + (boundary.z - pos.z) / dir.z; }

  var t_delta = vec3<f32>(FAR_T);
  if (abs(dir.x) > 1e-8) { t_delta.x = 1.0 / abs(dir.x); }
  if (abs(dir.y) > 1e-8) { t_delta.y = 1.0 / abs(dir.y); }
  if (abs(dir.z) > 1e-8) { t_delta.z = 1.0 / abs(dir.z); }

  return CADDState(cell, step, t_max, t_delta, t_exit, min_cell, max_cell);
}

fn ca_dda_in_bounds(state: CADDState) -> bool {
  return state.cell.x >= state.min_cell.x && state.cell.x < state.max_cell.x &&
    state.cell.y >= state.min_cell.y && state.cell.y < state.max_cell.y &&
    state.cell.z >= state.min_cell.z && state.cell.z < state.max_cell.z;
}

fn ca_dda_advance(state: CADDState) -> CADDState {
  var s = state;
  if (s.t_max.x < s.t_max.y) {
    if (s.t_max.x < s.t_max.z) {
      s.t_max.x += s.t_delta.x;
      s.cell.x += s.step.x;
    } else {
      s.t_max.z += s.t_delta.z;
      s.cell.z += s.step.z;
    }
  } else {
    if (s.t_max.y < s.t_max.z) {
      s.t_max.y += s.t_delta.y;
      s.cell.y += s.step.y;
    } else {
      s.t_max.z += s.t_delta.z;
      s.cell.z += s.step.z;
    }
  }
  return s;
}

fn sample_field(coord: vec3<i32>) -> vec2<f32> {
  let dims = textureDimensions(ca_field);
  let clamped = vec3<i32>(
    clamp(coord.x, 0, i32(dims.x) - 1),
    clamp(coord.y, 0, i32(dims.y) - 1),
    clamp(coord.z, 0, i32(dims.z) - 1),
  );
  return textureLoad(ca_field, clamped, 0).xy;
}

fn sample_volume_voxel(pos_os: vec3<f32>, info: VolumeRecord) -> vec2<f32> {
  if (any(pos_os < vec3<f32>(0.0)) || pos_os.x >= info.grid.x || pos_os.y >= info.grid.y || pos_os.z >= info.grid.z) {
    return vec2<f32>(0.0);
  }

  let cell = vec3<i32>(clamp(floor(pos_os), vec3<f32>(0.0), max(info.grid.xyz - vec3<f32>(1.0), vec3<f32>(0.0))));
  return sample_field(vec3<i32>(cell.x, cell.y, i32(info.grid.w) + cell.z));
}

fn sample_volume(pos_os: vec3<f32>, info: VolumeRecord) -> vec2<f32> {
  if (any(pos_os < vec3<f32>(0.0)) || pos_os.x >= info.grid.x || pos_os.y >= info.grid.y || pos_os.z >= info.grid.z) {
    return vec2<f32>(0.0);
  }

  let hi = max(info.grid.xyz - vec3<f32>(1.0), vec3<f32>(0.0));
  let coord = clamp(pos_os, vec3<f32>(0.0), hi);
  let base = vec3<i32>(floor(coord));
  let frac = coord - vec3<f32>(base);
  let max_local = vec3<i32>(max(info.grid.xyz - vec3<f32>(1.0), vec3<f32>(0.0)));

  let x1 = min(base.x + 1, max_local.x);
  let y1 = min(base.y + 1, max_local.y);
  let z1 = min(base.z + 1, max_local.z);
  let z0 = i32(info.grid.w);

  let c000 = sample_field(vec3<i32>(base.x, base.y, z0 + base.z));
  let c100 = sample_field(vec3<i32>(x1, base.y, z0 + base.z));
  let c010 = sample_field(vec3<i32>(base.x, y1, z0 + base.z));
  let c110 = sample_field(vec3<i32>(x1, y1, z0 + base.z));
  let c001 = sample_field(vec3<i32>(base.x, base.y, z0 + z1));
  let c101 = sample_field(vec3<i32>(x1, base.y, z0 + z1));
  let c011 = sample_field(vec3<i32>(base.x, y1, z0 + z1));
  let c111 = sample_field(vec3<i32>(x1, y1, z0 + z1));

  let c00 = mix(c000, c100, frac.x);
  let c10 = mix(c010, c110, frac.x);
  let c01 = mix(c001, c101, frac.x);
  let c11 = mix(c011, c111, frac.x);
  let c0 = mix(c00, c10, frac.y);
  let c1 = mix(c01, c11, frac.y);
  return mix(c0, c1, frac.z);
}

fn quantize01(v: f32, levels: f32) -> f32 {
  return floor(clamp(v, 0.0, 0.9999) * levels) / max(levels - 1.0, 1.0);
}

fn quantize_alpha(v: f32, levels: f32) -> f32 {
  return ceil(clamp(v, 0.0, 1.0) * levels) / levels;
}

fn hash21(p: vec2<f32>) -> f32 {
  let h = dot(p, vec2<f32>(127.1, 311.7));
  return fract(sin(h) * 43758.5453);
}

fn hash31(p: vec3<f32>) -> f32 {
  let h = dot(p, vec3<f32>(127.1, 311.7, 74.7));
  return fract(sin(h) * 43758.5453);
}

fn bounds_sample_pos(pos_os: vec3<f32>, bounds_min: vec3<f32>, bounds_max: vec3<f32>) -> vec3<f32> {
  return clamp(pos_os, bounds_min, max(bounds_max - vec3<f32>(0.001), bounds_min));
}

fn bounds_shell_distance(pos_os: vec3<f32>, bounds_min: vec3<f32>, bounds_max: vec3<f32>) -> f32 {
  let outside = max(bounds_min - pos_os, pos_os - bounds_max);
  return max(0.0, max(outside.x, max(outside.y, outside.z)));
}

fn bounds_shell_size(info: VolumeRecord) -> f32 {
  let volume_type = packed_volume_type(info);
  let preset = packed_volume_preset(info);
  var shell = select(0.8, 0.55, volume_type == 1u);
  if (preset == 2u && volume_type == 0u) {
    shell = 1.0;
  } else if (preset == 3u) {
    shell = 0.0;
  } else if (preset == 4u) {
    shell = 0.75;
  }
  return shell;
}

fn sample_volume_noisy_border(
  pos_os: vec3<f32>,
  info: VolumeRecord,
  bounds_min: vec3<f32>,
  bounds_max: vec3<f32>,
  noise_seed: f32,
) -> vec2<f32> {
  let sample_pos = bounds_sample_pos(pos_os, bounds_min, bounds_max);
  let shell_dist = bounds_shell_distance(pos_os, bounds_min, bounds_max);
  let border = sample_volume_voxel(sample_pos, info);
  if (shell_dist <= 1e-4) {
    return border;
  }

  if (packed_volume_preset(info) == 3u) {
    return vec2<f32>(0.0);
  }

  let shell = bounds_shell_size(info);
  if (shell_dist > shell) {
    return vec2<f32>(0.0);
  }

  let volume_type = packed_volume_type(info);
  let border_visible = max(border.x, max(border.y - select(0.0, 0.04, volume_type == 1u), 0.0));
  if (border_visible <= 0.001) {
    return vec2<f32>(0.0);
  }

  let voxel = floor(sample_pos);
  let noise = hash31(voxel + vec3<f32>(noise_seed, noise_seed * 1.7, noise_seed * 2.3));
  let shell_t = clamp(shell_dist / max(shell, 1e-4), 0.0, 1.0);
  let visible_keep = smoothstep(0.015, 0.12, border_visible);
  let keep = mix(0.82, 0.18, shell_t) * visible_keep;
  if (noise > keep) {
    return vec2<f32>(0.0);
  }

  let carry = mix(0.72, 0.22, shell_t) * visible_keep;
  return border * carry;
}

fn lobe_shape(p: vec2<f32>, center: vec2<f32>, radius: f32, stretch: vec2<f32>) -> f32 {
  let q = (p - center) / max(stretch, vec2<f32>(0.001));
  let d = length(q);
  return max(0.0, 1.0 - d / max(radius, 0.001));
}

fn packed_volume_type(info: VolumeRecord) -> u32 {
  return u32(info.render_params.z + 0.5) & 7u;
}

fn packed_volume_preset(info: VolumeRecord) -> u32 {
  return u32(info.render_params.z + 0.5) >> 3u;
}

fn volume_intensity(info: VolumeRecord) -> f32 {
  return clamp(info.shadow_tint.w, 0.0, 1.0);
}

fn plume_envelope(pos_os: vec3<f32>, info: VolumeRecord) -> f32 {
  let dim = max(info.grid.xyz, vec3<f32>(1.0));
  let uvw = (pos_os + 0.5) / dim;
  let p = (uvw.xz - vec2<f32>(0.5)) * 2.0;
  let h = clamp(uvw.y, 0.0, 1.0);
  let t = ca_params.elapsed;
  let preset = packed_volume_preset(info);
  let volume_type = packed_volume_type(info);
  let edge_x = min(uvw.x, 1.0 - uvw.x);
  let edge_z = min(uvw.z, 1.0 - uvw.z);
  let edge_y = min(uvw.y, 1.0 - uvw.y);
  let slice_noise = hash21(floor((p + vec2<f32>(2.0)) * 4.0) + vec2<f32>(floor(h * 18.0), floor(t * 2.0)));
  let wobble = vec2<f32>(
    sin(t * 0.8 + h * 6.0),
    cos(t * 0.6 + h * 4.5)
  ) * mix(0.24, 0.08, h);

  var shape = 0.0;
  if (preset == 1u) {
    let c0 = vec2<f32>(-0.1, -0.05) + wobble * 0.35;
    let c1 = vec2<f32>(0.08, 0.06) + wobble * 0.2;
    let l0 = lobe_shape(p, mix(c0, vec2<f32>(0.0, -0.02), smoothstep(0.05, 0.82, h)), mix(0.4, 0.16, h), vec2<f32>(0.56, 0.88));
    let l1 = lobe_shape(p, mix(c1, vec2<f32>(0.0, 0.0), smoothstep(0.05, 0.74, h)), mix(0.32, 0.14, h), vec2<f32>(0.46, 0.74));
    let cap = lobe_shape(p, vec2<f32>(0.0, 0.0), mix(0.18, 0.24, smoothstep(0.28, 0.72, h)), vec2<f32>(0.42, 0.6));
    shape = max(cap, max(l0, l1));
  } else if (preset == 2u) {
    if (volume_type == 0u) {
      let spread = smoothstep(0.05, 0.9, h);
      let drift = vec2<f32>(sin(t * 0.32 + h * 2.6), cos(t * 0.24 + h * 2.2)) * mix(0.08, 0.34, h);
      let c0 = vec2<f32>(-0.22, -0.1) + drift;
      let c1 = vec2<f32>(0.2, 0.14) - drift * 0.65;
      let c2 = vec2<f32>(0.05, -0.28) + vec2<f32>(0.0, sin(t * 0.4 + h * 4.0) * 0.14);
      let crown = lobe_shape(p, drift * 0.25, mix(0.42, 0.86, spread), vec2<f32>(1.2, 1.0));
      let l0 = lobe_shape(p, mix(c0, vec2<f32>(-0.14, -0.06), smoothstep(0.0, 0.35, h)), mix(0.58, 0.78, spread), vec2<f32>(1.0, 0.82));
      let l1 = lobe_shape(p, mix(c1, vec2<f32>(0.12, 0.08), smoothstep(0.0, 0.32, h)), mix(0.52, 0.72, spread), vec2<f32>(0.88, 1.08));
      let l2 = lobe_shape(p, mix(c2, vec2<f32>(0.0, -0.16), smoothstep(0.0, 0.28, h)), mix(0.44, 0.66, spread), vec2<f32>(0.86, 1.18));
      shape = max(crown, max(l0, max(l1, l2)));
    } else {
      let merge = smoothstep(0.2, 0.8, h);
      let c0 = vec2<f32>(-0.24, -0.1) + wobble;
      let c1 = vec2<f32>(0.18, 0.14) + wobble * 0.75;
      let c2 = vec2<f32>(0.02, -0.28) - wobble * 0.55;
      let crown = lobe_shape(p, wobble * 0.12, mix(0.46, 0.34, h), vec2<f32>(0.92, 0.78));
      let l0 = lobe_shape(p, mix(c0, vec2<f32>(-0.08, -0.04), merge), mix(0.66, 0.3, h), vec2<f32>(0.96, 0.76));
      let l1 = lobe_shape(p, mix(c1, vec2<f32>(0.08, 0.04), merge), mix(0.54, 0.24, h), vec2<f32>(0.78, 1.02));
      let l2 = lobe_shape(p, mix(c2, vec2<f32>(0.0, -0.08), smoothstep(0.1, 0.6, h)), mix(0.42, 0.2, h), vec2<f32>(0.74, 1.08));
      shape = max(crown, max(l0, max(l1, l2)));
    }
  } else if (preset == 3u) {
    let sway = vec2<f32>(
      sin(t * 2.8 + h * 9.0) * mix(0.14, 0.04, h),
      cos(t * 2.1 + h * 7.2) * mix(0.05, 0.02, h),
    );
    let core = lobe_shape(p, sway * 0.35, mix(0.62, 0.2, h), vec2<f32>(1.26, 0.19));
    let flare = lobe_shape(p, vec2<f32>(0.12 * sin(t * 3.6 + h * 11.0), 0.0) + sway, mix(0.82, 0.24, h), vec2<f32>(1.72, 0.28));
    let diamond_offset = 0.18 + 0.08 * sin(t * 6.8 + h * 13.0);
    let diamond = mix(0.34, 0.1, h);
    let diamond_a = lobe_shape(p, vec2<f32>(-diamond_offset, 0.0), diamond, vec2<f32>(0.68, 0.11));
    let diamond_b = lobe_shape(p, vec2<f32>(diamond_offset, 0.0), diamond, vec2<f32>(0.68, 0.11));
    let jet_round = 1.0 - smoothstep(mix(0.68, 0.22, h), mix(0.94, 0.38, h), length(p));
    shape = max(max(core, flare), max(diamond_a, diamond_b)) * jet_round;
  } else if (preset == 4u) {
    let cycle_period = 5.4;
    let phase = fract(t / cycle_period);
    let burst_time = phase * cycle_period;
    let expand = smoothstep(0.0, 0.48, burst_time);
    let after = smoothstep(0.32, 1.4, burst_time);
    let rise = smoothstep(0.24, 3.2, burst_time);
    let fade = 1.0 - smoothstep(4.0, 5.4, burst_time);
    let ball_center = vec3<f32>(0.5, mix(0.14, 0.78, rise), 0.5);
    let q = (uvw - ball_center) / vec3<f32>(
      mix(0.16, 0.44, expand),
      mix(0.12, 0.36, expand),
      mix(0.16, 0.44, expand)
    );
    let fireball = max(0.0, 1.0 - length(q));
    let drift = vec2<f32>(
      sin(t * 0.68 + h * 4.0 + info.grid.w * 0.03),
      cos(t * 0.56 + h * 3.2 + info.grid.w * 0.05)
    ) * mix(0.06, 0.22, h);
    
    // Toroidal cap (vortex ring)
    let ring_radius = mix(0.12, 0.42, after);
    let tube_radius = mix(0.08, 0.22, after);
    let p_xz = length(p - drift * 0.2);
    let ring_d = length(vec2<f32>(p_xz - ring_radius, (h - ball_center.y) * 2.2));
    let crown = max(0.0, 1.0 - ring_d / tube_radius);

    let shoulder_a = lobe_shape(p, vec2<f32>(-0.3, 0.05) + drift, mix(0.12, 0.4, after), vec2<f32>(0.48, 0.64));
    let shoulder_b = lobe_shape(p, vec2<f32>(0.28, -0.05) - drift * 0.8, mix(0.12, 0.38, after), vec2<f32>(0.46, 0.66));
    let stem = lobe_shape(
      p,
      vec2<f32>(sin(t * 0.8 + h * 5.2) * 0.04, 0.0),
      mix(0.14, 0.24, rise),
      vec2<f32>(mix(0.38, 0.16, h), mix(0.52, 0.2, h))
    ) * smoothstep(0.04, 0.88, h);

    let lower_hole = (1.0 - smoothstep(0.1, 0.3, length(p))) * (1.0 - smoothstep(0.04, 0.28, h));
    let detached_base = smoothstep(0.01, 0.1, h);
    let plume = max(crown, max(shoulder_a, max(shoulder_b, stem * 0.92))) * (1.0 - lower_hole * 0.62) * detached_base;
    
    // Aggressive mushroom taper: narrower base, wider cap.
    let profile = mix(0.18, 0.94, smoothstep(0.08, 0.68, h));
    let taper = 1.0 - smoothstep(profile - 0.2, profile + 0.1, length(p));
    shape = max(fireball * mix(1.1, 0.15, after), plume) * taper * fade;
  } else if (volume_type == 1u) {
    let c0 = vec2<f32>(-0.22, -0.08) + wobble;
    let c1 = vec2<f32>(0.17, 0.12) + wobble * 0.8;
    let c2 = vec2<f32>(0.03, -0.25) - wobble * 0.65;
    let l0 = lobe_shape(p, mix(c0, vec2<f32>(0.02, -0.04), smoothstep(0.15, 0.75, h)), mix(0.62, 0.26, h), vec2<f32>(0.9, 0.7));
    let l1 = lobe_shape(p, mix(c1, vec2<f32>(0.04, 0.03), smoothstep(0.0, 0.7, h)), mix(0.48, 0.22, h), vec2<f32>(0.7, 0.95));
    let l2 = lobe_shape(p, mix(c2, vec2<f32>(-0.02, 0.0), smoothstep(0.0, 0.55, h)), mix(0.38, 0.18, h), vec2<f32>(0.65, 1.05));
    shape = max(l0, max(l1, l2));
  } else {
    let drift = vec2<f32>(sin(t * 0.35 + h * 3.2), cos(t * 0.28 + h * 2.7)) * mix(0.18, 0.32, h);
    let l0 = lobe_shape(p, vec2<f32>(-0.28, -0.1) + drift, mix(0.7, 0.48, h), vec2<f32>(1.15, 0.72));
    let l1 = lobe_shape(p, vec2<f32>(0.12, 0.22) - drift * 0.7, mix(0.58, 0.42, h), vec2<f32>(0.82, 1.08));
    let l2 = lobe_shape(p, vec2<f32>(0.34, -0.06) + vec2<f32>(0.0, sin(t * 0.45 + h * 5.0) * 0.12), mix(0.46, 0.34, h), vec2<f32>(0.95, 0.68));
    shape = max(l0, max(l1, l2));
  }

  let breakup = smoothstep(0.18, 0.82, shape + (slice_noise - 0.5) * mix(0.26, 0.14, h));
  var top_start = 0.62;
  if (preset == 2u && volume_type == 0u) {
    top_start = 0.94;
  } else if (preset == 2u) {
    top_start = 0.82;
  } else if (preset == 3u) {
    top_start = 0.88;
  } else if (preset == 4u) {
    top_start = 0.95;
  }
  let top_fade = 1.0 - smoothstep(top_start, 1.0, h);
  let bottom_soften = smoothstep(0.0, 0.08, h);
  var edge_fade = smoothstep(0.02, 0.12, min(edge_x, edge_z));
  if (preset == 2u && volume_type == 0u) {
    edge_fade = smoothstep(0.08, 0.22, min(edge_x, edge_z));
  } else if (preset == 3u) {
    edge_fade = smoothstep(0.02, 0.12, min(edge_x, edge_z));
  } else if (preset == 4u) {
    edge_fade = smoothstep(0.05, 0.2, min(edge_x, edge_z));
  }
  let cap_fade = 1.0 - smoothstep(0.9, 1.0, 1.0 - edge_y);
  let body = mix(0.48, 1.0, breakup);
  return clamp(body * top_fade * bottom_soften * edge_fade * cap_fade, 0.0, 1.0);
}

fn bounds_soften(pos_os: vec3<f32>, bounds_min: vec3<f32>, bounds_max: vec3<f32>) -> f32 {
  let inner = min(pos_os - bounds_min, bounds_max - pos_os);
  let edge = max(0.0, min(inner.x, min(inner.y, inner.z)));
  return smoothstep(0.0, 2.6, edge);
}

fn volume_light_transmittance(pos_os: vec3<f32>, info: VolumeRecord, light_dir_ws: vec3<f32>, inside_volume: bool) -> f32 {
  let light_dir_os_raw = (info.world_to_local * vec4<f32>(light_dir_ws, 0.0)).xyz;
  let light_len = length(light_dir_os_raw);
  if (light_len < 1e-5) {
    return 1.0;
  }

  let light_dir_os = light_dir_os_raw / light_len;
  let bounds = intersect_aabb(Ray(pos_os, light_dir_os, 1.0 / make_safe_dir(light_dir_os)), vec3<f32>(0.0), info.grid.xyz);
  let t_exit = bounds.y;
  if (t_exit <= 0.0) {
    return 1.0;
  }

  let volume_type = packed_volume_type(info);
  let preset = packed_volume_preset(info);
  let intensity = volume_intensity(info);
  if (inside_volume && volume_type == 0u) {
    return 1.0;
  }

  let p_data = ca_presets[preset];
  var density_gain = 0.9;
  var shadow_gain = 1.0;
  var quant_levels = 4.0;
  var max_steps = 12u;

  if (volume_type == 0u) {
    max_steps = 14u;
    density_gain = 1.12;
    shadow_gain = 1.18;
    quant_levels = 6.0;
    if (preset == 2u) {
      max_steps = 16u;
      density_gain = 1.06;
      shadow_gain = 1.12;
      quant_levels = 6.0;
    }
  } else {
    max_steps = 8u;
    density_gain = 0.72;
    shadow_gain = 0.88;
  }

  let march_limit = t_exit;
  let grid_max = vec3<i32>(i32(info.grid.x), i32(info.grid.y), i32(info.grid.z));
  var state = ca_dda_init(pos_os, light_dir_os, vec3<i32>(0), grid_max, 0.0, march_limit);
  let hard_limit = min(
    max_steps,
    u32(max(4, grid_max.x + grid_max.y + grid_max.z + 4)),
  );
  var tau = 0.0;
  var t = 0.0;
  var steps = 0u;
  loop {
    if (t >= march_limit || tau > 8.0 || steps >= hard_limit || !ca_dda_in_bounds(state)) {
      break;
    }
    steps += 1u;
    let t_next = min(march_limit, min(state.t_max.x, min(state.t_max.y, state.t_max.z)));
    if (t_next <= t + 1e-5) {
      state = ca_dda_advance(state);
      t = t_next;
      continue;
    }
    let segment_len = max(t_next - t, 1e-4);
    let p = pos_os + light_dir_os * mix(t, t_next, 0.5);
    let s = sample_volume_voxel(p, info);
    let pack_type = packed_volume_type(info);
    let p_d = ca_presets[preset];
    let d_raw = max(s.x - p_d.smoke_density_cut, 0.0) * intensity * info.render_params.x;
    var d = d_raw;
    if (pack_type == 1u || preset == 4u) {
      let h_raw = max(s.y - p_d.fire_heat_cut, 0.0) * intensity * p_d.sigma_t_fire;
      d += h_raw * mix(0.12, 0.84, smoothstep(0.0, 0.5, h_raw));
    }
    let step_fade = mix(1.0, 0.72, t / max(march_limit, 1e-3));
    tau += d * segment_len * density_gain * step_fade;
    state = ca_dda_advance(state);
    t = t_next;
  }
  return quantize01(exp(-tau * shadow_gain), quant_levels);
}

fn primary_light_dir(pos_ws: vec3<f32>) -> vec3<f32> {
  let count = min(uCamera.num_lights, arrayLength(&lights));
  if (count == 0u) {
    return normalize(vec3<f32>(0.3, 1.0, 0.2));
  }
  let l = lights[0];
  if (u32(l.params.z) == 1u) {
    return normalize(-l.direction.xyz);
  }
  return normalize(l.position.xyz - pos_ws);
}

fn primary_light_color() -> vec3<f32> {
  let count = min(uCamera.num_lights, arrayLength(&lights));
  if (count == 0u) {
    return vec3<f32>(1.0);
  }
  return lights[0].color.xyz * max(lights[0].color.w, 0.0);
}

fn fire_color(t: f32) -> vec3<f32> {
  let x = clamp(t, 0.0, 1.5);
  let low = vec3<f32>(1.0, 0.22, 0.04);
  let mid = vec3<f32>(1.0, 0.6, 0.1);
  let high = vec3<f32>(1.0, 0.95, 0.75);
  let warm = mix(low, mid, clamp(x * 1.3, 0.0, 1.0));
  return mix(warm, high, clamp((x - 0.6) * 1.4, 0.0, 1.0));
}

fn preset_fire_color(t: f32, preset: u32) -> vec3<f32> {
  if (preset == 3u) {
    let x = clamp(t, 0.0, 1.5);
    let core = vec3<f32>(0.18, 0.42, 0.95);
    let body = vec3<f32>(0.32, 0.68, 1.0);
    let hot = vec3<f32>(0.72, 0.9, 1.0);
    let heated = mix(core, body, clamp(x * 1.05, 0.0, 1.0));
    return mix(heated, hot, clamp((x - 0.5) * 1.25, 0.0, 1.0));
  }
  return fire_color(t);
}

fn fire_band_color(heat: f32, env: f32, preset: u32) -> vec3<f32> {
  let h = clamp(heat, 0.0, 1.6);
  if (preset == 3u) {
    let ember = vec3<f32>(0.12, 0.2, 0.48);
    let body = vec3<f32>(0.28, 0.56, 0.95);
    let core = vec3<f32>(0.66, 0.84, 1.0);
    let ember_w = (1.0 - smoothstep(0.22, 0.6, h)) * mix(0.35, 1.0, 1.0 - env);
    let body_w = smoothstep(0.18, 0.7, h) * (1.0 - smoothstep(0.95, 1.35, h));
    let core_w = smoothstep(0.72, 1.18, h) * mix(0.7, 1.08, env);
    let total = max(ember_w + body_w + core_w, 1e-4);
    return (ember * ember_w + body * body_w + core * core_w) / total;
  }

  let ember = vec3<f32>(0.55, 0.08, 0.02);
  let body = vec3<f32>(1.0, 0.42, 0.08);
  let core = vec3<f32>(1.0, 0.92, 0.7);
  let ember_w = (1.0 - smoothstep(0.18, 0.52, h)) * mix(0.45, 1.1, 1.0 - env);
  let body_w = smoothstep(0.16, 0.62, h) * (1.0 - smoothstep(0.86, 1.18, h));
  let core_w = smoothstep(0.62, 1.02, h) * mix(0.7, 1.1, env);
  let total = max(ember_w + body_w + core_w, 1e-4);
  return (ember * ember_w + body * body_w + core * core_w) / total;
}

fn fire_band_strength(heat: f32, env: f32, preset: u32) -> vec3<f32> {
  let h = clamp(heat, 0.0, 1.6);
  if (preset == 3u) {
    return vec3<f32>(
      (1.0 - smoothstep(0.24, 0.66, h)) * mix(0.28, 0.82, 1.0 - env),
      smoothstep(0.14, 0.74, h) * (1.0 - smoothstep(0.94, 1.34, h)),
      smoothstep(0.54, 1.04, h) * mix(0.72, 1.16, env),
    );
  }
  return vec3<f32>(
    (1.0 - smoothstep(0.16, 0.48, h)) * mix(0.3, 0.82, 1.0 - env),
    smoothstep(0.16, 0.62, h) * (1.0 - smoothstep(0.84, 1.12, h)),
    smoothstep(0.58, 0.96, h) * mix(0.72, 1.08, env),
  );
}

fn hg_phase(cos_theta: f32, g: f32) -> f32 {
  let gg = g * g;
  let denom = pow(max(1.0 + gg - 2.0 * g * cos_theta, 1e-3), 1.5);
  return (1.0 - gg) / denom;
}

fn normalized_hg_phase(cos_theta: f32, g: f32) -> f32 {
  let peak = hg_phase(1.0, g);
  return clamp(hg_phase(cos_theta, g) / max(peak, 1e-3), 0.0, 1.0);
}

fn volume_phase_light(
  volume_type: u32,
  preset: u32,
  env: f32,
  smoke_density: f32,
  heat: f32,
  cos_theta: f32,
  ambient: vec3<f32>,
  light_color: vec3<f32>,
  light_trans: f32,
) -> vec3<f32> {
  var g = 0.24;
  var ambient_fill = 0.56;
  var direct_gain = 0.92;
  var edge_gain = 0.22;
  var quant_levels = 4.0;

  if (volume_type == 0u) {
    g = 0.38;
    ambient_fill = 0.52;
    direct_gain = 1.08;
    edge_gain = 0.3;
    if (preset == 2u) {
      g = 0.44;
      ambient_fill = 0.44;
      direct_gain = 1.12;
      edge_gain = 0.34;
    }
  } else {
    g = 0.08;
    ambient_fill = 0.12;
    direct_gain = 0.54;
    edge_gain = 0.06;
    if (preset == 3u) {
      g = 0.0;
      ambient_fill = 0.06;
      direct_gain = 0.42;
      edge_gain = 0.02;
    }
  }

  let forward = quantize01(normalized_hg_phase(cos_theta, g), quant_levels);
  let side = quantize01(pow(clamp(1.0 - abs(cos_theta), 0.0, 1.0), 2.2), 4.0);
  let edge_mask = smoothstep(0.01, 0.12, smoke_density) * (1.0 - smoothstep(0.58, 1.0, env));
  let edge_lift = side * edge_mask * edge_gain;
  let heat_cut = smoothstep(0.08, 0.65, heat);
  let ambient_term = ambient * ambient_fill * mix(1.0, 0.55, heat_cut);
  let direct_term = light_color * min(1.0, forward * direct_gain + edge_lift) * light_trans;
  return ambient_term + direct_term;
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32) -> VSOut {
  var pos = array<vec2<f32>, 3>(
    vec2<f32>(-1.0, -3.0),
    vec2<f32>(-1.0, 1.0),
    vec2<f32>(3.0, 1.0),
  );
  let p = pos[vid];
  var out: VSOut;
  out.position = vec4<f32>(p, 0.0, 1.0);
  out.uv = p * 0.5 + vec2<f32>(0.5, 0.5);
  return out;
}

@fragment
fn fs_main(@builtin(position) frag_pos: vec4<f32>, @location(0) uv: vec2<f32>) -> FSOut {
  let dims = textureDimensions(in_depth);
  let ipos = vec2<i32>(
    clamp(i32(frag_pos.x), 0, i32(dims.x) - 1),
    clamp(i32(frag_pos.y), 0, i32(dims.y) - 1),
  );
  var t_limit = textureLoad(in_depth, ipos, 0).r;
  if (t_limit >= FAR_T || t_limit <= 0.0) {
    t_limit = FAR_T;
  }

  let uv_screen = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let ray_ws = get_ray_from_uv(uv_screen);
  let light_color = primary_light_color();
  let ambient = uCamera.ambient_color.xyz;

  var accum_rgb = vec3<f32>(0.0);
  var accum_a = 0.0;
  var accum_w = 0.0;

  for (var i = 0u; i < ca_params.volume_count; i = i + 1u) {
    let info = volumes[i];
    let volume_type = packed_volume_type(info);
    let preset = packed_volume_preset(info);
    let intensity = volume_intensity(info);
    let bounds_info = volume_bounds[i];
    let bounds_min = vec3<f32>(bounds_info.min_coord.xyz);
    let bounds_max = vec3<f32>(bounds_info.max_coord.xyz);
    if (any(bounds_max <= bounds_min)) {
      continue;
    }
    let ray_os = transform_ray(ray_ws, info.world_to_local);
    let raw_bounds = intersect_aabb(ray_os, bounds_min, bounds_max);
    let inside_volume = raw_bounds.x < 0.0 && raw_bounds.y > 0.0;
    var border_shell = bounds_shell_size(info);
    if (inside_volume && volume_type == 0u) {
      border_shell = 0.0;
    }
    let march_min = max(bounds_min - vec3<f32>(border_shell), vec3<f32>(0.0));
    let march_max = min(bounds_max + vec3<f32>(border_shell), info.grid.xyz);
    let bounds = intersect_aabb(ray_os, march_min, march_max);
    let t_enter_os = max(bounds.x, 0.0);
    let t_exit_os = bounds.y;
    if (t_enter_os >= t_exit_os) {
      continue;
    }

    let enter_pos_os = ray_os.origin + ray_os.dir * t_enter_os;
    let exit_pos_os = ray_os.origin + ray_os.dir * t_exit_os;
    let enter_pos_ws = (info.local_to_world * vec4<f32>(enter_pos_os, 1.0)).xyz;
    let exit_pos_ws = (info.local_to_world * vec4<f32>(exit_pos_os, 1.0)).xyz;
    let t_enter = max(0.0, world_distance_along_ray(ray_ws, enter_pos_ws));
    let t_exit = min(t_limit, world_distance_along_ray(ray_ws, exit_pos_ws));
    if (t_enter >= t_exit) {
      continue;
    }

    let screen_jitter = hash21(vec2<f32>(f32(ipos.x) + f32(i) * 13.0, f32(ipos.y) + f32(i) * 29.0));
    let anchor_cell = floor(enter_pos_os * 0.75 + vec3<f32>(f32(i) * 1.7, f32(i) * 2.3, f32(i) * 3.1));
    let world_jitter = hash31(anchor_cell);
    let ray_jitter = mix(world_jitter, screen_jitter, 0.28);
    let dda_min = vec3<i32>(
      max(0, i32(floor(march_min.x))),
      max(0, i32(floor(march_min.y))),
      max(0, i32(floor(march_min.z))),
    );
    let dda_max = vec3<i32>(
      min(i32(info.grid.x), i32(ceil(march_max.x))),
      min(i32(info.grid.y), i32(ceil(march_max.y))),
      min(i32(info.grid.z), i32(ceil(march_max.z))),
    );
    if (dda_max.x <= dda_min.x || dda_max.y <= dda_min.y || dda_max.z <= dda_min.z) {
      continue;
    }

    var state = ca_dda_init(ray_os.origin, ray_os.dir, dda_min, dda_max, t_enter, t_exit);
    var t = t_enter;
    var trans = 1.0;
    var premul_rgb = vec3<f32>(0.0);
    var steps = 0u;
    let max_steps = u32(max(
      8,
      (dda_max.x - dda_min.x) +
      (dda_max.y - dda_min.y) +
      (dda_max.z - dda_min.z) +
      8,
    ));
    loop {
      if (t >= t_exit || trans <= 0.01 || steps >= max_steps || !ca_dda_in_bounds(state)) {
        break;
      }
      steps += 1u;

      let t_next = min(t_exit, min(state.t_max.x, min(state.t_max.y, state.t_max.z)));
      if (t_next <= t + 1e-5) {
        state = ca_dda_advance(state);
        t = t_next;
        continue;
      }

      let cell_hash = hash31(vec3<f32>(f32(state.cell.x), f32(state.cell.y), f32(state.cell.z)) + vec3<f32>(f32(i) * 0.91, f32(i) * 1.37, f32(i) * 1.79));
      let sample_phase = mix(0.32, 0.68, mix(ray_jitter, cell_hash, 0.55));
      let sample_t = mix(t, t_next, sample_phase);
      let pos_ws = ray_ws.origin + ray_ws.dir * sample_t;
      let pos_os = ray_os.origin + ray_os.dir * sample_t;
      let sample_pos = bounds_sample_pos(pos_os, bounds_min, bounds_max);
      let sample = sample_volume_noisy_border(pos_os, info, bounds_min, bounds_max, ray_jitter + f32(i) * 0.37);
      let segment_len = max(t_next - t, 1e-4);
      let env = plume_envelope(sample_pos, info) * bounds_soften(sample_pos, bounds_min, bounds_max);
      let uvw_local = (sample_pos + vec3<f32>(0.5)) / max(info.grid.xyz, vec3<f32>(1.0));
      let flame_h = clamp(uvw_local.y, 0.0, 1.0);
      let flame_radial = length((uvw_local.xz - vec2<f32>(0.5)) * 2.0);
      let p_data = ca_presets[preset];
      let raw_density = max(sample.x, 0.0) * env * intensity;
      let smoke_density = max(raw_density - p_data.smoke_density_cut, 0.0);
      let heat = max(sample.y, 0.0) * intensity;
      var apparent_heat = heat;
      if (preset == 2u && volume_type == 1u) {
        let top_cool = smoothstep(0.26, 0.98, flame_h);
        apparent_heat = heat * mix(1.04, 0.48, top_cool);
      } else if (preset == 3u) {
        let core_focus = 1.0 - smoothstep(0.12, 0.5, flame_radial);
        let base_focus = 1.0 - smoothstep(0.18, 0.78, flame_h);
        let shimmer = 0.88 + 0.28 * sin(ca_params.elapsed * 11.4 + pos_os.x * 0.8 + pos_os.z * 1.1 + flame_h * 18.0);
        apparent_heat = heat * mix(1.08, 1.42, core_focus) * mix(1.12, 0.82, flame_h) * shimmer + core_focus * base_focus * 0.08;
      } else if (preset == 4u) {
        let burst_hot = 1.0 - smoothstep(0.14, 0.74, flame_h);
        let edge_cool = smoothstep(0.22, 0.86, flame_radial);
        let core_focus = 1.0 - smoothstep(0.12, 0.34, flame_radial);
        apparent_heat = heat * mix(1.5, 0.5, smoothstep(0.12, 0.94, flame_h)) * mix(1.0, 0.44, edge_cool) + burst_hot * 0.18 + core_focus * burst_hot * 0.14;
      }
      let visible = select(
        smoke_density,
        max(heat - p_data.fire_heat_cut, 0.0) + smoke_density * 0.35,
        volume_type == 1u || preset == 4u,
      );
      if (visible > 0.001) {
        var smoke_term = pow(smoke_density, p_data.alpha_scale_smoke) * (1.25 + 0.75 * env) * info.render_params.x;
        if (preset == 2u && volume_type == 0u) {
          smoke_term = pow(smoke_density, 1.08) * (1.1 + 0.65 * env) * info.render_params.x;
        } else if (preset == 4u) {
          smoke_term = pow(smoke_density, 1.1) * (1.18 + 0.54 * env) * info.render_params.x;
        }
        var flame_term = 0.0;
          if (volume_type == 1u || preset == 4u) {
            let cooled = 1.0 - smoothstep(0.22, 0.9, apparent_heat);
            smoke_term *= mix(select(0.18, 0.32, preset == 4u), 1.0, cooled);
            flame_term = pow(max(apparent_heat - p_data.fire_heat_cut, 0.0), p_data.alpha_scale_fire) * (0.75 + 0.55 * env) * p_data.sigma_t_fire * info.render_params.y;
            if (preset == 2u) {
              let base_core = 1.0 - smoothstep(0.18, 0.44, flame_h);
              let neck = 1.0 - smoothstep(mix(0.78, 0.22, flame_h), mix(0.94, 0.34, flame_h), flame_radial);
              let top_band = smoothstep(0.34, 0.96, flame_h);
              let top_flicker_a = 0.72 + 0.28 * sin(ca_params.elapsed * 11.6 + pos_os.x * 0.92 + pos_os.z * 0.74 + flame_h * 14.0);
              let top_flicker_b = 0.74 + 0.26 * cos(ca_params.elapsed * 15.2 + pos_os.x * 1.28 - pos_os.z * 1.06 + flame_h * 19.0);
              let top_flicker = mix(1.0, top_flicker_a * top_flicker_b, top_band);
              let upper = smoothstep(0.3, 0.95, flame_h);
              flame_term *= mix(1.18 + base_core * 0.38, 0.42 + top_flicker * 0.52, upper) * neck;
            } else if (preset == 4u) {
              let phase = fract(ca_params.elapsed / 3.6);
              let burst_time = phase * 3.6;
              let flash = 1.0 - smoothstep(0.0, 0.22, burst_time);
              let after = smoothstep(0.16, 0.6, burst_time);
              let blast_shell = 1.0 - smoothstep(mix(0.84, 0.22, flash), mix(0.98, 0.38, flash), flame_radial);
              let center_core = (1.0 - smoothstep(0.1, 0.38, flame_h)) * (1.0 - smoothstep(0.12, 0.42, flame_radial));
              let lift_fade = 1.0 - smoothstep(0.48, 0.9, flame_h);
              let fireball_noise = 0.82 + 0.36 * hash13(pos_os * 1.8 + t * 0.45);
              let fireball = mix(1.6, 0.5, after) * mix(1.36, 0.54, flame_h) * fireball_noise;
              flame_term *= blast_shell * fireball * max(center_core * 1.28, lift_fade * 0.62);
            }
            if (preset == 3u) {
              let core_focus = 1.0 - smoothstep(0.1, 0.44, flame_radial);
              let tail = 1.0 - smoothstep(0.58, 0.98, flame_h);
              let flicker = 0.8 + 0.34 * sin(ca_params.elapsed * 12.8 + pos_os.x * 1.6 + pos_os.z * 1.3 + flame_h * 22.0);
              flame_term = pow(max(apparent_heat - 0.015, 0.0), 1.02) * (0.9 + 0.66 * env) * mix(1.18, 0.84, flame_h) * mix(0.9, 1.42, core_focus) * max(flicker, 0.35);
              flame_term += core_focus * tail * 0.08;
            }
          }
        var sigma_t = smoke_term * max(0.01, info.render_params.x) * p_data.sigma_t_smoke +
          flame_term * p_data.sigma_t_fire;
        var alpha_step = 1.0 - exp(-sigma_t * segment_len);
        if (preset == 4u) {
          let heat_alpha = clamp(apparent_heat * 1.15, 0.0, 1.0);
          let smoke_alpha = smoothstep(0.02, 0.28, smoke_density);
          alpha_step = clamp(alpha_step * mix(1.0 + smoke_alpha * 0.45, 1.48, heat_alpha), 0.0, 0.98);
        } else if (volume_type == 0u) {
          alpha_step = clamp(alpha_step * mix(1.35, 1.85, smoothstep(0.02, 0.2, smoke_density)), 0.0, 0.98);
        } else if (volume_type == 1u) {
          let heat_alpha = clamp(heat * select(1.2, 1.45, preset == 3u), 0.0, 1.0);
          alpha_step = clamp(alpha_step * mix(0.08, select(1.35, 1.65, preset == 3u), heat_alpha), 0.0, 0.98);
        }
        if (alpha_step > 1e-4) {
          let ldir = primary_light_dir(pos_ws);
          let cos_theta = clamp(dot(ldir, -ray_ws.dir), -1.0, 1.0);
          let light_trans = volume_light_transmittance(pos_os, info, ldir, inside_volume);
          let light_term = volume_phase_light(
            select(volume_type, 0u, preset == 4u),
            preset,
            env,
            smoke_density,
            heat,
            cos_theta,
            ambient,
            light_color,
            light_trans,
          );
          let shadow_mix = clamp(
            (1.0 - light_trans) * 0.65 +
            smoothstep(0.04, 0.22, smoke_density) * 0.35 +
            (1.0 - env) * 0.2,
            0.0,
            1.0,
          );
          let body_color = mix(info.scatter_color.xyz, info.shadow_tint.xyz, shadow_mix);
          var absorption_mix = smoothstep(0.06, 0.24, smoke_density) * mix(0.3, 0.82, 1.0 - light_trans);
          absorption_mix += smoothstep(0.18, 0.7, alpha_step) * 0.25;
          absorption_mix += (1.0 - env) * 0.12;
          if (volume_type == 1u) {
            absorption_mix *= 0.38;
          }
          let medium_color = mix(body_color, info.absorption_color.xyz, clamp(absorption_mix, 0.0, 1.0));
          var scatter = medium_color * light_term * (0.55 + 0.45 * env) * p_data.scatter_scale;
          if (volume_type == 1u || preset == 4u) {
            let heat_scatter = clamp(apparent_heat * select(1.1, 1.35, preset == 3u), 0.0, 1.0);
            scatter *= mix(select(0.08, 0.04, preset == 3u), select(0.34, 0.18, preset == 3u), heat_scatter);
            if (preset == 4u) {
              scatter *= mix(0.62, 0.24, heat_scatter);
            }
            if (preset == 3u) {
              scatter *= 0.1;
            }
          } else {
            scatter *= mix(1.2, 1.55, smoothstep(0.03, 0.22, smoke_density));
          }
          var source = scatter * select(smoke_term, smoke_term * 0.65, volume_type == 1u);
          if (preset == 4u) {
            source = scatter * smoke_term * 0.92;
          }
          if (volume_type == 1u || preset == 4u) {
            let fire_bands = fire_band_strength(heat, env, preset);
            let base_fire = preset_fire_color(apparent_heat, preset);
            let ember_tint = p_data.ember_tint.xyz;
            let ember_term = fire_bands.x * flame_term * mix(0.16, 0.42, 1.0 - light_trans);
            let body_term = fire_bands.y * flame_term * 0.12;
            let core_term = fire_bands.z * flame_term * mix(0.1, 0.3, light_trans);
            let band_tint = clamp(ember_tint * ember_term + p_data.fire_core_tint.xyz * body_term + vec3<f32>(1.0) * core_term, vec3<f32>(0.0), vec3<f32>(0.38));
            let flame = base_fire * flame_term * max(0.0, info.render_params.y);
            if (preset == 4u) {
              source += flame * (1.08 + 0.16 * light_trans) * (0.84 + core_term) + band_tint * flame_term * 0.62;
            } else {
              source += flame * (1.34 + 0.28 * light_trans) * (1.0 + core_term) + band_tint * flame_term;
              if (preset == 3u) {
                source += flame * (0.16 + 0.12 * core_term);
              }
            }
          }
          premul_rgb += trans * source * alpha_step * p_data.absorption_scale;
          trans *= (1.0 - alpha_step);
        }
      }

      state = ca_dda_advance(state);
      t = t_next;
    }

    let alpha_raw = clamp(1.0 - trans, 0.0, 0.995);
    let alpha_shaped = 1.0 - pow(1.0 - alpha_raw, 1.8);
    var alpha_levels = 6.0;
    if (volume_type == 0u) {
      alpha_levels = 10.0;
    }
    let alpha = clamp(quantize_alpha(alpha_shaped, alpha_levels), 0.0, 0.995);
    if (alpha > 1e-4) {
      let color = premul_rgb / max(alpha_raw, 1e-4);
      let z = clamp(t_enter / max(t_limit, 1e-4), 0.0, 1.0);
      let w = max(1e-3, alpha) * mix(0.65, 1.0, pow(1.0 - z, 2.5));
      let reveal_alpha = clamp(alpha * 1.55, 0.0, 1.35);
      accum_rgb += color * alpha * w;
      accum_a += reveal_alpha;
      accum_w += alpha * w;
    }
  }

  return FSOut(vec4<f32>(accum_rgb, accum_a), accum_w);
}
