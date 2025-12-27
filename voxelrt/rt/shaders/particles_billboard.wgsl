 // particles_billboard.wgsl
// Alpha-blended billboard particles rendered after deferred lighting.
// Implements manual depth test against GBuffer depth and circular mask.

// Shared CameraData layout (matches gbuffer.wgsl)
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

// Particle instance layout
struct ParticleInstance {
    pos: vec3<f32>,
    size: f32,
    color: vec4<f32>,
};

// Bindings
// Group 0: camera + instances
@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> instances: array<ParticleInstance>;

// Group 1: GBuffer depth (rgba32float) to emulate depth test and soft fade
@group(1) @binding(0) var gbuf_depth: texture_2d<f32>;

// VS outputs
struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) world_pos: vec3<f32>,
    @location(2) quad_uv: vec2<f32>, // 0..1 inside the quad
    @location(3) world_center: vec3<f32>,
};

// Compute camera right/up from inv_view (world-space)
fn camera_right() -> vec3<f32> {
    // inv_view columns are world basis vectors; column 0 = right
    return vec3<f32>(camera.inv_view[0].x, camera.inv_view[1].x, camera.inv_view[2].x);
}
fn camera_up() -> vec3<f32> {
    // column 1 = up
    return vec3<f32>(camera.inv_view[0].y, camera.inv_view[1].y, camera.inv_view[2].y);
}

// Generate a quad in clip space per instance using 6 vertices (two triangles)
@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let inst = instances[iid];

    // Triangle list for a unit quad centered at origin:
    // corners in CCW: (-0.5,-0.5), (0.5,-0.5), (0.5,0.5), (-0.5,0.5)
    var corner: vec2<f32>;
    switch (vid % 6u) {
        case 0u: { corner = vec2<f32>(-0.5, -0.5); }
        case 1u: { corner = vec2<f32>( 0.5, -0.5); }
        case 2u: { corner = vec2<f32>( 0.5,  0.5); }
        case 3u: { corner = vec2<f32>(-0.5, -0.5); }
        case 4u: { corner = vec2<f32>( 0.5,  0.5); }
        default: { corner = vec2<f32>(-0.5,  0.5); }
    }

    let r = normalize(camera_right());
    let u = normalize(camera_up());
    let world_pos = inst.pos + (r * corner.x + u * corner.y) * inst.size;

    var out: VSOut;
    out.position = camera.view_proj * vec4<f32>(world_pos, 1.0);
    out.color = inst.color;
    out.world_pos = world_pos;
    out.quad_uv = corner + vec2<f32>(0.5, 0.5);
    out.world_center = inst.pos;
    return out;
}

// Reconstruct camera ray from pixel position
fn ray_dir_from_screen(pos: vec4<f32>) -> vec3<f32> {
    let dim = vec2<f32>(textureDimensions(gbuf_depth));
    // Convert to NDC in [-1,1], matching gbuffer get_ray
    let ndc = vec2<f32>(pos.x / dim.x * 2.0 - 1.0, 1.0 - pos.y / dim.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    var view = camera.inv_proj * clip;
    view = view / view.w;
    let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
    let dir = normalize(world_target - camera.cam_pos.xyz);
    return dir;
}

@fragment
fn fs_main(in: VSOut) -> @location(0) vec4<f32> {
    // Manual depth test against GBuffer depth stored as ray distance t
    let dim = textureDimensions(gbuf_depth);
    let w = i32(dim.x);
    let h = i32(dim.y);
    let px = clamp(i32(floor(in.position.x)), 0, w - 1);
    let py = clamp(i32(floor(in.position.y)), 0, h - 1);
    let pix = vec2<i32>(px, py);
    let scene = textureLoad(gbuf_depth, pix, 0);
    let t_scene = scene.x;

    // Particle "t" along the camera ray for this pixel (use clamped pixel coords and instance center)
    let dir = ray_dir_from_screen(vec4<f32>(f32(px), f32(py), in.position.z, in.position.w));
    let t_particle = dot(in.world_center - camera.cam_pos.xyz, dir);

    // Discard if particle is behind scene geometry (epsilon)
    if (t_particle > t_scene - 3e-3) {
        discard;
    }

    // Circular mask inside the quad (soft edge)
    let d = length(in.quad_uv - vec2<f32>(0.5, 0.5)) * 2.0;
    let mask = clamp(1.0 - smoothstep(0.8, 1.0, d), 0.0, 1.0);

    // Compute alpha from instance color and circular mask; standard alpha blending in pipeline
    let alpha = clamp(in.color.a * mask, 0.0, 1.0);
    let rgb = in.color.rgb * mask;

    return vec4<f32>(rgb, alpha);
}
