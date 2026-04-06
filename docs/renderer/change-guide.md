# VoxelRT Change Guide

Use this guide when you are editing renderer code and need to answer three questions quickly:

1. Which layer owns the behavior?
2. What else must be updated when that layer changes?
3. What failure mode should I expect if I miss something?

## Ownership Map

- ECS bridge: `mod_voxelrt_client.go`, `mod_voxelrt_client_systems.go`
  - renderer-side identity maps, model caches, picking/edit helpers, and ECS-to-renderer sync
- App orchestration: `voxelrt/rt/app/`
  - WebGPU lifetime, pipeline creation, resize handling, update scheduling, and render-pass order
- GPU resource manager: `voxelrt/rt/gpu/`
  - scene buffers, G-buffer textures, WBOIT targets, half-resolution volumetric targets, shadows, Hi-Z, particles, sprites, analytic media, CA volumes, and probe GI resources
- Scene and culling: `voxelrt/rt/core/`
  - CPU scene state, frustum and Hi-Z culling, lights, camera, raycast, gizmos, and text primitives
- Sparse voxel storage: `voxelrt/rt/volume/`
  - `XBrickMap`, dirty tracking, edit behavior, compression, and traversal invariants
- Shader code: `voxelrt/rt/shaders/`
  - binding expectations and shader algorithms; match these with `voxelrt/rt/app/app_pipelines.go`

## Engine Timeline

`VoxelRtModule.Install` wires the renderer into three engine stages:

1. `Prelude`
   - syncs input state
   - clears frame text
   - starts GPU batch accumulation with `BufferManager.BeginBatch()`
2. `PreRender`
   - syncs ECS objects, analytic media, CA volumes, camera, text, lights, gizmos, particles, and sprites into renderer state
   - runs `RtApp.Update()`
3. `Render`
   - runs `RtApp.Render()`

If a change depends on scene upload, particle uploads, or text lifetime, it usually belongs in bridge code or `Update()`, not only inside `Render()`.

## Start Here by Change Type

| If you need to change... | Start here | Also inspect |
| --- | --- | --- |
| ECS sync for voxel objects, lights, text, gizmos, particles, or sprites | `mod_voxelrt_client_systems.go` | `voxelrt/rt/app/app_frame.go`, `voxelrt/rt/gpu/manager*.go` |
| Atmosphere or bounded fog/media behavior | `analytic_medium_ecs.go`, `analytic_medium_presets.go`, `mod_voxelrt_client_systems.go` | `voxelrt/rt/app/feature_analytic_medium.go`, `voxelrt/rt/app/app_medium.go`, `voxelrt/rt/shaders/analytic_medium.wgsl`, [`media.md`](media.md) |
| Stylized water surfaces | `water_surface_ecs.go`, `mod_voxelrt_client_systems.go` | `voxelrt/rt/app/feature_water.go`, `voxelrt/rt/app/app_water.go`, `voxelrt/rt/gpu/manager_water.go`, `voxelrt/rt/shaders/water_surface.wgsl`, [`media.md`](media.md) |
| Public picking, projection, or voxel-edit helpers | `mod_voxelrt_client.go` | `voxelrt/rt/core/scene.go`, `voxelrt/rt/volume/xbrickmap_edit.go` |
| Frame pass ordering or execution timing | `voxelrt/rt/app/app_frame.go` | `voxelrt/rt/app/app.go`, `voxelrt/rt/app/app_pipelines.go` |
| Pipeline layouts and shader bindings | `voxelrt/rt/app/app_pipelines.go` | relevant `.wgsl` file plus `voxelrt/rt/gpu/Create*BindGroups`; voxel payload bindings are shared across all voxel consumers |
| Scene upload, buffer growth, or bind-group churn | `voxelrt/rt/gpu/manager_scene.go`, `voxelrt/rt/gpu/manager_alloc.go` | `voxelrt/rt/app/app_frame.go`, `voxelrt/rt/gpu/manager_render_setup.go` |
| Voxel serialization, atlas layout, or dirty upload rules | `voxelrt/rt/gpu/manager_voxel.go` | `voxelrt/rt/volume/xbrickmap*.go`, `voxelrt/rt/gpu/manager.go`, and every voxel-reading shader |
| Culling or interaction regressions | `voxelrt/rt/core/scene.go`, `voxelrt/rt/core/camera.go` | `voxelrt/rt/gpu/manager_hiz.go` |
| Shadow behavior or update cadence | `voxelrt/rt/gpu/manager_shadow.go`, `voxelrt/rt/app/app_frame.go` | `voxelrt/rt/core/light.go`, `voxelrt/rt/shaders/shadow_map.wgsl` |
| Particle runtime behavior | `voxelrt/rt/gpu/manager_particles.go`, `voxelrt/rt/app/app_particles.go` | `particles_ecs.go`, [`particles.md`](particles.md) |
| CA volume behavior | `voxelrt/rt/gpu/manager_ca.go`, `voxelrt/rt/app/app_ca.go` | `voxelrt/rt/app/feature_ca_volumes.go`, `voxelrt/rt/app/app_frame.go`, `voxelrt/rt/shaders/resolve_transparency.wgsl` |

