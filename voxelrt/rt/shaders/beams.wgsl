// Depth-aware camera-facing world-space beams.

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
    distance_limits: vec4<f32>,
};

struct BeamInstance {
    start_width: vec4<f32>,
    end_core_fraction: vec4<f32>,
    core_color: vec4<f32>,
    halo_color: vec4<f32>,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> beam_pool: array<BeamInstance>;
@group(1) @binding(0) var gbuf_depth: texture_2d<f32>;

struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) world_pos: vec3<f32>,
    @location(1) lateral: f32,
    @location(2) @interpolate(flat) core_fraction: f32,
    @location(3) @interpolate(flat) core_color: vec4<f32>,
    @location(4) @interpolate(flat) halo_color: vec4<f32>,
};

fn raster_clip_pos(world_pos: vec3<f32>) -> vec4<f32> {
    var clip_pos = camera.view_proj * vec4<f32>(world_pos, 1.0);
    clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;
    return clip_pos;
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let beam = beam_pool[iid];
    let start = beam.start_width.xyz;
    let end = beam.end_core_fraction.xyz;
    let axis = normalize(end - start);
    let midpoint = (start + end) * 0.5;
    var to_camera = camera.cam_pos.xyz - midpoint;
    if (dot(to_camera, to_camera) < 1e-6) {
        to_camera = camera.inv_view[2].xyz;
    }
    to_camera = normalize(to_camera);
    var side = cross(axis, to_camera);
    if (dot(side, side) < 1e-6) {
        side = camera.inv_view[0].xyz;
        if (abs(dot(normalize(side), axis)) > 0.95) {
            side = camera.inv_view[1].xyz;
        }
        side = side - axis * dot(side, axis);
    }
    side = normalize(side);

    var use_end = false;
    var lateral = -1.0;
    switch (vid % 6u) {
        case 0u: {}
        case 1u: { use_end = true; }
        case 2u: { use_end = true; lateral = 1.0; }
        case 3u: {}
        case 4u: { use_end = true; lateral = 1.0; }
        default: { lateral = 1.0; }
    }
    var center = start;
    if (use_end) {
        center = end;
    }
    let world_pos = center + side * lateral * beam.start_width.w * 0.5;

    var out: VSOut;
    out.position = raster_clip_pos(world_pos);
    out.world_pos = world_pos;
    out.lateral = lateral;
    out.core_fraction = beam.end_core_fraction.w;
    out.core_color = beam.core_color;
    out.halo_color = beam.halo_color;
    return out;
}

struct FSOut {
    @location(0) accum: vec4<f32>,
    @location(1) weight: f32,
};

@fragment
fn fs_main(in: VSOut) -> FSOut {
    let dim = textureDimensions(gbuf_depth);
    let pix = vec2<i32>(
        clamp(i32(in.position.x), 0, i32(dim.x) - 1),
        clamp(i32(in.position.y), 0, i32(dim.y) - 1),
    );
    let scene_distance = textureLoad(gbuf_depth, pix, 0).x;
    let beam_distance = length(in.world_pos - camera.cam_pos.xyz);
    let depth_fade = 1.0 - smoothstep(0.02, 0.45, beam_distance - scene_distance);
    if (depth_fade <= 1e-3) {
        discard;
    }

    let edge = 1.0 - smoothstep(0.72, 1.0, abs(in.lateral));
    let core = 1.0 - smoothstep(
        clamp(in.core_fraction, 0.02, 0.98),
        min(1.0, clamp(in.core_fraction, 0.02, 0.98) + 0.12),
        abs(in.lateral),
    );
    let halo_alpha = in.halo_color.a * edge;
    let core_alpha = in.core_color.a * core;
    let alpha = clamp(max(halo_alpha, core_alpha) * depth_fade, 0.0, 1.0);
    if (alpha < 0.01) {
        discard;
    }
    let color = mix(in.halo_color.rgb, in.core_color.rgb, core);
    let depth_norm = clamp(beam_distance / 160.0, 0.0, 1.0);
    let weight = max(1e-3, alpha * pow(1.0 - depth_norm, 4.0));

    var out: FSOut;
    out.accum = vec4<f32>(color * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
