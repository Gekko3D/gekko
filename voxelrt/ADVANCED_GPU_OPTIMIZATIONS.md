# Advanced GPU Voxel Editing Optimizations

## Overview

This document provides detailed implementation guidance for three advanced optimizations that build upon the basic GPU-accelerated voxel editing system:

1. **GPU Brick Allocation**: Eliminate CPU involvement in brick creation
2. **GPU Compression**: Automatically compress solid bricks on GPU
3. **GPU Raycasting**: Move physics queries to GPU for zero-latency editing

---

## 1. GPU-Side Brick Allocation

### Problem Statement

**Current Limitation**: When editing a voxel in an empty region (no existing sector/brick), the edit is skipped because the GPU shader cannot allocate new bricks.

**Impact**: 
- Limits GPU editing to pre-existing geometry
- Requires CPU pre-allocation for editable regions
- Breaks the "edit anywhere" paradigm

### Solution Architecture

#### Memory Pool Design

**Concept**: Pre-allocate a large pool of bricks on the GPU and manage allocation via atomic counters.

```wgsl
// GPU-side brick pool
@group(3) @binding(0) var<storage, read_write> brick_pool: array<BrickRecord>;
@group(3) @binding(1) var<storage, read_write> brick_pool_free_list: array<u32>;
@group(3) @binding(2) var<storage, read_write> brick_pool_counter: atomic<u32>;

// Sector expansion buffer (for new sectors)
@group(3) @binding(3) var<storage, read_write> sector_expansion_queue: array<SectorExpansion>;
@group(3) @binding(4) var<storage, read_write> sector_queue_counter: atomic<u32>;

struct SectorExpansion {
    coords: vec3<i32>,
    first_brick_idx: u32,
}
```

#### Allocation Strategy

**Two-Pass Approach**:

**Pass 1: Allocation Pass**
- Scan edit commands
- For each edit requiring new brick:
  - Atomically increment `brick_pool_counter`
  - Store allocated index in temporary buffer
  - Queue sector expansion if needed

**Pass 2: Write Pass**
- Use allocated indices from Pass 1
- Write voxel data to newly allocated bricks
- Update sector/brick masks

#### Implementation Steps

**Step 1: Create Brick Pool** (~50 lines)

```wgsl
const BRICK_POOL_SIZE: u32 = 65536u; // 64K bricks = 256MB payload

fn allocate_brick() -> u32 {
    // Atomic allocation
    let idx = atomicAdd(&brick_pool_counter, 1u);
    if (idx >= BRICK_POOL_SIZE) {
        // Pool exhausted, return error sentinel
        return 0xFFFFFFFFu;
    }
    return idx;
}

fn initialize_brick(idx: u32) {
    brick_pool[idx].occupancy_mask_lo = 0u;
    brick_pool[idx].occupancy_mask_hi = 0u;
    brick_pool[idx].flags = 0u;
    // Payload offset = idx * 512 bytes
    brick_pool[idx].atlas_offset = idx * 512u;
}
```

**Step 2: Sector Expansion Queue** (~80 lines)

```wgsl
fn queue_sector_expansion(sx: i32, sy: i32, sz: i32, brick_idx: u32) {
    let queue_idx = atomicAdd(&sector_queue_counter, 1u);
    if (queue_idx < MAX_SECTOR_EXPANSIONS) {
        sector_expansion_queue[queue_idx].coords = vec3<i32>(sx, sy, sz);
        sector_expansion_queue[queue_idx].first_brick_idx = brick_idx;
    }
}
```

**Step 3: CPU-Side Sector Table Update** (~100 lines Go)

After GPU edit pass, read `sector_expansion_queue` and update CPU-side sector table:

```go
func (m *GpuBufferManager) ProcessSectorExpansions() {
    // Read queue counter
    counter := readAtomicCounter(m.SectorQueueCounterBuf)
    
    // Read expansion queue
    expansions := readSectorExpansions(m.SectorExpansionQueueBuf, counter)
    
    for _, exp := range expansions {
        // Add new sector to CPU-side map
        sKey := [3]int{exp.Coords[0], exp.Coords[1], exp.Coords[2]}
        sector := volume.NewSector(sKey[0], sKey[1], sKey[2])
        xbm.Sectors[sKey] = sector
        
        // Mark dirty for next GPU upload
        xbm.DirtySectors[sKey] = true
    }
}
```

