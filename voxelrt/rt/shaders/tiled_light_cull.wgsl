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
    pad2: vec2<f32>,
    ao_quality: vec4<f32>,
};

struct DirectionalShadowCascade {
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
    params: vec4<f32>,
};

struct Light {
    position: vec4<f32>,
    direction: vec4<f32>,
    color: vec4<f32>,
    params: vec4<f32>,
    shadow_meta: vec4<u32>,
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
    directional_cascades: array<DirectionalShadowCascade, 2>,
};

struct TileLightListParams {
    tile_size: u32,
    tiles_x: u32,
    tiles_y: u32,
    max_lights_per_tile: u32,
    screen_width: u32,
    screen_height: u32,
    num_tiles: u32,
    pad0: u32,
};

struct TileLightHeader {
    offset: u32,
    count: u32,
    overflow: u32,
    pad0: u32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;

@group(1) @binding(0) var<uniform> tile_params: TileLightListParams;
@group(1) @binding(1) var<storage, read_write> tile_headers: array<TileLightHeader>;
@group(1) @binding(2) var<storage, read_write> tile_light_indices: array<u32>;

fn camera_axis_ws(axis: vec3<f32>) -> vec3<f32> {
    let axis_ws = (camera.inv_view * vec4<f32>(axis, 0.0)).xyz;
    if (length(axis_ws) < 1e-5) {
        return axis;
    }
    return normalize(axis_ws);
}

fn clip_to_uv(clip: vec4<f32>) -> vec2<f32> {
    let ndc = clip.xy / clip.w;
    return vec2<f32>(ndc.x * 0.5 + 0.5, -ndc.y * 0.5 + 0.5);
}

fn light_affects_tile(light: Light, tile_coord: vec2<u32>) -> bool {
    let light_type = u32(light.params.z);
    if (light_type == 1u) {
        return true;
    }

    let light_range = max(light.params.x, 0.0);
    if (light_range <= 0.0) {
        return false;
    }

    if (distance(camera.cam_pos.xyz, light.position.xyz) <= light_range) {
        return true;
    }

    let center_clip = camera.view_proj * vec4<f32>(light.position.xyz, 1.0);
    let right_clip = camera.view_proj * vec4<f32>(light.position.xyz + camera_axis_ws(vec3<f32>(1.0, 0.0, 0.0)) * light_range, 1.0);
    let up_clip = camera.view_proj * vec4<f32>(light.position.xyz + camera_axis_ws(vec3<f32>(0.0, 1.0, 0.0)) * light_range, 1.0);
    if (center_clip.w <= 0.0 || right_clip.w <= 0.0 || up_clip.w <= 0.0) {
        return true;
    }

    let center_uv = clip_to_uv(center_clip);
    let right_uv = clip_to_uv(right_clip);
    let up_uv = clip_to_uv(up_clip);
    let radius_u = max(abs(right_uv.x - center_uv.x), abs(up_uv.x - center_uv.x));
    let radius_v = max(abs(right_uv.y - center_uv.y), abs(up_uv.y - center_uv.y));
    let light_min_uv = center_uv - vec2<f32>(radius_u, radius_v);
    let light_max_uv = center_uv + vec2<f32>(radius_u, radius_v);

    let tile_min_px = vec2<f32>(
        f32(tile_coord.x * tile_params.tile_size),
        f32(tile_coord.y * tile_params.tile_size),
    );
    let tile_max_px = vec2<f32>(
        f32(min((tile_coord.x + 1u) * tile_params.tile_size, tile_params.screen_width)),
        f32(min((tile_coord.y + 1u) * tile_params.tile_size, tile_params.screen_height)),
    );
    let screen_size = vec2<f32>(f32(tile_params.screen_width), f32(tile_params.screen_height));
    let tile_min_uv = tile_min_px / screen_size;
    let tile_max_uv = tile_max_px / screen_size;

    return !(
        light_max_uv.x < tile_min_uv.x ||
        light_min_uv.x > tile_max_uv.x ||
        light_max_uv.y < tile_min_uv.y ||
        light_min_uv.y > tile_max_uv.y
    );
}

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    if (global_id.x >= tile_params.tiles_x || global_id.y >= tile_params.tiles_y) {
        return;
    }

    let tile_index = global_id.y * tile_params.tiles_x + global_id.x;
    let base_offset = tile_index * tile_params.max_lights_per_tile;

    var header = TileLightHeader(base_offset, 0u, 0u, 0u);
    for (var light_idx = 0u; light_idx < camera.num_lights; light_idx = light_idx + 1u) {
        if (!light_affects_tile(lights[light_idx], global_id.xy)) {
            continue;
        }
        if (header.count < tile_params.max_lights_per_tile) {
            tile_light_indices[base_offset + header.count] = light_idx;
            header.count = header.count + 1u;
        } else {
            header.overflow = 1u;
        }
    }

    tile_headers[tile_index] = header;
}
