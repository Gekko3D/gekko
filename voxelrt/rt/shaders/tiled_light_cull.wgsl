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
    distance_limits: vec4<f32>,
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

struct LocalLightCoverage {
    mode: u32,
    min_uv: vec2<f32>,
    max_uv: vec2<f32>,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;

@group(1) @binding(0) var<uniform> tile_params: TileLightListParams;
@group(1) @binding(1) var<storage, read_write> tile_headers: array<TileLightHeader>;
@group(1) @binding(2) var<storage, read_write> tile_light_indices: array<u32>;

const TILE_LIGHT_NEAR_PLANE_CLAMP = 1e-4;
const LOCAL_LIGHT_COVERAGE_HIDDEN = 0u;
const LOCAL_LIGHT_COVERAGE_PARTIAL = 1u;
const LOCAL_LIGHT_COVERAGE_FULLSCREEN = 2u;

fn camera_axis_ws(axis: vec3<f32>) -> vec3<f32> {
    let axis_ws = (camera.inv_view * vec4<f32>(axis, 0.0)).xyz;
    if (length(axis_ws) < 1e-5) {
        return axis;
    }
    return normalize(axis_ws);
}

fn project_local_light_coverage(light: Light) -> LocalLightCoverage {
    let light_range = max(light.params.x, 0.0);
    if (light_range <= 0.0) {
        return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_HIDDEN, vec2<f32>(0.0), vec2<f32>(0.0));
    }

    let camera_right = camera_axis_ws(vec3<f32>(1.0, 0.0, 0.0));
    let camera_up = camera_axis_ws(vec3<f32>(0.0, 1.0, 0.0));
    let camera_forward = camera_axis_ws(vec3<f32>(0.0, 0.0, -1.0));
    let delta = light.position.xyz - camera.cam_pos.xyz;
    let view_pos = vec3<f32>(
        dot(delta, camera_right),
        dot(delta, camera_up),
        -dot(delta, camera_forward),
    );

    if (length(view_pos) <= light_range) {
        return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_FULLSCREEN, vec2<f32>(0.0), vec2<f32>(1.0));
    }

    // OpenGL-style view space looks down -Z, so a light is fully behind the
    // camera when even the closest point on the sphere stays on or behind z=0.
    if (view_pos.z - light_range >= 0.0) {
        return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_HIDDEN, vec2<f32>(0.0), vec2<f32>(0.0));
    }

    var min_ndc = vec2<f32>(1e9, 1e9);
    var max_ndc = vec2<f32>(-1e9, -1e9);
    var visible = 0u;
    let signs = array<f32, 2>(-1.0, 1.0);

    for (var ix = 0u; ix < 2u; ix = ix + 1u) {
        for (var iy = 0u; iy < 2u; iy = iy + 1u) {
            for (var iz = 0u; iz < 2u; iz = iz + 1u) {
                var corner = vec3<f32>(
                    view_pos.x + signs[ix] * light_range,
                    view_pos.y + signs[iy] * light_range,
                    view_pos.z + signs[iz] * light_range,
                );
                if (corner.z >= 0.0) {
                    corner.z = -TILE_LIGHT_NEAR_PLANE_CLAMP;
                }

                let world_corner =
                    camera.cam_pos.xyz +
                    camera_right * corner.x +
                    camera_up * corner.y -
                    camera_forward * corner.z;
                let clip = camera.view_proj * vec4<f32>(world_corner, 1.0);
                if (abs(clip.w) < 1e-6) {
                    continue;
                }

                let ndc = clip.xy / clip.w;
                min_ndc = min(min_ndc, ndc);
                max_ndc = max(max_ndc, ndc);
                visible = 1u;
            }
        }
    }

    if (visible == 0u) {
        return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_HIDDEN, vec2<f32>(0.0), vec2<f32>(0.0));
    }
    if (max_ndc.x < -1.0 || min_ndc.x > 1.0 || max_ndc.y < -1.0 || min_ndc.y > 1.0) {
        return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_HIDDEN, vec2<f32>(0.0), vec2<f32>(0.0));
    }

    min_ndc = clamp(min_ndc, vec2<f32>(-1.0), vec2<f32>(1.0));
    max_ndc = clamp(max_ndc, vec2<f32>(-1.0), vec2<f32>(1.0));

    let min_u = min_ndc.x * 0.5 + 0.5;
    let max_u = max_ndc.x * 0.5 + 0.5;
    let min_v = (-max_ndc.y) * 0.5 + 0.5;
    let max_v = (-min_ndc.y) * 0.5 + 0.5;

    return LocalLightCoverage(LOCAL_LIGHT_COVERAGE_PARTIAL, vec2<f32>(min_u, min_v), vec2<f32>(max_u, max_v));
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

    let coverage = project_local_light_coverage(light);
    if (coverage.mode == LOCAL_LIGHT_COVERAGE_HIDDEN) {
        return false;
    }
    if (coverage.mode == LOCAL_LIGHT_COVERAGE_FULLSCREEN) {
        return true;
    }

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
        coverage.max_uv.x < tile_min_uv.x ||
        coverage.min_uv.x > tile_max_uv.x ||
        coverage.max_uv.y < tile_min_uv.y ||
        coverage.min_uv.y > tile_max_uv.y
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
