struct VertexInput {
    @location(0) position: vec3<f32>,
    // @location(1) color: vec4<f32>, // No longer used per-vertex

    // Instance attributes (Grouped for mat4 reconstruction)
    @location(2) inst_mat_col0: vec4<f32>,
    @location(3) inst_mat_col1: vec4<f32>,
    @location(4) inst_mat_col2: vec4<f32>,
    @location(5) inst_mat_col3: vec4<f32>,
    @location(6) inst_color: vec4<f32>,
    @location(7) inst_depth: vec4<f32>,
}

struct CameraUniform {
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

@group(0) @binding(0) var<uniform> camera: CameraUniform;

// Group 1: Scene depth from the G-buffer. Gizmos are rendered in an overlay
// pass, but still discard behind opaque scene geometry so dense debug views do
// not become unreadable through walls and floors.
@group(1) @binding(0) var depth_tex: texture_2d<f32>;

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) dist: f32,
    @location(2) depth_mode: f32,
}

const GIZMO_DEPTH_MODE_SCENE_OCCLUDED: f32 = 0.0;
const GIZMO_DEPTH_MODE_ALWAYS_VISIBLE: f32 = 1.0;

fn camera_far_t() -> f32 {
    return max(camera.distance_limits.y, 1.0);
}

fn visible_scene_depth(t: f32) -> bool {
    return t > 0.0 && t < camera_far_t();
}

@vertex
fn vs_main(in: VertexInput) -> VertexOutput {
    let instance_matrix = mat4x4<f32>(
        in.inst_mat_col0,
        in.inst_mat_col1,
        in.inst_mat_col2,
        in.inst_mat_col3
    );

    var out: VertexOutput;
    let world_pos = instance_matrix * vec4<f32>(in.position, 1.0);
    // CameraState currently produces GL-style clip Z in [-w, +w].
    // Raster pipelines in WGSL/WebGPU expect clip Z in [0, +w], so convert here
    // before line rasterization and fragment-stage depth comparisons.
    var clip_pos = camera.view_proj * world_pos;
    clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;
    out.position = clip_pos;
    out.color = in.inst_color;
    out.depth_mode = in.inst_depth.x;
    // Calculate distance from camera for depth testing
    out.dist = distance(camera.cam_pos.xyz, world_pos.xyz);
    return out;
}

@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    let dims = textureDimensions(depth_tex);
    let max_pix = vec2<i32>(i32(dims.x) - 1, i32(dims.y) - 1);
    let pix = clamp(vec2<i32>(in.position.xy), vec2<i32>(0, 0), max_pix);
    let scene_t = textureLoad(depth_tex, pix, 0).r;
    let slack = max(camera_far_t() * 0.00005, 0.05);
    if (in.depth_mode < GIZMO_DEPTH_MODE_ALWAYS_VISIBLE && visible_scene_depth(scene_t) && in.dist > scene_t + slack) {
        discard;
    }
    return in.color;
}
