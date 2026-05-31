// debris_midfield.wgsl
// Procedural instanced midfield debris billboard particles.

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

struct DebrisMidfieldRecord {
    position_opacity: vec4<f32>, // xyz: cell position world space, w: opacity
    normal_seed: vec4<f32>,      // xyz: plane normal world space, w: seed bitcast
    radii_gaps: vec4<f32>,       // x: inner radius, y: outer radius, z: gap inner, w: gap outer
    tint_pad: vec4<f32>,         // rgb: tint, w: density scale
    light_dir_pad: vec4<f32>,    // xyz: light dir world space, w: approach fade
    handoff_pad: vec4<f32>,      // x: active physical handoff cell flag, y: exact marker flag, z: radius meters
};

struct DebrisMidfieldParamsUniform {
    cell_count: u32,
    pad0: u32,
    pad1: u32,
    pad2: u32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;

@group(1) @binding(0) var<uniform> params: DebrisMidfieldParamsUniform;
@group(1) @binding(1) var<storage, read> cells: array<DebrisMidfieldRecord>;

@group(2) @binding(0) var scene_depth: texture_2d<f32>;
@group(2) @binding(1) var planet_depth: texture_2d<f32>;

struct VSOut {
    @builtin(position) position: vec4<f32>,
    @location(0) color: vec4<f32>,
    @location(1) uv: vec2<f32>,
    @location(2) view_pos: vec3<f32>,
    @location(3) light_dir: vec3<f32>,
    @location(4) opacity: f32,
    @location(5) size: f32,
    @location(6) shape: vec4<f32>,
    @location(7) active_handoff: f32,
    @location(8) exact_handoff: f32,
};

fn hash(seed: u32) -> u32 {
    var x = seed;
    x = x ^ (x >> 16u);
    x = x * 0x7feb352du;
    x = x ^ (x >> 15u);
    x = x * 0x846ca68bu;
    x = x ^ (x >> 16u);
    return x;
}

fn rand_f32(seed: u32) -> f32 {
    return f32(hash(seed)) / 4294967295.0;
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32, @builtin(instance_index) iid: u32) -> VSOut {
    let cell_index = iid / 64u; // 64 particles per cell
    let p_idx = iid % 64u;

    var out: VSOut;
    if (cell_index >= params.cell_count) {
        out.position = vec4<f32>(0.0);
        out.color = vec4<f32>(0.0);
        out.uv = vec2<f32>(0.0);
        out.view_pos = vec3<f32>(0.0);
        out.light_dir = vec3<f32>(0.0, 0.0, 1.0);
        out.opacity = 0.0;
        out.size = 1.0;
        out.shape = vec4<f32>(0.0);
        out.active_handoff = 0.0;
        out.exact_handoff = 0.0;
        return out;
    }

    let cell = cells[cell_index];
    let seed_bits = bitcast<u32>(cell.normal_seed.w);
    let p_seed = hash(seed_bits + p_idx);
    let active_handoff = cell.handoff_pad.x > 0.5;
    let exact_handoff = cell.handoff_pad.y > 0.5;
    if (exact_handoff && p_idx > 0u) {
        out.position = vec4<f32>(0.0);
        out.color = vec4<f32>(0.0);
        out.uv = vec2<f32>(0.0);
        out.view_pos = vec3<f32>(0.0);
        out.light_dir = vec3<f32>(0.0, 0.0, 1.0);
        out.opacity = 0.0;
        out.size = 1.0;
        out.shape = vec4<f32>(0.0);
        out.active_handoff = 1.0;
        out.exact_handoff = 1.0;
        return out;
    }

    // Orthonormal basis of the plane in world space
    let N = normalize(cell.normal_seed.xyz);
    var T = vec3<f32>(1.0, 0.0, 0.0);
    if (abs(N.x) > 0.9) {
        T = vec3<f32>(0.0, 1.0, 0.0);
    }
    T = normalize(cross(T, N));
    let B = normalize(cross(N, T));

    // Background cells stay broad and atmospheric. The active handoff cell uses
    // a smaller local halo around the physical pocket, rather than the full
    // cell width, so the inside-ring view stays populated without advertising
    // far-off background cards as precise mineable targets. Exact handoff
    // records are emitted from physical asteroid template positions.
    let rand_r = rand_f32(p_seed);
    let rand_theta = rand_f32(hash(p_seed + 1u)) * 6.2831853;
    let handoff_offset_limit = select(9000.0, 0.0, exact_handoff);
    let handoff_thickness_limit = select(520.0, 0.0, exact_handoff);
    let offset_limit = select(20000.0, handoff_offset_limit, active_handoff);
    let thickness_limit = select(1500.0, handoff_thickness_limit, active_handoff);
    let offset_radius = sqrt(rand_r) * offset_limit;
    
    let offset_t = offset_radius * cos(rand_theta);
    let offset_b = offset_radius * sin(rand_theta);
    let offset_n = (rand_f32(hash(p_seed + 2u)) - 0.5) * thickness_limit;

    let random_pos_world = cell.position_opacity.xyz + offset_t * T + offset_b * B + offset_n * N;
    let p_pos_world = select(random_pos_world, cell.position_opacity.xyz, exact_handoff);

    // Check distance limits for LOD crossfading
    let view_ray = p_pos_world - camera.cam_pos.xyz;
    let distance = length(view_ray);

    // Billboarding corners
    var corner: vec2<f32>;
    switch (vid % 6u) {
        case 0u: { corner = vec2<f32>(-0.5, -0.5); }
        case 1u: { corner = vec2<f32>( 0.5, -0.5); }
        case 2u: { corner = vec2<f32>( 0.5,  0.5); }
        case 3u: { corner = vec2<f32>(-0.5, -0.5); }
        case 4u: { corner = vec2<f32>( 0.5,  0.5); }
        default: { corner = vec2<f32>(-0.5,  0.5); }
    }

    let right = normalize(camera.inv_view[0].xyz);
    let up = normalize(camera.inv_view[1].xyz);
    
    // Non-exact atmospheric cards must read as debris flecks near the ship,
    // but regain larger billboard scale in the mid field so the ring does not
    // collapse into invisible dust. Exact handoff markers are the only records
    // allowed to use physical asteroid-scale size in close range.
    let size_roll = pow(rand_f32(hash(p_seed + 3u)), 1.35);
    let background_size_near = mix(1.5, 12.0, size_roll);
    let background_size_mid = mix(12.0, 72.0, size_roll);
    let active_halo_size_near = mix(2.0, 16.0, size_roll);
    let active_halo_size_mid = mix(8.0, 46.0, size_roll);
    let background_size = mix(background_size_near, background_size_mid, smoothstep(3500.0, 18000.0, distance));
    let active_halo_size = mix(active_halo_size_near, active_halo_size_mid, smoothstep(1800.0, 9000.0, distance));
    let random_size = select(background_size, active_halo_size, active_handoff);
    let exact_size = clamp(max(cell.handoff_pad.z, 1.0) * 2.4, 18.0, 220.0);
    let size = select(random_size, exact_size, exact_handoff);
    let world_pos = p_pos_world + (right * corner.x + up * corner.y) * size;

    var clip_pos = camera.view_proj * vec4<f32>(world_pos, 1.0);
    clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;

    out.position = clip_pos;
    let record_density = clamp(cell.tint_pad.w, 0.0, 1.0);
    let record_fade = clamp(cell.light_dir_pad.w, 0.0, 1.0);
    let tint = clamp(cell.tint_pad.rgb, vec3<f32>(0.0), vec3<f32>(1.0));
    let albedo_jitter = mix(0.20, 0.46, rand_f32(hash(p_seed + 4u)));
    let warm_jitter = rand_f32(hash(p_seed + 5u));
    let mineral_tint = mix(
        vec3<f32>(0.50, 0.48, 0.44),
        vec3<f32>(0.78, 0.68, 0.54),
        warm_jitter,
    );
    let rock_albedo = clamp(tint * mineral_tint * albedo_jitter, vec3<f32>(0.035), vec3<f32>(0.38));
    out.color = vec4<f32>(rock_albedo, 1.0);
    out.uv = corner + vec2<f32>(0.5, 0.5);
    out.view_pos = world_pos;
    out.light_dir = cell.light_dir_pad.xyz;
    
    // Calculate smooth LOD crossfade:
    // As player gets extremely close, we transition to the local physical pocket.
    // As player gets far, we fade out midfield cells.
    // Midfield active range: fade in between 300km and 200km. Inactive
    // background cells fade out before the physical gameplay envelope so
    // shader-random atmospheric particles do not become chaseable rocks.
    // Exact promotable records are emitted from deterministic template
    // positions and CPU-faded through the preload/crossfade band.
    var lod_alpha = 1.0;
    if (distance > 200000.0) {
        lod_alpha = 1.0 - smoothstep(200000.0, 300000.0, distance);
    } else if (active_handoff && distance < 1800.0) {
        lod_alpha = smoothstep(350.0, 1800.0, distance);
    } else if (!active_handoff && distance < 12000.0) {
        lod_alpha = smoothstep(3500.0, 12000.0, distance);
    }

    let visibility_scale = select(0.72, 0.92, active_handoff);
    out.opacity = min(cell.position_opacity.w, record_density * record_fade) * lod_alpha * visibility_scale;
    out.size = size;
    out.shape = vec4<f32>(
        rand_f32(hash(p_seed + 6u)),
        rand_f32(hash(p_seed + 7u)),
        rand_f32(hash(p_seed + 8u)),
        rand_f32(hash(p_seed + 9u)),
    );
    out.active_handoff = select(0.0, 1.0, active_handoff);
    out.exact_handoff = select(0.0, 1.0, exact_handoff);

    return out;
}

struct FSOut {
    @location(0) accum: vec4<f32>,
    @location(1) weight: f32,
};

fn sanitize_scene_depth(depth: f32) -> f32 {
    let far_t = max(camera.distance_limits.y, 1.0);
    if (depth > 0.0 && depth < far_t) {
        return depth;
    }
    return far_t;
}

@fragment
fn fs_main(in: VSOut) -> FSOut {
    let uv = in.uv;
    let centered = uv - vec2<f32>(0.5);
    let atmospheric_card = 1.0 - in.exact_handoff;
    let aspect = mix(0.72, 1.34, in.shape.x);
    let shaped = vec2<f32>(centered.x * aspect, centered.y / aspect);
    let dist = length(shaped);
    let edge_noise =
        0.030 * sin(dot(shaped, vec2<f32>(18.0, 31.0)) + in.shape.y * 6.2831853) +
        0.022 * sin(dot(shaped, vec2<f32>(-29.0, 17.0)) + in.shape.z * 6.2831853) +
        0.016 * sin(dot(shaped, vec2<f32>(43.0, -37.0)) + in.shape.w * 6.2831853);
    let chip_noise =
        0.5 +
        0.24 * sin(dot(shaped, vec2<f32>(91.0, -53.0)) + in.shape.x * 19.0) +
        0.18 * sin(dot(shaped, vec2<f32>(-127.0, 89.0)) + in.shape.z * 23.0);
    let chipped_edge = atmospheric_card * 0.035 * smoothstep(0.40, 0.86, chip_noise);
    let boundary = clamp(0.405 + edge_noise - chipped_edge, 0.30, 0.47);
    
    // Draw lumpy pseudo-rock silhouettes instead of ideal circles. Ring haze is
    // analytic far-ring work; midfield billboards must not grow into fog cards.
    if (dist > boundary) {
        discard;
    }

    let dim = textureDimensions(scene_depth);
    let pix = vec2<i32>(
        clamp(i32(in.position.x), 0, i32(dim.x) - 1),
        clamp(i32(in.position.y), 0, i32(dim.y) - 1),
    );
    let t_scene = sanitize_scene_depth(textureLoad(scene_depth, pix, 0).x);
    let t_planet = sanitize_scene_depth(textureLoad(planet_depth, pix, 0).x);
    let t_occluder = min(t_scene, t_planet);
    
    let view_ray = in.view_pos - camera.cam_pos.xyz;
    let t_pixel = length(view_ray);
    let bias = 1.5; // Slightly larger bias for large mid-field cards

    if (t_pixel > t_occluder + bias) {
        discard;
    }

    // Reconstruct a rough billboard normal. It is intentionally flatter and
    // noisier than a sphere so close impostors do not look like white balls.
    let normalized_xy = shaped / max(boundary, 0.001);
    let normal_z = sqrt(max(0.001, 1.0 - dot(normalized_xy, normalized_xy)));
    let bump =
        0.18 * sin(dot(uv, vec2<f32>(41.0, 57.0)) + in.shape.y * 9.0) +
        0.12 * sin(dot(uv, vec2<f32>(73.0, -29.0)) + in.shape.z * 11.0);
    let normal = normalize(vec3<f32>(normalized_xy * (0.62 + bump * 0.12), normal_z * 0.78));

    // Directional lighting from primary star
    let diff = max(dot(normal, normalize(in.light_dir)), 0.0);
    let shaded = 0.10 + 0.58 * diff;

    // Apply soft depth boundary culling (soft depth edge fading)
    let depth_diff = (t_occluder + bias) - t_pixel;
    let soft_factor = clamp(depth_diff / max(10.0, in.size * 0.5), 0.0, 1.0);

    let handoff_alpha_scale = mix(1.0, 0.96, in.active_handoff);
    let silhouette_alpha = 1.0 - smoothstep(boundary * 0.72, boundary, dist);
    let pit_mask = clamp(
        0.58 +
        0.24 * sin(dot(uv, vec2<f32>(151.0, 211.0)) + in.shape.y * 29.0) +
        0.18 * sin(dot(uv, vec2<f32>(-233.0, 101.0)) + in.shape.w * 31.0),
        0.0,
        1.0,
    );
    let dust_alpha = mix(1.0, clamp(0.44 + 0.56 * pit_mask, 0.36, 1.0), atmospheric_card);
    let base_alpha = in.opacity * handoff_alpha_scale * silhouette_alpha * dust_alpha * soft_factor;

    let edge_shadow = mix(0.46, 1.0, 1.0 - smoothstep(boundary * 0.55, boundary, dist));
    let grain = clamp(
        0.72 +
        0.18 * sin(dot(uv, vec2<f32>(97.0, 131.0)) + in.shape.w * 12.0) +
        0.10 * sin(dot(uv, vec2<f32>(181.0, -67.0)) + in.shape.x * 17.0),
        0.45,
        1.0,
    );
    let pit_shadow = mix(1.0, mix(0.54, 1.0, pit_mask), atmospheric_card);
    let atmospheric_darkening = mix(1.0, 0.62, atmospheric_card);
    let final_rgb = in.color.rgb * shaded * edge_shadow * grain * pit_shadow * atmospheric_darkening;
    let alpha = clamp(base_alpha, 0.0, 0.58);
    if (alpha < 0.001) {
        discard;
    }

    // Standard WBOIT weight function
    let depth_norm = clamp(t_pixel / 300000.0, 0.0, 1.0);
    let weight = max(1e-3, alpha) * pow(1.0 - depth_norm, 4.0);

    var out: FSOut;
    out.accum = vec4<f32>(final_rgb * alpha * weight, alpha);
    out.weight = alpha * weight;
    return out;
}
