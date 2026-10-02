# VoxelRT Runtime

This document describes the current renderer architecture and frame graph. It is the renderer source of truth for live behavior.

Related docs:

- [`overview.md`](overview.md)
- [`change-guide.md`](change-guide.md)
- [`editing.md`](editing.md)
- [`gbuffer-compaction-note.md`](gbuffer-compaction-note.md)
- [`media.md`](media.md)
- [`particles.md`](particles.md)
- [`verification.md`](verification.md)
- [`webgpu-bindgroup-lifetime-notes.md`](webgpu-bindgroup-lifetime-notes.md)
- [`voxelrt-render-graph-migration-plan.md`](voxelrt-render-graph-migration-plan.md)

## Ownership Boundaries

- ECS bridge: `mod_voxelrt_client*.go`
  - creates and synchronizes renderer-side objects, lights, camera state, text, gizmos, particles, sprites, analytic media, and skybox inputs
- `app.App`: `voxelrt/rt/app/`
  - owns WebGPU device and surface lifetime, render pipelines, resize flow, opaque storage output, and pass scheduling
- `gpu.GpuBufferManager`: `voxelrt/rt/gpu/`
  - owns G-buffer textures, WBOIT targets, half-resolution analytic-media targets, shadow maps, Hi-Z, scene buffers, voxel atlas resources, and most cached bind groups
- `core.Scene` and friends: `voxelrt/rt/core/`
  - own CPU scene state, camera and light math, culling, raycast, gizmos, and text primitives
- `volume.XBrickMap`: `voxelrt/rt/volume/`
  - owns sparse voxel storage, edit semantics, dirty tracking, traversal, and compression

## Engine Stage Flow

The renderer participates in three engine stages:

1. `Prelude`
   - input sync
   - text clear
   - `BufferManager.BeginBatch()`
2. `PreRender`
   - ECS-to-renderer sync in `voxelRtSystem`
   - `RtApp.Update()`
3. `Render`
   - `RtApp.Render()`

That split matters because bridge sync and GPU uploads happen before render-pass execution.

## `App.Update()`

`Update()` is the per-frame CPU preparation step. It currently:

1. builds view and projection matrices
2. reads the previous Hi-Z snapshot
3. runs `Scene.Commit(...)` with frustum culling and optional Hi-Z occlusion
4. updates profiler counters
5. calls `BufferManager.UpdateScene(...)`
6. rebuilds dependent bind groups if GPU resources were recreated
7. updates camera uniforms
8. updates analytic-media temporal history inputs and current half-resolution volumetric target selection
9. refreshes text and gizmo buffers

Important details:

- Hi-Z uses previous-frame data and is disabled during fast camera motion.
- `Scene.Commit(...)` produces both `VisibleObjects` and `ShadowObjects`.
- `UpdateScene(...)` can grow shadow maps or scene buffers, which forces downstream bind-group recreation.
- `CameraState.DepthMode` changes the projection and inverse-projection contract used by CPU culling helpers and WGSL ray reconstruction, but it does not change the G-buffer depth payload format.
- analytic media history uses previous-frame camera state and previous half-resolution volumetric buffers, so `Update()` has to prepare those inputs before feature execution

## `App.Render()`

The current live frame sequence is scheduled by the default render graph. `App.Render()` owns the outer frame shell: swapchain acquire, command encoder creation, frame-level profiler counters, graph recording, submit/present, readback handoff, and frame bookkeeping. Feature-stage compatibility slots plus core G-buffer, Hi-Z, shadows, tiled-light-cull, lighting, debug-scene, accumulation, and resolve work are recorded through graph nodes.

