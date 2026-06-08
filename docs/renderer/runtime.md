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
  - creates and synchronizes renderer-side objects, lights, camera state, text, gizmos, particles, sprites, analytic media, skybox inputs, and CA volumes
- `app.App`: `voxelrt/rt/app/`
  - owns WebGPU device and surface lifetime, render pipelines, resize flow, opaque storage output, and pass scheduling
- `gpu.GpuBufferManager`: `voxelrt/rt/gpu/`
  - owns G-buffer textures, WBOIT targets, half-resolution analytic-media targets, half-resolution CA volume targets, shadow maps, Hi-Z, scene buffers, voxel atlas resources, and most cached bind groups
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
| 4 | CA volume simulation and bounds | render graph feature node / CA volume feature | optional | Runs before generic pre-GBuffer-volume compatibility work and before G-buffer. |
| 5 | `FeatureCommandStagePreGBufferVolumes` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned CA volume simulation is skipped by this dispatcher. |
| 6 | G-buffer compute | render graph core node / `GpuBufferManager` | core | Writes G-buffer depth, normal, and material targets from visible voxel scene buffers. The graph node records pipeline, bind-group, and workgroup readiness counters for diagnostics. |
| 7 | Hi-Z generation | render graph core node / `GpuBufferManager` | core | Builds the previous-frame occlusion source used by the next `Update()`. The graph node records pipeline, depth-view, mip-view, bind-group, camera-buffer, and readback readiness counters for diagnostics. |
| 8 | `FeatureCommandStagePostGBuffer` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 9 | shadows | render graph core node / `GpuBufferManager` | core | Builds scheduled directional, spot, and point-light shadow updates. The graph node owns shadow update summary/profiler counters and delegates update scheduling/dispatch to `GpuBufferManager`. |
| 10 | `FeatureCommandStagePreLighting` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 11 | skybox update | render graph feature node / skybox feature | optional | Consumes pending `SkyboxResources` input before light-list and lighting work; the registered pre-update bridge only collects ECS input. |
| 12 | tiled light cull | render graph core node / `GpuBufferManager` | core conditional | Dispatches only when local point or spot lights exist; otherwise clears light-list state. The graph node records readiness counters for diagnostics. |
| 13 | deferred lighting | render graph core node / `GpuBufferManager` | core | Writes the HDR opaque lighting target. The graph node records pipeline, bind-group, and workgroup readiness counters for diagnostics. |
| 14 | `FeatureCommandStagePostLighting` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned CA volumes, astronomical bodies, planet bodies, and analytic media are skipped by this dispatcher. |
| 15 | CA volume render pass | render graph feature node / CA volume feature | optional | Renders or clears half-resolution CA volume targets after lighting and before far-field celestial composition. |
| 16 | astronomical render pass | render graph feature node / astronomical feature | optional | Renders far-field celestial bodies after CA volumes. |
| 17 | planet bodies render pass | render graph feature node / planet body feature | optional | Renders far-body planet surfaces after astronomical bodies. |
| 18 | analytic media render pass | render graph feature node / analytic media feature | optional | Renders or clears the half-resolution analytic-media targets after planet bodies. |
| 19 | debug scene compute | render graph core node | optional | Runs only when renderer debug mode and scene debug overlay are active. |
| 20 | accumulation render pass | render graph core node + feature registry | optional pass shell | Opens when a legacy or graph-owned accumulation contributor exists, or when the previous frame had one, so stale WBOIT contents can be cleared. |
| 21 | `FeaturePassStageAccumulation` | graph-owned in-pass contributors | optional | Current built-in contributors: transparent overlay, sprites, water, far planet rings, debris midfield, and particles. |
| 22 | `FeatureCommandStagePreResolve` | render graph compatibility node / feature registry | optional | Reserved stage; no default feature currently owns required work here. |
| 23 | resolve render pass | render graph core node | core | Composites opaque lighting, WBOIT, analytic media, and CA volume targets to the swapchain. |
| 24 | text overlay | render graph feature node / text feature | optional | First feature-owned graph node migrated out of the post-resolve compatibility stage. |
| 25 | gizmos overlay | render graph feature node / gizmo feature | optional | Feature-owned graph node migrated out of the post-resolve compatibility stage. |
| 26 | `FeatureScreenStagePostResolve` | render graph compatibility node / feature registry | optional | Reserved compatibility slot; graph-owned features are skipped by this dispatcher. |
| 27 | submit, present, readback handoff, frame bookkeeping | `App.Render()` | core | Submits the command buffer, presents, resolves Hi-Z readback, commits volumetric history, records camera state, and advances the frame index. |

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
2. CA volume simulation and bounds passes
3. G-buffer compute pass
4. Hi-Z generation compute pass
5. shadow pass
6. skybox update marker through explicit `feature-skybox-update`
7. deferred lighting compute pass
8. astronomical and planet-body post-lighting passes
9. analytic media half-resolution render pass
   - renders bounded atmosphere/fog media into dedicated half-resolution color and front-depth history/render targets
   - reprojects previous analytic-media history in shader
