const FAR_T: f32 = 60000.0;
const PI: f32 = 3.14159265359;

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

struct MediumParams {
  medium_count: u32,
  pad0: u32,
  pad1: u32,
  pad2: u32,
};

struct MediumRecord {
  bounds: vec4<f32>,
  shape0: vec4<f32>,
  shape1: vec4<f32>,
  scatter: vec4<f32>,
  absorption: vec4<f32>,
  emission: vec4<f32>,
  params: vec4<f32>,
  noise: vec4<f32>,
  style0: vec4<f32>,
  style1: vec4<f32>,
  style2: vec4<f32>,
};

struct HistoryParams {
  prev_view_proj: mat4x4<f32>,
  prev_cam_pos: vec4<f32>,
  params0: vec4<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

struct FSOut {
  @location(0) color: vec4<f32>,
  @location(1) depth: f32,
};

@group(0) @binding(0) var<uniform> uCamera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;
@group(1) @binding(0) var<uniform> medium_params: MediumParams;
@group(1) @binding(1) var<storage, read> media: array<MediumRecord>;
@group(2) @binding(0) var in_depth: texture_2d<f32>;
@group(2) @binding(1) var planet_depth: texture_2d<f32>;
@group(2) @binding(2) var prev_history: texture_2d<f32>;
@group(2) @binding(3) var prev_history_depth: texture_2d<f32>;
@group(2) @binding(4) var<uniform> history_params: HistoryParams;

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

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn hash13(p: vec3<f32>) -> f32 {
  let p3 = fract(p * 0.1031);
  let p3a = p3 + dot(p3, p3.yzx + 33.33);
  return fract((p3a.x + p3a.y) * p3a.z);
}

fn value_noise_3(p: vec3<f32>) -> f32 {
  let i = floor(p);
  let f = fract(p);
  let u = f * f * (3.0 - 2.0 * f);

  let n000 = hash13(i + vec3<f32>(0.0, 0.0, 0.0));
  let n100 = hash13(i + vec3<f32>(1.0, 0.0, 0.0));
  let n010 = hash13(i + vec3<f32>(0.0, 1.0, 0.0));
  let n110 = hash13(i + vec3<f32>(1.0, 1.0, 0.0));
  let n001 = hash13(i + vec3<f32>(0.0, 0.0, 1.0));
  let n101 = hash13(i + vec3<f32>(1.0, 0.0, 1.0));
  let n011 = hash13(i + vec3<f32>(0.0, 1.0, 1.0));
  let n111 = hash13(i + vec3<f32>(1.0, 1.0, 1.0));

  let nx00 = mix(n000, n100, u.x);
  let nx10 = mix(n010, n110, u.x);
  let nx01 = mix(n001, n101, u.x);
  let nx11 = mix(n011, n111, u.x);
  let nxy0 = mix(nx00, nx10, u.y);
  let nxy1 = mix(nx01, nx11, u.y);
  return mix(nxy0, nxy1, u.z);
}

fn fbm3(p: vec3<f32>) -> f32 {
  var total = 0.0;
  var amplitude = 0.5;
  var frequency = 1.0;
  for (var octave = 0; octave < 3; octave = octave + 1) {
    total += value_noise_3(p * frequency) * amplitude;
    frequency *= 2.07;
    amplitude *= 0.5;
  }
  return total;
}

fn get_ray_from_uv(uv: vec2<f32>) -> vec3<f32> {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = uCamera.inv_proj * clip;
  view = view / max(view.w, 1e-6);
  let world_target = (uCamera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
  return normalize(world_target - uCamera.cam_pos.xyz);
}

fn reconstruct_world_pos(uv: vec2<f32>, ray_dir: vec3<f32>, t_depth: f32) -> vec3<f32> {
  _ = uv;
  return uCamera.cam_pos.xyz + ray_dir * t_depth;
}

fn reproject_prev_uv(world_pos: vec3<f32>) -> vec2<f32> {
  let clip = history_params.prev_view_proj * vec4<f32>(world_pos, 1.0);
  if (clip.w <= 1e-5) {
    return vec2<f32>(-1.0, -1.0);
  }
  let ndc = clip.xy / clip.w;
  return vec2<f32>(ndc.x * 0.5 + 0.5, 0.5 - ndc.y * 0.5);
}

fn intersect_sphere(origin: vec3<f32>, dir: vec3<f32>, center: vec3<f32>, radius: f32) -> vec2<f32> {
  let oc = origin - center;
  let b = dot(oc, dir);
  let c = dot(oc, oc) - radius * radius;
  let h = b * b - c;
  if (h < 0.0) {
    return vec2<f32>(FAR_T, -FAR_T);
  }
  let s = sqrt(h);
  return vec2<f32>(-b - s, -b + s);
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

fn hg_phase(cos_theta: f32, g: f32) -> f32 {
  let gg = g * g;
  let denom = max(1.0 + gg - 2.0 * g * cos_theta, 1e-4);
  return (1.0 - gg) / (4.0 * PI * pow(denom, 1.5));
}

fn quat_conjugate(q: vec4<f32>) -> vec4<f32> {
  return vec4<f32>(-q.xyz, q.w);
}

fn quat_rotate(q: vec4<f32>, v: vec3<f32>) -> vec3<f32> {
  let t = 2.0 * cross(q.xyz, v);
  return v + q.w * t + cross(q.xyz, t);
}

fn medium_boundary_pos(m: MediumRecord, pos_ws: vec3<f32>) -> f32 {
  let shape = u32(m.noise.w + 0.5);
  if (shape == 1u) {
    let extents = max(m.shape0.xyz, vec3<f32>(1e-4));
    let inv_rot = quat_conjugate(m.shape1);
    let local = quat_rotate(inv_rot, pos_ws - m.bounds.xyz);
    let scaled = abs(local) / extents;
    return saturate(max(max(scaled.x, scaled.y), scaled.z));
  }

  let outer = m.bounds.w;
  let inner = m.shape0.w;
  let dist = length(pos_ws - m.bounds.xyz);
  let thickness = max(outer - inner, 1e-4);
  return saturate((dist - inner) / thickness);
}

fn medium_characteristic_thickness(m: MediumRecord) -> f32 {
  let shape = u32(m.noise.w + 0.5);
  if (shape == 1u) {
    let extents = max(m.shape0.xyz, vec3<f32>(1e-4));
    return max(min(min(extents.x, extents.y), extents.z) * 2.0, 1e-4);
  }
  let outer = m.bounds.w;
  let inner = m.shape0.w;
  if (inner > 0.0) {
    return max(outer - inner, 1e-4);
  }
  return max(outer, 1e-4);
}

fn intersect_box_local(origin: vec3<f32>, dir: vec3<f32>, half_extents: vec3<f32>) -> vec2<f32> {
  var tmin = -FAR_T;
  var tmax = FAR_T;
  for (var axis = 0; axis < 3; axis = axis + 1) {
    let o = origin[axis];
    let d = dir[axis];
    let e = half_extents[axis];
    if (abs(d) < 1e-5) {
      if (o < -e || o > e) {
        return vec2<f32>(FAR_T, -FAR_T);
      }
      continue;
    }
    let inv_d = 1.0 / d;
    var t1 = (-e - o) * inv_d;
    var t2 = (e - o) * inv_d;
    if (t1 > t2) {
      let tmp = t1;
      t1 = t2;
      t2 = tmp;
    }
    tmin = max(tmin, t1);
    tmax = min(tmax, t2);
    if (tmax < tmin) {
      return vec2<f32>(FAR_T, -FAR_T);
    }
  }
  return vec2<f32>(tmin, tmax);
}

fn intersect_medium(origin: vec3<f32>, dir: vec3<f32>, m: MediumRecord) -> vec2<f32> {
  let shape = u32(m.noise.w + 0.5);
  if (shape == 1u) {
    let inv_rot = quat_conjugate(m.shape1);
    let local_origin = quat_rotate(inv_rot, origin - m.bounds.xyz);
    let local_dir = quat_rotate(inv_rot, dir);
    return intersect_box_local(local_origin, local_dir, max(m.shape0.xyz, vec3<f32>(1e-4)));
  }
  return intersect_sphere(origin, dir, m.bounds.xyz, m.bounds.w);
}

fn first_positive_hit(hit: vec2<f32>) -> f32 {
  if (hit.x >= 0.0) {
    return hit.x;
  }
  if (hit.y >= 0.0) {
    return hit.y;
  }
  return FAR_T;
}

fn sanitize_opaque_depth(depth: f32) -> f32 {
  if (depth > 0.0 && depth < FAR_T) {
    return depth;
  }
  return FAR_T;
}

fn nearest_opaque_depth(ipos: vec2<i32>) -> f32 {
  let scene_t = sanitize_opaque_depth(textureLoad(in_depth, ipos, 0).r);
  let planet_t = sanitize_opaque_depth(textureLoad(planet_depth, ipos, 0).r);
  return min(scene_t, planet_t);
}

fn medium_density(m: MediumRecord, pos_ws: vec3<f32>, medium_idx: u32) -> f32 {
  let shape = u32(m.noise.w + 0.5);
  var radial = pos_ws - m.bounds.xyz;
  var shell_pos = 0.0;

  if (shape == 1u) {
    let extents = max(m.shape0.xyz, vec3<f32>(1e-4));
    let inv_rot = quat_conjugate(m.shape1);
    let local = quat_rotate(inv_rot, radial);
    let scaled = abs(local) / extents;
    shell_pos = max(max(scaled.x, scaled.y), scaled.z);
    if (shell_pos >= 1.0) {
      return 0.0;
    }
    radial = local;
  } else {
    let outer = m.bounds.w;
    let inner = m.shape0.w;
    let dist = length(radial);
    if (dist >= outer || dist <= inner) {
      return 0.0;
    }

    let thickness = max(outer - inner, 1e-4);
    shell_pos = saturate((dist - inner) / thickness);
  }

  var density = exp(-shell_pos * max(m.params.x, 0.05));
  let soft_extent = max(m.params.y, 1e-4);

  if (shape == 1u) {
    let extents = max(m.shape0.xyz, vec3<f32>(1e-4));
    let soft_ratio = saturate(soft_extent / max(min(min(extents.x, extents.y), extents.z), 1e-4));
    let outer_soft = 1.0 - smoothstep(1.0 - soft_ratio, 1.0, shell_pos);
    density *= outer_soft;
  } else {
    let outer = m.bounds.w;
    let dist = length(pos_ws - m.bounds.xyz);
    let outer_soft = 1.0 - smoothstep(outer - soft_extent, outer, dist);
    density *= outer_soft;
  }

  if (m.noise.y > 1e-4 && m.noise.x > 1e-4) {
    let dir = normalize(radial + vec3<f32>(1e-4, 0.0, 0.0));
    let n = fbm3(dir * m.noise.x + vec3<f32>(f32(medium_idx) * 3.1, -1.7, 2.9));
    let modulate = mix(1.0 - m.noise.y, 1.0, saturate(n));
    density *= modulate;
  }

  return density * max(m.scatter.w, 0.0);
}

@fragment
fn fs_main(@builtin(position) frag_pos: vec4<f32>, @location(0) uv: vec2<f32>) -> FSOut {
  _ = uv;
  let render_dims = textureDimensions(prev_history);
  let full_dims = textureDimensions(in_depth);
  let render_uv = (vec2<f32>(frag_pos.xy) + 0.5) / vec2<f32>(f32(render_dims.x), f32(render_dims.y));
  let ipos = vec2<i32>(
    clamp(i32(render_uv.x * f32(full_dims.x)), 0, i32(full_dims.x) - 1),
    clamp(i32(render_uv.y * f32(full_dims.y)), 0, i32(full_dims.y) - 1),
  );
  let t_limit = nearest_opaque_depth(ipos);

  let ray_origin = uCamera.cam_pos.xyz;
  let ray_dir = get_ray_from_uv(render_uv);
  let ambient = uCamera.ambient_color.xyz;
  let light_dir = primary_light_dir(ray_origin);
  let light_color = primary_light_color();
  let scene_has_opaque = t_limit < FAR_T * 0.5;

  var accum_rgb = vec3<f32>(0.0);
  var accum_a = 0.0;
  var accum_w = 0.0;
  var nearest_t = FAR_T;

  let count = min(medium_params.medium_count, arrayLength(&media));
  for (var i = 0u; i < count; i = i + 1u) {
    let m = media[i];
    let t_outer = intersect_medium(ray_origin, ray_dir, m);
    let t_start = max(t_outer.x, 0.0);
    let t_end = min(t_outer.y, t_limit);
    let has_opaque_behind = scene_has_opaque;
    if (t_end <= t_start) {
      continue;
    }
    nearest_t = min(nearest_t, t_start);

    let base_step_count = clamp(u32(max(m.noise.z, 4.0)), 4u, 24u);
    let min_step_count = select(4u, 8u, m.shape0.w > 0.0);
    let characteristic = medium_characteristic_thickness(m);
    let path_length = t_end - t_start;
    let coverage = saturate(path_length / characteristic);
    let density_budget = saturate(m.scatter.w * max(m.style1.x, m.style1.y) * characteristic * 0.25);
    let sample_factor = mix(0.18, 1.0, max(coverage * 0.55, density_budget));
    let step_count = clamp(u32(round(max(f32(min_step_count), f32(base_step_count) * sample_factor))), min_step_count, base_step_count);
    let segment_len = (t_end - t_start) / f32(step_count);
    if (segment_len <= 1e-5) {
      continue;
    }

    let pixel_jitter = hash13(vec3<f32>(f32(ipos.x), f32(ipos.y), f32(i) * 7.13 + history_params.params0.x * 12.3));
    var trans = vec3<f32>(1.0);
    var source = vec3<f32>(0.0);
    var integrated_tau = 0.0;
    for (var step = 0u; step < step_count; step = step + 1u) {
      let t = t_start + (f32(step) + pixel_jitter) * segment_len;
      let pos_ws = ray_origin + ray_dir * t;
      let density = medium_density(m, pos_ws, i);
      if (density <= 1e-5) {
        continue;
      }

      let radial_dir = normalize(pos_ws - m.bounds.xyz);
      let tangent = 1.0 - abs(dot(ray_dir, radial_dir));
      let tangent_boost = pow(saturate(tangent), max(m.style0.y, 0.25));
      let limb_scale = max(m.style0.x, 0.0) * select(0.32, 1.0, has_opaque_behind);
      let limb_boost = 1.0 + limb_scale * tangent_boost;
      let boundary_pos = medium_boundary_pos(m, pos_ws);
      var boundary_fade = 1.0;
      if (m.style1.w > m.style1.z + 1e-4) {
        boundary_fade = 1.0 - smoothstep(m.style1.z, m.style1.w, boundary_pos);
      }
      let space_edge_soften = mix(0.0, 1.0, boundary_fade);
      let horizon_glow = limb_boost * mix(0.5, 1.0, boundary_fade) * space_edge_soften;
      let cos_theta = dot(light_dir, -ray_dir);
      let phase = hg_phase(cos_theta, clamp(m.emission.w, -0.85, 0.85));
      let phase_term = 0.04 + phase * 1.15;
      let ambient_term = ambient * (m.params.w * 0.12) * boundary_fade;
      let direct_term = light_color * (m.params.z * phase_term) * horizon_glow;
      let space_scatter_soften = mix(0.0, 1.0, boundary_fade);
      let scatter = m.scatter.xyz * (ambient_term + direct_term) * 0.22 * space_scatter_soften + m.emission.xyz * (0.02 + tangent_boost * 0.05) * space_scatter_soften;
      let extinction_scale = select(max(m.style1.y, 1e-4), max(m.style1.x, 1e-4), has_opaque_behind);
      let optical = density * segment_len * extinction_scale * mix(0.45, 1.0, tangent_boost);
      integrated_tau += optical;
      
      let step_extinction = max(m.absorption.xyz * optical, vec3<f32>(1e-4));
      let alpha_step = vec3<f32>(1.0) - exp(-step_extinction);
      source += trans * scatter * alpha_step;

      if (has_opaque_behind && m.style0.z > 1e-4) {
        let haze_light = ambient * (0.03 + 0.02 * boundary_fade) +
          light_color * (0.012 + 0.03 * tangent_boost);
        let haze_tint = mix(m.scatter.xyz, m.scatter.xyz * m.absorption.xyz, clamp(m.style0.w, 0.0, 1.0));
        let disk_haze = haze_tint * haze_light * optical * m.style0.z * mix(1.0, 0.72, boundary_pos);
        source += trans * disk_haze;
      }

      trans *= exp(-step_extinction);
      if (max(trans.x, max(trans.y, trans.z)) <= 0.01) {
        break;
      }
    }

    let trans_scalar = dot(trans, vec3<f32>(0.33333334));
    let alpha_raw = clamp(1.0 - trans_scalar, 0.0, 0.995);
    if (alpha_raw <= 1e-4) {
      continue;
    }

    let color = source / max(alpha_raw, 1e-4);
    let z = clamp(t_start / max(t_limit, 1e-4), 0.0, 1.0);
    let tau_alpha = 1.0 - exp(-integrated_tau * select(m.style2.y, m.style2.x, has_opaque_behind));
    let alpha_shaped = max(alpha_raw, tau_alpha);
    let w = max(1e-3, alpha_shaped) * mix(0.32, 0.72, pow(1.0 - z, 2.0));
    let reveal_alpha = clamp(max(-log(max(trans_scalar, 1e-4)), integrated_tau) * select(m.style2.w, m.style2.z, has_opaque_behind), 0.0, 0.7);
    accum_rgb += color * alpha_shaped * w;
    accum_a += reveal_alpha;
    accum_w += alpha_shaped * w;
  }

  var current_color = vec3<f32>(0.0);
  var current_trans = 1.0;
  if (accum_w > 1e-5) {
    current_color = accum_rgb / accum_w;
    current_trans = exp(-2.0 * clamp(accum_a, 0.0, 50.0));
  }

  var out_color = vec4<f32>(current_color, current_trans);
  if (history_params.params0.w > 0.5 && nearest_t < FAR_T * 0.5) {
    let world_pos = reconstruct_world_pos(render_uv, ray_dir, nearest_t);
    let prev_uv = reproject_prev_uv(world_pos);
    if (all(prev_uv >= vec2<f32>(0.0)) && all(prev_uv <= vec2<f32>(1.0))) {
      let prev_dims = textureDimensions(prev_history);
      let prev_ipos = vec2<i32>(
        clamp(i32(prev_uv.x * f32(prev_dims.x)), 0, i32(prev_dims.x) - 1),
        clamp(i32(prev_uv.y * f32(prev_dims.y)), 0, i32(prev_dims.y) - 1),
      );
      let prev_depth = textureLoad(prev_history_depth, prev_ipos, 0).r;
      let prev_t = length(world_pos - history_params.prev_cam_pos.xyz);
      var history_weight = history_params.params0.z;
      if (prev_depth > 0.0 && prev_depth < FAR_T * 0.5) {
        let depth_delta = abs(prev_depth - prev_t);
        let rejection = saturate(1.0 - depth_delta / (0.005 * max(prev_t, 1.0) + 0.08));
        history_weight *= rejection;
      } else {
        history_weight = 0.0;
      }
      if (history_weight > 1e-4) {
        let prev_color = textureLoad(prev_history, prev_ipos, 0);
        out_color = mix(out_color, prev_color, history_weight);
      }
    }
  }

  let out_depth = select(nearest_t, FAR_T, nearest_t >= FAR_T * 0.5);
  return FSOut(out_color, out_depth);
}
