// voxelrt/shaders/gbuffer.wgsl

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const MICRO_SIZE: f32 = 2.0;
const EPS: f32 = 1e-3;
const EMPTY_VOXEL: u32 = 0u;
const AO_MODE_DEFAULT: u32 = 0u;
const AO_MODE_ENABLED: u32 = 1u;
const AO_MODE_DISABLED: u32 = 2u;
const BRICK_FLAG_SOLID: u32 = 1u;
const BRICK_FLAG_UNIFORM_MATERIAL: u32 = 2u;

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
    distance_limits: vec4<f32>,
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
    material_index: u32,
    payload_offset: u32,
    occupancy_mask_lo: u32,
    occupancy_mask_hi: u32,
    payload_page: u32,
    flags: u32,
    dense_occupancy_word_base: u32,
    padding: u32,
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
    ao: f32,
    voxel_center_ws: vec3<f32>,
    shadow_group_id: u32,
    two_sided_lighting: u32,
    shadow_seam_epsilon: f32,
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
    terrain_chunk: vec4<i32>, // xyz: chunk coord, w: chunk size in voxels
    is_planet_tile: u32,
    planet_tile_group_id: u32,
    emitter_link_id: u32,
    padding0: u32,
    planet_tile: vec4<i32>, // face, level, x, y
    direct_lookup_origin_mode: vec4<i32>, // xyz: sector origin, w: lookup mode
    direct_lookup_extent_base: vec4<u32>, // xyz: sector extents, w: table base
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

