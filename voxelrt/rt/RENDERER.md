# VoxelRT Renderer: Architecture and Frame Pipeline

This document explains how the real-time voxel renderer is structured, how it executes each frame, how resources are organized and bound, and how the various GPU passes and shaders interact.

Related docs
- PARTICLES.md — particle system, CPU sim + billboard pass (WBOIT accumulation)
- SHADER DOCS (one file per shader):
  - shaders/GBUFFER.md
  - shaders/DEFERRED_LIGHTING.md
  - shaders/SHADOW_MAP.md
  - shaders/PARTICLES_BILLBOARD.md
  - shaders/TRANSPARENT_OVERLAY.md
  - shaders/RESOLVE_TRANSPARENCY.md
  - shaders/FULLSCREEN.md
  - shaders/DEBUG.md
  - shaders/TEXT.md

These per-shader docs describe uniforms, bindings, algorithms, and tuning notes for each shader module.

## High-level Overview

- ECS (gekko package) builds the scene (voxel objects, lights, camera, text, particles data).
- The VoxelRtModule (gekko/mod_vox_rt.go) bridges ECS → renderer:
  - Syncs ECS components into rtApp.Scene and rtApp.Camera.
  - Collects CPU-simulated billboard particles then uploads instances.
  - Kicks rtApp.Update() followed by rtApp.Render() per frame.
- The renderer (rt/app/app.go) manages:
  - WebGPU device/swapchain, pipelines, textures, and bind groups.
  - Frame graph (current):
    - Compute G-Buffer
    - Compute Shadows
    - Compute Deferred Lighting (produces opaque lit color in a StorageTexture RGBA8)
    - Optional Compute Debug
    - Render Accumulation (Transparent Overlay + Particles) into WBOIT accum/weight targets
    - Render Resolve Transparency (composite opaque + accum/weight) to swapchain
    - Render Text overlay
  - FPS tracking and debug compute pass (optional).

## Core Types and Responsibilities

- App (rt/app/app.go)
  - Owns Device/Queue/Surface/Config.
  - Creates pipelines:
    - Compute: G-Buffer, Deferred Lighting, Shadow, Debug (optional).
    - Render: Transparent Overlay (fullscreen), Particles (billboards), Resolve Transparency (fullscreen), Text.
  - Manages storage/GB textures and bind groups.
  - Orchestrates render passes per frame.

- BufferManager (rt/gpu/manager.go)
  - Allocates and updates GPU buffers for scene (camera, instances, materials, lights, voxels).
  - Owns creation of G-Buffer, Lighting, Shadow bind groups.
  - Creates and manages WBOIT accumulation targets (RGBA16F accum, R16F weight).
  - Handles particle instance buffer upload and bind groups per frame.

