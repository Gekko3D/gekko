struct VertexOutput {
    @builtin(position) pos: vec4<f32>,
    @location(0) uv: vec2<f32>,
};

@group(0) @binding(0)
var output_tex: texture_2d<f32>;
@group(0) @binding(1)
var output_sampler: sampler;

@vertex
fn vs_main(@builtin(vertex_index) vertex_index: u32) -> VertexOutput {
    // Fullscreen triangle (covers [-1, 1] range)
    // 0: (-1, -1), 1: (3, -1), 2: (-1, 3)
    var pos = array<vec2<f32>, 3>(
        vec2<f32>(-1.0, -1.0),
        vec2<f32>( 3.0, -1.0),
        vec2<f32>(-1.0,  3.0)
    );
    
    var output : VertexOutput;
    output.pos = vec4<f32>(pos[vertex_index], 0.0, 1.0);
    
    output.uv = vec2<f32>(
        (output.pos.x + 1.0) * 0.5,
        (1.0 - output.pos.y) * 0.5
    );
    
    return output;
}

fn aces_tonemap(x: vec3<f32>) -> vec3<f32> {
    let a = 2.51;
    let b = 0.03;
    let c = 2.43;
    let d = 0.59;
    let e = 0.14;
    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), vec3<f32>(0.0), vec3<f32>(1.0));
}

@fragment
fn fs_main(@location(0) uv: vec2<f32>) -> @location(0) vec4<f32> {
    var hdr_color = textureSample(output_tex, output_sampler, uv).rgb;
    
    // Tone mapping
    var ldr_color = aces_tonemap(hdr_color);
    
    // Gamma correction
    ldr_color = pow(ldr_color, vec3<f32>(1.0 / 2.2));
    
    return vec4<f32>(ldr_color, 1.0);
}
