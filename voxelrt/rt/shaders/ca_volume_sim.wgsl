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
  sim_params: vec4<f32>,    // diffusion, buoyancy, cooling, dissipation
  render_params: vec4<f32>, // extinction, emission, type, steps_pending
  scatter_color: vec4<f32>, // rgb, step_dt
  grid: vec4<f32>,          // nx, ny, nz, atlas_z_offset
};

@group(0) @binding(0) var<uniform> params: CAParams;
@group(0) @binding(1) var<storage, read> volumes: array<VolumeRecord>;
@group(1) @binding(0) var src_field: texture_3d<f32>;
@group(1) @binding(1) var dst_field: texture_storage_3d<rgba32float, write>;

fn find_volume(coord: vec3<u32>) -> i32 {
  for (var i = 0u; i < params.volume_count; i = i + 1u) {
    let v = volumes[i];
    let res = vec3<u32>(u32(v.grid.x), u32(v.grid.y), u32(v.grid.z));
    let z0 = u32(v.grid.w);
    if (coord.x < res.x && coord.y < res.y && coord.z >= z0 && coord.z < z0 + res.z) {
      return i32(i);
    }
  }
  return -1;
}

fn sample_src(coord: vec3<i32>) -> vec4<f32> {
  let clamped = vec3<i32>(
    clamp(coord.x, 0, i32(params.atlas_width) - 1),
    clamp(coord.y, 0, i32(params.atlas_height) - 1),
    clamp(coord.z, 0, i32(params.atlas_depth) - 1),
  );
  return textureLoad(src_field, clamped, 0);
}

fn hash13(p: vec3<f32>) -> f32 {
  let h = dot(p, vec3<f32>(127.1, 311.7, 74.7));
  return fract(sin(h) * 43758.5453);
}

fn source_lobe(localf: vec3<f32>, center: vec2<f32>, radius: f32, stretch: vec2<f32>) -> f32 {
  let p = (localf.xz - center) / max(stretch, vec2<f32>(0.001));
  let d = length(p);
  return max(0.0, 1.0 - d / max(radius, 0.001));
}

