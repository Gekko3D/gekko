// voxelrt/shaders/raytrace.wgsl

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const MICRO_SIZE: f32 = 2.0;
const EPS: f32 = 1e-5;
const EMPTY_VOXEL: u32 = 0u;

// ============== STRUCTS ==============

struct CameraData {
    view_proj: mat4x4<f32>,
    inv_view: mat4x4<f32>,
    inv_proj: mat4x4<f32>,
    cam_pos: vec4<f32>,
    light_pos: vec4<f32>,
    ambient_color: vec4<f32>,
    debug_mode: u32,
    pad0: u32,
    pad1: u32,
    pad2: u32,
};

struct Instance {
    object_to_world: mat4x4<f32>,
    world_to_object: mat4x4<f32>,
    aabb_min: vec4<f32>,
    aabb_max: vec4<f32>,
    local_aabb_min: vec4<f32>,
    local_aabb_max: vec4<f32>,
    object_id: u32,
    padding: array<u32, 3>,
};

struct BVHNode {
    aabb_min: vec4<f32>,
    aabb_max: vec4<f32>,
    left: i32,
    right: i32,
    leaf_first: i32,
    leaf_count: i32,
    padding: vec4<i32>,
};

struct SectorRecord {
    origin_vox: vec4<i32>,
    sector_id: u32,       // first_brick_index
    brick_mask_lo: u32,
    brick_mask_hi: u32,
    padding: u32,
};

struct BrickRecord {
    atlas_offset: u32,
    occupancy_mask_lo: u32,
    occupancy_mask_hi: u32,
    flags: u32,
};

struct Tree64Node {
    mask_lo: u32,
    mask_hi: u32,
    child_ptr: u32,
    data: u32,
};

struct Ray {
    origin: vec3<f32>,
    dir: vec3<f32>,
    inv_dir: vec3<f32>,
};

struct HitResult {
    hit: bool,
    t: f32,
    palette_idx: u32,
    material_base: u32,
    normal: vec3<f32>,
    shading_pos: vec3<f32>,
};

struct ObjectParams {
    sector_table_base: u32,
    brick_table_base: u32,
    payload_base: u32,
    material_table_base: u32,
    tree64_base: u32,
    lod_threshold: f32,
    sector_count: u32,
    padding: u32,
};

struct Light {
    position: vec4<f32>,
    direction: vec4<f32>,
    color: vec4<f32>,
    params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
};

// ============== BIND GROUPS ==============

// Group 0: Scene
@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var<storage, read> nodes: array<BVHNode>;
@group(0) @binding(3) var<storage, read> lights: array<Light>;

// PBR-lite lighting calculation
fn calculate_lighting(
    hit_pos: vec3<f32>, 
    normal: vec3<f32>, 
    view_dir: vec3<f32>,
    base_color: vec3<f32>,
    emissive: vec3<f32>,
    roughness: f32,
    metalness: f32
) -> vec3<f32> {
    let diffuse_color = base_color * (1.0 - metalness);
    let ambient = camera.ambient_color.xyz * base_color;
    var result = ambient + emissive;

    let num_lights = arrayLength(&lights);
    for (var i = 0u; i < num_lights; i++) {
        let light = lights[i];
        var L = vec3<f32>(0.0);
        var dist_to_light = 1e9;
        var attenuation = 1.0;
        
        let light_type = u32(light.params.z);
        
        if (light_type == 1u) { // Directional
            L = -normalize(light.direction.xyz);
            attenuation = 1.0;
        } else {
            let L_vec = light.position.xyz - hit_pos;
            dist_to_light = length(L_vec);
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
                        attenuation = attenuation * spot_att;
                    }
                }
            }
        }
        
        if (attenuation > 0.0) {
            let light_dir = L;
            let half_dir = normalize(light_dir + view_dir);
            
            let NdotL = max(dot(normal, light_dir), 0.0);
            let NdotH = max(dot(normal, half_dir), 0.0);
            
            // Diffuse
            let diffuse = diffuse_color * NdotL;
            
            // Specular
            let spec_power = pow(2.0, (1.0 - roughness) * 10.0 + 1.0);
            let F0 = mix(vec3(0.04), base_color, metalness);
            let specular = pow(NdotH, spec_power) * F0;
            
            result += (diffuse + specular) * light.color.xyz * attenuation;
        }
    }
    return result;
}

