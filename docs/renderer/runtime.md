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

## Streaming policy installation

`VoxelRtModule.StreamingConfig` optionally installs a `VoxelRtStreamingConfig`
once after successful renderer initialization, before its engine state is
published. A nil configuration preserves existing manager defaults. `Apply`
copies managed admission, managed frame and sector lookup policies exactly;
enabled zero values retain their independent pause semantics. Optional `Upload`,
`NativeWork` and `NativeAdmission` pointers leave existing policies unchanged
when nil. Later manager policy changes are not reset each frame.

`DefaultVoxelRtStreamingConfig()` returns fresh storage composing the existing
managed admission/frame, bounded lookup and upload defaults. It enables native
work with two creates, 16 MiB of create bytes and 4 MiB of copy bytes per voxel update.
These are configuration defaults, not measured performance targets; existing
sole indivisible oversized-create progress and accounting still apply. The
preset does not choose a physical native admission cap. Games separately opt
ordinary compiled workers into `EnableManagedPreparedAssets`; configuring GPU
service does not change worker preparation eligibility. Bootstrap resources and
raw lookup compatibility work retain their documented exceptions; this preset
does not establish a whole-frame time or aggregate memory bound.

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
3. calls `BufferManager.PrepareManagedGeometryFrame(Scene)` once, using the opt-in managed frame budget
4. runs `Scene.Commit(...)` with frustum culling and optional Hi-Z occlusion
5. updates profiler counters
6. calls `BufferManager.UpdateScene(...)` once for native admission and upload
7. rebuilds dependent bind groups if GPU resources were recreated
8. updates camera uniforms
9. updates analytic-media temporal history inputs and current half-resolution volumetric target selection
10. refreshes text and gizmo buffers

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
32-byte sector slots, assigned brick-range capacity, and actual auxiliary/payload
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
  - `XBrickMap` microvoxels and solid-brick fast paths use the same object-space-to-world-space normal rule. Any future activation of legacy `tree64` LOD must preserve that rule. Use the inverse-transpose-style transform, especially when non-uniform scale is possible.
- Lighting may vary voxel-to-voxel, but color identity should remain voxel-stable.
  - The renderer can show shape through per-voxel lighting, AO, and shadows, but it should not smear voxel colors into gradients across neighboring voxels.

Current implementation notes:

