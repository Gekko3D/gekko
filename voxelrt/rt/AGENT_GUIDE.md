# VoxelRT Agent Guide

This guide is for agent-driven changes inside the renderer. It answers three questions quickly:

1. Where should I start reading for this kind of change?
2. Which layer owns the behavior I want to modify?
3. What else must I re-check when I touch that layer?

## Ownership Map

- ECS bridge: `../../mod_voxelrt_client.go`, `../../mod_voxelrt_client_systems.go`
  - Owns renderer-side identity maps, model caches, picking/edit helpers, and ECS-to-renderer sync.
- App orchestration: `app/`
  - Owns WebGPU device/surface lifetime, pipeline creation, resize handling, update scheduling, and render pass order.
- GPU resource manager: `gpu/`
  - Owns scene buffers, G-buffer textures, WBOIT targets, shadow maps, Hi-Z, particles, sprites, skybox, CA volumes, and most bind groups.
- Scene and culling: `core/`
  - Owns CPU scene state, frustum/Hi-Z culling, lights, camera, raycast, gizmos, and text primitives.
- Sparse voxel storage: `volume/`
  - Owns `XBrickMap`, dirty tracking, edit behavior, compression, and traversal invariants.
- Shader code: `shaders/`
  - Owns pass-specific binding expectations and shader algorithms. Match this with `app/app_pipelines.go`.
  - The legacy fullscreen blit shader still exists, but live composition goes through the resolve pass.
  - The old per-shader markdown docs were removed; use WGSL plus pipeline setup as the source of truth.

## Engine Timeline

`VoxelRtModule.Install` wires the renderer into three engine stages:

1. `Prelude`
   - Syncs mouse/input state.
   - Clears frame text.
   - Starts GPU batch accumulation with `BufferManager.BeginBatch()`.
2. `PreRender`
   - `voxelRtSystem`: syncs ECS objects, CA volumes, camera, text, lights, gizmos, particles, and sprites into renderer state.
   - `voxelRtUpdateSystem`: runs `RtApp.Update()`.
3. `Render`
   - `voxelRtRenderSystem`: runs `RtApp.Render()`.

That sequencing matters. A change that depends on scene upload, particle uploads, or text lifetime usually belongs in the bridge or `Update()`, not only inside `Render()`.

## Actual Frame Graph

`App.Update()`:

1. Builds camera matrices.
2. Reads back the previous Hi-Z snapshot.
3. Runs `Scene.Commit(...)` with frustum planes and optional Hi-Z occlusion.
4. Updates profiler counters.
5. Calls `BufferManager.UpdateScene(...)`.
6. Rebuilds dependent bind groups if `UpdateScene(...)` returns `recreated=true`.
7. Updates camera uniforms, text buffers, and gizmo buffers.

`App.Render()`:

1. Particle simulation compute passes.
2. CA volume simulation and bounds compute passes.
3. G-buffer compute pass.
4. Hi-Z generation pass.
5. Shadow pass with scheduled spot and directional updates.
6. Probe GI bake compute pass for the current dirty batch.
7. Deferred lighting compute pass into the opaque storage texture.
8. Optional debug compute pass.
9. Accumulation render pass:
   - CA volumes
   - transparent voxel overlay
   - particles
   - sprites
10. Resolve render pass to the swapchain:
   - resolve transparency over opaque lighting
   - text overlay
   - gizmos

## Start Here By Change Type

| If you need to change... | Start here | Also inspect |
| --- | --- | --- |
| ECS sync for voxel objects, lights, text, gizmos, particles, or sprites | `../../mod_voxelrt_client_systems.go` | `app/app_frame.go`, `gpu/manager*.go` |
| Public picking, projection, or voxel edit helpers | `../../mod_voxelrt_client.go` | `core/scene.go`, `volume/xbrickmap_edit.go` |
| Frame pass ordering or when work executes | `app/app_frame.go` | `app/app.go`, `app/app_pipelines.go` |
| Pipeline layouts and shader bindings | `app/app_pipelines.go` | relevant `.wgsl` file in `shaders/`, plus `gpu/Create*BindGroups` |
| Scene upload, buffer growth, or bind group churn | `gpu/manager_scene.go`, `gpu/manager_alloc.go` | `app/app_frame.go`, `gpu/manager_render_setup.go` |
| Voxel serialization, atlas layout, or dirty upload rules | `gpu/manager_voxel.go` | `volume/xbrickmap*.go` |
| Culling or interaction regressions | `core/scene.go`, `core/camera.go` | `gpu/manager_hiz.go`, `core/culling_test.go` |
| Shadow behavior or update cadence | `gpu/manager_shadow.go`, `app/app_frame.go` | `core/light.go`, `shaders/shadow_map.wgsl` |
| Probe GI placement, dirty tracking, or bake resources | `app/app_probe_gi.go`, `gpu/manager_probe_gi.go` | `gpu/probe_gi.go`, `shaders/probe_gi_bake.wgsl`, `shaders/deferred_lighting.wgsl` |
| Particle runtime behavior | `gpu/manager_particles.go`, `app/app_particles.go` | `../../particles_ecs.go`, `PARTICLES.md` |
| CA volume behavior | `gpu/manager_ca.go`, `app/app_ca.go` | `app/app_frame.go` |

