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
    motion: vec4<f32>,
    render_params: vec4<f32>,
    pixelation: vec4<f32>,
};

const PI: f32 = 3.141592653589793;
const TAU: f32 = 6.283185307179586;
const MAX_BEAM_SEGMENTS: u32 = 24u;

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
    @location(5) longitudinal: f32,
    @location(6) @interpolate(flat) scroll_params: vec3<f32>,
    @location(7) @interpolate(flat) pixelation: vec3<f32>,
};

fn raster_clip_pos(world_pos: vec3<f32>) -> vec4<f32> {
    var clip_pos = camera.view_proj * vec4<f32>(world_pos, 1.0);
    clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;
    return clip_pos;
}

fn beam_basis(axis: vec3<f32>) -> mat2x3<f32> {
    var reference = vec3<f32>(0.0, 1.0, 0.0);
    if (abs(dot(reference, axis)) > 0.95) {
        reference = vec3<f32>(1.0, 0.0, 0.0);
    }
    let right = normalize(cross(axis, reference));
    let up = normalize(cross(axis, right));
    return mat2x3<f32>(right, up);
}

fn beam_center(beam: BeamInstance, basis: mat2x3<f32>, t: f32) -> vec3<f32> {
    let amplitude = max(beam.motion.x, 0.0) * sin(PI * t);
    let phase = TAU * (beam.motion.y * t + beam.motion.z * beam.render_params.w + beam.motion.w);
    let offset = basis[0] * sin(phase) + basis[1] * cos(phase);
    return mix(beam.start_width.xyz, beam.end_core_fraction.xyz, t) + offset * amplitude;
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let beam = beam_pool[iid];
    let start = beam.start_width.xyz;
    let end = beam.end_core_fraction.xyz;
    let axis = normalize(end - start);
    let segment_count = clamp(u32(beam.render_params.x + 0.5), 1u, MAX_BEAM_SEGMENTS);
    let segment_index = vid / 6u;
    let local_vid = vid % 6u;

    var out: VSOut;
    out.lateral = 0.0;
    out.core_fraction = beam.end_core_fraction.w;
    out.core_color = beam.core_color;
    out.halo_color = beam.halo_color;
    out.longitudinal = 0.0;
    out.scroll_params = vec3<f32>(
        beam.motion.y,
        -beam.render_params.y * beam.render_params.w + beam.motion.w,
        clamp(beam.render_params.z, 0.0, 1.0),
    );
    out.pixelation = max(beam.pixelation.xyz, vec3<f32>(0.0));
    out.world_pos = start;
    if (segment_index >= segment_count) {
        out.position = vec4<f32>(2.0, 2.0, 0.0, 1.0);
        return out;
    }

    let basis = beam_basis(axis);
    let t0 = f32(segment_index) / f32(segment_count);
    let t1 = f32(segment_index + 1u) / f32(segment_count);
    let center0 = beam_center(beam, basis, t0);
    let center1 = beam_center(beam, basis, t1);
    let segment_delta = center1 - center0;
    var segment_axis = axis;
    if (dot(segment_delta, segment_delta) > 1e-8) {
        segment_axis = normalize(segment_delta);
    }
    let midpoint = (center0 + center1) * 0.5;
    var to_camera = camera.cam_pos.xyz - midpoint;
    if (dot(to_camera, to_camera) < 1e-6) {
        to_camera = camera.inv_view[2].xyz;
    }
    to_camera = normalize(to_camera);
    var side = cross(segment_axis, to_camera);
    if (dot(side, side) < 1e-6) {
        side = camera.inv_view[0].xyz;
        if (abs(dot(normalize(side), segment_axis)) > 0.95) {
            side = camera.inv_view[1].xyz;
        }
        side = side - segment_axis * dot(side, segment_axis);
    }
    side = normalize(side);

    var use_segment_end = false;
    var lateral = -1.0;
    switch (local_vid) {
        case 0u: {}
        case 1u: { use_segment_end = true; }
        case 2u: { use_segment_end = true; lateral = 1.0; }
        case 3u: {}
        case 4u: { use_segment_end = true; lateral = 1.0; }
        default: { lateral = 1.0; }
    }
    var center = center0;
    var longitudinal = t0;
    if (use_segment_end) {
        center = center1;
        longitudinal = t1;
    }
    let world_pos = center + side * lateral * beam.start_width.w * 0.5;

    out.position = raster_clip_pos(world_pos);
    out.world_pos = world_pos;
    out.lateral = lateral;
    out.longitudinal = longitudinal;
    return out;
}

struct FSOut {
    @location(0) accum: vec4<f32>,
    @location(1) weight: f32,
};

fn quantize_repeat(value: f32, samples: f32) -> f32 {
    if (samples < 2.0) {
        return value;
    }
    let count = floor(samples + 0.5);
    return (floor(fract(value) * count) + 0.5) / count;
}

fn quantize_unit(value: f32, samples: f32) -> f32 {
    if (samples < 2.0) {
        return value;
    }
    let steps = floor(samples + 0.5) - 1.0;
    return floor(clamp(value, 0.0, 1.0) * steps + 0.5) / steps;
}

@fragment
fn fs_main(in: VSOut) -> FSOut {
    let screen_pixel_size = max(in.pixelation.z, 1.0);
    let snapped_screen = (floor(in.position.xy / screen_pixel_size) + 0.5) * screen_pixel_size;
    let sample_delta = select(
        vec2<f32>(0.0),
        snapped_screen - in.position.xy,
        in.pixelation.z >= 2.0,
    );
    let sampled_longitudinal = in.longitudinal +
        dpdx(in.longitudinal) * sample_delta.x +
        dpdy(in.longitudinal) * sample_delta.y;
    let sampled_lateral = in.lateral +
        dpdx(in.lateral) * sample_delta.x +
        dpdy(in.lateral) * sample_delta.y;
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

    let pattern_phase = quantize_repeat(
        in.scroll_params.x * sampled_longitudinal + in.scroll_params.y,
        in.pixelation.x,
    );
    let modulation = mix(
        1.0,
        0.5 + 0.5 * sin(TAU * pattern_phase),
        in.scroll_params.z,
    );
    let lateral = quantize_unit(sampled_lateral * 0.5 + 0.5, in.pixelation.y) * 2.0 - 1.0;
    let edge = 1.0 - smoothstep(0.72, 1.0, abs(lateral));
    let core = 1.0 - smoothstep(
        clamp(in.core_fraction, 0.02, 0.98),
        min(1.0, clamp(in.core_fraction, 0.02, 0.98) + 0.12),
        abs(lateral),
    );
    let halo_alpha = in.halo_color.a * edge;
    let core_alpha = in.core_color.a * core;
    let texture_alpha = select(1.0, modulation, in.pixelation.x >= 2.0);
    let alpha = clamp(max(halo_alpha, core_alpha) * depth_fade * texture_alpha, 0.0, 1.0);
    if (alpha < 0.01) {
        discard;
    }
    let color = mix(in.halo_color.rgb, in.core_color.rgb, core) * modulation;
    let depth_norm = clamp(beam_distance / 160.0, 0.0, 1.0);
    let weight = max(1e-3, alpha * pow(1.0 - depth_norm, 4.0));

    var out: FSOut;
    out.accum = vec4<f32>(color * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
