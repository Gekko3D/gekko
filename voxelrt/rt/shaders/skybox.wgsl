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
    layer_type: u32,       // 0: Noise, 1: Stars, 2: Nebula
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

// Ridged fBM for nebula-like structures
fn ridged_fbm(p: vec3<f32>, octaves: i32, persistence: f32, lacunarity: f32) -> f32 {
    var val = 0.0;
    var amp = 1.0;
    var freq = 1.0;
    var weight = 1.0;
    
    for (var i = 0; i < octaves; i++) {
        var n = noise3(p * freq);
        n = 1.0 - abs(n * 2.0 - 1.0);
        n = n * n * weight;
        weight = n;
        val += n * amp;
        
        freq *= lacunarity;
        amp *= persistence;
    }
    return val;
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
        
        let p = (dir + l.offset.xyz) * l.offset.w + f32(l.seed) * 0.137;
        var n = 0.0;
        var layer_alpha = 0.0;
        var layer_c = vec3<f32>(0.0);

        if (l.layer_type == 1u) { // Stars
            let star_p = p * 100.0;
            let i_p = floor(star_p);
            let f_p = fract(star_p);
            
            let h = hash(i_p);
            if (h > l.color_a.w) { // Threshold for star density
                let size_h = hash(i_p + 0.1);
                let star_size = 0.1 + size_h * 0.4;
                let dist = length(f_p - 0.5);
                let star = smoothstep(star_size, 0.0, dist);
                
                layer_c = mix(l.color_a.xyz, l.color_b.xyz, size_h);
                layer_alpha = star * l.color_b.w * (0.5 + 0.5 * sin(f32(l.seed) + h * 10.0));
            }
        } else if (l.layer_type == 2u) { // Nebula
            n = ridged_fbm(p, l.octaves, l.persistence, l.lacunarity);
            let threshold = l.color_a.w;
            if (n > threshold) {
                n = (n - threshold) / (2.0 - threshold);
                layer_c = mix(l.color_a.xyz, l.color_b.xyz, n);
                layer_alpha = n * l.color_b.w;
            }
        } else { // Standard Noise
            n = fbm(p, l.octaves, l.persistence, l.lacunarity);
            if (l.invert != 0u) { n = 1.0 - n; }
            let threshold = l.color_a.w;
            if (n < threshold) {
                n = 0.0;
            } else if (threshold < 1.0) {
                n = (n - threshold) / (1.0 - threshold);
            }
            layer_c = mix(l.color_a.xyz, l.color_b.xyz, n);
            layer_alpha = n * l.color_b.w;
        }

        // Blend Modes
        if (l.blend_mode == 0u) { // Alpha
            final_color = mix(final_color, layer_c, layer_alpha);
        } else if (l.blend_mode == 1u) { // Add
            final_color += layer_c * layer_alpha;
        } else if (l.blend_mode == 2u) { // Multiply
            final_color = mix(final_color, final_color * layer_c, layer_alpha);
        }
    }

    // Improved Sun rendering
    let sun_dot = dot(dir, -uniforms.sun_dir.xyz);

    let intensity = uniforms.sun_dir.w;
    
    // Core lens/glow
    let sun_glow = pow(max(0.0, sun_dot), 1000.0) * 2.0;
    let atmosphere_glow = pow(max(0.0, sun_dot), 100.0) * 0.5;
    
    let sun_color = vec3<f32>(1.0, 0.9, 0.7) * intensity;
    final_color += sun_color * (sun_glow + atmosphere_glow);
    
    // Hard sun disk
    if (sun_dot > 0.9998) {
        final_color = mix(final_color, vec3<f32>(1.5, 1.4, 1.2) * intensity, smoothstep(0.9998, 0.9999, sun_dot));
    }

    textureStore(out_tex, global_id.xy, vec4<f32>(final_color, 1.0));
}