| Order | Work | Owner | Optionality | Notes |
| --- | --- | --- | --- | --- |
| 1 | swapchain acquire and command encoder creation | `App.Render()` | core | Creates the swapchain view and command encoder for the frame. |
| 2 | particle simulation/spawn | render graph feature node / particles feature | optional | Runs before generic pre-G-buffer compatibility work and before G-buffer. |
| 3 | `FeatureCommandStagePreGBuffer` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned particle simulation is skipped by this dispatcher. |
| 4 | G-buffer compute | render graph core node / `GpuBufferManager` | core | Writes G-buffer depth, normal, and material targets from visible voxel scene buffers. The graph node records pipeline, bind-group, and workgroup readiness counters for diagnostics. |
| 5 | Hi-Z generation | render graph core node / `GpuBufferManager` | core | Builds the previous-frame occlusion source used by the next `Update()`. The graph node records pipeline, depth-view, mip-view, bind-group, camera-buffer, and readback readiness counters for diagnostics. |
| 6 | `FeatureCommandStagePostGBuffer` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 7 | shadows | render graph core node / `GpuBufferManager` | core | Builds scheduled directional, spot, and point-light shadow updates. The graph node owns shadow update summary/profiler counters and delegates update scheduling/dispatch to `GpuBufferManager`. |
| 8 | `FeatureCommandStagePreLighting` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 9 | skybox update | render graph feature node / skybox feature | optional | Consumes pending `SkyboxResources` input before light-list and lighting work; the registered pre-update bridge only collects ECS input. |
| 10 | tiled light cull | render graph core node / `GpuBufferManager` | core conditional | Dispatches only when local point or spot lights exist; otherwise clears light-list state. The graph node records readiness counters for diagnostics. |
| 11 | deferred decal overlay | render graph feature node / decals feature | opt-in | Clears and, when input is present, rasterizes instanced projector volumes into a full-resolution premultiplied `RGBA8Unorm` overlay. It is after tiled light culling and before deferred lighting. |
| 12 | deferred lighting | render graph core node / `GpuBufferManager` | core | Composites the decal overlay into base color before BRDF evaluation, then writes the HDR opaque lighting target. The graph node records pipeline, bind-group, and workgroup readiness counters for diagnostics. |
| 13 | `FeatureCommandStagePostLighting` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned astronomical bodies, planet bodies, and analytic media are skipped by this dispatcher. |
| 14 | astronomical render pass | render graph feature node / astronomical feature | optional | Renders far-field celestial bodies after lighting. |
| 15 | planet bodies render pass | render graph feature node / planet body feature | optional | Renders far-body planet surfaces after astronomical bodies. |
| 16 | analytic media render pass | render graph feature node / analytic media feature | optional | Renders or clears the half-resolution analytic-media targets after planet bodies. |
| 17 | debug scene compute | render graph core node | optional | Runs only when renderer debug mode and scene debug overlay are active. |
| 18 | accumulation render pass | render graph core node + feature registry | optional pass shell | Opens when a legacy or graph-owned accumulation contributor exists, or when the previous frame had one, so stale WBOIT contents can be cleared. |
| 19 | `FeaturePassStageAccumulation` | graph-owned in-pass contributors | optional | Current built-in contributors: transparent overlay, sprites, water, far planet rings, debris midfield, and particles. |
| 20 | `FeatureCommandStagePreResolve` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 21 | resolve render pass | render graph core node | core | Composites opaque lighting, WBOIT, and analytic media to the swapchain. |
| 22 | text overlay | render graph feature node / text feature | optional | First feature-owned graph node migrated out of the post-resolve compatibility stage. |
| 23 | gizmos overlay | render graph feature node / gizmo feature | optional | Feature-owned graph node migrated out of the post-resolve compatibility stage. |
| 24 | `FeatureScreenStagePostResolve` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned features are skipped by this dispatcher. |
| 25 | submit, present, readback handoff, frame bookkeeping | `App.Render()` | core | Submits the command buffer, presents, resolves Hi-Z readback, commits volumetric history, records camera state, and advances the frame index. |

The feature-stage sequence is now the compatibility layer between the old feature registry and the render-graph migration. It is intentionally less expressive than final feature-owned graph nodes: any new feature that does not fit an existing stage still has to add another stage or register an explicit graph node. Features that implement graph-owned nodes are skipped by the compatibility command/pass/screen dispatchers so they do not render twice while migration is incremental. Graph-owned features that still draw inside renderer-owned passes use the render-graph pass-stage dispatch path; this keeps shared pass shells such as WBOIT accumulation intact while individual contributors migrate.

The render graph now participates in renderer lifecycle dispatch as well as pass recording. `App` forwards setup, resize, scene-buffer recreation, per-frame update, and shutdown into the graph; current core and compatibility nodes keep those hooks no-op, but explicit optional feature nodes can use them without adding new central `App.Render()` branches.

### Custom Render Extensions

Games can add renderer extensions through `VoxelRtModule` without patching the core renderer:

- `RenderFeatures` registers `VoxelRtRenderFeature` values on the internal voxel RT app before renderer initialization.
- `RenderGraphNodes` appends `VoxelRtRenderNodeSpec` values to the default graph before graph lifecycle setup.
- `BridgeFeatures` declares optional ECS sync gates. A custom bridge should require the custom app feature name and any custom graph node names it depends on.

Custom graph node names should be stable and unique. Prefer names with a feature prefix, such as `feature-my-effect`, and declare explicit `After` dependencies against existing graph nodes like `core-resolve`, `core-accumulation`, or a built-in feature node. Missing dependencies and duplicate node names fail graph compilation.

Custom nodes receive the same lifecycle calls as built-in graph nodes: setup, resize, scene-buffer recreation, per-frame update, record, and shutdown. `RenderFeatures` still own feature state and compatibility stage declarations; `RenderGraphNodes` own graph scheduling. If a custom ECS bridge is optional, declare it in `BridgeFeatures` so games that do not register the feature pay no bridge sync cost.

Older notes may describe a probe-GI bake pass. The live `App.Render()` path inspected for this inventory does not currently record a distinct probe-GI dispatch; if probe GI is restored as a live pass, it should become either a core graph node or an optional feature node with explicit dependencies.

Equivalent high-level sequence:

1. particle simulation compute passes
2. G-buffer compute pass
3. Hi-Z generation compute pass
4. shadow pass
5. skybox update marker through explicit `feature-skybox-update`
6. deferred decal overlay through explicit `feature-decals` when `DecalFeature` is registered
7. deferred lighting compute pass
8. astronomical and planet-body post-lighting passes
9. analytic media half-resolution render pass
   - renders bounded atmosphere/fog media into dedicated half-resolution color and front-depth history/render targets
   - reprojects previous analytic-media history in shader
10. optional debug compute pass
11. accumulation render pass
   - transparent voxel overlay through graph-owned accumulation contribution
   - particles through graph-owned accumulation contribution
   - sprites through graph-owned accumulation contribution
   - water through graph-owned accumulation contribution
   - far planet rings and debris midfield through graph-owned accumulation contribution
12. resolve render pass
   - composites opaque lighting, WBOIT transparency, and half-resolution analytic media
13. post-resolve overlay passes
   - text overlay through explicit `feature-text-overlay`
   - gizmos through explicit `feature-gizmos-overlay`

The legacy fullscreen blit pipeline still exists in setup code, but the resolve path is the live compositor.

## Feature Inventory

Built-in features are registered from `voxelrt/rt/app/feature_registry.go`. This table records their current render-stage ownership and bridge ownership before graph migration.

