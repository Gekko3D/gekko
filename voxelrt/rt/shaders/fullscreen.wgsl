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

@fragment
fn fs_main(@location(0) uv: vec2<f32>) -> @location(0) vec4<f32> {
    return textureSample(output_tex, output_sampler, uv);
}
