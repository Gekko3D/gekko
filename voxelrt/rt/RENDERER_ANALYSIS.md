# Renderer Analysis and Shooter-Focused Roadmap

This document evaluates the current VoxelRT renderer and outlines a prioritized roadmap to reach shooter-grade performance and robustness (Teardown-like). It complements RENDERER.md, which describes the existing pipeline.

Related
- rt/RENDERER.md — pipeline and passes
- rt/EDITOR.md — editing pipeline and data flow
- rt/PARTICLES.md — CPU sim and billboard pass
- rt/shaders/* — per-shader docs

## 1. Workload Model for a Teardown-like Shooter

Gameplay assumptions
- Fully destructible voxel environments with frequent edits (explosions, tools).
- Many small dynamic lights (weapons, sparks, fire, destruction).
- Heavy particles/debris and smoke/fire volumetrics.
- Large traversable levels requiring streaming/paging.
- Stable 60–120 FPS at 1080p (scales to 1440p) on mid-range GPUs.

Typical stressors
- Bursty edit throughput (large explosions -> many voxel changes).
- High light counts with shadows (spot + point; moving).
- Large particle counts (sparks, dust, smoke) overlapping geometry.
- Rapid camera movement (aliasing and temporal stability become visible).

## 2. Current Renderer Snapshot (Summary)

See RENDERER.md for details. Briefly:
- Pass graph: G-Buffer (compute) → Shadows (compute) → Deferred Lighting (compute) → Accumulation (render: Transparent Overlay + Particles) → Resolve (render) → Text (render).
- G-Buffer surfaces include depth as ray-distance “t” (RGBA32F.x), world normals, material, position.
- Lighting samples G-Buffer + shadow maps + lights; writes to RGBA8 storage texture (opaque lit color).
- Transparent Overlay raycasts first transparent voxel surface per pixel up to the opaque t-limit and contributes to WBOIT accumulation targets.
- Particles contribute depth-weighted, additive billboards into the same WBOIT accumulation targets; a Resolve pass composites opaque + transparency.
- Scene/Camera/BufferManager handle resource creation and updates.

Strengths
- Clean, modular pass ordering; WGSL compute-heavy (flexible, portable).
- WBOIT transparency path avoids sorting and provides stable order-independent composition for overlays and particles.
- ECS-first data sync; particles integrated with minimal pipeline overhead.
- Editor integrated with safe COW and deferred GPU sync.

## 3. Constraints and Bottlenecks

- Buffer churn on edits
  - BufferManager may recreate large GPU buffers and bind groups when sizes change (objects, sectors, materials). Frequent destruction can spike frame time.
- Lighting scalability
  - Per-pixel over all lights: many small lights without culling becomes costly.
  - Shadow maps cost grows with light count; no caching/tiers/cascades yet.
- Bandwidth pressure
  - Multiple RGBA32F targets (depth/material/position) + RGBA16F normals → high bandwidth.
- Visibility/culling
  - No CPU frustum culling per object/sector; compute runs across full screen regardless of content.
- Transparency approximation
  - WBOIT is an approximation; complex overlapping translucent stacks can deviate from ground truth. Transparent Overlay currently finds the first transparent hit before the opaque t-limit and contributes via WBOIT.
- Particles quality
  - Manual depth compare with “t”; soft particles not yet implemented; popping at grazing angles mitigated but not fully solved for all cases.
- Asynchrony/pipelining
  - Minimal CPU–GPU overlap; serialization and buffer rebuilds impact frame where edits occur.
- Streaming/world scale
  - No paging/regions for very large worlds; everything allocated in active scene.
- Temporal stability
  - No TAA/temporal accumulation; jaggies and shimmer in motion.
- Tooling/observability
  - Limited per-pass timing and counters; difficult to target hot spots precisely.

## 4. Shooter-Focused Roadmap (Prioritized)

Target: 1080p/60–120 FPS on mid-range GPU; sustained performance under destruction and many lights.

Phase 0 — Profiling and stability (Immediate)
- Add timers/counters:
  - Per-pass GPU/CPU time (G-Buffer, Shadows, Lighting, Accumulation, Resolve, Text; optionally split Transparent Overlay vs Particles).
  - Frame stats: #objects, #visible objects, #sectors serialized, #lights (shadowed), #particles, edit deltas.
- Cap edit throughput:
  - Budget voxel changes per frame; queue the rest.
- Crash-proof paths:
  - Defensive checks on buffer growth; guard against null bind groups.

Phase 1 — Data flow, memory, and uploads (High)
- Dirty-region updates:
  - Track per-object dirty Sectors/Bricks; serialize only changed chunks.
- Stable buffers:
  - Use slab/pool allocators; grow in geometric steps (1.5–2.0x) to reduce reallocation.
  - Keep bind groups stable by binding to large buffers with offsets; avoid per-frame rebinding.
- Background build:
  - Serialize Sectors/Bricks on worker goroutines; stage to CPU buffers; copy to GPU next frame.
- Particle buffers:
  - Reuse bind groups; allocate instance buffers with headroom; resize infrequently.

Phase 2 — Visibility and culling (High)
- CPU frustum culling:
  - Object-level culling; optional per-Sector culling inside objects.
- Optional screen tiling:
  - Skip shading tiles with no geometry (requires prepass or instance binning).
- LOD policy:
  - Distance-based LOD for bricks (Tree64/decimation) with stable transitions.

Phase 3 — Lighting and shadow scalability (High)
- Tiled/clustered lighting:
  - Build per-tile/cluster light lists; shade only with relevant lights.
- Shadow cost control:
  - Tiered shadows: per-light resolution and update frequency; cache across frames.
  - PCF with configurable kernel; bias per-type.
  - Lower-res/lower-rate shadows for distant or dim lights.
- G-Buffer packing:
  - Consider R32F for depth, RG16F for normals, packed materials (RGBA8/16F). Validate precision.

Phase 4 — Destruction, debris, and particles (Medium/High)
- Batching edits:
  - Accumulate explosion edits; apply in chunks over multiple frames.
- Debris path:
  - Emit coarse rigid chunks + particles; cap count; fade/LOD (lifetime + distance).
- Particles quality:
  - Soft particles (fade with |t_scene − t_particle|).
  - Velocity-aligned/stretched billboards for sparks; flipbook atlases for smoke/fire.
  - Distance/importance-based spawn LOD.
- Optional GPU particles:
  - Keep CPU emitters for gameplay; consider compute integration for high-density effects.

Phase 5 — Temporal stability and resolution control (Medium)
- TAA:
  - Motion vectors from camera + depth (and optionally normals). Resolve tuned for voxel edges.
- Jitter + resolve:
  - Halton jitter to reduce spec aliasing; sharpen pass.
- DRS:
  - Dynamic resolution scaling to meet frame budget.

Phase 6 — Streaming and world scale (Medium)
- Region paging:
  - Partition level into Regions (Sectors sets). Load/unload based on player proximity.
- Background I/O:
  - Stream Sectors from disk; incremental GPU upload; graceful eviction.

Phase 7 — Tooling and authoring (Medium)
- Editor UX:
  - Brush gizmos, falloff, box/line/fill, undo/redo with sparse deltas.
- Debug views:
  - Tiles/clusters overlays, culled Sectors, shadow tiers, light list density heatmaps.
- Asset preprocessing:
  - Bake LOD levels, Regions, and material metadata to speed runtime.

Phase 8 — Optional GI/path effects (Low, later)
- Screen-space GI/SSAO as stopgap.
- Voxel cone tracing or SDF AO/indirect with temporal denoise.

## 5. Performance Budgets and Quality Presets

KPIs (suggested)
- 1080p “Performance”: 60–120 FPS; ≤10 ms GPU avg on mid-range.
- 1440p “Quality”: 60 FPS; ≤14 ms GPU avg.

Per-preset toggles
- Shadows: resolution (512/1024/2048), PCF kernel (0/2/4 taps), update rate (every N frames).
- Lighting: clustered on/off; max lights per tile/cluster.
- G-Buffer: formats (Depth R32F vs RGBA32F.x; normals RG16F vs RGBA16F).
- Transparency: WBOIT k exponent, alpha scaling for overlays/particles.
- Particles: cap count, soft particles on/off, flipbook on/off.
- TAA/DRS: on/off and scale limits.
- Streaming: region radius.

## 6. Risks and Mitigations

- Frequent buffer rebuilds → stutter:
  - Pools, dirty-chunk uploads, background serialization, and geometric growth.
- Many shadowed lights → heavy GPU cost:
  - Clustered lighting + shadow tiers + caching + variable update frequency.
- Transparency approximation limits:
  - WBOIT handles many cases well but is not exact for deep stacks; expose k and alpha scaling; consider special-casing thick volumes if needed.
- Particles overdraw and popping:
  - Soft particles and LOD; tune epsilon; atlas animations for variety at lower counts.
- Large worlds:
  - Streaming Regions; precomputed LOD and metadata; debug visualizations to tune thresholds.

## 7. Short-Term Action List (Next Steps)

- Instrumentation: timers and counters; basic in-game overlay.
  - Measure: G-Buffer, Shadows, Lighting, Accumulation (Overlay + Particles), Resolve, Text.
- Dirty uploads: implement per-object dirty tracking and partial GPU updates.
- Particle stability: soft-particles (simple depth-delta fade) and flipbook support.
- Shadow tiers: add per-light tiering and update cadence control.
- Culling: CPU frustum culling for objects; optional per-Sector mask.
- Buffers: pooled allocations and geometric growth policy for instance/material buffers.

With these steps, the renderer will scale better under shooter workloads while maintaining clarity and modularity of the current pipeline.
