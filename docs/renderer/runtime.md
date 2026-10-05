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

## Voxel point reads

`(*volume.Brick).VoxelValue(x, y, z)` reads the authoritative dense payload cell.
Indices must be within `[0, volume.BrickSize)`. Reads do not interpret or repair
material flags, occupancy, atlas offsets or auxiliary normals. Public raw
`Payload` edits remain visible, including when metadata is stale; copied bricks
retain independent inline payloads.

Engine queries, collision, GPU packing, snapshots and editor export use this
accessor for point reads. Brick-owned mutation/scans and whole-array capture or
comparison retain direct access. This is a migration prerequisite for compact
storage; it changes neither representation nor immutable ownership.

## Dense voxel construction

`volume.BuildXBrickMap(iter.Seq[volume.VoxelWrite])` consumes ordered writes once
and returns a fresh, independently owned editable `XBrickMap`. A nil sequence is
empty. Zero removes a voxel; duplicate and no-op writes retain `SetVoxel` content
and revision semantics, including removed-sector revision history. Negative
coordinates use the existing sector/brick partitioning.

Construction updates occupancy and dirty coverage while consuming writes, then
finalizes material flags once per surviving brick. Initial uploads and normal
halos include transient and deleted writes. Bounds remain lazily computed;
copies and changed-sector snapshots keep their existing ownership. This uses
the current dense payload, without live GPU offsets or shared mutable backing.

Imported full/proxy construction streams effective material values through this
builder. Source-zero records remain ignored, decoded records remain authoritative,
and auxiliary normal records attach afterward through the existing copied path.
Fresh construction uses its existing dirty keys to mark each normal-halo key
once, retaining exact transient/deleted coverage without another scratch map.
Per-write halo enumeration and decoded voxel-record storage remain.

Terrain columns, voxel-object snapshots and offline imported aux construction use
the same dense builder. Terrain retains column order and its supplied solid
value. Snapshot and aux converters keep source-zero filtering and their existing
clean dirty-state publication; nil input retains each converter's prior behavior.
The builder changes reconstruction work, not persisted content or normal encoding.

Ordinary VOX asset construction also streams ordered writes, including zero
material deletions, then computes tight bounds and clears dirtiness. Declared
model dimensions still override asset-local bounds. Persistence workers stream
nonzero captured brick payloads in their original order and restore captured
cached bounds exactly; capture ownership and durable publication are unchanged.

Shift/Center, resampling and disconnected-component reconstruction apply ordered
edit streams to their original fresh destination maps. Source voxel/auxiliary
data stays independent; material flags finalize before output bounds/publication.
Traversal, sampling, component selection and captured bounds retain their rules.
Resampling still returns the original source when its iteration limit rejects
the request. These paths introduce no shared mutable payload or extra write list.

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