@compute @workgroup_size(4, 4, 4)
fn simulate(@builtin(global_invocation_id) gid: vec3<u32>) {
  if (gid.x >= params.atlas_width || gid.y >= params.atlas_height || gid.z >= params.atlas_depth) {
    return;
  }

  let volume_idx = find_volume(gid);
  if (volume_idx < 0) {
    textureStore(dst_field, vec3<i32>(gid), vec4<f32>(0.0));
    return;
  }

  let v = volumes[u32(volume_idx)];
  let res = vec3<u32>(u32(v.grid.x), u32(v.grid.y), u32(v.grid.z));
  let z0 = u32(v.grid.w);
  let local = vec3<u32>(gid.x, gid.y, gid.z - z0);
  let steps = u32(clamp(v.render_params.w + 0.5, 0.0, 4.0));
  if (steps == 0u) {
    textureStore(dst_field, vec3<i32>(gid), sample_src(vec3<i32>(gid)));
    return;
  }

  let diffusion = clamp(v.sim_params.x, 0.0, 1.0);
  let buoyancy = clamp(v.sim_params.y, -1.0, 1.0);
  let cooling = clamp(v.sim_params.z, 0.0, 1.0);
  let dissipation = clamp(v.sim_params.w, 0.0, 1.0);
  let step_dt = max(v.scatter_color.w, params.dt);
  let volume_type = u32(v.render_params.z + 0.5);
  let localf = vec3<f32>(f32(local.x), f32(local.y), f32(local.z));
  let dimsf = max(vec3<f32>(f32(res.x), f32(res.y), f32(res.z)), vec3<f32>(1.0));
  let uvw = (localf + 0.5) / dimsf;

  var density = sample_src(vec3<i32>(gid)).x;
  var temp = sample_src(vec3<i32>(gid)).y;

  for (var step_idx = 0u; step_idx < steps; step_idx = step_idx + 1u) {
    let xp = sample_src(vec3<i32>(i32(min(gid.x + 1u, res.x - 1u)), i32(gid.y), i32(gid.z))).xy;
    let xm = sample_src(vec3<i32>(max(i32(gid.x) - 1, 0), i32(gid.y), i32(gid.z))).xy;
    let yp = sample_src(vec3<i32>(i32(gid.x), min(i32(gid.y) + 1, i32(res.y) - 1), i32(gid.z))).xy;
    let ym = sample_src(vec3<i32>(i32(gid.x), max(i32(gid.y) - 1, 0), i32(gid.z))).xy;
    let zp = sample_src(vec3<i32>(i32(gid.x), i32(gid.y), min(i32(gid.z) + 1, i32(z0 + res.z) - 1))).xy;
    let zm = sample_src(vec3<i32>(i32(gid.x), i32(gid.y), max(i32(gid.z) - 1, i32(z0)))).xy;

    let avg_density = (xp.x + xm.x + yp.x + ym.x + zp.x + zm.x) / 6.0;
    let avg_temp = (xp.y + xm.y + yp.y + ym.y + zp.y + zm.y) / 6.0;

    let center_x = (uvw.x - 0.5) * 2.0;
    let center_z = (uvw.z - 0.5) * 2.0;
    let radial = clamp(length(vec2<f32>(center_x, center_z)), 0.0, 1.0);
    let height = clamp(uvw.y, 0.0, 1.0);
    let side_vent = smoothstep(0.72, 1.0, radial);
    let top_vent = smoothstep(0.68, 1.0, height);
    let base_decay = max(0.0, 1.0 - dissipation * step_dt * 3.0);
    let vent_decay = 1.0 - side_vent * 0.22 - top_vent * 0.55;

    let swirl_phase = params.elapsed * 1.7 + localf.y * 0.11;
    let swirl_noise = hash13(vec3<f32>(floor(localf.xz * 0.5), floor(params.elapsed * 2.0)));
    let side_pick = step(0.5, fract(swirl_noise + 0.25 * sin(swirl_phase)));
    let side_density = mix(xm.x + zm.x, xp.x + zp.x, side_pick) * 0.5;
    let side_temp = mix(xm.y + zm.y, xp.y + zp.y, side_pick) * 0.5;

    let upwind_density = mix(avg_density, ym.x, clamp(buoyancy * 0.7, 0.0, 1.0));
    let upwind_temp = mix(avg_temp, ym.y, clamp((0.3 + buoyancy) * 0.65, 0.0, 1.0));

    density = mix(density, avg_density, diffusion * 0.25);
    density = mix(density, upwind_density, clamp(0.2 + buoyancy * 0.55, 0.0, 0.9));
    density = mix(density, side_density, clamp(diffusion * 0.18 + (1.0 - height) * 0.08, 0.0, 0.35));
    density *= base_decay * max(0.0, vent_decay);

    if (volume_type == 1u) {
      let temp_decay = max(0.0, 1.0 - cooling * step_dt * 4.0);
      let cooling_with_height = 1.0 - height * 0.35;
      temp = mix(temp, upwind_temp, clamp(0.3 + diffusion * 0.2, 0.0, 0.7));
      temp = mix(temp, side_temp, 0.08);
      temp *= temp_decay * cooling_with_height * (1.0 - top_vent * 0.45);
      density = max(density, temp * mix(0.22, 0.08, height));
    } else {
      temp = mix(temp, avg_temp, 0.08) * 0.82;
    }
  }

  let nx = max(1.0, f32(res.x));
  let nz = max(1.0, f32(res.z));
  let cx = nx * 0.5;
  let cz = nz * 0.5;
  let dx = f32(local.x) + 0.5 - cx;
  let dz = f32(local.z) + 0.5 - cz;
  let radius = sqrt(dx * dx + dz * dz);
  let radial_norm = radius / max(1.0, max(nx, nz) * 0.5);
  if (local.y < 4u) {
    let t = params.elapsed;
    let base_center = vec2<f32>(cx, cz);
    let jitter_a = vec2<f32>(sin(t * 1.9), cos(t * 1.4)) * vec2<f32>(1.3, 0.9);
    let jitter_b = vec2<f32>(cos(t * 1.2 + 0.7), sin(t * 1.7 + 1.1)) * vec2<f32>(1.0, 1.4);
    let jitter_c = vec2<f32>(sin(t * 2.3 + 2.0), sin(t * 1.5 + 0.3)) * vec2<f32>(0.8, 1.1);

    var source_shape = 0.0;
    if (volume_type == 1u) {
      let lobe_a = source_lobe(localf, base_center + vec2<f32>(-2.2, -0.8) + jitter_a, 1.35, vec2<f32>(1.1, 0.8));
      let lobe_b = source_lobe(localf, base_center + vec2<f32>(1.6, 0.9) + jitter_b, 1.15, vec2<f32>(0.8, 1.0));
      let lobe_c = source_lobe(localf, base_center + vec2<f32>(0.4, -2.0) + jitter_c, 0.95, vec2<f32>(0.7, 1.2));
      source_shape = max(lobe_a, max(lobe_b, lobe_c));
    } else {
      let drift = vec2<f32>(sin(t * 0.6), cos(t * 0.4 + 0.5)) * vec2<f32>(1.8, 1.2);
      let lobe_a = source_lobe(localf, base_center + vec2<f32>(-3.2, -1.5) + drift, 2.4, vec2<f32>(1.8, 1.0));
      let lobe_b = source_lobe(localf, base_center + vec2<f32>(1.1, 2.4) - drift * 0.6, 2.0, vec2<f32>(1.1, 1.7));
      let lobe_c = source_lobe(localf, base_center + vec2<f32>(3.0, -0.9) + vec2<f32>(0.0, sin(t * 0.9) * 1.4), 1.6, vec2<f32>(1.5, 0.9));
      source_shape = max(lobe_a, max(lobe_b, lobe_c));
    }

    let breakup = hash13(vec3<f32>(floor(localf.x * 0.8), floor(localf.z * 0.8), floor(params.elapsed * 3.0)));
    let breakup_mask = smoothstep(0.22, 0.82, source_shape + (breakup - 0.5) * 0.42);
    let vertical = 1.0 - f32(local.y) / 4.5;
    let source_strength = max(0.0, breakup_mask * vertical);
    density = max(density, 0.08 + source_strength * mix(0.65, 0.95, 1.0 - uvw.y));
    if (volume_type == 1u) {
      temp = max(temp, source_strength * 1.45);
    } else {
      density *= 1.0 - smoothstep(0.78, 1.0, radial_norm) * 0.18;
    }
  }

  density *= 1.0 - smoothstep(0.85, 1.0, uvw.y) * 0.85;
  density *= 1.0 - smoothstep(0.82, 1.0, radial_norm) * 0.45;
  temp *= 1.0 - smoothstep(0.78, 1.0, uvw.y) * 0.75;

  density = clamp(density, 0.0, 1.5);
  temp = clamp(temp, 0.0, 2.0);
  textureStore(dst_field, vec3<i32>(gid), vec4<f32>(density, temp, 0.0, 0.0));
}
