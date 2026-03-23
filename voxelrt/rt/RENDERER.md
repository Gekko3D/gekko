# VoxelRT Renderer

This document describes the current renderer architecture and frame graph. It is the source of truth for runtime behavior. Roadmap and speculative optimization notes belong in `RENDERER_ANALYSIS.md`.

The old per-shader markdown docs were removed because they had drifted behind the code. For pass-specific details, use the WGSL source plus the matching pipeline-setup code.

Related docs:

- `AGENT_GUIDE.md`
- `VERIFY.md`
- `EDITOR.md`
- `PARTICLES.md`
- `shaders/*.wgsl`
- `shaders/shaders.go`

## Ownership Boundaries

- ECS bridge: `../../mod_voxelrt_client*.go`
  - Creates and synchronizes renderer-side objects, lights, camera state, text, gizmos, particles, sprites, skybox inputs, and CA volumes.
- `app.App`: `app/`
  - Owns WebGPU device/surface lifetime, render pipelines, resize flow, opaque storage output, and pass scheduling.
- `gpu.GpuBufferManager`: `gpu/`
  - Owns G-buffer textures, WBOIT targets, shadow maps, Hi-Z, scene buffers, voxel atlas resources, and most cached bind groups.
- `core.Scene` and friends: `core/`
  - Own CPU scene state, camera/light math, culling, raycast, gizmos, and text primitives.
- `volume.XBrickMap`: `volume/`
  - Owns sparse voxel storage, edit semantics, dirty tracking, traversal, and compression.

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

That split is important because bridge sync and GPU resource upload happen before render-pass execution.

## `App.Update()`

`Update()` is the per-frame CPU preparation step. It currently:

1. builds view/projection matrices
2. reads the previous Hi-Z snapshot
3. runs `Scene.Commit(...)` with frustum and optional Hi-Z occlusion
4. updates profiler counters
5. calls `BufferManager.UpdateScene(...)`
6. rebuilds dependent bind groups if GPU resources were recreated
7. updates camera uniforms
8. refreshes text and gizmo buffers

Important details:

- Hi-Z uses previous-frame data and is disabled during fast camera motion.
- `Scene.Commit(...)` produces both `VisibleObjects` and `ShadowObjects`.
- `UpdateScene(...)` can trigger shadow-map growth and scene-buffer growth, which forces downstream bind-group recreation.

## `App.Render()`

The current frame graph is:

1. particle simulation compute passes
2. CA volume simulation and bounds passes
3. G-buffer compute pass
4. Hi-Z generation compute pass
5. shadow pass
6. probe GI bake compute pass for a capped dirty-probe batch
7. deferred lighting compute pass
8. optional debug compute pass
9. accumulation render pass
   - CA volumes
   - transparent voxel overlay
   - particles
   - sprites
10. resolve render pass
   - resolve transparency over opaque lighting
   - text overlay
   - gizmos

The old fullscreen blit pipeline still exists in setup code, but it is not the live compositor. The resolve pipeline is the active path to the swapchain.

## Render Targets And Formats

### Opaque lighting output

- `App.StorageTexture`: `RGBA16Float`
- written by deferred lighting
- sampled by the resolve pass

### G-buffer

- depth: `RGBA32Float`
- normal: `RGBA16Float`
- material: `RGBA32Float`
- position: `RGBA32Float`

### Transparency / WBOIT

- accumulation: `RGBA16Float`
- weight: `R16Float`

### Other major resources

- shadow maps: 2D array textures managed by `GpuBufferManager`
- Hi-Z: `R32Float` mip chain built from the G-buffer depth texture at half resolution
- voxel payload atlas: fixed-size 3D `R8Uint` texture
- probe GI: uniform metadata plus storage buffers for per-probe SH coefficients and distance moments

## Scene And Culling Model

- `Scene.Objects` is the authoritative CPU-side object list.
- `Scene.Commit(...)` updates `WorldAABB` values, runs frustum culling, then optionally applies Hi-Z occlusion.
- `VisibleObjects` drives main scene buffers and the camera-facing BVH.
- `ShadowObjects` drives a broader shadow BVH so off-screen casters can still affect visible receivers.
- `Scene.Raycast()` uses CPU-side scene and voxel data. It does not rely on `VisibleObjects` or GPU BVHs.

Hi-Z specifics:

- mip 0 starts at half the G-buffer resolution
- readback targets a coarse mip around 64 pixels wide
- readback uses a 0/1/2/3 state machine inside `gpu/manager_hiz.go`
- newly dirty objects get warmup/hysteresis handling to reduce false occlusion pops

## Bridge Responsibilities

`VoxelRtState` is the bridge state, not just a wrapper around `RtApp`.

It owns:

- loaded model templates
- entity-to-object and object-to-entity maps
- CA volume object mapping
- particle pool ownership by entity
- helper APIs for projection, raycast, and voxel editing
- cached skybox/atlas tracking

The bridge also:

- swaps in custom voxel maps
- marks structure changes when needed
- uploads particle emitters and spawn requests
- rebuilds frame text and gizmos

## Specialized Subsystems

### Shadows

- directional lights use cascades
- spot lights are prioritized by camera distance
- only a budgeted subset of spot-light shadows is refreshed each frame

### Particles

- CPU authors emitters and spawn requests
- GPU simulates particle state
- render is indirect and writes into WBOIT targets
- particle simulation depends on current scene voxel buffers

### CA volumes

- have their own simulation and bounds passes
- accumulate into the same transparency targets as the transparent overlay and particles

### Sprites, text, and gizmos

- sprites participate in the accumulation pass
- text and gizmos are drawn during the resolve pass
- text is frame-ephemeral and rebuilt each frame

### Probe GI

- GI is opt-in through `VoxelRtModule.ProbeGI`
- probe placement is auto-derived from the union of scene object world AABBs
- large scenes expand probe spacing until the grid fits the v1 cap
- scene, light, and skybox changes mark probes dirty and the bake pass updates only a capped batch per frame
- deferred lighting samples the probe buffers for opaque voxels only

## Resource Recreation Rules

### Resize

`App.Resize()` recreates:

- surface configuration
- opaque storage output
- debug/fullscreen bind groups
- G-buffer textures
- G-buffer, lighting, and shadow bind groups
- probe GI bake bind groups
- transparent-overlay bind groups
- particles, sprites, CA volume, and resolve pipelines

### Scene-resource growth

If `BufferManager.UpdateScene(...)` returns `recreated=true`, `App.Update()` rebuilds dependent bind groups. This currently includes debug, G-buffer, lighting, probe GI, shadows, transparent overlay, CA volume passes, gizmos, and particle sim bindings.

This is the main invalidation path for renderer plumbing changes.

## Common Sources Of Drift

If you update the code, update these docs too when any of the following changes:

- pass ordering
- storage or G-buffer formats
- which subsystems participate in accumulation or resolve
- ownership of a resource between `App` and `GpuBufferManager`
- the `recreated=true` bind-group rebuild contract
- culling or shadow scheduling rules