#### Performance Impact

**Before**: Edit to empty region → skipped  
**After**: Edit to empty region → 0.2ms (allocation + write)

**Memory Cost**: 256MB for 64K brick pool (acceptable on modern GPUs)

---

## 2. GPU Compression Detection

### Problem Statement

**Current Limitation**: Solid bricks (uniform voxels) are compressed on CPU but not on GPU. GPU edits to solid bricks are skipped.

**Impact**:
- Cannot edit large uniform structures (walls, floors)
- Wastes memory (uncompressed bricks)

### Solution Architecture

#### Compression Detection

**Concept**: After writing voxels, scan the brick to detect if all voxels are identical.

```wgsl
fn try_compress_brick(brick_idx: u32) -> bool {
    let brick = bricks[brick_idx];
    
    // Skip if already solid
    if ((brick.flags & 1u) != 0u) {
        return false;
    }
    
    // Read first voxel
    let first_voxel = load_u8(brick.atlas_offset);
    if (first_voxel == 0u) {
        return false; // Empty brick
    }
    
    // Scan all 512 voxels
    var all_same = true;
    for (var i = 0u; i < 512u; i++) {
        let val = load_u8(brick.atlas_offset + i);
        if (val != first_voxel) {
            all_same = false;
            break;
        }
    }
    
    if (all_same) {
        // Compress: set solid flag and store value in atlas_offset
        atomicOr(&bricks[brick_idx].flags, 1u);
        atomicStore(&bricks[brick_idx].atlas_offset, u32(first_voxel));
        
        // Queue payload slot for freeing (CPU-side)
        queue_payload_free(brick.atlas_offset);
        return true;
    }
    
    return false;
}
```

#### Decompression on Write

**Concept**: When editing a solid brick, decompress it first.

```wgsl
fn decompress_brick_if_needed(brick_idx: u32, new_value: u8) {
    let brick = bricks[brick_idx];
    
    if ((brick.flags & 1u) != 0u) {
        // Solid brick
        let solid_value = u8(brick.atlas_offset);
        
        if (solid_value == new_value) {
            return; // No change needed
        }
        
        // Allocate new payload slot
        let new_offset = allocate_payload_slot();
        if (new_offset == 0xFFFFFFFFu) {
            return; // Allocation failed
        }
        
        // Fill payload with solid value
        for (var i = 0u; i < 512u; i++) {
            write_u8(new_offset + i, solid_value);
        }
        
        // Update brick record
        atomicStore(&bricks[brick_idx].atlas_offset, new_offset);
        atomicAnd(&bricks[brick_idx].flags, ~1u); // Clear solid flag
        atomicOr(&bricks[brick_idx].occupancy_mask_lo, 0xFFFFFFFFu);
        atomicOr(&bricks[brick_idx].occupancy_mask_hi, 0xFFFFFFFFu);
    }
}
```

#### Implementation Steps

**Step 1: Add Compression Pass** (~120 lines)

Create separate compute shader `compress_bricks.wgsl`:

```wgsl
@compute @workgroup_size(64, 1, 1)
fn compress_bricks(@builtin(global_invocation_id) id: vec3<u32>) {
    let brick_idx = id.x;
    if (brick_idx >= brick_count) {
        return;
    }
    
    try_compress_brick(brick_idx);
}
```

**Step 2: Integrate into Render Loop** (~20 lines Go)

```go
// After edit flush, run compression pass
if len(m.PendingEdits) > 0 {
    m.FlushEdits(0)
    m.CompressBricks() // New method
}
```

**Step 3: Payload Slot Management** (~150 lines Go)

Implement free list for payload slots:

```go
type PayloadAllocator struct {
    FreeSlots []uint32
    NextSlot  uint32
}

func (p *PayloadAllocator) Allocate() uint32 {
    if len(p.FreeSlots) > 0 {
        slot := p.FreeSlots[len(p.FreeSlots)-1]
        p.FreeSlots = p.FreeSlots[:len(p.FreeSlots)-1]
        return slot
    }
    slot := p.NextSlot
    p.NextSlot += 512
    return slot
}

func (p *PayloadAllocator) Free(slot uint32) {
    p.FreeSlots = append(p.FreeSlots, slot)
}
```

#### Performance Impact

**Memory Savings**: 
- Large uniform wall (1000 bricks): 512KB → 4KB (128× compression)
- Typical scene: 30-50% memory reduction

**Compression Cost**: ~0.5ms per 1000 bricks (amortized over frames)

---

## 3. GPU Raycasting for Physics

### Problem Statement

**Current Limitation**: Physics raycasts (bullets, collision) use CPU `RayMarch`, which doesn't see GPU edits until sync.

**Impact**:
- 1-frame delay between destruction and physics response
- CPU raycasting is slower than GPU (10-20× difference)

### Solution Architecture

#### Compute Shader Raycasting

**Concept**: Dispatch raycasts as compute shader, read results via buffer readback.

```wgsl
struct RaycastQuery {
    origin: vec3<f32>,
    direction: vec3<f32>,
    max_distance: f32,
    query_id: u32,
}

struct RaycastResult {
    hit: bool,
    distance: f32,
    position: vec3<f32>,
    normal: vec3<f32>,
    voxel_value: u32,
}

@group(0) @binding(0) var<storage, read> raycast_queries: array<RaycastQuery>;
@group(0) @binding(1) var<storage, read_write> raycast_results: array<RaycastResult>;
@group(0) @binding(2) var<uniform> query_count: u32;

@compute @workgroup_size(64, 1, 1)
fn raycast_batch(@builtin(global_invocation_id) id: vec3<u32>) {
    let query_idx = id.x;
    if (query_idx >= query_count) {
        return;
    }
    
    let query = raycast_queries[query_idx];
    
    // Reuse existing traversal logic from raytrace.wgsl
    let ray = Ray(query.origin, query.direction, 1.0 / query.direction);
    let hit = traverse_xbrickmap(ray, /* ... */);
    
    raycast_results[query_idx].hit = hit.hit;
    raycast_results[query_idx].distance = hit.t;
    raycast_results[query_idx].position = query.origin + query.direction * hit.t;
    raycast_results[query_idx].normal = hit.normal;
    raycast_results[query_idx].voxel_value = hit.palette_idx;
}
```

#### Asynchronous Readback

**Challenge**: GPU→CPU readback has latency (~1-2ms).

**Solution**: Double-buffered queries with async readback.

```go
type RaycastManager struct {
    QueryBuf    [2]*wgpu.Buffer // Double buffer
    ResultBuf   [2]*wgpu.Buffer
    CurrentBuf  int
    
    PendingQueries []RaycastQuery
    ResultCallbacks map[uint32]func(RaycastResult)
}

func (r *RaycastManager) QueueRaycast(origin, dir mgl32.Vec3, callback func(RaycastResult)) {
    query := RaycastQuery{
        Origin: origin,
        Direction: dir,
        MaxDistance: 1000.0,
        QueryID: r.NextQueryID,
    }
    r.PendingQueries = append(r.PendingQueries, query)
    r.ResultCallbacks[r.NextQueryID] = callback
    r.NextQueryID++
}

func (r *RaycastManager) Flush() {
    // Write queries to GPU
    r.Device.GetQueue().WriteBuffer(r.QueryBuf[r.CurrentBuf], 0, serializeQueries(r.PendingQueries))
    
    // Dispatch compute
    encoder := r.Device.CreateCommandEncoder(nil)
    pass := encoder.BeginComputePass(nil)
    pass.SetPipeline(r.RaycastPipeline)
    pass.SetBindGroup(0, r.BindGroups[r.CurrentBuf], nil)
    pass.DispatchWorkgroups((len(r.PendingQueries) + 63) / 64, 1, 1)
    pass.End()
    r.Device.GetQueue().Submit(encoder.Finish(nil))
    
    // Async readback (results available next frame)
    r.ResultBuf[r.CurrentBuf].MapAsync(wgpu.MapModeRead, 0, wgpu.WholeMapSize, func(status wgpu.BufferMapAsyncStatus) {
        if status == wgpu.BufferMapAsyncStatusSuccess {
            results := readResults(r.ResultBuf[r.CurrentBuf])
            for _, result := range results {
                if callback, ok := r.ResultCallbacks[result.QueryID]; ok {
                    callback(result)
                    delete(r.ResultCallbacks, result.QueryID)
                }
            }
            r.ResultBuf[r.CurrentBuf].Unmap()
        }
    })
    
    // Swap buffers
    r.CurrentBuf = 1 - r.CurrentBuf
    r.PendingQueries = r.PendingQueries[:0]
}
```

