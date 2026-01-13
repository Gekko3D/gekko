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
};

struct Light {
    position: vec4<f32>,
    direction: vec4<f32>,
    color: vec4<f32>,
    params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
};

struct Ray {
    origin: vec3<f32>,
    dir: vec3<f32>,
    inv_dir: vec3<f32>,
};

// ============== BIND GROUPS ==============

// Group 0: Scene
@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> lights: array<Light>;

// Group 1: G-Buffer Input
@group(1) @binding(0) var in_depth: texture_2d<f32>;
@group(1) @binding(1) var in_normal: texture_2d<f32>;
@group(1) @binding(2) var in_material: texture_2d<f32>;
@group(1) @binding(3) var in_position: texture_2d<f32>;

// Output: Final Color
@group(1) @binding(4) var out_color: texture_storage_2d<rgba8unorm, write>;

// Shadow Maps
@group(1) @binding(5) var in_shadow_maps: texture_2d_array<f32>;

// Group 2: Voxel Data (reuse)
@group(2) @binding(3) var<storage, read> materials: array<vec4<f32>>;

// ============== LIGHTING CALCULATION ==============

fn calculate_lighting(
    hit_pos: vec3<f32>, 
    normal: vec3<f32>, 
    view_dir: vec3<f32>,
    base_color: vec3<f32>,
    emissive: vec3<f32>,
    roughness: f32,
    metalness: f32,
    light_idx: u32
) -> vec3<f32> {
    let diffuse_color = base_color * (1.0 - metalness);
    let light = lights[light_idx];
    var L = vec3<f32>(0.0);
    var attenuation = 1.0;
    let light_type = u32(light.params.z);

    // Early debug overlay: show directional (sun) shadow center sample regardless of attenuation
    if (camera.debug_mode == 2u && light_type == 1u) {
        let pos_ls = light.view_proj * vec4<f32>(hit_pos, 1.0);
        let proj_pos = pos_ls.xyz / pos_ls.w;
        let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);
        let tex_dim = textureDimensions(in_shadow_maps);
        let base_px_f = shadow_uv * vec2<f32>(f32(tex_dim.x), f32(tex_dim.y));
        let base_px = vec2<i32>(
            i32(clamp(base_px_f.x, 0.0, f32(tex_dim.x - 1u))),
            i32(clamp(base_px_f.y, 0.0, f32(tex_dim.y - 1u)))
        );
        let layer = i32(light_idx);
        let center_depth = textureLoad(in_shadow_maps, base_px, layer, 0).r;
        let val = 1.0 - clamp(center_depth * 0.5 + 0.5, 0.0, 1.0);
        return vec3<f32>(val, 0.0, 0.0);
    }
    
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
            let factor = dist_sq / (range * range);
            let smooth_factor = max(0.0, 1.0 - factor * factor);
            let inv_sq = 1.0 / (dist_sq + 1.0);
            attenuation = inv_sq * smooth_factor * smooth_factor * light.color.w * 50.0;
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
        var pos_ws = hit_pos;
        if (light_type == 1u) {
            let receiver_offset = 0.25;
            pos_ws = hit_pos + normal * receiver_offset;
        }
        let pos_ls = light.view_proj * vec4<f32>(pos_ws, 1.0);
        let proj_pos = pos_ls.xyz / pos_ls.w;
        let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);

        if (pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0) {
            let tex_dim = textureDimensions(in_shadow_maps);
            let base_px_f = shadow_uv * vec2<f32>(f32(tex_dim.x), f32(tex_dim.y));
            let base_px = vec2<i32>(
                i32(clamp(base_px_f.x, 0.0, f32(tex_dim.x - 1u))),
                i32(clamp(base_px_f.y, 0.0, f32(tex_dim.y - 1u)))
            );
            let layer = i32(light_idx);

            var my_depth_n = clamp(proj_pos.z, -1.0, 1.0);
            var my_depth_m = 0.0;
            if (light_type == 2u) {
                // For spot lights, compare in linear distance space (meters), offset along normal to reduce self-shadow flicker
                let receiver_offset = 0.25;
                let pos_off = hit_pos + normal * receiver_offset;
                my_depth_m = distance(light.position.xyz, pos_off);
            }

            // Slope-scaled depth bias with resolution scaling; stronger for spot lights
            var baseBias = 1.5 / f32(tex_dim.x);
            var slopeBias = 0.002;
            if (light_type == 2u) { // Spot light
                baseBias = 3.0 / f32(tex_dim.x);
                slopeBias = 0.01;
            }
            let NdL = max(dot(normal, L), 0.0);
            let bias = baseBias + slopeBias * (1.0 - NdL);


            // PCF with radius dependent on light type (spot uses 5x5, others 3x3)
            let max_px = vec2<i32>(i32(tex_dim.x) - 1, i32(tex_dim.y) - 1);
            var visibility = 0.0;
            var radius: i32 = 1;
            if (light_type == 2u) { radius = 2; }
            let kernel = (radius * 2 + 1);
            let sample_count = f32(kernel * kernel);
            for (var dy: i32 = -radius; dy <= radius; dy = dy + 1) {
                for (var dx: i32 = -radius; dx <= radius; dx = dx + 1) {
                    let off = base_px + vec2<i32>(dx, dy);
                    let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
                    var sd = textureLoad(in_shadow_maps, clamped_off, layer, 0).r;
                    if (light_type == 2u) {
                        // Linear distance comparison for spot lights (meters)
                        let baseBiasM = 0.05;
                        let slopeBiasM = 0.1;
                        let biasM = baseBiasM + slopeBiasM * (1.0 - NdL);
                        visibility += select(0.0, 1.0, sd >= my_depth_m - biasM);
                    } else {
                        // NDC comparison for directional/others
                        sd = clamp(sd, -1.0, 1.0);
                        visibility += select(0.0, 1.0, sd >= my_depth_n - bias);
                    }
                }
            }
            visibility = visibility / sample_count;
            attenuation *= visibility;
        }

        let half_dir = normalize(L + view_dir);
        let NdotL = max(dot(normal, L), 0.0);
        let NdotH = max(dot(normal, half_dir), 0.0);
        let diffuse = diffuse_color * NdotL;
        let spec_power = pow(2.0, (1.0 - roughness) * 10.0 + 1.0);
        let F0 = mix(vec3(0.04), base_color, metalness);
        let specular = pow(NdotH, spec_power) * F0;
        return (diffuse + specular) * light.color.xyz * attenuation * light.color.w;
    }
    return vec3<f32>(0.0);
}

