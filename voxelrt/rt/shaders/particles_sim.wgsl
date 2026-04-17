// particles_sim.wgsl

struct Particle {
    pos: vec3<f32>,
    size: f32,
    color: vec4<f32>,
    velocity: vec3<f32>,
    life: f32,
    max_life: f32,
    gravity: f32,
    drag: f32,
    sprite_index: u32,
    atlas_cols: u32,
    atlas_rows: u32,
    alpha_mode: u32,
    pad1: u32,
};

struct EmitterParams {
    pos: vec3<f32>,
    spawn_count: u32,

    rot: vec4<f32>,

    life_min: f32,
    life_max: f32,
    speed_min: f32,
    speed_max: f32,

    size_min: f32,
    size_max: f32,
    gravity: f32,
    drag: f32,

    color_min: vec4<f32>,
    color_max: vec4<f32>,

    cone_angle: f32,
    sprite_index: u32,
    atlas_cols: u32,
    atlas_rows: u32,

    alpha_mode: u32,
    pad1: u32,
    pad2: u32,
    pad3: u32,
};

struct SimulationParams {
    dt: f32,
    seed: u32,
    max_particles: u32,
    emitter_count: u32,
    inv_vsize: f32,
    pad1: f32,
    pad2: f32,
    pad3: f32,
};

struct Counters {
    dead_count: atomic<u32>,
    alive_count: atomic<u32>,
    spawn_request_count: atomic<u32>,
    pad: u32,
};

struct DrawIndirectArgs {
    vertex_count: u32,
    instance_count: atomic<u32>,
    first_vertex: u32,
    first_instance: u32,
};

struct SpawnRequest {
    emitter_idx: u32,
};

@group(0) @binding(0) var<uniform> params: SimulationParams;
@group(0) @binding(1) var<storage, read_write> particles: array<Particle>;
@group(0) @binding(2) var<storage, read_write> dead_pool: array<u32>;
@group(0) @binding(3) var<storage, read_write> alive_list: array<u32>;
@group(0) @binding(4) var<storage, read_write> counters: Counters;
@group(0) @binding(5) var<storage, read_write> draw_args: DrawIndirectArgs;
@group(1) @binding(0) var<storage, read> emitters: array<EmitterParams>;
@group(1) @binding(1) var<storage, read> spawn_requests: array<SpawnRequest>;

// Group 2: Voxel Data (Shared with Renderer)
struct SectorRecord { origin_vox: vec4<i32>, brick_table_index: u32, brick_mask_lo: u32, brick_mask_hi: u32, padding: u32 };
struct BrickRecord { material_index: u32, payload_offset: u32, occupancy_mask_lo: u32, occupancy_mask_hi: u32, payload_page: u32, flags: u32, dense_occupancy_word_base: u32, padding: u32 };
struct SectorGridEntry { coords: vec4<i32>, base_idx: u32, sector_idx: i32, padding: vec2<u32> };
struct SectorGridParams { grid_size: u32, grid_mask: u32, padding0: u32, padding1: u32 };
struct ObjectParams { sector_table_base: u32, brick_table_base: u32, payload_base: u32, material_table_base: u32, tree64_base: u32, lod_threshold: f32, sector_count: u32, ambient_occlusion_mode: u32, shadow_group_id: u32, shadow_seam_epsilon: f32, is_terrain_chunk: u32, terrain_group_id: u32, terrain_chunk: vec4<i32>, is_planet_tile: u32, planet_tile_group_id: u32, emitter_link_id: u32, padding2: u32, planet_tile: vec4<i32>, direct_lookup_origin_mode: vec4<i32>, direct_lookup_extent_base: vec4<u32> };

@group(2) @binding(0) var<storage, read> sectors: array<SectorRecord>;
@group(2) @binding(1) var<storage, read> bricks: array<BrickRecord>;
@group(2) @binding(7) var<storage, read> object_params: array<ObjectParams>;
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
@group(2) @binding(8) var<storage, read> instances: array<Instance>;
@group(2) @binding(9) var<storage, read> sector_grid: array<SectorGridEntry>;
@group(2) @binding(10) var<uniform> sector_grid_params: SectorGridParams;
@group(2) @binding(11) var<storage, read> direct_sector_lookup_words: array<u32>;
@group(2) @binding(13) var<storage, read> dense_occupancy_words: array<u32>;
const LOOKUP_MODE_DIRECT: i32 = 1;
const BRICK_FLAG_SOLID: u32 = 1u;