// Group 1: Output
@group(1) @binding(0) var out_tex: texture_storage_2d<rgba8unorm, write>;

// Group 2: Voxel Data
@group(2) @binding(0) var<storage, read> sectors: array<SectorRecord>;
@group(2) @binding(1) var<storage, read> bricks: array<BrickRecord>;
@group(2) @binding(2) var<storage, read> voxel_payload: array<u32>;
@group(2) @binding(3) var<storage, read> materials: array<vec4<f32>>;
@group(2) @binding(4) var<storage, read> object_params: array<ObjectParams>;
@group(2) @binding(5) var<storage, read> tree64_nodes: array<Tree64Node>;

// ============== 64-BIT MASK HELPERS ==============

fn bit_test64(mask_lo: u32, mask_hi: u32, idx: u32) -> bool {
    if (idx < 32u) {
        return (mask_lo & (1u << idx)) != 0u;
    } else {
        return (mask_hi & (1u << (idx - 32u))) != 0u;
    }
}

fn popcnt64_lower(mask_lo: u32, mask_hi: u32, idx: u32) -> u32 {
    // Count set bits below idx
    if (idx == 0u) {
        return 0u;
    }
    if (idx < 32u) {
        let mask = (1u << idx) - 1u;
        return countOneBits(mask_lo & mask);
    } else if (idx == 32u) {
        return countOneBits(mask_lo);
    } else {
        let hi_mask = (1u << (idx - 32u)) - 1u;
        return countOneBits(mask_lo) + countOneBits(mask_hi & hi_mask);
    }
}

// ============== RAY HELPERS ==============

fn intersect_aabb(ray: Ray, min_b: vec3<f32>, max_b: vec3<f32>) -> vec2<f32> {
    let t0s = (min_b - ray.origin) * ray.inv_dir;
    let t1s = (max_b - ray.origin) * ray.inv_dir;
    let tsmaller = min(t0s, t1s);
    let tbigger = max(t0s, t1s);
    let tmin = max(tsmaller.x, max(tsmaller.y, tsmaller.z));
    let tmax = min(tbigger.x, min(tbigger.y, tbigger.z));
    return vec2<f32>(tmin, tmax);
}

fn step_to_next_cell(p: vec3<f32>, dir: vec3<f32>, inv_dir: vec3<f32>, cell_size: f32) -> f32 {
    let cell = floor(p / cell_size);
    let next_bound = select(cell * cell_size, (cell + 1.0) * cell_size, dir > vec3<f32>(0.0));
    let t_to_bound = (next_bound - p) * inv_dir;
    
    // Select minimal positive t
    var t_min = 1e20f;
    if (abs(dir.x) > 1e-6 && t_to_bound.x > 0.0) { t_min = min(t_min, t_to_bound.x); }
    if (abs(dir.y) > 1e-6 && t_to_bound.y > 0.0) { t_min = min(t_min, t_to_bound.y); }
    if (abs(dir.z) > 1e-6 && t_to_bound.z > 0.0) { t_min = min(t_min, t_to_bound.z); }
    
    return t_min + EPS;
}

fn load_u8(byte_offset: u32) -> u32 {
    let word_idx = byte_offset / 4u;
    let byte_idx = byte_offset % 4u;
    let word = voxel_payload[word_idx];
    return (word >> (byte_idx * 8u)) & 0xFFu;
}

// ============== RAY GENERATION ==============

fn get_ray(uv: vec2<f32>) -> Ray {
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    var view = camera.inv_proj * clip;
    view = view / view.w;
    let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
    let origin = camera.cam_pos.xyz;
    let dir = normalize(world_target - origin);
    return Ray(origin, dir, 1.0 / dir);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
    let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
    let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
    return Ray(new_origin, new_dir, 1.0 / new_dir);
}

// ============== SECTOR LOOKUP ==============

var<private> g_cached_sector_id: i32 = -1;
var<private> g_cached_sector_coords: vec3<i32> = vec3<i32>(-999, -999, -999);
var<private> g_cached_sector_base: u32 = 0xFFFFFFFFu;