fn ndc_for_shadow(uv: vec2<f32>) -> vec2<f32> {
    return vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
}

// NOTE: We need inverse() in deferred_lighting too if we use this logic.
// Or we just store world-space distance in shadow map.
// Actually, shadow_dist IS world-space distance from light's "near plane".

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

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let size = textureDimensions(in_depth);
    if (global_id.x >= size.x || global_id.y >= size.y) { return; }
    
    let uv = (vec2<f32>(f32(global_id.x), f32(global_id.y)) + 0.5) / vec2<f32>(f32(size.x), f32(size.y));
    let sky_color = vec4<f32>(uv.x * 0.3, uv.y * 0.3, 0.4, 1.0);

    let depth = textureLoad(in_depth, global_id.xy, 0).r;
    if (depth >= 50000.0) {
        textureStore(out_color, global_id.xy, sky_color);
        return;
    }
    
    let normal = textureLoad(in_normal, global_id.xy, 0).xyz;
    let mat_data = textureLoad(in_material, global_id.xy, 0);
    
    let palette_idx = u32(mat_data.x + 0.5);
    let material_base = u32(mat_data.w + 0.5);
    
    let mat_idx = material_base + palette_idx * 4u;
    let mat_packed = materials[mat_idx];
    let base_color = mat_packed.xyz;
    let emissive = materials[mat_idx + 1u].xyz;
    let pbr_params = materials[mat_idx + 2u];
    let roughness = clamp(pbr_params.x, 0.0, 1.0);
    let metalness = clamp(pbr_params.y, 0.0, 1.0);
    
    // Use stored voxel center from G-Buffer to ensure uniform lighting per voxel (blocky look)
    let hit_pos_ws = textureLoad(in_position, global_id.xy, 0).xyz;
    
    let view_dir = normalize(camera.cam_pos.xyz - hit_pos_ws);
    
    var final_color = base_color * camera.ambient_color.xyz + emissive;
    
    // Force use of lights and position texture to prevent stripping
    let dummy = lights[0].color.x + textureLoad(in_position, vec2<i32>(0,0), 0).x;
    
    // Loop through all lights
    let num_lights = camera.num_lights;
    for (var i = 0u; i < num_lights; i++) {
        final_color += calculate_lighting(hit_pos_ws, normal, view_dir, base_color, emissive, roughness, metalness, i);
    }
    
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
        textureStore(out_color, global_id.xy, vec4<f32>(final_color, 1.0));
}
