# Performance Analysis & Improvement Plan: Voxel Shooter

This document analyzes the suitability of the current VoxelRT engine for a fast-paced shooter game (similar to Voxelstein 3D) and outlines necessary performance improvements.

## 1. Limitations Analysis

The current engine is designed for static or slowly changing scenes. Converting it to a dynamic shooter reveals several critical bottlenecks.

### A. Critical Bottleneck: Geometry Updates (Destruction)
*   **Current State**: `GpuBufferManager` rebuilds the entire voxel buffer (Sectors, Bricks, Payload) whenever `SetVoxel` is called.
*   **Impact**: Shooting a wall to destroy it will cause a massive frame time spike (hundreds of milliseconds for large scenes), making the game unplayable during action.
*   **Reason**: O(N) CPU cost to serialize entire scene + O(N) bandwidth to upload it.

### B. High Latency: Physics Queries
*   **Current State**: Raycasting (bullets, player collision) is done via `RayMarch` on the CPU (Go).
*   **Impact**: Go's CPU traversal is significantly slower than GPU. Doing 10-20 raycasts per frame for bullets/enemies might be fine, but complex collision for physics (player movement against complex voxel terrain) will eat into CPU time.

### C. Shading & Lighting Performance
*   **Current State**: The shader iterates *all* lights for *every* hit.
*   **Impact**: Muzzle flashes, explosions, and projectiles create many dynamic lights. With 20+ dynamic lights, the inner loop `(Lights * RaySteps)` becomes prohibitively expensive.
*   **Visuals**: Shadows are hard raytraced. This is accurate but expensive (doubles/triples traversal cost per pixel).

### D. Rendering Artifacts
*   **Current State**: 64-step limit in DDA traversal.
*   **Impact**: Long corridors or looking across large maps will show "holes" or skybox leaking through geometry where the ray "gives up".

## 2. Improvement Plan

To achieve a stable 60+ FPS Voxelstein experience, the following optimizations are required, in order of priority.

### Phase 1: Dynamic Editing (The "Destruction" Fix)

This is the most critical fix. You cannot have a shooter without it.

1.  **Implement Sparse GPU Updates**:
    *   Stop rebuilding buffers.
    *   Implement a **Simple Block Allocator** in `GpuBufferManager`.
    *   **Action**: When a voxel changes:
        *   If it fits in existing brick payload (overwriting non-zero with non-zero): Use `queue.WriteBuffer` to update *only* that byte.
        *   If it requires new brick allocation: Allocate a slot from a "Free List" on the GPU buffer, update the Sector Table to point to it, and upload only the new Brick Record.
    *   **Goal**: Complexity drops from O(N) to O(1).
2.  **Chunked Uploads**: 
    *   For massive explosions, mark "Dirty Chunks" and only re-upload those sectors.

### Phase 2: Shooter Physics

1.  **Optimized CPU Traversal**:
    *   The current `RayMarch` in Go is a direct port. Optimize it by using flat arrays instead of map lookups for the "Local Activity Sector" (caching the player's surroundings).
2.  **Compute Shader Collision (Optional)**:
    *   If CPU physics is too slow, move particle/projectile collision to a Compute Shader that runs before the render pass.

### Phase 3: Rendering Speed

1.  **Light Culling (Tiled/Clustered)**:
    *   Divide screen into 16x16 tiles.
    *   Compute which lights touch which tile.
    *   Shader only loops through relevant lights.
    *   **Goal**: Support 100+ dynamic lights (explosions/projectiles).
2.  **Shadow Optimization**:
    *   **Option A (Fast)**: Screen-Space Interaction Shadows (SSAO/SSDO) instead of full raytraced shadows for small details.
    *   **Option B (Hybrid)**: Only cast shadows from the Sun and 1-2 nearest persistent lights. Ignore transient lights (muzzle flashes) for shadows.

### Phase 4: Visuals & Polish

1.  **MagicaVoxel Imports**:
    *   Complete `MATL` integration (already in progress) to allow glowing magma, metallic guns, etc.
2.  **Blue Noise Dithering**:
    *   Use blue noise for ray start positions or shadow bias to trade banding for noise, which TAA (Temporal Anti-Aliasing) can then smooth out.
3.  **Weapon Viewmodel**:
    *   Render the gun as a separate pass (standard rasterization or separate voxel object) to prevent it from clipping into walls.

## 3. Technology Stack Recommendation

*   **Language**: Go is fine for logic, but keep the "heavy lifting" (traversal/updates) strictly optimized or moved to Compute Shaders.
*   **Graphics API**: WebGPU (via wgpu) is excellent for this. It handles the compute pipelines well.

## 4. Immediate Next Steps

1.  **Profile** `xbrickmap.go`'s `SetVoxel` to confirm the bottleneck.
2.  **Refactor `GpuBufferManager`**:
    *   Add `UpdateVoxel(x, y, z, val)` method.
    *   Implement `queue.WriteBuffer` for single-byte updates.