struct ObjectLookupEntry {
    coords: vec4<i32>,
    group_id: u32,
    object_id: i32,
    padding: vec2<u32>,
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
@group(2) @binding(11) var<storage, read> direct_sector_lookup_words: array<u32>;
@group(2) @binding(12) var<storage, read> object_lookup: array<ObjectLookupEntry>;
@group(2) @binding(13) var<storage, read> dense_occupancy_words: array<u32>;

// ============== HELPERS ==============

fn camera_far_t() -> f32 {
    return max(camera.distance_limits.y, 1.0);
}

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

fn dense_occupancy_test(word_base: u32, voxel_idx: u32) -> bool {
    if (word_base == 0xFFFFFFFFu) {
        return false;
    }
    let word = dense_occupancy_words[word_base + (voxel_idx >> 5u)];
    let bit = 1u << (voxel_idx & 31u);
    return (word & bit) != 0u;
}

fn brick_is_solid(flags: u32) -> bool {
    return (flags & BRICK_FLAG_SOLID) != 0u;
}

fn brick_is_uniform_material(flags: u32) -> bool {
    return (flags & BRICK_FLAG_UNIFORM_MATERIAL) != 0u;
}

fn make_safe_dir(d: vec3<f32>) -> vec3<f32> {
    let eps = 1e-6;
    let sx = select(d.x, (select(1.0, -1.0, d.x < 0.0)) * eps, abs(d.x) < eps);
    let sy = select(d.y, (select(1.0, -1.0, d.y < 0.0)) * eps, abs(d.y) < eps);
    let sz = select(d.z, (select(1.0, -1.0, d.z < 0.0)) * eps, abs(d.z) < eps);
    return vec3<f32>(sx, sy, sz);
}

fn shadow_seam_epsilon_at_hit(voxel_center_os: vec3<f32>, local_aabb_min: vec3<f32>, local_aabb_max: vec3<f32>, seam_epsilon: f32) -> f32 {
    if (seam_epsilon <= 0.0) {
        return 0.0;
    }
    let dist_to_min = abs(voxel_center_os - local_aabb_min);
    let dist_to_max = abs(local_aabb_max - voxel_center_os);
    let near_boundary =
        dist_to_min.x <= seam_epsilon || dist_to_min.y <= seam_epsilon || dist_to_min.z <= seam_epsilon ||
        dist_to_max.x <= seam_epsilon || dist_to_max.y <= seam_epsilon || dist_to_max.z <= seam_epsilon;
    return select(0.0, seam_epsilon, near_boundary);
}

fn saturate(v: f32) -> f32 {
    return clamp(v, 0.0, 1.0);
}

fn quantize_ao_levels(ao: f32, levels: f32) -> f32 {
    return floor(saturate(ao) * levels + 0.5) / levels;
}

fn quantize_ao(ao: f32) -> f32 {
    return quantize_ao_levels(ao, 4.0);
}

// ============== REUSE TRAVERSAL LOGIC FROM RAYTRACE.WGSL ==============

// Note: In a real implementation we would use imports if WGSL supported them well, 
// or common include files. For now we duplicate the necessary traversal functions.

var<private> g_cached_sector_id: i32 = -1;
var<private> g_cached_sector_coords: vec3<i32> = vec3<i32>(-999, -999, -999);
var<private> g_cached_sector_base: u32 = 0xFFFFFFFFu;
const LOOKUP_MODE_HASH: i32 = 0;
const LOOKUP_MODE_DIRECT: i32 = 1;

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

fn find_sector_hash(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
    let size = sector_grid_params.grid_size;
    if (size == 0u) { return -1; }
    let mask = sector_grid_params.grid_mask;
    let h = (u32(sx) * 73856093u ^ u32(sy) * 19349663u ^ u32(sz) * 83492791u ^ params.sector_table_base * 99999989u) & mask;
    for (var i = 0u; i < 16u; i++) {
        let idx = (h + i) & mask;
        let entry = sector_grid[idx];
        if (entry.sector_idx == -1) { return -1; }
        if (entry.coords.x == sx && entry.coords.y == sy && entry.coords.z == sz && entry.base_idx == params.sector_table_base) {
            return entry.sector_idx;
        }
    }
    return -1;
}

fn sector_grid_word(word_idx: u32) -> u32 {
    return direct_sector_lookup_words[word_idx];
}

fn find_sector_direct(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
    let local = vec3<i32>(sx, sy, sz) - params.direct_lookup_origin_mode.xyz;
    if (any(local < vec3<i32>(0))) {
        return -1;
    }
    let extent = params.direct_lookup_extent_base.xyz;
    let local_u = vec3<u32>(local);
    if (local_u.x >= extent.x || local_u.y >= extent.y || local_u.z >= extent.z) {
        return -1;
    }
    let idx = params.direct_lookup_extent_base.w + local_u.x + local_u.y * extent.x + local_u.z * extent.x * extent.y;
    let sector_idx = sector_grid_word(idx);
    if (sector_idx == 0xFFFFFFFFu) {
        return -1;
    }
    return i32(sector_idx);
}

fn find_sector(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
    if (params.direct_lookup_origin_mode.w == LOOKUP_MODE_DIRECT) {
        return find_sector_direct(sx, sy, sz, params);
    }
    return find_sector_hash(sx, sy, sz, params);
}

fn sample_occupancy_local(v: vec3<i32>, params: ObjectParams) -> f32 {
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
    if (!bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
        return 0.0;
    }
    let packed_idx = sector.brick_table_index + brick_idx_local;
    let brick = bricks[packed_idx];
    let b_flags = brick.flags;
    if (!brick_is_solid(b_flags)) {
        let mx = (v.x >> 1u) & 3;
        let my = (v.y >> 1u) & 3;
        let mz = (v.z >> 1u) & 3;
        let mvid = vec3<u32>(u32(mx), u32(my), u32(mz));
        let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
        
        let b_mask_lo = brick.occupancy_mask_lo;
        let b_mask_hi = brick.occupancy_mask_hi;
        if (!bit_test64(b_mask_lo, b_mask_hi, micro_idx)) { return 0.0; }
        
        let vx = v.x & 7;
        let vy = v.y & 7;
        let vz = v.z & 7;
        let vvid = vec3<u32>(u32(vx), u32(vy), u32(vz));
        let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
        return select(0.0, 1.0, dense_occupancy_test(brick.dense_occupancy_word_base, voxel_idx));
    }
    return 1.0;
}

fn floor_div_i32(a: i32, b: i32) -> i32 {
    var q = a / b;
    let r = a % b;
    if (r != 0 && ((r < 0) != (b < 0))) {
        q = q - 1;
    }
    return q;
}

fn positive_mod_i32(a: i32, b: i32) -> i32 {
    var r = a % b;
    if (r < 0) {
        r = r + abs(b);
    }
    return r;
}

fn find_terrain_chunk_object_id(chunk_coord: vec3<i32>, terrain_group_id: u32) -> i32 {
    let lookup_header = object_lookup[0];
    let size = u32(lookup_header.coords.x);
    if (size == 0u) { return -1; }
    let mask = u32(lookup_header.coords.y);
    let start_idx = u32(lookup_header.coords.z);
    let h = (u32(chunk_coord.x) * 73856093u ^
             u32(chunk_coord.y) * 19349663u ^
             u32(chunk_coord.z) * 83492791u ^
             terrain_group_id * 1640531513u) & mask;
    for (var i = 0u; i < size; i++) {
        let idx = ((h + i) & mask) + start_idx;
        let entry = object_lookup[idx];
        if (entry.object_id == -1) { return -1; }
        if (entry.group_id == terrain_group_id &&
            entry.coords.x == chunk_coord.x &&
            entry.coords.y == chunk_coord.y &&
            entry.coords.z == chunk_coord.z) {
            return entry.object_id;
        }
    }
    return -1;
}

fn point_inside_local_bounds(p: vec3<f32>, inst: Instance, padding: f32) -> bool {
    let pad = vec3<f32>(padding);
    return all(p >= inst.local_aabb_min.xyz - pad) && all(p <= inst.local_aabb_max.xyz + pad);
}

fn find_planet_tile_object_id(tile_coord: vec4<i32>, planet_group_id: u32) -> i32 {
    let lookup_header = object_lookup[1];
    let size = u32(lookup_header.coords.x);
    if (size == 0u) { return -1; }
    let mask = u32(lookup_header.coords.y);
    let start_idx = u32(lookup_header.coords.z);
    let h = (u32(tile_coord.x) * 2654435761u ^
             u32(tile_coord.y) * 2246822519u ^
             u32(tile_coord.z) * 3266489917u ^
             u32(tile_coord.w) * 668265263u ^
             planet_group_id * 1640531513u) & mask;
    for (var i = 0u; i < size; i++) {
        let idx = ((h + i) & mask) + start_idx;
        let entry = object_lookup[idx];
        if (entry.object_id == -1) { return -1; }
        if (entry.group_id == planet_group_id &&
            all(entry.coords == tile_coord)) {
            return entry.object_id;
        }
    }
    return -1;
}

fn sample_planet_tile_neighbor(world_pos: vec3<f32>, neighbor_object_id: i32) -> f32 {
    if (neighbor_object_id < 0) {
        return 0.0;
    }
    let neighbor_inst = instances[u32(neighbor_object_id)];
    let neighbor_pos = (neighbor_inst.world_to_object * vec4<f32>(world_pos, 1.0)).xyz;
    if (!point_inside_local_bounds(neighbor_pos, neighbor_inst, 0.75)) {
        return 0.0;
    }
    let neighbor_params = object_params[u32(neighbor_object_id)];
    return sample_occupancy_local(vec3<i32>(floor(neighbor_pos)), neighbor_params);
}

fn sample_occupancy_for_normal(v: vec3<i32>, inst: Instance, params: ObjectParams) -> f32 {
    let local_occ = sample_occupancy_local(v, params);
    if (params.is_terrain_chunk == 0u || params.terrain_group_id == 0u || params.terrain_chunk.w <= 0) {
        if (params.is_planet_tile == 0u || params.planet_tile_group_id == 0u) {
            return local_occ;
        }

        let local_pos = vec3<f32>(v) + vec3<f32>(0.5);
        if (local_occ > 0.5 || point_inside_local_bounds(local_pos, inst, 0.25)) {
            return local_occ;
        }

        let world_pos = (inst.object_to_world * vec4<f32>(local_pos, 1.0)).xyz;
        for (var dy = -1; dy <= 1; dy++) {
            for (var dx = -1; dx <= 1; dx++) {
                if (dx == 0 && dy == 0) {
                    continue;
                }
                let neighbor_tile = vec4<i32>(
                    params.planet_tile.x,
                    params.planet_tile.y,
                    params.planet_tile.z + dx,
                    params.planet_tile.w + dy,
                );
                let neighbor_object_id = find_planet_tile_object_id(neighbor_tile, params.planet_tile_group_id);
                let neighbor_occ = sample_planet_tile_neighbor(world_pos, neighbor_object_id);
                if (neighbor_occ > 0.5) {
                    return neighbor_occ;
                }
            }
        }
        return local_occ;
    }

    let chunk_size = params.terrain_chunk.w;
    if (v.x >= 0 && v.x < chunk_size && v.y >= 0 && v.y < chunk_size && v.z >= 0 && v.z < chunk_size) {
        return local_occ;
    }

    let chunk_offset_x = floor_div_i32(v.x, chunk_size);
    let chunk_offset_y = floor_div_i32(v.y, chunk_size);
    let chunk_offset_z = floor_div_i32(v.z, chunk_size);
    let neighbor_chunk = vec3<i32>(
        params.terrain_chunk.x + chunk_offset_x,
        params.terrain_chunk.y + chunk_offset_y,
        params.terrain_chunk.z + chunk_offset_z,
    );
    let neighbor_object_id = find_terrain_chunk_object_id(neighbor_chunk, params.terrain_group_id);
    if (neighbor_object_id < 0) {
        return 0.0;
    }

    let neighbor_params = object_params[u32(neighbor_object_id)];
    let neighbor_voxel = vec3<i32>(
        positive_mod_i32(v.x, chunk_size),
        positive_mod_i32(v.y, chunk_size),
        positive_mod_i32(v.z, chunk_size),
    );
    return sample_occupancy_local(neighbor_voxel, neighbor_params);
}

fn voxel_occupancy_gradient(vi: vec3<i32>, inst: Instance, params: ObjectParams) -> vec3<f32> {
    let dx = sample_occupancy_for_normal(vi + vec3<i32>(1, 0, 0), inst, params) - sample_occupancy_for_normal(vi + vec3<i32>(-1, 0, 0), inst, params);
    let dy = sample_occupancy_for_normal(vi + vec3<i32>(0, 1, 0), inst, params) - sample_occupancy_for_normal(vi + vec3<i32>(0, -1, 0), inst, params);
    let dz = sample_occupancy_for_normal(vi + vec3<i32>(0, 0, 1), inst, params) - sample_occupancy_for_normal(vi + vec3<i32>(0, 0, -1), inst, params);
    return vec3<f32>(dx, dy, dz);
}

fn estimate_normal(p: vec3<f32>, inst: Instance, params: ObjectParams) -> vec3<f32> {
    let vi = vec3<i32>(floor(p));
    let grad = voxel_occupancy_gradient(vi, inst, params);
    if (length(grad) < 0.01) { return vec3<f32>(0.0); }
    return -normalize(grad);
}

fn axis_tiebreak_sign(voxel_center_os: vec3<f32>, local_aabb_min: vec3<f32>, local_aabb_max: vec3<f32>, axis: u32) -> f32 {
    let dist_to_min = voxel_center_os[axis] - local_aabb_min[axis];
    let dist_to_max = local_aabb_max[axis] - voxel_center_os[axis];
    if (dist_to_min + 1e-4 < dist_to_max) {
        return -1.0;
    }
    return 1.0;
}

fn fallback_exposed_voxel_normal(
    voxel_center_os: vec3<f32>,
    local_aabb_min: vec3<f32>,
    local_aabb_max: vec3<f32>,
    inst: Instance,
    params: ObjectParams,
) -> vec3<f32> {
    let vi = vec3<i32>(floor(voxel_center_os));

    let occ_px = sample_occupancy_for_normal(vi + vec3<i32>(1, 0, 0), inst, params);
    let occ_nx = sample_occupancy_for_normal(vi + vec3<i32>(-1, 0, 0), inst, params);
    let occ_py = sample_occupancy_for_normal(vi + vec3<i32>(0, 1, 0), inst, params);
    let occ_ny = sample_occupancy_for_normal(vi + vec3<i32>(0, -1, 0), inst, params);
    let occ_pz = sample_occupancy_for_normal(vi + vec3<i32>(0, 0, 1), inst, params);
    let occ_nz = sample_occupancy_for_normal(vi + vec3<i32>(0, 0, -1), inst, params);

    let empty_px = 1.0 - occ_px;
    let empty_nx = 1.0 - occ_nx;
    let empty_py = 1.0 - occ_py;
    let empty_ny = 1.0 - occ_ny;
    let empty_pz = 1.0 - occ_pz;
    let empty_nz = 1.0 - occ_nz;

    let signed_exposure = vec3<f32>(
        empty_px - empty_nx,
        empty_py - empty_ny,
        empty_pz - empty_nz,
    );
    if (length(signed_exposure) >= 0.01) {
        return normalize(signed_exposure);
    }

    let unsigned_exposure = vec3<f32>(
        empty_px + empty_nx,
        empty_py + empty_ny,
        empty_pz + empty_nz,
    );
    var tie_break = vec3<f32>(0.0);
    if (unsigned_exposure.x > 0.01) {
        tie_break.x = axis_tiebreak_sign(voxel_center_os, local_aabb_min, local_aabb_max, 0u);
    }
    if (unsigned_exposure.y > 0.01) {
        tie_break.y = axis_tiebreak_sign(voxel_center_os, local_aabb_min, local_aabb_max, 1u);
    }
    if (unsigned_exposure.z > 0.01) {
        tie_break.z = axis_tiebreak_sign(voxel_center_os, local_aabb_min, local_aabb_max, 2u);
    }
    if (length(tie_break) >= 0.01) {
        return normalize(tie_break);
    }

    return vec3<f32>(0.0);
}

fn has_two_sided_voxel_exposure(voxel_center_os: vec3<f32>, inst: Instance, params: ObjectParams) -> bool {
    let vi = vec3<i32>(floor(voxel_center_os));
    let empty_px = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(1, 0, 0), inst, params);
    let empty_nx = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(-1, 0, 0), inst, params);
    let empty_py = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(0, 1, 0), inst, params);
    let empty_ny = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(0, -1, 0), inst, params);
    let empty_pz = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(0, 0, 1), inst, params);
    let empty_nz = 1.0 - sample_occupancy_for_normal(vi + vec3<i32>(0, 0, -1), inst, params);
    return
        (empty_px > 0.01 && empty_nx > 0.01) ||
        (empty_py > 0.01 && empty_ny > 0.01) ||
        (empty_pz > 0.01 && empty_nz > 0.01);
}

