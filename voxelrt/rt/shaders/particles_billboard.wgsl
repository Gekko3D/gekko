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
    num_lights: u32,
    pad1: u32,
    screen_size: vec2<f32>,
    pad2: vec2<u32>,
    ao_quality: vec4<f32>,
};

struct ParticleInstance {
    pos: vec3<f32>,
    size: f32,
    color: vec4<f32>,
    velocity: vec3<f32>,
    life: f32,
    max_life: f32,
    gravity: f32,
    drag: f32,
    sprite_index: u32,
    atlas_cols: u32,
    atlas_rows: u32,
    alpha_mode: u32,
    pad1: u32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> pool: array<ParticleInstance>;
@group(0) @binding(2) var<storage, read> alive_list: array<u32>;
@group(0) @binding(3) var atlas_tex: texture_2d<f32>;
@group(0) @binding(4) var atlas_sampler: sampler;

@group(1) @binding(0) var gbuf_depth: texture_2d<f32>;

struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) quad_uv: vec2<f32>,
    @location(2) world_pos: vec3<f32>,
    @location(3) life_pct: f32,
    @location(4) psize: f32,
    @location(5) @interpolate(flat) sprite_index: u32,
    @location(6) @interpolate(flat) atlas_cols: u32,
    @location(7) @interpolate(flat) atlas_rows: u32,
    @location(8) @interpolate(flat) alpha_mode: u32,
};

fn get_camera_right() -> vec3<f32> {
    return normalize(camera.inv_view[0].xyz);
}

fn get_camera_up() -> vec3<f32> {
    return normalize(camera.inv_view[1].xyz);
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let p_idx = alive_list[iid];
    let inst = pool[p_idx];
    
    var life_pct = inst.life / max(inst.max_life, 0.001);
    
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
    out.life_pct = life_pct;
    out.psize = inst.size;
    out.sprite_index = inst.sprite_index;
    out.atlas_cols = max(1u, inst.atlas_cols);
    out.atlas_rows = max(1u, inst.atlas_rows);
    out.alpha_mode = inst.alpha_mode;
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
    // Sprite Atlas Mapping (dynamic grid) with inset padding to avoid edge bleeding
    let col_w = 1.0 / f32(in.atlas_cols);
    let row_h = 1.0 / f32(in.atlas_rows);
    let sprite_x = f32(in.sprite_index % in.atlas_cols) * col_w;
    let sprite_y = f32(in.sprite_index / in.atlas_cols) * row_h;
    
    // Inset UVs by a tiny amount based on resolution fraction to prevent bleeding
    let padded_uv = in.quad_uv * 0.98 + 0.01;
    let sprite_uv = vec2<f32>(sprite_x, sprite_y) + padded_uv * vec2<f32>(col_w, row_h);
    let atlas_color = textureSample(atlas_tex, atlas_sampler, sprite_uv);
    var mask = atlas_color.a;
    if (in.alpha_mode == 1u) {
        mask = max(atlas_color.r, max(atlas_color.g, atlas_color.b));
    }

    let life_fade = smoothstep(0.0, 0.1, in.life_pct) * (1.0 - smoothstep(0.9, 1.0, in.life_pct));
    
    // Soft particle falloff (size-aware)
    let depth_diff = (t_scene + bias) - t_pixel;
    let soft_range = max(0.5, in.psize * 1.5);
    let soft_factor = clamp(depth_diff / soft_range, 0.0, 1.0);

    let alpha = clamp(in.color.a * mask * life_fade * soft_factor, 0.0, 1.0);
    if (alpha < 0.001) {
        discard;
    }

    // Use camera depth, not opaque scene depth behind the particle. Tying the
    // weight to t_scene makes particles near the ground or walls fade out when
    // their screen projection overlaps nearby opaque geometry.
    let depth_norm = clamp(t_pixel / 160.0, 0.0, 1.0);
    let k: f32 = 4.0;
    let weight = max(1e-3, alpha) * pow(1.0 - depth_norm, k);

    var out: FSOut;
    // Accumulate premultiplied color with weighted alpha, but store unweighted alpha in accum.a (for revealage)
    out.accum = vec4<f32>(in.color.rgb * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