fn bit_test64(mask_lo: u32, mask_hi: u32, idx: u32) -> bool {
    if (idx < 32u) { return (mask_lo & (1u << idx)) != 0u; }
    else { return (mask_hi & (1u << (idx - 32u))) != 0u; }
}

fn brick_is_solid(flags: u32) -> bool {
    return (flags & BRICK_FLAG_SOLID) != 0u;
}

fn find_sector_hash(sx: i32, sy: i32, sz: i32, base_idx: u32) -> i32 {
    let size = sector_grid_params.grid_size;
    if (size == 0u) { return -1; }
    let mask = sector_grid_params.grid_mask;
    let h = (u32(sx) * 73856093u ^ u32(sy) * 19349663u ^ u32(sz) * 83492791u ^ base_idx * 99999989u) & mask;
    for (var i = 0u; i < 128u; i++) {
        let idx = (h + i) & mask;
        let entry = sector_grid[idx];
        if (entry.sector_idx == -1) { return -1; }
        if (entry.coords.x == sx && entry.coords.y == sy && entry.coords.z == sz && entry.base_idx == base_idx) {
            return entry.sector_idx;
        }
    }
    return -1;
}

fn sector_grid_word(word_idx: u32) -> u32 {
    return direct_sector_lookup_words[word_idx];
}

fn find_sector_direct(sx: i32, sy: i32, sz: i32, op: ObjectParams) -> i32 {
    let local = vec3<i32>(sx, sy, sz) - op.direct_lookup_origin_mode.xyz;
    if (any(local < vec3<i32>(0))) { return -1; }
    let extent = op.direct_lookup_extent_base.xyz;
    let local_u = vec3<u32>(local);
    if (local_u.x >= extent.x || local_u.y >= extent.y || local_u.z >= extent.z) { return -1; }
    let idx = op.direct_lookup_extent_base.w + local_u.x + local_u.y * extent.x + local_u.z * extent.x * extent.y;
    let sector_idx = sector_grid_word(idx);
    if (sector_idx == 0xFFFFFFFFu) { return -1; }
    return i32(sector_idx);
}

fn find_sector(sx: i32, sy: i32, sz: i32, op: ObjectParams) -> i32 {
    if (op.direct_lookup_origin_mode.w == LOOKUP_MODE_DIRECT) {
        return find_sector_direct(sx, sy, sz, op);
    }
    return find_sector_hash(sx, sy, sz, op.sector_table_base);
}

fn dense_occupancy_test(word_base: u32, voxel_idx: u32) -> bool {
    if (word_base == 0xFFFFFFFFu) { return false; }
    let word = dense_occupancy_words[word_base + (voxel_idx >> 5u)];
    let bit = 1u << (voxel_idx & 31u);
    return (word & bit) != 0u;
}

fn check_voxel_occupancy(pos: vec3<f32>, op: ObjectParams) -> bool {
    // In object space, coordinates are directly in voxel units (1 unit = 1 voxel)
    let vox_pos = vec3<i32>(floor(pos));
    
    let sx = vox_pos.x >> 5; let sy = vox_pos.y >> 5; let sz = vox_pos.z >> 5;
    let s_idx = find_sector(sx, sy, sz, op);
    if (s_idx < 0) { return false; }
    
    let sector = sectors[s_idx];
    let bx = (vox_pos.x >> 3) & 3; let by = (vox_pos.y >> 3) & 3; let bz = (vox_pos.z >> 3) & 3;
    let b_idx = u32(bx + by*4 + bz*16);
    
    if (bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, b_idx)) {
        let brick = bricks[sector.brick_table_index + b_idx];
        if (brick_is_solid(brick.flags)) { return true; } // Solid brick
        
        let mx = (vox_pos.x >> 1) & 3; let my = (vox_pos.y >> 1) & 3; let mz = (vox_pos.z >> 1) & 3;
        let m_idx = u32(mx + my*4 + mz*16);
        if (!bit_test64(brick.occupancy_mask_lo, brick.occupancy_mask_hi, m_idx)) {
            return false;
        }
        let vx = vox_pos.x & 7; let vy = vox_pos.y & 7; let vz = vox_pos.z & 7;
        let voxel_idx = u32(vx + vy*8 + vz*64);
        return dense_occupancy_test(brick.dense_occupancy_word_base, voxel_idx);
    }
    return false;
}

