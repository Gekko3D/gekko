// voxelrt/shaders/shadow_map.wgsl

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const MICRO_SIZE: f32 = 2.0;
const EPS: f32 = 1e-3;
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
    shadow_meta: vec4<u32>, // x: first shadow layer, y: layer count, z: cascade count, w: reserved
    view_proj: mat4x4<f32>,
    inv_view_proj: mat4x4<f32>,
    directional_cascades: array<DirectionalShadowCascade, 2>,
};

struct ShadowUpdate {
    light_index: u32,
    shadow_layer: u32,
    cascade_index: u32,
    kind: u32,
    tier: u32,
    resolution: u32,
};

struct ShadowLayerParams {
    viewport_scale: vec2<f32>,
    effective_resolution: f32,
    inv_effective_resolution: f32,
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
    brick_table_index: u32,
    brick_mask_lo: u32,
    brick_mask_hi: u32,
    padding: u32,
};

struct BrickRecord {
    atlas_offset: u32,
    occupancy_mask_lo: u32,
    occupancy_mask_hi: u32,
    atlas_page: u32,
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
    hit_pos_ws: vec3<f32>,
    voxel_center_ws: vec3<f32>,
    shadow_group_id: u32,
};

struct ObjectParams {
    sector_table_base: u32,
    brick_table_base: u32,
    payload_base: u32,
    material_table_base: u32,
    tree64_base: u32,
    lod_threshold: f32,
    sector_count: u32,
    ambient_occlusion_mode: u32,
    shadow_group_id: u32,
    shadow_seam_epsilon: f32,
    is_terrain_chunk: u32,
    terrain_group_id: u32,
    terrain_chunk: vec4<i32>,
    is_planet_tile: u32,
    planet_tile_group_id: u32,
    padding2: vec2<u32>,
    planet_tile: vec4<i32>,
};

struct SectorGridEntry {
    coords: vec4<i32>, // sx, sy, sz, 0
    base_idx: u32,
    sector_idx: i32,
    padding: vec2<u32>,
};

struct SectorGridParams {
    grid_size: u32,
    grid_mask: u32,
    padding0: u32,
    padding1: u32,
};

// ============== BIND GROUPS ==============

@group(0) @binding(0) var<storage, read> shadow_updates: array<ShadowUpdate>;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var<storage, read> nodes: array<BVHNode>;
@group(0) @binding(3) var<storage, read> lights: array<Light>;
@group(0) @binding(4) var<storage, read> shadow_layer_params: array<ShadowLayerParams>;

// Group 1: Shadow Map Output
@group(1) @binding(0) var out_shadow_map: texture_storage_2d_array<rgba32float, write>;

// Group 2: Voxel Data
@group(2) @binding(0) var<storage, read> sectors: array<SectorRecord>;
@group(2) @binding(1) var<storage, read> bricks: array<BrickRecord>;
@group(2) @binding(2) var voxel_payload_0: texture_3d<u32>;
@group(2) @binding(3) var voxel_payload_1: texture_3d<u32>;
@group(2) @binding(4) var voxel_payload_2: texture_3d<u32>;
@group(2) @binding(5) var voxel_payload_3: texture_3d<u32>;
@group(2) @binding(6) var<storage, read> materials: array<vec4<f32>>;
@group(2) @binding(7) var<storage, read> object_params: array<ObjectParams>;
@group(2) @binding(8) var<storage, read> tree64_nodes: array<Tree64Node>;
@group(2) @binding(9) var<storage, read> sector_grid: array<SectorGridEntry>;
@group(2) @binding(10) var<uniform> sector_grid_params: SectorGridParams;

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

fn load_voxel_payload(page: u32, coords: vec3<u32>) -> u32 {
    switch page {
        case 0u: { return textureLoad(voxel_payload_0, vec3<i32>(coords), 0).r; }
        case 1u: { return textureLoad(voxel_payload_1, vec3<i32>(coords), 0).r; }
        case 2u: { return textureLoad(voxel_payload_2, vec3<i32>(coords), 0).r; }
        default: { return textureLoad(voxel_payload_3, vec3<i32>(coords), 0).r; }
    }
}

