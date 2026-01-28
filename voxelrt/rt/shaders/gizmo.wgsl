struct VertexInput {
    @location(0) position: vec3<f32>,
    // @location(1) color: vec4<f32>, // No longer used per-vertex

    // Instance attributes (Grouped for mat4 reconstruction)
    @location(2) inst_mat_col0: vec4<f32>,
    @location(3) inst_mat_col1: vec4<f32>,
    @location(4) inst_mat_col2: vec4<f32>,
    @location(5) inst_mat_col3: vec4<f32>,
    @location(6) inst_color: vec4<f32>,
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
};

@group(0) @binding(0) var<uniform> camera: CameraUniform;

// Group 1: Depth Texture for occlusion
@group(1) @binding(0) var depth_tex: texture_2d<f32>;

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) dist: f32,
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
    out.position = camera.view_proj * world_pos;
    out.color = in.inst_color;
    // Calculate distance from camera for depth testing
    out.dist = distance(camera.cam_pos.xyz, world_pos.xyz);
    return out;
}

@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    // Manual depth test against G-Buffer
    // in.position.xy are screen coordinates (pixels)
    let depth_val = textureLoad(depth_tex, vec2<i32>(in.position.xy), 0).r;
    
    // If the G-Buffer has a hit (depth < 60000) and it's closer than us, discard
    if (depth_val < 50000.0 && depth_val < in.dist - 0.1) {
        discard;
    }

    return in.color;
}
