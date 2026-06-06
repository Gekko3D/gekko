# VoxelRT Render Graph Migration Plan

This document defines the long-term plan for moving VoxelRT from a feature-stage renderer toward a small native render graph.

The goal is not to copy Bevy's render app. The goal is to make the live renderer easier to extend, easier to configure per game, and cheaper when optional features are disabled.

## Alignment Gate

Known from the current docs and code:

- VoxelRT already has a working feature-stage architecture under `voxelrt/rt/app/feature*.go`.
- `App.Render()` still owns the central frame sequence in `voxelrt/rt/app/app_frame.go`.
- `VoxelRtModule.FeatureConfig` already lets games enable or disable broad feature groups before renderer initialization.
- `mod_voxelrt_client_systems.go` still acts as a wide ECS-to-renderer bridge for many optional systems.
- Existing renderer docs say the stable core should remain window/device lifetime, scene upload, camera, G-buffer, lighting, and resolve.

Uncertain or still to decide:

- The exact graph granularity: one node per pass, one node per feature, or a small mix.
- How quickly feature-owned ECS extraction should replace the current monolithic bridge sync.
- Which resources should move from `App` fields into node-owned or feature-owned structs.
- Whether custom game render features need public graph APIs immediately or only after built-in features migrate.

This plan is a long-term architecture step. It is not a tactical performance patch and not a throwaway diagnostic.

## Recommended Direction

Build a small gekko-native render graph and migrate the existing feature-stage system into it incrementally.

Do not introduce a backend abstraction layer. Do not replace the WebGPU renderer. Do not change voxel scene, material, or ECS semantics as part of the graph migration unless a specific node boundary requires a small supporting change.

The graph should provide:

- explicit pass ordering
- explicit optional node registration
- feature-owned setup, resize, update, render, and invalidation hooks
- per-game feature presets that avoid registering unused nodes
- a path to feature-owned ECS extraction so disabled features do not run bridge sync

## Non-Goals

- Rewriting the opaque voxel renderer.
- Replacing `GpuBufferManager` in the first graph pass.
- Porting Bevy's `RenderApp`, extract schedule, or render graph APIs directly.
- Moving all resources out of `App` in one sweep.
- Changing the rendered look while migrating scheduler architecture.

## Target Architecture

The final renderer should have three layers:

1. Core renderer
   - window, device, surface, queue
   - swapchain acquire/present
   - camera and frame context
   - scene upload
   - core targets and core bind groups
   - graph compile/run
2. Render graph
   - ordered nodes
   - dependency validation
   - lifecycle dispatch
   - frame target access
   - profiler scope naming
3. Render features
   - optional bridge extraction
   - optional GPU resources and pipelines
   - one or more graph nodes
   - feature-local rebuild/invalidation logic

Conceptual API:

```go
type RenderNode interface {
    Name() string
    Enabled(*App) bool
    Setup(*App) error
    Resize(*App, uint32, uint32) error
    OnSceneBuffersRecreated(*App) error
    Update(*App) error
    Record(*App, *wgpu.CommandEncoder, *FrameContext) error
    Shutdown(*App)
}

type RenderNodeSpec struct {
    Name  string
    After []string
    Node  RenderNode
}

type FrameContext struct {
    Width uint32
    Height uint32
    SwapchainView *wgpu.TextureView
    WorkgroupsX uint32
    WorkgroupsY uint32
}
```

The concrete API may differ, but it should keep dependencies explicit and keep node execution simple enough to debug.

## Core Nodes

Core nodes are always present unless the renderer is running a specialized test mode.

| Node | Owner | Notes |
| --- | --- | --- |
| `g-buffer` | core | Runs voxel ray/G-buffer compute; currently graph-routed with readiness diagnostics. |
| `hiz` | core | Builds Hi-Z from G-buffer depth; currently graph-routed with readiness diagnostics. |
| `shadows` | core | Builds scheduled shadow updates; currently graph-routed with update summary and readiness diagnostics. |
| `tiled-light-cull` | core | May no-op when no local lights exist; currently graph-routed with readiness diagnostics. |
| `lighting` | core | Writes the HDR opaque lighting target; currently graph-routed with readiness diagnostics. |
| `debug-scene` | core/debug | Optional debug compute path; currently graph-routed. |
| `accumulation` | core | Owns WBOIT accumulation target clear and in-pass feature draws; currently graph-routed. |
| `resolve` | core | Composites opaque, WBOIT, and half-res volumetrics to the swapchain; currently graph-routed. |

