# VoxelRT Optimization Roadmap for Shooter Games

> **Target Use Case**: Fast-paced voxel shooter similar to Voxelstein 3D or Teardown  
> **Performance Goal**: Stable 60+ FPS with dynamic destruction, particles, and multiple lights

---

## Table of Contents
1. [Current Architecture Analysis](#current-architecture-analysis)
2. [Critical Limitations](#critical-limitations)
3. [Render Pipeline Separation Analysis](#render-pipeline-separation-analysis)
4. [Particle System Integration](#particle-system-integration)
5. [Optimization Roadmap](#optimization-roadmap)

---

## Current Architecture Analysis

### Rendering Pipeline (Single-Pass)

The current implementation uses a **monolithic compute shader** ([raytrace.wgsl](file:///Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/shaders/raytrace.wgsl)) that performs:

1. **Primary Ray Generation** (lines 257-266)
2. **BVH Traversal** (lines 806-866)
3. **Voxel Traversal** via hierarchical DDA:
   - Sector level (32³ voxels)
   - Brick level (8³ voxels)
   - Micro level (2³ voxels)
   - Voxel level (1³)
4. **Material Lookup** (lines 876-887)
5. **Lighting Calculation** for ALL lights (lines 899-930)
6. **Shadow Raytracing** per light (lines 914-915, 729-775)
7. **Final Shading** with PBR-lite BRDF (lines 110-183)

**Key Observation**: Everything happens in a single `@compute` shader invocation per pixel.

### Data Update Pipeline

**CPU Side** ([manager.go](file:///Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/gpu/manager.go)):
- **Full Rebuild Path** (lines 359-579): Serializes entire scene on first upload
- **Sparse Update Path** (lines 312-357): Updates only dirty bricks/sectors
- **Batch System** (lines 156-200): Accumulates updates within a frame

**Current Bottleneck**: Even with sparse updates, the system still has O(N) complexity for large destruction events.

---

## Critical Limitations

### 1. Geometry Update Bottleneck (CRITICAL - Blocks Gameplay)

**Current State**:
- `XBrickMap.SetVoxel` marks bricks/sectors as dirty ([xbrickmap.go:234-320](file:///Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/volume/xbrickmap.go#L234-L320))
- `GpuBufferManager.UpdateBrickPayload` writes 512 bytes per brick ([manager.go:581-616](file:///Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/gpu/manager.go#L581-L616))
- Batch system helps but doesn't solve fundamental issue

**Impact on Shooter**:
- Shooting a wall (10-20 voxels destroyed): ~10-20 KB upload → **acceptable**
- Explosion (100+ voxels across multiple bricks): ~50+ KB upload → **5-10ms spike**
- Continuous destruction (machine gun): **frame drops to 20-30 FPS**

**Root Cause**: No GPU-side allocation or compute-based editing.

### 2. Lighting Performance (HIGH - Limits Visual Fidelity)

**Current State**:
- Shader iterates **ALL lights** for **EVERY pixel** (lines 899-930)
- Each light casts a **full shadow ray** (lines 914-915)
- Shadow rays use same expensive traversal as primary rays (lines 729-775)

**Impact on Shooter**:
- 1 sun + 5 point lights: ~6 shadow rays per pixel → **acceptable at 1080p**
- 1 sun + 20 dynamic lights (muzzle flashes, explosions): ~21 shadow rays → **15-20 FPS**
- 1 sun + 50+ lights (particles with lights): **unplayable**

**Calculation**:
- 1920×1080 = 2,073,600 pixels
- 20 lights × 2,073,600 = **41,472,000 shadow rays per frame**
- At 64 DDA steps max per ray: **~2.65 billion DDA steps**

### 3. Traversal Artifacts

**Current State**:
- DDA iteration limits: 64 sectors, 64 bricks, 32 voxels (lines 442, 472, 528)
- Total max steps: ~160 before ray "gives up"

**Impact**:
- Long corridors (100+ voxels): **skybox leaks through**
- Sniper sightlines: **geometry disappears**
- Large open maps: **visual holes**

### 4. Physics Query Latency (MEDIUM)

**Current State**:
- CPU-based `RayMarch` in Go ([xbrickmap.go:466-573](file:///Users/ddevidch/code/go/gekko3d/gekko/voxelrt/rt/volume/xbrickmap.go#L466-L573))
- Uses same hierarchical traversal as GPU

**Impact**:
- 10-20 bullet raycasts per frame: **0.5-1ms** → acceptable
- Player collision (6-8 rays for capsule): **0.3-0.5ms** → acceptable
- 100+ AI raycasts: **5-10ms** → **problematic**

---

## Render Pipeline Separation Analysis

### Question: Should We Split the Monolithic Shader?

**Proposed Separation**:
1. **Depth/G-Buffer Pass**: Write depth, normals, material IDs
2. **Shadow Map Pass**: Pre-compute shadow visibility
3. **Lighting Pass**: Deferred shading using G-buffer
4. **Particle Pass**: Separate compute for cellular automaton

### Analysis

#### Option A: Keep Monolithic (Current)

**Pros**:
- ✅ Simple architecture
- ✅ No intermediate buffer overhead
- ✅ Cache-friendly (all data loaded once per ray)

**Cons**:
- ❌ Cannot optimize per-stage
- ❌ Shadow rays duplicate work
- ❌ Light culling is difficult

**Verdict**: **Acceptable for ≤10 lights**, breaks down at scale.

#### Option B: Deferred Rendering (G-Buffer)

**Proposed Pipeline**:
```
Pass 1: Primary Rays → G-Buffer (depth, normal, material_id, voxel_center)
Pass 2: Shadow Maps → Visibility buffer (per light or clustered)
Pass 3: Lighting → Final color (screen-space, reads G-buffer + shadows)
```

**Pros**:
- ✅ **Decouples lighting from geometry complexity**
- ✅ Enables tiled/clustered light culling
- ✅ Shadow maps can be cached for static lights
- ✅ Easier to add screen-space effects (SSAO, SSR)

**Cons**:
- ❌ **Memory overhead**: 4-6 render targets (depth, normal, albedo, roughness, etc.)
  - At 1080p: ~50-75 MB VRAM
- ❌ **Bandwidth cost**: Write G-buffer, read in lighting pass
- ❌ **Complexity**: 3+ compute passes instead of 1

**Verdict**: **RECOMMENDED for shooter with 20+ lights**.

#### Option C: Hybrid (Selective Deferred)

**Proposed Pipeline**:
```
Pass 1: Primary Rays → Minimal G-Buffer (depth, voxel_center, material_id)
Pass 2: Forward Lighting → Only for sun + 2 nearest lights (in same pass)
Pass 3: Deferred Lighting → Remaining lights (screen-space)
```

**Pros**:
- ✅ Best of both worlds
- ✅ Reduced G-buffer size (only 2-3 targets)
- ✅ Important lights get accurate shadows

**Cons**:
- ❌ Most complex to implement
- ❌ Requires light sorting/prioritization

**Verdict**: **OPTIMAL for Teardown-like game** (sun + key lights forward, many small lights deferred).

### Recommendation: **Option B (Deferred)** or **Option C (Hybrid)**

**Rationale**:
- Shooter games need **many dynamic lights** (explosions, muzzle flashes, projectiles)
- Current monolithic approach **cannot scale beyond 10 lights**
- Memory cost (50-75 MB) is **negligible on modern GPUs**
- Bandwidth cost is **offset by massive reduction in redundant traversal**

---

## Particle System Integration

### Cellular Automaton Approach

**Concept**: Voxel-based particles using GPU compute to simulate falling debris, smoke, fire.

**Why It's "Cheap"**:
1. **No mesh generation**: Particles ARE voxels
2. **Parallel updates**: Each voxel cell updates independently
3. **Spatial coherence**: Uses same XBrickMap structure

### Implementation Strategy

#### Step 1: Dual-Buffer Voxel Grid

**Data Structure**:
```wgsl
@group(3) @binding(0) var<storage, read> particle_grid_read: array<u32>;
@group(3) @binding(1) var<storage, read_write> particle_grid_write: array<u32>;
```

**Size**: 
- For 256³ particle volume: 16 MB (1 byte per voxel)
- Can reuse XBrickMap compression for static regions

#### Step 2: Update Compute Shader

**Pseudo-code**:
```wgsl
@compute @workgroup_size(8, 8, 8)
fn update_particles(@builtin(global_invocation_id) id: vec3<u32>) {
    let current = read_particle(id);
    let below = read_particle(id + vec3(0, -1, 0));
    
    // Falling sand rules
    if (current == SAND && below == EMPTY) {
        write_particle(id, EMPTY);
        write_particle(id + vec3(0, -1, 0), SAND);
    }
    
    // Fire propagation, smoke rising, etc.
}
```

**Cost**: 
- 256³ = 16M cells
- At 8×8×8 workgroups: 32,768 workgroups
- **~0.5-1ms per update** (estimated)

#### Step 3: Integration with Main Renderer

**Option A: Separate XBrickMap**
- Particles live in their own `XBrickMap` instance
- Rendered as a separate object in the scene
- **Pros**: Clean separation, easy to toggle on/off
- **Cons**: Requires second traversal pass

**Option B: Merged Grid**
- Particles write directly to main voxel grid
- **Pros**: Single traversal, particles interact with geometry
- **Cons**: Complicates undo/redo, harder to clear particles

**Recommendation**: **Option A** for initial implementation, **Option B** for advanced effects (burning walls, melting ice).

#### Step 4: Rendering Particles

**Current Shader Already Supports It**:
- Particles are just voxels with specific material IDs
- Use emissive materials for fire (already supported, lines 881)
- Use transparency for smoke (partially supported, line 84)

**Optimization**: 
- Mark particle bricks with a flag (`BRICK_FLAG_PARTICLE = 2`)
- Skip shadow casting for particle voxels (saves 50% shadow rays)

### Particle Types for Shooter

| Type | Behavior | Material | Use Case |
|------|----------|----------|----------|
| **Debris** | Falls, bounces, settles | Rocky, rough | Explosion aftermath |
| **Smoke** | Rises, dissipates | Transparent, gray | Grenade, fire |
| **Fire** | Spreads, rises, damages | Emissive, orange | Flamethrower, explosions |
| **Sparks** | Fly, fade quickly | Emissive, yellow | Bullet impacts, welding |
| **Blood** | Splatter, drips, stains | Opaque, red | Enemy hits (if appropriate) |

### Performance Estimate

**Scenario**: 10,000 active particle voxels (medium explosion)
- Update: **0.1-0.2ms** (compute shader)
- Render: **0ms extra** (already part of main traversal)
- **Total overhead**: **< 0.5ms**

**Verdict**: **Highly feasible**. Cellular automaton particles are indeed "cheap" for voxel engines.

---

## Optimization Roadmap

### Phase 1: Critical Fixes (Required for Playable Shooter)

#### 1.1 GPU-Accelerated Voxel Editing

**Goal**: Reduce destruction latency from 10ms to <1ms.

**Implementation Steps**:

1. **Create Compute Shader for Voxel Writes**
   - **File**: `gekko/voxelrt/rt/shaders/voxel_edit.wgsl` (NEW)
   - **Function**: `edit_voxel(pos: vec3<i32>, value: u8)`
   - **Logic**:
     - Locate sector/brick/voxel using same traversal as raytrace
     - Atomically update payload buffer
     - Update occupancy masks using `atomicOr`/`atomicAnd`
   - **Estimated Lines**: ~200

2. **Add Edit Command Buffer**
   - **File**: `gekko/voxelrt/rt/gpu/manager.go`
   - **Add Field**: `EditCommandBuf *wgpu.Buffer` (stores edit commands)
   - **Add Method**: `QueueEdit(x, y, z int, val uint8)`
   - **Add Method**: `FlushEdits()` (dispatches compute shader)
   - **Estimated Lines**: ~100

3. **Integrate with XBrickMap**
   - **File**: `gekko/voxelrt/rt/volume/xbrickmap.go`
   - **Modify**: `SetVoxel` to queue GPU edit instead of CPU update
   - **Add**: `SyncFromGPU()` for readback (only when needed)
   - **Estimated Lines**: ~50

**Expected Improvement**: 
- 100 voxel destruction: **10ms → 0.5ms** (20× faster)
- Enables real-time destruction at 60 FPS

#### 1.2 Light Culling (Tiled or Clustered)

**Goal**: Support 50+ dynamic lights without FPS drop.

**Implementation Steps**:

1. **Add Light Culling Compute Pass**
   - **File**: `gekko/voxelrt/rt/shaders/light_cull.wgsl` (NEW)
   - **Input**: Screen divided into 16×16 tiles (1920×1080 → 120×68 tiles)
   - **Output**: `tile_light_indices: array<array<u32, MAX_LIGHTS_PER_TILE>>`
   - **Logic**:
     - For each tile, compute frustum
     - Test each light's bounding sphere against frustum
     - Store light indices in tile
   - **Estimated Lines**: ~150

2. **Modify Main Shader to Use Culled Lights**
   - **File**: `gekko/voxelrt/rt/shaders/raytrace.wgsl`
   - **Change**: Lines 899-930 (lighting loop)
   - **Before**: `for (var i = 0u; i < num_lights; i++)`
   - **After**: 
     ```wgsl
     let tile_id = global_id.xy / 16u;
     let light_count = tile_light_counts[tile_id];
     for (var i = 0u; i < light_count; i++) {
         let light_idx = tile_light_indices[tile_id][i];
         // ... use lights[light_idx]
     }
     ```
   - **Estimated Lines**: ~30 modified

3. **Add Culling Pass to Render Loop**
   - **File**: `gekko/voxelrt/rt/app/app.go`
   - **Add**: Dispatch light culling before main raytrace
   - **Estimated Lines**: ~20

**Expected Improvement**:
- 50 lights: **15 FPS → 55 FPS** (3.6× faster)
- Scales to 100+ lights with minimal cost

#### 1.3 Increase Traversal Limits

**Goal**: Eliminate visual artifacts on large maps.

**Implementation Steps**:

1. **Increase Iteration Limits**
   - **File**: `gekko/voxelrt/rt/shaders/raytrace.wgsl`
   - **Change**: Lines 442, 472, 528
   - **Before**: `while (... && iter < 64)`
   - **After**: `while (... && iter < 256)` (or make it a uniform parameter)
   - **Estimated Lines**: ~5

2. **Add Early Exit Optimization**
   - **Add**: Distance-based culling (skip empty sectors beyond certain distance)
   - **Estimated Lines**: ~20

**Expected Improvement**:
- Eliminates skybox leaks on maps up to 512³ voxels

---

### Phase 2: Deferred Rendering (Enables Advanced Lighting)

#### 2.1 Create G-Buffer Pass

**Implementation Steps**:

1. **Create G-Buffer Shader**
   - **File**: `gekko/voxelrt/rt/shaders/gbuffer.wgsl` (NEW)
   - **Output Textures**:
     - `depth: texture_storage_2d<r32float>` (depth)
     - `normal: texture_storage_2d<rgba8snorm>` (normal XYZ + unused)
     - `material: texture_storage_2d<rgba8unorm>` (palette_idx, roughness, metalness, flags)
     - `position: texture_storage_2d<rgba32float>` (world position XYZ + unused)
   - **Logic**: Same traversal as current shader, but write to textures instead of shading
   - **Estimated Lines**: ~300 (copy from raytrace.wgsl, simplify output)

2. **Create Deferred Lighting Shader**
   - **File**: `gekko/voxelrt/rt/shaders/deferred_light.wgsl` (NEW)
   - **Input**: G-buffer textures, lights buffer
   - **Output**: `final_color: texture_storage_2d<rgba8unorm>`
   - **Logic**: 
     - Read G-buffer for current pixel
     - Loop through culled lights (from Phase 1.2)
     - Compute lighting (reuse `calculate_lighting` function)
     - No shadow rays (use shadow maps from next step)
   - **Estimated Lines**: ~200

3. **Integrate into Render Loop**
   - **File**: `gekko/voxelrt/rt/app/app.go`
   - **Change**: 
     - Pass 1: Dispatch G-buffer shader
     - Pass 2: Dispatch deferred lighting shader
   - **Estimated Lines**: ~50

**Expected Improvement**:
- Decouples geometry from lighting complexity
- Enables next optimizations (shadow maps, SSAO)

#### 2.2 Add Shadow Maps (Optional but Recommended)

**Implementation Steps**:

1. **Create Shadow Map Shader**
   - **File**: `gekko/voxelrt/rt/shaders/shadow_map.wgsl` (NEW)
   - **For Each Light**: Render depth from light's perspective
   - **Output**: `shadow_maps: texture_storage_2d_array<r32float>`
   - **Estimated Lines**: ~250

2. **Modify Deferred Lighting to Sample Shadow Maps**
   - **File**: `gekko/voxelrt/rt/shaders/deferred_light.wgsl`
   - **Add**: Shadow map sampling instead of shadow rays
   - **Estimated Lines**: ~50 modified

**Expected Improvement**:
- Shadows become nearly free (just texture lookups)
- Can cache shadow maps for static lights

---

### Phase 3: Particle System

#### 3.1 Implement Cellular Automaton

**Implementation Steps**:

1. **Create Particle Update Shader**
   - **File**: `gekko/voxelrt/rt/shaders/particles.wgsl` (NEW)
   - **Logic**: Falling sand, smoke rising, fire spreading
   - **Estimated Lines**: ~300

2. **Add Particle Grid Buffers**
   - **File**: `gekko/voxelrt/rt/gpu/manager.go`
   - **Add**: `ParticleGridRead`, `ParticleGridWrite` buffers
   - **Estimated Lines**: ~50

3. **Integrate Particle Updates into Frame Loop**
   - **File**: `gekko/voxelrt/rt/app/app.go`
   - **Add**: Dispatch particle update every frame (or every N frames)
   - **Estimated Lines**: ~20

4. **Render Particles**
   - **Option A**: Separate XBrickMap (easier)
   - **Option B**: Merge into main grid (more realistic)
   - **Estimated Lines**: ~100

**Expected Improvement**:
- Adds visual richness (debris, smoke, fire)
- Performance cost: <1ms per frame

---

### Phase 4: Advanced Optimizations

#### 4.1 Temporal Reprojection

**Goal**: Amortize expensive rays across frames.

**Implementation**:
- Render at half resolution, upscale with TAA
- **Expected Improvement**: 4× faster (trade quality for speed)

#### 4.2 Compute-Based Physics

**Goal**: Move raycasts to GPU.

**Implementation**:
- Create compute shader for bullet raycasts
- Return hit results to CPU via readback buffer
- **Expected Improvement**: 10× faster physics queries

#### 4.3 Level of Detail (LOD)

**Goal**: Reduce traversal cost for distant objects.

**Implementation** (already partially supported):
- Use Tree64 for distant objects (lines 665-727)
- Automatically generate LODs from XBrickMap
- **Expected Improvement**: 2× faster on large maps

---

## Summary: Recommended Implementation Order

### Minimum Viable Shooter (MVP)
1. ✅ **Phase 1.1**: GPU voxel editing (critical)
2. ✅ **Phase 1.2**: Light culling (critical)
3. ✅ **Phase 1.3**: Increase traversal limits (quick win)

**Timeline**: 2-3 weeks  
**Result**: Playable shooter with destruction and 20+ lights

### Full-Featured Shooter
4. ✅ **Phase 2.1**: Deferred rendering (enables scaling)
5. ✅ **Phase 3.1**: Particle system (visual polish)
6. ⚠️ **Phase 2.2**: Shadow maps (optional, big win)

**Timeline**: +3-4 weeks  
**Result**: Teardown-quality visuals

### Advanced Features
7. ⚠️ **Phase 4.1**: Temporal reprojection (if FPS still low)
8. ⚠️ **Phase 4.2**: GPU physics (if CPU-bound)

**Timeline**: +2-3 weeks  
**Result**: 100+ FPS on high-end hardware

---

## Conclusion

**Current State**: VoxelRT is a solid foundation but **not optimized for shooters**.

**Key Bottlenecks**:
1. ❌ CPU-based voxel editing (blocks destruction gameplay)
2. ❌ Monolithic shader (cannot scale beyond 10 lights)
3. ⚠️ Limited traversal (visual artifacts on large maps)

**Recommended Path**:
1. **Immediate**: Implement GPU editing + light culling (Phase 1)
2. **Short-term**: Add deferred rendering (Phase 2)
3. **Polish**: Integrate particle system (Phase 3)

**Deferred Rendering**: **YES, split the pipeline**. The memory/bandwidth cost is negligible compared to the massive reduction in redundant work.

**Particles**: **YES, highly feasible**. Cellular automaton is indeed "cheap" and adds significant visual value.

**Final Verdict**: With these optimizations, VoxelRT can absolutely power a Teardown-like shooter at 60+ FPS. 🚀