fn find_sector_cached(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
    if (sx == g_cached_sector_coords.x && sy == g_cached_sector_coords.y && sz == g_cached_sector_coords.z && 
        params.sector_table_base == g_cached_sector_base && g_cached_sector_id != -1) {
        return g_cached_sector_id;
    }
    
    let sid = find_sector(sx, sy, sz, params);
    g_cached_sector_id = sid;
    g_cached_sector_coords = vec3<i32>(sx, sy, sz);
    g_cached_sector_base = params.sector_table_base;
    return sid;
}

fn find_sector(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
    let limit = params.sector_table_base + params.sector_count;
    let expected_origin = vec4<i32>(sx, sy, sz, 0) * 32;
    for (var i = params.sector_table_base; i < limit; i = i + 1u) {
        let s = sectors[i];
        if (s.origin_vox.x == expected_origin.x && 
            s.origin_vox.y == expected_origin.y && 
            s.origin_vox.z == expected_origin.z) {
            return i32(i);
        }
    }
    return -1;
}

// ============== DDA HELPERS ==============

fn get_axis_normal(axis_idx: i32, dir: vec3<f32>) -> vec3<f32> {
    var n = vec3<f32>(0.0);
    if (axis_idx == 0) { n.x = -sign(dir.x); }
    else if (axis_idx == 1) { n.y = -sign(dir.y); }
    else { n.z = -sign(dir.z); }
    return n;
}

// ============== VOXEL SAMPLING ==============

fn sample_occupancy(v: vec3<i32>, params: ObjectParams) -> f32 {
    
    let sx = v.x / 32;
    let sy = v.y / 32;
    let sz = v.z / 32;
    
    let sector_idx = find_sector_cached(sx, sy, sz, params);
    if (sector_idx < 0) { return 0.0; }
    
    let sector = sectors[sector_idx];
    let bvid = vec3<u32>(v / 8) % 4u;
    let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;
    
    if (!bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) { return 0.0; }
    
    let packed_idx = params.brick_table_base + sector.sector_id + popcnt64_lower(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local);
    let brick = bricks[packed_idx];
    
    if (brick.flags == 0u) {
        let mvid = vec3<u32>(v / 2) % 4u;
        let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
        
        if (!bit_test64(brick.occupancy_mask_lo, brick.occupancy_mask_hi, micro_idx)) { return 0.0; }
        
        let v_idx_local = vec3<u32>(v) % 8u;
        let voxel_idx = v_idx_local.x + v_idx_local.y * 8u + v_idx_local.z * 64u;
        let palette_idx = load_u8(params.payload_base + brick.atlas_offset + voxel_idx);
        
        return select(0.0, 1.0, palette_idx != EMPTY_VOXEL);
    }
    return 1.0; // Assume solid for complex bricks
}

fn get_density(v: vec3<i32>, params: ObjectParams) -> f32 {
    var d = 0.0;
    for (var x = 0; x <= 1; x++) {
        for (var y = 0; y <= 1; y++) {
            for (var z = 0; z <= 1; z++) {
                d += sample_occupancy(v + vec3<i32>(x, y, z), params);
            }
        }
    }
    return d / 8.0;
}