## Resource Rebuild Rules

These rules are load-bearing. Many renderer regressions reduce to stale bind groups after a resize or buffer growth event.

### Resize

`App.Resize()` must refresh all resize-sensitive textures, views, bind groups, and pipelines. If a new feature adds a surface-size-dependent resource and `Resize()` is not updated, the renderer will drift after the first resize.

### `UpdateScene(...) == recreated`

When scene buffers or shadow-map capacity grow, `App.Update()` must rebuild dependent bind groups. The current code handles debug, G-buffer, lighting, shadows, transparent overlay, analytic media, CA volume render/sim/bounds, resolve, and gizmo camera/depth state. New features added on top of scene buffers need the same invalidation handling.

Voxel atlas resource changes now also fan out more widely. The paged payload atlas is exposed as four fixed `texture_3d<u32>` bindings, so bind-group builders and shader layouts have to stay synchronized across the whole voxel stack.

## Invariants and Gotchas

- CPU scene state is still authoritative for interaction.
  - Rendering may use GPU-side acceleration, but picking and edits still depend on `Scene.Objects` and `XBrickMap`.
- `VisibleObjects` and `ShadowObjects` are different filtered views.
  - Do not assume shadow casters are equivalent to visible objects.
- `XBrickMap` dirty flags matter.
  - Edits that skip dirty propagation can appear in CPU picking but never reach GPU buffers correctly.
- Voxel payload bindings move as a set.
  - `gbuffer.wgsl`, `shadow_map.wgsl`, `transparent_overlay.wgsl`, and `particles_sim.wgsl` all expect `voxel_payload_0..3`, and their bind-group builders must match.
- `BrickRecord` is no longer 16 bytes.
  - The live layout is `atlas_offset`, `occupancy_mask_lo`, `occupancy_mask_hi`, `atlas_page`, `flags` for 20 bytes total. Any CPU writer or WGSL struct drift here will corrupt voxel reads.
- Voxel shading style has a deliberate contract.
  - Keep voxel albedo/material lookup palette-driven and blocky.
  - Keep one normal per visible voxel cell rather than interpolating mesh-like normals across a voxel.
  - Prefer local occupancy-gradient normals to recover voxelized shape volume.
  - If the gradient is degenerate, use a deterministic occupancy-based fallback so the same visible voxel keeps one normal; keep face-entry only as a last resort and do not use object-center or radial fallbacks.
  - If the degenerate voxel is exposed on both sides of an axis, preserve the deterministic normal but treat direct local lighting as two-sided so 1-voxel-thick planes and rods still react to point and spot lights.
  - Keep normal transforms consistent across `XBrickMap`, solid-brick, and `tree64` paths; non-uniform scale makes this load-bearing.
- Text and gizmos are frame-lifetime data.
  - If you stop resubmitting them, they disappear by design.
- There are multiple debug knobs.
  - `App.DebugMode`, camera debug modes, overlay modes, and render modes are separate controls.
- Particles are hybrid.
  - Emitter state starts from ECS, but alive-particle state and draw counts are GPU-owned.
- Analytic media is not transparent voxels.
  - Bounded fog and atmosphere now use a dedicated half-resolution temporal volumetric path and resolve integration, not the WBOIT accumulation path.
- Water is still separate.
  - Do not try to route water surfaces through `AnalyticMediumComponent`.

## Common Failure Modes

- Black frame after a scene or resize change
  - suspect stale bind groups, invalid pipeline layouts, or a resource that was not recreated in `Resize()`
- Voxel surfaces render in one pass but disappear in another
  - suspect voxel payload page binding drift, mismatched `BrickRecord` layout, or a bind-group builder that was updated in only one pass
- Transparent content missing
  - inspect accumulation targets, resolve bindings, and particle or transparent-overlay bind groups
- Atmosphere or fog missing, stale, or sampling the wrong frame
  - inspect analytic-media history targets, `feature_analytic_medium.go`, resolve bindings, and previous-frame temporal inputs
- CA volumes missing, swapping, or clipping after a renderer change
  - inspect the dedicated CA half-resolution pass, CA resolve bindings, and miss-path behavior in `ca_volume_render.wgsl`
- Occlusion popping or disappearing objects
  - inspect Hi-Z enable rules, previous-frame snapshot use, and `Scene.Commit(...)`
- Edits visible to picking but not rendering
  - inspect `XBrickMap` dirty propagation and GPU upload paths
- A change works until the window resizes
  - the resource is probably surface-size-dependent and missing from `Resize()`

## Verification

Use [`verification.md`](verification.md) for the command list. In practice, the minimum useful checks are:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` when bridge code changed
