// sprites.wgsl
// Alpha-blended billboard sprites (UI or world-embedded).

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
};

struct SpriteInstance {
    pos: vec3<f32>,
    is_ui: u32,
    size: vec2<f32>,
    pad1: vec2<f32>,
    color: vec4<f32>,
    sprite_index: u32,
    atlas_cols: u32,
    atlas_rows: u32,
    pad2: u32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> sprite_pool: array<SpriteInstance>;
@group(0) @binding(2) var atlas_tex: texture_2d<f32>;
@group(0) @binding(3) var atlas_sampler: sampler;

@group(1) @binding(0) var gbuf_depth: texture_2d<f32>;

struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) quad_uv: vec2<f32>,
    @location(2) world_pos: vec3<f32>,
    @location(3) @interpolate(flat) sprite_index: u32,
    @location(4) @interpolate(flat) atlas_cols: u32,
    @location(5) @interpolate(flat) atlas_rows: u32,
    @location(6) @interpolate(flat) is_ui: u32,
};

fn get_camera_right() -> vec3<f32> {
    return normalize(camera.inv_view[0].xyz);
}

fn get_camera_up() -> vec3<f32> {
    return normalize(camera.inv_view[1].xyz);
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let inst = sprite_pool[iid];
    
    var corner: vec2<f32>;
    switch (vid % 6u) {
        case 0u: { corner = vec2<f32>(-0.5, -0.5); }
        case 1u: { corner = vec2<f32>( 0.5, -0.5); }
        case 2u: { corner = vec2<f32>( 0.5,  0.5); }
        case 3u: { corner = vec2<f32>(-0.5, -0.5); }
        case 4u: { corner = vec2<f32>( 0.5,  0.5); }
        default: { corner = vec2<f32>(-0.5,  0.5); }
    }

    var out: VSOut;
    out.color = inst.color;
    out.quad_uv = vec2<f32>(corner.x + 0.5, 0.5 - corner.y);
    out.sprite_index = inst.sprite_index;
    out.atlas_cols = max(1u, inst.atlas_cols);
    out.atlas_rows = max(1u, inst.atlas_rows);
    out.is_ui = inst.is_ui;

    if (inst.is_ui != 0u) {
        // UI Space: inst.pos.xy is screen pixels, inst.size is pixels
        // Convert to NDC [-1, 1]
        let ndc_pos = (inst.pos.xy + corner * inst.size) / camera.screen_size * 2.0 - 1.0;
        out.position = vec4<f32>(ndc_pos.x, -ndc_pos.y, 0.0, 1.0);
    } else {
        // World Space
        let r = normalize(get_camera_right());
        let u = normalize(get_camera_up());
        let world_pos = inst.pos + (r * corner.x * inst.size.x + u * corner.y * inst.size.y);
        out.position = camera.view_proj * vec4<f32>(world_pos, 1.0);
        out.world_pos = world_pos;
    }

    return out;
}

struct FSOut {
    @location(0) accum: vec4<f32>,
    @location(1) weight: f32,
};

@fragment
fn fs_main(in: VSOut) -> FSOut {
    // Sprite Atlas Mapping
    let col_w = 1.0 / f32(in.atlas_cols);
    let row_h = 1.0 / f32(in.atlas_rows);
    let sprite_x = f32(in.sprite_index % in.atlas_cols) * col_w;
    let sprite_y = f32(in.sprite_index / in.atlas_cols) * row_h;
    
    let sprite_uv = vec2<f32>(sprite_x, sprite_y) + in.quad_uv * vec2<f32>(col_w, row_h);
    let atlas_color = textureSample(atlas_tex, atlas_sampler, sprite_uv);
    
    let alpha = in.color.a * atlas_color.a;
    if (alpha < 0.01) {
        discard;
    }

    // Depth test only for world sprites
    if (in.is_ui == 0u) {
        let pix = vec2<i32>(in.position.xy);
        let t_scene = textureLoad(gbuf_depth, pix, 0).x;
        
        let view_ray = in.world_pos - camera.cam_pos.xyz;
        let t_pixel = length(view_ray);

        if (t_pixel > t_scene + 0.05) {
            discard;
        }
    }

    // WBOIT weighting
    let weight = max(1e-3, alpha); 

    var out: FSOut;
    out.accum = vec4<f32>(in.color.rgb * atlas_color.rgb * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