- Scene and Camera (rt/core/*.go)
  - Scene: voxel objects, materials, lights; supports Commit() to (re)build GPU representations.
  - CameraState: view/projection matrices and debug mode.

- Editor (rt/editor/editor.go)
  - Handles picking and editing operations (brush, applying voxel edits).
  - Not involved in render pipeline logic; used by App for events/picking.

## Frame Lifecycle

Flow (voxelRtSystem in gekko/mod_vox_rt.go):
1) ECS sync:
   - Add/remove/transform voxel objects → rtApp.Scene.
   - Update Camera from CameraComponent.
   - Gather Text items (HUD/debug).
   - CPU-simulate particles and CA-bridge and upload instances via BufferManager.UpdateParticles.

2) rtApp.Update() (rt/app/app.go)
   - Computes matrices (view, proj, inv) and writes camera uniforms.
   - Commits scene changes and updates buffers; may recreate bind groups if buffers reallocated.
   - Ensures G-Buffer, Lighting, Shadow, Particles, Transparent Overlay group bindings are valid.

3) rtApp.Render()
   - Compute Pass: G-Buffer (gbuffer.wgsl)
     - Computes and writes per-pixel G-Buffer targets (depth t, normal, material, position).
     - Dispatch: wgX=(Width+7)/8, wgY=(Height+7)/8.
   - Shadow Pass: (shadow_map.wgsl)
     - Generates or updates shadow maps (2D array). Number of lights affects work.
   - Compute Pass: Deferred Lighting (deferred_lighting.wgsl)
     - Samples G-Buffer + lights + shadow maps and writes final opaque color to a storage texture (RGBA8).
   - Compute Pass: Debug (debug.wgsl, optional)
     - Can visualize intermediate buffers to the storage texture.
   - Render Pass: Accumulation (WBOIT)
     - Transparent Overlay (transparent_overlay.wgsl): fullscreen raycast shading of nearest transparent voxel surface before opaque t; outputs accum/weight.
     - Particles (particles_billboard.wgsl): additive billboards contributing accum/weight with depth weighting.
     - Color attachments:
       - [0] Transparent Accum RGBA16Float (premultiplied color × weight)
       - [1] Transparent Weight R16Float (alpha × weight)
   - Render Pass: Resolve Transparency (resolve_transparency.wgsl)
     - Composites opaque StorageTexture (RGBA8) + accum/weight to the swapchain.
   - Render Pass: Text (text.wgsl)
     - Overlay text rendering using an atlas.

## Render Targets and Formats

- Storage output (opaque lit, intermediate): RGBA8 (write-only storage texture).
  - Deferred Lighting writes the final opaque image here.
- G-Buffer (created by BufferManager):
  - Depth: RGBA32Float (sampled as UnfilterableFloat). X stores ray distance “t” from camera.
  - Normal: RGBA16Float (sampled as Float). Encodes world normal and possibly packing extras.
  - Material: RGBA32Float (sampled as UnfilterableFloat). PBR properties (base color/metal/roughness/etc.).
  - Position: RGBA32Float (sampled as UnfilterableFloat). World-space position (or compressed).
- Transparency (WBOIT accumulation targets; created by BufferManager):
  - Accum: RGBA16Float (render attachment + sampled, unfilterable float when read)
  - Weight: R16Float (render attachment + sampled, unfilterable float when read)
- Shadow maps: 2D array (RGBA32Float currently), bound as sampled textures for lighting.

Notes
- Exact packing/semantics are defined by the corresponding shader code; see the per-shader docs.
- The previous fullscreen “blit” of the StorageTexture is superseded by the Resolve Transparency pass.

## Bind Group Layouts (key ones)

- Deferred Lighting (rt/app/app.go)
  - BGL0 (camera+lights):
    - binding(0): CameraData (uniform)
    - binding(1): Lights buffer (read-only storage)
  - BGL1 (G-Buffer + out color + shadows):
    - binding(0): Depth (RGBA32F, sampled unfilterable)
    - binding(1): Normal (RGBA16F, sampled float)
    - binding(2): Material (RGBA32F, sampled unfilterable)
    - binding(3): Position (RGBA32F, sampled unfilterable)
    - binding(4): Output color (storage RGBA8)
    - binding(5): Shadow maps (2D array texture)
  - BGL2 (materials/sectors):
    - binding(3): Materials/sector buffer (read-only storage)

- Transparent Overlay (rt/app/app.go::setupTransparentOverlayPipeline)
  - BGL0 (fragment):
    - binding(0): CameraData (uniform)
    - binding(1): Instances buffer (read-only storage)
    - binding(2): BVH nodes (read-only storage)
    - binding(3): Lights buffer (read-only storage)
  - BGL1 (fragment, voxel data):
    - binding(0..7): SectorTable, BrickTable, VoxelPayload, Materials, ObjectParams, Tree64, SectorGrid, SectorGridParams (all read-only storage)
  - BGL2 (fragment):
    - binding(0): GBuffer Depth (RGBA32F, sampled unfilterable)
    - binding(1): GBuffer Material (RGBA32F, sampled unfilterable)

- Particles (rt/app/app.go::setupParticlesPipeline)
  - BGL0:
    - binding(0): CameraData (uniform, VS/FS visibility)
    - binding(1): Instances buffer (read-only storage, VS)
  - BGL1:
    - binding(0): GBuffer depth (RGBA32F, sampled unfilterable)

- Resolve Transparency (rt/app/app.go::setupResolvePipeline)
  - BGL0:
    - binding(0): Opaque lit color (StorageTexture view sampled as float RGBA8)
    - binding(1): Transparent accum (RGBA16F, unfilterable float)
    - binding(2): Transparent weight (R16F, unfilterable float)
    - binding(3): Sampler

## Pass Ordering and Rationale

- Compute G-Buffer first: decouples voxel shading from lighting, produces reusable data.
- Shadows before lighting: provides per-light shadow textures for deferred lighting.
- Deferred Lighting: writes opaque shading into a storage texture (off-screen RGBA8).
- Accumulation (WBOIT): Transparent Overlay and Particles write depth-weighted contributions into accum/weight targets. No sorting needed.
- Resolve: composites opaque + transparent accumulations into the swapchain.
- Text: drawn last as UI overlay.

This yields:
- Flexibility to visualize intermediate buffers (debug compute pass).
- Order-independent, stable transparency composition via WBOIT approximation.

## Particles and Depth

See PARTICLES.md for details. Highlights:
- Billboard particles perform a manual depth test by comparing per-pixel ray distance “t” against G-Buffer depth (t_scene).
- WBOIT weighting (in particles and transparent overlay):
  - z = clamp(t_particle / max(t_scene, eps), 0..1)
  - w = max(1e-3, alpha) × pow(1 − z, k) with k≈8
  - accum.rgb += color.rgb × alpha × w; weight += alpha × w
- Resolve pass reconstructs transparent color as accum.rgb / max(weight, eps), then adds to opaque.

## Thread Group Size and Dispatch

- G-Buffer and Lighting compute shaders use an 8×8 workgroup in screen tiles:
  - dispatch ( (W+7)/8, (H+7)/8, 1 ).
- This is a common tile size; alter if profiling shows better occupancy with different sizes.

## Visibility and Culling

VoxelRT implements a two-stage visibility culling system to reduce GPU workload by filtering out objects that are not visible to the camera.

### CPU Frustum Culling

Location: rt/core/camera.go, rt/core/scene.go

The first stage performs frustum culling on the CPU before any GPU work:

1. **Frustum Extraction**: `CameraState.ExtractFrustum(viewProj)` extracts 6 planes (Left, Right, Bottom, Top, Near, Far) from the view-projection matrix using the Gribb-Hartmann method.

2. **AABB Test**: `AABBInFrustum(aabb, planes)` tests each object's world-space AABB against all 6 planes:
   - For each plane, finds the "positive vertex" (corner furthest in the direction of the normal).
   - If this vertex is behind any plane, the AABB is fully outside the frustum.

3. **Integration**: `Scene.Commit()` now accepts frustum planes and filters `Objects` into `VisibleObjects`.

### GPU Hi-Z Occlusion Culling

Location: rt/gpu/manager_hiz.go, rt/shaders/hiz.wgsl

The second stage uses a Hierarchical Z-Buffer (Hi-Z) to cull objects hidden behind other geometry:

1. **Hi-Z Generation**: After the G-Buffer pass, a compute shader builds a mip chain from the depth buffer:
   - Each mip level stores the MAX depth (furthest distance) of a 2×2 region from the previous level.
   - Uses R32Float format; stores linear ray distance (same as G-Buffer depth).

2. **CPU Readback**: A low-resolution mip (~64 pixels wide) is read back to CPU with 1-frame latency:
   - Uses async buffer mapping to avoid stalls.
   - State machine manages copy → map → read cycle.

3. **Occlusion Test**: `IsOccluded(aabb, hizData, w, h, lastViewProj)` on CPU:
   - Projects AABB corners to screen space using the PREVIOUS frame's view-projection (temporal latency).
   - Finds the nearest depth of the AABB (using clip.W as conservative approximation).
   - Samples the Hi-Z buffer to find the maximum occluder depth in that screen region.
   - If `minObjectDepth > maxOccluderDepth`, the object is fully hidden.

### Data Flow

```
Scene.Commit(planes, hizData, hizW, hizH, lastViewProj)
   │
   ├─ Frustum Culling (per object)
   │     └─ AABBInFrustum(worldAABB, planes)
   │
   └─ Hi-Z Occlusion Culling (per surviving object)
         └─ IsOccluded(worldAABB, hizData, w, h, lastViewProj)
                │
                └─ VisibleObjects[] (used by BufferManager)
```

### Key Files

- **rt/core/camera.go**: `ExtractFrustum()` - frustum plane extraction
- **rt/core/scene.go**: `AABBInFrustum()`, `IsOccluded()`, `Scene.Commit()` with culling
- **rt/gpu/manager_hiz.go**: Hi-Z texture setup, mip generation dispatch, async readback
- **rt/shaders/hiz.wgsl**: 2×2 MAX reduction compute shader
- **rt/core/culling_test.go**: Unit tests for frustum and occlusion culling

### Performance Notes

- Frustum culling is O(objects × 6 planes), very cheap on CPU.
- Hi-Z culling uses 1-frame-old data (temporal lag) but avoids GPU sync stalls.
- Hi-Z readback targets ~64px width for minimal bandwidth while maintaining culling accuracy.
- Objects that touch the near plane are conservatively marked visible.

### Limitations

- Hi-Z uses previous frame's depth: fast-moving objects or rapid camera turns may have false negatives (visible objects culled for 1 frame).
- No per-sector culling yet; culling is at object granularity.
- Readback latency means Hi-Z culling is most effective for static or slow-moving content.

## Synchronization and Buffer Re-creation

- Scene.Commit() marks/assembles changes for GPU.
- BufferManager.UpdateScene(Scene) will:
  - Recreate buffers that change in size (instances/materials/sectors/…).
  - Trigger re-creation of bind groups that depend on those buffers (lighting, shadows, debug).
- Particles:
  - UpdateParticles(instances) re-allocates instance buffer as needed.
  - Particles bind groups may be re-created every frame to handle dynamic buffer sizes.

## Debug Facilities

- Debug compute pass can replace the storage texture content for inspection.
- Text overlay supports FPS and custom messages.
- Editor integration allows picking and modifying voxels at runtime.

## Troubleshooting

- Wrong or missing output:
  - Ensure Surface/Config are valid and textures re-created on resize.
  - Verify bind group sample types match texture formats (e.g., depth as UnfilterableFloat).
- Black screen after changes:
  - Check that G-Buffer and Lighting bind groups were re-created after buffer/texture changes.
- Transparency not visible:
  - Verify TransparentAccum/Weight textures and their views exist.
  - Check accumulation pass has both color attachments bound and pipelines/bind groups set.
  - Ensure Resolve pipeline’s bind group (opaque/accum/weight/sampler) is valid after resize.
- Tile artifacts:
  - Validate workgroup size and boundary conditions in compute shaders.

## Future Work (Renderer)

- Multi-target G-Buffer variants and packing optimization.
- Tiled/clustered lighting for large light counts.
- Async compute for independent passes where feasible.
- Temporal accumulation options (denoise/refine).
- GPU-driven indirect draws for particles and voxel instances.

## Current Renderer Analysis

Strengths
- Clear pass graph: G-Buffer → Shadows → Deferred Lighting → Accumulation (Transparent Overlay + Particles) → Resolve → Text.
- ECS-first integration: scene sync and particle upload are cleanly separated.
- Compute-based G-Buffer and lighting simplify platform constraints and allow flexible buffers/bindings.
- WBOIT transparency path avoids sorting and composes effects robustly.

Key constraints and bottlenecks
- Scene buffer churn:
  - BufferManager may recreate buffers and bind groups when sizes change (edits or many objects), which can be costly on frames with destruction/edits spikes.
  - Particle bind groups may be recreated every frame to handle dynamic buffer sizes.
- Lighting scalability:
  - Lighting is per-pixel over all lights; without tiling/clustered culling, many small lights typical in shooter scenes can be expensive.
  - Shadow maps are computed per light; without caching/LOD, they can dominate cost.
- G-Buffer bandwidth:
  - Depth as RGBA32F and multiple RGBA32F targets drive bandwidth and cache pressure.
- Transparency approximation:
  - WBOIT is an approximation; complex overlapping translucent stacks may deviate from ground truth.
- Culling:
  - ✅ CPU frustum culling per object is now implemented.
  - ✅ Hi-Z occlusion culling per object is now implemented.
  - Per-sector culling within objects is not yet implemented.
- Asynchrony:
  - Passes run in sequence; little overlap of CPU jobs (edits, rebuilds) with GPU work.
- Temporal stability:
  - No TAA/temporal accumulation; aliasing and noise from fine voxel details and particles are more visible during motion.
- Streaming/world scale:
  - For large worlds (shooter with destruction and traversal), no paging/streaming of sectors yet.
- Destruction pipeline:
  - Edits go through full buffer rebuild more often than necessary; no per-chunk dirty upload or background build queues.

## Shooter-Focused Improvements Roadmap

Target: robust, destructible voxel shooter (Teardown-like), 60–120 FPS at 1080p with tens of dynamic lights, heavy particles/debris, and frequent edits.

Phase 0 — Baseline stability and profiling
- Add scoped GPU/CPU timers and frame captures per pass (G-Buffer, Shadows, Lighting, Accum, Resolve, Particles).
- Per-frame counters: number of objects, sectors, bricks, edits, particle count, lights, shadowed lights.
- Crash-proof edits: cap edit throughput per frame; defer large edits to background job queue.

Phase 1 — Data flow, memory, and uploads
- Dirty-region updates:
  - Track per-object dirty sectors/bricks; serialize only changed chunks to GPU buffers.
  - Maintain stable buffer pools and allocate from slabs; avoid wholesale reallocation.
- Bind-group stability:
  - Keep bind group layouts static; manage buffers via sized pools and offsets.
  - Cache/reuse particle/overlay bind groups; prefer fixed-capacity instance buffers that grow in steps (1.5x).
- Background build:
  - Move brick/sectors serialization to worker goroutines; stage into staging buffers; copy on next frame.
- Streaming/paging:
  - Optional world paging: per-Region activation based on player proximity; background load and eviction.

Phase 2 — Visibility and culling
- ✅ CPU frustum culling per object:
  - Implemented in `Scene.Commit()` using `AABBInFrustum()`. Filters `Objects` into `VisibleObjects`.
- ✅ Hi-Z occlusion culling:
  - Implemented via `manager_hiz.go` and `hiz.wgsl`. Uses 1-frame latency async readback.
  - `IsOccluded()` tests AABB against previous frame's Hi-Z buffer.
- Per-sector culling (within object):
  - TODO: Cull sectors by camera frustum within each visible object.
- Screen-space tiling:
  - Optionally skip G-Buffer tiles with no geometry by prepass raster (coarse depth tiles).
- LOD policy:
  - Per-object and per-sector LOD (brick decimation, Tree64) for distant content with stable transitions.

Phase 3 — Lighting and shadows scalability
- Tiled/clustered lighting:
  - Build per-tile or clustered light lists in compute; shade only with relevant lights.
- Shadow cost control:
  - Per-light quality tiers; cache directional shadow maps across frames (only update on camera/light change).
  - PCF kernels with variable radius; bias tuning per light type.
  - Lower-res shadows for distant/low-intensity lights.
- G-Buffer packing:
  - Consider R32F for depth, RG16F for normals, packed material in RGBA8/16F, to reduce bandwidth.

Phase 4 — Destruction, debris, and particles
- Edits throughput:
  - Batch voxel edits by explosion/time slice; accumulate voxel deltas and apply in chunks.
- Debris:
  - Emit rigidbody debris (coarse vox chunks) plus particles; limit count and apply lifetime LOD (fade or despawn).
- Particles quality:
  - Soft particles (fade based on |t_scene − t_particle|).
  - Velocity-aligned billboards for sparks/fragments.
  - Flipbooks/atlas animations; distance/importance-based spawn LOD.
- GPU-driven particles (optional):
  - Keep CPU emitters for gameplay; migrate integration to compute when needed.

Phase 5 — Temporal stability and anti-aliasing
- TAA option with motion vectors (from camera matrices and per-pixel depth/normal).
- Sharpen/resolve tuned for voxel edges; optional jitter to fight aliasing.
- Optional dynamic resolution scaling (DRS) to maintain frame time budget.

Phase 6 — Tooling and authoring
- Editor:
  - Visual brush gizmos, falloff, box/line/fill tools, undo/redo with sparse deltas.
  - Heatmaps for sector activity and cost.
- Debug views:
  - Visualize tiles/clusters, culled sectors, shadow cascades/tiers, light list density.
- Asset pipeline:
  - Precompute sector/brick metadata, LOD levels, and streaming regions.

Phase 7 — Physics and gameplay integration
- Collision:
  - Export simplified collision meshes per sector; incremental updates on edits.
- Nav/AI:
  - Incremental navmesh updates or voxel-pathfinding confined to Regions.
- Audio:
  - Cheap occlusion via voxel rays or sector material flags.

Phase 8 — Optional GI/path effects (later milestone)
- Voxel cone tracing or SDF-based AO/indirect; screen-space GI as stopgap.
- Temporal accumulation/denoise for indirect.

Engineering practices
- Frame budget gates: hard caps for lights, edits per frame, particles. Backoff strategies under load.
- Determinism options for replay if needed.
- Configurable quality presets (Low/Medium/High) toggling: shadow res/PCF size, G-Buffer formats, particle caps, lighting tiles, TAA/DRS.

Milestone goals (example)
- M1: 1080p 60 FPS on mid-range GPU, 1–2 large explosions per second, ≤20 shadowed lights, 30k particles peak.
- M2: 1440p 60 FPS, paging enabled, clustered lighting, soft particles, TAA.
- M3: Large levels with streaming, robust editing tools, stable multiplayer-compatible deterministic edits (if applicable).

References in this repo
- See PARTICLES.md for particle tuning/soft-particles ideas (updated for WBOIT).
- See SHADOW_MAP.md for shadow tiering and bias/PCF notes.
