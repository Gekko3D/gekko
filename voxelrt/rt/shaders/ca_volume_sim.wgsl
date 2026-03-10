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
  shadow_tint: vec4<f32>,
  absorption_color: vec4<f32>,
  grid: vec4<f32>,          // nx, ny, nz, atlas_z_offset
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

@group(0) @binding(0) var<uniform> params: CAParams;
@group(0) @binding(1) var<storage, read> volumes: array<VolumeRecord>;
@group(0) @binding(2) var<storage, read> presets: array<PresetRecord>;
@group(1) @binding(0) var src_field: texture_3d<f32>;
@group(1) @binding(1) var dst_field: texture_storage_3d<rgba16float, write>;

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

fn sample_src_local(local: vec3<i32>, res: vec3<u32>, z0: u32) -> vec2<f32> {
  let clamped = vec3<i32>(
    clamp(local.x, 0, i32(res.x) - 1),
    clamp(local.y, 0, i32(res.y) - 1),
    clamp(local.z, 0, i32(res.z) - 1),
  );
  return sample_src(vec3<i32>(clamped.x, clamped.y, i32(z0) + clamped.z)).xy;
}

fn sample_src_local4(local: vec3<i32>, res: vec3<u32>, z0: u32) -> vec4<f32> {
  let clamped = vec3<i32>(
    clamp(local.x, 0, i32(res.x) - 1),
    clamp(local.y, 0, i32(res.y) - 1),
    clamp(local.z, 0, i32(res.z) - 1),
  );
  return sample_src(vec3<i32>(clamped.x, clamped.y, i32(z0) + clamped.z));
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

fn source_box(localf: vec3<f32>, center: vec2<f32>, half_size: vec2<f32>, feather: f32) -> f32 {
  let d = abs(localf.xz - center) - max(half_size, vec2<f32>(0.001));
  let edge = max(d.x, d.y);
  return 1.0 - smoothstep(0.0, max(feather, 0.001), edge);
}

fn source_pulse(t: f32, rate: f32, phase: f32) -> f32 {
  return 0.5 + 0.5 * sin(t * rate + phase);
}

fn flow_potential(p: vec3<f32>, t: f32, preset: u32, volume_type: u32) -> f32 {
  let presetf = f32(preset);
  let typef = f32(volume_type);
  let band0 = sin(p.x * 0.19 + t * (0.7 + presetf * 0.05) + p.y * 0.04);
  let band1 = cos(p.z * 0.23 - t * (0.55 + typef * 0.12) + p.y * 0.06 + presetf * 0.8);
  let band2 = sin((p.x + p.z) * 0.14 + t * 0.32 + p.y * 0.08);
  let band3 = cos((p.x - p.z) * 0.17 - t * 0.44 + presetf * 0.6);
  return band0 + band1 * 0.9 + band2 * 0.65 + band3 * 0.45;
}

fn flow_curl(localf: vec3<f32>, uvw: vec3<f32>, t: f32, preset: u32, volume_type: u32) -> vec2<f32> {
  let eps = 1.0;
  let px = vec3<f32>(eps, 0.0, 0.0);
  let pz = vec3<f32>(0.0, 0.0, eps);
  let dphi_dz = (flow_potential(localf + pz, t, preset, volume_type) - flow_potential(localf - pz, t, preset, volume_type)) / (2.0 * eps);
  let dphi_dx = (flow_potential(localf + px, t, preset, volume_type) - flow_potential(localf - px, t, preset, volume_type)) / (2.0 * eps);

  var curl = vec2<f32>(dphi_dz, -dphi_dx);
  let p_data = presets[preset];
  var lean = vec2<f32>(0.0);
  let height = clamp(uvw.y, 0.0, 1.0);
  if (preset == 1u) {
    lean = vec2<f32>(sin(t * 2.6 + localf.y * 0.12), cos(t * 2.2 + localf.y * 0.09)) * mix(0.04, 0.12, height);
  } else if (preset == 2u) {
    let sign = select(-1.0, 1.0, volume_type == 1u);
    lean = vec2<f32>(
      sin(t * 0.85 + localf.z * 0.08) * 0.18,
      cos(t * 0.74 + localf.x * 0.08) * 0.16
    ) * mix(0.12, 0.4, height) * sign;
  } else if (preset == 3u) {
    lean = vec2<f32>(
      sin(t * 4.6 + localf.z * 0.23 + localf.y * 0.1) * 0.3 +
      sin(t * 9.4 + localf.y * 0.42 + localf.x * 0.07) * 0.1,
      cos(t * 3.6 + localf.x * 0.19) * 0.16 +
      sin(t * 6.8 + localf.z * 0.24 + localf.y * 0.09) * 0.1
    ) * mix(0.42, 1.02, 1.0 - height);
  } else {
    lean = vec2<f32>(sin(t * 0.6), cos(t * 0.48)) * mix(0.06, 0.2, height);
  }
  curl += lean;
  return curl;
}

fn packed_volume_type(v: VolumeRecord) -> u32 {
  return u32(v.render_params.z + 0.5) & 7u;
}

fn packed_volume_preset(v: VolumeRecord) -> u32 {
  return u32(v.render_params.z + 0.5) >> 3u;
}

fn volume_intensity(v: VolumeRecord) -> f32 {
  return clamp(v.shadow_tint.w, 0.0, 1.0);
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
  let volume_type = packed_volume_type(v);
  let preset = packed_volume_preset(v);
  let p_data = presets[preset];
  let intensity = volume_intensity(v);
  let localf = vec3<f32>(f32(local.x), f32(local.y), f32(local.z));
  let dimsf = max(vec3<f32>(f32(res.x), f32(res.y), f32(res.z)), vec3<f32>(1.0));
  let uvw = (localf + 0.5) / dimsf;
  let t = params.elapsed;
  let cycle_phase = fract((t + select(f32(z0) * 0.073, 0.0, preset == 4u)) / 3.6);
  let burst_time = cycle_phase * 3.6;
  let burst_flash = 1.0 - smoothstep(0.01, 0.32, burst_time);
  let burst_smoke = smoothstep(0.12, 0.55, burst_time) * (1.0 - smoothstep(0.8, 1.7, burst_time));
  let burst_afterglow = smoothstep(0.2, 0.55, burst_time) * (1.0 - smoothstep(1.4, 2.7, burst_time));
  let burst_rise = smoothstep(0.18, 1.1, burst_time) * (1.0 - smoothstep(2.1, 3.2, burst_time));

  let center_sample = sample_src(vec3<i32>(gid));
  var density = center_sample.x;
  var temp = center_sample.y;
  var velocity = center_sample.zw;

  for (var step_idx = 0u; step_idx < steps; step_idx = step_idx + 1u) {
    let xp4 = sample_src(vec3<i32>(i32(min(gid.x + 1u, res.x - 1u)), i32(gid.y), i32(gid.z)));
    let xm4 = sample_src(vec3<i32>(max(i32(gid.x) - 1, 0), i32(gid.y), i32(gid.z)));
    let yp4 = sample_src(vec3<i32>(i32(gid.x), min(i32(gid.y) + 1, i32(res.y) - 1), i32(gid.z)));
    let ym4 = sample_src(vec3<i32>(i32(gid.x), max(i32(gid.y) - 1, 0), i32(gid.z)));
    let zp4 = sample_src(vec3<i32>(i32(gid.x), i32(gid.y), min(i32(gid.z) + 1, i32(z0 + res.z) - 1)));
    let zm4 = sample_src(vec3<i32>(i32(gid.x), i32(gid.y), max(i32(gid.z) - 1, i32(z0))));
    let xp = xp4.xy;
    let xm = xm4.xy;
    let yp = yp4.xy;
    let ym = ym4.xy;
    let zp = zp4.xy;
    let zm = zm4.xy;

    let avg_density = (xp.x + xm.x + yp.x + ym.x + zp.x + zm.x) / 6.0;
    let avg_temp = (xp.y + xm.y + yp.y + ym.y + zp.y + zm.y) / 6.0;
    let avg_velocity = (xp4.zw + xm4.zw + yp4.zw + ym4.zw + zp4.zw + zm4.zw) / 6.0;

    // p_data defined earlier
    let center_x = (uvw.x - 0.5) * 2.0;
    let center_z = (uvw.z - 0.5) * 2.0;
    let radial = clamp(length(vec2<f32>(center_x, center_z)), 0.0, 1.0);
    let height = clamp(uvw.y, 0.0, 1.0);
    var side_vent = smoothstep(0.72, 1.0, radial);
    var top_vent = smoothstep(0.68, 1.0, height);
    if (preset == 2u) {
      side_vent = smoothstep(0.88, 1.18, radial);
      top_vent = smoothstep(0.9, 1.16, height);
    } else if (preset == 3u) {
      side_vent = smoothstep(0.96, 1.2, radial);
      top_vent = smoothstep(0.92, 1.14, height);
    } else if (preset == 1u) {
      side_vent = smoothstep(0.86, 1.1, radial);
      top_vent = smoothstep(0.82, 1.08, height);
    }
    var dissipation_scale = select(3.0, 2.2, preset == 1u);
    dissipation_scale = select(dissipation_scale, 1.6, preset == 3u);

    let base_decay = max(0.0, 1.0 - p_data.dissipation * step_dt * dissipation_scale);
    var vent_decay = 1.0 - side_vent * 0.22 - top_vent * 0.55;
    if (preset == 2u) {
      vent_decay = 1.0 - side_vent * 0.08 - top_vent * 0.16;
    } else if (preset == 3u) {
      vent_decay = 1.0 - side_vent * 0.03 - top_vent * 0.12;
    } else if (preset == 1u) {
      vent_decay = 1.0 - side_vent * 0.08 - top_vent * 0.2;
    }

    let swirl_phase = t * 1.7 + localf.y * 0.11;
    let swirl_noise = hash13(vec3<f32>(floor(localf.xz * 0.5), floor(t * 2.0)));
    let side_pick = step(0.5, fract(swirl_noise + 0.25 * sin(swirl_phase)));
    let side_density = mix(xm.x + zm.x, xp.x + zp.x, side_pick) * 0.5;
    let side_temp = mix(xm.y + zm.y, xp.y + zp.y, side_pick) * 0.5;

    let upwind_density = mix(avg_density, ym.x, clamp(buoyancy * 0.7, 0.0, 1.0));
    let upwind_temp = mix(avg_temp, ym.y, clamp((0.3 + buoyancy) * 0.65, 0.0, 1.0));

    var carry_mix = 0.12;
    var carry_drift = vec2<f32>(0.0);
    if (preset == 1u) {
      carry_drift = vec2<f32>(
        sin(swirl_phase * 1.6 + localf.z * 0.22),
        cos(swirl_phase * 1.35 + localf.x * 0.18),
      ) * mix(0.06, 0.3, height);
      carry_mix = 0.16;
    } else if (preset == 2u) {
      carry_drift = vec2<f32>(
        sin(swirl_phase * 0.95 + height * 4.2 + localf.z * 0.11),
        cos(swirl_phase * 0.82 + height * 3.5 + localf.x * 0.14),
      ) * mix(0.18, 0.95, height);
      carry_mix = select(0.34, 0.22, volume_type == 1u);
    } else if (preset == 3u) {
      carry_drift = vec2<f32>(sin(swirl_phase * 2.4), cos(swirl_phase * 2.0)) * vec2<f32>(0.38, 0.07);
      carry_mix = 0.14;
    } else {
      carry_drift = vec2<f32>(sin(swirl_phase), cos(swirl_phase * 0.9)) * mix(0.1, 0.45, height);
    }
    let carry = sample_src_local(
      vec3<i32>(
        i32(round(localf.x - carry_drift.x)),
        i32(local.y) - 1,
        i32(round(localf.z - carry_drift.y)),
      ),
      res,
      z0,
    );

    var swirl_force = vec2<f32>(0.0);
    var updraft = clamp(max(temp, avg_temp) * 0.7 + max(density, avg_density) * 0.12, 0.0, 1.6) * buoyancy;
    var vel_damping = 0.9;
    if (preset == 1u) {
      swirl_force = vec2<f32>(sin(swirl_phase * 1.9), cos(swirl_phase * 1.65)) * mix(0.03, 0.12, height);
      vel_damping = 0.88;
    } else if (preset == 2u) {
      swirl_force = vec2<f32>(sin(swirl_phase * 1.05 + localf.z * 0.12), cos(swirl_phase * 0.96 + localf.x * 0.12)) * mix(0.05, 0.2, height);
      updraft *= select(0.74, 1.18, volume_type == 1u);
      vel_damping = select(0.92, 0.9, volume_type == 1u);
    } else if (preset == 3u) {
      swirl_force = vec2<f32>(sin(swirl_phase * 2.0) * 0.04, cos(swirl_phase * 2.7) * 0.015);
      updraft *= 0.2;
      vel_damping = 0.97;
    } else if (preset == 4u) {
      let rel = normalize(vec2<f32>(center_x, center_z) + vec2<f32>(0.001));
      let lower_bias = 1.0 - smoothstep(0.22, 0.68, height);
      let radial_push = rel * burst_flash * mix(1.05, 0.16, smoothstep(0.0, 0.68, height));
      let side_roll = rel * burst_rise * mix(0.08, 0.24, smoothstep(0.24, 0.9, height));
      let roll_dir = vec2<f32>(
        sin(t * 0.82 + localf.y * 0.11 + localf.z * 0.06),
        cos(t * 0.74 + localf.y * 0.09 - localf.x * 0.05),
      );
      let cap_drift = roll_dir * (burst_afterglow + burst_rise * 0.42) * mix(0.05, 0.18, height);
      swirl_force += radial_push + side_roll + cap_drift * (clamp(max(density, temp) * 0.85 + avg_density * 0.35, 0.0, 1.4));
      updraft += burst_afterglow * mix(0.28, 1.06, lower_bias) + burst_rise * mix(0.26, 1.08, smoothstep(0.08, 0.84, height));
      vel_damping = mix(0.84, 0.95, burst_afterglow);
    } else {
      swirl_force = vec2<f32>(sin(swirl_phase), cos(swirl_phase * 0.92)) * mix(0.03, 0.1, height);
    }
    let flow_field = flow_curl(localf, uvw, t + f32(step_idx) * 0.13, preset, volume_type);
    var flow_strength = mix(0.04, 0.18, height) * (0.65 + p_data.diffusion * 0.75);
    if (preset == 1u) {
      flow_strength = mix(0.03, 0.12, height) * (0.8 + diffusion * 0.5);
    } else if (preset == 2u) {
      flow_strength = mix(0.08, select(0.28, 0.2, volume_type == 1u), height) * (0.8 + p_data.diffusion * 0.7);
    } else if (preset == 3u) {
      flow_strength = mix(0.1, 0.18, 1.0 - height) * (0.6 + p_data.diffusion * 0.35);
    } else if (preset == 4u) {
      flow_strength = mix(0.18, 0.14, height) * (0.92 + p_data.diffusion * 0.22) * mix(1.2, 0.68, burst_afterglow);
    }
    let activity = clamp(max(density, temp) * 0.85 + avg_density * 0.35, 0.0, 1.4);
    swirl_force += flow_field * flow_strength * activity;
    velocity = mix(velocity, avg_velocity, clamp(0.18 + diffusion * 0.22, 0.0, 0.42));
    velocity += swirl_force * (0.6 + diffusion * 0.5);
    velocity = mix(velocity, carry_drift * 0.12, clamp(carry_mix * 0.25, 0.0, 0.14));
    velocity *= vel_damping;
    velocity = clamp(velocity, vec2<f32>(-1.8), vec2<f32>(1.8));

    let advect_from = sample_src_local4(
      vec3<i32>(
        i32(round(localf.x - velocity.x)),
        i32(round(localf.y - updraft)),
        i32(round(localf.z - velocity.y)),
      ),
      res,
      z0,
    );

    var side_mix = clamp(diffusion * 0.18 + (1.0 - height) * 0.08, 0.0, 0.35);
    var upwind_mix = clamp(0.2 + buoyancy * 0.55, 0.0, 0.9);
    if (preset == 1u) {
      side_mix = clamp(0.18 + diffusion * 0.35 + height * 0.16, 0.0, 0.62);
      upwind_mix = clamp(0.12 + buoyancy * 0.28, 0.0, 0.48);
    } else if (preset == 2u) {
      side_mix = clamp(0.18 + diffusion * 0.35 + height * 0.16, 0.0, 0.62);
      upwind_mix = clamp(0.12 + buoyancy * 0.28, 0.0, 0.48);
      if (volume_type == 0u) {
        side_mix = clamp(0.14 + diffusion * 0.22 + height * 0.1, 0.0, 0.42);
        upwind_mix = clamp(0.1 + buoyancy * 0.22, 0.0, 0.32);
      }
    } else if (preset == 3u) {
      side_mix = clamp(0.04 + diffusion * 0.08, 0.0, 0.12);
      upwind_mix = clamp(0.32 + buoyancy * 0.24, 0.0, 0.6);
    } else if (preset == 4u) {
      side_mix = clamp(0.12 + burst_afterglow * 0.22 + height * 0.12, 0.0, 0.52);
      upwind_mix = clamp(0.24 + burst_rise * 0.42 + buoyancy * 0.18, 0.0, 0.78);
    }

    density = mix(density, advect_from.x, clamp(0.22 + max(buoyancy, 0.0) * 0.2, 0.0, 0.48));
    density = mix(density, avg_density, diffusion * 0.25);
    density = mix(density, upwind_density, upwind_mix);
    density = mix(density, side_density, side_mix);
    density = mix(density, carry.x, clamp(carry_mix * (0.4 + max(buoyancy, 0.0) * 0.45), 0.0, 0.5));
    density *= max(0.0, 1.0 - dissipation * step_dt * dissipation_scale) * max(0.0, vent_decay);

    if (preset == 4u || volume_type == 1u) {
      let temp_decay = max(0.0, 1.0 - cooling * step_dt * 4.0);
      let prev_heat = advect_from.y;
      var cooling_with_height = 1.0 - height * 0.35;
      if (preset == 1u) {
        cooling_with_height = 1.0 - height * 0.22;
      } else if (preset == 2u) {
        cooling_with_height = select(1.0 - height * 0.42, 1.0 - height * 0.78, volume_type == 1u);
      } else if (preset == 3u) {
        cooling_with_height = 1.0 - height * 0.16;
      }
      temp = mix(temp, advect_from.y, clamp(0.24 + max(buoyancy, 0.0) * 0.22, 0.0, 0.5));
      temp = mix(temp, upwind_temp, clamp(0.3 + diffusion * 0.2, 0.0, 0.7));
      temp = mix(temp, side_temp, 0.08);
      temp = mix(temp, carry.y, clamp(carry_mix * 0.5, 0.0, 0.32));
      var top_cooling = 1.0 - top_vent * 0.45;
      if (preset == 1u) {
        top_cooling = 1.0 - top_vent * 0.26;
      } else if (preset == 2u) {
        top_cooling = select(1.0 - top_vent * 0.16, 1.0 - top_vent * 0.68, volume_type == 1u);
      } else if (preset == 3u) {
        top_cooling = 1.0 - top_vent * 0.18;
      }
      temp *= temp_decay * cooling_with_height * top_cooling;
      let burnout = max(prev_heat - temp, 0.0);
      var burnout_to_smoke = 0.18;
      var embers_to_smoke = 0.018;
      if (preset == 1u) {
        burnout_to_smoke = 0.07;
        embers_to_smoke = 0.008;
      } else if (preset == 2u) {
        burnout_to_smoke = 0.28;
        embers_to_smoke = 0.03;
      } else if (preset == 4u) {
        burnout_to_smoke = 0.34;
        embers_to_smoke = 0.032;
      } else if (preset == 3u) {
        burnout_to_smoke = 0.025;
        embers_to_smoke = 0.004;
      }
      let smoke_prod = burnout * burnout_to_smoke * mix(0.25, select(1.15, 1.05, preset == 4u), height);
      let ember_smoke = max(temp - 0.22, 0.0) * embers_to_smoke * mix(0.1, 0.55, height);
      density = density * mix(0.985, 0.94, height) + smoke_prod + ember_smoke;
      if (preset == 2u) {
        let base_anchor = 1.0 - smoothstep(0.16, 0.42, height);
        let neck = 1.0 - smoothstep(0.32, 0.9, radial);
        let base_pulse = 0.86 + 0.22 * sin(t * 5.6 + localf.x * 0.31 + localf.z * 0.27 + height * 6.0);
        let top_break = 0.64 + 0.36 * sin(t * 9.8 + localf.x * 0.52 + localf.z * 0.44 + height * 13.0);
        let top_wobble = 0.72 + 0.28 * cos(t * 12.4 + localf.x * 0.67 - localf.z * 0.58 + height * 15.0);
        temp = max(temp, base_anchor * neck * base_pulse);
        temp *= mix(1.0, top_break * top_wobble, smoothstep(0.3, 0.95, height));
        density *= mix(1.02, 0.72, smoothstep(0.22, 0.9, height));
      } else if (preset == 4u) {
        let lower_bias = 1.0 - smoothstep(0.18, 0.62, height);
        temp *= mix(0.98, 0.44, smoothstep(0.08, 0.94, height));
        density = density * mix(0.98, 0.82, height) + burnout * 0.14;
        density += burst_afterglow * lower_bias * 0.03;
      }
      if (preset == 3u) {
        density *= mix(0.28, 0.12, height);
      }
    } else {
      temp = mix(temp, advect_from.y, clamp(0.12 + max(buoyancy, 0.0) * 0.1, 0.0, 0.24));
      temp = mix(temp, avg_temp, 0.08);
      temp = mix(temp, carry.y, clamp(carry_mix * 0.18, 0.0, 0.16)) * 0.82;
    }

    let erosion_noise = hash13(vec3<f32>(
      floor(localf.x * 0.7 + t * 1.5),
      floor(localf.y * 0.45),
      floor(localf.z * 0.7 - t * 1.1),
    ));
    var erosion = smoothstep(0.62, 0.98, erosion_noise) * mix(0.02, 0.14, height);
    if (preset == 1u) {
      erosion *= 0.35;
    } else if (preset == 3u) {
      erosion *= 0.18;
    }
    density *= 1.0 - erosion * select(1.0, 0.45, volume_type == 1u);
    temp *= 1.0 - erosion * 0.05;
  }

  let nx = max(1.0, f32(res.x));
  let nz = max(1.0, f32(res.z));
  let cx = nx * 0.5;
  let cz = nz * 0.5;
  let dx = f32(local.x) + 0.5 - cx;
  let dz = f32(local.z) + 0.5 - cz;
  let radius = sqrt(dx * dx + dz * dz);
  let radial_norm = radius / max(1.0, max(nx, nz) * 0.5);
  var source_height = 4u;
  if (preset == 2u) {
    source_height = select(6u, 7u, volume_type == 0u);
  } else if (preset == 3u) {
    source_height = 8u;
  } else if (preset == 4u) {
    source_height = 12u;
  } else if (preset == 1u) {
    source_height = 6u;
  }
  if (local.y < source_height) {
    let base_center = vec2<f32>(cx, cz);
    let jitter_a = vec2<f32>(sin(t * 1.9), cos(t * 1.4)) * vec2<f32>(1.3, 0.9);
    let jitter_b = vec2<f32>(cos(t * 1.2 + 0.7), sin(t * 1.7 + 1.1)) * vec2<f32>(1.0, 1.4);
    let jitter_c = vec2<f32>(sin(t * 2.3 + 2.0), sin(t * 1.5 + 0.3)) * vec2<f32>(0.8, 1.1);

    var source_shape = 0.0;
    if (preset == 1u) {
      let wick = source_box(localf, base_center + vec2<f32>(0.0, 0.0), vec2<f32>(0.22, 0.46), 0.26);
      let ember_a = source_lobe(localf, base_center + vec2<f32>(-0.22, 0.08) + jitter_a * 0.12, 0.42, vec2<f32>(0.32, 0.34));
      let ember_b = source_lobe(localf, base_center + vec2<f32>(0.18, -0.06) + jitter_b * 0.1, 0.36, vec2<f32>(0.28, 0.3));
      source_shape = max(wick, max(ember_a, ember_b));
    } else if (preset == 2u) {
      if (volume_type == 1u) {
        let log_a = source_box(localf, base_center + vec2<f32>(-0.9, -0.22), vec2<f32>(0.72, 0.18), 0.24);
        let log_b = source_box(localf, base_center + vec2<f32>(0.82, 0.24), vec2<f32>(0.64, 0.18), 0.24);
        let log_c = source_box(localf, base_center + vec2<f32>(0.08, -0.72), vec2<f32>(0.52, 0.17), 0.2);
        let hot_a = source_lobe(localf, base_center + vec2<f32>(-0.48, -0.16) + jitter_a * 0.12, 0.34, vec2<f32>(0.28, 0.24));
        let hot_b = source_lobe(localf, base_center + vec2<f32>(0.46, 0.18) + jitter_b * 0.12, 0.3, vec2<f32>(0.24, 0.28));
        let hot_c = source_lobe(localf, base_center + vec2<f32>(0.04, -0.56) + jitter_c * 0.1, 0.28, vec2<f32>(0.22, 0.32));
        source_shape = max(max(log_a, log_b), max(log_c, max(hot_a, max(hot_b, hot_c))));
      } else {
        let vent_a = source_box(localf, base_center + vec2<f32>(-0.7, -0.18), vec2<f32>(0.58, 0.34), 0.36);
        let vent_b = source_box(localf, base_center + vec2<f32>(0.62, 0.22), vec2<f32>(0.52, 0.34), 0.34);
        let vent_c = source_box(localf, base_center + vec2<f32>(0.0, -0.82), vec2<f32>(0.4, 0.28), 0.3);
        let vent_d = source_lobe(localf, base_center + vec2<f32>(0.2, 0.64), 0.46, vec2<f32>(0.42, 0.42));
        source_shape = max(max(vent_a, vent_b), max(vent_c, vent_d));
      }
    } else if (preset == 3u) {
      let jet_center = base_center - vec2<f32>(0.5, 0.5);
      let jet_band = hash13(vec3<f32>(floor(localf.x * 0.42 + 5.0), floor(localf.z * 0.42 + 9.0), floor(localf.y * 0.25)));
      let jet_cell = hash13(vec3<f32>(floor(localf.x * 0.78), floor(localf.y * 0.58), floor(localf.z * 0.78) + 19.0));
      let jitter = vec2<f32>(
        sin(t * 7.2 + localf.y * 0.33 + jet_band * 6.2831),
        cos(t * 5.9 + localf.y * 0.27 + jet_cell * 6.2831),
      ) * vec2<f32>(0.22, 0.1);
      let sway = vec2<f32>(
        sin(t * 2.7 + localf.y * 0.18) * 0.34,
        cos(t * 2.1 + localf.y * 0.14) * 0.14,
      ) + jitter;
      let slit = source_box(localf, jet_center + sway * 0.22, vec2<f32>(1.7, 0.2), 0.26);
      let core = source_box(localf, jet_center + sway * 0.08, vec2<f32>(1.08, 0.08), 0.1);
      let side_jitter = vec2<f32>(jitter.x, 0.0);
      let lobe_a = source_lobe(localf, jet_center + vec2<f32>(-0.54, 0.0) + jitter_a * vec2<f32>(0.16, 0.06) + side_jitter, 0.58, vec2<f32>(0.78, 0.22));
      let lobe_b = source_lobe(localf, jet_center + vec2<f32>(0.54, 0.0) - jitter_a * vec2<f32>(0.16, 0.06) - side_jitter, 0.58, vec2<f32>(0.78, 0.22));
      let lobe_c = source_lobe(localf, jet_center + vec2<f32>(0.0, 0.0) + jitter_c * vec2<f32>(0.08, 0.04), 0.44, vec2<f32>(0.5, 0.18));
      let shock = source_box(localf, jet_center + vec2<f32>(0.26 * sin(t * 7.6 + localf.y * 0.24), 0.0), vec2<f32>(0.72, 0.05), 0.09);
      let ragged = source_lobe(localf, jet_center + vec2<f32>(0.34 * sin(t * 10.2 + localf.y * 0.5), 0.0), 0.78, vec2<f32>(1.0, 0.16));
      source_shape = max(max(max(slit, core), max(lobe_a, lobe_b)), max(max(lobe_c, shock), ragged));
    } else if (preset == 4u) {
      let burst_center = base_center + vec2<f32>(sin(t * 0.41), cos(t * 0.33)) * 0.14;
      let core = source_lobe(localf, burst_center, 0.52, vec2<f32>(0.48, 0.48));
      let shoulder_a = source_lobe(localf, burst_center + vec2<f32>(-0.42, 0.1) + jitter_a * 0.12, 0.46, vec2<f32>(0.56, 0.42));
      let shoulder_b = source_lobe(localf, burst_center + vec2<f32>(0.34, -0.14) + jitter_b * 0.1, 0.42, vec2<f32>(0.48, 0.46));
      let shoulder_c = source_lobe(localf, burst_center + vec2<f32>(0.02, -0.48) + jitter_c * 0.1, 0.34, vec2<f32>(0.42, 0.58));
      source_shape = max(core, max(shoulder_a, max(shoulder_b, shoulder_c)));
    } else if (volume_type == 1u) {
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
    var breakup_mask = smoothstep(0.22, 0.82, source_shape + (breakup - 0.5) * 0.42);
    let source_band = hash13(vec3<f32>(floor(localf.x * 0.45 + 7.0), floor(localf.z * 0.45 + 13.0), f32(preset) * 11.0));
    let source_cell = hash13(vec3<f32>(floor(localf.x * 0.9), floor(localf.y * 0.7), floor(localf.z * 0.9) + f32(volume_type) * 17.0));
    let pulse_a = source_pulse(t, select(1.8, 3.6, volume_type == 1u), source_band * 6.2831);
    let pulse_b = source_pulse(t, select(0.9, 2.1, volume_type == 1u), source_cell * 6.2831 + f32(preset) * 0.7);
    let gust = source_pulse(t, 0.55, source_band * 3.1415 + f32(volume_type) * 0.8);
    let source_flicker = mix(0.72, 1.18, pulse_a) * mix(0.82, 1.12, pulse_b) * mix(0.9, 1.08, gust);
    let shape_shift = vec2<f32>(
      sin(t * (0.7 + f32(preset) * 0.09) + source_band * 4.5),
      cos(t * (0.58 + f32(volume_type) * 0.16) + source_cell * 5.2),
    );
    let shape_wave = source_lobe(
      localf,
      base_center + shape_shift * mix(0.12, 0.9, 1.0 - uvw.y),
      mix(0.26, 0.9, source_band),
      vec2<f32>(mix(0.3, 0.95, gust), mix(0.22, 0.8, pulse_b)),
    );
    var anim_shape = max(source_shape, shape_wave * mix(0.18, 0.5, 1.0 - uvw.y));
    if (preset == 2u && volume_type == 1u) {
      let taper = 1.0 - smoothstep(mix(0.88, 0.28, uvw.y), mix(1.06, 0.42, uvw.y), radial_norm * 2.0);
      anim_shape *= taper;
    } else if (preset == 4u) {
      let lift = smoothstep(0.18, 1.1, burst_time);
      let center3 = vec3<f32>(0.5, mix(0.18, 0.46, lift), 0.5);
      let shell3 = vec3<f32>(
        (uvw.x - center3.x) / mix(0.14, 0.3, burst_flash + burst_smoke * 0.18),
        (uvw.y - center3.y) / mix(0.1, 0.22, burst_flash + burst_afterglow * 0.8),
        (uvw.z - center3.z) / mix(0.14, 0.3, burst_flash + burst_smoke * 0.18),
      );
      let burst3 = max(0.0, 1.0 - length(shell3));
      let crown_center = vec3<f32>(0.5, mix(0.28, 0.74, lift), 0.5);
      let crown = max(0.0, 1.0 - length(vec3<f32>(
        (uvw.x - crown_center.x) / mix(0.22, 0.42, burst_smoke + burst_afterglow * 0.26),
        (uvw.y - crown_center.y) / mix(0.14, 0.2, burst_smoke + burst_rise * 0.24),
        (uvw.z - crown_center.z) / mix(0.22, 0.42, burst_smoke + burst_afterglow * 0.26),
      )));
      let stem = max(
        0.0,
        1.0 - length(vec3<f32>(
          radial_norm * 2.35,
          (uvw.y - mix(0.14, 0.34, lift)) * 3.2,
          0.0,
        )),
      ) * smoothstep(0.08, 0.74, uvw.y);
      let hollow = (1.0 - smoothstep(0.1, 0.28, radial_norm)) * (1.0 - smoothstep(0.06, 0.3, uvw.y));
      anim_shape = max(burst3 * mix(1.0, 0.28, burst_afterglow), max(crown * burst_afterglow, stem * burst_rise * 0.62)) * (1.0 - hollow * 0.58);
    } else if (preset == 3u) {
      let ragged_noise = hash13(vec3<f32>(floor(localf.x * 0.55 + t * 3.4), floor(localf.y * 0.4), floor(localf.z * 0.55 - t * 2.7)));
      let chunk_noise = hash13(vec3<f32>(floor(localf.x * 0.32), floor(localf.y * 0.24 + t * 1.4), floor(localf.z * 0.32)));
      let side_bite = smoothstep(0.62, 0.98, radial_norm) * mix(0.18, 0.54, ragged_noise);
      let tail_break = smoothstep(0.28, 0.96, uvw.y) * mix(0.76, 1.0, chunk_noise);
      anim_shape *= 1.0 - side_bite * tail_break;
      breakup_mask *= mix(0.72, 1.0, ragged_noise) * mix(0.78, 1.0, chunk_noise);
    }
    var vertical_scale = 4.5;
    if (preset == 1u) {
      vertical_scale = 6.2;
    }
    if (preset == 2u && volume_type == 0u) {
      vertical_scale = 7.5;
    } else if (preset == 2u) {
      vertical_scale = 7.2;
    } else if (preset == 3u) {
      vertical_scale = 8.0;
    } else if (preset == 4u) {
      vertical_scale = 12.4;
    }
    var vertical = 1.0 - f32(local.y) / vertical_scale;
    if (preset == 4u) {
      let shell = smoothstep(0.02, 0.32, burst_time) * (1.0 - smoothstep(0.34, 0.95, radial_norm));
      let lift = smoothstep(0.18, 1.1, burst_time);
      let lower_core = max(0.0, 1.0 - length(vec2<f32>(radial_norm * 1.8, (uvw.y - 0.16) * 3.8)));
      let lifted_cap = max(0.0, 1.0 - length(vec2<f32>(radial_norm * 0.94, (uvw.y - mix(0.28, 0.72, lift)) * 1.54)));
      let lower_hole = (1.0 - smoothstep(0.1, 0.28, radial_norm)) * (1.0 - smoothstep(0.06, 0.28, uvw.y));
      vertical = max(mix(lower_core, lifted_cap, mix(shell, lift, 0.54)), lifted_cap * burst_rise * 1.08) * (1.0 - lower_hole * 0.46);
    }
    let pulse = 0.72 + 0.28 * sin(
      params.elapsed * select(3.1, 5.4, volume_type == 1u) +
      f32(preset) * 0.9 +
      localf.x * 0.42 +
      localf.z * 0.3
    );
    var source_pulse_scale = 1.0;
    if (preset == 4u) {
      source_pulse_scale = burst_flash * 1.28 + burst_smoke * 0.62 + burst_afterglow * 0.22;
    } else if (preset == 3u) {
      source_pulse_scale = (0.78 + 0.46 * pulse_a) * (0.82 + 0.34 * pulse_b) * (0.84 + 0.24 * sin(t * 10.4 + localf.y * 0.34));
    }
    let plume_lane = smoothstep(0.18, 0.82, anim_shape);
    var source_strength = max(0.0, breakup_mask * plume_lane * vertical * pulse * source_flicker * source_pulse_scale) * intensity;
    if (preset == 4u) {
      let lower_bias = 1.0 - smoothstep(0.22, 0.72, uvw.y);
      source_strength *= mix(1.02, 0.72, smoothstep(0.12, 0.84, uvw.y));
      source_strength += burst_flash * lower_bias * max(0.0, 1.0 - radial_norm * 2.0) * 0.16;
      source_strength *= 1.0 - (1.0 - smoothstep(0.1, 0.28, radial_norm)) * (1.0 - smoothstep(0.04, 0.24, uvw.y)) * 0.42;
    } else if (preset == 3u) {
      let core_focus = 1.0 - smoothstep(0.12, 0.58, radial_norm);
      let base_focus = 1.0 - smoothstep(0.16, 0.82, uvw.y);
      let sputter = 0.7 + 0.3 * hash13(vec3<f32>(floor(localf.x * 0.45 + t * 5.2), floor(localf.y * 0.2), floor(localf.z * 0.45 - t * 4.8)));
      source_strength *= mix(1.08, 1.76, core_focus) * mix(1.28, 0.72, uvw.y) * sputter;
      source_strength += core_focus * base_focus * (0.12 + 0.1 * pulse_a) * intensity;
    }
    var base_injection = select(p_data.smoke_inject, p_data.fire_inject, volume_type == 1u);
    if (preset == 4u) {
      base_injection = 0.02; // Special case for explosion
    }
    base_injection *= intensity;
    var source_velocity = vec2<f32>(
      sin(t * 1.3 + source_band * 6.2831),
      cos(t * 1.05 + source_cell * 6.2831),
    ) * mix(0.05, 0.18, 1.0 - uvw.y);
    if (preset == 1u) {
      source_velocity = vec2<f32>(
        sin(t * 2.6 + source_band * 4.2),
        cos(t * 2.2 + source_cell * 4.8),
      ) * mix(0.02, 0.1, 1.0 - uvw.y);
    } else if (preset == 2u) {
      source_velocity = vec2<f32>(
        sin(t * 1.1 + source_band * 3.8) + sin(t * 2.4 + localf.z * 0.15) * 0.4,
        cos(t * 0.95 + source_cell * 4.1) + cos(t * 2.0 + localf.x * 0.18) * 0.35,
      ) * mix(0.08, select(0.34, 0.24, volume_type == 1u), 1.0 - uvw.y);
    } else if (preset == 3u) {
      source_velocity = vec2<f32>(
        sin(t * 4.8 + source_band * 7.8) * 0.24 +
        sin(t * 9.4 + localf.y * 0.52 + source_cell * 3.1) * 0.1,
        cos(t * 3.9 + source_cell * 5.8) * 0.16 +
        sin(t * 7.1 + source_band * 5.6 + localf.y * 0.21) * 0.08,
      ) * mix(0.34, 0.92, 1.0 - uvw.y);
    } else if (preset == 4u) {
      let rel = normalize(vec2<f32>(dx, dz) + vec2<f32>(0.001));
      let roll = vec2<f32>(
        sin(t * 0.8 + source_band * 4.8) * 0.16,
        cos(t * 0.72 + source_cell * 4.4) * 0.16,
      );
      source_velocity = (rel * (burst_flash * 0.82 + burst_smoke * 0.16) + roll * (burst_smoke + 0.34)) * mix(0.72, 0.14, uvw.y);
    }
    velocity = mix(velocity, source_velocity, clamp(source_strength * 0.7, 0.0, 0.65));
    if (preset == 4u || volume_type == 1u) {
      let temp_boost = select(1.72, 3.35, preset == 4u);
      let smoke_seed = p_data.smoke_seed;
      density = max(density, smoke_seed + source_strength * mix(0.02, select(0.065, 0.06, preset == 4u), 1.0 - uvw.y));
      temp = max(temp, source_strength * temp_boost * mix(1.02, 1.42, pulse_a));
      if (preset == 2u) {
        let base_anchor = 1.0 - smoothstep(0.12, 0.3, uvw.y);
        let radial_anchor = 1.0 - smoothstep(0.18, 0.62, radial_norm);
        temp = max(temp, source_strength * (1.08 + base_anchor * 0.34) * radial_anchor);
      }
      if (preset == 3u) {
        density = min(density, 0.38 + source_strength * 0.34);
        density *= 0.94 + 0.12 * hash13(vec3<f32>(floor(localf.x * 0.5 + t * 1.8), floor(localf.y * 0.32), floor(localf.z * 0.5)));
      } else if (preset == 4u) {
        density = mix(density, density + burst_smoke * mix(0.04, 0.1, 1.0 - uvw.y), 0.46);
        let lower_bias = 1.0 - smoothstep(0.18, 0.54, uvw.y);
        temp = temp * mix(1.0, 0.74, burst_smoke) + burst_flash * 0.11 + lower_bias * burst_afterglow * 0.07;
      }
    } else {
      density = max(density, base_injection * mix(0.8, 1.2, pulse_b) + source_strength * mix(0.65, 0.95, 1.0 - uvw.y));
      density *= 1.0 - smoothstep(0.78, 1.0, radial_norm) * 0.18;
      if (preset == 4u) {
        let lower_hole = (1.0 - smoothstep(0.12, 0.3, radial_norm)) * (1.0 - smoothstep(0.06, 0.28, uvw.y));
        density *= 1.0 - lower_hole * 0.9;
      }
    }
  }

  var top_fade = 1.0 - smoothstep(0.85, 1.0, uvw.y) * 0.85;
  var radial_fade = 1.0 - smoothstep(0.82, 1.0, radial_norm) * 0.45;
  var temp_fade = 1.0 - smoothstep(0.78, 1.0, uvw.y) * 0.75;
  if (preset == 2u && volume_type == 0u) {
    top_fade = 1.0 - smoothstep(0.96, 1.18, uvw.y) * 0.18;
    radial_fade = 1.0 - smoothstep(0.86, 1.04, radial_norm) * 0.2;
  } else if (preset == 2u) {
    top_fade = 1.0 - smoothstep(0.92, 1.08, uvw.y) * 0.32;
    radial_fade = 1.0 - smoothstep(0.92, 1.12, radial_norm) * 0.18;
  } else if (preset == 3u) {
    top_fade = 1.0 - smoothstep(0.86, 1.02, uvw.y) * 0.3;
    radial_fade = 1.0 - smoothstep(0.98, 1.14, radial_norm) * 0.08;
    temp_fade = 1.0 - smoothstep(0.82, 1.0, uvw.y) * 0.4;
  } else if (preset == 4u) {
    let lift = smoothstep(0.18, 1.1, burst_time);
    let lower_cut = smoothstep(0.04, 0.18, uvw.y);
    let inner_hole = (1.0 - smoothstep(0.1, 0.28, radial_norm)) * (1.0 - smoothstep(0.04, 0.28, uvw.y));
    top_fade = 1.0 - smoothstep(0.9, 1.04, uvw.y) * mix(0.12, 0.34, burst_afterglow);
    radial_fade = 1.0 - smoothstep(mix(0.58, 0.8, lift), mix(0.9, 1.04, lift), radial_norm) * 0.22;
    temp_fade = lower_cut * (1.0 - smoothstep(0.68, 1.0, uvw.y) * 0.58);
    density *= 1.0 - inner_hole * 0.34;
  }

  density *= top_fade;
  density *= radial_fade;
  temp *= temp_fade;
  velocity *= top_fade * radial_fade;

  density = clamp(density, 0.0, 1.5);
  temp = clamp(temp, 0.0, 2.0);
  velocity = clamp(velocity, vec2<f32>(-2.0), vec2<f32>(2.0));
  textureStore(dst_field, vec3<i32>(gid), vec4<f32>(density, temp, velocity.x, velocity.y));
}