fn estimate_normal(p: vec3<f32>, params: ObjectParams) -> vec3<f32> {
    let vi = vec3<i32>(floor(p));
    
    // 6-tap central difference gradient on occupancy
    let dx = sample_occupancy(vi + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
    let dy = sample_occupancy(vi + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
    let dz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
    
    let grad = vec3<f32>(dx, dy, dz);
    if (length(grad) < 0.01) { return vec3<f32>(0.0); }
    return -normalize(grad);
}



// ============== XBRICKMAP TRAVERSAL ==============

fn traverse_xbrickmap(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 1e20, 0u, 0u, vec3<f32>(0.0), vec3<f32>(0.0));
    
    // Load object params for base offsets
    let params = object_params[object_id];
    
    // Transform ray to object space
    let ray = transform_ray(ray_ws, inst.world_to_object);
    
    // Check AABB again in object space to get safe ranges
    let t_obj = intersect_aabb(ray, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
    var t_start = max(t_obj.x, 0.0);
    let t_end = t_obj.y;
    
    if (t_start >= t_end) { return result; }
    // Nudge t_start slightly to ensure we are inside for DDA setup
    t_start = max(t_start, 0.0) + EPS;

    // --- DDA SETUP (Hierarchical) ---
    // Make sure direction is not zero
    let dir = ray.dir + vec3<f32>(1e-5) * vec3<f32>(ray.dir == vec3<f32>(0.0));
    let inv_dir = 1.0 / dir;
    let step = vec3<i32>(sign(dir));
    let step_f = vec3<f32>(step);
    
    // Delta T for each level
    let t_delta_sector = abs(SECTOR_SIZE * inv_dir);
    let t_delta_brick = abs(BRICK_SIZE * inv_dir);
    let t_delta_micro = abs(MICRO_SIZE * inv_dir);

    // Initial Position
    var t_curr = t_start;
    var p_curr = ray.origin + dir * t_curr;

    // SECTOR DDA INITIALIZATION
    var sector_pos = vec3<i32>(floor(p_curr / SECTOR_SIZE));
    // Determine t_max for sector
    var dist_to_bound_sector = (vec3<f32>(sector_pos) * SECTOR_SIZE + select(vec3<f32>(0.0), vec3<f32>(SECTOR_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;
    // If t_max is behind us (because of precision), advance it? 
    // Actually we can compute t_max relative to p_curr:
    // dist = (bound - p_curr) / dir
    // t_max = t_curr + dist. 
    // Standard DDA usually tracks t_max from Origin. Let's stick to that.
    var t_max_sector = dist_to_bound_sector;

    // Adjust negative logic if needed (DDA standard doesn't need it if formula is correct)
    // Avoid inf issues? t_delta handles inf. 

    // OUTER LOOP: Sectors
    var iter_sectors = 0;
    while (t_curr < t_end && iter_sectors < 64) {
        iter_sectors += 1;

        // Check sector validity
        let sector_idx = find_sector_cached(sector_pos.x, sector_pos.y, sector_pos.z, params);
            
        if (sector_idx >= 0) {
            // SECTOR HIT -> Traverse Bricks
            let sector = sectors[sector_idx];
            let sector_origin = vec3<f32>(sector.origin_vox.xyz);
            
            // Calculate exit t for this sector
            var t_sector_exit = min(min(t_max_sector.x, t_max_sector.y), t_max_sector.z);
            t_sector_exit = min(t_sector_exit, t_end);

            // BRICK DDA INIT
            var t_brick = t_curr;
            let p_brick_start = ray.origin + dir * t_brick;
            
            // Local pos in sector
            let p_in_sector = p_brick_start - sector_origin;
            var brick_pos = vec3<i32>(floor(p_in_sector / BRICK_SIZE));
            brick_pos = clamp(brick_pos, vec3<i32>(0), vec3<i32>(3));

            // t_max for bricks
            var dist_to_bound_brick = (sector_origin + vec3<f32>(brick_pos) * BRICK_SIZE + select(vec3<f32>(0.0), vec3<f32>(BRICK_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;
            var t_max_brick = dist_to_bound_brick;
            
            // MIDDLE LOOP: Bricks
            var iter_bricks = 0;
            while (t_brick < t_sector_exit && iter_bricks < 64) {
                iter_bricks += 1;
                
                if (all(brick_pos >= vec3<i32>(0)) && all(brick_pos < vec3<i32>(4))) {
                       let bvid = vec3<u32>(brick_pos);
                       let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;
                       
                       if (bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
                           // BRICK HIT -> Traverse Micros/Voxels
                           let packed_idx = params.brick_table_base + sector.sector_id + popcnt64_lower(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local);
                           let brick = bricks[packed_idx];
                           
                           // Determine t_exit for brick
                           var t_brick_exit = min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z);
                           t_brick_exit = min(t_brick_exit, t_sector_exit);
                           
                           var t_micro = t_brick;
                           let p_micro_start = ray.origin + dir * t_micro;
                           let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
                           
                           // DDA for Voxels (size 1.0)
                           // Assuming flags==0 (uncompressed/simple). If not 0, fallback or skip?
                           // User prompt implies we check single voxels.
                           if (brick.flags == 0u) {
                               let p_in_brick = p_micro_start - brick_origin;
                               var voxel_pos = vec3<i32>(floor(p_in_brick)); // size 1.0
                               voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
                               
                               var dist_to_bound_u = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray.origin) * inv_dir;
                               var t_max_micro = dist_to_bound_u;
                               let t_delta_1 = abs(1.0 * inv_dir);

                               // INNER LOOP: Voxels
                               var iter_micro = 0;
                               var last_axis = 0;
                               
                               // Special case: check first voxel immediately? DDA usually checks then steps.
                               while (t_micro < t_brick_exit && iter_micro < 32) {
                                   iter_micro += 1;
                                   
                                   let vvid = vec3<u32>(voxel_pos);
                                   let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                                   
                                   // Check Micro Mask (2.0 size blocks)
                                   let mvid = vvid / 2u;
                                   let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
                                   // Optimization: check micro mask first? 
                                   // Yes. If micro mask bit is 0, we can skip 2.0 units?
                                   // For simplicity, just check bit.
                                   
                                   if (bit_test64(brick.occupancy_mask_lo, brick.occupancy_mask_hi, micro_idx)) {
                                       let actual_atlas_offset = params.payload_base + brick.atlas_offset + voxel_idx;
                                       let palette_idx = load_u8(actual_atlas_offset);
                                       
                                       if (palette_idx != EMPTY_VOXEL) {
                                            result.hit = true;
                                            result.t = t_micro;
                                            result.palette_idx = palette_idx;
                                            result.material_base = params.material_table_base;
                                            
                                            // Normal
                                            let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                                            let p_shading_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                            
                                            let smooth_n = estimate_normal(voxel_center_os, params);
                                            var final_n = smooth_n;
                                            if (length(smooth_n) < 0.01) {
                                                final_n = vec3<f32>(0.0, 1.0, 0.0);
                                            }
                                            
                                            result.normal = normalize((inst.object_to_world * vec4<f32>(final_n, 0.0)).xyz);
                                            result.shading_pos = p_shading_ws;
                                            
                                            return result;
                                       }
                                   }

                                   // Step Voxel DDA
                                   last_axis = 0;
                                   if (t_max_micro.x < t_max_micro.y) {
                                       if (t_max_micro.x < t_max_micro.z) {
                                           voxel_pos.x += step.x;
                                           t_micro = t_max_micro.x;
                                           t_max_micro.x += t_delta_1.x;
                                           last_axis = 0;
                                       } else {
                                           voxel_pos.z += step.z;
                                           t_micro = t_max_micro.z;
                                           t_max_micro.z += t_delta_1.z;
                                           last_axis = 2;
                                       }
                                   } else {
                                       if (t_max_micro.y < t_max_micro.z) {
                                           voxel_pos.y += step.y;
                                           t_micro = t_max_micro.y;
                                           t_max_micro.y += t_delta_1.y;
                                           last_axis = 1;
                                       } else {
                                           voxel_pos.z += step.z;
                                           t_micro = t_max_micro.z;
                                           t_max_micro.z += t_delta_1.z;
                                           last_axis = 2;
                                       }
                                   }
                                   
                                   if (t_micro >= t_brick_exit) { break; }
                                   // Check bounds of brick (0..7)
                                   if (any(voxel_pos < vec3<i32>(0)) || any(voxel_pos > vec3<i32>(7))) { break; }
                               }
                           }
                           
                           // If we exit micro loop but no hit, we continue brick loop.
                           // Important: t_brick must be updated to the exit of the micro loop?
                           // The micro loop steps t_micro until it exits brick or hits.
                           // So we should sync t_brick = t_micro?
                           // Actually, simplest is:
                           // t_brick = t_brick_exit; // Skip to next brick
                           // Because if we scanned everything in this brick and found nothing, move on.
                       }
                       // If brick empty, just step.
                    }

                    // Step Brick DDA
                    if (t_max_brick.x < t_max_brick.y) {
                        if (t_max_brick.x < t_max_brick.z) {
                            brick_pos.x += step.x;
                            t_brick = t_max_brick.x;
                            t_max_brick.x += t_delta_brick.x;
                        } else {
                            brick_pos.z += step.z;
                            t_brick = t_max_brick.z;
                            t_max_brick.z += t_delta_brick.z;
                        }
                    } else {
                        if (t_max_brick.y < t_max_brick.z) {
                            brick_pos.y += step.y;
                            t_brick = t_max_brick.y;
                            t_max_brick.y += t_delta_brick.y;
                        } else {
                            brick_pos.z += step.z;
                            t_brick = t_max_brick.z;
                            t_max_brick.z += t_delta_brick.z;
                        }
                    }
                    
                    if (t_brick >= t_sector_exit) { break; }
                    if (any(brick_pos < vec3<i32>(0)) || any(brick_pos > vec3<i32>(3))) { break; }
            }
        }
        
        // Step Sector DDA
        if (t_max_sector.x < t_max_sector.y) {
            if (t_max_sector.x < t_max_sector.z) {
                sector_pos.x += step.x;
                t_curr = t_max_sector.x;
                t_max_sector.x += t_delta_sector.x;
            } else {
                sector_pos.z += step.z;
                t_curr = t_max_sector.z;
                t_max_sector.z += t_delta_sector.z;
            }
        } else {
            if (t_max_sector.y < t_max_sector.z) {
                sector_pos.y += step.y;
                t_curr = t_max_sector.y;
                t_max_sector.y += t_delta_sector.y;
            } else {
                sector_pos.z += step.z;
                t_curr = t_max_sector.z;
                t_max_sector.z += t_delta_sector.z;
            }
        }
    }
    
    return result;
}

fn traverse_tree64(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 1e20, 0u, 0u, vec3<f32>(0.0), vec3<f32>(0.0));
    let params = object_params[object_id];
    
    if (params.tree64_base == 0xFFFFFFFFu) {
        return result;
    }
    
    let ray = transform_ray(ray_ws, inst.world_to_object);
    let os_min = inst.local_aabb_min.xyz;
    let os_max = inst.local_aabb_max.xyz;
    
    let t_obj = intersect_aabb(ray, os_min, os_max);
    var t = max(t_obj.x, 0.0) + EPS;
    let t_max_obj = t_obj.y;
    
    if (t >= t_max_obj) { return result; }
    
    let root = tree64_nodes[params.tree64_base];
    
    var iterations = 0;
    while (t < t_max_obj && iterations < 64) {
        iterations = iterations + 1;
        let p = ray.origin + ray.dir * t;
        
        let lx = u32(floor(p.x / 32.0)) % 4u;
        let ly = u32(floor(p.y / 32.0)) % 4u;
        let lz = u32(floor(p.z / 32.0)) % 4u;
        let bit = lx + ly*4u + lz*16u;
        
        if (bit_test64(root.mask_lo, root.mask_hi, bit)) {
            let l1_idx = params.tree64_base + root.child_ptr + popcnt64_lower(root.mask_lo, root.mask_hi, bit);
            let l1_node = tree64_nodes[l1_idx];
            
            let bx = u32(floor(p.x / 8.0)) % 4u;
            let by = u32(floor(p.y / 8.0)) % 4u;
            let bz = u32(floor(p.z / 8.0)) % 4u;
            let b_bit = bx + by*4u + bz*16u;
            
            if (bit_test64(l1_node.mask_lo, l1_node.mask_hi, b_bit)) {
                result.hit = true;
                result.t = t;
                result.palette_idx = l1_node.data; 
                result.material_base = params.material_table_base;
                
                let voxel_center_os = floor(p / 8.0) * 8.0 + 4.0;
                let p_shading_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;

                let smooth_n = estimate_normal(voxel_center_os, params);
                var final_n = smooth_n;
                if (length(smooth_n) < 0.01) {
                    final_n = vec3<f32>(0.0, 1.0, 0.0);
                }
                
                result.normal = normalize((inst.object_to_world * vec4<f32>(final_n, 0.0)).xyz); 
                result.shading_pos = p_shading_ws;
                return result;
            }
            t = t + step_to_next_cell(p, ray.dir, ray.inv_dir, 8.0);
        } else {
            t = t + step_to_next_cell(p, ray.dir, ray.inv_dir, 32.0);
        }
    }
    
    return result;
}



// ============== COLOR HELPERS ==============

fn hash_color(id: u32) -> vec3<f32> {
    let uid = id + 1u;
    let r = f32((uid * 12345u) & 255u) / 255.0;
    let g = f32((uid * 67890u) & 255u) / 255.0;
    let b = f32((uid * 54321u) & 255u) / 255.0;
    return vec3<f32>(r, g, b);
}

// ============== MAIN ==============



@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let dims = textureDimensions(out_tex);
    if (global_id.x >= dims.x || global_id.y >= dims.y) {
        return;
    }

    let uv = vec2<f32>(f32(global_id.x) / f32(dims.x), f32(global_id.y) / f32(dims.y));
    let ray = get_ray(uv);

    var closest_t = 1e20f;
    var hit_color = vec3<f32>(0.0);
    var hit_found = false;

    // TLAS Traversal
    var stack: array<i32, 64>;
    var stack_ptr = 0;
    stack[stack_ptr] = 0;
    stack_ptr += 1;

    if (arrayLength(&nodes) == 0u) {
        stack_ptr = 0;
    }

    var iterations_tlas = 0;
    let MAX_TLAS_ITERATIONS = 64;

    while (stack_ptr > 0) {
        if (iterations_tlas >= MAX_TLAS_ITERATIONS) { break; }
        iterations_tlas = iterations_tlas + 1;

        stack_ptr -= 1;
        let node_idx = stack[stack_ptr];
        let node = nodes[node_idx];

        let t_vals = intersect_aabb(ray, node.aabb_min.xyz, node.aabb_max.xyz);
        let t_min = t_vals.x;
        let t_max = t_vals.y;

        if (t_min <= t_max && t_max > 0.0 && t_min < closest_t) {
            if (node.leaf_count > 0) {
                let inst_idx = node.leaf_first;
                let inst = instances[inst_idx];
                
                let t_inst = intersect_aabb(ray, inst.aabb_min.xyz, inst.aabb_max.xyz);
                if (t_inst.x <= t_inst.y && t_inst.y > 0.0 && t_inst.x < closest_t) {
                    let params = object_params[inst.object_id];
                    let dist = distance(camera.cam_pos.xyz, inst.aabb_min.xyz); // Coarse distance
                    
                    var voxel_hit: HitResult;
                    if (dist > params.lod_threshold && params.tree64_base != 0xFFFFFFFFu) {
                        voxel_hit = traverse_tree64(ray, inst, t_inst.x, t_inst.y, inst.object_id);
                    } else {
                        voxel_hit = traverse_xbrickmap(ray, inst, t_inst.x, t_inst.y, inst.object_id);
                    }
                    
                    if (voxel_hit.hit && voxel_hit.t < closest_t) {
                        closest_t = voxel_hit.t;
                        
                        var base_color = vec3<f32>(0.0);
                        var emissive = vec3<f32>(0.0);
                        var roughness = 1.0;
                        var metalness = 0.0;
                        
                        let mat_idx = voxel_hit.material_base + voxel_hit.palette_idx * 4u;
                        let n_mats = arrayLength(&materials);

                        if (mat_idx + 3u < n_mats) {
                            base_color = materials[mat_idx].xyz;
                            emissive = materials[mat_idx + 1u].xyz;
                            let pbr_params = materials[mat_idx + 2u];
                            roughness = clamp(pbr_params.x, 0.0, 1.0);
                            metalness = clamp(pbr_params.y, 0.0, 1.0);
                        } else {
                            // Falling back to hash color for debug
                            base_color = hash_color(voxel_hit.palette_idx);
                        }
                        
                        // Shading
                        let hit_pos_ws = voxel_hit.shading_pos;
                        let view_dir = normalize(camera.cam_pos.xyz - hit_pos_ws);
                        
                        hit_color = calculate_lighting(
                            hit_pos_ws,
                            voxel_hit.normal,
                            view_dir,
                            base_color,
                            emissive,
                            roughness,
                            metalness
                        );

                        hit_found = true;
                    }
                }
            } else {
                if (node.left != -1) {
                    stack[stack_ptr] = node.left;
                    stack_ptr += 1;
                }
                if (node.right != -1 && stack_ptr < 64) {
                    stack[stack_ptr] = node.right;
                    stack_ptr += 1;
                }
            }
        }
    }

    var final_color = vec4<f32>(uv.x * 0.3, uv.y * 0.3, 0.4, 1.0); // Sky gradient
    
    if (hit_found) {
        final_color = vec4<f32>(hit_color, 1.0);
    }


    textureStore(out_tex, vec2<i32>(global_id.xy), final_color);
}
