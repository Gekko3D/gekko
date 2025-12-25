// voxelrt/shaders/voxel_edit.wgsl
// GPU-accelerated voxel editing compute shader

// ============== CONSTANTS ==============
const SECTOR_SIZE: i32 = 32;
const BRICK_SIZE: i32 = 8;
const MICRO_SIZE: i32 = 2;
const EMPTY_VOXEL: u32 = 0u;

// ============== STRUCTS ==============

struct EditCommand {
    position: vec3<i32>,  // Global voxel coordinates (x, y, z)
    value: u32,           // New palette index (0 = delete)
}

struct EditParams {
    edit_count: u32,
    object_id: u32,       // Which object to edit
    padding0: u32,
    padding1: u32,
}

struct SectorRecord {
    origin_vox: vec4<i32>,
    sector_id: u32,       // first_brick_index
    brick_mask_lo: u32,
    brick_mask_hi: u32,
    padding: u32,
}

struct BrickRecord {
    atlas_offset: u32,
    occupancy_mask_lo: u32,
    occupancy_mask_hi: u32,
    flags: u32,
}

struct ObjectParams {
    sector_table_base: u32,
    brick_table_base: u32,
    payload_base: u32,
    material_table_base: u32,
    tree64_base: u32,
    lod_threshold: f32,
    sector_count: u32,
    padding: u32,
}

struct BrickPoolParams {
    pool_size: u32,
    next_free_idx: atomic<u32>,
    padding0: u32,
    padding1: u32,
}

struct SectorExpansion {
    coords: vec3<i32>,
    first_brick_idx: u32,
    brick_count: u32,
    padding: array<u32, 3>,
}

struct SectorGridEntry {
    coords: vec3<i32>,
    base_idx: u32,
    sector_idx: i32,
    paddings: array<u32, 3>,
}

struct SectorGridParams {
    grid_size: u32,
    grid_mask: u32,
    padding0: u32,
    padding1: u32,
}

// ============== BIND GROUPS ==============

// Group 0: Edit Commands
@group(0) @binding(0) var<storage, read> edit_commands: array<EditCommand>;
@group(0) @binding(1) var<uniform> edit_params: EditParams;

// Group 1: Voxel Data (read-write)
@group(1) @binding(0) var<storage, read_write> sectors: array<SectorRecord>;
@group(1) @binding(1) var<storage, read_write> bricks: array<BrickRecord>;
@group(1) @binding(2) var<storage, read_write> voxel_payload: array<u32>;
@group(1) @binding(3) var<storage, read> object_params: array<ObjectParams>;
@group(1) @binding(4) var<storage, read> sector_grid: array<SectorGridEntry>;
@group(1) @binding(5) var<storage, read> sector_grid_params: SectorGridParams;

// Group 2: Brick Pool (for GPU allocation)
@group(2) @binding(0) var<storage, read_write> brick_pool_params: BrickPoolParams;
@group(2) @binding(1) var<storage, read_write> brick_pool: array<BrickRecord>;
@group(2) @binding(2) var<storage, read_write> brick_pool_payload: array<u32>;
@group(2) @binding(3) var<storage, read_write> sector_expansion_queue: array<SectorExpansion>;
@group(2) @binding(4) var<storage, read_write> expansion_queue_counter: atomic<u32>;

// ============== 64-BIT MASK HELPERS ==============

fn bit_test64(mask_lo: u32, mask_hi: u32, idx: u32) -> bool {
    if (idx < 32u) {
        return (mask_lo & (1u << idx)) != 0u;
    } else {
        return (mask_hi & (1u << (idx - 32u))) != 0u;
    }
}

