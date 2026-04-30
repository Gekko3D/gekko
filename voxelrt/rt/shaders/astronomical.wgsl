const KIND_STAR: u32 = 1u;
const KIND_GAS_GIANT: u32 = 3u;
const KIND_RING_OR_BELT: u32 = 5u;
const OCCLUSION_PRIORITY_SELECTED: u32 = 200u;

const STAR_CORE_MIN_PIXELS: f32 = 2.0;
const STAR_GLOW_MIN_PIXELS: f32 = 8.0;
const PLANET_DISC_MIN_PIXELS: f32 = 2.25;
const GAS_GIANT_DISC_MIN_PIXELS: f32 = 2.75;

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

struct AstronomicalParams {
  body_count: u32,
  pad0: u32,
  pad1: u32,
  pad2: u32,
};

struct AstronomicalRecord {
  direction_kind: vec4<f32>,
  angular: vec4<f32>,
  tint_emission: vec4<f32>,
  record_meta: vec4<u32>,
};

struct VSOut {
  @builtin(position) position: vec4<f32>,
  @location(0) uv: vec2<f32>,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(1) @binding(0) var<uniform> astronomical_params: AstronomicalParams;
@group(1) @binding(1) var<storage, read> astronomical_bodies: array<AstronomicalRecord>;
@group(2) @binding(0) var scene_depth: texture_2d<f32>;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn scene_depth_has_hit(depth: f32) -> bool {
  let far_t = max(camera.distance_limits.y, 1.0);
  let finite_limit = far_t - max(far_t * 1e-5, 1e-3);
  return depth > 0.0 && depth < finite_limit;
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

fn minimum_visual_radius(kind: u32, radians_per_pixel: f32) -> f32 {
  if (kind == KIND_STAR) {
    return STAR_CORE_MIN_PIXELS * radians_per_pixel;
  }
  if (kind == KIND_GAS_GIANT) {
    return GAS_GIANT_DISC_MIN_PIXELS * radians_per_pixel;
  }
  return PLANET_DISC_MIN_PIXELS * radians_per_pixel;
}

fn hash11(seed: u32) -> f32 {
  var n = seed ^ 0x9e3779b9u;
  n = (n ^ (n >> 16u)) * 0x7feb352du;
  n = (n ^ (n >> 15u)) * 0x846ca68bu;
  n = n ^ (n >> 16u);
  return f32(n & 0x00ffffffu) / f32(0x00ffffffu);
}

fn disc_sample_color(body: AstronomicalRecord, ray_dir: vec3<f32>, body_dir: vec3<f32>, angle: f32, radius: f32) -> vec4<f32> {
  let kind = u32(body.direction_kind.w + 0.5);
  let tint = body.tint_emission.rgb;
  let emission = max(body.tint_emission.w, 0.0);
  let phase = f32(body.record_meta.z) / 65535.0;
  let radial01 = saturate(angle / max(radius, 1e-6));
  let limb = smoothstep(1.0, 0.78, radial01);
  var color = tint;
  var alpha = smoothstep(1.0, 0.92, radial01);

  if (kind == KIND_GAS_GIANT) {
    let p = normalize(ray_dir - body_dir * dot(ray_dir, body_dir));
    let band_phase = hash11(body.record_meta.x) * 6.2831853;
    let bands = 0.5 + 0.5 * sin((p.y * 18.0) + band_phase);
    color = mix(tint * 0.72, min(tint * 1.32 + vec3<f32>(0.08, 0.06, 0.03), vec3<f32>(1.0)), bands);
  }

  if (kind == KIND_STAR) {
    let core = mix(tint, min(tint * (1.6 + emission * 0.45), vec3<f32>(1.0)), 0.65);
    return vec4<f32>(core, alpha);
  }

  let phase_light = mix(0.18, 1.0, saturate(phase));
  color = color * (phase_light * mix(0.68, 1.08, limb));
  return vec4<f32>(color, alpha);
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
fn fs_main(in: VSOut) -> @location(0) vec4<f32> {
  let dims = textureDimensions(scene_depth);
  let ipos = vec2<i32>(
    clamp(i32(in.position.x), 0, i32(dims.x) - 1),
    clamp(i32(in.position.y), 0, i32(dims.y) - 1),
  );
  let scene_t = textureLoad(scene_depth, ipos, 0).r;
  if (scene_depth_has_hit(scene_t)) {
    discard;
  }

  let uv = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let ray_dir = view_ray_from_uv(uv);
  var out_color = vec3<f32>(0.0);
  var out_alpha = 0.0;
  let count = min(astronomical_params.body_count, 256u);
  let radians_per_pixel = angular_radians_per_pixel();

  for (var i: u32 = 0u; i < count; i = i + 1u) {
    let body = astronomical_bodies[i];
    let kind = u32(body.direction_kind.w + 0.5);
    let body_dir = normalize(body.direction_kind.xyz);
    let cos_angle = clamp(dot(ray_dir, body_dir), -1.0, 1.0);
    let angle = acos(cos_angle);
    let physical_radius = max(body.angular.x, 0.0);
    let radius = max(physical_radius, minimum_visual_radius(kind, radians_per_pixel));
    var glow_radius = max(body.angular.y, radius);
    if (kind == KIND_STAR) {
      glow_radius = max(glow_radius, STAR_GLOW_MIN_PIXELS * radians_per_pixel);
    }
    let ring_inner = max(body.angular.z, 0.0);
    let ring_outer = max(body.angular.w, 0.0);

    var sample = vec4<f32>(0.0);
    if (kind == KIND_RING_OR_BELT && ring_outer > ring_inner && angle >= ring_inner && angle <= ring_outer) {
      let edge = min(angle - ring_inner, ring_outer - angle) / max(ring_outer - ring_inner, 1e-6);
      let alpha = smoothstep(0.0, 0.12, edge) * 0.32;
      sample = vec4<f32>(body.tint_emission.rgb, alpha);
    } else if (radius > 0.0 && angle <= radius) {
      sample = disc_sample_color(body, ray_dir, body_dir, angle, radius);
    } else if (kind == KIND_STAR && glow_radius > radius && angle <= glow_radius) {
      let glow01 = saturate((angle - radius) / max(glow_radius - radius, 1e-6));
      let alpha = pow(1.0 - glow01, 2.4) * (0.18 + 0.14 * max(body.tint_emission.w, 0.0));
      sample = vec4<f32>(body.tint_emission.rgb * (1.0 + body.tint_emission.w * 0.25), alpha);
    }

    if (sample.a > 0.001) {
      let remaining = 1.0 - out_alpha;
      out_color += sample.rgb * sample.a * remaining;
      out_alpha += sample.a * remaining;
    }
  }

  if (out_alpha <= 0.001) {
    discard;
  }
  return vec4<f32>(out_color / max(out_alpha, 1e-6), saturate(out_alpha));
}