10. CA volume half-resolution render pass
   - renders CA volumes into dedicated half-resolution color and front-depth targets
11. optional debug compute pass
12. accumulation render pass
   - transparent voxel overlay through graph-owned accumulation contribution
   - particles through graph-owned accumulation contribution
   - sprites through graph-owned accumulation contribution
   - water through graph-owned accumulation contribution
   - far planet rings and debris midfield through graph-owned accumulation contribution
13. resolve render pass
   - composites opaque lighting, WBOIT transparency, half-resolution analytic media, and half-resolution CA volumes
14. post-resolve overlay passes
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
| CA volumes | `feature_ca_volumes.go` | explicit `feature-ca-volumes-sim` and `feature-ca-volumes-render` graph nodes; owns typed `CAVolumeInput` / `CAVolumeFrameInput` application before GPU manager record packing | registered batched bridge system / `CellularVolumeComponent` query and budget adapter | optional volumetric |
| astronomical | `feature_astronomical.go` | explicit `feature-astronomical` graph node after post-lighting compatibility work; owns typed `AstronomicalBodyInput` application before GPU manager record packing | registered batched bridge system / `buildAstronomicalBodyInputs` adapter | optional SpaceSim feature |
| planet bodies | `feature_planet_body.go` | explicit `feature-planet-bodies` graph node after post-lighting compatibility work; owns typed `PlanetBodyInput` / `PlanetBodySurfaceInput` application before GPU manager record packing | registered batched bridge system / `buildPlanetBodyInputs` / `buildPlanetBodySurfacePreloadInputs` adapters | optional SpaceSim feature |
| far planet rings | `feature_far_planet_ring.go` | graph-owned contribution inside `core-accumulation`; owns typed `FarPlanetRingInput` application before GPU manager record packing | registered batched bridge system / `buildFarPlanetRingInputs` adapter | optional SpaceSim feature |
| debris midfield | `feature_debris_midfield.go` | graph-owned contribution inside `core-accumulation`; owns typed `DebrisMidfieldInput` application before GPU manager record packing | registered batched bridge system / `buildDebrisMidfieldInputs` adapter | optional SpaceSim feature |
| analytic media | `feature_analytic_medium.go` | explicit `feature-analytic-media` graph node after post-lighting compatibility work; owns typed `AnalyticMediumInput` application before GPU manager record packing | registered-feature-gated `AnalyticMediumComponent` query through `buildAnalyticMediumInputs` adapter | optional volumetric |
| water | `feature_water.go` | graph-owned contribution inside `core-accumulation`; owns typed `WaterSurfaceInput` / `WaterRippleInput` application before GPU manager record packing | registered-feature-gated `buildWaterSurfaceInputs` adapter | optional surface feature |
| transparency | `feature_transparency.go` | graph-owned contribution inside `core-accumulation` | transparent visible objects derived from core scene/material sync | optional composition feature |
| particles | `feature_particles.go` | explicit `feature-particles-sim` graph node for simulation/spawn; graph-owned contribution inside `core-accumulation` for draw; owns typed `ParticleEmitterInput` / `ParticleFrameInput` application, GPU byte packing, params upload, spawn upload, and bind-group refresh | registered-feature-gated `particlesSync` in `particles_ecs.go` | optional simulation/draw feature |
| sprites | `feature_sprites.go` | graph-owned contribution inside `core-accumulation`; owns typed `SpriteInstanceInput` / `SpriteBatchInput` application and GPU byte packing | registered-feature-gated `spritesSync` in `sprite_ecs.go`; entity-LOD sprite proxies are only produced when the sprite bridge is enabled | optional draw feature |

The current feature config can prevent disabled features from registering app-side feature objects and allocating their pipelines during `Setup()`. `VoxelRtModule.BridgeFeatures` declares the feature and graph-node requirements for optional ECS bridge sync, and defaults cover the built-in bridges. Text, gizmo, analytic-media, CA-volume, water, planet-body, astronomical, far-ring, debris, particle, sprite, and skybox bridge bodies are installed through this registration surface; core voxel-object sync still consults the sprite bridge gate only to decide whether entity-LOD impostor proxies may be emitted as runtime sprites. Features that share `core-accumulation` as their graph node still need feature-name bridge gates because a node-name-only gate would confuse water, sprites, transparency, particles, rings, and debris.

