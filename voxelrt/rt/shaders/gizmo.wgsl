struct VertexInput {
    @location(0) position: vec3<f32>,
    @location(1) color: vec4<f32>,
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

struct VertexOutput {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexInput) -> VertexOutput {
    var out: VertexOutput;
    out.position = camera.view_proj * vec4<f32>(in.position, 1.0);
    out.color = in.color;
    return out;
}

@fragment
fn fs_main(in: VertexOutput) -> @location(0) vec4<f32> {
    return in.color;
}