| Feature | App feature file | Render stage today | Bridge/source sync today | Core or optional |
| --- | --- | --- | --- | --- |
| text | `feature_text.go` | explicit `feature-text-overlay` graph node after resolve; owns shared text overlay resources for immediate `DrawText` UI/debug output plus ECS `TextOverlayItem` handoff | registered bridge system / `TextComponent` query through `buildTextBridgeItems` adapter appended after immediate UI text | optional overlay |
| gizmos | `feature_gizmos.go` | explicit `feature-gizmos-overlay` graph node after text overlay; owns renderer-side `GizmoOverlayItem` handoff | registered bridge system / `syncVoxelRtGizmos` through `buildGizmoBridgeItems` adapter | optional overlay |
| skybox | `feature_skybox.go` | explicit `feature-skybox-update` graph node before tiled light culling; owns renderer-side `SkyboxResources` / `SkyboxLayerInput` handoff while GPU texture/pipeline state remains in `GpuBufferManager` | registered pre-update bridge system / `syncSkybox` | optional lighting/background input |
| decals | `feature_decals.go` | explicit `feature-decals` graph node after tiled light culling and before deferred lighting; projects retained `DecalInstanceInput` volumes into the decal overlay | registered after-batch bridge system / `SetRuntimeDecals` → atlas-grouped `ApplyDecalInput` | opt-in surface overlay |
| astronomical | `feature_astronomical.go` | explicit `feature-astronomical` graph node after post-lighting compatibility work; owns typed `AstronomicalBodyInput` application before GPU manager record packing | registered batched bridge system / `buildAstronomicalBodyInputs` adapter | optional SpaceSim feature |
| planet bodies | `feature_planet_body.go` | explicit `feature-planet-bodies` graph node after post-lighting compatibility work; owns typed `PlanetBodyInput` / `PlanetBodySurfaceInput` application before GPU manager record packing | registered batched bridge system / `buildPlanetBodyInputs` / `buildPlanetBodySurfacePreloadInputs` adapters | optional SpaceSim feature |
| far planet rings | `feature_far_planet_ring.go` | graph-owned contribution inside `core-accumulation`; owns typed `FarPlanetRingInput` application before GPU manager record packing | registered batched bridge system / `buildFarPlanetRingInputs` adapter | optional SpaceSim feature |
| debris midfield | `feature_debris_midfield.go` | graph-owned contribution inside `core-accumulation`; owns typed `DebrisMidfieldInput` application before GPU manager record packing | registered batched bridge system / `buildDebrisMidfieldInputs` adapter | optional SpaceSim feature |
| analytic media | `feature_analytic_medium.go` | explicit `feature-analytic-media` graph node after post-lighting compatibility work; owns typed `AnalyticMediumInput` application before GPU manager record packing | registered-feature-gated `AnalyticMediumComponent` query through `buildAnalyticMediumInputs` adapter | optional volumetric |
| water | `feature_water.go` | graph-owned contribution inside `core-accumulation`; owns typed `WaterSurfaceInput` / `WaterRippleInput` application before GPU manager record packing | registered-feature-gated `buildWaterSurfaceInputs` adapter | optional surface feature |
| transparency | `feature_transparency.go` | graph-owned contribution inside `core-accumulation` | transparent visible objects derived from core scene/material sync | optional composition feature |
| particles | `feature_particles.go` | explicit `feature-particles-sim` graph node for simulation/spawn; graph-owned contribution inside `core-accumulation` for draw; owns typed `ParticleEmitterInput` / `ParticleFrameInput` application, GPU byte packing, params upload, spawn upload, and bind-group refresh | registered-feature-gated `particlesSync` in `particles_ecs.go` | optional simulation/draw feature |
| sprites | `feature_sprites.go` | graph-owned contribution inside `core-accumulation`; owns typed `SpriteInstanceInput` / `SpriteBatchInput` application and GPU byte packing | registered-feature-gated `spritesSync` in `sprite_ecs.go`; entity-LOD sprite proxies are only produced when the sprite bridge is enabled | optional draw feature |

The current feature config can prevent disabled features from registering app-side feature objects and allocating their pipelines during `Setup()`. Decals deliberately are not a default feature: a consumer registers `DecalFeature` through `VoxelRtModule.RenderFeatures`, which enables the `decals` bridge gate and `feature-decals` node. `VoxelRtModule.BridgeFeatures` declares the feature and graph-node requirements for optional ECS bridge sync, and defaults cover the built-in bridges. Text, gizmo, analytic-media, water, planet-body, astronomical, far-ring, debris, particle, sprite, skybox, and decal bridge bodies are installed through this registration surface; core voxel-object sync still consults the sprite bridge gate only to decide whether entity-LOD impostor proxies may be emitted as runtime sprites. Features that share `core-accumulation` as their graph node still need feature-name bridge gates because a node-name-only gate would confuse water, sprites, transparency, particles, rings, and debris.

## Bridge Sync Inventory

The remaining broad `voxelRtSystem` bridge is now core-only: it syncs voxel scene objects/materials, camera state, and scene lights. Optional feature bridges are installed through `VoxelRtModule.BridgeFeatures` around the `GPU Batch` and `RT Update` boundaries; sprite feature ownership is still consulted inside core instance sync only to decide whether entity-LOD impostor/dot proxies may become runtime sprites.

