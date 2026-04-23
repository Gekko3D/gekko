// resolve_transparency.wgsl
// Fullscreen resolve for weighted blended OIT.
// Inputs: opaque lit color, accumulated premultiplied transparent color sum, accumulated weight sum.
// Output: final color = opaque + accum.rgb / max(weight, eps)

struct VSOut {
  @builtin(position) position : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

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

@vertex
fn vs_main(@builtin(vertex_index) vi : u32) -> VSOut {
  var out : VSOut;
  let x = f32((vi << 1u) & 2u);
  let y = f32(vi & 2u);
  out.position = vec4<f32>(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);
  out.uv = vec2<f32>(x, y);
  return out;
}

// Group 0: Inputs
//  - 0: camera uniform
//  - 1: opaque lit color (RGBA8Unorm)
//  - 2: accum color (RGBA16Float), premultiplied by alpha and weight
//  - 3: accum weight (R16Float), sum of alpha*weight
//  - 4: volumetric color history (RGBA16Float, rgb=color, a=transmittance)
//  - 5: volumetric scene depth history (R16Float)
//  - 6: full-resolution scene depth
//  - 7: analytic planet depth
//  - 8: half-resolution CA color (rgb=radiance, a=transmittance)
//  - 9: half-resolution CA front depth
@group(0) @binding(0) var<uniform> camera : CameraData;
@group(0) @binding(1) var tOpaque : texture_2d<f32>;
@group(0) @binding(2) var tAccum  : texture_2d<f32>;
@group(0) @binding(3) var tWeight : texture_2d<f32>;
@group(0) @binding(4) var tVolume : texture_2d<f32>;
@group(0) @binding(5) var tVolumeDepth : texture_2d<f32>;
@group(0) @binding(6) var tSceneDepth : texture_2d<f32>;
@group(0) @binding(7) var tPlanetDepth : texture_2d<f32>;
@group(0) @binding(8) var tCAColor : texture_2d<f32>;
@group(0) @binding(9) var tCADepth : texture_2d<f32>;

fn camera_far_t() -> f32 {
  return max(camera.distance_limits.y, 1.0);
}

fn camera_far_half() -> f32 {
  return camera_far_t() * 0.5;
}

fn aces_tonemap(x: vec3<f32>) -> vec3<f32> {
  let a = 2.51;
  let b = 0.03;
  let c = 2.43;
  let d = 0.59;
  let e = 0.14;
  return clamp((x * (a * x + b)) / (x * (c * x + d) + e), vec3<f32>(0.0), vec3<f32>(1.0));
}

fn sanitize_scene_depth(depth: f32) -> f32 {
  let far_t = camera_far_t();
  if (depth > 0.0 && depth < far_t) {
    return depth;
  }
  return far_t;
}

fn sanitize_planet_depth(depth: f32) -> f32 {
  let far_t = camera_far_t();
  if (depth > 0.0 && depth < far_t) {
    return depth;
  }
  return far_t;
}

fn combined_opaque_depth(ipos: vec2<i32>) -> f32 {
  let scene_depth = sanitize_scene_depth(textureLoad(tSceneDepth, ipos, 0).r);
  let planet_depth = sanitize_planet_depth(textureLoad(tPlanetDepth, ipos, 0).r);
  return min(scene_depth, planet_depth);
}

fn depth_weight(current_depth: f32, sample_depth: f32) -> f32 {
  let far_half = camera_far_half();
  let current_finite = current_depth > 0.0 && current_depth < far_half;
  let sample_finite = sample_depth > 0.0 && sample_depth < far_half;
  if (!current_finite && sample_finite) {
    // Atmosphere/fog against open space should still upsample cleanly.
    return 1.0;
  }
  if (current_finite != sample_finite) {
    return 0.05;
  }
  if (!current_finite && !sample_finite) {
    return 1.0;
  }
  let delta = abs(current_depth - sample_depth);
  return exp(-delta * 0.04);
}

struct BilateralSample {
  color: vec4<f32>,
  depth: f32,
};

fn sample_halfres_bilateral(
  ipos: vec2<i32>,
  current_depth: f32,
  color_tex: texture_2d<f32>,
  depth_tex: texture_2d<f32>,
) -> BilateralSample {
  let full_dims = textureDimensions(tOpaque);
  let vol_dims = textureDimensions(color_tex);
  let uv = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(full_dims.x), f32(full_dims.y));
  let vol_pos = uv * vec2<f32>(f32(vol_dims.x), f32(vol_dims.y)) - vec2<f32>(0.5, 0.5);
  let base = vec2<i32>(floor(vol_pos));
  let frac = fract(vol_pos);