The graph should first preserve the current order:

```text
pre-gbuffer feature work
pre-gbuffer volume work
g-buffer
hiz
post-gbuffer feature work
shadows
pre-lighting feature work
tiled-light-cull
lighting
post-lighting feature work
debug-scene
accumulation
pre-resolve feature work
resolve
post-resolve feature work
submit/present
```

Then it can replace feature-stage blocks with explicit nodes.

## Optional Feature Nodes

Feature nodes should only exist when the feature is enabled. Disabled features should not create pipelines, allocate feature resources, sync feature ECS data, update feature GPU buffers, or record render work.

| Feature | Likely Nodes | Notes |
| --- | --- | --- |
| particles | `particles-sim`, `particles-accumulation` | Keep GPU simulation separate from draw contribution. |
| CA volumes | `ca-sim`, `ca-bounds`, `ca-render` | Half-resolution render after lighting; resolve consumes output. |
| analytic media | `analytic-media-render` | Half-resolution temporal path after lighting. |
| planet bodies | `planet-bodies` | SpaceSim feature; should not exist in action-oriented presets. |
| astronomical | `astronomical` | SpaceSim feature; also owns far rings/debris until split further. |
| far planet rings | `far-planet-rings` | May remain behind astronomical preset initially. |
| debris midfield | `debris-midfield` | SpaceSim feature; planned debris-band work can become a sibling node. |
| water | `water-accumulation` | Accumulation-pass participant. |
| transparency | `transparent-overlay` | Accumulation-pass participant. |
| sprites | `sprites-accumulation` | Accumulation-pass participant. |
| text | `text-overlay` | Post-resolve overlay; currently graph-routed as `feature-text-overlay`. |
| gizmos | `gizmos-overlay` | Post-resolve overlay; currently graph-routed as `feature-gizmos-overlay`. |
| skybox | `feature-skybox-update` | Renderer-side `SkyboxLayerInput` handoff exists; bridge collects ECS input before graph update through a tested adapter and the graph node packs/applies it. |

## ECS Bridge Direction

The graph migration must include bridge ownership or it will only decompose GPU pass recording.

Target bridge model:

```text
VoxelRtModule
  installs core renderer bridge
  installs enabled feature bridge systems
  initializes render graph with core nodes and enabled feature nodes
```

Core bridge remains centralized at first:

- camera
- core voxel instances
- materials and voxel payload upload
- lights
- CPU scene object identity and picking/edit support

Feature bridge systems move out of the monolithic sync path over time:

- particles
- sprites
- water
- CA volumes
- analytic media
- astronomical visuals
- planet bodies
- far rings and debris
- text and gizmos if useful

During migration, feature sync can still call existing helper functions. The important long-term boundary is that disabled features do not install or run their sync path.

## Feature Presets

Add named presets after graph registration is stable enough to benefit from them:

- `VoxelRtFeaturePresetDefault()`
  - preserves current behavior.
- `VoxelRtFeaturePresetActionGame()`
  - core voxels, lighting, shadows, text/gizmos as needed.
  - disables celestial, planet-body, far-ring, debris-midfield, analytic-media, water, CA, particles, or sprites unless the game opts back in.
- `VoxelRtFeaturePresetSpaceSim()`
  - enables celestial and large-scale visual features.
- `VoxelRtFeaturePresetMinimal()`
  - core opaque voxel rendering plus resolve only.

Feature presets are not a replacement for graph nodes. They are the public game-facing control surface for node registration.

## Migration Phases

### Phase 0: Baseline Inventory

Goal:

- Freeze the current pass order, feature ownership, and resource rebuild triggers before changing scheduler code.

Tasks:

- Update `runtime.md` with the live feature-stage order and mark core vs optional work.
- Add a table in `runtime.md` that maps feature sync blocks to their current helper code in `mod_voxelrt_client_systems.go`.
- Identify which features currently allocate pipelines during `Setup()`.
- Identify empty-pass cases where a feature opens a pass with no contribution.

Acceptance criteria:

- Every live render pass has an owner, dependencies, and rebuild trigger.
- Every optional bridge sync block has a feature owner.

### Phase 1: Graph Infrastructure Without Behavior Change

Goal:

- Add graph data structures while preserving the current frame order exactly.

Tasks:

- Add `render_graph.go`, `render_node.go`, and `frame_context.go` under `voxelrt/rt/app`.
- Add graph registration and validation tests.
- Add a compatibility node type that can wrap existing feature-stage dispatch.
- Keep `App.Render()` visually and behaviorally equivalent.

