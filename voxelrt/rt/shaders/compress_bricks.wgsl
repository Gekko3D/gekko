// compress_bricks.wgsl
// GPU-accelerated brick compression compute shader

// ============== CONSTANTS ==============
const MAX_BRICKS_TO_SCAN: u32 = 1024u;

// ============== STRUCTS ==============

struct CompressionParams {
    scan_count: u32,
    padding0: u32,
    padding1: u32,
    padding2: u32,
    bricks_to_scan: array<u32, 1024>,
}

struct BrickRecord {
    atlas_offset: u32,
    occupancy_mask_lo: u32,
    occupancy_mask_hi: u32,
    flags: u32,
}

struct PayloadFreeEntry {
    offset: u32,
    padding: array<u32, 3>,
}

// ============== BIND GROUPS ==============

// Group 0: Compression parameters
@group(0) @binding(0) var<uniform> compression_params: CompressionParams;

// Group 1: Brick data
@group(1) @binding(0) var<storage, read_write> bricks: array<BrickRecord>;
@group(1) @binding(1) var<storage, read> brick_pool_payload: array<u32>;

// Group 2: Free queue
@group(2) @binding(0) var<storage, read_write> payload_free_queue: array<PayloadFreeEntry>;
@group(2) @binding(1) var<storage, read_write> free_queue_counter: atomic<u32>;

// ============== COMPRESSION LOGIC ==============

fn is_brick_solid(brick_idx: u32, brick: BrickRecord) -> (bool, u32) {
    // Skip if already solid
    if ((brick.flags & 1u) != 0u) {
        return (false, 0u);
    }
    
    let payload_start = brick.atlas_offset / 4u; // Convert byte offset to u32 index
    
    // Read first voxel (first byte of first word)
    let first_word = brick_pool_payload[payload_start];
    let first_val = first_word & 0xFFu;
    
    // Don't compress empty bricks
    if (first_val == 0u) {
        return (false, 0u);
    }
    
    // Expected word if all voxels are the same
    let expected_word = u32(first_val) | (u32(first_val) << 8u) | 
                        (u32(first_val) << 16u) | (u32(first_val) << 24u);
    
    // Scan all 128 u32s (512 bytes total)
    for (var i = 0u; i < 128u; i++) {
        if (brick_pool_payload[payload_start + i] != expected_word) {
            return (false, 0u); // Not solid
        }
    }
    
    return (true, first_val); // Solid with this value
}

fn compress_brick(brick_idx: u32, brick: BrickRecord, solid_value: u32) {
    let old_offset = brick.atlas_offset;
    
    // Set solid flag (bit 0)
    atomicOr(&bricks[brick_idx].flags, 1u);
    
    // Store solid value in atlas_offset
    atomicStore(&bricks[brick_idx].atlas_offset, u32(solid_value));
    
    // Queue old payload for freeing
    let queue_idx = atomicAdd(&free_queue_counter, 1u);
    if (queue_idx < MAX_BRICKS_TO_SCAN) {
        payload_free_queue[queue_idx].offset = old_offset;
    }
}

// ============== MAIN COMPUTE SHADER ==============

@compute @workgroup_size(64, 1, 1)
fn compress_bricks(@builtin(global_invocation_id) id: vec3<u32>) {
    let scan_idx = id.x;
    
    // Bounds check
    if (scan_idx >= compression_params.scan_count) {
        return;
    }
    
    // Get brick index to scan
    let brick_idx = compression_params.bricks_to_scan[scan_idx];
    let brick = bricks[brick_idx];
    
    // Check if brick is solid
    let (is_solid, solid_value) = is_brick_solid(brick_idx, brick);
    
    if (is_solid) {
        // Compress the brick
        compress_brick(brick_idx, brick, solid_value);
    }
}