fn get_voxel_normal(pos: vec3<f32>, op: ObjectParams) -> vec3<f32> {
    // Check 6 neighbors to find the gradient/normal
    let dx = f32(check_voxel_occupancy(pos + vec3<f32>(0.2, 0.0, 0.0), op)) - f32(check_voxel_occupancy(pos - vec3<f32>(0.2, 0.0, 0.0), op));
    let dy = f32(check_voxel_occupancy(pos + vec3<f32>(0.0, 0.2, 0.0), op)) - f32(check_voxel_occupancy(pos - vec3<f32>(0.0, 0.2, 0.0), op));
    let dz = f32(check_voxel_occupancy(pos + vec3<f32>(0.0, 0.0, 0.2), op)) - f32(check_voxel_occupancy(pos - vec3<f32>(0.0, 0.0, 0.2), op));
    
    let n = -vec3<f32>(dx, dy, dz);
    if (length(n) < 0.01) { return vec3<f32>(0.0, 1.0, 0.0); } // Default to Up if inside a solid block
    return normalize(n);
}

fn get_occupancy_info(pos: vec3<f32>, out_normal: ptr<function, vec3<f32>>) -> bool {
    let num_instances = arrayLength(&instances);
    for (var i = 0u; i < num_instances; i++) {
        let inst = instances[i];
        if (pos.x < inst.aabb_min.x || pos.y < inst.aabb_min.y || pos.z < inst.aabb_min.z ||
            pos.x > inst.aabb_max.x || pos.y > inst.aabb_max.y || pos.z > inst.aabb_max.z) {
            continue;
        }

        let obj_pos = (inst.world_to_object * vec4<f32>(pos, 1.0)).xyz;
        let op = object_params[inst.object_id];
        if (check_voxel_occupancy(obj_pos, op)) {
            let n_os = get_voxel_normal(obj_pos, op);
            *out_normal = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
            return true;
        }
    }
    return false;
}

fn recycle_particle(idx: u32) {
    let dead_idx = atomicAdd(&counters.dead_count, 1u);
    dead_pool[dead_idx] = idx;
}

// PCG hash for random numbers
fn pcg_hash(input: u32) -> u32 {
    var state = input * 747796405u + 2891336453u;
    var word = ((state >> ((state >> 28u) + 4u)) ^ state) * 277803737u;
    return (word >> 22u) ^ word;
}

fn rand_f32(state: ptr<function, u32>) -> f32 {
    *state = pcg_hash(*state);
    return f32(*state) / 4294967295.0;
}

fn quat_rotate(q: vec4<f32>, v: vec3<f32>) -> vec3<f32> {
    let t = 2.0 * cross(q.xyz, v);
    return v + q.w * t + cross(q.xyz, t);
}

// SIMULATE: Update existing particles
@compute @workgroup_size(64)
fn simulate(@builtin(global_invocation_id) id: vec3<u32>) {
    let idx = id.x;
    if (idx >= params.max_particles) { return; }

    var p = particles[idx];
    if (p.life < p.max_life) {
        // Integrate using particle's own gravity/drag
        let gravity = p.gravity; 
        let drag = max(0.0, 1.0 - p.drag * params.dt);
        
        p.velocity.y -= gravity * params.dt;
        p.velocity *= drag;
        
        // Physics-based Collision
        let next_pos = p.pos + p.velocity * params.dt;
        var normal: vec3<f32>;
        var collided = false;
        if (get_occupancy_info(next_pos, &normal)) {
            collided = true;
            // Reflect! Standard physics bounce
            p.velocity = reflect(p.velocity, normal) * 0.4;
            // Stop if energy is too low
            if (length(p.velocity) < 0.2) {
                p.life = p.max_life;
            } else {
                // Push slightly out of surface
                p.pos = next_pos + normal * 0.08;
            }
        } else {
            p.pos = next_pos;
        }
        p.life += params.dt;
        
        if (p.life >= p.max_life) {
            p.life = p.max_life;
            recycle_particle(idx);
        } else {
            if (collided) {
                var inside_normal: vec3<f32>;
                if (get_occupancy_info(p.pos, &inside_normal)) {
                    p.life = p.max_life;
                    recycle_particle(idx);
                    particles[idx] = p;
                    return;
                }
            }
            let alive_idx = atomicAdd(&counters.alive_count, 1u);
            alive_list[alive_idx] = idx;
        }
        particles[idx] = p;
    }
}