Acceptance criteria:

- Existing visuals are unchanged.
- Graph validation catches duplicate node names and missing dependencies.
- Existing feature tests pass.

Current status:

- `render_graph.go`, `render_node.go`, and `frame_context.go` exist under `voxelrt/rt/app`.
- `App` initializes a default render graph that declares the live frame order.
- `App` forwards setup, resize, scene-buffer recreation, per-frame update, record, and shutdown lifecycle calls into the render graph.
- Default feature-stage nodes wrap the existing command/screen feature registry.
- `App.Render()` records the compiled default render graph after creating the frame context and frame-level profiler counters.
- `core-gbuffer`, `core-hiz`, `core-shadows`, `core-tiled-light-cull`, `core-lighting`, `core-debug-scene`, `core-accumulation`, and `core-resolve` are graph-routed core nodes.
- `core-gbuffer` records pipeline, bind-group, and workgroup readiness counters before dispatching G-buffer generation.
- `core-hiz` records pipeline, depth-view, mip-view, bind-group, camera-buffer, and readback readiness counters before dispatching Hi-Z generation.
- `core-shadows` owns shadow update summary/profiler counters and delegates scheduling, light preparation, dispatch, and cache recording to `GpuBufferManager`.
- `core-tiled-light-cull` records readiness counters and returns explicit errors if local lights exist but dispatch resources are missing.
- `core-lighting` records pipeline, bind-group, and workgroup readiness counters and returns explicit errors before dispatch when resources are missing.
- `core-accumulation` owns the WBOIT clear shell and delegates in-pass drawing to legacy `FeaturePassStageAccumulation` contributors plus graph-owned pass-stage contributors.
- `core-resolve` owns the swapchain clear and fullscreen composite pass.
- All current core render-pass bodies are routed through graph nodes.
- `feature-text-overlay` and `feature-gizmos-overlay` are explicit optional feature nodes migrated out of the post-resolve compatibility stage.
- `feature-astronomical` is an explicit optional feature node after post-lighting compatibility work and before planet bodies.
- `feature-planet-bodies` is an explicit optional feature node after post-lighting compatibility work and before analytic media.
- `feature-analytic-media` is an explicit optional feature node after post-lighting compatibility work and before debug/accumulation/resolve.
- `feature-ca-volumes-sim` and `feature-ca-volumes-render` are explicit optional feature nodes for CA simulation/bounds and half-resolution CA volume rendering.
- `transparency`, `sprites`, `water`, far planet rings, debris midfield, and particle drawing are graph-owned by `core-accumulation` and draw through the render-graph pass-stage dispatch path, preserving the single WBOIT pass shell.
- `feature-particles-sim` is an explicit optional feature node for particle simulation/spawn before generic pre-G-buffer compatibility work.
- Graph-owned features are skipped by compatibility command/pass/screen stage dispatchers to prevent duplicate rendering during incremental migration.
- Text and gizmo ECS bridge sync are now gated by registered graph feature ownership, so disabled overlay features skip their ECS query/sync path and clear stale frame data.
- Submit/present remains outside the graph boundary.
- Verified with `env GOCACHE=/tmp/gekko3d-gocache go test ./...`.

### Phase 2: Core Node Migration

Goal:

- Move core pass recording out of the monolithic `App.Render()` body.

Tasks:

- Create core nodes for G-buffer, Hi-Z, shadows, tiled light cull, lighting, debug, accumulation, and resolve.
- Keep swapchain acquire, command encoder creation, submit, present, FPS tracking, and frame index advancement in `App.Render()`.
- Preserve existing profiler scope names where practical.

Acceptance criteria:

- `App.Render()` becomes orchestration plus graph execution.
- Core-only rendering works with all optional features disabled.
- Resize and scene-buffer recreation behavior remains unchanged.
- Graph lifecycle hooks are available for explicit optional nodes without adding new central `App` branches.

### Phase 3: Optional Node Migration

Goal:

- Replace feature-stage hook dispatch with explicit optional graph nodes.

Tasks:

- Migrate low-risk features first: text, gizmos, sprites. Text and gizmos are explicit post-resolve graph nodes; sprites are graph-owned in-pass accumulation contributors.
- Migrate accumulation contributors: transparency, water, far rings, debris midfield, particles. Particle drawing and simulation are migrated, with simulation kept as a separate pre-G-buffer graph node.
- Migrate half-resolution or post-lighting features: analytic media, CA volumes, planet bodies, astronomical, far rings, debris midfield. Analytic media, CA volumes, astronomical bodies, and planet bodies are migrated.
- Remove feature-stage enums only after no feature depends on them.