fn fallback_face_normal(p_hit_os: vec3<f32>, vi_hit: vec3<i32>, ray_dir_os: vec3<f32>) -> vec3<f32> {
    let p_in_voxel = p_hit_os - (vec3<f32>(vi_hit) + 0.5);
    let abs_p = abs(p_in_voxel);
    var n_os = vec3<f32>(0.0);

    if (abs_p.x >= abs_p.y && abs_p.x >= abs_p.z) {
        var nx = select(1.0, -1.0, p_in_voxel.x < 0.0);
        if (abs(p_in_voxel.x) < 1e-4) {
            nx = -select(1.0, -1.0, ray_dir_os.x < 0.0);
        }
        n_os.x = nx;
    } else if (abs_p.y >= abs_p.x && abs_p.y >= abs_p.z) {
        var ny = select(1.0, -1.0, p_in_voxel.y < 0.0);
        if (abs(p_in_voxel.y) < 1e-4) {
            ny = -select(1.0, -1.0, ray_dir_os.y < 0.0);
        }
        n_os.y = ny;
    } else {
        var nz = select(1.0, -1.0, p_in_voxel.z < 0.0);
        if (abs(p_in_voxel.z) < 1e-4) {
            nz = -select(1.0, -1.0, ray_dir_os.z < 0.0);
        }
        n_os.z = nz;
    }

    return n_os;
}

