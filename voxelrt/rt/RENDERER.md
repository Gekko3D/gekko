# VoxelRT Renderer: Architecture and Frame Pipeline

This document explains how the real-time voxel renderer is structured, how it executes each frame, how resources are organized and bound, and how the various GPU passes and shaders interact.

Related docs
- PARTICLES.md — particle system, CPU sim + billboard pass
- SHADER DOCS (one file per shader):
  - shaders/GBUFFER.md
  - shaders/DEFERRED_LIGHTING.md
  - shaders/SHADOW_MAP.md
  - shaders/PARTICLES_BILLBOARD.md
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
  - G-Buffer compute pass → Shadow pass → Deferred Lighting compute → Blit → Particles → Text.
  - FPS tracking and debug compute pass (optional).

## Core Types and Responsibilities

- App (rt/app/app.go)
  - Owns Device/Queue/Surface/Config.
  - Creates pipelines:
    - Compute: GBuffer, Deferred Lighting, Shadow, Debug (optional).
    - Render: Blit (fullscreen), Particles (billboards), Text.
  - Manages storage/GB textures and bind groups.
  - Orchestrates render passes per frame.

- BufferManager (rt/gpu/manager.go)
  - Allocates and updates GPU buffers for scene (camera, instances, materials, lights, voxels).
  - Owns creation of G-Buffer, Lighting, Shadow bind groups.
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
   - Ensures G-Buffer, Lighting, Shadow, Particles group bindings are valid.

3) rtApp.Render()
   - Compute Pass: GBuffer (gbuffer.wgsl)
     - Computes and writes per-pixel G-Buffer targets (depth t, normal, material, position).
     - Dispatch: wgX=(Width+7)/8, wgY=(Height+7)/8.
   - Shadow Pass: (shadow_map.wgsl)
     - Generates or updates shadow maps (2D array). Number of lights affects work.
   - Compute Pass: Deferred Lighting (deferred_lighting.wgsl)
     - Samples G-Buffer + lights + shadow maps and writes final color to a storage texture (RGBA8).
   - Compute Pass: Debug (debug.wgsl, optional)
     - Can visualize intermediate buffers to the storage texture.
   - Render Pass: Fullscreen Blit (fullscreen.wgsl)
     - Copies/blits the storage texture to the swapchain color target.
   - Render Pass: Particles (particles_billboard.wgsl)
     - Additive billboards, manual depth test against G-Buffer depth, drawn on the swapchain.
   - Render Pass: Text (text.wgsl)
     - Overlay text rendering using an atlas.

## Render Targets and Formats

- Storage output (intermediate): RGBA8 (write-only storage texture).
  - Deferred Lighting writes the final shaded image here.
- G-Buffer (created by BufferManager):
  - Depth: RGBA32Float (sampled as UnfilterableFloat). X stores ray distance “t” from camera.
  - Normal: RGBA16Float (sampled as Float). Encodes world normal and possibly packing extras.
  - Material: RGBA32Float (sampled as UnfilterableFloat). PBR properties (base color/metal/roughness/etc.).
  - Position: RGBA32Float (sampled as UnfilterableFloat). World-space position (or compressed).
- Shadow maps: 2D array (format depends on shadow pipeline), bound as sampled textures for lighting.

Notes
- Exact packing/semantics are defined by the corresponding shader code; see the per-shader docs.

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

- Particles (rt/app/app.go::setupParticlesPipeline)
  - BGL0:
    - binding(0): CameraData (uniform, VS/FS visibility)
    - binding(1): Instances buffer (read-only storage, VS)
  - BGL1:
    - binding(0): GBuffer depth (RGBA32F, sampled unfilterable)

## Pass Ordering and Rationale