  var accum = vec4<f32>(0.0);
  var accum_depth = 0.0;
  var total_w = 0.0;
  for (var oy = 0; oy < 2; oy = oy + 1) {
    for (var ox = 0; ox < 2; ox = ox + 1) {
      let coord = vec2<i32>(
        clamp(base.x + ox, 0, i32(vol_dims.x) - 1),
        clamp(base.y + oy, 0, i32(vol_dims.y) - 1)
      );
      let spatial_x = mix(1.0 - frac.x, frac.x, f32(ox));
      let spatial_y = mix(1.0 - frac.y, frac.y, f32(oy));
      let spatial_w = spatial_x * spatial_y;
      let sample_depth = textureLoad(depth_tex, coord, 0).r;
      let w = spatial_w * depth_weight(current_depth, sample_depth);
      accum += textureLoad(color_tex, coord, 0) * w;
      accum_depth += sample_depth * w;
      total_w += w;
    }
  }
  if (total_w <= 1e-5) {
    let fallback = vec2<i32>(
      clamp(i32(round(vol_pos.x)), 0, i32(vol_dims.x) - 1),
      clamp(i32(round(vol_pos.y)), 0, i32(vol_dims.y) - 1)
    );
    return BilateralSample(textureLoad(color_tex, fallback, 0), textureLoad(depth_tex, fallback, 0).r);
  }
  return BilateralSample(accum / total_w, accum_depth / total_w);
}

fn composite_two_layers(base: vec3<f32>, front: vec4<f32>, back: vec4<f32>) -> vec3<f32> {
  return base * (front.a * back.a) + front.rgb + back.rgb * front.a;
}

@fragment
fn fs_main(@builtin(position) frag_pos: vec4<f32>, @location(0) uv: vec2<f32>) -> @location(0) vec4<f32> {
  // Fetch all inputs via textureLoad with integer pixel coords (no filtering)
  let far_half = camera_far_half();
  let dims = textureDimensions(tAccum);
  let ipos = vec2<i32>(
    clamp(i32(frag_pos.x), 0, i32(dims.x) - 1),
    clamp(i32(frag_pos.y), 0, i32(dims.y) - 1)
  );
  let copq = textureLoad(tOpaque,  ipos, 0).rgb;
  let current_depth = combined_opaque_depth(ipos);
  let acc4 = textureLoad(tAccum,   ipos, 0);
  let acc  = acc4.rgb;
  let accA = acc4.a;
  let w    = textureLoad(tWeight,  ipos, 0).r;
  let vol = sample_halfres_bilateral(ipos, current_depth, tVolume, tVolumeDepth);
  let ca = sample_halfres_bilateral(ipos, current_depth, tCAColor, tCADepth);
  let vol_valid = any(vol.color.rgb > vec3<f32>(1e-4)) || vol.color.a < 0.999;
  let ca_valid = ca.depth > 0.0 && ca.depth < far_half;
  var base = copq;
  if (vol_valid && ca_valid) {
    if (ca.depth <= vol.depth) {
      base = composite_two_layers(copq, ca.color, vol.color);
    } else {
      base = composite_two_layers(copq, vol.color, ca.color);
    }
  } else if (vol_valid) {
    base = copq * vol.color.a + vol.color.rgb;
  } else if (ca_valid) {
    base = copq * ca.color.a + ca.color.rgb;
  }

  // Use unweighted accumulated alpha (from accum.a) for revealage to reduce distance dependence
  let a_unweighted = clamp(accA, 0.0, 50.0);
  let w_scale: f32 = 2.0; // density control for revealage (tune 1.5..3.0)
  let T = exp(-w_scale * a_unweighted);
  let transp = acc / max(w, 1e-5);
  // Composite: attenuate background by T, add normalized transparent contribution
  var col = base * T + transp;
  
  // Tone mapping
  col = aces_tonemap(col);
  
  // Gamma correction
  col = pow(col, vec3<f32>(1.0 / 2.2));
  
  return vec4<f32>(col, 1.0);
}