## Bridge Sync Inventory

The remaining broad `voxelRtSystem` bridge is now core-only: it syncs voxel scene objects/materials, camera state, and scene lights. Optional feature bridges are installed through `VoxelRtModule.BridgeFeatures` around the `GPU Batch` and `RT Update` boundaries; sprite feature ownership is still consulted inside core instance sync only to decide whether entity-LOD impostor/dot proxies may become runtime sprites.

| Sync scope | Current owner | Renderer data updated | Future graph migration direction |
| --- | --- | --- | --- |
| `Sync Instances` | `voxelRtSystem` | core scene voxel objects, material tables, sprite-gated LOD impostor proxy selection | centralized core bridge |
| CA presets | registered batched bridge system / CA volume feature bridge | typed `CAVolumeFrameInput` preset-refresh request | renderer app owns CA preset upload before GPU manager packing; moved before `GPU Batch` through CA-volume feature bridge registration |
| `Sync CA` | registered batched bridge system / `CellularVolumeComponent` query | typed `CAVolumeInput` handoff with budgets, pending steps, counters, and CA params | renderer app owns CA volume input application before GPU manager packing; moved before `GPU Batch` through CA-volume feature bridge registration |
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
  - If the occupancy gradient is degenerate, derive a deterministic fallback from the voxel's exposed-face mask so thin symmetric features do not become view-dependent.
  - Use the hit face / ray entry direction only as a last resort when the occupancy-based fallback is still ambiguous.
- Single-voxel-thick features need two-sided direct lighting.
  - When a voxel is exposed on both sides of an axis, keep its normal deterministic, but evaluate direct point and spot lighting as two-sided so planes and rods still react to local lights from either side.
- Normal transforms must be consistent across traversal paths.
  - `XBrickMap` microvoxels, solid-brick fast paths, and `tree64` LOD hits must all use the same object-space-to-world-space normal rule. Use the inverse-transpose-style transform, especially when non-uniform scale is possible.
- Lighting may vary voxel-to-voxel, but color identity should remain voxel-stable.
  - The renderer can show shape through per-voxel lighting, AO, and shadows, but it should not smear voxel colors into gradients across neighboring voxels.

Current implementation notes:

- The live normal-generation path is in `voxelrt/rt/shaders/gbuffer.wgsl`.
- Neighbor-derived normals are baked during voxel upload into the voxel auxiliary sidecar, terrain chunks included across chunk boundaries.
- The sidecar keeps dense occupancy words followed by one packed normal byte per voxel. G-buffer, transparent overlay, and particle collision paths load the baked normal at the hit voxel instead of sampling six neighbors at hit time.
- The degenerate fallback path is occupancy-based and deterministic per voxel; face-entry is only a last resort.
- Degenerate thin voxels carry a two-sided direct-light flag through the G-buffer so deferred and transparent lighting agree on planes and rods.
- Deferred lighting consumes the stored G-buffer normal directly; the albedo/material lookup stays palette-driven.
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
- CA volume color target: `RGBA16Float`
- CA volume front-depth target: `R16Float`
- resolve upsamples both analytic media and CA volumes with depth-aware filtering against full-resolution scene depth

### Other major resources

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

### CA volumes

- simulation and bounds updates run before the main opaque passes
- CA volume rendering happens after deferred lighting in a dedicated half-resolution pass
- CA volumes are composited during resolve rather than through the WBOIT accumulation targets

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

- authored from `WaterSurfaceComponent`
- ECS sync hands typed water surface and ripple input to the renderer app
- rendered as dedicated horizontal surface bodies with visible side walls
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
- transparent-overlay bind groups
- particle, sprite, CA volume, analytic-medium, and resolve pipelines

### Scene-resource growth

When `UpdateScene(...)` recreates buffers, `App.Update()` must rebuild dependent bind groups. Renderer bugs after object-count growth or shadow-capacity growth are usually stale-bind-group issues.

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
- changing half-resolution volumetric or CA resolve inputs without updating resolve bind groups and shader bindings together
- changing analytic-media history or half-resolution target bindings without updating `feature_analytic_medium.go`, `app_medium.go`, `manager_medium.go`, and `resolve_transparency.wgsl` together
- changing voxel payload page bindings, dense-occupancy bindings, hybrid-lookup metadata, or `BrickRecord` layout in one pass but not the other voxel consumers
- changing a shader resource list without updating the corresponding hand-written pipeline layout in `voxelrt/rt/app/`
