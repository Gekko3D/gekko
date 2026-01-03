// particles_billboard.wgsl
// Alpha-blended billboard particles rendered after deferred lighting.

struct CameraData {
    view_proj: mat4x4<f32>,
    inv_view: mat4x4<f32>,
    inv_proj: mat4x4<f32>,
    cam_pos: vec4<f32>,
    light_pos: vec4<f32>,
    ambient_color: vec4<f32>,
    debug_mode: u32,
    render_mode: u32,
    pad1: u32,
    pad2: u32,
};

struct ParticleInstance {
    pos: vec3<f32>,
    size: f32,
    color: vec4<f32>,
    velocity: vec3<f32>,
    life_pct: f32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> instances: array<ParticleInstance>;
@group(1) @binding(0) var gbuf_depth: texture_2d<f32>;

struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) quad_uv: vec2<f32>,
    @location(2) world_pos: vec3<f32>,
    @location(3) life_pct: f32,
    @location(4) psize: f32,
};

fn get_camera_right() -> vec3<f32> {
    return vec3<f32>(camera.inv_view[0].x, camera.inv_view[1].x, camera.inv_view[2].x);
}

fn get_camera_up() -> vec3<f32> {
    return vec3<f32>(camera.inv_view[0].y, camera.inv_view[1].y, camera.inv_view[2].y);
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let inst = instances[iid];
    var corner: vec2<f32>;
    switch (vid % 6u) {
        case 0u: { corner = vec2<f32>(-0.5, -0.5); }
        case 1u: { corner = vec2<f32>( 0.5, -0.5); }
        case 2u: { corner = vec2<f32>( 0.5,  0.5); }
        case 3u: { corner = vec2<f32>(-0.5, -0.5); }
        case 4u: { corner = vec2<f32>( 0.5,  0.5); }
        default: { corner = vec2<f32>(-0.5,  0.5); }
    }

    var r = normalize(get_camera_right());
    var u = normalize(get_camera_up());
    
    // Velocity Alignment
    let speed = length(inst.velocity);
    if (speed > 4.0) {
        let forward = inst.velocity / speed;
        let view_vec = normalize(camera.cam_pos.xyz - inst.pos);
        let right_vec = cross(forward, view_vec);
        let r_len = length(right_vec);
        if (r_len > 0.1) {
            r = right_vec / r_len;
            u = forward;
        }
    }

    let world_pos = inst.pos + (r * corner.x + u * corner.y) * inst.size;
    var out: VSOut;
    out.position = camera.view_proj * vec4<f32>(world_pos, 1.0);
    out.color = inst.color;
    out.quad_uv = corner + vec2<f32>(0.5, 0.5);
    out.world_pos = world_pos;
    out.life_pct = inst.life_pct;
    out.psize = inst.size;
    return out;
}

struct FSOut {
    @location(0) accum: vec4<f32>,
    @location(1) weight: f32,
};

@fragment
fn fs_main(in: VSOut) -> FSOut {
    let dim = textureDimensions(gbuf_depth);
    let pix = vec2<i32>(clamp(i32(in.position.x), 0, i32(dim.x)-1), clamp(i32(in.position.y), 0, i32(dim.y)-1));
    
    // Depth test against G-Buffer
    let t_scene = textureLoad(gbuf_depth, pix, 0).x;
    
    // Reconstruct camera ray distance t_pixel for this fragment
    let view_ray = in.world_pos - camera.cam_pos.xyz;
    let t_pixel = length(view_ray);

    // Apply a small bias to prevent z-fighting with the very surface they might be spawning from
    let bias = 0.05;
    if (t_pixel > t_scene + bias) {
        discard;
    }

    // Alpha components
    let d = length(in.quad_uv - vec2<f32>(0.5, 0.5)) * 2.0;
    let mask = 1.0 - smoothstep(0.8, 1.0, d);
    let life_fade = smoothstep(0.0, 0.1, in.life_pct) * (1.0 - smoothstep(0.9, 1.0, in.life_pct));
    
    // Soft particle falloff (size-aware)
    let depth_diff = (t_scene + bias) - t_pixel;
    let soft_range = max(0.5, in.psize * 1.5);
    let soft_factor = clamp(depth_diff / soft_range, 0.0, 1.0);

    let alpha = clamp(in.color.a * mask * life_fade * soft_factor, 0.0, 1.0);
    if (alpha < 0.001) {
        discard;
    }

    // WBOIT weighting normalized by opaque limit (matches transparent overlay)
    let z_norm = clamp(t_pixel / max(t_scene, 1e-4), 0.0, 1.0);
    let k: f32 = 8.0;
    let weight = max(1e-3, alpha) * pow(1.0 - z_norm, k);

    var out: FSOut;
    // Accumulate premultiplied color with weighted alpha, but store unweighted alpha in accum.a (for revealage)
    out.accum = vec4<f32>(in.color.rgb * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
