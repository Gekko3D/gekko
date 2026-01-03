// resolve_transparency.wgsl
// Fullscreen resolve for weighted blended OIT.
// Inputs: opaque lit color, accumulated premultiplied transparent color sum, accumulated weight sum.
// Output: final color = opaque + accum.rgb / max(weight, eps)

struct VSOut {
  @builtin(position) position : vec4<f32>,
  @location(0) uv : vec2<f32>,
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

// Group 0: Inputs + sampler
//  - 0: opaque lit color (RGBA8Unorm)
//  - 1: accum color (RGBA16Float), premultiplied by alpha and weight
//  - 2: accum weight (R16Float), sum of alpha*weight
//  - 3: sampler
@group(0) @binding(0) var tOpaque : texture_2d<f32>;
@group(0) @binding(1) var tAccum  : texture_2d<f32>;
@group(0) @binding(2) var tWeight : texture_2d<f32>;
@group(0) @binding(3) var samp    : sampler;

fn saturate(v: vec3<f32>) -> vec3<f32> {
  return clamp(v, vec3<f32>(0.0), vec3<f32>(1.0));
}

@fragment
fn fs_main(@builtin(position) frag_pos: vec4<f32>, @location(0) uv: vec2<f32>) -> @location(0) vec4<f32> {
  // Fetch all inputs via textureLoad with integer pixel coords (no filtering)
  let dims = textureDimensions(tAccum);
  let ipos = vec2<i32>(
    clamp(i32(frag_pos.x), 0, i32(dims.x) - 1),
    clamp(i32(frag_pos.y), 0, i32(dims.y) - 1)
  );
  let copq = textureLoad(tOpaque,  ipos, 0).rgb;
  let acc4 = textureLoad(tAccum,   ipos, 0);
  let acc  = acc4.rgb;
  let accA = acc4.a;
  let w    = textureLoad(tWeight,  ipos, 0).r;

  // Use unweighted accumulated alpha (from accum.a) for revealage to reduce distance dependence
  let a_unweighted = clamp(accA, 0.0, 50.0);
  let w_scale: f32 = 2.0; // density control for revealage (tune 1.5..3.0)
  let T = exp(-w_scale * a_unweighted);
  let transp = acc / max(w, 1e-5);
  // Composite: attenuate background by T, add normalized transparent contribution
  let col = saturate(copq * T + transp);
  return vec4<f32>(col, 1.0);
}
