// voxelrt/shaders/deferred_lighting.wgsl

// ============== STRUCTS ==============

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
    params: vec4<f32>, // x: split_far, y: texel_world_size, z: depth_scale_to_ndc, w: reserved
};

struct Light {
    position: vec4<f32>,
    direction: vec4<f32>,
    color: vec4<f32>,
    params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
    shadow_meta: vec4<u32>,
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
    directional_cascades: array<DirectionalShadowCascade, 2>,
};

struct DirectionalCascadeSelection {
    primary_index: u32,
    secondary_index: u32,
    blend: f32,
};

struct ShadowLayerParams {
    viewport_scale: vec2<f32>,
    effective_resolution: f32,
    inv_effective_resolution: f32,
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

struct Ray {
    origin: vec3<f32>,
    dir: vec3<f32>,
    inv_dir: vec3<f32>,
};

struct LightingContribution {
    color: vec3<f32>,
    contributes: u32,
};

// ============== BIND GROUPS ==============

// Group 0: Scene
@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;
@group(0) @binding(2) var<storage, read> shadow_layer_params: array<ShadowLayerParams>;

// Group 1: G-Buffer Input
@group(1) @binding(0) var in_depth: texture_2d<f32>;
@group(1) @binding(1) var in_normal: texture_2d<f32>;
@group(1) @binding(2) var in_material: texture_2d<f32>;
@group(1) @binding(3) var in_position: texture_2d<f32>;

// Output: Final Color (HDR)
@group(1) @binding(4) var out_color: texture_storage_2d<rgba16float, write>;

// Shadow Maps
@group(1) @binding(5) var in_shadow_maps: texture_2d_array<f32>;

// Skybox
@group(1) @binding(6) var in_skybox: texture_2d<f32>;
@group(1) @binding(7) var skybox_sampler: sampler;

// Group 2: Voxel Data (reuse)
@group(2) @binding(3) var<storage, read> materials: array<vec4<f32>>;

// Group 3: Tiled lighting buffers
@group(3) @binding(0) var<uniform> tile_light_params: TileLightListParams;
@group(3) @binding(1) var<storage, read> tile_light_headers: array<TileLightHeader>;
@group(3) @binding(2) var<storage, read> tile_light_indices: array<u32>;

// ============== LIGHTING CALCULATION ==============

const PI: f32 = 3.14159265359;
const MIN_ROUGHNESS: f32 = 0.045;
const PBR_EPSILON: f32 = 1e-4;

fn saturate(v: f32) -> f32 {
    return clamp(v, 0.0, 1.0);
}

fn abs_i32(v: i32) -> i32 {
    return select(v, -v, v < 0);
}

fn max3f(v: vec3<f32>) -> f32 {
    return max(v.x, max(v.y, v.z));
}

fn srgb_channel_to_linear(v: f32) -> f32 {
    if (v <= 0.04045) {
        return v / 12.92;
    }
    return pow((v + 0.055) / 1.055, 2.4);
}

fn srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
    return vec3<f32>(
        srgb_channel_to_linear(c.x),
        srgb_channel_to_linear(c.y),
        srgb_channel_to_linear(c.z),
    );
}

fn dielectric_f0_from_ior(ior_input: f32) -> vec3<f32> {
    var ior = ior_input;
    if (ior <= 1.01) {
        ior = 1.5;
    }
    let reflectance = (ior - 1.0) / (ior + 1.0);
    return vec3<f32>(reflectance * reflectance);
}

fn fresnel_schlick(cos_theta: f32, F0: vec3<f32>) -> vec3<f32> {
    return F0 + (1.0 - F0) * pow(saturate(1.0 - cos_theta), 5.0);
}

fn fresnel_schlick_roughness(cos_theta: f32, F0: vec3<f32>, roughness: f32) -> vec3<f32> {
    return F0 + (max(vec3<f32>(1.0 - roughness), F0) - F0) * pow(saturate(1.0 - cos_theta), 5.0);
}