fn transform_normal_to_world(inst: Instance, normal_os: vec3<f32>) -> vec3<f32> {
    return normalize((transpose(inst.world_to_object) * vec4<f32>(normal_os, 0.0)).xyz);
}

fn dominant_axis_normal_i(normal_os: vec3<f32>) -> vec3<i32> {
    let abs_n = abs(normal_os);
    if (abs_n.x >= abs_n.y && abs_n.x >= abs_n.z) {
        return vec3<i32>(select(-1, 1, normal_os.x >= 0.0), 0, 0);
    }
    if (abs_n.y >= abs_n.x && abs_n.y >= abs_n.z) {
        return vec3<i32>(0, select(-1, 1, normal_os.y >= 0.0), 0);
    }
    return vec3<i32>(0, 0, select(-1, 1, normal_os.z >= 0.0));
}

fn compute_voxel_ao(voxel_center_os: vec3<f32>, normal_os: vec3<f32>, inst: Instance, params: ObjectParams, sample_budget: u32) -> f32 {
    let vi = vec3<i32>(floor(voxel_center_os));
    let normal_i = dominant_axis_normal_i(normal_os);

    var tangent_u = vec3<i32>(1, 0, 0);
    var tangent_v = vec3<i32>(0, 1, 0);
    if (abs(normal_i.x) > 0) {
        tangent_u = vec3<i32>(0, 1, 0);
        tangent_v = vec3<i32>(0, 0, 1);
    } else if (abs(normal_i.y) > 0) {
        tangent_u = vec3<i32>(1, 0, 0);
        tangent_v = vec3<i32>(0, 0, 1);
    } else {
        tangent_u = vec3<i32>(1, 0, 0);
        tangent_v = vec3<i32>(0, 1, 0);
    }

    let sample_radius = max(1, i32(round(max(camera.ao_quality.y, 1.0))));
    let normal_step = normal_i * sample_radius;
    let tangent_u_step = tangent_u * sample_radius;
    let tangent_v_step = tangent_v * sample_radius;

    var occlusion = 0.0;
    var total_weight = 0.0;
    if (sample_budget > 0u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step, inst, params) * 0.30;
        total_weight += 0.30;
    }
    if (sample_budget > 1u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step + tangent_u_step, inst, params) * 0.10;
        total_weight += 0.10;
    }
    if (sample_budget > 2u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step - tangent_u_step, inst, params) * 0.10;
        total_weight += 0.10;
    }
    if (sample_budget > 3u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step + tangent_v_step, inst, params) * 0.10;
        total_weight += 0.10;
    }
    if (sample_budget > 4u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step - tangent_v_step, inst, params) * 0.10;
        total_weight += 0.10;
    }
    if (sample_budget > 5u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step + tangent_u_step + tangent_v_step, inst, params) * 0.05;
        total_weight += 0.05;
    }
    if (sample_budget > 6u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step + tangent_u_step - tangent_v_step, inst, params) * 0.05;
        total_weight += 0.05;
    }
    if (sample_budget > 7u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step - tangent_u_step + tangent_v_step, inst, params) * 0.05;
        total_weight += 0.05;
    }
    if (sample_budget > 8u) {
        occlusion += sample_occupancy_for_normal(vi + normal_step - tangent_u_step - tangent_v_step, inst, params) * 0.05;
        total_weight += 0.05;
    }
    if (sample_budget > 9u) {
        occlusion += sample_occupancy_for_normal(vi + tangent_u_step, inst, params) * 0.025;
        total_weight += 0.025;
    }
    if (sample_budget > 10u) {
        occlusion += sample_occupancy_for_normal(vi - tangent_u_step, inst, params) * 0.025;
        total_weight += 0.025;
    }
    if (sample_budget > 11u) {
        occlusion += sample_occupancy_for_normal(vi + tangent_v_step, inst, params) * 0.025;
        total_weight += 0.025;
    }
    if (sample_budget > 12u) {
        occlusion += sample_occupancy_for_normal(vi - tangent_v_step, inst, params) * 0.025;
        total_weight += 0.025;
    }

    occlusion = occlusion / max(total_weight, 1e-4);
    let ao_raw = 1.0 - saturate(occlusion);
    let axis_strength = max(abs(normal_os.x), max(abs(normal_os.y), abs(normal_os.z)));
    let curved_factor = saturate((0.9 - axis_strength) / 0.3);
    let base_ao = quantize_ao(ao_raw);
    let curved_ao = quantize_ao_levels(mix(ao_raw, 1.0, 0.28), 3.0);
    return max(0.15, mix(base_ao, curved_ao, curved_factor));
}

