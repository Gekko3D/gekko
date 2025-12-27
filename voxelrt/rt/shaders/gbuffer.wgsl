// voxelrt/shaders/gbuffer.wgsl

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
    render_mode: u32,
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
    palette_idx: u32,
    material_idx: u32,
    normal: vec3<f32>,
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

// Group 0: Scene
@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var<storage, read> nodes: array<BVHNode>;

// Group 1: G-Buffer Output
@group(1) @binding(0) var out_depth: texture_storage_2d<rgba32float, write>;
@group(1) @binding(1) var out_normal: texture_storage_2d<rgba16float, write>;
@group(1) @binding(2) var out_material: texture_storage_2d<rgba32float, write>;
@group(1) @binding(3) var out_position: texture_storage_2d<rgba32float, write>;

// Group 2: Voxel Data
@group(2) @binding(0) var<storage, read> sectors: array<SectorRecord>;
@group(2) @binding(1) var<storage, read> bricks: array<BrickRecord>;
@group(2) @binding(2) var<storage, read> voxel_payload: array<atomic<u32>>;
@group(2) @binding(3) var<storage, read> materials: array<vec4<f32>>;
@group(2) @binding(4) var<storage, read> object_params: array<ObjectParams>;
@group(2) @binding(5) var<storage, read> tree64_nodes: array<Tree64Node>;
@group(2) @binding(6) var<storage, read> sector_grid: array<SectorGridEntry>;
@group(2) @binding(7) var<storage, read> sector_grid_params: SectorGridParams;

// ============== HELPERS ==============

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

fn make_safe_dir(d: vec3<f32>) -> vec3<f32> {
    let eps = 1e-6;
    let sx = select(d.x, (select(1.0, -1.0, d.x < 0.0)) * eps, abs(d.x) < eps);
    let sy = select(d.y, (select(1.0, -1.0, d.y < 0.0)) * eps, abs(d.y) < eps);
    let sz = select(d.z, (select(1.0, -1.0, d.z < 0.0)) * eps, abs(d.z) < eps);
    return vec3<f32>(sx, sy, sz);
}

// ============== REUSE TRAVERSAL LOGIC FROM RAYTRACE.WGSL ==============

// Note: In a real implementation we would use imports if WGSL supported them well, 
// or common include files. For now we duplicate the necessary traversal functions.

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

fn sample_occupancy(v: vec3<i32>, params: ObjectParams) -> f32 {
    let sx = v.x >> 5u;
    let sy = v.y >> 5u;
    let sz = v.z >> 5u;
    let sector_idx = find_sector_cached(sx, sy, sz, params);
    if (sector_idx < 0) { return 0.0; }
    let sector = sectors[sector_idx];
    let bx = (v.x >> 3u) & 3;
    let by = (v.y >> 3u) & 3;
    let bz = (v.z >> 3u) & 3;
    let bvid = vec3<u32>(u32(bx), u32(by), u32(bz));
    let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;
    if (!bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) { return 0.0; }
    let packed_idx = params.brick_table_base + sector.sector_id + popcnt64_lower(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local);
    
    let b_flags = atomicLoad(&bricks[packed_idx].flags);
    if (b_flags == 0u) {
        let mx = (v.x >> 1u) & 3;
        let my = (v.y >> 1u) & 3;
        let mz = (v.z >> 1u) & 3;
        let mvid = vec3<u32>(u32(mx), u32(my), u32(mz));
        let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
        
        let b_mask_lo = atomicLoad(&bricks[packed_idx].occupancy_mask_lo);
        let b_mask_hi = atomicLoad(&bricks[packed_idx].occupancy_mask_hi);
        if (!bit_test64(b_mask_lo, b_mask_hi, micro_idx)) { return 0.0; }
        
        let vx = v.x & 7;
        let vy = v.y & 7;
        let vz = v.z & 7;
        let vvid = vec3<u32>(u32(vx), u32(vy), u32(vz));
        let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
        let b_atlas = atomicLoad(&bricks[packed_idx].atlas_offset);
        let palette_idx = load_u8(params.payload_base + b_atlas + voxel_idx);
        return select(0.0, 1.0, palette_idx != EMPTY_VOXEL);
    }
    return 1.0;
}