fn load_u8(packed_offset: u32, atlas_page: u32, voxel_idx: u32) -> u32 {
    let ax = (packed_offset >> 20u) & 0x3FFu;
    let ay = (packed_offset >> 10u) & 0x3FFu;
    let az = packed_offset & 0x3FFu;

    let vx = voxel_idx % 8u;
    let vy = (voxel_idx / 8u) % 8u;
    let vz = voxel_idx / 64u;

    let coords = vec3<u32>(ax + vx, ay + vy, az + vz);
    return load_voxel_payload(atlas_page, coords);
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
    for (var i = 0u; i < 128u; i++) {
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

fn point_shadow_face_dir(face: u32, uv: vec2<f32>) -> vec3<f32> {
    let face_uv = uv * 2.0 - 1.0;
    switch face {
        case 0u: { return normalize(vec3<f32>(1.0, -face_uv.y, -face_uv.x)); }
        case 1u: { return normalize(vec3<f32>(-1.0, -face_uv.y, face_uv.x)); }
        case 2u: { return normalize(vec3<f32>(face_uv.x, 1.0, face_uv.y)); }
        case 3u: { return normalize(vec3<f32>(face_uv.x, -1.0, -face_uv.y)); }
        case 4u: { return normalize(vec3<f32>(face_uv.x, -face_uv.y, 1.0)); }
        default: { return normalize(vec3<f32>(-face_uv.x, -face_uv.y, -1.0)); }
    }
}

fn traverse_xbrickmap(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 60000.0, vec3<f32>(0.0), vec3<f32>(0.0), 0u);
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
                        let packed_idx = sector.brick_table_index + brick_idx_local;
                        let b_flags = bricks[packed_idx].flags;
                        let b_atlas = bricks[packed_idx].atlas_offset;
                        
                        var t_brick_exit = min(min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z), t_sector_exit);
                        if (b_flags == 1u) {
                            let mat_idx_s = params.material_table_base + b_atlas * 4u;
                            let pbr_s = materials[mat_idx_s + 2u];
                            if (pbr_s.w > 0.001) {
                                t_brick = t_brick_exit;
                            } else {
                                result.hit = true; result.t = t_brick;
                                let p_hit_os = ray.origin + dir * (t_brick + 0.01);
                                let hit_pos_os = ray.origin + dir * t_brick;
                                let voxel_center_os = floor(p_hit_os) + 0.5;
                                result.hit_pos_ws = (inst.object_to_world * vec4<f32>(hit_pos_os, 1.0)).xyz;
                                result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                result.shadow_group_id = params.shadow_group_id;
                                return result;
                            }
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
                                
                                let b_mask_lo = bricks[packed_idx].occupancy_mask_lo;
                                let b_mask_hi = bricks[packed_idx].occupancy_mask_hi;
                                if (bit_test64(b_mask_lo, b_mask_hi, micro_idx)) {
                                    let palette_idx = load_u8(b_atlas, bricks[packed_idx].atlas_page, voxel_idx);
                                    if (palette_idx != EMPTY_VOXEL) {
                                        let mat_idx_v = params.material_table_base + palette_idx * 4u;
                                        let pbr_v = materials[mat_idx_v + 2u];
                                        if (pbr_v.w <= 0.001) {
                                            result.hit = true; result.t = t_micro;
                                            let hit_pos_os = ray.origin + dir * t_micro;
                                            let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                                            result.hit_pos_ws = (inst.object_to_world * vec4<f32>(hit_pos_os, 1.0)).xyz;
                                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                            result.shadow_group_id = params.shadow_group_id;
                                            return result;
                                        }
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
    var result = HitResult(false, 60000.0, vec3<f32>(0.0), vec3<f32>(0.0), 0u);
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
                result.hit_pos_ws = (inst.object_to_world * vec4<f32>(p, 1.0)).xyz;
                let voxel_center_os = floor(p / 8.0) * 8.0 + 4.0;
                result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                result.shadow_group_id = params.shadow_group_id;
                return result;
            }
            t += step_to_next_cell(p, ray.dir, ray.inv_dir, 8.0);
        } else { t += step_to_next_cell(p, ray.dir, ray.inv_dir, 32.0); }
    }
    return result;
}