Managed streamed terrain can supply an independent worker-prepared first renderer
map. Bridge admission validates current source content before using it, preserving
object isolation, current public mutations and runtime-map readiness identity.
See [terrain registration ownership](../assets/runtime-assets.md#streamed-terrain-registration)
for candidate lifetime and fallback rules.

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

## Linked Emitter Source Radii

Light sync derives a source radius when `EmitterLinkID` is nonzero and
`SourceRadius` is zero after the existing negative-radius clamp. All requested
groups share one object scan per invocation. Each group uses the largest
matching object's world-AABB half-diagonal; absent groups remain zero. Only
requested groups update bounds. Explicit positive and NaN radii, light ordering,
ambient and Sun extraction keep their existing behavior.

The aggregate lasts for that sync only. Normal core passes still observe direct
scale, geometry, emitter-link and light edits and committed removals.
`VoxelRtState.VoxelEmitterRadiusObjectVisitsLastSync` counts object entries
visited for radius derivation, including unrelated entries. It is zero without
eligible requests or available sync inputs. This measures scan work, not FPS or
total bridge cost; it does not authorize dirty-only extraction.

## Effective Palette Fingerprints

Core instance sync reads each used object palette and evaluates its animations
for current elapsed time every frame. Per-frame palette deduplication remains.
`VoxelRtState` reuses the original material fingerprint only when all effective
fingerprint inputs exactly match an independently owned previous snapshot.
Floating-point comparisons preserve bits; property comparisons preserve dynamic
types and map membership. Same-ID edits through aliased material/animation data
remain visible. `SurfaceMaterials` is not a material-table input.

Snapshots belong to the current state and `AssetServer` identity. They own their
maps, slices and string backing. Server replacement clears them; complete sync
prunes unused palettes and releases empty ownership. Ordinary hidden and
successful sprite-LOD entities do not use object material snapshots; hidden
streamed objects do. Geometry, transforms, metadata, LOD, camera/lights, streamed
adoption and existing destination repair still run on live inputs.

`VoxelMaterialFingerprintBudgetBytes` is 8 MiB of conservatively accounted
retained snapshot data/metadata. Support and charge are checked before copying.
Oversized, over-budget or unsupported property inputs use the original per-frame
hash path and release any prior snapshot for that ID. Supported property values
are `float32`, `float64`, `int` and `string`. Admission has no fairness guarantee;
unused entries can delay new admission until pruning at the end of that frame.
The budget excludes assets, temporary effective palettes, historical material
tables and GPU allocations; it is not a process-memory ceiling.

Diagnostics: `VoxelMaterialFingerprintBuildCount` counts actual original hash
executions through the state key path, including fallback;
`VoxelMaterialFingerprintCount` and `VoxelMaterialFingerprintBytes` report current
retained snapshots and accounted bytes. Private accounting controls admission.
Exact comparison remains proportional to palette data; fewer hashes establish
no measured frame-time gain. Hash semantics and material-table construction are
unchanged. Rationale: [S3r decision](../roadmaps/streamed-rendering-s3c.md#s3r-effective-palette-fingerprint-reuse-decision).

## CPU Material-Table Cache

`VoxelRtState.SetVoxelMaterialTableCacheBudgetBytes(int64)` configures retained
CPU tables on the main thread: zero selects `DefaultVoxelMaterialTableCacheBytes`
(16 MiB), negative disables warm retention, positive sets an accounted byte limit.
Configuration changes the reported maximum immediately; the next complete
instance sync trims inactive entries, including when no new table is built.

Each key charges CPU material slice capacity plus conservative entry metadata
once. Current object keys, including hidden streamed objects, remain pinned and
refresh LRU age without counting cache hits. Inactive entries are evicted oldest
first until the budget fits or only pinned excess remains. Eviction releases
cache references and prunes map capacity; existing object copies remain valid.
Hashing and table construction remain unchanged. P4 gives each object independent
mutable rows while certified static tables share GPU blocks, as described below.
Inactive keys use an indexed minimum heap ordered by saved usage age. Warm hits
repair its order; pin transitions remove/add eligibility without making newly
inactive keys newer. Completed maintenance gathers current keys from instances,
then makes one owner pass for pins/accounting. Pressure selects victims directly
without collecting/sorting all inactive keys. Removed slots clear references;
empty ownership releases the heap, while nonempty capacity may remain.

`VoxelMaterialTableCacheStats()` returns a value with `Entries`, `Bytes`,
`PinnedBytes`, `MaxBytes`, `PressureBytes`, `Builds`, `Hits`, `Evictions` and
`EvictionCandidateVisits`.
Reads perform no work. Pins reflect completed maintenance; pressure is pinned
bytes above the current maximum. Builds count actual table construction, hits
count successful builder lookups, and evictions count removed cache keys.
Candidate visits count actual pressure victims cumulatively; reads, no-pressure
maintenance and all-pinned pressure add none. Private accounting owns retention.
Nil state returns zero stats. Temporary construction/active-key collection,
external borrowers, assets and GPU allocations are excluded; this is not a process
or GPU memory limit. [Ownership rationale](../roadmaps/streamed-rendering-s2a.md#s2c-cpu-material-table-cache-ownership-decision).

## Immutable GPU Material Blocks

The bridge certifies static palettes using an independently owned full semantic
snapshot, including surface kinds/tags, properties and provenance. This identity
is separate from the narrower effective-palette rendering fingerprint. Exact live
comparison reuses unchanged snapshots; canonical encoding runs once per used
palette ID/change, rather than once per instance or idle frame. Animated palettes,
frame overrides, unsupported identity values and over-budget inputs remain private.

`core.NewImmutableMaterialTable(rows, semanticKey)` seals at most 256 rows and an
opaque complete semantic key. Its identity encodes the key and exact row bits;
row access returns a defensive copy. `VoxelObject.SetImmutableMaterialTable` gives
the object independent public mutable rows. `ImmutableMaterialTable()` checks
those rows and permanently detaches certification on a mismatch until the owner
explicitly installs another handle. Unchanged bridge sync preserves an instance
edit rather than resealing it. Asset IDs, row pointers and rendering hashes alone
never certify sharing.

`VoxelMaterialSemanticIdentityBudgetBytes` limits bridge snapshots to 8 MiB of
conservatively accounted ownership. Support and charge checks precede cloning and
encoding. `VoxelMaterialSemanticIdentityBuildCount` counts actual canonical encode
attempts; `VoxelMaterialSemanticIdentityCount` and
`VoxelMaterialSemanticIdentityBytes` describe retained current snapshots. Unused
palettes, empty ownership and server replacement release this ownership. The budget
excludes instance row copies, the CPU table cache and GPU-manager packets; it is
not a process-memory limit.

The GPU manager owns one 256-row, 16 KiB block per exact certified identity and
separate per-object `MaterialAllocations` attachments. A block owns one CPU row
snapshot and one padded GPU packet, encoded once during its lifetime. Admission
reserves distinct blocks; service uses their highest-priority current requester
and uploads each block once per buffer generation. Late attachments to uploaded
blocks require no write. Readiness and shader publication require the current
binding and acknowledged generation. Attachment changes invalidate only the
object's shadow dependency; uploaded opacity acknowledgement remains object-local.

Shared slots retire after the final attachment through existing render-thread
queue ordering. Desired resident identities stay pinned across binding swaps.
Abandoned shared blocks can fund replacement admission only when no active scene
object still requests them, including objects outside the current service targets.
If a partial replacement reuses a slot, stale denied attachments are removed before
publication; failed resource growth preserves the old bindings. Optional new block
demand obeys the optional admission cap. Fully queued buffer migration carries
acknowledgement for already uploaded immutable blocks; pending blocks and external
generation resets still require service. Raw uncertified tables keep private
blocks and their existing mutable upload behavior.

After certification detaches, later private edits retain the legacy pointer/length
upload contract; replace the public slice to signal another update. P4 does not
change the CPU row cache's existing palette-ID/render-fingerprint key. Semantic
snapshot properties support finite `float32`, `float64`, `int` and `string` values;
other property types conservatively fall back to private ownership.

Shader layouts and 256-entry local addressing are unchanged. Physical buffers
retain their high-water capacity after release. Independent mutable instance rows
cost CPU memory (10 KiB for 256 rows); sharing reduces GPU block/packet duplication,
not every CPU copy. See [P4 delivery](../roadmaps/streamed-rendering-p4.md) for
verification and measured material traffic; no frame-time improvement is claimed.

## Allocation Snapshot Ownership

Manager-created voxel allocations count every sector-coordinate and brick-index
reference in their committed snapshots. Shared assignments remain live until the
final allocated reference disappears. Upload eligibility excludes only the exact
replacement unit and still protects moved bricks throughout the current sector.
Successful queued writes update ownership before checking whether selection or
revision changed; refused writes preserve the previous snapshot.

The manager validates allocation headers once at each outer update, preparation,
service or cleanup boundary. Reclamation then uses reference counts without
scanning resident sector/brick snapshots. Injected, foreign or replaced allocation
headers permanently restore legacy scan behavior for that manager and discard
private bookkeeping. Arbitrary writes inside derived GPU allocation snapshots are
not tracked producers; supported raw `XBrickMap` edits retain their existing dirty
processing contract.

Counts describe allocated edges, including pending and retained snapshots, rather
than desired CPU geometry or admission reservations. Existing remove-before-add
ordering, payload-mode eligibility and conservative auxiliary lifetime remain
unchanged. Whole-map structural preparation, admission planning and global lookup
publication remain separate atomic work. Bookkeeping storage is outside retained
GPU slot budgets; no whole-frame or process-memory ceiling is implied.

## Retained GPU Geometry Budget

`GpuBufferManager.RetainedVoxelMapBudgetBytes` caps retained assigned geometry
slots alongside `RetainedVoxelMapBudgetSectors`. The constructor uses
`DefaultRetainedVoxelMapBudgetBytes` (128 MiB, initially unmeasured). Nonpositive
values disable only the byte cap; the legacy sector cap remains independent.
Configuration takes effect during the next complete voxel update.

Each exact retained `XBrickMap` charges once: 256 bytes of entry metadata, actual
32-byte sector slots, 64-record brick-table blocks, and actual auxiliary/payload
slots. Slot identities are deduplicated within that map. Charge uses allocated
snapshots rather than current CPU contents or flags; uniform bricks without a
payload slot pay no payload charge. Empty/unallocated entries still pay metadata.
Shared sector/brick assignments remain live until their final allocated reference
disappears; retention charge still deduplicates only within each map.

Complete updates pin every selected render map and valid pending full upload in
`Scene.Objects`, including hidden uploads, and refresh active LRU age without
counting hits. Authoritative
CPU geometry does not require GPU allocation when a separate representation is
selected. Trim runs before
orphan cleanup and after uploads. Either enabled cap can evict inactive LRU maps
through the existing slot-release path. Active excess remains pinned. Retention
map capacity is pruned; eviction preserves CPU geometry and object materials.
Inactive candidates use a minimum heap keyed by existing `LastUse`; becoming
inactive preserves the saved age. Retain/activation updates repair its order,
and release removes the entry. Pressure selection avoids collecting/sorting all
inactive owners; normal pin/accounting maintenance still visits retained owners.
Removed entries clear their heap slots; a nonempty heap may retain peak capacity.

`RetainedVoxelMapStats.EvictionCandidateVisits` counts cumulative nonnil pressure
candidates selected, and streaming exposes
`GPURetainedVoxelMapEvictionCandidateVisits`. Reads, no-pressure maintenance,
all-pinned pressure and explicit releases do not advance it.

`RetainedVoxelMapStats()` adds `Bytes`, `PinnedBytes`, `MaxBytes` and
`PressureBytes` to existing counters. Reads sum private scalar accounting without
slot scans, allocation, trimming or work-counter changes. Retain captures charge
immediately; touched structure/uploads invalidate only that map and maintenance
refreshes its completed assigned-slot charge. Arbitrary direct GPU-plumbing map
writes are not tracked producers. Pins reflect the last maintenance boundary;
maximum reflects current configuration. Pressure is pinned bytes above an enabled
maximum, or zero when disabled. Nil manager returns zero stats.

This budget excludes physical buffer/atlas capacity, free/headroom slots, retired
resources, lookup/object/material buffers, CPU geometry/allocation snapshots and
temporary accounting. Eviction does not shrink GPU buffers or atlas pages; no
VRAM/process ceiling is claimed. [Ownership rationale](../roadmaps/streamed-rendering-s2a.md#s2d-retained-gpu-map-byte-ownership-decision).

## Physical Voxel GPU Admission

`SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: ...})` controls
optional growth of voxel resources. Zero disables the soft cap and preserves
ordinary loading. The default is zero: the four fixed R8Uint payload pages alone
can occupy 4 GiB at the supported 1,024³ page size.

The GPU manager counts current sector, brick, auxiliary, material, hash-grid,
direct-lookup and sector-grid parameter buffers, created staging replacements,
unreleased retired replacements, and created payload atlas pages. A replacement
needs room for old and new buffers at once. Retired bytes leave the charge only
after native release. Other renderer
resources, render targets and driver overhead are excluded; this is not a total
VRAM ceiling. Slot release and retention eviction do not shrink physical buffers.

Core objects default to required admission. `VoxelGPUAdmissionOptional` permits
new selected geometry and material owners to defer; the ECS bridge assigns it
only to explicitly hidden streamed objects. A compiled startup readiness wait
does not make an ordinary object optional. Valid pending full detail is always
optional, preserving its ready coarse display. Any required shared instance
makes geometry required, while a new optional instance still needs its own
material admission. Existing map and unchanged material owners remain pinned;
required growth can exceed the soft cap. Replacing a certified material block is
new demand and follows the object's optional classification. Fresh required
geometry preflights its first required material owner together. Zero-growth work can reuse slots under pressure;
smaller fitting arrivals continue past a blocked target. Admission preserves the
existing retention eviction policy; a blocked optional target can wait for slots
to become free or for the cap to increase.

Device buffer, storage-binding and uniform-binding limits remain hard for every
owner. Allocation headroom is reduced when content fits those limits. Planning
precedes slot assignment, content writes and lookup publication. Native resource
replacement publishes after allocation and migration preparation succeeds;
refusal preserves existing resources, CPU geometry and unacknowledged dirty work.
A hard-blocked structural edit freezes all content uploads for that map and
keeps lookup on its prior allocated sector snapshot. Reactivated retained geometry
stays pinned but cannot become ready until its lookup fits. New deferred maps
publish no lookup entries, even when they share assigned sector pointers.
An object without admitted materials or published lookup gets an invalid GPU
descriptor. Cached shader lookup rejects that descriptor before using a sector
cache, including when the valid shared map ID is zero. Record layouts stay unchanged.

Optional geometry and material demand share an aged admission order. Required
geometry and required materials of admitted geometry remain first. Waiting
optional demand gains one priority level every eight admission updates, stopping
at priority zero; older demand wins equal effective priority before stable order
and IDs. A required shared map does not give new optional materials required
priority. Raw geometry representatives still own joint material preflight.

Geometry age follows live object/map/pending-generation requests; material age
follows selected object/map requests. Shared geometry uses the best current
request without losing surviving users' wait. Edits to a still-live request retain age;
completion, detachment, required reclassification and new generations clear stale
optional demand. Backend fallback uses the same age snapshot and retains only
final-plan refusals. Smaller fitting work can pass a blocked request. Progress
requires recurring fitting capacity; no fixed latency is promised.

`VoxelGPUAdmissionStats()` reports `CurrentBufferBytes`, `StagingBytes`, `RetiredBufferBytes`,
`AtlasBytes`, `TotalBytes`, configured `MaxBytes`, `PressureBytes`, deferred map
counts, cumulative allocation failures and the last allocation error. Reads use
scalar state from the last voxel update without traversal or mutation. Byte sums
saturate; nil manager returns zero stats. Configuration takes effect at the next
voxel update. [Admission decision](../roadmaps/streamed-rendering-s1b.md#s1i-physical-voxel-resource-growth-admission).

## Voxel Buffer Creation and Migration

`SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: ...,
MaxCreates: ..., MaxCopyBytes: ...})` bounds native voxel-buffer growth per
`UpdateVoxelData` invocation after renderer bootstrap. The default is disabled,
preserving synchronous loading. Enabled zero creation bytes or count pauses
creation; zero copy bytes pauses migration. Copies use four-byte aligned ranges;
a copy budget below four bytes cannot make progress. Disabling limits completes
an existing generation using the ordinary unlimited work path.

Creation is indivisible. With positive creation limits, a resource larger than
the byte cap may be created only as the sole creation in that update. It is
reported as oversized. An oversized resource encountered after another creation
waits for a later update. This exception does not bypass physical admission or
device limits. Native creation and command submission have no elapsed-time cap.
Initial minimum bindings and fixed payload atlas creation remain synchronous.

The GPU manager retains one fixed physical staging generation for the seven
voxel buffers. Creation and copying advance independently of arriving logical
demand. Published buffers remain active until all replacements are complete;
buffer pointers publish together, followed by fresh admission from live scene
inputs. Removed or changed demand cannot acquire ownership from a captured plan
or restart copying indefinitely. Already admitted staging remains pinned when
the soft memory cap is lowered; subsequent optional growth needs fresh admission.

Copies submit before this update's content writes. Every write to a replaced
buffer also reaches its created staging replacement, including empty record
clears and lookup updates. Duplicate content bytes consume the global voxel
upload budget; payload textures are written once. Lookup writes retain their
separate existing budget scope. Mirroring stores no replay journal. Existing
material-generation invalidation still applies to private tables at publication
and can require material reupload before readiness returns. Fully migrated,
already uploaded immutable blocks carry acknowledgement into the published
generation; unuploaded blocks still wait for service.

An allocation or migration error preserves published resources and dirty CPU
authority. Unused unpublished buffers can release immediately; staging resources
referenced by copies or mirrored writes enter safe retirement. Their charge
survives until native release. The render submission fence covers every queued
use, including writes after the last migration submission. Waiting for a budget
is not an allocation failure.

`VoxelGPUWorkBudget()` reads configuration. `VoxelGPUWorkStats()` reports
`CreatedBytes`, `Creates`, `CopiedBytes`, `OversizedCreates` and `Pending` for the
last voxel update, without traversal or mutation. Nil manager reads return zero.
Whole-map structure preparation and global lookup rebuilding remain atomic;
these limits do not bound all renderer transfers or frame time.

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

### Local shadow cache dependencies

Point and spot layers use GPU-manager-owned local dependencies instead of global
scene/upload revisions. Exact scalar snapshots include selected caster membership,
render-map identity/revision, matrices, bounds, object metadata, allocation
identity and successful content uploads. Source radius and emitter links are
part of light identity. Spot dependencies use the existing conservative light
volume. Point dependencies use independent face cones over selected
`Scene.ShadowObjects`, including off-screen and grouped casters. Each face follows its fixed world-axis shader
direction (+X, -X, +Y, -Y, +Z, -Z). Conservative AABB plane separation includes
seams, origin contact and numerical uncertainty. Invalid bounds or unsupported
light/face inputs retain casters. Point shader traversal is not capped by light
range, so selected downstream casters remain dependencies even beyond that range.
Source radius does not narrow membership; scene selection itself is unchanged.
Point membership conservatively includes both world-space cones and cones over
the float32 render-relative bounds/position published to the GPU. This covers
rebasing roundoff without dropping world-space dependencies. Origin changes
recompute membership; identical selected inputs/membership preserve current faces.
Ordinary camera rebasing does not force point-map rebuilds. Invalid rebased inputs
retain casters. This preserves the existing world-input cache contract; it does
not promise bitwise fresh-map parity across arbitrary coordinate rebasing.

Within one preparation, supported point faces share a caster/light footprint
classification by selected source index. Only faces needing membership work enter
the requested mask; world bounds normalize once, and packed bounds normalize only
for requested bits not already covered by the world footprint. Scratch is lazy and
scoped to that point light and preparation, including distinct duplicate indices.
It cannot reuse footprints after light, origin, selection or bounds changes.
`GpuBufferManager.ShadowPointMembershipClassificationCount` cumulatively counts
actual footprint classifications, including invalid-input fallbacks. Unsupported
face assignments retain casters without classification. The existing intersection
counter still counts each consuming face predicate, so classifier sharing does not
change membership or acknowledgements. Mask and grouping scratch add reusable CPU
capacity beyond the per-owner member-storage diagnostic.

The manager captures and exactly compares all live selected caster scalar inputs
once per preparation. Changed snapshots receive unique manager-owned tokens;
volume owners compare compact member tokens instead of retaining full snapshots.
Unchanged snapshots preserve their tokens across source-index shifts and structural
reorder, including duplicate selected occurrences. Retired or reset snapshots cannot
reuse a token for different inputs. Structural selection changes use reusable
identity remapping and temporary shared storage; cleared old snapshots release
source references. Stable selections need no identity-map lookup or full-key copy.
`GpuBufferManager.ShadowDependencyMemberStorageBytes()` reports the backing-capacity
bytes of per-owner member indices and dependency records across spot, point-face
and directional owners. It excludes shared scalar inventory, temporary membership
and structural storage, owner headers, source content and GPU allocations. Capacity
may remain at its high-water mark while an owner lives; retired owners release it.

Unchanged volumes reuse sorted membership indices. With stable ordered caster
identities and count, a bounds change rechecks only changed indices for owners at
the immediately preceding membership revision. Both entry and removal update
membership; every retained member still compares its current shared snapshot token.
Cold owners, structural selection changes, changed light/projection/layer inputs,
point-origin changes and missing delta history require full membership scans.
The delta is manager-owned preparation state, not a producer notification queue.
`GpuBufferManager.ShadowMembershipIntersectionCount` cumulatively counts actual
caster-volume predicate evaluations across spot, point-face and directional
owners, including full fallbacks. It excludes scalar capture and retained-member
comparisons. Removed casters and lights release snapshot references. This adds
scoped CPU snapshot storage, including approximately 1 KiB of opacity metadata per material allocation;
existing cache budgets do not become a total process-memory ceiling.

Geometry uploads invalidate dependent placements, including shared maps and
written work whose source becomes stale afterward. Material uploads compare exact
transparency float bits and written row coverage. Identical palette reuploads
caused by buffer growth preserve local shadows; pointer identity alone cannot
hide an opacity change. Opacity captures precede execution and publish only after
successful writes, preserving unwritten tails and empty-table zero uploads.
Unattributed public `VoxelUploadRevision` changes conservatively invalidate all
local lights, including changes during a tracked executor.

`UpdateScene` prepares local dependencies and serializes light readiness after
voxel upload and lookup maintenance. Scheduling uses the same dependency
revision. `BuildShadowUpdates` and `RecordShadowUpdates` retain their synchronous
render-thread contract. Only recorded updates acknowledge dependencies. A point
light remains disabled until all six independent face dependencies are current.
Each face retains exact caster/upload inputs plus light, layer/face assignment and
effective resolution. Caster edits invalidate intersecting faces; position, source
radius, emitter links and other light inputs invalidate every face. A whole-map
resolution change affects all faces, while a face-specific resolution change
affects that face. Unrelated uploads do not restart partial face progress. Valid
point/spot layers remain cached regardless of age: local `CadenceFrames` metadata
does not trigger rebuilds.
Initial and invalidated lights retain the existing per-tier light budgets, nearest
light priority and point-face budgets/rotation. Valid point faces are filtered
before the face budget is applied. Pending work remains invalid until recorded;
a second caster change during partial refresh only discards acknowledgements of
faces dependent on that change. Global light or unknown-upload changes require
all six faces to acknowledge their new dependencies. Once current work completes,
local dispatch stops until inputs change. This does not redesign priority among
continuously dirty lights. Directional cascades use the per-layer dependencies below.
See [R2 ownership decision](../roadmaps/streamed-rendering-r2.md).

### Directional shadow cache dependencies

Each directional cascade has its own dependency generation. It shares the selected
caster snapshots, allocation-owned upload epochs and conservative unknown-upload
fallback described above. Exact keys additionally include cascade projection,
inverse projection, parameters, effective resolution and light/cascade/layer
assignment. Emitter links participate in light identity. Only recorded updates
acknowledge a cascade's current generation; recording another cascade cannot
acknowledge its pending content.

Membership follows the actual inverse-projection ray prism: orthographic XY and
the near-plane origin, with no downstream far-plane cutoff. The shader continues
traversal beyond its depth projection. Invalid bounds or unsupported, nonfinite,
singular or poorly conditioned inverse projections conservatively retain casters.
AABB projection uses affine endpoint intervals, with column-ordered sums and
translation last. These attain the same row extrema and cancellation guard as
eight-corner projection. All XYZ rows validate before clipping; numerical
uncertainty retains the caster. `GpuBufferManager.ShadowDirectionalBoundsProjectionCount`
cumulatively counts evaluated XYZ row projections: two endpoints per row, six
per valid AABB/cascade check. Invalid bounds or unsupported projections perform
no row projections. Membership predicate counts and dependency generations are
unchanged. Scalar comparisons remain live while unchanged membership reuses
indices. Removed layers release retained caster references.

Valid cascades remain cached regardless of age. `CadenceFrames` remains metadata
for compatibility but does not schedule periodic work. Initial or changed inputs
schedule the affected cascades; only recorded updates acknowledge their content.
The GPU manager's explicit forced-refresh request still schedules both cascades.
App shadow recording uses ordinary dependency scheduling: missing camera history
or motion metadata alone does not force work. `ShadowCameraMotion` remains a
profiler diagnostic. Actual camera changes rebuild cascades when their projection
or selected caster inputs change.

Directional XY texel grids are anchored in a fixed world light basis. Frustum
extents are fitted in camera-relative coordinates, so pure lateral translation
with unchanged orientation/intrinsics does not alter fit size. The world center
is rounded to the final texel grid; snapped zero is canonicalized for exact
bitwise dependency keys. For resolutions at least two, one base texel of fit
margin conservatively covers the maximum half-final-texel center shift. Resolution
one uses an unsnapped fit; zero keeps the existing 1024 default. Culling and render
projection use the same view and extent. Light-space depth is not quantized.

Sub-cell motion can reuse maps only while exact projection and caster dependencies
remain unchanged. Grid crossings, changed orientation/intrinsics/sun direction,
depth motion and caster inputs still invalidate affected cascades. This changes
grid placement and slightly expands coverage; it does not introduce approximate
cache equality, scrolling clipmaps or partial-map updates.

Cached cascade transforms stay paired with their maps until the scheduled rebuild
publishes new transforms through `PrepareShadowLights`. This does not
change projection generation, scene caster selection, shader traversal, GPU
layouts or the synchronous recording contract.

### Shadow update publication

`DispatchShadowPass` owns publication of the 24-byte shadow update records.
`CreateShadowBindGroups` ensures a usable update buffer and binds current resources
without rewriting queued records. Missing buffers use WebGPU's zero-initialized
storage; capacity-only setup does not publish a placeholder header. Update-buffer
growth and explicit scene-resource rebinds therefore preserve the published
light, layer, cascade, kind, tier and resolution fields.

Each dispatch call snapshots supported records into an immutable GPU copy-source
packet. Buckets preserve input order within the fixed 512, 256, 128, 1024 sequence;
unsupported resolutions and empty calls do no work. The manager provisions shared
storage once for the largest bucket, then records a copy to offset zero immediately
before each bucket's compute pass. It never queue-writes bucket records or submits
inside dispatch. Multiple calls on one encoder remain independent even if caller
slices are reused or destination storage grows before submission. Encoded commands
retain packet references; dispatch releases its source handle without destroying
the resource. Existing storage and bind-group retirement rules remain in force.

This adds one transient packet allocation per nonempty supported call and one
copy command per bucket. Shader bindings, 24-byte records, resolution-specific
work sizes and cached map/transform pairing are unchanged by record publication.

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

### Voxel capacity planning

Before structural preparation, capacity planning walks sectors only for unique
new maps or maps with `StructureDirty`. Clean allocated maps use existing
allocator tails. Hidden upload candidates remain eligible. Required sector and
brick records retain pointer deduplication, the fixed 64-record sector stride and
existing buffer headroom; planning reserves no slots and changes no maps.

`VoxelCapacityPlanningSectorVisitsLastUpdate` resets each update and counts
sector entries inspected by this planning step. Other scene scans and the work
within one new or dirty map remain.

### Normal neighbor preparation

After structural dirty preparation, each voxel update snapshots original dirty
bricks before propagating cross-object normal halos. Halo propagation runs even
when uploads are paused. One invocation-local lazy getter builds the full live
`Scene.Objects` neighbor context only for qualifying halo work or runtime auxiliary
baking. Valid precomputed sidecars bypass it. Idle/material-only frames need no
context unless dirty adjacency/planet sources require halo propagation.

Full-scene hidden neighbors, last duplicate ownership, explicit adjacency over
terrain fallback and current metadata remain the normal contract. No context or
object references survive the update. Packed occupancy/normal bytes and upload
completion remain unchanged.

`VoxelNormalContextBuildCount` counts cumulative actual context builds;
`VoxelNormalContextObjectVisitsLastUpdate` resets each update and counts entries
visited by that build, including nil entries. Dirty inspection, allocation cleanup
and upload-planning scans remain. These counters do not imply a frame-time gain.

### Terrain and planet lookup preparation

`UpdateScene` prepares both lookup buffers in one live `VisibleObjects` pass.
Unchanged encoded scalar rows reuse CPU tables; direct metadata writes, eligibility
changes and visible ordering remain observable without producer notifications.
Terrain and planet eligibility are independent. Ordered duplicates, first-match
probing, int32 coordinates/indices and the existing shader headers remain intact.
Both buffers still receive their existing GPU writes each frame.

`ObjectLookupCacheBudgetBytes` bounds retained row and encoded-byte capacities;
the constructor defaults to 4 MiB. Nonpositive budgets disable retention, and
oversized or lowered-budget ownership is released during preparation. Temporary
builds and GPU buffers are outside this CPU ceiling. No Scene/object pointers are
retained. Internal byte views are read-only and borrowed until the next preparation.

`ObjectLookupCacheBytes` reports retained capacity. `ObjectLookupBuildCount`
counts cumulative paired table builds; `ObjectLookupInputVisitsLastPrepare`
counts visible entries, including nil/ineligible entries, in the current pass.
This reduces CPU preparation; no frame-time gain or dirty-only extraction is claimed.

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

### Compiled asset LOD boundary

Explicit compiled LOD declarations publish authoritative level-0 geometry and a
separate verified coarse asset. Only declared part instances carry private intent.
Eligible instances use the coarse render representation while preserving fine CPU
authority. Existing `EntityLODComponent` simplified-voxel bands request coarse
hold. Without a valid band, instances request fine detail after coarse startup. No new distance
thresholds are inferred. Opt-in streamed instances use these voxel bands, while
legacy streamed entities continue bypassing automatic proxy/impostor selection.
Noncompiled simplified geometry retains center-nearest resampling and its existing
CPU/display behavior; ordinary sprite branches remain unchanged.

A compiled derivative's original content identity cannot certify the current
mutable runtime map. Integrating derivatives must qualify current geometry,
retain level-0 collision/navigation/edit authority and keep animated parts
independent. Coarse display and fine staging must remain under existing streaming
ownership, with readiness checked for the staged target before changing display.
Changing the target of an unfinished single-map ticket cancels that ticket.
Opt-in conservative coverage for single-material opaque assets is approved;
derivative frames, explicit compiler opt-in and qualified runtime integration are
available. See the
[C3 LOD diagnostic and gates](../roadmaps/streamed-rendering-c3.md#asset-lod-diagnostic-c3h0).

### Authoritative geometry and render representations

`VoxelObject.XBrickMap`, `Transform` and `WorldAABB` remain level-0 CPU
interaction authority. A private optional representation provides
`RenderVoxelMap`, `RenderObjectToWorld`, `RenderWorldToObject` and
`RenderWorldBounds`. With no selection, these preserve exact legacy values and
the existing bounds pointer. Scene object identity and CPU queries do not change.

`SetRenderLOD2` borrows a nonempty, separate coarse map and captures both source
and coarse map identities/revisions. Its zero-anchored transform is
`fullObjectToWorld × Scale(2)`; the inverse is `Scale(.5) × fullWorldToObject`.
Bounds transform all eight corners of the actual occupied coarse bounds. They
follow current instance transforms, including pointer replacement, without
changing the authoritative pivot, bounds or dirty flags. Only the coarse map's
existing AABB cache may refresh.

Terrain, planet tiles and objects with terrain/planet/voxel adjacency group IDs
reject this ordinary-asset representation. Adding those tags later invalidates
an existing selection; their lattice and neighbor sampling retain legacy rules.

Tracked geometry changes or loss of the authoritative transform invalidate an
active representation: render map/bounds become nil until the owner makes an
explicit transition. `ClearRenderRepresentation` releases representation
references and restores the legacy path. Rejected setters also clear selection;
neither operation certifies full geometry readiness. The bridge must check the
setter result and apply visibility/readiness policy before GPU extraction.

Revision guards do not prove raw mutable aliases, original C1 provenance,
current material opacity or upload readiness. Those qualifications remain with
the bridge and existing upload/streaming owners. The approved cold fallback is
to briefly hide an invalid coarse representation until full geometry is ready;
CPU interaction authority remains resident. Ordinary updates to an already
initialized full representation retain the existing dirty-upload behavior.

Scene frustum/HiZ and shadow selection, render BVHs, GPU records, sector lookup,
allocation planning, uploads and normal baking consume the selected representation.
Scene owns value snapshots of render bounds so selection changes rebuild affected
BVHs even when authoritative bounds stay unchanged. Sector lookup also observes
selected map identity and mutable map ID; equal-size retained-map swaps cannot
reuse stale lookup data. Invalid selections emit no GPU records or upload work.

Queued uploads capture selected map identity and revision. Obsolete work cannot
retarget authoritative geometry or acknowledge newer dirty queues; successful
writes still consume their budget. Source dirty queues remain untouched during
coarse-only display; an explicit full staging request permits its uploads without
changing display. `RenderVoxelObjectReady` checks the exact selected
target; `VoxelObjectReady` retains its authoritative full-target contract. Check
readiness after the current selection passes `UpdateScene` (`RtApp.Update`), before
rendering; these predicates observe queued uploads rather than certify an
unpublished scene selection.

`SetPendingFullUpload` stages the captured authoritative map behind a valid
ordinary coarse selection. It changes neither display nor geometry/dirty flags.
Repeated staging preserves its request generation; cancellation, restart and
representation replacement cannot reuse an unfinished request identity.
`PendingFullUploadMap` and `PendingFullUploadGeneration` fail closed and cancel
staging when representation guards fail. `ClearPendingFullUpload` cancels staging
without changing display; clearing or replacing the representation drops staging.

Existing GPU ownership services both targets: active pins, capacity, structural
preparation, shared upload budgets and sector lookup include pending geometry.
Materials remain per object; culling, records and neighbor adjacency remain
selected-only. Pending normal baking samples full geometry explicitly. Upload
work checks captured request generation as well as map/revision before execution
and acknowledgment. Priority and waiting age precede selected-role tie preference;
shared-map fallback ordering uses the actual upload map ID.

`PendingFullVoxelObjectReady` checks the current staged target after `UpdateScene`.
Readiness never promotes display. The owner may clear the representation only
when full geometry is ready; otherwise valid coarse display remains available.
Cancelling staging makes full GPU geometry inactive under existing retention and
orphan cleanup, while resident object material ownership remains unchanged.

This permanent separation introduces no helper scene objects or parallel
residency service. Private verified proof
publication follows [ordinary asset ownership](../assets/runtime-assets.md#compiled-lod-source-validation).

The bridge's private qualification guards compare actual full and coarse primary
storage against separately owned, immutable baselines created from authenticated
frames. Baseline construction/adoption must deep-copy sectors, packed-brick slices
and bricks and retain them privately; corresponding pointer checks are defensive,
not a general alias certificate. Original identities and revisions alone cannot
certify public mutable maps. Comparisons inspect signed sector keys/coordinates,
masks, packed cardinality/order, occupancy, flags and complete dense payloads.
Nil, GPU-first, structurally malformed and wholly unoccupied maps fail closed.
Bookkeeping, bounds caches, atlas offsets and generated auxiliary data do not
participate; scans never repair storage or consume dirty queues. Semantic primary
validity comes from the authenticated immutable baseline.

Current used material must have a valid nonzero palette index, alpha 255, exactly
zero transparency and transmission, and no animation targeting that value.
Nonfinite and nonzero optical values reject; signed zero qualifies. Unused slots
and non-opacity properties do not affect this gate. Qualify the effective current
material separately from shared geometry. The private instance candidate additionally
requires declared full/coarse IDs, the current effective geometry and resolution,
ordinary component/object lattice metadata, a transform and an existing palette.
Ordinary GPU retention is allowed; shared-terrain ownership is excluded. It reads
already-published maps without hydration, mutation or display activation.

One main-thread sync owns a zero-value qualification context. Exact primary scans
are linear and allocate no storage; the context shares true and false results by
actual-map/baseline pointer pair. It also constructs immutable proof namespace keys
once per sync. Asset membership, current materials, animation targets and instance
tags remain fresh on every call. Primary maps and private proofs must stay stable
within that sync; discard the entire context before the next frame to detect raw
writes that bypass revisions.

The bridge qualifies after applying current material, transform and lattice tags.
A cold eligible instance selects coarse geometry and waits locally for its queued
upload; it stays resident without adding a hidden ECS component. Parent visibility
and local readiness both control `RenderEnabled`. Fine demand stages authoritative
geometry only after coarse readiness. Post-Update observation captures target
pointer, mutable ID, revision and request generation; the next sync requalifies
eligibility and current allocation/material readiness before promotion, preceding
the next GPU update. Observation never changes display inside the update phase.
Coarse demand cancels fine staging. A previously published matching coarse target
may reuse its readiness stamp only after a fresh allocation/material check.

Eligibility loss clears coarse selection and pending staging. A cold full fallback
waits until its queued upload is ready; continuously active initialized full detail
keeps ordinary dirty-upload behavior. Coarse selection resets that initialization
latch, since inactive fine storage may be evicted. Object/fine-target replacement
resets captures and preserves an existing cold fallback wait. Rejected coarse
selection cannot restart a completed full fallback indefinitely. Raw writes still
require ordinary caller-owned dirty propagation for GPU updates; qualification
never repairs raw storage or dirty flags.

Controllers retain renderer target metadata, not verified proofs, asset leases or
ECS pointers. Entity removal, ordinary hidden residency removal and successful
sprite selection release owned representations/staging. Removing intent clears
selection immediately but retains a cold full wait until readiness, then retires
the controller. Animated parts and sibling instance staging remain independent.

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
independent of GPU readiness. Compiled opt-in tickets capture the selected render
map and use selected-target readiness; legacy tickets keep authoritative full-target
readiness. The role is fixed at adoption. Changing an unfinished target cancels its
ticket, while terminal coarse readiness remains latched during later fine staging.
V3 page selection and cross-layer coverage groups
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
occupancy/normals and mixed payloads, including duplicate buffer writes into an
active staging generation. A full sector consumes 64 brick records,
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
