// Procedural Skybox Generation Shader

struct SkyboxLayer {
    color_a: vec4<f32>,     // xyz = color, w = threshold
    color_b: vec4<f32>,     // xyz = color, w = opacity
    offset: vec4<f32>,      // xyz = offset, w = scale
    persistence: f32,
    lacunarity: f32,
    seed: i32,
    octaves: i32,
    blend_mode: u32,
    invert: u32,
    pad1: u32,
    pad2: u32,
};

struct SkyboxUniforms {
    layer_count: u32,
    sun_dir: vec4<f32>, // w = intensity
};

@group(0) @binding(0) var<uniform> uniforms: SkyboxUniforms;
@group(0) @binding(1) var<storage, read> layers: array<SkyboxLayer>;
@group(0) @binding(2) var out_tex: texture_storage_2d<rgba16float, write>;

// --- NOISE FUNCTIONS ---

fn hash(p: vec3<f32>) -> f32 {
    let p3 = fract(p * 0.1031);
    var p3_2 = p3 + dot(p3, p3.yzx + 33.33);
    return fract((p3_2.x + p3_2.y) * p3_2.z);
}

fn hash33(p: vec3<f32>) -> vec3<f32> {
    var p3 = fract(p * vec3<f32>(.1031, .1030, .0973));
    p3 += dot(p3, p3.yxz + 33.33);
    return fract((p3.xxy + p3.yxx) * p3.zyx);
}

fn noise3(p: vec3<f32>) -> f32 {
    let i = floor(p);
    let f = fract(p);
    let u = f * f * (3.0 - 2.0 * f);

    return mix(mix(mix(hash(i + vec3<f32>(0.0, 0.0, 0.0)), 
                       hash(i + vec3<f32>(1.0, 0.0, 0.0)), u.x),
                   mix(hash(i + vec3<f32>(0.0, 1.0, 0.0)), 
                       hash(i + vec3<f32>(1.0, 1.0, 0.0)), u.x), u.y),
               mix(mix(hash(i + vec3<f32>(0.0, 0.0, 1.0)), 
                       hash(i + vec3<f32>(1.0, 0.0, 1.0)), u.x),
                   mix(hash(i + vec3<f32>(0.0, 1.0, 1.0)), 
                       hash(i + vec3<f32>(1.0, 1.0, 1.0)), u.x), u.y), u.z);
}

fn fbm(p: vec3<f32>, octaves: i32, persistence: f32, lacunarity: f32) -> f32 {
    var val = 0.0;
    var amp = 1.0;
    var freq = 1.0;
    var max_amp = 0.0;
    
    var pos = p;
    for (var i = 0; i < octaves; i++) {
        val += noise3(pos * freq) * amp;
        max_amp += amp;
        freq *= lacunarity;
        amp *= persistence;
    }
    return val / max_amp;
}

// --- MAIN PASS ---

@compute @workgroup_size(8, 8)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let size = textureDimensions(out_tex);
    if (global_id.x >= size.x || global_id.y >= size.y) {
        return;
    }

    let uv = vec2<f32>(global_id.xy) / vec2<f32>(size);
    let PI = 3.14159265359;
    
    // Equirectangular to Direction
    let phi = uv.x * 2.0 * PI;
    let theta = uv.y * PI;
    let dx = sin(theta) * cos(phi);
    let dy = cos(theta);
    let dz = sin(theta) * sin(phi);
    let dir = vec3<f32>(dx, dy, dz);

    var final_color = vec3<f32>(0.0);

    for (var i: u32 = 0; i < uniforms.layer_count; i++) {
        let l = layers[i];
        
        // Sampling pos with animation offset
        let p = (dir + l.offset.xyz) * l.offset.w + f32(l.seed) * 0.137;
        
        var n = fbm(p, l.octaves, l.persistence, l.lacunarity);
        
        if (l.invert != 0u) {
            n = 1.0 - n;
        }

        var val = n;
        let threshold = l.color_a.w;
        if (val < threshold) {
            val = 0.0;
        } else if (threshold < 1.0) {
            val = (val - threshold) / (1.0 - threshold);
        }

        let layer_color = mix(l.color_a.xyz, l.color_b.xyz, val);
        let alpha = l.color_b.w * val;

        // Blend Modes
        if (l.blend_mode == 0u) { // Alpha
            final_color = mix(final_color, layer_color, alpha);
        } else if (l.blend_mode == 1u) { // Add
            final_color += layer_color * alpha;
        } else if (l.blend_mode == 2u) { // Multiply
            final_color = mix(final_color, final_color * layer_color, alpha);
        }
    }

    // Atmospheric scattering hint (very simple for now)
    // Add sun disk
    let sun_dot = max(0.0, dot(dir, uniforms.sun_dir.xyz));
    if (sun_dot > 0.999) {
        let sun_disk = smoothstep(0.999, 0.9995, sun_dot);
        final_color += vec3<f32>(1.0, 0.9, 0.8) * sun_disk * uniforms.sun_dir.w;
    }

    textureStore(out_tex, global_id.xy, vec4<f32>(final_color, 1.0));
}