fn popcnt64_lower(mask_lo: u32, mask_hi: u32, idx: u32) -> u32 {
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

// ============== SECTOR LOOKUP ==============

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

// ============== VOXEL LOCATION ==============

struct VoxelLocation {
    sector_idx: i32,
    brick_idx: i32,
    voxel_offset: u32,  // Byte offset in payload
    micro_idx: u32,     // For occupancy mask
    valid: bool,
}

fn locate_voxel(gx: i32, gy: i32, gz: i32, params: ObjectParams) -> VoxelLocation {
    var loc: VoxelLocation;
    loc.valid = false;
    
    // Calculate sector coordinates
    let sx = gx / SECTOR_SIZE;
    let sy = gy / SECTOR_SIZE;
    let sz = gz / SECTOR_SIZE;
    
    // Local coordinates within sector
    var slx = gx % SECTOR_SIZE;
    var sly = gy % SECTOR_SIZE;
    var slz = gz % SECTOR_SIZE;
    
    // Handle negative modulo
    if (slx < 0) { slx = slx + SECTOR_SIZE; }
    if (sly < 0) { sly = sly + SECTOR_SIZE; }
    if (slz < 0) { slz = slz + SECTOR_SIZE; }
    
    // Find sector
    let sector_idx = find_sector(sx, sy, sz, params);
    if (sector_idx < 0) {
        return loc; // Sector doesn't exist
    }
    
    let sector = sectors[sector_idx];
    
    // Calculate brick coordinates within sector
    let bx = slx / BRICK_SIZE;
    let by = sly / BRICK_SIZE;
    let bz = slz / BRICK_SIZE;
    let brick_idx_local = u32(bx + by * 4 + bz * 16);
    
    // Check if brick exists
    if (!bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
        return loc; // Brick doesn't exist
    }
    
    // Find brick in packed array
    let packed_idx = params.brick_table_base + sector.sector_id + 
                     popcnt64_lower(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local);
    let brick = bricks[packed_idx];
    
    // Calculate voxel coordinates within brick
    let vx = slx % BRICK_SIZE;
    let vy = sly % BRICK_SIZE;
    let vz = slz % BRICK_SIZE;
    let voxel_idx = u32(vx + vy * 8 + vz * 64);
    
    // Calculate micro block index (for occupancy mask)
    let mx = vx / MICRO_SIZE;
    let my = vy / MICRO_SIZE;
    let mz = vz / MICRO_SIZE;
    let micro_idx = u32(mx + my * 4 + mz * 16);
    
    // Calculate byte offset in payload
    let voxel_offset = params.payload_base + brick.atlas_offset + voxel_idx;
    
    loc.sector_idx = sector_idx;
    loc.brick_idx = i32(packed_idx);
    loc.voxel_offset = voxel_offset;
    loc.micro_idx = micro_idx;
    loc.valid = true;
    
    return loc;
}

// ============== ATOMIC VOXEL WRITE ==============

fn write_voxel_atomic(loc: VoxelLocation, value: u32) {
    // Calculate word and byte index
    let word_idx = loc.voxel_offset / 4u;
    let byte_idx = loc.voxel_offset % 4u;
    
    // Read current word
    let old_word = atomicLoad(&voxel_payload[word_idx]);
    
    // Create new word with updated byte
    var new_word = old_word;
    let shift = byte_idx * 8u;
    let mask = ~(0xFFu << shift);
    new_word = (new_word & mask) | (u32(value) << shift);
    
    // Atomic compare-and-swap loop
    var success = false;
    var attempts = 0u;
    while (!success && attempts < 100u) {
        let exchanged = atomicCompareExchangeWeak(&voxel_payload[word_idx], old_word, new_word);
        success = exchanged.exchanged;
        if (!success) {
            // Retry with updated old value
            let current = exchanged.old_value;
            new_word = (current & mask) | (u32(value) << shift);
        }
        attempts = attempts + 1u;
    }
}

// ============== OCCUPANCY MASK UPDATE ==============

fn update_occupancy_mask(brick_idx: i32, micro_idx: u32, occupied: bool) {
    // Determine which half of the 64-bit mask to update
    if (micro_idx < 32u) {
        let bit_mask = 1u << micro_idx;
        if (occupied) {
            atomicOr(&bricks[brick_idx].occupancy_mask_lo, bit_mask);
        } else {
            atomicAnd(&bricks[brick_idx].occupancy_mask_lo, ~bit_mask);
        }
    } else {
        let bit_mask = 1u << (micro_idx - 32u);
        if (occupied) {
            atomicOr(&bricks[brick_idx].occupancy_mask_hi, bit_mask);
        } else {
            atomicAnd(&bricks[brick_idx].occupancy_mask_hi, ~bit_mask);
        }
    }
}

// ============== BRICK POOL ALLOCATION ==============

const BRICK_POOL_SIZE: u32 = 65536u; // 64K bricks
const MAX_SECTOR_EXPANSIONS: u32 = 1024u;

fn allocate_brick() -> u32 {
    let idx = atomicAdd(&brick_pool_params.next_free_idx, 1u);
    if (idx >= BRICK_POOL_SIZE) {
        return 0xFFFFFFFFu; // Pool exhausted
    }
    return idx;
}

fn initialize_brick(pool_idx: u32, palette_value: u32) {
    // Initialize brick record in pool
    brick_pool[pool_idx].occupancy_mask_lo = 0u;
    brick_pool[pool_idx].occupancy_mask_hi = 0u;
    brick_pool[pool_idx].flags = 0u;
    brick_pool[pool_idx].atlas_offset = pool_idx * 512u;
    
    // Initialize payload to empty
    let payload_start = pool_idx * 128u; // 512 bytes = 128 u32s
    for (var i = 0u; i < 128u; i++) {
        brick_pool_payload[payload_start + i] = 0u;
    }
}

fn queue_sector_expansion(sx: i32, sy: i32, sz: i32, brick_idx: u32) {
    let queue_idx = atomicAdd(&expansion_queue_counter, 1u);
    if (queue_idx < MAX_SECTOR_EXPANSIONS) {
        sector_expansion_queue[queue_idx].coords = vec3<i32>(sx, sy, sz);
        sector_expansion_queue[queue_idx].first_brick_idx = brick_idx;
        sector_expansion_queue[queue_idx].brick_count = 1u;
    }
}

fn decompress_solid_brick(brick_idx: i32, new_value: u32) -> bool {
    let brick = bricks[brick_idx];
    
    if ((brick.flags & 1u) == 0u) {
        return false; // Not solid
    }
    
    let solid_value = brick.atlas_offset & 0xFFu;
    if (solid_value == new_value) {
        return false; // No change needed
    }
    
    // Allocate new payload slot from pool
    let pool_idx = allocate_brick();
    if (pool_idx == 0xFFFFFFFFu) {
        return false; // Allocation failed
    }
    
    // Fill payload with solid value
    let payload_start = pool_idx * 128u;
    let fill_word = u32(solid_value) | (u32(solid_value) << 8u) | 
                    (u32(solid_value) << 16u) | (u32(solid_value) << 24u);
    for (var i = 0u; i < 128u; i++) {
        brick_pool_payload[payload_start + i] = fill_word;
    }
    
    // Update brick record atomically
    atomicStore(&bricks[brick_idx].atlas_offset, pool_idx * 512u);
    atomicAnd(&bricks[brick_idx].flags, ~1u); // Clear solid flag
    atomicOr(&bricks[brick_idx].occupancy_mask_lo, 0xFFFFFFFFu);
    atomicOr(&bricks[brick_idx].occupancy_mask_hi, 0xFFFFFFFFu);
    
    return true;
}

// Helper for direct payload write to pool
fn write_voxel_to_pool(pool_idx: u32, voxel_idx: u32, value: u32) {
    let offset = pool_idx * 512u + voxel_idx;
    let word_idx = offset / 4u;
    let byte_idx = offset % 4u;
    let shift = byte_idx * 8u;
    let mask = ~(0xFFu << shift);
    
    var old_word = atomicLoad(&brick_pool_payload[word_idx]);
    var new_word = (old_word & mask) | (u32(value) << shift);
    
    var success = false;
    var attempts = 0u;
    while (!success && attempts < 100u) {
        let exchanged = atomicCompareExchangeWeak(&brick_pool_payload[word_idx], old_word, new_word);
        success = exchanged.exchanged;
        if (!success) {
            old_word = exchanged.old_value;
            new_word = (old_word & mask) | (u32(value) << shift);
        }
        attempts++;
    }
}

// ============== MAIN COMPUTE SHADER ==============

@compute @workgroup_size(64, 1, 1)
fn edit_voxels(@builtin(global_invocation_id) id: vec3<u32>) {
    let edit_idx = id.x;
    
    // Bounds check
    if (edit_idx >= edit_params.edit_count) {
        return;
    }
    
    // Load edit command
    let cmd = edit_commands[edit_idx];
    let params = object_params[edit_params.object_id];
    
    // Calculate sector coordinates
    let sx = cmd.position.x / SECTOR_SIZE;
    let sy = cmd.position.y / SECTOR_SIZE;
    let sz = cmd.position.z / SECTOR_SIZE;
    
    // Locate voxel in the hierarchy
    var loc = locate_voxel(cmd.position.x, cmd.position.y, cmd.position.z, params);
    
    if (!loc.valid) {
        // SECTOR OR BRICK DOESN'T EXIST - ALLOCATE FROM POOL!
        
        // Allocate new brick from pool
        let pool_idx = allocate_brick();
        if (pool_idx == 0xFFFFFFFFu) {
            return; // Pool exhausted, skip edit
        }
        
        // Initialize brick
        initialize_brick(pool_idx, cmd.value);
        
        // Queue sector expansion for CPU to process
        queue_sector_expansion(sx, sy, sz, pool_idx);
        
        // Calculate voxel index within brick
        var local_x = cmd.position.x % BRICK_SIZE;
        var local_y = cmd.position.y % BRICK_SIZE;
        var local_z = cmd.position.z % BRICK_SIZE;
        
        // Handle negative modulo
        if (local_x < 0) { local_x += BRICK_SIZE; }
        if (local_y < 0) { local_y += BRICK_SIZE; }
        if (local_z < 0) { local_z += BRICK_SIZE; }
        
        let voxel_idx = u32(local_x + local_y * 8 + local_z * 64);
        
        // Write the voxel value to pool
        write_voxel_to_pool(pool_idx, voxel_idx, cmd.value);
        
        // Update occupancy mask in pool
        let mx = local_x / MICRO_SIZE;
        let my = local_y / MICRO_SIZE;
        let mz = local_z / MICRO_SIZE;
        let micro_idx = u32(mx + my * 4 + mz * 16);
        
        if (cmd.value != EMPTY_VOXEL) {
            if (micro_idx < 32u) {
                atomicOr(&brick_pool[pool_idx].occupancy_mask_lo, 1u << micro_idx);
            } else {
                atomicOr(&brick_pool[pool_idx].occupancy_mask_hi, 1u << (micro_idx - 32u));
            }
        }
        
        return;
    }
    
    // Brick exists - check if it's solid and needs decompression
    let brick = bricks[loc.brick_idx];
    
    if ((brick.flags & 1u) != 0u) {
        // Solid brick - decompress first
        if (!decompress_solid_brick(loc.brick_idx, cmd.value)) {
            return; // Decompression failed
        }
        // Re-locate voxel after decompression
        loc = locate_voxel(cmd.position.x, cmd.position.y, cmd.position.z, params);
        if (!loc.valid) {
            return; // Should not happen, but safety check
        }
    }
    
    // Write new voxel value atomically
    write_voxel_atomic(loc, cmd.value);
    
    // Update occupancy mask
    let occupied = cmd.value != EMPTY_VOXEL;
    update_occupancy_mask(loc.brick_idx, loc.micro_idx, occupied);
}