fn distribution_ggx(NdotH: f32, roughness: f32) -> f32 {
    let a = max(roughness, MIN_ROUGHNESS);
    let alpha = a * a;
    let alpha2 = alpha * alpha;
    let denom = NdotH * NdotH * (alpha2 - 1.0) + 1.0;
    return alpha2 / max(PI * denom * denom, PBR_EPSILON);
}

fn geometry_schlick_ggx(NdotX: f32, roughness: f32) -> f32 {
    let r = roughness + 1.0;
    let k = (r * r) * 0.125;
    return NdotX / max(NdotX * (1.0 - k) + k, PBR_EPSILON);
}

fn geometry_smith(NdotV: f32, NdotL: f32, roughness: f32) -> f32 {
    return geometry_schlick_ggx(NdotV, roughness) * geometry_schlick_ggx(NdotL, roughness);
}

fn camera_forward_ws() -> vec3<f32> {
    return normalize((camera.inv_view * vec4<f32>(0.0, 0.0, -1.0, 0.0)).xyz);
}

fn choose_directional_cascade(light: Light, hit_pos: vec3<f32>) -> DirectionalCascadeSelection {
    let cascade_count = light.shadow_meta.z;
    if (cascade_count <= 1u) {
        return DirectionalCascadeSelection(0u, 0u, 0.0);
    }
    // Cascades are authored as view-depth slices, not spherical shells around the camera.
    let receiver_depth = max(dot(hit_pos - camera.cam_pos.xyz, camera_forward_ws()), 0.0);
    let split_depth = light.directional_cascades[0].params.x;
    let transition = max(4.0, max(light.directional_cascades[0].params.y * 24.0, split_depth * 0.12));
    let blend_start = max(0.0, split_depth - transition);
    let blend_end = split_depth + transition;
    if (receiver_depth <= blend_start) {
        return DirectionalCascadeSelection(0u, 0u, 0.0);
    }
    if (receiver_depth >= blend_end) {
        let far_idx = min(1u, cascade_count - 1u);
        return DirectionalCascadeSelection(far_idx, far_idx, 0.0);
    }
    let blend = smoothstep(blend_start, blend_end, receiver_depth);
    return DirectionalCascadeSelection(0u, min(1u, cascade_count - 1u), blend);
}

fn sample_directional_shadow(
    light: Light,
    hit_pos: vec3<f32>,
    normal: vec3<f32>,
    L: vec3<f32>,
    receiver_shadow_group_id: u32,
    receiver_shadow_seam_epsilon: f32,
    cascade_idx: u32
) -> f32 {
    var cascade_view_proj = light.directional_cascades[0].view_proj;
    var cascade_params = light.directional_cascades[0].params;
    if (cascade_idx != 0u) {
        cascade_view_proj = light.directional_cascades[1].view_proj;
        cascade_params = light.directional_cascades[1].params;
    }
    let receiver_normal_offset_world = max(0.08, 0.50 * cascade_params.y);
    let receiver_light_offset_world = max(0.04, 0.30 * cascade_params.y);
    let compare_bias_world = max(0.08, 0.90 * cascade_params.y);
    let pos_ws = hit_pos + normal * receiver_normal_offset_world + L * receiver_light_offset_world;
    let directional_compare_bias = compare_bias_world * cascade_params.z;
    let seam_pos_ls = cascade_view_proj * vec4<f32>(pos_ws - L * receiver_shadow_seam_epsilon, 1.0);
    let seam_depth_n = clamp(seam_pos_ls.z / seam_pos_ls.w, -1.0, 1.0);
    let receiver_pos_ls = cascade_view_proj * vec4<f32>(pos_ws, 1.0);
    let receiver_depth_n = clamp(receiver_pos_ls.z / receiver_pos_ls.w, -1.0, 1.0);
    let directional_seam_epsilon_n = abs(seam_depth_n - receiver_depth_n);

    let pos_ls = cascade_view_proj * vec4<f32>(pos_ws, 1.0);
    let proj_pos = pos_ls.xyz / pos_ls.w;
    let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);
    if (!(pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0)) {
        return 1.0;
    }

    let layer = light.shadow_meta.x + cascade_idx;
    let layer_params = shadow_layer_params[layer];
    let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
    let base_px_f = shadow_uv * vec2<f32>(f32(effective_resolution), f32(effective_resolution));
    let base_px = vec2<i32>(
        i32(clamp(base_px_f.x, 0.0, f32(effective_resolution - 1u))),
        i32(clamp(base_px_f.y, 0.0, f32(effective_resolution - 1u)))
    );
    let my_depth_n = clamp(proj_pos.z, -1.0, 1.0);
    let NdL = max(dot(normal, L), 0.0);
    let bias = directional_compare_bias + directional_compare_bias * 0.75 * (1.0 - NdL);
    let max_px = vec2<i32>(i32(effective_resolution) - 1, i32(effective_resolution) - 1);
    var visibility = 0.0;
    var sample_weight_sum = 0.0;
    for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
        for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
            let off = base_px + vec2<i32>(dx, dy);
            let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
            let shadow_sample = textureLoad(in_shadow_maps, clamped_off, i32(layer), 0);
            let sampled_depth_n = clamp(shadow_sample.r, -1.0, 1.0);
            let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
            let same_shadow_group =
                receiver_shadow_group_id != 0u &&
                sampled_shadow_group_id == receiver_shadow_group_id;
            let receiver_minus_occluder = my_depth_n - sampled_depth_n;
            let seam_lit = same_shadow_group && receiver_minus_occluder <= directional_seam_epsilon_n;
            let wx = f32(2 - abs(dx));
            let wy = f32(2 - abs(dy));
            let sample_weight = wx * wy;
            sample_weight_sum += sample_weight;
            visibility += sample_weight * select(0.0, 1.0, seam_lit || sampled_depth_n >= my_depth_n - bias);
        }
    }
    return visibility / max(sample_weight_sum, 1.0);
}

