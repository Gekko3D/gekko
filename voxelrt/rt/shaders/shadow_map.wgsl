// voxelrt/shaders/shadow_map.wgsl

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const MICRO_SIZE: f32 = 2.0;
const EPS: f32 = 1e-4;
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

struct Light {
    position: vec4<f32>,
    direction: vec4<f32>,
    color: vec4<f32>,
    params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
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
    atlas_offset: atomic<u32>,
    occupancy_mask_lo: atomic<u32>,
    occupancy_mask_hi: atomic<u32>,
    flags: atomic<u32>,
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
    voxel_center_ws: vec3<f32>,
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

struct SectorGridEntry {
    coords: vec3<i32>,
    base_idx: u32,
    sector_idx: i32,
    paddings: array<u32, 3>,
};

struct SectorGridParams {
    grid_size: u32,
    grid_mask: u32,
    padding0: u32,
    padding1: u32,
};

// ============== BIND GROUPS ==============

@group(0) @binding(0) var<storage, read> update_indices: array<u32>;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var<storage, read> nodes: array<BVHNode>;
@group(0) @binding(3) var<storage, read> lights: array<Light>;

// Group 1: Shadow Map Output
@group(1) @binding(0) var out_shadow_map: texture_storage_2d_array<rgba32float, write>;

// Group 2: Voxel Data
@group(2) @binding(0) var<storage, read> sectors: array<SectorRecord>;
@group(2) @binding(1) var<storage, read> bricks: array<BrickRecord>;
@group(2) @binding(2) var<storage, read> voxel_payload: array<atomic<u32>>;
@group(2) @binding(4) var<storage, read> object_params: array<ObjectParams>;
@group(2) @binding(5) var<storage, read> tree64_nodes: array<Tree64Node>;
@group(2) @binding(6) var<storage, read> sector_grid: array<SectorGridEntry>;
@group(2) @binding(7) var<storage, read> sector_grid_params: SectorGridParams;

// ============== TRAVERSAL LOGIC ==============

fn bit_test64(mask_lo: u32, mask_hi: u32, idx: u32) -> bool {
    if (idx < 32u) {
        return (mask_lo & (1u << idx)) != 0u;
    } else {
        return (mask_hi & (1u << (idx - 32u))) != 0u;
    }
}

fn popcnt64_lower(mask_lo: u32, mask_hi: u32, idx: u32) -> u32 {
    if (idx == 0u) { return 0u; }
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
    var t_min = 1e20f;
    if (abs(dir.x) > 1e-6 && t_to_bound.x > 0.0) { t_min = min(t_min, t_to_bound.x); }
    if (abs(dir.y) > 1e-6 && t_to_bound.y > 0.0) { t_min = min(t_min, t_to_bound.y); }
    if (abs(dir.z) > 1e-6 && t_to_bound.z > 0.0) { t_min = min(t_min, t_to_bound.z); }
    return t_min + EPS;
}

fn load_u8(byte_offset: u32) -> u32 {
    let word_idx = byte_offset / 4u;
    let byte_idx = byte_offset % 4u;
    let word = atomicLoad(&voxel_payload[word_idx]);
    return (word >> (byte_idx * 8u)) & 0xFFu;
}

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
    let size = sector_grid_params.grid_size;
    if (size == 0u) { return -1; }
    let h = (u32(sx) * 73856093u ^ u32(sy) * 19349663u ^ u32(sz) * 83492791u ^ params.sector_table_base * 99999989u) % size;
    for (var i = 0u; i < 32u; i++) {
        let idx = (h + i) % size;
        let entry = sector_grid[idx];
        if (entry.sector_idx == -1) { return -1; }
        if (entry.coords.x == sx && entry.coords.y == sy && entry.coords.z == sz && entry.base_idx == params.sector_table_base) {
            return entry.sector_idx;
        }
    }
    return -1;
}

fn make_safe_dir(d: vec3<f32>) -> vec3<f32> {
    let eps = 1e-6;
    let sx = select(d.x, (select(1.0, -1.0, d.x < 0.0)) * eps, abs(d.x) < eps);
    let sy = select(d.y, (select(1.0, -1.0, d.y < 0.0)) * eps, abs(d.y) < eps);
    let sz = select(d.z, (select(1.0, -1.0, d.z < 0.0)) * eps, abs(d.z) < eps);
    return vec3<f32>(sx, sy, sz);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
    let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
    let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
    let safe_dir = make_safe_dir(new_dir);
    return Ray(new_origin, new_dir, 1.0 / safe_dir);
}