- The live normal decode path is in `voxelrt/rt/shaders/gbuffer.wgsl`; CPU-side bake/upload lives in `voxelrt/rt/gpu/manager_voxel_normals.go`.
- Neighbor-derived normals are baked during voxel upload into the voxel auxiliary sidecar. Objects with voxel adjacency metadata sample adjacent chunks across boundaries; terrain metadata remains a compatibility fallback.
- The CPU sidecar keeps dense occupancy words followed by one 16-bit oct-encoded normal per voxel; GPU storage follows the [auxiliary layout contract](#packed-fitted-normal-storage). G-buffer, transparent overlay, and particle collision paths load the baked normal at the hit voxel instead of sampling six neighbors at hit time.
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

### Scene BVH traversal

Opaque, shadow and transparent scene traversal consume the single-instance-leaf
median tree generated by `bvh.TLASBuilder`. They finish the pending stack rather
than stopping after an arbitrary number of node visits. Near-first child order,
closest-hit/opaque-depth pruning and transparency's existing surface completion
rule remain unchanged. A shallow tree may still require many node visits when
overlapping instance bounds contain no voxel hit.

For `n` leaves, median splits give maximum edge depth `ceil(log2(n))`.
Depth-first traversal retains at most one sibling per ancestor plus the current
node, requiring at most `ceil(log2(n)) + 1` stack entries under either child order.
The `2n - 1` nodes use nonnegative signed 32-bit indices, so representable trees
have at most `2^30` leaves and require at most 31 entries. The existing 64-entry
shader stack therefore needs no dropping guards for generated trees. A future
builder, leaf format or traversal order change must re-establish this bound.

An empty pass publishes the existing all-zero 64-byte root. Shader traversal
pushes the root only if it has a leaf or positive child index, rejecting that
sentinel even when the allocated buffer retains stale trailing nodes. Buffer
capacity is not the live tree size. CPU picking and particle collision enumerate
instances directly; their candidate coverage does not depend on these scene BVHs.
Tree64 and its voxel fallback retain separate traversal limits.

### Sector traversal

`shaders/sector_dda.wgsl` owns sector stepping for opaque, shadow and transparent
voxel traversal. `shaders.go` prefixes this shared fragment to their exported
WGSL sources; pipeline builders must consume those composed sources.
Sector traversal has no fixed visit cap.

The helper accepts a finite, ordered object-space box and clipped world-ray
interval. Transformed directions remain unnormalized, retaining world-distance
`t`. It validates finite inputs, derived endpoint coordinates and signed 32-bit
sector conversion before stepping. Invalid intervals, zero directions and
stationary axes outside the object bounds return an inactive walk.

Integer limits intersect conservative object-sector bounds with the clipped
ray segment's sector-coordinate box. Negative rays starting exactly on a
sector plane begin in the preceding cell. Only nonzero axes participate in
boundary selection; equal times retain the Z, Y, X priority. Boundary times use
the actual direction component and are recomputed from integer coordinates.
Reported intervals remain clipped and nondecreasing.

Each transition advances one signed coordinate toward its terminal limit,
checking that limit before incrementing. A walk visits at most
`1 + (max_x - min_x) + (max_y - min_y) + (max_z - min_z)` cells. Termination thus
depends on integer progress, including zero-length tie cells and repeated
float32 boundary times, rather than an arbitrary counter or floating-time
progress alone. Endpoint boundary cells may produce a zero-length interval.

Nested sector-based brick and voxel walks mask stationary-axis boundary times
to their parent interval's end, preventing zero-axis steps from rewinding the
ray parameter. Their nonzero-axis arithmetic remains unchanged.

This contract corrects sector stepping within the supplied clip. Shared slab
clipping and tree64 traversal retain their numerical behavior and limits.
Float32 coordinate and distance precision still constrain geometry
at large magnitudes; sector completion does not establish full voxel accuracy
there. CPU interaction and particle collision retain their existing paths.

### Inner brick and voxel traversal

Sector-based opaque, shadow and transparent paths stop their inner walks at
both the parent ray interval and the owning local grid boundary. Brick loop
coordinates stay in `[0,4)^3`; voxel loop coordinates stay in `[0,8)^3` before
any body processing or unsigned index conversion. These loops have no fixed
visit counters.

Initialization clamps each coordinate into its grid. Each nonzero-axis step
moves one coordinate by its fixed sign, so a valid walk visits at most
`1 + 3*(4-1) = 10` bricks or `1 + 3*(8-1) = 22` voxels. A stationary axis has
its boundary time masked to the parent interval end; selecting it ends the
walk through the existing time condition. Integer bounds ensure termination
when float32 time increments or the existing epsilon round away, and prevent
out-of-grid coordinates from aliasing other cells' occupancy or material data.

The former counters already exceeded those valid-grid bounds. This change
establishes geometric termination and indexing safety, rather than increasing
ordinary valid path coverage. Existing biases, reciprocal clamps, hit ordering,
normal/material reads and transparency integration retain their arithmetic.
Tiny-direction or extreme-coordinate geometric accuracy, transparency segment
clipping, and Tree64 traversal require separate work.

### Dormant Tree64 representation

Managed object publication writes `ObjectParams.tree64_base = 0xffffffff` for
both admitted and rejected objects (`gpu/manager_scene.go` and
`gpu/manager_material.go`). `VoxelObject.Tree64LOD` has no builder, reader or
upload path in the workspace. `Tree64Buf` remains allocated and bound for the
legacy shader layout, but receives no representation data.

The opaque shader retains a Tree64 branch behind the invalid-base check, so
managed objects use XBrickMap regardless of `LODThreshold`. Shadow scene
traversal explicitly uses XBrickMap; transparent traversal also uses XBrickMap.
The legacy Tree64 loops and their voxel fallback therefore have no effect on
current managed rendering. Their visit caps remain dormant constraints, not
an active performance target.

Activating this representation requires a producer/publication contract first:
origin and coordinate domain, hierarchy bounds, child addressing, material
semantics, readiness and edit invalidation. The legacy shader's modulo-four
addressing and absent origin metadata do not establish support for arbitrary
signed or large object coordinates. Synthetic node buffers alone cannot verify
managed rendering coverage or performance. Retaining these compatibility fields
and bindings does not imply that Tree64 data is published or supported.

### Voxel capacity planning

Before structural preparation, capacity planning walks sectors only for unique
new maps or maps with `StructureDirty`. Existing maps contribute pending range
demand through their dirty sector/brick frontier. Clean allocated maps use
existing allocator capacity. Hidden upload candidates remain eligible. Planning
deduplicates physical sector pointers and simulates contiguous range reservations;
it assigns no slots and changes no maps. Required capacity includes current,
replacement and quarantined ranges, without fixed 2,048-record headroom.

`VoxelCapacityPlanningSectorVisitsLastUpdate` resets each update and counts
sector entries inspected by this planning step. Other scene scans and the work
within one new or dirty map remain.

### Managed generation admission (S1l7)

`GpuBufferManager` owns the explicit CPU admission ledger, extended by S1l14
for retained managed GPU snapshots and lifecycle metadata. Automatic service is
opt-in through the [managed frame budget](#managed-gpu-publication-s1l14). `SetManagedGeometryAdmissionBudget` selects `Enabled`, `MaxInputBytes` and
`MaxCopiedStageBytes`; the zero budget disables admission. The opt-in
`DefaultManagedGeometryAdmissionBudget` enables 128 MiB in each domain. These are
policy defaults, not measured limits. `ManagedGeometryAdmissionBudget` reports
the policy. `ManagedGeometryAdmissionStats` reports `InputBytes`,
`OwnedMetadataBytes`, `ReservedCopiedStageBytes`, `TotalStageBytes`,
`InputPressureBytes`, `StagePressureBytes`, `OwnerCount` and `GenerationCount`.
Pressure is the positive excess above each enabled cap. Owner/generation counts
retain their explicit CPU-admission meaning; byte totals also include retained
S1l14 GPU snapshot and lifecycle ownership.

`AdmitManagedGeometry(object)` lazily captures the current S1l6 producer only when
enabled. It returns a typed result: Disabled, Unavailable, Accepted, Coalesced,
Unchanged, Stale, SourceChanged, Pressure or Overflow. The first input becomes
accepted. A newer generation from the same attachment becomes the sole successor;
later generations replace that successor. Equal latest generations are unchanged;
older generations and wrap from the maximum token to zero are stale. A different
attachment is refused until explicit cancellation. Refusal changes no retained
input or charge. Unavailable producers do not implicitly drop historical inputs.
Admission honors the current policy after the trusted capture callback returns;
disabling from that callback releases the ledger and returns Disabled.

`ManagedGeometryInputs(object)` returns accepted and successor immutable values,
or zero values when absent. `AdvanceManagedGeometryInput(object, expected)`
promotes the successor only when expected matches the accepted source and
generation; absent successors or stale expectations do nothing. This is CPU
ownership bookkeeping, not GPU readiness or publication.
`CancelManagedGeometryInputs(object)` releases both descriptors. Disable clears
the ledger. Lowering enabled caps preserves admitted inputs and reports pressure;
future growth must fit the lowered limits. Latest-equal offers remain unchanged
under pressure; promotion and cancellation can release ownership under pressure.
Enabled zero caps pause new admissions.

Input bytes sum frozen `RetainedBytes`. Copied-stage reservations sum full
`CopyBytes`, the immutable ordinal-tree capacity and one maximum replacement
path (see S1l13), and a fixed 1,024-coordinate journal per generation.
Owned metadata counts the language-level size of intrusive owner nodes and
generation descriptors. `TotalStageBytes` includes owned metadata and copied-stage
reservations; the stage cap applies to that sum. No registry map or guessed map
bucket charge is used. These reservations exist before any sector copies,
tree nodes or journals are allocated. All arithmetic is checked. Replacement
preflight includes accepted, old successor and incoming charges at once, before
releasing the old successor. Caps are global across admitted objects; shared
geometry may be charged conservatively more than once.

Source tokens, producer/engine graphs, caller-retained captures, fixed manager
storage, allocator overhead, current CPU authority/renderer derivatives and all
physical GPU allocations are outside these domains. The ledger requires exclusive
manager access and its explicit CPU owner lookup is linear. S1l13 supplies CPU
reconciliation; [S1l14](#managed-gpu-publication-s1l14) adds opt-in frame service,
GPU snapshot ownership, coherent publication and bounded retirement.

### Managed sector copy service (S1l8)

`ServiceManagedGeometry(object, maxEntries)` explicitly copies at most
`maxEntries` sectors of the accepted input and returns the number copied in this
call. Nil managers, absent owners and nonpositive allowances do no work. Empty
accepted inputs are complete immediately. Each copied entry advances a saved
ordinal through the frozen input's signed lexicographic coordinates. The service
does not capture producers, traverse uncopied geometry, service the successor or
promote generations. Successor admission/coalescing never restarts accepted work.
Lowered admission caps do not prevent service of already reserved inputs.

Each serviced sector installs an immutable ordinal-tree leaf (S1l13). No
generation-sized array, map, sort or journal is allocated. Admission reserves
tree capacity and replacement scratch before service; descriptor cursor and root
storage count as owned metadata. Service consumes that reservation without
changing admission charges. Cancellation, disable and explicit promotion drop
manager ownership of old tree roots without traversing them. A promoted
successor begins with its own empty copy prefix; promotion remains explicit CPU
bookkeeping and does not require completion or imply GPU readiness.

`ManagedGeometryStage(object)` returns an immutable `ManagedGeometryStageView`
and availability flag. Its `Input`, `Len`, `Total`, `CopiedBytes` and `Complete`
methods describe accepted identity, copied count, captured count, sector-output
bytes copied and enumeration completion. `Coord(index)` and `CopySector(index)`
address the copied prefix in source order; invalid indices return zero/nil and
false. The zero view has no input, zero counts/bytes and `Complete() == true`;
the availability flag distinguishes it from an admitted empty generation.
Index inspection follows an ordinal-tree path and is logarithmic, outside the
service allowance. `CopySector` returns an independent mutable copy, never manager-owned
headers, bricks or auxiliary backing. Captured stage views remain unchanged after
further service, producer edits, successor replacement, cancellation, disable or
promotion. Caller-retained views/copies are outside manager ownership charges.

The entry allowance limits sectors copied per explicit call, not total calls per
frame or wall time. Each copy's full auxiliary capacity is already reserved, but
allocation/zeroing can depend on that capacity. Producer capture, inspection,
allocator overhead and Go GC reclamation remain separate costs. Calls require
exclusive manager access. This explicit copy API does not allocate or publish GPU
state. S1l13 supplies reconciliation and [S1l14](#managed-gpu-publication-s1l14)
supplies automatic frame service and GPU publication.

### Managed content work scheduling (S1l9)

The GPU manager owns an explicit accepted-generation coordinate journal and
overflow sweep scheduler. `QueueManagedGeometryContent(object, expected, coord)`
requires `expected` to match the accepted attachment and generation. It returns
`ManagedGeometryContentResult`: Unavailable for absent ownership, Mismatch for
stale identity, Ignored for coordinates outside accepted topology, Queued for a
new coordinate, Coalesced for an already queued coordinate, or SweepScheduled on
overflow. Signed coordinates are filtered by binary search over frozen accepted
coordinates without enumerating the input. Added topology belongs to successors.
Callers supply sector notifications, including fitted-normal halo sectors; this
API does not derive halos. Ordinary managed engine edits now supply them through
the [qualified edit feed](editing.md#managed-edit-notifications-s1l11).

Each generation retains at most 1,024 distinct pending coordinates, in a lazily
allocated fixed array. Duplicate checks scan at most that fixed capacity; no map,
sort, full-input scan or per-coordinate revision storage is created. Admission's
existing journal reservation covers the array. Its pointer, count and sweep state
are descriptor metadata, charged before acceptance. Queueing and servicing do
not change admission charges.

`RequestManagedGeometryContentSweep(object, expected)` returns false for missing
ownership or mismatched identity; otherwise it schedules conservative accepted-
coordinate work. Empty accepted inputs remain without pending work. A first
request starts a sweep at ordinal zero and subsumes the journal. Requests before
any sweep progress coalesce into that sweep. Requests after progress preserve its
cursor and schedule at most one follow-up full sweep. Journal overflow uses this
same path: queued coordinates are subsumed by the upcoming whole pass, or by the
guaranteed follow-up when the active pass has already advanced. New notifications
after the request remain journaled until consumed or subsumed by another request.
Completing a sweep does not discard those late coordinates.

`ManagedGeometryContentStatus(object)` returns a scalar
`ManagedGeometryContentStatus` and availability flag. Fields are `Input`,
`PendingCoordinates`, `SweepPending`, `SweepCursor`, `SweepAgain` and `Pending`.
The input is accepted identity; the cursor is the next accepted ordinal in the
active sweep. Completing a pass starts its requested follow-up at zero, or clears
the sweep and resets its cursor to zero. `Pending` means a journal coordinate or
sweep remains. The absent status is zero. Status reads do not capture producers.

`ServiceManagedGeometryContent(object, expected, maxEntries, visit)` attempts at
most `maxEntries` visits and returns the number attempted, including a failed
visit. Nil callbacks, nonpositive allowances and missing/mismatched ownership do
no work. Sweeps visit signed lexicographic accepted order before journal work;
journal order is unspecified. A true callback result consumes that coordinate
and marks CPU coverage as requiring materializing repair (S1l13).
False stops the call and preserves its coordinate/cursor for retry; a panic also
leaves that visit unconsumed. Callbacks may inspect immutable inputs/status but
must not mutate the manager or producer, admit/cancel/promote generations, or
reenter service. Manager operations require exclusive access.

Successor admission/coalescing leaves accepted work and structural-copy progress
unchanged. Promotion starts the successor with an empty journal/sweep; cancellation
and disable drop old journal ownership without traversing coordinates. Lowered
enabled caps still allow already reserved work. Content scheduling does not copy
or modify S1l8 entries, and retained copied-prefix views stay immutable.

This is notification scheduling, not live-content reconciliation or a readiness
certificate. A drained journal does not establish content coherence or GPU
readiness. [Qualified current-content reads](editing.md#qualified-current-sector-inputs-s1l10)
and the [engine edit/halo feed](editing.md#managed-edit-notifications-s1l11) are
available, along with [current-sector reservation preflight](#managed-current-sector-reservations-s1l12).
S1l13 supplies replacement storage and CPU reconciliation;
[S1l14](#managed-gpu-publication-s1l14) supplies opt-in frame integration and GPU
publication. The allowance bounds attempted coordinates per
explicit call, not total calls per frame, callback cost, wall time or Go GC
reclamation.

### Managed current-sector reservations (S1l12)

`ReserveManagedGeometrySector(object, expected, coord)` explicitly retains one
qualified current-sector input and reserves its independent copy output. Each
accepted generation owns at most one such pending reservation. This is a permanent
ownership/backpressure prerequisite for later replacement service; it does not
copy sectors, replace immutable stage entries or acknowledge content work.

The typed `ManagedGeometrySectorReservationResult` is Disabled for a disabled/nil
manager, Unavailable for missing ownership or failed qualified capture, Mismatch
for an unexpected accepted source/generation or changed accepted descriptor,
Ignored for coordinates outside accepted topology, Stale for a captured generation
older than the existing pending input, Pressure for a cap refusal, Overflow for
unrepresentable charges, and Reserved on success. Identity/topology rejection
happens before the sector reader runs. Enabled zero caps pause new reservations.
There is no full-input capture fallback. Qualified removal is a valid reserved
input with `Present() == false` and zero payload charges.

The global preflight adds the incoming view's `RetainedBytes()` to `InputBytes`,
its `CopyBytes()` to `ReservedCopiedStageBytes`, and the language-level size of a
typed reservation descriptor to `OwnedMetadataBytes`. The descriptor contains
the frozen sector input and its two cached charges. Its generation's pointer
field is already covered by generation metadata admission. `TotalStageBytes`
remains the sum of owned metadata and copied-stage reservations. Owner and
generation counts do not change. All additions are checked, including the total.

Preflight includes the entire old ledger plus the incoming input/output/descriptor
before allocating or retaining the new descriptor. This includes an existing
reservation, even for the same coordinate/publication, and all successors/other
objects. Only successful installation releases the previous reservation's exact
charges. Smaller replacements and tombstones still need this simultaneous peak;
refusal preserves previous ownership. The copy reservation covers only the
existing independent sector-output domain, not a future replacement index.
S1l13 precharges that representation and its transient path at admission.

After the trusted sector reader returns, reservation rechecks the current budget
and the exact accepted descriptor, so cancellation/readmission with equal source
and generation cannot charge a detached owner. Reader-triggered disable returns
Disabled; other accepted-ownership changes return Mismatch. Successor admission
may leave the accepted descriptor unchanged; its new charges participate in the
postcallback preflight. Readers may adjust policy or explicitly cancel, admit or
promote, but must not reenter reservation/release or staged/content service. Reader
panics propagate without outer reservation mutation; explicit callback side effects
remain. Core supplies source, selection, requested-coordinate and accepted-generation
qualification. Pending-generation comparison adds a further rollback guard.

`ManagedGeometrySectorReservation(object)` returns the held frozen sector input
and availability flag without capture; absence is zero/false. It remains readable
after producer edits, replacement, exposure, release or manager cancellation.
Caller-retained values are outside manager charges.
`ReleaseManagedGeometrySectorReservation(object, expected)` releases only a
matching accepted generation's pending reservation and reports whether one existed.
It invokes no provider and works under pressure or invalid live selection.
Cancellation, promotion away from accepted and disable also release pending
ownership. Successor coalescing and lowered enabled caps preserve it.

All calls require exclusive manager access. The temporary reader result is inspected
before preflight, like full-input admission; no sector copy or reservation descriptor
is allocated on refusal. Capture, fixed manager storage, allocator overhead, caller
captures and Go GC reclamation remain outside the ledger's domains. Journal/sweep
state and immutable copied-prefix views remain unchanged. S1l13 provides bounded
CPU replacement service; GPU publication and retirement are provided by [S1l14](#managed-gpu-publication-s1l14); this reservation is neither readiness nor a frame-time bound.

### Managed CPU content reconciliation (S1l13)

This explicit CPU service replaces accepted-sector content without changing its
fixed topology or publishing GPU state. It requires exclusive engine-thread and
manager access. The explicit API remains available; the opt-in [S1l14 frame service](#managed-gpu-publication-s1l14) invokes it before scene commit.

Accepted stages use an immutable midpoint tree over their frozen coordinate
ordinals. A node contains two child pointers, a sector pointer, its output-byte
charge and publication generation. Admission reserves `2*N-1` nodes plus one
maximum path of `ceil(log2(N))+1` nodes; empty topology reserves neither.
`ManagedGeometryStageStorageBytes(N)` returns this checked language-level byte
charge (negative or overflowing counts refuse). Payload and journal reservations
are separate. Each installation clones one path before swapping the root. The
scratch charge covers simultaneous old and incoming paths; historical roots held
by callers, allocator overhead and delayed GC reclamation are outside manager
ownership. The current root retains only current leaves, with no replacement
history or growing overlay.

`ManagedGeometryStageView` retains its root and copied prefix. `Complete` means
initial enumeration only. `CopiedBytes` sums current initialized leaf outputs;
`SectorGeneration(index)` returns a leaf's publication token. A removed sector
is an initialized tombstone: `CopySector` returns nil/true. Invalid indices return
nil/false. Inspection copies preserve brick values and auxiliary nilness, length
and capacity without exposing manager-owned backing. Old views remain frozen.

`SetManagedGeometryProducerWithGenerationReader` installs lazy full, sector and
scalar-generation callbacks under one fresh source identity. Existing setters
remain compatible but do not supply the scalar reader.
`CurrentManagedGeometryGeneration(expected)` invokes only that scalar reader,
checking attachment, ordinary render selection and non-rollback generation before
and after the callback. The engine adapter additionally checks live owner/binding,
asset and completed publication through its existing qualification path. Missing
or failed scalar readers never fall back to full or sector capture.

`ApplyManagedGeometrySectorReservation(object, expected)` consumes a previously
reserved candidate only for an initialized accepted ordinal. It requires the
qualified live generation to equal the candidate and not precede the leaf, and
rechecks exact owner, accepted descriptor and pending-candidate identity after
callbacks. Its result is Disabled, Unavailable, Mismatch, Uncopied, Stale, Overflow
or Applied. Refusal does not copy output, replace a root or acknowledge content.
The incoming output charge transfers from the pending descriptor to the stage;
the old leaf output, pending retained input and descriptor charges then release.
Already reserved replacement service works under lowered enabled caps, including
zero. The original frozen input remains charged. Cancellation, promotion and
disable release the updated stage and any pending candidate. Apply alone does
not acknowledge journal work or certify coherence.

`RecordManagedGeometryContentPublication(object, expected, previous, current)`
is a trusted exhaustive batch marker, called after all changed-sector and normal-
halo notifications. The engine edit feed records the previous and completed
publication versions, including finalized panic prefixes. No-op edits stay silent.
Continuous version edges advance covered publication even when changed topology
lies outside accepted coordinates. Duplicate current markers cannot repair a
coverage gap. Missing edges, rollback and successful legacy callback acknowledgements
require a whole stable materializing sweep before coherence can be certified.
Manual queueing alone makes no exhaustive-history assertion.

`ServiceManagedGeometryReconciliation(object, expected, maxEntries)` shares one
entry allowance across initial enumeration, pending candidates and content work.
It enumerates first, then refreshes/applies pending candidates and services sweeps
before journal entries. The return value counts attempted coordinates, including
failed content attempts. Nonpositive allowances and missing/mismatched ownership
do no work. A refused copy leaves the coordinate and cursor pending for retry.
With valid publication history, fresh leaves at the exact qualified generation
can be acknowledged without another copy. Repair sweeps materialize every ordinal;
only a retry already materialized by that pass may reuse its current leaf. New
captures still obey simultaneous old-plus-incoming preflight. Trusted readers may change policy or lifecycle ownership, but must not reenter reservation,
release, notification, publication-recording or reconciliation operations.
Postcallback identity checks prevent acknowledgements on detached owners.

Unknown publication history triggers a bounded full accepted-topology sweep.
Generation changes and late notifications preserve its cursor and require at most
one follow-up pass; they do not restart progress. Only a whole pass performed by
this materializing service at a stable qualified generation repairs missing
history. Legacy callback progress cannot form that certificate. Empty accepted
topology may be repaired by stable scalar qualification with positive allowance,
without consuming an entry. Repeated edits can delay coherence indefinitely.
The manager retains a scalar high-water token from successful qualified reads
and recorded publications, including status reads. A lower token is unavailable;
status may advance this scalar or invalidate certification after rollback or
unavailable qualification, but never schedules or copies work. Recovery to the
high-water token still requires a stable materializing sweep. A generation wrap requires a fresh
admitted identity.

`ManagedGeometryReconciliationStatus(object)` returns accepted `Input`, `Copied`,
`Total`, `Pending`, `Qualified`, `Generation`, `Coherent` and `RepairRequired`, plus
availability. `Pending` includes incomplete enumeration, journal/sweep/candidate
work and required repair. The getter performs scalar qualification only and
rechecks ownership after the callback. Coherent requires complete enumeration,
no journal/sweep/candidate, valid exhaustive coverage or completed repair, and covered version equal to the
qualified live version. This certifies CPU content for accepted topology only;
it says nothing about successor topology, GPU readiness or frame-time limits.

### Managed GPU publication (S1l14)

The automatic ordinary-managed consumer provides: hidden structural staging,
coherent display publication, continued current content and bounded retirement.
CPU authority and producer identity stay attached to `VoxelObject.XBrickMap`.
A separate managed render selection uses unit-scale transforms and conservative
finite-topology bounds. It does not reuse the LOD2 representation or implicitly
follow authority revision changes.

`ManagedGeometryFrameBudget` has `Enabled` and `MaxEntries` (default helper: true,
16); zero value leaves automatic service off. Creation also requires the existing
CPU admission budget. `PrepareManagedGeometryFrame(scene)` runs before scene
commit, sharing the entry allowance across CPU enumeration/reconciliation,
managed GPU coordinate preparation and retirement. Existing native admission,
growth and uploads run once in the normal post-commit update. A completed upload
can promote only on the next pre-commit service, after live source, CPU coverage,
GPU snapshot coverage and material readiness revalidation. Lookup publication
remains atomic; [S1m](#bounded-sector-lookup-publication-s1m) optionally spreads
its preparation across frames. Precharged coordinate/bounds backing allocation
and zeroing can scale with finite topology; entry limits do not establish a wall-clock frame bound. Initial objects stay unready until complete publication;
replacement refusal preserves current coverage.

`ManagedGeometryView.SameTopology` compares immutable coordinate-frontier identity
conservatively; payload/halo-only edits preserve that identity. Automatic admission
uses it to avoid staging an entire structural successor for ordinary content edits.
Independent nonempty frontiers conservatively differ even when coordinates match;
empty frontiers share the nil identity. Coordinate-only index storage remains
outside the existing `RetainedBytes` domain. Existing explicit CPU admission
semantics remain unchanged.

Manager-owned private maps use the existing `Allocations` ownership inventory;
allocation presence does not make a map reachable by selected object records.
Immutable CPU-stage sector payloads may be borrowed because upload execution and
normal baking do not modify them. Private map dirty bookkeeping is independent.
Complete topology and occupied bake bounds must exist before normal baking.
Display bounds stay conservative and separate: a precharged balanced per-coordinate
bounds aggregate caches occupied minima/maxima for the private sampling map.
Each prepared/replaced sector uses the existing occupancy-AABB algorithm locally;
updating its aggregate path takes logarithmic work without scanning other sectors.
Tombstones remove their bounds, including former extrema. Current candidates borrow
the current desired aggregate; structural targets own it until retirement. Existing
normal algorithms and current local halo invalidation stay unchanged, including
cached distant tie-breaks when a content edit changes global bounds outside their
halo. Hidden structural staging separately qualifies its uploaded normal context:
when stable occupied bounds change, its previously prepared prefix is revalidated
with bounded fresh snapshot copies and uploads. CPU enumeration progress survives;
repeated changes to these bake inputs can delay GPU publication. Replacement
preflight and submission-fenced retirement also cover those rebake copies. Managed coordinates bypass ordinary whole-map structural
preparation and cleanup. Admission considers their bounded pending frontier,
without whole-generation enumeration during each coordinate operation.

Current and staging ownership are independent. Current owns a finite topology,
its own 1,024-coordinate journal and cursor-preserving overflow/repair sweep.
The qualified engine edit feed notifies current and accepted staging topology,
including normal halos. Added coordinates belong to the structural successor.
Current content candidates use a hidden upload target over the complete desired
current topology. The selected allocation retains committed coordinate snapshots
until a complete replacement/removal transaction succeeds. Successful uploads
transfer captured ownership to current, invalidate lookup/shadow dependencies and
retire old edges; unsuccessful or obsolete work cannot consume notifications or
make a private candidate visible. Materials continue through existing per-object
ownership. Dirty emptiness alone is not a GPU coverage certificate.

Reserve retained inputs, borrowed desired payloads, uploaded snapshots and typed
GPU lifecycle metadata independently of CPU staging. Their charges participate
in the global input/copied-stage caps, including simultaneous old-plus-incoming
replacement peaks. Charge language-level descriptors, coordinate records,
allocation pointer arrays, occupied-bounds nodes and conservative
sector/brick/auxiliary-capacity domains.
Shared pointers may be charged more than once. Go map buckets and allocator/GC
overhead are explicitly outside these domains; no total heap ceiling is claimed.
Each retained charge survives CPU replacement, promotion, cancellation and disable
until its last managed owner releases it. Physical resource admission remains
under the existing GPU budget.

Retirement drops one captured coordinate's snapshot edges per entry, preserving
other current/staging/retiring references and existing submission-fenced range
reuse. Ordinary eviction must not release a managed map wholesale. Disable,
exposure, removal or source replacement cancels obsolete private work; an ordinary
compatibility handoff establishes replacement coverage before clearing managed
display. Unsupported lattice/LOD owners retain their existing owner rules.
Budget disable must not erase charges for still-owned snapshots. A zero enabled
entry allowance pauses service; opt-out handoff and retirement retain the last
nonzero/default drain allowance.

`ManagedGeometryFrameStats()` reports attempted entries and pending work.
`ManagedGeometryGPUStatus(object)` reports current/staging identity, publication
versions, readiness and retiring work without producer capture.
`NotifyManagedGeometryGPUContent(object, previous, current, writes)` supplies the
qualified complete edit/halo batch independently of CPU accepted ownership.
Core `SetManagedRenderGeometry(expected, target, minimum, maximum)` and
`ClearManagedRenderGeometry(expected)` establish/clear only the matching managed
selection; a nil selected target represents unready initial geometry.
`RenderLocalBounds()` supplies explicit managed display bounds to instance records
and shadow dependency keys, while private-map `ComputeAABB()` supplies occupied
bake bounds. Ordinary and LOD selections retain their existing local bounds.
CPU picking, collision, saves and producer reads continue to use authority.

<a id="streamed-ordinary-worker-integration-s1n"></a>

### Streamed ordinary worker integration (S1n/S1p)

Qualified cold and unborrowed verified warm compiled parts can supply managed
authority and the first renderer map from workers, using the existing managed
generation callbacks.
The [asset ownership contract](../assets/runtime-assets.md#worker-prepared-ordinary-managed-assets-s1n)
defines opt-in, permanent raw-borrow revocation, leases and hook ownership
transitions.
This integration changes no shader layouts or GPU budget defaults. Managed
structural promotion still requires complete content, materials and exact
committed lookup coverage; CPU authority remains independent of displayed stages.

### Bounded sector lookup publication (S1m)

`GpuBufferManager` owns optional lookup preparation through
`SetSectorLookupFrameBudget`, `SectorLookupFrameBudget` and
`SectorLookupFrameStats`. The zero policy preserves the initial legacy path.
`DefaultSectorLookupFrameBudget()` enables 1,024 entries, 64 KiB uploads and
128 MiB retained staging cap; these are unmeasured opt-in defaults. Service runs
once in the existing post-commit scene update, independently of managed geometry
service. `Compatibility`, `AttemptedEntries`, `UploadedBytes`, `Pending`,
`StageBytes`, `CurrentGeneration` and `RetiringEntries` expose its progress.

Current, preparing and retiring generations occupy fixed slots. An admitted
finite snapshot finishes despite newer edits; the successor coalesces those
edits and waits for cleanup. Private mutation hooks maintain persistent coordinate
and inverse-reference inventories. Capturing their roots takes constant work per
map, without cloning whole sector dictionaries. Captured roots immediately pin
unvisited sectors and bricks; selected references remain pinned through current
publication and retirement. Enumeration, captured-reference cleanup, hash-cell
initialization and each collision probe, direct-table initialization/fill, upload
chunks and retirement share the global entry allowance across maps. A sector
entry touches at most 64 brick references plus logarithmic inventory work.
Uploads share the byte allowance across all three tables in aligned chunks;
allowances below four bytes pause writes. Enabled zero entries pause bounded
capture, native creation, upload and retirement; zero upload bytes still allow
CPU preparation.

The three lookup buffers publish together with committed map identity, counts,
direct metadata, exact inventory root and managed generation. Object records and
readiness use that committed descriptor. Initial coverage stays hidden; refusal
or deferred work preserves the previous displayed coverage. Uploaded geometry
alone cannot certify structural readiness. Changed maps invalidate their scoped
shadow dependency keys at publication. Shader layouts and opaque, shadow,
transparent and particle-collision binding consumers remain unchanged.

`MaxStageBytes` preflights captured inventory nodes, retained sector/brick input
payloads and auxiliary capacities, selected-reference storage, descriptors and
CPU tables. Shared inputs can be conservatively charged more than once. Charges
survive budget reductions and remain until bounded cleanup releases their owners.
Permanent live inventories and published current CPU generations and inputs are
separate domains outside this cap; it is not an aggregate retained-memory or
process-heap ceiling. Native old, candidate and retiring buffers participate in
existing physical GPU admission. Native creation also shares existing creation
limits. Lookup creation waits for voxel growth; owned lookup destinations defer
new growth while fitting content work can continue.

Submitted lookup references wait for their exact last-use completion before
bounded release. Buffers retain their allocating backend's completion/release
callbacks and existing bind-group lifetime guards. Removal, disable or invalid
inputs abandon obsolete stages safely, releasing unused buffers and fencing used
ones. Disable retains safe ownership until compatibility handoff and retirement
finish. A hash requiring 128 probes refuses publication, reports pending work
and preserves current coverage; changed inputs allow a later successor.

Changed raw topology/identity or edge transitions and untracked public allocation
headers use an explicit `Compatibility` exception to lookup CPU, upload and stage
limits. Unchanged trusted raw objects can coexist with bounded managed service.
Native creation limits, physical admission, fences and fixed-slot backpressure
still apply, so compatibility does not promise immediate GPU publication.

Scene target enumeration still scales with object count. Backing-array allocation
and zeroing and native creates remain indivisible. Entry and byte limits establish
neither elapsed frame-time nor FPS guarantees. See the
[verification scope](verification.md#bounded-sector-lookup-native-regression).

### Auxiliary capacity admission

Auxiliary occupancy/normal storage is independent of sector brick-range capacity.
All nonnil brick modes, including solid bricks, require one
1,088-byte sidecar per distinct brick pointer. Admission inventories new and
structurally changed maps plus the dirty sector/brick frontier of existing maps,
including pending full targets. Clean resident sectors require no auxiliary walk.
Tracked mutations must maintain the existing dirty queues.

The planner preserves allocator high-water capacity and current free slots.
Fresh pointers shared by pending upload units reserve one global slot. A fresh
pointer exclusive to one complete unit can use a release credit from that unit
only when no future target still needs the old pointer. Surplus release credits
never fund another deferred upload. Structural credits are global only when
admitted structural preparation will remove the final committed reference before
content service. Candidate refusal rolls back demand and credits together.
Retained and shared snapshots keep their slots pinned.

Execution checks physical auxiliary capacity again before writing a complete
unit. Late demand that does not fit remains dirty until a later admission pass;
it cannot allocate or acknowledge an out-of-range sidecar. Existing safe release
and resurrection behavior remains intact. Growth uses aligned geometric capacity
without fixed 2,048-row auxiliary headroom, through the existing atomic migration
and bind-group recreation path. Released slots do not shrink buffers.

Conservative reservations may defer alias-heavy replacements until additional
capacity or final-reference releases become available. Unsupported public
allocation headers retain conservative compatibility handling. The brick record
layout, auxiliary word offsets and fitted normals are unchanged.

### Sparse sector publication

Manager-owned full-sector uploads write current occupied brick records. Packed
holes have no physical record. Legacy dense sectors retain explicit clears of
previously committed records and conservative full-block compatibility uploads.

Upload service captures sector membership before execution. Selected records,
payloads and auxiliary bytes queue before that captured sector header. A topology
edit during execution remains dirty for a later unit; it cannot expose a skipped
record through a newer header mask.
A newly assigned sector slot stays absent from hash/direct lookups until its
first complete header queues successfully. Reused slots can retain old physical
bytes while deferred; those bytes remain unreachable. First publication changes
the lookup revision, so cached lookup buffers rebuild. Existing published sectors
remain readable while replacement content is deferred.

Upload service rechecks the full-sector record set and byte cost immediately
before execution. Late tracked demand cannot skip a new record or exceed the remaining
frame budget. Brick counts describe logical selected records; migration mirrors
double the affected buffer bytes, not record counts. Header-only empty-sector
work consumes no brick records but still consumes sector/header budgets. Dirty
completion, shared ownership and auxiliary/payload capacity guards remain in the
same service boundary.

### Packed sector brick ranges

One physical `Sector` owns a committed brick range and mask, shared by all map
allocations referencing it. Occupied records are contiguous in ascending local
brick-index order. Capacity rounds to `0, 1, 2, 4, 8, 16, 32, 64` records; empty
sectors need no range. A unified record-coordinate allocator splits and coalesces
free intervals. Class padding is reserved capacity, never an uploaded live row.

The 32-byte sector header stores base at byte 16, mask words at bytes 20/24 and
layout at byte 28: zero denotes legacy dense indexing and one denotes packed
indexing. All four voxel consumers reject absent mask bits before reading
`base + popcount(mask below local index)` for packed sectors. Brick records,
material addressing, payload textures and auxiliary encoding remain unchanged.

Every membership change allocates a replacement range, including same-class
changes and shrinking. Service captures the complete occupied set, writes its
records first, then publishes the captured base/mask/layout together. A stable
membership edit reuses its range; a dirty-brick membership mismatch promotes to
a complete sector transaction and obeys sector, record and byte budgets. Denied
capacity or content budgets preserve the old publication. New pending sectors
receive no brick range until content service admits their first complete upload.

Admission reserves virtual offsets, not assigned ranges. Service claims those
exact spans so differing admission and content priorities cannot fragment an
otherwise feasible plan. Deferred content leaves its real range unassigned;
late growth can use spare capacity only after excluding other pending spans.
A completed free suffix can join unused tail capacity without buffer growth.

Publication synchronizes committed receipts of indexed owners sharing the
physical sector and invalidates their retention accounting and shadow epochs.
These receipts describe captured written pointers, including edits during
execution, rather than newer live CPU topology.

Replaced and final-owner ranges remain quarantined until
`MarkRetiredBuffersSubmitted` stamps an actual submission and its completion is
observed through `AdvanceRetiredBuffers`. Frame aging cannot release an unfenced
range. Admission cannot borrow quarantined capacity; it pays simultaneous old/new
ranges. Quarantine is physical capacity, excluded from assigned-map retention
charges. Released intervals can be reused without shrinking buffers.

Before the first nonempty managed lease, the allocator reserves the existing
legacy dense address prefix, including public `BrickAlloc` demand. Existing dense headers stay
layout zero. Ownership bookkeeping fallback cannot reinterpret an established
packed sector as dense. Arbitrary subsequent derived GPU allocation/allocator
mutations remain unsupported producers; conflicting legacy address growth must
fail closed rather than overlap managed ranges.

### Packed fitted-normal storage

Dense GPU normals remain the default. `GpuBufferManager.SetPackedVoxelNormals(true)`
opts in before geometry or auxiliary allocation and staged growth. Same-value
calls are idempotent; changing modes after allocation, including after release,
is rejected. CPU sidecars and authored `PrecomputedAux` remain 1,088 bytes.

BrickRecord byte 28 contains auxiliary layout bits: bit 0 selects packed
normals and bit 1 selects [packed mixed materials](#packed-mixed-material-storage).
With bit 0 set, the first sixteen occupancy words precede two original 16-bit normal
lanes per word, in ascending occupied voxel order (`x + y*8 + z*64`). This normal
prefix occupies `16 + ceil(occupied/2)` words, with a zero odd padding lane;
optional mixed-material lanes follow it. Valid
1,088-byte precomputed data is authoritative, including its occupancy header;
otherwise the existing normal baker supplies the dense source. Invalid-sized
precomputed slices are ignored. Encoding, validity and two-sided bits are copied
exactly, including invalid raw values. This selector is independent of the sector
header's byte-28 brick-addressing selector.

G-buffer, transparency and particle normal loads rank occupied voxels against
these occupancy words. Packed empty-voxel normal loads return zero; dense raw loads
retain their previous behavior. Shadow traversal uses the unchanged occupancy
header. Material tables and bindings remain unchanged. With material packing
disabled, mixed material reads continue to use the payload atlas.

Either packing policy uses the following packet ownership and publication rules.
A live packet belongs to a physical sector/local brick index. Shared physical
sectors share it; sharing a CPU brick between distinct sectors does not share
packets. Every actual packet upload replaces its packet, including
identical uploads in later updates and normal-only halos. Replaced, removed and
final-owner packets remain quarantined until an actual queue submission is
stamped and completed. Admission pays simultaneous old/new capacity, and retained
map accounting charges committed packet words rather than fixed dense slots.

Admission reserves exact virtual word spans without assigning deferred packets.
Each service work item captures at most 64 rows, including raw fields and
valid precomputed bytes, and bakes its packets before native writes. Actual byte
costs include migration mirrors and are checked with physical capacity and all
unit claims before execution. Each packet precedes its brick record; all records precede
the sector header. Source changes during execution leave dirty work pending;
neighbor inputs retain the existing revision/normal-halo ownership contract.
Late growth protects other pending spans before claiming spare space.

Within one update, identical shared-sector work can acknowledge an already
written packet without consuming a second physical unit budget. Equivalence
uses the latest successful physical publication, captured source/packet bytes,
topology and allocation identity; an A→B→A sequence therefore writes A again.
Target revision, pending generation and source checks still guard acknowledgement.
Existing public dense auxiliary slots keep a reserved legacy prefix; conflicting
later prefix growth fails closed. Packed allocation uses independent word
coordinates, with no change to the legacy dense slot API.

### Packed mixed-material storage

`GpuBufferManager.SetPackedVoxelMaterials(true)` independently selects packed
mixed-material storage before geometry, auxiliary allocation or staged growth.
Same-value calls are idempotent; later policy changes, including after release,
are rejected. Atlas materials and dense normals remain the defaults. CPU bricks,
authored sidecars and 256-entry local palette addressing are unchanged.

A mixed brick appends one original material byte per occupied voxel to its
auxiliary packet, in ascending canonical occupancy rank. Four bytes share each
little-endian `u32`; unused final lanes are zero. Occupancy comes from the valid
authored auxiliary header or the existing baker, including authored occupancy
that differs from raw payload. Material bytes come from captured `VoxelValue`
cells, including IDs 0 and 255. Uniform and solid bricks retain their scalar
material and normal sidecar, with no material suffix or material layout bit.

For `n` occupied voxels, mixed packets contain `272 + ceil(n/4)` words with dense
normals, or `16 + ceil(n/2) + ceil(n/4)` words with packed normals. Byte 28 is
therefore 0, 1, 2 or 3. A packed mixed record publishes its absolute material word
base in `payload_offset`; `payload_page` is zero and unused. G-buffer,
transparency and shadow material accessors rank against the packet occupancy,
extract the selected eight-bit lane, and return zero for empty or out-of-range
mixed voxel indices. Particle collisions consume occupancy and normals only.

The existing bound auxiliary pool owns the complete packet under the preceding
[fenced publication rules](#packed-fitted-normal-storage). Admission charges the
material suffix and simultaneous old/new packets, actual uploads include
migration mirrors, and retained accounting charges committed packet words.
Packed mixed uploads require neither atlas slots nor texture writes. The pool
is the first-page prototype: existing buffer/storage binding limits and 32-bit
word indices bound admission; unavailable capacity defers the complete unit.
No additional shader binding is required.

Atlas textures and bindings remain allocated for the default path and shared
pipeline layouts. Avoided assigned payload bytes are not physical atlas savings.
Declared texture capacity does not measure resident GPU memory.
Use [paired workload measurements](verification.md#packed-normal-workload-benchmark)
before choosing a policy; this prototype does not select a density threshold or
remove the atlas.

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
active staging generation. Packed full sectors consume occupied records;
legacy dense sectors retain the 64-record compatibility fallback.
Allocation/migration copies, lookup rebuilding, scene
buffers and CPU queue/normal-halo preparation remain outside this cap.

Shared maps upload geometry once, using their best instance priority/order.
Material attachments remain per object and certified immutable tables share
physical blocks. Work sorts by priority, order, map ID, kind and
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

Voxel payload uploads follow the same rule. `BrickRecord` remains 32 bytes with format-aware payload addressing:

- `material_index`
  - used by `Solid` and `UniformMaterial` bricks
- `payload_offset`
  - packed 3D atlas offset, or absolute material word base when auxiliary bit 1 is set
- `occupancy_mask_lo` / `occupancy_mask_hi`
  - coarse `2x2x2` microblock occupancy mask
- `payload_page`
  - payload atlas page; zero and unused for packed material records
- `flags`
  - includes `BrickFlagSolid` and `BrickFlagUniformMaterial`
- `voxel_aux_word_base`
  - exact `8x8x8` occupancy, fitted-normal and optional mixed-material packet pointer
- `auxiliary_layout` (byte 28)
  - bit 0 packs normals; bit 1 packs mixed materials; see [storage contract](#packed-fitted-normal-storage)

The live brick-mode contract is:

- `Solid`
  - whole brick occupied
  - reads `material_index`
  - does not allocate payload atlas storage
  - retains its occupancy/normal sidecar
- `UniformMaterial`
  - sparse occupancy
  - reads `material_index`
  - retains its occupancy/normal sidecar
  - does not allocate payload atlas storage
- payload-backed sparse
  - sparse occupancy
  - uses the atlas by default or occupancy-ranked material lanes when opted in
  - retains its occupancy/normal sidecar
  - assigns atlas slots only with material packing disabled

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