fn calculate_lighting(
    hit_pos: vec3<f32>, 
    normal: vec3<f32>, 
    view_dir: vec3<f32>,
    base_color: vec3<f32>,
    roughness: f32,
    metalness: f32,
    ior: f32,
    receiver_shadow_group_id: u32,
    receiver_shadow_seam_epsilon: f32,
    light_idx: u32
) -> LightingContribution {
    let light = lights[light_idx];
    var L = vec3<f32>(0.0);
    var attenuation = 1.0;
    let light_type = u32(light.params.z);

    if (light_type == 1u) { // Directional
        L = -normalize(light.direction.xyz);
    } else {
        let L_vec = light.position.xyz - hit_pos;
        let dist_to_light = length(L_vec);
        L = normalize(L_vec);
        let range = light.params.x;
        if (dist_to_light > range) {
            attenuation = 0.0;
        } else {
            let dist_sq = dist_to_light * dist_to_light;
            let factor = dist_to_light / range;
            let smooth_factor = max(0.0, 1.0 - factor * factor);
            let inv_sq = 1.0 / (dist_sq + 1.0);
            attenuation = inv_sq * smooth_factor * smooth_factor * 50.0;
            if (light_type == 2u) { // Spot
                let spot_dir = normalize(light.direction.xyz);
                let cos_cur = dot(-L, spot_dir);
                let cos_cone = light.params.y;
                if (cos_cur < cos_cone) {
                    attenuation = 0.0;
                } else {
                    let spot_att = smoothstep(cos_cone, cos_cone + 0.1, cos_cur);
                    attenuation *= spot_att;
                }
            }
        }
    }
    
    if (attenuation > 0.0) {
        // Shadowing
        if (light_type != 0u && light.shadow_meta.y > 0u) {
            if (light_type == 1u) {
                let selection = choose_directional_cascade(light, hit_pos);
                let primary_visibility = sample_directional_shadow(light, hit_pos, normal, L, receiver_shadow_group_id, receiver_shadow_seam_epsilon, selection.primary_index);
                let secondary_visibility = select(
                    primary_visibility,
                    sample_directional_shadow(light, hit_pos, normal, L, receiver_shadow_group_id, receiver_shadow_seam_epsilon, selection.secondary_index),
                    selection.secondary_index != selection.primary_index,
                );
                attenuation *= mix(primary_visibility, secondary_visibility, selection.blend);
            } else {
                var pos_ws = hit_pos;
                let shadow_view_proj = light.view_proj;
                let layer = light.shadow_meta.x;
                let layer_params = shadow_layer_params[layer];
                let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
                let pos_ls = shadow_view_proj * vec4<f32>(pos_ws, 1.0);
                let proj_pos = pos_ls.xyz / pos_ls.w;
                let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);

                if (pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0) {
                    let base_px_f = shadow_uv * vec2<f32>(f32(effective_resolution), f32(effective_resolution));
                    let base_px = vec2<i32>(
                        i32(clamp(base_px_f.x, 0.0, f32(effective_resolution - 1u))),
                        i32(clamp(base_px_f.y, 0.0, f32(effective_resolution - 1u)))
                    );

                    var my_depth_m = 0.0;
                    let receiver_offset = max(receiver_shadow_seam_epsilon * 0.5, 0.05);
                    let pos_off = hit_pos + normal * receiver_offset;
                    my_depth_m = distance(light.position.xyz, pos_off);

                    let NdL = max(dot(normal, L), 0.0);
                    let max_px = vec2<i32>(i32(effective_resolution) - 1, i32(effective_resolution) - 1);
                    var visibility = 0.0;
                    var sample_weight_sum = 0.0;
                    for (var dy: i32 = -2; dy <= 2; dy = dy + 1) {
                        for (var dx: i32 = -2; dx <= 2; dx = dx + 1) {
                            let off = base_px + vec2<i32>(dx, dy);
                            let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
                            let shadow_sample = textureLoad(in_shadow_maps, clamped_off, i32(layer), 0);
                            let sampled_depth = shadow_sample.r;
                            let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
                            let same_shadow_group =
                                receiver_shadow_group_id != 0u &&
                                sampled_shadow_group_id == receiver_shadow_group_id;
                            let baseBiasM = 0.05;
                            let slopeBiasM = 0.1;
                            let biasM = baseBiasM + slopeBiasM * (1.0 - NdL);
                            let receiver_minus_occluder = my_depth_m - sampled_depth;
                            let seam_lit = same_shadow_group && receiver_minus_occluder <= receiver_shadow_seam_epsilon;
                            sample_weight_sum += 1.0;
                            visibility += select(0.0, 1.0, seam_lit || sampled_depth >= my_depth_m - biasM);
                        }
                    }
                    attenuation *= visibility / max(sample_weight_sum, 1.0);
                }
            }
        }

        let NdotL = max(dot(normal, L), 0.0);
        let NdotV = max(dot(normal, view_dir), 0.0);
        if (NdotL <= 0.0 || NdotV <= 0.0) {
            return LightingContribution(vec3<f32>(0.0), 0u);
        }

        let half_dir = normalize(L + view_dir);
        let NdotH = max(dot(normal, half_dir), 0.0);
        let HdotV = max(dot(half_dir, view_dir), 0.0);
        let rough = max(roughness, MIN_ROUGHNESS);

        let dielectric_f0 = dielectric_f0_from_ior(ior);
        let F0 = mix(dielectric_f0, base_color, metalness);
        let F = fresnel_schlick(HdotV, F0);
        let D = distribution_ggx(NdotH, rough);
        let G = geometry_smith(NdotV, NdotL, rough);

        let numerator = D * G * F;
        let denominator = max(4.0 * NdotV * NdotL, PBR_EPSILON);
        let specular = numerator / denominator;

        let kS = F;
        let kD = (vec3<f32>(1.0) - kS) * (1.0 - metalness);
        let diffuse = kD * base_color / PI;
        let radiance = light.color.xyz * attenuation * light.color.w;
        let lit = (diffuse + specular) * radiance * NdotL;
        if (max(lit.x, max(lit.y, lit.z)) > 0.0) {
            return LightingContribution(lit, 1u);
        }
        return LightingContribution(vec3<f32>(0.0), 0u);
    }
    return LightingContribution(vec3<f32>(0.0), 0u);
}