- Compute G-Buffer first: decouples voxel shading from lighting, produces reusable data.
- Shadow pass before lighting: provides per-light shadow textures for deferred lighting.
- Deferred Lighting: writes final shading into a storage texture (off-screen).
- Blit: copies storage texture to swapchain (allows compute pipelines irrespective of swapchain format).
- Particles: additively blended in screen space, sampling G-Buffer depth to emulate depth test.
- Text: drawn last as UI overlay.

This yields:
- Flexibility to visualize intermediate buffers (debug compute pass).
- Stable composition order (deferred → scene-space particles → text).

## Particles and Depth

See PARTICLES.md for details. Highlights:
- Billboard particles perform a manual depth test by comparing per-pixel ray distance “t” against G-Buffer depth.
- Stability pass (implemented):
  - Use clamped pixel coords to sample G-Buffer depth.
  - Use instance center for computing t_particle.
  - Slightly larger epsilon to reduce grazing-angle popping.
- Additive blending; alpha is zero, RGB is premultiplied by mask.

## Thread Group Size and Dispatch

- G-Buffer and Lighting compute shaders use an 8×8 workgroup in screen tiles:
  - dispatch ( (W+7)/8, (H+7)/8, 1 ).
- This is a common tile size; alter if profiling shows better occupancy with different sizes.

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
- Particles not visible:
  - Verify render order is after blit.
  - Confirm G-Buffer depth binding for particles and epsilon in FS.
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
- Clear pass graph: G-Buffer → Shadows → Deferred Lighting → Blit → Particles → Text.
- ECS-first integration: scene sync and particle upload are cleanly separated.
- Compute-based G-Buffer and lighting simplify platform constraints and allow flexible buffers/bindings.
- Particles after blit with manual depth compare enable cheap, good-looking additive effects.

Key constraints and bottlenecks
- Scene buffer churn:
  - BufferManager may recreate buffers and bind groups when sizes change (edits or many objects), which can be costly on frames with destruction/edits spikes.
  - Particle bind groups may be recreated every frame to handle dynamic buffer sizes.
- Lighting scalability:
  - Lighting is per-pixel over all lights; without tiling/clustered culling, many small lights typical in shooter scenes can be expensive.
  - Shadow maps are computed per light; without caching/LOD, they can dominate cost.
- G-Buffer bandwidth:
  - Depth as RGBA32F and multiple RGBA32F targets drive bandwidth and cache pressure.
- Particles visibility:
  - Manual depth test depends on the “t” buffer; precision/epsilon need care. No soft particles yet.
- Culling:
  - No explicit CPU-side frustum culling per object/sector; compute pass covers entire screen regardless of content.
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
- Add scoped GPU/CPU timers and frame captures per pass (G-Buffer, Shadows, Lighting, Blit, Particles).
- Per-frame counters: number of objects, sectors, bricks, edits, particle count, lights, shadowed lights.
- Crash-proof edits: cap edit throughput per frame; defer large edits to background job queue.

Phase 1 — Data flow, memory, and uploads
- Dirty-region updates:
  - Track per-object dirty sectors/bricks; serialize only changed chunks to GPU buffers.
  - Maintain stable buffer pools and allocate from slabs; avoid wholesale reallocation.
- Bind-group stability:
  - Keep bind group layouts static; manage buffers via sized pools and offsets.
  - Cache/reuse particle bind groups; prefer fixed-capacity instance buffers that grow in steps (1.5x).
- Background build:
  - Move brick/sectors serialization to worker goroutines; stage into staging buffers; copy on next frame.
- Streaming/paging:
  - Optional world paging: per-Region activation based on player proximity; background load and eviction.

Phase 2 — Visibility and culling
- CPU frustum culling per object and per sector:
  - Build visible set (object-level). Within object, cull sectors by camera frustum.
- Screen-space tiling:
  - Optionally skip G-Buffer tiles with no geometry by prepass raster (coarse depth tiles) or hierarchical Z from instances (advanced).
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
- See PARTICLES.md for particle tuning/soft-particles ideas.
- See SHADOW_MAP.md for shadow tiering and bias/PCF notes.