fn estimate_normal(p: vec3<f32>, params: ObjectParams) -> vec3<f32> {
    let vi = vec3<i32>(floor(p));
    let dx = sample_occupancy(vi + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
    let dy = sample_occupancy(vi + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
    let dz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
    let grad = vec3<f32>(dx, dy, dz);
    if (length(grad) < 0.01) { return vec3<f32>(0.0); }
    return -normalize(grad);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
    let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
    let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
    let safe_dir = make_safe_dir(new_dir);
    return Ray(new_origin, new_dir, 1.0 / safe_dir);
}

fn traverse_xbrickmap(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, 60000.0, 0u, 0u, vec3<f32>(0.0), vec3<f32>(0.0));
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
                            // Solid brick: check transparency via material table
                            let mat_idx_s = params.material_table_base + b_atlas * 4u;
                            let pbr_s = materials[mat_idx_s + 2u]; // x=roughness, y=metalness, z=ior, w=transparency
                            if (pbr_s.w > 0.001) {
                                // Transparent solid: skip this brick, advance to its exit and continue tracing
                                t_brick = t_brick_exit;
                            } else {
                                result.hit = true; result.t = t_brick; result.palette_idx = b_atlas; result.material_idx = params.material_table_base;
                                let p_hit_os = ray.origin + dir * (t_brick + EPS);
                                let voxel_center_os = floor(p_hit_os) + 0.5;
                                let aabb_center_os = (inst.local_aabb_min.xyz + inst.local_aabb_max.xyz) * 0.5;
                                let vi_hit = vec3<i32>(floor(voxel_center_os));
                                let grad = vec3<f32>(
                                    sample_occupancy(vi_hit + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi_hit + vec3<i32>(-1, 0, 0), params),
                                    sample_occupancy(vi_hit + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi_hit + vec3<i32>(0, -1, 0), params),
                                    sample_occupancy(vi_hit + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi_hit + vec3<i32>(0, 0, -1), params)
                                );
                                let ax = abs(grad.x); let ay = abs(grad.y); let az = abs(grad.z);
                                var n_os = vec3<f32>(0.0);
                                if (max(ax, max(ay, az)) > 0.05) {
                                    if (ax >= ay && ax >= az) { n_os = vec3<f32>(-select(1.0, -1.0, grad.x < 0.0), 0.0, 0.0); }
                                    else if (ay >= ax && ay >= az) { n_os = vec3<f32>(0.0, -select(1.0, -1.0, grad.y < 0.0), 0.0); }
                                    else { n_os = vec3<f32>(0.0, 0.0, -select(1.0, -1.0, grad.z < 0.0)); }
                                } else {
                                    let dir_c = voxel_center_os - aabb_center_os;
                                    let adx = abs(dir_c.x); let ady = abs(dir_c.y); let adz = abs(dir_c.z);
                                    if (adx >= ady && adx >= adz) { n_os = vec3<f32>(select(1.0, -1.0, dir_c.x < 0.0), 0.0, 0.0); }
                                    else if (ady >= adx && ady >= adz) { n_os = vec3<f32>(0.0, select(1.0, -1.0, dir_c.y < 0.0), 0.0); }
                                    else { n_os = vec3<f32>(0.0, 0.0, select(1.0, -1.0, dir_c.z < 0.0)); }
                                }
                                result.normal = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                                result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
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
                            let b_mask_lo = atomicLoad(&bricks[packed_idx].occupancy_mask_lo);
                            let b_mask_hi = atomicLoad(&bricks[packed_idx].occupancy_mask_hi);
                            var iter_micro = 0;
                            while (t_micro < t_brick_exit && iter_micro < 32) {
                                iter_micro += 1;
                                let vvid = vec3<u32>(voxel_pos);
                                let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                                let mvid = vvid / 2u;
                                let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
                                
                                if (bit_test64(b_mask_lo, b_mask_hi, micro_idx)) {
                                    let actual_atlas_offset = params.payload_base + b_atlas + voxel_idx;
                                    let palette_idx = load_u8(actual_atlas_offset);
                                    if (palette_idx != EMPTY_VOXEL) {
                                        // Check transparency per-voxel
                                        let mat_idx_v = params.material_table_base + palette_idx * 4u;
                                        let pbr_v = materials[mat_idx_v + 2u];
                                        if (pbr_v.w > 0.001) {
                                            // Transparent voxel: skip and continue marching
                                        } else {
                                            result.hit = true; result.t = t_micro; result.palette_idx = palette_idx; result.material_idx = params.material_table_base;
                                            let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                                            let aabb_center_os = (inst.local_aabb_min.xyz + inst.local_aabb_max.xyz) * 0.5;
                                            let vi_hit = vec3<i32>(floor(voxel_center_os));
                                            let grad = vec3<f32>(
                                                sample_occupancy(vi_hit + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi_hit + vec3<i32>(-1, 0, 0), params),
                                                sample_occupancy(vi_hit + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi_hit + vec3<i32>(0, -1, 0), params),
                                                sample_occupancy(vi_hit + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi_hit + vec3<i32>(0, 0, -1), params)
                                            );
                                            let ax = abs(grad.x); let ay = abs(grad.y); let az = abs(grad.z);
                                            var n_os = vec3<f32>(0.0);
                                            if (max(ax, max(ay, az)) > 0.05) {
                                                if (ax >= ay && ax >= az) { n_os = vec3<f32>(-select(1.0, -1.0, grad.x < 0.0), 0.0, 0.0); }
                                                else if (ay >= ax && ay >= az) { n_os = vec3<f32>(0.0, -select(1.0, -1.0, grad.y < 0.0), 0.0); }
                                                else { n_os = vec3<f32>(0.0, 0.0, -select(1.0, -1.0, grad.z < 0.0)); }
                                            } else {
                                                let dir_c = voxel_center_os - aabb_center_os;
                                                let adx = abs(dir_c.x); let ady = abs(dir_c.y); let adz = abs(dir_c.z);
                                                if (adx >= ady && adx >= adz) { n_os = vec3<f32>(select(1.0, -1.0, dir_c.x < 0.0), 0.0, 0.0); }
                                                else if (ady >= adx && ady >= adz) { n_os = vec3<f32>(0.0, select(1.0, -1.0, dir_c.y < 0.0), 0.0); }
                                                else { n_os = vec3<f32>(0.0, 0.0, select(1.0, -1.0, dir_c.z < 0.0)); }
                                            }
                                            result.normal = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
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
                                t_micro += EPS;
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
    var result = HitResult(false, 60000.0, 0u, 0u, vec3<f32>(0.0), vec3<f32>(0.0));
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
        let step_local = vec3<i32>(sign(ray.dir));
        let bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step_local < vec3<i32>(0));
        let pb = p - bias;
        let lx = u32(floor(pb.x / 32.0)) % 4u; let ly = u32(floor(pb.y / 32.0)) % 4u; let lz = u32(floor(pb.z / 32.0)) % 4u;
        let bit = lx + ly*4u + lz*16u;
        if (bit_test64(root.mask_lo, root.mask_hi, bit)) {
            let l1_idx = params.tree64_base + root.child_ptr + popcnt64_lower(root.mask_lo, root.mask_hi, bit);
            let l1_node = tree64_nodes[l1_idx];
            let bx = u32(floor(pb.x / 8.0)) % 4u; let by = u32(floor(pb.y / 8.0)) % 4u; let bz = u32(floor(pb.z / 8.0)) % 4u;
            let b_bit = bx + by*4u + bz*16u;
            if (bit_test64(l1_node.mask_lo, l1_node.mask_hi, b_bit)) {
                // Refine inside the 8x8x8 block to avoid false hits on empty borders.
                let step_local2 = vec3<i32>(sign(ray.dir));
                let bias2 = select(vec3<f32>(0.0), vec3<f32>(EPS), step_local2 < vec3<i32>(0));
                let block_min = floor((p - bias2) / 8.0) * 8.0;
                let block_max = block_min + 8.0;
                let t_block = intersect_aabb(ray, block_min, block_max);
                var t_micro = max(t, t_block.x);
                var voxel_pos2 = vec3<i32>(floor((ray.origin + ray.dir * t_micro) - bias2));
                var t_max_micro2 = (vec3<f32>(voxel_pos2) + select(vec3<f32>(0.0), vec3<f32>(1.0), step_local2 > vec3<i32>(0)) - ray.origin) * ray.inv_dir;
                let t_delta_1_2 = abs(1.0 * ray.inv_dir);
                let t_exit_block = min(t_block.y, t_max_obj);
                var iter_ref = 0;
                while (t_micro < t_exit_block && iter_ref < 32) {
                    iter_ref++;
                    let vi2 = voxel_pos2;
                    if (sample_occupancy(vi2, params) > 0.5) {
                        // Tree64 palette/material is stored in node.data; skip transparent and continue
                        let palette_idx2 = l1_node.data;
                        let mat_idx2 = params.material_table_base + palette_idx2 * 4u;
                        let pbr2 = materials[mat_idx2 + 2u]; // w = transparency
                        if (pbr2.w > 0.001) {
                            // Transparent: do not return, keep marching within the block
                        } else {
                            result.hit = true; result.t = t_micro; result.palette_idx = palette_idx2; result.material_idx = params.material_table_base;
                            let voxel_center_os = vec3<f32>(vi2) + 0.5;
                            let aabb_center_os = (inst.local_aabb_min.xyz + inst.local_aabb_max.xyz) * 0.5;
                            let vi_hit = vec3<i32>(floor(voxel_center_os));
                            let grad = vec3<f32>(
                                sample_occupancy(vi_hit + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi_hit + vec3<i32>(-1, 0, 0), params),
                                sample_occupancy(vi_hit + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi_hit + vec3<i32>(0, -1, 0), params),
                                sample_occupancy(vi_hit + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi_hit + vec3<i32>(0, 0, -1), params)
                            );
                            let ax = abs(grad.x); let ay = abs(grad.y); let az = abs(grad.z);
                            var n_os = vec3<f32>(0.0);
                            if (max(ax, max(ay, az)) > 0.05) {
                                if (ax >= ay && ax >= az) { n_os = vec3<f32>(-select(1.0, -1.0, grad.x < 0.0), 0.0, 0.0); }
                                else if (ay >= ax && ay >= az) { n_os = vec3<f32>(0.0, -select(1.0, -1.0, grad.y < 0.0), 0.0); }
                                else { n_os = vec3<f32>(0.0, 0.0, -select(1.0, -1.0, grad.z < 0.0)); }
                            } else {
                                let dir_c = voxel_center_os - aabb_center_os;
                                let adx = abs(dir_c.x); let ady = abs(dir_c.y); let adz = abs(dir_c.z);
                                if (adx >= ady && adx >= adz) { n_os = vec3<f32>(select(1.0, -1.0, dir_c.x < 0.0), 0.0, 0.0); }
                                else if (ady >= adx && ady >= adz) { n_os = vec3<f32>(0.0, select(1.0, -1.0, dir_c.y < 0.0), 0.0); }
                                else { n_os = vec3<f32>(0.0, 0.0, select(1.0, -1.0, dir_c.z < 0.0)); }
                            }
                            result.normal = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                            return result;
                        }
                    }
                    if (t_max_micro2.x < t_max_micro2.y) {
                        if (t_max_micro2.x < t_max_micro2.z) { voxel_pos2.x += step_local2.x; t_micro = t_max_micro2.x; t_max_micro2.x += t_delta_1_2.x; }
                        else { voxel_pos2.z += step_local2.z; t_micro = t_max_micro2.z; t_max_micro2.z += t_delta_1_2.z; }
                    } else {
                        if (t_max_micro2.y < t_max_micro2.z) { voxel_pos2.y += step_local2.y; t_micro = t_max_micro2.y; t_max_micro2.y += t_delta_1_2.y; }
                        else { voxel_pos2.z += step_local2.z; t_micro = t_max_micro2.z; t_max_micro2.z += t_delta_1_2.z; }
                    }
                    t_micro += EPS;
                }
            }
            t += step_to_next_cell(p, ray.dir, ray.inv_dir, 8.0);
        } else { t += step_to_next_cell(p, ray.dir, ray.inv_dir, 32.0); }
    }
    return result;
}

fn get_ray(uv: vec2<f32>) -> Ray {
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    var view = camera.inv_proj * clip; view = view / view.w;
    let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
    let origin = camera.cam_pos.xyz;
    let dir = normalize(world_target - origin);
    let safe_dir = make_safe_dir(dir);
    return Ray(origin, dir, 1.0 / safe_dir);
}

// ============== MAIN G-BUFFER PASS ==============

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let size = textureDimensions(out_depth);
    if (global_id.x >= size.x || global_id.y >= size.y) { return; }
    
    let uv = (vec2<f32>(f32(global_id.x), f32(global_id.y)) + 0.5) / vec2<f32>(f32(size.x), f32(size.y));
    let ray = get_ray(uv);
    
    // Trace scene via BVH
    var hit_res = HitResult(false, 60000.0, 0u, 0u, vec3<f32>(0.0), vec3<f32>(0.0));
    
    var stack: array<i32, 64>;
    var stack_ptr = 0;
    
    let n_nodes = arrayLength(&nodes);
    if (n_nodes > 0u) {
        stack[stack_ptr] = 0;
        stack_ptr += 1;
    }
    
    var iterations = 0;
    while (stack_ptr > 0 && iterations < 128) { // Increased limit
        iterations++;
        stack_ptr--;
        let node_idx = stack[stack_ptr];
        if (node_idx < 0 || u32(node_idx) >= n_nodes) { continue; }
        
        let node = nodes[node_idx];
        let t_vals = intersect_aabb(ray, node.aabb_min.xyz, node.aabb_max.xyz);
        if (t_vals.x <= t_vals.y && t_vals.y > 0.0 && t_vals.x < hit_res.t) {
            if (node.leaf_count > 0) {
                for (var li = 0; li < node.leaf_count; li = li + 1) {
                    let inst = instances[u32(node.leaf_first + li)];
                    let t_inst = intersect_aabb(ray, inst.aabb_min.xyz, inst.aabb_max.xyz);
                    if (t_inst.x <= t_inst.y && t_inst.y > 0.0 && t_inst.x < hit_res.t) {
                        let params = object_params[inst.object_id];
                        let dist_cam = distance(camera.cam_pos.xyz, inst.aabb_min.xyz);
                        var res: HitResult;
                        if (dist_cam > params.lod_threshold && params.tree64_base != 0xFFFFFFFFu) {
                            res = traverse_tree64(ray, inst, t_inst.x, t_inst.y, inst.object_id);
                        } else {
                            res = traverse_xbrickmap(ray, inst, t_inst.x, t_inst.y, inst.object_id);
                        }
                        if (res.hit && res.t < hit_res.t) { hit_res = res; }
                    }
                }
            } else {
                if (node.left != -1) { stack[stack_ptr] = node.left; stack_ptr++; }
                if (node.right != -1 && stack_ptr < 64) { stack[stack_ptr] = node.right; stack_ptr++; }
            }
        }
    }
    
    // Write outputs
    if (hit_res.hit) {
        // Skip opaque G-Buffer write for transparent materials (let overlay pass composite)
        let palette_idx = hit_res.palette_idx;
        let mat_base = hit_res.material_idx;
        let mat_idx = mat_base + palette_idx * 4u;
        let pbr = materials[mat_idx + 2u]; // x=roughness, y=metalness, z=ior, w=transparency
        if (pbr.w > 0.001) {
            textureStore(out_depth, global_id.xy, vec4<f32>(60000.0, 0.0, 0.0, 0.0));
            textureStore(out_normal, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
            textureStore(out_material, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
            textureStore(out_position, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
            return;
        }

        textureStore(out_depth, global_id.xy, vec4<f32>(hit_res.t, 0.0, 0.0, 0.0));
        textureStore(out_normal, global_id.xy, vec4<f32>(hit_res.normal, 0.0));
        
        // Store raw palette_idx and material_idx (others fetched in lighting pass)
        textureStore(out_material, global_id.xy, vec4<f32>(f32(hit_res.palette_idx), 0.0, 0.0, f32(hit_res.material_idx)));
        textureStore(out_position, global_id.xy, vec4<f32>(hit_res.voxel_center_ws, 1.0));
    } else {
        textureStore(out_depth, global_id.xy, vec4<f32>(60000.0, 0.0, 0.0, 0.0));
        textureStore(out_normal, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
        textureStore(out_material, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
        textureStore(out_position, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
    }
}