Acceptance criteria:

- Each optional feature registers its own nodes.
- Disabled features do not allocate pipelines or render resources.
- Feature order is expressed by graph dependencies, not by hardcoded stage enums.

### Phase 4: Feature-Owned Bridge Extraction

Goal:

- Reduce ECS-to-renderer bridge complexity and eliminate disabled-feature sync cost.

Tasks:

- Add a feature bridge registration surface to `VoxelRtModule`. Initial `BridgeFeatures` registration now exists; text/gizmo/skybox bridge bodies are installed before renderer update, analytic media, CA volumes, water, planet bodies, astronomical, far rings, and debris use the batched bridge slot before `GPU Batch`, and particles/sprites use the after-batch slot.
- Move optional sync blocks out of the broad `voxelRtSystem` flow one feature at a time.
- Keep core voxel object sync centralized until the graph migration is stable.
- Add tests proving disabled feature sync does not call the corresponding `GpuBufferManager.Update*` path.

Acceptance criteria:

- Action-oriented games can run without celestial, planet-body, ring/debris, media, CA, water, particle, or sprite sync.
- SpaceSim keeps its current feature set through a preset.
- The remaining core bridge is smaller and clearly documented.

Current status:

- Text and gizmo ECS bridge sync are gated by registered graph feature ownership and are installed as registered bridge systems outside the broad `voxelRtSystem` flow. Text sync uses a tested `buildTextBridgeItems` adapter to append `TextOverlayItem` input to the renderer feature after any retained UI/debug text produced through `DrawText`; `voxelRtPreludeSystem` is the single per-frame clear point for shared text resources. Gizmo sync uses `buildGizmoBridgeItems` to hand `GizmoOverlayItem` input to the renderer feature.
- Particle ECS bridge sync is gated by registered `feature-particles-sim` graph ownership and installed as an after-batch bridge system, clearing stale spawn count when the particle feature is not registered. Particle sync now hands typed `ParticleEmitterInput` / `ParticleFrameInput` to the renderer app, which owns WGSL emitter byte packing, params upload, emitter/spawn GPU updates, and bind-group refresh; atlas asset lookup remains in the bridge for now.
- Water ECS bridge sync is gated by registered water feature ownership plus `core-accumulation` graph ownership and installed as a batched bridge system before `GPU Batch`; stale water contribution counts are cleared when water is not registered. Water sync now hands typed `WaterSurfaceInput` / `WaterRippleInput` to the renderer app; GPU manager packing remains behind the app feature boundary.
- Analytic-media ECS bridge sync is gated by registered `feature-analytic-media` graph ownership and installed as a batched bridge system before `GPU Batch`, clearing stale analytic-medium contribution count when the feature is not registered. Analytic-media sync now hands typed `AnalyticMediumInput` to the renderer app; GPU manager packing remains behind the app feature boundary.
- Planet-body ECS bridge sync is gated by registered `feature-planet-bodies` graph ownership and installed as a batched bridge system before `GPU Batch`, clearing stale planet-body contribution count when the feature is not registered. Planet-body sync now hands typed `PlanetBodyInput` / `PlanetBodySurfaceInput` to the renderer app; GPU manager packing and baked-surface signature handling remain behind the app feature boundary.
- Astronomical ECS bridge sync is gated by registered `feature-astronomical` graph ownership and installed as a batched bridge system before `GPU Batch`, clearing stale astronomical body count when the feature is not registered. Astronomical sync now hands typed `AstronomicalBodyInput` to the renderer app; GPU manager packing remains behind the app feature boundary.
- Far planet-ring ECS bridge sync is gated by registered far-ring feature ownership plus `core-accumulation` graph ownership and installed as a batched bridge system before `GPU Batch`; stale far-ring contribution count is cleared when the feature is not registered. Far-ring sync now hands typed `FarPlanetRingInput` to the renderer app; GPU manager packing remains behind the app feature boundary.
- Debris-midfield ECS bridge sync is gated by registered debris feature ownership plus `core-accumulation` graph ownership and installed as a batched bridge system before `GPU Batch`; stale debris contribution count is cleared when the feature is not registered. Debris sync now hands typed `DebrisMidfieldInput` to the renderer app; GPU manager packing remains behind the app feature boundary.
- CA volume ECS bridge sync and preset upload are gated by registered `feature-ca-volumes-sim` and `feature-ca-volumes-render` graph ownership and installed as a batched bridge system before `GPU Batch`; CA sync now hands typed `CAVolumeInput` / `CAVolumeFrameInput` to the renderer app, which owns preset upload, counter application, GPU host packing, and CA param upload. Stale CA contribution and budget counters, previous-pass state, and bridge-owned CA scene objects are cleared when the feature is not registered.
- Sprite ECS bridge sync is gated by registered sprite feature ownership plus `core-accumulation` graph ownership and installed as an after-batch bridge system; stale sprite counts and batches are cleared when the feature is not registered, and entity-LOD sprite proxies fall back to voxel object sync when sprites are disabled. Sprite sync now hands typed `SpriteInstanceInput` / `SpriteBatchInput` to the renderer app, which owns byte packing and GPU batch-desc conversion; atlas texture lookup remains in the bridge for now.
- Skybox ECS bridge sync is gated by registered skybox feature ownership plus `feature-skybox-update` graph ownership and installed as a pre-update bridge system, clearing cached skybox layer/sun bridge state and pending renderer input when the feature is not registered. A pure bridge adapter builds `SkyboxResources` input with renderer-level `SkyboxLayerInput` records; `feature-skybox-update` packs GPU layers and applies pending input during render graph update.
- `VoxelRtModule.BridgeFeatures` provides the first declarative bridge registration surface. Built-in defaults preserve the current gates, and module registrations can add or override bridge requirements without changing the central sync systems yet.
- The remaining broad `voxelRtSystem` path is guarded by regression coverage as core bridge only: `Sync Instances`, camera state, and `Sync Lights`. Optional bridge scopes must come from registered bridge systems rather than the broad system.