fn resolve_voxel_ao(voxel_center_os: vec3<f32>, normal_os: vec3<f32>, inst: Instance, params: ObjectParams) -> f32 {
    let sample_budget = min(u32(round(max(camera.ao_quality.x, 0.0))), 13u);
    if (sample_budget == 0u) {
        return 1.0;
    }
    if (params.ambient_occlusion_mode == AO_MODE_DISABLED) {
        return 1.0;
    }
    if (params.ambient_occlusion_mode == AO_MODE_DEFAULT || params.ambient_occlusion_mode == AO_MODE_ENABLED) {
        return compute_voxel_ao(voxel_center_os, normal_os, inst, params, sample_budget);
    }
    return 1.0;
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
    let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
    let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
    let safe_dir = make_safe_dir(new_dir);
    return Ray(new_origin, new_dir, 1.0 / safe_dir);
}

fn traverse_xbrickmap(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, object_id: u32) -> HitResult {
    var result = HitResult(false, camera_far_t(), 0u, 0u, vec3<f32>(0.0), 1.0, vec3<f32>(0.0), 0u, 0u, 0.0);
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
                        let brick = bricks[packed_idx];
                        let b_flags = brick.flags;
                        let b_material = brick.material_index;
                        
                        var t_brick_exit = min(min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z), t_sector_exit);
                        if (brick_is_solid(b_flags)) {
                            // Solid brick: check transparency via material table
                            let mat_idx_s = params.material_table_base + b_material * 4u;
                            let pbr_s = materials[mat_idx_s + 2u]; // x=roughness, y=metalness, z=ior, w=transparency
                            if (pbr_s.w > 0.001) {
                                // Transparent solid: skip this brick, advance to its exit and continue tracing
                                t_brick = t_brick_exit;
                            } else {
                                result.hit = true; result.t = t_brick; result.palette_idx = b_material; result.material_idx = params.material_table_base;
                                let p_hit_os = ray.origin + dir * (t_brick + (EPS * 0.1));
                                let vi_hit = vec3<i32>(floor(p_hit_os));
                                let voxel_center_os = vec3<f32>(vi_hit) + 0.5;
                                var n_os = estimate_normal(voxel_center_os, inst, params);
                                var two_sided_lighting = 0u;
                                if (length(n_os) < 0.01) {
                                    n_os = fallback_exposed_voxel_normal(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, inst, params);
                                    two_sided_lighting = select(0u, 1u, has_two_sided_voxel_exposure(voxel_center_os, inst, params));
                                }
                                if (length(n_os) < 0.01) {
                                    n_os = fallback_face_normal(p_hit_os, vi_hit, dir);
                                }

                                result.normal = transform_normal_to_world(inst, n_os);
                                result.ao = resolve_voxel_ao(voxel_center_os, n_os, inst, params);
                                result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                result.shadow_group_id = params.shadow_group_id;
                                result.two_sided_lighting = two_sided_lighting;
                                result.shadow_seam_epsilon = shadow_seam_epsilon_at_hit(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params.shadow_seam_epsilon);
                                return result;
                            }
                        }
                        if (!brick_is_solid(b_flags)) {
                            var t_micro = t_brick;
                            let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
                            let voxel_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
                            var voxel_pos = vec3<i32>(floor(((ray.origin + dir * t_micro) - brick_origin) - voxel_bias));
                            voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
                            var t_max_micro = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray.origin) * inv_dir;
                            let t_delta_1 = abs(1.0 * inv_dir);
                            let b_mask_lo = brick.occupancy_mask_lo;
                            let b_mask_hi = brick.occupancy_mask_hi;
                            var iter_micro = 0;
                            while (t_micro < t_brick_exit && iter_micro < 32) {
                                iter_micro += 1;
                                let vvid = vec3<u32>(voxel_pos);
                                let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                                let mvid = vvid / 2u;
                                let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
                                
        if (bit_test64(b_mask_lo, b_mask_hi, micro_idx) && dense_occupancy_test(brick.dense_occupancy_word_base, voxel_idx)) {
                                    let palette_idx = select(load_u8(brick.payload_offset, brick.payload_page, voxel_idx), b_material, brick_is_uniform_material(b_flags));
                                    if (palette_idx != EMPTY_VOXEL) {
                                        // Check transparency per-voxel
                                        let mat_idx_v = params.material_table_base + palette_idx * 4u;
                                        let pbr_v = materials[mat_idx_v + 2u];
                                        if (pbr_v.w > 0.001) {
                                            // Transparent voxel: skip and continue marching
                                        } else {
                                            result.hit = true; result.t = t_micro; result.palette_idx = palette_idx; result.material_idx = params.material_table_base;
                                            let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                                            let vi_hit = vec3<i32>(floor(voxel_center_os));
                                            let p_hit_os = ray.origin + dir * (t_micro + (EPS * 0.1));
                                            var n_os = estimate_normal(voxel_center_os, inst, params);
                                            var two_sided_lighting = 0u;
                                            if (length(n_os) < 0.01) {
                                                n_os = fallback_exposed_voxel_normal(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, inst, params);
                                                two_sided_lighting = select(0u, 1u, has_two_sided_voxel_exposure(voxel_center_os, inst, params));
                                            }
                                            if (length(n_os) < 0.01) {
                                                n_os = fallback_face_normal(p_hit_os, vi_hit, dir);
                                            }
                                            result.normal = transform_normal_to_world(inst, n_os);
                                            result.ao = resolve_voxel_ao(voxel_center_os, n_os, inst, params);
                                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                            result.shadow_group_id = params.shadow_group_id;
                                            result.two_sided_lighting = two_sided_lighting;
                                            result.shadow_seam_epsilon = shadow_seam_epsilon_at_hit(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params.shadow_seam_epsilon);
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
    var result = HitResult(false, camera_far_t(), 0u, 0u, vec3<f32>(0.0), 1.0, vec3<f32>(0.0), 0u, 0u, 0.0);
    let params = object_params[object_id];
    if (params.tree64_base == 0xFFFFFFFFu) { return result; }
    let ray = transform_ray(ray_ws, inst.world_to_object);
    let t_obj = intersect_aabb(ray, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
    var t = max(t_obj.x, 0.0) + EPS;
    let t_max_obj = t_obj.y;
    if (t >= t_max_obj) { return result; }
    let root = tree64_nodes[params.tree64_base];
    var iterations = 0;
    while (t < t_max_obj && iterations < 512) {
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
                    if (sample_occupancy_local(vi2, params) > 0.5) {
                        // Tree64 palette/material is stored in node.data; skip transparent and continue
                        let palette_idx2 = l1_node.data;
                        let mat_idx2 = params.material_table_base + palette_idx2 * 4u;
                        let pbr2 = materials[mat_idx2 + 2u]; // w = transparency
                        if (pbr2.w > 0.001) {
                            // Transparent: do not return, keep marching within the block
                        } else {
                            result.hit = true; result.t = t_micro; result.palette_idx = palette_idx2; result.material_idx = params.material_table_base;
                            let p_hit_os = ray.origin + ray.dir * (t_micro + (EPS * 0.1));
                            let voxel_center_os = vec3<f32>(vi2) + 0.5;
                            var n_os = estimate_normal(voxel_center_os, inst, params);
                            var two_sided_lighting = 0u;
                            if (length(n_os) < 0.01) {
                                n_os = fallback_exposed_voxel_normal(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, inst, params);
                                two_sided_lighting = select(0u, 1u, has_two_sided_voxel_exposure(voxel_center_os, inst, params));
                            }
                            if (length(n_os) < 0.01) {
                                n_os = fallback_face_normal(p_hit_os, vi2, ray.dir);
                            }

                            result.normal = transform_normal_to_world(inst, n_os);
                            result.ao = resolve_voxel_ao(voxel_center_os, n_os, inst, params);
                            result.voxel_center_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                            result.shadow_group_id = params.shadow_group_id;
                            result.two_sided_lighting = two_sided_lighting;
                            result.shadow_seam_epsilon = shadow_seam_epsilon_at_hit(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params.shadow_seam_epsilon);
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
    let far_t = camera_far_t();
    var hit_res = HitResult(false, far_t, 0u, 0u, vec3<f32>(0.0), 1.0, vec3<f32>(0.0), 0u, 0u, 0.0);
    
    var stack: array<i32, 64>;
    var stack_ptr = 0;
    
    let n_nodes = arrayLength(&nodes);
    if (n_nodes > 0u) {
        stack[stack_ptr] = 0;
        stack_ptr += 1;
    }
    
    var iterations = 0;
    while (stack_ptr > 0 && iterations < 512) { // Increased limit
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
            textureStore(out_depth, global_id.xy, vec4<f32>(far_t, 0.0, 0.0, 0.0));
            textureStore(out_normal, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
            textureStore(out_material, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
            return;
        }

        textureStore(out_depth, global_id.xy, vec4<f32>(hit_res.t, hit_res.voxel_center_ws));
        textureStore(out_normal, global_id.xy, vec4<f32>(hit_res.normal, hit_res.ao));
        let packed_shadow_group = hit_res.shadow_group_id * 2u + hit_res.two_sided_lighting;
        textureStore(out_material, global_id.xy, vec4<f32>(f32(hit_res.palette_idx), f32(packed_shadow_group), hit_res.shadow_seam_epsilon, f32(hit_res.material_idx)));
    } else {
        textureStore(out_depth, global_id.xy, vec4<f32>(far_t, 0.0, 0.0, 0.0));
        textureStore(out_normal, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
        textureStore(out_material, global_id.xy, vec4<f32>(0.0, 0.0, 0.0, 0.0));
    }
}