## Resource Rebuild Rules

These rules are load-bearing. A lot of renderer bugs reduce to stale bind groups after a resize or buffer growth.

### Resize

`App.Resize()` does all of the following:

- reconfigures the surface
- recreates the opaque storage texture
- recreates debug/fullscreen bind groups
- recreates G-buffer textures
- recreates G-buffer, lighting, and shadow bind groups
- recreates probe GI bake bind groups
- recreates transparent-overlay bind groups
- recreates particles, sprites, CA volume, and resolve pipelines

If a change adds a resize-sensitive texture or view, update `Resize()` or the renderer will drift after window resizes.

### `UpdateScene(...) == recreated`

When scene buffers or shadow-map capacity grow, `App.Update()` must rebuild dependent bind groups. The current code does that for:

- debug
- G-buffer
- lighting
- probe GI
- shadows
- transparent overlay
- CA volume render/sim/bounds
- gizmo camera/depth bind groups
- particle sim bind groups

If you add a new pass that depends on scene buffers, wire it into this path.

### Other invalidation points

- Particle atlas changes require particle render bind groups to stay valid.
- Hi-Z owns its own readback state machine and cached mip bind groups.
- `CreateGBufferTextures()` also recreates shadow-map textures. Keep that coupling in mind when touching resize logic.

## Invariants And Gotchas

### CPU scene state is still authoritative for interaction

- `Scene.Raycast()` works on CPU-side `Scene.Objects`.
- `VoxelSphereEdit` mutates CPU-side voxel data.
- GPU-side rendering reflects edits only after the normal update/upload path runs.

Do not assume visibility culling or GPU resources are the source of truth for picking.

### `VisibleObjects` and `ShadowObjects` are different views

- `VisibleObjects` is camera-facing work after frustum and optional Hi-Z culling.
- `ShadowObjects` is a broader set used to build the shadow BVH so off-screen casters still affect visible receivers.

### `XBrickMap` dirty flags matter

- `StructureDirty`: sectors or bricks were added or removed.
- `DirtySectors` / `DirtyBricks`: value updates that need GPU upload.
- `AABBDirty`: cached bounds need recompute.

Future upload work should preserve those meanings instead of introducing parallel flags.

### Text and gizmos are frame-lifetime data

- Text is cleared in `Prelude` and must be resubmitted every frame.
- Gizmos are rebuilt from ECS each frame and re-uploaded during `Update()`.

One-off debug writes outside the normal frame flow will disappear.

### There are multiple debug knobs

- `App.DebugMode`: enables the debug compute pass and profiler HUD.
- `Camera.DebugMode`: shader-side debug visualization mode passed through camera uniforms.
- `RenderMode`: alternate renderer output mode exposed through the bridge.

If a debug change appears ineffective, confirm you toggled the right control.

### Particles are hybrid, not purely CPU or purely GPU

- Emitters and spawn requests are authored on the CPU side.
- Per-particle sim is GPU-driven.
- The sim path also depends on current scene voxel buffers.
- The bridge currently uses one active atlas source per frame.

## Operational Limits Worth Knowing

- Voxel payload atlas is fixed-size and non-resizable. Exhaustion currently panics.
- Material tables are capped to 256 entries per object and truncated with a warning.
- Particle pool size is currently provisioned from the bridge with `UpdateParticles(1000000, ...)`.
- Spot-shadow updates are budgeted and rotated; they are not all refreshed every frame.
- Hi-Z uses previous-frame data and is suppressed during fast camera motion.

## Common Failure Modes

### Black frame after a scene or resize change

Check:

- `UpdateScene(...)` return path in `app/app_frame.go`
- dependent `Create*BindGroups(...)` calls
- `App.Resize()` resource recreation
- storage texture format and bind-group sample types

### Transparent content missing

Check:

- accumulation pass still includes the expected pipeline
- transparent/particle/sprite/CA bind groups are non-nil
- resolve bind group was recreated after resize
- WBOIT textures exist and match the expected formats

### Occlusion popping or disappearing objects

Check:

- `Scene.Commit(...)` options
- `FastCameraMotion`
- Hi-Z readback state in `gpu/manager_hiz.go`
- `AllowOcclusionCulling` on the object

### Edits visible to picking but not rendering

Check:

- CPU edit path touched the expected `XBrickMap`
- dirty flags were set appropriately
- `UpdateScene(...)` ran afterward
- object allocation still maps to the right `XBrickMap`

### A change works until the window resizes

Check:

- `App.Resize()`
- `BufferManager.CreateGBufferTextures(...)`
- resolve bind-group recreation
- any pipeline or bind group that references swapchain or size-dependent textures

## Verification

Use `VERIFY.md` for the command list. In practice:

- narrow package tests first
- then run the broader `gekko` module tests if the change crosses boundaries
- only use a windowed run when the change actually needs visual inspection