### Phase 5: Resource Ownership Cleanup

Goal:

- Move feature-specific pipelines and bind groups out of broad `App` fields where doing so reduces coupling.

Tasks:

- Add a code-level resource ownership inventory before moving fields so broad `App` fields, feature-hook-owned resources, shared pass state, and `GpuBufferManager` state are explicitly classified.
- Introduce feature-local state structs only where resource lifetime is truly feature-owned.
- Keep shared textures, scene buffers, shadow maps, G-buffer, Hi-Z, and resolve targets under core app or `GpuBufferManager`.
- Update rebuild hooks so feature resources rebuild from node/feature ownership, not a central invalidation switch.

Acceptance criteria:

- Adding a new optional feature does not require adding broad fields to `App` unless it genuinely needs shared renderer state.
- Resize and scene-buffer recreation paths are locally owned where practical.

Current status:

- `DefaultFeatureResourceInventory()` records the Phase 5 resource ownership map in code. It classifies core resources, feature resource holders, shared accumulation pass state, and resources already living in `GpuBufferManager`.
- Skybox is feature-bootstrapped with its generation pipeline stored in `GpuBufferManager`, and its renderer-side input handoff now lives in `App.SkyboxResources`; optional feature-owned `App` state uses resource holders rather than raw broad pipeline/pass fields.
- Text overlays now use `App.TextResources` for the renderer, atlas, pipeline, bind group, vertex buffer, queued overlay items, and vertex count.
- Gizmos are the first resource-holder relocation: the raw broad `App.GizmoPass` field moved behind `App.GizmoResources`, while `GizmoFeature` remains responsible for setup, resize/recreate bind-group rebuild, update, render, and shutdown.
- Transparency now uses `App.AccumulationResources` for the transparent overlay pipeline and previous-pass state, while WBOIT targets and transparent overlay bind groups remain in `GpuBufferManager`.
- Water is the first accumulation-contributor relocation: the raw broad `App.WaterPipeline` field moved behind `App.WaterResources`; typed water input application now lives at the renderer app feature boundary, while water bind groups, GPU record packing, and contribution readiness remain in `GpuBufferManager`.
- Far planet rings now follow the same accumulation-contributor resource-holder pattern: the raw broad `App.FarPlanetRingPipeline` field moved behind `App.FarPlanetRingResources`; typed far-ring input application now lives at the renderer app feature boundary, while far-ring bind groups, GPU record packing, and contribution readiness remain in `GpuBufferManager`.
- Debris midfield now follows the same accumulation-contributor resource-holder pattern: the raw broad `App.DebrisMidfieldPipeline` field moved behind `App.DebrisMidfieldResources`; typed debris input application now lives at the renderer app feature boundary, while debris bind groups, GPU record packing, and contribution readiness remain in `GpuBufferManager`.
- Sprites now follow the resource-holder pattern: the raw broad `App.SpritesPipeline` field moved behind `App.SpriteResources`, with a narrow public pipeline accessor for the ECS bridge while atlas, batches, bind groups, and contribution readiness remain in `GpuBufferManager`.
- Particles now use `App.ParticleResources` for render and simulation pipelines, spawn count, typed renderer input application, and atlas bootstrap state. Particle buffers, simulation bind groups, render bind groups, and contribution readiness remain in `GpuBufferManager`.
- CA volumes now use `App.CAVolumeResources` for render, simulation, bounds pipelines, previous-pass state, and typed renderer input application. CA buffers, targets, counters, bind groups, and mirrored compute pipeline layout state remain in `GpuBufferManager`.
- Analytic media now uses `App.AnalyticMediumResources` for its render pipeline and typed renderer input application, while media buffers, volumetric targets, bind groups, GPU record packing, and contribution readiness remain in `GpuBufferManager`.
- Astronomical bodies now use `App.AstronomicalResources` for their render pipeline and typed renderer input application, while astronomical buffers, bind groups, GPU record packing, and contribution readiness remain in `GpuBufferManager`.
- Planet bodies now use `App.PlanetBodyResources` for their render pipeline and typed renderer input application, while planet-body buffers, bind groups, depth target, baked-surface GPU packing/signature handling, and contribution readiness remain in `GpuBufferManager`.