fn traverse_xbrickmap(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 60000.0, vec3<f32>(0.0));
    let params = object_params[object_id];
    let ray = transform_ray(ray_ws, inst.world_to_object);
    let t_obj = intersect_aabb(ray, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
    var t_start = max(t_obj.x, 0.0) + EPS;
    let t_end = t_obj.y;
    if (t_start >= t_end) { return result; }
    let dir = ray.dir;
    let inv_dir = ray.inv_dir;
    let step = vec3<i32>(sign(dir));
    let t_delta_sector = abs(SECTOR_SIZE * inv_dir);
    let t_delta_brick = abs(BRICK_SIZE * inv_dir);
    var t_curr = t_start;
    let sector_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
    var sector_pos = vec3<i32>(floor(((ray.origin + dir * t_curr) - sector_bias) / SECTOR_SIZE));
    var t_max_sector = (vec3<f32>(sector_pos) * SECTOR_SIZE + select(vec3<f32>(0.0), vec3<f32>(SECTOR_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;
    var iter_sectors = 0;
    while (t_curr < t_end && iter_sectors < 64) {
        iter_sectors += 1;
        let sector_idx = find_sector_cached(sector_pos.x, sector_pos.y, sector_pos.z, params);
        if (sector_idx >= 0) {
            let sector = sectors[sector_idx];
            let sector_origin = vec3<f32>(sector.origin_vox.xyz);
            var t_sector_exit = min(min(min(t_max_sector.x, t_max_sector.y), t_max_sector.z), t_end);
            var t_brick = t_curr;
            let brick_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
            var brick_pos = vec3<i32>(floor((((ray.origin + dir * t_brick) - sector_origin) - brick_bias) / BRICK_SIZE));
            brick_pos = clamp(brick_pos, vec3<i32>(0), vec3<i32>(3));
            var t_max_brick = (sector_origin + vec3<f32>(brick_pos) * BRICK_SIZE + select(vec3<f32>(0.0), vec3<f32>(BRICK_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;
            var iter_bricks = 0;
            while (t_brick < t_sector_exit && iter_bricks < 64) {
                iter_bricks += 1;
                if (all(brick_pos >= vec3<i32>(0)) && all(brick_pos < vec3<i32>(4))) {
                    let bvid = vec3<u32>(brick_pos);
                    let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;
                    if (bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
                        let packed_idx = params.brick_table_base + sector.sector_id + popcnt64_lower(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local);
                        let b_flags = atomicLoad(&bricks[packed_idx].flags);
                        let b_atlas = atomicLoad(&bricks[packed_idx].atlas_offset);
                        
                        var t_brick_exit = min(min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z), t_sector_exit);
                        if (b_flags == 1u) {
                            result.hit = true; result.t = t_brick;
                            let p_hit_os = ray.origin + dir * (t_brick + 0.01);
                            let voxel_center_os = floor(p_hit_os) + 0.5;
                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                            return result;
                        }
                        if (b_flags == 0u) {
                            var t_micro = t_brick;
                            let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
                            let voxel_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
                            var voxel_pos = vec3<i32>(floor(((ray.origin + dir * t_micro) - brick_origin) - voxel_bias));
                            voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
                            var t_max_micro = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray.origin) * inv_dir;
                            let t_delta_1 = abs(1.0 * inv_dir);
                            var iter_micro = 0;
                            while (t_micro < t_brick_exit && iter_micro < 32) {
                                iter_micro += 1;
                                let vvid = vec3<u32>(voxel_pos);
                                let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                                let mvid = vvid / 2u;
                                let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
                                
                                let b_mask_lo = atomicLoad(&bricks[packed_idx].occupancy_mask_lo);
                                let b_mask_hi = atomicLoad(&bricks[packed_idx].occupancy_mask_hi);
                                if (bit_test64(b_mask_lo, b_mask_hi, micro_idx)) {
                                    let actual_atlas_offset = params.payload_base + b_atlas + voxel_idx;
                                    let palette_idx = load_u8(actual_atlas_offset);
                                    if (palette_idx != EMPTY_VOXEL) {
                                        result.hit = true; result.t = t_micro;
                                        let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                                        result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                        return result;
                                    }
                                }
                                if (t_max_micro.x < t_max_micro.y) {
                                    if (t_max_micro.x < t_max_micro.z) { voxel_pos.x += step.x; t_micro = t_max_micro.x; t_max_micro.x += t_delta_1.x; }
                                    else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                                } else {
                                    if (t_max_micro.y < t_max_micro.z) { voxel_pos.y += step.y; t_micro = t_max_micro.y; t_max_micro.y += t_delta_1.y; }
                                    else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                                }
                            }
                        }
                    }
                }
                if (t_max_brick.x < t_max_brick.y) {
                    if (t_max_brick.x < t_max_brick.z) { brick_pos.x += step.x; t_brick = t_max_brick.x; t_max_brick.x += t_delta_brick.x; }
                    else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
                } else {
                    if (t_max_brick.y < t_max_brick.z) { brick_pos.y += step.y; t_brick = t_max_brick.y; t_max_brick.y += t_delta_brick.y; }
                    else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
                }
            }
        }
        if (t_max_sector.x < t_max_sector.y) {
            if (t_max_sector.x < t_max_sector.z) { sector_pos.x += step.x; t_curr = t_max_sector.x; t_max_sector.x += t_delta_sector.x; }
            else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
        } else {
            if (t_max_sector.y < t_max_sector.z) { sector_pos.y += step.y; t_curr = t_max_sector.y; t_max_sector.y += t_delta_sector.y; }
            else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
        }
    }
    return result;
}

fn traverse_tree64(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 60000.0, vec3<f32>(0.0));
    let params = object_params[object_id];
    if (params.tree64_base == 0xFFFFFFFFu) { return result; }
    let ray = transform_ray(ray_ws, inst.world_to_object);
    let t_obj = intersect_aabb(ray, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
    var t = max(t_obj.x, 0.0) + EPS;
    let t_max_obj = t_obj.y;
    if (t >= t_max_obj) { return result; }
    let root = tree64_nodes[params.tree64_base];
    var iterations = 0;
    while (t < t_max_obj && iterations < 64) {
        iterations++;
        let p = ray.origin + ray.dir * t;
        let lx = u32(floor(p.x / 32.0)) % 4u; let ly = u32(floor(p.y / 32.0)) % 4u; let lz = u32(floor(p.z / 32.0)) % 4u;
        let bit = lx + ly*4u + lz*16u;
        if (bit_test64(root.mask_lo, root.mask_hi, bit)) {
            let l1_idx = params.tree64_base + root.child_ptr + popcnt64_lower(root.mask_lo, root.mask_hi, bit);
            let l1_node = tree64_nodes[l1_idx];
            let bx = u32(floor(p.x / 8.0)) % 4u; let by = u32(floor(p.y / 8.0)) % 4u; let bz = u32(floor(p.z / 8.0)) % 4u;
            let b_bit = bx + by*4u + bz*16u;
            if (bit_test64(l1_node.mask_lo, l1_node.mask_hi, b_bit)) {
                result.hit = true; result.t = t;
                let voxel_center_os = floor(p / 8.0) * 8.0 + 4.0;
                result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                return result;
            }
            t += step_to_next_cell(p, ray.dir, ray.inv_dir, 8.0);
        } else { t += step_to_next_cell(p, ray.dir, ray.inv_dir, 32.0); }
    }
    return result;
}

fn traverse_scene(ray: Ray) -> HitResult {
    var hit_res = HitResult(false, 60000.0, vec3<f32>(0.0));
    var stack: array<i32, 64>;
    var stack_ptr = 0;
    
    let n_nodes = arrayLength(&nodes);
    if (n_nodes > 0u) {
        stack[stack_ptr] = 0;
        stack_ptr += 1;
    }
    
    var iterations = 0;
    while (stack_ptr > 0 && iterations < 128) {
        iterations++;
        stack_ptr--;
        let node_idx = stack[stack_ptr];
        if (node_idx < 0 || u32(node_idx) >= n_nodes) { continue; }
        
        let node = nodes[node_idx];
        let t_vals = intersect_aabb(ray, node.aabb_min.xyz, node.aabb_max.xyz);
        if (t_vals.x <= t_vals.y && t_vals.y > 0.0 && t_vals.x < hit_res.t) {
            if (node.leaf_count > 0) {
                var li: i32 = 0;
                loop {
                    if (li >= node.leaf_count) { break; }
                    let inst = instances[node.leaf_first + li];
                    let t_inst = intersect_aabb(ray, inst.aabb_min.xyz, inst.aabb_max.xyz);
                    if (t_inst.x <= t_inst.y && t_inst.y > 0.0 && t_inst.x < hit_res.t) {
                        var res: HitResult;
                        // Force full traversal (no LOD) for shadow map to avoid blinking/flickering
                        res = traverse_xbrickmap(ray, inst, t_inst.x, t_inst.y, inst.object_id);
                        if (res.hit && res.t < hit_res.t) { hit_res = res; }
                    }
                    li = li + 1;
                }
            } else {
                if (node.left != -1) { stack[stack_ptr] = node.left; stack_ptr++; }
                if (node.right != -1 && stack_ptr < 64) { stack[stack_ptr] = node.right; stack_ptr++; }
            }
        }
    }
    return hit_res;
}

// ============== MAIN SHADOW PASS ==============

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let tex_dim = textureDimensions(out_shadow_map);
    if (global_id.x >= tex_dim.x || global_id.y >= tex_dim.y) { return; }
    
    let update_idx = global_id.z;
    let num_updates = arrayLength(&update_indices);
    if (update_idx >= num_updates) { return; }
    
    let light_idx = update_indices[update_idx];
    
    let light = lights[light_idx];
    
    let uv = (vec2<f32>(f32(global_id.x), f32(global_id.y)) + 0.5) / vec2<f32>(f32(tex_dim.x), f32(tex_dim.y));
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    
    // Generate ray for this pixel of the shadow map
    let p_near = light.inv_view_proj * vec4<f32>(ndc, -1.0, 1.0);
    let p_far = light.inv_view_proj * vec4<f32>(ndc, 1.0, 1.0);
    
    let origin = p_near.xyz / p_near.w;
    let p_target = p_far.xyz / p_far.w;
    let dir = normalize(p_target - origin);
    
    let safe_dir = make_safe_dir(dir);
    var ray = Ray(origin, dir, 1.0 / safe_dir);
    
    // Perform traversal
    var hit_res = traverse_scene(ray);
    
    // Force usage of tree64_nodes (Binding 5) to prevent layout mismatch
    // (We disabled traverse_tree64 so the compiler stripped the binding)
    let dummy = tree64_nodes[0].mask_lo;
    if (dummy > 0xFFFF0000u) { hit_res.t += 0.00001; } // Unlikely condition, effectively no-op
    
    // Write out normalized depth from light perspective (using voxel center for blocky shadows)
    if (hit_res.hit) {
        let pos_ls = light.view_proj * vec4<f32>(hit_res.voxel_center_ws, 1.0);
        let light_type = u32(light.params.z);
        if (light_type == 2u) {
            // Spot light: store linear distance from light position (meters)
            let depth_m = distance(light.position.xyz, hit_res.voxel_center_ws);
            textureStore(out_shadow_map, global_id.xy, light_idx, vec4<f32>(depth_m, 0.0, 0.0, 0.0));
        } else {
            // Directional/others: store NDC depth
            let depth_ndc = clamp(pos_ls.z / pos_ls.w, -1.0, 1.0);
            textureStore(out_shadow_map, global_id.xy, light_idx, vec4<f32>(depth_ndc, 0.0, 0.0, 0.0));
        }
    } else {
        let light_type = u32(light.params.z);
        if (light_type == 2u) {
            // For spot, no hit -> treat as far range in meters
            textureStore(out_shadow_map, global_id.xy, light_idx, vec4<f32>(light.params.x, 0.0, 0.0, 0.0));
        } else {
            textureStore(out_shadow_map, global_id.xy, light_idx, vec4<f32>(1.0, 0.0, 0.0, 0.0));
        }
    }
}