#### Implementation Steps

**Step 1: Extract Traversal Logic** (~200 lines)

Create `raycast.wgsl` with shared traversal functions:

```wgsl
// Shared between raytrace.wgsl and raycast.wgsl
fn traverse_voxel_grid(ray: Ray, params: ObjectParams) -> HitResult {
    // ... existing DDA logic ...
}
```

**Step 2: Create Raycast Pipeline** (~150 lines Go)

```go
func (m *RaycastManager) CreatePipeline(device *wgpu.Device) error {
    shader := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
        WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
            Code: shaders.RaycastWGSL,
        },
    })
    
    m.RaycastPipeline = device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
        Compute: wgpu.ProgrammableStageDescriptor{
            Module: shader,
            EntryPoint: "raycast_batch",
        },
    })
    
    return nil
}
```

**Step 3: Integrate into Game Loop** (~50 lines)

```go
// In game update loop
func (g *Game) Update() {
    // Queue physics raycasts
    for _, bullet := range g.Bullets {
        g.RaycastMgr.QueueRaycast(bullet.Pos, bullet.Dir, func(result RaycastResult) {
            if result.Hit {
                bullet.OnHit(result.Position, result.Normal)
            }
        })
    }
    
    // Flush raycasts (GPU dispatch)
    g.RaycastMgr.Flush()
    
    // Results arrive via callbacks (next frame or later)
}
```

#### Performance Impact

**CPU Raycasting**: 10 rays × 0.1ms = 1ms  
**GPU Raycasting**: 100 rays × 0.01ms = 0.1ms (10× faster)

**Latency**: 1-2 frames (acceptable for most gameplay)

---

## Implementation Priority

### Recommended Order

1. **GPU Compression** (Easiest, High Value)
   - Complexity: Medium
   - Benefit: 30-50% memory savings
   - Time: 1-2 days

2. **GPU Brick Allocation** (Medium, Critical for Full GPU Editing)
   - Complexity: High
   - Benefit: Enables "edit anywhere"
   - Time: 3-5 days

3. **GPU Raycasting** (Hardest, Optional)
   - Complexity: High
   - Benefit: 10× faster physics, zero-latency editing
   - Time: 3-4 days

### Total Estimated Time

**Full Implementation**: 7-11 days for all three features

---

## Complexity Analysis

| Feature | Shader Lines | Go Lines | Complexity | Risk |
|---------|-------------|----------|------------|------|
| GPU Compression | 150 | 200 | Medium | Low |
| GPU Allocation | 200 | 250 | High | Medium |
| GPU Raycasting | 250 | 300 | High | Medium |

**Total**: ~600 shader lines, ~750 Go lines

---

## Conclusion

These three optimizations transform the voxel editing system from "GPU-accelerated" to "fully GPU-native":

- **GPU Allocation**: Eliminates CPU bottleneck for new geometry
- **GPU Compression**: Automatic memory optimization
- **GPU Raycasting**: Zero-latency physics integration

**Combined Impact**: Enables true real-time voxel destruction with Teardown-level performance.