fn ndc_for_shadow(uv: vec2<f32>) -> vec2<f32> {
    return vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
}

// NOTE: We need inverse() in deferred_lighting too if we use this logic.
// Or we just store world-space distance in shadow map.
// Actually, shadow_dist IS world-space distance from light's "near plane".

fn dir_to_uv(dir: vec3<f32>) -> vec2<f32> {
    let PI = 3.14159265359;
    let u = 0.5 + atan2(dir.z, dir.x) / (2.0 * PI);
    let v = 0.5 - asin(dir.y) / PI;
    return vec2<f32>(u, v);
}

fn light_density_heatmap(count: u32) -> vec3<f32> {
    if (count == 0u) {
        return vec3<f32>(0.0, 0.0, 0.0);
    }
    if (count == 1u) {
        return vec3<f32>(0.10, 0.35, 1.00);
    }
    if (count <= 3u) {
        return vec3<f32>(0.00, 0.85, 1.00);
    }
    if (count <= 7u) {
        return vec3<f32>(0.95, 0.90, 0.10);
    }
    return vec3<f32>(1.00, 0.20, 0.10);
}

fn sample_directional_sky_ambient(normal: vec3<f32>, ao: f32) -> vec3<f32> {
    let sky_uv = dir_to_uv(normalize(normal));
    let sky_sample = textureSampleLevel(in_skybox, skybox_sampler, sky_uv, 0.0).xyz;
    let upness = saturate(normal.y * 0.5 + 0.5);
    let horizon = 1.0 - abs(normal.y);
    let hemi = 0.22 + 0.78 * upness;
    // AO is a crude proxy for "sky visibility". Use it to suppress sky/ambient in enclosed interiors.
    let ao_clamped = saturate(ao);
    let ao2 = ao_clamped * ao_clamped;
    let sky_mix = camera.ambient_color.w * ao2;
    let ambient_source = mix(camera.ambient_color.xyz, sky_sample, sky_mix);
    return ambient_source * (hemi + horizon * 0.12) * ao_clamped;
}

