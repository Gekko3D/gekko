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
};

struct Light {
  position: vec4<f32>,
  direction: vec4<f32>,
  color: vec4<f32>,
  params: vec4<f32>,
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
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
  grid: vec4<f32>,
};

struct Ray {
  origin: vec3<f32>,
  dir: vec3<f32>,
  inv_dir: vec3<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

struct FSOut {
  @location(0) accum: vec4<f32>,
  @location(1) weight: f32,
};

@group(0) @binding(0) var<uniform> uCamera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;
@group(1) @binding(0) var<uniform> ca_params: CAParams;
@group(1) @binding(1) var<storage, read> volumes: array<VolumeRecord>;
@group(1) @binding(2) var ca_field: texture_3d<f32>;
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

fn min_voxel_world_size(info: VolumeRecord) -> f32 {
  let sx = length(info.local_to_world[0].xyz);
  let sy = length(info.local_to_world[1].xyz);
  let sz = length(info.local_to_world[2].xyz);
  return max(1e-3, min(sx, min(sy, sz)));
}

fn quantize01(v: f32, levels: f32) -> f32 {
  return floor(clamp(v, 0.0, 0.9999) * levels) / max(levels - 1.0, 1.0);
}

fn hash21(p: vec2<f32>) -> f32 {
  let h = dot(p, vec2<f32>(127.1, 311.7));
  return fract(sin(h) * 43758.5453);
}

fn lobe_shape(p: vec2<f32>, center: vec2<f32>, radius: f32, stretch: vec2<f32>) -> f32 {
  let q = (p - center) / max(stretch, vec2<f32>(0.001));
  let d = length(q);
  return max(0.0, 1.0 - d / max(radius, 0.001));
}

fn plume_envelope(pos_os: vec3<f32>, info: VolumeRecord) -> f32 {
  let dim = max(info.grid.xyz, vec3<f32>(1.0));
  let uvw = (pos_os + 0.5) / dim;
  let p = (uvw.xz - vec2<f32>(0.5)) * 2.0;
  let h = clamp(uvw.y, 0.0, 1.0);
  let t = ca_params.elapsed;
  let slice_noise = hash21(floor((p + vec2<f32>(2.0)) * 4.0) + vec2<f32>(floor(h * 18.0), floor(t * 2.0)));
  let wobble = vec2<f32>(
    sin(t * 0.8 + h * 6.0),
    cos(t * 0.6 + h * 4.5)
  ) * mix(0.24, 0.08, h);

  var shape = 0.0;
  if (u32(info.render_params.z + 0.5) == 1u) {
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
  let top_fade = 1.0 - smoothstep(0.62, 1.0, h);
  let bottom_soften = smoothstep(0.0, 0.08, h);
  return clamp(breakup * top_fade * bottom_soften, 0.0, 1.0);
}

fn volume_light_transmittance(pos_os: vec3<f32>, info: VolumeRecord, light_dir_ws: vec3<f32>) -> f32 {
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

  let step_os = 1.0;
  var tau = 0.0;
  var t = step_os;
  var steps = 0;
  loop {
    if (t >= t_exit || tau > 6.0 || steps >= 6) {
      break;
    }
    steps += 1;
    let p = pos_os + light_dir_os * t;
    let d = sample_volume_voxel(p, info).x;
    tau += max(d, 0.0) * max(info.render_params.x, 0.01) * step_os * 0.8;
    t += step_os;
  }
  return quantize01(exp(-tau), 3.0);
}

fn primary_light_dir(pos_ws: vec3<f32>) -> vec3<f32> {
  let count = arrayLength(&lights);
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
  let count = arrayLength(&lights);
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
    let ray_os = transform_ray(ray_ws, info.world_to_local);
    let bounds = intersect_aabb(ray_os, vec3<f32>(0.0), info.grid.xyz);
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

    let dt = min_voxel_world_size(info);
    let volume_type = u32(info.render_params.z + 0.5);

    var t = t_enter + dt * 0.5;
    var trans = 1.0;
    var premul_rgb = vec3<f32>(0.0);
    var steps = 0;
    loop {
      if (t >= t_exit || trans <= 0.01 || steps >= 128) {
        break;
      }
      steps += 1;

      let pos_ws = ray_ws.origin + ray_ws.dir * t;
      let pos_os = (info.world_to_local * vec4<f32>(pos_ws, 1.0)).xyz;
      let sample = sample_volume_voxel(pos_os, info);
      let env = plume_envelope(pos_os, info);
      let raw_density = max(sample.x, 0.0) * env;
      let density = max(raw_density - 0.14, 0.0);
      let temp = max(sample.y, 0.0);
      if (density > 0.001) {
        let shaped_density = density * density * (1.1 + 0.6 * env);
        let sigma_t = shaped_density * max(0.01, info.render_params.x);
        let alpha_step = 1.0 - exp(-sigma_t * dt);
        if (alpha_step > 1e-4) {
          let ldir = primary_light_dir(pos_ws);
          let phase = quantize01(0.35 + 0.65 * clamp(dot(ldir, -ray_ws.dir), 0.0, 1.0), 3.0);
          let light_trans = volume_light_transmittance(pos_os, info, ldir);
          let light_term = ambient * 0.65 + light_color * phase * light_trans * 0.7;
          let scatter = info.scatter_color.xyz * light_term * (0.55 + 0.45 * env);
          var source = scatter;
          if (volume_type == 1u) {
            let flame = fire_color(temp) * shaped_density * temp * max(0.0, info.render_params.y);
            source += flame * (0.75 + 0.25 * light_trans);
          }
          premul_rgb += trans * source * alpha_step;
          trans *= (1.0 - alpha_step);
        }
      }

      t += dt;
    }

    let alpha = clamp(1.0 - trans, 0.0, 0.995);
    if (alpha > 1e-4) {
      let color = premul_rgb / alpha;
      let z = clamp(t_enter / max(t_limit, 1e-4), 0.0, 1.0);
      let w = max(1e-3, alpha) * pow(1.0 - z, 4.0);
      accum_rgb += color * alpha * w;
      accum_a += alpha;
      accum_w += alpha * w;
    }
  }

  return FSOut(vec4<f32>(accum_rgb, accum_a), accum_w);
}