// SPAWN: Initialize new particles from dead pool
@compute @workgroup_size(64)
fn spawn(@builtin(global_invocation_id) id: vec3<u32>) {
    let request_idx = id.x;
    let spawn_total = atomicLoad(&counters.spawn_request_count);
    if (request_idx >= spawn_total) { return; }

    // Try to get a dead particle index
    // Using atomicAdd with -1 (0xFFFFFFFF)
    let dead_pool_top = atomicAdd(&counters.dead_count, 0xFFFFFFFFu);
    if (dead_pool_top == 0u) {
        // No dead particles available, restore count and bail
        atomicAdd(&counters.dead_count, 1u);
        return;
    }
    let p_idx = dead_pool[dead_pool_top - 1u];

    // Initialize particle
    let emitter_idx = spawn_requests[request_idx].emitter_idx;
    let em = emitters[emitter_idx];
    
    var state = params.seed ^ pcg_hash(request_idx);
    
    var p: Particle;

    // Cone sampling
    let axis = vec3<f32>(0.0, 1.0, 0.0);
    let theta_max = em.cone_angle * 0.0174533; // deg to rad
    let u = rand_f32(&state);
    let v = rand_f32(&state);
    let cos_theta = mix(cos(theta_max), 1.0, u);
    let sin_theta = sqrt(1.0 - cos_theta * cos_theta);
    let phi = 6.283185 * v;
    let local_dir = vec3<f32>(cos(phi) * sin_theta, cos_theta, sin(phi) * sin_theta);
    let world_dir = quat_rotate(em.rot, local_dir);
    
    let speed = mix(em.speed_min, em.speed_max, rand_f32(&state));
    p.velocity = world_dir * speed;

    var spawn_pos = em.pos;
    var surface_normal = quat_rotate(em.rot, axis);
    if (length(surface_normal) < 0.001) {
        surface_normal = axis;
    } else {
        surface_normal = normalize(surface_normal);
    }

    var occupied = get_occupancy_info(spawn_pos, &surface_normal);
    if (occupied) {
        var escape_dir = normalize(surface_normal);
        if (length(escape_dir) < 0.001) {
            escape_dir = normalize(world_dir);
        }
        if (length(escape_dir) < 0.001) {
            escape_dir = axis;
        }

        for (var i = 0u; i < 12u && occupied; i++) {
            spawn_pos += escape_dir * 0.35;
            occupied = get_occupancy_info(spawn_pos, &surface_normal);
            if (length(surface_normal) > 0.001) {
                escape_dir = normalize(surface_normal);
            }
        }

        if (occupied) {
            recycle_particle(p_idx);
            return;
        }
    }

    p.pos = spawn_pos;
    
    p.life = 0.0;
    p.max_life = mix(em.life_min, em.life_max, rand_f32(&state));
    p.size = mix(em.size_min, em.size_max, rand_f32(&state));
    p.color = mix(em.color_min, em.color_max, rand_f32(&state));
    p.gravity = em.gravity;
    p.drag = em.drag;
    p.sprite_index = em.sprite_index;
    p.atlas_cols = em.atlas_cols;
    p.atlas_rows = em.atlas_rows;
    p.alpha_mode = em.alpha_mode;
    
    particles[p_idx] = p;
    let alive_idx = atomicAdd(&counters.alive_count, 1u);
    alive_list[alive_idx] = p_idx;
}

@compute @workgroup_size(1)
fn init_draw_args() {
    atomicStore(&counters.alive_count, 0u);
}

@compute @workgroup_size(1)
fn finalize_draw_args() {
    atomicStore(&draw_args.instance_count, atomicLoad(&counters.alive_count));
}