Core instance membership is owned by the private inventory in
`mod_voxelrt_client_inventory.go`. It caches every live entity with both
`TransformComponent` and `VoxelModelComponent`, grouped by archetype with
entity-ID/row locations, and rebuilds when the ECS storage owner or committed
structural revision changes. Hidden, unresolved, streamed and sprite-LOD
candidates remain members even when they have no renderer object. Equal stamps
from different storage owners cannot reuse membership or component IDs.

The inventory retains no component pointers or typed column aliases. Each pass
reacquires both exact typed columns once per archetype batch and runs the full
existing bridge body on live values: geometry normalization/resolution
(including same-ID source-map replacement), transforms and metadata,
elapsed-time materials, LOD/sprite selection, hidden residency and streamed
ticket adoption. Streamed begin/end sync and camera/light extraction still run
every pass, including empty inventories. Animation stays in `Update` and
hierarchy in `PostUpdate`; their results reach extraction at the existing
`PreRender` boundary after normal command flushes.

Core sync publishes changed committed geometry-reference normalization and
derived Transform Pivot at their existing assignments. Physics preparation also
publishes its committed normalization; neither changes geometry precedence,
collision, renderer object comparisons or command timing. See the
[voxel bridge publication contract](../engine/ecs.md#voxel-bridge-publication).
Live inputs still run through the bridge every pass.

Core flying/grounded camera controllers publish changed camera pose/look inputs;
the camera-dependent EntityLOD system publishes changed selection outputs. Their
math, policy and stage placement are unchanged. These are publication owners,
not extraction invalidation guarantees; see
[the ECS contract](../engine/ecs.md#camera-and-entitylod-publication).

Hierarchy now reuses storage-owned topology and skips child composition when
exact live parent/local/output TRS bits match. Every invocation still reads
current values, detects direct Parent edits before composition and repairs
direct child-world TRS edits. Direct attachment calls resolve immediately;
ancestor motion reaches descendants before the same frame's extraction. These
hierarchy counters do not authorize skipping any bridge processing. See
[S3d](../roadmaps/streamed-rendering-s3d.md) and
[the hierarchy owner contract](../engine/ecs.md#hierarchy-ownership).

`VoxelRtState.VoxelCandidateInventoryBuildCount` counts actual membership
rebuilds, including first or changed empty inventories;
`VoxelRtState.VoxelCandidateCount` reports raw Transform+VoxelModel membership.
These counters describe discovery work, not extraction cost or measured
performance. Rebuilds clear obsolete IDs and archetype references through
capacity tails. Empty inventories release aggregate slices; nonempty capacity
may retain its peak, with no byte ceiling. The helper is private and main-thread
owned, following query rules: field writes and buffered commands are supported;
immediate structural mutation or manual flush during iteration is unsupported.

| Sync scope | Current owner | Renderer data updated | Future graph migration direction |
| --- | --- | --- | --- |
| `Sync Instances` | `voxelRtSystem` | core scene voxel objects, material tables, sprite-gated LOD impostor proxy selection | centralized core bridge |
| `Sync Media` | registered batched bridge system / `buildAnalyticMediumInputs` | typed `AnalyticMediumInput` handoff | renderer app owns analytic-media input application before GPU manager packing; moved before `GPU Batch` |
| `Sync Planet Bodies` | registered batched bridge system / `buildPlanetBodyInputs` / `buildPlanetBodySurfacePreloadInputs` | typed `PlanetBodyInput` / `PlanetBodySurfaceInput` handoff | renderer app owns planet-body input application before GPU manager packing; moved before `GPU Batch` through planet-body feature bridge registration |
| `Sync Astronomical` | registered batched bridge system / `buildAstronomicalBodyInputs` | typed `AstronomicalBodyInput` handoff | renderer app owns astronomical input application before GPU manager packing; moved before `GPU Batch` through astronomical feature bridge registration |
| `Sync Far Planet Rings` | registered batched bridge system / `buildFarPlanetRingInputs` | typed `FarPlanetRingInput` handoff | renderer app owns far-ring input application before GPU manager packing; moved before `GPU Batch` through far-ring feature bridge registration |
| `Sync Midfield Debris` | registered batched bridge system / `buildDebrisMidfieldInputs` | typed `DebrisMidfieldInput` handoff | renderer app owns debris-midfield input application before GPU manager packing; moved before `GPU Batch` through debris feature bridge registration |
| `Sync Water` | registered batched bridge system / `buildWaterSurfaceInputs` | typed `WaterSurfaceInput` / `WaterRippleInput` handoff | renderer app owns water input application before GPU manager packing; moved before `GPU Batch` through water feature bridge registration |
| `Sync Lights` and camera pull | `voxelRtSystem` / `syncVoxelRtLights` | camera state, scene lights, ambient light | centralized core bridge |
| text query | registered bridge system plus `buildTextBridgeItems` adapter | frame-lifetime `TextOverlayItem` input appended to immediate `DrawText` UI/debug text | first bridge body moved out of broad `voxelRtSystem`; ECS-to-renderer conversion is isolated in a tested helper, and text is cleared once by `voxelRtPreludeSystem` before retained UI rendering |
| `Sync Gizmos` | registered bridge system / `syncVoxelRtGizmos` plus `buildGizmoBridgeItems` adapter | frame-lifetime `GizmoOverlayItem` input | bridge body moved out of broad `voxelRtSystem`; ECS-to-renderer conversion is isolated in a tested helper |
| `GPU Batch` | `voxelRtBatchEndSystem` | flushes batched GPU data uploads | explicit boundary between batched and after-batch bridge systems |
| `Sync Particles` | registered after-batch bridge system / `particlesSync` | particle atlas lookup plus typed `ParticleEmitterInput` / `ParticleFrameInput` handoff | renderer app owns particle byte packing, params upload, emitter/spawn GPU updates, and bind-group refresh; keep after `GPU Batch` unless particle uploads are made batch-safe |
| `Sync Sprites` | registered after-batch bridge system / `spritesSync` | sprite atlas lookup plus typed `SpriteInstanceInput` / `SpriteBatchInput` handoff | renderer app owns sprite byte packing and GPU batch-desc conversion; keep after `GPU Batch` unless sprite uploads are made batch-safe |
| `Sync Decals` | registered after-batch bridge system / `voxelRtDecalsBridgeSystem` | retained `DecalInstance` values converted to 80-byte GPU records and grouped by existing sprite-atlas key | skipped and cleared unless the consumer registered `DecalFeature`; one instance buffer and one draw per non-empty atlas batch |
| `Sync Skybox` | registered pre-update bridge system / `syncSkybox` plus `buildSkyboxBridgeInput` adapter | `SkyboxResources` / `SkyboxLayerInput` input | GPU application and GPU-layer packing are now owned by `feature-skybox-update`; the remaining ECS-to-renderer conversion is isolated in a tested bridge helper |

## Render Targets and Formats

### Opaque lighting output

- `App.StorageTexture`: `RGBA16Float`
- written by deferred lighting
- sampled by the resolve pass

### G-buffer

- depth: `RGBA32Float`
- normal: `RGBA16Float`
- material: `RGBA32Float`
- no dedicated position target
- `GBufferDepth.r` stores hit distance along the camera ray
- `GBufferDepth.gba` stores voxel-center world position for voxel-stable shadowing
- `GBufferMaterial.xy` stores the receiver shadow group as exact split 16-bit lanes, with the low lane also carrying the two-sided-lighting flag
- `GBufferMaterial.z` stores shadow seam epsilon
- `GBufferMaterial.w` stores the final material-table index
- deferred lighting can reconstruct the exact visible hit position from screen UV, camera inverse matrices, and `GBufferDepth.r`
- reverse-z affects only the projection matrices that generate those camera rays; `GBufferDepth.r` remains linear hit distance along the camera ray
- live opaque shading evaluates view- and direct-light terms from the stored voxel-center world position so each visible voxel shades as one cell
- deferred shadowing also uses the stored voxel-center world position so each visible voxel can receive one stable shadow response per light

#### Voxel shading contract

The voxel renderer intentionally keeps a blocky albedo/material look while allowing voxelized shapes to read with volume under lighting.

- A visible voxel keeps one material identity.
  - Albedo, emissive, and PBR parameters still come from the voxel palette/material lookup. Do not introduce cross-voxel albedo blending to "smooth" the image.
- A visible voxel uses one lighting normal.
  - Hits inside the same voxel should resolve to the same normal so the voxel shades as one cell, more like a pixel block than a triangle surface with interpolated normals.
- The normal should come from local voxel occupancy first.
  - The intended look is faceless microvoxels with shape volume. The live rule is: estimate a normal from neighboring occupied/empty voxels, then fall back only if that gradient is degenerate.
- Do not use object-center or radial fallback normals for shading.
  - Those produce a blobby "inflated" read that is unrelated to the local voxel surface and drift badly on concave or thin shapes.
- Degenerate gradients should still resolve to one per-voxel normal.
  - For a thin sheet with one exposed axis, fit a wider local occupancy neighborhood before using the deterministic exposed-face fallback; this preserves shallow voxelized ramps.
  - If that fit is degenerate or rejected, derive a deterministic fallback from the voxel's exposed-face mask so thin symmetric features do not become view-dependent.
  - Use the hit face / ray entry direction only as a last resort when the occupancy-based fallback is still ambiguous.
- Single-voxel-thick features need two-sided direct lighting.
  - When a voxel is exposed on both sides of an axis, keep its normal deterministic, but evaluate direct point and spot lighting as two-sided so planes and rods still react to local lights from either side.
- Normal transforms must be consistent across traversal paths.
  - `XBrickMap` microvoxels, solid-brick fast paths, and `tree64` LOD hits must all use the same object-space-to-world-space normal rule. Use the inverse-transpose-style transform, especially when non-uniform scale is possible.
- Lighting may vary voxel-to-voxel, but color identity should remain voxel-stable.
  - The renderer can show shape through per-voxel lighting, AO, and shadows, but it should not smear voxel colors into gradients across neighboring voxels.

Current implementation notes:

- The live normal decode path is in `voxelrt/rt/shaders/gbuffer.wgsl`; CPU-side bake/upload lives in `voxelrt/rt/gpu/manager_voxel_normals.go`.
- Neighbor-derived normals are baked during voxel upload into the voxel auxiliary sidecar. Objects with voxel adjacency metadata sample adjacent chunks across boundaries; terrain metadata remains a compatibility fallback.
- The sidecar keeps dense occupancy words followed by one 16-bit oct-encoded normal per voxel. G-buffer, transparent overlay, and particle collision paths load the baked normal at the hit voxel instead of sampling six neighbors at hit time.
- Cross-chunk normal seams depend on upload-time dirty propagation: structural dirty state must be prepared before cross-object normal halo propagation so newly loaded chunks rebake already-uploaded neighbor boundary bricks.
- The degenerate fallback path is occupancy-based and deterministic per voxel; face-entry is only a last resort.
- Degenerate thin voxels carry a two-sided direct-light flag through the G-buffer so deferred and transparent lighting agree on planes and rods.
- Deferred lighting consumes the stored G-buffer normal directly; the albedo/material lookup stays palette-driven.
- Voxel palette assets may define material animations. During ECS-to-renderer
  instance sync, the bridge builds an effective palette for the current engine
  time and lets the existing material-table fingerprint/cache decide whether a
  material table must be rebuilt. This is CPU-side palette sequencing; shaders
  still consume the normal material table.
- Material animation frames are generic engine data, not HL1-specific data.
  Today the runtime applies palette color, emissive color, emission strength,
  roughness, and transparency changes by producing an effective
  `VoxelPaletteAsset` before material-table upload.
- Current animation kinds are data labels, not renderer branches. Imported HL1
  content emits `palette_sequence` for `+0/+1/...` texture frames and
  `palette_scroll` for conveyor-style phase buckets; both resolve to the same
  effective palette update path before material-table upload.
- `palette_scroll` animations may carry `uv_scroll.velocity` metadata. That is
  preserved intent for the future textured voxel material path. It is not true
  shader-side UV scrolling yet, because current VoxelRT material hits do not
  carry per-surface UVs or texture/atlas references into the shader.
- Opaque deferred point and spot lights evaluate attenuation from the stored voxel center, matching the transparent overlay path.
- Point-light shadows use six cube faces stored in the shadow-map array and are sampled with hard voxel-stable compares.
  - Keep them discrete per receiving voxel. Do not add per-voxel gradient filtering that turns a microvoxel into a soft-lit surface patch.
- Voxel shadow sampling is intentionally hard.
  - `LightingQualityConfig.Shadow` still controls cascade distances and local-light tier bands.
  - Deprecated shadow-softness fields are ignored so a receiving voxel keeps one discrete shadow response.

### Transparency / WBOIT

- accumulation: `RGBA16Float`
- weight: `R16Float`

Current transparency modes:

- volumetric transparent media
  - uses transmission plus density and marches through voxel thickness
  - most expensive path
- thin surface glass
  - uses transmission with zero density and resolves from the first surface hit
  - keeps refraction but avoids marching through the full interior volume
- gameplay see-through
  - uses transparency with transmission, density, and refraction forced to zero
  - intended for readability helpers such as seeing a character or pickup through nearby cover
  - use `GameplaySeeThroughMaterial(...)` or `ApplyGameplaySeeThroughMaterial(...)` instead of palette alpha when you do not want glass-like optics
- dedicated water surfaces
  - use the water feature and `WaterSurfaceComponent`, not transparent voxel palettes or analytic media
  - keep a blocky stepped surface read with restrained refraction/tint
  - accumulate through WBOIT alongside other transparent surface features

### Half-resolution volumetrics

- analytic media history/render targets: `RGBA16Float`
- analytic media front-depth targets: `R16Float`
- resolve upsamples analytic media with depth-aware filtering against full-resolution scene depth

### Other major resources

- deferred decals (only when `DecalFeature` is registered):
  - `DecalResources.Pipeline` in `app.App`
  - one full-resolution `RGBA8Unorm` premultiplied overlay, a transparent 1×1 fallback, the shared 80-byte instance buffer, and atlas batch bind groups in `GpuBufferManager`
  - the lighting bind group always samples the active overlay or fallback; non-participating consumers allocate neither the full-resolution target nor the decal pipeline
- shadow maps: 2D array textures managed by `GpuBufferManager`
- Hi-Z: `R32Float` mip chain built from the G-buffer depth texture at half resolution
- voxel payload atlas:
  - 4 fixed 3D `R8Uint` texture pages
  - each page is capped by `MaxTextureDimension3D` and aligned down to `volume.BrickSize`
  - brick payload records now store packed `atlas_offset` plus `atlas_page`, so voxel consumers must bind all four payload pages together

## Scene and Culling Model

- `Scene.Objects` is the authoritative CPU-side object list.
- `Scene.Commit(...)` updates `WorldAABB` values, runs frustum culling, then optionally applies Hi-Z occlusion.
- `VisibleObjects` drives main scene buffers and the camera-facing BVH.
- `ShadowObjects` drives a broader shadow BVH so off-screen casters can still affect visible receivers.

### Incremental scene records

`GpuBufferManager` retains one compiled instance and parameter template per
object in the union of visible, transparent and shadow pass lists. Each pass
keeps its ordered arrays and render-relative BVH. Exact value snapshots cover
the actual transform matrices, local/world bounds, render origin, encoded
object metadata, allocation presence, material offsets and direct lookup data;
float comparisons use bits, including signed zero and NaN payloads. This remains
compatible with public in-place mutations and transforms whose dirty flags
were consumed by scene commit.

Idle inputs reuse their prepared records. Changes encode only affected unique
object templates, then rebuild affected aggregate arrays; assembly patches each
pass's instance index. A pass BVH rebuilds only when its ordered object identities,
world bounds or render origin change. Metadata-only parameter changes preserve
instance and BVH work. Existing 208-byte instance rows, 128-byte parameter rows,
BVH layout and empty zero sentinels remain the shader contract.

Preparation and publication run after voxel admission and all sector, terrain
and planet lookup maintenance, so material offsets and direct lookup metadata
reflect the current service frame. A CPU preparation does not acknowledge an
upload. Each record buffer remembers its successfully uploaded revision and
destination; unchanged bytes skip queue writes only at that same destination.
Nil, replaced or undersized buffers receive cached bytes, and a destination
replacement reports resource recreation even if its capacity is sufficient.
Queue write errors fail before advancing that record's publication latch.

`SceneInstanceRecordBuildCount` and `SceneObjectParamRecordBuildCount` count
encoded unique object templates; `SceneBVHBuildCount` counts actual nonempty
relative BVH builds. `SceneRecordUploadCount` counts successful queue writes to
the nine record buffers. All four are cumulative manager-lifetime counters.
`SceneRecordObjectCount` reports current pass-union ownership, including
shadow-only casters and excluding hidden resident objects. Removed references
and obsolete slice tails are cleared; empty passes release their aggregate
capacity and empty ownership maps are dropped. Nonempty Go maps and slices can
retain peak capacity; there is no scene-record byte ceiling.

Main-thread `InvalidateSceneRecords()` is nil-safe and discards preparation and
publication ownership after an explicit reset or external record overwrite,
while preserving cumulative counters. A new manager starts empty. Internal
prepared views are borrowed until the next preparation/reset. Lights, voxel
service, lookups, camera/feature updates, scene culling and bridge extraction
continue at their existing frame boundaries. ECS dirty extraction and future
layer selection remain separate S3 work. See [S3b](../roadmaps/streamed-rendering-s3b.md).

### Streamed voxel residency

Ordinary `VoxelRenderHiddenComponent` entities leave renderer residency. Adding
`StreamedVoxelRenderComponent` keeps the requested voxel geometry in
`Scene.Objects` while hidden, allowing GPU uploads to continue. The bridge sets
`RenderEnabled = false`; scene commit excludes the object from visible,
transparent, shadow and render BVH lists and visibility statistics. CPU ray
queries still use resident authoritative geometry. Automatic entity proxy and
impostor LOD are bypassed for marked entities.

The streaming owner supplies a unique nonzero ticket and its generation.
`VoxelRtState.StreamedVoxelStatus(ticket)` becomes known when the bridge observes
the marker and captures the actual runtime map/revision. Unfinished target or
ownership changes cancel the ticket; missing adoption data fails diagnostically.
After `RtApp.Update()`, the bridge checks allocation coverage, dirty work,
material state and sector lookup topology in constant time per uploading ticket.
`Ready` means required writes have been queued before a later visibility change,
without waiting for a GPU completion fence.

Terminal states remain latched. Later edits use the ordinary dirty-upload path.
The owner removes or changes the terminal marker before calling
`ForgetStreamedVoxel`; that method deletes only terminal records. Terminal
records retain status metadata without retaining captured geometry handles.
Priority and ticket order are copied to scheduling metadata. The existing v2
streamed runtime now stages its terrain, imported full chunks and sector proxies
through these tickets. It reveals an imported sector only when every required
full target is ready, and waits for a ready proxy before distance unloading.
Visibility publishes once after each observer/commit stage, before the later
renderer bridge. CPU collision/destruction residency and navigation remain
independent of GPU readiness. V3 page selection and cross-layer coverage groups
remain separate work. See
[S1a scope and verification](../roadmaps/streamed-rendering-s1a.md) and
[S1c integration](../roadmaps/streamed-rendering-s1c.md), plus the
[island residency contract](../content/island-streaming.md#renderer-residency-contract).

### Global voxel upload scheduling

`GpuBufferManager.UpdateVoxelData` schedules voxel content across all resident
objects, including hidden streamed objects. `SetVoxelUploadBudget` configures
bytes, sectors and brick records per service frame. Defaults are 4 MiB, 1,024
sectors and 65,536 brick records. Existing `SectorsPerFrame` callers configure
the same global sector cap. Zero pauses the corresponding resource; zero bytes
pauses every content write.

The byte cap covers material rows, sector/brick records, auxiliary
occupancy/normals and mixed payloads. A full sector consumes 64 brick records,
including empty clears. Allocation/migration copies, lookup rebuilding, scene
buffers and CPU queue/normal-halo preparation remain outside this cap.

Shared maps upload geometry once, using their best instance priority/order.
Materials remain per object. Work sorts by priority, order, map ID, kind and
signed coordinates. Waiting work gains one priority level every eight service
frames; older work wins equal effective priority. Removed work loses its age.
New core objects default to visible priority.

An atomic sector that cannot fit the configured frame limits stays dirty while
smaller eligible work proceeds. Atlas capacity is checked before execution;
replacement/clear work can reclaim obsolete slots within the same unit. Material
metadata becomes current only after its write is queued, preserving readiness
while content is deferred. Ordinary visible objects still upload progressively.

`VoxelUploadBytes`, `VoxelMaterialsUploaded`, `VoxelSectorsUploaded` and
`VoxelBricksUploaded` report admitted content. Brick counts include full-sector
records. Pending counters count physical shared-map queues once. See
[S1b scope and verification](../roadmaps/streamed-rendering-s1b.md).

## Specialized Subsystems

### Shadows

- directional shadows use cascades
- spot and directional shadow refresh are scheduled rather than fully rebuilt every frame
- shadow resources live under `voxelrt/rt/gpu/manager_shadow.go`

### Particles

- emitters are authored from ECS
- simulation is GPU-driven
- ECS sync hands typed particle frame input to the renderer app; renderer-side code owns the WGSL emitter packing and GPU buffer updates
- rendering happens in the accumulation pass
- details are in [`particles.md`](particles.md)

### Deferred decals

- opt-in retained surface overlays submitted through `VoxelRtState.SetRuntimeDecals`; the bridge copies, validates, and groups records by the existing sprite-atlas key
- local `+Z` points at the receiver; a shallow volume and receiver-normal cutoff keep projection from wrapping around nearby corners
- the pass clears its overlay every recorded frame, draws one instanced cube batch per non-empty atlas, and composites premultiplied linear color before deferred BRDF evaluation
- receivers are limited to opaque G-buffer surfaces; transparent geometry, sprites, particles, sky, and analytic bodies do not receive decals
- `DecalCount`, `DecalAtlasBatches`, `DecalDrawCalls`, `DecalTargetReady`, `DecalBindingsReady`, and `DecalPassRecorded` describe the pass state
- ActionGame currently owns its 4096-mark blood/soot pool; the generic renderer owns no gameplay retention or voxel edits

### Analytic media

- authored from `AnalyticMediumComponent`
- ECS sync hands typed analytic-media input to the renderer app
- current supported shapes:
  - sphere
  - box
- intended for bounded atmosphere and fog-style media
- rendering happens after deferred lighting in a dedicated half-resolution temporal pass
- compositing happens during resolve rather than through the WBOIT accumulation targets
- reusable presets live in `analytic_medium_presets.go`
- detailed authoring and subsystem notes are in [`media.md`](media.md)

### Water surfaces

- authored from `WaterSurfaceComponent` or resolved `WaterBodyComponent`
  patches
- ECS sync hands typed water surface and ripple input to the renderer app
- ungrouped water renders as dedicated horizontal box-shaped surface bodies with
  visible side walls
- patches with the same `continuity_group` render as a shared horizontal
  footprint: edge masks suppress internal patch sides while depth still drives
  absorption/thickness
- rendered during the accumulation pass instead of the half-resolution volumetric passes
- intended to stay stylized and voxel-adjacent, with stepped motion and discrete refraction/highlight response

### Sprites, text, and gizmos

- sprites render during accumulation
- text and gizmos are resolve-pass overlays
- text is frame-lifetime data and must be resubmitted every frame

### Probe GI

`core.VoxelObject` still has `ParticipatesInGI` metadata, but the live `App.Render()` path currently does not schedule a probe-GI bake or lighting-sample pass. If probe GI is reintroduced, document its resources and add it as an explicit graph node rather than hiding it inside another pass.

## Resource Recreation Rules

### Resize

`App.Resize()` must recreate or refresh all resources that depend on the surface size or views derived from it. That includes:

- surface configuration
- opaque storage texture
- G-buffer textures
- debug and fullscreen bind groups
- G-buffer, lighting, and shadow bind groups
- decal overlay target, decal pass bind groups, and the lighting binding that samples the active overlay or transparent fallback when decals are registered
- transparent-overlay bind groups
- particle, sprite, analytic-medium, and resolve pipelines

### Scene-resource growth

When `UpdateScene(...)` recreates buffers or detects a replaced scene-record
destination, `App.Update()` must rebuild dependent bind groups and advance
`SceneBindingRevision`. Renderer bugs after object-count growth, destination
replacement or shadow-capacity growth are usually stale-bind-group issues.

Voxel payload uploads follow the same rule. `BrickRecord` is now 32 bytes and uses explicit fields rather than overloaded payload/material storage:

- `material_index`
  - used by `Solid` and `UniformMaterial` bricks
- `payload_offset`
  - packed 3D payload-atlas offset used only by payload-backed sparse bricks
- `occupancy_mask_lo` / `occupancy_mask_hi`
  - coarse `2x2x2` microblock occupancy mask
- `payload_page`
  - payload atlas page for payload-backed sparse bricks
- `flags`
  - includes `BrickFlagSolid` and `BrickFlagUniformMaterial`
- `dense_occupancy_word_base`
  - exact `8x8x8` occupancy pointer for non-solid bricks

The live brick-mode contract is:

- `Solid`
  - whole brick occupied
  - reads `material_index`
  - does not allocate payload atlas storage
  - does not allocate dense occupancy
- `UniformMaterial`
  - sparse occupancy
  - reads `material_index`
  - allocates dense occupancy
  - does not allocate payload atlas storage
- payload-backed sparse
  - sparse occupancy
  - reads `payload_offset` and `payload_page`
  - allocates dense occupancy
  - allocates payload atlas storage

Any bind group or shader that reads voxel payload data must be recreated if payload pages or voxel-table resources were recreated, and any pass that reads `BrickRecord` must keep this field order aligned with the GPU upload path in `voxelrt/rt/gpu/manager_voxel.go`.
Hybrid sector lookup is now part of that same contract. `ObjectParams` is 128 bytes, qualifying objects use object-local direct lookup, `SectorGridBuf` still holds the hash-probed `SectorGridEntry` array, and `DirectSectorLookupBuf` now carries the compact direct-lookup words as a dedicated storage buffer. Any pass that reads voxel occupancy must keep its shader structs, bind groups, and hand-written pipeline layouts aligned with that live layout. This split depends on `App.Init()` requesting adapter-supported limits when creating the native WebGPU device.

## Common Sources of Drift

- prose docs describing the old fullscreen blit path as the live compositor
- forgetting that picking and editing still use CPU-side scene data
- changing resize-sensitive resources without updating `Resize()`
- changing scene-buffer layouts without rebuilding dependent bind groups
- changing half-resolution volumetric resolve inputs without updating resolve bind groups and shader bindings together
- changing analytic-media history or half-resolution target bindings without updating `feature_analytic_medium.go`, `app_medium.go`, `manager_medium.go`, and `resolve_transparency.wgsl` together
- changing decal target, G-buffer, or lighting bindings without updating `feature_decals.go`, `manager_decals.go`, `manager_render_setup.go`, and both decal shaders together
- changing voxel payload page bindings, dense-occupancy bindings, hybrid-lookup metadata, or `BrickRecord` layout in one pass but not the other voxel consumers
- changing a shader resource list without updating the corresponding hand-written pipeline layout in `voxelrt/rt/app/`