### Phase 6: Public Custom Feature API

Goal:

- Allow games to add custom render features without patching core pass logic.

Tasks:

- Expose a narrow feature registration API from `VoxelRtModule`.
- Document required node lifecycle and bridge rules.
- Provide one small sample feature or test-only feature proving the API.

Acceptance criteria:

- A game can register an optional feature with one graph node and one bridge sync path.
- Games that do not register the feature incur no setup, sync, update, or render cost.

Current status:

- `VoxelRtModule.RenderFeatures` accepts `VoxelRtRenderFeature` values, which are registered on the internal voxel RT app before renderer initialization.
- `VoxelRtModule.RenderGraphNodes` accepts `VoxelRtRenderNodeSpec` values, which are appended to the default render graph before graph lifecycle setup.
- `VoxelRtModule.BridgeFeatures` remains the ECS sync declaration surface; custom bridge gates can require both an app feature name and custom graph node names.
- `runtime.md` documents custom node naming, lifecycle, dependency, and bridge-gating rules; root tests cover registration, bridge gating, and graph lifecycle dispatch for a custom module node.

## Verification Plan

Automated verification should scale with the phase.

Minimum focused checks for graph/app work:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app ./voxelrt/rt/gpu ./voxelrt/rt/core
```

Bridge ownership changes:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test .
```

Broad graph or feature preset changes:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Consumer smoke checks:

```sh
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...

cd /Users/ddevidch/code/go/gekko3d/spacesim
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Manual visual/GPU checks that automated tests cannot replace:

- ActionGame with the action preset.
- SpaceSim with the space preset.
- Core-only/minimal preset.
- All default features enabled.
- Window resize after scene load.
- Transparent voxel overlay plus sprites/particles/water.
- CA volume and analytic-media scenes.
- SpaceSim celestial scene with planet bodies, astronomical visuals, rings, and debris.

## Risks

- Hidden resource dependencies may become graph edges only after a visual regression exposes them.
- Feature-owned bridge extraction may duplicate query work until common helpers are factored carefully.
- Moving resources out of `App` too early may make debugging harder.
- A graph API that is too generic can obscure important WebGPU details.
- A graph API that is too narrow can collapse back into stage enums.

## Risk Mitigation

- Preserve the old frame order through the first graph infrastructure pass.
- Migrate one feature at a time.
- Keep graph validation simple and strict.
- Add tests for disabled feature setup, disabled feature sync, and disabled feature render-node absence.
- Keep the CPU-authoritative scene and picking/edit path intact during the graph migration.
- Treat manual GPU visual checks as required for each phase that changes pass order, targets, or resource lifetime.

## Definition of Done

The migration is complete when:

- `App.Render()` no longer hardcodes feature-specific pass order.
- Core passes and optional passes are registered as graph nodes.
- Disabled features register no nodes and do not allocate feature pipelines/resources.
- Disabled features do not run ECS bridge sync.
- ActionGame and SpaceSim use explicit feature presets.
- New optional renderer features can be added without editing unrelated pass code.
- Renderer docs describe the graph, feature bridge ownership, and verification path.
