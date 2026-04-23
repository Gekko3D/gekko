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

struct PlanetParams {
  planet_count: u32,
  pad0: u32,
  pad1: u32,
  pad2: u32,
};

struct PlanetRecord {
  bounds: vec4<f32>,
  rotation: vec4<f32>,
  surface: vec4<f32>,
  noise: vec4<f32>,
  style: vec4<f32>,
  emission: vec4<f32>,
  bake_meta: vec4<u32>,
  band0: vec4<f32>,
  band1: vec4<f32>,
  band2: vec4<f32>,
  band3: vec4<f32>,
  band4: vec4<f32>,
  band5: vec4<f32>,
  atmosphere: vec4<f32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

struct FSOut {
  @location(0) color: vec4<f32>,
  @location(1) depth: f32,
};

struct PlanetSurfaceEval {
  signed_distance: f32,
  radius: f32,
  signed_height: f32,
  is_ocean: f32,
  dir_y: f32,
};

struct PlanetTraceHit {
  t: f32,
  radius: f32,
  signed_height: f32,
  is_ocean: f32,
  dir_y: f32,
};

struct CubeSphereFaceUV {
  face: i32,
  u: f32,
  v: f32,
};

struct PlanetDetailSettings {
  block_size: f32,
  height_steps: f32,
  near_weight: f32,
  mid_weight: f32,
};

struct BakedPlanetSurfaceSample {
  height: f32,
  normal_oct_x: f32,
  normal_oct_y: f32,
  material_band: f32,
};

struct PlanetSurfaceMaterial {
  base_color: vec3<f32>,
  normal_mix: f32,
  diffuse_scale: f32,
  ambient_scale: f32,
  spec_strength: f32,
  spec_power: f32,
  spec_tint_mix: f32,
  white_spec_mix: f32,
  rim_scale: f32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;
@group(1) @binding(0) var<uniform> planet_params: PlanetParams;
@group(1) @binding(1) var<storage, read> planets: array<PlanetRecord>;
@group(1) @binding(2) var<storage, read> planet_baked_surface: array<BakedPlanetSurfaceSample>;
@group(2) @binding(0) var scene_depth: texture_2d<f32>;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn max3f(v: vec3<f32>) -> f32 {
  return max(v.x, max(v.y, v.z));
}

fn compress_planet_highlights(color: vec3<f32>) -> vec3<f32> {
  return color / (vec3<f32>(1.0) + color);
}

fn quat_conjugate(q: vec4<f32>) -> vec4<f32> {
  return vec4<f32>(-q.xyz, q.w);
}

fn quat_rotate(q: vec4<f32>, v: vec3<f32>) -> vec3<f32> {
  let t = 2.0 * cross(q.xyz, v);
  return v + q.w * t + cross(q.xyz, t);
}

fn hash13(p: vec3<f32>) -> f32 {
  let p3 = fract(p * 0.1031);
  let p3a = p3 + dot(p3, p3.yzx + 33.33);
  return fract((p3a.x + p3a.y) * p3a.z);
}

fn fade_scalar(t: f32) -> f32 {
  return t * t * (3.0 - 2.0 * t);
}

fn lerp_scalar(a: f32, b: f32, t: f32) -> f32 {
  return a + (b - a) * t;
}

fn hashed_noise_3(x: i32, y: i32, z: i32, seed: u32) -> f32 {
  var n: u32 = bitcast<u32>(x) * 0x1f123bb5u ^ bitcast<u32>(y) * 0x5f356495u ^ bitcast<u32>(z) * 0x6c8e9cf5u ^ seed * 0x27d4eb2du;
  n = n ^ (n >> 15u);
  n = n * 0x2c1b3c6du;
  n = n ^ (n >> 12u);
  n = n * 0x297a2d39u;
  n = n ^ (n >> 15u);
  return f32(n & 0x00ffffffu) / f32(0x00ffffffu);
}

fn value_noise_3(p: vec3<f32>) -> f32 {
  return value_noise_3_seed(p, 0u);
}

fn value_noise_3_seed(p: vec3<f32>, seed: u32) -> f32 {
  let x0 = i32(floor(p.x));
  let y0 = i32(floor(p.y));
  let z0 = i32(floor(p.z));
  let x1 = x0 + 1;
  let y1 = y0 + 1;
  let z1 = z0 + 1;

  let tx = fade_scalar(p.x - f32(x0));
  let ty = fade_scalar(p.y - f32(y0));
  let tz = fade_scalar(p.z - f32(z0));

  let n000 = hashed_noise_3(x0, y0, z0, seed);
  let n100 = hashed_noise_3(x1, y0, z0, seed);
  let n010 = hashed_noise_3(x0, y1, z0, seed);
  let n110 = hashed_noise_3(x1, y1, z0, seed);
  let n001 = hashed_noise_3(x0, y0, z1, seed);
  let n101 = hashed_noise_3(x1, y0, z1, seed);
  let n011 = hashed_noise_3(x0, y1, z1, seed);
  let n111 = hashed_noise_3(x1, y1, z1, seed);

  let x00 = lerp_scalar(n000, n100, tx);
  let x10 = lerp_scalar(n010, n110, tx);
  let x01 = lerp_scalar(n001, n101, tx);
  let x11 = lerp_scalar(n011, n111, tx);
  let y0v = lerp_scalar(x00, x10, ty);
  let y1v = lerp_scalar(x01, x11, ty);
  return lerp_scalar(y0v, y1v, tz);
}

fn fbm3_seeded(p: vec3<f32>, octaves: i32, lacunarity: f32, gain: f32, seed: u32) -> f32 {
  var total = 0.0;
  var amplitude = 1.0;
  var frequency = 1.0;
  var weight = 0.0;
  for (var octave = 0; octave < octaves; octave = octave + 1) {
    total += value_noise_3_seed(p * frequency, seed + u32(octave) * 7919u) * amplitude;
    weight += amplitude;
    frequency *= lacunarity;
    amplitude *= gain;
  }
  if (weight <= 1e-6) {
    return 0.0;
  }
  return total / weight;
}

fn make_ray(uv: vec2<f32>) -> vec3<f32> {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = camera.inv_proj * clip;
  view = view / max(view.w, 1e-6);
  let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
  return normalize(world_target - camera.cam_pos.xyz);
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

fn intersect_sphere(origin: vec3<f32>, dir: vec3<f32>, center: vec3<f32>, radius: f32) -> vec2<f32> {
  let oc = origin - center;
  let b = dot(oc, dir);
  let c = dot(oc, oc) - radius * radius;
  let h = b * b - c;
  if (h < 0.0) {
    return vec2<f32>(camera_far_t(), -camera_far_t());
  }
  let s = sqrt(h);
  return vec2<f32>(-b - s, -b + s);
}

fn primary_light_dir(pos_ws: vec3<f32>) -> vec3<f32> {
  let count = min(camera.num_lights, arrayLength(&lights));
  if (count == 0u) {
    return normalize(vec3<f32>(0.4, 1.0, 0.2));
  }
  let l = lights[0];
  if (u32(l.params.z) == 1u) {
    return normalize(-l.direction.xyz);
  }
  return normalize(l.position.xyz - pos_ws);
}

fn primary_light_color() -> vec3<f32> {
  let count = min(camera.num_lights, arrayLength(&lights));
  if (count == 0u) {
    return vec3<f32>(1.0);
  }
  return lights[0].color.xyz * max(lights[0].color.w, 0.0);
}

fn hash12(p: vec2<f32>) -> f32 {
  let p3 = fract(vec3<f32>(p.x, p.y, p.x) * 0.1031);
  let q = p3 + dot(p3, p3.yzx + 33.33);
  return fract((q.x + q.y) * q.z);
}

fn ordered_dither_4x4(ipos: vec2<i32>) -> f32 {
  let x = u32(ipos.x) & 3u;
  let y = u32(ipos.y) & 3u;
  let index = y * 4u + x;
  var threshold = 0.0;
  switch (index) {
    case 0u: { threshold = 0.0; }
    case 1u: { threshold = 8.0; }
    case 2u: { threshold = 2.0; }
    case 3u: { threshold = 10.0; }
    case 4u: { threshold = 12.0; }
    case 5u: { threshold = 4.0; }
    case 6u: { threshold = 14.0; }
    case 7u: { threshold = 6.0; }
    case 8u: { threshold = 3.0; }
    case 9u: { threshold = 11.0; }
    case 10u: { threshold = 1.0; }
    case 11u: { threshold = 9.0; }
    case 12u: { threshold = 15.0; }
    case 13u: { threshold = 7.0; }
    case 14u: { threshold = 13.0; }
    default: { threshold = 5.0; }
  }
  return (threshold + 0.5) / 16.0;
}

fn cube_sphere_direction(face: i32, u: f32, v: f32) -> vec3<f32> {
  var dir = vec3<f32>(0.0, 0.0, 1.0);
  switch (face) {
    case 0: {
      dir = vec3<f32>(1.0, -v, -u);
    }
    case 1: {
      dir = vec3<f32>(-1.0, -v, u);
    }
    case 2: {
      dir = vec3<f32>(u, 1.0, v);
    }
    case 3: {
      dir = vec3<f32>(u, -1.0, -v);
    }
    case 4: {
      dir = vec3<f32>(u, -v, 1.0);
    }
    default: {
      dir = vec3<f32>(-u, -v, -1.0);
    }
  }
  return normalize(dir);
}

fn cube_sphere_face_uv(dir: vec3<f32>) -> CubeSphereFaceUV {
  let abs_dir = abs(dir);
  if (abs_dir.x >= abs_dir.y && abs_dir.x >= abs_dir.z) {
    let scale = max(abs_dir.x, 1e-5);
    if (dir.x >= 0.0) {
      return CubeSphereFaceUV(0, -dir.z / scale, -dir.y / scale);
    }
    return CubeSphereFaceUV(1, dir.z / scale, -dir.y / scale);
  }
  if (abs_dir.y >= abs_dir.x && abs_dir.y >= abs_dir.z) {
    let scale = max(abs_dir.y, 1e-5);
    if (dir.y >= 0.0) {
      return CubeSphereFaceUV(2, dir.x / scale, dir.z / scale);
    }
    return CubeSphereFaceUV(3, dir.x / scale, -dir.z / scale);
  }
  let scale = max(abs_dir.z, 1e-5);
  if (dir.z >= 0.0) {
    return CubeSphereFaceUV(4, dir.x / scale, -dir.y / scale);
  }
  return CubeSphereFaceUV(5, -dir.x / scale, -dir.y / scale);
}

fn quantize_cube_sphere_coord(coord: f32, step_size: f32) -> f32 {
  let clamped = clamp(coord, -1.0, 1.0);
  let shifted = clamped + 1.0;
  return clamp((floor(shifted / step_size) + 0.5) * step_size - 1.0, -1.0, 1.0);
}

fn quantized_planet_sample_dir(planet: PlanetRecord, dir_local: vec3<f32>, block_size: f32) -> vec3<f32> {
  let radius = max(planet.bounds.w, 1.0);
  let uv_step = clamp(block_size / radius, 1e-3, 2.0);
  let face_uv = cube_sphere_face_uv(dir_local);
  let snapped_u = quantize_cube_sphere_coord(face_uv.u, uv_step);
  let snapped_v = quantize_cube_sphere_coord(face_uv.v, uv_step);
  return cube_sphere_direction(face_uv.face, snapped_u, snapped_v);
}

fn planet_detail_settings(planet: PlanetRecord) -> PlanetDetailSettings {
  if (baked_planet_surface_available(planet)) {
    let baked_resolution = max(f32(planet.bake_meta.x), 2.0);
    let baked_uv_step = 2.0 / max(baked_resolution - 1.0, 1.0);
    return PlanetDetailSettings(
      max(planet.bounds.w * baked_uv_step, 0.5),
      max(planet.noise.y, 1.0),
      0.0,
      0.0,
    );
  }
  let altitude = max(length(camera.cam_pos.xyz - planet.bounds.xyz) - max(planet.bounds.w, planet.surface.x), 0.0);
  let far_block = max(planet.surface.w, 0.5);
  let mid_block = max(far_block * 0.5, 0.35);
  let near_block = max(far_block * 0.22, 0.18);
  let far_steps = max(planet.noise.y, 1.0);
  let mid_steps = min(far_steps * 2.0, 64.0);
  let near_steps = min(far_steps * 4.0, 96.0);
  let mid_alt_start = max(planet.surface.z * 4.0, planet.bounds.w * 0.045);
  let mid_alt_end = max(planet.surface.z * 20.0, planet.bounds.w * 0.16);
  let near_alt_start = max(planet.surface.z * 1.25, planet.bounds.w * 0.008);
  let near_alt_end = max(planet.surface.z * 6.0, planet.bounds.w * 0.04);
  let mid_weight = 1.0 - smoothstep(mid_alt_start, mid_alt_end, altitude);
  let near_weight = 1.0 - smoothstep(near_alt_start, near_alt_end, altitude);
  let block_after_mid = mix(far_block, mid_block, mid_weight);
  let block_size = mix(block_after_mid, near_block, near_weight);
  let steps_after_mid = mix(far_steps, mid_steps, mid_weight);
  let height_steps = mix(steps_after_mid, near_steps, near_weight);
  return PlanetDetailSettings(block_size, height_steps, near_weight, mid_weight);
}

fn baked_planet_surface_available(planet: PlanetRecord) -> bool {
  return planet.bake_meta.x > 1u && planet.bake_meta.z >= planet.bake_meta.x * planet.bake_meta.x * 6u;
}

fn baked_planet_surface_texel(planet: PlanetRecord, face: i32, x: i32, y: i32) -> BakedPlanetSurfaceSample {
  let resolution = max(i32(planet.bake_meta.x), 1);
  let clamped_face = clamp(face, 0, 5);
  let clamped_x = clamp(x, 0, resolution - 1);
  let clamped_y = clamp(y, 0, resolution - 1);
  let base = planet.bake_meta.y + u32(clamped_face * resolution * resolution + clamped_y * resolution + clamped_x);
  return planet_baked_surface[base];
}

fn decode_oct_normal(oct_xy: vec2<f32>) -> vec3<f32> {
  var v = vec3<f32>(oct_xy.xy, 1.0 - abs(oct_xy.x) - abs(oct_xy.y));
  if (v.z < 0.0) {
    let x = (1.0 - abs(v.y)) * select(-1.0, 1.0, v.x >= 0.0);
    let y = (1.0 - abs(v.x)) * select(-1.0, 1.0, v.y >= 0.0);
    v.x = x;
    v.y = y;
  }
  return normalize(v);
}

fn encode_oct_normal(normal: vec3<f32>) -> vec2<f32> {
  let denom = max(abs(normal.x) + abs(normal.y) + abs(normal.z), 1e-5);
  var n = normal / denom;
  if (n.z < 0.0) {
    let x = (1.0 - abs(n.y)) * select(-1.0, 1.0, n.x >= 0.0);
    let y = (1.0 - abs(n.x)) * select(-1.0, 1.0, n.y >= 0.0);
    n.x = x;
    n.y = y;
  }
  return vec2<f32>(clamp(n.x, -1.0, 1.0), clamp(n.y, -1.0, 1.0));
}

fn sample_planet_surface_baked(planet: PlanetRecord, sample_dir: vec3<f32>) -> BakedPlanetSurfaceSample {
  let face_uv = cube_sphere_face_uv(sample_dir);
  let resolution = max(f32(planet.bake_meta.x), 2.0);
  let texel_coord = (vec2<f32>(face_uv.u, face_uv.v) * 0.5 + vec2<f32>(0.5, 0.5)) * (resolution - 1.0);
  let x0 = i32(floor(texel_coord.x));
  let y0 = i32(floor(texel_coord.y));
  let x1 = min(x0 + 1, i32(resolution) - 1);
  let y1 = min(y0 + 1, i32(resolution) - 1);
  let tx = fract(texel_coord.x);
  let ty = fract(texel_coord.y);
  let s00 = baked_planet_surface_texel(planet, face_uv.face, x0, y0);
  let s10 = baked_planet_surface_texel(planet, face_uv.face, x1, y0);
  let s01 = baked_planet_surface_texel(planet, face_uv.face, x0, y1);
  let s11 = baked_planet_surface_texel(planet, face_uv.face, x1, y1);
  let h0 = mix(s00.height, s10.height, tx);
  let h1 = mix(s01.height, s11.height, tx);
  let height = clamp(mix(h0, h1, ty) * planet.surface.z, -planet.surface.z, planet.surface.z);
  let normal = normalize(
    decode_oct_normal(vec2<f32>(s00.normal_oct_x, s00.normal_oct_y)) * ((1.0 - tx) * (1.0 - ty)) +
    decode_oct_normal(vec2<f32>(s10.normal_oct_x, s10.normal_oct_y)) * (tx * (1.0 - ty)) +
    decode_oct_normal(vec2<f32>(s01.normal_oct_x, s01.normal_oct_y)) * ((1.0 - tx) * ty) +
    decode_oct_normal(vec2<f32>(s11.normal_oct_x, s11.normal_oct_y)) * (tx * ty)
  );
  let nearest_x = i32(round(texel_coord.x));
  let nearest_y = i32(round(texel_coord.y));
  let material = baked_planet_surface_texel(planet, face_uv.face, nearest_x, nearest_y).material_band;
  let oct_xy = encode_oct_normal(normal);
  return BakedPlanetSurfaceSample(height / max(planet.surface.z, 1e-4), oct_xy.x, oct_xy.y, material);
}

fn quantized_baked_planet_sample_dir(planet: PlanetRecord, dir_local: vec3<f32>) -> vec3<f32> {
  let face_uv = cube_sphere_face_uv(dir_local);
  let resolution = max(f32(planet.bake_meta.x), 2.0);
  let texel_coord = (vec2<f32>(face_uv.u, face_uv.v) * 0.5 + vec2<f32>(0.5, 0.5)) * (resolution - 1.0);
  let snapped_x = clamp(i32(round(texel_coord.x)), 0, i32(resolution) - 1);
  let snapped_y = clamp(i32(round(texel_coord.y)), 0, i32(resolution) - 1);
  let snapped_u = mix(-1.0, 1.0, f32(snapped_x) / max(resolution - 1.0, 1.0));
  let snapped_v = mix(-1.0, 1.0, f32(snapped_y) / max(resolution - 1.0, 1.0));
  return cube_sphere_direction(face_uv.face, snapped_u, snapped_v);
}

fn sample_planet_surface_baked_nearest(planet: PlanetRecord, sample_dir: vec3<f32>) -> BakedPlanetSurfaceSample {
  let face_uv = cube_sphere_face_uv(sample_dir);
  let resolution = max(f32(planet.bake_meta.x), 2.0);
  let texel_coord = (vec2<f32>(face_uv.u, face_uv.v) * 0.5 + vec2<f32>(0.5, 0.5)) * (resolution - 1.0);
  let x = clamp(i32(round(texel_coord.x)), 0, i32(resolution) - 1);
  let y = clamp(i32(round(texel_coord.y)), 0, i32(resolution) - 1);
  return baked_planet_surface_texel(planet, face_uv.face, x, y);
}

fn baked_surface_sample_height_world(planet: PlanetRecord, sample: BakedPlanetSurfaceSample) -> f32 {
  return sample.height * planet.surface.z;
}

fn baked_surface_sample_normal_local(sample: BakedPlanetSurfaceSample) -> vec3<f32> {
  return decode_oct_normal(vec2<f32>(sample.normal_oct_x, sample.normal_oct_y));
}

fn sample_planet_height_continuous(planet: PlanetRecord, sample_dir: vec3<f32>) -> f32 {
  if (baked_planet_surface_available(planet)) {
    let baked_dir = quantized_baked_planet_sample_dir(planet, sample_dir);
    return baked_surface_sample_height_world(planet, sample_planet_surface_baked_nearest(planet, baked_dir));
  }
  let detail_settings = planet_detail_settings(planet);
  let seed = u32(planet.noise.z);
  let amp = planet.surface.z;
  let continental = fbm3_seeded(sample_dir * planet.noise.x + vec3<f32>(1.7, -3.1, 2.3), 6, 2.0, 0.5, seed);
  let detail = fbm3_seeded(sample_dir * (planet.noise.x * 4.8) + vec3<f32>(-2.4, 5.1, -1.9), 4, 2.1, 0.52, seed + 17u);
  let ridge_raw = fbm3_seeded(sample_dir * (planet.noise.x * 2.35) + vec3<f32>(-4.2, 0.9, 5.6), 4, 1.95, 0.58, seed + 97u);
  let micro = fbm3_seeded(sample_dir * (planet.noise.x * 11.5) + vec3<f32>(7.4, -6.1, 2.8), 3, 2.2, 0.5, seed + 173u);
  let ridge = 1.0 - abs(ridge_raw * 2.0 - 1.0);
  let continental_shape = (continental * 2.0 - 1.0) * amp * 0.72;
  let ridge_shape = (ridge - 0.62) * amp * 0.34;
  let detail_shape = (detail * 2.0 - 1.0) * amp * mix(0.12, 0.22, detail_settings.mid_weight);
  let micro_shape = (micro * 2.0 - 1.0) * amp * 0.08 * detail_settings.near_weight;
  let sea_level_bias = amp * 0.14;
  return clamp(continental_shape + ridge_shape + detail_shape + micro_shape - sea_level_bias, -amp, amp);
}

fn quantize_planet_height(planet: PlanetRecord, height: f32, height_steps: f32) -> f32 {
  let amp = planet.surface.z;
  let steps = max(height_steps, 1.0);
  let step_size = (amp * 2.0) / steps;
  if (step_size <= 1e-5) {
    return height;
  }
  return floor((height + amp) / step_size + 0.5) * step_size - amp;
}

fn planet_polar_cap_metric(planet: PlanetRecord, signed_height: f32, dir_local: vec3<f32>) -> f32 {
  let amp = max(planet.surface.z, 1e-4);
  let seed = u32(planet.noise.z);
  let polar_noise = fbm3_seeded(dir_local * (planet.noise.x * 1.35) + vec3<f32>(6.7, -2.8, 4.1), 3, 2.0, 0.5, seed + 211u);
  let noise_offset = (polar_noise * 2.0 - 1.0) * 0.035;
  let height_boost = saturate(max(signed_height, 0.0) / amp) * 0.028;
  return saturate(abs(dir_local.y) + noise_offset + height_boost);
}

fn sample_planet_height(planet: PlanetRecord, dir_local: vec3<f32>) -> f32 {
  if (baked_planet_surface_available(planet)) {
    let baked_dir = quantized_baked_planet_sample_dir(planet, dir_local);
    let baked_sample = sample_planet_surface_baked_nearest(planet, baked_dir);
    return baked_surface_sample_height_world(planet, baked_sample);
  }
  let detail_settings = planet_detail_settings(planet);
  let sample_dir = quantized_planet_sample_dir(planet, dir_local, detail_settings.block_size);
  return quantize_planet_height(planet, sample_planet_height_continuous(planet, sample_dir), detail_settings.height_steps);
}

fn sample_planet_surface(planet: PlanetRecord, dir_world: vec3<f32>) -> vec4<f32> {
  let inv_rot = quat_conjugate(planet.rotation);
  let dir_local = normalize(quat_rotate(inv_rot, dir_world));
  let block_height = sample_planet_height(planet, dir_local);
  let signed_height = block_height;
  let base_radius = planet.bounds.w;
  let ocean_radius = planet.surface.x;
  let terrain_radius = base_radius + signed_height;
  let is_ocean = ocean_radius > 0.0 && signed_height < 0.0;
  let final_radius = select(terrain_radius, ocean_radius, is_ocean);
  return vec4<f32>(final_radius, signed_height, select(0.0, 1.0, is_ocean), dir_local.y);
}

fn evaluate_planet_surface(planet: PlanetRecord, pos_ws: vec3<f32>) -> PlanetSurfaceEval {
  let rel = pos_ws - planet.bounds.xyz;
  let dist = length(rel);
  if (dist <= 1e-5) {
    return PlanetSurfaceEval(planet.bounds.w, planet.bounds.w, 0.0, 0.0, 0.0);
  }
  let dir_world = rel / dist;
  let surface = sample_planet_surface(planet, dir_world);
  return PlanetSurfaceEval(dist - surface.x, surface.x, surface.y, surface.z, surface.w);
}

fn planet_surface_radius_local(planet: PlanetRecord, dir_local: vec3<f32>) -> f32 {
  let block_height = sample_planet_height(planet, dir_local);
  let signed_height = block_height;
  let base_radius = planet.bounds.w;
  let ocean_radius = planet.surface.x;
  let terrain_radius = base_radius + signed_height;
  let is_ocean = ocean_radius > 0.0 && signed_height < 0.0;
  return select(terrain_radius, ocean_radius, is_ocean);
}

fn planet_normal_sample_angle(planet: PlanetRecord, surface_radius: f32) -> f32 {
  let detail_settings = planet_detail_settings(planet);
  let block_size = detail_settings.block_size;
  let height_steps = max(detail_settings.height_steps, 1.0);
  let height_step_world = max((planet.surface.z * 2.0) / height_steps, 0.0);
  let sample_distance_world = max(block_size, height_step_world * 0.5);
  return clamp(sample_distance_world / max(surface_radius, 1.0), 1e-4, 0.08);
}

fn sample_planet_terrain_normal_world(planet: PlanetRecord, dir_world: vec3<f32>) -> vec3<f32> {
  let inv_rot = quat_conjugate(planet.rotation);
  let dir_local = normalize(quat_rotate(inv_rot, dir_world));
  if (baked_planet_surface_available(planet)) {
    let baked_dir = quantized_baked_planet_sample_dir(planet, dir_local);
    let baked_sample = sample_planet_surface_baked_nearest(planet, baked_dir);
    return normalize(quat_rotate(planet.rotation, baked_surface_sample_normal_local(baked_sample)));
  }
  let tangent_ref = select(vec3<f32>(0.0, 1.0, 0.0), vec3<f32>(1.0, 0.0, 0.0), abs(dir_local.y) > 0.92);
  let tangent_a = normalize(cross(tangent_ref, dir_local));
  let tangent_b = normalize(cross(dir_local, tangent_a));
  let radius_center = planet_surface_radius_local(planet, dir_local);
  let eps = planet_normal_sample_angle(planet, radius_center);

  let dir_a_pos = normalize(dir_local + tangent_a * eps);
  let dir_a_neg = normalize(dir_local - tangent_a * eps);
  let dir_b_pos = normalize(dir_local + tangent_b * eps);
  let dir_b_neg = normalize(dir_local - tangent_b * eps);
  let radius_a_pos = planet_surface_radius_local(planet, dir_a_pos);
  let radius_a_neg = planet_surface_radius_local(planet, dir_a_neg);
  let radius_b_pos = planet_surface_radius_local(planet, dir_b_pos);
  let radius_b_neg = planet_surface_radius_local(planet, dir_b_neg);

  let p_center = dir_local * radius_center;
  let p_a_pos = dir_a_pos * radius_a_pos;
  let p_a_neg = dir_a_neg * radius_a_neg;
  let p_b_pos = dir_b_pos * radius_b_pos;
  let p_b_neg = dir_b_neg * radius_b_neg;
  let normal_cross = cross(p_a_pos - p_a_neg, p_b_pos - p_b_neg);
  let fallback_cross = cross(p_a_pos - p_center, p_b_pos - p_center);
  var normal_local = normalize(select(fallback_cross, normal_cross, dot(normal_cross, normal_cross) > 1e-10));
  if (dot(normal_local, normal_local) <= 1e-10) {
    normal_local = dir_local;
  }
  if (dot(normal_local, dir_local) < 0.0) {
    normal_local = -normal_local;
  }
  return normalize(quat_rotate(planet.rotation, normal_local));
}

fn trace_planet_surface(planet: PlanetRecord, ray_origin: vec3<f32>, ray_dir: vec3<f32>, t_start: f32, t_end: f32) -> PlanetTraceHit {
  let start_t = max(t_start, 0.0);
  let end_t = max(t_end, start_t + 1.0);
  let detail_settings = planet_detail_settings(planet);
  let block_size = detail_settings.block_size;
  let trace_span = max(end_t - start_t, 1.0);
  let step_world = max(block_size * 0.35, 1.0);
  let step_count = clamp(i32(ceil(trace_span / step_world)), 48, 192);
  let refine_steps = 7;
  let step_size = (end_t - start_t) / f32(step_count);

  var prev_t = start_t;
  var prev_eval = evaluate_planet_surface(planet, ray_origin + ray_dir * prev_t);
  if (prev_eval.signed_distance <= 0.0) {
    return PlanetTraceHit(prev_t, prev_eval.radius, prev_eval.signed_height, prev_eval.is_ocean, prev_eval.dir_y);
  }

  for (var step = 1; step <= step_count; step = step + 1) {
    let t = min(start_t + f32(step) * step_size, end_t);
    let eval = evaluate_planet_surface(planet, ray_origin + ray_dir * t);
    if (eval.signed_distance <= 0.0) {
      var low_t = prev_t;
      var high_t = t;
      var high_eval = eval;
      for (var refine = 0; refine < refine_steps; refine = refine + 1) {
        let mid_t = (low_t + high_t) * 0.5;
        let mid_eval = evaluate_planet_surface(planet, ray_origin + ray_dir * mid_t);
        if (mid_eval.signed_distance <= 0.0) {
          high_t = mid_t;
          high_eval = mid_eval;
        } else {
          low_t = mid_t;
        }
      }
      return PlanetTraceHit(high_t, high_eval.radius, high_eval.signed_height, high_eval.is_ocean, high_eval.dir_y);
    }
    prev_t = t;
    prev_eval = eval;
  }

  return PlanetTraceHit(camera_far_t(), 0.0, 0.0, 0.0, 0.0);
}

fn planet_band_color(planet: PlanetRecord, band: i32) -> vec3<f32> {
  switch (band) {
    case 5: {
      return planet.band5.xyz;
    }
    case 4: {
      return planet.band4.xyz;
    }
    case 3: {
      return planet.band3.xyz;
    }
    case 2: {
      return planet.band2.xyz;
    }
    case 1: {
      return planet.band1.xyz;
    }
    default: {
      return planet.band0.xyz;
    }
  }
}

fn planet_surface_band(planet: PlanetRecord, signed_height: f32, dir_local: vec3<f32>, is_ocean: bool) -> i32 {
  let amp = max(planet.surface.z, 1e-4);
  if (is_ocean) {
    let ocean_depth = saturate(-signed_height / amp);
    return select(1, 0, ocean_depth > 0.58);
  }

  if (baked_planet_surface_available(planet)) {
    let baked_sample = sample_planet_surface_baked_nearest(planet, dir_local);
    return clamp(i32(round(baked_sample.material_band)), 0, 5);
  }

  let biome_mix = clamp(planet.noise.w, 0.0, 1.0);
  let snow_line = amp * lerp_scalar(0.9, 0.78, biome_mix);
  let polar_cap_start = lerp_scalar(0.992, 0.962, biome_mix);
  if (planet_polar_cap_metric(planet, signed_height, dir_local) > polar_cap_start || signed_height > snow_line) {
    return 5;
  }
  if (signed_height > amp * lerp_scalar(0.26, 0.14, biome_mix)) {
    return 4;
  }
  if (signed_height > amp * lerp_scalar(0.05, -0.04, biome_mix)) {
    return 3;
  }
  return 2;
}

fn planet_surface_material(planet: PlanetRecord, signed_height: f32, dir_local: vec3<f32>, is_ocean: bool, normal: vec3<f32>, view_dir: vec3<f32>) -> PlanetSurfaceMaterial {
  let band = planet_surface_band(planet, signed_height, dir_local, is_ocean);
  if (is_ocean) {
    let amp = max(planet.surface.z, 1e-4);
    let ocean_depth = saturate(-signed_height / amp);
    let ocean_base = mix(planet.band1.xyz, planet.band0.xyz, ocean_depth);
    let fresnel = pow(1.0 - saturate(dot(normal, view_dir)), 5.0);
    let horizon_tint = mix(ocean_base, vec3<f32>(1.0), 0.18 + 0.1 * (1.0 - ocean_depth));
    let sparkle = mix(0.18, 0.08, ocean_depth);
    return PlanetSurfaceMaterial(
      mix(ocean_base, horizon_tint, fresnel * 0.48),
      mix(0.78, 0.72, ocean_depth),
      mix(0.82, 0.72, ocean_depth),
      mix(0.94, 0.88, ocean_depth),
      mix(1.5, 1.7, ocean_depth),
      mix(78.0, 110.0, ocean_depth),
      0.72,
      0.34 + sparkle,
      0.3,
    );
  }

  if (band <= 1) {
    let lowland_base = mix(planet.band2.xyz, planet_band_color(planet, band), 0.7);
    return PlanetSurfaceMaterial(
      lowland_base,
      0.38,
      0.92,
      0.94,
      0.2,
      16.0,
      0.08,
      0.02,
      0.12,
    );
  }

  if (band == 2) {
    return PlanetSurfaceMaterial(
      planet.band2.xyz,
      0.34,
      1.0,
      1.02,
      0.38,
      22.0,
      0.16,
      0.04,
      0.16,
    );
  }
  if (band == 3) {
    return PlanetSurfaceMaterial(
      planet.band3.xyz,
      0.28,
      0.94,
      0.98,
      0.26,
      18.0,
      0.12,
      0.02,
      0.14,
    );
  }
  if (band == 4) {
    return PlanetSurfaceMaterial(
      planet.band4.xyz,
      0.22,
      0.9,
      0.92,
      0.22,
      14.0,
      0.1,
      0.02,
      0.12,
    );
  }
  return PlanetSurfaceMaterial(
    planet.band5.xyz,
    0.56,
    1.12,
    1.2,
    0.48,
    30.0,
    0.28,
    0.18,
    0.1,
  );
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
fn fs_main(in: VSOut) -> FSOut {
  let dims = textureDimensions(scene_depth);
  let ipos = vec2<i32>(
    clamp(i32(in.position.x), 0, i32(dims.x) - 1),
    clamp(i32(in.position.y), 0, i32(dims.y) - 1),
  );
  let uv = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let ray_dir = make_ray(uv);
  let scene_t = textureLoad(scene_depth, ipos, 0).r;

  let far_t = camera_far_t();
  var best_t = far_t;
  var best_color = vec3<f32>(0.0);
  var hit_any = false;

  for (var i: u32 = 0u; i < planet_params.planet_count; i = i + 1u) {
    let planet = planets[i];
    let center = planet.bounds.xyz;
    let detail_settings = planet_detail_settings(planet);
    let outer_radius = max(planet.bounds.w + planet.surface.z, planet.surface.x);
    let outer_hit = intersect_sphere(camera.cam_pos.xyz, ray_dir, center, outer_radius);
    if (outer_hit.x > outer_hit.y || outer_hit.y <= 0.0) {
      continue;
    }

    let candidate_t = max(outer_hit.x, 0.0);
    if (scene_depth_has_hit(scene_t) && candidate_t > scene_t + 0.05) {
      continue;
    }

    let surface_hit = trace_planet_surface(planet, camera.cam_pos.xyz, ray_dir, candidate_t, outer_hit.y);
    let t_hit = surface_hit.t;
    if (t_hit >= best_t || t_hit >= far_t) {
      continue;
    }
    if (scene_depth_has_hit(scene_t) && t_hit > scene_t + 0.05) {
      continue;
    }
    let hit_pos = camera.cam_pos.xyz + ray_dir * t_hit;
    let world_normal = normalize(hit_pos - center);
    let terrain_normal = sample_planet_terrain_normal_world(planet, world_normal);
    let inv_rot = quat_conjugate(planet.rotation);
    let local_normal = normalize(quat_rotate(inv_rot, world_normal));
    let block_local_normal = select(
      quantized_planet_sample_dir(planet, local_normal, detail_settings.block_size),
      quantized_baked_planet_sample_dir(planet, local_normal),
      baked_planet_surface_available(planet),
    );
    let block_normal = normalize(quat_rotate(planet.rotation, block_local_normal));
    let is_ocean = surface_hit.is_ocean > 0.5;
    let light_dir = primary_light_dir(hit_pos);
    let light_color = primary_light_color();
    let view_dir = normalize(camera.cam_pos.xyz - hit_pos);
    let surface_material = planet_surface_material(planet, surface_hit.signed_height, local_normal, is_ocean, terrain_normal, view_dir);
    let terrain_mix = clamp(surface_material.normal_mix + detail_settings.near_weight * 0.14, 0.0, 1.0);
    let shading_normal = normalize(mix(block_normal, terrain_normal, terrain_mix));
    let base_color = surface_material.base_color;
    let ndotl = max(dot(shading_normal, light_dir), 0.0);
    let diffuse = planet.style.y * ndotl * surface_material.diffuse_scale;
    let hemisphere_light = smoothstep(-0.08, 0.3, dot(world_normal, light_dir));
    let ambient_shadow = mix(0.08, 1.0, hemisphere_light);
    let ambient = (planet.style.x + dot(camera.ambient_color.xyz, vec3<f32>(0.3333))) * surface_material.ambient_scale * ambient_shadow;
    let spec = pow(max(dot(reflect(-light_dir, shading_normal), view_dir), 0.0), surface_material.spec_power) * planet.style.z * surface_material.spec_strength;
    let rim = pow(1.0 - saturate(dot(world_normal, view_dir)), 3.0) * planet.style.w;
    let atmosphere_mix = saturate((planet.surface.y - surface_hit.radius) / max(planet.atmosphere.w, 1.0));
    let spec_tint = mix(mix(base_color, light_color, surface_material.spec_tint_mix), vec3<f32>(1.0), surface_material.white_spec_mix);
    let rim_tint = mix(base_color, planet.atmosphere.xyz, 0.65);
    let rim_scale = surface_material.rim_scale;
    let emissive_strength = max(planet.emission.x, 0.0);
    let core_view = pow(saturate(dot(world_normal, view_dir) * 0.5 + 0.5), 1.6);
    let emissive_core = mix(base_color, planet.band5.xyz, 0.5 + 0.35 * core_view);
    let emissive_glow = mix(base_color, planet.atmosphere.xyz, 0.7);
    let emission = (emissive_core * (0.7 + 0.6 * core_view) + emissive_glow * (0.35 + 0.65 * rim)) * emissive_strength;
    let lit = base_color * (ambient + diffuse * light_color) + spec * spec_tint + rim_tint * rim * atmosphere_mix * rim_scale + emission;

    best_t = t_hit;
    best_color = clamp(compress_planet_highlights(max(lit, vec3<f32>(0.0))), vec3<f32>(0.0), vec3<f32>(1.0));
    hit_any = true;
  }

  if (!hit_any) {
    discard;
  }
  var out: FSOut;
  out.color = vec4<f32>(best_color, 1.0);
  out.depth = best_t;
  return out;
}