// ============== MAIN DEFERRED LIGHTING PASS ==============

fn get_ray(uv: vec2<f32>) -> Ray {
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    var view = camera.inv_proj * clip; view = view / view.w;
    let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
    let origin = camera.cam_pos.xyz;
    let dir = normalize(world_target - origin);
    return Ray(origin, dir, 1.0 / dir);
}

fn tile_index_for_pixel(pixel: vec2<u32>) -> u32 {
    let tile_coord = min(
        pixel / tile_light_params.tile_size,
        vec2<u32>(
            max(tile_light_params.tiles_x, 1u) - 1u,
            max(tile_light_params.tiles_y, 1u) - 1u,
        ),
    );
    return tile_coord.y * tile_light_params.tiles_x + tile_coord.x;
}

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let size = textureDimensions(in_depth);
    if (global_id.x >= size.x || global_id.y >= size.y) { return; }
    
    let uv = (vec2<f32>(f32(global_id.x), f32(global_id.y)) + 0.5) / vec2<f32>(f32(size.x), f32(size.y));
    let sky_color = vec4<f32>(uv.x * 0.3, uv.y * 0.3, 0.4, 1.0);

    let depth = textureLoad(in_depth, global_id.xy, 0).r;
    if (depth >= 50000.0) {
        let ray = get_ray(uv);
        let sky_uv = dir_to_uv(ray.dir);
        let sky_sample = textureSampleLevel(in_skybox, skybox_sampler, sky_uv, 0.0);
        textureStore(out_color, global_id.xy, sky_sample);
        return;
    }
    
    let normal_data = textureLoad(in_normal, global_id.xy, 0);
    let normal = normal_data.xyz;
    let ambient_occlusion = clamp(normal_data.w, 0.0, 1.0);
    let mat_data = textureLoad(in_material, global_id.xy, 0);
    
    let palette_idx = u32(mat_data.x + 0.5);
    let receiver_shadow_group_id = u32(mat_data.y + 0.5);
    let receiver_shadow_seam_epsilon = max(mat_data.z, 0.0);
    let material_base = u32(mat_data.w + 0.5);
    
    let mat_idx = material_base + palette_idx * 4u;
    let mat_packed = materials[mat_idx];
    let base_color = srgb_to_linear(mat_packed.xyz);
    let emissive_linear = srgb_to_linear(materials[mat_idx + 1u].xyz);
    let pbr_params = materials[mat_idx + 2u];
    let material_extra = materials[mat_idx + 3u];
    let roughness = clamp(pbr_params.x, 0.0, 1.0);
    let metalness = clamp(pbr_params.y, 0.0, 1.0);
    let ior = pbr_params.z;
    let emissive = emissive_linear * max(material_extra.x, 0.0);
    
    // Use stored voxel center from G-Buffer to ensure uniform lighting per voxel (blocky look)
    let hit_pos_ws = textureLoad(in_position, global_id.xy, 0).xyz;
    
    let view_dir = normalize(camera.cam_pos.xyz - hit_pos_ws);
    let NdotV = max(dot(normal, view_dir), 0.0);
    let dielectric_f0 = dielectric_f0_from_ior(ior);
    let F0 = mix(dielectric_f0, base_color, metalness);
    let ambient_fresnel = fresnel_schlick_roughness(NdotV, F0, roughness);
    let ambient_kd = (vec3<f32>(1.0) - ambient_fresnel) * (1.0 - metalness);
    let ambient_light = sample_directional_sky_ambient(normal, ambient_occlusion);
    let ambient_term = (ambient_kd * base_color + ambient_fresnel) * ambient_light;
    let indirect_color = ambient_term * ambient_occlusion;
    let emissive_term = emissive;
    var direct_color = vec3<f32>(0.0);
    let tile_header = tile_light_headers[tile_index_for_pixel(global_id.xy)];

    for (var i = 0u; i < tile_header.count; i++) {
        let light_idx = tile_light_indices[tile_header.offset + i];
        let contribution = calculate_lighting(hit_pos_ws, normal, view_dir, base_color, roughness, metalness, ior, receiver_shadow_group_id, receiver_shadow_seam_epsilon, light_idx);
        direct_color += contribution.color;
    }
    let final_color = indirect_color + direct_color + emissive_term;
    
    // Render modes
    if (camera.render_mode == 1u) {
        // Albedo only
        textureStore(out_color, global_id.xy, vec4<f32>(base_color, 1.0));
        return;
    }
    if (camera.render_mode == 2u) {
        // Normals visualization
        let nvis = normal * 0.5 + 0.5;
        textureStore(out_color, global_id.xy, vec4<f32>(nvis, 1.0));
        return;
    }
    if (camera.render_mode == 3u) {
        // G-Buffer visualization in 2x2 tiles
        var dbg = vec3<f32>(0.0);
        if (uv.y < 0.5) {
            if (uv.x < 0.5) {
                // Top-left: normals
                dbg = normal * 0.5 + 0.5;
            } else {
                // Top-right: depth
                let depth_vis = clamp(depth / 1000.0, 0.0, 1.0);
                dbg = vec3<f32>(depth_vis);
            }
        } else {
            if (uv.x < 0.5) {
                // Bottom-left: position (remapped)
                dbg = clamp(hit_pos_ws * 0.01 + vec3<f32>(0.5), vec3<f32>(0.0), vec3<f32>(1.0));
            } else {
                // Bottom-right: albedo
                dbg = base_color;
            }
        }
        textureStore(out_color, global_id.xy, vec4<f32>(dbg, 1.0));
        return;
    }
    if (camera.render_mode == 4u) {
        textureStore(out_color, global_id.xy, vec4<f32>(direct_color, 1.0));
        return;
    }
    if (camera.render_mode == 5u) {
        textureStore(out_color, global_id.xy, vec4<f32>(indirect_color, 1.0));
        return;
    }
    if (camera.render_mode == 6u) {
        textureStore(out_color, global_id.xy, vec4<f32>(light_density_heatmap(tile_header.count), 1.0));
        return;
    }
    textureStore(out_color, global_id.xy, vec4<f32>(final_color, 1.0));
}