fn traverse_scene(ray: Ray) -> HitResult {
    var hit_res = HitResult(false, 60000.0, vec3<f32>(0.0), vec3<f32>(0.0), 0u);
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
    let update_idx = global_id.z;
    let num_updates = arrayLength(&shadow_updates);
    if (update_idx >= num_updates) { return; }
    
    let update = shadow_updates[update_idx];
    let layer_params = shadow_layer_params[update.shadow_layer];
    let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
    if (global_id.x >= effective_resolution || global_id.y >= effective_resolution) { return; }
    let light_idx = update.light_index;
    
    let light = lights[light_idx];
    var light_view_proj = light.view_proj;
    var light_inv_view_proj = light.inv_view_proj;
    if (update.kind == 1u) {
        if (update.cascade_index == 0u) {
            light_view_proj = light.directional_cascades[0].view_proj;
            light_inv_view_proj = light.directional_cascades[0].inv_view_proj;
        } else {
            light_view_proj = light.directional_cascades[1].view_proj;
            light_inv_view_proj = light.directional_cascades[1].inv_view_proj;
        }
    }
    
    let uv = (vec2<f32>(f32(global_id.x), f32(global_id.y)) + 0.5) / vec2<f32>(f32(effective_resolution), f32(effective_resolution));
    let light_type = u32(light.params.z);
    var ray: Ray;
    if (update.kind == 2u) {
        let dir = point_shadow_face_dir(update.cascade_index, uv);
        let safe_dir = make_safe_dir(dir);
        ray = Ray(light.position.xyz, dir, 1.0 / safe_dir);
    } else {
        let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
        let p_near = light_inv_view_proj * vec4<f32>(ndc, -1.0, 1.0);
        let p_far = light_inv_view_proj * vec4<f32>(ndc, 1.0, 1.0);
        let origin = p_near.xyz / p_near.w;
        let p_target = p_far.xyz / p_far.w;
        let dir = normalize(p_target - origin);
        let safe_dir = make_safe_dir(dir);
        ray = Ray(origin, dir, 1.0 / safe_dir);
    }
    
    // Perform traversal
    var hit_res = traverse_scene(ray);
    
    // Force usage of tree64_nodes (Binding 5) to prevent layout mismatch
    // (We disabled traverse_tree64 so the compiler stripped the binding)
    let dummy = tree64_nodes[0].mask_lo;
    if (dummy > 0xFFFF0000u) { hit_res.t += 0.00001; } // Unlikely condition, effectively no-op
    
    // Write occluder depth from the entered surface, while keeping receiver evaluation voxel-stable elsewhere.
    if (hit_res.hit) {
        if (update.kind == 2u) {
            // Point lights keep the blocky microvoxel look by shadowing against voxel centers.
            let depth_m = distance(light.position.xyz, hit_res.voxel_center_ws);
            textureStore(out_shadow_map, global_id.xy, i32(update.shadow_layer), vec4<f32>(depth_m, f32(hit_res.shadow_group_id), 0.0, 0.0));
        } else if (light_type == 2u) {
            // Spot light: store linear distance from light position (meters)
            let depth_m = distance(light.position.xyz, hit_res.hit_pos_ws);
            textureStore(out_shadow_map, global_id.xy, i32(update.shadow_layer), vec4<f32>(depth_m, f32(hit_res.shadow_group_id), 0.0, 0.0));
        } else {
            // Directional/others: store NDC depth for stable generic shadowing.
            let pos_ls = light_view_proj * vec4<f32>(hit_res.hit_pos_ws, 1.0);
            let depth_ndc = clamp(pos_ls.z / pos_ls.w, -1.0, 1.0);
            textureStore(out_shadow_map, global_id.xy, i32(update.shadow_layer), vec4<f32>(depth_ndc, f32(hit_res.shadow_group_id), 0.0, 0.0));
        }
    } else {
        if (light_type == 2u || update.kind == 2u) {
            // Local lights treat misses as the light range in meters.
            textureStore(out_shadow_map, global_id.xy, i32(update.shadow_layer), vec4<f32>(light.params.x, 0.0, 0.0, 0.0));
        } else {
            textureStore(out_shadow_map, global_id.xy, i32(update.shadow_layer), vec4<f32>(1.0, 0.0, 0.0, 0.0));
        }
    }
}
